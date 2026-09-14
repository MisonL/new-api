package ollama

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestClaudeToOllamaChatDoesNotInjectUnsupportedStreamOptions(t *testing.T) {
	converted, err := (&Adaptor{}).ConvertClaudeRequest(nil, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{SupportStreamOptions: false}}, &dto.ClaudeRequest{
		Model: "qwen3",
		Messages: []dto.ClaudeMessage{{
			Role:    "user",
			Content: "hello",
		}},
	})

	require.NoError(t, err)
	request, ok := converted.(*OllamaChatRequest)
	require.True(t, ok)
	require.Len(t, request.Messages, 1)
}

func TestOpenAIChatToOllamaChatAcceptsStreamOptions(t *testing.T) {
	converted, err := openAIChatToOllamaChat(nil, &dto.GeneralOpenAIRequest{
		StreamOptions: &dto.StreamOptions{IncludeUsage: true},
		Messages:      []dto.Message{{Role: "user", Content: "hello"}},
	})

	require.NoError(t, err)
	require.Len(t, converted.Messages, 1)
}

func TestOpenAIToOllamaGenerateAcceptsStreamOptions(t *testing.T) {
	converted, err := openAIToGenerate(nil, &dto.GeneralOpenAIRequest{
		Prompt:        "hello",
		StreamOptions: &dto.StreamOptions{IncludeUsage: true},
	})

	require.NoError(t, err)
	require.Equal(t, "hello", converted.Prompt)
}

func TestOpenAIChatToOllamaChatRejectsMalformedContent(t *testing.T) {
	_, err := openAIChatToOllamaChat(nil, &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{
			Role:    "user",
			Content: common.RawMessage(`[{"type":"text"}`),
		}},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "content")
}

func TestOpenAIChatToOllamaChatRejectsMalformedToolArguments(t *testing.T) {
	message := dto.Message{Role: "assistant", Content: ""}
	message.SetToolCalls([]dto.ToolCallRequest{{
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      "lookup",
			Arguments: `[{"q":"status"}]`,
		},
	}})

	_, err := openAIChatToOllamaChat(nil, &dto.GeneralOpenAIRequest{Messages: []dto.Message{message}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "arguments")
}

func TestOpenAIChatToOllamaChatPreservesAssistantReasoning(t *testing.T) {
	reasoning := "decide before answering"
	converted, err := openAIChatToOllamaChat(nil, &dto.GeneralOpenAIRequest{Messages: []dto.Message{{
		Role:             "assistant",
		Content:          "answer",
		ReasoningContent: &reasoning,
	}}})

	require.NoError(t, err)
	require.Len(t, converted.Messages, 1)
	require.JSONEq(t, `"decide before answering"`, string(converted.Messages[0].Thinking))
}

func TestOpenAIChatToOllamaChatPreservesThinkingContentAlias(t *testing.T) {
	thinking := "alias reasoning"
	converted, err := openAIChatToOllamaChat(nil, &dto.GeneralOpenAIRequest{Messages: []dto.Message{
		{Role: "assistant", Content: "answer", ThinkingContent: &thinking},
	}})

	require.NoError(t, err)
	require.Len(t, converted.Messages, 1)
	require.JSONEq(t, `"alias reasoning"`, string(converted.Messages[0].Thinking))
}

func TestOpenAIChatToOllamaChatMapsToolCallIDToToolName(t *testing.T) {
	assistant := dto.Message{Role: "assistant", Content: ""}
	assistant.SetToolCalls([]dto.ToolCallRequest{{
		ID:   "call_123",
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      "lookup",
			Arguments: `{"q":"status"}`,
		},
	}})
	converted, err := openAIChatToOllamaChat(nil, &dto.GeneralOpenAIRequest{Messages: []dto.Message{
		assistant,
		{Role: "tool", ToolCallId: "call_123", Content: "ready"},
	}})

	require.NoError(t, err)
	require.Len(t, converted.Messages, 2)
	require.Equal(t, "lookup", converted.Messages[1].ToolName)
	require.Equal(t, "call_123", converted.Messages[1].ToolCallID)
	require.NotNil(t, converted.Messages[0].ToolCalls[0].Function.Index)
	require.Equal(t, 0, *converted.Messages[0].ToolCalls[0].Function.Index)
}

func TestOpenAIChatToOllamaChatRejectsUnknownRole(t *testing.T) {
	_, err := openAIChatToOllamaChat(nil, &dto.GeneralOpenAIRequest{Messages: []dto.Message{{
		Role:    "developer_tool",
		Content: "ignored",
	}}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "role")
}
