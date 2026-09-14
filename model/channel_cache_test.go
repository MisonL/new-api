package model

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func prepareChannelCacheTest(t *testing.T) {
	t.Helper()
	initCol()
	require.NoError(t, DB.AutoMigrate(&Ability{}))
	require.NoError(t, DB.Exec("DELETE FROM abilities").Error)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)

	channelSyncLock.Lock()
	group2model2channels = nil
	legacyRouteCandidateCache = nil
	channelsIDM = nil
	routeCandidateCacheV2 = nil
	routeCandidateCacheV2Ready = false
	routeCandidateCacheV2KnownByChannel = nil
	channelSyncLock.Unlock()
	channelCacheRefreshInFlight.Store(false)
	channelCacheRefreshPending.Store(false)
}

func TestUpdateChannelPinOrdersRefreshesMemoryCache(t *testing.T) {
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
	})
	common.MemoryCacheEnabled = true
	prepareChannelCacheTest(t)

	channelID := 90701
	initialPinOrder := int64(0)
	require.NoError(t, DB.Create(&Channel{
		Id:       channelID,
		Name:     "pin-cache-refresh",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "pin-cache-model",
		PinOrder: &initialPinOrder,
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "pin-cache-model",
		ChannelId: channelID,
		Enabled:   true,
	}).Error)

	InitChannelCache()
	cached, err := CacheGetChannel(channelID)
	require.NoError(t, err)
	require.Equal(t, initialPinOrder, cached.GetPinOrder())

	updatedPinOrder := int64(42)
	require.NoError(t, UpdateChannelPinOrders([]ChannelPinOrder{{
		Id:       channelID,
		PinOrder: &updatedPinOrder,
	}}))

	cached, err = CacheGetChannel(channelID)
	require.NoError(t, err)
	require.Equal(t, updatedPinOrder, cached.GetPinOrder())
}

func TestInitChannelCacheDoesNotDropPendingRefreshAfterConcurrentRun(t *testing.T) {
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
	})
	common.MemoryCacheEnabled = true
	prepareChannelCacheTest(t)

	channelID := 90702
	require.NoError(t, DB.Create(&Channel{
		Id:     channelID,
		Name:   "pending-cache-refresh",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "pending-cache-model",
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "pending-cache-model",
		ChannelId: channelID,
		Enabled:   true,
	}).Error)

	channelCacheRefreshInFlight.Store(true)
	channelCacheRefreshPending.Store(false)
	done := make(chan struct{})
	go func() {
		InitChannelCache()
		close(done)
	}()
	require.Eventually(t, channelCacheRefreshPending.Load, time.Second, time.Millisecond)
	channelCacheRefreshInFlight.Store(false)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for pending channel cache refresh")
	}

	cached, err := CacheGetChannel(channelID)
	require.NoError(t, err)
	require.Equal(t, channelID, cached.Id)
}

func TestCacheReloadChannelDoesNotPublishStaleConcurrentRead(t *testing.T) {
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	previousDB := DB
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		DB = previousDB
	})
	common.MemoryCacheEnabled = true
	prepareChannelCacheTest(t)

	channelID := 90703
	require.NoError(t, DB.Create(&Channel{
		Id:     channelID,
		Name:   "reload-before",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "reload-model",
	}).Error)
	InitChannelCache()

	entered := make(chan struct{})
	release := make(chan struct{})
	var channelQueryCount int32
	const callbackName = "test:cache_reload_channel_stale_read"
	require.NoError(t, DB.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Name != "Channel" {
			return
		}
		if atomic.AddInt32(&channelQueryCount, 1) != 1 {
			return
		}
		close(entered)
		<-release
	}))
	t.Cleanup(func() {
		_ = DB.Callback().Query().Remove(callbackName)
	})

	staleReloadDone := make(chan struct{})
	go func() {
		CacheReloadChannel(channelID)
		close(staleReloadDone)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stale channel read")
	}

	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channelID).Update("name", "reload-after").Error)
	CacheReloadChannel(channelID)
	cached, err := CacheGetChannel(channelID)
	require.NoError(t, err)
	require.Equal(t, "reload-after", cached.Name)

	close(release)
	select {
	case <-staleReloadDone:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stale channel reload")
	}

	cached, err = CacheGetChannel(channelID)
	require.NoError(t, err)
	require.Equal(t, "reload-after", cached.Name)
}

func TestCacheReloadChannelDoesNotResurrectDeletedChannel(t *testing.T) {
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	previousDB := DB
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		DB = previousDB
	})
	common.MemoryCacheEnabled = true
	prepareChannelCacheTest(t)

	channelID := 90704
	require.NoError(t, DB.Create(&Channel{
		Id:     channelID,
		Name:   "reload-deleted",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "reload-deleted-model",
	}).Error)

	entered := make(chan struct{})
	release := make(chan struct{})
	var channelQueryCount int32
	const callbackName = "test:cache_reload_channel_deleted_read"
	require.NoError(t, DB.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Name != "Channel" {
			return
		}
		if atomic.AddInt32(&channelQueryCount, 1) != 1 {
			return
		}
		close(entered)
		<-release
	}))
	t.Cleanup(func() {
		_ = DB.Callback().Query().Remove(callbackName)
	})

	staleReloadDone := make(chan struct{})
	go func() {
		CacheReloadChannel(channelID)
		close(staleReloadDone)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for deleted channel read")
	}

	require.NoError(t, DB.Delete(&Channel{}, channelID).Error)
	CacheReloadChannel(channelID)
	close(release)
	select {
	case <-staleReloadDone:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for deleted channel reload")
	}

	_, err := CacheGetChannel(channelID)
	require.Error(t, err)
}

func TestGetRandomSatisfiedChannelFallsBackToDatabaseOnCacheMiss(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	channel := &Channel{
		Id:     101,
		Name:   "fallback-channel",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "other-model",
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-5.4",
		ChannelId: channel.Id,
		Enabled:   true,
	}).Error)

	got, err := GetRandomSatisfiedChannel("default", "gpt-5.4", 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, channel.Id, got.Id)

	require.Eventually(t, func() bool {
		channelSyncLock.RLock()
		defer channelSyncLock.RUnlock()
		return isChannelIDInList(group2model2channels["default"]["gpt-5.4"], channel.Id)
	}, time.Second, 20*time.Millisecond)
}

func TestLegacyRouteCandidateCacheDoesNotFallbackAfterInitialization(t *testing.T) {
	prepareChannelCacheTest(t)

	channel := &Channel{
		Id:     103,
		Status: common.ChannelStatusEnabled,
	}

	channelSyncLock.Lock()
	channelsIDM = map[int]*Channel{channel.Id: channel}
	// The historical index may still contain a stale entry while a metadata
	// cache refresh has already published its complete key space.
	group2model2channels = map[string]map[string][]int{
		"default": {"current-model": {channel.Id}},
	}
	legacyRouteCandidateCache = map[string][]legacyRouteCandidateCacheEntry{
		routeCandidateCacheKey("default", "other-model"): {{channel: channel}},
	}
	channelSyncLock.Unlock()

	entries, hit := getLegacyRouteCandidateCacheEntries("default", routeModelCandidate{model: "current-model"})
	require.False(t, hit)
	require.Empty(t, entries)

	entries, hit = getLegacyRouteCandidateCacheEntries("default", routeModelCandidate{model: "other-model"})
	require.True(t, hit)
	require.Len(t, entries, 1)
}

func TestGetRandomSatisfiedChannelFallsBackToDatabaseWhenCacheExcluded(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	limitedChannel := &Channel{
		Id:       201,
		Name:     "limited-cache-channel",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "gpt-5.4",
		Priority: common.GetPointer[int64](10),
		Weight:   common.GetPointer[uint](1),
	}
	require.NoError(t, DB.Create(limitedChannel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-5.4",
		ChannelId: limitedChannel.Id,
		Enabled:   true,
		Priority:  limitedChannel.Priority,
		Weight:    *limitedChannel.Weight,
	}).Error)

	InitChannelCache()

	fallbackChannel := &Channel{
		Id:       202,
		Name:     "database-fallback-channel",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "gpt-5.4",
		Priority: common.GetPointer[int64](10),
		Weight:   common.GetPointer[uint](1),
	}
	require.NoError(t, DB.Create(fallbackChannel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-5.4",
		ChannelId: fallbackChannel.Id,
		Enabled:   true,
		Priority:  fallbackChannel.Priority,
		Weight:    *fallbackChannel.Weight,
	}).Error)

	got, err := GetRandomSatisfiedChannelExcluding("default", "gpt-5.4", 0, map[int]struct{}{
		limitedChannel.Id: {},
	})

	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, fallbackChannel.Id, got.Id)

	require.Eventually(t, func() bool {
		channelSyncLock.RLock()
		defer channelSyncLock.RUnlock()
		return isChannelIDInList(group2model2channels["default"]["gpt-5.4"], fallbackChannel.Id)
	}, time.Second, 20*time.Millisecond)
}

func TestLegacySelectionSkipsDisabledChannelsOnDatabaseFallback(t *testing.T) {
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	previousGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.GroupModelRouteHelperEnabled = false
	t.Setenv("CHANNEL_ROUTING_V2_ENABLED", "false")
	t.Setenv("CHANNEL_ROUTING_SHADOW_ENABLED", "false")
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = previousGroupModelRouteHelperEnabled
	})

	for _, memoryCacheEnabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("memory_cache_%t", memoryCacheEnabled), func(t *testing.T) {
			prepareChannelCacheTest(t)
			common.MemoryCacheEnabled = memoryCacheEnabled

			highPriority := int64(30)
			lowPriority := int64(10)
			weight := uint(1)
			high := &Channel{
				Id:       90608,
				Name:     "disabled-database-fallback-high",
				Status:   common.ChannelStatusManuallyDisabled,
				Group:    "default",
				Models:   "disabled-database-fallback-model",
				Priority: &highPriority,
				Weight:   &weight,
			}
			low := &Channel{
				Id:       90609,
				Name:     "enabled-database-fallback-low",
				Status:   common.ChannelStatusEnabled,
				Group:    "default",
				Models:   "disabled-database-fallback-model",
				Priority: &lowPriority,
				Weight:   &weight,
			}
			require.NoError(t, DB.Create(high).Error)
			require.NoError(t, DB.Create(&Ability{
				Group:     "default",
				Model:     "disabled-database-fallback-model",
				ChannelId: high.Id,
				Enabled:   true,
				Priority:  &highPriority,
				Weight:    weight,
			}).Error)

			if memoryCacheEnabled {
				InitChannelCache()
			}

			require.NoError(t, DB.Create(low).Error)
			require.NoError(t, DB.Create(&Ability{
				Group:     "default",
				Model:     "disabled-database-fallback-model",
				ChannelId: low.Id,
				Enabled:   true,
				Priority:  &lowPriority,
				Weight:    weight,
			}).Error)

			selected, err := GetRandomSatisfiedChannelExcluding(
				"default",
				"disabled-database-fallback-model",
				0,
				map[int]struct{}{high.Id: {}},
			)
			require.NoError(t, err)
			require.NotNil(t, selected)
			require.Equal(t, low.Id, selected.Id)
		})
	}
}

func TestV2RouteSelectionFallsBackWhenOneCandidateCacheKeyIsMissing(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = true
	t.Setenv("CHANNEL_ROUTING_V2_ENABLED", "true")
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	exact := &Channel{
		Id:     901,
		Name:   "exact-route-cache",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "OpenAI/gpt-4o",
	}
	normalized := &Channel{
		Id:     902,
		Name:   "normalized-route-db",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-4o",
	}
	require.NoError(t, DB.Create(exact).Error)
	require.NoError(t, DB.Create(normalized).Error)
	priority := int64(10)
	weight := uint(RouteWeightTotalBPS)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "OpenAI/gpt-4o", ChannelId: exact.Id, Enabled: true, RoutePriority: &priority, RouteWeightBPS: &weight, RoutePriorityConfigured: true, RouteWeightBPSConfigured: true}).Error)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "gpt-4o", ChannelId: normalized.Id, Enabled: true, RoutePriority: &priority, RouteWeightBPS: &weight, RoutePriorityConfigured: true, RouteWeightBPSConfigured: true}).Error)

	channelSyncLock.Lock()
	// The V2 completeness index is independent from the legacy candidate view.
	group2model2channels = nil
	channelsIDM = map[int]*Channel{
		exact.Id: exact.CloneForCache(),
	}
	routeCandidateCacheV2 = map[string][]routeCandidateV2{
		routeCandidateCacheKey("default", "OpenAI/gpt-4o"): {{
			Group:               "default",
			RequestedModel:      "OpenAI/gpt-4o",
			EffectiveRouteModel: "OpenAI/gpt-4o",
			Channel:             exact.CloneForCache(),
			RoutePriority:       10,
			RouteWeightBPS:      RouteWeightTotalBPS,
		}},
	}
	routeCandidateCacheV2KnownByChannel = map[string]map[int]struct{}{
		routeCandidateCacheKey("default", "OpenAI/gpt-4o"): {exact.Id: {}},
		routeCandidateCacheKey("default", "gpt-4o"):        {normalized.Id: {}},
	}
	routeCandidateCacheV2Ready = true
	channelSyncLock.Unlock()

	diagnostics := &dto.ChannelRouteSelectionDiagnostics{}
	selected, err := GetRandomSatisfiedChannelWithOptions(
		"default",
		"OpenAI/gpt-4o",
		0,
		nil,
		SatisfiedChannelSelectionOptions{Diagnostics: diagnostics},
	)
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Contains(t, diagnostics.CandidateChannelIDs, exact.Id)
	require.Contains(t, diagnostics.CandidateChannelIDs, normalized.Id)
	require.Equal(t, "database", diagnostics.SelectionPath)
}

