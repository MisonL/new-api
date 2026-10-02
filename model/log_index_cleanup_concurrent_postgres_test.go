package model

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestQuoteIdentifierEscapesEmbeddedQuotes(t *testing.T) {
	require.Equal(t, `"logs"`, quoteIdentifier("logs"))
	require.Equal(t, `"a""b"`, quoteIdentifier(`a"b`))
	require.Equal(t, `""`, quoteIdentifier(""))
}

// The concurrent drop is the one part of this migration whose behaviour is the
// server's, not GORM's: CONCURRENTLY is only valid outside a transaction block
// and cannot be prepared, so whether it works at all depends on how the
// statement reaches PostgreSQL. A fake executor cannot observe that, so this
// runs against a real server and checks the index is actually gone.
func TestDropLogIndexConcurrentlyRemovesIndexOnPostgres(t *testing.T) {
	dsn := postgresSequenceTestDSN()
	if dsn == "" {
		t.Skip("set NEW_API_POSTGRES_TEST_DSN or POSTGRES_TEST_DSN to run the PostgreSQL concurrent-drop test")
	}

	schemaName := "log_index_concurrent_test_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	rootDB := openPostgresSequenceTestDB(t, dsn)
	require.NoError(t, rootDB.Exec(fmt.Sprintf(`CREATE SCHEMA "%s"`, schemaName)).Error)
	t.Cleanup(func() {
		_ = rootDB.Exec(fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schemaName)).Error
		closeGormDB(rootDB)
	})

	db := openPostgresSequenceTestDB(t, postgresDSNWithSearchPath(t, dsn, schemaName))
	t.Cleanup(func() { closeGormDB(db) })
	require.NoError(t, db.AutoMigrate(&Log{}))

	const name = "idx_logs_probe_concurrent"
	require.NoError(t, db.Exec("CREATE INDEX "+quoteIdentifier(name)+" ON logs(created_at)").Error)
	require.True(t, db.Migrator().HasIndex(&Log{}, name), "the probe index must exist first")

	require.True(t, isPostgreSQL(db), "the probe connection must be PostgreSQL")
	require.NoError(t, dropLogIndexConcurrently(db, name))
	require.False(t, db.Migrator().HasIndex(&Log{}, name),
		"a concurrent drop must actually remove the index")

	// IF EXISTS keeps the second call a no-op rather than an error, so a rerun
	// after a partial migration does not abort startup.
	require.NoError(t, dropLogIndexConcurrently(db, name))
}

// The gorm executor must route PostgreSQL through the concurrent path and
// leave the other dialects on the migrator. This pins the routing itself: the
// real-PG test above proves the concurrent path works, and this proves it is
// the one actually selected for a PostgreSQL logs database.
func TestGormExecutorDropsIndexOnPostgres(t *testing.T) {
	dsn := postgresSequenceTestDSN()
	if dsn == "" {
		t.Skip("set NEW_API_POSTGRES_TEST_DSN or POSTGRES_TEST_DSN to run the PostgreSQL executor test")
	}

	schemaName := "log_index_executor_test_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	rootDB := openPostgresSequenceTestDB(t, dsn)
	require.NoError(t, rootDB.Exec(fmt.Sprintf(`CREATE SCHEMA "%s"`, schemaName)).Error)
	t.Cleanup(func() {
		_ = rootDB.Exec(fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schemaName)).Error
		closeGormDB(rootDB)
	})

	db := openPostgresSequenceTestDB(t, postgresDSNWithSearchPath(t, dsn, schemaName))
	t.Cleanup(func() { closeGormDB(db) })
	require.NoError(t, db.AutoMigrate(&Log{}))

	const name = "idx_logs_probe_executor"
	require.NoError(t, db.Exec("CREATE INDEX "+quoteIdentifier(name)+" ON logs(created_at)").Error)

	executor := gormLogIndexCleanupExecutor{db: db}
	require.True(t, executor.HasIndex(&Log{}, name))
	require.NoError(t, executor.DropIndex(&Log{}, name))
	require.False(t, executor.HasIndex(&Log{}, name), "the executor must drop the index")
}

// Dropping the index successfully does not prove the concurrent path was used:
// a plain DROP INDEX also works on PostgreSQL, it just locks the table. This
// pins the routing by exploiting the one observable difference -- CONCURRENTLY
// cannot run inside a transaction block, so inside a transaction the concurrent
// path fails while the migrator path succeeds. If someone deletes the
// isPostgreSQL branch, this test starts passing where it must fail.
func TestGormExecutorRoutesPostgresThroughConcurrentDrop(t *testing.T) {
	dsn := postgresSequenceTestDSN()
	if dsn == "" {
		t.Skip("set NEW_API_POSTGRES_TEST_DSN or POSTGRES_TEST_DSN to run the PostgreSQL routing test")
	}

	schemaName := "log_index_routing_test_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	rootDB := openPostgresSequenceTestDB(t, dsn)
	require.NoError(t, rootDB.Exec(fmt.Sprintf(`CREATE SCHEMA "%s"`, schemaName)).Error)
	t.Cleanup(func() {
		_ = rootDB.Exec(fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schemaName)).Error
		closeGormDB(rootDB)
	})

	db := openPostgresSequenceTestDB(t, postgresDSNWithSearchPath(t, dsn, schemaName))
	t.Cleanup(func() { closeGormDB(db) })
	require.NoError(t, db.AutoMigrate(&Log{}))

	const name = "idx_logs_probe_routing"
	require.NoError(t, db.Exec("CREATE INDEX "+quoteIdentifier(name)+" ON logs(created_at)").Error)

	// Control: the migrator's plain DROP INDEX is transaction-safe.
	err := db.Transaction(func(tx *gorm.DB) error {
		return tx.Migrator().DropIndex(&Log{}, name)
	})
	require.NoError(t, err, "a plain DROP INDEX must succeed inside a transaction")
	require.NoError(t, db.Exec("CREATE INDEX "+quoteIdentifier(name)+" ON logs(created_at)").Error)

	// Under test: the executor must fail here, which only the concurrent form does.
	err = db.Transaction(func(tx *gorm.DB) error {
		return gormLogIndexCleanupExecutor{db: tx}.DropIndex(&Log{}, name)
	})
	require.ErrorContains(t, err, "CONCURRENTLY",
		"the executor must use DROP INDEX CONCURRENTLY on PostgreSQL")
	require.True(t, db.Migrator().HasIndex(&Log{}, name),
		"the failed concurrent drop inside a transaction must leave the index in place")
}
