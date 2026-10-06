package zhipu

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// zhipuStreamHandler 自建扫描器，从不写 info.StreamStatus。智谱流式的终止信号是
// meta 事件（携带 usage，转换时设置 FinishReason=stop）；健康流结束后 EndReason
// 仍是 None，classifyStreamStatus 会把每条健康流记成 "error"，渠道健康探针也
// 永远无法标记成功（controller/relay.go:97 依赖 IsSuccessfulEnd）。

// gin 的 c.Stream 对底层 ResponseWriter 做 http.CloseNotifier 类型断言，
// httptest.ResponseRecorder 不实现该接口，直接用会 panic。包装一个即可。
type closeNotifyRecorder struct {
	*httptest.ResponseRecorder
	closeChan chan bool
}

func (r *closeNotifyRecorder) CloseNotify() <-chan bool { return r.closeChan }

func runZhipuStream(t *testing.T, body string) *relaycommon.RelayInfo {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := &closeNotifyRecorder{ResponseRecorder: httptest.NewRecorder(), closeChan: make(chan bool)}
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	// 生产环境在 controller/relay.go:68 初始化 StreamStatus，夹具必须一致，
	// 否则断言处解引用 nil。
	info := &relaycommon.RelayInfo{
		IsStream:     true,
		StreamStatus: relaycommon.NewStreamStatus(),
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "glm-4"},
	}

	_, apiErr := zhipuStreamHandler(c, info, resp)
	require.Nil(t, apiErr, "健康的智谱流不得返回错误")
	return info
}

// TestZhipuStreamWithMetaEndsNormally 是回归：以 meta 事件（携带 usage）收尾的
// 流必须被判定为正常结束。
func TestZhipuStreamWithMetaEndsNormally(t *testing.T) {
	body := "data: hi\n" +
		"meta: {\"request_id\":\"r1\",\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n"
	info := runZhipuStream(t, body)

	require.True(t, info.StreamStatus.IsNormalEnd(),
		"meta 事件收尾的智谱流必须记为正常结束，实际 EndReason=%q",
		info.StreamStatus.EndReason)
	require.True(t, info.StreamStatus.IsSuccessfulEnd(),
		"正常结束的智谱流必须视为成功，否则渠道健康探针永远无法标记成功")
}

// TestZhipuStreamWithoutMetaStaysInterrupted 是反向对照：干净结束但缺少 meta
// 事件的流（上游截断）必须仍被判定为中断。
func TestZhipuStreamWithoutMetaStaysInterrupted(t *testing.T) {
	info := runZhipuStream(t, "data: hi\n")

	require.False(t, info.StreamStatus.IsNormalEnd(),
		"缺少 meta 事件的智谱流必须仍记为中断，实际 EndReason=%q",
		info.StreamStatus.EndReason)
	require.Equal(t, relaycommon.StreamEndReasonUpstreamInterrupted, info.StreamStatus.EndReason)
}
