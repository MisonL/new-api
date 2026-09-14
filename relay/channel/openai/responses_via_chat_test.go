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
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestGetResponsesCompatIDUsesRecognizablePrefix(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	c.Set(common.RequestIdKey, "test-request-id")

	responseID := getResponsesCompatID(c)

	require.Equal(t, "resp_newapi_chatcmp_test-request-id", responseID)
}

func marshalStreamChunk(t *testing.T, chunk dto.ChatCompletionsStreamResponse) string {
	t.Helper()
	raw, err := common.Marshal(chunk)
	require.NoError(t, err)
	return "data: " + string(raw) + "\n"
}

func mustMarshalJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := common.Marshal(value)
	require.NoError(t, err)
	return raw
}

func indexAfter(t *testing.T, text string, marker string, after int) int {
	t.Helper()
	index := strings.Index(text[after:], marker)
	require.NotEqualf(t, -1, index, "missing marker %s in output: %s", marker, text)
	return after + index
}

type recordedResponsesSSEEvent struct {
	eventType string
	payload   dto.ResponsesStreamResponse
}

func recordedResponsesSSEEvents(t *testing.T, output string) []recordedResponsesSSEEvent {
	t.Helper()
	events := make([]recordedResponsesSSEEvent, 0)
	for _, block := range strings.Split(output, "\n\n") {
		var eventType string
		var data string
		for _, line := range strings.Split(block, "\n") {
			if strings.HasPrefix(line, "event: ") {
				eventType = strings.TrimPrefix(line, "event: ")
			}
			if strings.HasPrefix(line, "data: ") {
				data = strings.TrimPrefix(line, "data: ")
			}
		}
		if eventType == "" || data == "" || data == "[DONE]" {
			continue
		}
		var payload dto.ResponsesStreamResponse
		require.NoError(t, common.Unmarshal([]byte(data), &payload))
		events = append(events, recordedResponsesSSEEvent{eventType: eventType, payload: payload})
	}
	return events
}

func responseSSEOutputIndexForItem(t *testing.T, events []recordedResponsesSSEEvent, eventType string, itemID string) int {
	t.Helper()
	for _, event := range events {
		if event.eventType != eventType || event.payload.OutputIndex == nil {
			continue
		}
		actualItemID := event.payload.ItemID
		if event.payload.Item != nil {
			actualItemID = event.payload.Item.ID
		}
		if actualItemID == itemID {
			return *event.payload.OutputIndex
		}
	}
	require.Failf(t, "missing response item event", "event=%s item_id=%s", eventType, itemID)
	return -1
}

func completedResponseFromSSE(t *testing.T, events []recordedResponsesSSEEvent) *dto.OpenAIResponsesResponse {
	t.Helper()
	for _, event := range events {
		if event.eventType == "response.completed" {
			require.NotNil(t, event.payload.Response)
			return event.payload.Response
		}
	}
	require.Fail(t, "missing response.completed event")
	return nil
}

func TestMarshalResponsesCompatArgumentsWrapsValidJSONAsString(t *testing.T) {
	raw, err := marshalResponsesCompatArguments(`{"q":"hello"}`)
	require.NoError(t, err)
	require.JSONEq(t, `"{\"q\":\"hello\"}"`, string(raw))
}

func TestMarshalResponsesCompatArgumentsEscapesInvalidJSON(t *testing.T) {
	raw, err := marshalResponsesCompatArguments(`not-json`)
	require.NoError(t, err)
	require.JSONEq(t, `"not-json"`, string(raw))
}

func TestMarshalResponsesCompatArgumentsBoundaryInputs(t *testing.T) {
	testCases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "empty",
			in:   "",
			want: `""`,
		},
		{
			name: "embedded quotes",
			in:   `not "json"`,
			want: `"not \"json\""`,
		},
		{
			name: "json string stays json",
			in:   `"already escaped"`,
			want: `"\"already escaped\""`,
		},
		{
			name: "special characters escaped",
			in:   "line\n中文",
			want: "\"line\\n中文\"",
		},
		{
			name: "formatted json stays string",
			in:   "{\n  \"q\": \"hello\"\n}",
			want: "\"{\\n  \\\"q\\\": \\\"hello\\\"\\n}\"",
		},
		{
			name: "json trailing comma escaped as string",
			in:   `{"q":"hello",}`,
			want: `"{\"q\":\"hello\",}"`,
		},
		{
			name: "unquoted key escaped as string",
			in:   `{q:"hello"}`,
			want: `"{q:\"hello\"}"`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := marshalResponsesCompatArguments(tc.in)
			require.NoError(t, err)
			require.JSONEq(t, tc.want, string(raw))
		})
	}
}

