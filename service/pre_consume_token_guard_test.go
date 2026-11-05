package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

// PreConsumeTokenQuota 的准入此前是"读 remain_quota → Go 里比较 → 相对扣减"
// 三步：并发请求都能通过检查后各自扣减，令牌预算会被扣成负数。这里用
// "扣减未命中任何行"的确定性场景锁住接线——更新 0 行必须报错，不能静默成功。
func TestPreConsumeTokenQuotaFailsWhenTokenRowIsNotUpdated(t *testing.T) {
	truncate(t)
	model.KeyColumn()

	token := &model.Token{
		UserId:      1,
		Key:         "preconsume-guard-key",
		Name:        "preconsume-guard",
		Status:      common.TokenStatusEnabled,
		RemainQuota: 1000,
	}
	require.NoError(t, model.DB.Create(token).Error)

	info := &relaycommon.RelayInfo{TokenId: token.Id + 1000, TokenKey: token.Key}
	err := PreConsumeTokenQuota(info, 400)

	require.Error(t, err, "扣减未命中令牌行时必须返回错误，不能静默成功")
	var final model.Token
	require.NoError(t, model.DB.First(&final, token.Id).Error)
	require.Equal(t, 1000, final.RemainQuota, "失败的准入不得改动额度")
}

// 预算不足时失败且不改动额度，错误信息保留原有口径。
func TestPreConsumeTokenQuotaRejectsInsufficientBudget(t *testing.T) {
	truncate(t)
	model.KeyColumn()

	token := &model.Token{
		UserId:      1,
		Key:         "preconsume-guard-key-2",
		Name:        "preconsume-guard-2",
		Status:      common.TokenStatusEnabled,
		RemainQuota: 100,
	}
	require.NoError(t, model.DB.Create(token).Error)

	info := &relaycommon.RelayInfo{TokenId: token.Id, TokenKey: token.Key}
	err := PreConsumeTokenQuota(info, 200)

	require.Error(t, err)
	require.Contains(t, err.Error(), "token quota is not enough")
	var final model.Token
	require.NoError(t, model.DB.First(&final, token.Id).Error)
	require.Equal(t, 100, final.RemainQuota)
	require.Equal(t, 0, final.UsedQuota)
}

// 无限额度令牌不参与准入判断，但仍要照常记账（保持原有行为）。
func TestPreConsumeTokenQuotaStillTracksUnlimitedTokens(t *testing.T) {
	truncate(t)
	model.KeyColumn()

	token := &model.Token{
		UserId:         1,
		Key:            "preconsume-guard-key-3",
		Name:           "preconsume-guard-3",
		Status:         common.TokenStatusEnabled,
		RemainQuota:    0,
		UnlimitedQuota: true,
	}
	require.NoError(t, model.DB.Create(token).Error)

	info := &relaycommon.RelayInfo{TokenId: token.Id, TokenKey: token.Key, TokenUnlimited: true}
	require.NoError(t, PreConsumeTokenQuota(info, 300))

	var final model.Token
	require.NoError(t, model.DB.First(&final, token.Id).Error)
	require.Equal(t, -300, final.RemainQuota, "无限额度令牌仍需记录用量，允许透支")
	require.Equal(t, 300, final.UsedQuota)
}
