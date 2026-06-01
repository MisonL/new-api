package model_setting

import (
	"fmt"
	"regexp"
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
	Name           string                     `json:"name,omitempty"`
	Enabled        bool                       `json:"enabled"`
	SourceEndpoint string                     `json:"source_endpoint,omitempty"`
	TargetEndpoint string                     `json:"target_endpoint,omitempty"`
	AllChannels    bool                       `json:"all_channels"`
	ChannelIDs     []int                      `json:"channel_ids,omitempty"`
	ChannelTypes   []int                      `json:"channel_types,omitempty"`
	ModelPatterns  []string                   `json:"model_patterns,omitempty"`
	Options        *ProtocolConversionOptions `json:"options,omitempty"`
}

type ProtocolConversionOptions struct {
	EnableCustomToolBridge bool `json:"enable_custom_tool_bridge,omitempty"`
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

func validatePositiveIntList(label string, values []int) error {
	for _, value := range values {
		if value <= 0 {
			return fmt.Errorf("%s 必须是正整数", label)
		}
	}
	return nil
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

func validateRegexList(label string, values []string) error {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, err := regexp.Compile(trimmed); err != nil {
			return fmt.Errorf("%s 正则不合法: %s", label, trimmed)
		}
	}
	return nil
}

func ValidateAndNormalizeChatCompletionsToResponsesPolicy(policy *ChatCompletionsToResponsesPolicy) error {
	if policy == nil {
		return nil
	}

	if err := validatePositiveIntList("顶层 channel_ids", policy.ChannelIDs); err != nil {
		return err
	}
	if err := validatePositiveIntList("顶层 channel_types", policy.ChannelTypes); err != nil {
		return err
	}
	if err := validateRegexList("顶层 model_patterns", policy.ModelPatterns); err != nil {
		return err
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
		if err := validatePositiveIntList(fmt.Sprintf("规则 #%d channel_ids", i+1), rule.ChannelIDs); err != nil {
			return err
		}
		if err := validatePositiveIntList(fmt.Sprintf("规则 #%d channel_types", i+1), rule.ChannelTypes); err != nil {
			return err
		}
		if err := validateRegexList(fmt.Sprintf("规则 #%d model_patterns", i+1), rule.ModelPatterns); err != nil {
			return err
		}
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
		if rule.Options != nil && rule.Options.EnableCustomToolBridge &&
			(rule.SourceEndpoint != ProtocolEndpointResponses || rule.TargetEndpoint != ProtocolEndpointChatCompletions) {
			return fmt.Errorf("规则 #%d enable_custom_tool_bridge 仅支持 responses 到 chat_completions", i+1)
		}
	}

	return nil
}

func ChatCompletionsToResponsesPolicyWarnings(policy ChatCompletionsToResponsesPolicy, passThroughEnabled bool) []string {
	var warnings []string
	rules := policy.Rules
	if len(rules) == 0 {
		rules = []ProtocolConversionRule{policy.LegacyRule()}
	}

	for i, rule := range rules {
		label := fmt.Sprintf("规则 #%d", i+1)
		if strings.TrimSpace(rule.Name) != "" {
			label = strings.TrimSpace(rule.Name)
		}
		if !rule.Enabled {
			warnings = append(warnings, fmt.Sprintf("%s 已停用，不会参与匹配", label))
		}
		if !rule.AllChannels && len(rule.ChannelIDs) == 0 && len(rule.ChannelTypes) == 0 {
			warnings = append(warnings, fmt.Sprintf("%s 渠道范围为空，不会命中", label))
		}
	}
	if passThroughEnabled && len(rules) > 0 {
		warnings = append(warnings, "全局请求透传已开启，协议转换运行时会被跳过")
	}
	return warnings
}

var protocolPolicyKnownFields = []string{
	"enabled",
	"all_channels",
	"channel_ids",
	"channel_types",
	"model_patterns",
}

var protocolRuleKnownFields = []string{
	"name",
	"enabled",
	"source_endpoint",
	"target_endpoint",
	"all_channels",
	"channel_ids",
	"channel_types",
	"model_patterns",
}

var protocolOptionsKnownFields = []string{
	"enable_custom_tool_bridge",
}

func replaceKnownRawFields(dst map[string]common.RawMessage, src map[string]common.RawMessage, keys []string) {
	for _, key := range keys {
		if value, ok := src[key]; ok {
			dst[key] = value
		} else {
			delete(dst, key)
		}
	}
}

func rawObjectFromBytes(data []byte) map[string]common.RawMessage {
	var out map[string]common.RawMessage
	if err := common.Unmarshal(data, &out); err != nil || out == nil {
		return map[string]common.RawMessage{}
	}
	return out
}

func rawObjectFromMessage(data common.RawMessage) map[string]common.RawMessage {
	return rawObjectFromBytes(data)
}

func normalizeProtocolRuleOptionsRaw(originalRule map[string]common.RawMessage, normalizedRule map[string]common.RawMessage) error {
	originalOptions, hadOriginalOptions := originalRule["options"]
	normalizedOptions, hasNormalizedOptions := normalizedRule["options"]
	if !hadOriginalOptions && !hasNormalizedOptions {
		return nil
	}

	optionsRaw := rawObjectFromMessage(originalOptions)
	normalizedOptionsRaw := rawObjectFromMessage(normalizedOptions)
	replaceKnownRawFields(optionsRaw, normalizedOptionsRaw, protocolOptionsKnownFields)
	if len(optionsRaw) == 0 {
		delete(originalRule, "options")
		return nil
	}

	encoded, err := common.Marshal(optionsRaw)
	if err != nil {
		return fmt.Errorf("策略规则 options 序列化失败: %w", err)
	}
	originalRule["options"] = encoded
	return nil
}

func rawTrimmedString(data common.RawMessage) (string, bool) {
	var value string
	if err := common.Unmarshal(data, &value); err != nil {
		return "", false
	}
	return strings.TrimSpace(value), true
}

func rawNormalizedProtocolField(data common.RawMessage, key string) (string, bool) {
	value, ok := rawTrimmedString(data)
	if !ok {
		return "", false
	}
	if key == "source_endpoint" || key == "target_endpoint" {
		return normalizeProtocolEndpoint(value), true
	}
	return value, true
}

func rawProtocolFieldsEqual(left common.RawMessage, right common.RawMessage, key string) bool {
	leftValue, leftOk := rawNormalizedProtocolField(left, key)
	rightValue, rightOk := rawNormalizedProtocolField(right, key)
	return leftOk && rightOk && leftValue == rightValue
}

func rawNamePresenceMatches(originalValue common.RawMessage, hasOriginal bool, normalizedValue common.RawMessage, hasNormalized bool) bool {
	if hasOriginal == hasNormalized {
		return true
	}
	if hasOriginal {
		originalName, ok := rawTrimmedString(originalValue)
		return ok && originalName == ""
	}
	normalizedName, ok := rawTrimmedString(normalizedValue)
	return ok && normalizedName == ""
}

func protocolRuleIdentityMatches(originalRule map[string]common.RawMessage, normalizedRule map[string]common.RawMessage) bool {
	for _, key := range []string{"name", "source_endpoint", "target_endpoint"} {
		originalValue, hasOriginal := originalRule[key]
		normalizedValue, hasNormalized := normalizedRule[key]
		if !hasOriginal && !hasNormalized {
			continue
		}
		if key == "name" && hasOriginal != hasNormalized {
			if !rawNamePresenceMatches(originalValue, hasOriginal, normalizedValue, hasNormalized) {
				return false
			}
			continue
		}
		if hasOriginal != hasNormalized {
			return false
		}
		if !rawProtocolFieldsEqual(originalValue, normalizedValue, key) {
			return false
		}
	}
	return true
}

func findOriginalProtocolRule(
	originalRules []common.RawMessage,
	usedOriginalRules []bool,
	normalizedRule map[string]common.RawMessage,
	preferredIndex int,
) map[string]common.RawMessage {
	if preferredIndex < len(originalRules) && !usedOriginalRules[preferredIndex] {
		originalRule := rawObjectFromMessage(originalRules[preferredIndex])
		if protocolRuleIdentityMatches(originalRule, normalizedRule) {
			usedOriginalRules[preferredIndex] = true
			return originalRule
		}
	}
	for index, originalRuleRaw := range originalRules {
		if usedOriginalRules[index] {
			continue
		}
		originalRule := rawObjectFromMessage(originalRuleRaw)
		if protocolRuleIdentityMatches(originalRule, normalizedRule) {
			usedOriginalRules[index] = true
			return originalRule
		}
	}
	return map[string]common.RawMessage{}
}

func normalizeProtocolRulesRaw(original map[string]common.RawMessage, normalized map[string]common.RawMessage) error {
	normalizedRulesRaw, ok := normalized["rules"]
	if !ok {
		delete(original, "rules")
		return nil
	}

	var originalRules []common.RawMessage
	if raw, ok := original["rules"]; ok {
		_ = common.Unmarshal(raw, &originalRules)
	}
	var normalizedRules []common.RawMessage
	if err := common.Unmarshal(normalizedRulesRaw, &normalizedRules); err != nil {
		return err
	}

	mergedRules := make([]map[string]common.RawMessage, 0, len(normalizedRules))
	usedOriginalRules := make([]bool, len(originalRules))
	for i, normalizedRuleRaw := range normalizedRules {
		normalizedRule := rawObjectFromMessage(normalizedRuleRaw)
		originalRule := findOriginalProtocolRule(originalRules, usedOriginalRules, normalizedRule, i)
		replaceKnownRawFields(originalRule, normalizedRule, protocolRuleKnownFields)
		if err := normalizeProtocolRuleOptionsRaw(originalRule, normalizedRule); err != nil {
			return err
		}
		mergedRules = append(mergedRules, originalRule)
	}

	encoded, err := common.Marshal(mergedRules)
	if err != nil {
		return err
	}
	original["rules"] = encoded
	return nil
}

func validateProtocolRuleOptionsRaw(policy map[string]common.RawMessage) error {
	rulesRaw, ok := policy["rules"]
	if !ok {
		return nil
	}

	var rules []map[string]common.RawMessage
	if err := common.Unmarshal(rulesRaw, &rules); err != nil {
		return fmt.Errorf("策略 rules 必须是对象数组: %w", err)
	}
	for i, rule := range rules {
		if rule == nil {
			return fmt.Errorf("规则 #%d 不能为 null", i+1)
		}
		optionsRaw, ok := rule["options"]
		if !ok {
			continue
		}
		var options map[string]common.RawMessage
		if err := common.Unmarshal(optionsRaw, &options); err != nil || options == nil {
			return fmt.Errorf("规则 #%d options 必须是对象（不接受 null）", i+1)
		}
	}
	return nil
}

func NormalizeChatCompletionsToResponsesPolicyJSON(raw string) (string, error) {
	policyRaw := strings.TrimSpace(raw)
	if policyRaw == "" {
		policyRaw = "{}"
	}

	var original map[string]common.RawMessage
	if err := common.UnmarshalJsonStr(policyRaw, &original); err != nil {
		return "", fmt.Errorf("策略 JSON 解析失败: %w", err)
	}
	if original == nil {
		original = map[string]common.RawMessage{}
	}
	if err := validateProtocolRuleOptionsRaw(original); err != nil {
		return "", err
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

	normalized := rawObjectFromBytes(bytes)
	if err := normalizeProtocolRulesRaw(original, normalized); err != nil {
		return "", fmt.Errorf("策略 rules 序列化失败: %w", err)
	}
	replaceKnownRawFields(original, normalized, protocolPolicyKnownFields)

	mergedBytes, err := common.Marshal(original)
	if err != nil {
		return "", fmt.Errorf("策略 JSON 序列化失败: %w", err)
	}
	return string(mergedBytes), nil
}
