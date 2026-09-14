package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func TestProcessChannelErrorRecordsRedactedResponsesRequestShape(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))

	previousErrorLogEnabled := constant.ErrorLogEnabled
	constant.ErrorLogEnabled = true
	t.Cleanup(func() {
		constant.ErrorLogEnabled = previousErrorLogEnabled
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Set("id", 1)
	ctx.Set("token_name", "test-token")
	ctx.Set("original_model", "gpt-5.6-sol")
	ctx.Set("token_id", 5)
	ctx.Set("group", "default")
	ctx.Set("channel_id", 117)
	ctx.Set("channel_name", "test-channel")
	ctx.Set("channel_type", constant.ChannelTypeOpenAI)
	ctx.Set("username", "test-user")
	common.SetContextKey(ctx, constant.ContextKeyRequestStartTime, time.Now())
	common.SetContextKey(ctx, constant.ContextKeyRequestBodySize, int64(512))
	common.SetContextKey(ctx, constant.ContextKeyUserSetting, dto.UserSetting{RecordIpLog: false})

	relayInfo := &relaycommon.RelayInfo{
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.6-sol",
			Input: common.RawMessage(`[
				{"type":"additional_tools","tools":[{"type":"function","name":"secret_tool","parameters":{"type":"object"}}]},
				{"type":"message","role":"user","content":"secret prompt"}
			]`),
			ToolChoice: common.RawMessage(`{"type":"function","name":"secret_tool"}`),
		},
	}
	processChannelError(
		ctx,
		relayInfo,
		types.ChannelError{ChannelId: 117, ChannelType: constant.ChannelTypeOpenAI},
		types.NewErrorWithStatusCode(errors.New("upstream failed"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway),
	)

	var log model.Log
	require.NoError(t, db.Order("id DESC").First(&log).Error)
	var other map[string]interface{}
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	shape, ok := other["responses_request_shape"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, float64(512), shape["body_bytes"])
	require.Equal(t, true, shape["has_function_tools"])
	require.Equal(t, true, shape["has_additional_tools"])
	require.NotContains(t, log.Other, "secret prompt")
	require.NotContains(t, log.Other, "secret_tool")
}

func TestProcessChannelErrorRecordsStructuredResponsesErrorFields(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	previousErrorLogEnabled := constant.ErrorLogEnabled
	constant.ErrorLogEnabled = true
	t.Cleanup(func() { constant.ErrorLogEnabled = previousErrorLogEnabled })

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Set("id", 1)
	ctx.Set("token_name", "test-token")
	ctx.Set("original_model", "gpt-5.6-sol")
	ctx.Set("token_id", 5)
	ctx.Set("group", "default")
	ctx.Set("channel_id", 150)
	ctx.Set("channel_name", "mdkj.lol")
	ctx.Set("channel_type", constant.ChannelTypeOpenAI)
	ctx.Set("username", "test-user")
	common.SetContextKey(ctx, constant.ContextKeyRequestStartTime, time.Now())
	common.SetContextKey(ctx, constant.ContextKeyUserSetting, dto.UserSetting{RecordIpLog: false})
	common.SetContextKey(ctx, constant.ContextKeyResponsesErrorEvent, "response.failed")

	err := types.WithOpenAIError(types.OpenAIError{
		Type:    "invalid_request_error",
		Message: "invalid request",
		Param:   "input[1].namespace",
		Code:    "unknown_parameter",
	}, http.StatusBadRequest)
	processChannelError(ctx, &relaycommon.RelayInfo{}, types.ChannelError{ChannelId: 150, ChannelType: constant.ChannelTypeOpenAI}, err)

	var log model.Log
	require.NoError(t, db.Order("id DESC").First(&log).Error)
	var other map[string]interface{}
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	require.Equal(t, "upstream_4xx", other["error_category"])
	require.Equal(t, "invalid_request_error", other["upstream_error_type"])
	require.Equal(t, "unknown_parameter", other["upstream_error_code"])
	require.Equal(t, "input[1].namespace", other["error_param"])
	require.Equal(t, "response.failed", other["error_event"])
	require.NotContains(t, log.Other, "invalid request")
}

func TestSanitizeRelayErrorFieldRedactsCredentialsAndURLs(t *testing.T) {
	input := `Authorization: Bearer bearer-secret-123; key=sk-live-secret123; https://api.example.com/v1/responses?token=query-secret`
	got := sanitizeRelayErrorField(input)

	for _, secret := range []string{
		"bearer-secret-123",
		"sk-live-secret123",
		"query-secret",
		"api.example.com",
	} {
		require.NotContains(t, got, secret)
	}
	require.Contains(t, got, "Authorization: Bearer ***")
}

func TestSanitizeRelayErrorFieldRedactsHeaderAPIKeyAndUnderscoreProviderKeys(t *testing.T) {
	input := `Api-Key: api-header-secret; api_key=api-query-secret; sk_header-secret; rk_header-secret; sess_header-secret`
	got := sanitizeRelayErrorField(input)

	for _, secret := range []string{
		"api-header-secret",
		"api-query-secret",
		"sk_header-secret",
		"rk_header-secret",
		"sess_header-secret",
	} {
		require.NotContains(t, got, secret)
	}
	require.Contains(t, got, "Api-Key: ***")
	require.Contains(t, got, "api_key=***")
}

func TestSanitizeRelayErrorFieldAlwaysRedactsUnschemedQueryCredentials(t *testing.T) {
	input := "api.example.com?token=query-secret; 192.168.1.1?api_key=ip-secret"
	got := sanitizeRelayErrorField(input)

	for _, secret := range []string{"api.example.com", "query-secret", "192.168.1.1", "ip-secret"} {
		require.NotContains(t, got, secret)
	}
}

func TestNormalizeRelayErrorEventUsesWhitelist(t *testing.T) {
	for _, event := range []string{"json", "sse", "response.error", "error", "response.failed", " RESPONSE.FAILED "} {
		t.Run(event, func(t *testing.T) {
			require.NotEqual(t, "unknown", normalizeRelayErrorEvent(event))
		})
	}

	for _, event := range []string{"", "response.created", "https://api.example.com/?token=secret", "attacker-event"} {
		t.Run("unknown/"+event, func(t *testing.T) {
			require.Equal(t, "unknown", normalizeRelayErrorEvent(event))
		})
	}
}

func TestProcessChannelErrorHandlesNilRelayInfo(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))

	previousErrorLogEnabled := constant.ErrorLogEnabled
	constant.ErrorLogEnabled = true
	t.Cleanup(func() { constant.ErrorLogEnabled = previousErrorLogEnabled })

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Set("id", 1)
	ctx.Set("token_name", "test-token")
	ctx.Set("original_model", "gpt-5.6-sol")
	ctx.Set("token_id", 5)
	ctx.Set("group", "default")
	ctx.Set("channel_id", 117)
	ctx.Set("channel_name", "test-channel")
	ctx.Set("channel_type", constant.ChannelTypeOpenAI)
	ctx.Set("username", "test-user")
	common.SetContextKey(ctx, constant.ContextKeyRequestStartTime, time.Now())
	common.SetContextKey(ctx, constant.ContextKeyUserSetting, dto.UserSetting{RecordIpLog: false})

	require.NotPanics(t, func() {
		processChannelError(
			ctx,
			nil,
			types.ChannelError{ChannelId: 117, ChannelType: constant.ChannelTypeOpenAI},
			types.NewErrorWithStatusCode(errors.New("rate limited"), types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests),
		)
	})

	var log model.Log
	require.NoError(t, db.Order("id DESC").First(&log).Error)
	require.Equal(t, 117, log.ChannelId)
}

