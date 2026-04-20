package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

var ErrSubscriptionOrderPaymentMethodMismatch = errors.New("subscription order payment method mismatch")

func CompleteSubscriptionOrderWithPaymentMethod(tradeNo string, providerPayload string, expectedProvider string) error {
	if tradeNo == "" {
		return errors.New("tradeNo is empty")
	}
	refCol := "`trade_no`"
	if commonUsingPostgreSQL() {
		refCol = `"trade_no"`
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var order SubscriptionOrder
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(&order).Error; err != nil {
			return ErrSubscriptionOrderNotFound
		}
		if !paymentMethodMatchesProvider(order.PaymentMethod, expectedProvider) {
			return ErrSubscriptionOrderPaymentMethodMismatch
		}
		return completeSubscriptionOrderTx(tx, &order, providerPayload)
	})
}

func ExpireSubscriptionOrderWithPaymentMethod(tradeNo string, expectedProvider string) error {
	if tradeNo == "" {
		return errors.New("tradeNo is empty")
	}
	refCol := "`trade_no`"
	if commonUsingPostgreSQL() {
		refCol = `"trade_no"`
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var order SubscriptionOrder
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(&order).Error; err != nil {
			return ErrSubscriptionOrderNotFound
		}
		if !paymentMethodMatchesProvider(order.PaymentMethod, expectedProvider) {
			return ErrSubscriptionOrderPaymentMethodMismatch
		}
		if order.Status != common.TopUpStatusPending {
			return nil
		}
		order.Status = common.TopUpStatusExpired
		order.CompleteTime = common.GetTimestamp()
		return tx.Save(&order).Error
	})
}

func completeSubscriptionOrderTx(tx *gorm.DB, order *SubscriptionOrder, providerPayload string) error {
	if tx == nil || order == nil {
		return errors.New("invalid subscription order")
	}

	var logUserId int
	var logPlanTitle string
	var logMoney float64
	var logPaymentMethod string
	var upgradeGroup string

	if order.Status == common.TopUpStatusSuccess {
		return nil
	}
	if order.Status != common.TopUpStatusPending {
		return ErrSubscriptionOrderStatusInvalid
	}
	plan, err := GetSubscriptionPlanById(order.PlanId)
	if err != nil {
		return err
	}
	upgradeGroup = strings.TrimSpace(plan.UpgradeGroup)
	_, err = CreateUserSubscriptionFromPlanTx(tx, order.UserId, plan, "order")
	if err != nil {
		return err
	}
	if err := upsertSubscriptionTopUpTx(tx, order); err != nil {
		return err
	}
	order.Status = common.TopUpStatusSuccess
	order.CompleteTime = common.GetTimestamp()
	if providerPayload != "" {
		order.ProviderPayload = providerPayload
	}
	if err := tx.Save(order).Error; err != nil {
		return err
	}
	logUserId = order.UserId
	logPlanTitle = plan.Title
	logMoney = order.Money
	logPaymentMethod = order.PaymentMethod

	if upgradeGroup != "" && logUserId > 0 {
		_ = UpdateUserGroupCache(logUserId, upgradeGroup)
	}
	if logUserId > 0 {
		msg := fmt.Sprintf("订阅购买成功，套餐: %s，支付金额: %.2f，支付方式: %s", logPlanTitle, logMoney, logPaymentMethod)
		RecordLog(logUserId, LogTypeTopup, msg)
	}
	return nil
}
