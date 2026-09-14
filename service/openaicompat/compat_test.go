package openaicompat

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/stretchr/testify/require"
)

func mustMarshalJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := common.Marshal(value)
	require.NoError(t, err)
	return raw
}

func TestResponsesToolMergePreservesThinkingContentAlias(t *testing.T) {
	existingThinking := "existing"
	newThinking := " + new"
	messages := []dto.Message{{Role: "assistant", Content: "", ThinkingContent: &existingThinking}}
	toolMessage := dto.Message{Role: "assistant", Content: "", ThinkingContent: &newThinking}
	toolMessage.SetToolCalls([]dto.ToolCallRequest{{
		ID:       "call_1",
		Type:     "function",
		Function: dto.FunctionRequest{Name: "lookup", Arguments: `{}`},
	}})

	appendResponsesToolMessage(&messages, toolMessage)

	require.Len(t, messages, 1)
	require.Equal(t, "existing + new", messages[0].GetReasoningContent())
	require.NotNil(t, messages[0].ReasoningContent)
	require.Nil(t, messages[0].ThinkingContent)
	require.Len(t, messages[0].ParseToolCalls(), 1)
}

func TestProtocolConversionPolicySupportsLegacyAndRules(t *testing.T) {
	legacyPolicy := model_setting.ChatCompletionsToResponsesPolicy{
		Enabled:       true,
		AllChannels:   true,
		ModelPatterns: []string{"^gpt-5$"},
	}
	require.True(t, ShouldChatCompletionsUseResponsesPolicy(legacyPolicy, 1, 1, "gpt-5"))
	require.False(t, ShouldResponsesUseChatCompletionsPolicy(legacyPolicy, 1, 1, "gpt-5"))

	rulePolicy := model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: true,
		Rules: []model_setting.ProtocolConversionRule{
			{
				Name:           "responses-to-chat",
				Enabled:        true,
				SourceEndpoint: "/v1/responses",
				TargetEndpoint: "chat",
				AllChannels:    true,
				ModelPatterns:  []string{"^gpt-5$"},
			},
		},
	}
	require.True(t, ShouldResponsesUseChatCompletionsPolicy(rulePolicy, 1, 1, "gpt-5"))
	require.False(t, ShouldChatCompletionsUseResponsesPolicy(rulePolicy, 1, 1, "gpt-5"))
}

func TestProtocolConversionPolicyTopLevelEnabledGatesRules(t *testing.T) {
	policy := model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: false,
		Rules: []model_setting.ProtocolConversionRule{
			{
				Name:           "responses-to-chat",
				Enabled:        true,
				SourceEndpoint: model_setting.ProtocolEndpointResponses,
				TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
				AllChannels:    true,
				ModelPatterns:  []string{"^gpt-5$"},
			},
		},
	}

	require.False(t, ShouldResponsesUseChatCompletionsPolicy(policy, 1, 1, "gpt-5"))

	policy.Enabled = true
	require.True(t, ShouldResponsesUseChatCompletionsPolicy(policy, 1, 1, "gpt-5"))
}

func TestProtocolConversionPolicyEmptyModelPatternsMatchAllNonEmptyModels(t *testing.T) {
	policy := model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: true,
		Rules: []model_setting.ProtocolConversionRule{
			{
				Name:           "responses-to-chat-all-models",
				Enabled:        true,
				SourceEndpoint: model_setting.ProtocolEndpointResponses,
				TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
				AllChannels:    false,
				ChannelIDs:     []int{146},
			},
		},
	}

	require.True(t, ShouldResponsesUseChatCompletionsPolicy(policy, 146, 1, "deepseek-v4-flash"))
	require.False(t, ShouldResponsesUseChatCompletionsPolicy(policy, 146, 1, ""))
}

func TestResponsesRequestToChatCompletionsRequest(t *testing.T) {
	req := &dto.OpenAIResponsesRequest{
		Model:             "gpt-5",
		Input:             mustMarshalJSON(t, []map[string]any{{"role": "user", "content": []map[string]any{{"type": "input_text", "text": "hello"}, {"type": "input_image", "image_url": map[string]any{"url": "https://example.com/a.png", "detail": "high"}}, {"type": "input_video", "video_url": map[string]any{"url": "https://example.com/demo.mp4"}}}}, {"type": "function_call", "call_id": "call_1", "name": "lookup", "arguments": `{"q":"hello"}`}, {"type": "function_call_output", "call_id": "call_1", "output": `{"ok":true}`}}),
		Instructions:      mustMarshalJSON(t, "follow the system"),
		MaxOutputTokens:   common.GetPointer(uint(256)),
		ParallelToolCalls: mustMarshalJSON(t, false),
		ToolChoice:        mustMarshalJSON(t, map[string]any{"type": "function", "name": "lookup"}),
		Tools:             mustMarshalJSON(t, []map[string]any{{"type": "function", "name": "lookup", "description": "search", "parameters": map[string]any{"type": "object"}}}),
		PromptCacheKey:    mustMarshalJSON(t, "cache-key"),
		Reasoning:         &dto.Reasoning{Effort: "medium"},
		Text:              mustMarshalJSON(t, map[string]any{"format": map[string]any{"type": "json_schema", "name": "answer", "schema": map[string]any{"type": "object"}}}),
	}

	chatReq, err := ResponsesRequestToChatCompletionsRequest(req)
	require.NoError(t, err)
	require.Equal(t, "gpt-5", chatReq.Model)
	require.Equal(t, "medium", chatReq.ReasoningEffort)
	require.Equal(t, "cache-key", chatReq.PromptCacheKey)
	require.NotNil(t, chatReq.MaxTokens)
	require.Equal(t, uint(256), *chatReq.MaxTokens)
	require.NotNil(t, chatReq.ParallelTooCalls)
	require.False(t, *chatReq.ParallelTooCalls)
	require.Len(t, chatReq.Tools, 1)
	require.Equal(t, "lookup", chatReq.Tools[0].Function.Name)

	require.Len(t, chatReq.Messages, 4)
	require.Equal(t, "developer", chatReq.Messages[0].Role)
	require.Equal(t, "follow the system", chatReq.Messages[0].StringContent())
	require.Equal(t, "user", chatReq.Messages[1].Role)
	require.Len(t, chatReq.Messages[1].ParseContent(), 3)
	require.Equal(t, dto.ContentTypeVideoUrl, chatReq.Messages[1].ParseContent()[2].Type)
	require.Equal(t, "assistant", chatReq.Messages[2].Role)
	require.Len(t, chatReq.Messages[2].ParseToolCalls(), 1)
	require.Equal(t, "lookup", chatReq.Messages[2].ParseToolCalls()[0].Function.Name)
	require.Equal(t, "tool", chatReq.Messages[3].Role)
	require.Equal(t, "call_1", chatReq.Messages[3].ToolCallId)

	require.NotNil(t, chatReq.ResponseFormat)
	require.Equal(t, "json_schema", chatReq.ResponseFormat.Type)
}

