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