func TestOaiChatToResponsesStreamHandler(t *testing.T) {
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

func TestOaiChatToResponsesStreamHandlerMapsIncompleteFinishReasons(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		finish     string
		wantReason string
	}{
		{name: "length", finish: "length", wantReason: "max_output_tokens"},
		{name: "content filter", finish: "content_filter", wantReason: "content_filter"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			finishReason := testCase.finish
			text := "partial answer"
			body := marshalStreamChunk(t, dto.ChatCompletionsStreamResponse{
				Choices: []dto.ChatCompletionsStreamResponseChoice{{
					Index: 0,
					Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: &text},
				}},
			}) + marshalStreamChunk(t, dto.ChatCompletionsStreamResponse{
				Choices: []dto.ChatCompletionsStreamResponseChoice{{
					Index:        0,
					FinishReason: &finishReason,
				}},
			}) + "data: [DONE]\n"

			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			info := &relaycommon.RelayInfo{
				DisablePing:  true,
				StreamStatus: relaycommon.NewStreamStatus(),
				ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5"},
			}

			usage, apiErr := OaiChatToResponsesStreamHandler(c, info, &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(body)),
			})

			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			require.Contains(t, recorder.Body.String(), "event: response.incomplete")
			require.NotContains(t, recorder.Body.String(), "event: response.completed")
			events := recordedResponsesSSEEvents(t, recorder.Body.String())
			var terminal *dto.OpenAIResponsesResponse
			for _, event := range events {
				if event.eventType == "response.incomplete" {
					terminal = event.payload.Response
					break
				}
			}
			require.NotNil(t, terminal)
			require.JSONEq(t, `"incomplete"`, string(terminal.Status))
			require.NotNil(t, terminal.IncompleteDetails)
			require.Equal(t, testCase.wantReason, terminal.IncompleteDetails.Reason)
		})
	}
}

func TestOaiChatToResponsesStreamHandlerPreservesReasoningText(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
	})

	reasoningOne := "first "
	reasoningTwo := "thought"
	answer := "answer"
	finishReason := "stop"
	usage := &dto.Usage{PromptTokens: 11, CompletionTokens: 7, TotalTokens: 18}
	body := strings.Builder{}
	body.WriteString(marshalStreamChunk(t, dto.ChatCompletionsStreamResponse{
		Id:      "chatcmpl_reasoning",
		Object:  "chat.completion.chunk",
		Created: 1710000002,
		Model:   "deepseek-v4-flash",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index: 0,
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Role: "assistant"},
		}},
	}))
	body.WriteString(marshalStreamChunk(t, dto.ChatCompletionsStreamResponse{
		Id:      "chatcmpl_reasoning",
		Object:  "chat.completion.chunk",
		Created: 1710000002,
		Model:   "deepseek-v4-flash",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index: 0,
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ReasoningContent: &reasoningOne},
		}},
	}))
	body.WriteString(marshalStreamChunk(t, dto.ChatCompletionsStreamResponse{
		Id:      "chatcmpl_reasoning",
		Object:  "chat.completion.chunk",
		Created: 1710000002,
		Model:   "deepseek-v4-flash",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index: 0,
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ReasoningContent: &reasoningTwo},
		}},
	}))
	body.WriteString(marshalStreamChunk(t, dto.ChatCompletionsStreamResponse{
		Id:      "chatcmpl_reasoning",
		Object:  "chat.completion.chunk",
		Created: 1710000002,
		Model:   "deepseek-v4-flash",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index: 0,
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: &answer},
		}},
	}))
	body.WriteString(marshalStreamChunk(t, dto.ChatCompletionsStreamResponse{
		Id:      "chatcmpl_reasoning",
		Object:  "chat.completion.chunk",
		Created: 1710000002,
		Model:   "deepseek-v4-flash",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index:        0,
			FinishReason: &finishReason,
		}},
		Usage: usage,
	}))
	body.WriteString("data: [DONE]\n")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		DisablePing:  true,
		StreamStatus: relaycommon.NewStreamStatus(),
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "deepseek-v4-flash",
		},
	}

	convertedUsage, apiErr := OaiChatToResponsesStreamHandler(c, info, &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body.String())),
	})

	require.Nil(t, apiErr)
	require.NotNil(t, convertedUsage)
	require.Equal(t, 18, convertedUsage.TotalTokens)
	events := recordedResponsesSSEEvents(t, recorder.Body.String())
	reasoningDeltaCount := 0
	for _, event := range events {
		if event.eventType == "response.reasoning_text.delta" {
			reasoningDeltaCount++
		}
	}
	require.Equal(t, 2, reasoningDeltaCount)

	completed := completedResponseFromSSE(t, events)
	require.Len(t, completed.Output, 2)
	require.Equal(t, "reasoning", completed.Output[0].Type)
	require.Equal(t, "reasoning_text", completed.Output[0].Content[0].Type)
	require.Equal(t, "first thought", completed.Output[0].Content[0].Text)
	require.Equal(t, "message", completed.Output[1].Type)
	require.Equal(t, "answer", completed.Output[1].Content[0].Text)
}

