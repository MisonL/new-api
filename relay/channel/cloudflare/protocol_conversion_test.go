package cloudflare

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestConvertCf2CompletionsRequestRequiresPrompt(t *testing.T) {
	_, err := convertCf2CompletionsRequest(dto.GeneralOpenAIRequest{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "prompt")
}

func TestConvertCf2CompletionsRequestRejectsMessagesInsteadOfDroppingThem(t *testing.T) {
	_, err := convertCf2CompletionsRequest(dto.GeneralOpenAIRequest{Messages: []dto.Message{{
		Role: "user", Content: "Hello",
	}}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "messages")
}

func TestConvertCf2CompletionsRequestPreservesSinglePrompt(t *testing.T) {
	converted, err := convertCf2CompletionsRequest(dto.GeneralOpenAIRequest{Prompt: "Hello"})
	require.NoError(t, err)
	require.Equal(t, "Hello", converted.Prompt)
}

func TestConvertClaudeRequestReturnsExplicitUnsupportedError(t *testing.T) {
	_, err := (&Adaptor{}).ConvertClaudeRequest(nil, nil, &dto.ClaudeRequest{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not supported")
}
