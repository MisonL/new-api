package ollama

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

// ollamaStreamHandler 自建扫描器。它已经会在缺少 done 帧时返回错误，但健康路径
// 从不写 info.StreamStatus：EndReason 仍是 None，classifyStreamStatus 会把每条
// 健康流记成 "error"，渠道健康探针也永远无法标记成功
// （controller/relay.go:97 依赖 IsSuccessfulEnd）。

func runOllamaStreamEndReason(t *testing.T, body string) (*relaycommon.RelayInfo, bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	// 生产环境在 controller/relay.go:68 初始化 StreamStatus，夹具必须一致，
	// 否则断言处解引用 nil。
	info := &relaycommon.RelayInfo{
		IsStream:     true,
		StreamStatus: relaycommon.NewStreamStatus(),
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "qwen3"},
	}

	_, apiErr := ollamaStreamHandler(c, info, resp)
	return info, apiErr == nil
}

// TestOllamaStreamWithDoneEndsNormally 是回归：以 done 帧收尾的流必须被判定为
// 正常结束。
func TestOllamaStreamWithDoneEndsNormally(t *testing.T) {
	body := strings.Join([]string{
		`{"model":"qwen3","message":{"role":"assistant","content":"hi"},"done":false}`,
		`{"model":"qwen3","message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","prompt_eval_count":3,"eval_count":2}`,
	}, "\n")
	info, ok := runOllamaStreamEndReason(t, body)
	require.True(t, ok, "健康的 Ollama 流不得返回错误")

	require.True(t, info.StreamStatus.IsNormalEnd(),
		"done 帧收尾的 Ollama 流必须记为正常结束，实际 EndReason=%q",
		info.StreamStatus.EndReason)
	require.True(t, info.StreamStatus.IsSuccessfulEnd(),
		"正常结束的 Ollama 流必须视为成功，否则渠道健康探针永远无法标记成功")
}

// TestOllamaStreamWithoutDoneStaysInterrupted 是反向对照：缺少 done 帧的流已经
// 返回错误，不得被记成正常结束。
func TestOllamaStreamWithoutDoneStaysInterrupted(t *testing.T) {
	body := `{"model":"qwen3","message":{"role":"assistant","content":"hi"},"done":false}`
	info, ok := runOllamaStreamEndReason(t, body)
	require.False(t, ok, "缺少 done 帧的 Ollama 流必须返回错误")

	require.False(t, info.StreamStatus.IsNormalEnd(),
		"缺少 done 帧的 Ollama 流不得记为正常结束，实际 EndReason=%q",
		info.StreamStatus.EndReason)
}
