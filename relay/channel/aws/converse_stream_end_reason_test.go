package aws

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// converseStreamHandler 的成功路径要求 message_start 与 message_stop（Claude 还要求
// metadata），但从不写 info.StreamStatus：健康流以零值 EndReason 结束，
// classifyStreamStatus 把它记成 "error"，渠道健康探针也永远无法标记成功
// （controller/relay.go:97 依赖 IsSuccessfulEnd）。handler 本体需要具体的
// *bedrockruntime.Client，无法在单测里构造，因此终止校验被提取为
// finishConverseStream 并直接驱动。

func runFinishConverseStream(t *testing.T, state *converseStreamState, isClaude bool) (*types.NewAPIError, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	info := &relaycommon.RelayInfo{
		IsStream:     true,
		StreamStatus: relaycommon.NewStreamStatus(),
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "claude-x"},
	}
	apiErr, _ := finishConverseStream(c, info, state, isClaude)
	return apiErr, info
}

func healthyConverseState() *converseStreamState {
	return &converseStreamState{
		ID:               "resp_1",
		Model:            "claude-x",
		MessageStarted:   true,
		MessageStopped:   true,
		MetadataReceived: true,
		FinishReason:     "end_turn",
		Usage:            &dto.Usage{},
		ClaudeUsage:      &dto.ClaudeUsage{},
	}
}

// TestConverseStreamOpenAIEndsNormally 是回归：OpenAI 格式的成功路径必须把流记为
// 正常结束，否则渠道健康探针永远无法标记成功。
func TestConverseStreamOpenAIEndsNormally(t *testing.T) {
	apiErr, info := runFinishConverseStream(t, healthyConverseState(), false)

	require.Nil(t, apiErr, "完整的 OpenAI 流不得返回错误")
	require.True(t, info.StreamStatus.IsNormalEnd(),
		"message_stop 收尾的 Converse 流必须记为正常结束，实际 EndReason=%q",
		info.StreamStatus.EndReason)
	require.True(t, info.StreamStatus.IsSuccessfulEnd(),
		"正常结束的 Converse 流必须视为成功")
}

// TestConverseStreamClaudeEndsNormally 同上，Claude 格式路径（额外要求 metadata）。
func TestConverseStreamClaudeEndsNormally(t *testing.T) {
	apiErr, info := runFinishConverseStream(t, healthyConverseState(), true)

	require.Nil(t, apiErr, "完整的 Claude 流不得返回错误")
	require.True(t, info.StreamStatus.IsSuccessfulEnd(),
		"Claude 格式的 Converse 流必须视为成功，实际 EndReason=%q",
		info.StreamStatus.EndReason)
}

// TestConverseStreamWithoutMessageStopStaysUnclassified 是反向对照：缺少
// message_stop 的流必须返回错误，且不得被记成正常结束。
func TestConverseStreamWithoutMessageStopStaysUnclassified(t *testing.T) {
	state := healthyConverseState()
	state.MessageStopped = false
	apiErr, info := runFinishConverseStream(t, state, false)

	require.NotNil(t, apiErr, "缺少 message_stop 的流必须返回错误")
	require.False(t, info.StreamStatus.IsNormalEnd(),
		"缺少 message_stop 的流不得记为正常结束，实际 EndReason=%q",
		info.StreamStatus.EndReason)
}
