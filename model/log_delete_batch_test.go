package model

import (
	"context"
	"testing"

	"gorm.io/gorm"
)

// GORM renders a LIMIT clause on DELETE only for the MySQL dialector; SQLite
// and PostgreSQL silently drop it, so `Limit(limit).Delete(...)` deleted every
// expired row in one statement. DeleteOldLog now selects the primary keys
// first, which bounds every DELETE on all three dialects.
func TestDeleteOldLogBoundsEveryDeleteStatement(t *testing.T) {
	db := openTestDB(t, &Log{})
	previous := LOG_DB
	LOG_DB = db
	t.Cleanup(func() { LOG_DB = previous })

	const total = 250
	for i := 0; i < total; i++ {
		if err := db.Create(&Log{CreatedAt: int64(i), Type: 2, Content: "seed"}).Error; err != nil {
			t.Fatalf("seed log %d: %v", i, err)
		}
	}

	// Record how many rows each DELETE statement removed.
	var perStatement []int64
	if err := db.Callback().Delete().After("gorm:delete").Register("test:record_rows", func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "logs" {
			perStatement = append(perStatement, tx.RowsAffected)
		}
	}); err != nil {
		t.Fatalf("register delete callback: %v", err)
	}

	const limit = 100
	deleted, err := DeleteOldLog(context.Background(), 200, limit)
	if err != nil {
		t.Fatalf("DeleteOldLog: %v", err)
	}
	if deleted != 199 {
		t.Fatalf("deleted=%d, want 199", deleted)
	}
	if len(perStatement) < 2 {
		t.Fatalf("expected the work to be split into multiple DELETE statements, got %v", perStatement)
	}
	for i, rows := range perStatement {
		if rows > limit {
			t.Fatalf("DELETE statement %d removed %d rows, want at most %d (full sequence %v)", i, rows, limit, perStatement)
		}
	}

	var remaining int64
	if err := db.Model(&Log{}).Where("created_at < ?", 200).Count(&remaining).Error; err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("remaining=%d expired rows, want 0", remaining)
	}
}
