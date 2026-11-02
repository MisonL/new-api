package model

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// migrateQuotaDataUniqueIndexWithExecutor 存在的意义是让调用方指定要迁移的库。
// 若合并步骤仍走全局 DB，测试替身或并发测试会去改另一个库：目标库的重复桶
// 没被合并（随后建唯一索引失败），而全局库里无关的行被改写。
func TestQuotaDataMigrationUsesProvidedDBNotGlobal(t *testing.T) {
	globalDB := openTestDB(t, &QuotaData{})
	targetDB := openTestDB(t, &QuotaData{})

	previousDB := DB
	DB = globalDB
	t.Cleanup(func() { DB = previousDB })

	// 两个库都先模拟"唯一索引尚未建立"的存量形态，否则重复桶根本插不进去。
	require.NoError(t, targetDB.Migrator().DropIndex(&QuotaData{}, quotaDataBucketIndex))
	require.NoError(t, globalDB.Migrator().DropIndex(&QuotaData{}, quotaDataBucketIndex))

	seedDuplicateBucket(t, globalDB, 7, "global-user", "global-model", 3600)
	seedDuplicateBucket(t, targetDB, 8, "target-user", "target-model", 3600)

	require.NoError(t, migrateQuotaDataUniqueIndexWithExecutor(gormPrefillGroupMigrationExecutor{db: targetDB}))

	var targetRows []QuotaData
	require.NoError(t, targetDB.Where("user_id = ?", 8).Find(&targetRows).Error)
	require.Len(t, targetRows, 1, "目标库的重复桶必须被合并")
	require.Equal(t, 2, targetRows[0].Count)

	var globalRows []QuotaData
	require.NoError(t, globalDB.Where("user_id = ?", 7).Find(&globalRows).Error)
	require.Len(t, globalRows, 2, "全局库与本次迁移无关，不得被改写")

	require.True(t, targetDB.Migrator().HasIndex(&QuotaData{}, quotaDataBucketIndex))
	require.False(t, globalDB.Migrator().HasIndex(&QuotaData{}, quotaDataBucketIndex),
		"唯一索引只应建在传入的库上")
}

func seedDuplicateBucket(t *testing.T, db *gorm.DB, userID int, username, modelName string, createdAt int64) {
	t.Helper()
	for i := 0; i < 2; i++ {
		require.NoError(t, db.Exec(
			"INSERT INTO quota_data (user_id, username, model_name, created_at, count, quota, token_used) VALUES (?, ?, ?, ?, ?, ?, ?)",
			userID, username, modelName, createdAt, 1, 10, 4,
		).Error)
	}
}
