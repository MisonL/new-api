package dto

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuiltinCodexCLIHeaderProfileDoesNotUseExecIdentity(t *testing.T) {
	profile, exists := ResolveHeaderProfile("codex-cli", nil)
	require.True(t, exists)
	require.Contains(t, profile.Description, "显式选择")
	require.Equal(t, "codex-tui", profile.Headers["Originator"])
	require.True(t, strings.HasPrefix(profile.Headers["User-Agent"], "codex-tui/"))

	for key, value := range profile.Headers {
		normalizedKey := strings.ToLower(key)
		normalizedValue := strings.ToLower(value)
		require.NotContains(t, normalizedKey, "source")
		require.NotContains(t, normalizedValue, "codex_exec")
		require.NotContains(t, normalizedValue, "source=exec")
	}
}

func TestBuiltinCodexDesktopHeaderProfileUsesDesktopAppIdentity(t *testing.T) {
	profile, exists := ResolveHeaderProfile("codex-desktop", nil)
	require.True(t, exists)
	require.Contains(t, profile.Description, "Codex Desktop")
	require.Equal(t, BuiltinCodexDesktopUserAgent, profile.Headers["User-Agent"])
	require.Equal(t, BuiltinCodexDesktopOriginator, profile.Headers["Originator"])
	require.True(t, strings.HasPrefix(profile.Headers["User-Agent"], "Codex Desktop/"))
	require.NotContains(t, profile.Headers["User-Agent"], "codex-cli@")
	require.NotContains(t, strings.ToLower(profile.Headers["User-Agent"]), "codex_exec")
	require.NotContains(t, strings.ToLower(profile.Headers["User-Agent"]), "codex-tui")
}

func TestBuiltinAICodingCLIHeaderProfilesDoNotRequireAutomaticPassthrough(t *testing.T) {
	for _, profileID := range []string{"codex-cli", "codex-desktop", "claude-code", "gemini-cli", "qwen-code", "droid"} {
		profile, exists := ResolveHeaderProfile(profileID, nil)
		require.True(t, exists, profileID)
		require.False(t, profile.PassthroughRequired, profileID)
		require.Contains(t, profile.Description, "显式选择", profileID)
	}
}

func TestBuiltinAICodingCLIHeaderProfilesDefaultToLatestVersionMeta(t *testing.T) {
	tests := []struct {
		profileID   string
		packageName string
	}{
		{profileID: "codex-cli", packageName: "@openai/codex"},
		{profileID: "claude-code", packageName: "@anthropic-ai/claude-code"},
		{profileID: "gemini-cli", packageName: "@google/gemini-cli"},
		{profileID: "qwen-code", packageName: "@qwen-code/qwen-code"},
		{profileID: "droid", packageName: "droid"},
	}

	for _, test := range tests {
		t.Run(test.profileID, func(t *testing.T) {
			profile, exists := ResolveHeaderProfile(test.profileID, nil)
			require.True(t, exists)
			require.NotNil(t, profile.VersionMeta)
			require.Equal(t, test.profileID, profile.VersionMeta.BaseProfileID)
			require.Equal(t, test.packageName, profile.VersionMeta.PackageName)
			require.Equal(t, "npm", profile.VersionMeta.Source)
			require.Equal(t, HeaderProfileLatestVersion, profile.VersionMeta.Version)
			require.Equal(t, HeaderProfilePlatformMacOSX64, profile.VersionMeta.Platform)
		})
	}

	codexDesktop, exists := ResolveHeaderProfile("codex-desktop", nil)
	require.True(t, exists)
	require.Nil(t, codexDesktop.VersionMeta)
}

