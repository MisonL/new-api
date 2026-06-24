package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestChannelOtherSettingsDefaultsResponsesCompactAuto(t *testing.T) {
	channel := &Channel{OtherSettings: `{}`}

	settings := channel.GetOtherSettings()

	require.Empty(t, settings.ResponsesCompactMode)
	require.True(t, settings.IsAutoResponsesCompact())
	require.True(t, settings.AllowAutoNativeCompactBaseFallbackAt(time.Now()))
	require.Equal(t, dto.ResponsesCompactModeNative, settings.ResponsesCompactModeOrDefault())
	require.True(t, settings.HasNativeResponsesCompact())
	require.True(t, settings.ResponsesCompactContextFallbackEnabled())
	require.True(t, settings.ResponsesCompactSummaryModelFallbackEnabled())
	require.Equal(t, []string{"gpt-5.4"}, settings.ResponsesCompactSummaryFallbackModelsOrDefault())
}

func TestChannelOtherSettingsResponsesCompactAutoRoundTrip(t *testing.T) {
	channel := &Channel{}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeAuto,
	})

	settings := channel.GetOtherSettings()

	require.Equal(t, dto.ResponsesCompactModeAuto, settings.ResponsesCompactMode)
	require.True(t, settings.IsAutoResponsesCompact())
	require.True(t, settings.HasNativeResponsesCompact())
	require.False(t, settings.HasSyntheticResponsesCompact())
	require.Equal(t, dto.ResponsesCompactModeNative, settings.ResponsesCompactModeOrDefault())
}

func TestChannelOtherSettingsResponsesProxyProfileForcesSyntheticCompact(t *testing.T) {
	for _, profile := range []dto.ResponsesUpstreamProfile{
		dto.ResponsesUpstreamProfileGenericProxy,
		dto.ResponsesUpstreamProfileChatOnlyProxy,
	} {
		settings := dto.ChannelOtherSettings{
			ResponsesCompactMode:     dto.ResponsesCompactModeNative,
			ResponsesUpstreamProfile: profile,
		}

		require.Equal(t, profile, settings.NormalizedResponsesUpstreamProfile())
		require.True(t, settings.HasResponsesProxyCompatibilityProfile())
		require.True(t, settings.ShouldStripResponsesEncryptedReasoning())
		require.Equal(t, dto.ResponsesCompactModeNative, settings.ResponsesCompactModeOrDefault())
		require.Equal(t, dto.ResponsesCompactModeSynthetic, settings.EffectiveResponsesCompactModeOrDefault())
		require.True(t, settings.HasSyntheticResponsesCompact())
		require.False(t, settings.HasNativeResponsesCompact())
	}
}

func TestChannelOtherSettingsGenericOpenAIProfileKeepsNativeDefault(t *testing.T) {
	settings := dto.ChannelOtherSettings{
		ResponsesCompactMode:     dto.ResponsesCompactModeAuto,
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileGenericOpenAI,
	}

	require.Equal(t, dto.ResponsesUpstreamProfileGenericOpenAI, settings.NormalizedResponsesUpstreamProfile())
	require.False(t, settings.HasResponsesProxyCompatibilityProfile())
	require.Equal(t, dto.ResponsesCompactModeNative, settings.EffectiveResponsesCompactModeOrDefault())
	require.True(t, settings.ResolveResponsesChannelCapability(constant.ChannelTypeOpenAI).SupportsResponsesCompact)
}

