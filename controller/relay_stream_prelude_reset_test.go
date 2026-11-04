package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// prelude 标记存在 gin context 里，重试复用同一个上下文。参与置位的渠道
// 会在 handler 入口重新置 true；不参与的自管渠道（dify/cohere/palm 等）
// 从不清除它。若上一轮留下 true，这些渠道写出内容后失败时
// shouldRetry 的 Written() 保护会被短路，导致同一响应追加第二份内容。
// 每次 attempt 开始必须把它重置为“不安全”，由本轮渠道显式声明可重试。
func TestBeginResponsesStreamRelayAttemptResetsPreludeRetryFlag(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	common.SetContextKey(c, constant.ContextKeyResponsesStreamPreludeRetrySafe, true)
	info := &relaycommon.RelayInfo{IsStream: true, ReceivedResponseCount: 5}

	beginResponsesStreamRelayAttempt(c, info)

	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamPreludeRetrySafe),
		"新一轮 attempt 不能继承上一轮的内容帧状态")
	require.Zero(t, info.ReceivedResponseCount,
		"帧计数是 attempt 级状态，必须随新一轮归零")
}

func TestBeginResponsesStreamRelayAttemptKeepsNonStreamUntouched(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	common.SetContextKey(c, constant.ContextKeyResponsesStreamPreludeRetrySafe, true)
	info := &relaycommon.RelayInfo{IsStream: false, ReceivedResponseCount: 5}

	beginResponsesStreamRelayAttempt(c, info)

	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamPreludeRetrySafe))
	require.Equal(t, 5, info.ReceivedResponseCount)
}
