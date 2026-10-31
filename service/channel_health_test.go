package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func enableChannelHealthForTest(t *testing.T) {
	t.Helper()

	settings := operation_setting.GetGeneralSetting()
	previousEnabled := settings.ChannelHealthEnabled
	settings.ChannelHealthEnabled = true
	t.Cleanup(func() {
		settings.ChannelHealthEnabled = previousEnabled
	})
}

func useChannelHealthRedisForTest(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	enableChannelHealthForTest(t)

	server, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Cleanup(func() {
		_ = client.Close()
		server.Close()
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})
	return server
}

func observeChannelHealthGatewayThreshold(c *gin.Context, info *relaycommon.RelayInfo, err *types.NewAPIError) {
	for i := 0; i < channelHealthFailureThreshold; i++ {
		ObserveChannelHealthFailure(c, info, err)
	}
}

func TestChannelHealthDefaultsToDisabledWithoutRedisRoutingEffects(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})

	ctx, _ := gin.CreateTestContext(nil)
	decision := EvaluateChannelHealthSelection(ctx, "coding", "gpt-5.6-sol", 206)
	info := &relaycommon.RelayInfo{
		UsingGroup:      "coding",
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 206},
	}
	failure := types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)

	require.False(t, getChannelHealthConfig().Enabled)
	require.True(t, decision.Allow)
	require.Equal(t, ChannelHealthSelectionHealthy, decision.Action)
	require.False(t, common.GetContextKeyBool(ctx, constant.ContextKeyChannelHealthUnavailable))
	require.False(t, ShouldSuppressChannelAutoBanForHealth(ctx, info, failure))
}

func TestChannelHealthRecordsHealthyGatePassForAdministratorAudit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useChannelHealthRedisForTest(t)

	ctx, _ := gin.CreateTestContext(nil)
	decision := EvaluateChannelHealthSelection(ctx, "coding", "gpt-5.6-sol", 206)
	require.True(t, decision.Allow)
	require.Equal(t, ChannelHealthSelectionHealthy, decision.Action)

	adminInfo := make(map[string]interface{})
	AppendChannelHealthRoutingAdminInfo(ctx, adminInfo)
	healthInfo, ok := adminInfo["channel_health"].(dto.ChannelHealthRoutingInfo)
	require.True(t, ok)
	require.Equal(t, []dto.ChannelHealthRoutingEvent{{
		Group:          "coding",
		ModelName:      "gpt-5.6-sol",
		ChannelID:      206,
		Action:         string(ChannelHealthSelectionHealthy),
		RouteKey:       decision.RouteKey,
		EffectiveModel: "gpt-5.6-sol",
		Protocol:       "openai",
		Operation:      "relay.legacy.unknown./",
		UpstreamPath:   "/",
	}}, healthInfo.Events)
}

func TestChannelHealthCooldownTransitionsToSingleHalfOpenProbe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := useChannelHealthRedisForTest(t)

	ctx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "coding")
	info := &relaycommon.RelayInfo{
		UsingGroup:      "coding",
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 206},
	}
	failure := types.NewOpenAIError(
		errors.New("upstream gateway timeout"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusGatewayTimeout,
	)

	ObserveChannelHealthFailure(ctx, info, failure)

	first := EvaluateChannelHealthSelection(ctx, "coding", "gpt-5.6-sol", 206)
	require.False(t, first.Allow)
	require.Equal(t, ChannelHealthSelectionCooldown, first.Action)

	server.FastForward(DefaultChannelHealthCooldown)
	probe := EvaluateChannelHealthSelection(ctx, "coding", "gpt-5.6-sol", 206)
	require.True(t, probe.Allow)
	require.Equal(t, ChannelHealthSelectionHalfOpenProbe, probe.Action)

	competingCtx, _ := gin.CreateTestContext(nil)
	info.ChannelHealthRoute = &probe.Route
	competing := EvaluateChannelHealthSelection(competingCtx, "coding", "gpt-5.6-sol", 206)
	require.False(t, competing.Allow)
	require.Equal(t, ChannelHealthSelectionHalfOpenBusy, competing.Action)

	MarkChannelHealthProbeSuccess(ctx, info)

	recovered := EvaluateChannelHealthSelection(ctx, "coding", "gpt-5.6-sol", 206)
	require.True(t, recovered.Allow)
	require.Equal(t, ChannelHealthSelectionHealthy, recovered.Action)
}

func TestChannelHealthRuntimeStatusTracksCooldownAndProbeLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := useChannelHealthRedisForTest(t)

	failureCtx, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		UsingGroup:      "coding",
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 206},
	}
	observeChannelHealthGatewayThreshold(
		failureCtx,
		info,
		types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway),
	)

	cooldown := GetChannelHealthRuntimeStatus()
	require.True(t, cooldown.Enabled)
	require.True(t, cooldown.RedisAvailable)
	require.Len(t, cooldown.Entries, 1)
	require.Equal(t, "cooldown", cooldown.Entries[0].State)
	require.Equal(t, "coding", cooldown.Entries[0].Group)
	require.Equal(t, "gpt-5.6-sol", cooldown.Entries[0].ModelName)
	require.Equal(t, 206, cooldown.Entries[0].ChannelID)
	require.Greater(t, cooldown.Entries[0].CooldownRemainingSeconds, 0)
	require.Equal(t, http.StatusBadGateway, cooldown.Entries[0].StatusCode)

	server.FastForward(DefaultChannelHealthCooldown)
	probeCtx, _ := gin.CreateTestContext(nil)
	probe := EvaluateChannelHealthSelection(probeCtx, "coding", "gpt-5.6-sol", 206)
	require.True(t, probe.Allow)
	require.Equal(t, ChannelHealthSelectionHalfOpenProbe, probe.Action)

	halfOpen := GetChannelHealthRuntimeStatus()
	info.ChannelHealthRoute = &probe.Route
	require.True(t, halfOpen.RedisAvailable)
	require.Len(t, halfOpen.Entries, 1)
	require.Equal(t, "half_open_probe", halfOpen.Entries[0].State)
	require.Greater(t, halfOpen.Entries[0].ProbeLeaseRemainingSeconds, 0)

	MarkChannelHealthProbeSuccess(probeCtx, info)
	recovered := GetChannelHealthRuntimeStatus()
	require.True(t, recovered.RedisAvailable)
	require.Empty(t, recovered.Entries)
}

func TestChannelHealthRuntimeStatusRetainsLifecycleEventsAfterRecovery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := useChannelHealthRedisForTest(t)

	failureCtx, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		UsingGroup:      "coding",
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 206},
	}
	observeChannelHealthGatewayThreshold(
		failureCtx,
		info,
		types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway),
	)

	server.FastForward(DefaultChannelHealthCooldown)
	probeCtx, _ := gin.CreateTestContext(nil)
	probe := EvaluateChannelHealthSelection(probeCtx, "coding", "gpt-5.6-sol", 206)
	require.True(t, probe.Allow)
	require.Equal(t, ChannelHealthSelectionHalfOpenProbe, probe.Action)
	info.ChannelHealthRoute = &probe.Route
	MarkChannelHealthProbeSuccess(probeCtx, info)

	status := GetChannelHealthRuntimeStatus()
	require.True(t, status.RedisAvailable)
	require.Empty(t, status.Entries)
	require.NotEmpty(t, status.RecentEvents)
	require.Equal(t, string(ChannelHealthSelectionHalfOpenRecovered), status.RecentEvents[0].Action)
	require.Equal(t, 206, status.RecentEvents[0].ChannelID)
	require.Equal(t, "coding", status.RecentEvents[0].Group)
	require.Equal(t, "gpt-5.6-sol", status.RecentEvents[0].ModelName)
	require.Greater(t, status.RecentEvents[0].OccurredAt, int64(0))
	var cooldownEvent *dto.ChannelHealthRuntimeEvent
	for index := range status.RecentEvents {
		event := &status.RecentEvents[index]
		if event.Action == string(ChannelHealthSelectionCooldown) {
			cooldownEvent = event
			break
		}
	}
	require.NotNil(t, cooldownEvent)
	require.Equal(t, 206, cooldownEvent.ChannelID)
	require.Equal(t, "coding", cooldownEvent.Group)
	require.Equal(t, "gpt-5.6-sol", cooldownEvent.ModelName)
	require.Equal(t, http.StatusBadGateway, cooldownEvent.StatusCode)
	require.Equal(t, string(types.ErrorCodeBadResponseStatusCode), cooldownEvent.ErrorCode)
	require.Greater(t, cooldownEvent.OccurredAt, int64(0))
}

func TestChannelHealthRuntimeStatusRemovesStaleMetadataIndex(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useChannelHealthRedisForTest(t)

	digest := channelHealthDigest("coding", "gpt-5.6-sol", 206)
	ctx := context.Background()
	require.NoError(t, common.RDB.ZAdd(ctx, channelHealthRuntimeIndexKey, &redis.Z{
		Score:  float64(time.Now().Add(time.Minute).Unix()),
		Member: digest,
	}).Err())

	status := GetChannelHealthRuntimeStatus()

	require.True(t, status.RedisAvailable)
	require.Empty(t, status.Entries)
	_, err := common.RDB.ZScore(ctx, channelHealthRuntimeIndexKey, digest).Result()
	require.ErrorIs(t, err, redis.Nil)
}

