package palm

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// palmStreamHandler 把单条 JSON 包装成一帧 SSE。此前读取失败或解析失败只记
// 一行 SysLog 然后照常发送 [DONE]：客户端收到 200 空成功流，渠道健康与错误
// 分类都失真。错误必须转成 API 错误上抛，且不得发送 [DONE]。

type palmFailingBody struct {
	payload []byte
	sent    bool
	err     error
}

func (r *palmFailingBody) Read(p []byte) (int, error) {
	if !r.sent {
		r.sent = true
		return copy(p, r.payload), nil
	}
	return 0, r.err
}

// gin 的 c.Stream 对 ResponseWriter 做 http.CloseNotifier 断言，
// httptest.ResponseRecorder 不实现它；用本包已有的包装类型。
func newPalmStreamTestContext(t *testing.T) (*gin.Context, *streamingResponseWriter) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	writer := newStreamingResponseWriter()
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return c, writer
}

func TestPalmStreamReadErrorIsNotReportedAsSuccess(t *testing.T) {
	c, recorder := newPalmStreamTestContext(t)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(&palmFailingBody{payload: []byte(`{"candidates":[]`), err: errors.New("upstream reset")}),
	}

	apiErr, _ := palmStreamHandler(c, resp)

	require.NotNil(t, apiErr, "读取失败必须转成 API 错误")
	require.NotContains(t, recorder.Body.String(), "[DONE]",
		"失败流不得用 [DONE] 告诉客户端已完成：%s", recorder.Body.String())
}

func TestPalmStreamParseErrorIsNotReportedAsSuccess(t *testing.T) {
	c, recorder := newPalmStreamTestContext(t)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{not json`)),
	}

	apiErr, _ := palmStreamHandler(c, resp)

	require.NotNil(t, apiErr, "解析失败必须转成 API 错误")
	require.NotContains(t, recorder.Body.String(), "[DONE]",
		"失败流不得用 [DONE] 告诉客户端已完成：%s", recorder.Body.String())
}

// 反向对照：健康响应仍必须正常收尾。
func TestPalmStreamHealthyStillCompletes(t *testing.T) {
	c, recorder := newPalmStreamTestContext(t)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"candidates":[{"content":"hi"}]}`)),
	}

	apiErr, text := palmStreamHandler(c, resp)

	require.Nil(t, apiErr, "健康响应不得返回错误")
	require.Equal(t, "hi", text)
	require.Contains(t, recorder.Body.String(), `"content":"hi"`)
	require.Contains(t, recorder.Body.String(), "[DONE]")
}
