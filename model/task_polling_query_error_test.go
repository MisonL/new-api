package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const malformedCurrentTaskPrivateData = `{"route_snapshot":{"schema_version":6}}`

func TestUnfinishedTaskQueriesSurfacePrivateDataScanErrors(t *testing.T) {
	t.Run("sync task query", func(t *testing.T) {
		db := openTestDB(t, &Task{})
		previousDB := DB
		DB = db
		t.Cleanup(func() { DB = previousDB })

		require.NoError(t, db.Exec(
			"INSERT INTO tasks (task_id, status, progress, submit_time, private_data) VALUES (?, ?, ?, ?, ?)",
			"malformed-sync-task", TaskStatusInProgress, "10%", int64(1), malformedCurrentTaskPrivateData,
		).Error)
		require.NoError(t, db.Exec(
			"INSERT INTO tasks (task_id, status, progress, submit_time, private_data) VALUES (?, ?, ?, ?, ?)",
			"valid-sync-task", TaskStatusInProgress, "10%", int64(1), "{}",
		).Error)

		tasks, err := GetAllUnFinishSyncTasksWithError(10)
		require.Error(t, err)
		require.Len(t, tasks, 1)
		require.Equal(t, "valid-sync-task", tasks[0].TaskID)

		tasks, err = GetTimedOutUnfinishedTasksWithError(2, 10)
		require.Error(t, err)
		require.Len(t, tasks, 1)
		require.Equal(t, "valid-sync-task", tasks[0].TaskID)
	})

	t.Run("Midjourney task query", func(t *testing.T) {
		db := openTestDB(t, &Midjourney{})
		previousDB := DB
		DB = db
		t.Cleanup(func() { DB = previousDB })

		require.NoError(t, db.Exec(
			"INSERT INTO midjourneys (mj_id, status, progress, submit_time, private_data) VALUES (?, ?, ?, ?, ?)",
			"malformed-mj-task", "IN_PROGRESS", "10%", int64(1), malformedCurrentTaskPrivateData,
		).Error)
		require.NoError(t, db.Exec(
			"INSERT INTO midjourneys (mj_id, status, progress, submit_time, private_data) VALUES (?, ?, ?, ?, ?)",
			"valid-mj-task", "IN_PROGRESS", "10%", int64(1), "{}",
		).Error)

		tasks, err := GetAllUnFinishTasksWithError(10)
		require.Error(t, err)
		require.Len(t, tasks, 1)
		require.Equal(t, "valid-mj-task", tasks[0].MjId)

		tasks, err = GetTimedOutUnfinishedMidjourneyTasksWithError(2, 10)
		require.Error(t, err)
		require.Len(t, tasks, 1)
		require.Equal(t, "valid-mj-task", tasks[0].MjId)
	})
}

func TestTaskListQueriesSurfacePrivateDataScanErrors(t *testing.T) {
	t.Run("sync task queries", func(t *testing.T) {
		db := openTestDB(t, &Task{})
		previousDB := DB
		DB = db
		t.Cleanup(func() { DB = previousDB })

		require.NoError(t, db.Exec(
			"INSERT INTO tasks (task_id, user_id, status, progress, submit_time, private_data) VALUES (?, ?, ?, ?, ?, ?)",
			"malformed-sync-list-task", 7, TaskStatusInProgress, "10%", int64(1), malformedCurrentTaskPrivateData,
		).Error)

		_, err := TaskGetAllTasksWithError(0, 10, SyncTaskQueryParams{})
		require.Error(t, err)
		_, err = TaskGetAllUserTaskWithError(7, 0, 10, SyncTaskQueryParams{})
		require.Error(t, err)
	})

	t.Run("Midjourney task queries", func(t *testing.T) {
		db := openTestDB(t, &Midjourney{})
		previousDB := DB
		DB = db
		t.Cleanup(func() { DB = previousDB })

		require.NoError(t, db.Exec(
			"INSERT INTO midjourneys (mj_id, user_id, status, progress, submit_time, private_data) VALUES (?, ?, ?, ?, ?, ?)",
			"malformed-mj-list-task", 7, "IN_PROGRESS", "10%", int64(1), malformedCurrentTaskPrivateData,
		).Error)

		_, err := GetAllTasksWithError(0, 10, TaskQueryParams{})
		require.Error(t, err)
		_, err = GetAllUserTaskWithError(7, 0, 10, TaskQueryParams{})
		require.Error(t, err)
	})
}

func TestUnfinishedTaskQueriesIncludeNullStatus(t *testing.T) {
	t.Run("sync task", func(t *testing.T) {
		db := openTestDB(t, &Task{})
		previousDB := DB
		DB = db
		t.Cleanup(func() { DB = previousDB })

		require.NoError(t, db.Exec(
			"INSERT INTO tasks (task_id, status, progress, submit_time, private_data) VALUES (?, NULL, ?, ?, ?)",
			"null-status-task", "10%", int64(1), "{}",
		).Error)
		require.NoError(t, db.Exec(
			"INSERT INTO tasks (task_id, status, progress, submit_time, private_data) VALUES (?, NULL, ?, ?, ?)",
			"completed-progress-task", "100%", int64(1), "{}",
		).Error)
		var rawTaskData []struct {
			TaskID      string
			PrivateData string
		}
		require.NoError(t, db.Raw("SELECT task_id, private_data FROM tasks").Scan(&rawTaskData).Error)
		require.Len(t, rawTaskData, 2)
		for _, row := range rawTaskData {
			require.Equal(t, "{}", row.PrivateData)
		}

		tasks, err := GetAllUnFinishSyncTasksWithError(10)
		require.NoError(t, err)
		require.Len(t, tasks, 1)
		require.Equal(t, "null-status-task", tasks[0].TaskID)

		timedOut, err := GetTimedOutUnfinishedTasksWithError(2, 10)
		require.NoError(t, err)
		require.Len(t, timedOut, 1)
		require.Equal(t, "null-status-task", timedOut[0].TaskID)
		require.Equal(t, int64(1), TaskCountUnfinished())
	})

	t.Run("Midjourney task", func(t *testing.T) {
		db := openTestDB(t, &Midjourney{})
		previousDB := DB
		DB = db
		t.Cleanup(func() { DB = previousDB })

		require.NoError(t, db.Exec(
			"INSERT INTO midjourneys (mj_id, status, progress, submit_time, private_data) VALUES (?, NULL, ?, ?, ?)",
			"null-status-mj", "10%", int64(1), "{}",
		).Error)
		var rawMidjourneyData []struct {
			MjID        string
			PrivateData string
		}
		require.NoError(t, db.Raw("SELECT mj_id, private_data FROM midjourneys").Scan(&rawMidjourneyData).Error)
		require.Len(t, rawMidjourneyData, 1)
		require.Equal(t, "{}", rawMidjourneyData[0].PrivateData)

		tasks, err := GetAllUnFinishTasksWithError(10)
		require.NoError(t, err)
		require.Len(t, tasks, 1)
		require.Equal(t, "null-status-mj", tasks[0].MjId)

		timedOut, err := GetTimedOutUnfinishedMidjourneyTasksWithError(2, 10)
		require.NoError(t, err)
		require.Len(t, timedOut, 1)
		require.Equal(t, "null-status-mj", timedOut[0].MjId)
	})
}
