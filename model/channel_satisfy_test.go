package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

func withResponsesBootstrapCacheFixture(t *testing.T, channels map[int]*Channel) {
	t.Helper()

	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalGroupModelRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	originalChannelsIDM := channelsIDM

	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = true
	channelSyncLock.Lock()
	channelsIDM = channels
	channelSyncLock.Unlock()

	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = originalGroupModelRouteHelperEnabled
		channelSyncLock.Lock()
		channelsIDM = originalChannelsIDM
		channelSyncLock.Unlock()
	})
}

func TestHasResponsesBootstrapRecoveryEnabledChannelSkipsDisabledChannels(t *testing.T) {
	withResponsesBootstrapCacheFixture(t, map[int]*Channel{
		1: {
			Id:            1,
			Status:        common.ChannelStatusManuallyDisabled,
			Group:         "default",
			Models:        "gpt-5",
			OtherSettings: `{"responses_stream_bootstrap_recovery_enabled":true}`,
		},
		2: {
			Id:            2,
			Status:        common.ChannelStatusEnabled,
			Group:         "default",
			Models:        "gpt-5",
			OtherSettings: `{"responses_stream_bootstrap_recovery_enabled":false}`,
		},
	})

	if HasResponsesBootstrapRecoveryEnabledChannel([]string{"default"}, "gpt-5") {
		t.Fatal("disabled opted-in channel should not satisfy responses bootstrap recovery")
	}
}

func TestHasResponsesBootstrapRecoveryEnabledChannelAcceptsEnabledOptInChannel(t *testing.T) {
	withResponsesBootstrapCacheFixture(t, map[int]*Channel{
		1: {
			Id:            1,
			Status:        common.ChannelStatusEnabled,
			Group:         "default",
			Models:        "gpt-5",
			OtherSettings: `{"responses_stream_bootstrap_recovery_enabled":true}`,
		},
	})

	if !HasResponsesBootstrapRecoveryEnabledChannel([]string{"default"}, "gpt-5") {
		t.Fatal("enabled opted-in channel should satisfy responses bootstrap recovery")
	}
}

func TestHasResponsesBootstrapRecoveryCandidateChannelAcceptsDisabledOptInChannel(t *testing.T) {
	withResponsesBootstrapCacheFixture(t, map[int]*Channel{
		1: {
			Id:            1,
			Status:        common.ChannelStatusManuallyDisabled,
			Group:         "default",
			Models:        "gpt-5",
			OtherSettings: `{"responses_stream_bootstrap_recovery_enabled":true}`,
		},
	})

	if !HasResponsesBootstrapRecoveryCandidateChannel([]string{"default"}, "gpt-5") {
		t.Fatal("disabled opted-in channel should remain a bootstrap recovery candidate")
	}
}

func TestHasResponsesBootstrapRecoveryCandidateChannelRejectsNonOptInChannel(t *testing.T) {
	withResponsesBootstrapCacheFixture(t, map[int]*Channel{
		1: {
			Id:            1,
			Status:        common.ChannelStatusManuallyDisabled,
			Group:         "default",
			Models:        "gpt-5",
			OtherSettings: `{"responses_stream_bootstrap_recovery_enabled":false}`,
		},
	})

	if HasResponsesBootstrapRecoveryCandidateChannel([]string{"default"}, "gpt-5") {
		t.Fatal("non-opt-in channel should not satisfy bootstrap recovery candidate lookup")
	}
}

func TestHasResponsesBootstrapRecoveryCandidateChannelValidatesModelMapping(t *testing.T) {
	validMapping := `{"client-model":"provider/client-model"}`
	invalidMapping := `{"client-model":123}`
	withResponsesBootstrapCacheFixture(t, map[int]*Channel{
		1: {
			Id:            1,
			Status:        common.ChannelStatusManuallyDisabled,
			Group:         "default",
			Models:        "client-model",
			ModelMapping:  &validMapping,
			OtherSettings: `{"responses_stream_bootstrap_recovery_enabled":true}`,
		},
		2: {
			Id:            2,
			Status:        common.ChannelStatusManuallyDisabled,
			Group:         "default",
			Models:        "client-model",
			ModelMapping:  &invalidMapping,
			OtherSettings: `{"responses_stream_bootstrap_recovery_enabled":true}`,
		},
	})

	require.True(t, HasResponsesBootstrapRecoveryCandidateChannel([]string{"default"}, "client-model"))

	invalidOnly := `{"client-model":123}`
	withResponsesBootstrapCacheFixture(t, map[int]*Channel{
		3: {
			Id:            3,
			Status:        common.ChannelStatusManuallyDisabled,
			Group:         "default",
			Models:        "client-model",
			ModelMapping:  &invalidOnly,
			OtherSettings: `{"responses_stream_bootstrap_recovery_enabled":true}`,
		},
	})
	require.False(t, HasResponsesBootstrapRecoveryCandidateChannel([]string{"default"}, "client-model"))
}

