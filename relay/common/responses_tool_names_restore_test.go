package common

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

const (
	restoreTestOriginal = "mcp.calendar/search.v1"
	restoreTestAlias    = "newapi_tool_mcp_calendar_search_v1_898acec239"
)

func newRestorerForTest(t *testing.T, names ...string) *ResponsesToolNameRestorer {
	t.Helper()
	tools := make([]string, 0, len(names))
	for _, name := range names {
		tools = append(tools, `{"type":"function","name":"`+name+`","parameters":{}}`)
	}
	request := dto.OpenAIResponsesRequest{
		Model: "gpt-5.4",
		Tools: []byte("[" + strings.Join(tools, ",") + "]"),
	}
	_, result, restorer, err := NormalizeResponsesToolNamesWithRestorer(request)
	require.NoError(t, err)
	require.Positive(t, result.ChangedCount)
	require.True(t, restorer.HasAliases())
	return restorer
}

// The upstream echoes the alias in output[].name. The client declared the
// original name and must receive it back, or it cannot match the call to its own
// tool.
func TestRestoreRawJSONRewritesOutputItemName(t *testing.T) {
	restorer := newRestorerForTest(t, restoreTestOriginal)
	body := []byte(`{"id":"resp_1","output":[{"type":"function_call","name":"` + restoreTestAlias + `","call_id":"call_1","arguments":"{}"}],"usage":{"input_tokens":1}}`)

	restored, changed := restorer.RestoreRawJSON(body)
	require.True(t, changed)
	require.Contains(t, string(restored), `"name":"`+restoreTestOriginal+`"`)
	require.NotContains(t, string(restored), restoreTestAlias)
	// Fields this gateway does not model must survive the rewrite.
	require.Contains(t, string(restored), `"usage":{"input_tokens":1}`)
}

func TestRestoreRawJSONRewritesToolCallEventName(t *testing.T) {
	restorer := newRestorerForTest(t, restoreTestOriginal)
	event := []byte(`{"type":"response.function_call_arguments.delta","name":"` + restoreTestAlias + `","delta":"{}","sequence_number":3}`)

	restored, changed := restorer.RestoreRawJSON(event)
	require.True(t, changed)
	require.Contains(t, string(restored), `"name":"`+restoreTestOriginal+`"`)
	require.Contains(t, string(restored), `"sequence_number":3`)
}

// A non-tool event that happens to carry a name field must not be rewritten:
// the field is not a client-declared tool name there.
func TestRestoreRawJSONLeavesUnrelatedEventNameAlone(t *testing.T) {
	restorer := newRestorerForTest(t, restoreTestOriginal)
	event := []byte(`{"type":"response.output_text.delta","name":"` + restoreTestAlias + `","delta":"hi"}`)

	restored, changed := restorer.RestoreRawJSON(event)
	require.False(t, changed)
	require.Equal(t, string(event), string(restored))
}

// Nested tools[] entries and the completed response object inside a stream
// terminal event must both be rewritten.
func TestRestoreRawJSONRewritesNestedShapes(t *testing.T) {
	restorer := newRestorerForTest(t, restoreTestOriginal)
	event := []byte(`{"type":"response.completed","response":{"id":"resp_1","tools":[{"type":"function","name":"` + restoreTestAlias + `","function":{"name":"` + restoreTestAlias + `"}}],"output":[{"type":"function_call","name":"` + restoreTestAlias + `"}]}}`)

	restored, changed := restorer.RestoreRawJSON(event)
	require.True(t, changed)
	require.NotContains(t, string(restored), restoreTestAlias)
	require.Equal(t, 3, strings.Count(string(restored), restoreTestOriginal))
}

// A name that was never rewritten must pass through untouched, so the restorer
// can be applied unconditionally.
func TestRestoreRawJSONLeavesUnrelatedNamesAlone(t *testing.T) {
	restorer := newRestorerForTest(t, restoreTestOriginal)
	body := []byte(`{"output":[{"type":"function_call","name":"get_weather"},{"type":"message","name":"assistant"}]}`)

	restored, changed := restorer.RestoreRawJSON(body)
	require.False(t, changed)
	require.Equal(t, string(body), string(restored))
}

