package coze

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

// cozeChatStreamHandler 自建扫描器，从不写 info.StreamStatus。Coze 的终止信号
// 是 conversation.chat.completed 事件；健康流结束后 EndReason 仍是 None，
// classifyStreamStatus 会把每条健康流记成 "error"，渠道健康探针也永远无法标记
// 成功（controller/relay.go:97 依赖 IsSuccessfulEnd）。

func runCozeStream(t *testing.T, body string) *relaycommon.RelayInfo {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
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
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "coze-model"},
	}

	_, apiErr := cozeChatStreamHandler(c, info, resp)
	require.Nil(t, apiErr, "健康的 Coze 流不得返回错误")
	return info
}

func cozeDeltaEvent() string {
	return "event: conversation.message.delta\n" +
		"data: {\"content\":\"hi\"}\n\n"
}

// TestCozeStreamWithCompletedEndsNormally 是回归：以 conversation.chat.completed
// 收尾的流必须被判定为正常结束。
func TestCozeStreamWithCompletedEndsNormally(t *testing.T) {
	body := cozeDeltaEvent() +
		"event: conversation.chat.completed\n" +
		"data: {\"usage\":{\"input_count\":3,\"output_count\":2,\"token_count\":5}}\n\n"
	info := runCozeStream(t, body)

	require.True(t, info.StreamStatus.IsNormalEnd(),
		"completed 事件收尾的 Coze 流必须记为正常结束，实际 EndReason=%q",
		info.StreamStatus.EndReason)
	require.True(t, info.StreamStatus.IsSuccessfulEnd(),
		"正常结束的 Coze 流必须视为成功，否则渠道健康探针永远无法标记成功")
}

// TestCozeStreamWithoutCompletedStaysInterrupted 是反向对照：干净结束但缺少
// completed 事件的流（上游截断）必须仍被判定为中断。
func TestCozeStreamWithoutCompletedStaysInterrupted(t *testing.T) {
	info := runCozeStream(t, cozeDeltaEvent())

	require.False(t, info.StreamStatus.IsNormalEnd(),
		"缺少 completed 事件的 Coze 流必须仍记为中断，实际 EndReason=%q",
		info.StreamStatus.EndReason)
	require.Equal(t, relaycommon.StreamEndReasonUpstreamInterrupted, info.StreamStatus.EndReason)
}
