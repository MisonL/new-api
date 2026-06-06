package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestClaudeToOpenAIRequestPreservesAssistantThinkingForToolCalls(t *testing.T) {
	thinking := "Need to call the weather tool before answering."
	claudeRequest := dto.ClaudeRequest{
		Model: "deepseek-v4-flash",
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "How is the weather tomorrow?"},
			{
				Role: "assistant",
				Content: []any{
					map[string]any{
						"type":     "thinking",
						"thinking": thinking,
					},
					map[string]any{
						"type": "text",
						"text": "Let me check that.",
					},
					map[string]any{
						"type":  "tool_use",
						"id":    "toolu_01",
						"name":  "get_weather",
						"input": map[string]any{"city": "Hangzhou"},
					},
				},
			},
		},
	}

	openAIRequest, err := ClaudeToOpenAIRequest(claudeRequest, &relaycommon.RelayInfo{
		OriginModelName: "deepseek-v4-flash",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
		},
	})
	require.NoError(t, err)
	require.Len(t, openAIRequest.Messages, 2)

	assistant := openAIRequest.Messages[1]
	require.Equal(t, "assistant", assistant.Role)
	require.Equal(t, "Let me check that.", assistant.StringContent())
	require.Equal(t, thinking, assistant.GetReasoningContent())

	toolCalls := assistant.ParseToolCalls()
	require.Len(t, toolCalls, 1)
	require.Equal(t, "toolu_01", toolCalls[0].ID)
	require.Equal(t, "function", toolCalls[0].Type)
	require.Equal(t, "get_weather", toolCalls[0].Function.Name)
	require.JSONEq(t, `{"city":"Hangzhou"}`, toolCalls[0].Function.Arguments)
}

func TestClaudeToOpenAIRequestPreservesReasoningContentInRequestJSON(t *testing.T) {
	thinking := "The tool result answers the request."
	claudeRequest := dto.ClaudeRequest{
		Model: "deepseek-v4-flash",
		Messages: []dto.ClaudeMessage{
			{
				Role: "assistant",
				Content: []any{
					map[string]any{
						"type":     "thinking",
						"thinking": thinking,
					},
					map[string]any{
						"type": "text",
						"text": "Reading the result.",
					},
					map[string]any{
						"type":  "tool_use",
						"id":    "toolu_02",
						"name":  "read_file",
						"input": map[string]any{"path": "README.md"},
					},
				},
			},
			{
				Role: "user",
				Content: []any{
					map[string]any{
						"type":        "tool_result",
						"tool_use_id": "toolu_02",
						"content":     "file content",
					},
				},
			},
		},
	}

	openAIRequest, err := ClaudeToOpenAIRequest(claudeRequest, &relaycommon.RelayInfo{
		OriginModelName: "deepseek-v4-flash",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
		},
	})
	require.NoError(t, err)

	body, err := common.Marshal(openAIRequest)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"model":"deepseek-v4-flash",
		"messages":[
			{
				"role":"assistant",
				"content":"Reading the result.",
				"reasoning_content":"The tool result answers the request.",
				"tool_calls":[
					{
						"id":"toolu_02",
						"type":"function",
						"function":{
							"name":"read_file",
							"arguments":"{\"path\":\"README.md\"}"
						}
					}
				]
			},
			{
				"role":"tool",
				"content":"file content",
				"name":"read_file",
				"tool_call_id":"toolu_02"
			}
		]
	}`, string(body))
}

func TestClaudeToOpenAIRequestKeepsThinkingOnlyAssistantHistory(t *testing.T) {
	thinking := "Intermediate reasoning that must be passed back."
	claudeRequest := dto.ClaudeRequest{
		Model: "deepseek-v4-flash",
		Messages: []dto.ClaudeMessage{
			{
				Role: "assistant",
				Content: []any{
					map[string]any{
						"type":     "thinking",
						"thinking": thinking,
					},
				},
			},
		},
	}

	openAIRequest, err := ClaudeToOpenAIRequest(claudeRequest, &relaycommon.RelayInfo{
		OriginModelName: "deepseek-v4-flash",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
		},
	})
	require.NoError(t, err)
	require.Len(t, openAIRequest.Messages, 1)
	require.Equal(t, thinking, openAIRequest.Messages[0].GetReasoningContent())

	body, err := common.Marshal(openAIRequest)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"model":"deepseek-v4-flash",
		"messages":[
			{
				"role":"assistant",
				"content":"",
				"reasoning_content":"Intermediate reasoning that must be passed back."
			}
		]
	}`, string(body))
}

