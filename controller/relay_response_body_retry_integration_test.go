package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const responsesReadBodyRetryModel = "responses-read-body-retry-integration-test"

func TestRelayRetriesResponsesAfterUnreadableUpstreamBodyBeforeDownstreamWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupChannelControllerTestDB(t)
	configureResponsesReadBodyRetryRelay(t)

	var firstCalls atomic.Int32
	var secondCalls atomic.Int32
	firstUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		firstCalls.Add(1)
		writeTruncatedResponsesBody(w)
	}))
	defer firstUpstream.Close()
	secondUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		secondCalls.Add(1)
		writeSuccessfulResponsesBody(w)
	}))
	defer secondUpstream.Close()

	first, second := seedResponsesReadBodyRetryChannels(t, db, firstUpstream.URL, secondUpstream.URL)
	ctx, recorder := newResponsesReadBodyRetryContext(t, first, context.Background(), visibleResponsesReadBodyRetryRequest())

	Relay(ctx, types.RelayFormatOpenAIResponses)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, int32(1), firstCalls.Load())
	require.Equal(t, int32(1), secondCalls.Load())
	require.Equal(t, []string{"4101", "4102"}, ctx.GetStringSlice("use_channel"))
	require.JSONEq(t, successfulResponsesBody, recorder.Body.String())
	require.False(t, relaycommon.IsResponsesOpaqueEncryptedReplay(ctx))
	require.Equal(t, second.Id, common.GetContextKeyInt(ctx, constant.ContextKeyChannelId))
}

func TestRelayDoesNotRetryOpaqueResponsesAfterUnreadableUpstreamBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupChannelControllerTestDB(t)
	configureResponsesReadBodyRetryRelay(t)

	var firstCalls atomic.Int32
	var secondCalls atomic.Int32
	firstUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		firstCalls.Add(1)
		writeTruncatedResponsesBody(w)
	}))
	defer firstUpstream.Close()
	secondUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		secondCalls.Add(1)
		writeSuccessfulResponsesBody(w)
	}))
	defer secondUpstream.Close()

	first, _ := seedResponsesReadBodyRetryChannels(t, db, firstUpstream.URL, secondUpstream.URL)
	first.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialNewAPI,
	})
	require.NoError(t, db.Save(first).Error)
	ctx, recorder := newResponsesReadBodyRetryContext(t, first, context.Background(), opaqueResponsesReadBodyRetryRequest())

	Relay(ctx, types.RelayFormatOpenAIResponses)

	require.GreaterOrEqual(t, recorder.Code, http.StatusInternalServerError)
	require.Equal(t, int32(1), firstCalls.Load())
	require.Zero(t, secondCalls.Load())
	require.Equal(t, []string{"4101"}, ctx.GetStringSlice("use_channel"))
	require.True(t, relaycommon.IsResponsesOpaqueEncryptedReplay(ctx))
	require.Equal(t, "suppressed_cross_channel", common.GetContextKeyString(ctx, constant.ContextKey("responses_encrypted_reasoning_replay_retry_action")))
	requireResponsesReadBodyRetryQuotaRestored(t, db)
}

func TestRelayDoesNotRetryUnreadableUpstreamBodyAfterClientCancellation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupChannelControllerTestDB(t)
	configureResponsesReadBodyRetryRelay(t)

	var firstCalls atomic.Int32
	var secondCalls atomic.Int32
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var releaseOnce sync.Once
	unblockFirst := func() {
		releaseOnce.Do(func() {
			close(releaseFirst)
		})
	}
	t.Cleanup(unblockFirst)
	firstUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		firstCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "512")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resp_partial"`))
		w.(http.Flusher).Flush()
		close(firstStarted)
		<-releaseFirst
	}))
	defer firstUpstream.Close()
	secondUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		secondCalls.Add(1)
		writeSuccessfulResponsesBody(w)
	}))
	defer secondUpstream.Close()

	first, _ := seedResponsesReadBodyRetryChannels(t, db, firstUpstream.URL, secondUpstream.URL)
	requestContext, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ctx, recorder := newResponsesReadBodyRetryContext(t, first, requestContext, visibleResponsesReadBodyRetryRequest())
	relayDone := make(chan struct{})
	go func() {
		Relay(ctx, types.RelayFormatOpenAIResponses)
		close(relayDone)
	}()

	select {
	case <-firstStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first upstream request did not start")
	}
	cancel()
	unblockFirst()
	select {
	case <-relayDone:
	case <-time.After(2 * time.Second):
		t.Fatal("relay did not finish after upstream body closed")
	}

	require.GreaterOrEqual(t, recorder.Code, http.StatusInternalServerError)
	require.Equal(t, int32(1), firstCalls.Load())
	require.Zero(t, secondCalls.Load())
	require.Equal(t, []string{"4101"}, ctx.GetStringSlice("use_channel"))
	requireResponsesReadBodyRetryQuotaRestored(t, db)
}

