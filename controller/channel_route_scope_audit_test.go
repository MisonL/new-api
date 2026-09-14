package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRecordChannelRouteScopeAuditKeepsOldAndNewSnapshotsForAdmins(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	require.NoError(t, db.Create(&model.User{
		Id:          9001,
		Username:    "route-admin",
		Password:    "password123",
		DisplayName: "route-admin",
		Role:        common.RoleAdminUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}).Error)

	oldScope := &model.ChannelRouteScope{
		ID:                  "scope-a",
		Group:               "default",
		EffectiveRouteModel: "provider-model",
		RoutePriority:       3,
		Version:             "old-version",
		Items: []model.ChannelRouteScopeItem{{
			ChannelID:      1,
			ChannelName:    "provider",
			ChannelType:    1,
			ChannelStatus:  common.ChannelStatusEnabled,
			AbilityEnabled: true,
			RoutePriority:  3,
			RouteWeightBPS: 10000,
			MaxConcurrency: 2,
		}},
	}
	newScope := &model.ChannelRouteScope{
		ID:                  "scope-a",
		Group:               "default",
		EffectiveRouteModel: "provider-model",
		RoutePriority:       4,
		Version:             "new-version",
		Items: []model.ChannelRouteScopeItem{{
			ChannelID:      1,
			ChannelName:    "provider",
			ChannelType:    1,
			ChannelStatus:  common.ChannelStatusEnabled,
			AbilityEnabled: true,
			RoutePriority:  4,
			RouteWeightBPS: 10000,
			MaxConcurrency: 2,
		}},
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 9001)
	ctx.Set("username", "route-admin")
	recordChannelRouteScopeAudit(ctx, "priority", "", oldScope, newScope)

	var stored model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeManage).Last(&stored).Error)
	other, err := common.StrToMap(stored.Other)
	require.NoError(t, err)
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	require.EqualValues(t, 9001, adminInfo["admin_id"])
	require.Equal(t, "route-admin", adminInfo["admin_username"])
	audit, ok := adminInfo["route_scope_audit"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "priority", audit["operation"])
	require.Equal(t, "old-version", audit["old_version"])
	require.Equal(t, "new-version", audit["new_version"])
	require.Equal(t, "scope-a", audit["old_scope_id"])
	require.Equal(t, "scope-a", audit["new_scope_id"])
	require.Equal(t, "default", audit["group"])
	require.NotEmpty(t, audit["old_items"])
	require.NotEmpty(t, audit["new_items"])
}
