package service

import (
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func setupChannelSelectTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL
	previousRedisEnabled := common.RedisEnabled
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	previousGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousAutoGroups := setting.GetAutoGroups()
	previousUserUsableGroups := setting.UserUsableGroups2JSONString()

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	common.GroupModelRouteHelperEnabled = false
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["default"]`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"default"}`))

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormlogger.New(log.New(io.Discard, "", 0), gormlogger.Config{
			LogLevel:                  gormlogger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		}),
	})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))

	t.Cleanup(func() {
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
		common.RedisEnabled = previousRedisEnabled
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = previousGroupModelRouteHelperEnabled
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		if err := setting.UpdateAutoGroupsByJsonString(marshalStringSliceForTest(t, previousAutoGroups)); err != nil {
			t.Errorf("failed to restore auto groups: %v", err)
		}
		if err := setting.UpdateUserUsableGroupsByJSONString(previousUserUsableGroups); err != nil {
			t.Errorf("failed to restore user usable groups: %v", err)
		}
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func marshalStringSliceForTest(t *testing.T, values []string) string {
	t.Helper()
	raw, err := common.Marshal(values)
	if err != nil {
		t.Fatalf("failed to marshal string slice: %v", err)
	}
	return string(raw)
}

func TestAutoGroupRetryExcludesUsedChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name               string
		memoryCacheEnabled bool
	}{
		{
			name:               "database fallback",
			memoryCacheEnabled: false,
		},
		{
			name:               "memory cache",
			memoryCacheEnabled: true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			db := setupChannelSelectTestDB(t)
			common.MemoryCacheEnabled = tc.memoryCacheEnabled
			seedAutoGroupRetryChannels(t, db)
			if tc.memoryCacheEnabled {
				model.InitChannelCache()
			}

			assertAutoGroupSelection := func(usedChannels []string, retry int) (*model.Channel, string, error) {
				ctx, _ := gin.CreateTestContext(nil)
				common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
				ctx.Set("use_channel", usedChannels)
				return CacheGetRandomSatisfiedChannel(&RetryParam{
					Ctx:        ctx,
					TokenGroup: "auto",
					ModelName:  "gpt-5.5",
					Retry:      common.GetPointer(retry),
				})
			}

			t.Run("excludes used channel", func(t *testing.T) {
				channel, selectGroup, err := assertAutoGroupSelection([]string{"206"}, 1)
				require.NoError(t, err)
				require.Equal(t, "default", selectGroup)
				require.NotNil(t, channel)
				require.Equal(t, 207, channel.Id)
			})

			t.Run("all channels excluded", func(t *testing.T) {
				channel, selectGroup, err := assertAutoGroupSelection([]string{"206", "207"}, 1)
				require.NoError(t, err)
				require.Equal(t, "auto", selectGroup)
				require.Nil(t, channel)
			})

			t.Run("invalid used channel values ignored", func(t *testing.T) {
				channel, selectGroup, err := assertAutoGroupSelection([]string{"invalid", "-207", "0"}, 0)
				require.NoError(t, err)
				require.Equal(t, "default", selectGroup)
				require.NotNil(t, channel)
				require.Contains(t, []int{206, 207}, channel.Id)
			})

			t.Run("empty used channel list", func(t *testing.T) {
				channel, selectGroup, err := assertAutoGroupSelection([]string{}, 0)
				require.NoError(t, err)
				require.Equal(t, "default", selectGroup)
				require.NotNil(t, channel)
				require.Contains(t, []int{206, 207}, channel.Id)
			})
		})
	}
}

