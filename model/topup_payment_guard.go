package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

func ExpirePendingTopUpByTradeNoForPaymentMethod(tradeNo string, expectedProvider string) error {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}

	topUp := &TopUp{}
	refCol := "`trade_no`"
	if commonUsingPostgreSQL() {
		refCol = `"trade_no"`
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return errors.New("充值订单不存在")
		}
		if !paymentMethodMatchesProvider(topUp.PaymentMethod, expectedProvider) {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}
		topUp.Status = common.TopUpStatusExpired
		topUp.CompleteTime = common.GetTimestamp()
		return tx.Save(topUp).Error
	})
}

func MarkPendingTopUpFailedByTradeNoForPaymentMethod(tradeNo string, expectedProvider string) error {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}

	topUp := &TopUp{}
	refCol := "`trade_no`"
	if commonUsingPostgreSQL() {
		refCol = `"trade_no"`
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return errors.New("充值订单不存在")
		}
		if !paymentMethodMatchesProvider(topUp.PaymentMethod, expectedProvider) {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status != common.TopUpStatusPending {
			return nil
		}
		topUp.Status = common.TopUpStatusFailed
		topUp.CompleteTime = common.GetTimestamp()
		return tx.Save(topUp).Error
	})
}
