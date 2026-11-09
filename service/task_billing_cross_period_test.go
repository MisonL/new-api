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