func TestClassifyRelayErrorCategoryTreatsUpstreamResponseFailuresAsUpstream(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		code       types.ErrorCode
		statusCode int
		want       string
	}{
		{name: "bad response 502", code: types.ErrorCodeBadResponse, statusCode: http.StatusBadGateway, want: "upstream_5xx"},
		{name: "bad response body 503", code: types.ErrorCodeBadResponseBody, statusCode: http.StatusServiceUnavailable, want: "upstream_5xx"},
		{name: "cloudflare timeout", code: types.ErrorCodeBadResponseBody, statusCode: relayCloudflareTimeoutStatusCode, want: "upstream_timeout"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			err := types.NewErrorWithStatusCode(errors.New("upstream response failure"), testCase.code, testCase.statusCode)
			require.Equal(t, testCase.want, classifyRelayErrorCategory(nil, nil, err))
		})
	}
}

func TestClassifyRelayErrorCategoryTreatsInterruptedTransportAsUpstream(t *testing.T) {
	err := types.NewErrorWithStatusCode(
		errors.New("upstream stream interrupted"),
		types.ErrorCodeUpstreamTransportInterrupted,
		http.StatusBadGateway,
	)

	require.Equal(t, "upstream_transport", classifyRelayErrorCategory(nil, nil, err))
}

