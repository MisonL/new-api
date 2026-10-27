package model

import (
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to open test db: " + err.Error())
	}
	DB = db
	LOG_DB = db

	common.UsingSQLite = true
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	initCol()

	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get sql.DB: " + err.Error())
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(&Task{}, &User{}, &Token{}, &Log{}, &Channel{}, &Option{}, &QuotaData{}, &TopUp{}, &SubscriptionPlan{}, &SubscriptionOrder{}, &UserSubscription{}, &PerfMetric{}); err != nil {
		panic("failed to migrate: " + err.Error())
	}

	os.Exit(m.Run())
}

// truncateTables 在测试开始与结束各清一次主库。只在结束时清，任何忘记
// 清理的测试都会把行留给后面所有测试：-shuffle 与 -count 下会出现只有特定
// 顺序才复现的失败（例如 log_stat 的用例留下一条 consume 日志，随后断言
// "日志表为空"的用例就随机失败）。开始也清一次，使每个调用方拿到干净起点。
func truncateTables(t *testing.T) {
	t.Helper()
	truncateMainTestDB()
	t.Cleanup(truncateMainTestDB)
}

func truncateMainTestDB() {
	DB.Exec("DELETE FROM tasks")
	DB.Exec("DELETE FROM users")
	DB.Exec("DELETE FROM tokens")
	DB.Exec("DELETE FROM logs")
	DB.Exec("DELETE FROM channels")
	DB.Exec("DELETE FROM quota_data")
	DB.Exec("DELETE FROM top_ups")
	DB.Exec("DELETE FROM subscription_plans")
	DB.Exec("DELETE FROM subscription_orders")
	DB.Exec("DELETE FROM user_subscriptions")
	DB.Exec("DELETE FROM perf_metrics")
}

func insertTask(t *testing.T, task *Task) {
	t.Helper()
	task.CreatedAt = time.Now().Unix()
	task.UpdatedAt = time.Now().Unix()
	require.NoError(t, DB.Create(task).Error)
}

// ---------------------------------------------------------------------------
// Snapshot / Equal - pure logic tests (no DB)
// ---------------------------------------------------------------------------

func TestSnapshotEqual_Same(t *testing.T) {
	s := TaskSnapshot{
		Status:     TaskStatusInProgress,
		Progress:   "50%",
		StartTime:  1000,
		FinishTime: 0,
		FailReason: "",
		ResultURL:  "",
		Data:       json.RawMessage(`{"key":"value"}`),
	}
	assert.True(t, s.Equal(s))
}

func TestSnapshotEqual_DifferentStatus(t *testing.T) {
	a := TaskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage(`{}`)}
	b := TaskSnapshot{Status: TaskStatusSuccess, Data: json.RawMessage(`{}`)}
	assert.False(t, a.Equal(b))
}

func TestSnapshotEqual_DifferentProgress(t *testing.T) {
	a := TaskSnapshot{Status: TaskStatusInProgress, Progress: "30%", Data: json.RawMessage(`{}`)}
	b := TaskSnapshot{Status: TaskStatusInProgress, Progress: "60%", Data: json.RawMessage(`{}`)}
	assert.False(t, a.Equal(b))
}

func TestSnapshotEqual_DifferentData(t *testing.T) {
	a := TaskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage(`{"a":1}`)}
	b := TaskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage(`{"a":2}`)}
	assert.False(t, a.Equal(b))
}

func TestSnapshotEqual_NilVsEmpty(t *testing.T) {
	a := TaskSnapshot{Status: TaskStatusInProgress, Data: nil}
	b := TaskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage{}}
	// bytes.Equal(nil, []byte{}) == true
	assert.True(t, a.Equal(b))
}

func TestSnapshot_Roundtrip(t *testing.T) {
	task := &Task{
		Status:     TaskStatusInProgress,
		Progress:   "42%",
		StartTime:  1234,
		FinishTime: 5678,
		FailReason: "timeout",
		PrivateData: TaskPrivateData{
			ResultURL: "https://example.com/result.mp4",
		},
		Data: json.RawMessage(`{"model":"test-model"}`),
	}
	snap := task.Snapshot()
	assert.Equal(t, task.Status, snap.Status)
	assert.Equal(t, task.Progress, snap.Progress)
	assert.Equal(t, task.StartTime, snap.StartTime)
	assert.Equal(t, task.FinishTime, snap.FinishTime)
	assert.Equal(t, task.FailReason, snap.FailReason)
	assert.Equal(t, task.PrivateData.ResultURL, snap.ResultURL)
	assert.JSONEq(t, string(task.Data), string(snap.Data))
}

