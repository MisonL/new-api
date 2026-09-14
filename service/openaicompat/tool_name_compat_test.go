package openaicompat

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestResponsesChatCompatibilityKeepsInvalidToolNameReferencesAligned(t *testing.T) {
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6",
		Tools: mustMarshalJSON(t, []map[string]any{{
			"type": "function",
			"name": "mcp.calendar/search.v1",
		}}),
		ToolChoice: mustMarshalJSON(t, map[string]any{
			"type": "function",
			"name": "mcp.calendar/search.v1",
		}),
		Input: mustMarshalJSON(t, []map[string]any{
			{
				"type":      "function_call",
				"call_id":   "call_stable",
				"name":      "mcp.calendar/search.v1",
				"arguments": "{}",
			},
		}),
	}

	chatRequest, toolState, err := ResponsesRequestToChatCompletionsRequestWithState(request, ResponsesChatCompatibilityOptions{})
	require.NoError(t, err)
	require.Len(t, chatRequest.Tools, 1)
	alias := chatRequest.Tools[0].Function.Name
	require.NotEqual(t, "mcp.calendar/search.v1", alias)
	require.Regexp(t, `^[a-zA-Z0-9_-]+$`, alias)
	require.Equal(t, alias, chatRequest.ToolChoice.(map[string]any)["function"].(map[string]any)["name"])

	calls := chatRequest.Messages[0].ParseToolCalls()
	require.Len(t, calls, 1)
	require.Equal(t, alias, calls[0].Function.Name)
	require.Equal(t, "call_stable", calls[0].ID)

	identity, ok := toolState.Resolve(alias)
	require.True(t, ok)
	require.Equal(t, "mcp.calendar/search.v1", identity.Name)
}

func TestChatResponsesCompatibilityKeepsInvalidToolNameReferencesAligned(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{
		Model: "gpt-5.6",
		Tools: []dto.ToolCallRequest{{
			Type:     "function",
			Function: dto.FunctionRequest{Name: "mcp.calendar/search.v1"},
		}},
		ToolChoice: map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "mcp.calendar/search.v1"},
		},
		Messages: []dto.Message{{
			Role:    "assistant",
			Content: "",
		}},
	}
	request.Messages[0].SetToolCalls([]dto.ToolCallRequest{{
		ID:   "call_stable",
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      "mcp.calendar/search.v1",
			Arguments: "{}",
		},
	}})

	responsesRequest, err := ChatCompletionsRequestToResponsesRequest(request)
	require.NoError(t, err)
	var tools []map[string]any
	require.NoError(t, common.Unmarshal(responsesRequest.Tools, &tools))
	alias := tools[0]["name"].(string)
	require.NotEqual(t, "mcp.calendar/search.v1", alias)
	require.Regexp(t, `^[a-zA-Z0-9_-]+$`, alias)

	var toolChoice map[string]any
	require.NoError(t, common.Unmarshal(responsesRequest.ToolChoice, &toolChoice))
	require.Equal(t, alias, toolChoice["name"])

	var input []map[string]any
	require.NoError(t, common.Unmarshal(responsesRequest.Input, &input))
	require.Len(t, input, 2)
	require.Equal(t, alias, input[1]["name"])
	require.Equal(t, "call_stable", input[1]["call_id"])
}

func TestChatResponsesCompatibilityRejectsAliasCollisions(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{
		Model: "gpt-5.6",
		Tools: []dto.ToolCallRequest{
			{Type: "function", Function: dto.FunctionRequest{Name: "mcp.calendar/search.v1"}},
			{Type: "function", Function: dto.FunctionRequest{Name: dto.NormalizeToolName("mcp.calendar/search.v1")}},
		},
		Messages: []dto.Message{{Role: "user", Content: "continue"}},
	}

	_, err := ChatCompletionsRequestToResponsesRequest(request)
	require.Error(t, err)
	require.Contains(t, err.Error(), "conflicting")
}

func TestChatResponsesCompatibilityNormalizesUnknownInvalidHistoryName(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{
		Model:    "gpt-5.6",
		Messages: []dto.Message{{Role: "assistant", Content: ""}},
	}
	request.Messages[0].SetToolCalls([]dto.ToolCallRequest{{
		ID:       "call_unknown",
		Type:     "function",
		Function: dto.FunctionRequest{Name: "mcp.calendar/search.v1", Arguments: "{}"},
	}})

	responsesRequest, err := ChatCompletionsRequestToResponsesRequest(request)
	require.NoError(t, err)
	var input []map[string]any
	require.NoError(t, common.Unmarshal(responsesRequest.Input, &input))
	require.Equal(t, dto.NormalizeToolName("mcp.calendar/search.v1"), input[1]["name"])
}

func TestResponsesResponseToChatCompletionsResponseNormalizesNamespacedToolName(t *testing.T) {
	resp := &dto.OpenAIResponsesResponse{
		Model: "gpt-5.6",
		Output: []dto.ResponsesOutput{{
			Type:      "function_call",
			ID:        "fc_namespace_invalid",
			CallId:    "call_namespace_invalid",
			Namespace: "mcp.calendar/v1",
			Name:      "search.tool",
			Arguments: common.RawMessage(`{"q":"hello"}`),
		}},
	}

	chatResp, _, err := ResponsesResponseToChatCompletionsResponse(resp, "chatcmpl_namespace_invalid")
	require.NoError(t, err)
	calls := chatResp.Choices[0].Message.ParseToolCalls()
	require.Len(t, calls, 1)
	want := dto.NormalizeToolName("mcp.calendar/v1__search.tool")
	require.Equal(t, want, calls[0].Function.Name)
	require.Regexp(t, `^[a-zA-Z0-9_-]+$`, calls[0].Function.Name)
}
