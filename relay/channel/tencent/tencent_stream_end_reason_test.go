package tencent

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

// tencentStreamHandler 自建扫描器，从不写 info.StreamStatus。混元流式的终止信号
// 是尾包 Choices[0].FinishReason == "stop"（见 dto.go 注释）；健康流结束后
// EndReason 仍是 None，classifyStreamStatus 会把每条健康流记成 "error"，
// 渠道健康探针也永远无法标记成功（controller/relay.go:97 依赖 IsSuccessfulEnd）。

func runTencentStream(t *testing.T, body string) *relaycommon.RelayInfo {
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
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "hunyuan-lite"},
	}

	_, apiErr := tencentStreamHandler(c, info, resp)
	require.Nil(t, apiErr, "健康的混元流不得返回错误")
	return info
}

// TestTencentStreamWithFinishReasonEndsNormally 是回归：以尾包
// FinishReason="stop" 收尾的流必须被判定为正常结束。
func TestTencentStreamWithFinishReasonEndsNormally(t *testing.T) {
	body := "data: {\"Choices\":[{\"Delta\":{\"Content\":\"hi\"}}]}\n\n" +
		"data: {\"Choices\":[{\"FinishReason\":\"stop\"}]}\n\n"
	info := runTencentStream(t, body)

	require.True(t, info.StreamStatus.IsNormalEnd(),
		"尾包收尾的混元流必须记为正常结束，实际 EndReason=%q",
		info.StreamStatus.EndReason)
	require.True(t, info.StreamStatus.IsSuccessfulEnd(),
		"正常结束的混元流必须视为成功，否则渠道健康探针永远无法标记成功")
}

// TestTencentStreamWithoutFinishReasonStaysInterrupted 是反向对照：干净结束但
// 缺少尾包（FinishReason=stop）的流必须仍被判定为中断。
func TestTencentStreamWithoutFinishReasonStaysInterrupted(t *testing.T) {
	body := "data: {\"Choices\":[{\"Delta\":{\"Content\":\"hi\"}}]}\n\n"
	info := runTencentStream(t, body)

	require.False(t, info.StreamStatus.IsNormalEnd(),
		"缺少尾包的混元流必须仍记为中断，实际 EndReason=%q",
		info.StreamStatus.EndReason)
	require.Equal(t, relaycommon.StreamEndReasonUpstreamInterrupted, info.StreamStatus.EndReason)
}
