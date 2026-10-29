package zhipu

import (
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// waitForGoroutineExit 轮询全量 goroutine 栈，直到不再出现包含 marker 的栈帧。
func waitForGoroutineExit(t *testing.T, marker string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		if !strings.Contains(string(buf[:n]), marker) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("producer goroutine still running after client disconnect: %s", marker)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// 客户端断开时 c.Stream 直接返回，不再消费 dataChan/metaChan，生产者协程
// 会永久卡在无缓冲发送上：每个被中断的流泄漏一个协程和一条上游连接。
func TestZhipuStreamProducerExitsWhenClientDisconnects(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	resp := &http.Response{Body: pr, Header: http.Header{}}

	gone := make(chan bool)
	close(gone)
	recorder := &closeNotifyRecorder{ResponseRecorder: httptest.NewRecorder(), closeChan: gone}
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		IsStream:     true,
		StreamStatus: relaycommon.NewStreamStatus(),
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "glm-4"},
	}
	_, apiErr := zhipuStreamHandler(c, info, resp)
	require.Nil(t, apiErr)

	waitForGoroutineExit(t, "zhipuStreamHandler.func")
}