func TestProcessChannelErrorClassifiesLocalCapabilityAndStreamOutcome(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	previousErrorLogEnabled := constant.ErrorLogEnabled
	constant.ErrorLogEnabled = true
	t.Cleanup(func() { constant.ErrorLogEnabled = previousErrorLogEnabled })

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Set("id", 1)
	ctx.Set("token_name", "test-token")
	ctx.Set("original_model", "gpt-5.6-sol")
	ctx.Set("token_id", 5)
	ctx.Set("group", "default")
	ctx.Set("channel_id", 150)
	ctx.Set("channel_name", "mdkj.lol")
	ctx.Set("channel_type", constant.ChannelTypeOpenAI)
	ctx.Set("username", "test-user")
	common.SetContextKey(ctx, constant.ContextKeyRequestStartTime, time.Now())
	common.SetContextKey(ctx, constant.ContextKeyUserSetting, dto.UserSetting{RecordIpLog: false})

	streamStatus := relaycommon.NewStreamStatus()
	streamStatus.SetEndReason(relaycommon.StreamEndReasonUpstreamInterrupted, errors.New("unexpected EOF"))
	relayInfo := &relaycommon.RelayInfo{IsStream: true, ReceivedResponseCount: 2, StreamStatus: streamStatus}
	processChannelError(ctx, relayInfo, types.ChannelError{ChannelId: 150, ChannelType: constant.ChannelTypeOpenAI},
		types.NewErrorWithStatusCode(errors.New("namespace tools unsupported"), types.ErrorCodeChannelModelMappedError, http.StatusServiceUnavailable))

	var log model.Log
	require.NoError(t, db.Order("id DESC").First(&log).Error)
	var other map[string]interface{}
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	require.Equal(t, "local_capability", other["error_category"])
	streamInfo, ok := other["stream_status"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "partial", streamInfo["status"])
	require.Equal(t, "upstream_transport_interrupted", streamInfo["end_reason"])
}

func TestProtocolCompatibilitySkipsDoNotAutoBanChannels(t *testing.T) {
	channel := &model.Channel{Id: 117}
	tests := []struct {
		name string
		err  *types.NewAPIError
	}{
		{name: "namespace tools", err: unsupportedResponsesNamespaceToolsChannelError(channel)},
		{name: "responses to chat conversion", err: unsupportedResponsesToChatConversionError(channel, errors.New("unsupported input"))},
		{name: "encrypted replay", err: relaycommon.NewResponsesEncryptedReasoningReplayUnsupportedError(channel.Id)},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			require.True(t, isProtocolIncompatibleSkip(testCase.err))
		})
	}
}

