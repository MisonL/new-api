package model

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// The tag status write is a transaction, and a transaction is exactly the kind
// of logic whose behaviour depends on the real driver: rollback semantics,
// statement isolation and the generated UPDATE are the server's, not GORM's.
// The SQLite-backed test proves the intent; this one proves the driver agrees.
func TestDisableChannelByTagRollsBackOnPostgres(t *testing.T) {
	dsn := postgresSequenceTestDSN()
	if dsn == "" {
		t.Skip("set NEW_API_POSTGRES_TEST_DSN or POSTGRES_TEST_DSN to run the PostgreSQL rollback test")
	}

	schemaName := "channel_tag_status_test_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	rootDB := openPostgresSequenceTestDB(t, dsn)
	require.NoError(t, rootDB.Exec(fmt.Sprintf(`CREATE SCHEMA "%s"`, schemaName)).Error)
	t.Cleanup(func() {
		_ = rootDB.Exec(fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schemaName)).Error
		closeGormDB(rootDB)
	})

	testDB := openPostgresSequenceTestDB(t, postgresDSNWithSearchPath(t, dsn, schemaName))
	setupPostgresSequenceModelDB(t, testDB)
	// Log is migrated into the test schema as well. A tag status change now
	// writes an audit entry, and without a logs table in the schema the write
	// falls through search_path to the host database's public.logs, which is
	// exactly the pollution this schema isolation exists to prevent.
	require.NoError(t, DB.AutoMigrate(&Channel{}, &Ability{}, &Log{}))

	tag := "tag-status-atomic-pg"
	priority := int64(10)
	weight := uint(RouteWeightTotalBPS)
	ids := []int{90901, 90902}
	for _, id := range ids {
		require.NoError(t, DB.Create(&Channel{
			Id:       id,
			Name:     "tag-status-atomic-pg",
			Status:   common.ChannelStatusEnabled,
			Group:    "default",
			Models:   "tag-status-atomic-pg-model",
			Tag:      &tag,
			Priority: &priority,
			Weight:   common.GetPointer(uint(1)),
		}).Error)
		require.NoError(t, DB.Create(&Ability{
			Group:                    "default",
			Model:                    "tag-status-atomic-pg-model",
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

	// Fail only the ability write. On PostgreSQL the injected error aborts the
	// transaction, so the channel UPDATE that already ran inside it must be
	// rolled back by the server rather than merely skipped by the caller.
	const callbackName = "test:fail_ability_status_write_pg"
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "abilities" {
			tx.AddError(gorm.ErrInvalidTransaction)
		}
	}))
	removeCallback := func() { _ = DB.Callback().Update().Remove(callbackName) }
	t.Cleanup(removeCallback)

	err := DisableChannelByTag(tag)
	require.Error(t, err, "the injected ability-write failure must surface")

	// Removed here rather than only in Cleanup: the happy path below must not
	// still be intercepted by the injected failure.
	removeCallback()

	for _, id := range ids {
		var channel Channel
		require.NoError(t, DB.First(&channel, id).Error)
		require.Equal(t, common.ChannelStatusEnabled, channel.Status,
			"PostgreSQL committed the channel status although the ability write failed")

		var abilities []Ability
		require.NoError(t, DB.Where("channel_id = ?", id).Find(&abilities).Error)
		require.Len(t, abilities, 1)
		require.True(t, abilities[0].Enabled)
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

	// The audit entries must land in this schema's logs table. Reading them back
	// through the test DB is what makes the schema isolation a checked fact
	// rather than an assumption: an entry that went to another table would be
	// invisible here and the test would still pass.
	var auditLogs []Log
	require.NoError(t, DB.Where("type = ?", LogTypeManage).Find(&auditLogs).Error)
	require.Len(t, auditLogs, len(ids), "each affected channel must get its own audit entry in the test schema")
}