func TestChannelHealthRuntimeStatusUsesRedisClockForIndexCleanup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := useChannelHealthRedisForTest(t)
	server.SetTime(time.Now().Add(2 * time.Hour))

	digest := channelHealthDigest("coding", "gpt-5.6-luna", 206)
	ctx := context.Background()
	require.NoError(t, common.RDB.ZAdd(ctx, channelHealthRuntimeIndexKey, &redis.Z{
		Score:  float64(time.Now().Unix()),
		Member: digest,
	}).Err())

	status := GetChannelHealthRuntimeStatus()

	require.True(t, status.RedisAvailable)
	require.Empty(t, status.Entries)
	_, err := common.RDB.ZScore(ctx, channelHealthRuntimeIndexKey, digest).Result()
	require.ErrorIs(t, err, redis.Nil)
}

func TestChannelHealthRuntimeStatusKeepsIndexWhenMetadataIsMalformed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useChannelHealthRedisForTest(t)

	group := "coding"
	modelName := "gpt-5.6-luna"
	channelID := 207
	route := relaycommon.ChannelRouteIdentity{
		RequestedModel:         modelName,
		EffectiveUpstreamModel: modelName,
		FinalProtocol:          "openai",
		Operation:              "relay.test",
		UpstreamPath:           "/v1/chat/completions",
		ChannelID:              channelID,
	}.Normalized()
	digest := channelHealthDigestForRoute(group, route)
	ctx := context.Background()
	require.NoError(t, common.RDB.ZAdd(ctx, channelHealthRuntimeIndexKey, &redis.Z{
		Score:  float64(time.Now().Add(time.Minute).Unix()),
		Member: digest,
	}).Err())
	_, historyKey, _, _ := channelHealthKeysForRoute(group, route)
	metadataKey := channelHealthMetadataKeyForRoute(group, route)
	require.NoError(t, common.RDB.Set(ctx, historyKey, "1", time.Minute).Err())
	require.NoError(t, common.RDB.Set(ctx, metadataKey, "{malformed", time.Minute).Err())

	status := GetChannelHealthRuntimeStatus()

	require.True(t, status.RedisAvailable)
	require.Empty(t, status.Entries)
	_, err := common.RDB.ZScore(ctx, channelHealthRuntimeIndexKey, digest).Result()
	require.NoError(t, err)
}

func TestChannelHealthDoesNotCoolParameterErrorsAndCoolsRateLimitErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := useChannelHealthRedisForTest(t)

	ctx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "coding")
	info := &relaycommon.RelayInfo{
		UsingGroup:      "coding",
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 206},
	}

	for _, tc := range []struct {
		name        string
		err         *types.NewAPIError
		wantHealthy bool
	}{
		{
			name:        "bad request parameter error",
			wantHealthy: true,
			err: types.WithOpenAIError(types.OpenAIError{
				Message: "Unknown parameter: input[1].namespace",
				Code:    "unknown_parameter",
			}, http.StatusBadRequest),
		},
		{
			name:        "request too large",
			err:         types.InitOpenAIError(types.ErrorCodeUpstreamRequestTooLarge, http.StatusRequestEntityTooLarge),
			wantHealthy: true,
		},
		{
			name:        "rate limited",
			err:         types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests),
			wantHealthy: false,
		},
		{
			name:        "gateway status with parameter payload",
			wantHealthy: true,
			err: types.WithOpenAIError(types.OpenAIError{
				Message: "Invalid type for input[0].arguments",
				Code:    "invalid_type",
			}, http.StatusBadGateway),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server.FlushAll()
			ObserveChannelHealthFailure(ctx, info, tc.err)
			decision := EvaluateChannelHealthSelection(ctx, "coding", "gpt-5.6-sol", 206)
			require.Equal(t, tc.wantHealthy, decision.Allow)
			if tc.wantHealthy {
				require.Equal(t, ChannelHealthSelectionHealthy, decision.Action)
			} else {
				require.Equal(t, ChannelHealthSelectionCooldown, decision.Action)
			}
		})
	}
}

func TestChannelHealthGatewayFailuresRequireWindowThreshold(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := useChannelHealthRedisForTest(t)
	info := &relaycommon.RelayInfo{
		UsingGroup:      "coding",
		OriginModelName: "gpt-5.6-luna",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 206},
	}
	cases := []struct {
		name string
		err  *types.NewAPIError
	}{
		{name: "500", err: types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError)},
		{name: "502", err: types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)},
		{name: "503", err: types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusServiceUnavailable)},
		{name: "unexpected eof", err: types.NewOpenAIError(errors.New("unexpected EOF"), types.ErrorCodeUpstreamTransportInterrupted, http.StatusBadGateway)},
		{name: "connection reset", err: types.NewOpenAIError(errors.New("connection reset by peer"), types.ErrorCodeUpstreamTransportInterrupted, http.StatusBadGateway)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server.FlushAll()
			ctx, _ := gin.CreateTestContext(nil)
			for i := 0; i < channelHealthFailureThreshold-1; i++ {
				ObserveChannelHealthFailure(ctx, info, tc.err)
			}
			beforeThreshold := EvaluateChannelHealthSelection(ctx, "coding", "gpt-5.6-luna", 206)
			require.True(t, beforeThreshold.Allow)
			ObserveChannelHealthFailure(ctx, info, tc.err)
			afterThreshold := EvaluateChannelHealthSelection(ctx, "coding", "gpt-5.6-luna", 206)
			require.False(t, afterThreshold.Allow)
			require.Equal(t, ChannelHealthSelectionCooldown, afterThreshold.Action)
		})
	}
}

func TestChannelHealthFailureWithoutProbeTokenDoesNotDeleteActiveProbe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useChannelHealthRedisForTest(t)

	route := relaycommon.ChannelRouteIdentity{
		RequestedModel:         "gpt-5.6-luna",
		EffectiveUpstreamModel: "gpt-5.6-luna",
		FinalProtocol:          "openai",
		Operation:              "relay.openai.responses",
		UpstreamPath:           "/v1/responses",
		ChannelID:              206,
	}.Normalized()
	_, _, probeKey, _ := channelHealthKeysForRoute("coding", route)
	probeToken := "probe-owned-by-another-request"
	require.NoError(t, common.RDB.Set(context.Background(), probeKey, probeToken, time.Minute).Err())

	record := channelHealthFailureRecord{
		Group:      "coding",
		ModelName:  route.RequestedModel,
		ChannelID:  route.ChannelID,
		Route:      route,
		RouteKey:   route.Fingerprint,
		StatusCode: http.StatusBadGateway,
		ErrorCode:  types.ErrorCodeBadResponseStatusCode,
		Cooldown:   time.Minute,
		Decision:   ChannelHealthFailureDecision{Immediate: true},
	}
	_, err := storeChannelHealthFailure(context.Background(), common.RDB, getChannelHealthConfig(), record)
	require.NoError(t, err)
	storedToken, err := common.RDB.Get(context.Background(), probeKey).Result()
	require.NoError(t, err)
	require.Equal(t, probeToken, storedToken)
}

func TestClassifyChannelHealthFailureDistinguishesDialErrors(t *testing.T) {
	connectionRefused := types.NewOpenAIError(errors.New("dial tcp 10.0.0.5:443: connect: connection refused"), types.ErrorCodeDoRequestFailed, http.StatusBadGateway)
	refusedDecision := ClassifyChannelHealthFailure(nil, nil, connectionRefused)
	require.Equal(t, ChannelHealthFailureClassTimeout, refusedDecision.Class)

	dnsFailure := types.NewOpenAIError(errors.New("dial tcp: lookup upstream.example.test: no such host"), types.ErrorCodeDoRequestFailed, http.StatusBadGateway)
	dnsDecision := ClassifyChannelHealthFailure(nil, nil, dnsFailure)
	require.Equal(t, ChannelHealthFailureClassGateway, dnsDecision.Class)
}

// 超时类失败只冷却渠道，不再阻止跨渠道重试：网关在固定超时点切断连接时，
// 换一条线路通常立刻恢复。冷却（HealthAction=cooldown）与响应体未下发的
// 4xx/5xx 一样保留，自动禁用仍然被抑制。
func TestClassifyChannelHealthFailureKeepsTimeoutsRetryable(t *testing.T) {
	testCases := []struct {
		name string
		err  *types.NewAPIError
		want string
	}{
		{
			name: "gateway timeout 504",
			err:  types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusGatewayTimeout),
			want: "gateway_timeout",
		},
		{
			name: "cloudflare timeout 524",
			err:  types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, 524),
			want: "gateway_timeout",
		},
		{
			name: "transport timeout",
			err:  types.NewOpenAIError(errors.New("net/http: timeout awaiting response headers"), types.ErrorCodeDoRequestFailed, http.StatusBadGateway),
			want: "transport_timeout",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			decision := ClassifyChannelHealthFailure(nil, nil, tc.err)
			require.Equal(t, ChannelHealthFailureClassTimeout, decision.Class)
			require.Equal(t, tc.want, decision.Scope)
			require.Equal(t, "cooldown", decision.HealthAction)
			require.Equal(t, "retry", decision.RequestRetry)
			require.Equal(t, "suppress", decision.AutoBanAction)
			require.False(t, ShouldExcludeChannelForRetry(tc.err))
		})
	}
}

