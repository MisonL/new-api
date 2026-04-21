package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestHeaderStrategyStateAutoMigrate(t *testing.T) {
	db := openTestDB(t, &RequestHeaderStrategyState{})
	require.True(t, db.Migrator().HasTable(&RequestHeaderStrategyState{}))
}

func TestRequestHeaderStrategyStateRejectsDuplicatePrimaryKey(t *testing.T) {
	db := openTestDB(t, &RequestHeaderStrategyState{})

	first := RequestHeaderStrategyState{
		ScopeType: "channel",
		ScopeKey:  "channel:1",
	}
	require.NoError(t, db.Create(&first).Error)

	dup := RequestHeaderStrategyState{
		ScopeType: "channel",
		ScopeKey:  "channel:1",
	}
	require.Error(t, db.Create(&dup).Error)
}
