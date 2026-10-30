package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 上游 400 invalid_request 可能是该渠道不支持某个参数或不接受该请求形态，
// 换一个渠道仍可成功。默认重试状态码必须包含 400，否则这类请求会被钉死在
// 单个渠道上（生产日志里渠道 226 的 Codex Desktop 请求即此形态）。
//
// 这里锁定的是整条重试判定链：400 不得被 always-skip、skip-retry 标记、
// 渠道健康排除或确定性模型缺失等前置门限拦下，最终由状态码策略放行。
func TestShouldRetryUpstreamBadRequestByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	err := types.WithOpenAIError(types.OpenAIError{
		Message: "invalid_request",
		Type:    "invalid_request_error",
		Code:    "invalid_request",
	}, http.StatusBadRequest)

	require.True(t, shouldRetry(c, err, 1),
		"上游 400 invalid_request 默认必须允许换渠道重试")
}
