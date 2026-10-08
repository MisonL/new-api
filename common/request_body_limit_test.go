package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

// The anonymous request-body ceiling is read from
// ANONYMOUS_REQUEST_BODY_LIMIT_KB at startup: a positive value is used as-is,
// zero keeps the limit disabled, and a negative value falls back to the 512KB
// default.
func TestGetAnonymousRequestBodyLimitBytes(t *testing.T) {
	original := constant.AnonymousRequestBodyLimitKB
	t.Cleanup(func() { constant.AnonymousRequestBodyLimitKB = original })

	const defaultBytes int64 = 512 << 10
	cases := []struct {
		kb   int
		want int64
	}{
		{512, defaultBytes},
		{1024, 1024 << 10},
		{1, 1 << 10},
		{0, 0},
		{-1, defaultBytes},
	}
	for _, tc := range cases {
		constant.AnonymousRequestBodyLimitKB = tc.kb
		require.Equal(t, tc.want, GetAnonymousRequestBodyLimitBytes(), "kb=%d", tc.kb)
	}
}
