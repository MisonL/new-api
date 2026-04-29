package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestApplyMidjourneyRuntimeHeadersSetsRequestHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/mj/submit", nil)
	common.SetContextKey(ctx, constant.ContextKeyChannelHeaderOverride, map[string]any{
		"Host":   "cdn.example.com",
		"X-Test": "ok",
	})
	req, err := http.NewRequest(http.MethodPost, "https://origin.example.com/mj/submit", nil)
	require.NoError(t, err)

	require.NoError(t, applyMidjourneyRuntimeHeaders(ctx, req, "sk-test"))
	require.Equal(t, "cdn.example.com", req.Host)
	require.Equal(t, "cdn.example.com", req.Header.Get("Host"))
	require.Equal(t, "ok", req.Header.Get("X-Test"))
}

func TestApplyMidjourneyRuntimeHeadersNoopsWithoutOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/mj/submit", nil)
	req, err := http.NewRequest(http.MethodPost, "https://origin.example.com/mj/submit", nil)
	require.NoError(t, err)
	originalHost := req.Host

	require.NoError(t, applyMidjourneyRuntimeHeaders(ctx, req, "sk-test"))
	require.Equal(t, originalHost, req.Host)
	require.Empty(t, req.Header.Get("X-Test"))
}

func TestApplyMidjourneyRuntimeHeadersStringifiesValuesAndApiKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/mj/submit", nil)
	common.SetContextKey(ctx, constant.ContextKeyChannelHeaderOverride, map[string]any{
		"X-Count": 3,
		"X-Flag":  true,
		"X-Key":   "Bearer {api_key}",
	})
	req, err := http.NewRequest(http.MethodPost, "https://origin.example.com/mj/submit", nil)
	require.NoError(t, err)

	require.NoError(t, applyMidjourneyRuntimeHeaders(ctx, req, "sk-test"))
	require.Equal(t, "3", req.Header.Get("X-Count"))
	require.Equal(t, "true", req.Header.Get("X-Flag"))
	require.Equal(t, "Bearer sk-test", req.Header.Get("X-Key"))
}
