package model

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
)

// MySQLDialector must keep the driver's optional migrator capabilities, which
// gorm reaches by type assertion. Wrapping the gorm interfaces instead of the
// concrete driver types drops them and panics inside CreateTable.
func TestMySQLDialectorKeepsOptionalMigratorCapabilities(t *testing.T) {
	dialector, ok := MySQLDialector("user:pass@tcp(127.0.0.1:3306)/probe?charset=utf8mb4").(interface{ Migrator(*gorm.DB) gorm.Migrator })
	require.True(t, ok)

	db := &gorm.DB{Config: &gorm.Config{NamingStrategy: schema.NamingStrategy{}}}
	_, ok = dialector.Migrator(db).(interface {
		BuildIndexOptions([]schema.IndexOption, *gorm.Statement) []interface{}
	})
	require.True(t, ok, "the migrator must still expose the driver's BuildIndexOptions")
}

// A `uniqueIndex` column must not be reconciled as a unique CONSTRAINT. MySQL
// reports such a column as COLUMN_KEY=UNI, which gorm reads back as a constraint
// that was never created; it then asks the driver to drop uni_<table>_<column>
// and MySQL answers Error 1091, so the process cannot start on any boot after
// the table exists.
//
// The decision is expressed over the field, so it is asserted here against the
// same predicate the migrator uses rather than against a live server.
func TestUniqueIndexReconciliationPredicate(t *testing.T) {
	// The shape this correction exists for: a unique index, not a constraint.
	require.True(t, skipsUniqueIndexReconciliation(&schema.Field{
		Name: "Key", DBName: "key", UniqueIndex: "idx_tokens_key",
	}))

	// A real `unique` tag, and a column with a unique index declared alongside
	// it, must keep the stock reconciliation.
	require.False(t, skipsUniqueIndexReconciliation(&schema.Field{
		Name: "Slug", DBName: "slug", Unique: true,
	}))
	require.False(t, skipsUniqueIndexReconciliation(&schema.Field{
		Name: "Both", DBName: "both", Unique: true, UniqueIndex: "idx_x",
	}))

	// A plain column keeps it too.
	require.False(t, skipsUniqueIndexReconciliation(&schema.Field{Name: "Name", DBName: "name"}))
}

// The correction must not change how the dialector is identified, nor how it
// hands out the driver's own migrator type. The dialector is built without
// opening a connection: gorm.Open would dial, and this test is about the types
// handed back, not about reaching a server.
func TestMySQLDialectorDelegatesToTheDriver(t *testing.T) {
	dialector := MySQLDialector("user:pass@tcp(127.0.0.1:3306)/probe?charset=utf8mb4")
	require.Equal(t, "mysql", dialector.Name())

	wrapped, ok := dialector.(interface{ Migrator(*gorm.DB) gorm.Migrator })
	require.True(t, ok, "the dialector must still satisfy gorm's Dialector interface")

	// A bare DB is enough for the migrator factory; it is not used for queries.
	db := &gorm.DB{Config: &gorm.Config{NamingStrategy: schema.NamingStrategy{}}}
	require.NotNil(t, wrapped.Migrator(db))
	_ = migrator.Migrator{}
}
