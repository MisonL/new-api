package openaicompat

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/model_setting"
)

const (
	ProtocolEndpointChatCompletions = model_setting.ProtocolEndpointChatCompletions
	ProtocolEndpointResponses       = model_setting.ProtocolEndpointResponses
)

func normalizeProtocolEndpoint(endpoint string) string {
	switch strings.ToLower(strings.TrimSpace(endpoint)) {
	case "openai", "chat", "chat_completions", "chat-completions", "/v1/chat/completions":
		return ProtocolEndpointChatCompletions
	case "responses", "response", "openai-response", "openai-responses", "/v1/responses":
		return ProtocolEndpointResponses
	default:
		return strings.ToLower(strings.TrimSpace(endpoint))
	}
}

func isRuleMatch(rule model_setting.ProtocolConversionRule, sourceEndpoint string, targetEndpoint string, channelID int, channelType int, model string) bool {
	if !rule.IsChannelEnabled(channelID, channelType) {
		return false
	}
	if normalizeProtocolEndpoint(rule.SourceEndpoint) != normalizeProtocolEndpoint(sourceEndpoint) {
		return false
	}
	if normalizeProtocolEndpoint(rule.TargetEndpoint) != normalizeProtocolEndpoint(targetEndpoint) {
		return false
	}
	return matchAnyRegex(rule.ModelPatterns, model)
}

type ProtocolConversionRuleFilter func(rule *model_setting.ProtocolConversionRule) (bool, error)

func FindProtocolConversionRulePolicyWithFilter(policy model_setting.ChatCompletionsToResponsesPolicy, sourceEndpoint string, targetEndpoint string, channelID int, channelType int, model string, filter ProtocolConversionRuleFilter) (*model_setting.ProtocolConversionRule, error) {
	if !policy.Enabled {
		return nil, nil
	}

	for i := range policy.Rules {
		rule := &policy.Rules[i]
		if !isRuleMatch(*rule, sourceEndpoint, targetEndpoint, channelID, channelType, model) {
			continue
		}
		if filter != nil {
			allowed, err := filter(rule)
			if err != nil {
				return nil, err
			}
			if !allowed {
				continue
			}
		}
		return rule, nil
	}

	if policy.HasRules() {
		return nil, nil
	}

	legacyRule := policy.LegacyRule()
	if isRuleMatch(legacyRule, sourceEndpoint, targetEndpoint, channelID, channelType, model) {
		if filter == nil {
			return &legacyRule, nil
		}
		allowed, err := filter(&legacyRule)
		if err != nil {
			return nil, err
		}
		if allowed {
			return &legacyRule, nil
		}
	}
	return nil, nil
}

func FindProtocolConversionRulePolicy(policy model_setting.ChatCompletionsToResponsesPolicy, sourceEndpoint string, targetEndpoint string, channelID int, channelType int, model string) *model_setting.ProtocolConversionRule {
	rule, _ := FindProtocolConversionRulePolicyWithFilter(policy, sourceEndpoint, targetEndpoint, channelID, channelType, model, nil)
	return rule
}

func FindProtocolConversionRuleGlobal(sourceEndpoint string, targetEndpoint string, channelID int, channelType int, model string) *model_setting.ProtocolConversionRule {
	return FindProtocolConversionRulePolicy(
		model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy,
		sourceEndpoint,
		targetEndpoint,
		channelID,
		channelType,
		model,
	)
}

func ShouldConvertProtocolPolicy(policy model_setting.ChatCompletionsToResponsesPolicy, sourceEndpoint string, targetEndpoint string, channelID int, channelType int, model string) bool {
	return FindProtocolConversionRulePolicy(policy, sourceEndpoint, targetEndpoint, channelID, channelType, model) != nil
}

func ShouldConvertProtocolGlobal(sourceEndpoint string, targetEndpoint string, channelID int, channelType int, model string) bool {
	return FindProtocolConversionRuleGlobal(
		sourceEndpoint,
		targetEndpoint,
		channelID,
		channelType,
		model,
	) != nil
}

func ShouldChatCompletionsUseResponsesPolicy(policy model_setting.ChatCompletionsToResponsesPolicy, channelID int, channelType int, model string) bool {
	return ShouldConvertProtocolPolicy(
		policy,
		ProtocolEndpointChatCompletions,
		ProtocolEndpointResponses,
		channelID,
		channelType,
		model,
	)
}

func ShouldChatCompletionsUseResponsesGlobal(channelID int, channelType int, model string) bool {
	return ShouldConvertProtocolGlobal(
		ProtocolEndpointChatCompletions,
		ProtocolEndpointResponses,
		channelID,
		channelType,
		model,
	)
}

func ShouldResponsesUseChatCompletionsPolicy(policy model_setting.ChatCompletionsToResponsesPolicy, channelID int, channelType int, model string) bool {
	return ShouldConvertProtocolPolicy(
		policy,
		ProtocolEndpointResponses,
		ProtocolEndpointChatCompletions,
		channelID,
		channelType,
		model,
	)
}

func ShouldResponsesUseChatCompletionsGlobal(channelID int, channelType int, model string) bool {
	return ShouldConvertProtocolGlobal(
		ProtocolEndpointResponses,
		ProtocolEndpointChatCompletions,
		channelID,
		channelType,
		model,
	)
}
