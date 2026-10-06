package cloudflare

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

// cfStreamHandler 自建扫描器，从不写 info.StreamStatus。健康流以 [DONE] 收尾后
// EndReason 仍是 None（零值），classifyStreamStatus 会把每条健康流记成 "error"，
// 渠道健康探针也永远无法标记成功（controller/relay.go:97 依赖 IsSuccessfulEnd）。

func runCloudflareStream(t *testing.T, body string) *relaycommon.RelayInfo {
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
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "cf-model"},
	}

	apiErr, _ := cfStreamHandler(c, info, resp)
	require.Nil(t, apiErr, "健康的 Cloudflare 流不得返回错误")
	return info
}

// TestCloudflareStreamWithDoneEndsNormally 是回归：以 data: [DONE] 收尾的流必须
// 被判定为正常结束。
func TestCloudflareStreamWithDoneEndsNormally(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n" +
		"data: [DONE]\n\n"
	info := runCloudflareStream(t, body)

	require.True(t, info.StreamStatus.IsNormalEnd(),
		"[DONE] 收尾的 Cloudflare 流必须记为正常结束，实际 EndReason=%q",
		info.StreamStatus.EndReason)
	require.True(t, info.StreamStatus.IsSuccessfulEnd(),
		"正常结束的 Cloudflare 流必须视为成功，否则渠道健康探针永远无法标记成功")
}

// TestCloudflareStreamWithoutDoneStaysInterrupted 是反向对照：干净结束但缺少
// [DONE] 的流（上游截断）必须仍被判定为中断，修复不得把任何 EOF 都当正常结束。
func TestCloudflareStreamWithoutDoneStaysInterrupted(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"
	info := runCloudflareStream(t, body)

	require.False(t, info.StreamStatus.IsNormalEnd(),
		"缺少 [DONE] 的 Cloudflare 流必须仍记为中断，实际 EndReason=%q",
		info.StreamStatus.EndReason)
	require.Equal(t, relaycommon.StreamEndReasonUpstreamInterrupted, info.StreamStatus.EndReason)
}