func TestProcessChannelErrorRecordsChannelHealthRoutingAudit(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))

	previousErrorLogEnabled := constant.ErrorLogEnabled
	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	settings := operation_setting.GetGeneralSetting()
	previousEnabled := settings.ChannelHealthEnabled
	constant.ErrorLogEnabled = true
	common.RedisEnabled = false
	common.RDB = nil
	settings.ChannelHealthEnabled = true
	t.Cleanup(func() {
		constant.ErrorLogEnabled = previousErrorLogEnabled
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
		settings.ChannelHealthEnabled = previousEnabled
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Set("id", 1)
	ctx.Set("token_name", "test-token")
	ctx.Set("original_model", "gpt-5.6-sol")
	ctx.Set("token_id", 5)
	ctx.Set("group", "default")
	ctx.Set("channel_id", 117)
	ctx.Set("channel_name", "test-channel")
	ctx.Set("channel_type", constant.ChannelTypeOpenAI)
	ctx.Set("username", "test-user")
	common.SetContextKey(ctx, constant.ContextKeyRequestStartTime, time.Now())
	common.SetContextKey(ctx, constant.ContextKeyUserSetting, dto.UserSetting{RecordIpLog: false})

	decision := service.EvaluateChannelHealthSelection(ctx, "default", "gpt-5.6-sol", 117)
	require.True(t, decision.Allow)
	require.Equal(t, service.ChannelHealthSelectionUnavailable, decision.Action)

	processChannelError(
		ctx,
		&relaycommon.RelayInfo{},
		types.ChannelError{ChannelId: 117, ChannelType: constant.ChannelTypeOpenAI},
		types.NewErrorWithStatusCode(errors.New("upstream failed"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway),
	)

	var log model.Log
	require.NoError(t, db.Order("id DESC").First(&log).Error)
	var other map[string]interface{}
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	healthInfo, ok := adminInfo["channel_health"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, true, healthInfo["redis_unavailable"])
	events, ok := healthInfo["events"].([]interface{})
	require.True(t, ok)
	require.Len(t, events, 1)
	event, ok := events[0].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, string(service.ChannelHealthSelectionUnavailable), event["action"])
}

func TestRecordTerminalResponsesRoutingErrorLogPersistsRoutingSkip(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))

	previousErrorLogEnabled := constant.ErrorLogEnabled
	constant.ErrorLogEnabled = true
	t.Cleanup(func() {
		constant.ErrorLogEnabled = previousErrorLogEnabled
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Set("id", 1)
	ctx.Set("token_name", "test-token")
	ctx.Set("original_model", "gpt-5.6-sol")
	ctx.Set("token_id", 5)
	ctx.Set("group", "default")
	ctx.Set("channel_id", 150)
	ctx.Set("channel_name", "test-channel")
	ctx.Set("channel_type", constant.ChannelTypeOpenAI)
	ctx.Set("username", "test-user")
	common.SetContextKey(ctx, constant.ContextKeyRequestStartTime, time.Now())
	common.SetContextKey(ctx, constant.ContextKeyRequestBodySize, int64(512))
	common.SetContextKey(ctx, constant.ContextKeyUserSetting, dto.UserSetting{RecordIpLog: false})

	channel := &model.Channel{Id: 150}
	recordResponsesRoutingChannelSkip(ctx, channel, responsesRoutingChannelSkipReasonUnsupportedNamespaceTools)
	recordResponsesRoutingCandidate(ctx, channel.Id)
	attemptIndex := beginResponsesRoutingAttempt(ctx, channel.Id)
	completeResponsesRoutingAttempt(ctx, attemptIndex, unsupportedResponsesNamespaceToolsChannelError(channel))
	relayInfo := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.6-sol",
			Input: common.RawMessage(`[
				{"type":"additional_tools","tools":[{"type":"namespace","name":"secret_namespace","tools":[]}]},
				{"type":"message","role":"user","content":"secret prompt"}
			]`),
		},
	}

	recordTerminalResponsesRoutingErrorLog(ctx, relayInfo, unsupportedResponsesNamespaceToolsChannelError(channel))

	var log model.Log
	require.NoError(t, db.Order("id DESC").First(&log).Error)
	require.Equal(t, model.LogTypeError, log.Type)
	require.Equal(t, 150, log.ChannelId)
	var other map[string]interface{}
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	require.Equal(t, string(types.ErrorCodeChannelModelMappedError), other["error_code"])
	skipValues, ok := other["responses_routing_channel_skips"].([]interface{})
	require.True(t, ok)
	require.Len(t, skipValues, 1)
	skip, ok := skipValues[0].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, float64(150), skip["channel_id"])
	require.Equal(t, responsesRoutingChannelSkipReasonUnsupportedNamespaceTools, skip["reason"])
	routingTrace, ok := other["responses_routing_trace"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, float64(150), routingTrace["final_channel"])
	require.Equal(t, "channel_unavailable", routingTrace["terminal_reason"])
	require.NotContains(t, log.Other, "secret prompt")
	require.NotContains(t, log.Other, "secret_namespace")
}

