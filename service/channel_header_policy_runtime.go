package service

import (
	"errors"
	"fmt"
	"net/http"
	"net/textproto"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

const (
	runtimeRoundRobinMaxAttempts  = 12
	runtimeRoundRobinRetryBackoff = 2 * time.Millisecond
)

type runtimeHeaderPolicy struct {
	Tag                     string
	HeaderOverride          map[string]any
	HeaderPolicyMode        string
	OverrideHeaderUserAgent bool
	UserAgentStrategy       *dto.UserAgentStrategy
	UserAgentState          UserAgentStrategyState
}

type RuntimeHeaderPolicyAudit struct {
	HeaderPolicyMode        string   `json:"header_policy_mode"`
	AppliedHeaderKeys       []string `json:"applied_header_keys,omitempty"`
	HeaderProfileID         string   `json:"header_profile_id,omitempty"`
	HeaderProfileMode       string   `json:"header_profile_mode,omitempty"`
	HeaderProfileApplied    bool     `json:"header_profile_applied"`
	UserAgentApplied        bool     `json:"user_agent_applied"`
	SelectedUserAgent       string   `json:"selected_user_agent,omitempty"`
	UserAgentStrategyMode   string   `json:"ua_strategy_mode,omitempty"`
	UserAgentStrategyScope  string   `json:"ua_strategy_scope,omitempty"`
	OverrideStaticUserAgent bool     `json:"override_static_user_agent,omitempty"`
}

func BuildChannelRuntimeHeaderOverride(channel *model.Channel) (map[string]any, error) {
	headerOverride, _, err := BuildChannelRuntimeHeaderOverrideWithAudit(channel)
	return headerOverride, err
}

func BuildChannelRuntimeRequestHeaders(channel *model.Channel, key string, headers http.Header) (http.Header, error) {
	if headers == nil {
		headers = http.Header{}
	}
	if channel == nil {
		return headers, nil
	}
	apply, err := ShouldApplyChannelRuntimeRequestHeaders(channel)
	if err != nil {
		return nil, err
	}
	if !apply {
		return headers, nil
	}

	settings := channel.GetOtherSettings()
	profileHeaders, _, err := dto.ResolveHeaderProfileStrategyHeaders(settings.HeaderProfileStrategy, 0)
	if err != nil {
		return nil, err
	}
	for name, value := range profileHeaders {
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name == "" || value == "" || isRuntimeProfileUnsafeHeader(name) {
			continue
		}
		headers.Set(name, value)
	}

	headerOverride, _, err := BuildChannelRuntimeHeaderOverrideWithAudit(channel)
	if err != nil {
		return nil, err
	}
	for name, value := range headerOverride {
		if isRuntimeHeaderPassthroughRuleKey(name) || value == nil {
			continue
		}
		str := strings.TrimSpace(fmt.Sprintf("%v", value))
		if str == "" || strings.Contains(str, "{client_header:") {
			continue
		}
		if strings.Contains(str, "{api_key}") {
			str = strings.ReplaceAll(str, "{api_key}", key)
		}
		headers.Set(name, str)
	}
	return headers, nil
}

func ShouldApplyChannelRuntimeRequestHeaders(channel *model.Channel) (bool, error) {
	globalEnabled, err := getRequestHeaderPolicyAuxiliaryRequestsEnabled()
	if err != nil || !globalEnabled {
		return false, err
	}
	if channel == nil {
		return false, nil
	}
	settings := channel.GetOtherSettings()
	if settings.AuxiliaryRequestHeaderPolicyEnabled == nil {
		return true, nil
	}
	return *settings.AuxiliaryRequestHeaderPolicyEnabled, nil
}

func getRequestHeaderPolicyAuxiliaryRequestsEnabled() (bool, error) {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()

	raw, exists := common.OptionMap["RequestHeaderPolicyAuxiliaryRequestsEnabled"]
	if !exists || strings.TrimSpace(raw) == "" {
		return true, nil
	}
	enabled, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false, fmt.Errorf("辅助请求头策略全局开关不合法: %s", raw)
	}
	return enabled, nil
}

func ApplyRuntimeRequestHeaders(req *http.Request, headers http.Header) {
	if req == nil {
		return
	}
	for name, values := range headers {
		if len(values) == 0 {
			continue
		}
		req.Header.Del(name)
		for _, value := range values {
			req.Header.Add(name, value)
		}
		if strings.EqualFold(name, "Host") {
			req.Host = values[0]
		}
	}
}

