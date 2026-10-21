package model

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// logs 在正式环境是十 GB 级、数百万行。AutoMigrate 建缺失索引走的是 gorm 的
// CREATE INDEX，PostgreSQL 上整条语句期间持锁，等于一次写阻塞。
// ensureLogIndexesMatchModel 在 AutoMigrate 之前并发建出缺失索引。
//
// CONCURRENTLY 不能进事务块，所以判据是：在事务里调用必须报 CONCURRENTLY
// 失败（只有并发形式会这样），事务外则建出全部索引且可用。
func TestEnsureLogIndexesMatchModelOnPostgres(t *testing.T) {
	dsn := postgresSequenceTestDSN()
	if dsn == "" {
		t.Skip("set NEW_API_POSTGRES_TEST_DSN or POSTGRES_TEST_DSN to run the PostgreSQL concurrent-create test")
	}

	db, cleanup := openBareLogsSchema(t, dsn, "log_index_create_test_")
	defer cleanup()

	err := db.Transaction(func(tx *gorm.DB) error {
		return ensureLogIndexesMatchModel(tx)
	})
	require.ErrorContains(t, err, "CONCURRENTLY",
		"index creation must use CREATE INDEX CONCURRENTLY; only the concurrent form fails inside a transaction")

	allIndexes := parsedLogIndexColumns(t)
	require.NotEmpty(t, allIndexes)
	for name := range allIndexes {
		state, err := lookupIndexState(db, "logs", name)
		require.NoError(t, err)
		require.False(t, state.exists,
			"the failed in-transaction attempt must not leave index %s behind", name)
	}

	require.NoError(t, ensureLogIndexesMatchModel(db),
		"the real startup path (outside a transaction) must succeed")
	for name, columns := range allIndexes {
		state, err := lookupIndexState(db, "logs", name)
		require.NoError(t, err)
		require.True(t, state.exists, "index %s must exist after migration", name)
		require.True(t, state.valid, "index %s must be valid", name)
		require.Equal(t, columns, state.columns, "index %s must be built with the declared columns", name)
	}

	// 第二次调用必须是幂等的空操作：索引都有效时不应重建。
	require.NoError(t, ensureLogIndexesMatchModel(db))
}

// 并发建索引中途失败（唯一冲突、被取消、连接断开）会在 PostgreSQL 上留下
// INVALID 索引。只按名字判断存在性会把它当成已建好永久跳过；
// lookupIndexState 必须把它识别为不可用。
func TestLookupIndexStateDetectsInvalidIndex(t *testing.T) {
	dsn := postgresSequenceTestDSN()
	if dsn == "" {
		t.Skip("set NEW_API_POSTGRES_TEST_DSN or POSTGRES_TEST_DSN to run the PostgreSQL index-state test")
	}

	db, cleanup := openBareLogsSchema(t, dsn, "log_index_state_test_")
	defer cleanup()
	require.NoError(t, db.Exec(`INSERT INTO logs (username) VALUES ('dup'), ('dup')`).Error)

	const probe = "idx_logs_probe_invalid"
	err := db.Exec("CREATE UNIQUE INDEX CONCURRENTLY " + quoteIdentifier(probe) + " ON logs(username)").Error
	require.Error(t, err, "duplicate values must make the unique index build fail")

	state, stateErr := lookupIndexState(db, "logs", probe)
	require.NoError(t, stateErr)
	require.True(t, state.exists, "a failed concurrent build leaves the index behind")
	require.False(t, state.valid, "the leftover index must be reported invalid")

	require.NoError(t, db.Exec("DROP INDEX "+quoteIdentifier(probe)).Error)
	state, stateErr = lookupIndexState(db, "logs", probe)
	require.NoError(t, stateErr)
	require.False(t, state.exists)
}

// 自愈路径：失效索引存在时 ensureLogIndexesMatchModel 必须删掉重建，
// 而不是因为"索引名已存在"跳过。这里直接改目录表把索引标成失效，
// 等价于一次被取消的并发建索引留下的状态。
func TestEnsureLogIndexesRecreatesInvalidIndexOnPostgres(t *testing.T) {
	dsn := postgresSequenceTestDSN()
	if dsn == "" {
		t.Skip("set NEW_API_POSTGRES_TEST_DSN or POSTGRES_TEST_DSN to run the PostgreSQL self-heal test")
	}

	db, cleanup := openBareLogsSchema(t, dsn, "log_index_heal_test_")
	defer cleanup()

	const probe = "idx_logs_user_created_at"
	require.NoError(t, db.Exec("CREATE INDEX "+quoteIdentifier(probe)+" ON logs(created_at)").Error)
	require.NoError(t, db.Exec(
		"UPDATE pg_index SET indisvalid = false WHERE indexrelid = ?::regclass", probe).Error)

	state, err := lookupIndexState(db, "logs", probe)
	require.NoError(t, err)
	require.True(t, state.exists)
	require.False(t, state.valid)

	require.NoError(t, ensureLogIndexesMatchModel(db))

	state, err = lookupIndexState(db, "logs", probe)
	require.NoError(t, err)
	require.True(t, state.exists, "the invalid index must be recreated")
	require.True(t, state.valid, "the recreated index must be valid")
}

