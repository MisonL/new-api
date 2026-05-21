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

func TestProtocolConversionPolicySupportsLegacyAndRules(t *testing.T) {
	legacyPolicy := model_setting.ChatCompletionsToResponsesPolicy{
		Enabled:       true,
		AllChannels:   true,
		ModelPatterns: []string{"^gpt-5$"},
	}
	require.True(t, ShouldChatCompletionsUseResponsesPolicy(legacyPolicy, 1, 1, "gpt-5"))
	require.False(t, ShouldResponsesUseChatCompletionsPolicy(legacyPolicy, 1, 1, "gpt-5"))

	rulePolicy := model_setting.ChatCompletionsToResponsesPolicy{
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

func TestProtocolConversionPolicyEmptyModelPatternsMatchAllNonEmptyModels(t *testing.T) {
	policy := model_setting.ChatCompletionsToResponsesPolicy{
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

func TestResponsesRequestToChatCompletionsRequestRejectsCustomToolsByDefault(t *testing.T) {
	_, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: mustMarshalJSON(t, []map[string]any{
			{
				"type":    "custom_tool_call",
				"call_id": "call_custom_1",
				"name":    "shell",
				"input":   "pwd",
			},
		}),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "custom tool bridge is not enabled")
}

func TestResponsesRequestToChatCompletionsRequestBridgesCustomToolsWhenEnabled(t *testing.T) {
	chatReq, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: mustMarshalJSON(t, []map[string]any{
			{
				"role":    "user",
				"content": "run the command",
			},
			{
				"type":    "custom_tool_call",
				"call_id": "call_custom_1",
				"name":    "shell",
				"input":   "pwd",
			},
			{
				"type":    "custom_tool_call_output",
				"call_id": "call_custom_1",
				"output":  "/tmp/work",
			},
		}),
		ToolChoice: mustMarshalJSON(t, map[string]any{"type": "custom", "name": "shell"}),
		Tools: mustMarshalJSON(t, []map[string]any{
			{
				"type": "web_search",
			},
			{
				"type":        "custom",
				"name":        "shell",
				"description": "Run a shell command.",
				"format": map[string]any{
					"type": "text",
				},
			},
		}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})
	require.NoError(t, err)
	require.Len(t, chatReq.Tools, 1)
	require.Equal(t, dto.CustomType, chatReq.Tools[0].Type)
	require.JSONEq(t, `{"name":"shell","description":"Run a shell command.","format":{"type":"text"}}`, string(chatReq.Tools[0].Custom))
	require.Equal(t, map[string]any{
		"type": "custom",
		"custom": map[string]any{
			"name": "shell",
		},
	}, chatReq.ToolChoice)

	require.Len(t, chatReq.Messages, 3)
	require.Equal(t, "user", chatReq.Messages[0].Role)
	require.Equal(t, "assistant", chatReq.Messages[1].Role)
	toolCalls := chatReq.Messages[1].ParseToolCalls()
	require.Len(t, toolCalls, 1)
	require.Equal(t, "call_custom_1", toolCalls[0].ID)
	require.Equal(t, dto.CustomType, toolCalls[0].Type)
	require.JSONEq(t, `{"name":"shell","input":"pwd"}`, string(toolCalls[0].Custom))
	require.Equal(t, "tool", chatReq.Messages[2].Role)
	require.Equal(t, "call_custom_1", chatReq.Messages[2].ToolCallId)
	require.Equal(t, "/tmp/work", chatReq.Messages[2].StringContent())
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
	require.NotNil(t, usage)
	require.Equal(t, 10, usage.InputTokens)
	require.Equal(t, 5, usage.OutputTokens)
	require.Equal(t, 15, usage.TotalTokens)
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