func TestChannelOtherSettingsCapabilitySourcePriority(t *testing.T) {
	probed := false
	observed := false
	settings := dto.ChannelOtherSettings{
		ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
			Observed:                 &dto.ResponsesCapabilityObservation{ObservedAt: 10, StatusCode: 413, Reason: "payload too large"},
			Probe:                    &dto.ResponsesCapabilityObservation{ObservedAt: 20, StatusCode: 200, Reason: "probe ok"},
			SupportsResponsesCompact: &probed,
			SupportsNamespaceTools:   &observed,
		},
	}

	snapshot := settings.ResolveResponsesChannelCapability(constant.ChannelTypeOpenAI)
	require.Equal(t, "probe", snapshot.Source)
	require.False(t, snapshot.SupportsResponsesCompact)
	require.False(t, snapshot.SupportsNamespaceTools)
	require.NotNil(t, snapshot.Observed)
	require.NotNil(t, snapshot.Probe)

	settings.ResponsesUpstreamProfile = dto.ResponsesUpstreamProfileOfficialNewAPI
	snapshot = settings.ResolveResponsesChannelCapability(constant.ChannelTypeOpenAI)
	require.Equal(t, "probe", snapshot.Source)
	require.Equal(t, dto.ResponsesUpstreamProfileOfficialNewAPI, snapshot.Profile)
	require.False(t, snapshot.SupportsResponsesCompact)

	settings.ResponsesCapabilityRegistry = nil
	snapshot = settings.ResolveResponsesChannelCapability(constant.ChannelTypeOpenAI)
	require.Equal(t, "explicit_settings", snapshot.Source)
}

func TestChannelOtherSettingsSub2APIHTTPLimitedNativeCompact(t *testing.T) {
	settings := dto.ChannelOtherSettings{
		ResponsesCompactMode:     dto.ResponsesCompactModeAuto,
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileSub2APIHTTP,
	}

	require.Equal(t, dto.ResponsesUpstreamProfileSub2APIHTTP, settings.NormalizedResponsesUpstreamProfile())
	require.False(t, settings.HasResponsesProxyCompatibilityProfile())
	require.True(t, settings.HasResponsesEncryptedReasoningUnsupportedProfile())
	require.True(t, settings.ShouldStripResponsesEncryptedReasoning())
	require.True(t, settings.DisallowsResponsesRESTPreviousResponseID())
	require.Equal(t, dto.ResponsesCompactModeNative, settings.ResponsesCompactModeOrDefault())
	require.Equal(t, dto.ResponsesCompactModeNative, settings.EffectiveResponsesCompactModeOrDefault())
	require.True(t, settings.HasNativeResponsesCompact())
	require.False(t, settings.HasSyntheticResponsesCompact())
}

func TestChannelOtherSettingsDisabledResponsesCompactOverridesProxyProfile(t *testing.T) {
	settings := dto.ChannelOtherSettings{
		ResponsesCompactMode:     dto.ResponsesCompactModeDisabled,
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileGenericProxy,
	}

	require.True(t, settings.HasDisabledResponsesCompact())
	require.True(t, settings.HasResponsesProxyCompatibilityProfile())
	require.Equal(t, dto.ResponsesCompactModeDisabled, settings.ResponsesCompactModeOrDefault())
	require.Equal(t, dto.ResponsesCompactModeDisabled, settings.EffectiveResponsesCompactModeOrDefault())
	require.False(t, settings.HasNativeResponsesCompact())
	require.False(t, settings.HasSyntheticResponsesCompact())
}

func TestChannelOtherSettingsTrustedResponsesProfilesDoNotChangeDefaults(t *testing.T) {
	for _, profile := range []dto.ResponsesUpstreamProfile{
		dto.ResponsesUpstreamProfileOfficialOpenAI,
		dto.ResponsesUpstreamProfileOfficialNewAPI,
		dto.ResponsesUpstreamProfileSameClusterNewAPI,
		dto.ResponsesUpstreamProfileTrustedNewAPI,
		dto.ResponsesUpstreamProfileSub2APIWSV2,
		dto.ResponsesUpstreamProfile("unknown"),
	} {
		settings := dto.ChannelOtherSettings{
			ResponsesUpstreamProfile: profile,
		}

		require.False(t, settings.HasResponsesProxyCompatibilityProfile())
		require.False(t, settings.ShouldStripResponsesEncryptedReasoning())
		require.Equal(t, dto.ResponsesCompactModeNative, settings.EffectiveResponsesCompactModeOrDefault())
		require.True(t, settings.HasNativeResponsesCompact())
	}
}

func TestChannelOtherSettingsDefaultProfileAllowsCompactionItemPassthrough(t *testing.T) {
	settings := dto.ChannelOtherSettings{}

	require.Equal(t, dto.ResponsesUpstreamProfile(""), settings.NormalizedResponsesUpstreamProfile())
	require.False(t, settings.HasResponsesProxyCompatibilityProfile())
	require.True(t, settings.ResolveResponsesChannelCapability(constant.ChannelTypeOpenAI).SupportsCompactionItemPassthrough)
}

