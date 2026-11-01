package model

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 迁移的谓词拼的是 options 表的 key 列；key 在 MySQL 是保留字，未加方言引号
// 会直接报 1064，SQLite 与 PostgreSQL 的宽容掩盖不了这一点，只能在真实库上验证。
func useRetryStatusCodesMigrationMySQLDB(t *testing.T) {
	t.Helper()
	dsn := os.Getenv("NEW_API_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NEW_API_TEST_MYSQL_DSN not set; MySQL key-column quoting not covered by this run")
	}
	db, err := gorm.Open(MySQLDialector(dsn), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)

	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL
	previousDB := DB
	previousLogDB := LOG_DB
	common.UsingSQLite = false
	common.UsingMySQL = true
	common.UsingPostgreSQL = false
	DB = db
	LOG_DB = db
	initCol()
	t.Cleanup(func() {
		_ = db.Exec("DROP TABLE IF EXISTS options").Error
		closeGormDB(db)
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
		DB = previousDB
		LOG_DB = previousLogDB
		initCol()
	})
	require.NoError(t, db.Exec("DROP TABLE IF EXISTS options").Error)
	require.NoError(t, DB.AutoMigrate(&Option{}))
}

func useRetryStatusCodesMigrationPostgresDB(t *testing.T) {
	t.Helper()
	dsn := postgresSequenceTestDSN()
	if dsn == "" {
		t.Skip("set NEW_API_POSTGRES_TEST_DSN or POSTGRES_TEST_DSN to run the PostgreSQL migration test")
	}
	schemaName := "retry_status_codes_migration_test_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	rootDB := openPostgresSequenceTestDB(t, dsn)
	require.NoError(t, rootDB.Exec(fmt.Sprintf(`CREATE SCHEMA "%s"`, schemaName)).Error)
	t.Cleanup(func() {
		_ = rootDB.Exec(fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schemaName)).Error
		closeGormDB(rootDB)
	})
	testDB := openPostgresSequenceTestDB(t, postgresDSNWithSearchPath(t, dsn, schemaName))
	t.Cleanup(func() { closeGormDB(testDB) })
	setupPostgresSequenceModelDB(t, testDB)
	require.NoError(t, DB.AutoMigrate(&Option{}))
}

func assertRetryStatusCodesMigrationOnRealDB(t *testing.T) {
	t.Helper()
	legacy := "100-199,300-399,400-407,409-499,500-503,505-523,525-599"
	require.NoError(t, DB.Create(&Option{Key: "AutomaticRetryStatusCodes", Value: legacy}).Error)

	require.NoError(t, migrateRetryStatusCodesLegacyDefault())

	value, ok := retryStatusCodesOptionValue(t)
	require.True(t, ok)
	require.Equal(t, operation_setting.DefaultAutomaticRetryStatusCodes(), value)

	// 标记已写入：管理员随后改回旧范围，再次启动不得改写。
	require.NoError(t, DB.Model(&Option{}).Where(KeyColumn()+" = ?", "AutomaticRetryStatusCodes").Update("value", legacy).Error)
	require.NoError(t, migrateRetryStatusCodesLegacyDefault())
	value, ok = retryStatusCodesOptionValue(t)
	require.True(t, ok)
	require.Equal(t, legacy, value)
}

func TestMigrateRetryStatusCodesOnMySQL(t *testing.T) {
	useRetryStatusCodesMigrationMySQLDB(t)
	assertRetryStatusCodesMigrationOnRealDB(t)
}

func TestMigrateRetryStatusCodesOnPostgres(t *testing.T) {
	useRetryStatusCodesMigrationPostgresDB(t)
	assertRetryStatusCodesMigrationOnRealDB(t)
}
