package helper

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// dataHandler panic 时 writeMutex 没有被释放：后续 ping 写协程永久卡在
// Lock 上，而它计入了 wg，StreamScannerHandler 的 wg.Wait() 挂死，
// 请求 goroutine 与其占用的连接、计费会话一起泄漏。
func TestStreamScannerDataHandlerPanicDoesNotDeadlock(t *testing.T) {
	generalSetting := operation_setting.GetGeneralSetting()
	oldEnabled := generalSetting.PingIntervalEnabled
	oldSeconds := generalSetting.PingIntervalSeconds
	generalSetting.PingIntervalEnabled = true
	generalSetting.PingIntervalSeconds = 1
	t.Cleanup(func() {
		generalSetting.PingIntervalEnabled = oldEnabled
		generalSetting.PingIntervalSeconds = oldSeconds
	})

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	reader, writer := io.Pipe()
	go func() {
		defer writer.Close()
		_, _ = writer.Write([]byte("data: {\"x\":1}\n\n"))
		time.Sleep(5 * time.Second)
	}()

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       reader,
	}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	done := make(chan struct{})
	go func() {
		StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
			// 持锁期间让 ping ticker 先触发：写协程会阻塞在 writeMutex 上，
			// 随后处理器 panic，若锁不随 panic 释放，写协程将永久卡住。
			time.Sleep(1500 * time.Millisecond)
			panic("data handler exploded")
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("dataHandler panic 后 StreamScannerHandler 没有退出：写锁未释放导致 ping 写协程卡死")
	}
	if !strings.Contains(recorder.Body.String(), "PING") && recorder.Body.Len() == 0 {
		t.Log("no response body writes observed; only completion matters here")
	}
}
