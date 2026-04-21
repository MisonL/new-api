package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUserHeaderTemplateAutoMigrate(t *testing.T) {
	db := openTestDB(t, &UserHeaderTemplate{})
	require.True(t, db.Migrator().HasTable(&UserHeaderTemplate{}))
	require.True(t, db.Migrator().HasIndex(&UserHeaderTemplate{}, "idx_user_header_template_user_updated"))
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

func TestListUserHeaderTemplatesByUserIDOrdersNewestFirst(t *testing.T) {
	db := openTestDB(t, &UserHeaderTemplate{})
	previousDB := DB
	DB = db
	t.Cleanup(func() {
		DB = previousDB
	})

	require.NoError(t, db.Create(&UserHeaderTemplate{
		UserId:    1,
		Name:      "older",
		Content:   `{"X-Test":"older"}`,
		CreatedAt: 1,
		UpdatedAt: 10,
	}).Error)
	require.NoError(t, db.Create(&UserHeaderTemplate{
		UserId:    1,
		Name:      "newer-low-id",
		Content:   `{"X-Test":"newer-low-id"}`,
		CreatedAt: 2,
		UpdatedAt: 20,
	}).Error)
	require.NoError(t, db.Create(&UserHeaderTemplate{
		UserId:    1,
		Name:      "newer-high-id",
		Content:   `{"X-Test":"newer-high-id"}`,
		CreatedAt: 3,
		UpdatedAt: 20,
	}).Error)
	require.NoError(t, db.Create(&UserHeaderTemplate{
		UserId:    2,
		Name:      "other-user",
		Content:   `{"X-Test":"other-user"}`,
		CreatedAt: 4,
		UpdatedAt: 99,
	}).Error)

	templates, err := ListUserHeaderTemplatesByUserID(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, templates, 3)
	require.Equal(t, "newer-high-id", templates[0].Name)
	require.Equal(t, "newer-low-id", templates[1].Name)
	require.Equal(t, "older", templates[2].Name)
}