// Multiple distinct originals must each map back to their own name.
func TestRestoreRawJSONHandlesMultipleAliases(t *testing.T) {
	second := "mcp/other.tool v2"
	restorer := newRestorerForTest(t, restoreTestOriginal, second)
	body := []byte(`{"output":[{"type":"function_call","name":"` + restoreTestAlias + `"},{"type":"function_call","name":"` + dto.NormalizeToolName(second) + `"}]}`)

	restored, changed := restorer.RestoreRawJSON(body)
	require.True(t, changed)
	require.Contains(t, string(restored), restoreTestOriginal)
	require.Contains(t, string(restored), second)
}

// An empty restorer or empty payload is a no-op, not a crash.
func TestRestoreRawJSONHandlesEmptyInputs(t *testing.T) {
	var nilRestorer *ResponsesToolNameRestorer
	restored, changed := nilRestorer.RestoreRawJSON([]byte(`{"output":[]}`))
	require.False(t, changed)
	require.Equal(t, `{"output":[]}`, string(restored))

	empty := &ResponsesToolNameRestorer{originalByAlias: map[string]string{}}
	restored, changed = empty.RestoreRawJSON([]byte(`{"output":[]}`))
	require.False(t, changed)
	require.Equal(t, `{"output":[]}`, string(restored))

	restorer := newRestorerForTest(t, restoreTestOriginal)
	restored, changed = restorer.RestoreRawJSON(nil)
	require.False(t, changed)
	require.Nil(t, restored)
}

// A request whose names are all already valid produces no restorer, so the
// response path stays byte-identical.
func TestNormalizeResponsesToolNamesWithRestorerSkipsValidNames(t *testing.T) {
	request := dto.OpenAIResponsesRequest{
		Model: "gpt-5.4",
		Tools: []byte(`[{"type":"function","name":"get_weather","parameters":{}}]`),
	}
	_, result, restorer, err := NormalizeResponsesToolNamesWithRestorer(request)
	require.NoError(t, err)
	require.Zero(t, result.ChangedCount)
	require.False(t, restorer.HasAliases())
}

// The wrapper without the restorer must behave exactly as before.
func TestNormalizeResponsesToolNamesStillReturnsResult(t *testing.T) {
	request := dto.OpenAIResponsesRequest{
		Model: "gpt-5.4",
		Tools: []byte(`[{"type":"function","name":"` + restoreTestOriginal + `","parameters":{}}]`),
	}
	normalized, result, err := NormalizeResponsesToolNames(request)
	require.NoError(t, err)
	require.Equal(t, 1, result.ChangedCount)
	require.Contains(t, string(normalized.Tools), restoreTestAlias)
}

// A message item's name is upstream-defined, not a client declaration, so it
// must not be rewritten even when it collides with an alias.
func TestRestoreRawJSONLeavesNonToolCallItemNameAlone(t *testing.T) {
	restorer := newRestorerForTest(t, restoreTestOriginal)
	event := []byte(`{"type":"response.output_item.added","item":{"type":"message","name":"` + restoreTestAlias + `"}}`)

	restored, changed := restorer.RestoreRawJSON(event)
	require.False(t, changed)
	require.Equal(t, string(event), string(restored))
}

// A tool-call item must still be rewritten.
func TestRestoreRawJSONRewritesToolCallItemName(t *testing.T) {
	restorer := newRestorerForTest(t, restoreTestOriginal)
	event := []byte(`{"type":"response.output_item.added","item":{"type":"function_call","name":"` + restoreTestAlias + `"}}`)

	restored, changed := restorer.RestoreRawJSON(event)
	require.True(t, changed)
	require.Contains(t, string(restored), restoreTestOriginal)
}
