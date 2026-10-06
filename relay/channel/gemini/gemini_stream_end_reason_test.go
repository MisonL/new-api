package gemini

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

// Gemini 的 SSE 协议以候选分片携带 finishReason 作为终止信号，从不发送
// `data: [DONE]`。共享扫描器把「干净 EOF 且未见 [DONE]」判为
// upstream_transport_interrupted，因此每一条健康的 Gemini 流都会被记成上游中断。
// 处理器见到终止分片时必须显式记录正常结束。

func geminiChunk(text string, finishReason string, usage string) string {
	var candidate strings.Builder
	candidate.WriteString(`{`)
	if text != "" {
		candidate.WriteString(`"content":{"parts":[{"text":"` + text + `"}],"role":"model"}`)
	} else {
		// 真实的终止分片通常不带 parts，只有 finishReason。
		candidate.WriteString(`"content":{"role":"model"}`)
	}
	if finishReason != "" {
		candidate.WriteString(`,"finishReason":"` + finishReason + `","index":0`)
	}
	candidate.WriteString(`}`)
	chunk := `{"candidates":[` + candidate.String() + `]`
	if usage != "" {
		chunk += `,"usageMetadata":` + usage
	}
	return "data: " + chunk + "}\n\n"
}

func runGeminiStream(t *testing.T, body string) (*relaycommon.RelayInfo, int) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/g:streamGenerateContent", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		IsStream:    true,
		RelayFormat: types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-2.5-pro"},
	}

	usage, apiErr := GeminiChatStreamHandler(c, info, resp)
	require.Nil(t, apiErr, "健康的 Gemini 流不得返回错误")
	if usage == nil {
		return info, 0
	}
	return info, usage.TotalTokens
}

// TestGeminiStreamWithFinishReasonEndsNormally 是回归：以 finishReason 正常收尾的
// 流必须被判定为正常结束。
func TestGeminiStreamWithFinishReasonEndsNormally(t *testing.T) {
	body := geminiChunk("hi", "", `{"promptTokenCount":3,"candidatesTokenCount":2,"totalTokenCount":5}`) +
		geminiChunk("", "STOP", `{"promptTokenCount":3,"candidatesTokenCount":2,"totalTokenCount":5}`)

	info, _ := runGeminiStream(t, body)

	require.True(t, info.StreamStatus.IsNormalEnd(),
		"finishReason 收尾的 Gemini 流必须记为正常结束，实际 EndReason=%q end_error=%v",
		info.StreamStatus.EndReason, info.StreamStatus.EndError)
	require.True(t, info.StreamStatus.IsSuccessfulEnd(),
		"正常结束的 Gemini 流必须视为成功，否则渠道健康探针永远无法标记成功")
}

// TestGeminiStreamWithoutFinishReasonStaysInterrupted 是反向对照：缺少终止分片的流
// （上游中途断开）必须仍被判定为中断。
func TestGeminiStreamWithoutFinishReasonStaysInterrupted(t *testing.T) {
	body := geminiChunk("hi", "", `{"promptTokenCount":3,"candidatesTokenCount":2,"totalTokenCount":5}`)

	info, _ := runGeminiStream(t, body)

	require.False(t, info.StreamStatus.IsNormalEnd(),
		"缺少 finishReason 的 Gemini 流必须仍记为中断，实际 EndReason=%q",
		info.StreamStatus.EndReason)
	require.Equal(t, relaycommon.StreamEndReasonUpstreamInterrupted, info.StreamStatus.EndReason)
}

// TestGeminiStreamCountsUsageAfterFinishReason pins the decision not to stop the
// scanner at the terminal chunk: Gemini 可能在终止分片之后仍发送仅带
// usageMetadata 的分片，提前停止会漏计用量。
func TestGeminiStreamCountsUsageAfterFinishReason(t *testing.T) {
	body := geminiChunk("hi", "STOP", "") +
		"data: {\"usageMetadata\":{\"promptTokenCount\":3,\"candidatesTokenCount\":9,\"totalTokenCount\":12}}\n\n"

	info, totalTokens := runGeminiStream(t, body)

	require.True(t, info.StreamStatus.IsNormalEnd())
	require.Equal(t, 12, totalTokens,
		"终止分片之后的 usage 分片必须仍被计入")
}