func TestOaiChatToResponsesStreamHandlerReturnsRateLimitBeforeOutput(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"error\":{\"message\":\"rate limited\",\"code\":\"rate_limit_exceeded\"}}\n\n",
		)),
	}
	info := &relaycommon.RelayInfo{
		DisablePing: true,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5"},
	}

	usage, apiErr := OaiChatToResponsesStreamHandler(c, info, resp)
	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)
	require.Empty(t, recorder.Body.String())
}

func TestOaiChatToResponsesStreamHandlerKeepsCommittedRateLimitTerminal(t *testing.T) {
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
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index: 0,
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Role: "assistant"},
		}},
	}
	roleData := marshalStreamChunk(t, roleChunk)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			roleData + "data: {\"error\":{\"message\":\"rate limited\",\"type\":\"rate_limit_error\",\"code\":\"rate_limit_exceeded\"}}\n\n",
		)),
	}
	info := &relaycommon.RelayInfo{
		DisablePing: true,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5"},
	}

	usage, apiErr := OaiChatToResponsesStreamHandler(c, info, resp)
	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusOK, apiErr.StatusCode)
	require.NotEmpty(t, recorder.Body.String())
}

func TestOaiChatToResponsesHandlerMapsRateLimitErrorBody(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			`{"error":{"message":"rate limited","code":"rate_limit_exceeded"}}`,
		)),
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5"},
	}

	usage, apiErr := OaiChatToResponsesHandler(c, info, resp)
	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)
}

func TestOaiChatToResponsesHandlerMapsFailedSSEBody(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`event: response.failed`,
			`data: {"type":"response.failed","error":{"message":"invalid input","type":"invalid_request_error","param":"input","code":"invalid_type"}}`,
			``,
		}, "\n"))),
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5"},
	}

	usage, apiErr := OaiChatToResponsesHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.Equal(t, types.ErrorCode("invalid_type"), apiErr.GetErrorCode())
	require.Equal(t, "response.failed", common.GetContextKeyString(c, constant.ContextKeyResponsesErrorEvent))
	require.Empty(t, recorder.Body.String())
}

func TestOaiChatToResponsesHandlerClassifiesInterruptedBodyReadAsTransportFailure(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"},
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       responsesErrorReadCloser{err: io.ErrUnexpectedEOF},
	}

	usage, apiErr := OaiChatToResponsesHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	require.Equal(t, types.ErrorCodeUpstreamTransportInterrupted, apiErr.GetErrorCode())
	require.False(t, types.IsSkipRetryError(apiErr))
	require.Empty(t, recorder.Body.String())
}

func TestOaiChatToResponsesStreamHandlerRejectsTruncatedUpstreamStream(t *testing.T) {
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
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index: 0,
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Role: "assistant"},
		}},
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(marshalStreamChunk(t, roleChunk))),
	}
	info := &relaycommon.RelayInfo{
		DisablePing: true,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5"},
	}

	usage, apiErr := OaiChatToResponsesStreamHandler(c, info, resp)
	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	require.Equal(t, types.ErrorCodeUpstreamTransportInterrupted, apiErr.GetErrorCode())
	require.NotContains(t, recorder.Body.String(), "event: response.completed")
}

