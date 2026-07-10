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
