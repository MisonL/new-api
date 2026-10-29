package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// 令牌编辑表单只在打开抽屉时读取一次，保存时把当时看到的 remain_quota 原样
// 回传。固定用法是「只改名字」，此时请求体里的额度是打开抽屉那一刻的快照，
// 若整列写回，抽屉打开期间消耗掉的额度被静默恢复。
// 契约：请求体未携带 remain_quota 时，更新不得触碰该列。
func TestUpdateTokenWithoutRemainQuotaKeepsConsumedQuota(t *testing.T) {
	db := setupTokenControllerTestDB(t)

	token := model.Token{
		UserId:      1,
		Key:         "update-keep-quota-key",
		Name:        "rename-me",
		Status:      common.TokenStatusEnabled,
		RemainQuota: 100000,
	}
	require.NoError(t, db.Create(&token).Error)

	// 抽屉打开期间该令牌正常服务，扣掉 30000。
	require.NoError(t, db.Model(&model.Token{}).Where("id = ?", token.Id).
		Updates(map[string]any{"remain_quota": 70000, "used_quota": 30000}).Error)

	ctx, recorder := newAuthenticatedContext(t, http.MethodPut, "/api/token/", map[string]any{
		"id":              token.Id,
		"name":            "renamed",
		"unlimited_quota": false,
		"group":           "",
	}, 1)

	UpdateToken(ctx)

	response := decodeAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)

	var stored model.Token
	require.NoError(t, db.Where("id = ?", token.Id).First(&stored).Error)
	require.Equal(t, "renamed", stored.Name)
	require.Equal(t, 70000, stored.RemainQuota,
		"an update that does not carry remain_quota must not restore consumed quota")
}

// 管理端确实修改额度时必须照常写入，修复不能以「忽略该字段」为代价。
func TestUpdateTokenWithRemainQuotaAppliesIt(t *testing.T) {
	db := setupTokenControllerTestDB(t)

	token := model.Token{
		UserId:      1,
		Key:         "update-apply-quota-key",
		Name:        "quota-edit",
		Status:      common.TokenStatusEnabled,
		RemainQuota: 1000,
	}
	require.NoError(t, db.Create(&token).Error)

	ctx, recorder := newAuthenticatedContext(t, http.MethodPut, "/api/token/", map[string]any{
		"id":              token.Id,
		"name":            "quota-edit",
		"remain_quota":    500000,
		"unlimited_quota": false,
		"group":           "",
	}, 1)

	UpdateToken(ctx)

	response := decodeAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)

	var stored model.Token
	require.NoError(t, db.Where("id = ?", token.Id).First(&stored).Error)
	require.Equal(t, 500000, stored.RemainQuota)
}

// status_only 切换同样不该整列写回：它只该改 status。
func TestUpdateTokenStatusOnlyKeepsConsumedQuota(t *testing.T) {
	db := setupTokenControllerTestDB(t)

	token := model.Token{
		UserId:      1,
		Key:         "status-only-quota-key",
		Name:        "toggle-me",
		Status:      common.TokenStatusEnabled,
		RemainQuota: 5000,
	}
	require.NoError(t, db.Create(&token).Error)

	require.NoError(t, db.Model(&model.Token{}).Where("id = ?", token.Id).
		Update("remain_quota", 1234).Error)

	ctx, recorder := newAuthenticatedContext(t, http.MethodPut, "/api/token/?status_only=true", map[string]any{
		"id":     token.Id,
		"status": common.TokenStatusDisabled,
	}, 1)

	UpdateToken(ctx)

	response := decodeAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)

	var stored model.Token
	require.NoError(t, db.Where("id = ?", token.Id).First(&stored).Error)
	require.Equal(t, common.TokenStatusDisabled, stored.Status)
	require.Equal(t, 1234, stored.RemainQuota)
}
