package helper

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 上游连接中断时，bufio.Scanner 会把没有行结束符的最后一行也当作一个完整
// token 返回。SSE 帧只有以换行结尾才是完整的；把截断的 JSON 下发会让客户端
// 收到解析不了的半帧，且计数/计费把它当成一帧有效响应。
func TestStreamScannerHandler_DropsTruncatedFinalFrame(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 2
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	// 第一帧完整，第二帧在 JSON 中途断开且没有换行。
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"trunc"
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(body))}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	var mu sync.Mutex
	var frames []string
	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		mu.Lock()
		frames = append(frames, data)
		mu.Unlock()
	})

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{`{"choices":[{"delta":{"content":"ok"}}]}`}, frames,
		"truncated final frame without a line terminator must not be delivered")
}

// 正常终止帧即使没有末尾换行也必须保留：[DONE] 是完整语义标记，不依赖
// 后续内容。
func TestStreamScannerHandler_KeepsDoneWithoutTrailingNewline(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 2
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{Body: io.NopCloser(strings.NewReader("data: [DONE]"))}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	var mu sync.Mutex
	var frames []string
	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		mu.Lock()
		frames = append(frames, data)
		mu.Unlock()
	})

	mu.Lock()
	delivered := append([]string(nil), frames...)
	mu.Unlock()
	require.Empty(t, delivered)
	require.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
}