func TestV2RouteSelectionFallsBackFromInvalidCachedMappingToValidDatabaseCandidate(t *testing.T) {
	prepareChannelCacheTest(t)

	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	previousGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = true
	t.Setenv("CHANNEL_ROUTING_V2_ENABLED", "true")
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = previousGroupModelRouteHelperEnabled
	})

	modelName := "cache-invalid-mapping-fallback-model"
	highPriority := int64(20)
	lowPriority := int64(10)
	routeWeight := uint(RouteWeightTotalBPS)
	invalidMapping := "{"
	high := &Channel{
		Id:           903,
		Name:         "invalid-cached-mapping-high",
		Status:       common.ChannelStatusEnabled,
		Group:        "default",
		Models:       modelName,
		ModelMapping: &invalidMapping,
	}
	low := &Channel{
		Id:     904,
		Name:   "valid-database-fallback-low",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: modelName,
	}
	require.NoError(t, DB.Create(high).Error)
	require.NoError(t, DB.Create(low).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:                    "default",
		Model:                    modelName,
		ChannelId:                high.Id,
		Enabled:                  true,
		RoutePriority:            &highPriority,
		RouteWeightBPS:           &routeWeight,
		RoutePriorityConfigured:  true,
		RouteWeightBPSConfigured: true,
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:                    "default",
		Model:                    modelName,
		ChannelId:                low.Id,
		Enabled:                  true,
		RoutePriority:            &lowPriority,
		RouteWeightBPS:           &routeWeight,
		RoutePriorityConfigured:  true,
		RouteWeightBPSConfigured: true,
	}).Error)

	cacheKey := routeCandidateCacheKey("default", modelName)
	channelSyncLock.Lock()
	group2model2channels = nil
	channelsIDM = map[int]*Channel{
		high.Id: high.CloneForCache(),
		low.Id:  low.CloneForCache(),
	}
	routeCandidateCacheV2 = map[string][]routeCandidateV2{
		cacheKey: {{
			Group:               "default",
			RequestedModel:      modelName,
			EffectiveRouteModel: "provider/invalid-cached-model",
			Channel:             high.CloneForCache(),
			RoutePriority:       highPriority,
			RouteWeightBPS:      routeWeight,
		}},
	}
	// The completeness index says that the requested key has a cached
	// candidate. Its invalid mapping must therefore make the snapshot
	// incomplete instead of being treated as an empty, authoritative hit.
	routeCandidateCacheV2KnownByChannel = map[string]map[int]struct{}{
		cacheKey: {high.Id: {}},
	}
	routeCandidateCacheV2Ready = true
	channelSyncLock.Unlock()

	diagnostics := &dto.ChannelRouteSelectionDiagnostics{}
	selected, err := GetRandomSatisfiedChannelWithOptions(
		"default",
		modelName,
		0,
		nil,
		SatisfiedChannelSelectionOptions{Diagnostics: diagnostics},
	)
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, low.Id, selected.Id)
	require.Equal(t, "database", diagnostics.SelectionPath)
	require.Equal(t, low.Id, diagnostics.SelectedChannelID)
	require.Equal(t, modelName, diagnostics.SelectedCandidateModel)
	require.Equal(t, modelName, diagnostics.EffectiveRouteModel)
	require.Equal(t, lowPriority, diagnostics.RoutePriority)
}

func TestV2RouteSelectionFallsBackWhenKnownChannelSetGrowsBehindCachedSlice(t *testing.T) {
	prepareChannelCacheTest(t)

	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	previousGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = true
	t.Setenv("CHANNEL_ROUTING_V2_ENABLED", "true")
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = previousGroupModelRouteHelperEnabled
	})

	modelName := "cache-known-channel-set-growth-model"
	oldPriority := int64(20)
	newPriority := int64(10)
	routeWeight := uint(RouteWeightTotalBPS)
	oldChannel := &Channel{
		Id:     905,
		Name:   "stale-route-cache-member",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: modelName,
	}
	newChannel := &Channel{
		Id:     906,
		Name:   "new-route-cache-member",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: modelName,
	}
	require.NoError(t, DB.Create(oldChannel).Error)
	require.NoError(t, DB.Create(newChannel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:                    "default",
		Model:                    modelName,
		ChannelId:                oldChannel.Id,
		Enabled:                  true,
		RoutePriority:            &oldPriority,
		RouteWeightBPS:           &routeWeight,
		RoutePriorityConfigured:  true,
		RouteWeightBPSConfigured: true,
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:                    "default",
		Model:                    modelName,
		ChannelId:                newChannel.Id,
		Enabled:                  true,
		RoutePriority:            &newPriority,
		RouteWeightBPS:           &routeWeight,
		RoutePriorityConfigured:  true,
		RouteWeightBPSConfigured: true,
	}).Error)

	cacheKey := routeCandidateCacheKey("default", modelName)
	channelSyncLock.Lock()
	group2model2channels = nil
	channelsIDM = map[int]*Channel{
		oldChannel.Id: oldChannel.CloneForCache(),
		newChannel.Id: newChannel.CloneForCache(),
	}
	// Simulate an incremental update that published the completeness index for
	// the new channel before the stale cache slice was appended.
	routeCandidateCacheV2 = map[string][]routeCandidateV2{
		cacheKey: {{
			Group:               "default",
			RequestedModel:      modelName,
			EffectiveRouteModel: modelName,
			Channel:             oldChannel.CloneForCache(),
			RoutePriority:       oldPriority,
			RouteWeightBPS:      routeWeight,
		}},
	}
	routeCandidateCacheV2KnownByChannel = map[string]map[int]struct{}{
		cacheKey: {oldChannel.Id: {}, newChannel.Id: {}},
	}
	routeCandidateCacheV2Ready = true
	channelSyncLock.Unlock()

	diagnostics := &dto.ChannelRouteSelectionDiagnostics{}
	selected, err := GetRandomSatisfiedChannelWithOptions(
		"default",
		modelName,
		0,
		nil,
		SatisfiedChannelSelectionOptions{Diagnostics: diagnostics},
	)
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, oldChannel.Id, selected.Id)
	require.Equal(t, "database", diagnostics.SelectionPath)
	require.ElementsMatch(t, []int{oldChannel.Id, newChannel.Id}, diagnostics.CandidateChannelIDs)
}

func TestV2RouteSelectionDiagnosticsIncludeBodyLimitExclusions(t *testing.T) {
	prepareChannelCacheTest(t)

	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	previousGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = false
	t.Setenv("CHANNEL_ROUTING_V2_ENABLED", "true")
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = previousGroupModelRouteHelperEnabled
	})

	modelName := "gpt-v2-body-limit-diagnostic"
	now := time.Now().UTC()
	priority := int64(10)
	legacyWeight := uint(1)
	routeWeight := uint(RouteWeightTotalBPS)
	settings := dto.ChannelOtherSettings{
		RequestBodyLimit: &dto.ChannelRequestBodyLimit{
			MaxBytes:   100,
			ObservedAt: now.Unix(),
			Source:     "upstream_413",
			Reason:     "request body too large",
		},
	}
	rawSettings, err := common.Marshal(settings)
	require.NoError(t, err)
	channel := &Channel{
		Id:            90602,
		Name:          "v2-body-limit-diagnostic",
		Status:        common.ChannelStatusEnabled,
		Group:         "default",
		Models:        modelName,
		Priority:      &priority,
		Weight:        &legacyWeight,
		OtherSettings: string(rawSettings),
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:                    "default",
		Model:                    modelName,
		ChannelId:                channel.Id,
		Enabled:                  true,
		Priority:                 &priority,
		Weight:                   legacyWeight,
		RoutePriority:            &priority,
		RouteWeightBPS:           &routeWeight,
		RoutePriorityConfigured:  true,
		RouteWeightBPSConfigured: true,
	}).Error)

	diagnostics := &dto.ChannelRouteSelectionDiagnostics{}
	selected, err := GetRandomSatisfiedChannelWithOptions(
		"default",
		modelName,
		0,
		nil,
		SatisfiedChannelSelectionOptions{
			RequestBodySize:       128,
			BodyLimitTTLHours:     0,
			Now:                   now,
			ExcludeBeforePriority: true,
			Diagnostics:           diagnostics,
		},
	)
	require.NoError(t, err)
	require.Nil(t, selected)
	require.Contains(t, diagnostics.ExcludedChannelIDs, channel.Id)
}

func TestLegacyPooledCacheRouteFilterKeepsMappedCandidateIdentity(t *testing.T) {
	previousCache := group2model2channels
	previousLegacyCache := legacyRouteCandidateCache
	previousChannels := channelsIDM
	t.Cleanup(func() {
		channelSyncLock.Lock()
		group2model2channels = previousCache
		legacyRouteCandidateCache = previousLegacyCache
		channelsIDM = previousChannels
		channelSyncLock.Unlock()
	})

	channelID := 90603
	weight := uint(1)
	mapping := `{"raw-a":"provider/a","raw-b":"provider/b"}`
	channel := &Channel{Id: channelID, Status: common.ChannelStatusEnabled, Weight: &weight, ModelMapping: &mapping}
	channelSyncLock.Lock()
	group2model2channels = map[string]map[string][]int{
		"default": {"raw-a": {channelID}, "raw-b": {channelID}},
	}
	legacyRouteCandidateCache = nil
	channelsIDM = map[int]*Channel{channelID: channel.CloneForCache()}
	diagnostics := &dto.ChannelRouteSelectionDiagnostics{}
	selected, err, cacheHit := getRandomSatisfiedPooledRouteModelsFromCacheWithRouteFilter(
		"default",
		[]routeModelCandidate{{model: "raw-a"}, {model: "raw-b"}},
		0,
		nil,
		false,
		nil,
		func(_ *Channel, _ string, effectiveModel string) bool { return effectiveModel == "provider/b" },
		nil,
		diagnostics,
	)
	channelSyncLock.Unlock()

	require.True(t, cacheHit)
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, channelID, selected.Id)
	require.Equal(t, "raw-b", diagnostics.SelectedCandidateModel)
	require.Equal(t, "provider/b", diagnostics.EffectiveRouteModel)
}

func TestLegacyPooledDatabaseSelectionKeepsAbilityPriorityIdentity(t *testing.T) {
	prepareChannelCacheTest(t)

	previousRouteHelper := common.GroupModelRouteHelperEnabled
	common.GroupModelRouteHelperEnabled = true
	t.Cleanup(func() { common.GroupModelRouteHelperEnabled = previousRouteHelper })

	compactModel := ratio_setting.WithCompactModelSuffix("priority-identity-model")
	channelPriority := int64(1)
	channelWeight := uint(1)
	channel := &Channel{
		Id:       90604,
		Name:     "legacy-priority-identity",
		Type:     constant.ChannelTypeCodex,
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   compactModel + ",priority-identity-model",
		Priority: &channelPriority,
		Weight:   &channelWeight,
		ModelMapping: common.GetPointer(fmt.Sprintf(
			`{"%s":"provider/priority-model","priority-identity-model":"provider/priority-model"}`,
			compactModel,
		)),
	}
	require.NoError(t, DB.Create(channel).Error)

	exactPriority := int64(5)
	basePriority := int64(20)
	weight := uint(1)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     compactModel,
		ChannelId: channel.Id,
		Enabled:   true,
		Priority:  &exactPriority,
		Weight:    weight,
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "priority-identity-model",
		ChannelId: channel.Id,
		Enabled:   true,
		Priority:  &basePriority,
		Weight:    weight,
	}).Error)

	diagnostics := &dto.ChannelRouteSelectionDiagnostics{}
	selected, found, err := getChannelByRouteModels(
		"default",
		compactModel,
		getGroupModelRouteCandidateMeta(compactModel),
		0,
		nil,
		true,
		nil,
		nil,
		nil,
		diagnostics,
	)
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, selected)
	require.Equal(t, channel.Id, selected.Id)
	require.Equal(t, basePriority, diagnostics.RoutePriority)
	require.Equal(t, "priority-identity-model", diagnostics.SelectedCandidateModel)
}

func TestBuildLegacyRouteCandidateCacheFromAbilitiesKeepsChannelWeight(t *testing.T) {
	priority := int64(10)
	channelWeight := uint(37)
	abilityWeight := uint(2)
	channel := &Channel{Id: 90605, Status: common.ChannelStatusEnabled, Weight: &channelWeight}
	ability := &Ability{Group: "default", Model: "weight-identity-model", ChannelId: channel.Id, Enabled: true, Priority: &priority, Weight: abilityWeight}

	cache := buildLegacyRouteCandidateCacheFromAbilities([]*Ability{ability}, map[int]*Channel{channel.Id: channel})
	entries := cache[routeCandidateCacheKey("default", ability.Model)]
	require.Len(t, entries, 1)
	require.Equal(t, abilityWeight, entries[0].abilityWeight)
	require.Equal(t, channelWeight, entries[0].channelWeight)
}

