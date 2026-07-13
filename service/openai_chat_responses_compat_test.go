package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/stretchr/testify/require"
)

func TestFindResponsesViaChatRuleTrimsSyntheticPreviousResponseID(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	previousPolicy := settings.ChatCompletionsToResponsesPolicy
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: true,
		Rules: []model_setting.ProtocolConversionRule{
			{
				Name:           "responses-to-chat",
				Enabled:        true,
				SourceEndpoint: model_setting.ProtocolEndpointResponses,
				TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
				AllChannels:    true,
				ModelPatterns:  []string{"^gpt-5\\.5$"},
			},
		},
	}
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = previousPolicy
	})

	rule, err := FindResponsesViaChatRule(
		context.Background(),
		relayconstant.RelayModeResponses,
		false,
		dto.ChannelSettings{},
		160,
		0,
		"gpt-5.5",
		&dto.OpenAIResponsesRequest{
			PreviousResponseID: "  resp_newapi_synthcmp_nabc123_123  ",
		},
	)

	require.NoError(t, err)
	require.NotNil(t, rule)
	require.Equal(t, "responses-to-chat", rule.Name)
}

func TestFindResponsesViaChatRuleOnlyWhenToolBridgeRequiredSkipsOrdinaryRequest(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	previousPolicy := settings.ChatCompletionsToResponsesPolicy
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = previousPolicy
	})

	var policy model_setting.ChatCompletionsToResponsesPolicy
	require.NoError(t, common.Unmarshal([]byte(`{
		"enabled": true,
		"rules": [{
			"name": "custom-tools-only",
			"enabled": true,
			"source_endpoint": "responses",
			"target_endpoint": "chat_completions",
			"channel_ids": [205],
			"model_patterns": ["^gpt-5\\.5$"],
			"options": {
				"enable_custom_tool_bridge": true,
				"only_when_tool_bridge_required": true
			}
		}]
	}`), &policy))
	settings.ChatCompletionsToResponsesPolicy = policy

	rule, err := FindResponsesViaChatRule(
		context.Background(),
		relayconstant.RelayModeResponses,
		false,
		dto.ChannelSettings{},
		205,
		1,
		"gpt-5.5",
		&dto.OpenAIResponsesRequest{Input: common.RawMessage(`[{"type":"message","role":"user","content":"plain request"}]`)},
	)

	require.NoError(t, err)
	require.Nil(t, rule)
}

func TestFindResponsesViaChatRuleOnlyWhenToolBridgeRequiredSkipsFunctionOnlyAdditionalTools(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	previousPolicy := settings.ChatCompletionsToResponsesPolicy
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = previousPolicy
	})

	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: true,
		Rules: []model_setting.ProtocolConversionRule{{
			Name:           "custom-tools-only",
			Enabled:        true,
			SourceEndpoint: model_setting.ProtocolEndpointResponses,
			TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
			ChannelIDs:     []int{205},
			ModelPatterns:  []string{"^gpt-5\\.5$"},
			Options: &model_setting.ProtocolConversionOptions{
				EnableCustomToolBridge:     true,
				OnlyWhenToolBridgeRequired: true,
			},
		}},
	}

	rule, err := FindResponsesViaChatRule(
		context.Background(),
		relayconstant.RelayModeResponses,
		false,
		dto.ChannelSettings{},
		205,
		1,
		"gpt-5.5",
		&dto.OpenAIResponsesRequest{Input: common.RawMessage(`[
			{"type":"additional_tools","role":"developer","tools":[
				{"type":"function","name":"lookup","parameters":{"type":"object"}}
			]}
		]`)},
	)

	require.NoError(t, err)
	require.Nil(t, rule)
}

