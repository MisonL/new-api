package middleware

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitizeAccessLogPathRedactsQueryValues(t *testing.T) {
	path := "/api/channel/search?keyword=sk-sensitive-value&group=default&group=backup"

	got := sanitizeAccessLogPath(path)

	require.Equal(t, "/api/channel/search?group=REDACTED&group=REDACTED&keyword=REDACTED", got)
	require.NotContains(t, got, "sk-sensitive-value")
	require.NotContains(t, got, "default")
}

func TestSanitizeAccessLogPathRedactsMalformedQuery(t *testing.T) {
	got := sanitizeAccessLogPath("/api/channel/search?keyword=%zz")

	require.Equal(t, "/api/channel/search?REDACTED", got)
}