// 凭据失效仍标记为 exclude：本请求内不再选这条线路，但重试门放行其它渠道/凭据。
func TestClassifyChannelHealthFailureKeepsCredentialFailuresExcluded(t *testing.T) {
	err := types.NewOpenAIError(errors.New("auth_not_found: no auth available (providers=codex)"), types.ErrorCodeBadResponseStatusCode, http.StatusServiceUnavailable)
	decision := ClassifyChannelHealthFailure(nil, nil, err)
	require.Equal(t, ChannelHealthFailureClassAuth, decision.Class)
	require.Equal(t, "exclude", decision.RequestRetry)
	require.True(t, ShouldExcludeChannelForRetry(err))
}

func TestChannelHealthAdminTestDoesNotWriteOnlineState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useChannelHealthRedisForTest(t)
	ctx, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		UsingGroup:      "coding",
		OriginModelName: "gpt-5.6-luna",
		IsChannelTest:   true,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 206},
	}
	err := types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)
	decision := ClassifyChannelHealthFailure(ctx, info, err)
	require.Equal(t, channelHealthFailureAdminTest, decision.Class)
	require.Equal(t, "ignore", decision.HealthAction)
	ObserveChannelHealthFailure(ctx, info, err)
	selection := EvaluateChannelHealthSelection(ctx, "coding", "gpt-5.6-luna", 206)
	require.True(t, selection.Allow)
	require.Equal(t, ChannelHealthSelectionHealthy, selection.Action)
}

func TestChannelHealthUsesCredentialAndRetryAfterSpecificCooldowns(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := useChannelHealthRedisForTest(t)
	ctx, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		UsingGroup:      "coding",
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 206},
	}

	authErr := types.NewErrorWithStatusCode(errors.New("auth_not_found: no auth available (providers=codex)"), types.ErrorCodeBadResponseStatusCode, http.StatusUnauthorized)
	ObserveChannelHealthFailure(ctx, info, authErr)
	status := GetChannelHealthRuntimeStatus()
	require.Len(t, status.Entries, 1)
	firstTTL := status.Entries[0].CooldownRemainingSeconds
	require.GreaterOrEqual(t, firstTTL, int((channelHealthAuthCooldown - time.Second).Seconds()))

	server.FlushAll()
	rateLimitErr := types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests)
	rateLimitErr.RetryAfterSeconds = 42
	ObserveChannelHealthFailure(ctx, info, rateLimitErr)
	status = GetChannelHealthRuntimeStatus()
	require.Len(t, status.Entries, 1)
	require.LessOrEqual(t, status.Entries[0].CooldownRemainingSeconds, 42)
	require.Greater(t, status.Entries[0].CooldownRemainingSeconds, 0)
}

func TestChannelHealthRetryAfterIsClampedToConfiguredBounds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		UsingGroup:      "coding",
		OriginModelName: "gpt-5.6-luna",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 206},
	}
	for _, tc := range []struct {
		name       string
		retryAfter int
		want       time.Duration
	}{
		{name: "zero uses fallback", retryAfter: 0, want: channelHealthRateLimitFallback},
		{name: "negative uses fallback", retryAfter: -10, want: channelHealthRateLimitFallback},
		{name: "large value is capped", retryAfter: operation_setting.MaxChannelHealthCooldownSeconds * 10, want: time.Duration(operation_setting.MaxChannelHealthCooldownSeconds) * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests)
			err.RetryAfterSeconds = tc.retryAfter
			record, ok := newChannelHealthFailureRecord(ctx, info, err)
			require.True(t, ok)
			require.Equal(t, tc.want, record.Cooldown)
		})
	}
}

func TestChannelHealthProbeRouteMatchIncludesCredentialIndex(t *testing.T) {
	base := relaycommon.ChannelRouteIdentity{
		RequestedModel:         "gpt-5.6-luna",
		EffectiveUpstreamModel: "provider/gpt-5.6-luna",
		FinalProtocol:          "openai",
		UpstreamPath:           "/v1/chat/completions",
		ChannelID:              206,
		MultiKeyIndex:          1,
		CredentialScoped:       true,
	}.Normalized()
	otherKey := base
	otherKey.MultiKeyIndex = 2
	otherKey.Fingerprint = ""
	otherKey = otherKey.Normalized()

	require.True(t, channelHealthRouteMatchesProbe(base, base))
	require.False(t, channelHealthRouteMatchesProbe(base, otherKey))
	baseProbe := base
	baseProbe.MultiKeyIndex = 0
	baseProbe.CredentialScoped = false
	baseProbe.Fingerprint = ""
	baseProbe = baseProbe.Normalized()
	keyZero := baseProbe
	keyZero.CredentialScoped = true
	keyZero.Fingerprint = ""
	keyZero = keyZero.Normalized()
	require.True(t, channelHealthProbeRouteCompatible(keyZero, baseProbe))
	require.False(t, channelHealthProbeRouteCompatible(baseProbe, keyZero))
}

func TestChannelHealthProbeRouteCompatibleAllowsRelayProtocolPathRewrite(t *testing.T) {
	probe := relaycommon.ChannelRouteIdentity{
		RequestedModel:         "gpt-5.6-luna",
		EffectiveUpstreamModel: "provider/gpt-5.6-luna",
		FinalProtocol:          "openai_responses",
		Operation:              channelHealthOperation(constant.ChannelTypeOpenAI, relayconstant.RelayModeResponses, "/v1/responses"),
		UpstreamPath:           "/v1/responses",
		ChannelID:              206,
	}.Normalized()
	actual := probe
	actual.FinalProtocol = "openai"
	actual.Operation = channelHealthOperation(constant.ChannelTypeOpenAI, relayconstant.RelayModeResponses, "/v1/chat/completions")
	actual.UpstreamPath = "/v1/chat/completions"
	actual.Fingerprint = ""
	actual = actual.Normalized()

	require.NotEqual(t, probe.Operation, actual.Operation)
	require.True(t, channelHealthProbeRouteCompatible(actual, probe))
}

func TestChannelHealthProbeRouteCompatibleRejectsUnexplainedRelayOperationChange(t *testing.T) {
	probe := relaycommon.ChannelRouteIdentity{
		RequestedModel:         "gpt-5.6-luna",
		EffectiveUpstreamModel: "provider/gpt-5.6-luna",
		FinalProtocol:          "openai",
		Operation:              channelHealthOperation(constant.ChannelTypeOpenAI, relayconstant.RelayModeChatCompletions, "/v1/chat/completions"),
		UpstreamPath:           "/v1/chat/completions",
		ChannelID:              206,
	}.Normalized()
	actual := probe
	actual.Operation = channelHealthOperation(constant.ChannelTypeOpenAI, relayconstant.RelayModeResponses, "/v1/chat/completions")
	actual.Fingerprint = ""
	actual = actual.Normalized()

	require.NotEqual(t, probe.Operation, actual.Operation)
	require.False(t, channelHealthProbeRouteCompatible(actual, probe))
}

func TestChannelHealthProbeRouteCompatibleKeepsTaskOperationsStrict(t *testing.T) {
	probe := relaycommon.ChannelRouteIdentity{
		RequestedModel:         "suno-v4",
		EffectiveUpstreamModel: "provider/suno-v4",
		FinalProtocol:          "openai",
		Operation:              "task.suno.generate",
		UpstreamPath:           "/v1/tasks",
		ChannelID:              206,
	}.Normalized()
	actual := probe
	actual.Operation = "task.suno.generate.poll"
	actual.UpstreamPath = "/v1/tasks/status"
	actual.Fingerprint = ""
	actual = actual.Normalized()

	require.False(t, channelHealthProbeRouteCompatible(actual, probe))
}

func TestChannelHealthResponsesChatHalfOpenFailureCoolsOriginalProbeRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := useChannelHealthRedisForTest(t)
	channel := &model.Channel{
		Id:   206,
		Type: constant.ChannelTypeOpenAI,
		Name: "responses-chat-bridge",
	}

	initialCtx, _ := gin.CreateTestContext(nil)
	initialCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	initialRoute, err := BuildChannelHealthRouteForChannel(initialCtx, "coding", "gpt-5.6-luna", channel)
	require.NoError(t, err)
	initialInfo := &relaycommon.RelayInfo{
		UsingGroup:          "coding",
		OriginModelName:     "gpt-5.6-luna",
		RelayMode:           relayconstant.RelayModeResponses,
		UpstreamRequestPath: "/v1/responses",
		ChannelHealthRoute:  &initialRoute,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   channel.Id,
			ChannelType: channel.Type,
		},
	}
	ObserveChannelHealthFailure(
		initialCtx,
		initialInfo,
		types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusGatewayTimeout),
	)

	server.FastForward(DefaultChannelHealthCooldown)
	probeCtx, _ := gin.CreateTestContext(nil)
	probeCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	probe := EvaluateChannelHealthSelectionForChannel(probeCtx, "coding", "gpt-5.6-luna", channel)
	require.True(t, probe.Allow)
	require.Equal(t, ChannelHealthSelectionHalfOpenProbe, probe.Action)

	// The adaptor has already restored RelayMode to Responses while the
	// request metadata records the concrete Chat Completions path that failed.
	actualInfo := &relaycommon.RelayInfo{
		UsingGroup:          "coding",
		OriginModelName:     "gpt-5.6-luna",
		RelayMode:           relayconstant.RelayModeResponses,
		UpstreamRequestPath: "/v1/chat/completions",
		ChannelHealthRoute:  &probe.Route,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   channel.Id,
			ChannelType: channel.Type,
		},
	}
	ObserveChannelHealthFailure(
		probeCtx,
		actualInfo,
		types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError),
	)

	state := channelHealthRouting(probeCtx)
	require.Nil(t, state.Probe)
	cooldownKey, _, _, _ := channelHealthKeysForRoute("coding", probe.Route)
	present, err := common.RDB.Exists(context.Background(), cooldownKey).Result()
	require.NoError(t, err)
	require.Equal(t, int64(1), present)

	nextCtx, _ := gin.CreateTestContext(nil)
	nextCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	next := EvaluateChannelHealthSelectionForChannel(nextCtx, "coding", "gpt-5.6-luna", channel)
	require.False(t, next.Allow)
	require.Equal(t, ChannelHealthSelectionCooldown, next.Action)
}

