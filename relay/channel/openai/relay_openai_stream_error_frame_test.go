package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// buildErrorFrameStream returns an SSE body where the upstream answered HTTP 200
// but reported the failure inside a frame. This is how several upstreams signal
// rate limits, quota exhaustion and content-policy refusals.
func buildErrorFrameStream(frames ...string) string {
	lines := make([]string, 0, len(frames)*2+2)
	for _, frame := range frames {
		lines = append(lines, "data: "+frame, "")
	}
	lines = append(lines, "data: [DONE]", "")
	return strings.Join(lines, "\n")
}

func newErrorFrameRelayInfo() *relaycommon.RelayInfo {
	info := &relaycommon.RelayInfo{
		RelayMode:          relayconstant.RelayModeChatCompletions,
		RelayFormat:        types.RelayFormatOpenAI,
		ShouldIncludeUsage: false,
		IsStream:           true,
		DisablePing:        true,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-5.6-sol",
		},
	}
	info.SetEstimatePromptTokens(10)
	return info
}

func newErrorFrameResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// A frame-level error must fail the request. Before the fix the frame was
// forwarded as an ordinary chunk, the request settled as a success, and the
// caller was billed for a request the upstream had rejected.
func TestOaiStreamHandlerFailsOnErrorFrame(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	body := buildErrorFrameStream(
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-5.6-sol","choices":[{"index":0,"delta":{"content":"partial"}}]}`,
		`{"error":{"message":"upstream rate limited","type":"rate_limit_error","code":"rate_limit_exceeded"}}`,
	)
	info := newErrorFrameRelayInfo()

	usage, apiErr := OaiStreamHandler(c, info, newErrorFrameResponse(body))

	require.Nil(t, usage, "a failed stream must not report billable usage")
	require.NotNil(t, apiErr, "an upstream error frame must fail the request")
	require.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)
	require.Contains(t, apiErr.Error(), "upstream rate limited")
	require.False(t, info.StreamStatus.IsSuccessfulEnd(),
		"a stream that ended on an error frame is not a successful end")
	require.Greater(t, info.StreamStatus.ErrorCount, 0,
		"the error frame must be recorded on the stream status")
}

// The error frame must not reach the client as content.
func TestOaiStreamHandlerDoesNotForwardErrorFrame(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	body := buildErrorFrameStream(
		`{"error":{"message":"content policy refusal","type":"invalid_request_error","code":"content_policy_violation"}}`,
	)
	info := newErrorFrameRelayInfo()

	_, apiErr := OaiStreamHandler(c, info, newErrorFrameResponse(body))

	require.NotNil(t, apiErr)
	require.NotContains(t, recorder.Body.String(), "content policy refusal",
		"the raw upstream error frame must not be forwarded as stream content")
}

// An ordinary stream must keep settling as a success.
func TestOaiStreamHandlerKeepsSuccessForCleanStream(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	body := buildErrorFrameStream(
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-5.6-sol","choices":[{"index":0,"delta":{"content":"hello"}}]}`,
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-5.6-sol","choices":[{"index":0,"finish_reason":"stop"}]}`,
	)
	info := newErrorFrameRelayInfo()

	usage, apiErr := OaiStreamHandler(c, info, newErrorFrameResponse(body))

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.True(t, info.StreamStatus.IsSuccessfulEnd())
	require.Equal(t, 0, info.StreamStatus.ErrorCount)
}

// An empty or contentless `error` frame is not a failure. GetError is non-nil
// for any present `error` key, so without the content check a benign
// placeholder frame aborted a stream that was still healthy and settled it as
// a 502 with an empty message.
func TestOaiStreamHandlerIgnoresEmptyErrorFrame(t *testing.T) {
	for _, emptyFrame := range []string{
		`{"error":{}}`,
		`{"error":""}`,
		`{"error":{"message":""}}`,
	} {
		t.Run(emptyFrame, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

			body := buildErrorFrameStream(
				`{"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-5.6-sol","choices":[{"index":0,"delta":{"content":"hello"}}]}`,
				emptyFrame,
				`{"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-5.6-sol","choices":[{"index":0,"finish_reason":"stop"}]}`,
			)
			info := newErrorFrameRelayInfo()

			usage, apiErr := OaiStreamHandler(c, info, newErrorFrameResponse(body))

			require.Nil(t, apiErr, "an empty error frame must not fail a healthy stream")
			require.NotNil(t, usage)
			require.True(t, info.StreamStatus.IsSuccessfulEnd())
		})
	}
}

// An error frame that carries only a code (no message) is still a failure, so
// the guard must not be narrowed to a message-only check.
func TestOaiStreamHandlerFailsOnCodeOnlyErrorFrame(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	body := buildErrorFrameStream(
		`{"error":{"code":"insufficient_quota"}}`,
	)
	info := newErrorFrameRelayInfo()

	usage, apiErr := OaiStreamHandler(c, info, newErrorFrameResponse(body))

	require.Nil(t, usage)
	require.NotNil(t, apiErr, "an error frame carrying a code must fail the request")
	require.False(t, info.StreamStatus.IsSuccessfulEnd())
}

// 真实上游的 error 帧是流的最后一帧，后面不会补 [DONE]。此时扫描器先以
// HandlerStop 结束，状态码映射不能因此被 502 bad_response_body 分支遮蔽：
// 限流要按 429 返回，客户端才能正确退避，渠道限流观测也才看得到。
func TestOaiStreamHandlerMapsTerminalErrorFrameWithoutDone(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	body := strings.Join([]string{
		"data: " + `{"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-5.6-sol","choices":[{"index":0,"delta":{"content":"partial"}}]}`,
		"",
		"data: " + `{"error":{"message":"upstream rate limited","type":"rate_limit_error","code":"rate_limit_exceeded"}}`,
		"",
	}, "\n")
	info := newErrorFrameRelayInfo()

	usage, apiErr := OaiStreamHandler(c, info, newErrorFrameResponse(body))

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode,
		"a terminal rate-limit error frame must map to 429, not 502")
}
