package dto

import (
	"fmt"
	"strings"
)

const (
	HeaderProfilePlatformMacOSArm64   = "macos-arm64"
	HeaderProfilePlatformLinuxX64     = "linux-x64"
	HeaderProfilePlatformLinuxArm64   = "linux-arm64"
	HeaderProfilePlatformWindowsX64   = "windows-x64"
	HeaderProfilePlatformWindowsArm64 = "windows-arm64"
)

type aiCodingCLIPlatformTokens struct {
	CodexOS       string
	CodexArch     string
	GeminiOS      string
	GeminiArch    string
	QwenOS        string
	QwenArch      string
	DisplaySuffix string
}

var aiCodingCLIPlatformTokenMap = map[string]aiCodingCLIPlatformTokens{
	HeaderProfilePlatformMacOSX64: {
		CodexOS:       "Mac OS 15.7.3",
		CodexArch:     "x86_64",
		GeminiOS:      "darwin",
		GeminiArch:    "x64",
		QwenOS:        "darwin",
		QwenArch:      "x64",
		DisplaySuffix: "macOS x64",
	},
	HeaderProfilePlatformMacOSArm64: {
		CodexOS:       "Mac OS 15.7.3",
		CodexArch:     "aarch64",
		GeminiOS:      "darwin",
		GeminiArch:    "arm64",
		QwenOS:        "darwin",
		QwenArch:      "arm64",
		DisplaySuffix: "macOS arm64",
	},
	HeaderProfilePlatformLinuxX64: {
		CodexOS:       "Linux",
		CodexArch:     "x86_64",
		GeminiOS:      "linux",
		GeminiArch:    "x64",
		QwenOS:        "linux",
		QwenArch:      "x64",
		DisplaySuffix: "Linux x64",
	},
	HeaderProfilePlatformLinuxArm64: {
		CodexOS:       "Linux",
		CodexArch:     "aarch64",
		GeminiOS:      "linux",
		GeminiArch:    "arm64",
		QwenOS:        "linux",
		QwenArch:      "arm64",
		DisplaySuffix: "Linux arm64",
	},
	HeaderProfilePlatformWindowsX64: {
		CodexOS:       "Windows NT 10.0",
		CodexArch:     "x86_64",
		GeminiOS:      "win32",
		GeminiArch:    "x64",
		QwenOS:        "win32",
		QwenArch:      "x64",
		DisplaySuffix: "Windows x64",
	},
	HeaderProfilePlatformWindowsArm64: {
		CodexOS:       "Windows NT 10.0",
		CodexArch:     "aarch64",
		GeminiOS:      "win32",
		GeminiArch:    "arm64",
		QwenOS:        "win32",
		QwenArch:      "arm64",
		DisplaySuffix: "Windows arm64",
	},
}

func ValidateHeaderProfileVersionMeta(profile HeaderProfile) error {
	if profile.VersionMeta != nil && !IsSupportedHeaderProfilePlatform(profile.VersionMeta.Platform) {
		return fmt.Errorf("version_meta.platform invalid: %s", profile.VersionMeta.Platform)
	}
	meta := resolveHeaderProfileVersionMeta(profile)
	if meta == nil {
		return nil
	}
	baseProfileID := strings.TrimSpace(meta.BaseProfileID)
	packageName := strings.TrimSpace(meta.PackageName)
	version := normalizeHeaderProfileVersionValue(meta.Version, true)
	if baseProfileID == "" {
		return fmt.Errorf("version_meta.base_profile_id is required")
	}
	expectedPackageName, knownBaseProfile := builtinAICodingCLIPackageNames[baseProfileID]
	if !knownBaseProfile {
		return fmt.Errorf("version_meta.base_profile_id invalid: %s", baseProfileID)
	}
	profileIDBase := headerProfileIDBase(profile.ID)
	if _, builtinProfileID := builtinAICodingCLIPackageNames[profileIDBase]; builtinProfileID && profileIDBase != baseProfileID {
		return fmt.Errorf("version_meta.base_profile_id does not match profile id")
	}
	if packageName == "" {
		return fmt.Errorf("version_meta.package_name is required")
	}
	if packageName != expectedPackageName {
		return fmt.Errorf("version_meta.package_name does not match %s", baseProfileID)
	}
	if strings.TrimSpace(meta.Source) != "" && strings.TrimSpace(meta.Source) != "npm" && strings.TrimSpace(meta.Source) != "fallback" {
		return fmt.Errorf("version_meta.source invalid: %s", meta.Source)
	}
	if version == "" {
		return fmt.Errorf("version_meta.version invalid")
	}
	if !IsSupportedHeaderProfilePlatform(meta.Platform) {
		return fmt.Errorf("version_meta.platform invalid: %s", meta.Platform)
	}
	return nil
}

func headerProfileIDBase(profileID string) string {
	trimmedID := strings.TrimSpace(profileID)
	if baseProfileID, _, ok := strings.Cut(trimmedID, "@"); ok {
		return strings.TrimSpace(baseProfileID)
	}
	return trimmedID
}

func IsSupportedHeaderProfilePlatform(platform string) bool {
	normalizedPlatform := strings.TrimSpace(platform)
	if normalizedPlatform == "" {
		return true
	}
	_, exists := aiCodingCLIPlatformTokenMap[normalizedPlatform]
	return exists
}

