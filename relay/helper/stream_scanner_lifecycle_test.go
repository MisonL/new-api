package helper

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

// dataHandler 卡在写响应上时，StreamScannerHandler 不能在它写完之前返回：
// gin 会把 Context 放回池里复用，遗留协程之后的写会落到另一个请求的响应上。
func TestStreamScannerWaitsForHandlerBeforeReturning(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	var calls atomic.Int64
	body := strings.Repeat("data: {\"x\":1}\n\n", 3)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{DisablePing: true}

	start := time.Now()
	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		if calls.Add(1) == 1 {
			time.Sleep(7 * time.Second)
		}
	})
	elapsed := time.Since(start)
	if calls.Load() != 3 {
		t.Fatalf("StreamScannerHandler returned with %d of 3 handler calls done; it must wait for the data handler", calls.Load())
	}
	if elapsed < 6*time.Second {
		t.Fatalf("StreamScannerHandler returned after %v while the data handler was still blocked", elapsed.Round(10*time.Millisecond))
	}
}

// blockingWriteDeadlineWriter 的 Write 会一直阻塞，直到 SetWriteDeadline 被调用；
// 用来验证扫描器在返回前给底层连接设了写截止时间，卡死的客户端写入能被打断。
type blockingWriteDeadlineWriter struct {
	header   http.Header
	blocked  chan struct{}
	unblock  chan struct{}
	deadline atomic.Bool
	status   int
}

func newBlockingWriteDeadlineWriter() *blockingWriteDeadlineWriter {
	return &blockingWriteDeadlineWriter{
		header:  http.Header{},
		blocked: make(chan struct{}, 1),
		unblock: make(chan struct{}),
		status:  http.StatusOK,
	}
}

func (w *blockingWriteDeadlineWriter) Header() http.Header { return w.header }

func (w *blockingWriteDeadlineWriter) WriteHeader(status int) { w.status = status }

func (w *blockingWriteDeadlineWriter) Write(data []byte) (int, error) {
	select {
	case w.blocked <- struct{}{}:
	default:
	}
	<-w.unblock
	return 0, http.ErrHandlerTimeout
}

func (w *blockingWriteDeadlineWriter) Flush() {}

func (w *blockingWriteDeadlineWriter) SetWriteDeadline(time.Time) error {
	if w.deadline.CompareAndSwap(false, true) {
		close(w.unblock)
	}
	return nil
}

func TestStreamScannerBoundsBlockedClientWritesOnShutdown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	writer := newBlockingWriteDeadlineWriter()
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	body := strings.Repeat("data: {\"x\":1}\n\n", 2)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{DisablePing: true}

	done := make(chan struct{})
	go func() {
		defer close(done)
		StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
			_ = StringData(c, data)
		})
	}()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("StreamScannerHandler did not return; blocked client writes must be bounded by a write deadline")
	}
	if !writer.deadline.Load() {
		t.Fatal("StreamScannerHandler must set a write deadline before waiting for blocked writers")
	}
}