func TestLegacySelectionFallsThroughInvalidHighPriorityMapping(t *testing.T) {
	prepareChannelCacheTest(t)

	modelName := "mapping-fallback-model"
	highPriority := int64(20)
	lowPriority := int64(10)
	weight := uint(1)
	invalidMapping := "{"
	high := &Channel{
		Id:           90606,
		Type:         constant.ChannelTypeCodex,
		Status:       common.ChannelStatusEnabled,
		Group:        "default",
		Models:       modelName,
		ModelMapping: &invalidMapping,
	}
	low := &Channel{
		Id:     90607,
		Type:   constant.ChannelTypeCodex,
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: modelName,
	}
	require.NoError(t, DB.Create(high).Error)
	require.NoError(t, DB.Create(low).Error)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: modelName, ChannelId: high.Id, Enabled: true, Priority: &highPriority, Weight: weight}).Error)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: modelName, ChannelId: low.Id, Enabled: true, Priority: &lowPriority, Weight: weight}).Error)

	diagnostics := &dto.ChannelRouteSelectionDiagnostics{}
	selected, found, err := getChannelByRouteModel(
		"default",
		modelName,
		routeModelCandidate{model: modelName},
		0,
		nil,
		false,
		nil,
		func(_ *Channel, _ string, _ string) bool { return true },
		nil,
		diagnostics,
	)
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, selected)
	require.Equal(t, low.Id, selected.Id)
	require.Equal(t, lowPriority, diagnostics.RoutePriority)
}

func TestLegacyCacheFallsBackWhenMappingRejectsCachedCandidates(t *testing.T) {
	prepareChannelCacheTest(t)

	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	previousGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = false
	t.Setenv("CHANNEL_ROUTING_V2_ENABLED", "false")
	t.Setenv("CHANNEL_ROUTING_SHADOW_ENABLED", "false")
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = previousGroupModelRouteHelperEnabled
	})

	modelName := "cached-mapping-error-model"
	highPriority := int64(20)
	lowPriority := int64(10)
	weight := uint(1)
	invalidMapping := "{"
	high := &Channel{
		Id:           90610,
		Status:       common.ChannelStatusEnabled,
		Group:        "default",
		Models:       modelName,
		ModelMapping: &invalidMapping,
	}
	low := &Channel{
		Id:     90611,
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: modelName,
	}
	require.NoError(t, DB.Create(high).Error)
	require.NoError(t, DB.Create(low).Error)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: modelName, ChannelId: high.Id, Enabled: true, Priority: &highPriority, Weight: weight}).Error)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: modelName, ChannelId: low.Id, Enabled: true, Priority: &lowPriority, Weight: weight}).Error)

	// Simulate a stale legacy route cache that still contains only the invalid
	// high-priority channel while the database has a usable lower-priority
	// fallback. A cache hit must not make this configuration look complete.
	channelSyncLock.Lock()
	legacyRouteCandidateCache = map[string][]legacyRouteCandidateCacheEntry{
		routeCandidateCacheKey("default", modelName): {{
			channel:       high.CloneForCache(),
			priority:      highPriority,
			abilityWeight: weight,
			channelWeight: weight,
		}},
	}
	group2model2channels = map[string]map[string][]int{
		"default": {modelName: {high.Id}},
	}
	channelsIDM = map[int]*Channel{high.Id: high.CloneForCache()}
	channelSyncLock.Unlock()

	resolver := RouteModelResolver(func(channel *Channel, requestedModel string, compact bool) (string, bool, error) {
		return ResolveChannelModelMappingForRouting(channel.GetModelMapping(), requestedModel, compact)
	})
	selected, err := GetRandomSatisfiedChannelWithOptions(
		"default",
		modelName,
		0,
		nil,
		SatisfiedChannelSelectionOptions{RouteModelResolver: resolver},
	)
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, low.Id, selected.Id)
}

func TestLegacyCompactSelectionDoesNotReturnPartialCacheHit(t *testing.T) {
	prepareChannelCacheTest(t)

	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	previousGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = true
	t.Setenv("CHANNEL_ROUTING_V2_ENABLED", "false")
	t.Setenv("CHANNEL_ROUTING_SHADOW_ENABLED", "false")
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = previousGroupModelRouteHelperEnabled
	})

	baseModel := "partial-cache-model"
	compactModel := ratio_setting.WithCompactModelSuffix(baseModel)
	highPriority := int64(20)
	lowPriority := int64(10)
	weight := uint(1)
	invalidMapping := "{"
	high := &Channel{
		Id:           90620,
		Type:         constant.ChannelTypeCodex,
		Status:       common.ChannelStatusEnabled,
		Group:        "default",
		Models:       compactModel,
		ModelMapping: &invalidMapping,
	}
	fallback := &Channel{
		Id:     90621,
		Type:   constant.ChannelTypeCodex,
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: baseModel,
	}
	require.NoError(t, DB.Create(high).Error)
	require.NoError(t, DB.Create(fallback).Error)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: compactModel, ChannelId: high.Id, Enabled: true, Priority: &highPriority, Weight: weight}).Error)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: baseModel, ChannelId: fallback.Id, Enabled: true, Priority: &lowPriority, Weight: weight}).Error)

	// Simulate a partial legacy snapshot: the exact compact candidate has an
	// invalid mapping, while the base-model bucket contains a stale channel
	// that no longer exists in the database. Returning that cached candidate
	// would bypass the database fallback and route the request to stale state.
	stale := &Channel{Id: 90622, Type: constant.ChannelTypeCodex, Status: common.ChannelStatusEnabled}
	channelSyncLock.Lock()
	legacyRouteCandidateCache = map[string][]legacyRouteCandidateCacheEntry{
		routeCandidateCacheKey("default", compactModel): {{
			channel:       high.CloneForCache(),
			priority:      highPriority,
			abilityWeight: weight,
			channelWeight: weight,
		}},
		routeCandidateCacheKey("default", baseModel): {{
			channel:       stale,
			priority:      lowPriority,
			abilityWeight: weight,
			channelWeight: weight,
		}},
	}
	group2model2channels = map[string]map[string][]int{
		"default": {compactModel: {high.Id}, baseModel: {stale.Id}},
	}
	channelsIDM = map[int]*Channel{high.Id: high.CloneForCache(), stale.Id: stale.CloneForCache()}
	channelSyncLock.Unlock()

	selected, err := GetRandomSatisfiedChannel("default", compactModel, 0)
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, fallback.Id, selected.Id)
}

func TestUpdateChannelStatusRefreshesMemoryCacheAfterEnable(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	channel := &Channel{
		Id:     102,
		Name:   "auto-disabled-channel",
		Status: common.ChannelStatusAutoDisabled,
		Group:  "default",
		Models: "gpt-5.4",
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-5.4",
		ChannelId: channel.Id,
		Enabled:   false,
	}).Error)

	InitChannelCache()

	got, err := GetRandomSatisfiedChannel("default", "gpt-5.4", 0)
	require.NoError(t, err)
	require.Nil(t, got)

	require.True(t, UpdateChannelStatus(channel.Id, "", common.ChannelStatusEnabled, ""))

	got, err = GetRandomSatisfiedChannel("default", "gpt-5.4", 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, channel.Id, got.Id)

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()
	require.True(t, isChannelIDInList(group2model2channels["default"]["gpt-5.4"], channel.Id))
}

func TestUpdateMultiKeyStatusIgnoresUnknownUsingKey(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
	})

	channel := &Channel{
		Id:     103,
		Name:   "multi-key-unknown",
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-5.4",
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
			MultiKeyMode: constant.MultiKeyModePolling,
			MultiKeyStatusList: map[int]int{
				1: common.ChannelStatusAutoDisabled,
			},
		},
	}
	require.NoError(t, DB.Create(channel).Error)

	require.False(t, UpdateChannelStatus(channel.Id, "missing-key", common.ChannelStatusAutoDisabled, "bad key"))

	var updated Channel
	require.NoError(t, DB.First(&updated, "id = ?", channel.Id).Error)
	require.Equal(t, common.ChannelStatusEnabled, updated.Status)
	require.Equal(t, map[int]int{1: common.ChannelStatusAutoDisabled}, updated.ChannelInfo.MultiKeyStatusList)
}

func TestUpdateMultiKeyStatusDisablesAndRestoresChannel(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	channel := &Channel{
		Id:     104,
		Name:   "multi-key-routing",
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-5.4",
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
			MultiKeyMode: constant.MultiKeyModePolling,
		},
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-5.4",
		ChannelId: channel.Id,
		Enabled:   true,
	}).Error)
	InitChannelCache()

	require.True(t, UpdateChannelStatus(channel.Id, "key-a", common.ChannelStatusAutoDisabled, "key a failed"))
	got, err := GetRandomSatisfiedChannel("default", "gpt-5.4", 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, channel.Id, got.Id)

	require.True(t, UpdateChannelStatus(channel.Id, "key-b", common.ChannelStatusAutoDisabled, "key b failed"))
	got, err = GetRandomSatisfiedChannel("default", "gpt-5.4", 0)
	require.NoError(t, err)
	require.Nil(t, got)

	var disabled Channel
	require.NoError(t, DB.First(&disabled, "id = ?", channel.Id).Error)
	require.Equal(t, common.ChannelStatusAutoDisabled, disabled.Status)

	require.True(t, UpdateChannelStatus(channel.Id, "key-a", common.ChannelStatusEnabled, ""))
	got, err = GetRandomSatisfiedChannel("default", "gpt-5.4", 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, channel.Id, got.Id)

	var restored Channel
	require.NoError(t, DB.First(&restored, "id = ?", channel.Id).Error)
	require.NotContains(t, restored.ChannelInfo.MultiKeyDisabledReason, 0)
	require.NotContains(t, restored.ChannelInfo.MultiKeyDisabledTime, 0)
}

func TestUpdateMultiKeyStatusRestoresKeyWhenChannelAlreadyEnabled(t *testing.T) {
	prepareChannelCacheTest(t)

	channel := &Channel{
		Id:        105,
		Name:      "multi-key-already-enabled",
		Key:       "key-a\nkey-b",
		Status:    common.ChannelStatusEnabled,
		Group:     "default",
		Models:    "gpt-5.4",
		OtherInfo: `{"status_reason":"previous failure","status_time":123}`,
		ChannelInfo: ChannelInfo{
			IsMultiKey:             true,
			MultiKeySize:           2,
			MultiKeyMode:           constant.MultiKeyModePolling,
			MultiKeyStatusList:     map[int]int{1: common.ChannelStatusAutoDisabled},
			MultiKeyDisabledReason: map[int]string{1: "key b failed"},
			MultiKeyDisabledTime:   map[int]int64{1: 123},
		},
	}
	require.NoError(t, DB.Create(channel).Error)

	require.True(t, UpdateChannelStatus(channel.Id, "key-b", common.ChannelStatusEnabled, ""))

	var updated Channel
	require.NoError(t, DB.First(&updated, "id = ?", channel.Id).Error)
	require.Equal(t, common.ChannelStatusEnabled, updated.Status)
	require.Empty(t, updated.ChannelInfo.MultiKeyStatusList)
	require.Empty(t, updated.ChannelInfo.MultiKeyDisabledReason)
	require.Empty(t, updated.ChannelInfo.MultiKeyDisabledTime)
	info := updated.GetOtherInfo()
	require.NotContains(t, info, "status_reason")
	require.NotContains(t, info, "status_time")
}

func TestUpdateMultiKeyStatusRestoresKeyWithNilDisableMetadata(t *testing.T) {
	prepareChannelCacheTest(t)

	channel := &Channel{
		Id:     106,
		Name:   "multi-key-nil-disable-metadata",
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-5.4",
		ChannelInfo: ChannelInfo{
			IsMultiKey:         true,
			MultiKeySize:       2,
			MultiKeyMode:       constant.MultiKeyModePolling,
			MultiKeyStatusList: map[int]int{1: common.ChannelStatusAutoDisabled},
		},
	}
	require.NoError(t, DB.Create(channel).Error)

	require.NotPanics(t, func() {
		require.True(t, UpdateChannelStatus(channel.Id, "key-b", common.ChannelStatusEnabled, ""))
	})

	var updated Channel
	require.NoError(t, DB.First(&updated, "id = ?", channel.Id).Error)
	require.Equal(t, common.ChannelStatusEnabled, updated.Status)
	require.Empty(t, updated.ChannelInfo.MultiKeyStatusList)
	require.Empty(t, updated.ChannelInfo.MultiKeyDisabledReason)
	require.Empty(t, updated.ChannelInfo.MultiKeyDisabledTime)
}

func TestGetRandomSatisfiedChannelExcludingSkipsUsedChannelsAtSamePriority(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	priority := int64(10)
	weight := uint(1)
	channels := []*Channel{
		{Id: 201, Name: "used-a", Status: common.ChannelStatusEnabled, Group: "default", Models: "gpt-5.5", Priority: &priority, Weight: &weight},
		{Id: 202, Name: "used-b", Status: common.ChannelStatusEnabled, Group: "default", Models: "gpt-5.5", Priority: &priority, Weight: &weight},
		{Id: 203, Name: "fresh", Status: common.ChannelStatusEnabled, Group: "default", Models: "gpt-5.5", Priority: &priority, Weight: &weight},
	}
	for _, channel := range channels {
		require.NoError(t, DB.Create(channel).Error)
		require.NoError(t, DB.Create(&Ability{
			Group:     "default",
			Model:     "gpt-5.5",
			ChannelId: channel.Id,
			Enabled:   true,
			Priority:  channel.Priority,
			Weight:    *channel.Weight,
		}).Error)
	}
	InitChannelCache()

	got, err := GetRandomSatisfiedChannelExcluding("default", "gpt-5.5", 0, map[int]struct{}{201: {}, 202: {}})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, 203, got.Id)
}