func TestClaudeToOpenAIRequestIgnoresNonAssistantThinkingBlocks(t *testing.T) {
	claudeRequest := dto.ClaudeRequest{
		Model: "mimo-v2.5-pro",
		Messages: []dto.ClaudeMessage{
			{
				Role: "user",
				Content: []any{
					map[string]any{
						"type":     "thinking",
						"thinking": "invalid user-side thinking",
					},
					map[string]any{
						"type": "text",
						"text": "hello",
					},
				},
			},
		},
	}

	openAIRequest, err := ClaudeToOpenAIRequest(claudeRequest, &relaycommon.RelayInfo{
		OriginModelName: "mimo-v2.5-pro",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
		},
	})
	require.NoError(t, err)
	require.Len(t, openAIRequest.Messages, 1)
	require.Equal(t, "user", openAIRequest.Messages[0].Role)
	require.Empty(t, openAIRequest.Messages[0].GetReasoningContent())
	contents := openAIRequest.Messages[0].ParseContent()
	require.Len(t, contents, 1)
	require.Equal(t, "text", contents[0].Type)
	require.Equal(t, "hello", contents[0].Text)
}

func TestClaudeToOpenAIRequestConcatenatesMultipleThinkingBlocks(t *testing.T) {
	claudeRequest := dto.ClaudeRequest{
		Model: "deepseek-v4-flash",
		Messages: []dto.ClaudeMessage{
			{
				Role: "assistant",
				Content: []any{
					map[string]any{
						"type":     "thinking",
						"thinking": "first step.",
					},
					map[string]any{
						"type":     "thinking",
						"thinking": "second step.",
					},
				},
			},
		},
	}

	openAIRequest, err := ClaudeToOpenAIRequest(claudeRequest, &relaycommon.RelayInfo{
		OriginModelName: "deepseek-v4-flash",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
		},
	})
	require.NoError(t, err)
	require.Len(t, openAIRequest.Messages, 1)
	require.Equal(t, "first step.\nsecond step.", openAIRequest.Messages[0].GetReasoningContent())
}

func TestResponseOpenAI2ClaudePreservesReasoningContentWithToolCalls(t *testing.T) {
	reasoning := "Need to call the weather tool before answering."
	message := dto.Message{Role: "assistant"}
	message.ReasoningContent = &reasoning
	message.SetToolCalls([]dto.ToolCallRequest{
		{
			ID:   "call_1",
			Type: "function",
			Function: dto.FunctionRequest{
				Name:      "get_weather",
				Arguments: `{"city":"Hangzhou"}`,
			},
		},
	})
	response := &dto.OpenAITextResponse{
		Id:    "chatcmpl-test",
		Model: "deepseek-v4-flash",
		Choices: []dto.OpenAITextResponseChoice{
			{
				Message:      message,
				FinishReason: "tool_calls",
			},
		},
	}

	claudeResponse := ResponseOpenAI2Claude(response, &relaycommon.RelayInfo{})
	require.Len(t, claudeResponse.Content, 2)
	require.Equal(t, "thinking", claudeResponse.Content[0].Type)
	require.Equal(t, reasoning, *claudeResponse.Content[0].Thinking)
	require.Equal(t, "tool_use", claudeResponse.Content[1].Type)
	require.Equal(t, "call_1", claudeResponse.Content[1].Id)
	require.Equal(t, "get_weather", claudeResponse.Content[1].Name)
	require.Equal(t, map[string]interface{}{"city": "Hangzhou"}, claudeResponse.Content[1].Input)
}

