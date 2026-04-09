package model_setting

import (
	"fmt"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

const (
	ProtocolEndpointChatCompletions = "chat_completions"
	ProtocolEndpointResponses       = "responses"
)

type ChatCompletionsToResponsesPolicy struct {
	Enabled       bool                     `json:"enabled"`
	AllChannels   bool                     `json:"all_channels"`
	ChannelIDs    []int                    `json:"channel_ids,omitempty"`
	ChannelTypes  []int                    `json:"channel_types,omitempty"`
	ModelPatterns []string                 `json:"model_patterns,omitempty"`
	Rules         []ProtocolConversionRule `json:"rules,omitempty"`
}

func (p ChatCompletionsToResponsesPolicy) IsChannelEnabled(channelID int, channelType int) bool {
	if !p.Enabled {
		return false
	}
	if p.AllChannels {
		return true
	}

	if channelID > 0 && len(p.ChannelIDs) > 0 && slices.Contains(p.ChannelIDs, channelID) {
		return true
	}
	if channelType > 0 && len(p.ChannelTypes) > 0 && slices.Contains(p.ChannelTypes, channelType) {
		return true
	}
	return false
}

type ProtocolConversionRule struct {
	Name           string   `json:"name,omitempty"`
	Enabled        bool     `json:"enabled"`
	SourceEndpoint string   `json:"source_endpoint,omitempty"`
	TargetEndpoint string   `json:"target_endpoint,omitempty"`
	AllChannels    bool     `json:"all_channels"`
	ChannelIDs     []int    `json:"channel_ids,omitempty"`
	ChannelTypes   []int    `json:"channel_types,omitempty"`
	ModelPatterns  []string `json:"model_patterns,omitempty"`
}

func (r ProtocolConversionRule) IsChannelEnabled(channelID int, channelType int) bool {
	if !r.Enabled {
		return false
	}
	if r.AllChannels {
		return true
	}

	if channelID > 0 && len(r.ChannelIDs) > 0 && slices.Contains(r.ChannelIDs, channelID) {
		return true
	}
	if channelType > 0 && len(r.ChannelTypes) > 0 && slices.Contains(r.ChannelTypes, channelType) {
		return true
	}
	return false
}

func (p ChatCompletionsToResponsesPolicy) HasRules() bool {
	return len(p.Rules) > 0
}

func (p ChatCompletionsToResponsesPolicy) LegacyRule() ProtocolConversionRule {
	return ProtocolConversionRule{
		Name:           "legacy-chat-completions-to-responses",
		Enabled:        p.Enabled,
		SourceEndpoint: ProtocolEndpointChatCompletions,
		TargetEndpoint: ProtocolEndpointResponses,
		AllChannels:    p.AllChannels,
		ChannelIDs:     append([]int(nil), p.ChannelIDs...),
		ChannelTypes:   append([]int(nil), p.ChannelTypes...),
		ModelPatterns:  append([]string(nil), p.ModelPatterns...),
	}
}

type GlobalSettings struct {
	PassThroughRequestEnabled        bool                             `json:"pass_through_request_enabled"`
	ThinkingModelBlacklist           []string                         `json:"thinking_model_blacklist"`
	ChatCompletionsToResponsesPolicy ChatCompletionsToResponsesPolicy `json:"chat_completions_to_responses_policy"`
}

// 默认配置
var defaultOpenaiSettings = GlobalSettings{
	PassThroughRequestEnabled: false,
	ThinkingModelBlacklist: []string{
		"moonshotai/kimi-k2-thinking",
		"kimi-k2-thinking",
	},
	ChatCompletionsToResponsesPolicy: ChatCompletionsToResponsesPolicy{
		Enabled:     false,
		AllChannels: true,
	},
}

// 全局实例
var globalSettings = defaultOpenaiSettings

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("global", &globalSettings)
}

func GetGlobalSettings() *GlobalSettings {
	return &globalSettings
}

// ShouldPreserveThinkingSuffix 判断模型是否配置为保留 thinking/-nothinking/-low/-high/-medium 后缀
func ShouldPreserveThinkingSuffix(modelName string) bool {
	target := strings.TrimSpace(modelName)
	if target == "" {
		return false
	}

	for _, entry := range globalSettings.ThinkingModelBlacklist {
		if strings.TrimSpace(entry) == target {
			return true
		}
	}
	return false
}

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

func normalizePositiveIntList(values []int) []int {
	if len(values) == 0 {
		return nil
	}
	result := make([]int, 0, len(values))
	seen := make(map[int]struct{}, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func normalizeStringList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	return result
}

func ValidateAndNormalizeChatCompletionsToResponsesPolicy(policy *ChatCompletionsToResponsesPolicy) error {
	if policy == nil {
		return nil
	}

	policy.ChannelIDs = normalizePositiveIntList(policy.ChannelIDs)
	policy.ChannelTypes = normalizePositiveIntList(policy.ChannelTypes)
	policy.ModelPatterns = normalizeStringList(policy.ModelPatterns)

	if len(policy.Rules) == 0 {
		return nil
	}

	for i := range policy.Rules {
		rule := &policy.Rules[i]
		rule.Name = strings.TrimSpace(rule.Name)
		rule.SourceEndpoint = normalizeProtocolEndpoint(rule.SourceEndpoint)
		rule.TargetEndpoint = normalizeProtocolEndpoint(rule.TargetEndpoint)
		rule.ChannelIDs = normalizePositiveIntList(rule.ChannelIDs)
		rule.ChannelTypes = normalizePositiveIntList(rule.ChannelTypes)
		rule.ModelPatterns = normalizeStringList(rule.ModelPatterns)

		switch rule.SourceEndpoint {
		case ProtocolEndpointChatCompletions, ProtocolEndpointResponses:
		default:
			return fmt.Errorf("规则 #%d 源协议不支持: %s", i+1, rule.SourceEndpoint)
		}
		switch rule.TargetEndpoint {
		case ProtocolEndpointChatCompletions, ProtocolEndpointResponses:
		default:
			return fmt.Errorf("规则 #%d 目标协议不支持: %s", i+1, rule.TargetEndpoint)
		}
		if rule.SourceEndpoint == rule.TargetEndpoint {
			return fmt.Errorf("规则 #%d 源协议和目标协议不能相同", i+1)
		}
	}

	return nil
}

func NormalizeChatCompletionsToResponsesPolicyJSON(raw string) (string, error) {
	policyRaw := strings.TrimSpace(raw)
	if policyRaw == "" {
		policyRaw = "{}"
	}

	var policy ChatCompletionsToResponsesPolicy
	if err := common.UnmarshalJsonStr(policyRaw, &policy); err != nil {
		return "", fmt.Errorf("策略 JSON 解析失败: %w", err)
	}
	if err := ValidateAndNormalizeChatCompletionsToResponsesPolicy(&policy); err != nil {
		return "", err
	}

	bytes, err := common.Marshal(policy)
	if err != nil {
		return "", fmt.Errorf("策略 JSON 序列化失败: %w", err)
	}
	return string(bytes), nil
}
