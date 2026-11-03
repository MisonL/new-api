package model

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// PostgreSQL 的 ON CONFLICT DO UPDATE 里，SET 右侧的裸列名 count 会与 INSERT
// 目标列同名而报 "column reference \"count\" is ambiguous"（SQLSTATE 42702）。
// perf_metrics 早已用表名限定规避，quota_data 直到生产用上才暴露。SQLite 与
// MySQL 都没有这个歧义，因此必须用真实 PostgreSQL 才能覆盖。
func TestQuotaDataUpsertOnPostgres(t *testing.T) {
	dsn := postgresSequenceTestDSN()
	if dsn == "" {
		t.Skip("set NEW_API_POSTGRES_TEST_DSN or POSTGRES_TEST_DSN to run the PostgreSQL upsert test")
	}
	schemaName := "quota_data_upsert_test_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	rootDB := openPostgresSequenceTestDB(t, dsn)
	require.NoError(t, rootDB.Exec(fmt.Sprintf(`CREATE SCHEMA "%s"`, schemaName)).Error)
	t.Cleanup(func() {
		_ = rootDB.Exec(fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schemaName)).Error
		closeGormDB(rootDB)
	})
	testDB := openPostgresSequenceTestDB(t, postgresDSNWithSearchPath(t, dsn, schemaName))
	t.Cleanup(func() { closeGormDB(testDB) })
	setupPostgresSequenceModelDB(t, testDB)

	require.NoError(t, migrateQuotaDataUniqueIndex())

	for i := 0; i < 2; i++ {
		require.NoError(t, upsertQuotaData(&QuotaData{
			UserID: 7, Username: "u7", ModelName: "m7", CreatedAt: 3600, Count: 1, Quota: 10, TokenUsed: 1,
		}), "PostgreSQL 上的 upsert 不得因列名歧义失败")
	}

	var row QuotaData
	require.NoError(t, testDB.Where("user_id = ?", 7).First(&row).Error)
	require.Equal(t, 2, row.Count)
	require.Equal(t, 20, row.Quota)
	require.Equal(t, 2, row.TokenUsed)
}

var _ = gorm.ErrRecordNotFound