func TestChannelConcurrencyExclusionStaysIndependentFromHealthAndAttempts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("CHANNEL_ROUTING_V2_ENABLED", "false")
	t.Setenv("CHANNEL_ROUTING_SHADOW_ENABLED", "false")

	for _, memoryCacheEnabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("memory_cache_%t", memoryCacheEnabled), func(t *testing.T) {
			db := setupChannelSelectTestDB(t)
			common.MemoryCacheEnabled = memoryCacheEnabled
			highPriority := int64(20)
			lowPriority := int64(10)
			weight := uint(1)
			high := &model.Channel{
				Id:       9931,
				Name:     "concurrency-full-high",
				Type:     constant.ChannelTypeOpenAI,
				Status:   common.ChannelStatusEnabled,
				Group:    "default",
				Models:   "concurrency-independent-model",
				Priority: &highPriority,
				Weight:   &weight,
			}
			low := &model.Channel{
				Id:       9932,
				Name:     "concurrency-available-low",
				Type:     constant.ChannelTypeOpenAI,
				Status:   common.ChannelStatusEnabled,
				Group:    "default",
				Models:   "concurrency-independent-model",
				Priority: &lowPriority,
				Weight:   &weight,
			}
			for _, channel := range []*model.Channel{high, low} {
				require.NoError(t, db.Create(channel).Error)
				require.NoError(t, db.Create(&model.Ability{
					Group:     "default",
					Model:     "concurrency-independent-model",
					ChannelId: channel.Id,
					Enabled:   true,
					Priority:  channel.Priority,
					Weight:    *channel.Weight,
				}).Error)
			}
			if memoryCacheEnabled {
				model.InitChannelCache()
			}

			for _, tokenGroup := range []string{"default", "auto"} {
				t.Run(tokenGroup, func(t *testing.T) {
					ctx, _ := gin.CreateTestContext(nil)
					common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
					serviceCtx := ctx
					ExcludeChannelDueToConcurrency(serviceCtx, high.Id)

					excluded, hasHealthExclusions := getChannelSelectionExcludedChannels(ctx, "default", "concurrency-independent-model")
					require.False(t, hasHealthExclusions)
					_, stillExcluded := excluded[high.Id]
					require.False(t, stillExcluded)
					filter := channelHealthRouteCandidateFilter(ctx, "default", "concurrency-independent-model")
					require.False(t, filter(high, "concurrency-independent-model", "concurrency-independent-model"))
					require.True(t, filter(low, "concurrency-independent-model", "concurrency-independent-model"))

					retry := 0
					selected, selectedGroup, err := CacheGetRandomSatisfiedChannel(&RetryParam{
						Ctx:        ctx,
						TokenGroup: tokenGroup,
						ModelName:  "concurrency-independent-model",
						Retry:      &retry,
					})
					require.NoError(t, err)
					require.NotNil(t, selected)
					require.Equal(t, low.Id, selected.Id)
					if tokenGroup == "auto" {
						require.Equal(t, "default", selectedGroup)
					} else {
						require.Equal(t, tokenGroup, selectedGroup)
					}
					require.Empty(t, ctx.GetStringSlice("use_channel"), "a capacity skip must not be recorded as an upstream attempt")
				})
			}
		})
	}
}

func TestCacheGetRandomSatisfiedChannelRejectsNilContext(t *testing.T) {
	channel, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
	})

	require.Error(t, err)
	require.Nil(t, channel)
	require.Equal(t, "default", group)
	require.ErrorContains(t, err, "context is nil")
}

func TestLegacySelectionSkipsInvalidModelMappingForFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupChannelSelectTestDB(t)
	common.GroupModelRouteHelperEnabled = false

	highPriority := int64(20)
	lowPriority := int64(10)
	weight := uint(1)
	invalidMapping := `{invalid`
	invalid := &model.Channel{
		Id:           701,
		Name:         "invalid-mapping",
		Status:       common.ChannelStatusEnabled,
		Group:        "default",
		Models:       "gpt-5.5",
		Priority:     &highPriority,
		Weight:       &weight,
		ModelMapping: &invalidMapping,
	}
	valid := &model.Channel{
		Id:       702,
		Name:     "valid-fallback",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "gpt-5.5",
		Priority: &lowPriority,
		Weight:   &weight,
	}
	for _, channel := range []*model.Channel{invalid, valid} {
		require.NoError(t, db.Create(channel).Error)
		require.NoError(t, db.Create(&model.Ability{
			Group:     "default",
			Model:     "gpt-5.5",
			ChannelId: channel.Id,
			Enabled:   true,
			Priority:  channel.Priority,
			Weight:    *channel.Weight,
		}).Error)
	}

	ctx, _ := gin.CreateTestContext(nil)
	channel, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.NoError(t, err)
	require.Equal(t, "default", group)
	require.NotNil(t, channel)
	require.Equal(t, valid.Id, channel.Id)
}