func TestChannelOtherSettingsSub2APIHTTPDisallowsRESTPreviousResponseID(t *testing.T) {
	settings := dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileSub2APIHTTP,
	}

	require.False(t, settings.HasResponsesProxyCompatibilityProfile())
	require.True(t, settings.DisallowsResponsesRESTPreviousResponseID())
	wsSettings := dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileSub2APIWSV2,
	}
	require.False(t, wsSettings.DisallowsResponsesRESTPreviousResponseID())
}

func TestChannelOtherSettingsResponsesCompactAutoFallbackUsesDefaultThreeHourInterval(t *testing.T) {
	now := time.Date(2026, 5, 26, 23, 30, 0, 0, time.UTC)
	settings := dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeAuto,
	}

	settings.MarkResponsesCompactAutoFallback(now, "status_code=404")

	require.True(t, settings.HasActiveResponsesCompactAutoFallback(now))
	require.Equal(t, dto.ResponsesCompactModeSynthetic, settings.ResponsesCompactModeOrDefaultAt(now))
	require.True(t, settings.HasActiveResponsesCompactAutoFallback(now.Add(3*time.Hour-time.Second)))
	require.False(t, settings.HasActiveResponsesCompactAutoFallback(now.Add(3*time.Hour)))
	require.Equal(t, dto.ResponsesCompactModeNative, settings.ResponsesCompactModeOrDefaultAt(now.Add(3*time.Hour)))
	require.Zero(t, settings.ResponsesCompactAutoFallbackDate)
	require.Equal(t, now.Unix(), settings.ResponsesCompactAutoFallbackAt)
}

func TestChannelOtherSettingsResponsesCompactAutoFallbackUsesConfiguredInterval(t *testing.T) {
	now := time.Date(2026, 5, 26, 23, 30, 0, 0, time.UTC)
	settings := dto.ChannelOtherSettings{
		ResponsesCompactMode:                           dto.ResponsesCompactModeAuto,
		ResponsesCompactAutoFallbackRetryIntervalHours: 6,
	}

	settings.MarkResponsesCompactAutoFallback(now, "status_code=404")

	require.True(t, settings.HasActiveResponsesCompactAutoFallback(now.Add(6*time.Hour-time.Second)))
	require.False(t, settings.HasActiveResponsesCompactAutoFallback(now.Add(6*time.Hour)))
}

func TestChannelOtherSettingsResponsesCompactAutoFallbackKeepsLegacyDateCompatible(t *testing.T) {
	now := time.Date(2026, 5, 26, 23, 30, 0, 0, time.UTC)
	settings := dto.ChannelOtherSettings{
		ResponsesCompactMode:             dto.ResponsesCompactModeAuto,
		ResponsesCompactAutoFallbackDate: dto.ResponsesCompactAutoFallbackDate(now),
	}

	require.True(t, settings.HasActiveResponsesCompactAutoFallback(now))
	require.False(t, settings.HasActiveResponsesCompactAutoFallback(now.AddDate(0, 0, 1)))
}

func TestChannelOtherSettingsResponsesCompactAutoFallbackRetryIntervalBounds(t *testing.T) {
	require.Equal(t, 3, (*dto.ChannelOtherSettings)(nil).ResponsesCompactAutoFallbackRetryIntervalHoursOrDefault())
	for _, tt := range []struct {
		raw      int
		expected int
	}{
		{raw: 0, expected: 3},
		{raw: -1, expected: 1},
		{raw: 1, expected: 1},
		{raw: 168, expected: 168},
		{raw: 169, expected: 168},
	} {
		settings := dto.ChannelOtherSettings{
			ResponsesCompactAutoFallbackRetryIntervalHours: tt.raw,
		}
		require.Equal(t, tt.expected, settings.ResponsesCompactAutoFallbackRetryIntervalHoursOrDefault())
	}
}