func TestGetRandomSatisfiedChannelExcludingReturnsCacheHitWithoutDatabaseFallback(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	modelName := "gpt-cache-only"
	priority := int64(10)
	weight := uint(1)
	channels := []*Channel{
		{Id: 230, Name: "excluded-cache-channel", Status: common.ChannelStatusEnabled, Group: "default", Models: modelName, Priority: &priority, Weight: &weight},
		{Id: 231, Name: "selected-cache-channel", Status: common.ChannelStatusEnabled, Group: "default", Models: modelName, Priority: &priority, Weight: &weight},
	}
	for _, channel := range channels {
		require.NoError(t, DB.Create(channel).Error)
		require.NoError(t, DB.Create(&Ability{
			Group:     "default",
			Model:     modelName,
			ChannelId: channel.Id,
			Enabled:   true,
			Priority:  channel.Priority,
			Weight:    *channel.Weight,
		}).Error)
	}
	InitChannelCache()

	require.NoError(t, DB.Exec("DELETE FROM abilities WHERE model = ?", modelName).Error)
	require.NoError(t, DB.Exec("DELETE FROM channels WHERE id IN (?, ?)", 230, 231).Error)

	got, err := GetRandomSatisfiedChannelExcluding("default", modelName, 0, map[int]struct{}{230: {}})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, 231, got.Id)
}

func TestGetRandomSatisfiedChannelFallbackHonorsDatabaseRequestBodyLimit(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	modelName := "gpt-body-limit-fallback"
	InitChannelCache()

	priority := int64(10)
	weight := uint(1)
	limitedSettings := dto.ChannelOtherSettings{
		RequestBodyLimit: &dto.ChannelRequestBodyLimit{
			MaxBytes:   100,
			ObservedAt: time.Now().UTC().Unix(),
			Source:     "upstream_413",
			Reason:     "request body too large",
		},
	}
	rawSettings, err := common.Marshal(limitedSettings)
	require.NoError(t, err)

	limitedChannel := &Channel{
		Id:            232,
		Name:          "database-limited-channel",
		Status:        common.ChannelStatusEnabled,
		Group:         "default",
		Models:        modelName,
		Priority:      &priority,
		Weight:        &weight,
		OtherSettings: string(rawSettings),
	}
	require.NoError(t, DB.Create(limitedChannel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     modelName,
		ChannelId: limitedChannel.Id,
		Enabled:   true,
		Priority:  limitedChannel.Priority,
		Weight:    *limitedChannel.Weight,
	}).Error)

	got, err := GetRandomSatisfiedChannelExcludingWithRequestBodyLimit("default", modelName, 0, nil, 128, 0, time.Now().UTC())
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestGetRandomSatisfiedChannelRequestBodyLimitFallsBackToLowerPriority(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	modelName := "gpt-body-limit-lower-priority"
	InitChannelCache()

	highPriority := int64(20)
	lowPriority := int64(10)
	weight := uint(1)
	limitedSettings := dto.ChannelOtherSettings{
		RequestBodyLimit: &dto.ChannelRequestBodyLimit{
			MaxBytes:   100,
			ObservedAt: time.Now().UTC().Unix(),
			Source:     "upstream_413",
			Reason:     "request body too large",
		},
	}
	rawSettings, err := common.Marshal(limitedSettings)
	require.NoError(t, err)

	channels := []*Channel{
		{
			Id:            233,
			Name:          "limited-high-priority-channel",
			Status:        common.ChannelStatusEnabled,
			Group:         "default",
			Models:        modelName,
			Priority:      &highPriority,
			Weight:        &weight,
			OtherSettings: string(rawSettings),
		},
		{
			Id:       234,
			Name:     "allowed-low-priority-channel",
			Status:   common.ChannelStatusEnabled,
			Group:    "default",
			Models:   modelName,
			Priority: &lowPriority,
			Weight:   &weight,
		},
	}
	for _, channel := range channels {
		require.NoError(t, DB.Create(channel).Error)
		require.NoError(t, DB.Create(&Ability{
			Group:     "default",
			Model:     modelName,
			ChannelId: channel.Id,
			Enabled:   true,
			Priority:  channel.Priority,
			Weight:    *channel.Weight,
		}).Error)
	}

	got, err := GetRandomSatisfiedChannelExcludingWithRequestBodyLimit("default", modelName, 0, nil, 128, 0, time.Now().UTC())
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, 234, got.Id)
}

func TestGetRandomSatisfiedChannelExcludingKeepsRetryPriorityStable(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	highPriority := int64(20)
	midPriority := int64(10)
	lowPriority := int64(0)
	weight := uint(1)
	channels := []*Channel{
		{Id: 210, Name: "used-high", Status: common.ChannelStatusEnabled, Group: "default", Models: "gpt-5.5", Priority: &highPriority, Weight: &weight},
		{Id: 211, Name: "mid", Status: common.ChannelStatusEnabled, Group: "default", Models: "gpt-5.5", Priority: &midPriority, Weight: &weight},
		{Id: 212, Name: "low", Status: common.ChannelStatusEnabled, Group: "default", Models: "gpt-5.5", Priority: &lowPriority, Weight: &weight},
	}
	for _, channel := range channels {
		require.NoError(t, DB.Create(channel).Error)
		require.NoError(t, DB.Create(&Ability{
			Group:     "default",
			Model:     "gpt-5.5",
			ChannelId: channel.Id,
			Enabled:   true,
			Priority:  channel.Priority,
			Weight:    *channel.Weight,
		}).Error)
	}
	InitChannelCache()

	got, err := GetRandomSatisfiedChannelExcluding("default", "gpt-5.5", 1, map[int]struct{}{210: {}})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, 211, got.Id)
}

func TestLegacyRouteFilterUsesAbilityPriorityForRetry(t *testing.T) {
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	previousGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.GroupModelRouteHelperEnabled = false
	t.Setenv("CHANNEL_ROUTING_V2_ENABLED", "false")
	t.Setenv("CHANNEL_ROUTING_SHADOW_ENABLED", "false")
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = previousGroupModelRouteHelperEnabled
	})

	testCases := []struct {
		name               string
		memoryCacheEnabled bool
	}{
		{name: "database", memoryCacheEnabled: false},
		{name: "memory cache", memoryCacheEnabled: true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			prepareChannelCacheTest(t)
			common.MemoryCacheEnabled = tc.memoryCacheEnabled
			channelPriorityA := int64(10)
			channelPriorityB := int64(20)
			abilityPriorityA := int64(100)
			abilityPriorityB := int64(50)
			weight := uint(1)
			channelA := &Channel{
				Id:       90501,
				Name:     "ability-priority-a",
				Status:   common.ChannelStatusEnabled,
				Group:    "default",
				Models:   "legacy-priority-model",
				Priority: &channelPriorityA,
				Weight:   &weight,
			}
			channelB := &Channel{
				Id:       90502,
				Name:     "ability-priority-b",
				Status:   common.ChannelStatusEnabled,
				Group:    "default",
				Models:   "legacy-priority-model",
				Priority: &channelPriorityB,
				Weight:   &weight,
			}
			for _, channel := range []*Channel{channelA, channelB} {
				require.NoError(t, DB.Create(channel).Error)
			}
			require.NoError(t, DB.Create(&Ability{
				Group:     "default",
				Model:     "legacy-priority-model",
				ChannelId: channelA.Id,
				Enabled:   true,
				Priority:  &abilityPriorityA,
				Weight:    weight,
			}).Error)
			require.NoError(t, DB.Create(&Ability{
				Group:     "default",
				Model:     "legacy-priority-model",
				ChannelId: channelB.Id,
				Enabled:   true,
				Priority:  &abilityPriorityB,
				Weight:    weight,
			}).Error)
			if tc.memoryCacheEnabled {
				InitChannelCache()
			}

			routeFilter := func(_ *Channel, _ string, _ string) bool { return true }
			selected, err := GetRandomSatisfiedChannelWithOptions(
				"default",
				"legacy-priority-model",
				0,
				nil,
				SatisfiedChannelSelectionOptions{RouteCandidateFilter: routeFilter},
			)
			require.NoError(t, err)
			require.NotNil(t, selected)
			require.Equal(t, channelA.Id, selected.Id)

			selected, err = GetRandomSatisfiedChannelWithOptions(
				"default",
				"legacy-priority-model",
				1,
				nil,
				SatisfiedChannelSelectionOptions{RouteCandidateFilter: routeFilter},
			)
			require.NoError(t, err)
			require.NotNil(t, selected)
			require.Equal(t, channelB.Id, selected.Id)
		})
	}
}

func TestShadowV2PreflightFailureDoesNotOverrideLegacyResult(t *testing.T) {
	prepareChannelCacheTest(t)

	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	previousGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = false
	common.GroupModelRouteHelperEnabled = false
	t.Setenv("CHANNEL_ROUTING_V2_ENABLED", "false")
	t.Setenv("CHANNEL_ROUTING_SHADOW_ENABLED", "true")
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = previousGroupModelRouteHelperEnabled
	})

	priority := int64(10)
	weight := uint(1)
	routeWeight := uint(RouteWeightTotalBPS)
	channel := &Channel{
		Id:       90503,
		Name:     "shadow-primary",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "shadow-preflight-model",
		Priority: &priority,
		Weight:   &weight,
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:                    "default",
		Model:                    "shadow-preflight-model",
		ChannelId:                channel.Id,
		Enabled:                  true,
		Priority:                 &priority,
		Weight:                   weight,
		RoutePriority:            &priority,
		RouteWeightBPS:           &routeWeight,
		RoutePriorityConfigured:  true,
		RouteWeightBPSConfigured: true,
	}).Error)

	preflightCalls := 0
	preflightErr := errors.New("shadow-only preflight failure")
	diagnostics := &dto.ChannelRouteSelectionDiagnostics{}
	selected, err := GetRandomSatisfiedChannelWithOptions(
		"default",
		"shadow-preflight-model",
		0,
		nil,
		SatisfiedChannelSelectionOptions{
			Diagnostics: diagnostics,
			CandidatePreflight: func(*Channel) (bool, error) {
				preflightCalls++
				if preflightCalls == 2 {
					return false, preflightErr
				}
				return true, nil
			},
		},
	)

	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, channel.Id, selected.Id)
	require.Equal(t, 2, preflightCalls, "legacy and shadow V2 paths must each run the preflight")
	require.Equal(t, channel.Id, diagnostics.SelectedChannelID)
	var candidatePreflightErr *CandidatePreflightError
	require.False(t, errors.As(err, &candidatePreflightErr), "discarded shadow failure must not escape as the primary error")
}

func TestGetNextEnabledKeyUpdatesPollingIndexInMemoryCache(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
	})

	channel := &Channel{
		Id:     235,
		Name:   "polling-cache-channel",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-polling-cache",
		Key:    "sk-a\nsk-b",
		ChannelInfo: ChannelInfo{
			IsMultiKey:         true,
			MultiKeySize:       2,
			MultiKeyMode:       constant.MultiKeyModePolling,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusEnabled, 1: common.ChannelStatusEnabled},
		},
	}
	require.NoError(t, DB.Create(channel).Error)
	InitChannelCache()

	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	key, idx, apiErr := cached.GetNextEnabledKey()
	require.Nil(t, apiErr)
	require.Equal(t, "sk-a", key)
	require.Equal(t, 0, idx)

	updatedSettings, err := MarkChannelRequestBodyLimit(channel.Id, dto.ChannelRequestBodyLimit{
		MaxBytes:   128,
		ObservedAt: time.Now().UTC().Unix(),
		Source:     "test",
	})
	require.NoError(t, err)
	require.NotNil(t, updatedSettings.RequestBodyLimit)

	cached, err = CacheGetChannel(channel.Id)
	require.NoError(t, err)
	key, idx, apiErr = cached.GetNextEnabledKey()
	require.Nil(t, apiErr)
	require.Equal(t, "sk-b", key)
	require.Equal(t, 1, idx)

	cached, err = CacheGetChannel(channel.Id)
	require.NoError(t, err)
	settings := cached.GetOtherSettings()
	require.NotNil(t, settings.RequestBodyLimit)
	require.Equal(t, int64(128), settings.RequestBodyLimit.MaxBytes)
}

func TestGetNextEnabledKeyExcludingSkipsEphemeralCredentialIndexes(t *testing.T) {
	channel := &Channel{
		Id:  241,
		Key: "key-a\nkey-b\nkey-c",
		ChannelInfo: ChannelInfo{
			IsMultiKey:         true,
			MultiKeySize:       3,
			MultiKeyMode:       "unknown",
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusEnabled, 1: common.ChannelStatusEnabled, 2: common.ChannelStatusEnabled},
		},
	}

	key, index, apiErr := channel.GetNextEnabledKeyExcluding(map[int]struct{}{0: {}})
	require.Nil(t, apiErr)
	require.Equal(t, "key-b", key)
	require.Equal(t, 1, index)

	_, _, apiErr = channel.GetNextEnabledKeyExcluding(map[int]struct{}{0: {}, 1: {}, 2: {}})
	require.NotNil(t, apiErr)
	require.Equal(t, common.ChannelStatusEnabled, channel.ChannelInfo.MultiKeyStatusList[0])
	require.Equal(t, types.ErrorCodeChannelNoAvailableKey, apiErr.GetErrorCode())
}