func TestChannelRouteModelResolverPinsMappingForRequest(t *testing.T) {
	first := `{"client":"provider/first"}`
	second := `{"client":"provider/second"}`
	channel := &model.Channel{Id: 9901, ModelMapping: &first}
	resolver := newChannelRouteModelResolver()

	modelName, mapped, err := resolver(channel, "client", false)
	require.NoError(t, err)
	require.True(t, mapped)
	require.Equal(t, "provider/first", modelName)

	channel.ModelMapping = &second
	modelName, mapped, err = resolver(channel, "client", false)
	require.NoError(t, err)
	require.True(t, mapped)
	require.Equal(t, "provider/first", modelName)
}

func TestChannelRouteModelResolverIsSafeForConcurrentCallers(t *testing.T) {
	mapping := `{"client":"provider/model"}`
	channel := &model.Channel{Id: 99011, ModelMapping: &mapping}
	resolver := newChannelRouteModelResolver()

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 32; j++ {
				modelName, mapped, err := resolver(channel, "client", false)
				if err != nil || !mapped || modelName != "provider/model" {
					t.Errorf("unexpected resolver result: model=%q mapped=%t err=%v", modelName, mapped, err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestChannelRouteModelResolverIsReusedAcrossSelectionCalls(t *testing.T) {
	first := `{"client":"provider/first"}`
	second := `{"client":"provider/second"}`
	ctx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(ctx, constant.ContextKeyChannelId, 9902)
	common.SetContextKey(ctx, constant.ContextKeyChannelModelMapping, first)
	channel := &model.Channel{Id: 9902, ModelMapping: &first}

	modelName, mapped, err := ResolveChannelModelMappingForContext(ctx, channel, "client", false)
	require.NoError(t, err)
	require.True(t, mapped)
	require.Equal(t, "provider/first", modelName)

	channel.ModelMapping = &second
	common.SetContextKey(ctx, constant.ContextKeyChannelModelMapping, second)
	modelName, mapped, err = ResolveChannelModelMappingForContext(ctx, channel, "client", false)
	require.NoError(t, err)
	require.True(t, mapped)
	require.Equal(t, "provider/first", modelName)
}

func TestResolveChannelModelMappingForContextUsesInitialSelectedChannelSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(ctx, constant.ContextKeyChannelId, 201)
	common.SetContextKey(ctx, constant.ContextKeyChannelModelMapping, `{"client-model":"provider/channel-201"}`)

	// The initial distributor selection intentionally passes a lightweight
	// Channel into the relay path. The authoritative mapping was already
	// installed on the request context by SetupContextForSelectedChannel.
	lightweight := &model.Channel{Id: 201}
	modelName, mapped, err := ResolveChannelModelMappingForContext(ctx, lightweight, "client-model", false)
	require.NoError(t, err)
	require.True(t, mapped)
	require.Equal(t, "provider/channel-201", modelName)

	// A later context mutation must not change the request-scoped snapshot.
	common.SetContextKey(ctx, constant.ContextKeyChannelModelMapping, `{"client-model":"provider/changed"}`)
	modelName, mapped, err = ResolveChannelModelMappingForContext(ctx, lightweight, "client-model", false)
	require.NoError(t, err)
	require.True(t, mapped)
	require.Equal(t, "provider/channel-201", modelName)
}

func TestChannelHealthCandidateFilterRejectsAnyResolverError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	channel := &model.Channel{Id: 9904, Type: constant.ChannelTypeOpenAI}
	resolver := model.RouteModelResolver(func(_ *model.Channel, _ string, _ bool) (string, bool, error) {
		return "", false, errors.New("route resolver unavailable")
	})

	filter := channelHealthCandidateFilter(ctx, "default", "gpt-5.5", resolver)
	require.NotNil(t, filter)
	require.False(t, filter(channel))
}

func TestChannelHealthRouteCandidateFilterUsesEffectiveModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	channel := &model.Channel{Id: 9905, Type: constant.ChannelTypeOpenAI}

	routeA, err := BuildChannelHealthRouteForChannelWithEffectiveModel(ctx, "default", "public-model", channel, "provider/model-a")
	require.NoError(t, err)
	_, err = BuildChannelHealthRouteForChannelWithEffectiveModel(ctx, "default", "public-model", channel, "provider/model-b")
	require.NoError(t, err)
	require.True(t, markChannelHealthSelectionExclusionForRoute(ctx, "default", "public-model", channel.Id, routeA.Fingerprint))

	filter := channelHealthRouteCandidateFilter(ctx, "default", "public-model")
	require.NotNil(t, filter)
	require.False(t, filter(channel, "raw-a", "provider/model-a"))
	require.True(t, filter(channel, "raw-b", "provider/model-b"))
}

func TestChannelRouteModelResolverUpgradesEmptySnapshotOnce(t *testing.T) {
	resolver := newChannelRouteModelResolver()
	channel := &model.Channel{Id: 9904}

	modelName, mapped, err := resolver(channel, "client", false)
	require.NoError(t, err)
	require.False(t, mapped)
	require.Equal(t, "client", modelName)

	mapping := `{"client":"provider/from-context"}`
	channel.ModelMapping = &mapping
	modelName, mapped, err = resolver(channel, "client", false)
	require.NoError(t, err)
	require.True(t, mapped)
	require.Equal(t, "provider/from-context", modelName)

	second := `{"client":"provider/changed"}`
	channel.ModelMapping = &second
	modelName, mapped, err = resolver(channel, "client", false)
	require.NoError(t, err)
	require.True(t, mapped)
	require.Equal(t, "provider/from-context", modelName)
}

func TestChannelRouteModelResolverUpgradesSemanticEmptySnapshotOnce(t *testing.T) {
	for _, emptyMapping := range []string{" ", "{}", " { } "} {
		resolver := newChannelRouteModelResolver()
		channel := &model.Channel{Id: 9914, ModelMapping: &emptyMapping}

		modelName, mapped, err := resolver(channel, "client", false)
		require.NoError(t, err)
		require.False(t, mapped)
		require.Equal(t, "client", modelName)

		mapping := `{"client":"provider/from-context"}`
		channel.ModelMapping = &mapping
		modelName, mapped, err = resolver(channel, "client", false)
		require.NoError(t, err)
		require.True(t, mapped)
		require.Equal(t, "provider/from-context", modelName)

		second := `{"client":"provider/changed"}`
		channel.ModelMapping = &second
		modelName, mapped, err = resolver(channel, "client", false)
		require.NoError(t, err)
		require.True(t, mapped)
		require.Equal(t, "provider/from-context", modelName)
	}
}

func TestRequestScopedChannelRouteModelResolverPinsSettledEmptyMapping(t *testing.T) {
	ctx, _ := gin.CreateTestContext(nil)
	channel := &model.Channel{Id: 9924}
	resolver := newChannelRouteModelResolver(ctx)

	modelName, mapped, err := resolver(channel, "client", false)
	require.NoError(t, err)
	require.False(t, mapped)
	require.Equal(t, "client", modelName)

	mapping := `{"client":"provider/from-edit"}`
	channel.ModelMapping = &mapping
	modelName, mapped, err = resolver(channel, "client", false)
	require.NoError(t, err)
	require.False(t, mapped)
	require.Equal(t, "client", modelName)
}

func TestRequestScopedChannelRouteModelResolverAllowsExplicitContextMappingSupplement(t *testing.T) {
	ctx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(ctx, constant.ContextKeyChannelId, 9925)
	channel := &model.Channel{Id: 9925}
	resolver := newChannelRouteModelResolver(ctx)

	modelName, mapped, err := resolver(channel, "client", false)
	require.NoError(t, err)
	require.False(t, mapped)
	require.Equal(t, "client", modelName)

	mapping := `{"client":"provider/from-context"}`
	common.SetContextKey(ctx, constant.ContextKeyChannelModelMapping, mapping)
	modelName, mapped, err = ResolveChannelModelMappingForContext(ctx, channel, "client", false)
	require.NoError(t, err)
	require.True(t, mapped)
	require.Equal(t, "provider/from-context", modelName)

	changed := `{"client":"provider/changed"}`
	common.SetContextKey(ctx, constant.ContextKeyChannelModelMapping, changed)
	modelName, mapped, err = ResolveChannelModelMappingForContext(ctx, channel, "client", false)
	require.NoError(t, err)
	require.True(t, mapped)
	require.Equal(t, "provider/from-context", modelName)
}

func TestResolveChannelModelMappingForContextUsesContextForSemanticEmptyChannelMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(ctx, constant.ContextKeyChannelId, 9926)
	common.SetContextKey(ctx, constant.ContextKeyChannelModelMapping, `{"client":"provider/from-context"}`)
	channelMapping := "{}"
	channel := &model.Channel{Id: 9926, ModelMapping: &channelMapping}

	modelName, mapped, err := ResolveChannelModelMappingForContext(ctx, channel, "client", false)
	require.NoError(t, err)
	require.True(t, mapped)
	require.Equal(t, "provider/from-context", modelName)

	// Once the context snapshot is settled, a later edit of either source must
	// not change the route selected for this request.
	changed := `{"client":"provider/changed"}`
	channel.ModelMapping = &changed
	common.SetContextKey(ctx, constant.ContextKeyChannelModelMapping, changed)
	modelName, mapped, err = ResolveChannelModelMappingForContext(ctx, channel, "client", false)
	require.NoError(t, err)
	require.True(t, mapped)
	require.Equal(t, "provider/from-context", modelName)
}