func TestResponseOpenAI2ClaudePreservesTextBeforeToolCalls(t *testing.T) {
	reasoning := "Need to call the weather tool before answering."
	message := dto.Message{Role: "assistant"}
	message.ReasoningContent = &reasoning
	message.SetStringContent("Let me check that.")
	message.SetToolCalls([]dto.ToolCallRequest{
		{
			ID:   "call_1",
			Type: "function",
			Function: dto.FunctionRequest{
				Name:      "get_weather",
				Arguments: `{"city":"Hangzhou"}`,
			},
		},
	})
	response := &dto.OpenAITextResponse{
		Id:    "chatcmpl-test",
		Model: "deepseek-v4-flash",
		Choices: []dto.OpenAITextResponseChoice{
			{
				Message:      message,
				FinishReason: "tool_calls",
			},
		},
	}

	claudeResponse := ResponseOpenAI2Claude(response, &relaycommon.RelayInfo{})
	require.Len(t, claudeResponse.Content, 3)
	require.Equal(t, "thinking", claudeResponse.Content[0].Type)
	require.Equal(t, reasoning, *claudeResponse.Content[0].Thinking)
	require.Equal(t, "text", claudeResponse.Content[1].Type)
	require.Equal(t, "Let me check that.", *claudeResponse.Content[1].Text)
	require.Equal(t, "tool_use", claudeResponse.Content[2].Type)
	require.Equal(t, "call_1", claudeResponse.Content[2].Id)
	require.Equal(t, "get_weather", claudeResponse.Content[2].Name)
	require.Equal(t, map[string]interface{}{"city": "Hangzhou"}, claudeResponse.Content[2].Input)
}

func TestResponseOpenAI2ClaudePreservesReasoningContentWithText(t *testing.T) {
	reasoning := "The tool result answers the request."
	message := dto.Message{Role: "assistant"}
	message.ReasoningContent = &reasoning
	message.SetStringContent("Here is the final answer.")
	response := &dto.OpenAITextResponse{
		Id:    "chatcmpl-test",
		Model: "deepseek-v4-flash",
		Choices: []dto.OpenAITextResponseChoice{
			{
				Message:      message,
				FinishReason: "stop",
			},
		},
	}

	claudeResponse := ResponseOpenAI2Claude(response, &relaycommon.RelayInfo{})
	require.Len(t, claudeResponse.Content, 2)
	require.Equal(t, "thinking", claudeResponse.Content[0].Type)
	require.Equal(t, reasoning, *claudeResponse.Content[0].Thinking)
	require.Equal(t, "text", claudeResponse.Content[1].Type)
	require.Equal(t, "Here is the final answer.", *claudeResponse.Content[1].Text)
}

func TestResponseOpenAI2ClaudePreservesReasoningFromChoiceJSON(t *testing.T) {
	raw := common.RawMessage(`{
		"id":"chatcmpl-json",
		"model":"deepseek-v4-flash",
		"choices":[
			{
				"index":0,
				"message":{
					"role":"assistant",
					"content":"Here is the final answer.",
					"reasoning_content":"The tool result answers the request."
				},
				"finish_reason":"stop"
			}
		]
	}`)

	var response dto.OpenAITextResponse
	require.NoError(t, common.Unmarshal(raw, &response))

	claudeResponse := ResponseOpenAI2Claude(&response, &relaycommon.RelayInfo{})
	require.Len(t, claudeResponse.Content, 2)
	require.Equal(t, "thinking", claudeResponse.Content[0].Type)
	require.Equal(t, "The tool result answers the request.", *claudeResponse.Content[0].Thinking)
	require.Equal(t, "text", claudeResponse.Content[1].Type)
	require.Equal(t, "Here is the final answer.", *claudeResponse.Content[1].Text)
}
