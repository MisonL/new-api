package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestIsDeepSeekModelNameRecognizesProviderAliases(t *testing.T) {
	for _, model := range []string{
		"deepseek-v4-flash",
		"deepseek-ai/DeepSeek-R1-0528",
		"provider/deepseek-v4-pro",
		"openrouter/deepseek/deepseek-r1",
		"provider:deepseek-v4-flash",
		"bedrock.deepseek.r1-v1:0",
		"bedrock.deepseek-r1",
	} {
		if !IsDeepSeekModelName(model) {
			t.Errorf("model %q should be recognized", model)
		}
	}
	for _, model := range []string{"notdeepseek-model", "notdeepseek.model", "some-provider/gpt-5.6"} {
		if IsDeepSeekModelName(model) {
			t.Errorf("model %q should not be recognized", model)
		}
	}
}

func TestGeminiToOpenAIRequestPreservesThoughtTextAndFunctionResponseID(t *testing.T) {
	responseID, err := common.Marshal("call_weather")
	require.NoError(t, err)

	request := &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{
			{
				Role: "model",
				Parts: []dto.GeminiPart{
					{Thought: true, Text: "I need the weather tool."},
					{Text: "I will check it."},
					{FunctionCall: &dto.FunctionCall{
						ID:           "call_weather",
						FunctionName: "get_weather",
						Arguments:    map[string]any{"city": "Hangzhou"},
					}},
				},
			},
			{
				Role: "user",
				Parts: []dto.GeminiPart{{FunctionResponse: &dto.GeminiFunctionResponse{
					ID:       responseID,
					Name:     "get_weather",
					Response: map[string]interface{}{"temperature": 24},
				}}},
			},
		},
	}

	openAIRequest, err := GeminiToOpenAIRequest(request, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-luna"}})
	require.NoError(t, err)
	require.Len(t, openAIRequest.Messages, 2)

	assistant := openAIRequest.Messages[0]
	require.Equal(t, "assistant", assistant.Role)
	require.Equal(t, "I will check it.", assistant.StringContent())
	require.Equal(t, "I need the weather tool.", assistant.GetReasoningContent())
	calls := assistant.ParseToolCalls()
	require.Len(t, calls, 1)
	require.Equal(t, "call_weather", calls[0].ID)
	require.Equal(t, "get_weather", calls[0].Function.Name)

	toolMessage := openAIRequest.Messages[1]
	require.Equal(t, "tool", toolMessage.Role)
	require.Equal(t, "call_weather", toolMessage.ToolCallId)
	require.JSONEq(t, `{"temperature":24}`, toolMessage.StringContent())
}

func TestGeminiToOpenAIRequestPreservesTextAlongsideFunctionCall(t *testing.T) {
	request := &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{{
			Role: "model",
			Parts: []dto.GeminiPart{
				{Text: "Before calling."},
				{FunctionCall: &dto.FunctionCall{
					FunctionName: "lookup",
					Arguments:    map[string]any{"q": "status"},
				}},
			},
		}},
	}

	openAIRequest, err := GeminiToOpenAIRequest(request, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-luna"}})
	require.NoError(t, err)
	require.Len(t, openAIRequest.Messages, 1)
	require.Equal(t, "Before calling.", openAIRequest.Messages[0].StringContent())
	require.Len(t, openAIRequest.Messages[0].ParseToolCalls(), 1)
}

func TestGeminiToOpenAIRequestPreservesFunctionResponseAndTextOrder(t *testing.T) {
	responseID, err := common.Marshal("call_lookup")
	require.NoError(t, err)
	request := &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{
			{Role: "model", Parts: []dto.GeminiPart{{FunctionCall: &dto.FunctionCall{
				ID:           "call_lookup",
				FunctionName: "lookup",
				Arguments:    map[string]any{"q": "status"},
			}}}},
			{Role: "user", Parts: []dto.GeminiPart{
				{FunctionResponse: &dto.GeminiFunctionResponse{
					ID:       responseID,
					Name:     "lookup",
					Response: map[string]any{"ok": true},
				}},
				{Text: "Now summarize the result."},
			}},
		},
	}

	converted, err := GeminiToOpenAIRequest(request, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-luna"}})
	require.NoError(t, err)
	require.Len(t, converted.Messages, 3)
	require.Equal(t, "assistant", converted.Messages[0].Role)
	require.Equal(t, "tool", converted.Messages[1].Role)
	require.Equal(t, "call_lookup", converted.Messages[1].ToolCallId)
	require.Equal(t, "user", converted.Messages[2].Role)
	require.Equal(t, "Now summarize the result.", converted.Messages[2].StringContent())
}

