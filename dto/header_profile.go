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
	BuiltinCodexCLIUserAgent  = "codex-tui/0.128.0 (Mac OS 15.7.3; x86_64) ghostty/1.3.1 (codex-tui; 0.128.0)"
	BuiltinCodexCLIOriginator = "codex-tui"
	BuiltinDroidCLIUserAgent  = "factory-cli/0.115.0"
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
	newBuiltinCLIHeaderProfile(
		"claude-code",
		"Claude Code",
		map[string]string{
			"User-Agent": "claude-cli/2.1.126 (external, sdk-cli)",
		},
		"固定请求头静态快照来自本机实抓 Claude Code 2.1.126 `/v1/messages?beta=true` 请求；真实请求还会携带 X-Claude-Code-Session-Id、Anthropic-Version、Anthropic-Beta、X-Stainless-* 等 SDK 头，选择此模板时会自动写入 Claude CLI 请求头透传规则。",
		true,
	),
	newBuiltinCLIHeaderProfile(
		"gemini-cli",
		"Gemini CLI",
		map[string]string{
			"User-Agent": "GeminiCLI/0.40.1/gemini-3.1-pro-preview (darwin; x64; terminal)",
		},
		"固定请求头静态快照来自本机实抓 Gemini CLI 0.40.1 的 streamGenerateContent 请求；真实请求还会携带 x-goog-api-client 等运行时头，选择此模板时会自动写入 Gemini CLI 请求头透传规则。",
		true,
	),
	newBuiltinCLIHeaderProfile(
		"qwen-code",
		"Qwen Code",
		map[string]string{
			"User-Agent": "QwenCode/0.15.6 (darwin; x64)",
		},
		"固定请求头静态快照来自本机 Qwen Code 0.15.6 的 OpenAI-compatible `/chat/completions` 请求；真实请求还会携带已实抓的 x-stainless-* 运行时头，选择此模板时会自动写入 Qwen Code 请求头透传规则。",
		true,
	),
	newBuiltinCLIHeaderProfile(
		"droid",
		"Droid CLI",
		map[string]string{
			"User-Agent": BuiltinDroidCLIUserAgent,
		},
		"固定请求头静态快照来自本机实抓 Droid 0.115.0 的 OpenAI-compatible `/v1/chat/completions` 请求；真实请求还会携带 X-Stainless-* 运行时头，选择此模板时会自动写入 Droid CLI 请求头透传规则。",
		true,
	),
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
		Description:         "固定请求头静态快照来自 Codex CLI 0.128.0 交互式 TUI 请求头生成逻辑；选择此模板时会自动写入 Codex CLI 请求头透传规则，保留真实 CLI 的会话、窗口与 turn metadata 动态头。",
		PassthroughRequired: true,
		Headers: map[string]string{
			"User-Agent": BuiltinCodexCLIUserAgent,
			"Originator": BuiltinCodexCLIOriginator,
		},
	}
}

func newBuiltinCLIHeaderProfile(id string, name string, headers map[string]string, description string, passthroughRequired bool) HeaderProfile {
	return HeaderProfile{
		ID:                  id,
		Name:                name,
		Category:            HeaderProfileCategoryAICodingCLI,
		Scope:               HeaderProfileScopeBuiltin,
		ReadOnly:            true,
		Description:         description,
		PassthroughRequired: passthroughRequired,
		Headers:             headers,
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
