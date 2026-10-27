package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// OpenAI 格式上游的空回复：200 之后只发出 role 帧或空 delta，随后断流。
// 这类中断在客户端还没拿到任何内容时应当换渠道重试；内容一旦下发就不能重放。

func newOpenAIStreamContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return c
}

func openAIStreamResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func newOpenAIStreamInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		IsStream:    true,
		RelayFormat: types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
	}
}

func TestOpenAIStreamInterruptedBeforeContentIsRetryable(t *testing.T) {
	c := newOpenAIStreamContext(t)
	info := newOpenAIStreamInfo()

	body := "data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n"

	usage, apiErr := OaiStreamHandler(c, info, openAIStreamResponse(body))

	require.Nil(t, usage)
	require.NotNil(t, apiErr, "只收到 role 帧就断流时必须报错，而不是按成功返回空回复")
	require.Equal(t, types.ErrorCodeUpstreamTransportInterrupted, apiErr.GetErrorCode())
}

func TestOpenAIStreamInterruptedAfterContentIsNotRetryable(t *testing.T) {
	c := newOpenAIStreamContext(t)
	info := newOpenAIStreamInfo()

	body := "data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n"

	_, apiErr := OaiStreamHandler(c, info, openAIStreamResponse(body))

	require.Nil(t, apiErr, "内容已下发后断流保持原行为，不能重放")
}

func TestOpenAIStreamHealthyContentIsNotTreatedAsEmpty(t *testing.T) {
	c := newOpenAIStreamContext(t)
	info := newOpenAIStreamInfo()

	body := "data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"

	usage, apiErr := OaiStreamHandler(c, info, openAIStreamResponse(body))

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamPreludeRetrySafe),
		"内容帧到达后必须清掉 prelude 重试标记")
}