func TestResolveChannelModelMappingForContextDoesNotOverrideInvalidChannelMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(ctx, constant.ContextKeyChannelId, 9927)
	common.SetContextKey(ctx, constant.ContextKeyChannelModelMapping, `{"client":"provider/from-context"}`)
	channelMapping := "{invalid"
	channel := &model.Channel{Id: 9927, ModelMapping: &channelMapping}

	_, _, err := ResolveChannelModelMappingForContext(ctx, channel, "client", false)
	require.Error(t, err)
	require.True(t, model.IsChannelModelMappingError(err))
}

func TestRequestScopedChannelRouteModelResolverUsesContextMappingForSemanticEmptyChannelMapping(t *testing.T) {
	for _, emptyMapping := range []string{"{}", " { } "} {
		t.Run(emptyMapping, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(nil)
			common.SetContextKey(ctx, constant.ContextKeyChannelId, 9926)
			common.SetContextKey(ctx, constant.ContextKeyChannelModelMapping, `{"client":"provider/from-context"}`)
			channel := &model.Channel{Id: 9926, ModelMapping: &emptyMapping}

			modelName, mapped, err := ResolveChannelModelMappingForContext(ctx, channel, "client", false)
			require.NoError(t, err)
			require.True(t, mapped)
			require.Equal(t, "provider/from-context", modelName)
		})
	}
}