func TestResponsesRequestToChatCompletionsRequestRejectsUnsupportedMessageRole(t *testing.T) {
	_, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: mustMarshalJSON(t, []any{
			map[string]any{
				"type":    "message",
				"role":    "tool",
				"content": "invalid",
			},
		}),
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "message role")
}

func TestChatCompletionsRequestToResponsesRequestRejectsMalformedContentArray(t *testing.T) {
	_, err := ChatCompletionsRequestToResponsesRequest(&dto.GeneralOpenAIRequest{
		Model: "gpt-5",
		Messages: []dto.Message{{
			Role:    "user",
			Content: []any{"invalid"},
		}},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "content[0]")
}

func TestResponsesRequestToChatCompletionsRequestPreservesDeepSeekReasoningText(t *testing.T) {
	chatReq, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "deepseek-v4-flash",
		Input: mustMarshalJSON(t, []any{
			map[string]any{
				"type": "reasoning",
				"content": []map[string]any{
					{"type": "reasoning_text", "text": "first\n"},
					{"type": "reasoning_text", "text": "second"},
				},
			},
			map[string]any{
				"type":    "message",
				"role":    "assistant",
				"content": []map[string]any{{"type": "output_text", "text": "answer"}},
			},
			map[string]any{
				"type":      "function_call",
				"call_id":   "call_lookup",
				"name":      "lookup",
				"arguments": `{"query":"status"}`,
			},
		}),
	})

	require.NoError(t, err)
	require.Len(t, chatReq.Messages, 1)
	assistant := chatReq.Messages[0]
	require.Equal(t, "assistant", assistant.Role)
	require.Equal(t, "answer", assistant.StringContent())
	require.Equal(t, "first\nsecond", assistant.GetReasoningContent())
	require.Len(t, assistant.ParseToolCalls(), 1)
	require.Equal(t, "call_lookup", assistant.ParseToolCalls()[0].ID)
}

func TestResponsesRequestToChatCompletionsRequestRejectsUnsupportedReasoningShape(t *testing.T) {
	_, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "deepseek-v4-flash",
		Input: mustMarshalJSON(t, []any{
			map[string]any{
				"type":    "reasoning",
				"summary": []map[string]any{{"type": "summary_text", "text": "summary only"}},
			},
			map[string]any{"type": "message", "role": "assistant", "content": "answer"},
		}),
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "reasoning content is required")
}

func TestChatCompletionsRequestToResponsesRequestPreservesDeepSeekReasoningBeforeToolCalls(t *testing.T) {
	reasoning := "  preserve this reasoning\n"
	req := &dto.GeneralOpenAIRequest{
		Model: "deepseek-v4-flash",
		Messages: []dto.Message{
			{Role: "user", Content: "check the weather"},
			{
				Role:             "assistant",
				Content:          "I will check that.",
				ReasoningContent: &reasoning,
			},
		},
	}
	req.Messages[1].SetToolCalls([]dto.ToolCallRequest{{
		ID:   "call_weather",
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      "get_weather",
			Arguments: `{"city":"Hangzhou"}`,
		},
	}})

	responsesReq, err := ChatCompletionsRequestToResponsesRequestWithOptions(req, ChatCompletionsToResponsesOptions{
		PreserveReasoningContent: true,
	})
	require.NoError(t, err)

	var items []map[string]any
	require.NoError(t, common.Unmarshal(responsesReq.Input, &items))
	require.Len(t, items, 4)
	require.Equal(t, "user", items[0]["role"])
	require.Equal(t, "reasoning", items[1]["type"])
	require.Equal(t, []any{map[string]any{
		"type": "reasoning_text",
		"text": reasoning,
	}}, items[1]["content"])
	require.Equal(t, "assistant", items[2]["role"])
	require.Equal(t, "I will check that.", items[2]["content"])
	require.Equal(t, "function_call", items[3]["type"])
	require.Equal(t, "call_weather", items[3]["call_id"])
}

func TestChatCompletionsRequestToResponsesRequestPreservesExplicitEmptyReasoning(t *testing.T) {
	reasoning := ""
	req := &dto.GeneralOpenAIRequest{
		Model: "deepseek-v4-flash",
		Messages: []dto.Message{{
			Role:             "assistant",
			Content:          "",
			ReasoningContent: &reasoning,
		}},
	}

	responsesReq, err := ChatCompletionsRequestToResponsesRequestWithOptions(req, ChatCompletionsToResponsesOptions{
		PreserveReasoningContent: true,
	})
	require.NoError(t, err)

	var items []map[string]any
	require.NoError(t, common.Unmarshal(responsesReq.Input, &items))
	require.Len(t, items, 2)
	require.Equal(t, "reasoning", items[0]["type"])
	require.Equal(t, []any{map[string]any{
		"type": "reasoning_text",
		"text": "",
	}}, items[0]["content"])
	require.Equal(t, "assistant", items[1]["role"])
}

func TestChatCompletionsRequestToResponsesRequestDoesNotEmitPlainTextReasoningByDefault(t *testing.T) {
	reasoning := "do not send to native OpenAI Responses"
	req := &dto.GeneralOpenAIRequest{
		Model: "gpt-5.6-luna",
		Messages: []dto.Message{{
			Role:             "assistant",
			Content:          "answer",
			ReasoningContent: &reasoning,
		}},
	}

	responsesReq, err := ChatCompletionsRequestToResponsesRequest(req)
	require.NoError(t, err)

	var items []map[string]any
	require.NoError(t, common.Unmarshal(responsesReq.Input, &items))
	require.Len(t, items, 1)
	require.Equal(t, "assistant", items[0]["role"])
}

