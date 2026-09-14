package mistral

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestRequestOpenAI2MistralRejectsMalformedContent(t *testing.T) {
	_, err := requestOpenAI2Mistral(&dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{
			Role:    "user",
			Content: common.RawMessage(`[{"type":"text"}`),
		}},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "content")
}

func TestRequestOpenAI2MistralRejectsMalformedToolCalls(t *testing.T) {
	_, err := requestOpenAI2Mistral(&dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{
			Role:      "assistant",
			Content:   "",
			ToolCalls: common.RawMessage(`{"type":"function"}`),
		}},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "tool_calls")
}

func TestRequestOpenAI2MistralPreservesRequestAndMessageFields(t *testing.T) {
	name := "speaker"
	reasoning := "internal"
	converted, err := requestOpenAI2Mistral(&dto.GeneralOpenAIRequest{
		Model:           "mistral-large-latest",
		ReasoningEffort: "low",
		Messages: []dto.Message{{
			Role:             "assistant",
			Content:          "answer",
			Name:             &name,
			ReasoningContent: &reasoning,
		}},
	})
	require.NoError(t, err)
	require.Equal(t, "mistral-large-latest", converted.Model)
	require.Equal(t, "low", converted.ReasoningEffort)
	require.Equal(t, &name, converted.Messages[0].Name)
	require.Equal(t, &reasoning, converted.Messages[0].ReasoningContent)
}

func TestRequestOpenAI2MistralMapsDeveloperRoleAndRejectsMissingImageURL(t *testing.T) {
	converted, err := requestOpenAI2Mistral(&dto.GeneralOpenAIRequest{Messages: []dto.Message{{
		Role: "developer", Content: "policy",
	}}})
	require.NoError(t, err)
	require.Equal(t, "system", converted.Messages[0].Role)

	_, err = requestOpenAI2Mistral(&dto.GeneralOpenAIRequest{Messages: []dto.Message{{
		Role:    "user",
		Content: []dto.MediaContent{{Type: dto.ContentTypeImageURL, ImageUrl: map[string]any{}}},
	}}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "image_url.url")
}

func TestRequestOpenAI2MistralMapsToolCallIDAcrossMessages(t *testing.T) {
	assistant := dto.Message{Role: "assistant", Content: ""}
	assistant.SetToolCalls([]dto.ToolCallRequest{{
		ID:   "long-tool-call-id",
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      "lookup",
			Arguments: `{"q":"status"}`,
		},
	}})
	converted, err := requestOpenAI2Mistral(&dto.GeneralOpenAIRequest{Messages: []dto.Message{
		assistant,
		{Role: "tool", ToolCallId: "long-tool-call-id", Content: "ready"},
	}})

	require.NoError(t, err)
	calls, err := converted.Messages[0].ParseToolCallsStrict()
	require.NoError(t, err)
	require.Len(t, calls, 1)
	require.Len(t, calls[0].ID, 9)
	require.Equal(t, calls[0].ID, converted.Messages[1].ToolCallId)
}

func TestConvertClaudeRequestReturnsExplicitUnsupportedError(t *testing.T) {
	_, err := (&Adaptor{}).ConvertClaudeRequest(nil, nil, &dto.ClaudeRequest{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not supported")
}