func TestOaiChatToResponsesStreamHandlerRejectsCustomToolCallByDefault(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
	})

	toolIndex := 0
	toolChunk := dto.ChatCompletionsStreamResponse{
		Id:      "chatcmpl_custom",
		Object:  "chat.completion.chunk",
		Created: 1710000001,
		Model:   "gpt-5",
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Index: 0,
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					ToolCalls: []dto.ToolCallResponse{
						{
							Index:  common.GetPointer(toolIndex),
							ID:     "call_custom_1",
							Type:   dto.CustomType,
							Custom: mustMarshalJSON(t, map[string]any{"name": "apply_patch", "input": "*** Begin Patch"}),
						},
					},
				},
			},
		},
	}

	body := strings.Builder{}
	body.WriteString(marshalStreamChunk(t, toolChunk))
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

	_, apiErr := OaiChatToResponsesStreamHandler(c, info, resp)
	require.NotNil(t, apiErr)
	require.Contains(t, apiErr.Error(), "custom tool bridge is not enabled")
}

func TestOaiChatToResponsesStreamHandlerCustomToolCallWhenEnabled(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
	})

	toolIndex := 0
	toolChunk := dto.ChatCompletionsStreamResponse{
		Id:      "chatcmpl_custom",
		Object:  "chat.completion.chunk",
		Created: 1710000001,
		Model:   "gpt-5",
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Index: 0,
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					ToolCalls: []dto.ToolCallResponse{
						{
							Index:  common.GetPointer(toolIndex),
							ID:     "call_custom_1",
							Type:   dto.CustomType,
							Custom: mustMarshalJSON(t, map[string]any{"name": "apply_patch", "input": "*** Begin Patch"}),
						},
					},
				},
			},
		},
	}
	finishReason := "tool_calls"
	finishChunk := dto.ChatCompletionsStreamResponse{
		Id:      "chatcmpl_custom",
		Object:  "chat.completion.chunk",
		Created: 1710000001,
		Model:   "gpt-5",
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Index:        0,
				FinishReason: &finishReason,
			},
		},
	}

	body := strings.Builder{}
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

	_, apiErr := OaiChatToResponsesStreamHandlerWithOptions(
		c,
		info,
		resp,
		service.ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true},
	)
	require.Nil(t, apiErr)

	output := recorder.Body.String()
	require.Contains(t, output, "\"type\":\"custom_tool_call\"")
	require.Contains(t, output, "\"call_id\":\"call_custom_1\"")
	require.Contains(t, output, "\"name\":\"apply_patch\"")
	require.Contains(t, output, "\"input\":\"*** Begin Patch\"")
	require.NotContains(t, output, "response.function_call_arguments.delta")
}