func TestUpdateBalanceRefreshesMemoryCache(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
	})

	channel := &Channel{
		Id:     236,
		Name:   "balance-cache-channel",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-balance-cache",
	}
	require.NoError(t, DB.Create(channel).Error)
	InitChannelCache()

	channel.UpdateBalance(12.34)

	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.Equal(t, 12.34, cached.Balance)
	require.NotZero(t, cached.BalanceUpdatedTime)
}

func TestUpdateBalanceKeepsPollingIndexInMemoryCache(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
	})

	channel := &Channel{
		Id:     237,
		Name:   "balance-polling-cache-channel",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-balance-polling-cache",
		Key:    "sk-a\nsk-b\nsk-c",
		ChannelInfo: ChannelInfo{
			IsMultiKey:         true,
			MultiKeySize:       3,
			MultiKeyMode:       constant.MultiKeyModePolling,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusEnabled, 1: common.ChannelStatusEnabled, 2: common.ChannelStatusEnabled},
		},
	}
	require.NoError(t, DB.Create(channel).Error)
	InitChannelCache()

	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	key, idx, apiErr := cached.GetNextEnabledKey()
	require.Nil(t, apiErr)
	require.Equal(t, "sk-a", key)
	require.Equal(t, 0, idx)

	channel.UpdateBalance(12.34)

	cached, err = CacheGetChannel(channel.Id)
	require.NoError(t, err)
	key, idx, apiErr = cached.GetNextEnabledKey()
	require.Nil(t, apiErr)
	require.Equal(t, "sk-b", key)
	require.Equal(t, 1, idx)
}

func TestCacheUpdateChannelKeepsPollingIndexInMemoryCache(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
	})

	channel := &Channel{
		Id:     238,
		Name:   "update-polling-cache-channel",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-update-polling-cache",
		Key:    "sk-a\nsk-b\nsk-c",
		ChannelInfo: ChannelInfo{
			IsMultiKey:         true,
			MultiKeySize:       3,
			MultiKeyMode:       constant.MultiKeyModePolling,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusEnabled, 1: common.ChannelStatusEnabled, 2: common.ChannelStatusEnabled},
		},
	}
	require.NoError(t, DB.Create(channel).Error)
	InitChannelCache()

	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	key, idx, apiErr := cached.GetNextEnabledKey()
	require.Nil(t, apiErr)
	require.Equal(t, "sk-a", key)
	require.Equal(t, 0, idx)

	updated := channel.CloneForCache()
	updated.Name = "update-polling-cache-channel-renamed"
	updated.ChannelInfo.MultiKeyPollingIndex = 0
	CacheUpdateChannel(updated)

	cached, err = CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.Equal(t, "update-polling-cache-channel-renamed", cached.Name)
	key, idx, apiErr = cached.GetNextEnabledKey()
	require.Nil(t, apiErr)
	require.Equal(t, "sk-b", key)
	require.Equal(t, 1, idx)
}

func TestCacheUpdateChannelDoesNotKeepPollingIndexWhenModeChanges(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
	})

	channel := &Channel{
		Id:     239,
		Name:   "update-polling-mode-change-channel",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-update-polling-mode-change",
		Key:    "sk-a\nsk-b\nsk-c",
		ChannelInfo: ChannelInfo{
			IsMultiKey:         true,
			MultiKeySize:       3,
			MultiKeyMode:       constant.MultiKeyModePolling,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusEnabled, 1: common.ChannelStatusEnabled, 2: common.ChannelStatusEnabled},
		},
	}
	require.NoError(t, DB.Create(channel).Error)
	InitChannelCache()

	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	_, _, apiErr := cached.GetNextEnabledKey()
	require.Nil(t, apiErr)

	updated := channel.CloneForCache()
	updated.ChannelInfo.MultiKeyMode = constant.MultiKeyModeRandom
	updated.ChannelInfo.MultiKeyPollingIndex = 0
	CacheUpdateChannel(updated)

	cached, err = CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.Equal(t, constant.MultiKeyModeRandom, cached.ChannelInfo.MultiKeyMode)
	require.Equal(t, 0, cached.ChannelInfo.MultiKeyPollingIndex)
}

func TestCacheUpdateChannelDoesNotKeepPollingIndexWhenOldModeWasNotPolling(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
	})

	channel := &Channel{
		Id:     240,
		Name:   "update-random-to-polling-channel",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-update-random-to-polling",
		Key:    "sk-a\nsk-b\nsk-c",
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         3,
			MultiKeyMode:         constant.MultiKeyModeRandom,
			MultiKeyPollingIndex: 2,
			MultiKeyStatusList:   map[int]int{0: common.ChannelStatusEnabled, 1: common.ChannelStatusEnabled, 2: common.ChannelStatusEnabled},
		},
	}
	require.NoError(t, DB.Create(channel).Error)
	InitChannelCache()

	updated := channel.CloneForCache()
	updated.ChannelInfo.MultiKeyMode = constant.MultiKeyModePolling
	updated.ChannelInfo.MultiKeyPollingIndex = 0
	CacheUpdateChannel(updated)

	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.Equal(t, constant.MultiKeyModePolling, cached.ChannelInfo.MultiKeyMode)
	require.Equal(t, 0, cached.ChannelInfo.MultiKeyPollingIndex)
}

func TestChooseCachedRouteChannelUsesDatabaseWeightSelection(t *testing.T) {
	priority := int64(10)
	zeroWeight := uint(0)
	heavyWeight := uint(100)
	channels := []*Channel{
		{Id: 220, Name: "zero-weight", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &zeroWeight},
		{Id: 221, Name: "heavy-weight", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &heavyWeight},
	}

	got, err := chooseCachedRouteChannelWithRandom(channels, 0, nil, func(max int) int {
		require.Equal(t, 120, max)
		return 6
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, 220, got.Id)
}

func TestChooseCachedRouteChannelUsesDatabaseWeightBoundaries(t *testing.T) {
	priority := int64(10)
	zeroWeight := uint(0)
	heavyWeight := uint(100)
	channels := []*Channel{
		{Id: 222, Name: "zero-weight", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &zeroWeight},
		{Id: 223, Name: "heavy-weight", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &heavyWeight},
	}

	testCases := []struct {
		name       string
		random     int
		expectedID int
	}{
		{name: "zero weight keeps base share", random: 9, expectedID: 222},
		{name: "heavy weight starts after base share", random: 10, expectedID: 223},
		{name: "heavy weight owns weighted tail", random: 119, expectedID: 223},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := chooseCachedRouteChannelWithRandom(channels, 0, nil, func(max int) int {
				require.Equal(t, 120, max)
				return tc.random
			})
			require.NoError(t, err)
			require.NotNil(t, got)
			require.Equal(t, tc.expectedID, got.Id)
		})
	}
}

func TestChooseCachedRouteChannelUsesDatabaseWeightForAllZeroWeights(t *testing.T) {
	priority := int64(10)
	zeroWeight := uint(0)
	channels := []*Channel{
		{Id: 224, Name: "zero-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &zeroWeight},
		{Id: 225, Name: "zero-b", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &zeroWeight},
	}

	got, err := chooseCachedRouteChannelWithRandom(channels, 0, nil, func(max int) int {
		require.Equal(t, 20, max)
		return 10
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, 225, got.Id)
}

func TestChooseCachedRouteChannelWeightsOnlyTargetPriority(t *testing.T) {
	highPriority := int64(10)
	lowPriority := int64(1)
	zeroWeight := uint(0)
	heavyWeight := uint(100)
	channels := []*Channel{
		{Id: 226, Name: "high-a", Status: common.ChannelStatusEnabled, Priority: &highPriority, Weight: &zeroWeight},
		{Id: 227, Name: "high-b", Status: common.ChannelStatusEnabled, Priority: &highPriority, Weight: &zeroWeight},
		{Id: 228, Name: "low-heavy", Status: common.ChannelStatusEnabled, Priority: &lowPriority, Weight: &heavyWeight},
	}

	got, err := chooseCachedRouteChannelWithPriorityMode(channels, 0, nil, false, func(max int) int {
		require.Equal(t, 20, max)
		return 15
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, 227, got.Id)
}

func TestChooseCachedRouteChannelWeightsOnlyUnexcludedChannels(t *testing.T) {
	priority := int64(10)
	zeroWeight := uint(0)
	heavyWeight := uint(100)
	channels := []*Channel{
		{Id: 229, Name: "excluded-heavy", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &heavyWeight},
		{Id: 232, Name: "selected-zero", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &zeroWeight},
	}

	got, err := chooseCachedRouteChannelWithPriorityMode(channels, 0, map[int]struct{}{229: {}}, false, func(max int) int {
		require.Equal(t, 10, max)
		return 0
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, 232, got.Id)
}

func TestIsChannelEnabledForGroupModelFallsBackToDatabaseOnCacheMiss(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	channel := &Channel{
		Id:     103,
		Name:   "satisfy-fallback-channel",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "other-model",
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-5.4-mini",
		ChannelId: channel.Id,
		Enabled:   true,
	}).Error)

	require.True(t, IsChannelEnabledForGroupModel("default", "gpt-5.4-mini", channel.Id))
}

func TestInitChannelCacheKeepsPreviousSnapshotOnScanError(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	channel := &Channel{
		Id:     104,
		Name:   "stable-cache-channel",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-5.4",
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-5.4",
		ChannelId: channel.Id,
		Enabled:   true,
	}).Error)

	InitChannelCache()

	require.NoError(t, DB.Exec(
		fmt.Sprintf(
			"INSERT INTO channels (id, type, %s, status, name, models, %s, channel_info, settings) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
			commonKeyCol,
			commonGroupCol,
		),
		999,
		1,
		"broken-key",
		common.ChannelStatusEnabled,
		"broken-channel",
		"broken-model",
		"default",
		`{invalid`,
		"",
	).Error)

	InitChannelCache()

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()
	require.True(t, isChannelIDInList(group2model2channels["default"]["gpt-5.4"], channel.Id))
	require.Nil(t, channelsIDM[999])
}

func TestChannelInfoScanSupportsStringValue(t *testing.T) {
	var info ChannelInfo
	err := info.Scan(`{"is_multi_key":false,"multi_key_size":0,"multi_key_status_list":{},"multi_key_disabled_reason":{},"multi_key_disabled_time":{},"multi_key_polling_index":0,"multi_key_mode":"random"}`)
	require.NoError(t, err)
	require.False(t, info.IsMultiKey)
	require.Equal(t, 0, info.MultiKeySize)
	require.Equal(t, 0, info.MultiKeyPollingIndex)
	require.Equal(t, "random", string(info.MultiKeyMode))
}

func TestChannelInfoValueReturnsStringJSON(t *testing.T) {
	value, err := (ChannelInfo{
		IsMultiKey:           true,
		MultiKeySize:         1,
		MultiKeyStatusList:   map[int]int{0: common.ChannelStatusEnabled},
		MultiKeyPollingIndex: 0,
		MultiKeyMode:         constant.MultiKeyModePolling,
	}).Value()
	require.NoError(t, err)
	require.IsType(t, "", value)
	require.JSONEq(t, `{"is_multi_key":true,"multi_key_size":1,"multi_key_status_list":{"0":1},"multi_key_polling_index":0,"multi_key_mode":"polling"}`, value.(string))
}

func TestGroupModelRouteHelperDisabledWhenExplicitlyTurnedOff(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	channel := &Channel{
		Id:     105,
		Name:   "group-model-disabled",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-4o-gizmo-*",
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-4o-gizmo-*",
		ChannelId: channel.Id,
		Enabled:   true,
	}).Error)

	InitChannelCache()

	got, err := GetRandomSatisfiedChannel("default", "gpt-4o-gizmo-special", 0)
	require.NoError(t, err)
	require.Nil(t, got)
	require.False(t, IsChannelEnabledForGroupModel("default", "gpt-4o-gizmo-special", channel.Id))
}

func TestGetRandomSatisfiedChannelUsesGroupModelRouteHelperWhenEnabled(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	channel := &Channel{
		Id:     106,
		Name:   "group-model-cache-hit",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-4o-gizmo-*",
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-4o-gizmo-*",
		ChannelId: channel.Id,
		Enabled:   true,
	}).Error)

	InitChannelCache()

	got, err := GetRandomSatisfiedChannel("default", "gpt-4o-gizmo-special", 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, channel.Id, got.Id)
}

func TestGetRandomSatisfiedChannelFallsBackFromCompactSuffixToBaseModel(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	channel := &Channel{
		Id:     112,
		Type:   constant.ChannelTypeOpenAI,
		Name:   "compact-base-model",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-5.5",
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeNative,
	})
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-5.5",
		ChannelId: channel.Id,
		Enabled:   true,
	}).Error)

	InitChannelCache()

	got, err := GetRandomSatisfiedChannel("default", ratio_setting.WithCompactModelSuffix("gpt-5.5"), 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, channel.Id, got.Id)

	common.MemoryCacheEnabled = false
	got, err = GetRandomSatisfiedChannel("default", ratio_setting.WithCompactModelSuffix("gpt-5.5"), 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, channel.Id, got.Id)
}

