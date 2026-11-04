package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 预扣订阅时，查询活跃订阅的失败曾被改写成 "no active subscription"，
// billing_session 用字符串包含匹配把它当成额度不足：subscription_first
// 静默回退钱包，subscription_only 误报 403。数据库错误必须原样上抛。

func setupSubscriptionErrorMappingTest(t *testing.T) *gorm.DB {
	t.Helper()
	db := openTestDB(t, &SubscriptionPlan{}, &UserSubscription{}, &SubscriptionPreConsumeRecord{})
	previousDB := DB
	DB = db
	t.Cleanup(func() { DB = previousDB })
	return db
}

func TestPreConsumeUserSubscriptionPropagatesQueryError(t *testing.T) {
	db := setupSubscriptionErrorMappingTest(t)

	// 只建记录表，不建 user_subscriptions：查询活跃订阅必然报真实数据库错误。
	require.NoError(t, db.Exec("DROP TABLE IF EXISTS user_subscriptions").Error)

	_, err := PreConsumeUserSubscription("query-error-request", 9201, "model-a", 0, 100)
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrNoActiveSubscription),
		"数据库错误不能被伪装成没有活跃订阅")
	require.Contains(t, err.Error(), "query active subscriptions")
}

func TestPreConsumeUserSubscriptionUsesSentinelWhenNoRows(t *testing.T) {
	setupSubscriptionErrorMappingTest(t)

	_, err := PreConsumeUserSubscription("no-subscription-request", 9202, "model-a", 0, 100)
	require.ErrorIs(t, err, ErrNoActiveSubscription)
}

func TestSubscriptionQuotaInsufficientUsesSentinel(t *testing.T) {
	db := setupSubscriptionErrorMappingTest(t)

	now := common.GetTimestamp()
	plan := SubscriptionPlan{
		Id:                9203,
		Title:             "limited",
		TotalAmount:       10,
		QuotaResetPeriod:  SubscriptionResetNever,
		UpgradeGroup:      "",
	}
	require.NoError(t, db.Create(&plan).Error)
	require.NoError(t, db.Create(&UserSubscription{
		Id:          9203,
		UserId:      9203,
		PlanId:      9203,
		Status:      "active",
		AmountTotal: 10,
		AmountUsed:  9,
		StartTime:   now - 3600,
		EndTime:     now + 3600,
	}).Error)

	_, err := PreConsumeUserSubscription("quota-short-request", 9203, "model-a", 0, 100)
	require.ErrorIs(t, err, ErrSubscriptionQuotaInsufficient)
}
