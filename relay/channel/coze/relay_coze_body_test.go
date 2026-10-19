package coze

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func newCozeStreamContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request, _ = http.NewRequest(http.MethodPost, "/v1/chat", nil)
	return c
}

// 扫描出错时函数在 scanner.Err() 分支提前返回，末尾（若有）不会执行。
// 响应体必须在这里也被关闭。
func TestCozeChatStreamHandlerClosesBodyOnScanError(t *testing.T) {
	body := &bodyCloseProbe{reader: errReader{}}
	resp := &http.Response{StatusCode: http.StatusOK, Body: body}

	cozeChatStreamHandler(newCozeStreamContext(), nil, resp)

	if body.closed != 1 {
		t.Fatalf("cozeChatStreamHandler closed the upstream body %d times, want 1", body.closed)
	}
}

// 上游正常结束后同样要关闭。
func TestCozeChatStreamHandlerClosesBodyOnNormalEnd(t *testing.T) {
	body := &bodyCloseProbe{reader: strings.NewReader("event: conversation.chat.completed\ndata: {\"usage\":{\"token_count\":3}}\n\n")}
	resp := &http.Response{StatusCode: http.StatusOK, Body: body}
	info := &relaycommon.RelayInfo{
		StreamStatus: relaycommon.NewStreamStatus(),
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "coze"},
	}

	cozeChatStreamHandler(newCozeStreamContext(), info, resp)

	if body.closed != 1 {
		t.Fatalf("cozeChatStreamHandler closed the upstream body %d times, want 1", body.closed)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