func TestResponsesRequestToChatCompletionsRequestPreservesInputAudio(t *testing.T) {
	chatReq, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: mustMarshalJSON(t, []any{
			map[string]any{
				"type": "input_audio",
				"input_audio": map[string]any{
					"data":   "DIRECT_AUDIO_DATA",
					"format": "wav",
				},
			},
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "transcribe this"},
					{"type": "input_audio", "input_audio": map[string]any{"data": "MESSAGE_AUDIO_DATA", "format": "mp3"}},
				},
			},
		}),
	})

	require.NoError(t, err)
	require.Len(t, chatReq.Messages, 2)

	directAudio := chatReq.Messages[0].ParseContent()
	require.Len(t, directAudio, 1)
	require.Equal(t, dto.ContentTypeInputAudio, directAudio[0].Type)
	require.NotNil(t, directAudio[0].GetInputAudio())
	require.Equal(t, "DIRECT_AUDIO_DATA", directAudio[0].GetInputAudio().Data)
	require.Equal(t, "wav", directAudio[0].GetInputAudio().Format)

	messageAudio := chatReq.Messages[1].ParseContent()
	require.Len(t, messageAudio, 2)
	require.Equal(t, dto.ContentTypeInputAudio, messageAudio[1].Type)
	require.NotNil(t, messageAudio[1].GetInputAudio())
	require.Equal(t, "MESSAGE_AUDIO_DATA", messageAudio[1].GetInputAudio().Data)
	require.Equal(t, "mp3", messageAudio[1].GetInputAudio().Format)
}

func TestResponsesRequestToChatCompletionsRequestRejectsUnsupportedPreviousResponseID(t *testing.T) {
	_, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: "resp_123",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "previous_response_id")
}

func TestResponsesRequestToChatCompletionsRequestAllowsIgnoredInclude(t *testing.T) {
	chatReq, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model:   "gpt-5",
		Input:   mustMarshalJSON(t, "hello"),
		Include: mustMarshalJSON(t, []string{"reasoning.encrypted_content"}),
	})
	require.NoError(t, err)
	require.Equal(t, "gpt-5", chatReq.Model)
	require.Len(t, chatReq.Messages, 1)
	require.Equal(t, "hello", chatReq.Messages[0].StringContent())
}

func TestResponsesRequestToChatCompletionsRequestRejectsUnsupportedInclude(t *testing.T) {
	_, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model:   "gpt-5",
		Input:   mustMarshalJSON(t, "hello"),
		Include: mustMarshalJSON(t, []string{"message.output_text.logprobs"}),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "message.output_text.logprobs")
}

func TestResponsesRequestToChatCompletionsRequestRejectsInvalidIncludeShape(t *testing.T) {
	_, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model:   "gpt-5",
		Input:   mustMarshalJSON(t, "hello"),
		Include: mustMarshalJSON(t, "reasoning.encrypted_content"),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "include must be an array")
}

func TestResponsesResponseToChatCompletionsResponseMapsReasoningSummary(t *testing.T) {
	resp := &dto.OpenAIResponsesResponse{
		ID:        "resp_reasoning",
		CreatedAt: 1710000000,
		Model:     "gpt-5.4-mini",
		Output: []dto.ResponsesOutput{
			{
				Type: "reasoning",
				Summary: []dto.ResponsesReasoningSummaryPart{
					{Type: "summary_text", Text: "checked constraints"},
					{Type: "summary_text", Text: "selected final answer"},
				},
				EncryptedContent: common.RawMessage(`"opaque"`),
			},
			{
				Type: "message",
				Role: "assistant",
				Content: []dto.ResponsesOutputContent{
					{Type: "output_text", Text: "done"},
				},
			},
		},
	}

	chatResp, _, err := ResponsesResponseToChatCompletionsResponse(resp, "chatcmpl_reasoning")

	require.NoError(t, err)
	require.Len(t, chatResp.Choices, 1)
	require.Equal(t, "done", chatResp.Choices[0].Message.Content)
	require.Equal(t, "checked constraints\n\nselected final answer", chatResp.Choices[0].Message.GetReasoningContent())
}

func TestResponsesResponseToChatCompletionsResponseMapsOutputTokenDetails(t *testing.T) {
	resp := &dto.OpenAIResponsesResponse{
		ID:        "resp_usage_details",
		CreatedAt: 1710000000,
		Model:     "gpt-5.4-mini",
		Usage: &dto.Usage{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
			OutputTokensDetails: &dto.OutputTokenDetails{
				ReasoningTokens: 6,
				ImageTokens:     7,
				AudioTokens:     8,
			},
		},
	}

	_, usage, err := ResponsesResponseToChatCompletionsResponse(resp, "chatcmpl_usage_details")

	require.NoError(t, err)
	require.Equal(t, 10, usage.PromptTokens)
	require.Equal(t, 20, usage.CompletionTokens)
	require.Equal(t, 30, usage.TotalTokens)
	require.Equal(t, 6, usage.CompletionTokenDetails.ReasoningTokens)
	require.Equal(t, 7, usage.CompletionTokenDetails.ImageTokens)
	require.Equal(t, 8, usage.CompletionTokenDetails.AudioTokens)
}

func TestResponsesResponseToChatCompletionsResponseMapsSummaryOnlyReasoning(t *testing.T) {
	resp := &dto.OpenAIResponsesResponse{
		ID:        "resp_reasoning_only",
		CreatedAt: 1710000000,
		Model:     "gpt-5.4-mini",
		Reasoning: &dto.Reasoning{Summary: "inspected state and selected the safe route"},
	}

	chatResp, _, err := ResponsesResponseToChatCompletionsResponse(resp, "chatcmpl_reasoning_only")

	require.NoError(t, err)
	require.Len(t, chatResp.Choices, 1)
	require.Equal(t, "", chatResp.Choices[0].Message.Content)
	require.Equal(t, "inspected state and selected the safe route", chatResp.Choices[0].Message.GetReasoningContent())
}

func TestResponsesResponseToChatCompletionsResponseMapsReasoningContentSummaryText(t *testing.T) {
	resp := &dto.OpenAIResponsesResponse{
		ID:        "resp_reasoning_content_summary",
		CreatedAt: 1710000000,
		Model:     "gpt-5.4-mini",
		Output: []dto.ResponsesOutput{
			{
				Type: "reasoning",
				Content: []dto.ResponsesOutputContent{
					{Type: "summary_text", Text: "content summary"},
				},
				EncryptedContent: common.RawMessage(`"opaque"`),
			},
		},
	}

	chatResp, _, err := ResponsesResponseToChatCompletionsResponse(resp, "chatcmpl_reasoning_content_summary")

	require.NoError(t, err)
	require.Len(t, chatResp.Choices, 1)
	require.Equal(t, "", chatResp.Choices[0].Message.Content)
	require.Equal(t, "content summary", chatResp.Choices[0].Message.GetReasoningContent())
}

