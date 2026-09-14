package zhipu

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestRequestOpenAI2ZhipuPreservesTextHistory(t *testing.T) {
	converted, err := requestOpenAI2Zhipu(dto.GeneralOpenAIRequest{Messages: []dto.Message{
		{Role: "system", Content: "Policy."},
		{Role: "user", Content: "Question"},
		{Role: "assistant", Content: "Answer"},
	}})
	require.NoError(t, err)
	require.Equal(t, []ZhipuMessage{
		{Role: "system", Content: "Policy."},
		{Role: "user", Content: "Okay"},
		{Role: "user", Content: "Question"},
		{Role: "assistant", Content: "Answer"},
	}, converted.Prompt)
}

func TestRequestOpenAI2ZhipuRejectsUnrepresentableContent(t *testing.T) {
	_, err := requestOpenAI2Zhipu(dto.GeneralOpenAIRequest{Messages: []dto.Message{{
		Role:    "user",
		Content: common.RawMessage(`[{"type":"file","file":{"file_id":"file-1"}}]`),
	}}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "file")
}

func TestRequestOpenAI2ZhipuRejectsUnsupportedOptions(t *testing.T) {
	maxTokens := uint(32)
	_, err := requestOpenAI2Zhipu(dto.GeneralOpenAIRequest{
		MaxTokens: &maxTokens,
		Messages:  []dto.Message{{Role: "user", Content: "Hello"}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "options")
}
