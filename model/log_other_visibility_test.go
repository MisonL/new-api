package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// 普通用户的日志列表和日志详情会原样返回 other。渠道身份、参数覆盖审计、
// 请求头策略审计和渠道拦截原因在 detail schema 中都标记为管理员专用，
// 前端仅靠 isAdmin 隐藏，API 仍会下发；这里校验服务端已把它们剔除。
func TestUserLogsHideAdminOnlyOtherFields(t *testing.T) {
	truncateTables(t)

	require.NoError(t, DB.Create(&User{
		Id:          911,
		Username:    "log-other-user",
		Password:    "password123",
		DisplayName: "log-other-user",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}).Error)

	adminOnlyOther := map[string]interface{}{
		"channel_id":            150,
		"channel_name":          "mdkj.lol",
		"channel_type":          1,
		"po":                    []string{"set system = internal prompt"},
		"request_header_policy": map[string]interface{}{"mode": "profile"},
		"header_policy_mode":    "profile",
		"reject_reason":         "gemini_block_reason=SAFETY",
		"request_conversion":    []string{"openai", "claude"},
		"admin_info":            map[string]interface{}{"use_channel": []string{"150"}},
		"upstream_request_path": "/v1/messages",
		"channel_capability_snapshot": map[string]interface{}{
			"profile": "provider-private",
		},
	}
	userVisibleOther := map[string]interface{}{
		"model_ratio":      1.5,
		"completion_ratio": 4.0,
		"request_path":     "/v1/chat/completions",
		"status_code":      float64(200),
		"error_code":       "",
	}
	merged := make(map[string]interface{}, len(adminOnlyOther)+len(userVisibleOther))
	for k, v := range adminOnlyOther {
		merged[k] = v
	}
	for k, v := range userVisibleOther {
		merged[k] = v
	}

	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    911,
		Username:  "log-other-user",
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeConsume,
		Content:   "request completed",
		ModelName: "gpt-5.6-sol",
		ChannelId: 150,
		TokenName: "token-a",
		Group:     "default",
		RequestId: "req-log-other",
		Other:     common.MapToJsonStr(merged),
	}).Error)

	userLogs, total, err := GetUserLogs(911, LogTypeConsume, 0, 0, "", false, "", 0, 10, "", "", "", false, false)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, userLogs, 1)

	userOther, err := common.StrToMap(userLogs[0].Other)
	require.NoError(t, err)
	for key := range adminOnlyOther {
		require.NotContains(t, userOther, key, "普通用户的日志不得下发管理员专用字段 %s", key)
	}
	for key, want := range userVisibleOther {
		require.Equal(t, want, userOther[key], "普通用户仍需看到 %s", key)
	}

	// 管理员的日志视图必须保持原样，否则排障信息会一起丢掉。
	adminLogs, total, err := GetAllLogs(LogTypeConsume, 0, 0, "", false, "", "", 0, 10, 0, "", "", "", false, false)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, adminLogs, 1)
	adminOther, err := common.StrToMap(adminLogs[0].Other)
	require.NoError(t, err)
	for key := range adminOnlyOther {
		require.Contains(t, adminOther, key, "管理员视图必须保留 %s", key)
	}
}
