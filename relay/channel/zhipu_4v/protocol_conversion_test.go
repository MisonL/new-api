package zhipu_4v

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestRequestOpenAI2ZhipuRejectsMalformedContent(t *testing.T) {
	_, err := requestOpenAI2Zhipu(dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{
			Role:    "user",
			Content: common.RawMessage(`[{"type":"text"}`),
		}},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "content")
}

func TestRequestOpenAI2ZhipuRejectsImageWithoutURL(t *testing.T) {
	_, err := requestOpenAI2Zhipu(dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{
			Role:    "user",
			Content: []any{map[string]any{"type": dto.ContentTypeImageURL}},
		}},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "image_url.url")
}

func TestRequestOpenAI2ZhipuRejectsMalformedToolCalls(t *testing.T) {
	_, err := requestOpenAI2Zhipu(dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{
			Role:      "assistant",
			Content:   "",
			ToolCalls: common.RawMessage(`{"type":"function"}`),
		}},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "tool_calls")
}

func TestRequestOpenAI2ZhipuPreservesMessageMetadata(t *testing.T) {
	name := "speaker"
	reasoning := "internal"
	converted, err := requestOpenAI2Zhipu(dto.GeneralOpenAIRequest{Messages: []dto.Message{{
		Role:             "assistant",
		Content:          "answer",
		Name:             &name,
		ReasoningContent: &reasoning,
	}}})
	require.NoError(t, err)
	require.Equal(t, &name, converted.Messages[0].Name)
	require.Equal(t, &reasoning, converted.Messages[0].ReasoningContent)
}

func TestRequestOpenAI2ZhipuRejectsUnsupportedMediaType(t *testing.T) {
	_, err := requestOpenAI2Zhipu(dto.GeneralOpenAIRequest{Messages: []dto.Message{{
		Role:    "user",
		Content: []any{map[string]any{"type": dto.ContentTypeInputAudio, "input_audio": map[string]any{"data": "AA==", "format": "wav"}}},
	}}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "input_audio")
}