func TestOaiChatToResponsesStreamHandlerRestoresCodexToolIdentityAcrossDeltas(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
	})

	tests := []struct {
		name            string
		tool            map[string]any
		chatName        string
		arguments       []string
		wantType        string
		wantName        string
		wantNamespace   string
		wantExecution   string
		wantInput       string
		wantNoArgDeltas bool
	}{
		{
			name: "namespace function",
			tool: map[string]any{
				"type": "namespace",
				"name": "collaboration",
				"tools": []map[string]any{{
					"type":       "function",
					"name":       "send_message",
					"parameters": map[string]any{"type": "object"},
				}},
			},
			chatName:      "collaboration__send_message",
			arguments:     []string{`{"message":`, `"done"}`},
			wantType:      "function_call",
			wantName:      "send_message",
			wantNamespace: "collaboration",
		},
		{
			name:            "tool search",
			tool:            map[string]any{"type": "tool_search"},
			chatName:        "tool_search",
			arguments:       []string{`{"query":`, `"gmail"}`},
			wantType:        "tool_search_call",
			wantExecution:   "client",
			wantNoArgDeltas: true,
		},
		{
			name: "custom tool function",
			tool: map[string]any{
				"type":   "custom",
				"name":   "apply_patch",
				"format": map[string]any{"type": "text"},
			},
			chatName:        "newapi_custom__apply_patch",
			arguments:       []string{`{"input":"*** Begin `, `Patch"}`},
			wantType:        "custom_tool_call",
			wantName:        "apply_patch",
			wantInput:       "*** Begin Patch",
			wantNoArgDeltas: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, toolState, err := service.ResponsesRequestToChatCompletionsRequestWithState(&dto.OpenAIResponsesRequest{
				Model: "gpt-5.6-sol",
				Input: mustMarshalJSON(t, []any{
					map[string]any{"type": "additional_tools", "role": "developer", "tools": []map[string]any{tt.tool}},
					map[string]any{"type": "message", "role": "user", "content": []map[string]any{{"type": "input_text", "text": "continue"}}},
				}),
			}, service.ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})
			require.NoError(t, err)

			toolIndex := 0
			firstChunk := dto.ChatCompletionsStreamResponse{
				Id:      "chatcmpl_tools",
				Object:  "chat.completion.chunk",
				Created: 1710000001,
				Model:   "gpt-5.6-sol",
				Choices: []dto.ChatCompletionsStreamResponseChoice{{
					Index: 0,
					Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{
						Index: common.GetPointer(toolIndex),
						ID:    "call_tools_1",
						Type:  "function",
						Function: dto.FunctionResponse{
							Name:      tt.chatName,
							Arguments: tt.arguments[0],
						},
					}}},
				}},
			}
			secondChunk := dto.ChatCompletionsStreamResponse{
				Id:      "chatcmpl_tools",
				Object:  "chat.completion.chunk",
				Created: 1710000001,
				Model:   "gpt-5.6-sol",
				Choices: []dto.ChatCompletionsStreamResponseChoice{{
					Index: 0,
					Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{
						Index: common.GetPointer(toolIndex),
						Function: dto.FunctionResponse{
							Arguments: tt.arguments[1],
						},
					}}},
				}},
			}
			finishReason := "tool_calls"
			finishChunk := dto.ChatCompletionsStreamResponse{
				Id:      "chatcmpl_tools",
				Object:  "chat.completion.chunk",
				Created: 1710000001,
				Model:   "gpt-5.6-sol",
				Choices: []dto.ChatCompletionsStreamResponseChoice{{Index: 0, FinishReason: &finishReason}},
			}

			body := marshalStreamChunk(t, firstChunk) + marshalStreamChunk(t, secondChunk) + marshalStreamChunk(t, finishChunk) + "data: [DONE]\n"
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			info := &relaycommon.RelayInfo{
				DisablePing: true,
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"},
			}

			_, apiErr := OaiChatToResponsesStreamHandlerWithOptionsAndState(
				c,
				info,
				&http.Response{Body: io.NopCloser(strings.NewReader(body))},
				service.ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true},
				toolState,
			)
			require.Nil(t, apiErr)

			output := recorder.Body.String()
			require.Contains(t, output, `"type":"`+tt.wantType+`"`)
			require.Contains(t, output, `"call_id":"call_tools_1"`)
			if tt.wantName != "" {
				require.Contains(t, output, `"name":"`+tt.wantName+`"`)
			}
			if tt.wantNamespace != "" {
				require.Contains(t, output, `"namespace":"`+tt.wantNamespace+`"`)
				require.NotContains(t, output, tt.chatName)
			}
			if tt.wantExecution != "" {
				require.Contains(t, output, `"execution":"`+tt.wantExecution+`"`)
			}
			if tt.wantNoArgDeltas {
				require.NotContains(t, output, "response.function_call_arguments.delta")
			}
			if tt.wantInput != "" {
				require.Contains(t, output, "event: response.custom_tool_call_input.delta")
				require.Contains(t, output, "event: response.custom_tool_call_input.done")
				require.Contains(t, output, `"input":"`+tt.wantInput+`"`)
			}
		})
	}
}

func TestOaiChatToResponsesStreamHandlerDelaysCustomToolUntilNameArrives(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
	})

	_, toolState, err := service.ResponsesRequestToChatCompletionsRequestWithState(&dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: mustMarshalJSON(t, []any{
			map[string]any{"type": "additional_tools", "role": "developer", "tools": []map[string]any{{"type": "custom", "name": "apply_patch", "format": map[string]any{"type": "text"}}}},
			map[string]any{"type": "message", "role": "user", "content": []map[string]any{{"type": "input_text", "text": "patch"}}},
		}),
	}, service.ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true})
	require.NoError(t, err)

	toolIndex := 0
	firstChunk := dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index: 0,
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{
				Index: common.GetPointer(toolIndex),
				ID:    "call_custom_late",
				Type:  "function",
				Function: dto.FunctionResponse{
					Arguments: `{"input":"pa`,
				},
			}}},
		}},
	}
	secondChunk := dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index: 0,
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{
				Index: common.GetPointer(toolIndex),
				Type:  "function",
				Function: dto.FunctionResponse{
					Name:      "newapi_custom__apply_patch",
					Arguments: `tch"}`,
				},
			}}},
		}},
	}
	finishReason := "tool_calls"
	finishChunk := dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Index: 0, FinishReason: &finishReason}}}
	body := marshalStreamChunk(t, firstChunk) + marshalStreamChunk(t, secondChunk) + marshalStreamChunk(t, finishChunk) + "data: [DONE]\n"

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"}}

	_, apiErr := OaiChatToResponsesStreamHandlerWithOptionsAndState(
		c,
		info,
		&http.Response{Body: io.NopCloser(strings.NewReader(body))},
		service.ResponsesChatCompatibilityOptions{EnableCustomToolBridge: true},
		toolState,
	)
	require.Nil(t, apiErr)

	output := recorder.Body.String()
	require.Contains(t, output, `"type":"custom_tool_call"`)
	require.Contains(t, output, `"name":"apply_patch"`)
	require.Contains(t, output, `"input":"patch"`)
	require.Contains(t, output, "event: response.custom_tool_call_input.delta")
	require.Contains(t, output, "event: response.custom_tool_call_input.done")
	require.NotContains(t, output, "response.function_call_arguments.delta")
	require.NotContains(t, output, `"type":"function_call"`)
}

