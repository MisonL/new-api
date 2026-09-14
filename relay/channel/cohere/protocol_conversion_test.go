package cohere

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestRequestOpenAI2CoherePreservesHistoryAndUsesOnlyFinalUserAsMessage(t *testing.T) {
	converted, err := requestOpenAI2Cohere(dto.GeneralOpenAIRequest{Messages: []dto.Message{
		{Role: "system", Content: "Follow the policy."},
		{Role: "user", Content: "First question"},
		{Role: "assistant", Content: "First answer"},
		{Role: "user", Content: "Second question"},
	}})
	require.NoError(t, err)
	require.Equal(t, "Second question", converted.Message)
	require.Equal(t, []ChatHistory{
		{Role: "SYSTEM", Message: "Follow the policy."},
		{Role: "USER", Message: "First question"},
		{Role: "CHATBOT", Message: "First answer"},
	}, converted.ChatHistory)
}

func TestRequestOpenAI2CohereRejectsUnrepresentableContent(t *testing.T) {
	_, err := requestOpenAI2Cohere(dto.GeneralOpenAIRequest{Messages: []dto.Message{{
		Role:    "user",
		Content: common.RawMessage(`[{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]`),
	}}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "image_url")
}

func TestRequestOpenAI2CohereRequiresFinalUserMessage(t *testing.T) {
	_, err := requestOpenAI2Cohere(dto.GeneralOpenAIRequest{Messages: []dto.Message{{
		Role: "user", Content: "question",
	}, {
		Role: "assistant", Content: "prefill",
	}}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "final message")
}
