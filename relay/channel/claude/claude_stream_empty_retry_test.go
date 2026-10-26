package claude

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 生产日志里的空回复长这样：上游 200 之后只发出 message_start，随后
// GOAWAY / unexpected EOF 断流。旧行为是补一个 message_stop 当成功返回，
// 客户端拿到空消息且按 prompt token 计费。现在必须返回可重试错误。

func claudePreludeOnlyBody() string {
	return "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-x\",\"usage\":{\"input_tokens\":150000,\"output_tokens\":0}}}\n\n"
}

func newClaudeStreamContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	return c, recorder
}

func claudeStreamResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestClaudeStreamInterruptedBeforeContentIsRetryable(t *testing.T) {
	c, _ := newClaudeStreamContext(t)
	info := &relaycommon.RelayInfo{
		IsStream:    true,
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-x"},
	}

	usage, apiErr := ClaudeStreamHandler(c, claudeStreamResponse(claudePreludeOnlyBody()), info)

	require.Nil(t, usage)
	require.NotNil(t, apiErr, "只收到 message_start 就断流时必须报错，而不是返回空回复")
	require.Equal(t, types.ErrorCodeUpstreamTransportInterrupted, apiErr.GetErrorCode())
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamPreludeRetrySafe),
		"只有生命周期帧时重试是安全的，必须允许控制器换渠道")
}

func TestClaudeStreamInterruptedAfterContentIsNotRetryable(t *testing.T) {
	c, _ := newClaudeStreamContext(t)
	info := &relaycommon.RelayInfo{
		IsStream:    true,
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-x"},
	}

	_, apiErr := ClaudeStreamHandler(c, claudeStreamResponse(claudeStreamBody(false)), info)

	require.Nil(t, apiErr, "内容已下发后断流不能再重试，保持原行为")
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamPreludeRetrySafe),
		"内容帧到达后必须清掉 prelude 重试标记")
}

func TestClaudeStreamRetryDoesNotRepeatMessageStart(t *testing.T) {
	c, recorder := newClaudeStreamContext(t)

	firstInfo := &relaycommon.RelayInfo{
		IsStream:    true,
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-x"},
	}
	_, apiErr := ClaudeStreamHandler(c, claudeStreamResponse(claudePreludeOnlyBody()), firstInfo)
	require.NotNil(t, apiErr)

	// 模拟控制器换渠道重试：同一个 gin context，第二次拿到健康流。
	secondInfo := &relaycommon.RelayInfo{
		IsStream:    true,
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-x"},
	}
	_, apiErr = ClaudeStreamHandler(c, claudeStreamResponse(claudeStreamBody(true)), secondInfo)
	require.Nil(t, apiErr)

	body := recorder.Body.String()
	require.Equal(t, 1, strings.Count(body, `"type":"message_start"`),
		"重试不得重复下发 message_start，否则客户端会重建消息状态")
	require.Contains(t, body, `"text":"hi"`, "重试渠道的内容必须正常下发")
}
