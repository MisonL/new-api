package dto

import (
	"strings"
	"time"

	"github.com/QuantumNous/new-api/constant"
)

type ChannelSettings struct {
	ForceFormat            bool   `json:"force_format,omitempty"`
	ThinkingToContent      bool   `json:"thinking_to_content,omitempty"`
	Proxy                  string `json:"proxy"`
	PassThroughBodyEnabled bool   `json:"pass_through_body_enabled,omitempty"`
	SystemPrompt           string `json:"system_prompt,omitempty"`
	SystemPromptOverride   bool   `json:"system_prompt_override,omitempty"`
}

type VertexKeyType string

const (
	VertexKeyTypeJSON   VertexKeyType = "json"
	VertexKeyTypeAPIKey VertexKeyType = "api_key"
)

type AwsKeyType string

const (
	AwsKeyTypeAKSK   AwsKeyType = "ak_sk" // 默认
	AwsKeyTypeApiKey AwsKeyType = "api_key"
)

type ResponsesCompactMode string

const (
	// Auto mode tries native compact first and temporarily falls back to synthetic summaries after native compatibility failures.
	// Legacy convert mode keeps its compatible /v1/responses behavior via synthetic summaries.
	ResponsesCompactModeAuto      ResponsesCompactMode = "auto"
	ResponsesCompactModeNative    ResponsesCompactMode = "native"
	ResponsesCompactModeSynthetic ResponsesCompactMode = "synthetic_summary"
	ResponsesCompactModeDisabled  ResponsesCompactMode = "disabled"
)

type ResponsesUpstreamProfile string

const (
	ResponsesUpstreamProfileOfficialOpenAI    ResponsesUpstreamProfile = "official_openai"
	ResponsesUpstreamProfileOfficialNewAPI    ResponsesUpstreamProfile = "official_newapi"
	ResponsesUpstreamProfileSameClusterNewAPI ResponsesUpstreamProfile = "same_cluster_newapi"
	ResponsesUpstreamProfileTrustedNewAPI     ResponsesUpstreamProfile = "trusted_newapi"
	ResponsesUpstreamProfileSub2APIHTTP       ResponsesUpstreamProfile = "sub2api_http"
	ResponsesUpstreamProfileSub2APIWSV2       ResponsesUpstreamProfile = "sub2api_wsv2"
	ResponsesUpstreamProfileGenericOpenAI     ResponsesUpstreamProfile = "generic_openai"
	ResponsesUpstreamProfileGenericProxy      ResponsesUpstreamProfile = "generic_proxy"
	ResponsesUpstreamProfileChatOnlyProxy     ResponsesUpstreamProfile = "chat_only_proxy"
)

const DefaultResponsesCompactSyntheticFallbackModel = "gpt-5.4"