func TestOaiChatToResponsesStreamHandlerUsesStableOutputIndexesWhenToolPrecedesText(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
	})

	toolIndex := 0
	toolChunk := dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{
		Index: 0,
		Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{
			Index: common.GetPointer(toolIndex),
			ID:    "call_tool_first",
			Type:  "function",
			Function: dto.FunctionResponse{
				Name:      "lookup",
				Arguments: `{"query":"status"}`,
			},
		}}},
	}}}
	textChunk := dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{
		Index: 0,
		Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: common.GetPointer("after tool")},
	}}}
	finishReason := "stop"
	finishChunk := dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Index: 0, FinishReason: &finishReason}}}
	body := marshalStreamChunk(t, toolChunk) + marshalStreamChunk(t, textChunk) + marshalStreamChunk(t, finishChunk) + "data: [DONE]\n"

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"}}
	_, apiErr := OaiChatToResponsesStreamHandler(c, info, &http.Response{Body: io.NopCloser(strings.NewReader(body))})
	require.Nil(t, apiErr)

	events := recordedResponsesSSEEvents(t, recorder.Body.String())
	require.Equal(t, 0, responseSSEOutputIndexForItem(t, events, dto.ResponsesOutputTypeItemAdded, "fc_0"))
	require.Equal(t, 0, responseSSEOutputIndexForItem(t, events, dto.ResponsesOutputTypeItemDone, "fc_0"))
	require.Equal(t, 1, responseSSEOutputIndexForItem(t, events, dto.ResponsesOutputTypeItemAdded, "msg_0"))
	require.Equal(t, 1, responseSSEOutputIndexForItem(t, events, dto.ResponsesOutputTypeItemDone, "msg_0"))

	completed := completedResponseFromSSE(t, events)
	require.Len(t, completed.Output, 2)
	require.Equal(t, "function_call", completed.Output[0].Type)
	require.Equal(t, "message", completed.Output[1].Type)
	require.Equal(t, "after tool", completed.Output[1].Content[0].Text)
}

func TestOaiChatToResponsesStreamHandlerFinalizesSparseToolIndexes(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
	})

	toolIndex := 2
	toolChunk := dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{
		Index: 0,
		Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{
			Index: common.GetPointer(toolIndex),
			ID:    "call_sparse",
			Type:  "function",
			Function: dto.FunctionResponse{
				Name:      "lookup",
				Arguments: `{"query":"sparse"}`,
			},
		}}},
	}}}
	finishReason := "tool_calls"
	finishChunk := dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Index: 0, FinishReason: &finishReason}}}
	body := marshalStreamChunk(t, toolChunk) + marshalStreamChunk(t, finishChunk) + "data: [DONE]\n"

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"}}
	_, apiErr := OaiChatToResponsesStreamHandler(c, info, &http.Response{Body: io.NopCloser(strings.NewReader(body))})
	require.Nil(t, apiErr)

	events := recordedResponsesSSEEvents(t, recorder.Body.String())
	require.Equal(t, 0, responseSSEOutputIndexForItem(t, events, dto.ResponsesOutputTypeItemAdded, "fc_2"))
	require.Equal(t, 0, responseSSEOutputIndexForItem(t, events, dto.ResponsesOutputTypeItemDone, "fc_2"))
	completed := completedResponseFromSSE(t, events)
	require.Len(t, completed.Output, 1)
	require.Equal(t, "function_call", completed.Output[0].Type)
	require.Equal(t, "call_sparse", completed.Output[0].CallId)
}