func TestResponsesResponseToChatCompletionsResponseIgnoresReasoningSummaryConfigValues(t *testing.T) {
	resp := &dto.OpenAIResponsesResponse{
		ID:        "resp_reasoning_config",
		CreatedAt: 1710000000,
		Model:     "gpt-5.4-mini",
		Reasoning: &dto.Reasoning{Summary: "auto"},
		Output: []dto.ResponsesOutput{
			{
				Type: "message",
				Role: "assistant",
				Content: []dto.ResponsesOutputContent{
					{Type: "output_text", Text: "done"},
				},
			},
		},
	}

	chatResp, _, err := ResponsesResponseToChatCompletionsResponse(resp, "chatcmpl_reasoning_config")

	require.NoError(t, err)
	require.Empty(t, chatResp.Choices[0].Message.GetReasoningContent())
}

func TestResponsesResponseToChatCompletionsResponsePreservesExplicitEmptyReasoningText(t *testing.T) {
	resp := &dto.OpenAIResponsesResponse{
		ID:        "resp_empty_reasoning_text",
		CreatedAt: 1710000000,
		Model:     "deepseek-v4-flash",
		Output: []dto.ResponsesOutput{
			{
				Type: "reasoning",
				Content: []dto.ResponsesOutputContent{
					{Type: "reasoning_text", Text: ""},
				},
			},
			{
				Type:    "message",
				Role:    "assistant",
				Content: []dto.ResponsesOutputContent{{Type: "output_text", Text: "tool follows"}},
			},
			{
				Type:      "function_call",
				ID:        "fc_empty_reasoning",
				CallId:    "call_empty_reasoning",
				Name:      "lookup",
				Arguments: common.RawMessage(`{"q":"status"}`),
			},
		},
	}

	chatResp, _, err := ResponsesResponseToChatCompletionsResponse(resp, "chatcmpl_empty_reasoning_text")

	require.NoError(t, err)
	require.Len(t, chatResp.Choices, 1)
	require.NotNil(t, chatResp.Choices[0].Message.ReasoningContent)
	require.Empty(t, *chatResp.Choices[0].Message.ReasoningContent)
	require.Equal(t, "tool follows", chatResp.Choices[0].Message.Content)
	require.Len(t, chatResp.Choices[0].Message.ParseToolCalls(), 1)
}

func TestResponsesResponseToChatCompletionsResponseMapsIncompleteReason(t *testing.T) {
	tests := []struct {
		name         string
		reason       string
		want         string
		withToolCall bool
	}{
		{name: "max output tokens", reason: "max_output_tokens", want: "length"},
		{name: "content filter", reason: "content_filter", want: "content_filter"},
		{name: "unknown reason defaults to length", reason: "other", want: "length"},
		{name: "tool call takes precedence", reason: "max_output_tokens", want: "tool_calls", withToolCall: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &dto.OpenAIResponsesResponse{
				ID:                "resp_incomplete",
				CreatedAt:         1710000000,
				Model:             "gpt-5.6-sol",
				Status:            common.RawMessage(`"incomplete"`),
				IncompleteDetails: &dto.IncompleteDetails{Reason: tt.reason},
			}
			if tt.withToolCall {
				resp.Output = []dto.ResponsesOutput{{
					Type:      "function_call",
					ID:        "fc_incomplete",
					CallId:    "call_incomplete",
					Name:      "lookup",
					Arguments: common.RawMessage(`{"query":"status"}`),
				}}
			}

			chatResp, _, err := ResponsesResponseToChatCompletionsResponse(resp, "chatcmpl_incomplete")

			require.NoError(t, err)
			require.Len(t, chatResp.Choices, 1)
			require.Equal(t, tt.want, chatResp.Choices[0].FinishReason)
			if tt.withToolCall {
				require.Len(t, chatResp.Choices[0].Message.ParseToolCalls(), 1)
			} else {
				require.Empty(t, chatResp.Choices[0].Message.ParseToolCalls())
			}
		})
	}
}

func TestResponsesResponseToChatCompletionsResponseMapsLegacyIncompleteReason(t *testing.T) {
	resp := &dto.OpenAIResponsesResponse{
		ID:                "resp_incomplete_legacy",
		CreatedAt:         1710000000,
		Model:             "gpt-5.6-luna",
		Status:            common.RawMessage(`"incomplete"`),
		IncompleteDetails: &dto.IncompleteDetails{Reasoning: "content_filter"},
	}

	chatResp, _, err := ResponsesResponseToChatCompletionsResponse(resp, "chatcmpl_incomplete_legacy")

	require.NoError(t, err)
	require.Len(t, chatResp.Choices, 1)
	require.Equal(t, "content_filter", chatResp.Choices[0].FinishReason)
}

func TestResponsesResponseToChatCompletionsResponseMapsCaseInsensitiveIncompleteStatus(t *testing.T) {
	resp := &dto.OpenAIResponsesResponse{
		ID:                "resp_incomplete_uppercase",
		CreatedAt:         1710000000,
		Model:             "gpt-5.6-luna",
		Status:            common.RawMessage(`"INCOMPLETE"`),
		IncompleteDetails: &dto.IncompleteDetails{Reason: "content_filter"},
	}

	chatResp, _, err := ResponsesResponseToChatCompletionsResponse(resp, "chatcmpl_incomplete_uppercase")

	require.NoError(t, err)
	require.Len(t, chatResp.Choices, 1)
	require.Equal(t, "content_filter", chatResp.Choices[0].FinishReason)
}

