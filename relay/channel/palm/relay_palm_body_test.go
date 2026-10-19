package palm

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// trackingBody 记录 Close 是否被调用。
type trackingBody struct {
	reader io.Reader
	closed bool
}

func (b *trackingBody) Read(p []byte) (int, error) { return b.reader.Read(p) }

func (b *trackingBody) Close() error {
	b.closed = true
	return nil
}

// streamingResponseWriter 给 gin.Context.Stream 提供 CloseNotify。
// httptest.ResponseRecorder 没有这个方法，直接用它会让 Stream 在
// CloseNotify 处 panic，测不到真正的关闭逻辑。
type streamingResponseWriter struct {
	*httptest.ResponseRecorder
	closeNotify chan bool
}

func newStreamingResponseWriter() *streamingResponseWriter {
	return &streamingResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
		closeNotify:      make(chan bool),
	}
}

func (w *streamingResponseWriter) CloseNotify() <-chan bool { return w.closeNotify }

func (w *streamingResponseWriter) Flush() {}

func newPalmStreamContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(newStreamingResponseWriter())
	c.Request, _ = http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return c
}

// failingBody 在读取时报错，模拟上游在传输中断开。
type failingBody struct {
	closed bool
}

func (b *failingBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func (b *failingBody) Close() error {
	b.closed = true
	return nil
}

// 读取失败时原来的实现直接 return，只有 ReadAll 成功才关闭响应体。
// 传输中断恰恰是最需要关闭连接的场景：连接挂在半读状态，上游 token 继续被
// 消耗，进程里也没有其他路径会再关它。
func TestPalmStreamHandlerClosesBodyOnReadFailure(t *testing.T) {
	body := &failingBody{}
	resp := &http.Response{StatusCode: http.StatusOK, Body: body}

	palmStreamHandler(newPalmStreamContext(), resp)

	if !body.closed {
		t.Fatalf("palmStreamHandler left the upstream response body open after a read failure")
	}
}

// panicResponseWriter 在第一次写响应时 panic，模拟流式写盘阶段崩溃。
type panicResponseWriter struct {
	*streamingResponseWriter
}

func (w *panicResponseWriter) Write(b []byte) (int, error) {
	panic("client writer exploded")
}

func (w *panicResponseWriter) WriteString(s string) (int, error) {
	panic("client writer exploded")
}

// 读失败时 goroutine 走提前 return 分支，不关 body；正常结束时靠函数末尾那行
// 关闭。写盘 panic 会跳过末尾那行，于是 resp.Body 永久泄漏，上游连接与预读
// 缓冲都挂在读循环里。把末尾关闭改成 defer 才能覆盖所有退出路径。
func TestPalmStreamHandlerClosesBodyOnPanic(t *testing.T) {
	body := &failingBody{}
	resp := &http.Response{StatusCode: http.StatusOK, Body: body}

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(&panicResponseWriter{
		streamingResponseWriter: newStreamingResponseWriter(),
	})
	c.Request, _ = http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	func() {
		defer func() { _ = recover() }()
		palmStreamHandler(c, resp)
	}()

	if !body.closed {
		t.Fatalf("palmStreamHandler left the upstream response body open when the client write panicked")
	}
}

// 上游解析失败时必须仍然关闭响应体。
func TestPalmStreamHandlerClosesBodyOnParseFailure(t *testing.T) {
	body := &trackingBody{reader: strings.NewReader("not json at all")}
	resp := &http.Response{StatusCode: http.StatusOK, Body: body}

	palmStreamHandler(newPalmStreamContext(), resp)

	if !body.closed {
		t.Fatalf("palmStreamHandler left the upstream response body open after a parse failure")
	}
}

func TestPalmStreamHandlerClosesBodyOnSuccess(t *testing.T) {
	payload := `{"candidates":[{"content":"hello"}]}`
	body := &trackingBody{reader: strings.NewReader(payload)}
	resp := &http.Response{StatusCode: http.StatusOK, Body: body}

	_, text := palmStreamHandler(newPalmStreamContext(), resp)

	if !body.closed {
		t.Fatalf("palmStreamHandler left the upstream response body open on the success path")
	}
	if text != "hello" {
		t.Fatalf("palmStreamHandler returned text %q, want %q", text, "hello")
	}
}