func TestGeminiToOpenAIRequestRejectsUnknownFunctionResponseID(t *testing.T) {
	responseID, err := common.Marshal("call_missing")
	require.NoError(t, err)
	request := &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{{
			Role: "user",
			Parts: []dto.GeminiPart{{FunctionResponse: &dto.GeminiFunctionResponse{
				ID:       responseID,
				Name:     "lookup",
				Response: map[string]any{"ok": true},
			}}},
		}},
	}

	_, err = GeminiToOpenAIRequest(request, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-luna"}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown functionCall id")
}

func TestGeminiToOpenAIRequestRejectsFunctionResponseNameMismatch(t *testing.T) {
	responseID, err := common.Marshal("call_lookup")
	require.NoError(t, err)
	request := &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{
			{Role: "model", Parts: []dto.GeminiPart{{FunctionCall: &dto.FunctionCall{
				ID:           "call_lookup",
				FunctionName: "lookup",
				Arguments:    map[string]any{},
			}}}},
			{Role: "user", Parts: []dto.GeminiPart{{FunctionResponse: &dto.GeminiFunctionResponse{
				ID:       responseID,
				Name:     "other",
				Response: map[string]any{"ok": true},
			}}}},
		},
	}

	_, err = GeminiToOpenAIRequest(request, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-luna"}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not match functionCall")
}

func TestResponseOpenAI2GeminiPreservesReasoningTextAndToolCalls(t *testing.T) {
	reasoning := "decide before calling"
	message := dto.Message{Role: "assistant", ReasoningContent: &reasoning, Content: "visible"}
	message.SetToolCalls([]dto.ToolCallRequest{{
		ID:   "call_1",
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      "lookup",
			Arguments: `{"q":"status"}`,
		},
	}})

	converted := ResponseOpenAI2Gemini(&dto.OpenAITextResponse{
		Model: "gpt-5.6-luna",
		Choices: []dto.OpenAITextResponseChoice{{
			Index:        0,
			Message:      message,
			FinishReason: "tool_calls",
		}},
	}, &relaycommon.RelayInfo{})

	require.Len(t, converted.Candidates, 1)
	parts := converted.Candidates[0].Content.Parts
	require.Len(t, parts, 3)
	require.True(t, parts[0].Thought)
	require.Equal(t, reasoning, parts[0].Text)
	require.Equal(t, "visible", parts[1].Text)
	require.NotNil(t, parts[2].FunctionCall)
}

func TestStreamResponseOpenAI2GeminiPreservesReasoningTextAndToolCalls(t *testing.T) {
	reasoning := "thinking"
	content := "answer"
	response := StreamResponseOpenAI2Gemini(&dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index: 0,
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				Content:          &content,
				ReasoningContent: &reasoning,
				ToolCalls: []dto.ToolCallResponse{{
					ID:   "call_1",
					Type: "function",
					Function: dto.FunctionResponse{
						Name:      "lookup",
						Arguments: `{"q":"status"}`,
					},
				}},
			},
		}},
	}, &relaycommon.RelayInfo{})

	require.NotNil(t, response)
	require.Len(t, response.Candidates, 1)
	parts := response.Candidates[0].Content.Parts
	require.Len(t, parts, 3)
	require.True(t, parts[0].Thought)
	require.Equal(t, reasoning, parts[0].Text)
	require.Equal(t, content, parts[1].Text)
	require.NotNil(t, parts[2].FunctionCall)
}

func TestStreamResponseOpenAI2GeminiRejectsUnknownFinishReason(t *testing.T) {
	finishReason := "refusal"
	converted, err := StreamResponseOpenAI2GeminiWithError(&dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			FinishReason: &finishReason,
		}},
	}, &relaycommon.RelayInfo{})

	require.Nil(t, converted)
	require.Error(t, err)
	require.Contains(t, err.Error(), "finish_reason")
}

func TestStreamResponseOpenAI2GeminiRejectsIncompleteToolCall(t *testing.T) {
	converted, err := StreamResponseOpenAI2GeminiWithError(&dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				ToolCalls: []dto.ToolCallResponse{{
					ID: "call_1",
					Function: dto.FunctionResponse{
						Arguments: `{"q":"status"}`,
					},
				}},
			},
		}},
	}, &relaycommon.RelayInfo{})

	require.Nil(t, converted)
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires id and function name")
}