func TestResolveHeaderProfileStrategyHeadersResolvesBuiltinLatestProfiles(t *testing.T) {
	latestVersions := map[string]string{
		"@openai/codex":             "0.200.0",
		"@anthropic-ai/claude-code": "2.2.0",
		"@google/gemini-cli":        "0.50.0",
		"@qwen-code/qwen-code":      "0.20.0",
		"droid":                     "0.140.0",
	}
	previousResolver := ResolveNpmCLILatestVersion
	ResolveNpmCLILatestVersion = func(packageName string) (string, bool) {
		version, ok := latestVersions[packageName]
		return version, ok
	}
	defer func() {
		ResolveNpmCLILatestVersion = previousResolver
	}()

	tests := []struct {
		profileID     string
		expectedAgent string
	}{
		{
			profileID:     "codex-cli",
			expectedAgent: "codex-tui/0.200.0 (Mac OS 15.7.3; x86_64) ghostty/1.3.1 (codex-tui; 0.200.0)",
		},
		{
			profileID:     "claude-code",
			expectedAgent: "claude-cli/2.2.0 (external, sdk-cli)",
		},
		{
			profileID:     "gemini-cli",
			expectedAgent: "GeminiCLI/0.50.0/gemini-3.1-pro-preview (darwin; x64; terminal)",
		},
		{
			profileID:     "qwen-code",
			expectedAgent: "QwenCode/0.20.0 (darwin; x64)",
		},
		{
			profileID:     "droid",
			expectedAgent: "factory-cli/0.140.0",
		},
	}

	for _, test := range tests {
		t.Run(test.profileID, func(t *testing.T) {
			headers, profileID, err := ResolveHeaderProfileStrategyHeaders(&HeaderProfileStrategy{
				Enabled:            true,
				Mode:               HeaderProfileModeFixed,
				SelectedProfileIDs: []string{test.profileID},
			}, 0)
			require.NoError(t, err)
			require.Equal(t, test.profileID, profileID)
			require.Equal(t, test.expectedAgent, headers["User-Agent"])
		})
	}
}

func TestBuildAICodingCLIUserAgentSupportsKnownPlatforms(t *testing.T) {
	tests := []struct {
		name          string
		profileID     string
		version       string
		platform      string
		expectedAgent string
	}{
		{
			name:          "codex-linux",
			profileID:     "codex-cli",
			version:       "0.200.0",
			platform:      HeaderProfilePlatformLinuxX64,
			expectedAgent: "codex-tui/0.200.0 (Linux; x86_64) ghostty/1.3.1 (codex-tui; 0.200.0)",
		},
		{
			name:          "codex-macos-arm64",
			profileID:     "codex-cli",
			version:       "0.200.0",
			platform:      HeaderProfilePlatformMacOSArm64,
			expectedAgent: "codex-tui/0.200.0 (Mac OS 15.7.3; aarch64) ghostty/1.3.1 (codex-tui; 0.200.0)",
		},
		{
			name:          "qwen-linux-arm64",
			profileID:     "qwen-code",
			version:       "0.20.0",
			platform:      HeaderProfilePlatformLinuxArm64,
			expectedAgent: "QwenCode/0.20.0 (linux; arm64)",
		},
		{
			name:          "gemini-windows",
			profileID:     "gemini-cli",
			version:       "0.50.0",
			platform:      HeaderProfilePlatformWindowsX64,
			expectedAgent: "GeminiCLI/0.50.0/gemini-3.1-pro-preview (win32; x64; terminal)",
		},
		{
			name:          "gemini-windows-arm64",
			profileID:     "gemini-cli",
			version:       "0.50.0",
			platform:      HeaderProfilePlatformWindowsArm64,
			expectedAgent: "GeminiCLI/0.50.0/gemini-3.1-pro-preview (win32; arm64; terminal)",
		},
		{
			name:          "qwen-invalid-defaults-macos",
			profileID:     "qwen-code",
			version:       "0.20.0",
			platform:      "linux-x64\nInjected",
			expectedAgent: "QwenCode/0.20.0 (darwin; x64)",
		},
		{
			name:          "claude-platform-independent",
			profileID:     "claude-code",
			version:       "2.2.0",
			platform:      HeaderProfilePlatformWindowsX64,
			expectedAgent: "claude-cli/2.2.0 (external, sdk-cli)",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.expectedAgent, buildAICodingCLIUserAgent(test.profileID, test.version, test.platform))
		})
	}
}

