package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

func prepareChannelCacheTest(t *testing.T) {
	t.Helper()
	initCol()
	require.NoError(t, DB.AutoMigrate(&Ability{}))
	require.NoError(t, DB.Exec("DELETE FROM abilities").Error)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)

	channelSyncLock.Lock()
	group2model2channels = nil
	channelsIDM = nil
	channelSyncLock.Unlock()
	channelCacheRefreshInFlight.Store(false)
	channelCacheRefreshPending.Store(false)
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

func TestGetRandomSatisfiedChannelRoutesCompactSuffixToBaseModelWhenEnabled(t *testing.T) {
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
		Name:   "compact-base-model",
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

	got, err := GetRandomSatisfiedChannel("default", ratio_setting.WithCompactModelSuffix("gpt-5.5"), 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, channel.Id, got.Id)
}

func TestGetRandomSatisfiedChannelPrefersExactCompactModelWhenEnabled(t *testing.T) {
	prepareChannelCacheTest(t)

	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = prevGroupModelRouteHelperEnabled
	})

	baseChannel := &Channel{
		Id:     113,
		Name:   "compact-base-model",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-5.5",
	}
	compactChannel := &Channel{
		Id:     114,
		Name:   "compact-exact-model",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: ratio_setting.WithCompactModelSuffix("gpt-5.5"),
	}
	require.NoError(t, DB.Create(baseChannel).Error)
	require.NoError(t, DB.Create(compactChannel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-5.5",
		ChannelId: baseChannel.Id,
		Enabled:   true,
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     ratio_setting.WithCompactModelSuffix("gpt-5.5"),
		ChannelId: compactChannel.Id,
		Enabled:   true,
	}).Error)

	InitChannelCache()

	got, err := GetRandomSatisfiedChannel("default", ratio_setting.WithCompactModelSuffix("gpt-5.5"), 0)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, compactChannel.Id, got.Id)
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
