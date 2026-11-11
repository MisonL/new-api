package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// 任务轮询查询用 COALESCE(status,”) NOT IN 过滤终态：对列套函数使 status
// 索引在三个库上都失效，表变大后每轮轮询全表扫。改写为
// `status IS NULL OR status NOT IN (...)` 后语义必须逐行等价，特别是 legacy
// NULL 行（COALESCE 给 ”，计入未完成）不得丢。
func TestTaskUnfinishedQueriesIncludeNullStatusRows(t *testing.T) {
	origDB, origLogDB := DB, LOG_DB
	t.Cleanup(func() { DB, LOG_DB = origDB, origLogDB })
	DB = openTestDB(t, &Task{})
	oldRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedis })

	mk := func(id int64, taskID, status string) {
		row := &Task{TaskID: taskID, Status: TaskStatus(status), Progress: "0%", SubmitTime: 100}
		row.ID = id
		require.NoError(t, DB.Create(row).Error)
	}
	mk(1, "task-null", "")
	mk(2, "task-in-progress", TaskStatusInProgress)
	mk(3, "task-submitted", TaskStatusSubmitted)
	mk(4, "task-success", TaskStatusSuccess)
	mk(5, "task-failure", TaskStatusFailure)
	// legacy NULL 行：查询语义必须与 COALESCE 版本一致，NULL 计入未完成。
	require.NoError(t, DB.Model(&Task{}).Where("id = ?", 1).UpdateColumn("status", nil).Error)

	timedOut, err := GetTimedOutUnfinishedTasksWithError(200, 100)
	require.NoError(t, err)
	ids := map[int64]bool{}
	for _, row := range timedOut {
		ids[row.ID] = true
	}
	require.True(t, ids[1], "NULL status 行必须计入未完成（与 COALESCE 语义一致）")
	require.True(t, ids[2])
	require.True(t, ids[3])
	require.False(t, ids[4], "SUCCESS 行必须被排除")
	require.False(t, ids[5], "FAILURE 行必须被排除")

	synced, err := GetAllUnFinishSyncTasksWithError(100)
	require.NoError(t, err)
	require.Len(t, synced, 3, "同步轮询查询同样必须包含 NULL 行且排除终态")

	count, err := TaskCountUnfinishedWithError()
	require.NoError(t, err)
	require.Equal(t, int64(3), count, "计数查询口径一致")
}
