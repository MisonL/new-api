package service

import (
	"crypto/sha256"
	"encoding/hex"
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
	runtimeHeaderProfileScopeType = "channel_header_profile"
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
	HeaderPolicyMode          string                    `json:"header_policy_mode"`
	AppliedHeaderKeys         []string                  `json:"applied_header_keys,omitempty"`
	AppliedHeaders            []AppliedHeaderAuditEntry `json:"applied_headers,omitempty"`
	AppliedUserAgent          string                    `json:"applied_user_agent,omitempty"`
	HeaderProfileID           string                    `json:"header_profile_id,omitempty"`
	HeaderProfileMode         string                    `json:"header_profile_mode,omitempty"`
	HeaderProfileApplied      bool                      `json:"header_profile_applied"`
	UserAgentApplied          bool                      `json:"user_agent_applied"`
	SelectedUserAgent         string                    `json:"selected_user_agent,omitempty"`
	UserAgentStrategyMode     string                    `json:"ua_strategy_mode,omitempty"`
	UserAgentStrategyScope    string                    `json:"ua_strategy_scope,omitempty"`
	OverrideStaticUserAgent   bool                      `json:"override_static_user_agent,omitempty"`
	CodexDynamicHeadersStatus string                    `json:"codex_dynamic_headers_status,omitempty"`
	CodexDynamicHeaderKeys    []string                  `json:"codex_dynamic_header_keys,omitempty"`
}

type AppliedHeaderAuditEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

const AppliedHeaderAuditRedactedValue = "[redacted]"

var visibleHeaderAuditValueKeys = map[string]struct{}{
	"originator":            {},
	"session_id":            {},
	"user-agent":            {},
	"x-client-request-id":   {},
	"x-codex-beta-features": {},
	"x-codex-window-id":     {},
}

func BuildChannelRuntimeHeaderOverride(channel *model.Channel) (map[string]any, error) {
	headerOverride, _, err := BuildChannelRuntimeHeaderOverrideWithAudit(channel)
	return headerOverride, err
}

func BuildChannelRuntimeRequestHeaders(channel *model.Channel, key string, headers http.Header) (http.Header, error) {
	return buildChannelRuntimeRequestHeaders(channel, key, headers, "", "")
}

// BuildChannelRuntimeRequestHeadersForProfile applies the exact header profile
// captured when an asynchronous task was submitted. It intentionally bypasses
// the current round-robin/random profile cursor; a task must not change client
// identity between submission, polling and content retrieval.
func BuildChannelRuntimeRequestHeadersForProfile(channel *model.Channel, key string, headers http.Header, profileID string) (http.Header, error) {
	return buildChannelRuntimeRequestHeaders(channel, key, headers, strings.TrimSpace(profileID), "")
}

// BuildChannelRuntimeRequestHeadersForProfileWithFingerprint replays a
// persisted asynchronous-task profile and rejects content drift explicitly.
// The fingerprint is over the resolved profile headers only; credentials and
// client-header passthrough values are never persisted in the task snapshot.
func BuildChannelRuntimeRequestHeadersForProfileWithFingerprint(channel *model.Channel, key string, headers http.Header, profileID string, expectedFingerprint string) (http.Header, error) {
	return buildChannelRuntimeRequestHeaders(channel, key, headers, strings.TrimSpace(profileID), strings.TrimSpace(expectedFingerprint))
}

