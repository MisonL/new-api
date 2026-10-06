package cohere

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

// cohereStreamHandler 自建扫描器，从不写 info.StreamStatus。Cohere 流式的终止
// 信号是 is_finished=true 的收尾块；健康流结束后 EndReason 仍是 None，
// classifyStreamStatus 会把每条健康流记成 "error"，渠道健康探针也永远无法标记
// 成功（controller/relay.go:97 依赖 IsSuccessfulEnd）。

// gin 的 c.Stream 对底层 ResponseWriter 做 http.CloseNotifier 类型断言，
// httptest.ResponseRecorder 不实现该接口，直接用会 panic。包装一个即可。
type closeNotifyRecorder struct {
	*httptest.ResponseRecorder
	closeChan chan bool
}

func (r *closeNotifyRecorder) CloseNotify() <-chan bool { return r.closeChan }

func runCohereStream(t *testing.T, body string) *relaycommon.RelayInfo {
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
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "command-r"},
	}

	_, apiErr := cohereStreamHandler(c, info, resp)
	require.Nil(t, apiErr, "健康的 Cohere 流不得返回错误")
	return info
}

// TestCohereStreamWithFinishedEndsNormally 是回归：以 is_finished=true 收尾的流
// 必须被判定为正常结束。
func TestCohereStreamWithFinishedEndsNormally(t *testing.T) {
	body := "{\"text\":\"hi\",\"is_finished\":false}\n" +
		"{\"is_finished\":true,\"finish_reason\":\"COMPLETE\",\"response\":{\"meta\":{\"billed_units\":{\"input_tokens\":3,\"output_tokens\":2}}}}\n"
	info := runCohereStream(t, body)

	require.True(t, info.StreamStatus.IsNormalEnd(),
		"is_finished 收尾的 Cohere 流必须记为正常结束，实际 EndReason=%q",
		info.StreamStatus.EndReason)
	require.True(t, info.StreamStatus.IsSuccessfulEnd(),
		"正常结束的 Cohere 流必须视为成功，否则渠道健康探针永远无法标记成功")
}

// TestCohereStreamWithoutFinishedStaysInterrupted 是反向对照：干净结束但缺少
// is_finished 收尾块的流（上游截断）必须仍被判定为中断。
func TestCohereStreamWithoutFinishedStaysInterrupted(t *testing.T) {
	info := runCohereStream(t, "{\"text\":\"hi\",\"is_finished\":false}\n")

	require.False(t, info.StreamStatus.IsNormalEnd(),
		"缺少 is_finished 的 Cohere 流必须仍记为中断，实际 EndReason=%q",
		info.StreamStatus.EndReason)
	require.Equal(t, relaycommon.StreamEndReasonUpstreamInterrupted, info.StreamStatus.EndReason)
}
