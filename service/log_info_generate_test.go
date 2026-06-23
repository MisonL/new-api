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
		AppliedHeaders:          []AppliedHeaderAuditEntry{{Key: "User-Agent", Value: dto.BuiltinCodexCLIUserAgent}, {Key: "X-Test", Value: "test-value"}},
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
	require.Equal(t, []AppliedHeaderAuditEntry{{Key: "User-Agent", Value: dto.BuiltinCodexCLIUserAgent}, {Key: "X-Test", Value: AppliedHeaderAuditRedactedValue}}, info["applied_headers"])
}

func TestCollectRuntimeHeaderAuditEntriesRedactsNonVisibleHeaderValues(t *testing.T) {
	entries := collectRuntimeHeaderAuditEntries(map[string]any{
		"Api-Key":               "azure-secret",
		"Originator":            "codex-tui",
		"X-Codex-Turn-Metadata": `{"session_id":"session-123","workspaces":{"/repo":{"associated_remote_urls":{"origin":"https://example.com/repo.git"}}}}`,
		"X-Codex-Window-Id":     "window-123",
		"X-Test":                "test-value",
		"X-Upstream-Auth":       "Bearer copied-secret",
	})

	require.ElementsMatch(t, []AppliedHeaderAuditEntry{
		{Key: "Api-Key", Value: AppliedHeaderAuditRedactedValue},
		{Key: "Originator", Value: "codex-tui"},
		{Key: "X-Codex-Turn-Metadata", Value: AppliedHeaderAuditRedactedValue},
		{Key: "X-Codex-Window-Id", Value: "window-123"},
		{Key: "X-Test", Value: AppliedHeaderAuditRedactedValue},
		{Key: "X-Upstream-Auth", Value: AppliedHeaderAuditRedactedValue},
	}, entries)
}

func TestAppendRequestHeaderPolicyInfoSanitizesRawAuditValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	common.SetContextKey(ctx, constant.ContextKeyChannelHeaderPolicyAudit, RuntimeHeaderPolicyAudit{
		HeaderPolicyMode:  "merge",
		AppliedHeaderKeys: []string{"Originator", "X-Upstream-Auth"},
		AppliedHeaders: []AppliedHeaderAuditEntry{
			{Key: "Originator", Value: "codex-tui"},
			{Key: "X-Upstream-Auth", Value: "Bearer copied-secret"},
		},
	})

	other := map[string]interface{}{}
	AppendRequestHeaderPolicyInfo(ctx, other)

	info, ok := other["request_header_policy"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, []AppliedHeaderAuditEntry{
		{Key: "Originator", Value: "codex-tui"},
		{Key: "X-Upstream-Auth", Value: AppliedHeaderAuditRedactedValue},
	}, info["applied_headers"])
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

func TestGenerateTextOtherInfoIncludesResponsesProfileAndPreviousIDAction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	common.SetContextKey(ctx, constant.ContextKeyResponsesPreviousIDAction, "rejected_by_upstream_profile")

	other := GenerateTextOtherInfo(ctx, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileSub2APIHTTP,
			},
		},
	}, 1, 1, 1, 0, 0, -1, -1)

	require.Equal(t, "sub2api_http", other["responses_upstream_profile"])
	require.Equal(t, "rejected_by_upstream_profile", other["responses_previous_id_action"])
}