func TestResponsesResponseToChatCompletionsResponseKeepsToolCallsAlongsideText(t *testing.T) {
	resp := &dto.OpenAIResponsesResponse{
		ID:        "resp_text_and_tool",
		CreatedAt: 1710000000,
		Model:     "gpt-5.6-luna",
		Output: []dto.ResponsesOutput{
			{
				Type:    "message",
				Role:    "assistant",
				Content: []dto.ResponsesOutputContent{{Type: "output_text", Text: "checking"}},
			},
			{
				Type:      "function_call",
				ID:        "fc_text_and_tool",
				CallId:    "call_text_and_tool",
				Name:      "lookup",
				Arguments: common.RawMessage(`{"query":"status"}`),
			},
		},
	}

	chatResp, _, err := ResponsesResponseToChatCompletionsResponse(resp, "chatcmpl_text_and_tool")

	require.NoError(t, err)
	require.Equal(t, "checking", chatResp.Choices[0].Message.Content)
	require.Equal(t, "tool_calls", chatResp.Choices[0].FinishReason)
	require.Len(t, chatResp.Choices[0].Message.ParseToolCalls(), 1)
	call := chatResp.Choices[0].Message.ParseToolCalls()[0]
	require.Equal(t, "call_text_and_tool", call.ID)
	require.Equal(t, "lookup", call.Function.Name)
}

func TestResponsesResponseToChatCompletionsResponseNormalizesUnknownInvalidToolName(t *testing.T) {
	resp := &dto.OpenAIResponsesResponse{
		Model: "gpt-5.6",
		Output: []dto.ResponsesOutput{{
			Type:      "function_call",
			ID:        "fc_unknown",
			CallId:    "call_unknown",
			Name:      "mcp.calendar/search.v1",
			Arguments: common.RawMessage(`{"q":"hello"}`),
		}},
	}

	chatResp, _, err := ResponsesResponseToChatCompletionsResponse(resp, "chatcmpl_unknown")
	require.NoError(t, err)
	calls := chatResp.Choices[0].Message.ParseToolCalls()
	require.Len(t, calls, 1)
	require.Equal(t, dto.NormalizeToolName("mcp.calendar/search.v1"), calls[0].Function.Name)
}

func TestResponsesRequestToChatCompletionsRequestDeduplicatesIdenticalEffectiveTools(t *testing.T) {
	tool := map[string]any{
		"type":       "function",
		"name":       "lookup",
		"parameters": map[string]any{"type": "object"},
	}
	chatReq, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Tools: mustMarshalJSON(t, []map[string]any{tool}),
		Input: mustMarshalJSON(t, []any{
			map[string]any{"type": "additional_tools", "role": "developer", "tools": []map[string]any{tool}},
			map[string]any{"type": "message", "role": "user", "content": []map[string]any{{"type": "input_text", "text": "continue"}}},
		}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.NoError(t, err)
	require.Len(t, chatReq.Tools, 1)
	require.Equal(t, "lookup", chatReq.Tools[0].Function.Name)
}

func TestResponsesRequestToChatCompletionsRequestRejectsEffectiveToolNameCollisions(t *testing.T) {
	tests := []struct {
		name       string
		topTool    map[string]any
		additional map[string]any
	}{
		{
			name:       "function and tool search",
			topTool:    map[string]any{"type": "function", "name": "tool_search", "parameters": map[string]any{"type": "object"}},
			additional: map[string]any{"type": "tool_search"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
				Model: "gpt-5.6-sol",
				Tools: mustMarshalJSON(t, []map[string]any{tt.topTool}),
				Input: mustMarshalJSON(t, []any{
					map[string]any{"type": "additional_tools", "role": "developer", "tools": []map[string]any{tt.additional}},
					map[string]any{"type": "message", "role": "user", "content": []map[string]any{{"type": "input_text", "text": "continue"}}},
				}),
			}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

			require.Error(t, err)
			require.Contains(t, err.Error(), "duplicated")
		})
	}
}

func TestResponsesChatCompatibilityRestoresAdditionalNamespaceToolCall(t *testing.T) {
	chatReq, toolState, err := ResponsesRequestToChatCompletionsRequestWithState(&dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: mustMarshalJSON(t, []any{
			map[string]any{
				"type": "additional_tools",
				"role": "developer",
				"tools": []map[string]any{
					{
						"type": "namespace",
						"name": "collaboration",
						"tools": []map[string]any{{
							"type":       "function",
							"name":       "send_message",
							"parameters": map[string]any{"type": "object"},
						}},
					},
					{
						"type": "namespace",
						"name": "codex",
						"tools": []map[string]any{{
							"type":       "function",
							"name":       "send_message",
							"parameters": map[string]any{"type": "object"},
						}},
					},
				},
			},
			map[string]any{
				"type":    "message",
				"role":    "user",
				"content": []map[string]any{{"type": "input_text", "text": "send status"}},
			},
		}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.NoError(t, err)
	require.Len(t, chatReq.Tools, 2)
	require.Equal(t, "collaboration__send_message", chatReq.Tools[0].Function.Name)
	require.Equal(t, "codex__send_message", chatReq.Tools[1].Function.Name)

	chatResp := &dto.OpenAITextResponse{
		Model: "gpt-5.6-sol",
		Choices: []dto.OpenAITextResponseChoice{{
			Message: dto.Message{Role: "assistant", Content: ""},
		}},
	}
	chatResp.Choices[0].Message.SetToolCalls([]dto.ToolCallRequest{{
		ID:   "call_namespace_1",
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      "collaboration__send_message",
			Arguments: `{"message":"done"}`,
		},
	}})

	responsesResp, _, err := ChatCompletionsResponseToResponsesResponseWithOptionsAndState(
		chatResp,
		"resp_namespace",
		ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true},
		toolState,
	)
	require.NoError(t, err)
	require.Len(t, responsesResp.Output, 1)
	require.Equal(t, "function_call", responsesResp.Output[0].Type)
	require.Equal(t, "collaboration", responsesResp.Output[0].Namespace)
	require.Equal(t, "send_message", responsesResp.Output[0].Name)
	require.Equal(t, "call_namespace_1", responsesResp.Output[0].CallId)
}

