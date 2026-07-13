package openaicompat

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestResponsesRequestToChatCompletionsRequestRejectsCustomToolsByDefault(t *testing.T) {
	_, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: mustMarshalJSON(t, []map[string]any{{
			"type":    "custom_tool_call",
			"call_id": "call_custom_1",
			"name":    "shell",
			"input":   "pwd",
		}}),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "custom tool bridge is not enabled")
}

func TestResponsesRequestToChatCompletionsRequestRejectsMalformedCustomToolHistory(t *testing.T) {
	_, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: mustMarshalJSON(t, []map[string]any{{
			"type":    "custom_tool_call",
			"call_id": "call_custom_1",
			"name":    "shell",
		}}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.Error(t, err)
	require.Contains(t, err.Error(), "input is required")
}

func TestResponsesRequestToChatCompletionsRequestRejectsNonStringCustomToolInput(t *testing.T) {
	for _, input := range []any{nil, map[string]any{"command": "pwd"}, []any{"pwd"}, true, 1} {
		_, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
			Model: "gpt-5.6-sol",
			Input: mustMarshalJSON(t, []map[string]any{{
				"type":    "custom_tool_call",
				"call_id": "call_custom_1",
				"name":    "shell",
				"input":   input,
			}}),
		}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

		require.Error(t, err)
		require.Contains(t, err.Error(), "input must be a string")
	}
}

func TestResponsesRequestToChatCompletionsRequestValidatesStructuredToolOutput(t *testing.T) {
	tests := []struct {
		name        string
		output      any
		wantContent string
		wantErr     string
	}{
		{
			name:        "text items",
			output:      []map[string]any{{"type": "input_text", "text": "completed"}, {"type": "input_text", "text": "next"}},
			wantContent: "completed\nnext",
		},
		{
			name:    "object",
			output:  map[string]any{"result": "completed"},
			wantErr: "output must be a string or an array of input_text items",
		},
		{
			name:    "null",
			output:  nil,
			wantErr: "output must be a string or an array of input_text items",
		},
		{
			name:    "invalid text item",
			output:  []map[string]any{{"type": "input_text", "text": true}},
			wantErr: "output[0].text must be a string",
		},
		{
			name:    "image item",
			output:  []map[string]any{{"type": "input_image", "image_url": "https://example.com/image.png"}},
			wantErr: `content type "input_image" is not supported`,
		},
		{
			name:    "encrypted item",
			output:  []map[string]any{{"type": "encrypted_content", "encrypted_content": "opaque"}},
			wantErr: `content type "encrypted_content" is not supported`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chatReq, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
				Model: "gpt-5.6-sol",
				Input: mustMarshalJSON(t, []map[string]any{{
					"type":    "function_call_output",
					"call_id": "call_function_1",
					"output":  tt.output,
				}}),
			}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Len(t, chatReq.Messages, 1)
			require.Equal(t, tt.wantContent, chatReq.Messages[0].StringContent())
		})
	}
}

func TestParseChatCustomToolPayloadRequiresStringInput(t *testing.T) {
	for _, payload := range []map[string]any{
		{"name": "shell"},
		{"name": "shell", "input": nil},
		{"name": "shell", "input": map[string]any{"command": "pwd"}},
		{"name": "shell", "input": []any{"pwd"}},
	} {
		_, _, err := parseChatCustomToolPayload(mustMarshalJSON(t, payload))
		require.Error(t, err)
	}
}

