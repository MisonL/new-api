package model

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestHeaderStrategyStateAutoMigrate(t *testing.T) {
	db := openTestDB(t, &RequestHeaderStrategyState{})
	require.True(t, db.Migrator().HasTable(&RequestHeaderStrategyState{}))
}

func TestRequestHeaderStrategyStateScopeTypeSupportsHeaderProfileRuntimeScope(t *testing.T) {
	db := openTestDB(t, &RequestHeaderStrategyState{})

	state := RequestHeaderStrategyState{
		ScopeType: "channel_header_profile",
		ScopeKey:  "channel:9209:ea8e70a2f2a7c29cc085408f0c1e7df3",
	}
	require.NoError(t, db.Create(&state).Error)

	field, ok := reflect.TypeOf(RequestHeaderStrategyState{}).FieldByName("ScopeType")
	require.True(t, ok)
	require.Contains(t, string(field.Tag), "varchar(64)")

	var createSQL string
	require.NoError(t, db.Raw("SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?", "request_header_strategy_states").Scan(&createSQL).Error)
	require.Contains(t, strings.ToLower(createSQL), "varchar(64)")
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
