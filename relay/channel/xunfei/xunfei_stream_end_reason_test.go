package xunfei

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// 讯飞协议以 payload.status==2 标记终止帧，但 reader goroutine 把终止帧与读错误
// 一律走同一条 stopChan，消费者在 c.Stream 里拿到 stopChan 后无从区分，于是
// StreamStatus 从不被写入：健康流 EndReason 为零值被 classifyStreamStatus 记成
// "error"，健康探针永不成功（controller/relay.go:97），上游截断反而无从记录。
// handler 需要 info 参数来记录结束原因。

type wsFrame struct {
	Text []byte
	Done bool
}

// gin 的 c.Stream 对底层 ResponseWriter 做 http.CloseNotifier 类型断言，
// httptest.ResponseRecorder 不实现该接口，直接用会 panic。包装一个即可。
type closeNotifyRecorder struct {
	*httptest.ResponseRecorder
	closeChan chan bool
}

func (r *closeNotifyRecorder) CloseNotify() <-chan bool { return r.closeChan }

func runXunfeiStreamWithFrames(t *testing.T, frames []wsFrame) *relaycommon.RelayInfo {
	t.Helper()
	gin.SetMode(gin.TestMode)

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var req map[string]any
		_ = conn.ReadJSON(&req)
		for _, frame := range frames {
			if frame.Done {
				// 模拟上游直接断开连接：没有终止帧，写方静默关闭。
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, frame.Text); err != nil {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}))
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	authURL := buildXunfeiAuthUrl(wsURL, "app-id", "api-secret")

	recorder := &closeNotifyRecorder{ResponseRecorder: httptest.NewRecorder(), closeChan: make(chan bool)}
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		IsStream:     true,
		StreamStatus: relaycommon.NewStreamStatus(),
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "spark-lite"},
	}

	request := dto.GeneralOpenAIRequest{Model: "spark-lite"}
	_, apiErr := xunfeiStreamHandler(c, info, request, "app-id", "api-secret", authURL)
	require.Nil(t, apiErr, "健康的讯飞流不得返回错误")
	return info
}

func xunfeiTextFrame(content string) wsFrame {
	return wsFrame{Text: []byte(`{"payload":{"choices":{"text":[{"content":"` + content + `"}],"status":1},"usage":{"text":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}}}`)}
}

func xunfeiTerminalFrame() wsFrame {
	return wsFrame{Text: []byte(`{"payload":{"choices":{"text":[],"status":2}}}`)}
}

// TestXunfeiStreamWithTerminalStatusEndsNormally 是回归：以 status=2 终止帧收尾的
// 流必须被判定为正常结束。
func TestXunfeiStreamWithTerminalStatusEndsNormally(t *testing.T) {
	info := runXunfeiStreamWithFrames(t, []wsFrame{xunfeiTextFrame("hi"), xunfeiTerminalFrame()})

	require.True(t, info.StreamStatus.IsNormalEnd(),
		"终止帧收尾的讯飞流必须记为正常结束，实际 EndReason=%q end_error=%v",
		info.StreamStatus.EndReason, info.StreamStatus.EndError)
	require.True(t, info.StreamStatus.IsSuccessfulEnd(),
		"正常结束的讯飞流必须视为成功，否则渠道健康探针永远无法标记成功")
}

// TestXunfeiStreamWithoutTerminalStatusStaysInterrupted 是反向对照：连接在没有
// 终止帧的情况下断开（上游截断）必须仍被判定为中断，修复不得把任何收尾都当正常结束。
func TestXunfeiStreamWithoutTerminalStatusStaysInterrupted(t *testing.T) {
	info := runXunfeiStreamWithFrames(t, []wsFrame{xunfeiTextFrame("hi"), {Done: true}})

	require.False(t, info.StreamStatus.IsNormalEnd(),
		"缺少终止帧的讯飞流必须仍记为中断，实际 EndReason=%q",
		info.StreamStatus.EndReason)
	require.Equal(t, relaycommon.StreamEndReasonUpstreamInterrupted, info.StreamStatus.EndReason)
}
