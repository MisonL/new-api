package model

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func useRetryStatusCodesMigrationTestDB(t *testing.T) {
	t.Helper()
	previousDB := DB
	previousLogDB := LOG_DB
	db := openTestDB(t, &Option{})
	DB = db
	LOG_DB = db
	t.Cleanup(func() {
		DB = previousDB
		LOG_DB = previousLogDB
	})
}

func retryStatusCodesOptionValue(t *testing.T) (string, bool) {
	t.Helper()
	var option Option
	err := DB.Where(KeyColumn()+" = ?", "AutomaticRetryStatusCodes").Limit(1).Find(&option).Error
	require.NoError(t, err)
	if option.Key == "" {
		return "", false
	}
	return option.Value, true
}

// 历史出厂默认值把 408/504/524 排除在重试之外。数据库里存着这些原样默认值的
// 实例（管理员只是保存过一次设置页，并未自定义）必须升级到新出厂规则，否则
// 启动时数据库值会覆盖代码默认值，代码层的放开对它们永远不生效。
func TestMigrateRetryStatusCodesUpgradesStoredLegacyDefaults(t *testing.T) {
	for _, legacy := range []string{
		"100-199,300-399,401-407,409-499,500-503,505-523,525-599",
		"100-199,300-399,400-407,409-499,500-503,505-523,525-599",
	} {
		legacy := legacy
		t.Run(legacy, func(t *testing.T) {
			useRetryStatusCodesMigrationTestDB(t)
			require.NoError(t, DB.Create(&Option{Key: "AutomaticRetryStatusCodes", Value: legacy}).Error)

			require.NoError(t, migrateRetryStatusCodesLegacyDefault())

			value, ok := retryStatusCodesOptionValue(t)
			require.True(t, ok)
			require.Equal(t, operation_setting.DefaultAutomaticRetryStatusCodes(), value)
		})
	}
}

// 管理员自定义过的范围是运营决定，迁移绝不能改写。
func TestMigrateRetryStatusCodesKeepsAdministratorCustomValue(t *testing.T) {
	useRetryStatusCodesMigrationTestDB(t)
	custom := "429,500-503"
	require.NoError(t, DB.Create(&Option{Key: "AutomaticRetryStatusCodes", Value: custom}).Error)

	require.NoError(t, migrateRetryStatusCodesLegacyDefault())

	value, ok := retryStatusCodesOptionValue(t)
	require.True(t, ok)
	require.Equal(t, custom, value)
}

// 从未保存过该选项的实例直接使用代码出厂规则，迁移不应凭空写入一行。
func TestMigrateRetryStatusCodesDoesNotCreateMissingOption(t *testing.T) {
	useRetryStatusCodesMigrationTestDB(t)

	require.NoError(t, migrateRetryStatusCodesLegacyDefault())

	_, ok := retryStatusCodesOptionValue(t)
	require.False(t, ok)
}

// 迁移只执行一次：管理员在迁移之后又手动改回旧范围，下次启动必须尊重这个选择。
func TestMigrateRetryStatusCodesRunsOnlyOnce(t *testing.T) {
	useRetryStatusCodesMigrationTestDB(t)
	legacy := "100-199,300-399,400-407,409-499,500-503,505-523,525-599"
	require.NoError(t, DB.Create(&Option{Key: "AutomaticRetryStatusCodes", Value: legacy}).Error)
	require.NoError(t, migrateRetryStatusCodesLegacyDefault())

	require.NoError(t, DB.Model(&Option{}).Where(KeyColumn()+" = ?", "AutomaticRetryStatusCodes").Update("value", legacy).Error)
	require.NoError(t, migrateRetryStatusCodesLegacyDefault())

	value, ok := retryStatusCodesOptionValue(t)
	require.True(t, ok)
	require.Equal(t, legacy, value)

	var marker Option
	require.NoError(t, DB.Where(KeyColumn()+" = ?", retryStatusCodesLegacyDefaultMigrationOptionKey).First(&marker).Error)
	require.Equal(t, "done", marker.Value)
}

// 两个 master 同时启动时，双方都会先读到"无标记"、再进入事务写标记。
// 标记写入必须对主键冲突幂等：后写入者若以主键冲突报错退出，
// runDBMigration 会让整个进程启动失败。
func TestMigrateRetryStatusCodesMarkerWriteIsIdempotent(t *testing.T) {
	useRetryStatusCodesMigrationTestDB(t)
	legacy := "100-199,300-399,400-407,409-499,500-503,505-523,525-599"
	require.NoError(t, DB.Create(&Option{Key: "AutomaticRetryStatusCodes", Value: legacy}).Error)

	// 模拟竞争：第一次事务已完成（标记已写、选项已升级），第二个 master 的
	// 事务在读完"无标记"之后才执行到这里，重复执行迁移主体。
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return applyRetryStatusCodesLegacyDefault(tx)
	}))
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return applyRetryStatusCodesLegacyDefault(tx)
	}), "标记已存在时重复写入必须幂等，不能以主键冲突中断启动")
}