func TestStreamResponseOpenAI2GeminiBuffersToolArgumentDeltas(t *testing.T) {
	info := &relaycommon.RelayInfo{}
	firstChunk := &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index: 0,
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				ToolCalls: []dto.ToolCallResponse{{
					Index: common.GetPointer(0),
					ID:    "call_1",
					Type:  "function",
					Function: dto.FunctionResponse{
						Name:      "lookup",
						Arguments: `{"q"`,
					},
				}},
			},
		}},
	}
	converted, err := StreamResponseOpenAI2GeminiWithError(firstChunk, info)
	require.NoError(t, err)
	require.Nil(t, converted)

	secondChunk := &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index: 0,
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				ToolCalls: []dto.ToolCallResponse{{
					Index:    common.GetPointer(0),
					Function: dto.FunctionResponse{Arguments: `:"status"}`},
				}},
			},
		}},
	}
	converted, err = StreamResponseOpenAI2GeminiWithError(secondChunk, info)
	require.NoError(t, err)
	require.NotNil(t, converted)
	require.Len(t, converted.Candidates, 1)
	require.Len(t, converted.Candidates[0].Content.Parts, 1)
	require.Equal(t, "call_1", converted.Candidates[0].Content.Parts[0].FunctionCall.ID)
	require.Equal(t, "lookup", converted.Candidates[0].Content.Parts[0].FunctionCall.FunctionName)
	require.Equal(t, map[string]interface{}{"q": "status"}, converted.Candidates[0].Content.Parts[0].FunctionCall.Arguments)

	finishReason := "tool_calls"
	converted, err = StreamResponseOpenAI2GeminiWithError(&dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index:        0,
			FinishReason: &finishReason,
		}},
	}, info)
	require.NoError(t, err)
	require.NotNil(t, converted)
	require.Len(t, converted.Candidates, 1)
	require.Equal(t, "STOP", *converted.Candidates[0].FinishReason)
}

func TestStreamResponseOpenAI2GeminiRejectsIncompleteToolArgumentsAtFinish(t *testing.T) {
	info := &relaycommon.RelayInfo{}
	_, err := StreamResponseOpenAI2GeminiWithError(&dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index: 0,
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				ToolCalls: []dto.ToolCallResponse{{
					Index: common.GetPointer(0),
					ID:    "call_1",
					Type:  "function",
					Function: dto.FunctionResponse{
						Name:      "lookup",
						Arguments: `{"q"`,
					},
				}},
			},
		}},
	}, info)
	require.NoError(t, err)

	finishReason := "tool_calls"
	converted, err := StreamResponseOpenAI2GeminiWithError(&dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index:        0,
			FinishReason: &finishReason,
		}},
	}, info)
	require.Nil(t, converted)
	require.Error(t, err)
	require.Contains(t, err.Error(), "complete JSON object")
}

func TestStreamResponseOpenAI2GeminiPreservesUsageOnlyChunk(t *testing.T) {
	converted, err := StreamResponseOpenAI2GeminiWithError(&dto.ChatCompletionsStreamResponse{
		Usage: &dto.Usage{PromptTokens: 11, CompletionTokens: 7, TotalTokens: 18},
	}, &relaycommon.RelayInfo{})

	require.NoError(t, err)
	require.NotNil(t, converted)
	require.Empty(t, converted.Candidates)
	require.Equal(t, 11, converted.UsageMetadata.PromptTokenCount)
	require.Equal(t, 7, converted.UsageMetadata.CandidatesTokenCount)
	require.Equal(t, 18, converted.UsageMetadata.TotalTokenCount)
}

