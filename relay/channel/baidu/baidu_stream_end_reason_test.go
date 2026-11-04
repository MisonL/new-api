package baidu

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

// 文心流式协议以 is_end=true 收尾，从不发送 `data: [DONE]`。共享扫描器只认
// 字面 [DONE]，处理器又从不调用 sr.MarkDone()，于是每条健康的文心流都被记成
// upstream_interrupted：渠道健康探针永远无法标记成功，截断与正常收尾也无法区分。

func baiduChunk(result string, isEnd bool, usage string) string {
	chunk := `{"id":"bot-1","object":"chat.completion","created":1700000000,"model":"ernie-4.0","result":"` + result + `"`
	if isEnd {
		chunk += `,"is_end":true`
	} else {
		chunk += `,"is_end":false`
	}
	if usage != "" {
		chunk += `,"usage":` + usage
	}
	return "data: " + chunk + "}\n\n"
}

func runBaiduStream(t *testing.T, body string) (*relaycommon.RelayInfo, *http.Response) {
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
	info := &relaycommon.RelayInfo{
		IsStream:     true,
		RelayFormat:  types.RelayFormatOpenAI,
		StreamStatus: relaycommon.NewStreamStatus(),
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "ernie-4.0"},
	}
	apiErr, _ := baiduStreamHandler(c, info, resp)
	require.Nil(t, apiErr, "健康的文心流不得返回错误")
	return info, resp
}

// TestBaiduStreamWithIsEndEndsNormally 是回归：is_end=true 收尾的流必须记为正常结束。
func TestBaiduStreamWithIsEndEndsNormally(t *testing.T) {
	body := baiduChunk("he", false, "") +
		baiduChunk("llo", true, `{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}`)

	info, _ := runBaiduStream(t, body)

	require.True(t, info.StreamStatus.IsNormalEnd(),
		"is_end 收尾的文心流必须记为正常结束，实际 EndReason=%q end_error=%v",
		info.StreamStatus.EndReason, info.StreamStatus.EndError)
	require.True(t, info.StreamStatus.IsSuccessfulEnd(),
		"正常结束的文心流必须视为成功，否则渠道健康探针永远无法标记成功")
}

// TestBaiduStreamWithoutIsEndStaysInterrupted 是反向对照：没有 is_end 的流
// （上游截断）必须仍记为中断，修复不得把任何收尾都当正常结束。
func TestBaiduStreamWithoutIsEndStaysInterrupted(t *testing.T) {
	body := baiduChunk("he", false, "")

	info, _ := runBaiduStream(t, body)

	require.False(t, info.StreamStatus.IsNormalEnd(),
		"缺少 is_end 的文心流必须仍记为中断，实际 EndReason=%q", info.StreamStatus.EndReason)
	require.Equal(t, relaycommon.StreamEndReasonUpstreamInterrupted, info.StreamStatus.EndReason)
}
