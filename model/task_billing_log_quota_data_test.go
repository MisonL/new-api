package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// 任务计费日志写进了 logs（type=2 消费），却从不更新 quota_data；而
// bin/repair_channel_quota_logs.go 是按 type=2 的日志重建 quota_data 的。
// 于是任务消费在看板上永远缺失，一旦有人跑修复脚本，这些任务又会整批
// 冒出来，看板出现无法解释的跳变。两条路径必须同一口径。
func TestRecordTaskBillingLogUpdatesQuotaData(t *testing.T) {
	db := openTestDB(t, &QuotaData{}, &Log{}, &User{})
	previousDB, previousLogDB := DB, LOG_DB
	DB, LOG_DB = db, db
	previousExport := common.DataExportEnabled
	common.DataExportEnabled = true
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.DataExportEnabled = previousExport
		CacheQuotaDataLock.Lock()
		CacheQuotaData = map[string]*QuotaData{}
		CacheQuotaDataLock.Unlock()
	})

	require.NoError(t, db.Create(&User{Id: 71, Username: "task-log-user", Status: common.UserStatusEnabled}).Error)

	CacheQuotaDataLock.Lock()
	CacheQuotaData = map[string]*QuotaData{}
	CacheQuotaDataLock.Unlock()

	RecordTaskBillingLog(RecordTaskBillingLogParams{
		UserId:    71,
		LogType:   LogTypeConsume,
		ChannelId: 71,
		ModelName: "task-log-model",
		Quota:     500,
		Group:     "default",
		Other:     map[string]interface{}{"task_id": "t-71"},
	})

	// 聚合写入与 RecordConsumeLog 一致走 gopool，异步完成。
	require.Eventually(t, func() bool {
		CacheQuotaDataLock.Lock()
		defer CacheQuotaDataLock.Unlock()
		return len(CacheQuotaData) == 1
	}, 2*time.Second, 5*time.Millisecond, "任务消费日志必须同步计入看板聚合，否则修复脚本重建时会出现跳变")

	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()
	for _, entry := range CacheQuotaData {
		require.Equal(t, 71, entry.UserID)
		require.Equal(t, "task-log-model", entry.ModelName)
		require.Equal(t, 500, entry.Quota)
		require.Equal(t, 1, entry.Count)
	}
}

// 退款日志不能计入看板消费聚合：修复脚本只按 type=2 重建，把退款也累加
// 会让看板与修复结果再次分叉。
func TestRecordTaskBillingLogRefundSkipsQuotaData(t *testing.T) {
	db := openTestDB(t, &QuotaData{}, &Log{}, &User{})
	previousDB, previousLogDB := DB, LOG_DB
	DB, LOG_DB = db, db
	previousExport := common.DataExportEnabled
	common.DataExportEnabled = true
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.DataExportEnabled = previousExport
	})

	CacheQuotaDataLock.Lock()
	CacheQuotaData = map[string]*QuotaData{}
	CacheQuotaDataLock.Unlock()
	t.Cleanup(func() {
		CacheQuotaDataLock.Lock()
		CacheQuotaData = map[string]*QuotaData{}
		CacheQuotaDataLock.Unlock()
	})

	RecordTaskBillingLog(RecordTaskBillingLogParams{
		UserId:    72,
		LogType:   LogTypeRefund,
		ModelName: "task-refund-model",
		Quota:     500,
	})

	// 给异步路径一个确定的窗口，再断言退款没有进入聚合。
	time.Sleep(200 * time.Millisecond)
	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()
	require.Empty(t, CacheQuotaData, "退款不是消费，不能进入消费看板聚合")
}
