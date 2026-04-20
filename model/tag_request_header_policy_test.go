package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func openTestDB(t *testing.T, models ...interface{}) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	if len(models) > 0 {
		require.NoError(t, db.AutoMigrate(models...))
	}

	return db
}

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
