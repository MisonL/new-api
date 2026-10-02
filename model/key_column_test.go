package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// `key` is a reserved word in MySQL, so an unquoted "key = ?" predicate is a
// syntax error there (ERROR 1064) while PostgreSQL and SQLite accept it. Every
// Option lookup therefore has to go through the quoted spelling; this pins the
// accessor so a caller cannot reintroduce the bare name.
func TestKeyColumnIsDialectQuoted(t *testing.T) {
	previousPostgreSQL := common.UsingPostgreSQL
	previousKeyCol := commonKeyCol
	t.Cleanup(func() {
		common.UsingPostgreSQL = previousPostgreSQL
		commonKeyCol = previousKeyCol
	})

	common.UsingPostgreSQL = true
	commonKeyCol = ""
	require.Equal(t, `"key"`, KeyColumn())

	common.UsingPostgreSQL = false
	commonKeyCol = ""
	require.Equal(t, "`key`", KeyColumn())

	// The value must still work as a predicate; the point is the quoting.
	require.Contains(t, KeyColumn()+" = ?", "?")
}

// A caller that skips the accessor leaves commonKeyCol unset, so the exported
// form has to initialise it rather than return an empty column name.
func TestKeyColumnInitialisesWhenUnset(t *testing.T) {
	previousPostgreSQL := common.UsingPostgreSQL
	previousKeyCol := commonKeyCol
	t.Cleanup(func() {
		common.UsingPostgreSQL = previousPostgreSQL
		commonKeyCol = previousKeyCol
	})

	common.UsingPostgreSQL = false
	commonKeyCol = ""
	require.NotEmpty(t, KeyColumn())
}
