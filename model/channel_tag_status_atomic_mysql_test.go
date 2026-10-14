package model

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestDisableChannelByTagRollsBackOnMySQL is the MySQL counterpart of
// TestDisableChannelByTagRollsBackOnPostgres. The tag status write moved into a
// single transaction so that the channel row and its ability rows can no longer
// disagree, and whether that rollback actually happens is the server's
// behaviour rather than GORM's: MySQL has to be observed, not assumed.
//
// The failure is injected through a GORM update callback, so it cannot be
// produced from the HTTP layer; this drives the real DisableChannelByTag against
// a real MySQL server instead.
//
// Skipped unless NEW_API_TEST_MYSQL_DSN points at a throwaway database. The test
// migrates and truncates the tables it uses, so that DSN must not name a
// database holding real data.
func TestDisableChannelByTagRollsBackOnMySQL(t *testing.T) {
	dsn := os.Getenv("NEW_API_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NEW_API_TEST_MYSQL_DSN not set; MySQL rollback semantics not covered by this run")
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}, &Log{}))
	t.Cleanup(func() { closeGormDB(db) })

	setupMySQLTagStatusModelDB(t, db)

	tag := "tag-status-atomic-mysql"
	priority := int64(10)
	weight := uint(RouteWeightTotalBPS)
	ids := []int{90911, 90912}
	// A throwaway database is reused across runs, and the audit assertion at the
	// end counts rows. Clear only this test's own rows before seeding, otherwise
	// a second run on the same database sees the first run's entries and the
	// failure blames the code for the harness's leftovers.
	for _, id := range ids {
		_ = DB.Where("channel_id = ?", id).Delete(&Ability{}).Error
		_ = DB.Delete(&Channel{}, id).Error
	}
	require.NoError(t, DB.Where("type = ? AND content LIKE ?", LogTypeManage, "%tag-status-atomic-mysql%").Delete(&Log{}).Error)

	for _, id := range ids {
		require.NoError(t, DB.Create(&Channel{
			Id:       id,
			Name:     "tag-status-atomic-mysql",
			Status:   common.ChannelStatusEnabled,
			Group:    "default",
			Models:   "tag-status-atomic-mysql-model",
			Tag:      &tag,
			Priority: &priority,
			Weight:   common.GetPointer(uint(1)),
		}).Error)
		require.NoError(t, DB.Create(&Ability{
			Group:                    "default",
			Model:                    "tag-status-atomic-mysql-model",
			ChannelId:                id,
			Enabled:                  true,
			Priority:                 &priority,
			Weight:                   1,
			RoutePriority:            &priority,
			RouteWeightBPS:           &weight,
			RoutePriorityConfigured:  true,
			RouteWeightBPSConfigured: true,
			Tag:                      &tag,
		}).Error)
	}
	t.Cleanup(func() {
		for _, id := range ids {
			_ = DB.Where("channel_id = ?", id).Delete(&Ability{}).Error
			_ = DB.Delete(&Channel{}, id).Error
		}
		_ = DB.Where("type = ? AND content LIKE ?", LogTypeManage, "%tag-status-atomic-mysql%").Delete(&Log{}).Error
	})

	// Fail only the ability write, after the channel UPDATE has already run
	// inside the same transaction.
	const callbackName = "test:fail_ability_status_write_mysql"
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "abilities" {
			tx.AddError(gorm.ErrInvalidTransaction)
		}
	}))
	removeCallback := func() { _ = DB.Callback().Update().Remove(callbackName) }
	t.Cleanup(removeCallback)

	err = DisableChannelByTag(tag)
	require.Error(t, err, "the injected ability-write failure must surface")

	// Removed here rather than only in Cleanup: the happy path below must not
	// still be intercepted by the injected failure.
	removeCallback()

	for _, id := range ids {
		var channel Channel
		require.NoError(t, DB.First(&channel, id).Error)
		require.Equal(t, common.ChannelStatusEnabled, channel.Status,
			"MySQL committed the channel status although the ability write failed")

		var abilities []Ability
		require.NoError(t, DB.Where("channel_id = ?", id).Find(&abilities).Error)
		require.Len(t, abilities, 1)
		require.True(t, abilities[0].Enabled,
			"the ability must still be enabled because the transaction rolled back")
	}

	// The happy path must commit both sides on this driver as well.
	require.NoError(t, DisableChannelByTag(tag))
	for _, id := range ids {
		var channel Channel
		require.NoError(t, DB.First(&channel, id).Error)
		require.Equal(t, common.ChannelStatusManuallyDisabled, channel.Status)

		var abilities []Ability
		require.NoError(t, DB.Where("channel_id = ?", id).Find(&abilities).Error)
		require.Len(t, abilities, 1)
		require.False(t, abilities[0].Enabled)
	}

	var auditLogs []Log
	require.NoError(t, DB.Where("type = ?", LogTypeManage).Find(&auditLogs).Error)
	require.Len(t, auditLogs, len(ids), "each affected channel must get its own audit entry")
}

// setupMySQLTagStatusModelDB points the package-level handles at the MySQL test
// database and restores every global it touched, so the fixture cannot leak the
// dialect into a later test in the same package.
func setupMySQLTagStatusModelDB(t *testing.T, testDB *gorm.DB) {
	t.Helper()

	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL
	previousDB := DB
	previousLogDB := LOG_DB

	common.UsingSQLite = false
	common.UsingMySQL = true
	common.UsingPostgreSQL = false
	DB = testDB
	LOG_DB = testDB
	initCol()

	t.Cleanup(func() {
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
		DB = previousDB
		LOG_DB = previousLogDB
		initCol()
	})
}