func TestDatabaseSelectionUsesRequestScopedRouteModelResolver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("CHANNEL_ROUTING_V2_ENABLED", "false")
	t.Setenv("CHANNEL_ROUTING_SHADOW_ENABLED", "false")
	db := setupChannelSelectTestDB(t)

	priority := int64(10)
	weight := uint(1)
	invalidMapping := `{invalid`
	channel := &model.Channel{
		Id:           9903,
		Name:         "request-snapshot-mapping",
		Status:       common.ChannelStatusEnabled,
		Group:        "default",
		Models:       "client",
		Priority:     &priority,
		Weight:       &weight,
		ModelMapping: &invalidMapping,
	}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "default",
		Model:     "client",
		ChannelId: channel.Id,
		Enabled:   true,
		Priority:  &priority,
		Weight:    weight,
	}).Error)

	resolverCalls := 0
	resolver := model.RouteModelResolver(func(_ *model.Channel, requested string, _ bool) (string, bool, error) {
		resolverCalls++
		return "provider/first", requested != "", nil
	})
	selected, err := model.GetRandomSatisfiedChannelWithOptions(
		"default",
		"client",
		0,
		nil,
		model.SatisfiedChannelSelectionOptions{RouteModelResolver: resolver},
	)
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, channel.Id, selected.Id)
	require.Positive(t, resolverCalls)
}

