package zhipu

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
// 单行超过 64KiB 的智谱 SSE 帧会让 Scan 停在 ErrTooLong，meta 终止事件
// 永远读不到，健康流被判成中断。
func runZhipuStreamWithRecorder(t *testing.T, body string) (*relaycommon.RelayInfo, *httptest.ResponseRecorder, *types.NewAPIError) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := &closeNotifyRecorder{ResponseRecorder: httptest.NewRecorder(), closeChan: make(chan bool)}
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
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "glm-4"},
	}

	_, apiErr := zhipuStreamHandler(c, info, resp)
	return info, recorder.ResponseRecorder, apiErr
}

func TestZhipuStreamCarriesFrameOver64KiB(t *testing.T) {
	long := strings.Repeat("b", 70*1024)
	body := "data: {\"choices\":[{\"content\":\"" + long + "\"}]}\n" +
		"meta: {\"request_id\":\"r1\",\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n"
	info, recorder, apiErr := runZhipuStreamWithRecorder(t, body)

	require.Nil(t, apiErr, "超过 64KiB 的帧不得把流变成错误响应")
	require.True(t, info.StreamStatus.IsNormalEnd(),
		"超过 64KiB 的帧不得截断流，meta 事件必须仍然可达，实际 EndReason=%q",
		info.StreamStatus.EndReason)
	require.True(t, info.StreamStatus.IsSuccessfulEnd(),
		"超长帧后的 meta 必须把流记为成功，实际 EndReason=%q",
		info.StreamStatus.EndReason)
	require.Contains(t, recorder.Body.String(), long,
		"超长帧的内容必须完整转发给客户端")
}
