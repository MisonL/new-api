package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// DeleteLogsByFilter 原为一条无界 DELETE：大表上长时间持锁，且没有走
// WithContext，请求取消不会中止。改为复用分批删除（带 ctx），语义仍是
// “删完所有匹配行”，但每批只锁少量行、可被取消。
func TestDeleteLogsByFilterDeletesAllMatchesInBatches(t *testing.T) {
	db := openTestDB(t, &Log{})
	previousLogDB := LOG_DB
	LOG_DB = db
	t.Cleanup(func() { LOG_DB = previousLogDB })

	const total = 250
	rows := make([]Log, 0, total)
	for i := 0; i < total; i++ {
		rows = append(rows, Log{Type: 2, Content: "batch-delete", CreatedAt: int64(i)})
	}
	require.NoError(t, db.CreateInBatches(rows, 100).Error)

	// 单条无界 DELETE 在 250 行上只产生一条语句；分批实现至少 3 条。
	deleteStatements := 0
	require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register(
		"test:count_log_delete_statements",
		func(tx *gorm.DB) {
			if tx.Statement != nil && tx.Statement.Table == "logs" {
				deleteStatements++
			}
		},
	))

	deleted, err := DeleteLogsByFilter(context.Background(), LogFilter{LogType: 2})
	require.NoError(t, err)
	require.Equal(t, int64(total), deleted)
	require.GreaterOrEqual(t, deleteStatements, 3,
		"大批量删除必须分批执行，不能是一条无界 DELETE")

	var remaining int64
	require.NoError(t, db.Model(&Log{}).Where("type = ?", 2).Count(&remaining).Error)
	require.Zero(t, remaining)
}

func TestDeleteLogsByFilterHonorsCancelledContext(t *testing.T) {
	db := openTestDB(t, &Log{})
	previousLogDB := LOG_DB
	LOG_DB = db
	t.Cleanup(func() { LOG_DB = previousLogDB })

	require.NoError(t, db.Create(&Log{Type: 2, Content: "keep-me"}).Error)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	deleted, err := DeleteLogsByFilter(ctx, LogFilter{LogType: 2})
	require.Error(t, err)
	require.Zero(t, deleted)

	var remaining int64
	require.NoError(t, db.Model(&Log{}).Where("type = ?", 2).Count(&remaining).Error)
	require.Equal(t, int64(1), remaining)
}

