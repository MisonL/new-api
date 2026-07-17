package common

import (
	"encoding/json"
	"testing"

	rootcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestStripEncryptedReasoningFromResponsesInputKeepsPlainInput(t *testing.T) {
	result, err := StripEncryptedReasoningFromResponsesInput(json.RawMessage(`"hello"`))
	require.NoError(t, err)
	require.Equal(t, json.RawMessage(`"hello"`), result.Input)
	require.Zero(t, result.RemovedCount())
	require.Zero(t, result.RemainingCount)
}

func TestNormalizeResponsesInputItemIDsRemovesOnlyInvalidConstrainedIDs(t *testing.T) {
	input := rootcommon.RawMessage(`[
		{"type":"function_call","id":"item_function","call_id":"call_1","name":"exec","arguments":"{}"},
		{"type":"message","id":"item_message","role":"assistant","content":[]},
		{"type":"reasoning","id":"item_reasoning","summary":[]},
		{"type":"function_call","id":"fc_valid","call_id":"call_2","name":"exec","arguments":"{}"},
		{"type":"message","id":"msg_valid","role":"assistant","content":[]},
		{"type":"reasoning","id":"rs_valid","summary":[]},
		{"type":"function_call_output","id":"fco_output","call_id":"call_1","output":"ok"},
		{"type":"web_search_call","id":"ws_valid"}
	]`)

	got, result, err := NormalizeResponsesInputItemIDs(input)
	require.NoError(t, err)
	require.Equal(t, 3, result.RemovedCount)

	var items []map[string]any
	require.NoError(t, rootcommon.Unmarshal(got, &items))
	require.Len(t, items, 8)
	for _, index := range []int{0, 1, 2} {
		_, hasID := items[index]["id"]
		require.False(t, hasID)
	}
	require.Equal(t, "call_1", items[0]["call_id"])
	require.Equal(t, "fc_valid", items[3]["id"])
	require.Equal(t, "msg_valid", items[4]["id"])
	require.Equal(t, "rs_valid", items[5]["id"])
	require.Equal(t, "fco_output", items[6]["id"])
	require.Equal(t, "ws_valid", items[7]["id"])
}

func TestNormalizeResponsesInputItemIDsUsesCodexItemTypePrefixes(t *testing.T) {
	tests := []struct {
		itemType string
		id       string
		keep     bool
	}{
		{itemType: "additional_tools", id: "at_1", keep: true},
		{itemType: "message", id: "msg_1", keep: true},
		{itemType: "agent_message", id: "amsg_1", keep: true},
		{itemType: "reasoning", id: "rs_1", keep: true},
		{itemType: "local_shell_call", id: "lsh_1", keep: true},
		{itemType: "function_call", id: "fc_1", keep: true},
		{itemType: "tool_call", id: "fc_legacy", keep: true},
		{itemType: "mcp_tool_call", id: "fc_mcp_legacy", keep: true},
		{itemType: "tool_search_call", id: "tsc_1", keep: true},
		{itemType: "function_call_output", id: "fco_1", keep: true},
		{itemType: "custom_tool_call", id: "ctc_1", keep: true},
		{itemType: "custom_tool_call_output", id: "ctco_1", keep: true},
		{itemType: "tool_search_output", id: "tso_1", keep: true},
		{itemType: "web_search_call", id: "ws_1", keep: true},
		{itemType: "image_generation_call", id: "ig_1", keep: true},
		{itemType: "compaction", id: "cmp_1", keep: true},
		{itemType: "context_compaction", id: "cmp_2", keep: true},
		{itemType: "compaction_summary", id: "cmp_3", keep: true},
		{itemType: "function_call", id: "", keep: false},
		{itemType: "function_call", id: "fc_", keep: false},
		{itemType: "function_call", id: "fc1", keep: false},
		{itemType: "tool_search_call", id: "fc_wrong", keep: false},
		{itemType: "custom_tool_call_output", id: "ctc_wrong", keep: false},
		{itemType: "unknown_item", id: "legacy_id", keep: true},
	}

	inputItems := make([]map[string]any, 0, len(tests))
	for _, tc := range tests {
		item := map[string]any{"type": tc.itemType, "id": tc.id}
		if tc.itemType == "function_call" {
			item["call_id"] = "call_stable"
		}
		inputItems = append(inputItems, item)
	}
	rawInput, err := rootcommon.Marshal(inputItems)
	require.NoError(t, err)

	got, result, err := NormalizeResponsesInputItemIDs(rawInput)
	require.NoError(t, err)
	require.Equal(t, 5, result.RemovedCount)

	var gotItems []map[string]any
	require.NoError(t, rootcommon.Unmarshal(got, &gotItems))
	require.Len(t, gotItems, len(tests))
	for index, tc := range tests {
		_, hasID := gotItems[index]["id"]
		require.Equal(t, tc.keep, hasID, "%s/%s", tc.itemType, tc.id)
		if tc.itemType == "function_call" {
			require.Equal(t, "call_stable", gotItems[index]["call_id"])
		}
	}
}

func TestNormalizeResponsesInputItemIDsKeepsUnchangedInput(t *testing.T) {
	inputs := []rootcommon.RawMessage{
		rootcommon.RawMessage(`"plain input"`),
		rootcommon.RawMessage(`[{"type":"function_call","call_id":"call_1","name":"exec","arguments":"{}"}]`),
		rootcommon.RawMessage(`[{"type":"function_call","id":"fc_valid","call_id":"call_1","name":"exec","arguments":"{}"}]`),
	}

	for _, input := range inputs {
		got, result, err := NormalizeResponsesInputItemIDs(input)
		require.NoError(t, err)
		require.Zero(t, result.RemovedCount)
		require.Equal(t, json.RawMessage(input), got)
	}
}

func TestStripResponsesReasoningContextPreservesOtherReasoningFields(t *testing.T) {
	request := dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Reasoning: &dto.Reasoning{
			Effort:  "high",
			Summary: "auto",
			Mode:    rootcommon.RawMessage(`"adaptive"`),
			Context: rootcommon.RawMessage(`"all_turns"`),
			Extra: map[string]json.RawMessage{
				"future_reasoning_option": rootcommon.RawMessage(`{"enabled":true}`),
			},
		},
	}

	got, stripped := StripResponsesReasoningContext(request)

	require.True(t, stripped)
	require.NotNil(t, got.Reasoning)
	require.Empty(t, got.Reasoning.Context)
	require.Equal(t, "high", got.Reasoning.Effort)
	require.Equal(t, "auto", got.Reasoning.Summary)
	require.JSONEq(t, `"adaptive"`, string(got.Reasoning.Mode))
	require.JSONEq(t, `{"enabled":true}`, string(got.Reasoning.Extra["future_reasoning_option"]))
	require.JSONEq(t, `"all_turns"`, string(request.Reasoning.Context))

	encoded, err := rootcommon.Marshal(got)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), `"context"`)
	require.Contains(t, string(encoded), `"future_reasoning_option"`)
}

