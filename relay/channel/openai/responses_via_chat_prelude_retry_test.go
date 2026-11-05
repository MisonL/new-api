package openai

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// responses_via_chat 从不初始化 prelude 标记：只发出前导帧（response.created /
// response.in_progress / 角色帧）后上游断流时，ReceivedResponseCount 已经大于 0，
// 而标记始终为 false，中断被判成不可重试。实际只发过生命周期帧，换渠道重试
// 不会重复任何内容，与 openai/claude 渠道的口径必须一致。

type responsesViaChatInterruptingBody struct {
	payload []byte
	sent    bool
}

func (r *responsesViaChatInterruptingBody) Read(p []byte) (int, error) {
	if !r.sent {
		r.sent = true
		return copy(p, r.payload), nil
	}
	return 0, errors.New("connection reset by peer")
}

func newResponsesViaChatTestContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		IsStream:     true,
		DisablePing:  true,
		StreamStatus: relaycommon.NewStreamStatus(),
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5"},
	}
	return c, recorder, info
}

func TestResponsesViaChatPreludeInterruptStaysRetryable(t *testing.T) {
	c, recorder, info := newResponsesViaChatTestContext(t)

	// 只有角色帧：不携带任何内容，等价于 claude 的 message_start 前导帧。
	roleChunk := dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl-prelude",
		Model: "gpt-5",
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Role: "assistant"}},
		},
	}
	body := marshalStreamChunk(t, roleChunk)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(&responsesViaChatInterruptingBody{payload: []byte(body)}),
	}

	_, apiErr := OaiChatToResponsesStreamHandlerWithOptions(c, info, resp, service.ResponsesChatCompatibilityOptions{})

	require.NotNil(t, apiErr, "前导帧后断流必须以错误结束")
	require.Equal(t, types.ErrorCodeUpstreamTransportInterrupted, apiErr.GetErrorCode(),
		"只发过前导帧就被中断，必须允许换渠道重试：%s", apiErr.Error())
	require.False(t, types.IsSkipRetryError(apiErr),
		"前导帧场景必须保持可重试，不能被标成 skip-retry：%s", apiErr.Error())
	require.NotContains(t, recorder.Body.String(), "output_text.delta",
		"前导帧场景不应已经下发内容")
}

func TestResponsesViaChatInterruptAfterContentIsNotRetryable(t *testing.T) {
	c, recorder, info := newResponsesViaChatTestContext(t)

	contentText := "hello"
	contentChunk := dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl-content",
		Model: "gpt-5",
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: &contentText}},
		},
	}
	body := marshalStreamChunk(t, contentChunk)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(&responsesViaChatInterruptingBody{payload: []byte(body)}),
	}

	_, apiErr := OaiChatToResponsesStreamHandlerWithOptions(c, info, resp, service.ResponsesChatCompatibilityOptions{})

	require.NotNil(t, apiErr, "内容帧后断流必须以错误结束")
	require.Equal(t, types.ErrorCodeUpstreamTransportInterrupted, apiErr.GetErrorCode())
	require.True(t, types.IsSkipRetryError(apiErr),
		"内容已经下发后不能再换渠道重试，否则会重复输出：%s", apiErr.Error())
	require.Contains(t, recorder.Body.String(), "hello", "已经下发的内容必须保留")
}
