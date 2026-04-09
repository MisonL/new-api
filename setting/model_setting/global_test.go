package model_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeChatCompletionsToResponsesPolicyJSON_ValidRule(t *testing.T) {
	raw := `{
		"rules": [
			{
				"name": "chat-to-responses",
				"enabled": true,
				"source_endpoint": "/v1/chat/completions",
				"target_endpoint": "responses",
				"all_channels": false,
				"channel_ids": [3, 3, -1, 0, 2],
				"channel_types": [1, 1, 0],
				"model_patterns": [" ^gpt-5.*$ ", "", "   "]
			}
		]
	}`

	normalized, err := NormalizeChatCompletionsToResponsesPolicyJSON(raw)
	require.NoError(t, err)

	var policy ChatCompletionsToResponsesPolicy
	require.NoError(t, common.UnmarshalJsonStr(normalized, &policy))
	require.Len(t, policy.Rules, 1)

	rule := policy.Rules[0]
	assert.Equal(t, ProtocolEndpointChatCompletions, rule.SourceEndpoint)
	assert.Equal(t, ProtocolEndpointResponses, rule.TargetEndpoint)
	assert.Equal(t, []int{3, 2}, rule.ChannelIDs)
	assert.Equal(t, []int{1}, rule.ChannelTypes)
	assert.Equal(t, []string{"^gpt-5.*$"}, rule.ModelPatterns)
}

func TestNormalizeChatCompletionsToResponsesPolicyJSON_RejectSameEndpoint(t *testing.T) {
	raw := `{
		"rules": [
			{
				"name": "invalid-rule",
				"enabled": true,
				"source_endpoint": "responses",
				"target_endpoint": "responses",
				"all_channels": true,
				"model_patterns": ["^gpt-5.*$"]
			}
		]
	}`

	_, err := NormalizeChatCompletionsToResponsesPolicyJSON(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不能相同")
}

func TestNormalizeChatCompletionsToResponsesPolicyJSON_RejectUnsupportedEndpoint(t *testing.T) {
	raw := `{
		"rules": [
			{
				"name": "invalid-endpoint",
				"enabled": true,
				"source_endpoint": "custom",
				"target_endpoint": "responses",
				"all_channels": true,
				"model_patterns": ["^gpt-5.*$"]
			}
		]
	}`

	_, err := NormalizeChatCompletionsToResponsesPolicyJSON(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "源协议不支持")
}

func TestNormalizeChatCompletionsToResponsesPolicyJSON_EmptyInput(t *testing.T) {
	normalized, err := NormalizeChatCompletionsToResponsesPolicyJSON("")
	require.NoError(t, err)
	assert.Equal(t, `{"enabled":false,"all_channels":false}`, normalized)
}
