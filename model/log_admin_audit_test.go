package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
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

func TestGetAllLogsAttachesNonSensitiveChannelDetail(t *testing.T) {
	truncateTables(t)

	baseURL := "https://upstream.example"
	tag := "mynav"
	testModel := "gpt-5.5"
	openAIOrganization := "org-hidden"
	priority := int64(12)
	weight := uint(30)
	autoBan := 1
	require.NoError(t, DB.Create(&Channel{
		Id:                 301,
		Type:               constant.ChannelTypeOpenAI,
		Status:             common.ChannelStatusEnabled,
		Name:               "mynav-primary",
		Key:                "sk-hidden",
		OpenAIOrganization: &openAIOrganization,
		BaseURL:            &baseURL,
		Models:             "gpt-5.5,gpt-5.5-compact",
		Group:              "default,codex",
		Tag:                &tag,
		TestModel:          &testModel,
		ResponseTime:       280,
		Priority:           &priority,
		Weight:             &weight,
		AutoBan:            &autoBan,
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 3,
		},
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    101,
		Username:  "target-user",
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeConsume,
		Content:   "consume",
		ModelName: "gpt-5.5",
		ChannelId: 301,
		TokenName: "token-a",
		Group:     "default",
		RequestId: "req-a",
		Other:     "{}",
	}).Error)

	adminLogs, total, err := GetAllLogs(LogTypeConsume, 0, 0, "", "", "", 0, 10, 0, "", "")
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, adminLogs, 1)
	require.Equal(t, "mynav-primary", adminLogs[0].ChannelName)
	require.NotNil(t, adminLogs[0].ChannelDetail)
	require.Equal(t, 301, adminLogs[0].ChannelDetail.Id)
	require.Equal(t, "mynav-primary", adminLogs[0].ChannelDetail.Name)
	require.Equal(t, constant.ChannelTypeOpenAI, adminLogs[0].ChannelDetail.Type)
	require.Equal(t, "OpenAI", adminLogs[0].ChannelDetail.TypeName)
	require.Equal(t, common.ChannelStatusEnabled, adminLogs[0].ChannelDetail.Status)
	require.Equal(t, baseURL, adminLogs[0].ChannelDetail.BaseURL)
	require.Equal(t, "default,codex", adminLogs[0].ChannelDetail.Group)
	require.Equal(t, tag, adminLogs[0].ChannelDetail.Tag)
	require.Equal(t, testModel, adminLogs[0].ChannelDetail.TestModel)
	require.Equal(t, 280, adminLogs[0].ChannelDetail.ResponseTime)
	require.Equal(t, priority, *adminLogs[0].ChannelDetail.Priority)
	require.Equal(t, weight, *adminLogs[0].ChannelDetail.Weight)
	require.Equal(t, autoBan, *adminLogs[0].ChannelDetail.AutoBan)
	require.Equal(t, 2, adminLogs[0].ChannelDetail.ModelsCount)
	require.True(t, adminLogs[0].ChannelDetail.IsMultiKey)
	require.Equal(t, 3, adminLogs[0].ChannelDetail.MultiKeySize)

	detailJSON, err := common.Marshal(adminLogs[0].ChannelDetail)
	require.NoError(t, err)
	detailPayload := string(detailJSON)
	require.NotContains(t, detailPayload, "sk-hidden")
	require.NotContains(t, detailPayload, "org-hidden")
	require.NotContains(t, detailPayload, `"key"`)
	require.NotContains(t, detailPayload, "openai_organization")
}