func TestStripEncryptedReasoningFromResponsesInputRemovesEncryptedReasoning(t *testing.T) {
	input := json.RawMessage(`[
		{"type":"reasoning","encrypted_content":"opaque"},
		{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}
	]`)

	result, err := StripEncryptedReasoningFromResponsesInput(input)
	require.NoError(t, err)
	require.Equal(t, 1, result.RemovedCount())
	require.Equal(t, 1, result.RemainingCount)
	require.NotContains(t, string(result.Input), "opaque")
	require.Contains(t, string(result.Input), `"type":"message"`)
}

func TestStripEncryptedReasoningFromResponsesInputKeepsNonStringEncryptedContent(t *testing.T) {
	input := json.RawMessage(`[
		{"type":"reasoning","encrypted_content":{"unexpected":true}},
		{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}
	]`)

	result, err := StripEncryptedReasoningFromResponsesInput(input)
	require.NoError(t, err)
	require.Zero(t, result.RemovedCount())
	require.Equal(t, 2, result.RemainingCount)
	require.Contains(t, string(result.Input), `"unexpected":true`)
}

func TestStripEncryptedReasoningFromResponsesRequestRejectsOnlyEncryptedInput(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: rootcommon.RawMessage(`[
			{"type":"reasoning","encrypted_content":"opaque"}
		]`),
	}

	got, result, err := StripEncryptedReasoningFromResponsesRequest(req)
	require.ErrorIs(t, err, ErrResponsesEncryptedReasoningOnlyInput)
	require.Equal(t, req.Model, got.Model)
	require.Equal(t, req.Input, got.Input)
	require.Equal(t, 1, result.RemovedCount())
	require.Zero(t, result.RemainingCount)
}

