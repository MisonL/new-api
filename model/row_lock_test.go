package model

import (
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type rowLockProbe struct {
	Id      int    `gorm:"primaryKey"`
	TradeNo string `gorm:"column:trade_no"`
}

// A row lock is only a lock if the dialect emits it. The previous spelling,
// Set("gorm:query_option", "FOR UPDATE"), is a GORM v1 setting that GORM v2
// never reads, so every payment "locked" read ran unlocked. This pins both
// halves of the replacement: the clause is present where it works, and absent
// where the driver rejects it.
func TestForUpdateEmitsLockClausePerDialect(t *testing.T) {
	sdb, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{DryRun: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	var a rowLockProbe
	sqliteSQL := forUpdate(sdb.Session(&gorm.Session{DryRun: true})).
		Where("trade_no = ?", "T1").First(&a).Statement.SQL.String()
	if strings.Contains(sqliteSQL, "FOR UPDATE") {
		t.Fatalf("sqlite must not receive FOR UPDATE (driver rejects it): %s", sqliteSQL)
	}

	pdb, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  "host=unused user=unused dbname=unused sslmode=disable",
		PreferSimpleProtocol: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	var b rowLockProbe
	postgresSQL := forUpdate(pdb.Session(&gorm.Session{DryRun: true})).
		Where("trade_no = ?", "T1").First(&b).Statement.SQL.String()
	if !strings.Contains(postgresSQL, "FOR UPDATE") {
		t.Fatalf("postgres row lock is missing; the read is unprotected: %s", postgresSQL)
	}
}
