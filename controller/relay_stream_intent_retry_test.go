package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

// info.IsStream is derived from the upstream response during an attempt
// (helper.ShouldTreatAsEventStream), and that derivation happens before the
// status code is even looked at. Because the retry loop reuses one RelayInfo
// across channels, a first channel that answers with an SSE content type leaves
// IsStream true for every later attempt -- and ShouldTreatAsEventStream returns
// its requestedStream argument unchanged once it is true, so nothing resets it.
//
// The consequence is that a client which did not ask for streaming gets the
// streaming path applied to a later channel's ordinary JSON response.
//
// The client's own intent is recorded once in the gin context under
// constant.ContextKeyIsStream and never rewritten, so the loop can restore it
// per attempt.

// TestRetryRestoresClientStreamIntentAcrossChannels is the regression: a
// non-stream client must get channel B's JSON, even though channel A answered
// with an SSE content type.
func TestRetryRestoresClientStreamIntentAcrossChannels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupChannelControllerTestDB(t)
	// The key and group column names are package-level state that initCol sets;
	// KeyColumn triggers it. Without this the token lookup renders as
	// `WHERE  = ""` and the relay fails for a reason unrelated to this test.
	require.NotEmpty(t, model.KeyColumn())

	var firstCalls, secondCalls int
	// A retryable failure that carries an SSE content type and a body that does
	// not begin like JSON: this is what makes ShouldTreatAsEventStream report a
	// stream for a client that never asked for one.
	firstUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		firstCalls++
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream exploded"))
	}))
	defer firstUpstream.Close()

	secondUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		secondCalls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chatcmpl-b","object":"chat.completion","created":1,` +
			`"model":"relay-stream-intent-test","choices":[{"index":0,` +
			`"message":{"role":"assistant","content":"from-channel-b"},"finish_reason":"stop"}],` +
			`"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer secondUpstream.Close()

	modelName := "relay-stream-intent-test"
	initialQuota := 1_000_000
	priority := int64(10)
	weight := uint(1)
	autoBan := 0
	channels := []*model.Channel{
		{
			Id: 301, Name: "sse-content-type-failure", Key: "test-key-301",
			Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled,
			Group: "default", Models: modelName, BaseURL: &firstUpstream.URL,
			Priority: &priority, Weight: &weight, AutoBan: &autoBan,
		},
		{
			Id: 302, Name: "json-success", Key: "test-key-302",
			Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled,
			Group: "default", Models: modelName, BaseURL: &secondUpstream.URL,
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
		Id: 1, Username: "relay-stream-intent-user", Password: "test-password",
		Quota: initialQuota, Group: "default", Status: common.UserStatusEnabled,
	}).Error)

	previousRetryTimes := common.RetryTimes
	previousErrorLogEnabled := constant.ErrorLogEnabled
	previousCountToken := constant.CountToken
	previousFetchSetting := *system_setting.GetFetchSetting()
	previousRateLimitRetry := operation_setting.GetUpstreamRateLimitRetryLimit()
	common.RetryTimes = 3
	constant.ErrorLogEnabled = false
	constant.CountToken = false
	system_setting.GetFetchSetting().EnableSSRFProtection = false
	service.InitHttpClient()
	t.Cleanup(func() {
		common.RetryTimes = previousRetryTimes
		operation_setting.SetUpstreamRateLimitRetryLimit(previousRateLimitRetry)
		constant.ErrorLogEnabled = previousErrorLogEnabled
		constant.CountToken = previousCountToken
		*system_setting.GetFetchSetting() = previousFetchSetting
		service.InitHttpClient()
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	// /pg marks this a playground request, which skips the token lookup. The
	// shared test helper (setupChannelControllerTestDB) migrates no tokens
	// table, and this test exercises stream selection, not billing.
	// No "stream" field: the client asked for a non-streaming response.
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/pg/v1/chat/completions",
		strings.NewReader(`{"model":"`+modelName+`","messages":[{"role":"user","content":"ping"}]}`),
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

	require.Equal(t, 1, firstCalls)
	require.Equal(t, 1, secondCalls, "首个渠道可重试失败后必须切到第二个渠道")
	require.Contains(t, recorder.Body.String(), "from-channel-b",
		"客户端未请求流式，第二个渠道的 JSON 响应必须原样返回；"+
			"若这里为空，说明第一次尝试派生的 IsStream 泄漏到了重试")
	require.NotContains(t, recorder.Header().Get("Content-Type"), "text/event-stream",
		"非流式客户端不得收到 SSE 响应头")
}
