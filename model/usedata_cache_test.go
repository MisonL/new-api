package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// SaveQuotaDataCache 把内存里的看板聚合刷进 quota_data。原实现无论写库成功
// 与否都清空整个缓存：一次数据库抖动就丢掉整窗（默认 5 分钟）的聚合，且没有
// 任何重试机会——这条路径同时是优雅关闭时的最后一次落盘。
func TestSaveQuotaDataCacheKeepsFailedEntries(t *testing.T) {
	db := openTestDB(t, &QuotaData{})
	previous := DB
	DB = db
	t.Cleanup(func() {
		DB = previous
		CacheQuotaDataLock.Lock()
		CacheQuotaData = make(map[string]*QuotaData)
		CacheQuotaDataLock.Unlock()
	})

	CacheQuotaDataLock.Lock()
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()

	LogQuotaData(1, "alice", "gpt-4", 100, 3600, 10)
	LogQuotaData(2, "bob", "gpt-4", 200, 3600, 20)

	// 让写库失败：删掉表。
	require.NoError(t, db.Migrator().DropTable(&QuotaData{}))

	SaveQuotaDataCache()

	CacheQuotaDataLock.Lock()
	remaining := len(CacheQuotaData)
	CacheQuotaDataLock.Unlock()
	require.Equal(t, 2, remaining,
		"写库失败的聚合必须留在缓存里等待下次重试，不能被静默丢弃")

	// 表恢复后重试应把两条都落库并清空缓存。
	require.NoError(t, db.AutoMigrate(&QuotaData{}))
	SaveQuotaDataCache()

	CacheQuotaDataLock.Lock()
	remaining = len(CacheQuotaData)
	CacheQuotaDataLock.Unlock()
	require.Equal(t, 0, remaining, "成功落库后缓存必须清空")

	var count int64
	require.NoError(t, db.Model(&QuotaData{}).Count(&count).Error)
	require.Equal(t, int64(2), count)
}
