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

func runCloudflareStream(t *testing.T, body string) (*relaycommon.RelayInfo, *httptest.ResponseRecorder) {
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
	return info, recorder
}

// TestCloudflareStreamWithDoneEndsNormally 是回归：以 data: [DONE] 收尾的流必须
// 被判定为正常结束。
func TestCloudflareStreamWithDoneEndsNormally(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n" +
		"data: [DONE]\n\n"
	info, _ := runCloudflareStream(t, body)

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
	info, _ := runCloudflareStream(t, body)

	require.False(t, info.StreamStatus.IsNormalEnd(),
		"缺少 [DONE] 的 Cloudflare 流必须仍记为中断，实际 EndReason=%q",
		info.StreamStatus.EndReason)
	require.Equal(t, relaycommon.StreamEndReasonUpstreamInterrupted, info.StreamStatus.EndReason)
}

// TestCloudflareStreamAcceptsDataLinesWithoutSpace：SSE 规范允许 data: 后不带
// 空格，共享扫描器用 TrimSpace 兼容两种写法（stream_scanner.go:343）。本渠道
// 只认 "data: " 前缀时，这类帧会 Unmarshal 失败并被静默丢弃。
func TestCloudflareStreamAcceptsDataLinesWithoutSpace(t *testing.T) {
	body := "data:{\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n" +
		"data:[DONE]\n\n"
	info, recorder := runCloudflareStream(t, body)

	require.Contains(t, recorder.Body.String(), `"content":"hi"`,
		"data: 不带空格的帧必须被解析并转发，而不是被静默丢弃")
	require.True(t, info.StreamStatus.IsNormalEnd(),
		"data:[DONE] 收尾的流必须记为正常结束，实际 EndReason=%q",
		info.StreamStatus.EndReason)
}
