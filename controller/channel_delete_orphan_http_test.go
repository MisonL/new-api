package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// The model-level tests prove the delete functions remove abilities. This one
// drives the real HTTP handlers, because the orphan is only reachable through
// the endpoints and the handlers are what an administrator actually calls.
func TestDeleteChannelHandlerRemovesAbilities(t *testing.T) {
	db := setupChannelControllerTestDB(t)

	const id = 91001
	tag := "http-delete-orphan"
	priority := int64(10)
	require.NoError(t, db.Create(&model.Channel{
		Id: id, Name: "http-delete-orphan", Status: common.ChannelStatusEnabled,
		Group: "default", Models: "http-delete-orphan-model",
		Tag: &tag, Priority: &priority, Weight: common.GetPointer(uint(1)),
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "default", Model: "http-delete-orphan-model", ChannelId: id,
		Enabled: true, Priority: &priority, Weight: 1, Tag: &tag,
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(id)}}

	DeleteChannel(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`)

	var channelCount, abilityCount int64
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", id).Count(&channelCount).Error)
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", id).Count(&abilityCount).Error)
	require.Equal(t, int64(0), channelCount)
	require.Equal(t, int64(0), abilityCount,
		"DELETE /api/channel/:id left the channel's abilities behind")
}

func TestDeleteDisabledChannelHandlerRemovesAbilities(t *testing.T) {
	db := setupChannelControllerTestDB(t)

	tag := "http-delete-disabled-orphan"
	priority := int64(10)
	ids := []int{91002, 91003}
	for _, id := range ids {
		require.NoError(t, db.Create(&model.Channel{
			Id: id, Name: "http-delete-disabled-orphan",
			Status: common.ChannelStatusManuallyDisabled,
			Group:  "default", Models: "http-delete-disabled-orphan-model",
			Tag: &tag, Priority: &priority, Weight: common.GetPointer(uint(1)),
		}).Error)
		// A disabled ability row is the shape that would win the insert conflict
		// and make a later channel reusing this id unroutable.
		require.NoError(t, db.Create(&model.Ability{
			Group: "default", Model: "http-delete-disabled-orphan-model", ChannelId: id,
			Enabled: false, Priority: &priority, Weight: 1, Tag: &tag,
		}).Error)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	DeleteDisabledChannel(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`)

	for _, id := range ids {
		var abilityCount int64
		require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", id).Count(&abilityCount).Error)
		require.Equal(t, int64(0), abilityCount,
			"DELETE /api/channel/disabled left channel %d's abilities behind", id)
	}
}
