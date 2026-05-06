package model

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestExpireDueSubscriptionsSelectsOnlyMaintenanceColumns(t *testing.T) {
	query := DB.Session(&gorm.Session{DryRun: true}).
		Select("id", "user_id", "plan_id", "status", "end_time", "upgrade_group").
		Where("status = ? AND end_time > 0 AND end_time <= ?", "active", int64(100)).
		Order("end_time asc, id asc").
		Limit(300).
		Find(&[]UserSubscription{})

	sql := query.Statement.SQL.String()
	require.NotContains(t, strings.ToUpper(sql), "SELECT *")
	require.Contains(t, sql, "upgrade_group")
	require.Contains(t, sql, "end_time")
}

func TestResetDueSubscriptionsSelectsOnlyMaintenanceColumns(t *testing.T) {
	query := DB.Session(&gorm.Session{DryRun: true}).
		Select("id", "plan_id", "status", "end_time", "next_reset_time").
		Where("next_reset_time > 0 AND next_reset_time <= ? AND status = ?", int64(100), "active").
		Order("next_reset_time asc").
		Limit(300).
		Find(&[]UserSubscription{})

	sql := query.Statement.SQL.String()
	require.NotContains(t, strings.ToUpper(sql), "SELECT *")
	require.Contains(t, sql, "next_reset_time")
	require.Contains(t, sql, "end_time")
}
