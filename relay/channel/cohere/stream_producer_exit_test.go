package cohere

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

// 客户端断开时 c.Stream 直接返回，生产者协程会卡在向无缓冲 channel 发送。
// 没有任何路径能让它退出：每个被中断的流都泄漏一个协程和一条上游连接。
func TestCohereStreamProducerExitsWhenClientDisconnects(t *testing.T) {
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
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "command-r"},
	}
	usage, apiErr := cohereStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)

	waitForGoroutineExit(t, "cohereStreamHandler.func")
}