func TestResponsesRequestToChatCompletionsRequestBridgesCustomToolsWhenEnabled(t *testing.T) {
	chatReq, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: mustMarshalJSON(t, []map[string]any{
			{"role": "user", "content": "run the command"},
			{"type": "custom_tool_call", "call_id": "call_custom_1", "name": "shell", "input": "pwd"},
			{"type": "custom_tool_call_output", "call_id": "call_custom_1", "output": "/tmp/work"},
		}),
		ToolChoice: mustMarshalJSON(t, map[string]any{"type": "custom", "name": "shell"}),
		Tools: mustMarshalJSON(t, []map[string]any{
			{"type": "tool_search"},
			{"type": "custom", "name": "shell", "description": "Run a shell command.", "format": map[string]any{"type": "text"}},
		}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})
	require.NoError(t, err)
	require.Len(t, chatReq.Tools, 2)
	require.Equal(t, "function", chatReq.Tools[0].Type)
	require.Equal(t, "tool_search", chatReq.Tools[0].Function.Name)
	require.Equal(t, "function", chatReq.Tools[1].Type)
	require.Equal(t, "newapi_custom__shell", chatReq.Tools[1].Function.Name)
	require.Contains(t, chatReq.Tools[1].Function.Description, "Run a shell command.")
	require.JSONEq(t, `{"type":"object","additionalProperties":false,"properties":{"input":{"type":"string","description":"Complete input for the custom tool."}},"required":["input"]}`, string(mustMarshalJSON(t, chatReq.Tools[1].Function.Parameters)))
	require.Equal(t, map[string]any{"type": "function", "function": map[string]any{"name": "newapi_custom__shell"}}, chatReq.ToolChoice)

	require.Len(t, chatReq.Messages, 3)
	require.Equal(t, "user", chatReq.Messages[0].Role)
	require.Equal(t, "assistant", chatReq.Messages[1].Role)
	toolCalls := chatReq.Messages[1].ParseToolCalls()
	require.Len(t, toolCalls, 1)
	require.Equal(t, "call_custom_1", toolCalls[0].ID)
	require.Equal(t, "function", toolCalls[0].Type)
	require.Equal(t, "newapi_custom__shell", toolCalls[0].Function.Name)
	require.JSONEq(t, `{"input":"pwd"}`, toolCalls[0].Function.Arguments)
	require.Equal(t, "tool", chatReq.Messages[2].Role)
	require.Equal(t, "call_custom_1", chatReq.Messages[2].ToolCallId)
	require.Equal(t, "/tmp/work", chatReq.Messages[2].StringContent())
}

func TestResponsesRequestToChatCompletionsRequestBridgesAdditionalToolsWhenEnabled(t *testing.T) {
	chatReq, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: mustMarshalJSON(t, []any{
			map[string]any{"type": "additional_tools", "role": "developer", "tools": []map[string]any{
				{"type": "custom", "name": "exec", "description": "Run a shell command.", "format": map[string]any{"type": "text"}},
				{"type": "function", "name": "wait", "parameters": map[string]any{"type": "object"}},
				{"type": "namespace", "name": "collaboration", "tools": []map[string]any{{"type": "function", "name": "send_message", "parameters": map[string]any{"type": "object"}}}},
			}},
			map[string]any{"type": "message", "role": "user", "content": []map[string]any{{"type": "input_text", "text": "run the task"}}},
		}),
		Tools: mustMarshalJSON(t, []map[string]any{{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}}}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.NoError(t, err)
	require.Len(t, chatReq.Tools, 4)
	require.Equal(t, "lookup", chatReq.Tools[0].Function.Name)
	require.Equal(t, "function", chatReq.Tools[1].Type)
	require.Equal(t, "newapi_custom__exec", chatReq.Tools[1].Function.Name)
	require.Equal(t, "wait", chatReq.Tools[2].Function.Name)
	require.Equal(t, "collaboration__send_message", chatReq.Tools[3].Function.Name)
	require.Len(t, chatReq.Messages, 1)
	require.Equal(t, "user", chatReq.Messages[0].Role)
	require.Equal(t, "run the task", chatReq.Messages[0].StringContent())
}

func TestResponsesRequestToChatCompletionsRequestSeparatesFunctionAndCustomToolNames(t *testing.T) {
	chatReq, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Tools: mustMarshalJSON(t, []map[string]any{
			{"type": "function", "name": "exec", "parameters": map[string]any{"type": "object"}},
			{"type": "custom", "name": "exec", "format": map[string]any{"type": "text"}},
		}),
		Input: mustMarshalJSON(t, "continue"),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.NoError(t, err)
	require.Len(t, chatReq.Tools, 2)
	require.Equal(t, "exec", chatReq.Tools[0].Function.Name)
	require.Equal(t, "newapi_custom__exec", chatReq.Tools[1].Function.Name)
}

