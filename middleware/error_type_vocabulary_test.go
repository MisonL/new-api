package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Middleware rejections are this gateway's own errors, so their client-facing
// type comes from the OpenAI vocabulary. Every one of them used to carry the
// internal marker "new_api_error", which appears nowhere in the OpenAI API.
func TestAbortWithOpenAiMessageUsesStandardType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name       string
		statusCode int
		code       types.ErrorCode
		want       string
	}{
		{name: "bad request", statusCode: http.StatusBadRequest, code: types.ErrorCodeInvalidRequest, want: "invalid_request_error"},
		{name: "unauthorized", statusCode: http.StatusUnauthorized, code: types.ErrorCodeChannelInvalidKey, want: "authentication_error"},
		{name: "forbidden", statusCode: http.StatusForbidden, code: types.ErrorCodeAccessDenied, want: "permission_error"},
		{name: "rate limited", statusCode: http.StatusTooManyRequests, code: types.ErrorCodeChannelConcurrencyExceeded, want: "rate_limit_error"},
		{name: "unavailable", statusCode: http.StatusServiceUnavailable, code: types.ErrorCodeGetChannelFailed, want: "server_error"},
		{name: "no code supplied", statusCode: http.StatusBadRequest, want: "invalid_request_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

			var code []types.ErrorCode
			if tc.code != "" {
				code = append(code, tc.code)
			}
			abortWithOpenAiMessage(ctx, tc.statusCode, "rejected", code...)

			require.True(t, ctx.IsAborted())
			require.Equal(t, tc.statusCode, recorder.Code)
			require.Contains(t, recorder.Body.String(), `"type":"`+tc.want+`"`)
			require.NotContains(t, recorder.Body.String(), `"new_api_error"`)
		})
	}
}

// The 429 helper must keep its Retry-After hint while carrying a standard type.
func TestAbortRateLimitedUsesStandardType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/status", nil)

	abortRateLimited(ctx, 60)

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Equal(t, "60", recorder.Header().Get("Retry-After"))
	require.Contains(t, recorder.Body.String(), `"type":"rate_limit_error"`)
	require.NotContains(t, recorder.Body.String(), `"new_api_error"`)
}
