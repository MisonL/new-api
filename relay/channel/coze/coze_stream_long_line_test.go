package coze

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
// 单行超过 64KiB 的 Coze SSE 帧会让 Scan 停在 ErrTooLong，循环直接退出、
// scanner.Err() 返回 BadResponseBody——超长帧把整条流变成错误响应。
func runCozeStreamWithRecorder(t *testing.T, body string) (*relaycommon.RelayInfo, *httptest.ResponseRecorder, *types.NewAPIError) {
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
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "coze-model"},
	}

	_, apiErr := cozeChatStreamHandler(c, info, resp)
	return info, recorder, apiErr
}

func TestCozeStreamCarriesFrameOver64KiB(t *testing.T) {
	long := strings.Repeat("b", 70*1024)
	body := "event: conversation.message.delta\n" +
		"data: {\"content\":\"" + long + "\"}\n\n" +
		"event: conversation.chat.completed\n" +
		"data: {\"usage\":{\"input_count\":3,\"output_count\":2,\"token_count\":5}}\n\n"
	info, recorder, apiErr := runCozeStreamWithRecorder(t, body)

	require.Nil(t, apiErr, "超过 64KiB 的帧不得把流变成错误响应，实际 err=%v", apiErr)
	require.True(t, info.StreamStatus.IsNormalEnd(),
		"超过 64KiB 的帧不得截断流，completed 事件必须仍然可达，实际 EndReason=%q",
		info.StreamStatus.EndReason)
	require.True(t, info.StreamStatus.IsSuccessfulEnd(),
		"超长帧后的 completed 必须把流记为成功，实际 EndReason=%q",
		info.StreamStatus.EndReason)
	require.Contains(t, recorder.Body.String(), long,
		"超长帧的内容必须完整转发给客户端")
}
