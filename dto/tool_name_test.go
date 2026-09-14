package dto

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeToolNamePreservesValidNames(t *testing.T) {
	require.Equal(t, "lookup_file", NormalizeToolName("lookup_file"))
	require.True(t, IsValidToolName(NormalizeToolName("lookup_file")))
}

func TestNormalizeToolNameMakesInvalidNamesDeterministicAndSafe(t *testing.T) {
	got := NormalizeToolName("mcp.calendar/search.v1")
	require.Equal(t, got, NormalizeToolName("mcp.calendar/search.v1"))
	require.NotEqual(t, "mcp.calendar/search.v1", got)
	require.LessOrEqual(t, len(got), ToolNameMaxLength)
	require.Regexp(t, regexp.MustCompile(`^[a-zA-Z0-9_-]+$`), got)
	require.NotEmpty(t, got)
}

func TestNormalizeToolNameKeepsDistinctInvalidNamesDistinct(t *testing.T) {
	first := NormalizeToolName("mcp.calendar/search")
	second := NormalizeToolName("mcp.calendar.search")
	require.NotEqual(t, first, second)
}

func TestNormalizeToolNameHandlesWhitespaceAndLongNames(t *testing.T) {
	got := NormalizeToolName("  tool name with spaces and a very long suffix that exceeds the upstream limit  ")
	require.LessOrEqual(t, len(got), ToolNameMaxLength)
	require.Regexp(t, regexp.MustCompile(`^[a-zA-Z0-9_-]+$`), got)
}
