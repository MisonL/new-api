package model

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

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
