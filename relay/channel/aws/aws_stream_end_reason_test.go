package aws

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// awsStreamHandler 自管 AWS 事件流扫描，不经过 StreamScannerHandler，
// 因此既不会写 info.StreamStatus，也不会走扫描器的中断判定。收尾逻辑被
// 提取为 finishAwsStream 以便脱离真实的 *bedrockruntime.Client 直接驱动。
//
// 判据与 ClaudeStreamHandler 一致：见过 message_stop 就是正常结束；未见终止
// 标记就要看客户端是否已收到内容——prelude 阶段断流可换渠道重放，已下发
// 内容帧之后再断流则按部分成功收尾，不得重放。

func runFinishAwsStream(t *testing.T, claudeInfo *claude.ClaudeResponseInfo, streamErr error, received int, preludeSafe bool) (*relaycommon.RelayInfo, *types.NewAPIError) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	info := &relaycommon.RelayInfo{
		IsStream:              true,
		StreamStatus:          relaycommon.NewStreamStatus(),
		ReceivedResponseCount: received,
	}
	common.SetContextKey(c, constant.ContextKeyResponsesStreamPreludeRetrySafe, preludeSafe)

	return info, finishAwsStream(c, info, claudeInfo, streamErr)
}

func newAwsClaudeInfo(sawMessageStop bool) *claude.ClaudeResponseInfo {
	return &claude.ClaudeResponseInfo{
		ResponseId:     "msg_1",
		Model:          "claude-x",
		Usage:          &dto.Usage{},
		SawMessageStop: sawMessageStop,
	}
}

// 回归：以 message_stop 收尾的健康流必须记为正常结束，否则渠道健康探针
// 永远无法标记恢复（controller/relay.go 依赖 IsSuccessfulEnd）。
func TestAwsStreamEndsNormallyAfterMessageStop(t *testing.T) {
	info, apiErr := runFinishAwsStream(t, newAwsClaudeInfo(true), nil, 4, false)

	require.Nil(t, apiErr, "完整的 AKSK 流不得返回错误")
	require.True(t, info.StreamStatus.IsNormalEnd(),
		"message_stop 收尾的流必须记为正常结束，实际 EndReason=%q", info.StreamStatus.EndReason)
	require.True(t, info.StreamStatus.IsSuccessfulEnd(),
		"message_stop 收尾的流必须视为成功")
}

// 首个内容帧之前断流（客户端只可能收到 ping）必须变成可重试错误，而不是
// 静默补 message_stop、空回复按成功计费。
func TestAwsStreamInterruptedBeforeFirstFrameIsRetryable(t *testing.T) {
	info, apiErr := runFinishAwsStream(t, newAwsClaudeInfo(false), io.ErrUnexpectedEOF, 0, true)

	require.NotNil(t, apiErr, "首个内容帧前断流必须返回可重试错误")
	require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	require.Equal(t, relaycommon.StreamEndReasonUpstreamInterrupted, info.StreamStatus.EndReason,
		"未见到 message_stop 的流必须记为上游中断")
	require.ErrorIs(t, info.StreamStatus.EndError, io.ErrUnexpectedEOF,
		"中断原因必须保留 SDK 事件流的真实错误")
}

// prelude 阶段（已转发 message_start 等生命周期事件，但尚无内容帧）断流同样
// 可以安全重放。
func TestAwsStreamInterruptedDuringPreludeIsRetryable(t *testing.T) {
	_, apiErr := runFinishAwsStream(t, newAwsClaudeInfo(false), io.ErrUnexpectedEOF, 2, true)

	require.NotNil(t, apiErr, "prelude 阶段断流必须返回可重试错误")
	require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
}

// 内容帧已经下发后再断流不得重放：客户端会看到重复内容。必须退回按部分
// 成功收尾，与 ClaudeStreamHandler 的行为一致。
func TestAwsStreamInterruptedAfterContentIsNotRetried(t *testing.T) {
	info, apiErr := runFinishAwsStream(t, newAwsClaudeInfo(false), io.ErrUnexpectedEOF, 5, false)

	require.Nil(t, apiErr, "内容已下发后的断流不得重试")
	require.Equal(t, relaycommon.StreamEndReasonUpstreamInterrupted, info.StreamStatus.EndReason,
		"流确实中断过，EndReason 必须记录中断")
}

// SDK 以干净 EOF 结束但上游没发 message_stop（例如被截断的响应）同样按
// 中断处理，不能因为 stream.Err() 为 nil 就当成正常结束。
func TestAwsStreamCleanEOFWithoutMessageStopIsInterrupted(t *testing.T) {
	info, apiErr := runFinishAwsStream(t, newAwsClaudeInfo(false), nil, 0, true)

	require.NotNil(t, apiErr, "缺少 message_stop 的干净 EOF 必须返回可重试错误")
	require.Equal(t, relaycommon.StreamEndReasonUpstreamInterrupted, info.StreamStatus.EndReason)
}
