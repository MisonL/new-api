package model

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestInsertChannelSynchronizesDriftedPostgresSequence(t *testing.T) {
	dsn := postgresSequenceTestDSN()
	if dsn == "" {
		t.Skip("set NEW_API_POSTGRES_TEST_DSN or POSTGRES_TEST_DSN to run PostgreSQL sequence test")
	}

	schemaName := "channel_sequence_test_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	rootDB := openPostgresSequenceTestDB(t, dsn)
	require.NoError(t, rootDB.Exec(fmt.Sprintf(`CREATE SCHEMA "%s"`, schemaName)).Error)
	t.Cleanup(func() {
		_ = rootDB.Exec(fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schemaName)).Error
		closeGormDB(rootDB)
	})

	testDB := openPostgresSequenceTestDB(t, postgresDSNWithSearchPath(t, dsn, schemaName))
	setupPostgresSequenceModelDB(t, testDB)
	require.NoError(t, DB.AutoMigrate(&Channel{}, &Ability{}))

	source := &Channel{
		Id:            100,
		Type:          0,
		Key:           "sk-source",
		Status:        common.ChannelStatusEnabled,
		Name:          "source",
		Models:        "gpt-5",
		Group:         "default",
		OtherSettings: "{}",
	}
	require.NoError(t, source.Insert())

	sequenceName := ""
	require.NoError(t, DB.Raw("SELECT pg_get_serial_sequence('channels', 'id')").Scan(&sequenceName).Error)
	require.NotEmpty(t, sequenceName)
	require.NoError(t, DB.Exec("SELECT setval(?::regclass, 1, true)", sequenceName).Error)

	inserted := &Channel{
		Type:          0,
		Key:           "sk-inserted",
		Status:        common.ChannelStatusEnabled,
		Name:          "inserted",
		Models:        "gpt-5",
		Group:         "default",
		OtherSettings: "{}",
	}
	require.NoError(t, inserted.Insert())
	require.Equal(t, 101, inserted.Id)

	var ability Ability
	require.NoError(t, DB.First(&ability, "channel_id = ?", inserted.Id).Error)
}

func postgresSequenceTestDSN() string {
	if dsn := os.Getenv("NEW_API_POSTGRES_TEST_DSN"); dsn != "" {
		return dsn
	}
	return os.Getenv("POSTGRES_TEST_DSN")
}

func openPostgresSequenceTestDB(t *testing.T, dsn string) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  dsn,
		PreferSimpleProtocol: true,
	}), &gorm.Config{})
	require.NoError(t, err)
	return db
}

func postgresDSNWithSearchPath(t *testing.T, dsn string, schemaName string) string {
	t.Helper()

	if strings.Contains(dsn, "://") {
		parsed, err := url.Parse(dsn)
		require.NoError(t, err)
		query := parsed.Query()
		query.Set("options", "-c search_path="+schemaName+",public")
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	return strings.TrimSpace(dsn) + " options='-c search_path=" + schemaName + ",public'"
}

func setupPostgresSequenceModelDB(t *testing.T, testDB *gorm.DB) {
	t.Helper()

	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL
	previousDB := DB
	previousLogDB := LOG_DB

	common.UsingSQLite = false
	common.UsingMySQL = false
	common.UsingPostgreSQL = true
	DB = testDB
	LOG_DB = testDB
	initCol()

	t.Cleanup(func() {
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
		DB = previousDB
		LOG_DB = previousLogDB
		initCol()
		closeGormDB(testDB)
	})
}

func closeGormDB(db *gorm.DB) {
	sqlDB, err := db.DB()
	if err == nil {
		_ = sqlDB.Close()
	}
}
