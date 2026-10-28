package baidu

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// closeTrackingBody 记录是否被关闭；Read 始终失败，模拟读取中断或超限。
type closeTrackingBody struct {
	closed bool
}

func (b *closeTrackingBody) Read(p []byte) (int, error) {
	return 0, errors.New("read interrupted")
}

func (b *closeTrackingBody) Close() error {
	b.closed = true
	return nil
}

// 读取失败的响应体此前不关闭：这个分支直接返回，没有任何调用方会再替它关闭，
// 连接与文件描述符因此泄漏。整棵 relay/channel 树上的同类分支由
// TestResponseBodyReadersDeferClose 静态守住，这里用真实调用验证运行时行为。
func TestBaiduHandlerClosesBodyOnReadError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	body := &closeTrackingBody{}
	resp := &http.Response{StatusCode: http.StatusOK, Body: body}

	apiErr, usage := baiduHandler(c, &relaycommon.RelayInfo{}, resp)

	require.NotNil(t, apiErr)
	require.Nil(t, usage)
	require.True(t, body.closed, "a failed body read must still close the response body")
}