func TestChannelOtherSettingsDoesNotMarkFallbackForExplicitNonAutoMode(t *testing.T) {
	now := time.Date(2026, 5, 26, 23, 30, 0, 0, time.UTC)
	settings := dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeNative,
	}

	settings.MarkResponsesCompactAutoFallback(now, "status_code=404")

	require.Equal(t, dto.ResponsesCompactModeNative, settings.ResponsesCompactMode)
	require.Zero(t, settings.ResponsesCompactAutoFallbackDate)
	require.Zero(t, settings.ResponsesCompactAutoFallbackAt)
	require.Empty(t, settings.ResponsesCompactAutoFallbackReason)
}

func TestResponsesCompactAutoFallbackDateUsesUTC(t *testing.T) {
	shanghai := time.FixedZone("UTC+8", 8*60*60)
	localNextDay := time.Date(2026, 5, 27, 1, 30, 0, 0, shanghai)

	require.Equal(t, 20260526, dto.ResponsesCompactAutoFallbackDate(localNextDay))
}

func TestMarkResponsesCompactAutoFallbackPersistsState(t *testing.T) {
	channel := &Channel{
		Id:            8601,
		Name:          "compact-auto-fallback",
		Key:           "test-key",
		Status:        1,
		Group:         "default",
		Models:        "gpt-5",
		OtherSettings: `{"responses_compact_mode":"auto"}`,
	}
	require.NoError(t, DB.Delete(&Channel{}, channel.Id).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Delete(&Channel{}, channel.Id).Error)
	})
	require.NoError(t, DB.Create(channel).Error)

	before := time.Now().UTC().Unix()
	require.NoError(t, MarkResponsesCompactAutoFallback(channel.Id, "status_code=404"))
	after := time.Now().UTC().Unix()

	var got Channel
	require.NoError(t, DB.First(&got, channel.Id).Error)
	settings := got.GetOtherSettings()
	require.Equal(t, dto.ResponsesCompactModeAuto, settings.ResponsesCompactMode)
	require.GreaterOrEqual(t, settings.ResponsesCompactAutoFallbackAt, before)
	require.LessOrEqual(t, settings.ResponsesCompactAutoFallbackAt, after)
	require.Zero(t, settings.ResponsesCompactAutoFallbackDate)
	require.Equal(t, "status_code=404", settings.ResponsesCompactAutoFallbackReason)
}

func TestMarkResponsesCapabilityObservationPersistsObservedRegistry(t *testing.T) {
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalChannelsIDM := channelsIDM
	originalGroup2Model2Channels := group2model2channels
	common.MemoryCacheEnabled = true
	channelSyncLock.Lock()
	channelsIDM = make(map[int]*Channel)
	group2model2channels = nil
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		channelSyncLock.Lock()
		channelsIDM = originalChannelsIDM
		group2model2channels = originalGroup2Model2Channels
		channelSyncLock.Unlock()
	})

	channel := &Channel{
		Id:            8602,
		Name:          "capability-observed",
		Key:           "test-key",
		Status:        1,
		Group:         "default",
		Models:        "gpt-5",
		OtherSettings: `{"responses_compact_mode":"auto","responses_upstream_profile":"generic_openai"}`,
	}
	require.NoError(t, DB.Delete(&Channel{}, channel.Id).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Delete(&Channel{}, channel.Id).Error)
	})
	require.NoError(t, DB.Create(channel).Error)
	cachedChannel := &Channel{
		Id:            channel.Id,
		Name:          "stale-cache-name",
		Key:           channel.Key,
		Status:        channel.Status,
		Group:         channel.Group,
		Models:        channel.Models,
		OtherSettings: channel.OtherSettings,
	}
	CacheUpdateChannel(cachedChannel)

	settings, err := MarkResponsesCapabilityObservation(channel.Id, dto.ResponsesCapabilityObservation{
		ObservedAt: 100,
		StatusCode: 413,
		ErrorCode:  "upstream_request_too_large",
		Reason:     "payload too large",
	})
	require.NoError(t, err)
	require.NotNil(t, settings.ResponsesCapabilityRegistry)
	require.Equal(t, "observed_calls", settings.ResponsesCapabilityRegistry.Source)
	require.Equal(t, 413, settings.ResponsesCapabilityRegistry.Observed.StatusCode)

	var got Channel
	require.NoError(t, DB.First(&got, channel.Id).Error)
	persisted := got.GetOtherSettings()
	require.Equal(t, dto.ResponsesUpstreamProfileGenericOpenAI, persisted.ResponsesUpstreamProfile)
	require.NotNil(t, persisted.ResponsesCapabilityRegistry)
	require.Equal(t, "upstream_request_too_large", persisted.ResponsesCapabilityRegistry.Observed.ErrorCode)
	channelSyncLock.RLock()
	cached := channelsIDM[channel.Id]
	channelSyncLock.RUnlock()
	require.NotNil(t, cached)
	require.False(t, cached == cachedChannel)
	require.Equal(t, "stale-cache-name", cached.Name)
	cachedSettings := cached.GetOtherSettings()
	require.NotNil(t, cachedSettings.ResponsesCapabilityRegistry)
	require.Equal(t, "upstream_request_too_large", cachedSettings.ResponsesCapabilityRegistry.Observed.ErrorCode)
}

