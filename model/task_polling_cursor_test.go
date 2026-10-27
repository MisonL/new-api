package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 轮询每轮只取 TASK_QUERY_LIMIT 条，旧实现固定 Order("id")：一旦有一批
// 卡在非终态的行占满窗口（渠道故障、上游长期不返回），id 更大的新任务
// 永远排不进这一批，直到那批行超时。改为游标推进后，连续两轮必须覆盖到
// 窗口之外的后续任务。
func TestUnfinishSyncTasksAfterIDCoversBeyondLimit(t *testing.T) {
	db := openTestDB(t, &Task{})
	previousDB := DB
	DB = db
	t.Cleanup(func() { DB = previousDB })

	for _, id := range []string{"stuck-1", "stuck-2", "newer-3"} {
		require.NoError(t, db.Exec(
			"INSERT INTO tasks (task_id, status, progress, submit_time, private_data) VALUES (?, ?, ?, ?, ?)",
			id, TaskStatusInProgress, "10%", int64(1), "{}",
		).Error)
	}

	first, err := GetUnfinishSyncTasksAfterIDWithError(0, 2)
	require.NoError(t, err)
	require.Len(t, first, 2)
	require.Equal(t, "stuck-1", first[0].TaskID)
	require.Equal(t, "stuck-2", first[1].TaskID)

	second, err := GetUnfinishSyncTasksAfterIDWithError(first[len(first)-1].ID, 2)
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.Equal(t, "newer-3", second[0].TaskID,
		"游标之后的轮次必须看到窗口外的任务，否则它们永远得不到轮询")

	wrapped, err := GetUnfinishSyncTasksAfterIDWithError(0, 2)
	require.NoError(t, err)
	require.Len(t, wrapped, 2)
}
