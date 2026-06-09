package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

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
