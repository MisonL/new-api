package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRuntimePollingIndexesAutoMigrate(t *testing.T) {
	db := openTestDB(t, &CustomOAuthProvider{}, &Midjourney{}, &Task{}, &UserSubscription{}, &Log{})

	require.True(t, db.Migrator().HasIndex(&CustomOAuthProvider{}, "idx_custom_oauth_providers_enabled"))
	require.True(t, db.Migrator().HasIndex(&Midjourney{}, "idx_midjourneys_progress_id"))
	require.True(t, db.Migrator().HasIndex(&Task{}, "idx_tasks_progress_status_submit"))
	require.True(t, db.Migrator().HasIndex(&Task{}, "idx_tasks_progress_status_id"))
	require.True(t, db.Migrator().HasIndex(&UserSubscription{}, "idx_user_sub_status_end_id"))
	require.True(t, db.Migrator().HasIndex(&Log{}, "idx_logs_request_id_created_at"))
	require.True(t, db.Migrator().HasIndex(&Log{}, "idx_logs_upstream_request_id_created_at"))
}
