package model

import (
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 2FA 的失败计数与状态更新都不能整行写回。t 是调用方早先加载的快照，
// 整行 Save 会把并发请求刚推进的 last_used_step 覆盖成旧值（重放保护
// 失效），也会把并发刚触发的 locked_until 抹成 nil（锁定被解除）。
func setupTwoFAStaleWriteDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := openTestDB(t, &TwoFA{}, &TwoFABackupCode{})
	previousDB := DB
	DB = db
	t.Cleanup(func() { DB = previousDB })
	return db
}

func TestIncrementFailedAttemptsDoesNotRollBackLastUsedStep(t *testing.T) {
	db := setupTwoFAStaleWriteDB(t)

	const userId = 7701
	require.NoError(t, db.Create(&TwoFA{UserId: userId, IsEnabled: true}).Error)

	// 请求 A 加载了一个 stale 结构体
	stale, err := GetTwoFAByUserId(userId)
	require.NoError(t, err)

	// 请求 B 成功消费，把 last_used_step 推进到 1000
	require.NoError(t, db.Model(&TwoFA{}).Where("id = ?", stale.Id).
		Updates(map[string]any{"last_used_step": 1000}).Error)

	// 请求 A 记一次失败
	require.NoError(t, stale.IncrementFailedAttempts())

	var stored TwoFA
	require.NoError(t, db.Where("user_id = ?", userId).First(&stored).Error)
	require.Equal(t, int64(1000), stored.LastUsedStep,
		"a failed attempt must not roll back the consumed TOTP step")
	require.Equal(t, 1, stored.FailedAttempts)
}

func TestIncrementFailedAttemptsDoesNotWipeLock(t *testing.T) {
	db := setupTwoFAStaleWriteDB(t)

	const userId = 7702
	require.NoError(t, db.Create(&TwoFA{UserId: userId, IsEnabled: true}).Error)

	stale, err := GetTwoFAByUserId(userId)
	require.NoError(t, err)

	// 另一个请求触发锁定
	lockUntil := time.Now().Add(time.Duration(common.LockoutDuration) * time.Second)
	require.NoError(t, db.Model(&TwoFA{}).Where("id = ?", stale.Id).
		Updates(map[string]any{"failed_attempts": common.MaxFailAttempts, "locked_until": lockUntil}).Error)

	// stale 请求记一次失败：整行 Save 会把 locked_until 写回 nil
	require.NoError(t, stale.IncrementFailedAttempts())

	var stored TwoFA
	require.NoError(t, db.Where("user_id = ?", userId).First(&stored).Error)
	require.NotNil(t, stored.LockedUntil, "a failed attempt must not clear an active lock")
}

// 并发失败请求必须累加而不是互相覆盖：每个请求只知道自己那一次，
// 靠数据库里的增量才能让第 N 次真正触发锁定。
func TestIncrementFailedAttemptsCountsConcurrentFailures(t *testing.T) {
	db := setupTwoFAStaleWriteDB(t)

	const userId = 7703
	require.NoError(t, db.Create(&TwoFA{UserId: userId, IsEnabled: true}).Error)

	const workers = 8
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			record, err := GetTwoFAByUserId(userId)
			if err != nil || record == nil {
				return
			}
			_ = record.IncrementFailedAttempts()
		}()
	}
	wg.Wait()

	var stored TwoFA
	require.NoError(t, db.Where("user_id = ?", userId).First(&stored).Error)
	require.Equal(t, workers, stored.FailedAttempts,
		"concurrent failures must accumulate instead of overwriting each other")
	require.NotNil(t, stored.LockedUntil,
		"the lock must engage once the counter reaches MaxFailAttempts")
}

// 备用码成功路径同样不能整行写回：它与 TOTP 路径交错执行，
// 整行 Save 会覆盖 TOTP 路径刚推进的步进。
func TestValidateBackupCodeSuccessDoesNotRollBackLastUsedStep(t *testing.T) {
	db := setupTwoFAStaleWriteDB(t)

	const userId = 7704
	record := &TwoFA{UserId: userId, IsEnabled: true}
	require.NoError(t, db.Create(record).Error)

	codes, err := common.GenerateBackupCodes()
	require.NoError(t, err)
	require.NoError(t, CreateBackupCodes(userId, codes[:1]))

	// stale 快照：step 还是 0
	stale, err := GetTwoFAByUserId(userId)
	require.NoError(t, err)

	// 并发的 TOTP 成功路径把步进推到 1000
	require.NoError(t, db.Model(&TwoFA{}).Where("id = ?", stale.Id).
		Updates(map[string]any{"last_used_step": 1000}).Error)

	valid, err := stale.ValidateBackupCodeAndUpdateUsage(codes[0])
	require.NoError(t, err)
	require.True(t, valid)

	var stored TwoFA
	require.NoError(t, db.Where("user_id = ?", userId).First(&stored).Error)
	require.Equal(t, int64(1000), stored.LastUsedStep,
		"a successful backup code must not roll back the consumed TOTP step")
	require.Equal(t, 0, stored.FailedAttempts)
}
