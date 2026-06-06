package model

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestSyntheticCompactStateRecordAutoMigrate(t *testing.T) {
	db := openTestDB(t, &SyntheticCompactStateRecord{})

	require.True(t, db.Migrator().HasTable(&SyntheticCompactStateRecord{}))
	require.True(t, db.Migrator().HasColumn(&SyntheticCompactStateRecord{}, "group_name"))
	require.True(t, db.Migrator().HasColumn(&SyntheticCompactStateRecord{}, "expires_at"))
}

func TestSyntheticCompactSummaryCiphertextDBType(t *testing.T) {
	require.Equal(t, "MEDIUMTEXT", SyntheticCompactSummaryCiphertextDBType("mysql"))
	require.Equal(t, "TEXT", SyntheticCompactSummaryCiphertextDBType("postgres"))
	require.Equal(t, "TEXT", SyntheticCompactSummaryCiphertextDBType("sqlite"))
}

func TestSaveSyntheticCompactStateRecordRejectsInvalidInput(t *testing.T) {
	originDB := DB
	t.Cleanup(func() {
		DB = originDB
	})
	DB = openTestDB(t, &SyntheticCompactStateRecord{})

	tests := []struct {
		name   string
		record SyntheticCompactStateRecord
	}{
		{
			name: "empty id",
			record: SyntheticCompactStateRecord{
				SummaryCiphertext: "encrypted",
			},
		},
		{
			name: "blank id",
			record: SyntheticCompactStateRecord{
				ID:                "   ",
				SummaryCiphertext: "encrypted",
			},
		},
		{
			name: "empty summary ciphertext",
			record: SyntheticCompactStateRecord{
				ID: "resp_newapi_synthcmp_invalid",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := SaveSyntheticCompactStateRecord(context.Background(), tc.record)
			require.Error(t, err)

			var count int64
			require.NoError(t, DB.Model(&SyntheticCompactStateRecord{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestGetSyntheticCompactStateRecordIgnoresExpiredRecordWithoutDeleting(t *testing.T) {
	originDB := DB
	t.Cleanup(func() {
		DB = originDB
	})
	DB = openTestDB(t, &SyntheticCompactStateRecord{})

	now := time.Now().Unix()
	require.NoError(t, SaveSyntheticCompactStateRecord(context.Background(), SyntheticCompactStateRecord{
		ID:                "resp_newapi_synthcmp_expired",
		Model:             "gpt-5",
		SummaryCiphertext: "encrypted-expired",
		ExpiresAt:         now - 1,
	}))

	got, found, err := GetSyntheticCompactStateRecord(context.Background(), "resp_newapi_synthcmp_expired", now)

	require.NoError(t, err)
	require.False(t, found)
	require.Nil(t, got)

	var count int64
	require.NoError(t, DB.Model(&SyntheticCompactStateRecord{}).Where("id = ?", "resp_newapi_synthcmp_expired").Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestGetSyntheticCompactStateRecordKeepsNeverExpireRecord(t *testing.T) {
	originDB := DB
	t.Cleanup(func() {
		DB = originDB
	})
	DB = openTestDB(t, &SyntheticCompactStateRecord{})

	require.NoError(t, SaveSyntheticCompactStateRecord(context.Background(), SyntheticCompactStateRecord{
		ID:                "resp_newapi_synthcmp_noexpire",
		Model:             "gpt-5",
		SummaryCiphertext: "encrypted-noexpire",
		ExpiresAt:         0,
	}))

	got, found, err := GetSyntheticCompactStateRecord(context.Background(), "resp_newapi_synthcmp_noexpire", time.Now().Add(365*24*time.Hour).Unix())

	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, got)
	require.Equal(t, "resp_newapi_synthcmp_noexpire", got.ID)
}

func TestPruneExpiredSyntheticCompactStateRecordsKeepsValidRecord(t *testing.T) {
	originDB := DB
	t.Cleanup(func() {
		DB = originDB
	})
	DB = openTestDB(t, &SyntheticCompactStateRecord{})

	now := time.Now().Unix()
	require.NoError(t, SaveSyntheticCompactStateRecord(context.Background(), SyntheticCompactStateRecord{
		ID:                "resp_newapi_synthcmp_expired",
		Model:             "gpt-5",
		SummaryCiphertext: "encrypted-expired",
		ExpiresAt:         now - 1,
	}))
	require.NoError(t, SaveSyntheticCompactStateRecord(context.Background(), SyntheticCompactStateRecord{
		ID:                "resp_newapi_synthcmp_valid",
		Model:             "gpt-5",
		SummaryCiphertext: "encrypted-valid",
		ExpiresAt:         now + 60,
	}))
	require.NoError(t, SaveSyntheticCompactStateRecord(context.Background(), SyntheticCompactStateRecord{
		ID:                "resp_newapi_synthcmp_noexpire",
		Model:             "gpt-5",
		SummaryCiphertext: "encrypted-noexpire",
		ExpiresAt:         0,
	}))

	deleted, err := PruneExpiredSyntheticCompactStateRecords(context.Background(), now)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)

	var expiredCount int64
	require.NoError(t, DB.Model(&SyntheticCompactStateRecord{}).Where("id = ?", "resp_newapi_synthcmp_expired").Count(&expiredCount).Error)
	require.Zero(t, expiredCount)

	var validCount int64
	require.NoError(t, DB.Model(&SyntheticCompactStateRecord{}).Where("id = ?", "resp_newapi_synthcmp_valid").Count(&validCount).Error)
	require.EqualValues(t, 1, validCount)

	var noExpireCount int64
	require.NoError(t, DB.Model(&SyntheticCompactStateRecord{}).Where("id = ?", "resp_newapi_synthcmp_noexpire").Count(&noExpireCount).Error)
	require.EqualValues(t, 1, noExpireCount)
}

func TestPruneExpiredSyntheticCompactStateRecordsDeletesInBatches(t *testing.T) {
	originDB := DB
	t.Cleanup(func() {
		DB = originDB
	})
	DB = openTestDB(t, &SyntheticCompactStateRecord{})

	now := time.Now().Unix()
	for i := 0; i < SyntheticCompactStatePruneBatchSize+1; i++ {
		require.NoError(t, SaveSyntheticCompactStateRecord(context.Background(), SyntheticCompactStateRecord{
			ID:                "resp_newapi_synthcmp_batch_" + strconv.Itoa(i),
			Model:             "gpt-5",
			SummaryCiphertext: "encrypted-expired",
			ExpiresAt:         now - 1,
		}))
	}

	deleted, err := PruneExpiredSyntheticCompactStateRecords(context.Background(), now)
	require.NoError(t, err)
	require.EqualValues(t, SyntheticCompactStatePruneBatchSize+1, deleted)

	var count int64
	require.NoError(t, DB.Model(&SyntheticCompactStateRecord{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestPruneExpiredSyntheticCompactStateRecordsStopsWhenDeleteMakesNoProgress(t *testing.T) {
	originDB := DB
	t.Cleanup(func() {
		DB = originDB
	})
	DB = openTestDB(t, &SyntheticCompactStateRecord{})

	now := time.Now().Unix()
	require.NoError(t, SaveSyntheticCompactStateRecord(context.Background(), SyntheticCompactStateRecord{
		ID:                "resp_newapi_synthcmp_delete_blocked",
		Model:             "gpt-5",
		SummaryCiphertext: "encrypted-expired",
		ExpiresAt:         now - 1,
	}))
	require.NoError(t, DB.Callback().Delete().Before("gorm:delete").Register(
		"test:block_synthetic_compact_delete",
		func(tx *gorm.DB) {
			if tx.Statement != nil && tx.Statement.Schema != nil &&
				tx.Statement.Schema.Name == "SyntheticCompactStateRecord" {
				tx.Statement.SQL.Reset()
				tx.Statement.Vars = nil
				tx.Statement.AddClause(clause.Where{
					Exprs: []clause.Expression{clause.Expr{SQL: "1 = 0"}},
				})
			}
		},
	))

	deleted, err := PruneExpiredSyntheticCompactStateRecords(context.Background(), now)

	require.Error(t, err)
	require.Contains(t, err.Error(), "made no progress")
	require.Zero(t, deleted)
}
