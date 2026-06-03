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
	ID                  string                    `json:"id,omitempty"`
	Name                string                    `json:"name,omitempty"`
	Category            HeaderProfileCategory     `json:"category,omitempty"`
	Scope               HeaderProfileScope        `json:"scope,omitempty"`
	Headers             map[string]string         `json:"headers,omitempty"`
	ReadOnly            bool                      `json:"readonly,omitempty"`
	Description         string                    `json:"description,omitempty"`
	PassthroughRequired bool                      `json:"passthrough_required,omitempty"`
	VersionMeta         *HeaderProfileVersionMeta `json:"version_meta,omitempty"`
}

type HeaderProfileVersionMeta struct {
	BaseProfileID string `json:"base_profile_id,omitempty"`
	PackageName   string `json:"package_name,omitempty"`
	Source        string `json:"source,omitempty"`
	Version       string `json:"version,omitempty"`
	Platform      string `json:"platform,omitempty"`
}

type HeaderProfileStrategy struct {
	Enabled            bool              `json:"enabled,omitempty"`
	Mode               HeaderProfileMode `json:"mode,omitempty"`
	SelectedProfileIDs []string          `json:"selected_profile_ids,omitempty"`
	Profiles           []HeaderProfile   `json:"profiles,omitempty"`
}

const (
	BuiltinCodexCLIUserAgent      = "codex-tui/0.134.0 (Mac OS 15.7.3; x86_64) ghostty/1.3.1 (codex-tui; 0.134.0)"
	BuiltinCodexCLIOriginator     = "codex-tui"
	BuiltinCodexDesktopUserAgent  = "Codex Desktop/0.133.0-alpha.1 (Mac OS 15.7.3; x86_64) unknown (Codex Desktop; 26.519.41501)"
	BuiltinCodexDesktopOriginator = "Codex Desktop"
	BuiltinDroidCLIUserAgent      = "factory-cli/0.135.0"
	HeaderProfileLatestVersion    = "latest"
	HeaderProfilePlatformMacOSX64 = "macos-x64"
)

var ResolveNpmCLILatestVersion func(packageName string) (string, bool)

var builtinAICodingCLIPackageNames = map[string]string{
	"codex-cli":   "@openai/codex",
	"claude-code": "@anthropic-ai/claude-code",
	"gemini-cli":  "@google/gemini-cli",
	"qwen-code":   "@qwen-code/qwen-code",
	"droid":       "droid",
}

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
			"Sec-CH-UA":          "\"Google Chrome\";v=\"148\", \"Chromium\";v=\"148\", \"Not.A/Brand\";v=\"24\"",
			"Sec-CH-UA-Mobile":   "?0",
			"Sec-CH-UA-Platform": "\"macOS\"",
			"User-Agent":         "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36",
		},
	},
	newBuiltinCodexCLIHeaderProfile(),
	newBuiltinCodexDesktopHeaderProfile(),
	newBuiltinCLIHeaderProfile(
		"claude-code",
		"Claude Code",
		map[string]string{
			"User-Agent": "claude-cli/2.1.153 (external, sdk-cli)",
		},
		"默认使用 Claude Code npm latest 版本套用既有客户端 UA 格式；清单暂不可用时保留内置快照。此模板仅固定客户端身份。X-Claude-Code-Session-Id、Anthropic-Version、Anthropic-Beta、X-Stainless-* 等动态头需在高级参数覆盖中显式选择 Claude CLI 请求头透传模板。",
		false,
	),
	newBuiltinCLIHeaderProfile(
		"gemini-cli",
		"Gemini CLI",
		map[string]string{
			"User-Agent": "GeminiCLI/0.44.0/gemini-3.1-pro-preview (darwin; x64; terminal)",
		},
		"默认使用 Gemini CLI npm latest 版本套用既有客户端 UA 格式；清单暂不可用时保留内置快照。此模板仅固定客户端身份。x-goog-api-client 等动态头需在高级参数覆盖中显式选择 Gemini CLI 请求头透传模板。",
		false,
	),
	newBuiltinCLIHeaderProfile(
		"qwen-code",
		"Qwen Code",
		map[string]string{
			"User-Agent": "QwenCode/0.16.2 (darwin; x64)",
		},
		"默认使用 Qwen Code npm latest 版本套用既有客户端 UA 格式；清单暂不可用时保留内置快照。此模板仅固定客户端身份。x-stainless-* 动态头需在高级参数覆盖中显式选择 Qwen Code 请求头透传模板。",
		false,
	),
	newBuiltinCLIHeaderProfile(
		"droid",
		"Droid CLI",
		map[string]string{
			"User-Agent": BuiltinDroidCLIUserAgent,
		},
		"默认使用 Droid npm latest 版本套用既有客户端 UA 格式；清单暂不可用时保留内置快照。此模板仅固定客户端身份。X-Stainless-* 动态头需在高级参数覆盖中显式选择 Droid CLI 请求头透传模板。",
		false,
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
			"User-Agent":    "PostmanRuntime/7.54.0",
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
		Description:         "默认使用 Codex CLI npm latest 版本套用交互式 TUI 请求头生成逻辑；清单暂不可用时保留内置快照。此模板仅固定客户端身份。会话、窗口与 turn metadata 动态头需在高级参数覆盖中显式选择 Codex CLI 请求头透传模板。",
		PassthroughRequired: false,
		Headers: map[string]string{
			"User-Agent": BuiltinCodexCLIUserAgent,
			"Originator": BuiltinCodexCLIOriginator,
		},
		VersionMeta: newBuiltinNpmHeaderProfileVersionMeta("codex-cli"),
	}
}

