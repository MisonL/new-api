package dto

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuiltinCodexCLIHeaderProfileDoesNotUseExecIdentity(t *testing.T) {
	profile, exists := ResolveHeaderProfile("codex-cli", nil)
	require.True(t, exists)
	require.True(t, profile.PassthroughRequired)
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
