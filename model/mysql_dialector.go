package model

import (
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// mysqlDialector is this project's MySQL dialector. It is the driver's own
// dialector with one correction to the schema migrator.
//
// A `uniqueIndex` tag creates a unique INDEX named idx_<table>_<column>, but
// MySQL reports such a column as COLUMN_KEY=UNI, which gorm reads back as a
// UNIQUE CONSTRAINT. gorm then reconciles the difference by dropping a
// constraint named uni_<table>_<column> that was never created; the MySQL
// driver emits DROP FOREIGN KEY for it and the server answers Error 1091. That
// aborts the migration, so the process cannot start at all on any boot after
// the table already exists.
//
// The unique index itself is created and dropped by the ordinary index path, so
// there is no unique constraint for this code to reconcile. Columns declared
// with a real `unique` tag keep the stock behaviour.
//
// The embedded dialector and migrator are the concrete driver types, not the
// gorm interfaces: gorm type-asserts its migrator for optional capabilities
// (for example migrator.BuildIndexOptionsInterface) and an interface wrapper
// would drop them.
type mysqlDialector struct {
	*mysql.Dialector
}

// MySQLDialector returns the MySQL dialector this project opens connections
// with, so tests exercise the same migrator as the server.
func MySQLDialector(dsn string) gorm.Dialector {
	return mysqlDialector{Dialector: mysql.Open(dsn).(*mysql.Dialector)}
}

// mysqlOpenDialector is the dialector chooseDB must hand to gorm.Open.
//
// It is the single production entry point on purpose: MySQLDialector and its
// tests can be green while chooseDB still opens the stock driver, in which case
// the unique-index correction never runs and MySQL startup fails with Error
// 1091. Keeping one function means the wiring is asserted by calling it.
func mysqlOpenDialector(dsn string) gorm.Dialector {
	return MySQLDialector(dsn)
}

func (d mysqlDialector) Migrator(db *gorm.DB) gorm.Migrator {
	return mysqlMigrator{Migrator: d.Dialector.Migrator(db).(mysql.Migrator)}
}

type mysqlMigrator struct {
	mysql.Migrator
}

func (m mysqlMigrator) MigrateColumnUnique(value interface{}, field *schema.Field, columnType gorm.ColumnType) error {
	if skipsUniqueIndexReconciliation(field) {
		return nil
	}
	return m.Migrator.MigrateColumnUnique(value, field, columnType)
}

// skipsUniqueIndexReconciliation reports whether the field's uniqueness comes
// from a unique index rather than from a `unique` constraint. There is no
// constraint for gorm's reconciliation to add or drop in that case, and asking
// MySQL to drop the constraint it assumes exists fails with Error 1091.
func skipsUniqueIndexReconciliation(field *schema.Field) bool {
	return field != nil && field.UniqueIndex != "" && !field.Unique
}