func TestResponsesChatCompatibilityRestoresToolSearchCall(t *testing.T) {
	chatReq, toolState, err := ResponsesRequestToChatCompletionsRequestWithState(&dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: mustMarshalJSON(t, []any{
			map[string]any{
				"type":  "additional_tools",
				"role":  "developer",
				"tools": []map[string]any{{"type": "tool_search"}},
			},
			map[string]any{
				"type":    "message",
				"role":    "user",
				"content": []map[string]any{{"type": "input_text", "text": "find the mail tool"}},
			},
		}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.NoError(t, err)
	require.Len(t, chatReq.Tools, 1)
	require.Equal(t, "function", chatReq.Tools[0].Type)
	require.Equal(t, "tool_search", chatReq.Tools[0].Function.Name)

	chatResp := &dto.OpenAITextResponse{
		Model:   "gpt-5.6-sol",
		Choices: []dto.OpenAITextResponseChoice{{Message: dto.Message{Role: "assistant", Content: ""}}},
	}
	chatResp.Choices[0].Message.SetToolCalls([]dto.ToolCallRequest{{
		ID:   "call_search_1",
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      "tool_search",
			Arguments: `{"query":"gmail search"}`,
		},
	}})

	responsesResp, _, err := ChatCompletionsResponseToResponsesResponseWithOptionsAndState(
		chatResp,
		"resp_tool_search",
		ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true},
		toolState,
	)
	require.NoError(t, err)
	require.Len(t, responsesResp.Output, 1)
	require.Equal(t, "tool_search_call", responsesResp.Output[0].Type)
	require.Equal(t, "client", responsesResp.Output[0].Execution)
	require.JSONEq(t, `{"query":"gmail search"}`, string(responsesResp.Output[0].Arguments))
	require.Equal(t, "call_search_1", responsesResp.Output[0].CallId)
}

func TestResponsesRequestToChatCompletionsRequestRejectsEncryptedToolOutput(t *testing.T) {
	_, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: mustMarshalJSON(t, []map[string]any{{
			"type":    "function_call_output",
			"call_id": "call_opaque_1",
			"output": []map[string]any{{
				"type":              "encrypted_content",
				"encrypted_content": "opaque",
			}},
		}}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.Error(t, err)
	require.Contains(t, err.Error(), `content type "encrypted_content" is not supported`)
}

func TestResponsesRequestToChatCompletionsRequestPreservesNamespaceAndToolSearchHistory(t *testing.T) {
	chatReq, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: mustMarshalJSON(t, []map[string]any{
			{
				"type":      "function_call",
				"call_id":   "call_namespace_1",
				"namespace": "collaboration",
				"name":      "send_message",
				"arguments": `{"message":"working"}`,
			},
			{
				"type":    "function_call_output",
				"call_id": "call_namespace_1",
				"output":  "ok",
			},
			{
				"type":      "tool_search_call",
				"call_id":   "call_search_1",
				"execution": "client",
				"arguments": map[string]any{"query": "mail"},
			},
			{
				"type":      "tool_search_output",
				"call_id":   "call_search_1",
				"execution": "client",
				"status":    "completed",
				"tools": []map[string]any{{
					"type": "namespace",
					"name": "mail",
					"tools": []map[string]any{{
						"type":       "function",
						"name":       "search",
						"parameters": map[string]any{"type": "object"},
					}},
				}},
			},
		}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.NoError(t, err)
	require.Len(t, chatReq.Messages, 4)
	firstCalls := chatReq.Messages[0].ParseToolCalls()
	require.Len(t, firstCalls, 1)
	require.Equal(t, "collaboration__send_message", firstCalls[0].Function.Name)
	require.Equal(t, "call_namespace_1", firstCalls[0].ID)
	require.Equal(t, "tool", chatReq.Messages[1].Role)
	searchCalls := chatReq.Messages[2].ParseToolCalls()
	require.Len(t, searchCalls, 1)
	require.Equal(t, "tool_search", searchCalls[0].Function.Name)
	require.JSONEq(t, `{"query":"mail"}`, searchCalls[0].Function.Arguments)
	require.Equal(t, "tool", chatReq.Messages[3].Role)
	require.JSONEq(t, `[{"type":"namespace","name":"mail","tools":[{"type":"function","name":"search","parameters":{"type":"object"}}]}]`, chatReq.Messages[3].StringContent())
	require.Len(t, chatReq.Tools, 1)
	require.Equal(t, "mail__search", chatReq.Tools[0].Function.Name)
}

func TestResponsesRequestToChatCompletionsRequestBridgesNamespaceToolsWhenEnabled(t *testing.T) {
	chatReq, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model:      "gpt-5",
		Input:      mustMarshalJSON(t, "read thread status"),
		ToolChoice: mustMarshalJSON(t, map[string]any{"type": "namespace", "name": "codex_app"}),
		Tools: mustMarshalJSON(t, []map[string]any{
			{
				"type":        "namespace",
				"name":        "codex_app",
				"description": "Tools in the codex_app namespace.",
				"tools": []map[string]any{
					{
						"type":        "function",
						"name":        "read_thread",
						"description": "Read recent status and turn summaries for one Codex thread.",
						"parameters": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"threadId": map[string]any{"type": "string"},
							},
							"required": []string{"threadId"},
						},
					},
					{
						"type":        "function",
						"name":        "list_threads",
						"description": "List recent Codex threads.",
						"input_schema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"limit": map[string]any{"type": "number"},
							},
						},
					},
				},
			},
			{
				"type":        "namespace",
				"name":        "unused_app",
				"description": "Tools outside the selected namespace.",
				"tools": []map[string]any{
					{
						"type":        "function",
						"name":        "delete_thread",
						"description": "Delete a thread.",
						"parameters": map[string]any{
							"type": "object",
						},
					},
				},
			},
		}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.NoError(t, err)
	require.Len(t, chatReq.Tools, 2)
	require.Equal(t, "function", chatReq.Tools[0].Type)
	require.Equal(t, "codex_app__read_thread", chatReq.Tools[0].Function.Name)
	require.Equal(t, "Tools in the codex_app namespace. Read recent status and turn summaries for one Codex thread.", chatReq.Tools[0].Function.Description)
	readThreadParams, err := common.Marshal(chatReq.Tools[0].Function.Parameters)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"type": "object",
		"properties": {
			"threadId": {"type": "string"}
		},
		"required": ["threadId"]
	}`, string(readThreadParams))
	require.Equal(t, "function", chatReq.Tools[1].Type)
	require.Equal(t, "codex_app__list_threads", chatReq.Tools[1].Function.Name)
	require.Equal(t, "Tools in the codex_app namespace. List recent Codex threads.", chatReq.Tools[1].Function.Description)
	listThreadsParams, err := common.Marshal(chatReq.Tools[1].Function.Parameters)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"type": "object",
		"properties": {
			"limit": {"type": "number"}
		}
	}`, string(listThreadsParams))
	require.Equal(t, "required", chatReq.ToolChoice)
}

