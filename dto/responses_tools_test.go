package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesRequestEffectiveToolsMergesTopLevelAndAdditionalTools(t *testing.T) {
	req := &OpenAIResponsesRequest{
		Tools: common.RawMessage(`[{"type":"function","name":"lookup"}]`),
		Input: common.RawMessage(`[
			"plain input",
			{"type":"additional_tools","role":"developer","tools":[
				{"type":"custom","name":"exec"},
				{"type":"namespace","name":"collaboration","tools":[{"type":"function","name":"send_message"}]}
			]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	tools, err := req.EffectiveTools()

	require.NoError(t, err)
	require.Len(t, tools, 3)
	require.JSONEq(t, `{"type":"function","name":"lookup"}`, string(tools[0]))
	require.JSONEq(t, `{"type":"custom","name":"exec"}`, string(tools[1]))
	require.JSONEq(t, `{"type":"namespace","name":"collaboration","tools":[{"type":"function","name":"send_message"}]}`, string(tools[2]))
}

func TestOpenAIResponsesRequestEffectiveToolsRejectsMalformedAdditionalTools(t *testing.T) {
	req := &OpenAIResponsesRequest{
		Input: common.RawMessage(`[{"type":"additional_tools","tools":{"type":"function","name":"lookup"}}]`),
	}

	_, err := req.EffectiveTools()

	require.Error(t, err)
	require.Contains(t, err.Error(), "additional_tools.tools must be an array")
}

func TestOpenAIResponsesRequestEffectiveToolsIncludesLoadedToolSearchTools(t *testing.T) {
	req := &OpenAIResponsesRequest{
		Tools: common.RawMessage(`[{"type":"tool_search","execution":"client","parameters":{"type":"object"}}]`),
		Input: common.RawMessage(`[
			{"type":"tool_search_call","id":"tsc_search","call_id":"call_search","execution":"client","arguments":"{\"query\":\"deploy\"}"},
			{"type":"tool_search_output","id":"tso_search","call_id":"call_search","status":"completed","execution":"client","tools":[
				{"type":"function","name":"loaded_deploy","description":"LOADED_TOOL_SEARCH_MARKER","parameters":{"type":"object"}}
			]}
		]`),
	}

	tools, err := req.EffectiveTools()

	require.NoError(t, err)
	require.Len(t, tools, 2)
	require.JSONEq(t, `{"type":"tool_search","execution":"client","parameters":{"type":"object"}}`, string(tools[0]))
	require.JSONEq(t, `{"type":"function","name":"loaded_deploy","description":"LOADED_TOOL_SEARCH_MARKER","parameters":{"type":"object"}}`, string(tools[1]))
}

func TestOpenAIResponsesRequestEffectiveToolsIncludesOfficialLoadedToolSearchNamespace(t *testing.T) {
	req := &OpenAIResponsesRequest{
		Input: common.RawMessage(`[
			{"type":"tool_search_call","id":"tsc_search","call_id":"call_search","execution":"client","arguments":"{\"query\":\"calendar\"}"},
			{"type":"tool_search_output","id":"tso_search","call_id":"call_search","status":"completed","execution":"client","tools":[
				{"type":"namespace","name":"mcp__calendar","description":"OFFICIAL_LOADED_NAMESPACE_MARKER","tools":[
					{"type":"function","name":"create_event","defer_loading":true,"parameters":{"type":"object"}}
				]}
			]}
		]`),
	}

	tools, err := req.EffectiveTools()

	require.NoError(t, err)
	require.Len(t, tools, 1)
	require.JSONEq(t, `{"type":"namespace","name":"mcp__calendar","description":"OFFICIAL_LOADED_NAMESPACE_MARKER","tools":[{"type":"function","name":"create_event","defer_loading":true,"parameters":{"type":"object"}}]}`, string(tools[0]))
}

func TestOpenAIResponsesRequestEffectiveToolsRejectsMalformedLoadedToolSearchTools(t *testing.T) {
	req := &OpenAIResponsesRequest{
		Input: common.RawMessage(`[
			{"type":"tool_search_output","call_id":"call_search","status":"completed","execution":"client","tools":{"type":"function","name":"loaded_deploy"}}
		]`),
	}

	_, err := req.EffectiveTools()

	require.Error(t, err)
	require.Contains(t, err.Error(), "tool_search_output.tools must be an array")
}

func TestOpenAIResponsesRequestEffectiveToolsRejectsInvalidClientToolSearchOutputMetadata(t *testing.T) {
	tests := []struct {
		name string
		item string
		want string
	}{
		{
			name: "missing call id",
			item: `{"type":"tool_search_output","status":"completed","execution":"client","tools":[]}`,
			want: "tool_search_output.call_id is required",
		},
		{
			name: "incomplete status",
			item: `{"type":"tool_search_output","call_id":"call_search","status":"in_progress","execution":"client","tools":[]}`,
			want: "tool_search_output.status must be completed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := &OpenAIResponsesRequest{Input: common.RawMessage("[" + tc.item + "]")}

			_, err := req.EffectiveTools()

			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestOpenAIResponsesRequestEffectiveToolsDoesNotCountServerToolSearchOutput(t *testing.T) {
	req := &OpenAIResponsesRequest{Input: common.RawMessage(`[
		{"type":"tool_search_output","status":"completed","execution":"server","tools":[
			{"type":"function","name":"server_only","description":"SERVER_TOOL_MUST_NOT_BE_COUNTED"}
		]}
	]`)}

	tools, err := req.EffectiveTools()

	require.NoError(t, err)
	require.Empty(t, tools)
}

func TestOpenAIResponsesRequestEffectiveToolsCacheInvalidatesWhenRequestChanges(t *testing.T) {
	req := &OpenAIResponsesRequest{
		Tools: common.RawMessage(`[{"type":"function","name":"first"}]`),
	}

	first, err := req.EffectiveTools()
	require.NoError(t, err)
	require.Len(t, first, 1)
	require.JSONEq(t, `{"type":"function","name":"first"}`, string(first[0]))

	req.Tools = common.RawMessage(`[{"type":"function","name":"second"}]`)
	second, err := req.EffectiveTools()
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.JSONEq(t, `{"type":"function","name":"second"}`, string(second[0]))

	req.Input = common.RawMessage(`[{"type":"additional_tools","tools":[{"type":"function","name":"loaded"}]}]`)
	third, err := req.EffectiveTools()
	require.NoError(t, err)
	require.Len(t, third, 2)
	require.JSONEq(t, `{"type":"function","name":"loaded"}`, string(third[1]))
}