var runtimeProfileUnsafeHeaderNames = map[string]struct{}{
	"accept-encoding":     {},
	"authorization":       {},
	"connection":          {},
	"content-length":      {},
	"cookie":              {},
	"host":                {},
	"keep-alive":          {},
	"origin":              {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"proxy-connection":    {},
	"te":                  {},
	"trailer":             {},
	"transfer-encoding":   {},
	"upgrade":             {},
	"cf-connecting-ip":    {},
	"forwarded":           {},
	"x-api-key":           {},
	"x-forwarded-for":     {},
	"x-forwarded-host":    {},
	"x-forwarded-proto":   {},
	"x-goog-api-key":      {},
	"x-real-ip":           {},
}

func isRuntimeProfileUnsafeHeader(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if strings.HasPrefix(normalized, "sec-fetch-") {
		return true
	}
	_, exists := runtimeProfileUnsafeHeaderNames[normalized]
	return exists
}

func BuildChannelRuntimeHeaderOverrideWithAudit(channel *model.Channel) (map[string]any, *RuntimeHeaderPolicyAudit, error) {
	if channel == nil {
		return map[string]any{}, &RuntimeHeaderPolicyAudit{}, nil
	}

	channelHeaders := normalizeRuntimeHeaderOverrideMap(channel.GetHeaderOverride())
	channelSettings := channel.GetOtherSettings()
	channelStrategy, channelState, err := resolveRuntimeUserAgentStrategy(channelSettings.UserAgentStrategy)
	if err != nil {
		return nil, nil, err
	}

	tagPolicy, err := loadRuntimeTagHeaderPolicy(channel.GetTag())
	if err != nil {
		return nil, nil, err
	}

	mode, err := resolveEffectiveHeaderPolicyMode(string(channelSettings.HeaderPolicyMode), tagPolicy.HeaderPolicyMode)
	if err != nil {
		return nil, nil, err
	}
	finalHeaders := resolveRuntimeHeaderOverrideByMode(mode, channelHeaders, tagPolicy.HeaderOverride)
	audit := &RuntimeHeaderPolicyAudit{
		HeaderPolicyMode:  mode,
		AppliedHeaderKeys: collectRuntimeHeaderKeys(finalHeaders),
	}

	finalStrategy, scopeType, scopeKey, shouldOverride := resolveRuntimeUserAgentPolicy(
		mode,
		channel.Id,
		channelSettings.OverrideHeaderUserAgent,
		channelStrategy,
		channelState,
		tagPolicy,
	)
	if finalStrategy == nil || !finalStrategy.Enabled {
		return finalHeaders, audit, nil
	}
	audit.UserAgentStrategyMode = finalStrategy.Mode
	audit.UserAgentStrategyScope = scopeKey
	audit.OverrideStaticUserAgent = shouldOverride

	selectedUA, err := selectRuntimeUserAgent(finalStrategy, scopeType, scopeKey)
	if err != nil {
		return nil, nil, err
	}
	if selectedUA == "" {
		return finalHeaders, audit, nil
	}
	audit.SelectedUserAgent = selectedUA

	if hasRuntimeHeaderKey(finalHeaders, "User-Agent") {
		if shouldOverride {
			setRuntimeHeaderValue(finalHeaders, "User-Agent", selectedUA)
			audit.UserAgentApplied = true
		}
		audit.AppliedHeaderKeys = collectRuntimeHeaderKeys(finalHeaders)
		return finalHeaders, audit, nil
	}

	setRuntimeHeaderValue(finalHeaders, "User-Agent", selectedUA)
	audit.UserAgentApplied = true
	audit.AppliedHeaderKeys = collectRuntimeHeaderKeys(finalHeaders)
	return finalHeaders, audit, nil
}

func loadRuntimeTagHeaderPolicy(tag string) (*runtimeHeaderPolicy, error) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return &runtimeHeaderPolicy{}, nil
	}

	record := model.TagRequestHeaderPolicy{}
	err := model.DB.Where("tag = ?", tag).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &runtimeHeaderPolicy{}, nil
	}
	if err != nil {
		return nil, err
	}

	headerOverride, err := decodeStoredRuntimeHeaderOverride(record.HeaderOverride)
	if err != nil {
		return nil, err
	}
	strategy, state, err := decodeStoredRuntimeUserAgentStrategy(record.UserAgentStrategyJSON)
	if err != nil {
		return nil, err
	}

	return &runtimeHeaderPolicy{
		Tag:                     tag,
		HeaderOverride:          headerOverride,
		HeaderPolicyMode:        strings.TrimSpace(record.HeaderPolicyMode),
		OverrideHeaderUserAgent: record.OverrideHeaderUserAgent,
		UserAgentStrategy:       strategy,
		UserAgentState:          state,
	}, nil
}