func TestFindResponsesViaChatRuleOnlyWhenToolBridgeRequiredAllowsOrdinaryInputItems(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	previousPolicy := settings.ChatCompletionsToResponsesPolicy
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = previousPolicy
	})

	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: true,
		Rules: []model_setting.ProtocolConversionRule{{
			Name:           "custom-tools-only",
			Enabled:        true,
			SourceEndpoint: model_setting.ProtocolEndpointResponses,
			TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
			ChannelIDs:     []int{205},
			ModelPatterns:  []string{"^gpt-5\\.5$"},
			Options: &model_setting.ProtocolConversionOptions{
				EnableCustomToolBridge:     true,
				OnlyWhenToolBridgeRequired: true,
			},
		}},
	}

	rule, err := FindResponsesViaChatRule(
		context.Background(),
		relayconstant.RelayModeResponses,
		false,
		dto.ChannelSettings{},
		205,
		1,
		"gpt-5.5",
		&dto.OpenAIResponsesRequest{Input: common.RawMessage(`[
			"plain input",
			{"role":"user","content":"ordinary message"}
		]`)},
	)

	require.NoError(t, err)
	require.Nil(t, rule)
}

func TestFindResponsesViaChatRuleOnlyWhenToolBridgeRequiredMatchesStringCustomTool(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	previousPolicy := settings.ChatCompletionsToResponsesPolicy
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = previousPolicy
	})

	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: true,
		Rules: []model_setting.ProtocolConversionRule{{
			Name:           "custom-tools-only",
			Enabled:        true,
			SourceEndpoint: model_setting.ProtocolEndpointResponses,
			TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
			ChannelIDs:     []int{205},
			ModelPatterns:  []string{"^gpt-5\\.5$"},
			Options: &model_setting.ProtocolConversionOptions{
				EnableCustomToolBridge:     true,
				OnlyWhenToolBridgeRequired: true,
			},
		}},
	}

	rule, err := FindResponsesViaChatRule(
		context.Background(),
		relayconstant.RelayModeResponses,
		false,
		dto.ChannelSettings{},
		205,
		1,
		"gpt-5.5",
		&dto.OpenAIResponsesRequest{
			Tools: common.RawMessage(`["exec"]`),
			Input: common.RawMessage(`[
				{"type":"message","role":"user","content":"run command"}
			]`),
		},
	)

	require.NoError(t, err)
	require.NotNil(t, rule)
}

func TestFindResponsesViaChatRuleOnlyWhenToolBridgeRequiredMatchesNamespacedFunctionHistory(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	previousPolicy := settings.ChatCompletionsToResponsesPolicy
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = previousPolicy
	})

	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: true,
		Rules: []model_setting.ProtocolConversionRule{{
			Name:           "custom-tools-only",
			Enabled:        true,
			SourceEndpoint: model_setting.ProtocolEndpointResponses,
			TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
			ChannelIDs:     []int{205},
			ModelPatterns:  []string{"^gpt-5\\.5$"},
			Options: &model_setting.ProtocolConversionOptions{
				EnableCustomToolBridge:     true,
				OnlyWhenToolBridgeRequired: true,
			},
		}},
	}

	rule, err := FindResponsesViaChatRule(
		context.Background(),
		relayconstant.RelayModeResponses,
		false,
		dto.ChannelSettings{},
		205,
		1,
		"gpt-5.5",
		&dto.OpenAIResponsesRequest{Input: common.RawMessage(`[
			{"type":"function_call","call_id":"call_1","namespace":"workspace","name":"read_file","arguments":"{\"path\":\"README.md\"}"},
			{"type":"function_call_output","call_id":"call_1","output":"contents"}
		]`)},
	)

	require.NoError(t, err)
	require.NotNil(t, rule)
}

func TestFindResponsesViaChatRuleRejectsLocalChatCompatibilityPreviousResponseID(t *testing.T) {
	rule, err := FindResponsesViaChatRule(
		context.Background(),
		relayconstant.RelayModeResponses,
		false,
		dto.ChannelSettings{},
		205,
		1,
		"gpt-5.5",
		&dto.OpenAIResponsesRequest{PreviousResponseID: "resp_newapi_chatcmp_test"},
	)

	require.Nil(t, rule)
	require.ErrorContains(t, err, "chat compatibility")
}

