package gemini

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponseGeminiChat2OpenAIPreservesThoughtTextAndToolCalls(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	response := &dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Index: 0,
			Content: dto.GeminiChatContent{
				Role: "model",
				Parts: []dto.GeminiPart{
					{Thought: true, Text: "first thought"},
					{Text: "visible text"},
					{Thought: true, Text: "second thought"},
					{FunctionCall: &dto.FunctionCall{
						ID:           "call_original",
						FunctionName: "lookup",
						Arguments:    map[string]any{"q": "status"},
					}},
				},
			},
		}},
	}

	converted := responseGeminiChat2OpenAI(c, response)
	require.Len(t, converted.Choices, 1)
	choice := converted.Choices[0]
	require.Equal(t, "visible text", choice.Message.StringContent())
	require.Equal(t, "first thought\nsecond thought", choice.Message.GetReasoningContent())
	require.Equal(t, "tool_calls", choice.FinishReason)
	calls := choice.Message.ParseToolCalls()
	require.Len(t, calls, 1)
	require.Equal(t, "call_original", calls[0].ID)
}

func TestStreamResponseGeminiChat2OpenAIPreservesMixedThoughtAndText(t *testing.T) {
	response, isStop := streamResponseGeminiChat2OpenAI(&dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Index: 0,
			Content: dto.GeminiChatContent{
				Role: "model",
				Parts: []dto.GeminiPart{
					{Thought: true, Text: "thinking"},
					{Text: "answer"},
				},
			},
		}},
	})

	require.False(t, isStop)
	require.Len(t, response.Choices, 1)
	delta := response.Choices[0].Delta
	require.Equal(t, "answer", delta.GetContentString())
	require.Equal(t, "thinking", delta.GetReasoningContent())
}

func TestConvertOpenAI2GeminiPreservesReasoningAndToolCallID(t *testing.T) {
	reasoning := "decide before calling"
	message := dto.Message{Role: "assistant", ReasoningContent: &reasoning}
	message.SetToolCalls([]dto.ToolCallRequest{{
		ID:   "call_123",
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      "lookup",
			Arguments: `{"q":"status"}`,
		},
	}})

	converted, err := CovertOpenAI2Gemini(nil, dto.GeneralOpenAIRequest{
		Model:    "gemini-2.5-flash",
		Messages: []dto.Message{{Role: "user", Content: "check"}, message},
	}, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			UpstreamModelName: "gemini-2.5-flash",
		},
	})
	require.NoError(t, err)
	require.Len(t, converted.Contents, 2)
	require.Equal(t, "model", converted.Contents[1].Role)
	require.Len(t, converted.Contents[1].Parts, 2)
	require.True(t, converted.Contents[1].Parts[0].Thought)
	require.Equal(t, reasoning, converted.Contents[1].Parts[0].Text)
	require.Equal(t, "call_123", converted.Contents[1].Parts[1].FunctionCall.ID)
}

func TestConvertOpenAI2GeminiPreservesThinkingContentAlias(t *testing.T) {
	thinking := "alias reasoning"
	converted, err := CovertOpenAI2Gemini(nil, dto.GeneralOpenAIRequest{
		Model: "gemini-2.5-flash",
		Messages: []dto.Message{
			{Role: "user", Content: "check"},
			{Role: "assistant", Content: "answer", ThinkingContent: &thinking},
		},
	}, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			UpstreamModelName: "gemini-2.5-flash",
		},
	})

	require.NoError(t, err)
	require.Len(t, converted.Contents, 2)
	require.True(t, converted.Contents[1].Parts[0].Thought)
	require.Equal(t, thinking, converted.Contents[1].Parts[0].Text)
}