func TestResponsesRequestToChatCompletionsRequestRejectsUnsupportedCustomToolFormat(t *testing.T) {
	_, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Tools: mustMarshalJSON(t, []map[string]any{
			{"type": "custom", "name": "exec", "format": map[string]any{"type": "grammar", "syntax": "lark"}},
		}),
		Input: mustMarshalJSON(t, "continue"),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.Error(t, err)
	require.Contains(t, err.Error(), `format type "grammar" is not supported`)
}

func TestResponsesRequestToChatCompletionsRequestBridgesStringCustomTool(t *testing.T) {
	chatReq, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Tools: common.RawMessage(`["exec"]`),
		Input: mustMarshalJSON(t, "continue"),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.NoError(t, err)
	require.Len(t, chatReq.Tools, 1)
	require.Equal(t, "function", chatReq.Tools[0].Type)
	require.Equal(t, "newapi_custom__exec", chatReq.Tools[0].Function.Name)
}

func TestResponsesRequestToChatCompletionsRequestRejectsUnavailableToolChoice(t *testing.T) {
	_, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model:      "gpt-5.6-sol",
		Input:      mustMarshalJSON(t, "continue"),
		ToolChoice: mustMarshalJSON(t, map[string]any{"type": "custom", "name": "exec"}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.Error(t, err)
	require.Contains(t, err.Error(), "not available")
}

func TestResponsesRequestToChatCompletionsRequestRejectsUnsupportedBuiltInTools(t *testing.T) {
	_, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model:      "gpt-5.6-sol",
		Input:      mustMarshalJSON(t, "continue"),
		ToolChoice: mustMarshalJSON(t, "auto"),
		Tools: mustMarshalJSON(t, []map[string]any{
			{"type": "web_search"},
			{"type": "file_search"},
		}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.Error(t, err)
	require.Contains(t, err.Error(), `tool type "web_search" is not supported`)
}

func TestResponsesRequestToChatCompletionsRequestRejectsUnsupportedNestedBuiltInTools(t *testing.T) {
	_, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: mustMarshalJSON(t, "continue"),
		Tools: mustMarshalJSON(t, []map[string]any{{
			"type":  "namespace",
			"name":  "workspace",
			"tools": []map[string]any{{"type": "web_search"}},
		}}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.Error(t, err)
	require.Contains(t, err.Error(), `namespace tool type "web_search" is not supported`)
}

func TestResponsesRequestToChatCompletionsRequestPreservesStrictFunctionTools(t *testing.T) {
	chatReq, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: mustMarshalJSON(t, []any{
			map[string]any{"type": "additional_tools", "role": "developer", "tools": []map[string]any{
				{"type": "function", "name": "lookup", "strict": true, "parameters": map[string]any{"type": "object"}},
				{"type": "namespace", "name": "workspace", "tools": []map[string]any{{"type": "function", "name": "read", "strict": true, "parameters": map[string]any{"type": "object"}}}},
			}},
			map[string]any{"type": "message", "role": "user", "content": []map[string]any{{"type": "input_text", "text": "continue"}}},
		}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.NoError(t, err)
	require.Len(t, chatReq.Tools, 2)
	require.NotNil(t, chatReq.Tools[0].Function.Strict)
	require.True(t, *chatReq.Tools[0].Function.Strict)
	require.NotNil(t, chatReq.Tools[1].Function.Strict)
	require.True(t, *chatReq.Tools[1].Function.Strict)
}

func TestResponsesRequestToChatCompletionsRequestRejectsMalformedFunctionToolStrict(t *testing.T) {
	_, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: mustMarshalJSON(t, "continue"),
		Tools: mustMarshalJSON(t, []map[string]any{
			{"type": "function", "name": "lookup", "strict": "true", "parameters": map[string]any{"type": "object"}},
		}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.Error(t, err)
	require.Contains(t, err.Error(), "strict must be a boolean")
}

func TestResponsesRequestToChatCompletionsRequestGroupsConsecutiveToolCalls(t *testing.T) {
	chatReq, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: mustMarshalJSON(t, []map[string]any{
			{"type": "function_call", "call_id": "call_function", "name": "lookup", "arguments": `{"query":"status"}`},
			{"type": "custom_tool_call", "call_id": "call_custom", "name": "shell", "input": "pwd"},
			{"type": "tool_search_call", "call_id": "call_search", "arguments": `{"query":"mail"}`},
			{"type": "function_call_output", "call_id": "call_function", "output": "found"},
			{"type": "custom_tool_call_output", "call_id": "call_custom", "output": "/tmp"},
			{"type": "tool_search_output", "call_id": "call_search", "status": "completed", "execution": "client", "tools": []map[string]any{}},
		}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.NoError(t, err)
	require.Len(t, chatReq.Messages, 4)
	calls := chatReq.Messages[0].ParseToolCalls()
	require.Len(t, calls, 3)
	require.Equal(t, "call_function", calls[0].ID)
	require.Equal(t, "lookup", calls[0].Function.Name)
	require.Equal(t, "call_custom", calls[1].ID)
	require.Equal(t, "newapi_custom__shell", calls[1].Function.Name)
	require.Equal(t, "call_search", calls[2].ID)
	require.Equal(t, "tool_search", calls[2].Function.Name)
	for index, callID := range []string{"call_function", "call_custom", "call_search"} {
		require.Equal(t, "tool", chatReq.Messages[index+1].Role)
		require.Equal(t, callID, chatReq.Messages[index+1].ToolCallId)
	}
}

func TestResponsesRequestToChatCompletionsRequestRejectsNamespacedHistoryWithoutToolName(t *testing.T) {
	_, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: mustMarshalJSON(t, []map[string]any{{
			"type":      "function_call",
			"call_id":   "call_namespace",
			"namespace": "workspace",
			"arguments": `{"path":"/tmp"}`,
		}}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.Error(t, err)
	require.Contains(t, err.Error(), "requires call_id and name")
}

func TestResponsesChatCompatibilityBridgesNamespacedCustomTools(t *testing.T) {
	chatReq, toolState, err := ResponsesRequestToChatCompletionsRequestWithState(&dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: mustMarshalJSON(t, []any{
			map[string]any{"type": "additional_tools", "role": "developer", "tools": []map[string]any{{
				"type":  "namespace",
				"name":  "workspace",
				"tools": []map[string]any{{"type": "custom", "name": "exec", "format": map[string]any{"type": "text"}}},
			}}},
			map[string]any{"type": "message", "role": "user", "content": []map[string]any{{"type": "input_text", "text": "run a command"}}},
		}),
		ToolChoice: mustMarshalJSON(t, map[string]any{"type": "namespace", "name": "workspace"}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.NoError(t, err)
	require.Len(t, chatReq.Tools, 1)
	require.Equal(t, "newapi_custom__workspace__exec", chatReq.Tools[0].Function.Name)
	require.Equal(t, "required", chatReq.ToolChoice)

	chatResp := &dto.OpenAITextResponse{Model: "gpt-5.6-sol", Choices: []dto.OpenAITextResponseChoice{{Message: dto.Message{Role: "assistant", Content: ""}}}}
	chatResp.Choices[0].Message.SetToolCalls([]dto.ToolCallRequest{{
		ID:   "call_namespaced_custom",
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      "newapi_custom__workspace__exec",
			Arguments: `{"input":"pwd"}`,
		},
	}})

	responsesResp, _, err := ChatCompletionsResponseToResponsesResponseWithOptionsAndState(
		chatResp,
		"resp_namespaced_custom",
		ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true},
		toolState,
	)
	require.NoError(t, err)
	require.Len(t, responsesResp.Output, 1)
	require.Equal(t, "custom_tool_call", responsesResp.Output[0].Type)
	require.Equal(t, "workspace", responsesResp.Output[0].Namespace)
	require.Equal(t, "exec", responsesResp.Output[0].Name)
	require.Equal(t, "pwd", responsesResp.Output[0].Input)
}

func TestResponsesRequestToChatCompletionsRequestBridgesNamespacedCustomToolChoice(t *testing.T) {
	chatReq, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: mustMarshalJSON(t, []any{
			map[string]any{"type": "additional_tools", "role": "developer", "tools": []map[string]any{{
				"type":  "namespace",
				"name":  "workspace",
				"tools": []map[string]any{{"type": "custom", "name": "exec", "format": map[string]any{"type": "text"}}},
			}}},
			map[string]any{"type": "message", "role": "user", "content": []map[string]any{{"type": "input_text", "text": "run a command"}}},
		}),
		ToolChoice: mustMarshalJSON(t, map[string]any{"type": "custom", "namespace": "workspace", "name": "exec"}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.NoError(t, err)
	require.Equal(t, map[string]any{"type": "function", "function": map[string]any{"name": "newapi_custom__workspace__exec"}}, chatReq.ToolChoice)
}

func TestResponsesRequestToChatCompletionsRequestBridgesNamespacedCustomToolHistory(t *testing.T) {
	chatReq, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: mustMarshalJSON(t, []map[string]any{
			{"type": "custom_tool_call", "call_id": "call_namespaced_custom", "namespace": "workspace", "name": "exec", "input": "pwd"},
			{"type": "custom_tool_call_output", "call_id": "call_namespaced_custom", "output": "/tmp"},
		}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.NoError(t, err)
	require.Len(t, chatReq.Messages, 2)
	calls := chatReq.Messages[0].ParseToolCalls()
	require.Len(t, calls, 1)
	require.Equal(t, "newapi_custom__workspace__exec", calls[0].Function.Name)
	require.JSONEq(t, `{"input":"pwd"}`, calls[0].Function.Arguments)
	require.Equal(t, "call_namespaced_custom", chatReq.Messages[1].ToolCallId)
}

func TestResponsesRequestToChatCompletionsRequestUsesOfficialToolSearchOutputTools(t *testing.T) {
	chatReq, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: mustMarshalJSON(t, []map[string]any{{
			"type":      "tool_search_output",
			"call_id":   "call_search",
			"status":    "completed",
			"execution": "client",
			"tools": []map[string]any{{
				"type": "namespace",
				"name": "mail",
				"tools": []map[string]any{{
					"type":       "function",
					"name":       "search",
					"parameters": map[string]any{"type": "object"},
				}},
			}},
		}}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.NoError(t, err)
	require.Len(t, chatReq.Messages, 1)
	require.Equal(t, "tool", chatReq.Messages[0].Role)
	require.Equal(t, "call_search", chatReq.Messages[0].ToolCallId)
	require.JSONEq(t, `[{"type":"namespace","name":"mail","tools":[{"type":"function","name":"search","parameters":{"type":"object"}}]}]`, chatReq.Messages[0].StringContent())
	require.Len(t, chatReq.Tools, 1)
	require.Equal(t, "mail__search", chatReq.Tools[0].Function.Name)
}

func TestResponsesChatCompatibilityLoadsOfficialToolSearchDefinitions(t *testing.T) {
	const namespace = "mcp__codex_apps__calendar"
	const toolName = "calendar_create_event"
	chatReq, toolState, err := ResponsesRequestToChatCompletionsRequestWithState(&dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: mustMarshalJSON(t, []any{
			map[string]any{"type": "additional_tools", "role": "developer", "tools": []map[string]any{{"type": "tool_search"}}},
			map[string]any{"type": "tool_search_call", "call_id": "call_search", "arguments": `{"query":"calendar"}`},
			map[string]any{
				"type":      "tool_search_output",
				"call_id":   "call_search",
				"status":    "completed",
				"execution": "client",
				"tools": []map[string]any{{
					"type":        "namespace",
					"name":        namespace,
					"description": "Plan events and manage your calendar.",
					"tools": []map[string]any{{
						"type":          "function",
						"name":          toolName,
						"strict":        false,
						"defer_loading": true,
						"parameters":    map[string]any{"type": "object", "properties": map[string]any{}},
					}},
				}},
			},
			map[string]any{"type": "message", "role": "user", "content": []map[string]any{{"type": "input_text", "text": "Create an event."}}},
		}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.NoError(t, err)
	require.Len(t, chatReq.Tools, 2)
	require.Equal(t, "tool_search", chatReq.Tools[0].Function.Name)
	require.Equal(t, namespace+"__"+toolName, chatReq.Tools[1].Function.Name)
	require.Contains(t, chatReq.Tools[1].Function.Description, "Plan events and manage your calendar.")
	require.NotNil(t, chatReq.Tools[1].Function.Strict)
	require.False(t, *chatReq.Tools[1].Function.Strict)
	require.Len(t, chatReq.Messages, 3)
	require.Equal(t, "tool", chatReq.Messages[1].Role)
	require.JSONEq(t, `[{"type":"namespace","name":"mcp__codex_apps__calendar","description":"Plan events and manage your calendar.","tools":[{"type":"function","name":"calendar_create_event","strict":false,"defer_loading":true,"parameters":{"type":"object","properties":{}}}]}]`, chatReq.Messages[1].StringContent())

	chatResp := &dto.OpenAITextResponse{Model: "gpt-5.6-sol", Choices: []dto.OpenAITextResponseChoice{{Message: dto.Message{Role: "assistant", Content: ""}}}}
	chatResp.Choices[0].Message.SetToolCalls([]dto.ToolCallRequest{{
		ID:   "call_calendar",
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      namespace + "__" + toolName,
			Arguments: `{"title":"Planning"}`,
		},
	}})
	responsesResp, _, err := ChatCompletionsResponseToResponsesResponseWithOptionsAndState(
		chatResp,
		"resp_calendar",
		ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true},
		toolState,
	)
	require.NoError(t, err)
	require.Len(t, responsesResp.Output, 1)
	require.Equal(t, "function_call", responsesResp.Output[0].Type)
	require.Equal(t, namespace, responsesResp.Output[0].Namespace)
	require.Equal(t, toolName, responsesResp.Output[0].Name)
}

func TestResponsesRequestToChatCompletionsRequestRejectsMalformedOfficialToolSearchOutput(t *testing.T) {
	_, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: mustMarshalJSON(t, []map[string]any{{
			"type":      "tool_search_output",
			"call_id":   "call_search",
			"status":    "completed",
			"execution": "client",
			"tools":     map[string]any{"namespace": "mail"},
		}}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.Error(t, err)
	require.Contains(t, err.Error(), "tools must be an array")
}

func TestResponsesRequestToChatCompletionsRequestRejectsOfficialToolSearchOutputWithoutStatus(t *testing.T) {
	_, err := ResponsesRequestToChatCompletionsRequestWithOptions(&dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: mustMarshalJSON(t, []map[string]any{{
			"type":      "tool_search_output",
			"call_id":   "call_search",
			"execution": "client",
			"tools":     []map[string]any{},
		}}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})

	require.Error(t, err)
	require.Contains(t, err.Error(), "status must be completed")
}

func TestChatCompletionsResponseToResponsesResponseRestoresCustomToolFromChatFunction(t *testing.T) {
	_, toolState, err := ResponsesRequestToChatCompletionsRequestWithState(&dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: mustMarshalJSON(t, []any{
			map[string]any{"type": "additional_tools", "role": "developer", "tools": []map[string]any{{"type": "custom", "name": "apply_patch", "format": map[string]any{"type": "text"}}}},
			map[string]any{"type": "message", "role": "user", "content": []map[string]any{{"type": "input_text", "text": "patch"}}},
		}),
	}, ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})
	require.NoError(t, err)

	chatResp := &dto.OpenAITextResponse{Model: "gpt-5.6-sol", Choices: []dto.OpenAITextResponseChoice{{Message: dto.Message{Role: "assistant", Content: ""}}}}
	chatResp.Choices[0].Message.SetToolCalls([]dto.ToolCallRequest{{
		ID:   "call_custom_function",
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      "newapi_custom__apply_patch",
			Arguments: `{"input":"*** Begin Patch\n*** End Patch"}`,
		},
	}})

	responsesResp, _, err := ChatCompletionsResponseToResponsesResponseWithOptionsAndState(chatResp, "resp_custom_function", ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true}, toolState)
	require.NoError(t, err)
	require.Len(t, responsesResp.Output, 1)
	require.Equal(t, "custom_tool_call", responsesResp.Output[0].Type)
	require.Equal(t, "apply_patch", responsesResp.Output[0].Name)
	require.Equal(t, "*** Begin Patch\n*** End Patch", responsesResp.Output[0].Input)
	require.Equal(t, "call_custom_function", responsesResp.Output[0].CallId)
}