func buildChannelRuntimeRequestHeaders(channel *model.Channel, key string, headers http.Header, forcedProfileID string, expectedProfileFingerprint string) (http.Header, error) {
	if headers == nil {
		headers = http.Header{}
	}
	if expectedProfileFingerprint != "" && forcedProfileID == "" {
		return nil, errors.New("persisted header profile fingerprint is missing its profile identity")
	}
	if channel == nil {
		if forcedProfileID != "" || expectedProfileFingerprint != "" {
			return nil, fmt.Errorf("persisted header profile identity cannot be replayed without a channel")
		}
		return headers, nil
	}
	apply, err := ShouldApplyChannelRuntimeRequestHeaders(channel)
	if err != nil {
		return nil, err
	}
	if !apply {
		if forcedProfileID != "" || expectedProfileFingerprint != "" {
			return nil, fmt.Errorf("persisted header profile cannot be replayed because header policy is disabled for channel %d", channel.Id)
		}
		return headers, nil
	}

	settings := channel.GetOtherSettings()
	var profileHeaders map[string]string
	if forcedProfileID != "" {
		if settings.HeaderProfileStrategy == nil || !settings.HeaderProfileStrategy.Enabled {
			return nil, fmt.Errorf("persisted header profile %q is no longer enabled for channel %d", forcedProfileID, channel.Id)
		}
		selected := false
		for _, selectedID := range settings.HeaderProfileStrategy.SelectedProfileIDs {
			if runtimeHeaderProfileSelectionMatches(forcedProfileID, selectedID) {
				selected = true
				break
			}
		}
		if !selected {
			return nil, fmt.Errorf("persisted header profile %q is no longer selected for channel %d", forcedProfileID, channel.Id)
		}
		forcedStrategy := *settings.HeaderProfileStrategy
		forcedStrategy.Mode = dto.HeaderProfileModeFixed
		forcedStrategy.SelectedProfileIDs = []string{forcedProfileID}
		profileHeaders, _, err = dto.ResolveHeaderProfileStrategyHeaders(&forcedStrategy, 0)
	} else {
		profileHeaders, _, err = resolveChannelRuntimeHeaderProfileHeaders(channel.Id, settings.HeaderProfileStrategy)
	}
	if err != nil {
		return nil, err
	}
	if expectedProfileFingerprint != "" && model.ComputeTaskRouteHeaderProfileFingerprint(profileHeaders) != expectedProfileFingerprint {
		return nil, fmt.Errorf("persisted header profile %q content changed for channel %d", forcedProfileID, channel.Id)
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

// runtimeHeaderProfileSelectionMatches allows a task snapshot to replay a
// captured version when the channel still selects the same base profile (or
// its latest alias). Explicitly pinned selections must remain an exact match.
func runtimeHeaderProfileSelectionMatches(forcedProfileID string, selectedID string) bool {
	forcedProfileID = strings.TrimSpace(forcedProfileID)
	selectedID = strings.TrimSpace(selectedID)
	if forcedProfileID == "" || selectedID == "" {
		return false
	}
	if forcedProfileID == selectedID {
		return true
	}
	forcedBase, forcedVersion, forcedVersioned := strings.Cut(forcedProfileID, "@")
	selectedBase, selectedVersion, selectedVersioned := strings.Cut(selectedID, "@")
	if forcedBase != selectedBase {
		return false
	}
	if !forcedVersioned {
		// An unversioned snapshot refers to the dynamic base profile. Treat only
		// the explicit latest alias as the same selection; a pinned version must
		// not be silently replaced by a dynamic profile.
		return !selectedVersioned || strings.EqualFold(strings.TrimSpace(selectedVersion), dto.HeaderProfileLatestVersion)
	}
	if !selectedVersioned {
		return strings.EqualFold(strings.TrimSpace(forcedVersion), dto.HeaderProfileLatestVersion)
	}
	// Two explicitly versioned selections are compatible only when they are
	// exactly the same version. In particular, a pinned version must not match
	// either another pinned version or the dynamic latest alias.
	return strings.EqualFold(strings.TrimSpace(forcedVersion), strings.TrimSpace(selectedVersion))
}

func ResolveChannelRuntimeHeaderProfileHeaders(channelID int, strategy *dto.HeaderProfileStrategy) (map[string]string, string, error) {
	return resolveChannelRuntimeHeaderProfileHeaders(channelID, strategy)
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

	channelHeaders := map[string]any{}
	if channel.HeaderOverride != nil {
		validatedHeaders, err := validateHeaderTemplateAllowNull(*channel.HeaderOverride)
		if err != nil {
			return nil, nil, fmt.Errorf("channel header override is invalid: %w", err)
		}
		channelHeaders, err = normalizeRuntimeHeaderOverrideMapStrict(toAnyHeaderMap(validatedHeaders))
		if err != nil {
			return nil, nil, fmt.Errorf("channel header override is ambiguous: %w", err)
		}
	}
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
	finalHeaders, err := resolveRuntimeHeaderOverrideByMode(mode, channelHeaders, tagPolicy.HeaderOverride)
	if err != nil {
		return nil, nil, err
	}
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
		finalizeRuntimeHeaderPolicyAudit(audit, finalHeaders)
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
		finalizeRuntimeHeaderPolicyAudit(audit, finalHeaders)
		return finalHeaders, audit, nil
	}
	audit.SelectedUserAgent = selectedUA

	if hasRuntimeHeaderKey(finalHeaders, "User-Agent") {
		if shouldOverride {
			setRuntimeHeaderValue(finalHeaders, "User-Agent", selectedUA)
			audit.UserAgentApplied = true
		}
		finalizeRuntimeHeaderPolicyAudit(audit, finalHeaders)
		return finalHeaders, audit, nil
	}

	setRuntimeHeaderValue(finalHeaders, "User-Agent", selectedUA)
	audit.UserAgentApplied = true
	finalizeRuntimeHeaderPolicyAudit(audit, finalHeaders)
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

	headers, err := validateHeaderTemplateAllowNull(trimmed)
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

func resolveRuntimeHeaderOverrideByMode(mode string, channelHeaders map[string]any, tagHeaders map[string]any) (map[string]any, error) {
	// Channel headers are normalized and validated by the public builder before
	// this resolver is called. Normalize tag headers here because they come from
	// the persisted tag policy and are not otherwise validated at this boundary.
	var err error
	tagHeaders, err = normalizeRuntimeHeaderOverrideMapStrict(tagHeaders)
	if err != nil {
		return nil, fmt.Errorf("tag header override is ambiguous: %w", err)
	}
	switch mode {
	case string(dto.HeaderPolicyModePreferTag):
		if len(tagHeaders) > 0 {
			return cloneRuntimeHeaderOverrideMap(tagHeaders), nil
		}
		return cloneRuntimeHeaderOverrideMap(channelHeaders), nil
	case string(dto.HeaderPolicyModeMerge):
		return mergeRuntimeHeaderOverrideMaps(tagHeaders, channelHeaders), nil
	default:
		if len(channelHeaders) > 0 {
			return cloneRuntimeHeaderOverrideMap(channelHeaders), nil
		}
		return cloneRuntimeHeaderOverrideMap(tagHeaders), nil
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
	return nextRoundRobinRuntimeValue(scopeType, scopeKey, userAgents)
}

func nextRoundRobinRuntimeValue(scopeType string, scopeKey string, values []string) (string, error) {
	if scopeType == "" || scopeKey == "" {
		return "", errors.New("轮询作用域不能为空")
	}
	if len(values) == 0 {
		return "", errors.New("轮询列表不能为空")
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
			return values[0], nil
		case err != nil:
			return "", err
		default:
			index := int(state.RoundRobinCursor % int64(len(values)))
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
				return values[index], nil
			}
			sleepRuntimeRoundRobinRetry(attempt)
		}
	}

	return "", errors.New("轮询状态更新失败")
}

func resolveChannelRuntimeHeaderProfileHeaders(channelID int, strategy *dto.HeaderProfileStrategy) (map[string]string, string, error) {
	if strategy == nil || !strategy.Enabled {
		return nil, "", nil
	}

	requestIndex := 0
	if strategy.Mode == dto.HeaderProfileModeRoundRobin {
		selectedProfileIDs := normalizeRuntimeHeaderProfileIDs(strategy.SelectedProfileIDs)
		if len(selectedProfileIDs) > 1 {
			if channelID <= 0 {
				return nil, "", errors.New("请求头模板轮询需要有效渠道 ID")
			}
			scopeKey := fmt.Sprintf("channel:%d:%s", channelID, runtimeRoundRobinValuesFingerprint(selectedProfileIDs))
			selectedProfileID, err := nextRoundRobinRuntimeValue(
				runtimeHeaderProfileScopeType,
				scopeKey,
				selectedProfileIDs,
			)
			if err != nil {
				return nil, "", err
			}
			for index, profileID := range selectedProfileIDs {
				if profileID == selectedProfileID {
					requestIndex = index
					break
				}
			}
		}
	}

	return dto.ResolveHeaderProfileStrategyHeaders(strategy, requestIndex)
}

func normalizeRuntimeHeaderProfileIDs(profileIDs []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(profileIDs))
	for _, profileID := range profileIDs {
		trimmedID := strings.TrimSpace(profileID)
		if trimmedID == "" {
			continue
		}
		if _, exists := seen[trimmedID]; exists {
			continue
		}
		seen[trimmedID] = struct{}{}
		result = append(result, trimmedID)
	}
	return result
}

