package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/stretchr/testify/require"
)

func TestShouldRouteResponsesViaChatSkipsResponsesCompact(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})

	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Rules: []model_setting.ProtocolConversionRule{
			{
				Name:           "responses-to-chat",
				Enabled:        true,
				SourceEndpoint: model_setting.ProtocolEndpointResponses,
				TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
				AllChannels:    true,
				ModelPatterns:  []string{`^gpt-5(\..+)?$`},
			},
		},
	}

	responsesInfo := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   157,
			ChannelType: 1,
		},
	}
	require.True(t, shouldRouteResponsesViaChat(responsesInfo, false))

	compactInfo := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   157,
			ChannelType: 1,
		},
	}
	require.False(t, shouldRouteResponsesViaChat(compactInfo, false))
}

func TestFindResponsesViaChatRuleCarriesCustomToolBridgeOption(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})

	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Rules: []model_setting.ProtocolConversionRule{
			{
				Name:           "responses-to-chat-codex",
				Enabled:        true,
				SourceEndpoint: model_setting.ProtocolEndpointResponses,
				TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
				AllChannels:    true,
				ModelPatterns:  []string{`^gpt-5(\..+)?$`},
				Options: &model_setting.ProtocolConversionOptions{
					EnableCustomToolBridge: true,
				},
			},
		},
	}

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   157,
			ChannelType: 1,
		},
	}

	rule := findResponsesViaChatRule(info, false)
	require.NotNil(t, rule)
	require.True(t, responsesViaChatOptionsFromRule(rule).EnableCustomToolBridge)
	require.False(t, responsesViaChatOptionsFromRule(nil).EnableCustomToolBridge)
}

func TestShouldConvertResponsesRequestForCodexEncryptedContextStrip(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				StripCodexEncryptedContext: true,
			},
		},
	}
	require.True(t, relaycommon.ShouldConvertResponsesRequest(info))

	info.ChannelOtherSettings.StripCodexEncryptedContext = false
	require.False(t, relaycommon.ShouldConvertResponsesRequest(info))

	info.RelayMode = relayconstant.RelayModeResponsesCompact
	info.ChannelOtherSettings.StripCodexEncryptedContext = true
	info.ChannelType = constant.ChannelTypeCodex
	require.False(t, relaycommon.ShouldConvertResponsesRequest(info))

	info.ChannelType = constant.ChannelTypeOpenAI
	require.True(t, relaycommon.ShouldConvertResponsesRequest(info))

	info.ChannelType = constant.ChannelTypeAzure
	require.True(t, relaycommon.ShouldConvertResponsesRequest(info))

	info.ChannelOtherSettings.StripCodexEncryptedContext = false
	require.False(t, relaycommon.ShouldConvertResponsesRequest(info))

	info.ChannelType = constant.ChannelTypeOpenAI
	require.False(t, relaycommon.ShouldConvertResponsesRequest(info))

	info.ChannelOtherSettings.ResponsesCompactMode = dto.ResponsesCompactModeNative
	require.False(t, relaycommon.ShouldConvertResponsesRequest(info))
}