func TestHasResponsesBootstrapRecoveryCandidateChannelRespectsRouteHelperFlag(t *testing.T) {
	withResponsesBootstrapCacheFixture(t, map[int]*Channel{
		1: {
			Id:            1,
			Status:        common.ChannelStatusEnabled,
			Group:         "default",
			Models:        "gpt-4o-gizmo-*",
			OtherSettings: `{"responses_stream_bootstrap_recovery_enabled":true}`,
		},
	})

	common.GroupModelRouteHelperEnabled = false
	if HasResponsesBootstrapRecoveryCandidateChannel([]string{"default"}, "gpt-4o-gizmo-special") {
		t.Fatal("disabled route helper should not match normalized bootstrap recovery models")
	}

	common.GroupModelRouteHelperEnabled = true
	if !HasResponsesBootstrapRecoveryCandidateChannel([]string{"default"}, "gpt-4o-gizmo-special") {
		t.Fatal("enabled route helper should match normalized bootstrap recovery models")
	}
}

func TestIsResponsesBootstrapRecoveryEnabledInOtherSettings(t *testing.T) {
	if !IsResponsesBootstrapRecoveryEnabledInOtherSettings(`{"responses_stream_bootstrap_recovery_enabled":true}`) {
		t.Fatal("raw settings should enable bootstrap recovery when opt-in is true")
	}
	if IsResponsesBootstrapRecoveryEnabledInOtherSettings(`{"responses_stream_bootstrap_recovery_enabled":false}`) {
		t.Fatal("raw settings should not enable bootstrap recovery when opt-in is false")
	}
	if IsResponsesBootstrapRecoveryEnabledInOtherSettings(`{invalid`) {
		t.Fatal("invalid raw settings should not enable bootstrap recovery")
	}
}

func TestIsChannelEnabledForGroupModelWithResolverUsesPreparedCompactCandidate(t *testing.T) {
	previousMemoryCache := common.MemoryCacheEnabled
	previousCandidates := group2model2channels
	previousChannels := channelsIDM
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCache
		group2model2channels = previousCandidates
		channelsIDM = previousChannels
	})

	common.MemoryCacheEnabled = true
	compactModel := ratio_setting.WithCompactModelSuffix("gpt-5.5")
	channel := &Channel{
		Id:     90401,
		Type:   constant.ChannelTypeCodex,
		Models: compactModel,
	}
	group2model2channels = map[string]map[string][]int{
		"default": {compactModel: {channel.Id}},
	}
	channelsIDM = map[int]*Channel{channel.Id: channel}

	resolverCalls := 0
	resolver := func(candidate *Channel, requested string, compact bool) (string, bool, error) {
		resolverCalls++
		require.Same(t, channel, candidate)
		require.Contains(t, []string{compactModel, "gpt-5.5"}, requested)
		require.True(t, compact)
		return "provider-compact", true, nil
	}

	require.True(t, IsChannelEnabledForGroupModelWithResolver("default", compactModel, channel.Id, resolver))
	require.NotZero(t, resolverCalls)
}

func TestIsChannelEnabledForGroupModelUsesCompleteCacheForNegativeResult(t *testing.T) {
	previousMemoryCache := common.MemoryCacheEnabled
	previousRouteCacheReady := routeCandidateCacheV2Ready
	previousGroupModelChannels := group2model2channels
	previousChannels := channelsIDM
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCache
		channelSyncLock.Lock()
		routeCandidateCacheV2Ready = previousRouteCacheReady
		group2model2channels = previousGroupModelChannels
		channelsIDM = previousChannels
		channelSyncLock.Unlock()
	})

	common.MemoryCacheEnabled = true
	channelSyncLock.Lock()
	routeCandidateCacheV2Ready = true
	group2model2channels = map[string]map[string][]int{
		"default": {"other-model": {9906}},
	}
	channelsIDM = map[int]*Channel{
		9906: {Id: 9906, Status: common.ChannelStatusEnabled},
	}
	channelSyncLock.Unlock()

	// A complete cache snapshot proves that this channel is not enabled for the
	// requested model. The function must return without consulting the database.
	require.False(t, IsChannelEnabledForGroupModelWithResolver("default", "missing-model", 9906, nil))
}