func TestMarkResponsesCapabilityObservationDebouncesSameError(t *testing.T) {
	channel := &Channel{
		Id:            8603,
		Name:          "capability-observed-debounce",
		Key:           "test-key",
		Status:        1,
		Group:         "default",
		Models:        "gpt-5",
		OtherSettings: `{"responses_compact_mode":"auto"}`,
	}
	require.NoError(t, DB.Delete(&Channel{}, channel.Id).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Delete(&Channel{}, channel.Id).Error)
	})
	require.NoError(t, DB.Create(channel).Error)

	_, err := MarkResponsesCapabilityObservation(channel.Id, dto.ResponsesCapabilityObservation{
		ObservedAt: 100,
		StatusCode: 413,
		ErrorCode:  "upstream_request_too_large",
		Reason:     "first",
	})
	require.NoError(t, err)
	settings, err := MarkResponsesCapabilityObservation(channel.Id, dto.ResponsesCapabilityObservation{
		ObservedAt: 120,
		StatusCode: 413,
		ErrorCode:  "upstream_request_too_large",
		Reason:     "same error",
	})
	require.NoError(t, err)
	require.Equal(t, int64(100), settings.ResponsesCapabilityRegistry.Observed.ObservedAt)
	require.Equal(t, "first", settings.ResponsesCapabilityRegistry.Observed.Reason)

	settings, err = MarkResponsesCapabilityObservation(channel.Id, dto.ResponsesCapabilityObservation{
		ObservedAt: 121,
		StatusCode: 400,
		ErrorCode:  "bad_request",
		Reason:     "different error",
	})
	require.NoError(t, err)
	require.Equal(t, int64(121), settings.ResponsesCapabilityRegistry.Observed.ObservedAt)
	require.Equal(t, "bad_request", settings.ResponsesCapabilityRegistry.Observed.ErrorCode)

	settings, err = MarkResponsesCapabilityObservation(channel.Id, dto.ResponsesCapabilityObservation{
		ObservedAt: 110,
		StatusCode: 413,
		ErrorCode:  "upstream_request_too_large",
		Reason:     "older different error",
	})
	require.NoError(t, err)
	require.Equal(t, int64(121), settings.ResponsesCapabilityRegistry.Observed.ObservedAt)
	require.Equal(t, "bad_request", settings.ResponsesCapabilityRegistry.Observed.ErrorCode)

	settings, err = MarkResponsesCapabilityObservation(channel.Id, dto.ResponsesCapabilityObservation{
		ObservedAt: 430,
		StatusCode: 400,
		ErrorCode:  "bad_request",
		Reason:     "same error after debounce",
	})
	require.NoError(t, err)
	require.Equal(t, int64(430), settings.ResponsesCapabilityRegistry.Observed.ObservedAt)
	require.Equal(t, "same error after debounce", settings.ResponsesCapabilityRegistry.Observed.Reason)
}