func TestClaudeToOpenAIRequestRejectsMalformedToolList(t *testing.T) {
	_, err := ClaudeToOpenAIRequest(dto.ClaudeRequest{
		Model: "gpt-5.6-luna",
		Tools: map[string]any{"name": "lookup"},
	}, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-luna"}})

	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to parse Claude tools")
}

func TestToJSONStringReturnsSerializationError(t *testing.T) {
	_, err := toJSONString(make(chan int))
	require.Error(t, err)
}

func TestGeminiToOpenAIRequestRejectsMalformedTools(t *testing.T) {
	request := &dto.GeminiChatRequest{
		Tools: common.RawMessage(`{"functionDeclarations":"invalid"}`),
	}

	_, err := GeminiToOpenAIRequest(request, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-luna"}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "functionDeclarations")
}

func TestClaudeToOpenAIRequestNormalizesNilToolInputToObject(t *testing.T) {
	request := dto.ClaudeRequest{
		Model: "gpt-5.6-luna",
		Messages: []dto.ClaudeMessage{{
			Role: "assistant",
			Content: []dto.ClaudeMediaMessage{{
				Type: "tool_use",
				Id:   "toolu_1",
				Name: "lookup",
			}},
		}},
	}

	converted, err := ClaudeToOpenAIRequest(request, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-luna"}})
	require.NoError(t, err)
	calls := converted.Messages[0].ParseToolCalls()
	require.Len(t, calls, 1)
	require.JSONEq(t, `{}`, calls[0].Function.Arguments)
}

func TestGeminiToOpenAIRequestMapsGenerationParameters(t *testing.T) {
	topP := 0.0
	topK := 16.0
	maxTokens := uint(128)
	presencePenalty := float32(-0.25)
	frequencyPenalty := float32(0.5)
	candidateCount := 1
	request := &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{{Role: "user", Parts: []dto.GeminiPart{{Text: "hello"}}}},
		GenerationConfig: dto.GeminiChatGenerationConfig{
			TopP:             &topP,
			TopK:             &topK,
			MaxOutputTokens:  &maxTokens,
			PresencePenalty:  &presencePenalty,
			FrequencyPenalty: &frequencyPenalty,
			CandidateCount:   &candidateCount,
		},
	}

	converted, err := GeminiToOpenAIRequest(request, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-luna"}})
	require.NoError(t, err)
	require.NotNil(t, converted.TopP)
	require.Equal(t, 0.0, *converted.TopP)
	require.NotNil(t, converted.TopK)
	require.Equal(t, 16, *converted.TopK)
	require.Equal(t, maxTokens, *converted.MaxTokens)
	require.Equal(t, -0.25, *converted.PresencePenalty)
	require.Equal(t, 0.5, *converted.FrequencyPenalty)
	require.Equal(t, 1, *converted.N)
}

func TestGeminiToOpenAIRequestRejectsNonObjectResponseSchema(t *testing.T) {
	_, err := GeminiToOpenAIRequest(&dto.GeminiChatRequest{
		GenerationConfig: dto.GeminiChatGenerationConfig{ResponseSchema: "invalid"},
	}, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-luna"}})

	require.Error(t, err)
	require.Contains(t, err.Error(), "response schema")
}

func TestGeminiToOpenAIRequestRejectsUnrepresentableToolConfigFields(t *testing.T) {
	_, err := GeminiToOpenAIRequest(&dto.GeminiChatRequest{
		ToolConfig: &dto.ToolConfig{
			FunctionCallingConfig: &dto.FunctionCallingConfig{Mode: "AUTO", AllowedFunctionNames: []string{"lookup"}},
		},
	}, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-luna"}})

	require.Error(t, err)
	require.Contains(t, err.Error(), "allowedFunctionNames")
}

func TestGeminiToOpenAIRequestRejectsFunctionResponseMetadata(t *testing.T) {
	_, err := GeminiToOpenAIRequest(&dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{{
			Role: "user",
			Parts: []dto.GeminiPart{{FunctionResponse: &dto.GeminiFunctionResponse{
				Name:         "lookup",
				Response:     map[string]interface{}{"ok": true},
				WillContinue: common.RawMessage(`true`),
			}}},
		}},
	}, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-luna"}})

	require.Error(t, err)
	require.Contains(t, err.Error(), "willContinue")
}

func TestResponseOpenAI2GeminiRejectsMalformedToolArguments(t *testing.T) {
	message := dto.Message{Role: "assistant", Content: ""}
	message.SetToolCalls([]dto.ToolCallRequest{{
		ID:   "call_bad",
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      "lookup",
			Arguments: "not-json",
		},
	}})

	converted, err := ResponseOpenAI2GeminiWithError(&dto.OpenAITextResponse{
		Model:   "gpt-5.6-luna",
		Choices: []dto.OpenAITextResponseChoice{{Message: message, FinishReason: "tool_calls"}},
	}, &relaycommon.RelayInfo{})

	require.Nil(t, converted)
	require.Error(t, err)
	require.Contains(t, err.Error(), "arguments must be a JSON object")
}
