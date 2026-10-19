package zhipu

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

type bodyCloseProbe struct {
	reader io.Reader
	closed int
}

func (b *bodyCloseProbe) Read(p []byte) (int, error) { return b.reader.Read(p) }

func (b *bodyCloseProbe) Close() error {
	b.closed++
	return nil
}

// streamingWriter 给 gin.Context.Stream 提供 CloseNotify。
type streamingWriter struct {
	*httptest.ResponseRecorder
	closeNotify chan bool
}

func newStreamingWriter() *streamingWriter {
	return &streamingWriter{
		ResponseRecorder: httptest.NewRecorder(),
		closeNotify:      make(chan bool),
	}
}

func (w *streamingWriter) CloseNotify() <-chan bool { return w.closeNotify }
func (w *streamingWriter) Flush()                   {}

func newZhipuStreamContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(newStreamingWriter())
	c.Request, _ = http.NewRequest(http.MethodPost, "/v1/chat", nil)
	return c
}

// 客户端在流式过程中断开时 c.Stream 会提前返回 true，函数末尾的关闭语句在
// 原实现里位于 c.Stream 之后，看似会执行；但读取协程仍阻塞在无缓冲 channel 的
// 发送上，响应体不关闭就永远读不到 EOF。这里验证所有退出路径都关闭一次。
func TestZhipuStreamHandlerClosesBodyOnClientGone(t *testing.T) {
	body := &bodyCloseProbe{reader: errReader{}}
	resp := &http.Response{StatusCode: http.StatusOK, Body: body}
	info := &relaycommon.RelayInfo{
		StreamStatus: relaycommon.NewStreamStatus(),
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "glm-4"},
	}

	zhipuStreamHandler(newZhipuStreamContext(), info, resp)

	if body.closed != 1 {
		t.Fatalf("zhipuStreamHandler closed the upstream body %d times, want 1", body.closed)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