func decodeStoredRuntimeHeaderOverride(raw string) (map[string]any, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return map[string]any{}, nil
	}

	headers, err := ValidateHeaderTemplate(trimmed)
	if err != nil {
		return nil, err
	}
	return toAnyHeaderMap(headers), nil
}

func decodeStoredRuntimeUserAgentStrategy(raw string) (*dto.UserAgentStrategy, UserAgentStrategyState, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, UserAgentStrategyStateUnconfigured, nil
	}

	var strategy dto.UserAgentStrategy
	if err := common.UnmarshalJsonStr(trimmed, &strategy); err != nil {
		return nil, UserAgentStrategyStateInvalid, errors.New("已存储的UA策略格式不合法")
	}

	return resolveRuntimeUserAgentStrategy(&strategy)
}

func resolveRuntimeUserAgentStrategy(strategy *dto.UserAgentStrategy) (*dto.UserAgentStrategy, UserAgentStrategyState, error) {
	if strategy == nil {
		return nil, UserAgentStrategyStateUnconfigured, nil
	}

	result, err := ResolveUserAgentStrategy(strategy)
	if err != nil {
		return nil, UserAgentStrategyStateInvalid, err
	}
	if strategy.Enabled {
		return result.Strategy, result.State, nil
	}

	normalized := &dto.UserAgentStrategy{Enabled: false}
	mode := strings.TrimSpace(strategy.Mode)
	if mode != "" {
		normalized.Mode = mode
	}
	userAgents := MergeUserAgents(strategy.UserAgents, nil)
	if len(userAgents) > 0 {
		normalized.UserAgents = userAgents
	}

	return normalized, UserAgentStrategyStateDisabled, nil
}

func resolveEffectiveHeaderPolicyMode(channelMode string, tagMode string) (string, error) {
	normalizedTagMode, err := parseRuntimeHeaderPolicyMode(tagMode, true)
	if err != nil {
		return "", err
	}
	if normalizedTagMode != string(dto.HeaderPolicyModeSystemDefault) {
		return normalizedTagMode, nil
	}

	normalizedChannelMode, err := parseRuntimeHeaderPolicyMode(channelMode, true)
	if err != nil {
		return "", err
	}
	if normalizedChannelMode != string(dto.HeaderPolicyModeSystemDefault) {
		return normalizedChannelMode, nil
	}

	return getRequestHeaderPolicyDefaultMode()
}

func parseRuntimeHeaderPolicyMode(raw string, allowSystemDefault bool) (string, error) {
	mode := strings.TrimSpace(raw)
	if mode == "" {
		return string(dto.HeaderPolicyModeSystemDefault), nil
	}

	switch dto.HeaderPolicyMode(mode) {
	case dto.HeaderPolicyModePreferChannel, dto.HeaderPolicyModePreferTag, dto.HeaderPolicyModeMerge:
		return mode, nil
	case dto.HeaderPolicyModeSystemDefault:
		if allowSystemDefault {
			return mode, nil
		}
		return "", fmt.Errorf("请求头优先级模式不支持 system_default")
	default:
		return "", fmt.Errorf("请求头优先级模式不合法: %s", raw)
	}
}

func getRequestHeaderPolicyDefaultMode() (string, error) {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()

	raw, exists := common.OptionMap["RequestHeaderPolicyDefaultMode"]
	if !exists {
		return string(dto.HeaderPolicyModePreferChannel), nil
	}
	return parseRuntimeHeaderPolicyMode(raw, false)
}

func resolveRuntimeHeaderOverrideByMode(mode string, channelHeaders map[string]any, tagHeaders map[string]any) map[string]any {
	switch mode {
	case string(dto.HeaderPolicyModePreferTag):
		if len(tagHeaders) > 0 {
			return cloneRuntimeHeaderOverrideMap(tagHeaders)
		}
		return cloneRuntimeHeaderOverrideMap(channelHeaders)
	case string(dto.HeaderPolicyModeMerge):
		return mergeRuntimeHeaderOverrideMaps(tagHeaders, channelHeaders)
	default:
		if len(channelHeaders) > 0 {
			return cloneRuntimeHeaderOverrideMap(channelHeaders)
		}
		return cloneRuntimeHeaderOverrideMap(tagHeaders)
	}
}

