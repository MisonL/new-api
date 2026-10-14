package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A persisted literal "null" is legacy data, not a mapping. SQL NULL and the
// empty string already mean "no mapping" and are accepted; the literal token
// must be treated the same way. Rejecting it silently removed the channel from
// routing, which surfaced to operators as "no available channel for model"
// with no trace of the real cause.
func TestParseChannelModelMappingAcceptsLiteralNull(t *testing.T) {
	for _, mapping := range []string{"", "   ", "null", " null ", "NULL", "Null"} {
		t.Run("mapping="+mapping, func(t *testing.T) {
			parsed, err := parseChannelModelMapping(mapping)
			require.NoError(t, err)
			require.Empty(t, parsed)
		})
	}
}

// The literal null must normalize to the same routing result as an empty
// mapping, so a channel carrying it stays selectable.
func TestResolveChannelModelMappingTreatsLiteralNullAsEmpty(t *testing.T) {
	for _, mapping := range []string{"", "null", " null "} {
		t.Run("mapping="+mapping, func(t *testing.T) {
			modelName, mapped, err := ResolveChannelModelMapping(mapping, "gpt-5.6-luna", false)
			require.NoError(t, err)
			require.Equal(t, "gpt-5.6-luna", modelName)
			require.False(t, mapped)
		})
	}
}

// Anything else that is not a JSON object must still be rejected: the fix is
// specific to the literal null token, not a general loosening of the grammar.
func TestParseChannelModelMappingStillRejectsNonObjectJSON(t *testing.T) {
	for _, mapping := range []string{"[]", `"null"`, "123", "true", `["a","b"]`, "{", "not json"} {
		t.Run("mapping="+mapping, func(t *testing.T) {
			_, err := parseChannelModelMapping(mapping)
			require.Error(t, err)
			require.True(t, IsChannelModelMappingError(err))
		})
	}
}