func TestResolveHeaderProfileStrategyHeadersResolvesLatestVersionMeta(t *testing.T) {
	previousResolver := ResolveNpmCLILatestVersion
	ResolveNpmCLILatestVersion = func(packageName string) (string, bool) {
		require.Equal(t, "@openai/codex", packageName)
		return "0.200.0", true
	}
	defer func() {
		ResolveNpmCLILatestVersion = previousResolver
	}()

	headers, profileID, err := ResolveHeaderProfileStrategyHeaders(&HeaderProfileStrategy{
		Enabled:            true,
		Mode:               HeaderProfileModeFixed,
		SelectedProfileIDs: []string{"codex-cli@latest"},
		Profiles: []HeaderProfile{
			{
				ID: "codex-cli@latest",
				Headers: map[string]string{
					"User-Agent": "codex-tui/0.134.0 (Mac OS 15.7.3; x86_64) ghostty/1.3.1 (codex-tui; 0.134.0)",
					"Originator": "codex-tui",
				},
				VersionMeta: &HeaderProfileVersionMeta{
					BaseProfileID: "codex-cli",
					PackageName:   "@openai/codex",
					Source:        "npm",
					Version:       HeaderProfileLatestVersion,
					Platform:      HeaderProfilePlatformLinuxX64,
				},
			},
		},
	}, 0)
	require.NoError(t, err)
	require.Equal(t, "codex-cli@latest", profileID)
	require.Equal(t, "codex-tui/0.200.0 (Linux; x86_64) ghostty/1.3.1 (codex-tui; 0.200.0)", headers["User-Agent"])
	require.Equal(t, "codex-tui", headers["Originator"])
}

func TestResolveHeaderProfileStrategyHeadersUsesVersionMetaPlatform(t *testing.T) {
	previousResolver := ResolveNpmCLILatestVersion
	ResolveNpmCLILatestVersion = func(packageName string) (string, bool) {
		require.Equal(t, "@google/gemini-cli", packageName)
		return "0.50.0", true
	}
	defer func() {
		ResolveNpmCLILatestVersion = previousResolver
	}()

	headers, profileID, err := ResolveHeaderProfileStrategyHeaders(&HeaderProfileStrategy{
		Enabled:            true,
		Mode:               HeaderProfileModeFixed,
		SelectedProfileIDs: []string{"gemini-cli@latest"},
		Profiles: []HeaderProfile{
			{
				ID: "gemini-cli@latest",
				Headers: map[string]string{
					"User-Agent": "GeminiCLI/0.44.0/gemini-3.1-pro-preview (darwin; x64; terminal)",
				},
				VersionMeta: &HeaderProfileVersionMeta{
					BaseProfileID: "gemini-cli",
					PackageName:   "@google/gemini-cli",
					Source:        "npm",
					Version:       HeaderProfileLatestVersion,
					Platform:      HeaderProfilePlatformWindowsArm64,
				},
			},
		},
	}, 0)
	require.NoError(t, err)
	require.Equal(t, "gemini-cli@latest", profileID)
	require.Equal(t, "GeminiCLI/0.50.0/gemini-3.1-pro-preview (win32; arm64; terminal)", headers["User-Agent"])
}

func TestResolveHeaderProfileStrategyHeadersResolvesBuiltinLatestWithoutSnapshot(t *testing.T) {
	previousResolver := ResolveNpmCLILatestVersion
	ResolveNpmCLILatestVersion = func(packageName string) (string, bool) {
		require.Equal(t, "@openai/codex", packageName)
		return "0.200.0", true
	}
	defer func() {
		ResolveNpmCLILatestVersion = previousResolver
	}()

	headers, profileID, err := ResolveHeaderProfileStrategyHeaders(&HeaderProfileStrategy{
		Enabled:            true,
		Mode:               HeaderProfileModeFixed,
		SelectedProfileIDs: []string{"codex-cli@latest"},
	}, 0)
	require.NoError(t, err)
	require.Equal(t, "codex-cli@latest", profileID)
	require.Equal(t, "codex-tui/0.200.0 (Mac OS 15.7.3; x86_64) ghostty/1.3.1 (codex-tui; 0.200.0)", headers["User-Agent"])
	require.Equal(t, BuiltinCodexCLIOriginator, headers["Originator"])
}

