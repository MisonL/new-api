package coze

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

// Coze 的带内错误事件（event: error）此前只写一行 SysLog 后 return false，
// 循环继续把流当正常结束处理：客户端收到 [DONE] 与 200 成功，渠道侧按估算
// 文本结算。带内错误必须转成 API 错误，且不得再发 [DONE]。

func TestCozeStreamErrorEventIsNotReportedAsSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	body := "event: conversation.message.delta\n" +
		"data: {\"content\":\"hi\"}\n\n" +
		"event: error\n" +
		"data: {\"code\":5000,\"message\":\"internal error\"}\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		IsStream:     true,
		StreamStatus: relaycommon.NewStreamStatus(),
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "coze-model"},
	}

	_, apiErr := cozeChatStreamHandler(c, info, resp)

	require.NotNil(t, apiErr, "带内错误事件必须转成 API 错误")
	if apiErr != nil {
		require.Contains(t, apiErr.Error(), "internal error",
			"上游错误信息必须透传，实际：%s", apiErr.Error())
	}
	require.NotContains(t, recorder.Body.String(), "[DONE]",
		"错误流不得用 [DONE] 告诉客户端已完成：%s", recorder.Body.String())
	require.False(t, info.StreamStatus.IsSuccessfulEnd(),
		"带内错误的流不得记为成功结束，实际 EndReason=%q", info.StreamStatus.EndReason)
}

// 反向对照：completed 收尾的健康流仍须返回 nil 且发送 [DONE] 之外的正常收尾。
func TestCozeHealthyStreamStillCompletes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	body := "event: conversation.message.delta\n" +
		"data: {\"content\":\"hi\"}\n\n" +
		"event: conversation.chat.completed\n" +
		"data: {\"usage\":{\"input_count\":3,\"output_count\":2,\"token_count\":5}}\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		IsStream:     true,
		StreamStatus: relaycommon.NewStreamStatus(),
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "coze-model"},
	}

	_, apiErr := cozeChatStreamHandler(c, info, resp)

	require.Nil(t, apiErr, "健康流不得返回错误")
	require.Contains(t, recorder.Body.String(), "[DONE]")
	require.True(t, info.StreamStatus.IsSuccessfulEnd())
}
