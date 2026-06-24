package model

import (
	"context"
	"errors"
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
	require.True(t, db.Migrator().HasColumn(&SyntheticCompactStateRecord{}, "kind"))
	require.True(t, db.Migrator().HasColumn(&SyntheticCompactStateRecord{}, "model_at_creation"))
	require.True(t, db.Migrator().HasColumn(&SyntheticCompactStateRecord{}, "owner_scope"))
	require.True(t, db.Migrator().HasColumn(&SyntheticCompactStateRecord{}, "scope_policy"))
	require.True(t, db.Migrator().HasColumn(&SyntheticCompactStateRecord{}, "upstream_response_id"))
	require.True(t, db.Migrator().HasColumn(&SyntheticCompactStateRecord{}, "source_instance"))
	require.True(t, db.Migrator().HasColumn(&SyntheticCompactStateRecord{}, "state_hash"))
	require.True(t, db.Migrator().HasColumn(&SyntheticCompactStateRecord{}, "last_access_at"))
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

func TestSaveSyntheticCompactStateRecordPopulatesControlFields(t *testing.T) {
	originDB := DB
	t.Cleanup(func() {
		DB = originDB
	})
	DB = openTestDB(t, &SyntheticCompactStateRecord{})

	require.NoError(t, SaveSyntheticCompactStateRecord(context.Background(), SyntheticCompactStateRecord{
		ID:                "resp_newapi_synthcmp_control_fields",
		Model:             "gpt-5.5",
		SummaryCiphertext: "encrypted-control-fields",
		UserID:            10,
		TokenID:           20,
		Group:             "default",
		SourceInstance:    "nabcdef",
		CreatedAt:         time.Now().Add(-time.Hour).Unix(),
		ExpiresAt:         time.Now().Add(time.Hour).Unix(),
	}))

	var record SyntheticCompactStateRecord
	require.NoError(t, DB.Where("id = ?", "resp_newapi_synthcmp_control_fields").First(&record).Error)
	require.Equal(t, SyntheticCompactStateKindSyntheticSummary, record.Kind)
	require.Equal(t, "gpt-5.5", record.ModelAtCreation)
	require.Equal(t, SyntheticCompactOwnerScope(10, 20, "default"), record.OwnerScope)
	require.Equal(t, SyntheticCompactScopePolicyModelFlexible, record.ScopePolicy)
	require.Equal(t, "nabcdef", record.SourceInstance)
	require.NotEmpty(t, record.StateHash)
	require.NotZero(t, record.LastAccessAt)
}

func TestSyntheticCompactStateRecordHashUsesLengthPrefixes(t *testing.T) {
	left := SyntheticCompactStateRecord{
		ID:                "a",
		Kind:              SyntheticCompactStateKindSyntheticSummary,
		ModelAtCreation:   "b\x00c",
		OwnerScope:        "scope",
		ScopePolicy:       SyntheticCompactScopePolicyModelFlexible,
		SummaryCiphertext: "encrypted",
		CreatedAt:         1,
		ExpiresAt:         2,
	}
	right := SyntheticCompactStateRecord{
		ID:                "a\x00b",
		Kind:              SyntheticCompactStateKindSyntheticSummary,
		ModelAtCreation:   "c",
		OwnerScope:        "scope",
		ScopePolicy:       SyntheticCompactScopePolicyModelFlexible,
		SummaryCiphertext: "encrypted",
		CreatedAt:         1,
		ExpiresAt:         2,
	}

	require.NotEqual(t, syntheticCompactStateRecordHash(left), syntheticCompactStateRecordHash(right))
}

func TestGetSyntheticCompactStateRecordUpdatesLastAccessAt(t *testing.T) {
	originDB := DB
	t.Cleanup(func() {
		DB = originDB
	})
	DB = openTestDB(t, &SyntheticCompactStateRecord{})

	now := time.Now().Unix()
	require.NoError(t, SaveSyntheticCompactStateRecord(context.Background(), SyntheticCompactStateRecord{
		ID:                "resp_newapi_synthcmp_access",
		Model:             "gpt-5",
		SummaryCiphertext: "encrypted-access",
		LastAccessAt:      now - 60,
		ExpiresAt:         now + 60,
	}))

	got, found, err := GetSyntheticCompactStateRecord(context.Background(), "resp_newapi_synthcmp_access", now)

	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, got)
	require.Equal(t, now, got.LastAccessAt)
	var record SyntheticCompactStateRecord
	require.NoError(t, DB.Where("id = ?", "resp_newapi_synthcmp_access").First(&record).Error)
	require.Equal(t, now, record.LastAccessAt)
}

func TestGetSyntheticCompactStateRecordDoesNotUpdateRecentLastAccessAt(t *testing.T) {
	originDB := DB
	t.Cleanup(func() {
		DB = originDB
	})
	DB = openTestDB(t, &SyntheticCompactStateRecord{})

	now := time.Now().Unix()
	recentAccess := now - 30
	require.NoError(t, SaveSyntheticCompactStateRecord(context.Background(), SyntheticCompactStateRecord{
		ID:                "resp_newapi_synthcmp_recent_access",
		Model:             "gpt-5",
		SummaryCiphertext: "encrypted-recent-access",
		LastAccessAt:      recentAccess,
		ExpiresAt:         now + 60,
	}))

	got, found, err := GetSyntheticCompactStateRecord(context.Background(), "resp_newapi_synthcmp_recent_access", now)

	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, got)
	require.Equal(t, recentAccess, got.LastAccessAt)
	var record SyntheticCompactStateRecord
	require.NoError(t, DB.Where("id = ?", "resp_newapi_synthcmp_recent_access").First(&record).Error)
	require.Equal(t, recentAccess, record.LastAccessAt)
}

func TestGetSyntheticCompactStateRecordIgnoresMetadataRefreshFailure(t *testing.T) {
	originDB := DB
	t.Cleanup(func() {
		DB = originDB
	})
	DB = openTestDB(t, &SyntheticCompactStateRecord{})

	now := time.Now().Unix()
	lastAccess := now - SyntheticCompactStateLastAccessUpdateIntervalSeconds - 1
	require.NoError(t, SaveSyntheticCompactStateRecord(context.Background(), SyntheticCompactStateRecord{
		ID:                "resp_newapi_synthcmp_refresh_failed",
		Model:             "gpt-5",
		SummaryCiphertext: "encrypted",
		CreatedAt:         now - 120,
		LastAccessAt:      lastAccess,
		ExpiresAt:         now + 3600,
	}))
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(
		"test:block_synthetic_compact_update",
		func(tx *gorm.DB) {
			if tx.Statement != nil && tx.Statement.Schema != nil &&
				tx.Statement.Schema.Name == "SyntheticCompactStateRecord" {
				tx.AddError(errors.New("forced metadata refresh failure"))
			}
		},
	))
	t.Cleanup(func() {
		_ = DB.Callback().Update().Remove("test:block_synthetic_compact_update")
	})

	got, found, err := GetSyntheticCompactStateRecord(context.Background(), "resp_newapi_synthcmp_refresh_failed", now)

	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, got)
	require.Equal(t, "resp_newapi_synthcmp_refresh_failed", got.ID)
	require.Equal(t, lastAccess, got.LastAccessAt)
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