func TestFindResponsesViaChatRuleRejectsLocalChatCompatibilityPreviousIDBeforeCompactionTrigger(t *testing.T) {
	rule, err := FindResponsesViaChatRule(
		context.Background(),
		relayconstant.RelayModeResponses,
		false,
		dto.ChannelSettings{},
		205,
		1,
		"gpt-5.5",
		&dto.OpenAIResponsesRequest{
			PreviousResponseID: "resp_newapi_chatcmp_test",
			Input:              common.RawMessage(`[{"type":"compaction_trigger"}]`),
		},
	)

	require.Nil(t, rule)
	require.ErrorIs(t, err, ErrResponsesChatCompatibilityPreviousIDUnsupported)
}

func TestFindResponsesViaChatRuleRejectsLocalChatCompatibilityPreviousIDWhenPassThroughEnabled(t *testing.T) {
	rule, err := FindResponsesViaChatRule(
		context.Background(),
		relayconstant.RelayModeResponses,
		true,
		dto.ChannelSettings{},
		205,
		1,
		"gpt-5.5",
		&dto.OpenAIResponsesRequest{PreviousResponseID: "resp_newapi_chatcmp_test"},
	)

	require.Nil(t, rule)
	require.ErrorIs(t, err, ErrResponsesChatCompatibilityPreviousIDUnsupported)
}

func TestFindResponsesViaChatRuleOnlyWhenToolBridgeRequiredMatchesCustomToolsAndHistory(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	previousPolicy := settings.ChatCompletionsToResponsesPolicy
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = previousPolicy
	})

	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: true,
		Rules: []model_setting.ProtocolConversionRule{{
			Name:           "custom-tools-only",
			Enabled:        true,
			SourceEndpoint: model_setting.ProtocolEndpointResponses,
			TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
			ChannelIDs:     []int{205},
			ModelPatterns:  []string{"^gpt-5\\.5$"},
			Options: &model_setting.ProtocolConversionOptions{
				EnableCustomToolBridge:     true,
				OnlyWhenToolBridgeRequired: true,
			},
		}},
	}

	testCases := []struct {
		name    string
		request *dto.OpenAIResponsesRequest
	}{
		{
			name: "additional custom tool",
			request: &dto.OpenAIResponsesRequest{Input: common.RawMessage(`[
				{"type":"additional_tools","tools":[{"type":"custom","name":"exec"}]}
			]`)},
		},
		{
			name: "additional namespace tool",
			request: &dto.OpenAIResponsesRequest{Input: common.RawMessage(`[
				{"type":"additional_tools","tools":[{"type":"namespace","name":"workspace","tools":[{"type":"function","name":"read_file","parameters":{"type":"object"}}]}]}
			]`)},
		},
		{
			name: "additional tool search",
			request: &dto.OpenAIResponsesRequest{Input: common.RawMessage(`[
				{"type":"additional_tools","tools":[{"type":"tool_search","name":"search"}]}
			]`)},
		},
		{
			name: "custom tool history",
			request: &dto.OpenAIResponsesRequest{Input: common.RawMessage(`[
				{"type":"custom_tool_call","call_id":"call_1","name":"exec","input":"pwd"},
				{"type":"custom_tool_call_output","call_id":"call_1","output":"/repo"}
			]`)},
		},
		{
			name: "tool search history",
			request: &dto.OpenAIResponsesRequest{Input: common.RawMessage(`[
				{"type":"tool_search_call","call_id":"call_1","name":"search","arguments":"{}"},
				{"type":"tool_search_output","call_id":"call_1","output":"[]"}
			]`)},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			rule, err := FindResponsesViaChatRule(
				context.Background(),
				relayconstant.RelayModeResponses,
				false,
				dto.ChannelSettings{},
				205,
				1,
				"gpt-5.5",
				testCase.request,
			)

			require.NoError(t, err)
			require.NotNil(t, rule)
			require.Equal(t, "custom-tools-only", rule.Name)
		})
	}
}

