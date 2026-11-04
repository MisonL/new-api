package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TwoFA.Update 原为 DB.Save(t)：控制器只构造 {Id, UserId, Secret, IsEnabled=false}
// 就调用，Save 会写全部字段——created_at 被写成零值（MySQL 严格模式下超出
// DATETIME 下界直接报错，PG/SQLite 留下公元 1 年），failed_attempts /
// locked_until / last_used_step 被清零，锁定与重放保护被绕过。
func TestTwoFAUpdateOnlyTouchesSecretAndEnabled(t *testing.T) {
	db := openTestDB(t, &TwoFA{})
	previousDB := DB
	DB = db
	t.Cleanup(func() { DB = previousDB })

	lockedUntil := time.Now().Add(10 * time.Minute).Truncate(time.Second)
	createdAt := time.Date(2026, 3, 1, 8, 30, 0, 0, time.UTC)
	require.NoError(t, db.Create(&TwoFA{
		Id:             8801,
		UserId:         8801,
		Secret:         "OLD-SECRET",
		IsEnabled:      true,
		FailedAttempts: 3,
		LockedUntil:    &lockedUntil,
		LastUsedStep:   99,
		CreatedAt:      createdAt,
	}).Error)

	update := &TwoFA{Id: 8801, UserId: 8801, Secret: "NEW-SECRET", IsEnabled: false}
	require.NoError(t, update.Update())

	var reloaded TwoFA
	require.NoError(t, db.First(&reloaded, 8801).Error)
	require.Equal(t, "NEW-SECRET", reloaded.Secret)
	require.False(t, reloaded.IsEnabled)
	require.Equal(t, 3, reloaded.FailedAttempts, "失败计数不能被重置")
	require.NotNil(t, reloaded.LockedUntil, "锁定截止时间不能被清空")
	require.Equal(t, int64(99), reloaded.LastUsedStep, "重放保护步进不能被清零")
	require.Equal(t, createdAt.Unix(), reloaded.CreatedAt.Unix(), "created_at 不能被写成零值")
}
