package cohere

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

// streamingWriter 提供 gin.Context.Stream 需要的 CloseNotify。
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

func newCohereStreamContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(newStreamingWriter())
	c.Request, _ = http.NewRequest(http.MethodPost, "/v1/chat", nil)
	return c
}

// 上游在读的过程中就断开，扫描器读到 EOF。这个处理器原先没有任何关闭点，
// 响应体会一直挂到 GC，连接也无法归还连接池。
func TestCohereStreamHandlerClosesBodyOnUpstreamEOF(t *testing.T) {
	body := &bodyCloseProbe{reader: io.LimitReader(nil, 0)}
	resp := &http.Response{StatusCode: http.StatusOK, Body: body}
	info := &relaycommon.RelayInfo{
		StreamStatus: relaycommon.NewStreamStatus(),
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "command-r"},
	}

	cohereStreamHandler(newCohereStreamContext(), info, resp)

	if body.closed != 1 {
		t.Fatalf("cohereStreamHandler closed the upstream body %d times, want 1", body.closed)
	}
}
