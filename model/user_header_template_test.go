package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUserHeaderTemplateAutoMigrate(t *testing.T) {
	db := openTestDB(t, &UserHeaderTemplate{})
	require.True(t, db.Migrator().HasTable(&UserHeaderTemplate{}))
}

func TestUserHeaderTemplateUniqueIndex(t *testing.T) {
	db := openTestDB(t, &UserHeaderTemplate{})

	first := UserHeaderTemplate{
		UserId: 1,
		Name:   "default",
	}
	require.NoError(t, db.Create(&first).Error)

	dup := UserHeaderTemplate{
		UserId: 1,
		Name:   "default",
	}
	require.Error(t, db.Create(&dup).Error)
}