func TestGenerateTextOtherInfoIncludesMissingLocalSyntheticStateAction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	common.SetContextKey(ctx, constant.ContextKeyResponsesCompactVisibleOnlyFallbackAttempted, true)
	common.SetContextKey(ctx, constant.ContextKeyResponsesPreviousIDAction, "stale_local_synthetic_state_visible_only")
	common.SetContextKey(ctx, constant.ContextKeyResponsesCompactStateLookup, "miss")
	common.SetContextKey(ctx, constant.ContextKeyResponsesCompactStateScopeResult, "not_found")
	common.SetContextKey(ctx, constant.ContextKeyResponsesCompactMarkerKind, "synthetic_summary")
	common.SetContextKey(ctx, constant.ContextKeyResponsesCompactRouteDecision, "visible_only_fallback")
	common.SetContextKey(ctx, constant.ContextKeyResponsesCompactFallbackReason, "state_not_found_visible_input")

	other := GenerateTextOtherInfo(ctx, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialOpenAI,
			},
		},
	}, 1, 1, 1, 0, 0, -1, -1)

	require.Equal(t, "official_openai", other["responses_upstream_profile"])
	require.Equal(t, "stale_local_synthetic_state_visible_only", other["responses_previous_id_action"])
	require.Equal(t, true, other["responses_compact_visible_only_fallback"])
	require.Equal(t, "miss", other["responses_compact_state_lookup"])
	require.Equal(t, "not_found", other["responses_compact_state_scope_result"])
	require.Equal(t, "synthetic_summary", other["responses_compact_marker_kind"])
	require.Equal(t, "visible_only_fallback", other["responses_compact_route_decision"])
	require.Equal(t, "state_not_found_visible_input", other["responses_compact_fallback_reason"])
	require.NotContains(t, other, "responses_compact_state_restored")
	require.NotContains(t, other, "responses_compact_model_changed")
	require.NotContains(t, other, "responses_compact_state_hash")
	snapshot, ok := other["channel_capability_snapshot"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "explicit_settings", snapshot["source"])
	require.Equal(t, "official_openai", snapshot["profile"])
	require.Equal(t, "auto", snapshot["compact_mode_setting"])
	require.Equal(t, "native", snapshot["compact_mode_effective"])
	require.Equal(t, true, snapshot["supports_responses"])
	require.Equal(t, true, snapshot["supports_responses_compact"])
	require.Equal(t, true, snapshot["supports_chat"])
	require.Equal(t, true, snapshot["supports_rest_previous_response_id"])
	require.Equal(t, true, snapshot["supports_compaction_item_passthrough"])
	require.Equal(t, true, snapshot["supports_namespace_tools"])
}

func TestGenerateTextOtherInfoIncludesNativeOpaqueRecordedState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/responses/compact", nil)
	common.SetContextKey(ctx, constant.ContextKeyResponsesCompactStateLookup, "recorded")
	common.SetContextKey(ctx, constant.ContextKeyResponsesCompactStateScopeResult, "strict")
	common.SetContextKey(ctx, constant.ContextKeyResponsesCompactMarkerKind, "native_opaque")
	common.SetContextKey(ctx, constant.ContextKeyResponsesCompactRouteDecision, "native_opaque_recorded")
	common.SetContextKey(ctx, constant.ContextKeyResponsesCompactStateHash, "opaque-hash-for-log")
	common.SetContextKey(ctx, constant.ContextKeyResponsesCompactionOutput, true)

	other := GenerateTextOtherInfo(ctx, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialOpenAI,
			},
		},
	}, 1, 1, 1, 0, 0, -1, -1)

	require.Equal(t, "official_openai", other["responses_upstream_profile"])
	require.Equal(t, "recorded", other["responses_compact_state_lookup"])
	require.Equal(t, "strict", other["responses_compact_state_scope_result"])
	require.Equal(t, "native_opaque", other["responses_compact_marker_kind"])
	require.Equal(t, "native_opaque_recorded", other["responses_compact_route_decision"])
	require.Equal(t, "opaque-hash-for-log", other["responses_compact_state_hash"])
	require.Equal(t, "remote_v2", other["responses_compact_mode"])
	require.Equal(t, "/v1/responses/compact", other["responses_compact_upstream_path"])
}

func TestGenerateTextOtherInfoIncludesResponsesCompactChannelSkip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	common.SetContextKey(ctx, constant.ContextKeyResponsesCompactChannelSkip, "channel_skipped_unsupported_compaction")

	other := GenerateTextOtherInfo(ctx, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileSub2APIHTTP,
			},
		},
	}, 1, 1, 1, 0, 0, -1, -1)

	require.Equal(t, "channel_skipped_unsupported_compaction", other["responses_compact_channel_skip"])
}

func TestResponsesChannelCapabilitySnapshotTreatsSub2APIHTTPAsNoRESTPreviousID(t *testing.T) {
	snapshot := ResponsesChannelCapabilitySnapshot(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}, dto.ChannelOtherSettings{
		ResponsesCompactMode:     dto.ResponsesCompactModeAuto,
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileSub2APIHTTP,
	})

	require.Equal(t, "sub2api_http", snapshot["profile"])
	require.Equal(t, "auto", snapshot["compact_mode_setting"])
	require.Equal(t, "native", snapshot["compact_mode_effective"])
	require.Equal(t, true, snapshot["supports_responses"])
	require.Equal(t, true, snapshot["supports_responses_compact"])
	require.Equal(t, false, snapshot["supports_rest_previous_response_id"])
	require.Equal(t, false, snapshot["supports_compaction_item_passthrough"])
	require.Equal(t, false, snapshot["supports_namespace_tools"])
}

