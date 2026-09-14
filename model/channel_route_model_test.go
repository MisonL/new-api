package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

func TestResponsesCompactionNamespaceUnsupportedPreferenceDefaultsOn(t *testing.T) {
	channel := &Channel{Type: constant.ChannelTypeOpenAI}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileGenericProxy,
	})

	require.True(t, channelPrefersResponsesCompactionWhenNamespaceUnsupported(channel))
	require.True(t, IsResponsesCompactionNamespacePreferred(channel, ratio_setting.WithCompactModelSuffix("gpt-5.5")))

	disabled := false
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile:                             dto.ResponsesUpstreamProfileGenericProxy,
		PreferForResponsesCompactionWhenNamespaceUnsupported: &disabled,
	})
	require.False(t, channelPrefersResponsesCompactionWhenNamespaceUnsupported(channel))
}

func TestGroupModelRouteCandidatesRejectWhitespaceOnlyModel(t *testing.T) {
	for _, modelName := range []string{" ", "\t", "\n", " \t\n "} {
		require.Empty(t, getGroupModelRouteCandidateMeta(modelName), modelName)
		require.Empty(t, getGroupModelRouteCandidates(modelName), modelName)
	}
}

func TestResponsesCompactionNamespaceUnknownIsNotPreferred(t *testing.T) {
	channel := &Channel{Type: constant.ChannelTypeOpenAI}
	channel.SetOtherSettings(dto.ChannelOtherSettings{})

	require.False(t, channelPrefersResponsesCompactionWhenNamespaceUnsupported(channel))
	require.False(t, IsResponsesCompactionNamespacePreferred(channel, ratio_setting.WithCompactModelSuffix("gpt-5.5")))
}

func TestResponsesCompactionNamespacePreferenceUsesExplicitCapabilityOverride(t *testing.T) {
	unsupported := false
	channel := &Channel{Type: constant.ChannelTypeOpenAI}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialOpenAI,
		ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
			SupportsNamespaceTools: &unsupported,
		},
	})

	require.True(t, channelPrefersResponsesCompactionWhenNamespaceUnsupported(channel))

	supported := true
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileGenericProxy,
		ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
			SupportsNamespaceTools: &supported,
		},
	})
	require.False(t, channelPrefersResponsesCompactionWhenNamespaceUnsupported(channel))
}

func TestPreferResponsesCompactionChannelsFallsBackAfterPreferredExclusion(t *testing.T) {
	preferred := &Channel{Id: 1, Type: constant.ChannelTypeOpenAI}
	preferred.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileGenericProxy,
	})
	regular := &Channel{Id: 2, Type: constant.ChannelTypeOpenAI}
	regular.SetOtherSettings(dto.ChannelOtherSettings{})
	channels := []*Channel{regular, preferred}

	require.Equal(t, []*Channel{preferred}, preferResponsesCompactionChannels(channels, true, nil))
	require.Equal(t, []*Channel{regular}, preferResponsesCompactionChannels(channels, true, map[int]struct{}{preferred.Id: {}}))
	require.Equal(t, channels, preferResponsesCompactionChannels(channels, false, nil))
}

func TestChannelSupportsCompactRouteCandidateDisabledOverridesProxyProfile(t *testing.T) {
	channel := &Channel{
		Type:   constant.ChannelTypeOpenAI,
		Models: "gpt-5.5",
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode:     dto.ResponsesCompactModeDisabled,
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileGenericProxy,
	})

	require.True(t, channelSupportsCompactRouteCandidate(channel, routeModelCandidate{model: "gpt-5.5"}))
	require.False(t, channelSupportsCompactRouteCandidate(channel, routeModelCandidate{
		model:          ratio_setting.WithCompactModelSuffix("gpt-5.5"),
		compactRequest: true,
	}))
}

func TestChannelSupportsCompactRouteCandidateAllowsAzure(t *testing.T) {
	channel := &Channel{
		Type:   constant.ChannelTypeAzure,
		Models: "gpt-5.5",
	}

	require.True(t, channelSupportsCompactRouteCandidate(channel, routeModelCandidate{
		model:          ratio_setting.WithCompactModelSuffix("gpt-5.5"),
		compactRequest: true,
	}))
}

