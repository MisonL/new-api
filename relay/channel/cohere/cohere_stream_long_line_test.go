package cohere

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
// 单行超过 64KiB 的 Cohere SSE 帧会让 Scan 停在 ErrTooLong，收尾块
// is_finished=true 永远读不到，健康流被判成中断。
func runCohereStreamWithRecorder(t *testing.T, body string) (*relaycommon.RelayInfo, *httptest.ResponseRecorder, *types.NewAPIError) {
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
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "command-r"},
	}

	_, apiErr := cohereStreamHandler(c, info, resp)
	return info, recorder.ResponseRecorder, apiErr
}

func TestCohereStreamCarriesFrameOver64KiB(t *testing.T) {
	long := strings.Repeat("b", 70*1024)
	body := "{\"text\":\"" + long + "\",\"is_finished\":false}\n" +
		"{\"is_finished\":true,\"finish_reason\":\"COMPLETE\",\"response\":{\"meta\":{\"billed_units\":{\"input_tokens\":3,\"output_tokens\":2}}}}\n"
	info, recorder, apiErr := runCohereStreamWithRecorder(t, body)

	require.Nil(t, apiErr, "超过 64KiB 的帧不得把流变成错误响应")
	require.True(t, info.StreamStatus.IsNormalEnd(),
		"超过 64KiB 的帧不得截断流，收尾块必须仍然可达，实际 EndReason=%q",
		info.StreamStatus.EndReason)
	require.True(t, info.StreamStatus.IsSuccessfulEnd(),
		"超长帧后的收尾块必须把流记为成功，实际 EndReason=%q",
		info.StreamStatus.EndReason)
	require.Contains(t, recorder.Body.String(), long,
		"超长帧的内容必须完整转发给客户端")
}