func TestChannelHealthProbeMatchRequiresRelayRoute(t *testing.T) {
	route := relaycommon.ChannelRouteIdentity{
		RequestedModel:         "gpt-5.6-luna",
		EffectiveUpstreamModel: "provider/gpt-5.6-luna",
		FinalProtocol:          "openai",
		UpstreamPath:           "/v1/chat/completions",
		ChannelID:              206,
	}.Normalized()
	state := &channelHealthRoutingState{}
	probe := channelHealthProbe{ChannelID: 206, Route: route}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 206}}

	require.False(t, channelHealthProbeMatchesRelayInfo(state, probe, info))
	info.ChannelHealthRoute = &route
	require.True(t, channelHealthProbeMatchesRelayInfo(state, probe, info))

	incompatible := route
	incompatible.EffectiveUpstreamModel = "other/provider-model"
	incompatible = incompatible.Normalized()
	info.ChannelHealthRoute = &incompatible
	require.False(t, channelHealthProbeMatchesRelayInfo(state, probe, info))
}

func TestReleaseChannelHealthProbeForChannelClearsReservedProbe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := useChannelHealthRedisForTest(t)

	info := &relaycommon.RelayInfo{
		UsingGroup:      "coding",
		OriginModelName: "gpt-5.6-release-probe",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 206},
	}
	failureCtx, _ := gin.CreateTestContext(nil)
	observeChannelHealthGatewayThreshold(
		failureCtx,
		info,
		types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway),
	)
	server.FastForward(DefaultChannelHealthCooldown)

	probeCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	probeCtx.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	decision := EvaluateChannelHealthSelection(probeCtx, info.UsingGroup, info.OriginModelName, info.ChannelMeta.ChannelId)
	require.Equal(t, ChannelHealthSelectionHalfOpenProbe, decision.Action)
	state := channelHealthRouting(probeCtx)
	require.NotNil(t, state.Probe)

	ReleaseChannelHealthProbeForChannel(probeCtx, info.ChannelMeta.ChannelId)
	require.Nil(t, state.Probe)
	_, _, probeKey, _ := channelHealthKeysForRoute(decision.Group, decision.Route)
	require.False(t, common.RDB.Exists(context.Background(), probeKey).Val() > 0)

	nextCtx, _ := gin.CreateTestContext(nil)
	next := EvaluateChannelHealthSelection(nextCtx, info.UsingGroup, info.OriginModelName, info.ChannelMeta.ChannelId)
	require.True(t, next.Allow)
	require.Equal(t, ChannelHealthSelectionHalfOpenProbe, next.Action)
	ReleaseChannelHealthProbeForChannel(nextCtx, info.ChannelMeta.ChannelId)
}

func TestChannelHealthBaseRouteDoesNotReuseStaleCredentialCooldown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useChannelHealthRedisForTest(t)

	channel := &model.Channel{
		Id:   206,
		Type: constant.ChannelTypeOpenAI,
		Name: "multi-key",
		Key:  "key-a\nkey-b",
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
		},
	}
	ctx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(ctx, constant.ContextKeyChannelId, channel.Id)
	common.SetContextKey(ctx, constant.ContextKeyChannelIsMultiKey, true)
	common.SetContextKey(ctx, constant.ContextKeyChannelMultiKeyIndex, 0)

	baseRoute, err := BuildChannelHealthRouteForChannel(ctx, "coding", "gpt-5.6-luna", channel)
	require.NoError(t, err)
	keyZeroRoute := baseRoute
	keyZeroRoute.CredentialScoped = true
	keyZeroRoute.Fingerprint = ""
	keyZeroRoute = keyZeroRoute.Normalized()
	require.NotEqual(t, baseRoute.Fingerprint, keyZeroRoute.Fingerprint)

	ObserveChannelHealthFailure(ctx, &relaycommon.RelayInfo{
		UsingGroup:         "coding",
		OriginModelName:    "gpt-5.6-luna",
		ChannelMeta:        &relaycommon.ChannelMeta{ChannelId: channel.Id, ChannelIsMultiKey: true, ChannelMultiKeyIndex: 0},
		ChannelHealthRoute: &keyZeroRoute,
	}, types.NewErrorWithStatusCode(errors.New("auth_not_found: no auth available"), types.ErrorCodeBadResponseStatusCode, http.StatusUnauthorized))

	baseDecision := EvaluateChannelHealthSelectionForChannelAtBaseRoute(ctx, "coding", "gpt-5.6-luna", channel)
	require.True(t, baseDecision.Allow)
	keyDecision := EvaluateChannelHealthSelectionForChannel(ctx, "coding", "gpt-5.6-luna", channel)
	require.False(t, keyDecision.Allow)
	require.Equal(t, ChannelHealthSelectionCooldown, keyDecision.Action)
}

func TestBuildChannelHealthRouteDoesNotReusePreviousChannelContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	common.SetContextKey(ctx, constant.ContextKeyChannelId, 201)
	common.SetContextKey(ctx, constant.ContextKeyChannelModelMapping, `{"gpt-5.6-luna":"stale/provider-model"}`)
	common.SetContextKey(ctx, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeSynthetic,
	})

	candidate := &model.Channel{
		Id:   202,
		Type: constant.ChannelTypeOpenAI,
	}
	route, err := BuildChannelHealthRouteForChannel(ctx, "coding", "gpt-5.6-luna", candidate)
	require.NoError(t, err)
	require.Equal(t, "gpt-5.6-luna", route.EffectiveUpstreamModel)
	require.Equal(t, "/v1/responses/compact", route.UpstreamPath)
}

func TestBuildChannelHealthRoutePrefersCandidateMappingForMatchingContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(ctx, constant.ContextKeyChannelId, 202)
	common.SetContextKey(ctx, constant.ContextKeyChannelModelMapping, `{"gpt-5.6-luna":"stale/provider-model"}`)

	candidate := &model.Channel{
		Id:   202,
		Type: constant.ChannelTypeOpenAI,
	}
	candidateMapping := `{"gpt-5.6-luna":"fresh/provider-model"}`
	candidate.ModelMapping = &candidateMapping

	route, err := BuildChannelHealthRouteForChannel(ctx, "coding", "gpt-5.6-luna", candidate)
	require.NoError(t, err)
	require.Equal(t, "fresh/provider-model", route.EffectiveUpstreamModel)
}

func TestBuildChannelHealthRouteUsesContextMappingForSemanticEmptyCandidateMapping(t *testing.T) {
	for _, emptyMapping := range []string{"{}", " { } "} {
		t.Run(emptyMapping, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			ctx, _ := gin.CreateTestContext(nil)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			common.SetContextKey(ctx, constant.ContextKeyChannelId, 2031)
			common.SetContextKey(ctx, constant.ContextKeyChannelModelMapping, `{"gpt-5.6-luna":"context/provider-model"}`)
			candidate := &model.Channel{Id: 2031, Type: constant.ChannelTypeOpenAI, ModelMapping: &emptyMapping}

			route, err := BuildChannelHealthRouteForChannel(ctx, "coding", "gpt-5.6-luna", candidate)
			require.NoError(t, err)
			require.Equal(t, "context/provider-model", route.EffectiveUpstreamModel)
		})
	}
}

func TestBuildChannelHealthRouteReusesSelectedRouteDiagnostics(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(ctx, constant.ContextKeyChannelRouteDiagnostics, dto.ChannelRouteSelectionDiagnostics{
		RequestedModel:      "gpt-5.6-luna",
		EffectiveRouteModel: "provider/gpt-5.6-luna",
		SelectedChannelID:   204,
	})
	invalidMapping := "{"
	candidate := &model.Channel{Id: 204, Type: constant.ChannelTypeOpenAI, ModelMapping: &invalidMapping}

	route, err := BuildChannelHealthRouteForChannel(ctx, "coding", "gpt-5.6-luna", candidate)
	require.NoError(t, err)
	require.Equal(t, "provider/gpt-5.6-luna", route.EffectiveUpstreamModel)
}

func TestChannelHealthActualRouteRecomputesRelayOperationAfterPathRewrite(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		OriginModelName:     "gpt-5.6-luna",
		UpstreamRequestPath: "/v1/chat/completions",
		RelayMode:           0,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         205,
			ChannelType:       constant.ChannelTypeOpenAI,
			UpstreamModelName: "provider/gpt-5.6-luna",
		},
		ChannelHealthRoute: &relaycommon.ChannelRouteIdentity{
			RequestedModel:         "gpt-5.6-luna",
			EffectiveUpstreamModel: "provider/gpt-5.6-luna",
			FinalProtocol:          "openai_responses",
			Operation:              channelHealthOperation(constant.ChannelTypeOpenAI, 0, "/v1/responses"),
			UpstreamPath:           "/v1/responses",
			ChannelID:              205,
		},
	}

	actual := channelHealthActualRouteForInfo(ctx, info)
	require.Equal(t, "/v1/chat/completions", actual.UpstreamPath)
	require.Equal(t, "openai", actual.FinalProtocol)
	require.Equal(t, channelHealthOperation(constant.ChannelTypeOpenAI, 0, "/v1/chat/completions"), actual.Operation)
	require.NotEqual(t, channelHealthOperation(constant.ChannelTypeOpenAI, 0, "/v1/responses"), actual.Operation)
}