func TestCompactSelectionRecordsNamespacePreference(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, memoryCacheEnabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("memory_cache_%t", memoryCacheEnabled), func(t *testing.T) {
			db := setupChannelSelectTestDB(t)
			common.MemoryCacheEnabled = memoryCacheEnabled
			common.GroupModelRouteHelperEnabled = true
			highPriority := int64(20)
			lowPriority := int64(10)
			normal := &model.Channel{
				Id:       301,
				Type:     constant.ChannelTypeOpenAI,
				Name:     "compact-normal",
				Status:   common.ChannelStatusEnabled,
				Group:    "default",
				Models:   "gpt-5.5",
				Priority: &highPriority,
			}
			preferred := &model.Channel{
				Id:       302,
				Type:     constant.ChannelTypeOpenAI,
				Name:     "compact-namespace-unsupported",
				Status:   common.ChannelStatusEnabled,
				Group:    "default",
				Models:   "gpt-5.5",
				Priority: &lowPriority,
			}
			normal.SetOtherSettings(dto.ChannelOtherSettings{ResponsesCompactMode: dto.ResponsesCompactModeNative})
			preferred.SetOtherSettings(dto.ChannelOtherSettings{
				ResponsesCompactMode:     dto.ResponsesCompactModeSynthetic,
				ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileGenericProxy,
			})
			for _, channel := range []*model.Channel{normal, preferred} {
				require.NoError(t, db.Create(channel).Error)
				require.NoError(t, db.Create(&model.Ability{
					Group:     "default",
					Model:     "gpt-5.5",
					ChannelId: channel.Id,
					Enabled:   true,
					Priority:  channel.Priority,
				}).Error)
			}
			if memoryCacheEnabled {
				model.InitChannelCache()
			}

			ctx, _ := gin.CreateTestContext(nil)
			channel, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
				Ctx:        ctx,
				TokenGroup: "default",
				ModelName:  ratio_setting.WithCompactModelSuffix("gpt-5.5"),
				Retry:      common.GetPointer(0),
			})
			require.NoError(t, err)
			require.Equal(t, "default", group)
			require.NotNil(t, channel)
			require.Equal(t, preferred.Id, channel.Id)
			require.Equal(t, responsesCompactRouteDecisionNamespaceUnsupportedPreferred, common.GetContextKeyString(ctx, constant.ContextKeyResponsesCompactRouteDecision))

			ctx.Set("use_channel", []string{fmt.Sprintf("%d", preferred.Id)})
			channel, group, err = CacheGetRandomSatisfiedChannel(&RetryParam{
				Ctx:        ctx,
				TokenGroup: "default",
				ModelName:  ratio_setting.WithCompactModelSuffix("gpt-5.5"),
				Retry:      common.GetPointer(1),
			})
			require.NoError(t, err)
			require.Equal(t, "default", group)
			require.NotNil(t, channel)
			require.Equal(t, normal.Id, channel.Id)
			require.Equal(t, responsesCompactRouteDecisionNamespaceUnsupportedPreferredFallback, common.GetContextKeyString(ctx, constant.ContextKeyResponsesCompactRouteDecision))
		})
	}
}

