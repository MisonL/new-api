package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func marshalStreamChunk(t *testing.T, chunk dto.ChatCompletionsStreamResponse) string {
	t.Helper()
	raw, err := common.Marshal(chunk)
	require.NoError(t, err)
	return "data: " + string(raw) + "\n"
}

func indexAfter(t *testing.T, text string, marker string, after int) int {
	t.Helper()
	index := strings.Index(text[after:], marker)
	require.NotEqualf(t, -1, index, "missing marker %s in output: %s", marker, text)
	return after + index
}

func TestOaiChatToResponsesStreamHandler(t *testing.T) {
	t.Parallel()

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
	})

	roleChunk := dto.ChatCompletionsStreamResponse{
		Id:      "chatcmpl_test",
		Object:  "chat.completion.chunk",
		Created: 1710000001,
		Model:   "gpt-5",
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Index: 0,
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					Role: "assistant",
				},
			},
		},
	}

	textChunk := dto.ChatCompletionsStreamResponse{
		Id:      "chatcmpl_test",
		Object:  "chat.completion.chunk",
		Created: 1710000001,
		Model:   "gpt-5",
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Index: 0,
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					Content: common.GetPointer("hello"),
				},
			},
		},
	}

	toolIndex := 0
	toolChunk := dto.ChatCompletionsStreamResponse{
		Id:      "chatcmpl_test",
		Object:  "chat.completion.chunk",
		Created: 1710000001,
		Model:   "gpt-5",
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Index: 0,
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					ToolCalls: []dto.ToolCallResponse{
						{
							Index: common.GetPointer(toolIndex),
							ID:    "call_1",
							Type:  "function",
							Function: dto.FunctionResponse{
								Name:      "lookup",
								Arguments: "{\"q\":\"x\"}",
							},
						},
					},
				},
			},
		},
	}

	finishReason := "tool_calls"
	finishChunk := dto.ChatCompletionsStreamResponse{
		Id:      "chatcmpl_test",
		Object:  "chat.completion.chunk",
		Created: 1710000001,
		Model:   "gpt-5",
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Index:        0,
				FinishReason: &finishReason,
			},
		},
		Usage: &dto.Usage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}

	body := strings.Builder{}
	body.WriteString(marshalStreamChunk(t, roleChunk))
	body.WriteString(marshalStreamChunk(t, textChunk))
	body.WriteString(marshalStreamChunk(t, toolChunk))
	body.WriteString(marshalStreamChunk(t, finishChunk))
	body.WriteString("data: [DONE]\n")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	resp := &http.Response{
		Body: io.NopCloser(strings.NewReader(body.String())),
	}
	info := &relaycommon.RelayInfo{
		DisablePing: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-5",
		},
	}

	usage, apiErr := OaiChatToResponsesStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, 10, usage.InputTokens)
	require.Equal(t, 5, usage.OutputTokens)
	require.Equal(t, 15, usage.TotalTokens)

	output := recorder.Body.String()
	createdIndex := indexAfter(t, output, "event: response.created", 0)
	inProgressIndex := indexAfter(t, output, "event: response.in_progress", createdIndex+1)
	itemAddedIndex := indexAfter(t, output, "event: response.output_item.added", inProgressIndex+1)
	contentAddedIndex := indexAfter(t, output, "event: response.content_part.added", itemAddedIndex+1)
	textDeltaIndex := indexAfter(t, output, "event: response.output_text.delta", contentAddedIndex+1)
	functionDeltaIndex := indexAfter(t, output, "event: response.function_call_arguments.delta", textDeltaIndex+1)
	textDoneIndex := indexAfter(t, output, "event: response.output_text.done", functionDeltaIndex+1)
	contentDoneIndex := indexAfter(t, output, "event: response.content_part.done", textDoneIndex+1)
	firstItemDoneIndex := indexAfter(t, output, "event: response.output_item.done", contentDoneIndex+1)
	functionDoneIndex := indexAfter(t, output, "event: response.function_call_arguments.done", firstItemDoneIndex+1)
	secondItemDoneIndex := indexAfter(t, output, "event: response.output_item.done", functionDoneIndex+1)
	completedIndex := indexAfter(t, output, "event: response.completed", secondItemDoneIndex+1)

	require.Less(t, createdIndex, inProgressIndex)
	require.Less(t, inProgressIndex, itemAddedIndex)
	require.Less(t, itemAddedIndex, contentAddedIndex)
	require.Less(t, contentAddedIndex, textDeltaIndex)
	require.Less(t, textDeltaIndex, functionDeltaIndex)
	require.Less(t, functionDeltaIndex, textDoneIndex)
	require.Less(t, textDoneIndex, contentDoneIndex)
	require.Less(t, contentDoneIndex, firstItemDoneIndex)
	require.Less(t, firstItemDoneIndex, functionDoneIndex)
	require.Less(t, functionDoneIndex, secondItemDoneIndex)
	require.Less(t, secondItemDoneIndex, completedIndex)
	require.Equal(t, 2, strings.Count(output, "event: response.output_item.done"))

	require.Contains(t, output, "\"text\":\"hello\"")
	require.Contains(t, output, "\"call_id\":\"call_1\"")
	require.Contains(t, output, "\"name\":\"lookup\"")
	require.Contains(t, output, "\"arguments\":\"{\\\"q\\\":\\\"x\\\"}\"")
	require.Contains(t, output, "\"total_tokens\":15")
}
