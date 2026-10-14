package common

import (
	"testing"

	rootcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestNormalizeResponsesToolNamesKeepsDeclarationsChoiceAndHistoryAligned(t *testing.T) {
	original := "mcp.calendar/search.v1"
	request := dto.OpenAIResponsesRequest{
		Tools:      rootcommon.RawMessage(`[{"type":"function","name":"mcp.calendar/search.v1"}]`),
		ToolChoice: rootcommon.RawMessage(`{"type":"function","name":"mcp.calendar/search.v1"}`),
		Input:      rootcommon.RawMessage(`[{"type":"function_call","call_id":"call_1","name":"mcp.calendar/search.v1","arguments":"{}"}]`),
	}

	normalized, result, err := NormalizeResponsesToolNames(request)
	require.NoError(t, err)
	require.Equal(t, 3, result.ChangedCount)
	alias := dto.NormalizeToolName(original)

	var tools []map[string]any
	require.NoError(t, rootcommon.Unmarshal(normalized.Tools, &tools))
	require.Equal(t, alias, tools[0]["name"])
	var choice map[string]any
	require.NoError(t, rootcommon.Unmarshal(normalized.ToolChoice, &choice))
	require.Equal(t, alias, choice["name"])
	var input []map[string]any
	require.NoError(t, rootcommon.Unmarshal(normalized.Input, &input))
	require.Equal(t, alias, input[0]["name"])
	require.Regexp(t, `^[a-zA-Z0-9_-]+$`, alias)
}

func TestNormalizeResponsesToolNamesTraversesNamespaceAndLoadedTools(t *testing.T) {
	request := dto.OpenAIResponsesRequest{
		Tools: rootcommon.RawMessage(`[{"type":"namespace","name":"mcp.calendar/v1","tools":[{"type":"function","name":"search.tool"}]}]`),
		Input: rootcommon.RawMessage(`[{"type":"tool_search_output","call_id":"call_search","status":"completed","execution":"client","tools":[{"type":"custom","name":"patch tool"}]},{"type":"custom_tool_call","call_id":"call_1","namespace":"mcp.calendar/v1","name":"search.tool","input":"{}"}]`),
	}

	normalized, result, err := NormalizeResponsesToolNames(request)
	require.NoError(t, err)
	require.Equal(t, 4, result.ChangedCount)
	var tools []map[string]any
	require.NoError(t, rootcommon.Unmarshal(normalized.Tools, &tools))
	require.Equal(t, dto.NormalizeToolName("mcp.calendar/v1"), tools[0]["name"])
	nestedTools, ok := tools[0]["tools"].([]any)
	require.True(t, ok)
	nestedTool, ok := nestedTools[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, dto.NormalizeToolName("search.tool"), nestedTool["name"])

	var input []map[string]any
	require.NoError(t, rootcommon.Unmarshal(normalized.Input, &input))
	loadedTools, ok := input[0]["tools"].([]any)
	require.True(t, ok)
	loadedTool, ok := loadedTools[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, dto.NormalizeToolName("patch tool"), loadedTool["name"])
	require.Equal(t, dto.NormalizeToolName("search.tool"), input[1]["name"])
	require.Equal(t, "mcp.calendar/v1", input[1]["namespace"])
}

func TestNormalizeResponsesToolNamesCoversLegacyAndMCPHistory(t *testing.T) {
	request := dto.OpenAIResponsesRequest{
		Input: rootcommon.RawMessage(`[
			{"type":"tool_call","name":"legacy.tool"},
			{"type":"mcp_tool_call","name":"legacy/mcp.tool"},
			{"type":"mcp_approval_request","name":"approval tool"},
			{"type":"mcp_call","name":"server/tool.v1"}
		]`),
	}

	normalized, result, err := NormalizeResponsesToolNames(request)
	require.NoError(t, err)
	require.Equal(t, 4, result.ChangedCount)

	var input []map[string]any
	require.NoError(t, rootcommon.Unmarshal(normalized.Input, &input))
	for index, original := range []string{"legacy.tool", "legacy/mcp.tool", "approval tool", "server/tool.v1"} {
		require.Equal(t, dto.NormalizeToolName(original), input[index]["name"])
	}
}

func TestNormalizeResponsesToolNamesRejectsAliasCollision(t *testing.T) {
	alias := dto.NormalizeToolName("mcp.calendar/search.v1")
	request := dto.OpenAIResponsesRequest{
		Tools: rootcommon.RawMessage(`[{"type":"function","name":"mcp.calendar/search.v1"},{"type":"function","name":"` + alias + `"}]`),
	}

	_, _, err := NormalizeResponsesToolNames(request)
	require.Error(t, err)
	require.Contains(t, err.Error(), "conflicting")
}

func TestNormalizeResponsesToolNamesLeavesValidNamesUntouched(t *testing.T) {
	request := dto.OpenAIResponsesRequest{
		Tools:      rootcommon.RawMessage(`[{"type":"function","name":"read_file"}]`),
		ToolChoice: rootcommon.RawMessage(`{"type":"function","name":"read_file"}`),
		Input:      rootcommon.RawMessage(`[{"type":"function_call","name":"read_file","call_id":"call_1"}]`),
	}

	normalized, result, err := NormalizeResponsesToolNames(request)
	require.NoError(t, err)
	require.Zero(t, result.ChangedCount)
	require.JSONEq(t, string(request.Tools), string(normalized.Tools))
	require.JSONEq(t, string(request.ToolChoice), string(normalized.ToolChoice))
	require.JSONEq(t, string(request.Input), string(normalized.Input))
}

// A reference position (a historical call in input[], or tool_choice) may carry
// the alias produced by a declaration in the same request. Resolving it must
// return the alias itself; treating it as a new declaration reported a spurious
// conflict against the declaration that produced it.
func TestNormalizeResponsesToolNamesResolvesReferenceToOwnAlias(t *testing.T) {
	original := "my tool!"
	alias := dto.NormalizeToolName(original)

	request := dto.OpenAIResponsesRequest{
		Tools: rootcommon.RawMessage(`[{"type":"function","name":` + mustJSON(t, original) + `}]`),
		Input: rootcommon.RawMessage(`[{"type":"function_call","call_id":"call_1","name":` + mustJSON(t, alias) + `,"arguments":"{}"}]`),
	}

	normalized, result, err := NormalizeResponsesToolNames(request)
	require.NoError(t, err, "a reference to an alias produced in the same request must resolve")
	require.Equal(t, 1, result.ChangedCount, "only the declaration needs rewriting")

	var input []map[string]any
	require.NoError(t, rootcommon.Unmarshal(normalized.Input, &input))
	require.Equal(t, alias, input[0]["name"])
}

// tool_choice is a reference position too, so choosing an alias must resolve.
func TestNormalizeResponsesToolNamesResolvesToolChoiceAlias(t *testing.T) {
	original := "my tool!"
	alias := dto.NormalizeToolName(original)

	request := dto.OpenAIResponsesRequest{
		Tools:      rootcommon.RawMessage(`[{"type":"function","name":` + mustJSON(t, original) + `}]`),
		ToolChoice: rootcommon.RawMessage(`{"type":"function","name":` + mustJSON(t, alias) + `}`),
	}

	normalized, _, err := NormalizeResponsesToolNames(request)
	require.NoError(t, err)

	var choice map[string]any
	require.NoError(t, rootcommon.Unmarshal(normalized.ToolChoice, &choice))
	require.Equal(t, alias, choice["name"])
}

// Two declarations that collide on one alias remain a hard conflict; the
// reference fix must not weaken declaration-time collision detection.
func TestNormalizeResponsesToolNamesStillRejectsDeclarationCollision(t *testing.T) {
	alias := dto.NormalizeToolName("mcp.calendar/search.v1")
	request := dto.OpenAIResponsesRequest{
		Tools: rootcommon.RawMessage(`[{"type":"function","name":"mcp.calendar/search.v1"},{"type":"function","name":` + mustJSON(t, alias) + `}]`),
	}

	_, _, err := NormalizeResponsesToolNames(request)
	require.Error(t, err)
	require.Contains(t, err.Error(), "conflicting")
}

func mustJSON(t *testing.T, value string) string {
	t.Helper()
	encoded, err := rootcommon.Marshal(value)
	require.NoError(t, err)
	return string(encoded)
}
