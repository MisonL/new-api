package dto

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMessageParseContentStrictRejectsMalformedArrayMember(t *testing.T) {
	message := Message{Content: []any{"not-an-object"}}

	parts, err := message.ParseContentStrict()

	require.Error(t, err)
	require.Nil(t, parts)
	require.Contains(t, err.Error(), "content[0]")
}

func TestMessageParseContentStrictRejectsMissingType(t *testing.T) {
	message := Message{Content: []any{map[string]any{"text": "hello"}}}

	parts, err := message.ParseContentStrict()

	require.Error(t, err)
	require.Nil(t, parts)
	require.Contains(t, err.Error(), "type is required")
}

func TestMessageParseToolCallsStrictRejectsMalformedPayload(t *testing.T) {
	message := Message{ToolCalls: []byte(`{"id":"call_1"}`)}

	calls, err := message.ParseToolCallsStrict()

	require.Error(t, err)
	require.Nil(t, calls)
	require.Contains(t, err.Error(), "tool_calls must be an array")
}

func TestMessageParseContentStrictPreservesEmptyTextPart(t *testing.T) {
	message := Message{Content: []any{map[string]any{"type": "text", "text": ""}}}

	parts, err := message.ParseContentStrict()

	require.NoError(t, err)
	require.Len(t, parts, 1)
	require.Equal(t, ContentTypeText, parts[0].Type)
	require.Empty(t, parts[0].Text)
}