func TestStripEncryptedReasoningFromResponsesRequestKeepsOriginalOnParseError(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: rootcommon.RawMessage(`[`),
	}

	got, _, err := StripEncryptedReasoningFromResponsesRequest(req)

	require.Error(t, err)
	require.Equal(t, req.Model, got.Model)
	require.Equal(t, req.Input, got.Input)
}

func TestStripEncryptedReasoningFromResponsesRequestKeepsMixedInput(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: rootcommon.RawMessage(`[
			{"type":"reasoning","encrypted_content":"opaque"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}
		]`),
	}

	got, result, err := StripEncryptedReasoningFromResponsesRequest(req)
	require.NoError(t, err)
	require.Equal(t, "gpt-5.5", got.Model)
	require.Equal(t, 1, result.RemovedCount())
	require.Equal(t, 1, result.RemainingCount)
	require.NotContains(t, string(got.Input), "opaque")
}

func TestNormalizeContextCompactionForResponsesRequestConvertsEncryptedAndDropsEmptyMarkers(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: rootcommon.RawMessage(`[
			{"type":"context_compaction","encrypted_content":"context-summary"},
			{"type":"context_compaction"},
			{"type":"compaction","encrypted_content":"native-summary"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	got, result, err := NormalizeContextCompactionForResponsesRequest(req)

	require.NoError(t, err)
	require.Equal(t, 1, result.ConvertedCount)
	require.Equal(t, 1, result.DroppedCount)
	require.Equal(t, 3, result.RemainingCount)
	require.NotContains(t, string(got.Input), `"type":"context_compaction"`)
	require.JSONEq(t, `[
		{"type":"compaction","encrypted_content":"context-summary"},
		{"type":"compaction","encrypted_content":"native-summary"},
		{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
	]`, string(got.Input))
}

func TestNormalizeContextCompactionForResponsesRequestRejectsOnlyEmptyMarkers(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: rootcommon.RawMessage(`[
			{"type":"context_compaction"},
			{"type":"context_compaction","encrypted_content":""}
		]`),
	}

	got, result, err := NormalizeContextCompactionForResponsesRequest(req)

	require.ErrorIs(t, err, ErrResponsesContextCompactionOnlyInput)
	require.Equal(t, req.Input, got.Input)
	require.Zero(t, result.ConvertedCount)
	require.Equal(t, 2, result.DroppedCount)
	require.Zero(t, result.RemainingCount)
}

func TestNormalizeContextCompactionForResponsesRequestRejectsInvalidEncryptedContentType(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: rootcommon.RawMessage(`[
			{"type":"context_compaction","encrypted_content":{"unexpected":true}},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	got, result, err := NormalizeContextCompactionForResponsesRequest(req)

	require.Error(t, err)
	require.Contains(t, err.Error(), "context_compaction encrypted_content must be a string")
	require.Equal(t, req.Input, got.Input)
	require.Zero(t, result.ConvertedCount)
	require.Zero(t, result.DroppedCount)
}

func TestStripResponsesInputEncryptedContentItemRemovesToolOutputCarrier(t *testing.T) {
	for _, itemType := range []string{"function_call_output", "custom_tool_call_output"} {
		t.Run(itemType, func(t *testing.T) {
			req := dto.OpenAIResponsesRequest{
				Model: "gpt-5.6",
				Input: rootcommon.RawMessage(`[
					{"type":"` + itemType + `","call_id":"call_1","output":[
						{"type":"input_text","text":"keep this tool result"},
						{"type":"encrypted_content","encrypted_content":"opaque"}
					]},
					{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
				]`),
			}

			got, removed, err := StripResponsesInputEncryptedContentItem(req, "input[0].output[1].encrypted_content", "")

			require.NoError(t, err)
			require.True(t, removed)
			require.Contains(t, string(got.Input), "keep this tool result")
			require.NotContains(t, string(got.Input), `"encrypted_content":"opaque"`)
		})
	}
}

func TestStripResponsesInputEncryptedContentItemRemovesUnpopulatedReasoning(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.6",
		Input: rootcommon.RawMessage(`[
			{"type":"reasoning","encrypted_content":""},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	got, removed, err := StripResponsesInputEncryptedContentItem(req, "input[0].encrypted_content", "")

	require.NoError(t, err)
	require.True(t, removed)
	require.NotContains(t, string(got.Input), `"type":"reasoning"`)
	require.Contains(t, string(got.Input), `"type":"message"`)
}

func TestStripResponsesInputEncryptedContentItemDoesNotRemoveNonReasoningTopLevelItem(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.6",
		Input: rootcommon.RawMessage(`[
			{"type":"message","role":"assistant","encrypted_content":""},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	got, removed, err := StripResponsesInputEncryptedContentItem(req, "input[0].encrypted_content", "")

	require.NoError(t, err)
	require.False(t, removed)
	require.Equal(t, req.Input, got.Input)
}

func TestValidResponsesToolOutputEncryptedContentTarget(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Input: rootcommon.RawMessage(`[
			{"type":"function_call_output","call_id":"call_1","output":[{"type":"input_text","text":"done"}]},
			{"type":"compaction","encrypted_content":"opaque"}
		]`),
	}

	target, valid, err := ValidResponsesToolOutputEncryptedContentTarget(
		req,
		"",
		"[ObjectParam] [input[0].encrypted_content] [missing_required_parameter] Missing required parameter: 'input[0].encrypted_content'.",
	)

	require.NoError(t, err)
	require.True(t, valid)
	require.Equal(t, 0, target.InputIndex)
	require.Equal(t, "function_call_output", target.ItemType)

	_, valid, err = ValidResponsesToolOutputEncryptedContentTarget(req, "input[1].encrypted_content", "")
	require.NoError(t, err)
	require.False(t, valid)
}

func TestStripResponsesInputEncryptedContentItemRequiresEncryptedContentItemType(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.6",
		Input: rootcommon.RawMessage(`[
			{"type":"function_call_output","call_id":"call_1","output":[
				{"type":"input_text","text":"keep this tool result","encrypted_content":"opaque"},
				{"type":"input_text","text":"another tool result"}
			]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	got, removed, err := StripResponsesInputEncryptedContentItem(req, "input[0].output[0].encrypted_content", "")

	require.NoError(t, err)
	require.False(t, removed)
	require.Equal(t, req.Input, got.Input)
}

func TestStripResponsesInputEncryptedContentItemDoesNotUseMessagePathWhenParamProvided(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.6",
		Input: rootcommon.RawMessage(`[
			{"type":"reasoning","encrypted_content":"opaque"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	got, removed, err := StripResponsesInputEncryptedContentItem(req, "input[0].unsupported", "Missing required parameter: 'input[0].encrypted_content'.")

	require.NoError(t, err)
	require.False(t, removed)
	require.Equal(t, req.Input, got.Input)
}

func TestStripResponsesInputEncryptedContentItemUsesStructuredObjectParamFallback(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.6",
		Input: rootcommon.RawMessage(`[
			{"type":"reasoning","encrypted_content":"opaque"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	got, removed, err := StripResponsesInputEncryptedContentItem(
		req,
		"",
		"[ObjectParam] [input[0].encrypted_content] [missing_required_parameter] Missing required parameter: 'input[0].encrypted_content'.",
	)

	require.NoError(t, err)
	require.True(t, removed)
	require.NotContains(t, string(got.Input), "opaque")
	require.Contains(t, string(got.Input), `"type":"message"`)
}

func TestStripResponsesInputEncryptedContentItemUsesStructuredObjectParamFallbackForAgentMessage(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.6",
		Input: rootcommon.RawMessage(`[
			{"type":"agent_message","content":[
				{"type":"output_text","text":"keep this agent result"},
				{"type":"encrypted_content","encrypted_content":"opaque"}
			]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	got, removed, err := StripResponsesInputEncryptedContentItem(
		req,
		"",
		"[ObjectParam] [input[0].content[1].encrypted_content] [unsupported_parameter] Unsupported parameter: 'input[0].content[1].encrypted_content'.",
	)

	require.NoError(t, err)
	require.True(t, removed)
	require.Contains(t, string(got.Input), "keep this agent result")
	require.NotContains(t, string(got.Input), `"encrypted_content":"opaque"`)
}

func TestStripResponsesInputEncryptedContentItemDoesNotUseUnstructuredMessageFallback(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.6",
		Input: rootcommon.RawMessage(`[
			{"type":"reasoning","encrypted_content":"opaque"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	got, removed, err := StripResponsesInputEncryptedContentItem(
		req,
		"",
		"The user text quoted input[0].encrypted_content with missing_required_parameter, but this is not an ObjectParam field error.",
	)

	require.NoError(t, err)
	require.False(t, removed)
	require.Equal(t, req.Input, got.Input)
	require.False(t, HasResponsesInputEncryptedContentPath("", "The user text quoted input[0].encrypted_content with missing_required_parameter, but this is not an ObjectParam field error."))
}

func TestResponsesRequestHasNamespaceTools(t *testing.T) {
	tests := []struct {
		name      string
		request   *dto.OpenAIResponsesRequest
		expected  bool
		wantError bool
	}{
		{name: "nil request"},
		{name: "empty tools", request: &dto.OpenAIResponsesRequest{}},
		{
			name:    "function tool only",
			request: &dto.OpenAIResponsesRequest{Tools: rootcommon.RawMessage(`[{"type":"function","name":"shell"}]`)},
		},
		{
			name:     "namespace tool",
			request:  &dto.OpenAIResponsesRequest{Tools: rootcommon.RawMessage(`[{"type":"namespace","name":"codex_app","tools":[]}]`)},
			expected: true,
		},
		{
			name: "namespace tool from additional tools",
			request: &dto.OpenAIResponsesRequest{Input: rootcommon.RawMessage(`[
				{"type":"additional_tools","tools":[{"type":"namespace","name":"collaboration","tools":[]}]},
				{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
			]`)},
			expected: true,
		},
		{
			name:      "malformed tools array",
			request:   &dto.OpenAIResponsesRequest{Tools: rootcommon.RawMessage(`[{`)},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := ResponsesRequestHasNamespaceTools(tt.request)
			require.Equal(t, tt.wantError, err != nil)
			require.Equal(t, tt.expected, actual)
		})
	}
}

func TestResponsesRequestHasCustomTools(t *testing.T) {
	tests := []struct {
		name    string
		request *dto.OpenAIResponsesRequest
		want    bool
	}{
		{
			name: "custom tool declaration",
			request: &dto.OpenAIResponsesRequest{
				Tools: rootcommon.RawMessage(`[{"type":"custom","name":"apply_patch","format":{"type":"grammar"}}]`),
			},
			want: true,
		},
		{
			name: "string custom tool declaration",
			request: &dto.OpenAIResponsesRequest{
				Tools: rootcommon.RawMessage(`["apply_patch"]`),
			},
			want: true,
		},
		{
			name: "custom tool in additional tools",
			request: &dto.OpenAIResponsesRequest{
				Input: rootcommon.RawMessage(`[
					{"type":"additional_tools","tools":[{"type":"custom","name":"apply_patch"}]},
					{"type":"message","role":"user","content":"continue"}
				]`),
			},
			want: true,
		},
		{
			name: "custom tool history",
			request: &dto.OpenAIResponsesRequest{
				Input: rootcommon.RawMessage(`[{"type":"custom_tool_call","call_id":"call_1","name":"apply_patch","input":"*** Begin Patch"}]`),
			},
			want: true,
		},
		{
			name: "custom tool choice",
			request: &dto.OpenAIResponsesRequest{
				ToolChoice: rootcommon.RawMessage(`{"type":"custom","name":"apply_patch"}`),
			},
			want: true,
		},
		{
			name: "ordinary function tool",
			request: &dto.OpenAIResponsesRequest{
				Tools: rootcommon.RawMessage(`[{"type":"function","name":"lookup"}]`),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResponsesRequestHasCustomTools(tt.request)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestResponsesRequestHasFunctionTools(t *testing.T) {
	tests := []struct {
		name    string
		request *dto.OpenAIResponsesRequest
		want    bool
	}{
		{
			name:    "function tool declaration",
			request: &dto.OpenAIResponsesRequest{Tools: rootcommon.RawMessage(`[{"type":"function","name":"lookup"}]`)},
			want:    true,
		},
		{
			name: "function tool in additional tools",
			request: &dto.OpenAIResponsesRequest{Input: rootcommon.RawMessage(`[
				{"type":"additional_tools","tools":[{"type":"function","name":"lookup"}]},
				{"type":"message","role":"user","content":"continue"}
			]`)},
			want: true,
		},
		{
			name:    "function tool history",
			request: &dto.OpenAIResponsesRequest{Input: rootcommon.RawMessage(`[{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"}]`)},
			want:    true,
		},
		{
			name:    "function tool output history",
			request: &dto.OpenAIResponsesRequest{Input: rootcommon.RawMessage(`[{"type":"function_call_output","call_id":"call_1","output":"ok"}]`)},
			want:    true,
		},
		{
			name:    "function tool choice",
			request: &dto.OpenAIResponsesRequest{ToolChoice: rootcommon.RawMessage(`{"type":"function","name":"lookup"}`)},
			want:    true,
		},
		{
			name:    "custom tool only",
			request: &dto.OpenAIResponsesRequest{Tools: rootcommon.RawMessage(`[{"type":"custom","name":"apply_patch"}]`)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResponsesRequestHasFunctionTools(tt.request)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestResponsesNamespaceHistoryIndex(t *testing.T) {
	tests := []struct {
		name      string
		param     string
		wantIndex int
		wantOK    bool
	}{
		{
			name:      "valid parameter",
			param:     "input[42].namespace",
			wantIndex: 42,
			wantOK:    true,
		},
		{
			name:      "valid parameter with whitespace",
			param:     " input[1].namespace ",
			wantIndex: 1,
			wantOK:    true,
		},
		{
			name:   "missing index",
			param:  "input[].namespace",
			wantOK: false,
		},
		{
			name:   "different field",
			param:  "input[1].name",
			wantOK: false,
		},
		{
			name:   "nested namespace field",
			param:  "input[1].tool.namespace",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotIndex, gotOK := ResponsesNamespaceHistoryIndex(tt.param)

			require.Equal(t, tt.wantOK, gotOK)
			if tt.wantOK {
				require.Equal(t, tt.wantIndex, gotIndex)
			}
		})
	}
}

func TestResponsesNamespaceHistoryIndexFromError(t *testing.T) {
	tests := []struct {
		name      string
		param     string
		message   string
		wantIndex int
		wantOK    bool
	}{
		{
			name:      "uses param first",
			param:     "input[245].namespace",
			message:   "Unknown parameter: 'input[538].namespace'.",
			wantIndex: 245,
			wantOK:    true,
		},
		{
			name:      "extracts from message when param is empty",
			message:   "Unknown parameter: 'input[538].namespace'.",
			wantIndex: 538,
			wantOK:    true,
		},
		{
			name:    "ignores unrelated message",
			message: "Unknown parameter: 'input[538].name'.",
			wantOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotIndex, gotOK := ResponsesNamespaceHistoryIndexFromError(tt.param, tt.message)

			require.Equal(t, tt.wantOK, gotOK)
			if tt.wantOK {
				require.Equal(t, tt.wantIndex, gotIndex)
			}
		})
	}
}

func TestResponsesArgumentsHistoryIndex(t *testing.T) {
	tests := []struct {
		name      string
		param     string
		wantIndex int
		wantOK    bool
	}{
		{
			name:      "valid parameter",
			param:     "input[333].arguments",
			wantIndex: 333,
			wantOK:    true,
		},
		{
			name:      "valid parameter with whitespace",
			param:     " input[2].arguments ",
			wantIndex: 2,
			wantOK:    true,
		},
		{
			name:   "different field",
			param:  "input[2].namespace",
			wantOK: false,
		},
		{
			name:   "nested arguments field",
			param:  "input[2].function.arguments",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotIndex, gotOK := ResponsesArgumentsHistoryIndex(tt.param)

			require.Equal(t, tt.wantOK, gotOK)
			if tt.wantOK {
				require.Equal(t, tt.wantIndex, gotIndex)
			}
		})
	}
}

func TestResponsesArgumentsHistoryIndexFromError(t *testing.T) {
	tests := []struct {
		name      string
		param     string
		message   string
		wantIndex int
		wantOK    bool
	}{
		{
			name:      "uses param first",
			param:     "input[333].arguments",
			message:   "Invalid type for 'input[334].arguments': expected an object, but got a string instead.",
			wantIndex: 333,
			wantOK:    true,
		},
		{
			name:      "extracts from message when param is empty",
			message:   "Invalid type for 'input[333].arguments': expected an object, but got a string instead.",
			wantIndex: 333,
			wantOK:    true,
		},
		{
			name:    "ignores unrelated message",
			message: "Invalid type for 'input[333].name'.",
			wantOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotIndex, gotOK := ResponsesArgumentsHistoryIndexFromError(tt.param, tt.message)

			require.Equal(t, tt.wantOK, gotOK)
			if tt.wantOK {
				require.Equal(t, tt.wantIndex, gotIndex)
			}
		})
	}
}

func TestStripNamespaceFromResponsesInputHistory(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.6",
		Input: rootcommon.RawMessage(`[
			{"type":"custom_tool_call","call_id":"call_keep","namespace":"keep_namespace","input":"{}"},
			{"type":"custom_tool_call","call_id":"call_rejected","namespace":"rejected_namespace","input":"{}"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	got, removed, err := StripNamespaceFromResponsesInputHistory(req, 1)

	require.NoError(t, err)
	require.True(t, removed)
	require.Contains(t, string(got.Input), `"namespace":"keep_namespace"`)
	require.NotContains(t, string(got.Input), `"namespace":"rejected_namespace"`)
	require.Contains(t, string(got.Input), `"call_id":"call_rejected"`)
}

func TestResponsesRequestHasToolSearch(t *testing.T) {
	tests := []struct {
		name    string
		request *dto.OpenAIResponsesRequest
		want    bool
	}{
		{
			name: "additional tools declaration",
			request: &dto.OpenAIResponsesRequest{Input: rootcommon.RawMessage(`[
				{"type":"additional_tools","tools":[{"type":"tool_search"}]},
				{"type":"message","role":"user","content":"find a tool"}
			]`)},
			want: true,
		},
		{
			name:    "tool search history",
			request: &dto.OpenAIResponsesRequest{Input: rootcommon.RawMessage(`[{"type":"tool_search_output","call_id":"call_1","status":"completed","tools":[]}]`)},
			want:    true,
		},
		{
			name:    "tool search choice",
			request: &dto.OpenAIResponsesRequest{Input: rootcommon.RawMessage(`"find a tool"`), ToolChoice: rootcommon.RawMessage(`{"type":"tool_search"}`)},
			want:    true,
		},
		{
			name:    "ordinary function tool",
			request: &dto.OpenAIResponsesRequest{Tools: rootcommon.RawMessage(`[{"type":"function","name":"lookup"}]`)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasToolSearch, err := ResponsesRequestHasToolSearch(tt.request)

			require.NoError(t, err)
			require.Equal(t, tt.want, hasToolSearch)
		})
	}
}

func TestStripNamespaceFromResponsesInputHistoryKeepsOriginalWhenNothingRemoved(t *testing.T) {
	tests := []struct {
		name      string
		input     rootcommon.RawMessage
		itemIndex int
	}{
		{
			name:      "plain input",
			input:     rootcommon.RawMessage(`"hello"`),
			itemIndex: 0,
		},
		{
			name: "negative index",
			input: rootcommon.RawMessage(`[
				{"type":"custom_tool_call","namespace":"keep_namespace"}
			]`),
			itemIndex: -1,
		},
		{
			name: "index out of range",
			input: rootcommon.RawMessage(`[
				{"type":"custom_tool_call","namespace":"keep_namespace"}
			]`),
			itemIndex: 1,
		},
		{
			name: "target item has no namespace",
			input: rootcommon.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
			]`),
			itemIndex: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := dto.OpenAIResponsesRequest{
				Model: "gpt-5.6",
				Input: tt.input,
			}

			got, removed, err := StripNamespaceFromResponsesInputHistory(req, tt.itemIndex)

			require.NoError(t, err)
			require.False(t, removed)
			require.Equal(t, req.Input, got.Input)
		})
	}
}

func TestNormalizeStringObjectArgumentsFromResponsesInputHistory(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.6",
		Input: rootcommon.RawMessage(`[
			{"type":"function_call","call_id":"call_keep","name":"read_file","arguments":"{\"path\":\"keep.txt\"}"},
			{"type":"function_call","call_id":"call_rejected","name":"edit_file","arguments":"{\"path\":\"target.txt\",\"ok\":true}"},
			{"type":"function_call","call_id":"call_invalid","name":"shell","arguments":"not json"}
		]`),
	}

	got, result, err := NormalizeStringObjectArgumentsFromResponsesInputHistory(req, 1)

	require.NoError(t, err)
	require.Equal(t, 2, result.NormalizedCount)
	require.True(t, result.TargetNormalized)
	require.Contains(t, string(got.Input), `"arguments":{"path":"keep.txt"}`)
	require.Contains(t, string(got.Input), `"arguments":{"ok":true,"path":"target.txt"}`)
	require.Contains(t, string(got.Input), `"arguments":"not json"`)
}

func TestStringifyObjectArgumentsFromResponsesInputHistory(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.6",
		Input: rootcommon.RawMessage(`[
			{"type":"function_call","call_id":"call_keep","arguments":{"path":"keep.txt"}},
			{"type":"function_call","call_id":"call_rejected","arguments":{"path":"target.txt","ok":true}},
			{"type":"function_call","call_id":"call_string","arguments":"already string"},
			{"type":"function_call","call_id":"call_array","arguments":[1,2]}
		]`),
	}

	got, result, err := StringifyObjectArgumentsFromResponsesInputHistory(req, 1)

	require.NoError(t, err)
	require.Equal(t, 2, result.NormalizedCount)
	require.True(t, result.TargetNormalized)
	require.Contains(t, string(got.Input), `"arguments":"{\"path\":\"keep.txt\"}"`)
	require.Contains(t, string(got.Input), `"arguments":"{\"ok\":true,\"path\":\"target.txt\"}"`)
	require.Contains(t, string(got.Input), `"arguments":"already string"`)
	require.Contains(t, string(got.Input), `"arguments":[1,2]`)
}

func TestStringifyObjectArgumentsFromResponsesInputHistoryKeepsOriginalWhenTargetNotNormalized(t *testing.T) {
	tests := []struct {
		name      string
		input     rootcommon.RawMessage
		itemIndex int
	}{
		{
			name:      "plain input",
			input:     rootcommon.RawMessage(`"hello"`),
			itemIndex: 0,
		},
		{
			name:      "index out of range",
			input:     rootcommon.RawMessage(`[{"type":"function_call","arguments":{"path":"keep.txt"}}]`),
			itemIndex: 2,
		},
		{
			name:      "target arguments is array",
			input:     rootcommon.RawMessage(`[{"type":"function_call","arguments":[1,2]}]`),
			itemIndex: 0,
		},
		{
			name:      "target arguments is already string",
			input:     rootcommon.RawMessage(`[{"type":"function_call","arguments":"already string"}]`),
			itemIndex: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := dto.OpenAIResponsesRequest{Model: "gpt-5.6", Input: tt.input}
			got, result, err := StringifyObjectArgumentsFromResponsesInputHistory(req, tt.itemIndex)

			require.NoError(t, err)
			require.False(t, result.TargetNormalized)
			require.Equal(t, req.Input, got.Input)
		})
	}
}

func TestNormalizeStringObjectArgumentsFromResponsesInputHistoryKeepsOriginalWhenTargetNotNormalized(t *testing.T) {
	tests := []struct {
		name      string
		input     rootcommon.RawMessage
		itemIndex int
	}{
		{
			name:      "plain input",
			input:     rootcommon.RawMessage(`"hello"`),
			itemIndex: 0,
		},
		{
			name: "index out of range",
			input: rootcommon.RawMessage(`[
				{"type":"function_call","arguments":"{\"path\":\"keep.txt\"}"}
			]`),
			itemIndex: 2,
		},
		{
			name: "target arguments is array",
			input: rootcommon.RawMessage(`[
				{"type":"function_call","arguments":"[1,2]"}
			]`),
			itemIndex: 0,
		},
		{
			name: "target arguments is already object",
			input: rootcommon.RawMessage(`[
				{"type":"function_call","arguments":{"path":"keep.txt"}}
			]`),
			itemIndex: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := dto.OpenAIResponsesRequest{
				Model: "gpt-5.6",
				Input: tt.input,
			}

			got, result, err := NormalizeStringObjectArgumentsFromResponsesInputHistory(req, tt.itemIndex)

			require.NoError(t, err)
			require.False(t, result.TargetNormalized)
			require.Equal(t, req.Input, got.Input)
		})
	}
}
