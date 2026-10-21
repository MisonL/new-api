package model

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// MySQL 把 clause.OnConflict{DoNothing: true} 翻译成 ON DUPLICATE KEY UPDATE id=id，
// 重复键时 RowsAffected 的取值取决于 CLIENT_FOUND_ROWS 标志。幂等回退依赖
// RowsAffected == 0 识别「记录已存在」，必须在真实 MySQL 上钉住这一语义。
// 仅在 NEW_API_TEST_MYSQL_DSN 指向一次性数据库时运行。
func openSubscriptionMySQLDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := os.Getenv("NEW_API_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NEW_API_TEST_MYSQL_DSN not set; MySQL duplicate-key semantics not covered by this run")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)

	previousDB := DB
	DB = db
	t.Cleanup(func() {
		_ = db.Exec("DROP TABLE IF EXISTS subscription_pre_consume_records").Error
		_ = db.Exec("DROP TABLE IF EXISTS user_subscriptions").Error
		_ = db.Exec("DROP TABLE IF EXISTS subscription_plans").Error
		DB = previousDB
	})
	require.NoError(t, DB.AutoMigrate(&SubscriptionPlan{}, &UserSubscription{}, &SubscriptionPreConsumeRecord{}))
	return db
}

func TestPreConsumeUserSubscriptionIdempotentFallbackOnMySQL(t *testing.T) {
	db := openSubscriptionMySQLDB(t)

	const requestId = "mysql-idempotent-request-1"
	const userId = 9101

	plan := &SubscriptionPlan{
		Id:              990201,
		Title:           "mysql-idempotency",
		DurationUnit:    "month",
		DurationValue:   1,
		Enabled:         true,
		TotalAmount:     1_000_000,
		QuotaResetPeriod: SubscriptionResetNever,
	}
	require.NoError(t, db.Create(plan).Error)

	now := time.Now().Unix()
	picked := &UserSubscription{
		UserId: userId, PlanId: plan.Id, AmountTotal: 100_000, AmountUsed: 500,
		StartTime: now - 60, EndTime: now + 3600, Status: "active",
	}
	require.NoError(t, db.Create(picked).Error)
	other := &UserSubscription{
		UserId: userId, PlanId: plan.Id, AmountTotal: 200_000, AmountUsed: 0,
		StartTime: now - 30, EndTime: now + 7200, Status: "active",
	}
	require.NoError(t, db.Create(other).Error)

	injected := false
	require.NoError(t, DB.Callback().Create().Before("gorm:create").
		Register("test:inject_duplicate_preconsume_record", func(tx *gorm.DB) {
			if injected || tx.Statement == nil || tx.Statement.Schema == nil {
				return
			}
			if tx.Statement.Schema.Table != "subscription_pre_consume_records" {
				return
			}
			injected = true
			err := DB.Exec(
				`INSERT INTO subscription_pre_consume_records `+
					`(request_id, user_id, user_subscription_id, pre_consumed, status, created_at, updated_at) `+
					`VALUES (?, ?, ?, ?, ?, ?, ?)`,
				requestId, userId, other.Id, int64(42), "consumed", now, now).Error
			if err != nil {
				tx.AddError(err)
			}
		}))

	res, err := PreConsumeUserSubscription(requestId, userId, "mysql-idempotency-model", 0, 100)
	require.True(t, injected, "探针必须真的触发重复键路径")
	require.NoError(t, err, "重复键必须回退为幂等成功")
	require.NotNil(t, res)
	require.Equal(t, other.Id, res.UserSubscriptionId)
	require.Equal(t, int64(42), res.PreConsumed)

	var after UserSubscription
	require.NoError(t, DB.First(&after, picked.Id).Error)
	require.Equal(t, int64(500), after.AmountUsed,
		"失败事务的额度累加必须随事务一起回滚")
}
