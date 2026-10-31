package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 内容策略/安全拒绝由请求内容本身决定，换渠道必然同样被拒，且会把每个被尝试
// 的渠道各记一次健康冷却。shouldRetry 必须判为不可重试。此处的形态覆盖 OpenAI
// 与 Anthropic 两家最常见的 code/type/message，以及非流式与带内错误帧两条路径。
func TestShouldRetryRejectsContentPolicyRefusal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name string
		err  *types.NewAPIError
	}{
		{
			name: "openai content_policy_violation",
			err: types.WithOpenAIError(types.OpenAIError{
				Message: "Your request was rejected",
				Type:    "invalid_request_error",
				Code:    "content_policy_violation",
			}, http.StatusBadRequest),
		},
		{
			name: "openai content_filter code",
			err: types.WithOpenAIError(types.OpenAIError{
				Message: "Your request was rejected by the safety system",
				Type:    "invalid_request_error",
				Code:    "content_filter",
			}, http.StatusBadRequest),
		},
		{
			name: "anthropic content policy type",
			err: types.WithOpenAIError(types.OpenAIError{
				Message: "Your request violates our usage policy",
				Type:    "content_policy_violation",
				Code:    "content_policy_violation",
			}, http.StatusBadRequest),
		},
		{
			name: "message-only safety refusal",
			err: types.WithOpenAIError(types.OpenAIError{
				Message: "prompt is blocked by the safety system",
				Type:    "invalid_request_error",
				Code:    "invalid_request",
			}, http.StatusBadRequest),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			require.False(t, shouldRetry(c, tc.err, 1),
				"内容策略/安全拒绝必须保持终态，不能换渠道重放")
		})
	}
}

// 非内容策略的 400 与 402 仍应可重试：前者可能是渠道不接受该请求形态，后者是
// 上游账号配额耗尽。
func TestShouldRetryNonContentPolicyErrorsStillRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("plain 400 invalid_request", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		err := types.WithOpenAIError(types.OpenAIError{
			Message: "invalid_request",
			Type:    "invalid_request_error",
			Code:    "invalid_request",
		}, http.StatusBadRequest)
		require.True(t, shouldRetry(c, err, 1),
			"普通 400 invalid_request 必须允许换渠道重试")
	})

	t.Run("402 insufficient_quota frame", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		err := types.WithOpenAIError(types.OpenAIError{
			Message: "insufficient_quota",
			Type:    "insufficient_quota",
			Code:    "insufficient_quota",
		}, http.StatusPaymentRequired)
		require.True(t, shouldRetry(c, err, 1),
			"带内 402 配额错误必须允许换渠道重试")
	})
}
