package tencent

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

// bufio.NewScanner 默认 token 上限 64KiB，共享扫描器显式抬到 64MiB
// （stream_scanner.go:218），本渠道的裸 NewScanner 没有调 Buffer：
// 单行超过 64KiB 的混元 SSE 帧会让 Scan 停在 ErrTooLong，尾包
// FinishReason=stop 永远读不到，健康流被判成中断。
func runTencentStreamWithRecorder(t *testing.T, body string) (*relaycommon.RelayInfo, *httptest.ResponseRecorder, *types.NewAPIError) {
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
		StreamStatus: relaycommon.NewStreamStatus(),
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "hunyuan-lite"},
	}

	_, apiErr := tencentStreamHandler(c, info, resp)
	return info, recorder, apiErr
}

func TestTencentStreamCarriesFrameOver64KiB(t *testing.T) {
	long := strings.Repeat("b", 70*1024)
	body := "data: {\"Choices\":[{\"Delta\":{\"Content\":\"hi\"}}]}\n\n" +
		"data: {\"Choices\":[{\"Delta\":{\"Content\":\"" + long + "\"}}]}\n\n" +
		"data: {\"Choices\":[{\"FinishReason\":\"stop\"}]}\n\n"
	info, recorder, apiErr := runTencentStreamWithRecorder(t, body)

	require.Nil(t, apiErr, "超过 64KiB 的帧不得把流变成错误响应")
	require.True(t, info.StreamStatus.IsNormalEnd(),
		"超过 64KiB 的帧不得截断流，尾包必须仍然可达，实际 EndReason=%q",
		info.StreamStatus.EndReason)
	require.True(t, info.StreamStatus.IsSuccessfulEnd(),
		"超长帧后的尾包必须把流记为成功，实际 EndReason=%q",
		info.StreamStatus.EndReason)
	require.Contains(t, recorder.Body.String(), long,
		"超长帧的内容必须完整转发给客户端")
}
