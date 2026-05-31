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
