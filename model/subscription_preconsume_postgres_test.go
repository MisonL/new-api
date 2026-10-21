package model

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// PreConsumeUserSubscription 的幂等回退路径依赖「重复键之后还能读到已提交记录」。
// PostgreSQL 在语句失败后会把整个事务标记为 aborted，事务内任何后续语句都返回
// 25P02，因此回退查询必须换到事务外的连接。SQLite 不会中止事务，这条缺陷在
// 默认测试里不可见，只能在真实 PostgreSQL 上验证。
// 仅在 NEW_API_POSTGRES_TEST_DSN 指向一次性库时运行。
func setupSubscriptionPreConsumePostgresDB(t *testing.T) {
	t.Helper()

	dsn := postgresSequenceTestDSN()
	if dsn == "" {
		t.Skip("set NEW_API_POSTGRES_TEST_DSN or POSTGRES_TEST_DSN to run PostgreSQL idempotency test")
	}

	schemaName := "subscription_preconsume_test_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	rootDB := openPostgresSequenceTestDB(t, dsn)
	require.NoError(t, rootDB.Exec(fmt.Sprintf(`CREATE SCHEMA "%s"`, schemaName)).Error)
	t.Cleanup(func() {
		_ = rootDB.Exec(fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schemaName)).Error
		closeGormDB(rootDB)
	})

	testDB := openPostgresSequenceTestDB(t, postgresDSNWithSearchPath(t, dsn, schemaName))
	setupPostgresSequenceModelDB(t, testDB)
	require.NoError(t, DB.AutoMigrate(&SubscriptionPlan{}, &UserSubscription{}, &SubscriptionPreConsumeRecord{}))
}

func TestPreConsumeUserSubscriptionIdempotentFallbackOnPostgres(t *testing.T) {
	setupSubscriptionPreConsumePostgresDB(t)

	const requestId = "pg-idempotent-request-1"
	const userId = 9001

	plan := &SubscriptionPlan{
		Id:              990101,
		Title:           "pg-idempotency",
		DurationUnit:    "month",
		DurationValue:   1,
		Enabled:         true,
		TotalAmount:     1_000_000,
		QuotaResetPeriod: SubscriptionResetNever,
	}
	require.NoError(t, DB.Create(plan).Error)

	now := time.Now().Unix()
	// 事务会按 end_time asc 选中 picked；注入的重复记录指向 other，
	// 因此返回的 UserSubscriptionId 能区分「用候选订阅」和「用重复记录」两种写法。
	picked := &UserSubscription{
		UserId: userId, PlanId: plan.Id, AmountTotal: 100_000, AmountUsed: 500,
		StartTime: now - 60, EndTime: now + 3600, Status: "active",
	}
	require.NoError(t, DB.Create(picked).Error)
	other := &UserSubscription{
		UserId: userId, PlanId: plan.Id, AmountTotal: 200_000, AmountUsed: 0,
		StartTime: now - 30, EndTime: now + 7200, Status: "active",
	}
	require.NoError(t, DB.Create(other).Error)

	// 模拟并发：在事务真正 INSERT 之前，由另一个连接提交同 requestId 的记录。
	// 事务的 INSERT 随即命中唯一键冲突，这正是生产里并发重复请求的形态。
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

	res, err := PreConsumeUserSubscription(requestId, userId, "pg-idempotency-model", 0, 100)
	require.True(t, injected, "探针必须真的触发重复键路径，否则本用例没有覆盖回退分支")
	require.NoError(t, err, "重复键必须回退为幂等成功，而不是把 PG 的 25P02 原样抛出")
	require.NotNil(t, res)
	require.Equal(t, other.Id, res.UserSubscriptionId,
		"幂等回退必须返回已提交记录里的订阅 id")
	require.Equal(t, int64(42), res.PreConsumed,
		"幂等回退必须返回已提交记录的预扣额度")

	var after UserSubscription
	require.NoError(t, DB.First(&after, picked.Id).Error)
	require.Equal(t, int64(500), after.AmountUsed,
		"失败事务的额度累加必须随事务一起回滚")
}