func resolveRuntimeUserAgentPolicy(
	mode string,
	channelID int,
	channelOverride bool,
	channelStrategy *dto.UserAgentStrategy,
	channelState UserAgentStrategyState,
	tagPolicy *runtimeHeaderPolicy,
) (*dto.UserAgentStrategy, string, string, bool) {
	switch mode {
	case string(dto.HeaderPolicyModePreferTag):
		return preferRuntimeUserAgentPolicy(
			tagPolicy.UserAgentStrategy,
			tagPolicy.UserAgentState,
			tagPolicy.OverrideHeaderUserAgent,
			"tag",
			"tag:"+tagPolicy.Tag,
			channelStrategy,
			channelState,
			channelOverride,
			"channel",
			fmt.Sprintf("channel:%d", channelID),
		)
	case string(dto.HeaderPolicyModeMerge):
		return mergeRuntimeUserAgentPolicy(channelID, channelOverride, channelStrategy, channelState, tagPolicy)
	default:
		return preferRuntimeUserAgentPolicy(
			channelStrategy,
			channelState,
			channelOverride,
			"channel",
			fmt.Sprintf("channel:%d", channelID),
			tagPolicy.UserAgentStrategy,
			tagPolicy.UserAgentState,
			tagPolicy.OverrideHeaderUserAgent,
			"tag",
			"tag:"+tagPolicy.Tag,
		)
	}
}

func preferRuntimeUserAgentPolicy(
	primary *dto.UserAgentStrategy,
	primaryState UserAgentStrategyState,
	primaryOverride bool,
	primaryScopeType string,
	primaryScopeKey string,
	fallback *dto.UserAgentStrategy,
	fallbackState UserAgentStrategyState,
	fallbackOverride bool,
	fallbackScopeType string,
	fallbackScopeKey string,
) (*dto.UserAgentStrategy, string, string, bool) {
	switch primaryState {
	case UserAgentStrategyStateNormalized:
		return primary, primaryScopeType, primaryScopeKey, primaryOverride
	case UserAgentStrategyStateDisabled:
		return nil, "", "", false
	}

	if fallbackState == UserAgentStrategyStateNormalized {
		return fallback, fallbackScopeType, fallbackScopeKey, fallbackOverride
	}
	return nil, "", "", false
}

func mergeRuntimeUserAgentPolicy(
	channelID int,
	channelOverride bool,
	channelStrategy *dto.UserAgentStrategy,
	channelState UserAgentStrategyState,
	tagPolicy *runtimeHeaderPolicy,
) (*dto.UserAgentStrategy, string, string, bool) {
	if tagPolicy.UserAgentState == UserAgentStrategyStateDisabled || channelState == UserAgentStrategyStateDisabled {
		return nil, "", "", false
	}

	tagEnabled := tagPolicy.UserAgentState == UserAgentStrategyStateNormalized
	channelEnabled := channelState == UserAgentStrategyStateNormalized
	if !tagEnabled && !channelEnabled {
		return nil, "", "", false
	}
	if !tagEnabled {
		return channelStrategy, "channel", fmt.Sprintf("channel:%d", channelID), channelOverride
	}
	if !channelEnabled {
		return tagPolicy.UserAgentStrategy, "tag", "tag:" + tagPolicy.Tag, tagPolicy.OverrideHeaderUserAgent
	}

	merged := &dto.UserAgentStrategy{
		Enabled:    true,
		Mode:       tagPolicy.UserAgentStrategy.Mode,
		UserAgents: MergeUserAgents(tagPolicy.UserAgentStrategy.UserAgents, channelStrategy.UserAgents),
	}
	return merged, "channel", fmt.Sprintf("channel:%d:merged_tag:%s", channelID, tagPolicy.Tag), tagPolicy.OverrideHeaderUserAgent || channelOverride
}

func selectRuntimeUserAgent(strategy *dto.UserAgentStrategy, scopeType string, scopeKey string) (string, error) {
	if strategy == nil || !strategy.Enabled || len(strategy.UserAgents) == 0 {
		return "", nil
	}

	switch strategy.Mode {
	case "random":
		return strategy.UserAgents[common.GetRandomInt(len(strategy.UserAgents))], nil
	case "round_robin":
		return nextRoundRobinRuntimeUserAgent(scopeType, scopeKey, strategy.UserAgents)
	default:
		return "", fmt.Errorf("UA策略模式不支持: %s", strategy.Mode)
	}
}