func TestChannelHealthActualRoutePreservesTaskOperationAfterPathRewrite(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/tasks", nil)
	info := &relaycommon.RelayInfo{
		OriginModelName:     "suno-v4",
		UpstreamRequestPath: "/v1/tasks/status",
		RelayMode:           0,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         206,
			ChannelType:       constant.ChannelTypeOpenAI,
			UpstreamModelName: "provider/suno-v4",
		},
		ChannelHealthRoute: &relaycommon.ChannelRouteIdentity{
			RequestedModel:         "suno-v4",
			EffectiveUpstreamModel: "provider/suno-v4",
			FinalProtocol:          "openai",
			Operation:              "task.suno.generate",
			UpstreamPath:           "/v1/tasks",
			ChannelID:              206,
		},
	}

	actual := channelHealthActualRouteForInfo(ctx, info)
	require.Equal(t, "task.suno.generate", actual.Operation)
}

func TestChannelHealthSelectionRejectsInvalidCandidateMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctx, _ := gin.CreateTestContext(nil)
	candidate := &model.Channel{
		Id:   203,
		Type: constant.ChannelTypeOpenAI,
	}
	invalidMapping := `{"gpt-5.6-luna":""}`
	candidate.ModelMapping = &invalidMapping

	decision := EvaluateChannelHealthSelectionForChannelAtBaseRoute(ctx, "coding", "gpt-5.6-luna", candidate)
	require.False(t, decision.Allow)
	require.Equal(t, ChannelHealthSelectionInvalidRoute, decision.Action)
	require.Error(t, decision.Err)
	adminInfo := make(map[string]interface{})
	AppendChannelHealthRoutingAdminInfo(ctx, adminInfo)
	healthInfo, ok := adminInfo["channel_health"].(dto.ChannelHealthRoutingInfo)
	require.True(t, ok)
	require.Len(t, healthInfo.Events, 1)
	require.Equal(t, string(types.ErrorCodeChannelModelMappedError), healthInfo.Events[0].ErrorCode)
}

func TestBindChannelHealthRouteRejectsStaleSelectedRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	common.SetContextKey(ctx, constant.ContextKeyChannelId, 202)
	common.SetContextKey(ctx, constant.ContextKeyChannelModelMapping, `{"gpt-5.6-luna":"new/provider-model"}`)

	selectedChannelHealthRoute(ctx, relaycommon.ChannelRouteIdentity{
		RequestedModel:         "gpt-5.6-luna",
		EffectiveUpstreamModel: "old/provider-model",
		FinalProtocol:          "openai_responses",
		UpstreamPath:           "/v1/responses/compact",
		ChannelID:              202,
	})
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-5.6-luna",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId: 202,
		},
	}

	BindChannelHealthRoute(ctx, info, "new/provider-model")
	require.NotNil(t, info.ChannelHealthRoute)
	require.Equal(t, "new/provider-model", info.ChannelHealthRoute.EffectiveUpstreamModel)
}

func TestChannelHealthCredentialFailureReleasesUnscopedProbe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := useChannelHealthRedisForTest(t)

	channel := &model.Channel{
		Id:   206,
		Type: constant.ChannelTypeOpenAI,
		Name: "multi-key",
		Key:  "key-a\nkey-b",
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: true,
		},
	}
	failureCtx, _ := gin.CreateTestContext(nil)
	baseInfo := &relaycommon.RelayInfo{
		UsingGroup:      "coding",
		OriginModelName: "gpt-5.6-luna",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channel.Id},
	}
	observeChannelHealthGatewayThreshold(failureCtx, baseInfo, types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway))
	server.FastForward(DefaultChannelHealthCooldown)

	probeCtx, _ := gin.CreateTestContext(nil)
	probe := EvaluateChannelHealthSelectionForChannelAtBaseRoute(probeCtx, "coding", "gpt-5.6-luna", channel)
	require.True(t, probe.Allow)
	require.Equal(t, ChannelHealthSelectionHalfOpenProbe, probe.Action)

	common.SetContextKey(probeCtx, constant.ContextKeyChannelId, channel.Id)
	common.SetContextKey(probeCtx, constant.ContextKeyChannelIsMultiKey, true)
	common.SetContextKey(probeCtx, constant.ContextKeyChannelMultiKeyIndex, 0)
	keyRoute, err := BuildChannelHealthRouteForChannel(probeCtx, "coding", "gpt-5.6-luna", channel)
	require.NoError(t, err)
	keyRoute.CredentialScoped = true
	keyRoute.Fingerprint = ""
	keyRoute = keyRoute.Normalized()
	ObserveChannelHealthFailure(probeCtx, &relaycommon.RelayInfo{
		UsingGroup:         "coding",
		OriginModelName:    "gpt-5.6-luna",
		ChannelMeta:        &relaycommon.ChannelMeta{ChannelId: channel.Id, ChannelIsMultiKey: true, ChannelMultiKeyIndex: 0},
		ChannelHealthRoute: &keyRoute,
	}, types.NewErrorWithStatusCode(errors.New("auth_not_found: no auth available"), types.ErrorCodeBadResponseStatusCode, http.StatusUnauthorized))

	nextProbeCtx, _ := gin.CreateTestContext(nil)
	nextProbe := EvaluateChannelHealthSelectionForChannelAtBaseRoute(nextProbeCtx, "coding", "gpt-5.6-luna", channel)
	require.True(t, nextProbe.Allow)
	require.Equal(t, ChannelHealthSelectionHalfOpenProbe, nextProbe.Action)
}

func TestChannelHealthAuthenticationCooldownIsIndependentPerCredential(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useChannelHealthRedisForTest(t)

	channel := &model.Channel{
		Id:   206,
		Type: constant.ChannelTypeOpenAI,
		Name: "multi-key",
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: true,
		},
	}
	failureCtx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(failureCtx, constant.ContextKeyChannelId, channel.Id)
	common.SetContextKey(failureCtx, constant.ContextKeyChannelIsMultiKey, true)
	common.SetContextKey(failureCtx, constant.ContextKeyChannelMultiKeyIndex, 1)
	baseRoute, err := BuildChannelHealthRouteForChannel(failureCtx, "coding", "gpt-5.6-luna", channel)
	require.NoError(t, err)
	keyRoute := baseRoute
	keyRoute.MultiKeyIndex = 1
	keyRoute.CredentialScoped = true
	keyRoute.Fingerprint = ""
	keyRoute = keyRoute.Normalized()
	info := &relaycommon.RelayInfo{
		UsingGroup:         "coding",
		OriginModelName:    "gpt-5.6-luna",
		ChannelMeta:        &relaycommon.ChannelMeta{ChannelId: channel.Id, ChannelIsMultiKey: true, ChannelMultiKeyIndex: 1},
		ChannelHealthRoute: &keyRoute,
	}
	authErr := types.NewErrorWithStatusCode(errors.New("auth_not_found: no auth available"), types.ErrorCodeBadResponseStatusCode, http.StatusUnauthorized)
	ObserveChannelHealthFailure(failureCtx, info, authErr)

	keyOneCtx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(keyOneCtx, constant.ContextKeyChannelId, channel.Id)
	common.SetContextKey(keyOneCtx, constant.ContextKeyChannelIsMultiKey, true)
	common.SetContextKey(keyOneCtx, constant.ContextKeyChannelMultiKeyIndex, 1)
	keyOne := EvaluateChannelHealthSelectionForChannel(keyOneCtx, "coding", "gpt-5.6-luna", channel)
	require.False(t, keyOne.Allow)
	require.Equal(t, ChannelHealthSelectionCooldown, keyOne.Action)

	keyTwoCtx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(keyTwoCtx, constant.ContextKeyChannelId, channel.Id)
	common.SetContextKey(keyTwoCtx, constant.ContextKeyChannelIsMultiKey, true)
	common.SetContextKey(keyTwoCtx, constant.ContextKeyChannelMultiKeyIndex, 2)
	keyTwo := EvaluateChannelHealthSelectionForChannel(keyTwoCtx, "coding", "gpt-5.6-luna", channel)
	require.True(t, keyTwo.Allow)
	require.Equal(t, ChannelHealthSelectionHealthy, keyTwo.Action)
}

