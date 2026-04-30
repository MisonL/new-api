package model

import (
	"errors"
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

func calculateInviteRebateQuota(topUpQuota int) (int, error) {
	if topUpQuota <= 0 || common.InviteRebateRate == 0 {
		return 0, nil
	}
	if math.IsNaN(common.InviteRebateRate) ||
		math.IsInf(common.InviteRebateRate, 0) ||
		common.InviteRebateRate < common.InviteRebateRateMin ||
		common.InviteRebateRate > common.InviteRebateRateMax {
		return 0, errors.New("邀请充值返利比例配置不合法")
	}

	rebate := decimal.NewFromInt(int64(topUpQuota)).
		Mul(decimal.NewFromFloat(common.InviteRebateRate)).
		Div(decimal.NewFromInt(100)).
		IntPart()
	if rebate <= 0 {
		return 0, nil
	}
	return int(rebate), nil
}

func RewardInviterForTopUp(tx *gorm.DB, inviteeUserId int, topUpQuota int) (int, int, error) {
	return RewardInviterForTopUpWithMeta(tx, inviteeUserId, topUpQuota, InvitationRewardMeta{})
}

func RewardInviterForTopUpWithMeta(tx *gorm.DB, inviteeUserId int, topUpQuota int, meta InvitationRewardMeta) (int, int, error) {
	if tx == nil {
		return 0, 0, errors.New("数据库事务为空")
	}
	if inviteeUserId <= 0 {
		return 0, 0, errors.New("被邀请用户无效")
	}

	rebateQuota, err := calculateInviteRebateQuota(topUpQuota)
	if err != nil || rebateQuota <= 0 {
		return 0, 0, err
	}

	var invitee User
	if err := tx.Select("id", "inviter_id").Where("id = ?", inviteeUserId).First(&invitee).Error; err != nil {
		return 0, 0, err
	}
	if invitee.InviterId <= 0 {
		return 0, 0, nil
	}
	if invitee.InviterId == inviteeUserId {
		return 0, 0, errors.New("邀请人不能是充值用户本人")
	}

	result := tx.Model(&User{}).Where("id = ?", invitee.InviterId).Updates(map[string]interface{}{
		"aff_quota":   gorm.Expr("aff_quota + ?", rebateQuota),
		"aff_history": gorm.Expr("aff_history + ?", rebateQuota),
	})
	if result.Error != nil {
		return 0, 0, result.Error
	}
	if result.RowsAffected == 0 {
		common.SysLog(fmt.Sprintf("invite rebate skipped: inviter %d not found", invitee.InviterId))
		return 0, 0, nil
	}
	if err := createInvitationTopUpRecord(tx, invitee.InviterId, inviteeUserId, topUpQuota, rebateQuota, common.InviteRebateRate, meta); err != nil {
		return 0, 0, err
	}

	return invitee.InviterId, rebateQuota, nil
}

func RecordInviteTopUpRebateLog(inviterId int, rebateQuota int, inviteeUserId int) {
	if inviterId <= 0 || rebateQuota <= 0 {
		return
	}
	RecordLog(
		inviterId,
		LogTypeSystem,
		fmt.Sprintf("邀请用户 %d 充值返利 %s", inviteeUserId, logger.LogQuota(rebateQuota)),
	)
}