func TestChannelSupportsCompactRouteCandidateAutoFallbackUsesSyntheticOnly(t *testing.T) {
	now := time.Now()
	compactModel := ratio_setting.WithCompactModelSuffix("gpt-5.5")
	settings := dto.ChannelOtherSettings{
		ResponsesCompactMode:                           dto.ResponsesCompactModeAuto,
		ResponsesCompactAutoFallbackAt:                 now.Unix(),
		ResponsesCompactAutoFallbackRetryIntervalHours: 3,
	}

	compactOnlyChannel := &Channel{
		Type:   constant.ChannelTypeOpenAI,
		Models: compactModel,
	}
	compactOnlyChannel.SetOtherSettings(settings)

	require.False(t, channelSupportsCompactRouteCandidate(compactOnlyChannel, routeModelCandidate{
		model:          compactModel,
		compactRequest: true,
	}))

	baseChannel := &Channel{
		Type:   constant.ChannelTypeOpenAI,
		Models: "gpt-5.5",
	}
	baseChannel.SetOtherSettings(settings)

	require.True(t, channelSupportsCompactRouteCandidate(baseChannel, routeModelCandidate{
		model:          compactModel,
		compactRequest: true,
	}))
	require.True(t, channelSupportsCompactRouteCandidate(baseChannel, routeModelCandidate{
		model:           "gpt-5.5",
		compactRequest:  true,
		compactFallback: true,
	}))
}

func TestChannelSupportsCompactRouteCandidateAutoWithoutFallbackAllowsNative(t *testing.T) {
	compactModel := ratio_setting.WithCompactModelSuffix("gpt-5.5")
	channel := &Channel{
		Type:   constant.ChannelTypeOpenAI,
		Models: compactModel,
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeAuto,
	})

	require.True(t, channelSupportsCompactRouteCandidate(channel, routeModelCandidate{
		model:          compactModel,
		compactRequest: true,
	}))
}

func TestChannelSupportsCompactRouteCandidateAutoAllowsBaseModelAfterExpiredFallback(t *testing.T) {
	compactModel := ratio_setting.WithCompactModelSuffix("gpt-5.5")
	channel := &Channel{
		Type:   constant.ChannelTypeOpenAI,
		Models: "gpt-5.5",
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode:                           dto.ResponsesCompactModeAuto,
		ResponsesCompactAutoFallbackAt:                 time.Now().Add(-4 * time.Hour).Unix(),
		ResponsesCompactAutoFallbackRetryIntervalHours: 3,
		UpstreamModelUpdateCheckEnabled:                true,
		UpstreamModelUpdateLastCheckTime:               time.Now().Add(-time.Hour).Unix(),
	})

	require.True(t, channelSupportsCompactRouteCandidate(channel, routeModelCandidate{
		model:          compactModel,
		compactRequest: true,
	}))
	require.True(t, channelSupportsCompactRouteCandidate(channel, routeModelCandidate{
		model:           "gpt-5.5",
		compactRequest:  true,
		compactFallback: true,
	}))
}

func TestChannelSupportsCompactRouteCandidateAutoBaseFallbackHonorsCompactSignals(t *testing.T) {
	compactModel := ratio_setting.WithCompactModelSuffix("gpt-5.5")
	channel := &Channel{
		Type:   constant.ChannelTypeOpenAI,
		Models: "gpt-5.5",
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode:             dto.ResponsesCompactModeAuto,
		UpstreamModelUpdateCheckEnabled:  true,
		UpstreamModelUpdateLastCheckTime: time.Now().Unix(),
		UpstreamModelUpdateLastDetectedModels: []string{
			ratio_setting.WithCompactModelSuffix("gpt-5.4"),
		},
	})

	require.False(t, channelSupportsCompactRouteCandidate(channel, routeModelCandidate{
		model:          compactModel,
		compactRequest: true,
	}))
	require.False(t, channelSupportsCompactRouteCandidate(channel, routeModelCandidate{
		model:           "gpt-5.5",
		compactRequest:  true,
		compactFallback: true,
	}))
}
