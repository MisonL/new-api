package middleware

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSpecificChannelIDAcceptsInternalRepresentations(t *testing.T) {
	for _, value := range []any{"201", 201, int64(201), uint(201)} {
		id, err := parseSpecificChannelID(value)
		require.NoError(t, err, value)
		require.Equal(t, 201, id, value)
	}
}

func TestParseSpecificChannelIDRejectsMalformedContextValues(t *testing.T) {
	for _, value := range []any{nil, true, "", "not-an-id", 0, -1, uint64(^uint64(0))} {
		id, err := parseSpecificChannelID(value)
		require.Error(t, err, value)
		require.Zero(t, id, value)
	}
}