func TestFindResponsesViaChatRuleOnlyWhenToolBridgeRequiredFallsThroughToLaterRule(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	previousPolicy := settings.ChatCompletionsToResponsesPolicy
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = previousPolicy
	})

	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: true,
		Rules: []model_setting.ProtocolConversionRule{
			{
				Name:           "custom-tools-only",
				Enabled:        true,
				SourceEndpoint: model_setting.ProtocolEndpointResponses,
				TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
				ChannelIDs:     []int{205},
				Options: &model_setting.ProtocolConversionOptions{
					EnableCustomToolBridge:     true,
					OnlyWhenToolBridgeRequired: true,
				},
			},
			{
				Name:           "fallback",
				Enabled:        true,
				SourceEndpoint: model_setting.ProtocolEndpointResponses,
				TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
				ChannelIDs:     []int{205},
			},
		},
	}

	rule, err := FindResponsesViaChatRule(
		context.Background(),
		relayconstant.RelayModeResponses,
		false,
		dto.ChannelSettings{},
		205,
		1,
		"gpt-5.5",
		&dto.OpenAIResponsesRequest{Input: common.RawMessage(`[{"type":"message","role":"user","content":"plain request"}]`)},
	)

	require.NoError(t, err)
	require.NotNil(t, rule)
	require.Equal(t, "fallback", rule.Name)
}

func TestFindResponsesViaChatRuleOnlyWhenToolBridgeRequiredSkipsRemoteCompaction(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	previousPolicy := settings.ChatCompletionsToResponsesPolicy
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = previousPolicy
	})

	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: true,
		Rules: []model_setting.ProtocolConversionRule{{
			Name:           "custom-tools-only",
			Enabled:        true,
			SourceEndpoint: model_setting.ProtocolEndpointResponses,
			TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
			ChannelIDs:     []int{205},
			Options: &model_setting.ProtocolConversionOptions{
				EnableCustomToolBridge:     true,
				OnlyWhenToolBridgeRequired: true,
			},
		}},
	}

	rule, err := FindResponsesViaChatRule(
		context.Background(),
		relayconstant.RelayModeResponses,
		false,
		dto.ChannelSettings{},
		205,
		1,
		"gpt-5.5",
		&dto.OpenAIResponsesRequest{Input: common.RawMessage(`[
			{"type":"compaction","encrypted_content":"opaque-native-compact"},
			{"type":"custom_tool_call","call_id":"call_1","name":"exec","input":"pwd"}
		]`)},
	)

	require.NoError(t, err)
	require.Nil(t, rule)
}

func TestFindResponsesViaChatRuleOnlyWhenToolBridgeRequiredRejectsMalformedToolFields(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	previousPolicy := settings.ChatCompletionsToResponsesPolicy
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = previousPolicy
	})

	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: true,
		Rules: []model_setting.ProtocolConversionRule{{
			Name:           "custom-tools-only",
			Enabled:        true,
			SourceEndpoint: model_setting.ProtocolEndpointResponses,
			TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
			ChannelIDs:     []int{205},
			Options: &model_setting.ProtocolConversionOptions{
				EnableCustomToolBridge:     true,
				OnlyWhenToolBridgeRequired: true,
			},
		}},
	}

	for _, request := range []*dto.OpenAIResponsesRequest{
		{Input: common.RawMessage(`[{"type":"additional_tools","tools":{}}]`)},
		{Input: common.RawMessage(`[{"type":"message","role":"user","content":"plain request"}]`), ToolChoice: common.RawMessage(`42`)},
	} {
		rule, err := FindResponsesViaChatRule(
			context.Background(),
			relayconstant.RelayModeResponses,
			false,
			dto.ChannelSettings{},
			205,
			1,
			"gpt-5.5",
			request,
		)

		require.Nil(t, rule)
		require.ErrorIs(t, err, ErrResponsesChatCompatibilityToolBridgeRequestInvalid)
	}
}
