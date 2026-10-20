package model

import (
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// MySQL 的 UPDATE 默认返回"实际改变"的行数而不是匹配的行数，
// TOTP 重放保护依赖条件更新后的 RowsAffected == 0 判定"该码已被消费"，
// 因此必须在真实 MySQL 上验证这一契约。
// 仅在 NEW_API_TEST_MYSQL_DSN 指向一次性数据库时运行。
func openTwoFAMySQLDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := os.Getenv("NEW_API_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NEW_API_TEST_MYSQL_DSN not set; MySQL TOTP conditional-update semantics not covered by this run")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&TwoFA{}))

	previousDB := DB
	DB = db
	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM two_fas").Error
		DB = previousDB
	})
	return db
}

// 升级路径：旧库没有 last_used_step 列，AutoMigrate 必须补列，
// 且既有行的步进回填为 0（首用可通过，不会被误判为已消费）。
func TestTwoFAMigrationAddsLastUsedStepOnMySQL(t *testing.T) {
	db := openTwoFAMySQLDB(t)

	require.True(t, db.Migrator().HasColumn(&TwoFA{}, "last_used_step"))

	key, err := common.GenerateTOTPSecret("mysql-migration-user")
	require.NoError(t, err)
	legacy := &TwoFA{UserId: 601, Secret: key.Secret(), IsEnabled: true}
	require.NoError(t, db.Create(legacy).Error)

	// 模拟旧版本表结构：删除新列后重新 AutoMigrate。
	require.NoError(t, db.Migrator().DropColumn(&TwoFA{}, "last_used_step"))
	require.False(t, db.Migrator().HasColumn(&TwoFA{}, "last_used_step"))

	require.NoError(t, db.AutoMigrate(&TwoFA{}))
	require.True(t, db.Migrator().HasColumn(&TwoFA{}, "last_used_step"))

	var stored TwoFA
	require.NoError(t, db.Where("user_id = ?", 601).First(&stored).Error)
	require.Equal(t, int64(0), stored.LastUsedStep,
		"existing rows must backfill last_used_step to 0, not block the first use")

	code, err := totp.GenerateCode(key.Secret(), time.Now().UTC())
	require.NoError(t, err)
	ok, err := stored.ValidateTOTPAndUpdateUsage(code)
	require.NoError(t, err)
	require.True(t, ok, "a legacy row with step 0 must accept its first code")
}

func TestValidateTOTPAndUpdateUsageRejectsReplayOnMySQL(t *testing.T) {
	db := openTwoFAMySQLDB(t)

	key, err := common.GenerateTOTPSecret("mysql-replay-user")
	require.NoError(t, err)
	twoFA := &TwoFA{UserId: 602, Secret: key.Secret(), IsEnabled: true}
	require.NoError(t, db.Create(twoFA).Error)

	code, err := totp.GenerateCode(key.Secret(), time.Now().UTC())
	require.NoError(t, err)

	ok, err := twoFA.ValidateTOTPAndUpdateUsage(code)
	require.NoError(t, err)
	require.True(t, ok, "first use must succeed")

	stored, err := GetTwoFAByUserId(602)
	require.NoError(t, err)
	require.Greater(t, stored.LastUsedStep, int64(0), "step must be persisted")

	ok, err = stored.ValidateTOTPAndUpdateUsage(code)
	require.NoError(t, err)
	require.False(t, ok, "MySQL RowsAffected must report 0 for the same step")
}

func TestValidateTOTPAndUpdateUsageConcurrentSameCodeOnMySQL(t *testing.T) {
	db := openTwoFAMySQLDB(t)

	key, err := common.GenerateTOTPSecret("mysql-concurrent-user")
	require.NoError(t, err)
	twoFA := &TwoFA{UserId: 603, Secret: key.Secret(), IsEnabled: true}
	require.NoError(t, db.Create(twoFA).Error)

	code, err := totp.GenerateCode(key.Secret(), time.Now().UTC())
	require.NoError(t, err)

	const workers = 8
	results := make(chan bool, workers)
	for i := 0; i < workers; i++ {
		go func() {
			record, err := GetTwoFAByUserId(603)
			if err != nil || record == nil {
				results <- false
				return
			}
			ok, _ := record.ValidateTOTPAndUpdateUsage(code)
			results <- ok
		}()
	}
	succeeded := 0
	for i := 0; i < workers; i++ {
		if <-results {
			succeeded++
		}
	}
	require.Equal(t, 1, succeeded, "exactly one concurrent submission may succeed on MySQL")
}

// 真实 MySQL 驱动错误文本必须被 ApiErrorMessage 归入"服务暂时不可用"，
// 否则连接串、库名与 SQL 片段会原样回给客户端。
func TestApiErrorMessageHidesMySQLDriverErrors(t *testing.T) {
	db := openTwoFAMySQLDB(t)

	var count int64
	err := db.Table("table_that_does_not_exist").Count(&count).Error
	require.Error(t, err)
	require.Contains(t, err.Error(), "Error 1146", "precondition: MySQL must report a missing-table error")
	require.Equal(t, "服务暂时不可用，请稍后重试", common.ApiErrorMessage(err))

	key, err := common.GenerateTOTPSecret("mysql-dup-user")
	require.NoError(t, err)
	require.NoError(t, db.Create(&TwoFA{UserId: 604, Secret: key.Secret()}).Error)
	err = db.Create(&TwoFA{UserId: 604, Secret: key.Secret()}).Error
	require.Error(t, err)
	require.Contains(t, err.Error(), "Duplicate entry", "precondition: MySQL must report a duplicate-key error")
	require.Equal(t, "服务暂时不可用，请稍后重试", common.ApiErrorMessage(err))
}