func TestConvertOpenAI2GeminiMapsSamplingAndPenaltyParameters(t *testing.T) {
	topP := 0.0
	topK := 12
	presencePenalty := -0.5
	frequencyPenalty := 0.75
	candidateCount := 1

	converted, err := CovertOpenAI2Gemini(nil, dto.GeneralOpenAIRequest{
		Model:            "gemini-2.5-flash",
		TopP:             &topP,
		TopK:             &topK,
		PresencePenalty:  &presencePenalty,
		FrequencyPenalty: &frequencyPenalty,
		N:                &candidateCount,
		Messages:         []dto.Message{{Role: "user", Content: "hello"}},
	}, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			UpstreamModelName: "gemini-2.5-flash",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, converted.GenerationConfig.TopP)
	require.Equal(t, 0.0, *converted.GenerationConfig.TopP)
	require.NotNil(t, converted.GenerationConfig.TopK)
	require.Equal(t, 12.0, *converted.GenerationConfig.TopK)
	require.NotNil(t, converted.GenerationConfig.PresencePenalty)
	require.Equal(t, float32(-0.5), *converted.GenerationConfig.PresencePenalty)
	require.NotNil(t, converted.GenerationConfig.FrequencyPenalty)
	require.Equal(t, float32(0.75), *converted.GenerationConfig.FrequencyPenalty)
	require.NotNil(t, converted.GenerationConfig.CandidateCount)
	require.Equal(t, 1, *converted.GenerationConfig.CandidateCount)
}

func TestConvertOpenAI2GeminiCleansRawJSONToolParameters(t *testing.T) {
	converted, err := CovertOpenAI2Gemini(nil, dto.GeneralOpenAIRequest{
		Model: "gemini-2.5-flash",
		Tools: []dto.ToolCallRequest{{
			Type: "function",
			Function: dto.FunctionRequest{
				Name:       "lookup",
				Parameters: common.RawMessage(`{"type":"object","properties":{"q":{"type":"string","additionalProperties":false}},"additionalProperties":false}`),
			},
		}},
		Messages: []dto.Message{{Role: "user", Content: "hello"}},
	}, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			UpstreamModelName: "gemini-2.5-flash",
		},
	})

	require.NoError(t, err)
	tools := converted.GetTools()
	require.Len(t, tools, 1)
	declarations, err := common.Any2Type[[]dto.FunctionRequest](tools[0].FunctionDeclarations)
	require.NoError(t, err)
	require.Len(t, declarations, 1)
	parameters, ok := declarations[0].Parameters.(map[string]interface{})
	require.True(t, ok)
	_, hasAdditionalProperties := parameters["additionalProperties"]
	require.False(t, hasAdditionalProperties)
}

func TestConvertOpenAI2GeminiPreservesTextAfterMarkdownImage(t *testing.T) {
	converted, err := CovertOpenAI2Gemini(nil, dto.GeneralOpenAIRequest{
		Model:    "gemini-2.5-flash",
		Messages: []dto.Message{{Role: "user", Content: "before ![image](data:image/png;base64,AA==) after"}},
	}, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			UpstreamModelName: "gemini-2.5-flash",
		},
	})

	require.NoError(t, err)
	parts := converted.Contents[0].Parts
	require.Len(t, parts, 3)
	require.Equal(t, "before ", parts[0].Text)
	require.NotNil(t, parts[1].InlineData)
	require.Equal(t, " after", parts[2].Text)
}

func TestConvertOpenAI2GeminiRejectsUnresolvableToolResponseName(t *testing.T) {
	_, err := CovertOpenAI2Gemini(nil, dto.GeneralOpenAIRequest{
		Model: "gemini-2.5-flash",
		Messages: []dto.Message{{
			Role:       "tool",
			ToolCallId: "call_missing",
			Content:    "result",
		}},
	}, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			UpstreamModelName: "gemini-2.5-flash",
		},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "function name is required")
}

func TestConvertOpenAI2GeminiRejectsUnknownMessageRole(t *testing.T) {
	_, err := CovertOpenAI2Gemini(nil, dto.GeneralOpenAIRequest{
		Model:    "gemini-2.5-flash",
		Messages: []dto.Message{{Role: "developer_tool", Content: "hello"}},
	}, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			UpstreamModelName: "gemini-2.5-flash",
		},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "role")
}