func TestChannelHealthSelectionExclusionKeepsMultiKeyChannelRetryable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	channel := &model.Channel{
		Id:   206,
		Type: constant.ChannelTypeOpenAI,
		Key:  "key-a\nkey-b\nkey-c",
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 3,
		},
	}
	common.SetContextKey(ctx, constant.ContextKeyChannelId, channel.Id)
	common.SetContextKey(ctx, constant.ContextKeyChannelIsMultiKey, true)
	common.SetContextKey(ctx, constant.ContextKeyChannelMultiKeyIndex, 1)

	route, err := BuildChannelHealthRouteForChannel(ctx, "coding", "gpt-5.6-luna", channel)
	require.NoError(t, err)
	route.MultiKeyIndex = 1
	route.CredentialScoped = true
	route.Fingerprint = ""
	route = route.Normalized()
	require.True(t, ExcludeChannelHealthSelectionForRoute(ctx, "coding", "gpt-5.6-luna", route))
	MarkChannelHealthSelectionRetryableForRoute(ctx, "coding", "gpt-5.6-luna", route)
	require.Empty(t, channelHealthSelectionExclusions(ctx, "coding", "gpt-5.6-luna"))
	require.Equal(t, map[int]struct{}{1: {}}, GetChannelHealthExcludedMultiKeyIndexes(ctx, "coding", "gpt-5.6-luna", channel))

	route.MultiKeyIndex = 0
	route.CredentialScoped = true
	route.Fingerprint = ""
	route = route.Normalized()
	require.True(t, ExcludeChannelHealthSelectionForRoute(ctx, "coding", "gpt-5.6-luna", route))
	MarkChannelHealthSelectionRetryableForRoute(ctx, "coding", "gpt-5.6-luna", route)
	require.Empty(t, channelHealthSelectionExclusions(ctx, "coding", "gpt-5.6-luna"))
	require.Equal(t, map[int]struct{}{0: {}, 1: {}}, GetChannelHealthExcludedMultiKeyIndexes(ctx, "coding", "gpt-5.6-luna", channel))
}

func TestChannelHealthDoesNotClassifyGateway502AuthTextAsCredentialFailure(t *testing.T) {
	err := types.NewErrorWithStatusCode(
		errors.New("auth_not_found: no auth available (providers=codex)"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusBadGateway,
	)

	require.False(t, isChannelCredentialUnavailable(err))
}

func TestChannelHealthUsesOriginalUnauthorizedStatusAfterResponseMapping(t *testing.T) {
	for _, tc := range []struct {
		name    string
		message string
		status  int
		origin  int
	}{
		{name: "invalid api key", message: "invalid api key", status: http.StatusBadRequest, origin: http.StatusUnauthorized},
		{name: "authentication failed", message: "authentication failed", status: http.StatusBadRequest, origin: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := types.NewErrorWithStatusCode(errors.New(tc.message), types.ErrorCodeBadResponseStatusCode, tc.status)
			err.OriginalStatusCode = tc.origin

			require.True(t, isChannelCredentialUnavailable(err))
		})
	}
}

func TestChannelHealthUsesOriginalUpstreamGatewayStatusAfterResponseMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useChannelHealthRedisForTest(t)

	ctx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "coding")
	info := &relaycommon.RelayInfo{
		UsingGroup:      "coding",
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 206},
	}
	err := types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadRequest)
	err.OriginalStatusCode = http.StatusBadGateway

	observeChannelHealthGatewayThreshold(ctx, info, err)

	decision := EvaluateChannelHealthSelection(ctx, "coding", "gpt-5.6-sol", 206)
	require.False(t, decision.Allow)
	require.Equal(t, ChannelHealthSelectionCooldown, decision.Action)
}

func TestChannelHealthSelectionFallsThroughCooledHighPriorities(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, memoryCacheEnabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "database fallback", true: "memory cache"}[memoryCacheEnabled], func(t *testing.T) {
			db := setupChannelSelectTestDB(t)
			useChannelHealthRedisForTest(t)
			common.MemoryCacheEnabled = memoryCacheEnabled
			seedPriorityRetryChannels(t, db)
			if memoryCacheEnabled {
				model.InitChannelCache()
			}

			ctx, _ := gin.CreateTestContext(nil)
			common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
			common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
			failure := types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)
			for _, channelID := range []int{206, 207} {
				observeChannelHealthGatewayThreshold(ctx, &relaycommon.RelayInfo{
					UsingGroup:      "default",
					OriginModelName: "gpt-5.5",
					ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channelID},
				}, failure)
			}

			selected, group, err := CacheGetRandomSatisfiedChannel(&RetryParam{
				Ctx:        ctx,
				TokenGroup: "default",
				ModelName:  "gpt-5.5",
				Retry:      common.GetPointer(0),
			})

			require.NoError(t, err)
			require.Equal(t, "default", group)
			require.NotNil(t, selected)
			require.Equal(t, 208, selected.Id)
		})
	}
}

func TestChannelHealthFailsOpenWhenRedisIsUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	enableChannelHealthForTest(t)
	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})

	ctx, _ := gin.CreateTestContext(nil)
	decision := EvaluateChannelHealthSelection(ctx, "coding", "gpt-5.6-sol", 206)

	require.True(t, decision.Allow)
	require.Equal(t, ChannelHealthSelectionUnavailable, decision.Action)
	require.True(t, common.GetContextKeyBool(ctx, constant.ContextKeyChannelHealthUnavailable))
}

func TestChannelHealthFailsOpenWhenRedisCommandFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := useChannelHealthRedisForTest(t)
	server.Close()

	ctx, _ := gin.CreateTestContext(nil)
	decision := EvaluateChannelHealthSelection(ctx, "coding", "gpt-5.6-sol", 206)

	require.True(t, decision.Allow)
	require.Equal(t, ChannelHealthSelectionUnavailable, decision.Action)
	require.True(t, common.GetContextKeyBool(ctx, constant.ContextKeyChannelHealthUnavailable))
	require.False(t, GetChannelHealthRuntimeStatus().RedisAvailable)
}

func TestChannelHealthDisabledLeavesGatewayFailureForExistingAutoBanPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useChannelHealthRedisForTest(t)

	settings := operation_setting.GetGeneralSetting()
	previousEnabled := settings.ChannelHealthEnabled
	settings.ChannelHealthEnabled = false
	t.Cleanup(func() {
		settings.ChannelHealthEnabled = previousEnabled
	})

	ctx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "coding")
	info := &relaycommon.RelayInfo{
		UsingGroup:      "coding",
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 206},
	}
	failure := types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)

	require.False(t, ShouldSuppressChannelAutoBanForHealth(ctx, info, failure))
	ObserveChannelHealthFailure(ctx, info, failure)

	decision := EvaluateChannelHealthSelection(ctx, "coding", "gpt-5.6-sol", 206)
	require.True(t, decision.Allow)
	require.Equal(t, ChannelHealthSelectionHealthy, decision.Action)
}

func TestChannelHealthProbeReleaseKeepsFailureHistoryAvailableForNextProbe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := useChannelHealthRedisForTest(t)

	failureCtx, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		UsingGroup:      "coding",
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 206},
	}
	failure := types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)
	observeChannelHealthGatewayThreshold(failureCtx, info, failure)
	server.FastForward(DefaultChannelHealthCooldown)

	probeCtx, _ := gin.CreateTestContext(nil)
	probe := EvaluateChannelHealthSelection(probeCtx, "coding", "gpt-5.6-sol", 206)
	require.True(t, probe.Allow)
	require.Equal(t, ChannelHealthSelectionHalfOpenProbe, probe.Action)

	// Local failures before dispatch, such as body, billing, or channel setup
	// errors, must not consume the probe lease or clear the prior health history.
	ReleaseChannelHealthProbe(probeCtx, info)

	nextProbeCtx, _ := gin.CreateTestContext(nil)
	nextProbe := EvaluateChannelHealthSelection(nextProbeCtx, "coding", "gpt-5.6-sol", 206)
	require.True(t, nextProbe.Allow)
	require.Equal(t, ChannelHealthSelectionHalfOpenProbe, nextProbe.Action)
	ReleaseChannelHealthProbe(nextProbeCtx, info)
}

func TestChannelHealthProbeRenewalRestartsAfterSameContextRelease(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := useChannelHealthRedisForTest(t)

	info := &relaycommon.RelayInfo{
		UsingGroup:      "coding",
		OriginModelName: "gpt-5.6-luna",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 206},
	}
	failureCtx, _ := gin.CreateTestContext(nil)
	observeChannelHealthGatewayThreshold(
		failureCtx,
		info,
		types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway),
	)
	server.FastForward(DefaultChannelHealthCooldown)

	requestContext, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	probeCtx, _ := gin.CreateTestContext(nil)
	probeCtx.Request = httptest.NewRequest(http.MethodPost, "/", nil).WithContext(requestContext)
	first := EvaluateChannelHealthSelection(probeCtx, "coding", "gpt-5.6-luna", 206)
	require.True(t, first.Allow)
	require.Equal(t, ChannelHealthSelectionHalfOpenProbe, first.Action)
	firstToken := first.probeToken
	require.NotEmpty(t, firstToken)

	state := channelHealthRouting(probeCtx)
	state.LeaseRenewalMu.Lock()
	require.Contains(t, state.LeaseRenewals, firstToken)
	state.LeaseRenewalMu.Unlock()

	ReleaseChannelHealthProbe(probeCtx, info)
	state.LeaseRenewalMu.Lock()
	require.NotContains(t, state.LeaseRenewals, firstToken)
	state.LeaseRenewalMu.Unlock()

	second := EvaluateChannelHealthSelection(probeCtx, "coding", "gpt-5.6-luna", 206)
	require.True(t, second.Allow)
	require.Equal(t, ChannelHealthSelectionHalfOpenProbe, second.Action)
	require.NotEqual(t, firstToken, second.probeToken)
	state.LeaseRenewalMu.Lock()
	require.Contains(t, state.LeaseRenewals, second.probeToken)
	state.LeaseRenewalMu.Unlock()
	ReleaseChannelHealthProbe(probeCtx, info)
}