func TestResponsesChannelCapabilitySnapshotMarksSafeDefaultSource(t *testing.T) {
	snapshot := ResponsesChannelCapabilitySnapshot(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}, dto.ChannelOtherSettings{})

	require.Equal(t, "safe_default", snapshot["source"])
	require.Empty(t, snapshot["profile"])
	require.Equal(t, true, snapshot["supports_compaction_item_passthrough"])
}

func TestResponsesChannelCapabilitySnapshotKeepsObservationOutOfPublicSnapshot(t *testing.T) {
	observedAt := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC).Unix()
	snapshot := ResponsesChannelCapabilitySnapshot(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}, dto.ChannelOtherSettings{
		ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
			Observed: &dto.ResponsesCapabilityObservation{
				ObservedAt: observedAt,
				StatusCode: 413,
				ErrorCode:  string("upstream_request_too_large"),
				Reason:     "payload too large",
			},
		},
	})

	require.Equal(t, "observed_calls", snapshot["source"])
	require.NotContains(t, snapshot, "observed")
	require.NotContains(t, snapshot, "probe")
}

func TestGenerateTextOtherInfoPutsCapabilityObservationInAdminInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/responses", nil)

	observedAt := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC).Unix()
	other := GenerateTextOtherInfo(ctx, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
					Observed: &dto.ResponsesCapabilityObservation{
						ObservedAt: observedAt,
						StatusCode: 413,
						ErrorCode:  string("upstream_request_too_large"),
						Reason:     "payload too large",
					},
				},
			},
		},
	}, 1, 1, 1, 0, 0, -1, -1)

	snapshot, ok := other["channel_capability_snapshot"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "observed_calls", snapshot["source"])
	require.NotContains(t, snapshot, "observed")
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	observed, ok := adminInfo["responses_channel_capability_observed"].(*dto.ResponsesCapabilityObservation)
	require.True(t, ok)
	require.Equal(t, 413, observed.StatusCode)
	require.Equal(t, observedAt, observed.ObservedAt)
}

func TestResponsesChannelCapabilitySnapshotTreatsProxyProfileAsSyntheticOnly(t *testing.T) {
	snapshot := ResponsesChannelCapabilitySnapshot(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}, dto.ChannelOtherSettings{
		ResponsesCompactMode:     dto.ResponsesCompactModeAuto,
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileGenericProxy,
	})

	require.Equal(t, "generic_proxy", snapshot["profile"])
	require.Equal(t, "auto", snapshot["compact_mode_setting"])
	require.Equal(t, "synthetic_summary", snapshot["compact_mode_effective"])
	require.Equal(t, true, snapshot["supports_responses"])
	require.Equal(t, false, snapshot["supports_responses_compact"])
	require.Equal(t, true, snapshot["supports_chat"])
	require.Equal(t, false, snapshot["supports_rest_previous_response_id"])
	require.Equal(t, false, snapshot["supports_compaction_item_passthrough"])
	require.Equal(t, false, snapshot["supports_namespace_tools"])
}

