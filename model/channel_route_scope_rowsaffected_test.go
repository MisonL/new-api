package model

import (
	"os"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The route-scope ability write must not treat "no rows changed" as "row not
// found". MySQL reports changed rows by default, so re-saving the values already
// stored returns RowsAffected == 0 exactly like an UPDATE that matched nothing.
// Saving a route scope submits every member, including untouched ones, so this
// difference decided whether the whole save failed on MySQL.
//
// SQLite and PostgreSQL report matched rows, so they cannot reproduce the
// MySQL-side failure. The MySQL case therefore runs only when
// NEW_API_TEST_MYSQL_DSN points at a throwaway database; the SQLite case pins
// the contract on every run.

func setupRouteScopeAbilityDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&Ability{}))
}

func routeScopeAbilityTestRow() Ability {
	return Ability{
		Group:                    "default",
		Model:                    "gpt-5.4",
		ChannelId:                127,
		Enabled:                  true,
		RoutePriorityConfigured:  true,
		RouteWeightBPSConfigured: true,
	}
}

// A no-op rewrite of an existing ability must succeed.
func TestUpdateRouteScopeAbilityAcceptsUnchangedValues(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	setupRouteScopeAbilityDB(t, db)

	ability := routeScopeAbilityTestRow()
	require.NoError(t, db.Create(&ability).Error)

	// First write establishes the values.
	require.NoError(t, updateRouteScopeAbility(db, ability, 10, 5000))
	// Second write stores the same values. This is the case MySQL reports as
	// zero changed rows.
	require.NoError(t, updateRouteScopeAbility(db, ability, 10, 5000),
		"re-saving identical route values must not be reported as a missing ability")
}

// A genuinely missing ability must still be reported.
func TestUpdateRouteScopeAbilityRejectsMissingAbility(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	setupRouteScopeAbilityDB(t, db)

	missing := routeScopeAbilityTestRow()
	missing.ChannelId = 999

	err = updateRouteScopeAbility(db, missing, 10, 5000)
	require.Error(t, err)
	require.Contains(t, err.Error(), "route scope ability not found")
}

// Runs the same no-op case against a real MySQL server, where RowsAffected
// reports changed rows. This calls updateRouteScopeAbility itself, so it fails
// if the function goes back to deciding existence from RowsAffected.
// Skipped unless a DSN is provided.
func TestUpdateRouteScopeAbilityAcceptsUnchangedValuesOnMySQL(t *testing.T) {
	dsn := os.Getenv("NEW_API_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NEW_API_TEST_MYSQL_DSN not set; MySQL RowsAffected semantics not covered by this run")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)

	// updateRouteScopeAbility writes through the Ability model, so the real
	// table is the only one that exercises the fix.
	require.NoError(t, db.AutoMigrate(&Ability{}))
	row := Ability{Group: "rowsaffected-probe", Model: "gpt-5.4", ChannelId: 127}
	require.NoError(t, db.Where("`group` = ? AND model = ? AND channel_id = ?", row.Group, row.Model, row.ChannelId).
		Delete(&Ability{}).Error)
	t.Cleanup(func() {
		_ = db.Where("`group` = ? AND model = ? AND channel_id = ?", row.Group, row.Model, row.ChannelId).
			Delete(&Ability{}).Error
	})
	require.NoError(t, db.Create(&row).Error)

	// Confirm the premise on this server before asserting the fix: on MySQL an
	// identical rewrite reports zero changed rows.
	noop := db.Model(&Ability{}).
		Where("`group` = ? AND model = ? AND channel_id = ?", row.Group, row.Model, row.ChannelId).
		Updates(map[string]any{"route_priority": 0, "route_weight_bps": 0})
	require.NoError(t, noop.Error)
	require.Equal(t, int64(0), noop.RowsAffected,
		"MySQL must report zero changed rows for an identical rewrite for this test to be meaningful")

	// The first call stores new values; the second stores the same values again,
	// which is the no-op case that used to be reported as a missing ability.
	require.NoError(t, updateRouteScopeAbility(db, row, 10, 5000))
	require.NoError(t, updateRouteScopeAbility(db, row, 10, 5000),
		"re-saving identical route values must not be reported as a missing ability")

	missing := Ability{Group: row.Group, Model: row.Model, ChannelId: 999}
	err = updateRouteScopeAbility(db, missing, 10, 5000)
	require.Error(t, err)
	require.Contains(t, err.Error(), "route scope ability not found")
}