func resolveLatestHeaderProfileHeaders(profile HeaderProfile, headers map[string]string) {
	meta := resolveHeaderProfileVersionMeta(profile)
	if meta == nil || headers == nil || ResolveNpmCLILatestVersion == nil {
		return
	}
	if strings.TrimSpace(meta.Version) != HeaderProfileLatestVersion {
		return
	}
	packageName := resolveAICodingCLIPackageName(meta)
	latestVersion, ok := ResolveNpmCLILatestVersion(packageName)
	normalizedLatestVersion := normalizeHeaderProfileVersionValue(latestVersion, false)
	if !ok || normalizedLatestVersion == "" {
		return
	}
	baseProfileID := resolveAICodingCLIBaseProfileID(profile, meta)
	userAgent := buildAICodingCLIUserAgent(baseProfileID, normalizedLatestVersion, meta.Platform)
	if userAgent == "" {
		return
	}
	headers["User-Agent"] = userAgent
}

func resolveHeaderProfileVersionMeta(profile HeaderProfile) *HeaderProfileVersionMeta {
	if profile.VersionMeta != nil {
		meta := *profile.VersionMeta
		enrichHeaderProfileVersionMeta(&meta, profile)
		return &meta
	}
	profileID := strings.TrimSpace(profile.ID)
	if meta := newBuiltinNpmHeaderProfileVersionMeta(profileID); meta != nil {
		return meta
	}
	baseProfileID, version, ok := strings.Cut(profileID, "@")
	if !ok {
		return nil
	}
	meta := newBuiltinNpmHeaderProfileVersionMeta(baseProfileID)
	if meta == nil {
		return nil
	}
	meta.Version = strings.TrimSpace(version)
	return meta
}

func enrichHeaderProfileVersionMeta(meta *HeaderProfileVersionMeta, profile HeaderProfile) {
	if meta == nil {
		return
	}
	if strings.TrimSpace(meta.BaseProfileID) == "" {
		meta.BaseProfileID = resolveAICodingCLIBaseProfileID(profile, meta)
	}
	if strings.TrimSpace(meta.PackageName) == "" {
		meta.PackageName = builtinAICodingCLIPackageNames[strings.TrimSpace(meta.BaseProfileID)]
	}
	meta.Platform = normalizeHeaderProfilePlatform(meta.Platform)
}

func resolveAICodingCLIPackageName(meta *HeaderProfileVersionMeta) string {
	if meta == nil {
		return ""
	}
	packageName := strings.TrimSpace(meta.PackageName)
	if packageName != "" {
		return packageName
	}
	return builtinAICodingCLIPackageNames[strings.TrimSpace(meta.BaseProfileID)]
}

func resolveAICodingCLIBaseProfileID(profile HeaderProfile, meta *HeaderProfileVersionMeta) string {
	if meta != nil {
		if baseProfileID := strings.TrimSpace(meta.BaseProfileID); baseProfileID != "" {
			return baseProfileID
		}
		for profileID, packageName := range builtinAICodingCLIPackageNames {
			if strings.TrimSpace(meta.PackageName) == packageName {
				return profileID
			}
		}
	}
	profileID := strings.TrimSpace(profile.ID)
	if baseProfileID, _, ok := strings.Cut(profileID, "@"); ok {
		return strings.TrimSpace(baseProfileID)
	}
	return profileID
}

func buildAICodingCLIUserAgent(profileID string, version string, platform string) string {
	profileID = strings.TrimSpace(profileID)
	version = normalizeHeaderProfileVersionValue(version, false)
	if profileID == "" || version == "" {
		return ""
	}
	platformTokens := aiCodingCLIPlatformTokensFor(platform)
	switch profileID {
	case "codex-cli":
		return fmt.Sprintf("codex-tui/%s (%s; %s) ghostty/1.3.1 (codex-tui; %s)", version, platformTokens.CodexOS, platformTokens.CodexArch, version)
	case "claude-code":
		return fmt.Sprintf("claude-cli/%s (external, sdk-cli)", version)
	case "gemini-cli":
		return fmt.Sprintf("GeminiCLI/%s/gemini-3.1-pro-preview (%s; %s; terminal)", version, platformTokens.GeminiOS, platformTokens.GeminiArch)
	case "qwen-code":
		return fmt.Sprintf("QwenCode/%s (%s; %s)", version, platformTokens.QwenOS, platformTokens.QwenArch)
	case "droid":
		return fmt.Sprintf("factory-cli/%s", version)
	default:
		return ""
	}
}

func aiCodingCLIPlatformTokensFor(platform string) aiCodingCLIPlatformTokens {
	normalizedPlatform := normalizeHeaderProfilePlatform(platform)
	if tokens, ok := aiCodingCLIPlatformTokenMap[normalizedPlatform]; ok {
		return tokens
	}
	return aiCodingCLIPlatformTokenMap[HeaderProfilePlatformMacOSX64]
}

func normalizeHeaderProfilePlatform(platform string) string {
	normalizedPlatform := strings.TrimSpace(platform)
	if normalizedPlatform == "" {
		return HeaderProfilePlatformMacOSX64
	}
	if _, exists := aiCodingCLIPlatformTokenMap[normalizedPlatform]; exists {
		return normalizedPlatform
	}
	return HeaderProfilePlatformMacOSX64
}

func normalizeHeaderProfileVersionValue(version string, allowLatest bool) string {
	normalizedVersion := strings.TrimSpace(version)
	if allowLatest && normalizedVersion == HeaderProfileLatestVersion {
		return normalizedVersion
	}
	if normalizedVersion == "" ||
		normalizedVersion == HeaderProfileLatestVersion ||
		len(normalizedVersion) > 64 {
		return ""
	}
	for index, char := range normalizedVersion {
		if index == 0 && (char < '0' || char > '9') {
			return ""
		}
		if char >= '0' && char <= '9' {
			continue
		}
		if char >= 'A' && char <= 'Z' {
			continue
		}
		if char >= 'a' && char <= 'z' {
			continue
		}
		if char == '.' || char == '-' || char == '+' {
			continue
		}
		return ""
	}
	return normalizedVersion
}
