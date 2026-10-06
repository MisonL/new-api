package claude

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Anthropic 的 SSE 协议以 message_stop 作为终止事件，从不发送 OpenAI 风格的
// `data: [DONE]`。共享扫描器把「干净 EOF 且未见 [DONE]」一律判为
// upstream_transport_interrupted，因此每一条健康的 Claude 流都会被记成
// 上游中断：日志里打两条 ERROR、stream_status 记成 partial、渠道健康探针也
// 永远无法标记成功。处理器见到 message_stop 时必须显式记录正常结束。

func claudeStreamBody(withMessageStop bool) string {
	var body strings.Builder
	body.WriteString("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-x\",\"usage\":{\"input_tokens\":3,\"output_tokens\":1}}}\n\n")
	body.WriteString("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
	body.WriteString("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n")
	body.WriteString("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
	body.WriteString("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":7}}\n\n")
	if withMessageStop {
		body.WriteString("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}
	// 两种情况都不发送 data: [DONE]，Anthropic 不提供该哨兵。
	return body.String()
}

func runClaudeStream(t *testing.T, body string) *relaycommon.RelayInfo {
	t.Helper()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		IsStream:    true,
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-x"},
	}

	_, apiErr := ClaudeStreamHandler(c, resp, info)
	require.Nil(t, apiErr, "健康的 Claude 流不得返回错误")
	return info
}

// TestClaudeStreamWithMessageStopEndsNormally 是回归：以 message_stop 正常收尾的
// 流必须被判定为正常结束。
func TestClaudeStreamWithMessageStopEndsNormally(t *testing.T) {
	info := runClaudeStream(t, claudeStreamBody(true))

	require.True(t, info.StreamStatus.IsNormalEnd(),
		"message_stop 收尾的 Claude 流必须记为正常结束，实际 EndReason=%q end_error=%v",
		info.StreamStatus.EndReason, info.StreamStatus.EndError)
	require.True(t, info.StreamStatus.IsSuccessfulEnd(),
		"正常结束的 Claude 流必须视为成功，否则渠道健康探针永远无法标记成功")
}

// TestClaudeStreamWithoutMessageStopStaysInterrupted 是反向对照：缺少
// message_stop 的流（上游中途断开）必须仍被判定为中断，修复不得把
// 「任何 EOF」都当成正常结束。
func TestClaudeStreamWithoutMessageStopStaysInterrupted(t *testing.T) {
	info := runClaudeStream(t, claudeStreamBody(false))

	require.False(t, info.StreamStatus.IsNormalEnd(),
		"缺少 message_stop 的 Claude 流必须仍记为中断，实际 EndReason=%q",
		info.StreamStatus.EndReason)
	require.Equal(t, relaycommon.StreamEndReasonUpstreamInterrupted, info.StreamStatus.EndReason)
}