func TestGetRandomSatisfiedChannelFallsBackFromCompactSuffixToAzureBaseModel(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	channel := &Channel{
		Id:     130,
		Type:   constant.ChannelTypeAzure,
		Name:   "azure-compact-base-model",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-5.5",
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-5.5",
		ChannelId: channel.Id,
		Enabled:   true,
	}).Error)

	InitChannelCache()

	compactModel := ratio_setting.WithCompactModelSuffix("gpt-5.5")
	require.True(t, IsChannelEnabledForGroupModel("default", compactModel, channel.Id))
	got, err := GetRandomSatisfiedChannel("default", compactModel, 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, channel.Id, got.Id)

	common.MemoryCacheEnabled = false
	require.True(t, IsChannelEnabledForGroupModel("default", compactModel, channel.Id))
	got, err = GetRandomSatisfiedChannel("default", compactModel, 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, channel.Id, got.Id)
}

func TestGetRandomSatisfiedChannelDisabledCompactModeKeepsBaseModelRouting(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	channel := &Channel{
		Id:     113,
		Type:   constant.ChannelTypeOpenAI,
		Name:   "compact-disabled-base-model",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-5.5",
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode:     dto.ResponsesCompactModeDisabled,
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileGenericProxy,
	})
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-5.5",
		ChannelId: channel.Id,
		Enabled:   true,
	}).Error)

	InitChannelCache()

	baseModel := "gpt-5.5"
	compactModel := ratio_setting.WithCompactModelSuffix(baseModel)
	require.True(t, IsChannelEnabledForGroupModel("default", baseModel, channel.Id))
	require.False(t, IsChannelEnabledForGroupModel("default", compactModel, channel.Id))

	got, err := GetRandomSatisfiedChannel("default", baseModel, 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, channel.Id, got.Id)

	got, err = GetRandomSatisfiedChannel("default", compactModel, 0)
	require.NoError(t, err)
	require.Nil(t, got)

	common.MemoryCacheEnabled = false
	require.True(t, IsChannelEnabledForGroupModel("default", baseModel, channel.Id))
	require.False(t, IsChannelEnabledForGroupModel("default", compactModel, channel.Id))

	got, err = GetRandomSatisfiedChannel("default", baseModel, 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, channel.Id, got.Id)

	got, err = GetRandomSatisfiedChannel("default", compactModel, 0)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestGetRandomSatisfiedChannelRestrictsCompactFallbackToDetectedCompactModels(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	channel := &Channel{
		Id:     117,
		Type:   constant.ChannelTypeOpenAI,
		Name:   "compact-detected-subset",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-5.4,gpt-5.5",
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeNative,
		UpstreamModelUpdateLastDetectedModels: []string{
			ratio_setting.WithCompactModelSuffix("gpt-5.5"),
		},
	})
	require.NoError(t, DB.Create(channel).Error)
	for _, modelName := range []string{"gpt-5.4", "gpt-5.5"} {
		require.NoError(t, DB.Create(&Ability{
			Group:     "default",
			Model:     modelName,
			ChannelId: channel.Id,
			Enabled:   true,
		}).Error)
	}

	InitChannelCache()

	got, err := GetRandomSatisfiedChannel("default", ratio_setting.WithCompactModelSuffix("gpt-5.4"), 0)
	require.NoError(t, err)
	require.Nil(t, got)

	got, err = GetRandomSatisfiedChannel("default", ratio_setting.WithCompactModelSuffix("gpt-5.5"), 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, channel.Id, got.Id)

	common.MemoryCacheEnabled = false
	got, err = GetRandomSatisfiedChannel("default", ratio_setting.WithCompactModelSuffix("gpt-5.4"), 0)
	require.NoError(t, err)
	require.Nil(t, got)

	got, err = GetRandomSatisfiedChannel("default", ratio_setting.WithCompactModelSuffix("gpt-5.5"), 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, channel.Id, got.Id)
}

func TestGetRandomSatisfiedChannelAllowsCompactFallbackThroughExplicitMapping(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	modelMapping := `{"gpt-5.4-openai-compact":"gpt-5.5"}`
	channel := &Channel{
		Id:           120,
		Type:         constant.ChannelTypeOpenAI,
		Name:         "compact-mapped-model",
		Status:       common.ChannelStatusEnabled,
		Group:        "default",
		Models:       "gpt-5.4,gpt-5.5",
		ModelMapping: &modelMapping,
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeNative,
		UpstreamModelUpdateLastDetectedModels: []string{
			ratio_setting.WithCompactModelSuffix("gpt-5.5"),
		},
	})
	require.NoError(t, DB.Create(channel).Error)
	for _, modelName := range []string{"gpt-5.4", "gpt-5.5"} {
		require.NoError(t, DB.Create(&Ability{
			Group:     "default",
			Model:     modelName,
			ChannelId: channel.Id,
			Enabled:   true,
		}).Error)
	}

	InitChannelCache()

	got, err := GetRandomSatisfiedChannel("default", ratio_setting.WithCompactModelSuffix("gpt-5.4"), 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, channel.Id, got.Id)

	common.MemoryCacheEnabled = false
	got, err = GetRandomSatisfiedChannel("default", ratio_setting.WithCompactModelSuffix("gpt-5.4"), 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, channel.Id, got.Id)
}

func TestGetRandomSatisfiedChannelSkipsCheckedNativeChannelWithoutCompactSignals(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	channel := &Channel{
		Id:     118,
		Type:   constant.ChannelTypeOpenAI,
		Name:   "compact-checked-without-signal",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-5.4",
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode:                  dto.ResponsesCompactModeNative,
		UpstreamModelUpdateCheckEnabled:       true,
		UpstreamModelUpdateLastCheckTime:      12345,
		UpstreamModelUpdateLastDetectedModels: []string{},
	})
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-5.4",
		ChannelId: channel.Id,
		Enabled:   true,
	}).Error)

	InitChannelCache()

	got, err := GetRandomSatisfiedChannel("default", ratio_setting.WithCompactModelSuffix("gpt-5.4"), 0)
	require.NoError(t, err)
	require.Nil(t, got)

	common.MemoryCacheEnabled = false
	got, err = GetRandomSatisfiedChannel("default", ratio_setting.WithCompactModelSuffix("gpt-5.4"), 0)
	require.NoError(t, err)
	require.Nil(t, got)

	common.MemoryCacheEnabled = true
	uncheckedChannel := &Channel{
		Id:     119,
		Type:   constant.ChannelTypeOpenAI,
		Name:   "compact-unchecked-without-signal",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-5.4",
	}
	uncheckedChannel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode:            dto.ResponsesCompactModeNative,
		UpstreamModelUpdateCheckEnabled: true,
	})
	require.NoError(t, DB.Create(uncheckedChannel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-5.4",
		ChannelId: uncheckedChannel.Id,
		Enabled:   true,
	}).Error)

	InitChannelCache()

	got, err = GetRandomSatisfiedChannel("default", ratio_setting.WithCompactModelSuffix("gpt-5.4"), 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, uncheckedChannel.Id, got.Id)

	common.MemoryCacheEnabled = false
	got, err = GetRandomSatisfiedChannel("default", ratio_setting.WithCompactModelSuffix("gpt-5.4"), 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, uncheckedChannel.Id, got.Id)
}

func TestGetRandomSatisfiedChannelPoolsExactCompactAndBaseFallbackChannels(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	basePriority := int64(20)
	compactPriority := int64(10)
	baseChannel := &Channel{
		Id:       113,
		Type:     constant.ChannelTypeOpenAI,
		Name:     "compact-base-model",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "gpt-5.5",
		Priority: &basePriority,
	}
	compactChannel := &Channel{
		Id:       114,
		Type:     constant.ChannelTypeOpenAI,
		Name:     "compact-exact-model",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   ratio_setting.WithCompactModelSuffix("gpt-5.5"),
		Priority: &compactPriority,
	}
	baseChannel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeNative,
	})
	compactChannel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeNative,
	})
	require.NoError(t, DB.Create(baseChannel).Error)
	require.NoError(t, DB.Create(compactChannel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-5.5",
		ChannelId: baseChannel.Id,
		Enabled:   true,
		Priority:  &basePriority,
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     ratio_setting.WithCompactModelSuffix("gpt-5.5"),
		ChannelId: compactChannel.Id,
		Enabled:   true,
		Priority:  &compactPriority,
	}).Error)

	InitChannelCache()

	compactModel := ratio_setting.WithCompactModelSuffix("gpt-5.5")
	require.True(t, IsChannelEnabledForGroupModel("default", compactModel, baseChannel.Id))
	require.True(t, IsChannelEnabledForGroupModel("default", compactModel, compactChannel.Id))

	got, err := GetRandomSatisfiedChannel("default", compactModel, 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, baseChannel.Id, got.Id)

	got, err = GetRandomSatisfiedChannel("default", compactModel, 1)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, compactChannel.Id, got.Id)

	common.MemoryCacheEnabled = false
	require.True(t, IsChannelEnabledForGroupModel("default", compactModel, baseChannel.Id))
	require.True(t, IsChannelEnabledForGroupModel("default", compactModel, compactChannel.Id))

	got, err = GetRandomSatisfiedChannel("default", compactModel, 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, baseChannel.Id, got.Id)

	got, err = GetRandomSatisfiedChannel("default", compactModel, 1)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, compactChannel.Id, got.Id)
}

func TestGetRandomSatisfiedChannelPrefersNamespaceUnsupportedForCompact(t *testing.T) {
	prepareChannelCacheTest(t)

	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	previousGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.GroupModelRouteHelperEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = previousGroupModelRouteHelperEnabled
	})

	highPriority := int64(20)
	lowPriority := int64(10)
	normalChannel := &Channel{
		Id:       230,
		Type:     constant.ChannelTypeOpenAI,
		Name:     "compact-normal-priority",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "gpt-5.5",
		Priority: &highPriority,
	}
	preferredChannel := &Channel{
		Id:       231,
		Type:     constant.ChannelTypeOpenAI,
		Name:     "compact-namespace-unsupported",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "gpt-5.5",
		Priority: &lowPriority,
	}
	normalChannel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeNative,
	})
	preferredChannel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode:     dto.ResponsesCompactModeSynthetic,
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileGenericProxy,
	})
	for _, channel := range []*Channel{normalChannel, preferredChannel} {
		require.NoError(t, DB.Create(channel).Error)
		require.NoError(t, DB.Create(&Ability{
			Group:     "default",
			Model:     "gpt-5.5",
			ChannelId: channel.Id,
			Enabled:   true,
			Priority:  channel.Priority,
		}).Error)
	}

	compactModel := ratio_setting.WithCompactModelSuffix("gpt-5.5")
	for _, memoryCacheEnabled := range []bool{true, false} {
		common.MemoryCacheEnabled = memoryCacheEnabled
		if memoryCacheEnabled {
			InitChannelCache()
		}
		got, err := GetRandomSatisfiedChannel("default", compactModel, 0)
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Equal(t, preferredChannel.Id, got.Id)

		got, err = GetRandomSatisfiedChannelExcluding("default", compactModel, 1, map[int]struct{}{preferredChannel.Id: {}})
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Equal(t, normalChannel.Id, got.Id)
	}

	preferredDisabled := false
	preferredSettings := preferredChannel.GetOtherSettings()
	preferredSettings.PreferForResponsesCompactionWhenNamespaceUnsupported = &preferredDisabled
	preferredChannel.SetOtherSettings(preferredSettings)
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", preferredChannel.Id).Update("settings", preferredChannel.OtherSettings).Error)
	common.MemoryCacheEnabled = true
	InitChannelCache()

	got, err := GetRandomSatisfiedChannel("default", compactModel, 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, normalChannel.Id, got.Id)

	got, err = GetRandomSatisfiedChannelExcluding("default", compactModel, 0, map[int]struct{}{preferredChannel.Id: {}})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, normalChannel.Id, got.Id)
}

func TestGetRandomSatisfiedChannelDatabaseCompactPoolKeepsBaseFallbackPriorities(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = false
	common.GroupModelRouteHelperEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	highPriority := int64(30)
	compactPriority := int64(20)
	lowPriority := int64(10)
	baseHighChannel := &Channel{
		Id:       217,
		Type:     constant.ChannelTypeOpenAI,
		Name:     "compact-db-base-high-priority",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "gpt-5.5",
		Priority: &highPriority,
	}
	compactChannel := &Channel{
		Id:       218,
		Type:     constant.ChannelTypeOpenAI,
		Name:     "compact-db-exact-middle-priority",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   ratio_setting.WithCompactModelSuffix("gpt-5.5"),
		Priority: &compactPriority,
	}
	baseLowChannel := &Channel{
		Id:       219,
		Type:     constant.ChannelTypeOpenAI,
		Name:     "compact-db-base-low-priority",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "gpt-5.5",
		Priority: &lowPriority,
	}
	for _, channel := range []*Channel{baseHighChannel, compactChannel, baseLowChannel} {
		channel.SetOtherSettings(dto.ChannelOtherSettings{
			ResponsesCompactMode: dto.ResponsesCompactModeNative,
		})
		require.NoError(t, DB.Create(channel).Error)
	}
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-5.5",
		ChannelId: baseHighChannel.Id,
		Enabled:   true,
		Priority:  &highPriority,
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     ratio_setting.WithCompactModelSuffix("gpt-5.5"),
		ChannelId: compactChannel.Id,
		Enabled:   true,
		Priority:  &compactPriority,
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-5.5",
		ChannelId: baseLowChannel.Id,
		Enabled:   true,
		Priority:  &lowPriority,
	}).Error)

	compactModel := ratio_setting.WithCompactModelSuffix("gpt-5.5")
	got, err := GetRandomSatisfiedChannel("default", compactModel, 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, baseHighChannel.Id, got.Id)

	got, err = GetRandomSatisfiedChannel("default", compactModel, 1)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, compactChannel.Id, got.Id)

	got, err = GetRandomSatisfiedChannel("default", compactModel, 2)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, baseLowChannel.Id, got.Id)
}