func configureResponsesReadBodyRetryRelay(t *testing.T) {
	t.Helper()
	previousRetryTimes := common.RetryTimes
	previousErrorLogEnabled := constant.ErrorLogEnabled
	previousCountToken := constant.CountToken
	common.RetryTimes = 1
	constant.ErrorLogEnabled = false
	constant.CountToken = false
	service.InitHttpClient()
	t.Cleanup(func() {
		common.RetryTimes = previousRetryTimes
		constant.ErrorLogEnabled = previousErrorLogEnabled
		constant.CountToken = previousCountToken
		service.InitHttpClient()
	})
}

func seedResponsesReadBodyRetryChannels(t *testing.T, db *gorm.DB, firstURL string, secondURL string) (*model.Channel, *model.Channel) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&model.Token{}))
	priority := int64(10)
	weight := uint(1)
	autoBan := 0
	channels := []*model.Channel{
		{
			Id:       4101,
			Name:     "responses-read-body-first",
			Key:      "test-key-4101",
			Type:     constant.ChannelTypeOpenAI,
			Status:   common.ChannelStatusEnabled,
			Group:    "default",
			Models:   responsesReadBodyRetryModel,
			BaseURL:  &firstURL,
			Priority: &priority,
			Weight:   &weight,
			AutoBan:  &autoBan,
		},
		{
			Id:       4102,
			Name:     "responses-read-body-second",
			Key:      "test-key-4102",
			Type:     constant.ChannelTypeOpenAI,
			Status:   common.ChannelStatusEnabled,
			Group:    "default",
			Models:   responsesReadBodyRetryModel,
			BaseURL:  &secondURL,
			Priority: &priority,
			Weight:   &weight,
			AutoBan:  &autoBan,
		},
	}
	for _, channel := range channels {
		require.NoError(t, db.Create(channel).Error)
		require.NoError(t, db.Create(&model.Ability{
			Group:     "default",
			Model:     responsesReadBodyRetryModel,
			ChannelId: channel.Id,
			Enabled:   true,
			Priority:  channel.Priority,
			Weight:    *channel.Weight,
		}).Error)
	}
	require.Contains(t, model.GetGroupEnabledModels("default"), responsesReadBodyRetryModel)
	require.NoError(t, db.Create(&model.User{
		Id:       1,
		Username: "responses-read-body-retry-user",
		Password: "test-password",
		Quota:    1_000_000,
		Group:    "default",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Token{
		Id:          1,
		UserId:      1,
		Key:         "test-token-1",
		Name:        "responses-read-body-retry-token",
		Status:      common.TokenStatusEnabled,
		ExpiredTime: -1,
		RemainQuota: 1_000_000,
		Group:       "default",
	}).Error)
	return channels[0], channels[1]
}

func newResponsesReadBodyRetryContext(t *testing.T, selectedChannel *model.Channel, requestContext context.Context, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequestWithContext(requestContext, http.MethodPost, "/v1/responses", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("relay_mode", relayconstant.RelayModeResponses)
	common.SetContextKey(ctx, constant.ContextKeyUserId, 1)
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenId, 1)
	common.SetContextKey(ctx, constant.ContextKeyTokenKey, "test-token-1")
	common.SetContextKey(ctx, constant.ContextKeyUserSetting, dto.UserSetting{
		AcceptUnsetRatioModel: true,
		BillingPreference:     "wallet_only",
	})
	setupErr := middleware.SetupContextForSelectedChannel(ctx, selectedChannel, responsesReadBodyRetryModel)
	if setupErr != nil {
		t.Fatalf("set selected channel: status=%d code=%q error=%q", setupErr.StatusCode, setupErr.GetErrorCode(), setupErr.Error())
	}
	return ctx, recorder
}

func requireResponsesReadBodyRetryQuotaRestored(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.Eventually(t, func() bool {
		var user model.User
		var token model.Token
		if err := db.First(&user, 1).Error; err != nil {
			return false
		}
		if err := db.First(&token, 1).Error; err != nil {
			return false
		}
		return user.Quota == 1_000_000 && token.RemainQuota == 1_000_000
	}, time.Second, 10*time.Millisecond, "asynchronous billing refund did not finish before test cleanup")
}

func writeTruncatedResponsesBody(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", "512")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"id":"resp_truncated"`))
}

func writeSuccessfulResponsesBody(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(successfulResponsesBody))
}

func visibleResponsesReadBodyRetryRequest() string {
	return `{"model":"` + responsesReadBodyRetryModel + `","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"ping"}]}]}`
}

func opaqueResponsesReadBodyRetryRequest() string {
	return `{"model":"` + responsesReadBodyRetryModel + `","input":[{"type":"reasoning","encrypted_content":"opaque-state"}]}`
}

const successfulResponsesBody = `{"id":"resp_ok","object":"response","model":"responses-read-body-retry-integration-test","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`