func TestChannelHealthProbeReservationRejectsConcurrentCooldown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useChannelHealthRedisForTest(t)

	group := "coding"
	modelName := "gpt-5.6-sol"
	channelID := 206
	cooldownKey, historyKey, probeKey := channelHealthKeys(group, modelName, channelID)
	metadataKey := channelHealthMetadataKey(group, modelName, channelID)
	digest := channelHealthDigest(group, modelName, channelID)
	ctx := context.Background()
	require.NoError(t, common.RDB.Set(ctx, cooldownKey, "1", time.Minute).Err())
	require.NoError(t, common.RDB.Set(ctx, historyKey, "1", time.Minute).Err())

	reserved, historyPresent, cooldownPresent, err := reserveChannelHealthProbe(ctx, common.RDB, channelHealthProbeReservation{
		ProbeKey:    probeKey,
		CooldownKey: cooldownKey,
		HistoryKey:  historyKey,
		MetadataKey: metadataKey,
		Token:       "probe-token",
		Digest:      digest,
		Lease:       defaultChannelHealthProbeLease,
	})
	require.NoError(t, err)
	require.False(t, reserved)
	require.True(t, historyPresent)
	require.True(t, cooldownPresent)
	probeExists, err := common.RDB.Exists(ctx, probeKey).Result()
	require.NoError(t, err)
	require.Zero(t, probeExists)
}

func TestChannelHealthProbeReservationUsesRedisClockForRuntimeIndex(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := useChannelHealthRedisForTest(t)
	redisNow := time.Unix(1700000000, 0)
	server.SetTime(redisNow)

	group := "coding"
	modelName := "gpt-5.6-sol"
	channelID := 206
	cooldownKey, historyKey, probeKey := channelHealthKeys(group, modelName, channelID)
	metadataKey := channelHealthMetadataKey(group, modelName, channelID)
	digest := channelHealthDigest(group, modelName, channelID)
	ctx := context.Background()
	require.NoError(t, common.RDB.Set(ctx, historyKey, "1", time.Minute).Err())

	historyTTL := defaultChannelHealthProbeLease * channelHealthProbeHistoryLeases
	reserved, _, _, err := reserveChannelHealthProbe(ctx, common.RDB, channelHealthProbeReservation{
		ProbeKey:    probeKey,
		CooldownKey: cooldownKey,
		HistoryKey:  historyKey,
		MetadataKey: metadataKey,
		Token:       "probe-token",
		Digest:      digest,
		Lease:       defaultChannelHealthProbeLease,
	})
	require.NoError(t, err)
	require.True(t, reserved)

	runtimeScore, err := common.RDB.ZScore(ctx, channelHealthRuntimeIndexKey, digest).Result()
	require.NoError(t, err)
	require.Equal(t, float64(redisNow.Add(historyTTL).Unix()), runtimeScore)
}

func TestChannelHealthProbeRenewalExtendsMetadataTTL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := useChannelHealthRedisForTest(t)

	group := "coding"
	modelName := "gpt-5.6-luna"
	route := relaycommon.ChannelRouteIdentity{
		RequestedModel:         modelName,
		EffectiveUpstreamModel: modelName,
		FinalProtocol:          "openai",
		UpstreamPath:           "/v1/chat/completions",
		ChannelID:              206,
	}.Normalized()
	_, historyKey, probeKey, _ := channelHealthKeysForRoute(group, route)
	metadataKey := channelHealthMetadataKeyForRoute(group, route)
	digest := channelHealthDigestForRoute(group, route)
	ctx := context.Background()
	require.NoError(t, common.RDB.Set(ctx, probeKey, "probe-token", time.Second).Err())
	require.NoError(t, common.RDB.Set(ctx, historyKey, "1", time.Second).Err())
	require.NoError(t, common.RDB.Set(ctx, metadataKey, "metadata", time.Second).Err())
	require.NoError(t, common.RDB.ZAdd(ctx, channelHealthRuntimeIndexKey, &redis.Z{Score: 1, Member: digest}).Err())

	probe := channelHealthProbe{Group: group, ModelName: modelName, ChannelID: route.ChannelID, Route: route, Token: "probe-token"}
	server.FastForward(500 * time.Millisecond)
	require.True(t, renewChannelHealthProbeWithClient(common.RDB, probe))
	metadataTTL, err := common.RDB.PTTL(ctx, metadataKey).Result()
	require.NoError(t, err)
	require.Greater(t, metadataTTL, getChannelHealthConfig().ProbeLease)
	historyTTL, err := common.RDB.PTTL(ctx, historyKey).Result()
	require.NoError(t, err)
	require.Greater(t, historyTTL, getChannelHealthConfig().ProbeLease)
	runtimeScore, err := common.RDB.ZScore(ctx, channelHealthRuntimeIndexKey, digest).Result()
	require.NoError(t, err)
	require.Greater(t, runtimeScore, float64(1))
}

func TestChannelHealthProbeRenewalRejectsPersistedExpiredLease(t *testing.T) {
	server := useChannelHealthRedisForTest(t)
	group := "coding"
	modelName := "gpt-5.6-luna"
	route := relaycommon.ChannelRouteIdentity{
		RequestedModel:         modelName,
		EffectiveUpstreamModel: modelName,
		FinalProtocol:          "openai",
		UpstreamPath:           "/v1/chat/completions",
		ChannelID:              206,
	}.Normalized()
	_, historyKey, probeKey, _ := channelHealthKeysForRoute(group, route)
	metadataKey := channelHealthMetadataKeyForRoute(group, route)
	ctx := context.Background()
	require.NoError(t, common.RDB.Set(ctx, probeKey, "probe-token", time.Second).Err())
	require.NoError(t, common.RDB.Set(ctx, historyKey, "1", time.Second).Err())
	require.NoError(t, common.RDB.Set(ctx, metadataKey, "metadata", time.Second).Err())
	server.FastForward(1500 * time.Millisecond)
	probe := channelHealthProbe{Group: group, ModelName: modelName, ChannelID: route.ChannelID, Route: route, Token: "probe-token"}
	require.Equal(t, channelHealthProbeRenewLeaseMissing, renewChannelHealthProbeStatus(common.RDB, probe, getChannelHealthConfig().ProbeLease))
}

func TestChannelHealthProbeRenewalFailureThresholdCancelsRequest(t *testing.T) {
	server := useChannelHealthRedisForTest(t)
	settings := operation_setting.GetGeneralSetting()
	previousLease := settings.ChannelHealthProbeLeaseSeconds
	settings.ChannelHealthProbeLeaseSeconds = 1
	t.Cleanup(func() { settings.ChannelHealthProbeLeaseSeconds = previousLease })

	failureCtx, _ := gin.CreateTestContext(nil)
	observeChannelHealthGatewayThreshold(
		failureCtx,
		&relaycommon.RelayInfo{
			UsingGroup:      "coding",
			OriginModelName: "gpt-5.6-luna",
			ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 206},
		},
		types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway),
	)
	server.FastForward(DefaultChannelHealthCooldown)

	requestContext, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	probeCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	probeCtx.Request = httptest.NewRequest(http.MethodPost, "/", nil).WithContext(requestContext)
	decision := EvaluateChannelHealthSelection(probeCtx, "coding", "gpt-5.6-luna", 206)
	require.Equal(t, ChannelHealthSelectionHalfOpenProbe, decision.Action)
	server.SetError("LOADING Redis is loading the dataset in memory")
	require.Eventually(t, func() bool {
		return probeCtx.Request.Context().Err() != nil
	}, 5*time.Second, 20*time.Millisecond)
	require.ErrorIs(t, probeCtx.Request.Context().Err(), context.Canceled)
	server.SetError("")
}

func TestChannelHealthFailureRestoresRequestContextAfterConcurrencyLease(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := useChannelHealthRedisForTest(t)

	info := &relaycommon.RelayInfo{
		UsingGroup:      "coding",
		OriginModelName: "gpt-5.6-probe-concurrency",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 208},
	}
	failureCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	failureCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	failureRoute := relaycommon.ChannelRouteIdentity{
		RequestedModel:         info.OriginModelName,
		EffectiveUpstreamModel: info.OriginModelName,
		FinalProtocol:          "openai",
		Operation:              "relay.0.0./v1/chat/completions",
		UpstreamPath:           "/v1/chat/completions",
		ChannelID:              info.ChannelMeta.ChannelId,
	}.Normalized()
	info.ChannelHealthRoute = &failureRoute
	observeChannelHealthGatewayThreshold(
		failureCtx,
		info,
		types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway),
	)
	server.FastForward(DefaultChannelHealthCooldown)

	probeCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	probeCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	decision := EvaluateChannelHealthSelection(probeCtx, "coding", info.OriginModelName, info.ChannelMeta.ChannelId)
	require.Equal(t, ChannelHealthSelectionHalfOpenProbe, decision.Action)
	info.ChannelHealthRoute = &decision.Route

	lease, err := AcquireChannelConcurrency(probeCtx, &model.Channel{Id: info.ChannelMeta.ChannelId, MaxConcurrency: 1})
	require.NoError(t, err)
	require.NotNil(t, lease)

	ObserveChannelHealthFailure(
		probeCtx,
		info,
		types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway),
	)
	lease.Release()

	require.NoError(t, probeCtx.Request.Context().Err())
}