type ResponsesCapabilityObservation struct {
	ObservedAt int64  `json:"observed_at,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type ResponsesChannelCapabilityRegistry struct {
	Source                            string                          `json:"source,omitempty"`
	Observed                          *ResponsesCapabilityObservation `json:"observed,omitempty"`
	Probe                             *ResponsesCapabilityObservation `json:"probe,omitempty"`
	SupportsResponses                 *bool                           `json:"supports_responses,omitempty"`
	SupportsResponsesCompact          *bool                           `json:"supports_responses_compact,omitempty"`
	SupportsChat                      *bool                           `json:"supports_chat,omitempty"`
	SupportsRestPreviousResponseID    *bool                           `json:"supports_rest_previous_response_id,omitempty"`
	SupportsCompactionItemPassthrough *bool                           `json:"supports_compaction_item_passthrough,omitempty"`
	SupportsNamespaceTools            *bool                           `json:"supports_namespace_tools,omitempty"`
}

type ResponsesChannelCapabilitySnapshot struct {
	Source                            string
	Profile                           ResponsesUpstreamProfile
	CompactModeSetting                ResponsesCompactMode
	CompactModeEffective              ResponsesCompactMode
	SupportsResponses                 bool
	SupportsResponsesCompact          bool
	SupportsChat                      bool
	SupportsRestPreviousResponseID    bool
	SupportsCompactionItemPassthrough bool
	SupportsNamespaceTools            bool
	Observed                          *ResponsesCapabilityObservation
	Probe                             *ResponsesCapabilityObservation
}

const (
	DefaultResponsesCompactAutoFallbackRetryIntervalHours = 3
	MinResponsesCompactAutoFallbackRetryIntervalHours     = 1
	MaxResponsesCompactAutoFallbackRetryIntervalHours     = 168
)

type ChannelOtherSettings struct {
	AzureResponsesVersion                          string                              `json:"azure_responses_version,omitempty"`
	ResponsesCompactMode                           ResponsesCompactMode                `json:"responses_compact_mode,omitempty"`
	ResponsesCompactAutoFallbackDate               int                                 `json:"responses_compact_auto_fallback_date,omitempty"`
	ResponsesCompactAutoFallbackAt                 int64                               `json:"responses_compact_auto_fallback_at,omitempty"`
	ResponsesCompactAutoFallbackReason             string                              `json:"responses_compact_auto_fallback_reason,omitempty"`
	ResponsesCompactAutoFallbackRetryIntervalHours int                                 `json:"responses_compact_auto_fallback_retry_interval_hours,omitempty"`
	ResponsesCompactContextFallback                *bool                               `json:"responses_compact_context_fallback,omitempty"`
	ResponsesCompactSummaryModelFallback           *bool                               `json:"responses_compact_summary_model_fallback,omitempty"`
	ResponsesCompactSummaryFallbackModels          []string                            `json:"responses_compact_summary_fallback_models,omitempty"`
	ResponsesUpstreamProfile                       ResponsesUpstreamProfile            `json:"responses_upstream_profile,omitempty"`
	ResponsesCapabilityRegistry                    *ResponsesChannelCapabilityRegistry `json:"responses_capability_registry,omitempty"`
	VertexKeyType                                  VertexKeyType                       `json:"vertex_key_type,omitempty"` // "json" or "api_key"
	OpenRouterEnterprise                           *bool                               `json:"openrouter_enterprise,omitempty"`
	ClaudeBetaQuery                                bool                                `json:"claude_beta_query,omitempty"`         // Claude 渠道是否强制追加 ?beta=true
	AllowServiceTier                               bool                                `json:"allow_service_tier,omitempty"`        // 是否允许 service_tier 透传（默认过滤以避免额外计费）
	AllowInferenceGeo                              bool                                `json:"allow_inference_geo,omitempty"`       // 是否允许 inference_geo 透传（仅 Claude，默认过滤以满足数据驻留合规
	AllowSpeed                                     bool                                `json:"allow_speed,omitempty"`               // 是否允许 speed 透传（仅 Claude，默认过滤以避免意外切换推理速度模式）
	AllowSafetyIdentifier                          bool                                `json:"allow_safety_identifier,omitempty"`   // 是否允许 safety_identifier 透传（默认过滤以保护用户隐私）
	DisableStore                                   bool                                `json:"disable_store,omitempty"`             // 是否禁用 store 透传（默认允许透传，禁用后可能导致 Codex 无法使用）
	AllowIncludeObfuscation                        bool                                `json:"allow_include_obfuscation,omitempty"` // 是否允许 stream_options.include_obfuscation 透传（默认过滤以避免关闭流混淆保护）
	StripCodexEncryptedContext                     bool                                `json:"strip_codex_encrypted_context,omitempty"`
	AwsKeyType                                     AwsKeyType                          `json:"aws_key_type,omitempty"`
	HeaderPolicyMode                               HeaderPolicyMode                    `json:"header_policy_mode,omitempty"`
	AuxiliaryRequestHeaderPolicyEnabled            *bool                               `json:"auxiliary_request_header_policy_enabled,omitempty"`
	OverrideHeaderUserAgent                        bool                                `json:"override_header_user_agent,omitempty"`
	UserAgentStrategy                              *UserAgentStrategy                  `json:"ua_strategy,omitempty"`
	UpstreamModelUpdateCheckEnabled                bool                                `json:"upstream_model_update_check_enabled,omitempty"`        // 是否检测上游模型更新
	UpstreamModelUpdateAutoSyncEnabled             bool                                `json:"upstream_model_update_auto_sync_enabled,omitempty"`    // 是否自动同步上游模型更新
	UpstreamModelUpdateLastCheckTime               int64                               `json:"upstream_model_update_last_check_time,omitempty"`      // 上次检测时间
	UpstreamModelUpdateLastDetectedModels          []string                            `json:"upstream_model_update_last_detected_models,omitempty"` // 上次检测到的可加入模型
	UpstreamModelUpdateLastRemovedModels           []string                            `json:"upstream_model_update_last_removed_models,omitempty"`  // 上次检测到的可删除模型
	UpstreamModelUpdateIgnoredModels               []string                            `json:"upstream_model_update_ignored_models,omitempty"`       // 手动忽略的模型
	HeaderProfileStrategy                          *HeaderProfileStrategy              `json:"header_profile_strategy,omitempty"`
}

func (s *ChannelOtherSettings) IsOpenRouterEnterprise() bool {
	if s == nil || s.OpenRouterEnterprise == nil {
		return false
	}
	return *s.OpenRouterEnterprise
}

func (s *ChannelOtherSettings) NormalizedResponsesUpstreamProfile() ResponsesUpstreamProfile {
	if s == nil {
		return ""
	}
	switch s.ResponsesUpstreamProfile {
	case ResponsesUpstreamProfileOfficialOpenAI,
		ResponsesUpstreamProfileOfficialNewAPI,
		ResponsesUpstreamProfileSameClusterNewAPI,
		ResponsesUpstreamProfileTrustedNewAPI,
		ResponsesUpstreamProfileSub2APIHTTP,
		ResponsesUpstreamProfileSub2APIWSV2,
		ResponsesUpstreamProfileGenericOpenAI,
		ResponsesUpstreamProfileGenericProxy,
		ResponsesUpstreamProfileChatOnlyProxy:
		return s.ResponsesUpstreamProfile
	default:
		return ""
	}
}

func (s *ChannelOtherSettings) HasResponsesProxyCompatibilityProfile() bool {
	profile := s.NormalizedResponsesUpstreamProfile()
	return profile == ResponsesUpstreamProfileGenericProxy ||
		profile == ResponsesUpstreamProfileChatOnlyProxy
}

func (s *ChannelOtherSettings) ResolveResponsesChannelCapability(channelType int) ResponsesChannelCapabilitySnapshot {
	modeSetting := s.NormalizedResponsesCompactModeSetting()
	effectiveMode := s.EffectiveResponsesCompactModeOrDefault()
	profile := s.NormalizedResponsesUpstreamProfile()
	snapshot := ResponsesChannelCapabilitySnapshot{
		Source:                            "safe_default",
		Profile:                           profile,
		CompactModeSetting:                modeSetting,
		CompactModeEffective:              effectiveMode,
		SupportsResponses:                 defaultSupportsResponsesCapability(channelType),
		SupportsResponsesCompact:          defaultSupportsResponsesCompactCapability(channelType, s, effectiveMode, profile),
		SupportsChat:                      defaultSupportsChatCapability(channelType, profile),
		SupportsRestPreviousResponseID:    defaultSupportsRESTPreviousResponseIDCapability(s, profile),
		SupportsCompactionItemPassthrough: defaultSupportsCompactionItemPassthroughCapability(profile),
		SupportsNamespaceTools:            defaultSupportsNamespaceToolsCapability(profile),
	}
	registry := s.responsesCapabilityRegistry()
	if registry != nil {
		if registry.Observed != nil {
			snapshot.Source = "observed_calls"
			snapshot.Observed = registry.Observed
		}
		if registry.Probe != nil {
			snapshot.Source = "probe"
			snapshot.Probe = registry.Probe
		}
		applyCapabilityOverrides(&snapshot, registry)
	}
	if snapshot.Source == "safe_default" && (profile != "" || s.explicitResponsesCapabilityConfigured()) {
		snapshot.Source = "explicit_settings"
	}
	return snapshot
}

func (s *ChannelOtherSettings) responsesCapabilityRegistry() *ResponsesChannelCapabilityRegistry {
	if s == nil {
		return nil
	}
	return s.ResponsesCapabilityRegistry
}

func (s *ChannelOtherSettings) explicitResponsesCapabilityConfigured() bool {
	return s != nil && (s.ResponsesCompactMode != "" || s.ResponsesUpstreamProfile != "")
}

func applyCapabilityOverrides(snapshot *ResponsesChannelCapabilitySnapshot, registry *ResponsesChannelCapabilityRegistry) {
	if snapshot == nil || registry == nil {
		return
	}
	if registry.SupportsResponses != nil {
		snapshot.SupportsResponses = *registry.SupportsResponses
	}
	if registry.SupportsResponsesCompact != nil {
		snapshot.SupportsResponsesCompact = *registry.SupportsResponsesCompact
	}
	if registry.SupportsChat != nil {
		snapshot.SupportsChat = *registry.SupportsChat
	}
	if registry.SupportsRestPreviousResponseID != nil {
		snapshot.SupportsRestPreviousResponseID = *registry.SupportsRestPreviousResponseID
	}
	if registry.SupportsCompactionItemPassthrough != nil {
		snapshot.SupportsCompactionItemPassthrough = *registry.SupportsCompactionItemPassthrough
	}
	if registry.SupportsNamespaceTools != nil {
		snapshot.SupportsNamespaceTools = *registry.SupportsNamespaceTools
	}
}

func defaultSupportsResponsesCapability(channelType int) bool {
	switch channelType {
	case constant.ChannelTypeOpenAI, constant.ChannelTypeAzure, constant.ChannelTypeCodex:
		return true
	default:
		return false
	}
}

func defaultSupportsResponsesCompactCapability(channelType int, settings *ChannelOtherSettings, effectiveMode ResponsesCompactMode, profile ResponsesUpstreamProfile) bool {
	if effectiveMode != ResponsesCompactModeNative || (settings != nil && settings.HasDisabledResponsesCompact()) {
		return false
	}
	switch profile {
	case ResponsesUpstreamProfileGenericProxy, ResponsesUpstreamProfileChatOnlyProxy:
		return false
	default:
		return channelType == constant.ChannelTypeOpenAI
	}
}

func defaultSupportsChatCapability(channelType int, profile ResponsesUpstreamProfile) bool {
	if profile == ResponsesUpstreamProfileChatOnlyProxy {
		return true
	}
	switch channelType {
	case constant.ChannelTypeOpenAI, constant.ChannelTypeAzure, constant.ChannelTypeCodex:
		return true
	default:
		return false
	}
}

func defaultSupportsRESTPreviousResponseIDCapability(settings *ChannelOtherSettings, profile ResponsesUpstreamProfile) bool {
	if settings != nil && settings.DisallowsResponsesRESTPreviousResponseID() {
		return false
	}
	switch profile {
	case ResponsesUpstreamProfileOfficialOpenAI,
		ResponsesUpstreamProfileOfficialNewAPI,
		ResponsesUpstreamProfileSameClusterNewAPI,
		ResponsesUpstreamProfileTrustedNewAPI,
		ResponsesUpstreamProfileSub2APIWSV2:
		return true
	default:
		return false
	}
}

func defaultSupportsCompactionItemPassthroughCapability(profile ResponsesUpstreamProfile) bool {
	switch profile {
	case ResponsesUpstreamProfileGenericOpenAI,
		ResponsesUpstreamProfileSub2APIHTTP,
		ResponsesUpstreamProfileGenericProxy,
		ResponsesUpstreamProfileChatOnlyProxy:
		return false
	default:
		return true
	}
}

func defaultSupportsNamespaceToolsCapability(profile ResponsesUpstreamProfile) bool {
	switch profile {
	case ResponsesUpstreamProfileOfficialOpenAI,
		ResponsesUpstreamProfileOfficialNewAPI,
		ResponsesUpstreamProfileSameClusterNewAPI,
		ResponsesUpstreamProfileTrustedNewAPI,
		ResponsesUpstreamProfileSub2APIWSV2:
		return true
	default:
		return false
	}
}

func (s *ChannelOtherSettings) HasResponsesEncryptedReasoningUnsupportedProfile() bool {
	profile := s.NormalizedResponsesUpstreamProfile()
	return s.HasResponsesProxyCompatibilityProfile() ||
		profile == ResponsesUpstreamProfileSub2APIHTTP
}

func (s *ChannelOtherSettings) ShouldStripResponsesEncryptedReasoning() bool {
	return s != nil && (s.StripCodexEncryptedContext || s.HasResponsesEncryptedReasoningUnsupportedProfile())
}

func (s *ChannelOtherSettings) DisallowsResponsesRESTPreviousResponseID() bool {
	return s != nil && s.NormalizedResponsesUpstreamProfile() == ResponsesUpstreamProfileSub2APIHTTP
}

func (s *ChannelOtherSettings) HasNativeResponsesCompact() bool {
	return s != nil && s.EffectiveResponsesCompactModeOrDefault() == ResponsesCompactModeNative
}

func (s *ChannelOtherSettings) HasSyntheticResponsesCompact() bool {
	return s != nil && s.EffectiveResponsesCompactModeOrDefault() == ResponsesCompactModeSynthetic
}

func (s *ChannelOtherSettings) HasDisabledResponsesCompact() bool {
	return s != nil && s.NormalizedResponsesCompactModeSetting() == ResponsesCompactModeDisabled
}

func (s *ChannelOtherSettings) IsAutoResponsesCompact() bool {
	return s == nil || s.ResponsesCompactMode == "" || s.ResponsesCompactMode == ResponsesCompactModeAuto
}

func (s *ChannelOtherSettings) AllowAutoNativeCompactBaseFallbackAt(now time.Time) bool {
	if s != nil && !s.IsAutoResponsesCompact() {
		return false
	}
	return !s.HasActiveResponsesCompactAutoFallback(now)
}

func (s *ChannelOtherSettings) ResponsesCompactContextFallbackEnabled() bool {
	return s == nil || s.ResponsesCompactContextFallback == nil || *s.ResponsesCompactContextFallback
}

func (s *ChannelOtherSettings) ResponsesCompactSummaryModelFallbackEnabled() bool {
	return s == nil || s.ResponsesCompactSummaryModelFallback == nil || *s.ResponsesCompactSummaryModelFallback
}

func (s *ChannelOtherSettings) ResponsesCompactSummaryFallbackModelsOrDefault() []string {
	models := []string{DefaultResponsesCompactSyntheticFallbackModel}
	if s != nil && len(s.ResponsesCompactSummaryFallbackModels) > 0 {
		models = s.ResponsesCompactSummaryFallbackModels
	}
	result := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		result = append(result, model)
	}
	if len(result) == 0 {
		return []string{DefaultResponsesCompactSyntheticFallbackModel}
	}
	return result
}

func (s *ChannelOtherSettings) ResponsesCompactAutoFallbackRetryIntervalHoursOrDefault() int {
	if s == nil || s.ResponsesCompactAutoFallbackRetryIntervalHours == 0 {
		return DefaultResponsesCompactAutoFallbackRetryIntervalHours
	}
	if s.ResponsesCompactAutoFallbackRetryIntervalHours < MinResponsesCompactAutoFallbackRetryIntervalHours {
		return MinResponsesCompactAutoFallbackRetryIntervalHours
	}
	if s.ResponsesCompactAutoFallbackRetryIntervalHours > MaxResponsesCompactAutoFallbackRetryIntervalHours {
		return MaxResponsesCompactAutoFallbackRetryIntervalHours
	}
	return s.ResponsesCompactAutoFallbackRetryIntervalHours
}

func (s *ChannelOtherSettings) NormalizedResponsesCompactModeSetting() ResponsesCompactMode {
	if s == nil || s.ResponsesCompactMode == "" {
		return ResponsesCompactModeAuto
	}
	switch s.ResponsesCompactMode {
	case ResponsesCompactModeAuto:
		return ResponsesCompactModeAuto
	case ResponsesCompactModeNative:
		return ResponsesCompactModeNative
	case ResponsesCompactModeSynthetic:
		return ResponsesCompactModeSynthetic
	case ResponsesCompactModeDisabled:
		return ResponsesCompactModeDisabled
	case ResponsesCompactMode("convert"):
		return ResponsesCompactModeSynthetic
	case ResponsesCompactMode("unsupported"):
		return ResponsesCompactModeNative
	default:
		return ResponsesCompactModeAuto
	}
}

func (s *ChannelOtherSettings) ResponsesCompactModeOrDefault() ResponsesCompactMode {
	return s.ResponsesCompactModeOrDefaultAt(time.Now())
}

func (s *ChannelOtherSettings) EffectiveResponsesCompactModeOrDefault() ResponsesCompactMode {
	return s.EffectiveResponsesCompactModeOrDefaultAt(time.Now())
}

func (s *ChannelOtherSettings) EffectiveResponsesCompactModeOrDefaultAt(now time.Time) ResponsesCompactMode {
	if s != nil && s.HasDisabledResponsesCompact() {
		return ResponsesCompactModeDisabled
	}
	if s != nil && s.HasResponsesProxyCompatibilityProfile() {
		return ResponsesCompactModeSynthetic
	}
	return s.ResponsesCompactModeOrDefaultAt(now)
}

func (s *ChannelOtherSettings) ResponsesCompactModeOrDefaultAt(now time.Time) ResponsesCompactMode {
	if s == nil || s.ResponsesCompactMode == "" {
		return ResponsesCompactModeNative
	}
	switch s.ResponsesCompactMode {
	case ResponsesCompactModeAuto:
		if s.HasActiveResponsesCompactAutoFallback(now) {
			return ResponsesCompactModeSynthetic
		}
		return ResponsesCompactModeNative
	case ResponsesCompactModeNative:
		return ResponsesCompactModeNative
	case ResponsesCompactModeSynthetic:
		return ResponsesCompactModeSynthetic
	case ResponsesCompactModeDisabled:
		return ResponsesCompactModeDisabled
	case ResponsesCompactMode("convert"):
		return ResponsesCompactModeSynthetic
	default:
		return ResponsesCompactModeNative
	}
}

func (s *ChannelOtherSettings) HasActiveResponsesCompactAutoFallback(now time.Time) bool {
	if s == nil || s.ResponsesCompactMode != ResponsesCompactModeAuto {
		return false
	}
	if s.ResponsesCompactAutoFallbackAt > 0 {
		elapsed := now.UTC().Unix() - s.ResponsesCompactAutoFallbackAt
		intervalSeconds := int64(s.ResponsesCompactAutoFallbackRetryIntervalHoursOrDefault()) * int64(time.Hour/time.Second)
		return elapsed >= 0 && elapsed < intervalSeconds
	}
	return s.ResponsesCompactAutoFallbackDate == ResponsesCompactAutoFallbackDate(now)
}

func (s *ChannelOtherSettings) MarkResponsesCompactAutoFallback(now time.Time, reason string) {
	if s == nil {
		return
	}
	if s.ResponsesCompactMode == "" {
		s.ResponsesCompactMode = ResponsesCompactModeAuto
	}
	if s.ResponsesCompactMode != ResponsesCompactModeAuto {
		return
	}
	s.ResponsesCompactAutoFallbackAt = now.UTC().Unix()
	s.ResponsesCompactAutoFallbackDate = 0
	s.ResponsesCompactAutoFallbackReason = reason
}

func ResponsesCompactAutoFallbackDate(t time.Time) int {
	year, month, day := t.UTC().Date()
	return year*10000 + int(month)*100 + day
}
