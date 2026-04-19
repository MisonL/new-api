package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestRecordLogWithAdminInfoKeepsAuditForAdminsOnly(t *testing.T) {
	truncateTables(t)

	require.NoError(t, DB.Create(&User{
		Id:          101,
		Username:    "target-user",
		Password:    "password123",
		DisplayName: "target-user",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}).Error)

	RecordLogWithAdminInfo(101, LogTypeManage, "管理员增加用户额度 1000", map[string]interface{}{
		"admin_id":       7,
		"admin_username": "root-admin",
	})

	adminLogs, total, err := GetAllLogs(LogTypeManage, 0, 0, "", "", "", 0, 10, 0, "", "")
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, adminLogs, 1)
	require.Contains(t, adminLogs[0].Other, "admin_info")
	require.Equal(t, "管理员增加用户额度 1000", adminLogs[0].Content)

	userLogs, total, err := GetUserLogs(101, LogTypeManage, 0, 0, "", "", 0, 10, "", "")
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, userLogs, 1)
	require.NotContains(t, userLogs[0].Other, "admin_info")
	require.Equal(t, "管理员增加用户额度 1000", userLogs[0].Content)
}

func TestRecordTopupLogStoresAdminOnlyAuditFields(t *testing.T) {
	truncateTables(t)

	previousNodeName := common.NodeName
	common.NodeName = "dev-node-a"
	t.Cleanup(func() {
		common.NodeName = previousNodeName
	})

	require.NoError(t, DB.Create(&User{
		Id:          102,
		Username:    "topup-user",
		Password:    "password123",
		DisplayName: "topup-user",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}).Error)

	RecordTopupLog(102, "使用在线充值成功", "198.51.100.23", "stripe", "stripe")

	var stored Log
	require.NoError(t, DB.Last(&stored).Error)
	require.Equal(t, LogTypeTopup, stored.Type)
	require.Equal(t, "198.51.100.23", stored.Ip)

	other, err := common.StrToMap(stored.Other)
	require.NoError(t, err)
	adminInfoRaw, ok := other["admin_info"]
	require.True(t, ok)

	adminInfo, ok := adminInfoRaw.(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "198.51.100.23", adminInfo["caller_ip"])
	require.Equal(t, "stripe", adminInfo["payment_method"])
	require.Equal(t, "stripe", adminInfo["callback_payment_method"])
	require.Equal(t, common.Version, adminInfo["version"])
	require.Equal(t, "dev-node-a", adminInfo["node_name"])
	require.Contains(t, adminInfo, "server_ip")

	userLogs, total, err := GetUserLogs(102, LogTypeTopup, 0, 0, "", "", 0, 10, "", "")
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, userLogs, 1)
	require.NotContains(t, userLogs[0].Other, "admin_info")
}
