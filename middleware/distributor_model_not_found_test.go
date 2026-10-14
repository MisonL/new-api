package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// A model with no enabled ability in the caller's group can never be routed, so
// the distributor answered a retryable 503 for a request that could never
// succeed. It is a client mistake and belongs in 404 model_not_found.
func TestModelHasEnabledAbilityInGroupsDistinguishesRoutableModels(t *testing.T) {
	db := setupDistributorBootstrapTestDB(t)

	channel := &model.Channel{
		Id:     1201,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "test-key",
		Status: common.ChannelStatusEnabled,
		Name:   "routable-channel",
		Models: "gpt-5.5",
		Group:  "default",
	}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, channel.AddAbilities(nil))
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	require.True(t, modelHasEnabledAbilityInGroups(ctx, "default", "gpt-5.5"))
	require.False(t, modelHasEnabledAbilityInGroups(ctx, "default", "no-such-model-xyz"))
	require.False(t, modelHasEnabledAbilityInGroups(ctx, "default", ""))
	require.False(t, modelHasEnabledAbilityInGroups(ctx, "group-without-abilities", "gpt-5.5"))
}

func TestAbortNoRoutableModelUsesNotFoundForUnknownModel(t *testing.T) {
	db := setupDistributorBootstrapTestDB(t)

	channel := &model.Channel{
		Id:     1202,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "test-key",
		Status: common.ChannelStatusEnabled,
		Name:   "routable-channel-2",
		Models: "gpt-5.5",
		Group:  "default",
	}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, channel.AddAbilities(nil))
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	abortNoRoutableModel(ctx, "default", "no-such-model-xyz")

	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.Contains(t, recorder.Body.String(), "model_not_found")
	require.True(t, ctx.IsAborted())
}

// A model that exists but currently has no free channel is a transient capacity
// problem: it keeps its 503 so the caller knows to retry.
func TestAbortNoRoutableModelKeepsServiceUnavailableForKnownModel(t *testing.T) {
	db := setupDistributorBootstrapTestDB(t)

	channel := &model.Channel{
		Id:     1203,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "test-key",
		Status: common.ChannelStatusEnabled,
		Name:   "routable-channel-3",
		Models: "gpt-5.5",
		Group:  "default",
	}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, channel.AddAbilities(nil))
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	abortNoRoutableModel(ctx, "default", "gpt-5.5")

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Contains(t, recorder.Body.String(), "get_channel_failed")
	require.True(t, strings.Contains(recorder.Body.String(), "gpt-5.5"))
}
