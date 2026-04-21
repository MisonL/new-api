package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrefillGroupAutoMigrateCreatesCompositeIndex(t *testing.T) {
	db := openTestDB(t, &PrefillGroup{})
	require.True(t, db.Migrator().HasTable(&PrefillGroup{}))
	require.True(t, db.Migrator().HasIndex(&PrefillGroup{}, "idx_prefill_groups_type_deleted_updated"))
}

func TestGetAllPrefillGroupsContextOrdersByUpdatedTimeDesc(t *testing.T) {
	db := openTestDB(t, &PrefillGroup{})
	previousDB := DB
	DB = db
	t.Cleanup(func() {
		DB = previousDB
	})

	require.NoError(t, db.Create(&PrefillGroup{
		Name:        "older",
		Type:        "model",
		Items:       JSONValue(`["a"]`),
		CreatedTime: 1,
		UpdatedTime: 10,
	}).Error)
	require.NoError(t, db.Create(&PrefillGroup{
		Name:        "newer",
		Type:        "model",
		Items:       JSONValue(`["b"]`),
		CreatedTime: 2,
		UpdatedTime: 20,
	}).Error)
	require.NoError(t, db.Create(&PrefillGroup{
		Name:        "other-type",
		Type:        "tag",
		Items:       JSONValue(`["c"]`),
		CreatedTime: 3,
		UpdatedTime: 99,
	}).Error)

	groups, err := GetAllPrefillGroupsContext(context.Background(), "model")
	require.NoError(t, err)
	require.Len(t, groups, 2)
	require.Equal(t, "newer", groups[0].Name)
	require.Equal(t, "older", groups[1].Name)
}
