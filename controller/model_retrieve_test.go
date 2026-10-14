package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newRetrieveModelContext(t *testing.T, modelId string, userId int) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models/"+modelId, nil)
	ctx.Params = gin.Params{{Key: "model", Value: modelId}}
	if userId > 0 {
		ctx.Set("id", userId)
	}
	return ctx, recorder
}

// RetrieveModel answered from openAIModelsMap, the static catalogue built from
// each adaptor's hardcoded GetModelList(). A model the gateway serves but that
// is absent from that catalogue came back as 200 + "does not exist", which an
// OpenAI SDK treats as a successful lookup.
func TestRetrieveModelReturnsRoutableModelAbsentFromStaticCatalogue(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Create(&model.User{
		Id:       2001,
		Username: "retrieve-user",
		Password: "password",
		Group:    "default",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id: 11, Name: "routable", Key: "sk-routable",
		Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "default", Model: "grok-4.6", ChannelId: 11, Enabled: true,
	}).Error)
	require.Contains(t, model.GetGroupEnabledModels("default"), "grok-4.6")

	ctx, recorder := newRetrieveModelContext(t, "grok-4.6", 2001)
	RetrieveModel(ctx, constant.ChannelTypeOpenAI)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"id":"grok-4.6"`)
	require.NotContains(t, recorder.Body.String(), "does not exist")
}

// A model the request cannot route is a 404. Returning 200 with an error object
// made the failure indistinguishable from success for SDK callers.
func TestRetrieveModelReturnsNotFoundForUnroutableModel(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Create(&model.User{
		Id:       2002,
		Username: "retrieve-user-2",
		Password: "password",
		Group:    "default",
		Status:   common.UserStatusEnabled,
	}).Error)

	ctx, recorder := newRetrieveModelContext(t, "no-such-model-xyz", 2002)
	RetrieveModel(ctx, constant.ChannelTypeOpenAI)

	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.Contains(t, recorder.Body.String(), "model_not_found")
	require.Contains(t, recorder.Body.String(), "does not exist")
}

// A channel that is disabled must not make its models look routable.
func TestRetrieveModelIgnoresDisabledChannelAbilities(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Create(&model.User{
		Id:       2003,
		Username: "retrieve-user-3",
		Password: "password",
		Group:    "default",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id: 12, Name: "disabled", Key: "sk-disabled",
		Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusManuallyDisabled,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "default", Model: "claude-opus-4", ChannelId: 12, Enabled: true,
	}).Error)

	ctx, recorder := newRetrieveModelContext(t, "claude-opus-4", 2003)
	RetrieveModel(ctx, constant.ChannelTypeOpenAI)

	require.Equal(t, http.StatusNotFound, recorder.Code)
}