func nextRoundRobinRuntimeUserAgent(scopeType string, scopeKey string, userAgents []string) (string, error) {
	if scopeType == "" || scopeKey == "" {
		return "", errors.New("UA轮询作用域不能为空")
	}

	now := common.GetTimestamp()
	for attempt := 0; attempt < runtimeRoundRobinMaxAttempts; attempt++ {
		state := model.RequestHeaderStrategyState{}
		err := model.DB.Where("scope_type = ? AND scope_key = ?", scopeType, scopeKey).First(&state).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			state = model.RequestHeaderStrategyState{
				ScopeType:        scopeType,
				ScopeKey:         scopeKey,
				RoundRobinCursor: 1,
				Version:          1,
				UpdatedAt:        now,
			}
			if err := model.DB.Create(&state).Error; err != nil {
				if isRuntimeDuplicateConstraintError(err) {
					sleepRuntimeRoundRobinRetry(attempt)
					continue
				}
				return "", err
			}
			return userAgents[0], nil
		case err != nil:
			return "", err
		default:
			index := int(state.RoundRobinCursor % int64(len(userAgents)))
			result := model.DB.Model(&model.RequestHeaderStrategyState{}).
				Where("scope_type = ? AND scope_key = ? AND version = ?", scopeType, scopeKey, state.Version).
				Updates(map[string]any{
					"round_robin_cursor": state.RoundRobinCursor + 1,
					"version":            state.Version + 1,
					"updated_at":         now,
				})
			if result.Error != nil {
				return "", result.Error
			}
			if result.RowsAffected == 1 {
				return userAgents[index], nil
			}
			sleepRuntimeRoundRobinRetry(attempt)
		}
	}

	return "", errors.New("UA轮询状态更新失败")
}

func sleepRuntimeRoundRobinRetry(attempt int) {
	time.Sleep(time.Duration(attempt+1) * runtimeRoundRobinRetryBackoff)
}

func normalizeRuntimeHeaderOverrideMap(source map[string]any) map[string]any {
	if len(source) == 0 {
		return map[string]any{}
	}

	normalized := make(map[string]any, len(source))
	for key, value := range source {
		normalizedKey := normalizeRuntimeHeaderKey(key)
		if normalizedKey == "" {
			continue
		}
		if isRuntimeHeaderPassthroughRuleKey(normalizedKey) {
			normalized[normalizedKey] = value
			continue
		}

		for existingKey := range normalized {
			if isRuntimeHeaderPassthroughRuleKey(existingKey) {
				continue
			}
			if strings.EqualFold(existingKey, normalizedKey) {
				delete(normalized, existingKey)
			}
		}
		normalized[normalizedKey] = value
	}
	return normalized
}

func normalizeRuntimeHeaderKey(key string) string {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return ""
	}
	if isRuntimeHeaderPassthroughRuleKey(trimmed) {
		return trimmed
	}
	return textproto.CanonicalMIMEHeaderKey(trimmed)
}

func mergeRuntimeHeaderOverrideMaps(base map[string]any, overlay map[string]any) map[string]any {
	merged := cloneRuntimeHeaderOverrideMap(base)
	for key, value := range normalizeRuntimeHeaderOverrideMap(overlay) {
		if isRuntimeHeaderPassthroughRuleKey(key) {
			merged[key] = value
			continue
		}
		setRuntimeHeaderValue(merged, key, value)
	}
	return merged
}

func cloneRuntimeHeaderOverrideMap(source map[string]any) map[string]any {
	cloned := make(map[string]any)
	for key, value := range normalizeRuntimeHeaderOverrideMap(source) {
		cloned[key] = value
	}
	return cloned
}

func hasRuntimeHeaderKey(source map[string]any, target string) bool {
	for key := range source {
		if isRuntimeHeaderPassthroughRuleKey(key) {
			continue
		}
		if strings.EqualFold(key, target) {
			return true
		}
	}
	return false
}

func setRuntimeHeaderValue(target map[string]any, key string, value any) {
	normalizedKey := normalizeRuntimeHeaderKey(key)
	for existingKey := range target {
		if isRuntimeHeaderPassthroughRuleKey(existingKey) {
			continue
		}
		if strings.EqualFold(existingKey, normalizedKey) {
			delete(target, existingKey)
		}
	}
	target[normalizedKey] = value
}

func toAnyHeaderMap(headers map[string]string) map[string]any {
	result := make(map[string]any, len(headers))
	for key, value := range headers {
		result[key] = value
	}
	return result
}

func isRuntimeDuplicateConstraintError(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}

	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "duplicate") || strings.Contains(lower, "unique")
}

func isRuntimeHeaderPassthroughRuleKey(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	if key == "*" {
		return true
	}
	lower := strings.ToLower(key)
	return strings.HasPrefix(lower, "re:") || strings.HasPrefix(lower, "regex:")
}

func collectRuntimeHeaderKeys(source map[string]any) []string {
	keys := make([]string, 0, len(source))
	for key := range normalizeRuntimeHeaderOverrideMap(source) {
		if isRuntimeHeaderPassthroughRuleKey(key) {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
