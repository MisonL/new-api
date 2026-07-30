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
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponsesToolCapabilityObservationFromContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	common.SetContextKey(c, constant.ContextKeyResponsesObservedCustomToolCall, true)
	common.SetContextKey(c, constant.ContextKeyResponsesObservedNamespaceToolCall, true)
	now := time.Unix(100, 0).UTC()

	observation, ok := responsesToolCapabilityObservationFromContext(c, now)

	require.True(t, ok)
	require.Equal(t, int64(100), observation.ObservedAt)
	require.True(t, observation.CustomToolCall)
	require.True(t, observation.NamespaceToolCall)
	require.False(t, observation.ToolSearchCall)
}

func TestResponsesToolCapabilityObservationFromContextRequiresEvidence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)

	_, ok := responsesToolCapabilityObservationFromContext(c, time.Unix(100, 0).UTC())

	require.False(t, ok)
}

func TestRecordResponsesStreamOptionsIncludeUsageCapabilityObservationAfterRecovery(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	channel := &model.Channel{
		Id:     8620,
		Name:   "stream-options-observation",
		Key:    "test-key",
		Status: 1,
		Group:  "default",
		Models: "gpt-5.6-sol",
	}
	require.NoError(t, db.Create(channel).Error)

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	common.SetContextKey(c, constant.ContextKeyResponsesStreamOptionsIncludeUsageRetry, true)
	common.SetContextKey(c, constant.ContextKeyResponsesStreamOptionsIncludeUsageRetryChannelID, channel.Id)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   channel.Id,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}

	recordResponsesStreamOptionsIncludeUsageCapabilityObservation(c, info)

	require.True(t, info.ChannelOtherSettings.ExplicitlyDisablesResponsesStreamOptionsIncludeUsage())
	var persisted model.Channel
	require.NoError(t, db.First(&persisted, channel.Id).Error)
	persistedSettings := persisted.GetOtherSettings()
	require.True(t, persistedSettings.ExplicitlyDisablesResponsesStreamOptionsIncludeUsage())
}

func TestRecordResponsesStreamOptionsIncludeUsageCapabilityObservationDoesNotCrossChannels(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	channel := &model.Channel{
		Id:     8621,
		Name:   "stream-options-observation-other-channel",
		Key:    "test-key",
		Status: 1,
		Group:  "default",
		Models: "gpt-5.6-sol",
	}
	require.NoError(t, db.Create(channel).Error)

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	common.SetContextKey(c, constant.ContextKeyResponsesStreamOptionsIncludeUsageRetry, true)
	common.SetContextKey(c, constant.ContextKeyResponsesStreamOptionsIncludeUsageRetryChannelID, 8622)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   channel.Id,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}

	recordResponsesStreamOptionsIncludeUsageCapabilityObservation(c, info)

	var persisted model.Channel
	require.NoError(t, db.First(&persisted, channel.Id).Error)
	require.Nil(t, persisted.GetOtherSettings().ResponsesCapabilityRegistry)
}

func TestRecordChannelRequestBodyLimitSkipsRecoverableRemoteCompaction413(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	previousPolicy := model_setting.GetGlobalSettings().RequestBodyLimitPolicy
	model_setting.GetGlobalSettings().RequestBodyLimitPolicy = model_setting.RequestBodyLimitPolicy{Enabled: true}
	t.Cleanup(func() {
		model_setting.GetGlobalSettings().RequestBodyLimitPolicy = previousPolicy
	})
	channel := &model.Channel{
		Id:     8623,
		Name:   "remote-compaction-413",
		Key:    "test-key",
		Status: 1,
		Group:  "default",
		Models: "gpt-5.6-sol",
	}
	require.NoError(t, db.Create(channel).Error)

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("X-Codex-Beta-Features", "remote_compaction_v2")
	common.SetContextKey(c, constant.ContextKeyRequestBodySize, int64(12345))
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		IsStream:        true,
		OriginModelName: "gpt-5.6-sol",
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.6-sol",
			Input: common.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"compact"}]},
				{"type":"compaction_trigger"}
			]`),
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   channel.Id,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	err := types.NewOpenAIError(
		errors.New("upstream request too large"),
		types.ErrorCodeUpstreamRequestTooLarge,
		http.StatusRequestEntityTooLarge,
	)

	recordChannelRequestBodyLimit(c, info, err)

	var persisted model.Channel
	require.NoError(t, db.First(&persisted, channel.Id).Error)
	require.Nil(t, persisted.GetOtherSettings().RequestBodyLimit)
}

func TestRecordChannelRequestBodyLimitSkipsOpaqueResponsesReplay(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	previousPolicy := model_setting.GetGlobalSettings().RequestBodyLimitPolicy
	model_setting.GetGlobalSettings().RequestBodyLimitPolicy = model_setting.RequestBodyLimitPolicy{Enabled: true}
	t.Cleanup(func() {
		model_setting.GetGlobalSettings().RequestBodyLimitPolicy = previousPolicy
	})
	channel := &model.Channel{
		Id:     8624,
		Name:   "opaque-replay-413",
		Key:    "test-key",
		Status: 1,
		Group:  "default",
		Models: "gpt-5.6-sol",
	}
	require.NoError(t, db.Create(channel).Error)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetResponsesRequestMetadata(c, common.ResponsesRequestMetadata{
		Model:                     "gpt-5.6-sol",
		ModelPresent:              true,
		InputPresent:              true,
		InputKind:                 common.ResponsesJSONValueKindArray,
		HasOpaqueEncryptedContent: true,
	})
	common.SetContextKey(c, constant.ContextKeyRequestBodySize, int64(12345))
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   channel.Id,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	err := types.NewOpenAIError(
		errors.New("upstream request too large"),
		types.ErrorCodeUpstreamRequestTooLarge,
		http.StatusRequestEntityTooLarge,
	)

	recordChannelRequestBodyLimit(c, info, err)

	var persisted model.Channel
	require.NoError(t, db.First(&persisted, channel.Id).Error)
	require.Nil(t, persisted.GetOtherSettings().RequestBodyLimit)
}

func TestRecordChannelRequestBodyLimitRecordsNonOpaqueResponsesReplay(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	previousPolicy := model_setting.GetGlobalSettings().RequestBodyLimitPolicy
	model_setting.GetGlobalSettings().RequestBodyLimitPolicy = model_setting.RequestBodyLimitPolicy{Enabled: true}
	t.Cleanup(func() {
		model_setting.GetGlobalSettings().RequestBodyLimitPolicy = previousPolicy
	})
	channel := &model.Channel{
		Id:     8625,
		Name:   "non-opaque-replay-413",
		Key:    "test-key",
		Status: 1,
		Group:  "default",
		Models: "gpt-5.6-sol",
	}
	require.NoError(t, db.Create(channel).Error)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyRequestBodySize, int64(12345))
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.6-sol",
			Input: common.RawMessage(`[{"type":"message","role":"user","content":"continue"}]`),
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   channel.Id,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	err := types.NewOpenAIError(
		errors.New("upstream request too large"),
		types.ErrorCodeUpstreamRequestTooLarge,
		http.StatusRequestEntityTooLarge,
	)

	recordChannelRequestBodyLimit(c, info, err)

	var persisted model.Channel
	require.NoError(t, db.First(&persisted, channel.Id).Error)
	limit := persisted.GetOtherSettings().RequestBodyLimit
	require.NotNil(t, limit)
	require.EqualValues(t, 12345, limit.MaxBytes)
	require.Equal(t, "upstream_413", limit.Source)
}
