package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// 异步任务的预扣记录属于提交时的订阅周期。任务轮询期间发生周期重置后，
// 终态差额结算与失败退款都必须像同步退款路径一样识别出跨周期，只结束这笔
// 差额，不得用上个周期的预扣冲掉新周期已经产生的用量。
func TestTaskFinalSettlementDropsCrossPeriodCredit(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID, subID = 41, 41, 41, 41
	const preConsumed = 4000
	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, "sk-task-cross-settle", 100000)
	seedChannel(t, channelID)

	resetAt := time.Now().Unix()
	seedSubscription(t, subID, userID, 100000, 100) // 新周期已经产生的用量
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("id = ?", subID).
		Updates(map[string]any{"last_reset_time": resetAt}).Error)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceSubscription, subID)
	task.PrivateData.BillingContext.RequestId = "task-cross-settle-request"

	record := &model.SubscriptionPreConsumeRecord{
		RequestId:          "task-cross-settle-request",
		UserId:             userID,
		UserSubscriptionId: subID,
		PreConsumed:        preConsumed,
		Status:             "consumed",
	}
	require.NoError(t, model.DB.Create(record).Error)
	require.NoError(t, model.DB.Model(&model.SubscriptionPreConsumeRecord{}).Where("id = ?", record.Id).
		Update("created_at", resetAt-60).Error)

	// 实际消耗 1000，低于预扣 4000：差额 -3000 属于上一个周期。
	require.NoError(t, RecalculateTaskQuota(ctx, task, 1000, "cross-period probe"))

	var sub model.UserSubscription
	require.NoError(t, model.DB.Select("amount_used").Where("id = ?", subID).First(&sub).Error)
	require.Equal(t, int64(100), sub.AmountUsed, "跨周期差额不得回落进新周期用量")
	require.Equal(t, 1000, task.Quota)
}

func TestTaskRefundDropsCrossPeriodRefund(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID, subID = 42, 42, 42, 42
	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, "sk-task-cross-refund", 100000)
	seedChannel(t, channelID)

	resetAt := time.Now().Unix()
	seedSubscription(t, subID, userID, 100000, 100)
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("id = ?", subID).
		Updates(map[string]any{"last_reset_time": resetAt}).Error)

	task := makeTask(userID, channelID, 3000, tokenID, BillingSourceSubscription, subID)
	task.PrivateData.BillingContext.RequestId = "task-cross-refund-request"

	record := &model.SubscriptionPreConsumeRecord{
		RequestId:          "task-cross-refund-request",
		UserId:             userID,
		UserSubscriptionId: subID,
		PreConsumed:        3000,
		Status:             "consumed",
	}
	require.NoError(t, model.DB.Create(record).Error)
	require.NoError(t, model.DB.Model(&model.SubscriptionPreConsumeRecord{}).Where("id = ?", record.Id).
		Update("created_at", resetAt-60).Error)

	require.NoError(t, RefundTaskQuota(ctx, task, "cross-period refund probe"))

	var sub model.UserSubscription
	require.NoError(t, model.DB.Select("amount_used").Where("id = ?", subID).First(&sub).Error)
	require.Equal(t, int64(100), sub.AmountUsed, "跨周期退款不得冲掉新周期用量")
}

// 反向对照：同一周期内的任务终态差额结算必须照常回落。
func TestTaskFinalSettlementAppliesWithinOnePeriod(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID, subID = 43, 43, 43, 43
	const preConsumed = 4000
	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, "sk-task-same-settle", 100000)
	seedChannel(t, channelID)

	resetAt := time.Now().Unix()
	seedSubscription(t, subID, userID, 100000, 1000)
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("id = ?", subID).
		Updates(map[string]any{"last_reset_time": resetAt}).Error)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceSubscription, subID)
	task.PrivateData.BillingContext.RequestId = "task-same-settle-request"

	record := &model.SubscriptionPreConsumeRecord{
		RequestId:          "task-same-settle-request",
		UserId:             userID,
		UserSubscriptionId: subID,
		PreConsumed:        preConsumed,
		Status:             "consumed",
	}
	require.NoError(t, model.DB.Create(record).Error)
	require.NoError(t, model.DB.Model(&model.SubscriptionPreConsumeRecord{}).Where("id = ?", record.Id).
		Update("created_at", resetAt+60).Error)

	require.NoError(t, RecalculateTaskQuota(ctx, task, 1000, "same-period probe"))

	var sub model.UserSubscription
	require.NoError(t, model.DB.Select("amount_used").Where("id = ?", subID).First(&sub).Error)
	require.Equal(t, int64(0), sub.AmountUsed, "同周期差额必须回落：1000-1000=0")
}

// 历史任务行可能没有 BillingContext（旧版本写入、或缺字段的私有数据）。
// 退款/结算路径必须先判空：无上下文时退化为不带跨周期守卫的调整，而不是
// 在轮询 goroutine 里空指针崩溃，让这笔任务永远无法结清。
func TestTaskRefundWithoutBillingContextDoesNotPanic(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID, subID = 44, 44, 44, 44
	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, "sk-task-nil-context", 100000)
	seedChannel(t, channelID)
	seedSubscription(t, subID, userID, 100000, 500)

	task := makeTask(userID, channelID, 300, tokenID, BillingSourceSubscription, subID)
	task.PrivateData.BillingContext = nil

	require.NotPanics(t, func() {
		require.NoError(t, RefundTaskQuota(ctx, task, "nil billing context probe"))
	})

	var sub model.UserSubscription
	require.NoError(t, model.DB.Select("amount_used").Where("id = ?", subID).First(&sub).Error)
	require.Equal(t, int64(200), sub.AmountUsed, "无上下文的退款按原行为直接回落")
}

// 反向对照：有预扣记录但任务缺 BillingContext 时没有键可回查，
// 退化为原行为（同周期回落），而不是 panic 或跳过结算。
func TestTaskFinalSettlementWithoutBillingContextAppliesWithinPeriod(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID, subID = 45, 45, 45, 45
	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, "sk-task-nil-settle", 100000)
	seedChannel(t, channelID)
	seedSubscription(t, subID, userID, 100000, 1000)

	task := makeTask(userID, channelID, 4000, tokenID, BillingSourceSubscription, subID)
	task.PrivateData.BillingContext = nil

	require.NotPanics(t, func() {
		require.NoError(t, RecalculateTaskQuota(ctx, task, 1000, "nil context settle probe"))
	})

	var sub model.UserSubscription
	require.NoError(t, model.DB.Select("amount_used").Where("id = ?", subID).First(&sub).Error)
	require.Equal(t, int64(0), sub.AmountUsed)
}
