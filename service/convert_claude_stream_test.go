package service

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestStreamResponseOpenAI2ClaudeBuffersToolArgumentsUntilStart(t *testing.T) {
	info := newClaudeStreamInfo(1)

	first := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl-test",
		Model: "claude-test",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				ToolCalls: []dto.ToolCallResponse{{
					Index:    testPtr(0),
					ID:       "call_1",
					Function: dto.FunctionResponse{Arguments: "{\"path\""},
				}},
			},
		}},
	}, info)
	require.Equal(t, []string{"message_start"}, claudeResponseTypes(first))
	requireClaudeStreamBlocksWellFormed(t, first)

	info.SendResponseCount = 2
	second := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl-test",
		Model: "claude-test",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				ToolCalls: []dto.ToolCallResponse{{
					Index:    testPtr(0),
					Function: dto.FunctionResponse{Name: "Read", Arguments: ":\"file.go\"}"},
				}},
			},
		}},
	}, info)

	require.Equal(t, []string{"content_block_start", "content_block_delta"}, claudeResponseTypes(second))
	require.Equal(t, "Read", second[0].ContentBlock.Name)
	require.Equal(t, "{\"path\":\"file.go\"}", *second[1].Delta.PartialJson)
	requireClaudeStreamBlocksWellFormed(t, append(first, second...))
}

func TestStreamResponseOpenAI2ClaudeStopsOnlyStartedToolBlocks(t *testing.T) {
	info := newClaudeStreamInfo(1)

	first := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl-test",
		Model: "claude-test",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				ToolCalls: []dto.ToolCallResponse{{
					Index:    testPtr(0),
					Function: dto.FunctionResponse{Arguments: "{\"missing_name\":true}"},
				}},
			},
		}},
	}, info)
	require.Equal(t, []string{"message_start"}, claudeResponseTypes(first))

	info.SendResponseCount = 2
	finishReason := "tool_calls"
	final := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl-test",
		Model: "claude-test",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			FinishReason: &finishReason,
		}},
		Usage: &dto.Usage{PromptTokens: 10, CompletionTokens: 1, TotalTokens: 11},
	}, info)

	require.Equal(t, []string{"message_delta", "message_stop"}, claudeResponseTypes(final))
	requireClaudeStreamBlocksWellFormed(t, append(first, final...))
}

func TestStreamResponseOpenAI2ClaudeDoesNotDuplicateToolBlockStart(t *testing.T) {
	info := newClaudeStreamInfo(1)

	first := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl-test",
		Model: "claude-test",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				ToolCalls: []dto.ToolCallResponse{{
					Index:    testPtr(0),
					ID:       "call_1",
					Function: dto.FunctionResponse{Name: "Edit", Arguments: "{\"file\""},
				}},
			},
		}},
	}, info)
	require.Equal(t, []string{"message_start", "content_block_start", "content_block_delta"}, claudeResponseTypes(first))

	info.SendResponseCount = 2
	second := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl-test",
		Model: "claude-test",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				ToolCalls: []dto.ToolCallResponse{{
					Index:    testPtr(0),
					ID:       "call_1",
					Function: dto.FunctionResponse{Name: "Edit", Arguments: ":\"main.go\"}"},
				}},
			},
		}},
	}, info)

	require.Equal(t, []string{"content_block_delta"}, claudeResponseTypes(second))
	requireClaudeStreamBlocksWellFormed(t, append(first, second...))
}

func TestStreamResponseOpenAI2ClaudeEmitsThinkingTextAndToolBlocksFromOneChunk(t *testing.T) {
	info := newClaudeStreamInfo(1)
	reasoning := "Need to inspect current weather."
	content := "Let me check."

	responses := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl-test",
		Model: "deepseek-v4-flash",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				ReasoningContent: &reasoning,
				Content:          &content,
				ToolCalls: []dto.ToolCallResponse{{
					Index: testPtr(0),
					ID:    "call_1",
					Type:  "function",
					Function: dto.FunctionResponse{
						Name:      "get_weather",
						Arguments: `{"city":"Hangzhou"}`,
					},
				}},
			},
		}},
	}, info)

	require.Equal(t, []string{
		"message_start",
		"content_block_start",
		"content_block_delta",
		"content_block_stop",
		"content_block_start",
		"content_block_delta",
		"content_block_stop",
		"content_block_start",
		"content_block_delta",
	}, claudeResponseTypes(responses))
	require.Equal(t, "thinking", responses[1].ContentBlock.Type)
	require.Equal(t, "thinking_delta", responses[2].Delta.Type)
	require.Equal(t, reasoning, *responses[2].Delta.Thinking)
	require.Equal(t, "text", responses[4].ContentBlock.Type)
	require.Equal(t, "text_delta", responses[5].Delta.Type)
	require.Equal(t, content, *responses[5].Delta.Text)
	require.Equal(t, "tool_use", responses[7].ContentBlock.Type)
	require.Equal(t, "get_weather", responses[7].ContentBlock.Name)
	require.Equal(t, `{"city":"Hangzhou"}`, *responses[8].Delta.PartialJson)
	requireClaudeStreamBlocksWellFormed(t, responses)
}

