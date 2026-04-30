package dto

import (
	"fmt"
	"math/rand"
	"strings"
)

type HeaderProfileScope string

const (
	HeaderProfileScopeBuiltin HeaderProfileScope = "builtin"
	HeaderProfileScopeUser    HeaderProfileScope = "user"
)

type HeaderProfileCategory string

const (
	HeaderProfileCategoryBrowser     HeaderProfileCategory = "browser"
	HeaderProfileCategoryAICodingCLI HeaderProfileCategory = "ai_coding_cli"
	HeaderProfileCategoryAPISDK      HeaderProfileCategory = "api_sdk"
	HeaderProfileCategoryCustom      HeaderProfileCategory = "custom"
)

type HeaderProfileMode string

const (
	HeaderProfileModeFixed      HeaderProfileMode = "fixed"
	HeaderProfileModeRoundRobin HeaderProfileMode = "round_robin"
	HeaderProfileModeRandom     HeaderProfileMode = "random"
)

type HeaderProfile struct {
	ID                  string                `json:"id,omitempty"`
	Name                string                `json:"name,omitempty"`
	Category            HeaderProfileCategory `json:"category,omitempty"`
	Scope               HeaderProfileScope    `json:"scope,omitempty"`
	Headers             map[string]string     `json:"headers,omitempty"`
	ReadOnly            bool                  `json:"readonly,omitempty"`
	Description         string                `json:"description,omitempty"`
	PassthroughRequired bool                  `json:"passthrough_required,omitempty"`
}

type HeaderProfileStrategy struct {
	Enabled            bool              `json:"enabled,omitempty"`
	Mode               HeaderProfileMode `json:"mode,omitempty"`
	SelectedProfileIDs []string          `json:"selected_profile_ids,omitempty"`
	Profiles           []HeaderProfile   `json:"profiles,omitempty"`
}

const (
	BuiltinCodexCLIUserAgent  = "codex_exec/0.125.0 (Mac OS 15.7.3; x86_64) ghostty/1.3.1 (codex_exec; 0.125.0)"
	BuiltinCodexCLIOriginator = "codex_exec"
)

var BuiltinHeaderProfiles = []HeaderProfile{
	{
		ID:       "chrome-macos",
		Name:     "Chrome macOS",
		Category: HeaderProfileCategoryBrowser,
		Scope:    HeaderProfileScopeBuiltin,
		ReadOnly: true,
		Headers: map[string]string{
			"Accept":             "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
			"Accept-Language":    "en-US,en;q=0.9",
			"Sec-CH-UA":          "\"Google Chrome\";v=\"135\", \"Chromium\";v=\"135\", \"Not.A/Brand\";v=\"24\"",
			"Sec-CH-UA-Mobile":   "?0",
			"Sec-CH-UA-Platform": "\"macOS\"",
			"User-Agent":         "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_4) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36",
		},
	},
	newBuiltinCodexCLIHeaderProfile(),
	newBuiltinAICodingCLIHeaderProfile("claude-code", "Claude Code", "Claude-Code/1.0", "claude-code", "固定请求头只用于普通渠道标识；Claude 官方客户端链路如需保留会话与 SDK 元数据，还必须在高级参数覆盖中启用 Claude CLI 请求头透传模板。", true),
	newBuiltinAICodingCLIHeaderProfile("gemini-cli", "Gemini CLI", "GeminiCLI/1.0", "gemini-cli", "固定请求头用于普通渠道标识；若上游要求真实客户端会话头，应在高级参数覆盖中额外配置 pass_headers。", false),
	newBuiltinAICodingCLIHeaderProfile("qwen-code", "Qwen Code", "Qwen-Code/1.0", "qwen-code", "固定请求头用于普通渠道标识；不能替代真实 CLI 请求中携带的动态会话头。", false),
	newBuiltinAICodingCLIHeaderProfile("opencode", "OpenCode", "OpenCode/1.0", "opencode", "固定请求头用于普通渠道标识；不能替代真实客户端动态请求头。", false),
	newBuiltinAICodingCLIHeaderProfile("droid", "Droid", "Droid/1.0", "droid", "固定请求头用于普通渠道标识；不能替代真实客户端动态请求头。", false),
	newBuiltinAICodingCLIHeaderProfile("amp", "Amp", "AmpCLI/1.0", "amp", "固定请求头用于普通渠道标识；不能替代真实客户端动态请求头。", false),
	{
		ID:       "postman-runtime",
		Name:     "Postman Runtime",
		Category: HeaderProfileCategoryAPISDK,
		Scope:    HeaderProfileScopeBuiltin,
		ReadOnly: true,
		Headers: map[string]string{
			"Accept":        "*/*",
			"Cache-Control": "no-cache",
			"Postman-Token": "00000000-0000-0000-0000-000000000000",
			"User-Agent":    "PostmanRuntime/7.43.0",
		},
	},
}