func TestRecordTerminalResponsesRoutingErrorLogSkipsNonRoutingErrors(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))

	previousErrorLogEnabled := constant.ErrorLogEnabled
	constant.ErrorLogEnabled = true
	t.Cleanup(func() {
		constant.ErrorLogEnabled = previousErrorLogEnabled
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Set("id", 1)
	ctx.Set("token_name", "test-token")
	ctx.Set("original_model", "gpt-5.6-sol")
	ctx.Set("token_id", 5)
	ctx.Set("group", "default")
	ctx.Set("channel_id", 150)
	ctx.Set("username", "test-user")
	common.SetContextKey(ctx, constant.ContextKeyUserSetting, dto.UserSetting{RecordIpLog: false})

	recordTerminalResponsesRoutingErrorLog(ctx, &relaycommon.RelayInfo{}, types.NewError(errors.New("selection failed"), types.ErrorCodeGetChannelFailed))

	var count int64
	require.NoError(t, db.Model(&model.Log{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestRecordTerminalResponsesRoutingErrorLogPersistsChannelHealthBypass(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))

	server, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousErrorLogEnabled := constant.ErrorLogEnabled
	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	settings := operation_setting.GetGeneralSetting()
	previousEnabled := settings.ChannelHealthEnabled
	constant.ErrorLogEnabled = true
	common.RedisEnabled = true
	common.RDB = client
	settings.ChannelHealthEnabled = true
	t.Cleanup(func() {
		_ = client.Close()
		server.Close()
		constant.ErrorLogEnabled = previousErrorLogEnabled
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
		settings.ChannelHealthEnabled = previousEnabled
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Set("id", 1)
	ctx.Set("token_name", "test-token")
	ctx.Set("original_model", "gpt-5.6-sol")
	ctx.Set("token_id", 5)
	ctx.Set("group", "default")
	ctx.Set("username", "test-user")
	common.SetContextKey(ctx, constant.ContextKeyRequestStartTime, time.Now())
	common.SetContextKey(ctx, constant.ContextKeyUserSetting, dto.UserSetting{RecordIpLog: false})

	relayInfo := &relaycommon.RelayInfo{
		UsingGroup:      "default",
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 117},
	}
	for i := 0; i < 3; i++ {
		service.ObserveChannelHealthFailure(
			ctx,
			relayInfo,
			types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway),
		)
	}
	decision := service.EvaluateChannelHealthSelection(ctx, "default", "gpt-5.6-sol", 117)
	require.False(t, decision.Allow)
	require.Equal(t, service.ChannelHealthSelectionCooldown, decision.Action)

	recordTerminalResponsesRoutingErrorLog(
		ctx,
		relayInfo,
		types.NewError(errors.New("no channel remained after health routing"), types.ErrorCodeGetChannelFailed),
	)

	var log model.Log
	require.NoError(t, db.Order("id DESC").First(&log).Error)
	var other map[string]interface{}
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	healthInfo, ok := adminInfo["channel_health"].(map[string]interface{})
	require.True(t, ok)
	events, ok := healthInfo["events"].([]interface{})
	require.True(t, ok)
	require.NotEmpty(t, events)
}
