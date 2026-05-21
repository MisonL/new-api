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
				"source_endpoint": "responses",
				"target_endpoint": "/v1/chat/completions",
				"all_channels": false,
				"channel_ids": [3, 3, 2],
				"channel_types": [1, 1],
				"model_patterns": [" ^gpt-5.*$ ", "", "   "],
				"options": {
					"enable_custom_tool_bridge": true
				}
			}
		]
	}`

	normalized, err := NormalizeChatCompletionsToResponsesPolicyJSON(raw)
	require.NoError(t, err)

	var policy ChatCompletionsToResponsesPolicy
	require.NoError(t, common.UnmarshalJsonStr(normalized, &policy))
	require.Len(t, policy.Rules, 1)

	rule := policy.Rules[0]
	assert.Equal(t, ProtocolEndpointResponses, rule.SourceEndpoint)
	assert.Equal(t, ProtocolEndpointChatCompletions, rule.TargetEndpoint)
	assert.Equal(t, []int{3, 2}, rule.ChannelIDs)
	assert.Equal(t, []int{1}, rule.ChannelTypes)
	assert.Equal(t, []string{"^gpt-5.*$"}, rule.ModelPatterns)
	require.NotNil(t, rule.Options)
	assert.True(t, rule.Options.EnableCustomToolBridge)
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

func TestNormalizeChatCompletionsToResponsesPolicyJSON_RejectInvalidChannelID(t *testing.T) {
	raw := `{
		"rules": [
			{
				"name": "invalid-channel",
				"enabled": true,
				"source_endpoint": "responses",
				"target_endpoint": "chat_completions",
				"all_channels": false,
				"channel_ids": [3, -1],
				"model_patterns": ["^gpt-5.*$"]
			}
		]
	}`

	_, err := NormalizeChatCompletionsToResponsesPolicyJSON(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "channel_ids")
}

func TestNormalizeChatCompletionsToResponsesPolicyJSON_RejectInvalidRegex(t *testing.T) {
	raw := `{
		"rules": [
			{
				"name": "invalid-regex",
				"enabled": true,
				"source_endpoint": "responses",
				"target_endpoint": "chat_completions",
				"all_channels": true,
				"model_patterns": ["["]
			}
		]
	}`

	_, err := NormalizeChatCompletionsToResponsesPolicyJSON(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "正则不合法")
}

func TestNormalizeChatCompletionsToResponsesPolicyJSON_RejectCustomToolBridgeForWrongDirection(t *testing.T) {
	raw := `{
		"rules": [
			{
				"name": "chat-to-responses",
				"enabled": true,
				"source_endpoint": "chat_completions",
				"target_endpoint": "responses",
				"all_channels": true,
				"model_patterns": ["^gpt-5.*$"],
				"options": {
					"enable_custom_tool_bridge": true
				}
			}
		]
	}`

	_, err := NormalizeChatCompletionsToResponsesPolicyJSON(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "enable_custom_tool_bridge")
}

func TestNormalizeChatCompletionsToResponsesPolicyJSON_PreservesUnknownFields(t *testing.T) {
	raw := `{
		"future_policy": {"enabled": true},
		"rules": [
			{
				"name": " responses-to-chat ",
				"enabled": true,
				"source_endpoint": "responses",
				"target_endpoint": "chat_completions",
				"all_channels": false,
				"channel_ids": [117],
				"model_patterns": [" ^gpt-5.*$ "],
				"future_rule": "keep",
				"options": {
					"enable_custom_tool_bridge": true,
					"some_future_field": "keep"
				}
			}
		]
	}`

	normalized, err := NormalizeChatCompletionsToResponsesPolicyJSON(raw)
	require.NoError(t, err)

	var policyRaw map[string]common.RawMessage
	require.NoError(t, common.Unmarshal([]byte(normalized), &policyRaw))
	assert.JSONEq(t, `{"enabled":true}`, string(policyRaw["future_policy"]))

	var rules []map[string]common.RawMessage
	require.NoError(t, common.Unmarshal(policyRaw["rules"], &rules))
	require.Len(t, rules, 1)
	assert.JSONEq(t, `"keep"`, string(rules[0]["future_rule"]))
	assert.JSONEq(t, `"responses-to-chat"`, string(rules[0]["name"]))
	assert.JSONEq(t, `["^gpt-5.*$"]`, string(rules[0]["model_patterns"]))

	var options map[string]common.RawMessage
	require.NoError(t, common.Unmarshal(rules[0]["options"], &options))
	assert.JSONEq(t, `true`, string(options["enable_custom_tool_bridge"]))
	assert.JSONEq(t, `"keep"`, string(options["some_future_field"]))
}

func TestNormalizeChatCompletionsToResponsesPolicyJSON_DoesNotAttachUnknownFieldsToDifferentRule(t *testing.T) {
	raw := `{
		"rules": [
			{
				"name": " first ",
				"enabled": true,
				"source_endpoint": "responses",
				"target_endpoint": "chat_completions",
				"all_channels": true,
				"model_patterns": ["^gpt-5.*$"],
				"future_rule": "first-only"
			},
			{
				"name": "second",
				"enabled": true,
				"source_endpoint": "chat_completions",
				"target_endpoint": "responses",
				"all_channels": true,
				"model_patterns": ["^gpt-4o.*$"]
			}
		]
	}`

	normalized, err := NormalizeChatCompletionsToResponsesPolicyJSON(raw)
	require.NoError(t, err)

	var policyRaw map[string]common.RawMessage
	require.NoError(t, common.Unmarshal([]byte(normalized), &policyRaw))
	var rules []map[string]common.RawMessage
	require.NoError(t, common.Unmarshal(policyRaw["rules"], &rules))
	require.Len(t, rules, 2)
	assert.JSONEq(t, `"first-only"`, string(rules[0]["future_rule"]))
	assert.NotContains(t, rules[1], "future_rule")
}

func TestNormalizeProtocolRulesRaw_DropsUnknownFieldsWhenRuleIdentityChanges(t *testing.T) {
	original := map[string]common.RawMessage{
		"rules": common.RawMessage(`[
			{
				"name": "first",
				"enabled": true,
				"source_endpoint": "responses",
				"target_endpoint": "chat_completions",
				"all_channels": true,
				"model_patterns": ["^gpt-5.*$"],
				"future_rule": "first-only"
			}
		]`),
	}
	normalized := map[string]common.RawMessage{
		"rules": common.RawMessage(`[
			{
				"name": "second",
				"enabled": true,
				"source_endpoint": "chat_completions",
				"target_endpoint": "responses",
				"all_channels": true,
				"model_patterns": ["^gpt-4o.*$"]
			}
		]`),
	}

	require.NoError(t, normalizeProtocolRulesRaw(original, normalized))

	var rules []map[string]common.RawMessage
	require.NoError(t, common.Unmarshal(original["rules"], &rules))
	require.Len(t, rules, 1)
	assert.JSONEq(t, `"second"`, string(rules[0]["name"]))
	assert.NotContains(t, rules[0], "future_rule")
}

func TestNormalizeProtocolRulesRaw_PreservesUnknownFieldsWhenNameMissing(t *testing.T) {
	original := map[string]common.RawMessage{
		"rules": common.RawMessage(`[
			{
				"enabled": true,
				"source_endpoint": "responses",
				"target_endpoint": "chat_completions",
				"all_channels": true,
				"model_patterns": ["^gpt-5.*$"],
				"future_rule": "keep-me",
				"options": {
					"future_option": "keep-option"
				}
			},
			{
				"enabled": true,
				"source_endpoint": "chat_completions",
				"target_endpoint": "responses",
				"all_channels": true,
				"model_patterns": ["^gpt-4o.*$"],
				"future_rule": "keep-reverse"
			}
		]`),
	}
	normalized := map[string]common.RawMessage{
		"rules": common.RawMessage(`[
			{
				"enabled": false,
				"source_endpoint": "responses",
				"target_endpoint": "chat_completions",
				"all_channels": true,
				"model_patterns": ["^gpt-5.*$"],
				"options": {}
			},
			{
				"enabled": false,
				"source_endpoint": "chat_completions",
				"target_endpoint": "responses",
				"all_channels": true,
				"model_patterns": ["^gpt-4o.*$"]
			}
		]`),
	}

	require.NoError(t, normalizeProtocolRulesRaw(original, normalized))

	var rules []map[string]common.RawMessage
	require.NoError(t, common.Unmarshal(original["rules"], &rules))
	require.Len(t, rules, 2)
	assert.JSONEq(t, `false`, string(rules[0]["enabled"]))
	assert.JSONEq(t, `"keep-me"`, string(rules[0]["future_rule"]))
	assert.JSONEq(t, `false`, string(rules[1]["enabled"]))
	assert.JSONEq(t, `"keep-reverse"`, string(rules[1]["future_rule"]))

	var options map[string]common.RawMessage
	require.NoError(t, common.Unmarshal(rules[0]["options"], &options))
	assert.JSONEq(t, `"keep-option"`, string(options["future_option"]))
}

func TestNormalizeProtocolRulesRaw_TreatsMissingAndEmptyNameAsSameIdentity(t *testing.T) {
	original := map[string]common.RawMessage{
		"rules": common.RawMessage(`[
			{
				"name": "",
				"enabled": true,
				"source_endpoint": "responses",
				"target_endpoint": "chat_completions",
				"all_channels": true,
				"model_patterns": ["^gpt-5.*$"],
				"future_rule": "keep-empty-name-rule"
			}
		]`),
	}
	normalized := map[string]common.RawMessage{
		"rules": common.RawMessage(`[
			{
				"enabled": false,
				"source_endpoint": "responses",
				"target_endpoint": "chat_completions",
				"all_channels": true,
				"model_patterns": ["^gpt-5.*$"]
			}
		]`),
	}

	require.NoError(t, normalizeProtocolRulesRaw(original, normalized))

	var rules []map[string]common.RawMessage
	require.NoError(t, common.Unmarshal(original["rules"], &rules))
	require.Len(t, rules, 1)
	assert.NotContains(t, rules[0], "name")
	assert.JSONEq(t, `false`, string(rules[0]["enabled"]))
	assert.JSONEq(t, `"keep-empty-name-rule"`, string(rules[0]["future_rule"]))
}

func TestNormalizeChatCompletionsToResponsesPolicyJSON_RejectsNonObjectOptions(t *testing.T) {
	raw := `{
		"rules": [
			{
				"name": "invalid-options",
				"enabled": true,
				"source_endpoint": "responses",
				"target_endpoint": "chat_completions",
				"all_channels": true,
				"model_patterns": ["^gpt-5.*$"],
				"options": "not-object"
			}
		]
	}`

	_, err := NormalizeChatCompletionsToResponsesPolicyJSON(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "options")
}

func TestNormalizeChatCompletionsToResponsesPolicyJSON_RejectsNullOptions(t *testing.T) {
	raw := `{
		"rules": [
			{
				"name": "null-options",
				"enabled": true,
				"source_endpoint": "responses",
				"target_endpoint": "chat_completions",
				"all_channels": true,
				"model_patterns": ["^gpt-5.*$"],
				"options": null
			}
		]
	}`

	_, err := NormalizeChatCompletionsToResponsesPolicyJSON(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "options")
}

func TestNormalizeChatCompletionsToResponsesPolicyJSON_RejectsNullRule(t *testing.T) {
	raw := `{
		"rules": [
			null
		]
	}`

	_, err := NormalizeChatCompletionsToResponsesPolicyJSON(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不能为 null")
}

func TestNormalizeProtocolRulesRaw_PreservesUnknownFieldsAfterRuleDeletion(t *testing.T) {
	original := map[string]common.RawMessage{
		"rules": common.RawMessage(`[
			{
				"name": "deleted-rule",
				"enabled": true,
				"source_endpoint": "responses",
				"target_endpoint": "chat_completions",
				"all_channels": true,
				"model_patterns": ["^gpt-5.*$"],
				"future_rule": "delete-me"
			},
			{
				"name": "kept-rule",
				"enabled": true,
				"source_endpoint": "chat_completions",
				"target_endpoint": "responses",
				"all_channels": true,
				"model_patterns": ["^gpt-4o.*$"],
				"future_rule": "keep-me",
				"options": {
					"future_option": "keep-option"
				}
			}
		]`),
	}
	normalized := map[string]common.RawMessage{
		"rules": common.RawMessage(`[
			{
				"name": "kept-rule",
				"enabled": false,
				"source_endpoint": "chat_completions",
				"target_endpoint": "responses",
				"all_channels": true,
				"model_patterns": ["^gpt-4o.*$"],
				"options": {}
			}
		]`),
	}

	require.NoError(t, normalizeProtocolRulesRaw(original, normalized))

	var rules []map[string]common.RawMessage
	require.NoError(t, common.Unmarshal(original["rules"], &rules))
	require.Len(t, rules, 1)
	assert.JSONEq(t, `"kept-rule"`, string(rules[0]["name"]))
	assert.JSONEq(t, `false`, string(rules[0]["enabled"]))
	assert.JSONEq(t, `"keep-me"`, string(rules[0]["future_rule"]))

	var options map[string]common.RawMessage
	require.NoError(t, common.Unmarshal(rules[0]["options"], &options))
	assert.JSONEq(t, `"keep-option"`, string(options["future_option"]))
}

func TestNormalizeChatCompletionsToResponsesPolicyJSON_PreservesUnknownFieldsWithEndpointAliases(t *testing.T) {
	raw := `{
		"rules": [
			{
				"name": "alias-rule",
				"enabled": true,
				"source_endpoint": "openai",
				"target_endpoint": "openai-response",
				"all_channels": true,
				"model_patterns": ["^gpt-4o.*$"],
				"future_rule": "keep"
			}
		]
	}`

	normalized, err := NormalizeChatCompletionsToResponsesPolicyJSON(raw)
	require.NoError(t, err)

	var policyRaw map[string]common.RawMessage
	require.NoError(t, common.Unmarshal([]byte(normalized), &policyRaw))
	var rules []map[string]common.RawMessage
	require.NoError(t, common.Unmarshal(policyRaw["rules"], &rules))
	require.Len(t, rules, 1)
	assert.JSONEq(t, `"keep"`, string(rules[0]["future_rule"]))
	assert.JSONEq(t, `"chat_completions"`, string(rules[0]["source_endpoint"]))
	assert.JSONEq(t, `"responses"`, string(rules[0]["target_endpoint"]))
}

func TestNormalizeChatCompletionsToResponsesPolicyJSON_EmptyInput(t *testing.T) {
	normalized, err := NormalizeChatCompletionsToResponsesPolicyJSON("")
	require.NoError(t, err)
	assert.JSONEq(t, `{"enabled":false,"all_channels":false}`, normalized)
}

func TestChatCompletionsToResponsesPolicyWarnings(t *testing.T) {
	policy := ChatCompletionsToResponsesPolicy{
		Rules: []ProtocolConversionRule{
			{
				Name:           "disabled-empty",
				Enabled:        false,
				SourceEndpoint: ProtocolEndpointResponses,
				TargetEndpoint: ProtocolEndpointChatCompletions,
				AllChannels:    false,
			},
		},
	}

	warnings := ChatCompletionsToResponsesPolicyWarnings(policy, true)
	assert.Contains(t, warnings, "disabled-empty 已停用，不会参与匹配")
	assert.Contains(t, warnings, "disabled-empty 渠道范围为空，不会命中")
	assert.NotContains(t, warnings, "disabled-empty 模型正则为空，不会命中")
	assert.Contains(t, warnings, "全局请求透传已开启，协议转换运行时会被跳过")
}