func TestChannelHealthProbeRenewalTerminationRestoresContextBeforeRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := useChannelHealthRedisForTest(t)
	settings := operation_setting.GetGeneralSetting()
	previousLease := settings.ChannelHealthProbeLeaseSeconds
	settings.ChannelHealthProbeLeaseSeconds = 1
	t.Cleanup(func() { settings.ChannelHealthProbeLeaseSeconds = previousLease })

	info := &relaycommon.RelayInfo{
		UsingGroup:      "coding",
		OriginModelName: "gpt-5.6-probe-renewal-retry",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 209},
	}
	failureCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	failureCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	observeChannelHealthGatewayThreshold(
		failureCtx,
		info,
		types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway),
	)
	server.FastForward(DefaultChannelHealthCooldown)

	requestContext := context.Background()
	probeCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	probeCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(requestContext)
	decision := EvaluateChannelHealthSelection(probeCtx, "coding", info.OriginModelName, info.ChannelMeta.ChannelId)
	require.Equal(t, ChannelHealthSelectionHalfOpenProbe, decision.Action)
	info.ChannelHealthRoute = &decision.Route

	server.SetError("LOADING Redis is loading the dataset in memory")
	require.Eventually(t, func() bool {
		return probeCtx.Request.Context().Err() != nil
	}, 5*time.Second, 20*time.Millisecond)
	require.ErrorIs(t, probeCtx.Request.Context().Err(), context.Canceled)
	server.SetError("")

	lease, err := AcquireChannelConcurrency(probeCtx, &model.Channel{Id: info.ChannelMeta.ChannelId, MaxConcurrency: 1})
	require.NoError(t, err)
	require.NotNil(t, lease)
	defer lease.Release()

	ObserveChannelHealthFailure(
		probeCtx,
		info,
		types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway),
	)
	lease.Release()

	// The health cleanup must restore the request that existed before the
	// canceled probe wrapper. Otherwise a subsequent retry inherits context.Canceled.
	require.NoError(t, probeCtx.Request.Context().Err())
	retry := EvaluateChannelHealthSelection(probeCtx, "coding", info.OriginModelName, info.ChannelMeta.ChannelId)
	require.False(t, retry.Allow)
	require.Equal(t, ChannelHealthSelectionCooldown, retry.Action)
	require.NoError(t, probeCtx.Request.Context().Err())
}

func TestChannelHealthProbeRenewalMissingLeaseCancelsRequestWhenStateEntryWasRemoved(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := useChannelHealthRedisForTest(t)
	settings := operation_setting.GetGeneralSetting()
	previousLease := settings.ChannelHealthProbeLeaseSeconds
	settings.ChannelHealthProbeLeaseSeconds = 1
	t.Cleanup(func() { settings.ChannelHealthProbeLeaseSeconds = previousLease })

	info := &relaycommon.RelayInfo{
		UsingGroup:      "coding",
		OriginModelName: "gpt-5.6-missing-renewal-state",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 207},
	}
	failureCtx, _ := gin.CreateTestContext(nil)
	observeChannelHealthGatewayThreshold(
		failureCtx,
		info,
		types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway),
	)
	server.FastForward(DefaultChannelHealthCooldown)

	requestContext, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	probeCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	probeCtx.Request = httptest.NewRequest(http.MethodPost, "/", nil).WithContext(requestContext)
	decision := EvaluateChannelHealthSelection(probeCtx, "coding", info.OriginModelName, info.ChannelMeta.ChannelId)
	require.Equal(t, ChannelHealthSelectionHalfOpenProbe, decision.Action)
	state := channelHealthRouting(probeCtx)
	state.LeaseRenewalMu.Lock()
	delete(state.LeaseRenewalState, decision.probeToken)
	state.LeaseRenewalMu.Unlock()
	_, _, probeKey, _ := channelHealthKeysForRoute(decision.Group, decision.Route)
	require.NoError(t, common.RDB.Del(context.Background(), probeKey).Err())

	require.Eventually(t, func() bool {
		return probeCtx.Request.Context().Err() != nil
	}, 3*time.Second, 20*time.Millisecond)
	require.ErrorIs(t, probeCtx.Request.Context().Err(), context.Canceled)
	ReleaseChannelHealthProbe(probeCtx, info)
}

func TestChannelHealthTerminatedProbeCannotBeMarkedSuccessful(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := useChannelHealthRedisForTest(t)

	info := &relaycommon.RelayInfo{
		UsingGroup:      "coding",
		OriginModelName: "gpt-5.6-terminated-probe",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 206},
	}
	failureCtx, _ := gin.CreateTestContext(nil)
	observeChannelHealthGatewayThreshold(
		failureCtx,
		info,
		types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway),
	)
	server.FastForward(DefaultChannelHealthCooldown)

	probeCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	probeCtx.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	probe := EvaluateChannelHealthSelection(probeCtx, "coding", info.OriginModelName, info.ChannelMeta.ChannelId)
	require.Equal(t, ChannelHealthSelectionHalfOpenProbe, probe.Action)
	info.ChannelHealthRoute = &probe.Route

	state := channelHealthRouting(probeCtx)
	state.LeaseRenewalMu.Lock()
	state.LeaseRenewalState[probe.probeToken].terminated = true
	state.LeaseRenewalMu.Unlock()
	MarkChannelHealthProbeSuccess(probeCtx, info)

	nextCtx, _ := gin.CreateTestContext(nil)
	next := EvaluateChannelHealthSelection(nextCtx, "coding", info.OriginModelName, info.ChannelMeta.ChannelId)
	require.True(t, next.Allow)
	require.Equal(t, ChannelHealthSelectionHalfOpenProbe, next.Action)
	ReleaseChannelHealthProbe(nextCtx, info)
}

func TestChannelHealthExpiredProbeLeaseCannotClearNewProbe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := useChannelHealthRedisForTest(t)

	info := &relaycommon.RelayInfo{
		UsingGroup:      "coding",
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 206},
	}
	failureCtx, _ := gin.CreateTestContext(nil)
	observeChannelHealthGatewayThreshold(
		failureCtx,
		info,
		types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway),
	)
	server.FastForward(DefaultChannelHealthCooldown)

	firstProbeCtx, _ := gin.CreateTestContext(nil)
	firstProbe := EvaluateChannelHealthSelection(firstProbeCtx, "coding", "gpt-5.6-sol", 206)
	require.Equal(t, ChannelHealthSelectionHalfOpenProbe, firstProbe.Action)
	info.ChannelHealthRoute = &firstProbe.Route
	server.FastForward(defaultChannelHealthProbeLease)

	secondProbeCtx, _ := gin.CreateTestContext(nil)
	secondProbe := EvaluateChannelHealthSelection(secondProbeCtx, "coding", "gpt-5.6-sol", 206)
	require.Equal(t, ChannelHealthSelectionHalfOpenProbe, secondProbe.Action)

	MarkChannelHealthProbeSuccess(firstProbeCtx, info)
	competingCtx, _ := gin.CreateTestContext(nil)
	competing := EvaluateChannelHealthSelection(competingCtx, "coding", "gpt-5.6-sol", 206)
	require.False(t, competing.Allow)
	require.Equal(t, ChannelHealthSelectionHalfOpenBusy, competing.Action)

	MarkChannelHealthProbeSuccess(secondProbeCtx, info)
	recoveredCtx, _ := gin.CreateTestContext(nil)
	recovered := EvaluateChannelHealthSelection(recoveredCtx, "coding", "gpt-5.6-sol", 206)
	require.True(t, recovered.Allow)
	require.Equal(t, ChannelHealthSelectionHealthy, recovered.Action)
}

func TestChannelHealthConcurrentHalfOpenSelectionHasSingleProbe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := useChannelHealthRedisForTest(t)

	failureCtx, _ := gin.CreateTestContext(nil)
	observeChannelHealthGatewayThreshold(
		failureCtx,
		&relaycommon.RelayInfo{
			UsingGroup:      "coding",
			OriginModelName: "gpt-5.6-sol",
			ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 206},
		},
		types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway),
	)
	server.FastForward(DefaultChannelHealthCooldown)

	const contenders = 32
	start := make(chan struct{})
	results := make(chan ChannelHealthSelection, contenders)
	var wg sync.WaitGroup
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, _ := gin.CreateTestContext(nil)
			<-start
			results <- EvaluateChannelHealthSelection(ctx, "coding", "gpt-5.6-sol", 206)
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	probeCount := 0
	busyCount := 0
	for decision := range results {
		switch decision.Action {
		case ChannelHealthSelectionHalfOpenProbe:
			probeCount++
		case ChannelHealthSelectionHalfOpenBusy:
			busyCount++
		default:
			t.Fatalf("unexpected concurrent selection action: %s", decision.Action)
		}
	}
	require.Equal(t, 1, probeCount)
	require.Equal(t, contenders-1, busyCount)
}

func TestChannelHealthAutoGroupStateIsolatedBySelectedGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useChannelHealthRedisForTest(t)

	failureCtx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(failureCtx, constant.ContextKeyAutoGroup, "premium")
	info := &relaycommon.RelayInfo{
		UsingGroup:      "auto",
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 206},
	}
	observeChannelHealthGatewayThreshold(failureCtx, info, types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway))

	premiumCtx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(premiumCtx, constant.ContextKeyAutoGroup, "premium")
	premium := EvaluateChannelHealthSelection(premiumCtx, "auto", "gpt-5.6-sol", 206)
	require.False(t, premium.Allow)
	require.Equal(t, ChannelHealthSelectionCooldown, premium.Action)

	defaultCtx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(defaultCtx, constant.ContextKeyAutoGroup, "default")
	defaultGroup := EvaluateChannelHealthSelection(defaultCtx, "auto", "gpt-5.6-sol", 206)
	require.True(t, defaultGroup.Allow)
	require.Equal(t, ChannelHealthSelectionHealthy, defaultGroup.Action)
}
