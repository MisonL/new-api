package model

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// quota_data 的写入是"先查再插"：两个进程同时刷同一个 (user, username,
// model, hour) 桶时都查不到行、都执行 INSERT，落出两行重复聚合。此后
// increaseQuotaData 的 UPDATE 会同时命中两行，看板数值翻倍，而后续读取
// 又把两行分别汇总。表上必须有唯一索引兜底，写入改为 upsert。
func TestQuotaDataBucketIsUnique(t *testing.T) {
	db := openTestDB(t, &QuotaData{})
	previousDB := DB
	DB = db
	t.Cleanup(func() { DB = previousDB })

	require.NoError(t, migrateQuotaDataUniqueIndex())

	bucket := &QuotaData{UserID: 1, Username: "u", ModelName: "m", CreatedAt: 3600, Count: 1, Quota: 10, TokenUsed: 3}
	require.NoError(t, db.Create(bucket).Error)

	duplicate := &QuotaData{UserID: 1, Username: "u", ModelName: "m", CreatedAt: 3600, Count: 1, Quota: 10, TokenUsed: 3}
	require.Error(t, db.Create(duplicate).Error, "同一小时桶只能有一行，否则聚合会被重复累加")
}

// 迁移必须先把已有重复行合并再建唯一索引，否则建索引失败、启动报错。
func TestQuotaDataUniqueMigrationMergesExistingDuplicates(t *testing.T) {
	db := openTestDB(t, &QuotaData{})
	previousDB := DB
	DB = db
	t.Cleanup(func() { DB = previousDB })

	// 模拟尚未迁移过的部署：表已存在但没有唯一索引，且已经积累了重复桶。
	require.NoError(t, db.Migrator().DropIndex(&QuotaData{}, quotaDataBucketIndex))
	for i := 0; i < 2; i++ {
		require.NoError(t, db.Exec(
			"INSERT INTO quota_data (user_id, username, model_name, created_at, count, quota, token_used) VALUES (?, ?, ?, ?, ?, ?, ?)",
			2, "dup-user", "dup-model", int64(7200), 1, 10, 4,
		).Error)
	}

	require.NoError(t, migrateQuotaDataUniqueIndex())

	var rows []QuotaData
	require.NoError(t, db.Where("user_id = ? AND created_at = ?", 2, int64(7200)).Find(&rows).Error)
	require.Len(t, rows, 1, "重复桶必须被合并成一行")
	require.Equal(t, 2, rows[0].Count)
	require.Equal(t, 20, rows[0].Quota)
	require.Equal(t, 8, rows[0].TokenUsed)
}

// 刷新缓存时同一个桶再次出现必须走 upsert，而不是新建第二行。
func TestSaveQuotaDataCacheUpsertsBucket(t *testing.T) {
	db := openTestDB(t, &QuotaData{})
	previousDB := DB
	DB = db
	t.Cleanup(func() {
		DB = previousDB
		CacheQuotaDataLock.Lock()
		CacheQuotaData = map[string]*QuotaData{}
		CacheQuotaDataLock.Unlock()
	})
	require.NoError(t, migrateQuotaDataUniqueIndex())

	require.NoError(t, db.Create(&QuotaData{
		UserID: 3, Username: "u3", ModelName: "m3", CreatedAt: 3600, Count: 1, Quota: 10, TokenUsed: 2,
	}).Error)

	CacheQuotaDataLock.Lock()
	CacheQuotaData["3-u3-m3-3600"] = &QuotaData{
		UserID: 3, Username: "u3", ModelName: "m3", CreatedAt: 3600, Count: 2, Quota: 30, TokenUsed: 5,
	}
	CacheQuotaDataLock.Unlock()

	SaveQuotaDataCache()

	var rows []QuotaData
	require.NoError(t, db.Where("user_id = ?", 3).Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, 3, rows[0].Count)
	require.Equal(t, 40, rows[0].Quota)
	require.Equal(t, 7, rows[0].TokenUsed)
}

