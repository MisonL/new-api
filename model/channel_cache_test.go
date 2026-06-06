package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
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
		ResponsesCompactMode: dto.ResponsesCompactModeDisabled,
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

func TestChannelCacheSyncEnabledDefaultsToTrue(t *testing.T) {
	t.Setenv("CHANNEL_CACHE_SYNC_ENABLED", "")

	require.True(t, channelCacheSyncEnabled())
}

func TestChannelCacheSyncEnabledCanBeDisabled(t *testing.T) {
	t.Setenv("CHANNEL_CACHE_SYNC_ENABLED", "false")

	require.False(t, channelCacheSyncEnabled())
}