func TestResponsesChannelCapabilitySnapshotProfileMatrix(t *testing.T) {
	testCases := []struct {
		name                                  string
		profile                               dto.ResponsesUpstreamProfile
		wantCompactMode                       string
		wantSupportsResponsesCompact          bool
		wantSupportsRESTPreviousResponseID    bool
		wantSupportsCompactionItemPassthrough bool
		wantSupportsNamespaceTools            bool
	}{
		{
			name:                                  "official newapi",
			profile:                               dto.ResponsesUpstreamProfileOfficialNewAPI,
			wantCompactMode:                       "native",
			wantSupportsResponsesCompact:          true,
			wantSupportsRESTPreviousResponseID:    true,
			wantSupportsCompactionItemPassthrough: true,
			wantSupportsNamespaceTools:            true,
		},
		{
			name:                                  "sub2api http",
			profile:                               dto.ResponsesUpstreamProfileSub2APIHTTP,
			wantCompactMode:                       "native",
			wantSupportsResponsesCompact:          true,
			wantSupportsRESTPreviousResponseID:    false,
			wantSupportsCompactionItemPassthrough: false,
			wantSupportsNamespaceTools:            false,
		},
		{
			name:                                  "sub2api websocket v2",
			profile:                               dto.ResponsesUpstreamProfileSub2APIWSV2,
			wantCompactMode:                       "native",
			wantSupportsResponsesCompact:          true,
			wantSupportsRESTPreviousResponseID:    true,
			wantSupportsCompactionItemPassthrough: true,
			wantSupportsNamespaceTools:            true,
		},
		{
			name:                                  "generic openai",
			profile:                               dto.ResponsesUpstreamProfileGenericOpenAI,
			wantCompactMode:                       "native",
			wantSupportsResponsesCompact:          true,
			wantSupportsRESTPreviousResponseID:    false,
			wantSupportsCompactionItemPassthrough: false,
			wantSupportsNamespaceTools:            false,
		},
		{
			name:                                  "generic proxy",
			profile:                               dto.ResponsesUpstreamProfileGenericProxy,
			wantCompactMode:                       "synthetic_summary",
			wantSupportsResponsesCompact:          false,
			wantSupportsRESTPreviousResponseID:    false,
			wantSupportsCompactionItemPassthrough: false,
			wantSupportsNamespaceTools:            false,
		},
		{
			name:                                  "chat only proxy",
			profile:                               dto.ResponsesUpstreamProfileChatOnlyProxy,
			wantCompactMode:                       "synthetic_summary",
			wantSupportsResponsesCompact:          false,
			wantSupportsRESTPreviousResponseID:    false,
			wantSupportsCompactionItemPassthrough: false,
			wantSupportsNamespaceTools:            false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := ResponsesChannelCapabilitySnapshot(&relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType: constant.ChannelTypeOpenAI,
				},
			}, dto.ChannelOtherSettings{
				ResponsesCompactMode:     dto.ResponsesCompactModeAuto,
				ResponsesUpstreamProfile: tc.profile,
			})

			require.Equal(t, string(tc.profile), snapshot["profile"])
			require.Equal(t, tc.wantCompactMode, snapshot["compact_mode_effective"])
			require.Equal(t, true, snapshot["supports_responses"])
			require.Equal(t, tc.wantSupportsResponsesCompact, snapshot["supports_responses_compact"])
			require.Equal(t, tc.wantSupportsRESTPreviousResponseID, snapshot["supports_rest_previous_response_id"])
			require.Equal(t, tc.wantSupportsCompactionItemPassthrough, snapshot["supports_compaction_item_passthrough"])
			require.Equal(t, tc.wantSupportsNamespaceTools, snapshot["supports_namespace_tools"])
		})
	}
}

func TestGenerateTextOtherInfoMarksCodexLocalCompactionFromHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	ctx.Request.Header.Set("x-codex-turn-metadata", `{
		"request_kind":"compaction",
		"compaction":{
			"trigger":"auto",
			"reason":"context_limit",
			"implementation":"responses",
			"phase":"background",
			"strategy":"local"
		}
	}`)

	other := GenerateTextOtherInfo(ctx, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, 1, 1, 1, 0, 0, -1, -1)

	require.Equal(t, "codex", other["responses_compact_client"])
	require.Equal(t, "responses", other["responses_compact_client_implementation"])
	require.Equal(t, "codex_local", other["responses_compact_mode"])
	require.Equal(t, "/v1/responses", other["responses_compact_upstream_path"])
	require.Equal(t, "auto", other["responses_compact_trigger"])
	require.Equal(t, "context_limit", other["responses_compact_reason"])
	require.Equal(t, "background", other["responses_compact_phase"])
	require.Equal(t, "local", other["responses_compact_strategy"])
}

func TestGenerateTextOtherInfoMarksCodexRemoteV2CompactionFromHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	ctx.Request.Header.Set("x-codex-turn-metadata", `{
		"request_kind":"compaction",
		"compaction":{
			"trigger":"manual",
			"implementation":"responses_compaction_v2"
		}
	}`)

	other := GenerateTextOtherInfo(ctx, &relaycommon.RelayInfo{
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.5",
			Input: common.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"compact"}]},
				{"type":"compaction_trigger"}
			]`),
		},
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, 1, 1, 1, 0, 0, -1, -1)

	require.Equal(t, "codex", other["responses_compact_client"])
	require.Equal(t, "responses_compaction_v2", other["responses_compact_client_implementation"])
	require.Equal(t, "remote_v2", other["responses_compact_mode"])
	require.Equal(t, "/v1/responses", other["responses_compact_upstream_path"])
	require.Equal(t, "manual", other["responses_compact_trigger"])
}

func TestGenerateTextOtherInfoMarksRemoteV2CompactionFromTriggerWithoutHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/responses", nil)

	other := GenerateTextOtherInfo(ctx, &relaycommon.RelayInfo{
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.5",
			Input: common.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"compact"}]},
				{"type":"compaction_trigger"}
			]`),
		},
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, 1, 1, 1, 0, 0, -1, -1)

	require.Equal(t, "remote_v2", other["responses_compact_mode"])
	require.Equal(t, "/v1/responses", other["responses_compact_upstream_path"])
}
