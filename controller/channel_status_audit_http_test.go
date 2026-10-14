package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// The model-level tests prove a status change is audited. This one drives the
// real handler, because the actor is read from the gin context that
// middleware.AdminAuth populates; a handler that forgot to pass it would leave
// every operator action looking like a system one and the model tests would
// still pass.
func TestUpdateChannelStatusHandlerAuditsTheOperator(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))

	const id = 92001
	priority := int64(10)
	require.NoError(t, db.Create(&model.Channel{
		Id: id, Name: "http-status-audit", Status: common.ChannelStatusEnabled,
		Group: "default", Models: "http-status-audit-model",
		Priority: &priority, Weight: common.GetPointer(uint(1)),
	}).Error)

	ctx, recorder := newChannelControllerContext(t, http.MethodPut, "/api/channel/", map[string]any{
		"id":     id,
		"status": common.ChannelStatusManuallyDisabled,
	})
	// These are the keys middleware.AdminAuth sets on an authenticated admin.
	ctx.Set("id", 77)
	ctx.Set("username", "http-audit-admin")

	UpdateChannel(ctx)

	response := decodeChannelAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)

	var entries []model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeManage).Find(&entries).Error)
	require.Len(t, entries, 1, "the handler must write exactly one audit entry")

	other, err := common.StrToMap(entries[0].Other)
	require.NoError(t, err)
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok, "the audit must carry admin_info")

	audit, ok := adminInfo["channel_status_audit"].(map[string]interface{})
	require.True(t, ok, "the audit must carry channel_status_audit")
	require.EqualValues(t, id, audit["channel_id"])
	require.Equal(t, "channel_update", audit["source"])
	require.EqualValues(t, common.ChannelStatusEnabled, audit["old_status"])
	require.EqualValues(t, common.ChannelStatusManuallyDisabled, audit["new_status"])
	require.EqualValues(t, 77, adminInfo["admin_id"])
	require.Equal(t, "http-audit-admin", adminInfo["admin_username"])
}

// The tag endpoints cover many channels at once, so the actor has to reach
// setChannelStatusByTag through a different handler. This drives that one too.
func TestDisableTagChannelsHandlerAuditsTheOperator(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))

	tag := "http-tag-status-audit"
	priority := int64(10)
	ids := []int{92002, 92003}
	for _, id := range ids {
		require.NoError(t, db.Create(&model.Channel{
			Id: id, Name: "http-tag-status-audit", Status: common.ChannelStatusEnabled,
			Group: "default", Models: "http-tag-status-audit-model",
			Tag: &tag, Priority: &priority, Weight: common.GetPointer(uint(1)),
		}).Error)
		require.NoError(t, db.Create(&model.Ability{
			Group: "default", Model: "http-tag-status-audit-model", ChannelId: id,
			Enabled: true, Priority: &priority, Weight: 1, Tag: &tag,
		}).Error)
	}

	ctx, recorder := newChannelControllerContext(t, http.MethodPost, "/api/channel/tag/disabled", map[string]any{
		"tag": tag,
	})
	ctx.Set("id", 78)
	ctx.Set("username", "http-tag-admin")

	DisableTagChannels(ctx)

	response := decodeChannelAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)

	var entries []model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeManage).Find(&entries).Error)
	require.Len(t, entries, len(ids), "each affected channel must get its own audit entry")

	for _, entry := range entries {
		other, err := common.StrToMap(entry.Other)
		require.NoError(t, err)
		adminInfo, ok := other["admin_info"].(map[string]interface{})
		require.True(t, ok)
		audit, ok := adminInfo["channel_status_audit"].(map[string]interface{})
		require.True(t, ok)
		require.Equal(t, "tag_disable", audit["source"])
		require.Equal(t, "http-tag-admin", adminInfo["admin_username"])
		require.EqualValues(t, 78, adminInfo["admin_id"])
		require.EqualValues(t, common.ChannelStatusManuallyDisabled, audit["new_status"])
	}
}

// The handlers no longer call InitChannelCache themselves: the model write ends
// with a full rebuild of its own. That removes a duplicate synchronous read of
// every channel and ability, but only if the model layer really does refresh.
// This drives the handler and checks the cache, so the simplification cannot
// silently drop the invalidation.
func TestDisableTagChannelsHandlerStillRefreshesChannelCache(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))

	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCacheEnabled })

	tag := "http-tag-cache-refresh"
	priority := int64(10)
	const id = 92004
	require.NoError(t, db.Create(&model.Channel{
		Id: id, Name: "http-tag-cache-refresh", Status: common.ChannelStatusEnabled,
		Group: "default", Models: "http-tag-cache-refresh-model",
		Tag: &tag, Priority: &priority, Weight: common.GetPointer(uint(1)),
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "default", Model: "http-tag-cache-refresh-model", ChannelId: id,
		Enabled: true, Priority: &priority, Weight: 1, Tag: &tag,
	}).Error)
	model.InitChannelCache()

	// The channel must be routable before the change, or the assertion below
	// would pass for the wrong reason.
	selected, err := model.GetRandomSatisfiedChannel("default", "http-tag-cache-refresh-model", 0)
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, id, selected.Id)

	ctx, recorder := newChannelControllerContext(t, http.MethodPost, "/api/channel/tag/disabled", map[string]any{
		"tag": tag,
	})
	ctx.Set("id", 79)
	ctx.Set("username", "http-tag-admin")

	DisableTagChannels(ctx)

	response := decodeChannelAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)

	selected, err = model.GetRandomSatisfiedChannel("default", "http-tag-cache-refresh-model", 0)
	require.NoError(t, err)
	require.Nil(t, selected,
		"the disabled channel must be gone from the cache the handler left behind")
}
