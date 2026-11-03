package claude

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Claude 渠道也会服务 OpenAI 格式的客户端（/v1/chat/completions 走格式转换）。
// prelude 重试标记原先只在 RelayFormatClaude 分支维护，OpenAI 格式下恒为 false：
// 上游只发 message_start 后断流时会被当成「已提交内容」，既不重试也按 prompt
// 计费，客户端拿到空回复。这条用例与 claude_stream_empty_retry_test.go 的第一条
// 对应，只把客户端格式换成 OpenAI。
func TestClaudeStreamInterruptedBeforeContentIsRetryableForOpenAIClient(t *testing.T) {
	c, _ := newClaudeStreamContext(t)
	info := &relaycommon.RelayInfo{
		IsStream:    true,
		RelayFormat: types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-x"},
	}

	usage, apiErr := ClaudeStreamHandler(c, claudeStreamResponse(claudePreludeOnlyBody()), info)

	require.Nil(t, usage)
	require.NotNil(t, apiErr, "OpenAI 客户端格式下，只收到 message_start 就断流也必须可重试")
	require.Equal(t, types.ErrorCodeUpstreamTransportInterrupted, apiErr.GetErrorCode())
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamPreludeRetrySafe))
}

// 内容帧下发后断流仍然不可重试，避免重放重复内容。
func TestClaudeStreamInterruptedAfterContentIsNotRetryableForOpenAIClient(t *testing.T) {
	c, _ := newClaudeStreamContext(t)
	info := &relaycommon.RelayInfo{
		IsStream:    true,
		RelayFormat: types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-x"},
	}

	_, apiErr := ClaudeStreamHandler(c, claudeStreamResponse(claudeStreamBody(false)), info)

	require.Nil(t, apiErr, "内容已下发后断流不能再重试")
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamPreludeRetrySafe))
}

// 上游 200 后迟迟不发数据帧时，ping 会先把响应写出去（Written() 为真）。
// 此时若 prelude 标记仍为 false，控制器会拒绝换渠道，客户端直接收 502。
// 入口统一置位后，这类「首帧前断流」仍是可重试的。
func TestClaudeStreamPreludeRetrySafeAfterPingBeforeAnyData(t *testing.T) {
	c, _ := newClaudeStreamContext(t)
	_, writeErr := c.Writer.Write([]byte(": PING\n\n"))
	require.NoError(t, writeErr)

	info := &relaycommon.RelayInfo{
		IsStream:    true,
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-x"},
	}

	_, apiErr := ClaudeStreamHandler(c, claudeStreamResponse(""), info)

	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeUpstreamTransportInterrupted, apiErr.GetErrorCode())
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamPreludeRetrySafe),
		"只写出过 ping 时重试仍然安全，标记必须为真供控制器判断")
}

var _ = httptest.NewRecorder
var _ = http.MethodPost
var _ = gin.TestMode