func TestResponsesRequestToChatCompletionsRequestRejectsMissingNamespaceToolChoiceTarget(t *testing.T) {
	_, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model:      "gpt-5",
		Input:      mustMarshalJSON(t, "read thread status"),
		ToolChoice: mustMarshalJSON(t, map[string]any{"type": "namespace", "name": "missing_app"}),
		Tools: mustMarshalJSON(t, []map[string]any{
			{
				"type": "namespace",
				"name": "codex_app",
				"tools": []map[string]any{{
					"type": "function",
					"name": "read_thread",
				}},
			},
		}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.Error(t, err)
	require.Contains(t, err.Error(), "tool_choice namespace")
	require.Contains(t, err.Error(), "missing_app")
}

func TestResponsesRequestToChatCompletionsRequestRejectsDuplicateNamespaceToolNames(t *testing.T) {
	_, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model:      "gpt-5",
		Input:      mustMarshalJSON(t, "read thread status"),
		ToolChoice: mustMarshalJSON(t, map[string]any{"type": "namespace", "name": "codex_app"}),
		Tools: mustMarshalJSON(t, []map[string]any{
			{
				"type":        "namespace",
				"name":        "codex_app",
				"description": "Tools in the codex_app namespace.",
				"tools": []map[string]any{
					{
						"type":        "function",
						"name":        "lookup",
						"description": "Read recent status.",
						"parameters": map[string]any{
							"type": "object",
						},
					},
					{
						"type":        "function",
						"name":        "lookup",
						"description": "Read thread summary.",
						"parameters": map[string]any{
							"type": "object",
						},
					},
				},
			},
		}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicated")
}

func TestResponsesRequestToChatCompletionsRequestIgnoresDuplicateFunctionToolNamesOutsideSelectedNamespace(t *testing.T) {
	chatReq, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model:      "gpt-5",
		Input:      mustMarshalJSON(t, "read thread status"),
		ToolChoice: mustMarshalJSON(t, map[string]any{"type": "namespace", "name": "codex_app"}),
		Tools: mustMarshalJSON(t, []map[string]any{
			{
				"type": "namespace",
				"name": "codex_app",
				"tools": []map[string]any{{
					"type": "function",
					"name": "lookup",
				}},
			},
			{
				"type": "namespace",
				"name": "ns2",
				"tools": []map[string]any{{
					"type": "function",
					"name": "lookup",
				}},
			},
		}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.NoError(t, err)
	require.Len(t, chatReq.Tools, 1)
	require.Equal(t, "codex_app__lookup", chatReq.Tools[0].Function.Name)
}

func TestResponsesRequestToChatCompletionsRequestRejectsNestedToolSearch(t *testing.T) {
	_, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: mustMarshalJSON(t, "read thread status"),
		Tools: mustMarshalJSON(t, []map[string]any{
			{
				"type": "namespace",
				"name": "codex_app",
				"tools": []map[string]any{
					{
						"type": "tool_search",
					},
					{
						"type":        "function",
						"name":        "read_thread",
						"description": "Read recent status.",
						"parameters": map[string]any{
							"type": "object",
						},
					},
				},
			},
		}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.Error(t, err)
	require.Contains(t, err.Error(), `namespace tool type "tool_search" is not supported`)
}

func TestResponsesRequestToChatCompletionsRequestRejectsUnsupportedInputToolSearchItems(t *testing.T) {
	_, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: mustMarshalJSON(t, []map[string]any{
			{
				"type": "tool_search",
			},
			{
				"type":    "message",
				"role":    "user",
				"content": []map[string]any{{"type": "input_text", "text": "continue"}},
			},
		}),
	}, ResponsesChatCompatibilityOptions{})

	require.Error(t, err)
	require.Contains(t, err.Error(), `input item type "tool_search" is not supported`)
}

func TestResponsesRequestToChatCompletionsRequestNamespaceToolRequiresCompatibleNestedTool(t *testing.T) {
	_, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: mustMarshalJSON(t, "read thread status"),
		Tools: mustMarshalJSON(t, []map[string]any{
			{
				"type": "namespace",
				"name": "codex_app",
				"tools": []map[string]any{
					{
						"type": "tool_search",
					},
				},
			},
		}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.Error(t, err)
	require.Contains(t, err.Error(), `namespace tool type "tool_search" is not supported`)
}

func TestResponsesRequestToChatCompletionsRequestRejectsNamespaceToolsByDefault(t *testing.T) {
	_, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: mustMarshalJSON(t, "read thread status"),
		Tools: mustMarshalJSON(t, []map[string]any{
			{
				"type": "namespace",
				"name": "codex_app",
			},
		}),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "custom tool bridge is not enabled")
}

func TestChatCompletionsResponseToResponsesResponse(t *testing.T) {
	chatResp := &dto.OpenAITextResponse{
		Id:      "chatcmpl_123",
		Model:   "gpt-5",
		Object:  "chat.completion",
		Created: int64(12345),
		Choices: []dto.OpenAITextResponseChoice{
			{
				Index: 0,
				Message: dto.Message{
					Role:    "assistant",
					Content: "done",
				},
				FinishReason: "tool_calls",
			},
		},
		Usage: dto.Usage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}
	chatResp.Choices[0].Message.SetToolCalls([]dto.ToolCallRequest{
		{
			ID:   "call_1",
			Type: "function",
			Function: dto.FunctionRequest{
				Name:      "lookup",
				Arguments: `{"q":"hello"}`,
			},
		},
	})

	responsesResp, usage, err := ChatCompletionsResponseToResponsesResponse(chatResp, "resp_123")
	require.NoError(t, err)
	require.Equal(t, "resp_123", responsesResp.ID)
	require.Equal(t, "response", responsesResp.Object)
	require.Len(t, responsesResp.Output, 2)
	require.Equal(t, "message", responsesResp.Output[0].Type)
	require.Equal(t, "done", responsesResp.Output[0].Content[0].Text)
	require.Equal(t, "function_call", responsesResp.Output[1].Type)
	require.Equal(t, "lookup", responsesResp.Output[1].Name)
	var arguments string
	require.NoError(t, common.Unmarshal(responsesResp.Output[1].Arguments, &arguments))
	require.Equal(t, `{"q":"hello"}`, arguments)
	require.NotNil(t, usage)
	require.Equal(t, 10, usage.InputTokens)
	require.Equal(t, 5, usage.OutputTokens)
	require.Equal(t, 15, usage.TotalTokens)
}

func TestChatCompletionsResponseToResponsesResponseMapsIncompleteFinishReasons(t *testing.T) {
	for _, testCase := range []struct {
		name           string
		finishReason   string
		wantReason     string
		wantIncomplete bool
	}{
		{name: "length", finishReason: "length", wantReason: "max_output_tokens", wantIncomplete: true},
		{name: "content filter", finishReason: "content_filter", wantReason: "content_filter", wantIncomplete: true},
		{name: "stop", finishReason: "stop"},
		{name: "tool calls", finishReason: "tool_calls"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			responsesResp, _, err := ChatCompletionsResponseToResponsesResponse(&dto.OpenAITextResponse{
				Model: "gpt-5",
				Choices: []dto.OpenAITextResponseChoice{{
					Message:      dto.Message{Role: "assistant", Content: "answer"},
					FinishReason: testCase.finishReason,
				}},
			}, "resp_finish_reason")

			require.NoError(t, err)
			if testCase.wantIncomplete {
				require.JSONEq(t, `"incomplete"`, string(responsesResp.Status))
				require.NotNil(t, responsesResp.IncompleteDetails)
				require.Equal(t, testCase.wantReason, responsesResp.IncompleteDetails.Reason)
			} else {
				require.JSONEq(t, `"completed"`, string(responsesResp.Status))
				require.Nil(t, responsesResp.IncompleteDetails)
			}
		})
	}
}

func TestChatCompletionsResponseToResponsesResponsePreservesReasoningText(t *testing.T) {
	reasoning := "  preserve this reasoning\n"
	chatResp := &dto.OpenAITextResponse{
		Id:      "chatcmpl_reasoning",
		Model:   "deepseek-v4-flash",
		Object:  "chat.completion",
		Created: int64(12345),
		Choices: []dto.OpenAITextResponseChoice{{
			Index: 0,
			Message: dto.Message{
				Role:             "assistant",
				Content:          "done",
				ReasoningContent: &reasoning,
			},
			FinishReason: "stop",
		}},
	}

	responsesResp, _, err := ChatCompletionsResponseToResponsesResponse(chatResp, "resp_reasoning")
	require.NoError(t, err)
	require.Len(t, responsesResp.Output, 2)
	require.Equal(t, "reasoning", responsesResp.Output[0].Type)
	require.Equal(t, "reasoning_text", responsesResp.Output[0].Content[0].Type)
	require.Equal(t, reasoning, responsesResp.Output[0].Content[0].Text)
	require.Equal(t, "message", responsesResp.Output[1].Type)
}

func TestChatCompletionsResponseToResponsesResponseEscapesInvalidToolArguments(t *testing.T) {
	chatResp := &dto.OpenAITextResponse{
		Id:      "chatcmpl_invalid_args",
		Model:   "gpt-5",
		Object:  "chat.completion",
		Created: int64(12345),
		Choices: []dto.OpenAITextResponseChoice{
			{
				Index:        0,
				Message:      dto.Message{Role: "assistant", Content: ""},
				FinishReason: "tool_calls",
			},
		},
	}
	chatResp.Choices[0].Message.SetToolCalls([]dto.ToolCallRequest{
		{
			ID:   "call_invalid",
			Type: "function",
			Function: dto.FunctionRequest{
				Name:      "lookup",
				Arguments: `not-json`,
			},
		},
	})

	responsesResp, _, err := ChatCompletionsResponseToResponsesResponse(chatResp, "resp_invalid_args")

	require.NoError(t, err)
	require.Len(t, responsesResp.Output, 1)
	require.JSONEq(t, `"not-json"`, string(responsesResp.Output[0].Arguments))
}

func TestChatCompletionsResponseToResponsesResponseRejectsMalformedContentArray(t *testing.T) {
	_, _, err := ChatCompletionsResponseToResponsesResponse(&dto.OpenAITextResponse{
		Model: "gpt-5",
		Choices: []dto.OpenAITextResponseChoice{{
			Message: dto.Message{
				Role:    "assistant",
				Content: []any{"invalid"},
			},
			FinishReason: "stop",
		}},
	}, "resp_invalid_content")

	require.Error(t, err)
	require.Contains(t, err.Error(), "content[0]")
}

func TestChatCompletionsResponseToResponsesResponseRejectsCustomToolCallByDefault(t *testing.T) {
	chatResp := &dto.OpenAITextResponse{
		Id:      "chatcmpl_custom",
		Model:   "gpt-5",
		Object:  "chat.completion",
		Created: int64(12345),
		Choices: []dto.OpenAITextResponseChoice{
			{
				Index:        0,
				Message:      dto.Message{Role: "assistant", Content: ""},
				FinishReason: "tool_calls",
			},
		},
	}
	chatResp.Choices[0].Message.SetToolCalls([]dto.ToolCallRequest{
		{
			ID:     "call_custom_1",
			Type:   dto.CustomType,
			Custom: mustMarshalJSON(t, map[string]any{"name": "apply_patch", "input": "*** Begin Patch\n*** End Patch"}),
		},
	})

	_, _, err := ChatCompletionsResponseToResponsesResponse(chatResp, "resp_custom")
	require.Error(t, err)
	require.Contains(t, err.Error(), "custom tool bridge is not enabled")
}

func TestChatCompletionsResponseToResponsesResponseBridgesCustomToolCallWhenEnabled(t *testing.T) {
	chatResp := &dto.OpenAITextResponse{
		Id:      "chatcmpl_custom",
		Model:   "gpt-5",
		Object:  "chat.completion",
		Created: int64(12345),
		Choices: []dto.OpenAITextResponseChoice{
			{
				Index:        0,
				Message:      dto.Message{Role: "assistant", Content: ""},
				FinishReason: "tool_calls",
			},
		},
	}
	chatResp.Choices[0].Message.SetToolCalls([]dto.ToolCallRequest{
		{
			ID:     "call_custom_1",
			Type:   dto.CustomType,
			Custom: mustMarshalJSON(t, map[string]any{"name": "apply_patch", "input": "*** Begin Patch\n*** End Patch"}),
		},
	})

	responsesResp, _, err := ChatCompletionsResponseToResponsesResponseWithOptions(
		chatResp,
		"resp_custom",
		ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true},
	)
	require.NoError(t, err)
	require.Len(t, responsesResp.Output, 1)
	require.Equal(t, "custom_tool_call", responsesResp.Output[0].Type)
	require.Equal(t, "call_custom_1", responsesResp.Output[0].CallId)
	require.Equal(t, "apply_patch", responsesResp.Output[0].Name)
	require.Equal(t, "*** Begin Patch\n*** End Patch", responsesResp.Output[0].Input)
}
