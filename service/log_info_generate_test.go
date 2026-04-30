package service

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGenerateTextOtherInfoOmitsInvalidFirstResponseLatency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	startTime := time.Unix(1_700_000_000, 0)
	relayInfo := &relaycommon.RelayInfo{
		StartTime:   startTime,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}

	other := GenerateTextOtherInfo(ctx, relayInfo, 1, 1, 1, 0, 0, -1, -1)
	_, exists := other["frt"]
	require.False(t, exists)
}

func TestGenerateTextOtherInfoIncludesValidFirstResponseLatency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	startTime := time.Unix(1_700_000_000, 0)
	relayInfo := &relaycommon.RelayInfo{
		StartTime:         startTime,
		FirstResponseTime: startTime.Add(1500 * time.Millisecond),
		ChannelMeta:       &relaycommon.ChannelMeta{},
	}

	other := GenerateTextOtherInfo(ctx, relayInfo, 1, 1, 1, 0, 0, -1, -1)
	require.Equal(t, 1500.0, other["frt"])
}

func TestAppendStreamStatusCategorizesClientGoneAsCanceled(t *testing.T) {
	ss := relaycommon.NewStreamStatus()
	ss.SetEndReason(relaycommon.StreamEndReasonClientGone, context.Canceled)

	other := make(map[string]interface{})
	appendStreamStatus(&relaycommon.RelayInfo{
		IsStream:     true,
		StreamStatus: ss,
	}, other)

	streamInfo, ok := other["stream_status"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "canceled", streamInfo["status"])
	require.Equal(t, "client_gone", streamInfo["end_reason"])
	require.Equal(t, context.Canceled.Error(), streamInfo["end_error"])
}

func TestAppendStreamStatusKeepsSoftErroredClientGoneAsError(t *testing.T) {
	ss := relaycommon.NewStreamStatus()
	ss.RecordError("upstream warning")
	ss.SetEndReason(relaycommon.StreamEndReasonClientGone, context.Canceled)

	other := make(map[string]interface{})
	appendStreamStatus(&relaycommon.RelayInfo{
		IsStream:     true,
		StreamStatus: ss,
	}, other)

	streamInfo, ok := other["stream_status"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "error", streamInfo["status"])
	require.Equal(t, "client_gone", streamInfo["end_reason"])
	require.Equal(t, 1, streamInfo["error_count"])
}

func TestAppendStreamStatusKeepsBenignClientGoneAsCanceled(t *testing.T) {
	ss := relaycommon.NewStreamStatus()
	ss.RecordError("request context done: context canceled")
	ss.SetEndReason(relaycommon.StreamEndReasonClientGone, context.Canceled)

	other := make(map[string]interface{})
	appendStreamStatus(&relaycommon.RelayInfo{
		IsStream:     true,
		StreamStatus: ss,
	}, other)

	streamInfo, ok := other["stream_status"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "canceled", streamInfo["status"])
	require.Equal(t, "client_gone", streamInfo["end_reason"])
	require.Equal(t, 1, streamInfo["error_count"])
}

func TestGenerateTextOtherInfoIncludesRequestHeaderPolicyAudit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	common.SetContextKey(ctx, constant.ContextKeyChannelHeaderPolicyAudit, RuntimeHeaderPolicyAudit{
		HeaderPolicyMode:        "merge",
		AppliedHeaderKeys:       []string{"User-Agent", "X-Test"},
		HeaderProfileID:         "codex-cli",
		HeaderProfileMode:       "fixed",
		HeaderProfileApplied:    true,
		UserAgentApplied:        true,
		SelectedUserAgent:       dto.BuiltinCodexCLIUserAgent,
		AppliedUserAgent:        dto.BuiltinCodexCLIUserAgent,
		UserAgentStrategyMode:   "round_robin",
		UserAgentStrategyScope:  "tag:new.xem8k5.top",
		OverrideStaticUserAgent: true,
	})

	other := GenerateTextOtherInfo(ctx, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}, 1, 1, 1, 0, 0, -1, -1)
	info, ok := other["request_header_policy"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "merge", info["mode"])
	require.Equal(t, "codex-cli", info["header_profile_id"])
	require.Equal(t, "fixed", info["header_profile_mode"])
	require.Equal(t, true, info["header_profile_applied"])
	require.Equal(t, "round_robin", info["ua_strategy_mode"])
	require.Equal(t, "tag:new.xem8k5.top", info["ua_strategy_scope"])
	require.Equal(t, dto.BuiltinCodexCLIUserAgent, info["selected_user_agent"])
	require.Equal(t, dto.BuiltinCodexCLIUserAgent, info["applied_user_agent"])
	require.Equal(t, true, info["override_static_user_agent"])
	require.Equal(t, true, info["user_agent_applied"])
	require.Equal(t, []string{"User-Agent", "X-Test"}, info["applied_header_keys"])
}

func TestGenerateTextOtherInfoKeepsSelectedAndAppliedUserAgentSeparate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	common.SetContextKey(ctx, constant.ContextKeyChannelHeaderPolicyAudit, RuntimeHeaderPolicyAudit{
		HeaderPolicyMode:  "merge",
		SelectedUserAgent: "selected-ua",
		AppliedUserAgent:  "applied-ua",
		UserAgentApplied:  true,
	})

	other := GenerateTextOtherInfo(ctx, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}, 1, 1, 1, 0, 0, -1, -1)
	info, ok := other["request_header_policy"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "selected-ua", info["selected_user_agent"])
	require.Equal(t, "applied-ua", info["applied_user_agent"])
}
