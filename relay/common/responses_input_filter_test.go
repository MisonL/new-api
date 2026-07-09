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