// 列形状校正：AutoMigrate 只按名字判断索引是否存在。历史 bug 窗口内新建的
// 库会把 *_type_created_at 建成缺少 type 列的两列索引，之后永远修不回来。
// ensureLogIndexesMatchModel 必须发现列不一致并重建。
func TestEnsureLogIndexesRebuildsIndexWithWrongColumnsOnPostgres(t *testing.T) {
	dsn := postgresSequenceTestDSN()
	if dsn == "" {
		t.Skip("set NEW_API_POSTGRES_TEST_DSN or POSTGRES_TEST_DSN to run the PostgreSQL index-shape test")
	}

	db, cleanup := openBareLogsSchema(t, dsn, "log_index_shape_test_")
	defer cleanup()

	// 模拟 bug 窗口内建出的退化形状：少了 type 列。
	const probe = "idx_logs_user_type_created_at"
	require.NoError(t, db.Exec("CREATE INDEX "+quoteIdentifier(probe)+" ON logs(user_id, created_at)").Error)

	state, err := lookupIndexState(db, "logs", probe)
	require.NoError(t, err)
	require.Equal(t, []string{"user_id", "created_at"}, state.columns)

	require.NoError(t, ensureLogIndexesMatchModel(db))

	state, err = lookupIndexState(db, "logs", probe)
	require.NoError(t, err)
	require.True(t, state.valid)
	require.Equal(t, []string{"user_id", "type", "created_at"}, state.columns,
		"an index whose columns differ from the model declaration must be rebuilt")
}

// 老库升级：logs 可能还没有模型里后来新增的列。并发建索引跑在 AutoMigrate
// 之前，若直接建就会报列不存在、整个启动迁移失败。缺列的索引必须跳过，
// 留给 AutoMigrate 补列后按普通路径建。
func TestEnsureLogIndexesSkipsIndexesOnMissingColumns(t *testing.T) {
	dsn := postgresSequenceTestDSN()
	if dsn == "" {
		t.Skip("set NEW_API_POSTGRES_TEST_DSN or POSTGRES_TEST_DSN to run the PostgreSQL missing-column test")
	}

	schemaName := "log_index_missing_col_test_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	rootDB := openPostgresSequenceTestDB(t, dsn)
	require.NoError(t, rootDB.Exec(fmt.Sprintf(`CREATE SCHEMA "%s"`, schemaName)).Error)
	t.Cleanup(func() {
		_ = rootDB.Exec(fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schemaName)).Error
		closeGormDB(rootDB)
	})

	db := openPostgresSequenceTestDB(t, postgresDSNWithSearchPath(t, dsn, schemaName))
	t.Cleanup(func() { closeGormDB(db) })
	// 只保留最小列集：没有 upstream_request_id 等后来新增的列。
	require.NoError(t, db.Exec(
		`CREATE TABLE logs (id bigserial PRIMARY KEY, user_id bigint, created_at bigint)`).Error)

	require.NoError(t, ensureLogIndexesMatchModel(db),
		"缺列的索引必须跳过，而不是让启动迁移失败")

	state, err := lookupIndexState(db, "logs", "idx_logs_user_created_at")
	require.NoError(t, err)
	require.True(t, state.exists, "列齐全的索引仍要建出来")

	state, err = lookupIndexState(db, "logs", "idx_logs_upstream_request_created_at_id")
	require.NoError(t, err)
	require.False(t, state.exists, "缺列的索引必须留给 AutoMigrate")
}

// openBareLogsSchema 在独立 schema 里用裸 DDL 建出与 Log 模型列兼容的 logs
// 表，但不建任何索引：postgres 驱动的 CreateTable 会连索引一起建，那样就
// 分不清索引是哪条路径建的。
func openBareLogsSchema(t *testing.T, dsn string, prefix string) (*gorm.DB, func()) {
	t.Helper()

	schemaName := prefix + strconv.FormatInt(time.Now().UnixNano(), 10)
	rootDB := openPostgresSequenceTestDB(t, dsn)
	require.NoError(t, rootDB.Exec(fmt.Sprintf(`CREATE SCHEMA "%s"`, schemaName)).Error)

	db := openPostgresSequenceTestDB(t, postgresDSNWithSearchPath(t, dsn, schemaName))
	require.NoError(t, db.Exec(`CREATE TABLE logs (
		id bigserial PRIMARY KEY,
		user_id bigint,
		created_at bigint,
		type bigint,
		content text,
		username varchar(255) DEFAULT '',
		token_name varchar(255) DEFAULT '',
		model_name varchar(255) DEFAULT '',
		quota bigint DEFAULT 0,
		prompt_tokens bigint DEFAULT 0,
		completion_tokens bigint DEFAULT 0,
		use_time bigint DEFAULT 0,
		is_stream boolean,
		channel_id bigint,
		channel_name varchar(255),
		token_id bigint DEFAULT 0,
		"group" varchar(255),
		ip varchar(255) DEFAULT '',
		request_id varchar(64) DEFAULT '',
		upstream_request_id varchar(128) DEFAULT '',
		other text
	)`).Error)

	return db, func() {
		closeGormDB(db)
		_ = rootDB.Exec(fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schemaName)).Error
		closeGormDB(rootDB)
	}
}
