package controller

import (
	"net/url"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type channelTestRuntimeSummary struct {
	RuntimeConfigEnabled    bool                       `json:"runtime_config_enabled"`
	HeaderConfigEnabled     bool                       `json:"header_config_enabled"`
	HeaderConfigured        bool                       `json:"header_configured"`
	HeaderApplied           bool                       `json:"header_applied"`
	HeaderKeys              []string                   `json:"header_keys,omitempty"`
	HeaderProfileID         string                     `json:"header_profile_id,omitempty"`
	HeaderProfileMode       string                     `json:"header_profile_mode,omitempty"`
	HeaderProfileApplied    bool                       `json:"header_profile_applied"`
	ParamOverrideEnabled    bool                       `json:"param_override_enabled"`
	ParamOverrideConfigured bool                       `json:"param_override_configured"`
	ParamOverrideApplied    bool                       `json:"param_override_applied"`
	ParamOverrideAudit      []string                   `json:"param_override_audit,omitempty"`
	ProxyEnabled            bool                       `json:"proxy_enabled"`
	ProxyConfigured         bool                       `json:"proxy_configured"`
	ProxyDisplay            string                     `json:"proxy_display,omitempty"`
	ModelMappingEnabled     bool                       `json:"model_mapping_enabled"`
	ModelMappingConfigured  bool                       `json:"model_mapping_configured"`
	ModelMappingApplied     bool                       `json:"model_mapping_applied"`
	OriginModel             string                     `json:"origin_model,omitempty"`
	UpstreamModel           string                     `json:"upstream_model,omitempty"`
	EndpointType            string                     `json:"endpoint_type,omitempty"`
	RequestPath             string                     `json:"request_path,omitempty"`
	FinalRequestPath        string                     `json:"final_request_path,omitempty"`
	ProtocolStrategy        string                     `json:"protocol_strategy,omitempty"`
	RelayFormat             string                     `json:"relay_format,omitempty"`
	FinalRelayFormat        string                     `json:"final_relay_format,omitempty"`
	RequestConversionChain  []string                   `json:"request_conversion_chain,omitempty"`
	TestPrompt              string                     `json:"test_prompt,omitempty"`
	MaxTokens               uint                       `json:"max_tokens,omitempty"`
	Stream                  bool                       `json:"stream"`
	ConfigWarnings          []string                   `json:"config_warnings,omitempty"`
	ErrorDiagnosis          *channelTestErrorDiagnosis `json:"error_diagnosis,omitempty"`
}

func buildChannelTestRuntimeSummary(channel *model.Channel, options channelTestOptions, modelName string, endpointType string, requestPath string, isStream bool) *channelTestRuntimeSummary {
	options = normalizeChannelTestOptions(options)
	summary := &channelTestRuntimeSummary{
		RuntimeConfigEnabled: options.UseRuntimeConfig,
		HeaderConfigEnabled:  options.UseHeaderConfig,
		ParamOverrideEnabled: options.UseParamOverride,
		ProxyEnabled:         options.UseProxy,
		ModelMappingEnabled:  options.UseModelMapping,
		OriginModel:          strings.TrimSpace(modelName),
		EndpointType:         strings.TrimSpace(endpointType),
		RequestPath:          strings.TrimSpace(requestPath),
		FinalRequestPath:     strings.TrimSpace(requestPath),
		ProtocolStrategy:     options.ResponseProtocol,
		TestPrompt:           options.TestPrompt,
		Stream:               isStream,
		HeaderKeys:           []string{},
		ParamOverrideAudit:   []string{},
		ConfigWarnings:       []string{},
	}
	if options.MaxTokens != nil {
		summary.MaxTokens = *options.MaxTokens
	}
	if channel == nil {
		return summary
	}

	setting, err := parseChannelTestSetting(channel)
	if err != nil {
		summary.ConfigWarnings = append(summary.ConfigWarnings, "channel setting invalid")
	}
	proxy := strings.TrimSpace(setting.Proxy)
	summary.ProxyConfigured = proxy != ""
	summary.ProxyDisplay = maskChannelTestProxy(proxy)

	settings, err := parseChannelTestOtherSettings(channel)
	if err != nil {
		summary.ConfigWarnings = append(summary.ConfigWarnings, "channel settings invalid")
	}
	if strategy := settings.HeaderProfileStrategy; strategy != nil && strategy.Enabled {
		summary.HeaderConfigured = true
		summary.HeaderProfileMode = string(strategy.Mode)
		if len(strategy.SelectedProfileIDs) > 0 {
			summary.HeaderProfileID = strings.Join(strategy.SelectedProfileIDs, ",")
		}
	}
	if strings.TrimSpace(channel.OtherSettings) != "" {
		if settings.HeaderPolicyMode != "" || settings.OverrideHeaderUserAgent || settings.UserAgentStrategy != nil {
			summary.HeaderConfigured = true
		}
	}
	if headerKeys := parseChannelTestHeaderKeys(channel.HeaderOverride); len(headerKeys) > 0 {
		summary.HeaderConfigured = true
		summary.HeaderKeys = headerKeys
	}

	summary.ParamOverrideConfigured = stringPointerHasConfig(channel.ParamOverride)
	summary.ModelMappingConfigured = stringPointerHasConfig(channel.ModelMapping)
	return summary
}

func finalizeChannelTestRuntimeSummary(summary *channelTestRuntimeSummary, c *gin.Context, info *relaycommon.RelayInfo) {
	if summary == nil {
		return
	}
	if info != nil {
		if requestPath := strings.TrimSpace(info.RequestURLPath); requestPath != "" {
			summary.FinalRequestPath = requestPath
		}
		summary.RelayFormat = string(info.RelayFormat)
		summary.FinalRelayFormat = string(info.GetFinalRequestRelayFormat())
		summary.RequestConversionChain = relayFormatsToStrings(info.RequestConversionChain)
		if info.ChannelMeta != nil {
			summary.UpstreamModel = strings.TrimSpace(info.UpstreamModelName)
			summary.ModelMappingApplied = info.IsModelMapped || (summary.OriginModel != "" && summary.UpstreamModel != "" && summary.OriginModel != summary.UpstreamModel)
		}
		if len(info.ParamOverrideAudit) > 0 {
			summary.ParamOverrideApplied = true
			summary.ParamOverrideAudit = append([]string{}, info.ParamOverrideAudit...)
		}
		if info.UseRuntimeHeadersOverride && len(info.RuntimeHeadersOverride) > 0 && summary.ParamOverrideConfigured {
			summary.ParamOverrideApplied = true
		}
		headerKeys := sortedInterfaceMapKeys(relaycommon.GetEffectiveHeaderOverride(info))
		if len(headerKeys) > 0 {
			summary.HeaderApplied = true
			summary.HeaderKeys = mergeSortedStrings(summary.HeaderKeys, headerKeys)
		}
	}
	if c == nil {
		return
	}
	if audit, ok := common.GetContextKeyType[service.RuntimeHeaderPolicyAudit](c, constant.ContextKeyChannelHeaderPolicyAudit); ok {
		if audit.HeaderProfileID != "" {
			summary.HeaderProfileID = audit.HeaderProfileID
		}
		if audit.HeaderProfileMode != "" {
			summary.HeaderProfileMode = audit.HeaderProfileMode
		}
		if audit.HeaderProfileApplied {
			summary.HeaderProfileApplied = true
			summary.HeaderApplied = true
		}
		if audit.UserAgentApplied || len(audit.AppliedHeaderKeys) > 0 {
			summary.HeaderApplied = true
			summary.HeaderKeys = mergeSortedStrings(summary.HeaderKeys, audit.AppliedHeaderKeys)
		}
	}
}

type channelTestErrorDiagnosis struct {
	Category   string `json:"category"`
	Summary    string `json:"summary"`
	Suggestion string `json:"suggestion"`
}

func relayFormatsToStrings(formats []types.RelayFormat) []string {
	if len(formats) == 0 {
		return nil
	}
	values := make([]string, 0, len(formats))
	for _, format := range formats {
		value := strings.TrimSpace(string(format))
		if value == "" {
			continue
		}
		values = append(values, value)
	}
	return values
}

func parseChannelTestHeaderKeys(raw *string) []string {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil
	}
	headers := map[string]interface{}{}
	if err := common.Unmarshal([]byte(*raw), &headers); err != nil {
		return nil
	}
	return sortedInterfaceMapKeys(headers)
}

func sortedInterfaceMapKeys(values map[string]interface{}) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func mergeSortedStrings(base []string, extra []string) []string {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	merged := make([]string, 0, len(base)+len(extra))
	for _, item := range append(base, extra...) {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, trimmed)
	}
	sort.Strings(merged)
	return merged
}

func stringPointerHasConfig(value *string) bool {
	return value != nil && strings.TrimSpace(*value) != "" && strings.TrimSpace(*value) != "{}"
}

func maskChannelTestProxy(proxy string) string {
	proxy = strings.TrimSpace(proxy)
	if proxy == "" {
		return ""
	}
	parsed, err := url.Parse(proxy)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "configured"
	}
	return parsed.Scheme + "://" + parsed.Host
}