func TestResolveHeaderProfileStrategyHeadersResolvesBuiltinPinnedVersionWithoutSnapshot(t *testing.T) {
	headers, profileID, err := ResolveHeaderProfileStrategyHeaders(&HeaderProfileStrategy{
		Enabled:            true,
		Mode:               HeaderProfileModeFixed,
		SelectedProfileIDs: []string{"claude-code@2.2.0"},
	}, 0)
	require.NoError(t, err)
	require.Equal(t, "claude-code@2.2.0", profileID)
	require.Equal(t, "claude-cli/2.2.0 (external, sdk-cli)", headers["User-Agent"])
}

func TestResolveHeaderProfileRejectsInvalidPinnedVersion(t *testing.T) {
	_, exists := ResolveHeaderProfile("claude-code@2.2.0\nInjected", nil)
	require.False(t, exists)

	_, exists = ResolveHeaderProfile("claude-code@latest", nil)
	require.True(t, exists)
}

func TestResolveHeaderProfileStrategyHeadersKeepsSnapshotWhenLatestUnavailable(t *testing.T) {
	previousResolver := ResolveNpmCLILatestVersion
	ResolveNpmCLILatestVersion = func(packageName string) (string, bool) {
		return "", false
	}
	defer func() {
		ResolveNpmCLILatestVersion = previousResolver
	}()

	headers, _, err := ResolveHeaderProfileStrategyHeaders(&HeaderProfileStrategy{
		Enabled:            true,
		Mode:               HeaderProfileModeFixed,
		SelectedProfileIDs: []string{"claude-code@latest"},
		Profiles: []HeaderProfile{
			{
				ID: "claude-code@latest",
				Headers: map[string]string{
					"User-Agent": "claude-cli/2.1.153 (external, sdk-cli)",
				},
				VersionMeta: &HeaderProfileVersionMeta{
					BaseProfileID: "claude-code",
					PackageName:   "@anthropic-ai/claude-code",
					Source:        "npm",
					Version:       HeaderProfileLatestVersion,
				},
			},
		},
	}, 0)
	require.NoError(t, err)
	require.Equal(t, "claude-cli/2.1.153 (external, sdk-cli)", headers["User-Agent"])
}

func TestResolveHeaderProfileStrategyHeadersKeepsSnapshotWhenLatestIsInvalid(t *testing.T) {
	previousResolver := ResolveNpmCLILatestVersion
	ResolveNpmCLILatestVersion = func(packageName string) (string, bool) {
		return "2.2.0\nInjected", true
	}
	defer func() {
		ResolveNpmCLILatestVersion = previousResolver
	}()

	headers, _, err := ResolveHeaderProfileStrategyHeaders(&HeaderProfileStrategy{
		Enabled:            true,
		Mode:               HeaderProfileModeFixed,
		SelectedProfileIDs: []string{"claude-code@latest"},
		Profiles: []HeaderProfile{
			{
				ID: "claude-code@latest",
				Headers: map[string]string{
					"User-Agent": "claude-cli/2.1.153 (external, sdk-cli)",
				},
				VersionMeta: &HeaderProfileVersionMeta{
					BaseProfileID: "claude-code",
					PackageName:   "@anthropic-ai/claude-code",
					Source:        "npm",
					Version:       HeaderProfileLatestVersion,
				},
			},
		},
	}, 0)
	require.NoError(t, err)
	require.Equal(t, "claude-cli/2.1.153 (external, sdk-cli)", headers["User-Agent"])
}

func TestResolveHeaderProfileStrategyHeadersTreatsLegacyFallbackLatestAsDynamic(t *testing.T) {
	previousResolver := ResolveNpmCLILatestVersion
	ResolveNpmCLILatestVersion = func(packageName string) (string, bool) {
		require.Equal(t, "@anthropic-ai/claude-code", packageName)
		return "2.2.0", true
	}
	defer func() {
		ResolveNpmCLILatestVersion = previousResolver
	}()

	headers, _, err := ResolveHeaderProfileStrategyHeaders(&HeaderProfileStrategy{
		Enabled:            true,
		Mode:               HeaderProfileModeFixed,
		SelectedProfileIDs: []string{"claude-code@latest"},
		Profiles: []HeaderProfile{
			{
				ID: "claude-code@latest",
				Headers: map[string]string{
					"User-Agent": "claude-cli/2.1.153 (external, sdk-cli)",
				},
				VersionMeta: &HeaderProfileVersionMeta{
					BaseProfileID: "claude-code",
					PackageName:   "@anthropic-ai/claude-code",
					Source:        "fallback",
					Version:       HeaderProfileLatestVersion,
				},
			},
		},
	}, 0)
	require.NoError(t, err)
	require.Equal(t, "claude-cli/2.2.0 (external, sdk-cli)", headers["User-Agent"])
}

