package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
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

func TestCoverPlusActionToNormalActionRejectsMalformedCustomIDWithoutPanic(t *testing.T) {
	tests := []*dto.MidjourneyRequest{
		nil,
		{CustomId: "MJ"},
		{CustomId: "MJ::JOB"},
		{CustomId: "MJ::JOB::upsample"},
		{CustomId: "MJ::JOB::variation"},
		{CustomId: "MJ::JOB::unknown_action::1"},
	}

	for _, request := range tests {
		t.Run(fmt.Sprintf("%v", request), func(t *testing.T) {
			require.NotNil(t, CoverPlusActionToNormalAction(request))
		})
	}
}

func TestCoverPlusActionToNormalActionSupportsNonJobIndexPosition(t *testing.T) {
	request := &dto.MidjourneyRequest{CustomId: "MJ::upsample::2::task-id"}

	require.Nil(t, CoverPlusActionToNormalAction(request))
	require.Equal(t, constant.MjActionUpscale, request.Action)
	require.Equal(t, 2, request.Index)
}

func TestConvertSimpleChangeParamsRejectsEmptyAndMalformedActionWithoutPanic(t *testing.T) {
	for _, content := range []string{"", "task ", "task   ", "task x", "task u", "task u12", "task v0", "task v5"} {
		t.Run(fmt.Sprintf("%q", content), func(t *testing.T) {
			require.Nil(t, ConvertSimpleChangeParams(content))
		})
	}
}

func TestConvertSimpleChangeParamsAcceptsValidWhitespaceSeparatedAction(t *testing.T) {
	request := ConvertSimpleChangeParams(" task-id   v3 ")

	require.NotNil(t, request)
	require.Equal(t, "task-id", request.TaskId)
	require.Equal(t, "VARIATION", request.Action)
	require.Equal(t, 3, request.Index)
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

func TestApplyMidjourneyRuntimeHeadersAdvancesHeaderProfileRoundRobin(t *testing.T) {
	setupChannelHeaderRuntimeTestDB(t, &model.TagRequestHeaderPolicy{}, &model.RequestHeaderStrategyState{})
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/mj/submit", nil)
	common.SetContextKey(ctx, constant.ContextKeyChannelId, 31)
	common.SetContextKey(ctx, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
		HeaderProfileStrategy: &dto.HeaderProfileStrategy{
			Enabled:            true,
			Mode:               dto.HeaderProfileModeRoundRobin,
			SelectedProfileIDs: []string{"codex-cli", "claude-code"},
		},
	})

	first, err := http.NewRequest(http.MethodPost, "https://origin.example.com/mj/submit", nil)
	require.NoError(t, err)
	require.NoError(t, applyMidjourneyRuntimeHeaders(ctx, first, "sk-test"))
	require.Equal(t, dto.BuiltinCodexCLIUserAgent, first.Header.Get("User-Agent"))

	second, err := http.NewRequest(http.MethodPost, "https://origin.example.com/mj/submit", nil)
	require.NoError(t, err)
	require.NoError(t, applyMidjourneyRuntimeHeaders(ctx, second, "sk-test"))
	require.Equal(t, dto.BuiltinClaudeCodeUserAgent, second.Header.Get("User-Agent"))
}
