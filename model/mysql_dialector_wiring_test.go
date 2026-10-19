package model

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// The MySQL correction only takes effect if the production connection path
// actually opens the wrapped dialector. It exists because MySQL reports a
// `uniqueIndex` column as COLUMN_KEY=UNI, which gorm reads back as a UNIQUE
// CONSTRAINT and then tries to drop with DROP FOREIGN KEY; MySQL answers
// Error 1091 and AutoMigrate aborts, so the process cannot start on any boot
// after the table exists.
//
// This pins the wiring, not the correction: opening the stock driver would
// bring the failing reconciliation back even though MySQLDialector and its own
// tests are still present and passing.
func TestChooseDBUsesTheMySQLCorrection(t *testing.T) {
	dialector := mysqlOpenDialector("user:pass@tcp(127.0.0.1:3306)/probe?charset=utf8mb4")

	require.Equal(t, "mysql", dialector.Name())

	db := &gorm.DB{Config: &gorm.Config{NamingStrategy: schema.NamingStrategy{}}}
	migrator, ok := dialector.Migrator(db).(interface {
		MigrateColumnUnique(interface{}, *schema.Field, gorm.ColumnType) error
	})
	require.True(t, ok, "the migrator must expose MigrateColumnUnique")

	// The correction is present when a unique-index column is skipped instead
	// of being reconciled as a constraint it never had.
	require.NoError(t, migrator.MigrateColumnUnique(nil, &schema.Field{
		Name: "Key", DBName: "key", UniqueIndex: "idx_tokens_key",
	}, nil))
}
