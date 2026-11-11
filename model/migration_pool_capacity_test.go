package model

import (
	"testing"

	"gorm.io/gorm"

	"github.com/stretchr/testify/require"
)

// 迁移锁连接来自同一个连接池（sqlDB.Conn）。若池上限为 1，持锁后迁移 DDL
// 永远等不到连接，启动挂死（SQL_MAX_OPEN_CONNS=1）。迁移期间必须保证池内
// 至少有 2 个连接的容量，并在释放锁时还原。
func TestEnsureMigrationPoolCapacityRaisesAndRestores(t *testing.T) {
	db, err := gormOpenSQLiteForPoolTest(t)
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	restore, changed := ensureMigrationPoolCapacity(sqlDB)
	require.True(t, changed, "上限为 1 时必须抬升容量")
	require.Equal(t, 2, int(sqlDB.Stats().MaxOpenConnections),
		"迁移期间池容量至少为 2（持锁连接 + 工作连接）")

	restore()
	require.Equal(t, 1, int(sqlDB.Stats().MaxOpenConnections), "释放锁后必须还原")
}

func TestEnsureMigrationPoolCapacityKeepsUnlimitedAndLargePools(t *testing.T) {
	db, err := gormOpenSQLiteForPoolTest(t)
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)

	sqlDB.SetMaxOpenConns(0) // 0 = 不限制
	restore, changed := ensureMigrationPoolCapacity(sqlDB)
	require.False(t, changed, "不限制容量的池不得改动")
	restore()
	require.Equal(t, 0, int(sqlDB.Stats().MaxOpenConnections))

	sqlDB.SetMaxOpenConns(10)
	restore, changed = ensureMigrationPoolCapacity(sqlDB)
	require.False(t, changed, "容量充足的池不得改动")
	restore()
	require.Equal(t, 10, int(sqlDB.Stats().MaxOpenConnections))
}

func gormOpenSQLiteForPoolTest(t *testing.T) (*gorm.DB, error) {
	return openTestDB(t), nil
}
