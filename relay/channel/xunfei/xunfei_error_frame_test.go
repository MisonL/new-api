package xunfei

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// 讯飞把鉴权失败、限流、余额不足等错误放在 header.code（非 0）与 header.message
// 里，随后关闭连接。全包从未读取 header.code：错误帧被当成一个空内容的数据帧
// 下发，非流式路径返回 200 空回复、流式路径返回 [DONE]，调用方看到"成功但空"，
// 计费按 0 token 结算，渠道健康与错误分类全部失真。

// xunfeiErrorFrame 是讯飞典型错误帧：只有 header，没有 payload。
func xunfeiErrorFrame(code int, message string) wsFrame {
	return wsFrame{Text: []byte(`{"header":{"code":` + strconv.Itoa(code) + `,"message":"` + message + `","sid":"test-sid","status":2}}`)}
}

func newXunfeiLocalServer(t *testing.T, frames []wsFrame) (authURL string) {
	t.Helper()
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
	return buildXunfeiAuthUrl(wsURL, "app-id", "api-secret")
}

func newXunfeiStreamContext(t *testing.T) (*gin.Context, *closeNotifyRecorder, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := &closeNotifyRecorder{ResponseRecorder: httptest.NewRecorder(), closeChan: make(chan bool)}
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		IsStream:     true,
		StreamStatus: relaycommon.NewStreamStatus(),
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "spark-lite"},
	}
	return c, recorder, info
}

// 流式：首帧即错误帧时不得下发 [DONE]，必须以 API 错误结束，让上层按失败重试。
func TestXunfeiStreamErrorFrameIsNotReportedAsSuccess(t *testing.T) {
	authURL := newXunfeiLocalServer(t, []wsFrame{xunfeiErrorFrame(10004, "auth failed")})
	c, recorder, info := newXunfeiStreamContext(t)

	_, apiErr := xunfeiStreamHandler(c, info, dto.GeneralOpenAIRequest{Model: "spark-lite"}, "app-id", "api-secret", authURL)

	require.NotNil(t, apiErr, "讯飞错误帧必须转成 API 错误，不能返回空成功流")
	body := recorder.Body.String()
	require.NotContains(t, body, "[DONE]", "错误流不得用 [DONE] 告诉客户端流已正常完成")
	require.False(t, info.StreamStatus.IsSuccessfulEnd(),
		"错误帧收尾的流不得记为成功结束，实际 EndReason=%q", info.StreamStatus.EndReason)
}

// 流式：已经下发过内容后收到错误帧，不得再用 [DONE] 收尾。
func TestXunfeiStreamErrorFrameAfterContentWritesErrorEvent(t *testing.T) {
	authURL := newXunfeiLocalServer(t, []wsFrame{
		xunfeiTextFrame("partial"),
		xunfeiErrorFrame(10013, "too many requests"),
	})
	c, recorder, info := newXunfeiStreamContext(t)

	_, apiErr := xunfeiStreamHandler(c, info, dto.GeneralOpenAIRequest{Model: "spark-lite"}, "app-id", "api-secret", authURL)

	body := recorder.Body.String()
	require.Contains(t, body, "partial", "已产出的内容不应被丢弃")
	if apiErr == nil {
		require.NotContains(t, body, "[DONE]", "带错误帧的流不得以 [DONE] 收尾")
		require.Contains(t, body, "too many requests", "错误必须以 SSE 错误帧下发给客户端")
	}
	require.False(t, info.StreamStatus.IsSuccessfulEnd(),
		"错误帧收尾的流不得记为成功结束，实际 EndReason=%q", info.StreamStatus.EndReason)
}

// 非流式：只有错误帧时必须返回错误，而不是 200 空回复。
func TestXunfeiNonStreamErrorFrameReturnsError(t *testing.T) {
	authURL := newXunfeiLocalServer(t, []wsFrame{xunfeiErrorFrame(11200, "quota exceeded")})
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	_, apiErr := xunfeiHandler(c, dto.GeneralOpenAIRequest{Model: "spark-lite"}, "app-id", "generalv3.5", authURL)

	require.NotNil(t, apiErr, "非流式路径同样必须把讯飞错误帧转成 API 错误")
	if apiErr != nil {
		require.Contains(t, strings.ToLower(apiErr.Error()), "quota exceeded",
			"上游错误信息必须透传，实际：%s", apiErr.Error())
	}
	require.NotContains(t, recorder.Body.String(), `"choices"`,
		"错误响应不得伪装成正常的 chat.completion 体：%s", recorder.Body.String())
}

// 反向对照：健康流（有内容 + status=2 终止帧）不得被新检查误判为错误。
func TestXunfeiHealthyStreamStillSucceedsAfterErrorCheck(t *testing.T) {
	authURL := newXunfeiLocalServer(t, []wsFrame{xunfeiTextFrame("hi"), xunfeiTerminalFrame()})
	c, recorder, info := newXunfeiStreamContext(t)

	usage, apiErr := xunfeiStreamHandler(c, info, dto.GeneralOpenAIRequest{Model: "spark-lite"}, "app-id", "generalv3.5", authURL)

	require.Nil(t, apiErr, "健康流不得因 header.code 检查而失败：%v", apiErr)
	require.NotNil(t, usage)
	require.Contains(t, recorder.Body.String(), "[DONE]")
	require.True(t, info.StreamStatus.IsSuccessfulEnd())
}

// 健康流的 header.code 为 0 或缺省，两种形态都必须放行。
func TestXunfeiZeroHeaderCodeIsAccepted(t *testing.T) {
	authURL := newXunfeiLocalServer(t, []wsFrame{
		{Text: []byte(`{"header":{"code":0,"message":"success","sid":"s","status":2},"payload":{"choices":{"text":[{"content":"ok"}],"status":1},"usage":{"text":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}}}`)},
		xunfeiTerminalFrame(),
	})
	c, _, info := newXunfeiStreamContext(t)

	_, apiErr := xunfeiStreamHandler(c, info, dto.GeneralOpenAIRequest{Model: "spark-lite"}, "app-id", "generalv3.5", authURL)
	require.Nil(t, apiErr, "code=0 的帧是正常帧：%v", apiErr)
	require.True(t, info.StreamStatus.IsSuccessfulEnd())
}
