package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// 计划被改为 never、或下次重置晚于 end_time 时，maybeReset 直接返回且不写
// next_reset_time。该行永远处于“到期”状态，重置任务每批都满额返回，
// service 层 `for { n := ResetDueSubscriptions(batch); if n < batch break }`
// 变成无 sleep 的紧循环。到期却无法重置的行必须清掉 next_reset_time。
func TestResetDueSubscriptionsClearsUnresettableRows(t *testing.T) {
	db := openTestDB(t, &SubscriptionPlan{}, &UserSubscription{})
	previousDB := DB
	DB = db
	t.Cleanup(func() { DB = previousDB })

	now := common.GetTimestamp()
	require.NoError(t, db.Create(&SubscriptionPlan{
		Id:               9401,
		Title:            "never-reset",
		QuotaResetPeriod: SubscriptionResetNever,
	}).Error)
	require.NoError(t, db.Create(&UserSubscription{
		Id:            9401,
		UserId:        9401,
		PlanId:        9401,
		Status:        "active",
		AmountTotal:   100,
		StartTime:     now - 7200,
		EndTime:       now + 7200,
		NextResetTime: now - 60,
		LastResetTime: now - 7200,
	}).Error)

	n, err := ResetDueSubscriptions(10)
	require.NoError(t, err)
	require.Equal(t, 1, n, "第一轮仍会取到该到期行")

	n, err = ResetDueSubscriptions(10)
	require.NoError(t, err)
	require.Zero(t, n, "无法重置的行必须离开到期集合，否则任务陷入紧循环")

	var reloaded UserSubscription
	require.NoError(t, db.First(&reloaded, 9401).Error)
	require.Zero(t, reloaded.NextResetTime)
}
