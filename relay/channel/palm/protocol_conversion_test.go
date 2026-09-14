package palm

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestRequestOpenAI2PaLMPreservesContextAndAlternatingHistory(t *testing.T) {
	converted, err := requestOpenAI2PaLM(dto.GeneralOpenAIRequest{Messages: []dto.Message{
		{Role: "system", Content: "Policy."},
		{Role: "user", Content: "Question"},
		{Role: "assistant", Content: "Answer"},
		{Role: "user", Content: "Follow-up"},
	}})
	require.NoError(t, err)
	require.Equal(t, "Policy.", converted.Prompt.Context)
	require.Equal(t, []PaLMChatMessage{
		{Author: "user", Content: "Question"},
		{Author: "assistant", Content: "Answer"},
		{Author: "user", Content: "Follow-up"},
	}, converted.Prompt.Messages)
}

func TestRequestOpenAI2PaLMRejectsNonTextContent(t *testing.T) {
	_, err := requestOpenAI2PaLM(dto.GeneralOpenAIRequest{Messages: []dto.Message{{
		Role:    "user",
		Content: common.RawMessage(`[{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]`),
	}}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "image_url")
}

func TestRequestOpenAI2PaLMMapsCandidateCount(t *testing.T) {
	n := 2
	converted, err := requestOpenAI2PaLM(dto.GeneralOpenAIRequest{
		N:        &n,
		Messages: []dto.Message{{Role: "user", Content: "Hello"}},
	})
	require.NoError(t, err)
	require.NotNil(t, converted.CandidateCount)
	require.Equal(t, 2, *converted.CandidateCount)
}