func TestMarkResponsesCapabilityObservationDebouncesFromCacheBeforeTransaction(t *testing.T) {
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalChannelsIDM := channelsIDM
	originalGroup2Model2Channels := group2model2channels
	common.MemoryCacheEnabled = true
	channelSyncLock.Lock()
	channelsIDM = make(map[int]*Channel)
	group2model2channels = nil
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		channelSyncLock.Lock()
		channelsIDM = originalChannelsIDM
		group2model2channels = originalGroup2Model2Channels
		channelSyncLock.Unlock()
	})

	channel := &Channel{
		Id:     8606,
		Name:   "capability-observed-cache-debounce",
		Key:    "test-key",
		Status: 1,
		Group:  "default",
		Models: "gpt-5",
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
			Observed: &dto.ResponsesCapabilityObservation{
				ObservedAt: 100,
				StatusCode: 413,
				ErrorCode:  "upstream_request_too_large",
				Reason:     "cached first",
			},
			Source: "observed_calls",
		},
	})
	require.NoError(t, DB.Delete(&Channel{}, channel.Id).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Delete(&Channel{}, channel.Id).Error)
	})
	require.NoError(t, DB.Create(channel).Error)
	CacheUpdateChannel(channel)

	settings, err := MarkResponsesCapabilityObservation(channel.Id, dto.ResponsesCapabilityObservation{
		ObservedAt: 120,
		StatusCode: 413,
		ErrorCode:  "upstream_request_too_large",
		Reason:     "same error",
	})
	require.NoError(t, err)
	require.Equal(t, int64(100), settings.ResponsesCapabilityRegistry.Observed.ObservedAt)
	require.Equal(t, "cached first", settings.ResponsesCapabilityRegistry.Observed.Reason)

	var got Channel
	require.NoError(t, DB.First(&got, channel.Id).Error)
	persisted := got.GetOtherSettings()
	require.Equal(t, int64(100), persisted.ResponsesCapabilityRegistry.Observed.ObservedAt)
	require.Equal(t, "cached first", persisted.ResponsesCapabilityRegistry.Observed.Reason)
}

func TestCacheUpdateChannelStatusReplacesCachedSnapshot(t *testing.T) {
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalChannelsIDM := channelsIDM
	common.MemoryCacheEnabled = true
	channelSyncLock.Lock()
	channelsIDM = make(map[int]*Channel)
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		channelSyncLock.Lock()
		channelsIDM = originalChannelsIDM
		channelSyncLock.Unlock()
	})

	cachedChannel := &Channel{
		Id:     8604,
		Name:   "cached-status",
		Status: common.ChannelStatusEnabled,
	}
	CacheUpdateChannel(cachedChannel)

	CacheUpdateChannelStatus(cachedChannel.Id, common.ChannelStatusAutoDisabled)

	channelSyncLock.RLock()
	cached := channelsIDM[cachedChannel.Id]
	channelSyncLock.RUnlock()
	require.NotNil(t, cached)
	require.False(t, cached == cachedChannel)
	require.Equal(t, common.ChannelStatusAutoDisabled, cached.Status)
	require.Equal(t, common.ChannelStatusEnabled, cachedChannel.Status)
}

func TestCacheUpdateChannelUsesDeepCopy(t *testing.T) {
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalChannelsIDM := channelsIDM
	common.MemoryCacheEnabled = true
	channelSyncLock.Lock()
	channelsIDM = make(map[int]*Channel)
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		channelSyncLock.Lock()
		channelsIDM = originalChannelsIDM
		channelSyncLock.Unlock()
	})

	source := &Channel{
		Id:     8605,
		Name:   "deep-copy-cache",
		Status: common.ChannelStatusEnabled,
		Keys:   []string{"key-a", "key-b"},
		ChannelInfo: ChannelInfo{
			MultiKeyStatusList:     map[int]int{1: common.ChannelStatusAutoDisabled},
			MultiKeyDisabledReason: map[int]string{1: "failed"},
			MultiKeyDisabledTime:   map[int]int64{1: 123},
		},
	}
	CacheUpdateChannel(source)

	source.Keys[0] = "mutated"
	source.ChannelInfo.MultiKeyStatusList[1] = common.ChannelStatusEnabled
	source.ChannelInfo.MultiKeyDisabledReason[1] = "mutated"
	source.ChannelInfo.MultiKeyDisabledTime[1] = 456

	cached, err := CacheGetChannel(source.Id)
	require.NoError(t, err)
	require.Equal(t, []string{"key-a", "key-b"}, cached.Keys)
	require.Equal(t, common.ChannelStatusAutoDisabled, cached.ChannelInfo.MultiKeyStatusList[1])
	require.Equal(t, "failed", cached.ChannelInfo.MultiKeyDisabledReason[1])
	require.Equal(t, int64(123), cached.ChannelInfo.MultiKeyDisabledTime[1])

	cached.Keys[0] = "reader-mutated"
	cached.ChannelInfo.MultiKeyStatusList[1] = common.ChannelStatusEnabled
	cachedInfo, err := CacheGetChannelInfo(source.Id)
	require.NoError(t, err)
	require.Equal(t, common.ChannelStatusAutoDisabled, cachedInfo.MultiKeyStatusList[1])
}