func TestGetRandomSatisfiedChannelCompactPoolSkipsExcludedExactChannel(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	priority := int64(10)
	baseChannel := &Channel{
		Id:       204,
		Type:     constant.ChannelTypeOpenAI,
		Name:     "compact-base-after-excluded-exact",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "gpt-5.5",
		Priority: &priority,
	}
	compactChannel := &Channel{
		Id:       205,
		Type:     constant.ChannelTypeOpenAI,
		Name:     "compact-exact-excluded",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   ratio_setting.WithCompactModelSuffix("gpt-5.5"),
		Priority: &priority,
	}
	baseChannel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeNative,
	})
	compactChannel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeNative,
	})
	require.NoError(t, DB.Create(baseChannel).Error)
	require.NoError(t, DB.Create(compactChannel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-5.5",
		ChannelId: baseChannel.Id,
		Enabled:   true,
		Priority:  &priority,
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     ratio_setting.WithCompactModelSuffix("gpt-5.5"),
		ChannelId: compactChannel.Id,
		Enabled:   true,
		Priority:  &priority,
	}).Error)

	InitChannelCache()

	got, err := GetRandomSatisfiedChannelExcluding(
		"default",
		ratio_setting.WithCompactModelSuffix("gpt-5.5"),
		0,
		map[int]struct{}{compactChannel.Id: {}},
	)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, baseChannel.Id, got.Id)
}

func TestGetRandomSatisfiedChannelUsesChannelWeightForDatabaseFallback(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = false
	common.GroupModelRouteHelperEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	priority := int64(10)
	zeroWeight := uint(0)
	heavyWeight := uint(100)
	baseChannel := &Channel{
		Id:       206,
		Type:     constant.ChannelTypeOpenAI,
		Name:     "compact-base-channel-weight",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "gpt-5.5",
		Priority: &priority,
		Weight:   &zeroWeight,
	}
	compactChannel := &Channel{
		Id:       207,
		Type:     constant.ChannelTypeOpenAI,
		Name:     "compact-exact-channel-weight",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   ratio_setting.WithCompactModelSuffix("gpt-5.5"),
		Priority: &priority,
		Weight:   &heavyWeight,
	}
	baseChannel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeNative,
	})
	compactChannel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeNative,
	})
	require.NoError(t, DB.Create(baseChannel).Error)
	require.NoError(t, DB.Create(compactChannel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-5.5",
		ChannelId: baseChannel.Id,
		Enabled:   true,
		Priority:  &priority,
		Weight:    heavyWeight,
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     ratio_setting.WithCompactModelSuffix("gpt-5.5"),
		ChannelId: compactChannel.Id,
		Enabled:   true,
		Priority:  &priority,
		Weight:    zeroWeight,
	}).Error)

	abilities := []Ability{
		{
			Group:     "default",
			Model:     "gpt-5.5",
			ChannelId: baseChannel.Id,
			Enabled:   true,
			Priority:  &priority,
			Weight:    heavyWeight,
		},
		{
			Group:     "default",
			Model:     ratio_setting.WithCompactModelSuffix("gpt-5.5"),
			ChannelId: compactChannel.Id,
			Enabled:   true,
			Priority:  &priority,
			Weight:    zeroWeight,
		},
	}
	channels, weights, err := loadRouteCandidateChannels(abilities, routeModelCandidate{
		model:          ratio_setting.WithCompactModelSuffix("gpt-5.5"),
		compactRequest: true,
	}, true)
	require.NoError(t, err)
	require.Len(t, channels, 2)
	require.Equal(t, []uint{zeroWeight, heavyWeight}, weights)
}

func TestLoadRouteCandidateChannelsReportsMissingChannel(t *testing.T) {
	prepareChannelCacheTest(t)

	priority := int64(10)
	abilities := []Ability{
		{
			Group:     "default",
			Model:     "gpt-5.5",
			ChannelId: 999,
			Enabled:   true,
			Priority:  &priority,
			Weight:    1,
		},
	}

	channels, weights, err := loadRouteCandidateChannels(abilities, routeModelCandidate{
		model: "gpt-5.5",
	}, false)

	require.Error(t, err)
	require.Contains(t, err.Error(), "数据库一致性错误，渠道# 999 不存在")
	require.Nil(t, channels)
	require.Nil(t, weights)
}

func TestGetRandomSatisfiedChannelDatabaseFallbackSkipsExcludedCompactChannel(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = false
	common.GroupModelRouteHelperEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	priority := int64(10)
	baseChannel := &Channel{
		Id:       208,
		Type:     constant.ChannelTypeOpenAI,
		Name:     "compact-db-base-after-excluded-exact",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "gpt-5.5",
		Priority: &priority,
	}
	compactChannel := &Channel{
		Id:       209,
		Type:     constant.ChannelTypeOpenAI,
		Name:     "compact-db-exact-excluded",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   ratio_setting.WithCompactModelSuffix("gpt-5.5"),
		Priority: &priority,
	}
	baseChannel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeNative,
	})
	compactChannel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeNative,
	})
	require.NoError(t, DB.Create(baseChannel).Error)
	require.NoError(t, DB.Create(compactChannel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-5.5",
		ChannelId: baseChannel.Id,
		Enabled:   true,
		Priority:  &priority,
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     ratio_setting.WithCompactModelSuffix("gpt-5.5"),
		ChannelId: compactChannel.Id,
		Enabled:   true,
		Priority:  &priority,
	}).Error)

	got, err := GetRandomSatisfiedChannelExcluding(
		"default",
		ratio_setting.WithCompactModelSuffix("gpt-5.5"),
		0,
		map[int]struct{}{compactChannel.Id: {}},
	)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, baseChannel.Id, got.Id)
}

func TestGetRandomSatisfiedChannelDatabaseFallbackKeepsRetryPriorityStable(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = false
	common.GroupModelRouteHelperEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	highPriority := int64(20)
	midPriority := int64(10)
	lowPriority := int64(0)
	weight := uint(1)
	channels := []*Channel{
		{Id: 213, Name: "db-used-high", Status: common.ChannelStatusEnabled, Group: "default", Models: "gpt-5.5", Priority: &highPriority, Weight: &weight},
		{Id: 214, Name: "db-mid", Status: common.ChannelStatusEnabled, Group: "default", Models: "gpt-5.5", Priority: &midPriority, Weight: &weight},
		{Id: 215, Name: "db-low", Status: common.ChannelStatusEnabled, Group: "default", Models: "gpt-5.5", Priority: &lowPriority, Weight: &weight},
	}
	for _, channel := range channels {
		require.NoError(t, DB.Create(channel).Error)
		require.NoError(t, DB.Create(&Ability{
			Group:     "default",
			Model:     "gpt-5.5",
			ChannelId: channel.Id,
			Enabled:   true,
			Priority:  channel.Priority,
			Weight:    *channel.Weight,
		}).Error)
	}

	got, err := GetRandomSatisfiedChannelExcluding("default", "gpt-5.5", 1, map[int]struct{}{213: {}})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, 214, got.Id)
}

func TestGetRandomSatisfiedChannelDatabaseFallbackReturnsNilWhenRetryPriorityExcluded(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = false
	common.GroupModelRouteHelperEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	priority := int64(10)
	weight := uint(1)
	channel := &Channel{
		Id:       216,
		Name:     "db-excluded-only-priority",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "gpt-5.5",
		Priority: &priority,
		Weight:   &weight,
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-5.5",
		ChannelId: channel.Id,
		Enabled:   true,
		Priority:  channel.Priority,
		Weight:    *channel.Weight,
	}).Error)

	got, err := GetRandomSatisfiedChannelExcluding("default", "gpt-5.5", 0, map[int]struct{}{216: {}})
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestGetRandomSatisfiedChannelNormalizesLegacyConvertCompactModeToSynthetic(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	compactModel := ratio_setting.WithCompactModelSuffix("gpt-5.5")
	legacyPriority := int64(12)
	nativePriority := int64(11)
	legacyChannel := &Channel{
		Id:       125,
		Type:     constant.ChannelTypeOpenAI,
		Name:     "compact-exact-legacy-convert",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   compactModel,
		Priority: &legacyPriority,
	}
	legacyChannel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactMode("convert"),
	})
	nativeChannel := &Channel{
		Id:       126,
		Type:     constant.ChannelTypeOpenAI,
		Name:     "compact-exact-native",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   compactModel,
		Priority: &nativePriority,
	}
	nativeChannel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeNative,
	})
	require.NoError(t, DB.Create(legacyChannel).Error)
	require.NoError(t, DB.Create(nativeChannel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     compactModel,
		ChannelId: legacyChannel.Id,
		Enabled:   true,
		Priority:  &legacyPriority,
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     compactModel,
		ChannelId: nativeChannel.Id,
		Enabled:   true,
		Priority:  &nativePriority,
	}).Error)

	InitChannelCache()

	require.True(t, IsChannelEnabledForGroupModel("default", compactModel, legacyChannel.Id))
	require.True(t, IsChannelEnabledForGroupModel("default", compactModel, nativeChannel.Id))

	got, err := GetRandomSatisfiedChannel("default", compactModel, 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, legacyChannel.Id, got.Id)

	common.GroupModelRouteHelperEnabled = false
	InitChannelCache()
	require.True(t, IsChannelEnabledForGroupModel("default", compactModel, legacyChannel.Id))
	require.True(t, IsChannelEnabledForGroupModel("default", compactModel, nativeChannel.Id))

	got, err = GetRandomSatisfiedChannel("default", compactModel, 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, legacyChannel.Id, got.Id)

	common.MemoryCacheEnabled = false
	require.True(t, IsChannelEnabledForGroupModel("default", compactModel, legacyChannel.Id))
	require.True(t, IsChannelEnabledForGroupModel("default", compactModel, nativeChannel.Id))

	got, err = GetRandomSatisfiedChannel("default", compactModel, 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, legacyChannel.Id, got.Id)

	common.GroupModelRouteHelperEnabled = true
	InitChannelCache()
	require.True(t, IsChannelEnabledForGroupModel("default", compactModel, legacyChannel.Id))
	require.True(t, IsChannelEnabledForGroupModel("default", compactModel, nativeChannel.Id))

	got, err = GetRandomSatisfiedChannel("default", compactModel, 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, legacyChannel.Id, got.Id)
}

func TestGetRandomSatisfiedChannelAllowsSyntheticCompactBaseModelFallback(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	syntheticChannel := &Channel{
		Id:     127,
		Type:   constant.ChannelTypeOpenAI,
		Name:   "compact-synthetic-base",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-5.5",
	}
	syntheticChannel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeSynthetic,
	})
	require.NoError(t, DB.Create(syntheticChannel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-5.5",
		ChannelId: syntheticChannel.Id,
		Enabled:   true,
	}).Error)

	compactModel := ratio_setting.WithCompactModelSuffix("gpt-5.5")
	InitChannelCache()

	require.True(t, IsChannelEnabledForGroupModel("default", compactModel, syntheticChannel.Id))
	got, err := GetRandomSatisfiedChannel("default", compactModel, 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, syntheticChannel.Id, got.Id)

	common.MemoryCacheEnabled = false
	require.True(t, IsChannelEnabledForGroupModel("default", compactModel, syntheticChannel.Id))
	got, err = GetRandomSatisfiedChannel("default", compactModel, 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, syntheticChannel.Id, got.Id)
}

func TestGetRandomSatisfiedChannelAllowsDefaultNativeOpenAIBaseFallbackForCompact(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	defaultPriority := int64(12)
	nativePriority := int64(11)
	defaultChannel := &Channel{
		Id:       115,
		Type:     constant.ChannelTypeOpenAI,
		Name:     "compact-base-default-native",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "gpt-5.5",
		Priority: &defaultPriority,
	}
	nativeChannel := &Channel{
		Id:       116,
		Type:     constant.ChannelTypeOpenAI,
		Name:     "compact-base-native",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "gpt-5.5",
		Priority: &nativePriority,
	}
	nativeChannel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeNative,
	})
	require.NoError(t, DB.Create(defaultChannel).Error)
	require.NoError(t, DB.Create(nativeChannel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-5.5",
		ChannelId: defaultChannel.Id,
		Enabled:   true,
		Priority:  &defaultPriority,
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-5.5",
		ChannelId: nativeChannel.Id,
		Enabled:   true,
		Priority:  &nativePriority,
	}).Error)

	InitChannelCache()

	compactModel := ratio_setting.WithCompactModelSuffix("gpt-5.5")
	require.True(t, IsChannelEnabledForGroupModel("default", compactModel, defaultChannel.Id))
	require.True(t, IsChannelEnabledForGroupModel("default", compactModel, nativeChannel.Id))

	got, err := GetRandomSatisfiedChannel("default", compactModel, 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, defaultChannel.Id, got.Id)

	common.MemoryCacheEnabled = false
	require.True(t, IsChannelEnabledForGroupModel("default", compactModel, defaultChannel.Id))
	require.True(t, IsChannelEnabledForGroupModel("default", compactModel, nativeChannel.Id))

	got, err = GetRandomSatisfiedChannel("default", compactModel, 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, defaultChannel.Id, got.Id)
}