func TestResolveHeaderProfileStrategyHeadersResolvesLatestWithoutPackageName(t *testing.T) {
	previousResolver := ResolveNpmCLILatestVersion
	ResolveNpmCLILatestVersion = func(packageName string) (string, bool) {
		require.Equal(t, "@google/gemini-cli", packageName)
		return "0.50.0", true
	}
	defer func() {
		ResolveNpmCLILatestVersion = previousResolver
	}()

	headers, _, err := ResolveHeaderProfileStrategyHeaders(&HeaderProfileStrategy{
		Enabled:            true,
		Mode:               HeaderProfileModeFixed,
		SelectedProfileIDs: []string{"gemini-cli@latest"},
		Profiles: []HeaderProfile{
			{
				ID: "gemini-cli@latest",
				Headers: map[string]string{
					"User-Agent": "GeminiCLI/0.44.0/gemini-3.1-pro-preview (darwin; x64; terminal)",
				},
				VersionMeta: &HeaderProfileVersionMeta{
					BaseProfileID: "gemini-cli",
					Source:        "npm",
					Version:       HeaderProfileLatestVersion,
				},
			},
		},
	}, 0)
	require.NoError(t, err)
	require.Equal(t, "GeminiCLI/0.50.0/gemini-3.1-pro-preview (darwin; x64; terminal)", headers["User-Agent"])
}

func TestResolveHeaderProfileStrategyHeadersResolvesLatestWithOnlyVersionMeta(t *testing.T) {
	previousResolver := ResolveNpmCLILatestVersion
	ResolveNpmCLILatestVersion = func(packageName string) (string, bool) {
		require.Equal(t, "@qwen-code/qwen-code", packageName)
		return "0.20.0", true
	}
	defer func() {
		ResolveNpmCLILatestVersion = previousResolver
	}()

	headers, _, err := ResolveHeaderProfileStrategyHeaders(&HeaderProfileStrategy{
		Enabled:            true,
		Mode:               HeaderProfileModeFixed,
		SelectedProfileIDs: []string{"qwen-code@latest"},
		Profiles: []HeaderProfile{
			{
				ID: "qwen-code@latest",
				Headers: map[string]string{
					"User-Agent": "QwenCode/0.16.2 (darwin; x64)",
				},
				VersionMeta: &HeaderProfileVersionMeta{
					Source:  "npm",
					Version: HeaderProfileLatestVersion,
				},
			},
		},
	}, 0)
	require.NoError(t, err)
	require.Equal(t, "QwenCode/0.20.0 (darwin; x64)", headers["User-Agent"])
}

func TestResolveHeaderProfileStrategyHeadersResolvesLatestWithoutBaseProfileID(t *testing.T) {
	previousResolver := ResolveNpmCLILatestVersion
	ResolveNpmCLILatestVersion = func(packageName string) (string, bool) {
		require.Equal(t, "@anthropic-ai/claude-code", packageName)
		return "2.2.0", true
	}
	defer func() {
		ResolveNpmCLILatestVersion = previousResolver
	}()

	headers, _, err := ResolveHeaderProfileStrategyHeaders(&HeaderProfileStrategy{
		Enabled:            true,
		Mode:               HeaderProfileModeFixed,
		SelectedProfileIDs: []string{"claude-code@latest"},
		Profiles: []HeaderProfile{
			{
				ID: "claude-code@latest",
				Headers: map[string]string{
					"User-Agent": "claude-cli/2.1.153 (external, sdk-cli)",
				},
				VersionMeta: &HeaderProfileVersionMeta{
					PackageName: "@anthropic-ai/claude-code",
					Source:      "npm",
					Version:     HeaderProfileLatestVersion,
				},
			},
		},
	}, 0)
	require.NoError(t, err)
	require.Equal(t, "claude-cli/2.2.0 (external, sdk-cli)", headers["User-Agent"])
}