func TestCacheUpdateChannelStatusRemovesRouteWithoutMutatingOriginalSlice(t *testing.T) {
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalChannelsIDM := channelsIDM
	originalGroup2Model2Channels := group2model2channels
	common.MemoryCacheEnabled = true
	channelSyncLock.Lock()
	channelsIDM = map[int]*Channel{
		8607: {Id: 8607, Status: common.ChannelStatusEnabled},
	}
	routeSlice := []int{8607, 8608}
	group2model2channels = map[string]map[string][]int{
		"default": {"gpt-5": routeSlice},
	}
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		channelSyncLock.Lock()
		channelsIDM = originalChannelsIDM
		group2model2channels = originalGroup2Model2Channels
		channelSyncLock.Unlock()
	})

	CacheUpdateChannelStatus(8607, common.ChannelStatusAutoDisabled)

	channelSyncLock.RLock()
	updatedRoute := group2model2channels["default"]["gpt-5"]
	channelSyncLock.RUnlock()
	require.Equal(t, []int{8608}, updatedRoute)
	require.Equal(t, []int{8607, 8608}, routeSlice)
}

func TestCacheGetChannelHandlesNilCachedEntry(t *testing.T) {
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalChannelsIDM := channelsIDM
	common.MemoryCacheEnabled = true
	channelSyncLock.Lock()
	channelsIDM = map[int]*Channel{8609: nil}
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		channelSyncLock.Lock()
		channelsIDM = originalChannelsIDM
		channelSyncLock.Unlock()
	})

	channel, err := CacheGetChannel(8609)
	require.Error(t, err)
	require.Nil(t, channel)

	info, err := CacheGetChannelInfo(8609)
	require.Error(t, err)
	require.Nil(t, info)
}

func TestCacheReloadChannelMissingRemovesRoute(t *testing.T) {
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalChannelsIDM := channelsIDM
	originalGroup2Model2Channels := group2model2channels
	common.MemoryCacheEnabled = true
	channelSyncLock.Lock()
	channelsIDM = map[int]*Channel{
		8608: {Id: 8608, Status: common.ChannelStatusEnabled},
	}
	group2model2channels = map[string]map[string][]int{
		"default": {"gpt-5": {8608}},
	}
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		channelSyncLock.Lock()
		channelsIDM = originalChannelsIDM
		group2model2channels = originalGroup2Model2Channels
		channelSyncLock.Unlock()
	})
	require.NoError(t, DB.Delete(&Channel{}, 8608).Error)

	CacheReloadChannel(8608)

	channelSyncLock.RLock()
	_, cached := channelsIDM[8608]
	modelRoutes := group2model2channels["default"]
	channelSyncLock.RUnlock()
	require.False(t, cached)
	require.Empty(t, modelRoutes)
}

func TestChannelOtherSettingsResponsesCompactNativeRoundTrip(t *testing.T) {
	channel := &Channel{}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeNative,
	})

	settings := channel.GetOtherSettings()

	require.Equal(t, dto.ResponsesCompactModeNative, settings.ResponsesCompactMode)
	require.True(t, settings.HasNativeResponsesCompact())
	require.Equal(t, dto.ResponsesCompactModeNative, settings.ResponsesCompactModeOrDefault())
}

