package model

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRuntimePollingIndexesAutoMigrate(t *testing.T) {
	db := openTestDB(t, &CustomOAuthProvider{}, &Midjourney{}, &Task{}, &UserSubscription{}, &Log{})

	require.True(t, db.Migrator().HasIndex(&CustomOAuthProvider{}, "idx_custom_oauth_providers_enabled"))
	require.True(t, db.Migrator().HasIndex(&Midjourney{}, "idx_midjourneys_progress_id"))
	require.True(t, db.Migrator().HasIndex(&Task{}, "idx_tasks_progress_status_submit"))
	require.True(t, db.Migrator().HasIndex(&Task{}, "idx_tasks_progress_status_id"))
	require.True(t, db.Migrator().HasIndex(&UserSubscription{}, "idx_user_sub_status_end_id"))
	require.True(t, db.Migrator().HasIndex(&Log{}, "idx_logs_request_id_created_at"))
	require.True(t, db.Migrator().HasIndex(&Log{}, "idx_logs_created_at_id"))
	require.True(t, db.Migrator().HasIndex(&Log{}, "idx_logs_upstream_request_created_at_id"))
	require.Equal(t, []string{"created_at", "id"}, sqliteIndexColumns(t, db, "idx_logs_created_at_id"))
	require.Equal(t, []string{"upstream_request_id", "created_at", "id"}, sqliteIndexColumns(t, db, "idx_logs_upstream_request_created_at_id"))
}

func sqliteIndexColumns(t *testing.T, db *gorm.DB, indexName string) []string {
	t.Helper()

	var rows []struct {
		Seqno int
		Name  string
	}
	require.NoError(t, db.Raw("SELECT seqno, name FROM pragma_index_info(?) ORDER BY seqno", indexName).Scan(&rows).Error)
	require.NotEmpty(t, rows)

	columns := make([]string, len(rows))
	for i, row := range rows {
		require.Equal(t, i, row.Seqno)
		columns[i] = row.Name
	}
	return columns
}