func TestTaskGetResultURLUsesValidatedLegacyFailureReason(t *testing.T) {
	tests := []struct {
		name       string
		failReason string
		want       string
	}{
		{name: "legacy https URL", failReason: "https://cdn.example/video.mp4", want: "https://cdn.example/video.mp4"},
		{name: "legacy data URL", failReason: "data:video/mp4;base64,AAAA", want: "data:video/mp4;base64,AAAA"},
		{name: "image data URL is not a video result", failReason: "data:image/png;base64,AAAA"},
		{name: "non-base64 data URL is not a video result", failReason: "data:video/mp4,AAAA"},
		{name: "plain failure message", failReason: "upstream returned an error"},
		{name: "relative path", failReason: "/tmp/video.mp4"},
		{name: "unsupported scheme", failReason: "ftp://cdn.example/video.mp4"},
		{name: "missing HTTP host", failReason: "https://"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &Task{FailReason: tt.failReason}
			assert.Equal(t, tt.want, task.GetResultURL())
		})
	}

	task := &Task{
		FailReason: "https://legacy.example/video.mp4",
		PrivateData: TaskPrivateData{
			ResultURL: "https://current.example/video.mp4",
		},
	}
	assert.Equal(t, "https://current.example/video.mp4", task.GetResultURL())
	assert.Empty(t, (*Task)(nil).GetResultURL())
}

func TestIsTaskStatusTransitionAllowedIsMonotonic(t *testing.T) {
	tests := []struct {
		name string
		from TaskStatus
		to   TaskStatus
		want bool
	}{
		{name: "initial to submitted", from: TaskStatusNotStart, to: TaskStatusSubmitted, want: true},
		{name: "queued to in progress", from: TaskStatusQueued, to: TaskStatusInProgress, want: true},
		{name: "in progress to success", from: TaskStatusInProgress, to: TaskStatusSuccess, want: true},
		{name: "in progress to failure", from: TaskStatusInProgress, to: TaskStatusFailure, want: true},
		{name: "success duplicate", from: TaskStatusSuccess, to: TaskStatusSuccess, want: true},
		{name: "failure duplicate", from: TaskStatusFailure, to: TaskStatusFailure, want: true},
		{name: "success to failure is rejected", from: TaskStatusSuccess, to: TaskStatusFailure, want: false},
		{name: "failure to success is rejected", from: TaskStatusFailure, to: TaskStatusSuccess, want: false},
		{name: "in progress to queued is rejected", from: TaskStatusInProgress, to: TaskStatusQueued, want: false},
		{name: "unknown incoming is rejected", from: TaskStatusInProgress, to: TaskStatusUnknown, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsTaskStatusTransitionAllowed(tt.from, tt.to))
		})
	}
}

// ---------------------------------------------------------------------------
// UpdateWithStatus CAS - DB integration tests
// ---------------------------------------------------------------------------

func TestUpdateWithStatus_Win(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID:   "task_cas_win",
		Status:   TaskStatusInProgress,
		Progress: "50%",
		Data:     json.RawMessage(`{}`),
	}
	insertTask(t, task)

	task.Status = TaskStatusSuccess
	task.Progress = "100%"
	won, err := task.UpdateWithStatus(TaskStatusInProgress)
	require.NoError(t, err)
	assert.True(t, won)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, TaskStatusSuccess, reloaded.Status)
	assert.Equal(t, "100%", reloaded.Progress)
}

func TestUpdateWithStatus_Lose(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID: "task_cas_lose",
		Status: TaskStatusFailure,
		Data:   json.RawMessage(`{}`),
	}
	insertTask(t, task)

	task.Status = TaskStatusSuccess
	won, err := task.UpdateWithStatus(TaskStatusInProgress) // wrong fromStatus
	require.NoError(t, err)
	assert.False(t, won)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, TaskStatusFailure, reloaded.Status) // unchanged
}

func TestUpdateWithStatus_ConcurrentWinner(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID: "task_cas_race",
		Status: TaskStatusInProgress,
		Quota:  1000,
		Data:   json.RawMessage(`{}`),
	}
	insertTask(t, task)

	const goroutines = 5
	wins := make([]bool, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			t := &Task{}
			*t = Task{
				ID:       task.ID,
				TaskID:   task.TaskID,
				Status:   TaskStatusSuccess,
				Progress: "100%",
				Quota:    task.Quota,
				Data:     json.RawMessage(`{}`),
			}
			t.CreatedAt = task.CreatedAt
			t.UpdatedAt = time.Now().Unix()
			won, err := t.UpdateWithStatus(TaskStatusInProgress)
			if err == nil {
				wins[idx] = won
			}
		}(i)
	}
	wg.Wait()

	winCount := 0
	for _, w := range wins {
		if w {
			winCount++
		}
	}
	assert.Equal(t, 1, winCount, "exactly one goroutine should win the CAS")
}
