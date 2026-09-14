package baidu

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestRequestOpenAI2BaiduPreservesLeadingSystemMessages(t *testing.T) {
	converted, err := requestOpenAI2Baidu(dto.GeneralOpenAIRequest{Messages: []dto.Message{
		{Role: "system", Content: "Policy."},
		{Role: "developer", Content: "Tone."},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi"},
	}})
	require.NoError(t, err)
	require.Equal(t, "Policy.\nTone.", converted.System)
	require.Equal(t, []BaiduMessage{{Role: "user", Content: "Hello"}, {Role: "assistant", Content: "Hi"}}, converted.Messages)
}

func TestRequestOpenAI2BaiduRejectsUnrepresentableContent(t *testing.T) {
	_, err := requestOpenAI2Baidu(dto.GeneralOpenAIRequest{Messages: []dto.Message{{
		Role:    "user",
		Content: common.RawMessage(`[{"type":"input_audio","input_audio":{"data":"AA==","format":"wav"}}]`),
	}}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "input_audio")
}

func TestRequestOpenAI2BaiduRejectsSystemMessageAfterConversation(t *testing.T) {
	_, err := requestOpenAI2Baidu(dto.GeneralOpenAIRequest{Messages: []dto.Message{
		{Role: "user", Content: "Hello"},
		{Role: "system", Content: "Late policy"},
	}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must precede")
}

func TestConvertClaudeRequestReturnsExplicitUnsupportedError(t *testing.T) {
	_, err := (&Adaptor{}).ConvertClaudeRequest(nil, nil, &dto.ClaudeRequest{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not supported")
}