func TestChannelOtherSettingsResponsesCompactSyntheticRoundTrip(t *testing.T) {
	channel := &Channel{}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeSynthetic,
	})

	settings := channel.GetOtherSettings()

	require.Equal(t, dto.ResponsesCompactModeSynthetic, settings.ResponsesCompactMode)
	require.True(t, settings.HasSyntheticResponsesCompact())
	require.Equal(t, dto.ResponsesCompactModeSynthetic, settings.ResponsesCompactModeOrDefault())
	require.False(t, settings.HasNativeResponsesCompact())
}

func TestChannelOtherSettingsResponsesCompactDisabledRoundTrip(t *testing.T) {
	channel := &Channel{}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeDisabled,
	})

	settings := channel.GetOtherSettings()

	require.Equal(t, dto.ResponsesCompactModeDisabled, settings.ResponsesCompactMode)
	require.True(t, settings.HasDisabledResponsesCompact())
	require.Equal(t, dto.ResponsesCompactModeDisabled, settings.NormalizedResponsesCompactModeSetting())
	require.Equal(t, dto.ResponsesCompactModeDisabled, settings.ResponsesCompactModeOrDefault())
	require.False(t, settings.IsAutoResponsesCompact())
	require.False(t, settings.HasNativeResponsesCompact())
	require.False(t, settings.HasSyntheticResponsesCompact())
}

func TestChannelOtherSettingsResponsesCompactLegacyModesNormalizeSafely(t *testing.T) {
	tests := []struct {
		name     string
		rawMode  dto.ResponsesCompactMode
		expected dto.ResponsesCompactMode
	}{
		{
			name:     "legacy convert",
			rawMode:  dto.ResponsesCompactMode("convert"),
			expected: dto.ResponsesCompactModeSynthetic,
		},
		{
			name:     "legacy auto",
			rawMode:  dto.ResponsesCompactMode("auto"),
			expected: dto.ResponsesCompactModeNative,
		},
		{
			name:     "disabled",
			rawMode:  dto.ResponsesCompactMode("disabled"),
			expected: dto.ResponsesCompactModeDisabled,
		},
		{
			name:     "legacy unsupported",
			rawMode:  dto.ResponsesCompactMode("unsupported"),
			expected: dto.ResponsesCompactModeNative,
		},
		{
			name:     "unknown",
			rawMode:  dto.ResponsesCompactMode("unexpected"),
			expected: dto.ResponsesCompactModeNative,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := dto.ChannelOtherSettings{
				ResponsesCompactMode: tt.rawMode,
			}

			require.Equal(t, tt.expected, settings.ResponsesCompactModeOrDefault())
			require.Equal(t, tt.expected == dto.ResponsesCompactModeNative, settings.HasNativeResponsesCompact())
			require.Equal(t, tt.expected == dto.ResponsesCompactModeSynthetic, settings.HasSyntheticResponsesCompact())
			require.Equal(t, tt.expected == dto.ResponsesCompactModeDisabled, settings.HasDisabledResponsesCompact())
		})
	}
}

func TestChannelOtherSettingsResponsesCompactFallbackControls(t *testing.T) {
	enabled := true
	disabled := false
	settings := dto.ChannelOtherSettings{
		ResponsesCompactContextFallback:       &disabled,
		ResponsesCompactSummaryModelFallback:  &enabled,
		ResponsesCompactSummaryFallbackModels: []string{" gpt-5.4 ", "", "gpt-5.4", "gpt-5.4-large"},
	}

	require.False(t, settings.ResponsesCompactContextFallbackEnabled())
	require.True(t, settings.ResponsesCompactSummaryModelFallbackEnabled())
	require.Equal(t, []string{"gpt-5.4", "gpt-5.4-large"}, settings.ResponsesCompactSummaryFallbackModelsOrDefault())

	settings.ResponsesCompactSummaryFallbackModels = []string{" ", ""}
	require.True(t, settings.ResponsesCompactSummaryModelFallbackEnabled())
	require.Equal(t, []string{"gpt-5.4"}, settings.ResponsesCompactSummaryFallbackModelsOrDefault())
}
