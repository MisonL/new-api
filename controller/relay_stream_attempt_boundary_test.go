package controller

import (
	"errors"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// StreamStatus 的终态原因由 sync.Once 锁定（relay/common/stream_status.go:48）。
// 因此每次上游 attempt 必须拿到全新的 StreamStatus，否则第一次失败流的
// 终态会永久占据该字段，把后续成功流的结束原因判成失败。
//
// beginResponsesStreamRelayAttempt 只对 RelayModeResponses* 重建，
// chat / claude / gemini 流式走 stream_scanner.go 的「仅 nil 时初始化」，
// 于是跨渠道重试时旧终态被继承。

func newAttemptBoundaryInfo(mode int) *relaycommon.RelayInfo {
	info := &relaycommon.RelayInfo{
		IsStream:  true,
		RelayMode: mode,
	}
	info.ChannelMeta = &relaycommon.ChannelMeta{}
	return info
}

// TestResponsesAttemptBoundaryResetsStreamStatus 是现有正确行为的对照：
// Responses 模式下每次 attempt 都必须重置 StreamStatus。
func TestResponsesAttemptBoundaryResetsStreamStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)

	info := newAttemptBoundaryInfo(relayconstant.RelayModeResponses)

	// attempt 1：上游连接中断，锁定 interrupted 终态。
	beginResponsesStreamRelayAttempt(c, info)
	require.NotNil(t, info.StreamStatus)
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonUpstreamInterrupted, errors.New("boom"))
	require.False(t, info.StreamStatus.IsSuccessfulEnd())

	// attempt 2：渠道重试后正常结束。必须拿到全新的 StreamStatus。
	beginResponsesStreamRelayAttempt(c, info)
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
	require.True(t, info.StreamStatus.IsSuccessfulEnd(),
		"Responses 重试后的成功流必须被判为成功结束")
}

// TestChatAttemptBoundaryResetsStreamStatus 是回归：
// chat 流的渠道重试同样不得继承上一次 attempt 的终态。
func TestChatAttemptBoundaryResetsStreamStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)

	info := newAttemptBoundaryInfo(relayconstant.RelayModeChatCompletions)

	// attempt 1：上游被截断，记录非正常终态。
	beginResponsesStreamRelayAttempt(c, info)
	require.NotNil(t, info.StreamStatus,
		"attempt 边界必须为 chat 流建立 StreamStatus")
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonUpstreamInterrupted, errors.New("boom"))

	// attempt 2：渠道重试后正常结束。
	beginResponsesStreamRelayAttempt(c, info)
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)

	require.True(t, info.StreamStatus.IsSuccessfulEnd(),
		"chat 流重试后的成功结束不得被上一次 attempt 的 interrupted 终态污染")
}

// TestGeminiAttemptBoundaryResetsStreamStatus 覆盖 Gemini 通道（同一非 Responses 分支）。
func TestGeminiAttemptBoundaryResetsStreamStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)

	info := newAttemptBoundaryInfo(relayconstant.RelayModeGemini)

	beginResponsesStreamRelayAttempt(c, info)
	require.NotNil(t, info.StreamStatus,
		"attempt 边界必须为 Gemini 流建立 StreamStatus")
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, errors.New("scanner blew up"))

	beginResponsesStreamRelayAttempt(c, info)
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)

	require.True(t, info.StreamStatus.IsSuccessfulEnd(),
		"Gemini 流重试后的成功结束不得被上一次 attempt 的 scanner_error 污染")
}
