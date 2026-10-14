package model

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// AddAbilities detects a leftover row by comparing RowsAffected against the
// number of rows it tried to insert. Whether that comparison holds depends on
// what the driver reports for an INSERT ... ON CONFLICT DO NOTHING that skips a
// row, so the contract is pinned per driver. SQLite and PostgreSQL report the
// rows actually inserted; MySQL is the one that could plausibly differ, and the
// repository already records that MySQL reports changed rather than matched rows
// for UPDATE.
//
// Skipped unless NEW_API_TEST_MYSQL_DSN points at a throwaway database.
func TestOnConflictDoNothingRowsAffectedOnMySQL(t *testing.T) {
	dsn := os.Getenv("NEW_API_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NEW_API_TEST_MYSQL_DSN not set; MySQL RowsAffected semantics not covered by this run")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Ability{}))

	priority := int64(10)
	existing := Ability{
		Group: "conflict-probe", Model: "conflict-model", ChannelId: 95001,
		Enabled: false, Priority: &priority, Weight: 1,
	}
	require.NoError(t, db.Where("`group` = ? AND model = ? AND channel_id = ?",
		existing.Group, existing.Model, existing.ChannelId).Delete(&Ability{}).Error)
	require.NoError(t, db.Create(&existing).Error)
	t.Cleanup(func() {
		_ = db.Where("`group` = ?", "conflict-probe").Delete(&Ability{}).Error
	})

	// Same key, different enabled value: must be skipped.
	conflicting := []Ability{{
		Group: "conflict-probe", Model: "conflict-model", ChannelId: 95001,
		Enabled: true, Priority: &priority, Weight: 1,
	}}
	res := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&conflicting)
	require.NoError(t, res.Error)
	require.EqualValues(t, 0, res.RowsAffected,
		"MySQL must report 0 inserted rows when the conflict skips the row; AddAbilities relies on this to detect a leftover row")

	// A fresh key in the same batch shape must report 1.
	fresh := []Ability{{
		Group: "conflict-probe", Model: "conflict-model-2", ChannelId: 95001,
		Enabled: true, Priority: &priority, Weight: 1,
	}}
	res2 := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&fresh)
	require.NoError(t, res2.Error)
	require.EqualValues(t, 1, res2.RowsAffected,
		"MySQL must report 1 inserted row for a non-conflicting insert")

	// The skipped row must keep its stored value rather than being overwritten.
	var stored Ability
	require.NoError(t, db.Where("`group` = ? AND model = ? AND channel_id = ?",
		"conflict-probe", "conflict-model", 95001).First(&stored).Error)
	require.False(t, stored.Enabled,
		"the conflicting row must keep its stored value")
}

// The end-to-end case on MySQL: with an orphaned row occupying the key,
// AddAbilities must report the conflict rather than silently leaving the
// channel without an ability. This calls the real function, so it fails if the
// check is removed or if MySQL's RowsAffected makes the comparison miss.
func TestAddAbilitiesRejectsOrphanConflictOnMySQL(t *testing.T) {
	dsn := os.Getenv("NEW_API_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NEW_API_TEST_MYSQL_DSN not set; MySQL RowsAffected semantics not covered by this run")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Ability{}))

	previousDB := DB
	DB = db
	t.Cleanup(func() { DB = previousDB })

	priority := int64(10)
	orphan := Ability{
		Group: "orphan-conflict", Model: "orphan-model", ChannelId: 95002,
		Enabled: false, Priority: &priority, Weight: 1,
	}
	require.NoError(t, db.Where("`group` = ?", "orphan-conflict").Delete(&Ability{}).Error)
	require.NoError(t, db.Create(&orphan).Error)
	t.Cleanup(func() {
		_ = db.Where("`group` = ?", "orphan-conflict").Delete(&Ability{}).Error
	})

	channel := &Channel{
		Id: 95002, Name: "orphan-conflict", Status: common.ChannelStatusEnabled,
		Group: "orphan-conflict", Models: "orphan-model",
	}
	err = channel.AddAbilities(nil)
	require.Error(t, err, "AddAbilities must not silently skip a conflicting insert on MySQL")
	require.Contains(t, err.Error(), "conflict with rows left behind by a deleted channel")
}
