package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// GetEnvOrDefault treats an unset or unparsable variable as the default, and
// a parsed "0" as 0.
func TestGetEnvOrDefault(t *testing.T) {
	t.Setenv("NEW_API_TEST_INT", "")
	require.Equal(t, 7, GetEnvOrDefault("NEW_API_TEST_INT", 7))

	t.Setenv("NEW_API_TEST_INT", "not-a-number")
	require.Equal(t, 7, GetEnvOrDefault("NEW_API_TEST_INT", 7))

	t.Setenv("NEW_API_TEST_INT", "0")
	require.Equal(t, 0, GetEnvOrDefault("NEW_API_TEST_INT", 7))

	t.Setenv("NEW_API_TEST_INT", "42")
	require.Equal(t, 42, GetEnvOrDefault("NEW_API_TEST_INT", 7))
}

// GetEnvOrDefaultAllowZero keeps the parsed "0" instead of collapsing it into
// the default, which is what lets a limit be switched off with 0.
func TestGetEnvOrDefaultAllowZero(t *testing.T) {
	t.Setenv("NEW_API_TEST_INT_ZERO", "")
	require.Equal(t, 7, GetEnvOrDefaultAllowZero("NEW_API_TEST_INT_ZERO", 7))

	t.Setenv("NEW_API_TEST_INT_ZERO", "not-a-number")
	require.Equal(t, 7, GetEnvOrDefaultAllowZero("NEW_API_TEST_INT_ZERO", 7))

	t.Setenv("NEW_API_TEST_INT_ZERO", "0")
	require.Equal(t, 0, GetEnvOrDefaultAllowZero("NEW_API_TEST_INT_ZERO", 7))

	t.Setenv("NEW_API_TEST_INT_ZERO", "42")
	require.Equal(t, 42, GetEnvOrDefaultAllowZero("NEW_API_TEST_INT_ZERO", 7))
}
