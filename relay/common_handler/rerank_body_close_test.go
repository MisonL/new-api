package common_handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// closeTrackingBody 记录是否被关闭；读失败模拟读取中断/超限。
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

// 读取失败的上游响应同样要关闭：不关闭就丢掉了连接回收的机会，而错误分支
// 没有任何调用方会再替它关闭。
func TestRerankHandlerClosesBodyOnReadError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request, _ = http.NewRequest(http.MethodPost, "/v1/rerank", nil)

	body := &closeTrackingBody{}
	resp := &http.Response{StatusCode: http.StatusOK, Body: body}

	usage, apiErr := RerankHandler(c, &relaycommon.RelayInfo{}, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.True(t, body.closed, "a failed body read must still close the response body")
}
