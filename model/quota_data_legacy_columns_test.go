package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 旧版 quota_data 没有 token_used 列。迁移先合并重复桶、最后才 AutoMigrate
// 补列：合并查询读取全部模型列，缺列时直接报错，InitDB 失败，补列的
// AutoMigrate 永远执行不到。补列必须排在合并之前。
func TestQuotaDataMigrationAddsMissingColumnsBeforeMerging(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.Exec(`CREATE TABLE quota_data (
		id integer PRIMARY KEY AUTOINCREMENT,
		user_id integer,
		username varchar(64) DEFAULT '',
		model_name varchar(64) DEFAULT '',
		created_at bigint,
		count integer DEFAULT 0,
		quota integer DEFAULT 0)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO quota_data
		(user_id, username, model_name, created_at, count, quota) VALUES
		(61, 'legacy', 'legacy-model', 3600, 1, 10),
		(61, 'legacy', 'legacy-model', 3600, 2, 20)`).Error)

	err := migrateQuotaDataUniqueIndexWithExecutor(gormPrefillGroupMigrationExecutor{db: db})
	require.NoError(t, err, "缺列的旧库不能被合并查询卡死")

	require.True(t, db.Migrator().HasColumn(&QuotaData{}, "token_used"))

	var rows []QuotaData
	require.NoError(t, db.Where("user_id = ?", 61).Find(&rows).Error)
	require.Len(t, rows, 1, "重复桶必须被合并")
	require.Equal(t, 3, rows[0].Count)
	require.Equal(t, 30, rows[0].Quota)
}
