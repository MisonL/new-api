package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 上游用 HTTP 200 + SSE 帧内 error 报告配额耗尽（生产里渠道 213 的
// insufficient_quota 就是这个形态）。这类失败属于渠道级条件：换一个渠道
// 就能成功。回归点是"带内配额错误帧必须触发换渠道重试"，而不是被 handler
// 单方面钉成终态，让客户端拿到一个 $0 的错误响应。
//
// 这是 402 修复前的真实行为：firstCalls=1, secondCalls=0，use_channel 只有
// 第一个渠道。
func TestRelayRetriesInBandQuotaFrameThroughAnotherChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupChannelControllerTestDB(t)
	// KeyColumn 触发列名初始化；缺它时 token 查询渲染成 `WHERE  = ""`，
	// 中继会因为与本用例无关的原因失败。
	require.NotEmpty(t, model.KeyColumn())

	var quotaFrameCalls atomic.Int32
	var healthyCalls atomic.Int32

	// 渠道 A：HTTP 200，SSE 流里只发一个 insufficient_quota error 帧，
	// 后面不补 [DONE]（真实上游就是这样结束的）。
	quotaUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		quotaFrameCalls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: " + `{"error":{"message":"积分不足，请充值或兑换","type":"insufficient_quota","code":"insufficient_quota"}}` + "\n\n"))
	}))
	defer quotaUpstream.Close()

	// 渠道 B：正常流，客户端必须收到它的内容。
	healthyUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		healthyCalls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: " + `{"id":"chatcmpl-b","object":"chat.completion.chunk","model":"relay-quota-frame-test","choices":[{"index":0,"delta":{"role":"assistant"}}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: " + `{"id":"chatcmpl-b","object":"chat.completion.chunk","model":"relay-quota-frame-test","choices":[{"index":0,"delta":{"content":"from-channel-b"}}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: " + `{"id":"chatcmpl-b","object":"chat.completion.chunk","model":"relay-quota-frame-test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer healthyUpstream.Close()

	modelName := "relay-quota-frame-test"
	initialQuota := 1_000_000
	priority := int64(10)
	weight := uint(1)
	autoBan := 0
	channels := []*model.Channel{
		{
			Id: 401, Name: "quota-exhausted-upstream", Key: "test-key-401",
			Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled,
			Group: "default", Models: modelName, BaseURL: &quotaUpstream.URL,
			Priority: &priority, Weight: &weight, AutoBan: &autoBan,
		},
		{
			Id: 402, Name: "healthy-upstream", Key: "test-key-402",
			Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled,
			Group: "default", Models: modelName, BaseURL: &healthyUpstream.URL,
			Priority: &priority, Weight: &weight, AutoBan: &autoBan,
		},
	}
	for _, channel := range channels {
		require.NoError(t, db.Create(channel).Error)
		require.NoError(t, db.Create(&model.Ability{
			Group: "default", Model: modelName, ChannelId: channel.Id, Enabled: true,
			Priority: channel.Priority, Weight: *channel.Weight,
		}).Error)
	}
	require.NoError(t, db.Create(&model.User{
		Id: 1, Username: "relay-quota-frame-user", Password: "test-password",
		Quota: initialQuota, Group: "default", Status: common.UserStatusEnabled,
	}).Error)

	previousRetryTimes := common.RetryTimes
	previousErrorLogEnabled := constant.ErrorLogEnabled
	previousCountToken := constant.CountToken
	previousFetchSetting := *system_setting.GetFetchSetting()
	previousRateLimitRetry := operation_setting.GetUpstreamRateLimitRetryLimit()
	previousRetryRanges := operation_setting.AutomaticRetryStatusCodeRanges()
	common.RetryTimes = 3
	constant.ErrorLogEnabled = false
	constant.CountToken = false
	system_setting.GetFetchSetting().EnableSSRFProtection = false
	service.InitHttpClient()
	t.Cleanup(func() {
		common.RetryTimes = previousRetryTimes
		operation_setting.SetUpstreamRateLimitRetryLimit(previousRateLimitRetry)
		operation_setting.SetAutomaticRetryStatusCodeRanges(previousRetryRanges)
		constant.ErrorLogEnabled = previousErrorLogEnabled
		constant.CountToken = previousCountToken
		*system_setting.GetFetchSetting() = previousFetchSetting
		service.InitHttpClient()
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/pg/v1/chat/completions",
		strings.NewReader(`{"model":"`+modelName+`","stream":true,"messages":[{"role":"user","content":"ping"}]}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("relay_mode", relayconstant.RelayModeChatCompletions)
	common.SetContextKey(ctx, constant.ContextKeyUserId, 1)
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUserSetting, dto.UserSetting{
		AcceptUnsetRatioModel: true,
		BillingPreference:     "wallet_only",
	})
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, channels[0], modelName))

	Relay(ctx, types.RelayFormatOpenAI)

	require.Equal(t, int32(1), quotaFrameCalls.Load(),
		"配额耗尽的渠道只应被尝试一次")
	require.Equal(t, int32(1), healthyCalls.Load(),
		"带内配额错误帧必须换到第二个渠道重试")
	require.Contains(t, recorder.Body.String(), "from-channel-b",
		"客户端必须拿到第二个渠道的正常内容")
	require.Len(t, ctx.GetStringSlice("use_channel"), 2,
		"重试链必须记录两个渠道")
	require.Equal(t, "401", ctx.GetStringSlice("use_channel")[0])
}

// 带内 402 配额错误帧是渠道级条件：上游账号配额耗尽可能只发生在某个渠道，
// 换一个渠道就能成功。handler 不再把帧错误钉成 skip-retry 之后，这条路径
// 落到配置的重试状态码策略上。安全边界由 stream prelude 提交标记守住：
// 只发出过 role/空 delta 帧时可以重放，出现过实质内容后禁止重放。
func TestShouldRetryInBandQuotaFrameFollowsRetryPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)

	quotaErr := types.WithOpenAIError(types.OpenAIError{
		Message: "积分不足，请充值或兑换",
		Type:    "insufficient_quota",
		Code:    "insufficient_quota",
	}, http.StatusPaymentRequired)

	writePreludeFrame := func(t *testing.T, c *gin.Context) {
		t.Helper()
		_, writeErr := c.Writer.Write([]byte("data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n"))
		require.NoError(t, writeErr)
	}

	t.Run("allowed while only prelude frames were sent", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		common.SetContextKey(c, constant.ContextKeyResponsesStreamPreludeRetrySafe, true)
		writePreludeFrame(t, c)

		require.True(t, shouldRetry(c, quotaErr, 1),
			"首帧内容之前的带内配额错误必须允许换渠道重试")
	})

	t.Run("blocked once real content reached the client", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		common.SetContextKey(c, constant.ContextKeyResponsesStreamPreludeRetrySafe, false)
		writePreludeFrame(t, c)

		require.False(t, shouldRetry(c, quotaErr, 1),
			"内容已下发后重放会重复内容，必须禁止")
	})
}