func newBuiltinCodexCLIHeaderProfile() HeaderProfile {
	return HeaderProfile{
		ID:                  "codex-cli",
		Name:                "Codex CLI",
		Category:            HeaderProfileCategoryAICodingCLI,
		Scope:               HeaderProfileScopeBuiltin,
		ReadOnly:            true,
		Description:         "固定请求头是 Codex CLI 0.125.0 的静态快照；真实 CLI 会携带会话与窗口动态头，需在高级参数覆盖中启用 Codex CLI 请求头透传模板。",
		PassthroughRequired: true,
		Headers: map[string]string{
			"User-Agent": BuiltinCodexCLIUserAgent,
			"Originator": BuiltinCodexCLIOriginator,
		},
	}
}

func newBuiltinAICodingCLIHeaderProfile(id string, name string, userAgent string, clientName string, description string, passthroughRequired bool) HeaderProfile {
	return HeaderProfile{
		ID:                  id,
		Name:                name,
		Category:            HeaderProfileCategoryAICodingCLI,
		Scope:               HeaderProfileScopeBuiltin,
		ReadOnly:            true,
		Description:         description,
		PassthroughRequired: passthroughRequired,
		Headers: map[string]string{
			"User-Agent":        userAgent,
			"X-Client-Name":     clientName,
			"X-Client-Platform": "terminal",
		},
	}
}

func ResolveHeaderProfile(profileID string, profiles []HeaderProfile) (HeaderProfile, bool) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return HeaderProfile{}, false
	}
	for _, profile := range profiles {
		if strings.TrimSpace(profile.ID) == profileID {
			return profile, true
		}
	}
	for _, profile := range BuiltinHeaderProfiles {
		if profile.ID == profileID {
			return profile, true
		}
	}
	return HeaderProfile{}, false
}

func ResolveHeaderProfileStrategyHeaders(strategy *HeaderProfileStrategy, requestIndex int) (map[string]string, string, error) {
	if strategy == nil || !strategy.Enabled {
		return nil, "", nil
	}
	profileID, err := selectHeaderProfileID(strategy, requestIndex)
	if err != nil {
		return nil, "", err
	}
	profile, exists := ResolveHeaderProfile(profileID, strategy.Profiles)
	if !exists {
		return nil, "", fmt.Errorf("header_profile_strategy selected profile not found: %s", profileID)
	}
	if len(profile.Headers) == 0 {
		return nil, "", fmt.Errorf("header_profile_strategy selected profile has empty headers: %s", profileID)
	}
	return copyHeaderProfileHeaders(profile.Headers), profile.ID, nil
}

func selectHeaderProfileID(strategy *HeaderProfileStrategy, requestIndex int) (string, error) {
	selectedIDs := normalizeHeaderProfileIDs(strategy.SelectedProfileIDs)
	switch strategy.Mode {
	case "", HeaderProfileModeFixed:
		if len(selectedIDs) != 1 {
			return "", fmt.Errorf("header_profile_strategy mode=fixed requires exactly 1 selected_profile_ids")
		}
		return selectedIDs[0], nil
	case HeaderProfileModeRoundRobin:
		if len(selectedIDs) == 0 {
			return "", fmt.Errorf("header_profile_strategy mode=round_robin requires selected_profile_ids")
		}
		if requestIndex < 0 {
			requestIndex = 0
		}
		return selectedIDs[requestIndex%len(selectedIDs)], nil
	case HeaderProfileModeRandom:
		if len(selectedIDs) == 0 {
			return "", fmt.Errorf("header_profile_strategy mode=random requires selected_profile_ids")
		}
		return selectedIDs[rand.Intn(len(selectedIDs))], nil
	default:
		return "", fmt.Errorf("header_profile_strategy mode invalid: %s", strategy.Mode)
	}
}

func normalizeHeaderProfileIDs(profileIDs []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(profileIDs))
	for _, profileID := range profileIDs {
		trimmed := strings.TrimSpace(profileID)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func copyHeaderProfileHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	copied := make(map[string]string, len(headers))
	for key, value := range headers {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		copied[trimmedKey] = value
	}
	return copied
}