func TestConvertOpenAI2GeminiRejectsMalformedContentArray(t *testing.T) {
	_, err := CovertOpenAI2Gemini(nil, dto.GeneralOpenAIRequest{
		Model: "gemini-2.5-flash",
		Messages: []dto.Message{{
			Role:    "user",
			Content: []any{"not-an-object"},
		}},
	}, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			UpstreamModelName: "gemini-2.5-flash",
		},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "content[0]")
}

func TestConvertOpenAI2GeminiRejectsNonTextSystemContent(t *testing.T) {
	_, err := CovertOpenAI2Gemini(nil, dto.GeneralOpenAIRequest{
		Model: "gemini-2.5-flash",
		Messages: []dto.Message{{
			Role: "system",
			Content: []any{map[string]any{
				"type":      "image_url",
				"image_url": map[string]any{"url": "https://example.com/image.png"},
			}},
		}},
	}, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			UpstreamModelName: "gemini-2.5-flash",
		},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "systemInstruction")
}

func TestConvertOpenAI2GeminiRejectsUnsupportedToolType(t *testing.T) {
	_, err := CovertOpenAI2Gemini(nil, dto.GeneralOpenAIRequest{
		Model: "gemini-2.5-flash",
		Tools: []dto.ToolCallRequest{{
			Type:     "custom",
			Function: dto.FunctionRequest{Name: "lookup"},
		}},
		Messages: []dto.Message{{Role: "user", Content: "hello"}},
	}, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			UpstreamModelName: "gemini-2.5-flash",
		},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "not supported")
}

func TestConvertOpenAI2GeminiRejectsNonTextToolResponseContent(t *testing.T) {
	_, err := CovertOpenAI2Gemini(nil, dto.GeneralOpenAIRequest{
		Model: "gemini-2.5-flash",
		Messages: []dto.Message{
			{Role: "assistant", Content: "call", ToolCalls: common.RawMessage(`[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]`)},
			{Role: "tool", ToolCallId: "call_1", Content: []any{map[string]any{
				"type":      "image_url",
				"image_url": map[string]any{"url": "https://example.com/image.png"},
			}}},
		},
	}, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			UpstreamModelName: "gemini-2.5-flash",
		},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "functionResponse")
}

func TestStreamResponseGeminiChat2OpenAIUsesStableGeneratedToolCallIDs(t *testing.T) {
	response := func() *dto.GeminiChatResponse {
		return &dto.GeminiChatResponse{Candidates: []dto.GeminiChatCandidate{{
			Index: 0,
			Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{
				{FunctionCall: &dto.FunctionCall{FunctionName: "lookup", Arguments: map[string]any{"q": "status"}}},
				{FunctionCall: &dto.FunctionCall{FunctionName: "other", Arguments: map[string]any{"q": "status"}}},
			}},
		}}}
	}

	first, _ := streamResponseGeminiChat2OpenAI(response())
	second, _ := streamResponseGeminiChat2OpenAI(response())
	require.Len(t, first.Choices[0].Delta.ToolCalls, 2)
	require.Equal(t, first.Choices[0].Delta.ToolCalls[0].ID, second.Choices[0].Delta.ToolCalls[0].ID)
	require.Equal(t, first.Choices[0].Delta.ToolCalls[1].ID, second.Choices[0].Delta.ToolCalls[1].ID)
	require.NotEqual(t, first.Choices[0].Delta.ToolCalls[0].ID, first.Choices[0].Delta.ToolCalls[1].ID)
}

func TestGeminiAdaptorUpdatesYouTubeFileDataMimeType(t *testing.T) {
	request := &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{{
			Role: "user",
			Parts: []dto.GeminiPart{{
				FileData: &dto.GeminiFileData{FileUri: "https://www.youtube.com/watch?v=test"},
			}},
		}},
	}

	_, err := (&Adaptor{}).ConvertGeminiRequest(nil, nil, request)
	require.NoError(t, err)
	require.Equal(t, "video/webm", request.Contents[0].Parts[0].FileData.MimeType)
}
