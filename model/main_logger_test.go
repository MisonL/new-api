package model

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestNewGormLoggerIgnoresRecordNotFound(t *testing.T) {
	var output bytes.Buffer
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: newGormLogger(&output),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&PasskeyCredential{}))

	var credential PasskeyCredential
	err = db.Where("user_id = ?", 404).First(&credential).Error
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	require.NotContains(t, output.String(), "record not found")
}

func TestRunDBMigrationUsesMigrationSlowThresholdAndRestoresLogger(t *testing.T) {
	var output bytes.Buffer
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: newGormLogger(&output),
	})
	require.NoError(t, err)
	originalLogger := db.Config.Logger

	err = runDBMigration("test", db, func() error {
		require.NotSame(t, originalLogger, db.Config.Logger)
		return db.Exec("SELECT 1").Error
	})

	require.NoError(t, err)
	require.Same(t, originalLogger, db.Config.Logger)
}

func TestNewGormLoggerWithSlowThresholdKeepsRecordNotFoundFiltering(t *testing.T) {
	var output bytes.Buffer
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: newGormLoggerWithSlowThreshold(&output, 2*time.Second),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&PasskeyCredential{}))

	var credential PasskeyCredential
	err = db.Where("user_id = ?", 404).First(&credential).Error
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	require.NotContains(t, output.String(), "record not found")
}

func TestBackgroundDBUsesMaintenanceSlowThreshold(t *testing.T) {
	var output bytes.Buffer
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: newGormLogger(&output),
	})
	require.NoError(t, err)

	previousDB := DB
	DB = db
	t.Cleanup(func() {
		DB = previousDB
	})

	require.NoError(t, backgroundDB().Exec("SELECT 1").Error)
	require.NotContains(t, output.String(), "SLOW SQL")
}