func TestRetryExhaustsSamePriorityBeforeDescending(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name               string
		tokenGroup         string
		memoryCacheEnabled bool
	}{
		{name: "database group", tokenGroup: "default", memoryCacheEnabled: false},
		{name: "database auto group", tokenGroup: "auto", memoryCacheEnabled: false},
		{name: "memory cache group", tokenGroup: "default", memoryCacheEnabled: true},
		{name: "memory cache auto group", tokenGroup: "auto", memoryCacheEnabled: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupChannelSelectTestDB(t)
			common.MemoryCacheEnabled = tc.memoryCacheEnabled
			seedPriorityRetryChannels(t, db)
			if tc.memoryCacheEnabled {
				model.InitChannelCache()
			}

			selectChannel := func(usedChannels []string) (*model.Channel, string, error) {
				ctx, _ := gin.CreateTestContext(nil)
				common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
				ctx.Set("use_channel", usedChannels)
				return CacheGetRandomSatisfiedChannel(&RetryParam{
					Ctx:        ctx,
					TokenGroup: tc.tokenGroup,
					ModelName:  "gpt-5.5",
					Retry:      common.GetPointer(1),
				})
			}

			channel, selectGroup, err := selectChannel([]string{"206"})
			require.NoError(t, err)
			require.Equal(t, "default", selectGroup)
			require.NotNil(t, channel)
			require.Equal(t, 207, channel.Id)

			channel, selectGroup, err = selectChannel([]string{"206", "207"})
			require.NoError(t, err)
			require.Equal(t, "default", selectGroup)
			require.NotNil(t, channel)
			require.Equal(t, 208, channel.Id)
		})
	}
}

func TestRetrySelectsHighestRemainingChannelAfterMultipleExcludedPriorities(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name               string
		tokenGroup         string
		memoryCacheEnabled bool
	}{
		{name: "database group", tokenGroup: "default", memoryCacheEnabled: false},
		{name: "database auto group", tokenGroup: "auto", memoryCacheEnabled: false},
		{name: "memory cache group", tokenGroup: "default", memoryCacheEnabled: true},
		{name: "memory cache auto group", tokenGroup: "auto", memoryCacheEnabled: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupChannelSelectTestDB(t)
			common.MemoryCacheEnabled = tc.memoryCacheEnabled
			seedMultipleExcludedPriorityChannels(t, db)
			if tc.memoryCacheEnabled {
				model.InitChannelCache()
			}

			ctx, _ := gin.CreateTestContext(nil)
			common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
			ctx.Set("use_channel", []string{"216", "217", "218"})

			channel, selectGroup, err := CacheGetRandomSatisfiedChannel(&RetryParam{
				Ctx:        ctx,
				TokenGroup: tc.tokenGroup,
				ModelName:  "gpt-5.5",
				Retry:      common.GetPointer(1),
			})

			require.NoError(t, err)
			require.Equal(t, "default", selectGroup)
			require.NotNil(t, channel)
			require.Equal(t, 219, channel.Id)
		})
	}
}

func TestAutoGroupSelectionPropagatesChannelLookupError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupChannelSelectTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	ctx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")

	channel, selectGroup, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx:        ctx,
		TokenGroup: "auto",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})
	require.Error(t, err)
	require.Nil(t, channel)
	require.Equal(t, "default", selectGroup)
}

func TestRequestBodyLimitExcludesOversizedChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name               string
		memoryCacheEnabled bool
	}{
		{
			name:               "database fallback",
			memoryCacheEnabled: false,
		},
		{
			name:               "memory cache",
			memoryCacheEnabled: true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			db := setupChannelSelectTestDB(t)
			common.MemoryCacheEnabled = tc.memoryCacheEnabled
			previousPolicy := model_setting.GetGlobalSettings().RequestBodyLimitPolicy
			model_setting.GetGlobalSettings().RequestBodyLimitPolicy = model_setting.RequestBodyLimitPolicy{Enabled: true}
			t.Cleanup(func() {
				model_setting.GetGlobalSettings().RequestBodyLimitPolicy = previousPolicy
			})

			seedAutoGroupRetryChannels(t, db)
			limitedSettings := dto.ChannelOtherSettings{
				RequestBodyLimit: &dto.ChannelRequestBodyLimit{
					MaxBytes:   100_000,
					ObservedAt: time.Now().UTC().Unix(),
					Source:     "upstream_413",
					Reason:     "request body too large",
				},
			}
			rawSettings, err := common.Marshal(limitedSettings)
			require.NoError(t, err)
			require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 206).Update("settings", string(rawSettings)).Error)

			if tc.memoryCacheEnabled {
				model.InitChannelCache()
			}

			ctx, _ := gin.CreateTestContext(nil)
			common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
			common.SetContextKey(ctx, constant.ContextKeyRequestBodySize, int64(128_000))

			channel, selectGroup, err := CacheGetRandomSatisfiedChannel(&RetryParam{
				Ctx:        ctx,
				TokenGroup: "auto",
				ModelName:  "gpt-5.5",
				Retry:      common.GetPointer(0),
			})

			require.NoError(t, err)
			require.Equal(t, "default", selectGroup)
			require.NotNil(t, channel)
			require.Equal(t, 207, channel.Id)
		})
	}
}