func TestStreamResponseOpenAI2ClaudeEmitsThinkingAcrossMultipleChunks(t *testing.T) {
	info := newClaudeStreamInfo(1)
	reasoning := "Need to inspect current weather."
	content := "Let me check."

	first := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl-test",
		Model: "deepseek-v4-flash",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				Content: &content,
			},
		}},
	}, info)
	require.Equal(t, []string{"message_start", "content_block_start", "content_block_delta"}, claudeResponseTypes(first))
	require.Equal(t, "text", first[1].ContentBlock.Type)
	require.Equal(t, "text_delta", first[2].Delta.Type)
	require.Equal(t, content, *first[2].Delta.Text)
	require.Nil(t, first[2].Delta.Thinking)

	info.SendResponseCount = 2
	second := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl-test",
		Model: "deepseek-v4-flash",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				ReasoningContent: &reasoning,
			},
		}},
	}, info)

	require.Equal(t, []string{"content_block_stop", "content_block_start", "content_block_delta"}, claudeResponseTypes(second))
	require.Equal(t, "thinking", second[1].ContentBlock.Type)
	require.Equal(t, "thinking_delta", second[2].Delta.Type)
	require.Equal(t, reasoning, *second[2].Delta.Thinking)
	require.Nil(t, second[2].Delta.Text)
	requireClaudeStreamBlocksWellFormed(t, append(first, second...))
}

func TestStreamResponseOpenAI2ClaudeDoesNotExposeReasoningForNonDeepSeekModel(t *testing.T) {
	info := newClaudeStreamInfo(1)
	reasoning := "internal reasoning"
	content := "Visible answer."

	responses := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl-test",
		Model: "gpt-5.5",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				ReasoningContent: &reasoning,
				Content:          &content,
			},
		}},
	}, &relaycommon.RelayInfo{
		SendResponseCount: 1,
		OriginModelName:   "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-5.5",
		},
		ClaudeConvertInfo: info.ClaudeConvertInfo,
	})

	require.Equal(t, []string{"message_start", "content_block_start", "content_block_delta"}, claudeResponseTypes(responses))
	require.Equal(t, "text", responses[1].ContentBlock.Type)
	require.Equal(t, "text_delta", responses[2].Delta.Type)
	require.Equal(t, content, *responses[2].Delta.Text)
	require.Nil(t, responses[2].Delta.Thinking)
	requireClaudeStreamBlocksWellFormed(t, responses)
}

func TestStreamResponseOpenAI2ClaudePreservesDoneChunkTextBeforeUsage(t *testing.T) {
	info := newClaudeStreamInfo(1)
	firstText := "Hello "
	first := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl-test",
		Model: "claude-test",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				Content: &firstText,
			},
		}},
	}, info)
	require.Equal(t, []string{"message_start", "content_block_start", "content_block_delta"}, claudeResponseTypes(first))

	info.SendResponseCount = 2
	finalText := "world"
	finishReason := "stop"
	finalTextChunk := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl-test",
		Model: "claude-test",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				Content: &finalText,
			},
			FinishReason: &finishReason,
		}},
	}, info)
	require.Equal(t, []string{"content_block_delta"}, claudeResponseTypes(finalTextChunk))
	require.Equal(t, finalText, *finalTextChunk[0].Delta.Text)

	info.SendResponseCount = 3
	usageChunk := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl-test",
		Model: "claude-test",
		Usage: &dto.Usage{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5},
	}, info)
	require.Equal(t, []string{"content_block_stop", "message_delta", "message_stop"}, claudeResponseTypes(usageChunk))
	require.Equal(t, "end_turn", *usageChunk[1].Delta.StopReason)
	require.Equal(t, 3, usageChunk[1].Usage.InputTokens)
	require.Equal(t, 2, usageChunk[1].Usage.OutputTokens)
	requireClaudeStreamBlocksWellFormed(t, append(append(first, finalTextChunk...), usageChunk...))
}

