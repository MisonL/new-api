package cloudflare

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 上游连接中途出错时，cfStreamHandler 只把 scanner.Err() 记进日志，随后照常
// 发送 [DONE]：客户端据此认为流已正常完成，计费按估算文本结算，渠道健康却
// 记成中断。错误不能被吞成成功——中断的流必须以错误帧收尾，且不得发送 [DONE]。

type failingBodyReader struct {
	payload []byte
	sent    bool
	err     error
}

func (r *failingBodyReader) Read(p []byte) (int, error) {
	if !r.sent {
		r.sent = true
		n := copy(p, r.payload)
		return n, nil
	}
	return 0, r.err
}

func TestCloudflareStreamTransportErrorDoesNotClaimCompletion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(&failingBodyReader{
			payload: []byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"),
			err:     errors.New("upstream connection reset"),
		}),
	}
	info := &relaycommon.RelayInfo{
		IsStream:     true,
		StreamStatus: relaycommon.NewStreamStatus(),
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "cf-model"},
	}

	apiErr, _ := cfStreamHandler(c, info, resp)

	body := recorder.Body.String()
	require.NotContains(t, body, "[DONE]",
		"传输错误的流不得以 [DONE] 收尾（客户端会当成正常完成）：%s", body)
	require.NotNil(t, apiErr, "传输错误必须向上返回，而不是只记日志")
	require.False(t, info.StreamStatus.IsSuccessfulEnd(),
		"传输错误的流不得记为成功结束，实际 EndReason=%q", info.StreamStatus.EndReason)
	require.Contains(t, body, "hi", "已经产出的内容不应被丢弃")
}

// 反向对照：健康流（有 [DONE]）必须仍然正常收尾并返回 nil。
func TestCloudflareStreamHealthyStillCompletes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n" +
				"data: [DONE]\n\n")),
	}
	info := &relaycommon.RelayInfo{
		IsStream:     true,
		StreamStatus: relaycommon.NewStreamStatus(),
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "cf-model"},
	}

	apiErr, _ := cfStreamHandler(c, info, resp)

	require.Nil(t, apiErr, "健康流不得返回错误")
	require.Contains(t, recorder.Body.String(), "[DONE]")
	require.True(t, info.StreamStatus.IsSuccessfulEnd())
}
