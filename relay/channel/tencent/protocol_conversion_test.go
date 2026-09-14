package tencent

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestRequestOpenAI2TencentPreservesProtocolOrder(t *testing.T) {
	converted, err := requestOpenAI2Tencent(dto.GeneralOpenAIRequest{Messages: []dto.Message{
		{Role: "system", Content: "Policy."},
		{Role: "user", Content: "Question"},
		{Role: "assistant", Content: "Answer"},
		{Role: "user", Content: "Follow-up"},
	}})
	require.NoError(t, err)
	require.Equal(t, []*TencentMessage{
		{Role: "system", Content: "Policy."},
		{Role: "user", Content: "Question"},
		{Role: "assistant", Content: "Answer"},
		{Role: "user", Content: "Follow-up"},
	}, converted.Messages)
}

func TestRequestOpenAI2TencentRejectsInvalidTurnOrder(t *testing.T) {
	_, err := requestOpenAI2Tencent(dto.GeneralOpenAIRequest{Messages: []dto.Message{
		{Role: "assistant", Content: "Answer"},
	}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected user")
}

func TestRequestOpenAI2TencentRejectsNonTextContent(t *testing.T) {
	_, err := requestOpenAI2Tencent(dto.GeneralOpenAIRequest{Messages: []dto.Message{
		{Role: "user", Content: []any{map[string]any{"type": dto.ContentTypeImageURL, "image_url": map[string]any{"url": "https://example.com/a.png"}}}},
	}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "image_url")
}

func TestConvertClaudeRequestReturnsExplicitUnsupportedError(t *testing.T) {
	_, err := (&Adaptor{}).ConvertClaudeRequest(nil, nil, &dto.ClaudeRequest{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not supported")
}