func TestStreamResponseOpenAI2ClaudeWithErrorRejectsUnknownFinishReason(t *testing.T) {
	reason := "provider_specific_terminal_state"
	responses, err := StreamResponseOpenAI2ClaudeWithError(&dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{FinishReason: &reason}},
	}, newClaudeStreamInfo(1))
	require.Error(t, err)
	require.Nil(t, responses)
	require.Contains(t, err.Error(), "finish_reason")
}

func newClaudeStreamInfo(sendResponseCount int) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		SendResponseCount: sendResponseCount,
		ClaudeConvertInfo: &relaycommon.ClaudeConvertInfo{
			LastMessagesType: relaycommon.LastMessageTypeNone,
		},
	}
}

func testPtr[T any](value T) *T {
	return &value
}

func claudeResponseTypes(responses []*dto.ClaudeResponse) []string {
	types := make([]string, 0, len(responses))
	for _, response := range responses {
		types = append(types, response.Type)
	}
	return types
}

func requireClaudeStreamBlocksWellFormed(t *testing.T, responses []*dto.ClaudeResponse) {
	t.Helper()
	openBlocks := map[int]bool{}
	for _, response := range responses {
		if response.Index == nil {
			continue
		}
		index := *response.Index
		switch response.Type {
		case "content_block_start":
			require.Falsef(t, openBlocks[index], "duplicate content block start at index %d", index)
			openBlocks[index] = true
		case "content_block_delta":
			require.Truef(t, openBlocks[index], "content block delta without start at index %d", index)
		case "content_block_stop":
			require.Truef(t, openBlocks[index], "content block stop without start at index %d", index)
			delete(openBlocks, index)
		}
	}
}

// 上游的 finish_reason="function_call"（旧式函数调用终止帧）在入口预检里被
// stopReasonOpenAI2ClaudeStrict 接受并映射为 tool_use，但 message_delta 的
// 产出点用的是非严格映射，其表里没有 function_call，原样透传。Anthropic
// SDK 的 stop_reason 枚举没有这个值，客户端会反序列化失败或走未知分支。
// 非流式路径走严格映射，同一上游状态流式却给出非法值，必须是同一个值。
func TestStreamResponseOpenAI2ClaudeFunctionCallFinishReasonMapsToToolUse(t *testing.T) {
	info := newClaudeStreamInfo(1)

	toolStart := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl-test",
		Model: "claude-test",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				ToolCalls: []dto.ToolCallResponse{{
					Index:    testPtr(0),
					ID:       "call_1",
					Type:     "function",
					Function: dto.FunctionResponse{Name: "Read", Arguments: "{}"},
				}},
			},
		}},
	}, info)
	require.NotEmpty(t, toolStart)

	info.SendResponseCount = 2
	final := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl-test",
		Model: "claude-test",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			FinishReason: testPtr("function_call"),
		}},
		Usage: &dto.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
	}, info)

	var stopReason string
	for _, response := range final {
		if response.Type == "message_delta" && response.Delta != nil && response.Delta.StopReason != nil {
			stopReason = *response.Delta.StopReason
		}
	}
	require.Equal(t, "tool_use", stopReason,
		"流式与非流式必须给出同一 stop_reason；function_call 不在 Anthropic 枚举里")
	requireClaudeStreamBlocksWellFormed(t, append(toolStart, final...))
}

// usage-only 终端帧（choices 为空）同样携带 info.FinishReason，走的是同一
// 非严格映射，也要被钉住。
func TestStreamResponseOpenAI2ClaudeUsageOnlyFunctionCallFinishReasonMapsToToolUse(t *testing.T) {
	info := newClaudeStreamInfo(2)
	info.FinishReason = "function_call"

	final := StreamResponseOpenAI2Claude(&dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl-test",
		Model: "claude-test",
		Usage: &dto.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
	}, info)

	var stopReason string
	for _, response := range final {
		if response.Type == "message_delta" && response.Delta != nil && response.Delta.StopReason != nil {
			stopReason = *response.Delta.StopReason
		}
	}
	require.Equal(t, "tool_use", stopReason)
}
