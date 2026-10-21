package model

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// SQLite 分支是新代码里唯一走 pragma 与 migrator.CreateIndex 的路径，
// PostgreSQL 门控测试覆盖不到。这里在全新内存库上验证：建出全部索引、
// 列顺序与模型声明一致、第二次调用幂等。
func TestEnsureLogIndexesMatchModelOnSQLite(t *testing.T) {
	db := openTestDB(t, &Log{})

	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })

	require.True(t, db.Migrator().HasTable(&Log{}), "probe table must exist first")

	// openTestDB 已建好索引；先全部删掉，模拟需要补齐的库。
	declared := parsedLogIndexColumns(t)
	for name := range declared {
		if db.Migrator().HasIndex(&Log{}, name) {
			require.NoError(t, db.Migrator().DropIndex(&Log{}, name))
		}
	}

	require.NoError(t, ensureLogIndexesMatchModel(db))
	for name, columns := range declared {
		state, err := lookupIndexState(db, "logs", name)
		require.NoError(t, err, "index %s", name)
		require.True(t, state.exists, "index %s must exist after migration", name)
		require.Equal(t, columns, state.columns, "index %s must match the declared columns", name)
	}

	// 幂等：全部有效且列一致时不得重建。
	require.NoError(t, ensureLogIndexesMatchModel(db))
	state, err := lookupIndexState(db, "logs", "idx_logs_user_type_created_at")
	require.NoError(t, err)
	require.Equal(t, []string{"user_id", "type", "created_at"}, state.columns)
}

// MySQL 分支走 migrator 的 CREATE INDEX（本仓库的 MySQLDialector 包装）。
// 这条路径在 SQLite 上验证不到，需要真实 MySQL。
func TestEnsureLogIndexesMatchModelOnMySQL(t *testing.T) {
	db := openLogIndexMySQLDB(t)

	declared := parsedLogIndexColumns(t)
	for name := range declared {
		if db.Migrator().HasIndex(&Log{}, name) {
			require.NoError(t, db.Migrator().DropIndex(&Log{}, name))
		}
	}

	require.NoError(t, ensureLogIndexesMatchModel(db))
	for name, columns := range declared {
		state, err := lookupIndexState(db, "logs", name)
		require.NoError(t, err, "index %s", name)
		require.True(t, state.exists, "index %s must exist after migration", name)
		require.Equal(t, columns, state.columns, "index %s must match the declared columns", name)
	}

	// 列形状不一致的索引必须重建（MySQL 上没有 INVALID 状态，只测形状）。
	const probe = "idx_logs_user_type_created_at"
	require.NoError(t, db.Migrator().DropIndex(&Log{}, probe))
	require.NoError(t, db.Exec("CREATE INDEX "+probe+" ON logs(user_id, created_at)").Error)
	require.NoError(t, ensureLogIndexesMatchModel(db))
	state, err := lookupIndexState(db, "logs", probe)
	require.NoError(t, err)
	require.Equal(t, []string{"user_id", "type", "created_at"}, state.columns)
}

func openLogIndexMySQLDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := os.Getenv("NEW_API_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NEW_API_TEST_MYSQL_DSN not set; MySQL index creation not covered by this run")
	}
	db, err := gorm.Open(MySQLDialector(dsn), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)

	previous := DB
	DB = db
	t.Cleanup(func() {
		_ = db.Exec("DROP TABLE IF EXISTS logs").Error
		DB = previous
	})
	require.NoError(t, db.Exec("DROP TABLE IF EXISTS logs").Error)
	require.NoError(t, db.AutoMigrate(&Log{}))
	return db
}