func newBuiltinCodexDesktopHeaderProfile() HeaderProfile {
	return HeaderProfile{
		ID:                  "codex-desktop",
		Name:                "Codex Desktop",
		Category:            HeaderProfileCategoryAICodingCLI,
		Scope:               HeaderProfileScopeBuiltin,
		ReadOnly:            true,
		Description:         "固定请求头静态快照来自 Codex Desktop App 0.133.0-alpha.1 真实请求；此模板仅固定 Codex App 客户端身份，不能与 codex-tui 混用。会话、窗口与 turn metadata 动态头需在高级参数覆盖中显式选择 Codex Desktop 请求头透传模板。",
		PassthroughRequired: false,
		Headers: map[string]string{
			"User-Agent": BuiltinCodexDesktopUserAgent,
			"Originator": BuiltinCodexDesktopOriginator,
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
		VersionMeta:         newBuiltinNpmHeaderProfileVersionMeta(id),
	}
}

func newBuiltinNpmHeaderProfileVersionMeta(baseProfileID string) *HeaderProfileVersionMeta {
	packageName := builtinAICodingCLIPackageNames[strings.TrimSpace(baseProfileID)]
	if packageName == "" {
		return nil
	}
	return &HeaderProfileVersionMeta{
		BaseProfileID: strings.TrimSpace(baseProfileID),
		PackageName:   packageName,
		Source:        "npm",
		Version:       HeaderProfileLatestVersion,
		Platform:      HeaderProfilePlatformMacOSX64,
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
	if baseProfileID, version, ok := strings.Cut(profileID, "@"); ok {
		normalizedBaseProfileID := strings.TrimSpace(baseProfileID)
		for _, profile := range BuiltinHeaderProfiles {
			if profile.ID != normalizedBaseProfileID {
				continue
			}
			meta := newBuiltinNpmHeaderProfileVersionMeta(profile.ID)
			normalizedVersion := normalizeHeaderProfileVersionValue(version, true)
			if meta == nil || normalizedVersion == "" {
				return HeaderProfile{}, false
			}
			profile.ID = normalizedBaseProfileID + "@" + normalizedVersion
			profile.Name = strings.TrimSpace(profile.Name + " " + normalizedVersion)
			profile.VersionMeta = meta
			profile.VersionMeta.Version = normalizedVersion
			profile.VersionMeta.Platform = normalizeHeaderProfilePlatform(profile.VersionMeta.Platform)
			if normalizedVersion != HeaderProfileLatestVersion {
				if userAgent := buildAICodingCLIUserAgent(normalizedBaseProfileID, normalizedVersion, profile.VersionMeta.Platform); userAgent != "" {
					profile.Headers = copyHeaderProfileHeaders(profile.Headers)
					profile.Headers["User-Agent"] = userAgent
				}
			}
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
	headers := copyHeaderProfileHeaders(profile.Headers)
	resolveLatestHeaderProfileHeaders(profile, headers)
	return headers, profile.ID, nil
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