func TestGetRandomSatisfiedChannelFallsBackToDatabaseWithGroupModelRouteHelperWhenEnabled(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	channel := &Channel{
		Id:     107,
		Name:   "group-model-db-fallback",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-4o-gizmo-*",
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-4o-gizmo-*",
		ChannelId: channel.Id,
		Enabled:   true,
	}).Error)

	got, err := GetRandomSatisfiedChannel("default", "gpt-4o-gizmo-special", 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, channel.Id, got.Id)
}

func TestGetRandomSatisfiedChannelRetryFallsBackToNormalizedRouteWhenEnabled(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = false
	common.GroupModelRouteHelperEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	channel := &Channel{
		Id:     109,
		Name:   "group-model-db-retry-fallback",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-4o-gizmo-*",
	}
	priority := int64(5)
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-4o-gizmo-*",
		ChannelId: channel.Id,
		Enabled:   true,
		Priority:  &priority,
	}).Error)

	got, err := GetRandomSatisfiedChannel("default", "gpt-4o-gizmo-special", 1)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, channel.Id, got.Id)
}

func TestGetRandomSatisfiedChannelFallsThroughExcludedExactRoute(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	exactChannel := &Channel{
		Id:     110,
		Name:   "group-model-exact-excluded",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-4o-gizmo-special",
	}
	normalizedChannel := &Channel{
		Id:     111,
		Name:   "group-model-normalized-fallback",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-4o-gizmo-*",
	}
	require.NoError(t, DB.Create(exactChannel).Error)
	require.NoError(t, DB.Create(normalizedChannel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     exactChannel.Models,
		ChannelId: exactChannel.Id,
		Enabled:   true,
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     normalizedChannel.Models,
		ChannelId: normalizedChannel.Id,
		Enabled:   true,
	}).Error)

	InitChannelCache()

	got, err := GetRandomSatisfiedChannelExcluding(
		"default",
		"gpt-4o-gizmo-special",
		0,
		map[int]struct{}{exactChannel.Id: {}},
	)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, normalizedChannel.Id, got.Id)
}

func TestIsChannelEnabledForGroupModelUsesGroupModelRouteHelperWhenEnabled(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	channel := &Channel{
		Id:     108,
		Name:   "group-model-satisfy",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-4o-gizmo-*",
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-4o-gizmo-*",
		ChannelId: channel.Id,
		Enabled:   true,
	}).Error)

	require.True(t, IsChannelEnabledForGroupModel("default", "gpt-4o-gizmo-special", channel.Id))
}

func TestCacheUpdateChannelRetainsRouteWhenDatabaseIsUnavailable(t *testing.T) {
	originalDB := DB
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalChannelsIDM := channelsIDM
	originalGroup2Model2Channels := group2model2channels
	originalLegacyRouteCandidateCache := legacyRouteCandidateCache
	originalRouteCandidateCache := routeCandidateCacheV2
	originalRouteCandidateCacheV2Ready := routeCandidateCacheV2Ready
	originalRouteCandidateCacheV2KnownByChannel := routeCandidateCacheV2KnownByChannel
	common.MemoryCacheEnabled = true
	channel := &Channel{
		Id:            8610,
		Name:          "db-failure-route-cache",
		Status:        common.ChannelStatusEnabled,
		Group:         "default",
		Models:        "gpt-5",
		OtherSettings: `{"responses_compact_mode":"auto"}`,
	}
	channelSyncLock.Lock()
	channelsIDM = map[int]*Channel{channel.Id: channel.CloneForCache()}
	group2model2channels = map[string]map[string][]int{
		"default": {"gpt-5": {channel.Id}},
	}
	legacyRouteCandidateCache = map[string][]legacyRouteCandidateCacheEntry{
		routeCandidateCacheKey("default", "gpt-5"): {{
			channel:       channel.CloneForCache(),
			priority:      37,
			abilityWeight: 9,
			channelWeight: 13,
		}},
	}
	routeCandidateCacheV2 = map[string][]routeCandidateV2{
		routeCandidateCacheKey("default", "gpt-5"): {{
			Group:               "default",
			RequestedModel:      "gpt-5",
			EffectiveRouteModel: "gpt-5",
			Channel:             channel.CloneForCache(),
			RouteWeightBPS:      RouteWeightTotalBPS,
		}},
	}
	routeCandidateCacheV2KnownByChannel = map[string]map[int]struct{}{
		routeCandidateCacheKey("default", "gpt-5"): {channel.Id: {}},
	}
	routeCandidateCacheV2Ready = true
	channelSyncLock.Unlock()
	DB = nil
	t.Cleanup(func() {
		DB = originalDB
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		channelSyncLock.Lock()
		channelsIDM = originalChannelsIDM
		group2model2channels = originalGroup2Model2Channels
		legacyRouteCandidateCache = originalLegacyRouteCandidateCache
		routeCandidateCacheV2 = originalRouteCandidateCache
		routeCandidateCacheV2Ready = originalRouteCandidateCacheV2Ready
		routeCandidateCacheV2KnownByChannel = originalRouteCandidateCacheV2KnownByChannel
		channelSyncLock.Unlock()
	})

	CacheUpdateChannelOtherSettings(channel.Id, `{"responses_compact_mode":"synthetic_summary"}`)

	channelSyncLock.RLock()
	cachedChannel := channelsIDM[channel.Id]
	entries := routeCandidateCacheV2[routeCandidateCacheKey("default", "gpt-5")]
	modelChannels := group2model2channels["default"]["gpt-5"]
	legacyEntries := legacyRouteCandidateCache[routeCandidateCacheKey("default", "gpt-5")]
	channelSyncLock.RUnlock()

	require.NotNil(t, cachedChannel)
	require.Equal(t, `{"responses_compact_mode":"synthetic_summary"}`, cachedChannel.OtherSettings)
	require.Len(t, entries, 1)
	require.NotNil(t, entries[0].Channel)
	require.Equal(t, channel.Id, entries[0].Channel.Id)
	require.Equal(t, []int{channel.Id}, modelChannels)
	require.Len(t, legacyEntries, 1)
	require.NotNil(t, legacyEntries[0].channel)
	require.Equal(t, `{"responses_compact_mode":"synthetic_summary"}`, legacyEntries[0].channel.OtherSettings)
	require.Equal(t, int64(37), legacyEntries[0].priority)
	require.Equal(t, uint(9), legacyEntries[0].abilityWeight)
	require.Equal(t, uint(13), legacyEntries[0].channelWeight)
}

func TestRefreshRouteCandidateCacheKeepsHistoricalFallbackWhenMetadataCacheIsUninitialized(t *testing.T) {
	originalDB := DB
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalChannelsIDM := channelsIDM
	originalGroup2Model2Channels := group2model2channels
	originalLegacyRouteCandidateCache := legacyRouteCandidateCache
	originalRouteCandidateCache := routeCandidateCacheV2
	originalRouteCandidateCacheV2Ready := routeCandidateCacheV2Ready
	originalRouteCandidateCacheV2KnownByChannel := routeCandidateCacheV2KnownByChannel

	common.MemoryCacheEnabled = true
	DB = nil
	priority := int64(23)
	weight := uint(7)
	channel := &Channel{
		Id:       8616,
		Name:     "historical-route-fallback",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "historical-model",
		Priority: &priority,
		Weight:   &weight,
	}
	channelSyncLock.Lock()
	channelsIDM = map[int]*Channel{channel.Id: channel.CloneForCache()}
	group2model2channels = map[string]map[string][]int{
		"default": {"historical-model": {channel.Id}},
	}
	legacyRouteCandidateCache = nil
	routeCandidateCacheV2 = nil
	routeCandidateCacheV2Ready = false
	routeCandidateCacheV2KnownByChannel = nil
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		DB = originalDB
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		channelSyncLock.Lock()
		channelsIDM = originalChannelsIDM
		group2model2channels = originalGroup2Model2Channels
		legacyRouteCandidateCache = originalLegacyRouteCandidateCache
		routeCandidateCacheV2 = originalRouteCandidateCache
		routeCandidateCacheV2Ready = originalRouteCandidateCacheV2Ready
		routeCandidateCacheV2KnownByChannel = originalRouteCandidateCacheV2KnownByChannel
		channelSyncLock.Unlock()
	})

	refreshRouteCandidateCacheV2(channel.Id, channel)

	channelSyncLock.RLock()
	require.Nil(t, legacyRouteCandidateCache)
	entries, hit := getLegacyRouteCandidateCacheEntries("default", routeModelCandidate{model: "historical-model"})
	channelSyncLock.RUnlock()
	require.True(t, hit)
	require.Len(t, entries, 1)
	require.NotNil(t, entries[0].channel)
	require.Equal(t, channel.Id, entries[0].channel.Id)
	require.Equal(t, priority, entries[0].priority)
	require.Equal(t, weight, entries[0].abilityWeight)
	require.Equal(t, weight, entries[0].channelWeight)
}

func TestCacheReloadChannelIgnoresUnavailableDatabase(t *testing.T) {
	previousDB := DB
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	DB = nil
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		DB = previousDB
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
	})

	require.NotPanics(t, func() {
		CacheReloadChannel(8615)
	})
}

func TestRefreshRouteCandidateCacheV2UsesCurrentChannelSnapshot(t *testing.T) {
	prepareChannelCacheTest(t)

	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	previousChannelsIDM := channelsIDM
	previousRouteCandidateCache := routeCandidateCacheV2
	previousRouteCandidateCacheReady := routeCandidateCacheV2Ready
	previousRouteCandidateCacheKnownByChannel := routeCandidateCacheV2KnownByChannel
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		channelSyncLock.Lock()
		channelsIDM = previousChannelsIDM
		routeCandidateCacheV2 = previousRouteCandidateCache
		routeCandidateCacheV2Ready = previousRouteCandidateCacheReady
		routeCandidateCacheV2KnownByChannel = previousRouteCandidateCacheKnownByChannel
		channelSyncLock.Unlock()
	})

	channel := &Channel{
		Id:     8611,
		Name:   "stale-enabled-route-cache",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-5",
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-5",
		ChannelId: channel.Id,
		Enabled:   true,
	}).Error)

	staleSnapshot := channel.CloneForCache()
	disabledSnapshot := channel.CloneForCache()
	disabledSnapshot.Status = common.ChannelStatusAutoDisabled
	channelSyncLock.Lock()
	channelsIDM = map[int]*Channel{channel.Id: disabledSnapshot}
	routeCandidateCacheV2 = map[string][]routeCandidateV2{
		routeCandidateCacheKey("default", "gpt-5"): {{
			Group:               "default",
			RequestedModel:      "gpt-5",
			EffectiveRouteModel: "gpt-5",
			Channel:             staleSnapshot,
			RouteWeightBPS:      RouteWeightTotalBPS,
		}},
	}
	routeCandidateCacheV2KnownByChannel = map[string]map[int]struct{}{
		routeCandidateCacheKey("default", "gpt-5"): {channel.Id: {}},
	}
	routeCandidateCacheV2Ready = true
	channelSyncLock.Unlock()

	refreshRouteCandidateCacheV2(channel.Id, staleSnapshot)

	channelSyncLock.RLock()
	entries := routeCandidateCacheV2[routeCandidateCacheKey("default", "gpt-5")]
	channelSyncLock.RUnlock()
	require.Empty(t, entries)
}

func TestChannelCacheSyncEnabledDefaultsToTrue(t *testing.T) {
	t.Setenv("CHANNEL_CACHE_SYNC_ENABLED", "")

	require.True(t, channelCacheSyncEnabled())
}

func TestChannelCacheSyncEnabledCanBeDisabled(t *testing.T) {
	t.Setenv("CHANNEL_CACHE_SYNC_ENABLED", "false")

	require.False(t, channelCacheSyncEnabled())
}

func TestRefreshRouteCandidateCacheRebuildsLegacyModelIndex(t *testing.T) {
	prepareChannelCacheTest(t)
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCacheEnabled })

	channel := &Channel{
		Id:     8614,
		Name:   "incremental-route-index",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "new-model",
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "new-model",
		ChannelId: channel.Id,
		Enabled:   true,
	}).Error)

	channelSyncLock.Lock()
	channelsIDM = map[int]*Channel{channel.Id: channel.CloneForCache()}
	group2model2channels = map[string]map[string][]int{
		"default": {"old-model": {channel.Id}},
	}
	legacyRouteCandidateCache = map[string][]legacyRouteCandidateCacheEntry{
		routeCandidateCacheKey("default", "old-model"): {{channel: channel.CloneForCache()}},
	}
	channelSyncLock.Unlock()

	refreshRouteCandidateCacheV2(channel.Id, channel)

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()
	require.Empty(t, group2model2channels["default"]["old-model"])
	require.Equal(t, []int{channel.Id}, group2model2channels["default"]["new-model"])
	require.Empty(t, legacyRouteCandidateCache[routeCandidateCacheKey("default", "old-model")])
	require.Len(t, legacyRouteCandidateCache[routeCandidateCacheKey("default", "new-model")], 1)
}
