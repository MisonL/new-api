package service

import (
	"fmt"
	"io"
	"log"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
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