func seedAutoGroupRetryChannels(t *testing.T, db *gorm.DB) {
	t.Helper()
	rateLimitedPriority := int64(10)
	fallbackPriority := int64(10)
	rateLimitedWeight := uint(1)
	fallbackWeight := uint(1)
	channels := []*model.Channel{
		{Id: 206, Name: "rate-limited", Status: common.ChannelStatusEnabled, Group: "default", Models: "gpt-5.5", Priority: &rateLimitedPriority, Weight: &rateLimitedWeight},
		{Id: 207, Name: "fallback", Status: common.ChannelStatusEnabled, Group: "default", Models: "gpt-5.5", Priority: &fallbackPriority, Weight: &fallbackWeight},
	}
	for _, channel := range channels {
		require.NoError(t, db.Create(channel).Error)
		require.NoError(t, db.Create(&model.Ability{
			Group:     "default",
			Model:     "gpt-5.5",
			ChannelId: channel.Id,
			Enabled:   true,
			Priority:  channel.Priority,
			Weight:    *channel.Weight,
		}).Error)
	}
}

func seedPriorityRetryChannels(t *testing.T, db *gorm.DB) {
	t.Helper()
	highPriority := int64(20)
	lowPriority := int64(10)
	weight := uint(1)
	channels := []*model.Channel{
		{Id: 206, Name: "high-a", Status: common.ChannelStatusEnabled, Group: "default", Models: "gpt-5.5", Priority: &highPriority, Weight: &weight},
		{Id: 207, Name: "high-b", Status: common.ChannelStatusEnabled, Group: "default", Models: "gpt-5.5", Priority: &highPriority, Weight: &weight},
		{Id: 208, Name: "low", Status: common.ChannelStatusEnabled, Group: "default", Models: "gpt-5.5", Priority: &lowPriority, Weight: &weight},
	}
	for _, channel := range channels {
		require.NoError(t, db.Create(channel).Error)
		require.NoError(t, db.Create(&model.Ability{
			Group:     "default",
			Model:     "gpt-5.5",
			ChannelId: channel.Id,
			Enabled:   true,
			Priority:  channel.Priority,
			Weight:    *channel.Weight,
		}).Error)
	}
}

func seedMultipleExcludedPriorityChannels(t *testing.T, db *gorm.DB) {
	t.Helper()
	priorities := map[int]int64{
		216: 40,
		217: 30,
		218: 20,
		219: 20,
		220: 10,
	}
	weight := uint(1)
	for id, priority := range priorities {
		channel := &model.Channel{
			Id:       id,
			Name:     fmt.Sprintf("priority-%d", id),
			Status:   common.ChannelStatusEnabled,
			Group:    "default",
			Models:   "gpt-5.5",
			Priority: common.GetPointer(priority),
			Weight:   &weight,
		}
		require.NoError(t, db.Create(channel).Error)
		require.NoError(t, db.Create(&model.Ability{
			Group:     "default",
			Model:     "gpt-5.5",
			ChannelId: channel.Id,
			Enabled:   true,
			Priority:  channel.Priority,
			Weight:    weight,
		}).Error)
	}
}
