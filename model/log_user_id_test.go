package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// GetUserLogs 返回的 id 必须是数据库主键：前端表格用 log.id 作为行 key，
// 用户点删除时把 record.id 发给 DELETE /api/log/self/:id。
//
// formatUserLogs 从 8580d76d5 起把 id 改写成 startIdx+i+1，于是第 N 页的
// 每行都拿到 1..pageSize 的序号。用这个序号删除会删掉另一个用户的日志
// （DeleteUserLogByID 用 user_id 约束，所以是删掉同一用户前缀位置的记录），
// 或者在新一页上删掉完全无关的记录。
func TestGetUserLogsReturnsDatabaseIDs(t *testing.T) {
	DB.Exec("DELETE FROM logs")
	t.Cleanup(func() { DB.Exec("DELETE FROM logs") })

	const userID = 9001
	created := make([]Log, 0, 6)
	for i := 0; i < 6; i++ {
		created = append(created, Log{
			// 显式使用远离 1..pageSize 的 id：被改写的序号是 startIdx+i+1，
			// 若真实 id 恰好也是小数字，测试就会因巧合而通过。
			Id:        500000 + i,
			UserId:    userID,
			CreatedAt: int64(1000 + i),
			Type:      2,
			Content:   "probe",
			ModelName: "probe-model",
			Quota:     i,
		})
	}
	require.NoError(t, LOG_DB.Create(&created).Error)

	// 第二页：startIdx=2, num=2
	logs, _, err := GetUserLogs(userID, 0, 0, 0, "", false, "", 2, 2, "", "", "", false, false)
	require.NoError(t, err)
	require.Len(t, logs, 2)

	seen := make(map[int]struct{}, len(created))
	for i := range created {
		seen[created[i].Id] = struct{}{}
	}
	for _, log := range logs {
		_, isReal := seen[log.Id]
		require.True(t, isReal,
			"returned id %d is not a database id; deleting it would hit an unrelated row", log.Id)
	}

	// 逐条删除必须真的删掉刚刚展示的那一条。
	target := logs[0]
	require.NoError(t, DeleteUserLogByID(userID, target.Id))

	var remaining int64
	require.NoError(t, LOG_DB.Model(&Log{}).Where("id = ?", target.Id).Count(&remaining).Error)
	require.Zero(t, remaining, "the id handed to the client must address the row it came from")
}
