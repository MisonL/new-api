package model

import (
	"fmt"
	"io"
	"log"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func openTestDB(t *testing.T, models ...interface{}) *gorm.DB {
	t.Helper()

	// DSN 必须对每次调用唯一。t.Name() 只在同一个测试函数内稳定，而
	// -count=N 会把同一函数重跑 N 轮：共享内存库（cache=shared）在上一轮
	// 没有任何 close 的情况下仍然存活，第二轮 AutoMigrate 与插入直接撞上
	// 一轮遗留的表和行，报 UNIQUE constraint failed。子测试的 t.Name() 含
	// 父路径，天然区分；这里补上单调序号覆盖 count 复用。
	testDBSequenceMu.Lock()
	testDBSequence++
	sequence := testDBSequence
	testDBSequenceMu.Unlock()

	dsn := fmt.Sprintf("file:%s_%d?mode=memory&cache=shared",
		strings.ReplaceAll(t.Name(), "/", "_"), sequence)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormlogger.New(log.New(io.Discard, "", 0), gormlogger.Config{
			LogLevel:                  gormlogger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		}),
	})
	require.NoError(t, err)

	if len(models) > 0 {
		require.NoError(t, db.AutoMigrate(models...))
	}

	return db
}

var testDBSequenceMu sync.Mutex
var testDBSequence int64

func TestTagRequestHeaderPolicyAutoMigrate(t *testing.T) {
	db := openTestDB(t, &TagRequestHeaderPolicy{}, &RequestHeaderStrategyState{}, &UserHeaderTemplate{})
	require.True(t, db.Migrator().HasTable(&TagRequestHeaderPolicy{}))
	require.True(t, db.Migrator().HasTable(&RequestHeaderStrategyState{}))
	require.True(t, db.Migrator().HasTable(&UserHeaderTemplate{}))
}

func TestTagRequestHeaderPolicyDefaultValuesPersist(t *testing.T) {
	db := openTestDB(t, &TagRequestHeaderPolicy{})

	record := TagRequestHeaderPolicy{Tag: "tag-default"}
	require.NoError(t, db.Create(&record).Error)

	var got TagRequestHeaderPolicy
	require.NoError(t, db.First(&got, "tag = ?", "tag-default").Error)
	require.Equal(t, "system_default", got.HeaderPolicyMode)
	require.False(t, got.OverrideHeaderUserAgent)
	require.Empty(t, got.UserAgentStrategyJSON)
}

func TestTagRequestHeaderPolicyTagDoesNotHardcodeLegacyLength(t *testing.T) {
	field, ok := reflect.TypeOf(TagRequestHeaderPolicy{}).FieldByName("Tag")
	require.True(t, ok)
	require.NotContains(t, string(field.Tag), "varchar(128)")
}