// upsert 必须在 SQL 层完成累加：两个并发写入各自只带增量，最终结果等于
// 两个增量之和，而不是互相覆盖。
func TestUpsertQuotaDataIsAdditive(t *testing.T) {
	db := openTestDB(t, &QuotaData{})
	previousDB := DB
	DB = db
	t.Cleanup(func() { DB = previousDB })
	require.NoError(t, migrateQuotaDataUniqueIndex())

	for i := 0; i < 2; i++ {
		require.NoError(t, upsertQuotaData(&QuotaData{
			UserID: 4, Username: "u4", ModelName: "m4", CreatedAt: 3600, Count: 1, Quota: 10, TokenUsed: 1,
		}))
	}

	var row QuotaData
	require.NoError(t, db.Where("user_id = ?", 4).First(&row).Error)
	require.Equal(t, 2, row.Count)
	require.Equal(t, 20, row.Quota)
	require.Equal(t, 2, row.TokenUsed)
}

var _ = gorm.ErrRecordNotFound

// MySQL 的 ON DUPLICATE KEY UPDATE 语义与 SQLite/PG 的 ON CONFLICT 不同，
// 唯一索引与 upsert 都必须在真实 MySQL 上验证。
func TestQuotaDataUpsertOnMySQL(t *testing.T) {
	dsn := os.Getenv("NEW_API_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NEW_API_TEST_MYSQL_DSN not set; MySQL upsert semantics not covered by this run")
	}
	db, err := gorm.Open(MySQLDialector(dsn), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	previous := DB
	DB = db
	t.Cleanup(func() {
		_ = db.Exec("DROP TABLE IF EXISTS quota_data").Error
		DB = previous
	})
	require.NoError(t, migrateQuotaDataUniqueIndex())

	for i := 0; i < 2; i++ {
		require.NoError(t, upsertQuotaData(&QuotaData{
			UserID: 5, Username: "u5", ModelName: "m5", CreatedAt: 3600, Count: 1, Quota: 10, TokenUsed: 1,
		}))
	}
	var row QuotaData
	require.NoError(t, db.Where("user_id = ?", 5).First(&row).Error)
	require.Equal(t, 2, row.Count)
	require.Equal(t, 20, row.Quota)
	require.Equal(t, 2, row.TokenUsed)
}

// 启动迁移的真实入口是 migrateDB：大 AutoMigrate 在各专用迁移之前执行。
// QuotaData 若留在大列表里，gorm 会按结构体标签先建 uk_qdt_bucket，存量
// 重复桶让建索引直接失败，专用迁移里的合并步骤根本没有机会执行，进程
// 无法启动。只调用 migrateQuotaDataUniqueIndex 的用例覆盖不到这个顺序。
func TestMigrateDBMergesQuotaDataDuplicatesBeforeUniqueIndex(t *testing.T) {
	db := openTestDB(t, &QuotaData{})
	previousDB, previousLogDB := DB, LOG_DB
	previousUsingSQLite, previousUsingMySQL, previousUsingPostgreSQL := common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL
	DB, LOG_DB = db, db
	common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = true, false, false
	initCol()
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = previousUsingSQLite, previousUsingMySQL, previousUsingPostgreSQL
		initCol()
	})

	require.NoError(t, db.Migrator().DropIndex(&QuotaData{}, quotaDataBucketIndex))
	for i := 0; i < 2; i++ {
		require.NoError(t, db.Exec(
			"INSERT INTO quota_data (user_id, username, model_name, created_at, count, quota, token_used) VALUES (?, ?, ?, ?, ?, ?, ?)",
			3, "dup-user", "dup-model", int64(10800), 1, 10, 4,
		).Error)
	}

	require.NoError(t, migrateDB())

	var rows []QuotaData
	require.NoError(t, db.Where("user_id = ? AND created_at = ?", 3, int64(10800)).Find(&rows).Error)
	require.Len(t, rows, 1, "重复桶必须被合并成一行")
	require.Equal(t, 2, rows[0].Count)
	require.Equal(t, 20, rows[0].Quota)
	require.Equal(t, 8, rows[0].TokenUsed)
	require.True(t, db.Migrator().HasIndex(&QuotaData{}, quotaDataBucketIndex))
}
