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

func TestResponsesInputHasOpaqueEncryptedContentRecursively(t *testing.T) {
	tests := []struct {
		name  string
		input rootcommon.RawMessage
		want  bool
	}{
		{
			name:  "plain input",
			input: rootcommon.RawMessage(`[{"type":"message","content":[{"type":"input_text","text":"continue"}]}]`),
		},
		{
			name:  "null encrypted content",
			input: rootcommon.RawMessage(`[{"type":"reasoning","encrypted_content":null}]`),
		},
		{
			name:  "empty encrypted content",
			input: rootcommon.RawMessage(`[{"type":"reasoning","encrypted_content":"   "}]`),
		},
		{
			name:  "local synthetic compact marker",
			input: rootcommon.RawMessage(`[{"type":"compaction","encrypted_content":"newapi.synthetic.compact:v2:n1:resp_local"}]`),
		},
		{
			name:  "top level opaque reasoning",
			input: rootcommon.RawMessage(`[{"type":"reasoning","encrypted_content":"opaque-reasoning-state"}]`),
			want:  true,
		},
		{
			name:  "nested opaque agent content",
			input: rootcommon.RawMessage(`[{"type":"agent_message","content":[{"type":"encrypted_content","encrypted_content":"opaque-agent-state"}]}]`),
			want:  true,
		},
		{
			name:  "future nested opaque field",
			input: rootcommon.RawMessage(`[{"type":"future_item","future":{"payload":[{"encrypted_content":"opaque-future-state"}]}}]`),
			want:  false,
		},
		{
			name:  "nested opaque tool output",
			input: rootcommon.RawMessage(`[{"type":"function_call_output","output":[{"type":"encrypted_content","encrypted_content":"opaque-tool-state"}]}]`),
			want:  true,
		},
		{
			name:  "non string encrypted content",
			input: rootcommon.RawMessage(`[{"type":"reasoning","encrypted_content":{"opaque":true}}]`),
			want:  true,
		},
		{
			name:  "similarly named field",
			input: rootcommon.RawMessage(`[{"type":"message","not_encrypted_content":"opaque-looking-text"}]`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResponsesInputHasOpaqueEncryptedContent(tt.input)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestResponsesInputHasOpaqueEncryptedContentIgnoresUnknownNestedFields(t *testing.T) {
	input := rootcommon.RawMessage(`[{
		"type":"future_item",
		"metadata":{"encrypted_content":"opaque-future-state"}
	}]`)

	got, err := ResponsesInputHasOpaqueEncryptedContent(input)

	require.NoError(t, err)
	require.False(t, got)
}

func TestResponsesInputHasOpaqueEncryptedContentTreatsEmptyInputAsAbsent(t *testing.T) {
	tests := []struct {
		name  string
		input rootcommon.RawMessage
	}{
		{
			name: "nil input",
		},
		{
			name:  "whitespace input",
			input: rootcommon.RawMessage(" \n\t"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResponsesInputHasOpaqueEncryptedContent(tt.input)

			require.NoError(t, err)
			require.False(t, got)
		})
	}
}

func TestResponsesInputHasOpaqueEncryptedContentRejectsMalformedInput(t *testing.T) {
	got, err := ResponsesInputHasOpaqueEncryptedContent(rootcommon.RawMessage(`[{"encrypted_content":"opaque"}`))

	require.Error(t, err)
	require.False(t, got)
}

func TestNormalizeResponsesInputItemIDsRemovesOnlyConstrainedIDs(t *testing.T) {
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
	_, functionHasID := items[0]["id"]
	_, messageHasID := items[1]["id"]
	_, reasoningHasID := items[2]["id"]
	require.False(t, functionHasID)
	require.False(t, messageHasID)
	require.False(t, reasoningHasID)
	require.Equal(t, "call_1", items[0]["call_id"])
	require.Equal(t, "fc_valid", items[3]["id"])
	require.Equal(t, "msg_valid", items[4]["id"])
	require.Equal(t, "rs_valid", items[5]["id"])
	require.Equal(t, "fco_output", items[6]["id"])
	require.Equal(t, "ws_valid", items[7]["id"])
}

func TestNormalizeResponsesInputItemIDsPreservesOpaqueReplayIDs(t *testing.T) {
	input := rootcommon.RawMessage(`[
		{"type":"reasoning","id":"client-reasoning-id","encrypted_content":"opaque-state"},
		{"type":"compaction","id":"client-compaction-id","encrypted_content":"opaque-state"},
		{"type":"function_call","id":"invalid-function-id","call_id":"call_1","name":"exec","arguments":"{}"},
		{"type":"message","id":"msg_valid","role":"user","content":[]}
	]`)

	got, result, err := NormalizeResponsesInputItemIDs(input)
	require.NoError(t, err)
	require.Equal(t, 1, result.RemovedCount)

	var items []map[string]any
	require.NoError(t, rootcommon.Unmarshal(got, &items))
	require.Equal(t, "client-reasoning-id", items[0]["id"])
	require.Equal(t, "client-compaction-id", items[1]["id"])
	_, hasFunctionID := items[2]["id"]
	require.False(t, hasFunctionID)
	require.Equal(t, "msg_valid", items[3]["id"])
}

func TestNormalizeResponsesInputItemIDsRemovesOnlyExplicitlyInvalidPrefixes(t *testing.T) {
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
		{itemType: "tool_search_call", id: "tsc_search_1", keep: true},
		{itemType: "function_call_output", id: "fco_1", keep: true},
		{itemType: "custom_tool_call", id: "ctc_custom_1", keep: true},
		{itemType: "custom_tool_call_output", id: "ctco_1", keep: true},
		{itemType: "tool_search_output", id: "tso_1", keep: true},
		{itemType: "web_search_call", id: "ws_1", keep: true},
		{itemType: "image_generation_call", id: "ig_1", keep: true},
		{itemType: "compaction", id: "cmp_1", keep: true},
		{itemType: "context_compaction", id: "cmp_2", keep: true},
		{itemType: "compaction_summary", id: "cmp_3", keep: true},
		{itemType: "shell_call", id: "sc_future", keep: true},
		{itemType: "mcp_call", id: "mcp_future", keep: true},
		{itemType: "unknown_item", id: "future_prefix_value", keep: true},
		{itemType: "function_call", id: "", keep: false},
		{itemType: "function_call", id: "fc_", keep: false},
		{itemType: "function_call", id: "item_wrong", keep: false},
		{itemType: "tool_search_call", id: "item_wrong", keep: false},
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
	removedCount := 0
	for _, tc := range tests {
		if !tc.keep {
			removedCount++
		}
	}
	require.Equal(t, removedCount, result.RemovedCount)

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

func TestNormalizeResponsesInputItemIDsPreservesOutputAndUnconstrainedIDs(t *testing.T) {
	input := rootcommon.RawMessage(`[
		{"type":"function_call_output","id":"fco_1","call_id":"call_1","output":"done"},
		{"type":"custom_tool_call_output","id":"ctco_legacy","call_id":"call_2","output":"done"},
		{"type":"tool_search_output","id":"tso_001","call_id":"call_3","status":"completed"},
		{"type":"web_search_call","id":"ws_search","status":"completed"},
		{"type":"reasoning","id":"rs_reasoning","summary":[]}
	]`)

	got, result, err := NormalizeResponsesInputItemIDs(input)

	require.NoError(t, err)
	require.Zero(t, result.RemovedCount)
	require.JSONEq(t, string(input), string(got))
}

func TestNormalizeResponsesInputItemIDsUsesCodexInputPrefixes(t *testing.T) {
	tests := []struct {
		itemType string
		prefix   string
	}{
		{itemType: "message", prefix: "msg"},
		{itemType: "additional_tools", prefix: "at"},
		{itemType: "agent_message", prefix: "amsg"},
		{itemType: "reasoning", prefix: "rs"},
		{itemType: "local_shell_call", prefix: "lsh"},
		{itemType: "function_call", prefix: "fc"},
		{itemType: "tool_call", prefix: "fc"},
		{itemType: "mcp_tool_call", prefix: "fc"},
		{itemType: "tool_search_call", prefix: "tsc"},
		{itemType: "function_call_output", prefix: "fco"},
		{itemType: "custom_tool_call", prefix: "ctc"},
		{itemType: "custom_tool_call_output", prefix: "ctco"},
		{itemType: "tool_search_output", prefix: "tso"},
		{itemType: "web_search_call", prefix: "ws"},
		{itemType: "image_generation_call", prefix: "ig"},
		{itemType: "compaction", prefix: "cmp"},
		{itemType: "context_compaction", prefix: "cmp"},
		{itemType: "compaction_summary", prefix: "cmp"},
	}

	inputItems := make([]map[string]any, 0, len(tests)*2)
	for _, tc := range tests {
		inputItems = append(inputItems,
			map[string]any{"type": tc.itemType, "id": tc.prefix + "_valid"},
			map[string]any{"type": tc.itemType, "id": "item_wrong"},
		)
	}
	rawInput, err := rootcommon.Marshal(inputItems)
	require.NoError(t, err)

	got, result, err := NormalizeResponsesInputItemIDs(rawInput)
	require.NoError(t, err)
	require.Equal(t, len(tests), result.RemovedCount)

	var items []map[string]any
	require.NoError(t, rootcommon.Unmarshal(got, &items))
	require.Len(t, items, len(inputItems))
	for index, tc := range tests {
		validItem := items[index*2]
		invalidItem := items[index*2+1]
		require.Equal(t, tc.prefix+"_valid", validItem["id"], tc.itemType)
		_, invalidHasID := invalidItem["id"]
		require.False(t, invalidHasID, tc.itemType)
	}
}

func TestNormalizeResponsesInputItemIDsPreservesNonStringIDsAndCallPairs(t *testing.T) {
	input := rootcommon.RawMessage(`[
		{"type":"function_call","id":null,"call_id":"call_null","name":"exec","arguments":"{}"},
		{"type":"function_call_output","id":42,"call_id":"call_number","output":"ok"},
		{"type":"custom_tool_call","id":{},"call_id":"call_object","name":"exec","input":"{}"},
		{"type":"tool_search_call","id":"TSC_wrong_case","call_id":"call_search","execution":"client","arguments":{}},
		{"type":"message","id":" msg_1 ","role":"user","content":[]},
		{"type":"unknown_future_item","id":42,"call_id":"call_future"}
	]`)

	got, result, err := NormalizeResponsesInputItemIDs(input)
	require.NoError(t, err)
	require.Equal(t, 5, result.RemovedCount)

	var items []map[string]json.RawMessage
	require.NoError(t, rootcommon.Unmarshal(got, &items))
	require.Len(t, items, 6)
	for _, index := range []int{0, 1, 2, 3, 4} {
		_, hasID := items[index]["id"]
		require.False(t, hasID, "item index %d still has an invalid id", index)
	}
	require.Equal(t, `"call_null"`, string(items[0]["call_id"]))
	require.Equal(t, `"call_number"`, string(items[1]["call_id"]))
	require.Equal(t, `"call_object"`, string(items[2]["call_id"]))
	require.Equal(t, `"call_search"`, string(items[3]["call_id"]))
	require.Equal(t, `"call_future"`, string(items[5]["call_id"]))
	require.Equal(t, `42`, string(items[5]["id"]))
}

func TestNormalizeResponsesInputItemIDsPreservesIDsForUnknownTypes(t *testing.T) {
	input := rootcommon.RawMessage(`[
		{"type":"future_item","id":"rs_future"},
		{"type":"future_item","id":"legacy"},
		{"type":"future_item","id":42},
		{"type":"compaction_trigger","id":"cmp_trigger"},
		{"type":"other","id":"other_id"},
		{"type":"other","id":"future_prefix_value"}
	]`)

	got, result, err := NormalizeResponsesInputItemIDs(input)
	require.NoError(t, err)
	require.Zero(t, result.RemovedCount)
	require.JSONEq(t, string(input), string(got))
}

func TestNormalizeResponsesInputItemIDsKeepsMsgPrefixRegardlessOfSuffix(t *testing.T) {
	input := rootcommon.RawMessage(`[
		{"type":"message","id":"MSG_1","role":"user","content":[]},
		{"type":"message","id":"msg_1","role":"user","content":[]},
		{"type":"message","id":"msg_","role":"user","content":[]},
		{"type":"message","id":"msg_1_extra","role":"user","content":[]}
	]`)

	got, result, err := NormalizeResponsesInputItemIDs(input)
	require.NoError(t, err)
	require.Equal(t, 2, result.RemovedCount)

	var items []map[string]json.RawMessage
	require.NoError(t, rootcommon.Unmarshal(got, &items))
	require.Len(t, items, 4)
	_, firstHasID := items[0]["id"]
	_, secondHasID := items[1]["id"]
	_, thirdHasID := items[2]["id"]
	_, fourthHasID := items[3]["id"]
	require.False(t, firstHasID)
	require.True(t, secondHasID)
	require.False(t, thirdHasID)
	require.True(t, fourthHasID)
}

func TestNormalizeResponsesInputItemIDsKeepsFutureItemWithGenericPrefix(t *testing.T) {
	input := rootcommon.RawMessage(`[
		{"type":"shell_call","id":"sc_future"},
		{"type":"apply_patch_call","id":"ap_future"},
		{"type":"mcp_call","id":"mcp_future"},
		{"type":"multi_agent_call_output","id":"ma_future"},
		{"type":"future_item","id":"server_future"}
	]`)

	got, result, err := NormalizeResponsesInputItemIDs(input)
	require.NoError(t, err)
	require.Zero(t, result.RemovedCount)
	require.JSONEq(t, string(input), string(got))
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

func TestResponsesRequestHasFunctionToolsIncludesLoadedToolSearchTools(t *testing.T) {
	request := &dto.OpenAIResponsesRequest{
		Input: rootcommon.RawMessage(`[
			{"type":"tool_search_call","call_id":"call_search","execution":"client","arguments":"{\"query\":\"deploy\"}"},
			{"type":"tool_search_output","call_id":"call_search","status":"completed","execution":"client","tools":[
				{"type":"function","name":"loaded_deploy","parameters":{"type":"object"}}
			]}
		]`),
	}

	hasFunctionTools, err := ResponsesRequestHasFunctionTools(request)

	require.NoError(t, err)
	require.True(t, hasFunctionTools)
}

func TestResponsesRequestHasNamespaceToolsIncludesOfficialLoadedToolSearchNamespace(t *testing.T) {
	request := &dto.OpenAIResponsesRequest{
		Input: rootcommon.RawMessage(`[
			{"type":"tool_search_call","id":"tsc_search","call_id":"call_search","execution":"client","arguments":"{\"query\":\"calendar\"}"},
			{"type":"tool_search_output","id":"tso_search","call_id":"call_search","status":"completed","execution":"client","tools":[
				{"type":"namespace","name":"mcp__calendar","tools":[
					{"type":"function","name":"create_event","defer_loading":true,"parameters":{"type":"object"}}
				]}
			]}
		]`),
	}

	hasNamespaceTools, err := ResponsesRequestHasNamespaceTools(request)

	require.NoError(t, err)
	require.True(t, hasNamespaceTools)
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

func TestNormalizeContextCompactionForResponsesRequestConvertsSyntheticAndDropsEmptyMarkers(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: rootcommon.RawMessage(`[
			{"type":"context_compaction","encrypted_content":"newapi.synthetic.compact:v2:n1:resp_context"},
			{"type":"context_compaction"},
			{"type":"compaction","encrypted_content":"newapi.synthetic.compact:v2:n2:resp_native"},
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
		{"type":"compaction","encrypted_content":"newapi.synthetic.compact:v2:n1:resp_context"},
		{"type":"compaction","encrypted_content":"newapi.synthetic.compact:v2:n2:resp_native"},
		{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
	]`, string(got.Input))
}

func TestNormalizeContextCompactionForResponsesRequestPreservesOpaqueInput(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: rootcommon.RawMessage(`[
			{"type":"context_compaction","encrypted_content":"opaque-context-state"},
			{"type":"context_compaction"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	got, result, err := NormalizeContextCompactionForResponsesRequest(req)

	require.NoError(t, err)
	require.Equal(t, req.Input, got.Input)
	require.Zero(t, result.ChangedCount())
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

func TestNormalizeContextCompactionForResponsesRequestPreservesNonStringEncryptedContent(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: rootcommon.RawMessage(`[
			{"type":"context_compaction","encrypted_content":{"unexpected":true}},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	got, result, err := NormalizeContextCompactionForResponsesRequest(req)

	require.NoError(t, err)
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

func TestStripResponsesInputEncryptedContentItemKeepsSoleReasoningItem(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.6",
		Input: rootcommon.RawMessage(`[
			{"type":"reasoning","encrypted_content":"opaque"}
		]`),
	}

	got, removed, err := StripResponsesInputEncryptedContentItem(req, "input[0].encrypted_content", "")

	require.NoError(t, err)
	require.False(t, removed)
	require.Equal(t, req.Input, got.Input)
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

	for _, invalidItem := range []string{
		`{"type":"function_call_output","output":"done"}`,
		`{"type":"function_call_output","call_id":"call_1","output":""}`,
		`{"type":"function_call_output","call_id":"call_1","output":[]}`,
		`{"type":"function_call_output","call_id":"call_1","output":{}}`,
	} {
		req.Input = rootcommon.RawMessage("[" + invalidItem + "]")
		_, valid, err = ValidResponsesToolOutputEncryptedContentTarget(req, "input[0].encrypted_content", "")
		require.NoError(t, err)
		require.False(t, valid, invalidItem)
	}
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
			name:    "top level declaration",
			request: &dto.OpenAIResponsesRequest{Tools: rootcommon.RawMessage(`[{"type":"tool_search","execution":"client"}]`)},
			want:    true,
		},
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