func runtimeRoundRobinValuesFingerprint(values []string) string {
	hash := sha256.New()
	for _, value := range values {
		hash.Write([]byte(strings.TrimSpace(value)))
		hash.Write([]byte{0})
	}
	sum := hash.Sum(nil)
	return hex.EncodeToString(sum[:16])
}

func sleepRuntimeRoundRobinRetry(attempt int) {
	time.Sleep(time.Duration(attempt+1) * runtimeRoundRobinRetryBackoff)
}

func normalizeRuntimeHeaderOverrideMap(source map[string]any) map[string]any {
	if len(source) == 0 {
		return map[string]any{}
	}

	keys := make([]string, 0, len(source))
	for key := range source {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	normalized := make(map[string]any, len(source))
	for _, key := range keys {
		value := source[key]
		normalizedKey := normalizeRuntimeHeaderKey(key)
		if normalizedKey == "" {
			continue
		}
		if _, exists := normalized[normalizedKey]; exists {
			// This helper is retained for already-validated internal maps. When
			// called defensively on a legacy map, keep the lexicographically first
			// spelling so map iteration order cannot change the applied header.
			continue
		}
		normalized[normalizedKey] = value
	}
	return normalized
}

// normalizeRuntimeHeaderOverrideMapStrict is the error-returning boundary for
// runtime maps. Header names are case-insensitive on the wire, so accepting
// two spellings of the same name would make the selected value depend on map
// iteration order.
func normalizeRuntimeHeaderOverrideMapStrict(source map[string]any) (map[string]any, error) {
	if len(source) == 0 {
		return map[string]any{}, nil
	}
	keys := make([]string, 0, len(source))
	for key := range source {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	normalized := make(map[string]any, len(source))
	seen := make(map[string]string, len(source))
	for _, key := range keys {
		normalizedKey := normalizeRuntimeHeaderKey(key)
		if normalizedKey == "" {
			continue
		}
		lookupKey := normalizedKey
		if !isRuntimeHeaderPassthroughRuleKey(normalizedKey) {
			lookupKey = strings.ToLower(normalizedKey)
		}
		if previousKey, exists := seen[lookupKey]; exists {
			return nil, fmt.Errorf("request headers contain duplicate names %q and %q", previousKey, key)
		}
		seen[lookupKey] = key
		normalized[normalizedKey] = source[key]
	}
	return normalized, nil
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

func collectRuntimeHeaderAuditEntries(source map[string]any) []AppliedHeaderAuditEntry {
	headers := normalizeRuntimeHeaderOverrideMap(source)
	keys := collectRuntimeHeaderKeys(headers)
	if len(keys) == 0 {
		return nil
	}

	entries := make([]AppliedHeaderAuditEntry, 0, len(keys))
	for _, key := range keys {
		value, exists := headers[key]
		if !exists {
			continue
		}
		text := strings.TrimSpace(fmt.Sprintf("%v", value))
		if text == "" {
			continue
		}
		entries = append(entries, AppliedHeaderAuditEntry{
			Key:   key,
			Value: RedactHeaderAuditValue(key, text),
		})
	}
	return entries
}

func RedactHeaderAuditValue(key string, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if isSensitiveHeaderAuditValue(value) {
		return AppliedHeaderAuditRedactedValue
	}
	if _, ok := visibleHeaderAuditValueKeys[strings.ToLower(strings.TrimSpace(key))]; ok {
		return value
	}
	return AppliedHeaderAuditRedactedValue
}

func isSensitiveHeaderAuditValue(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	if lower == "" {
		return false
	}
	if strings.HasPrefix(lower, "bearer ") ||
		strings.HasPrefix(lower, "basic ") ||
		strings.HasPrefix(lower, "digest ") ||
		strings.HasPrefix(lower, "apikey ") ||
		strings.HasPrefix(lower, "api-key ") ||
		strings.HasPrefix(lower, "token ") ||
		strings.HasPrefix(lower, "sk-") ||
		strings.HasPrefix(lower, "sk_") {
		return true
	}
	return strings.Contains(lower, "=") && strings.Contains(lower, ";")
}

func finalizeRuntimeHeaderPolicyAudit(audit *RuntimeHeaderPolicyAudit, headers map[string]any) {
	if audit == nil {
		return
	}
	audit.AppliedHeaderKeys = collectRuntimeHeaderKeys(headers)
	audit.AppliedHeaders = collectRuntimeHeaderAuditEntries(headers)
	audit.AppliedUserAgent = getRuntimeHeaderStringValue(headers, "User-Agent")
}

func getRuntimeHeaderStringValue(source map[string]any, target string) string {
	for key, value := range normalizeRuntimeHeaderOverrideMap(source) {
		if isRuntimeHeaderPassthroughRuleKey(key) {
			continue
		}
		if strings.EqualFold(key, target) {
			if value == nil {
				return ""
			}
			return strings.TrimSpace(fmt.Sprintf("%v", value))
		}
	}
	return ""
}
