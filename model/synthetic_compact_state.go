package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

const SyntheticCompactStatePruneBatchSize = 1000
const SyntheticCompactStateLastAccessUpdateIntervalSeconds = 60

const (
	SyntheticCompactStateKindSyntheticSummary = "synthetic_summary"
	SyntheticCompactStateKindNativeOpaque     = "native_opaque"

	SyntheticCompactScopePolicyModelFlexible = "model_flexible"
	SyntheticCompactScopePolicyStrict        = "strict"
	SyntheticCompactOwnerScopeUnbound        = "unbound"
)

type SyntheticCompactSummaryCiphertext string

func (SyntheticCompactSummaryCiphertext) GormDataType() string {
	return "text"
}

func (SyntheticCompactSummaryCiphertext) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db == nil || db.Dialector == nil {
		return "TEXT"
	}
	return SyntheticCompactSummaryCiphertextDBType(db.Dialector.Name())
}

func SyntheticCompactSummaryCiphertextDBType(dialect string) string {
	if strings.EqualFold(strings.TrimSpace(dialect), "mysql") {
		return "MEDIUMTEXT"
	}
	return "TEXT"
}

type SyntheticCompactStateRecord struct {
	ID                 string                            `gorm:"primaryKey;type:varchar(96)"`
	Kind               string                            `gorm:"type:varchar(32);index"`
	Model              string                            `gorm:"type:varchar(191);index"`
	ModelAtCreation    string                            `gorm:"type:varchar(191);index"`
	SummaryCiphertext  SyntheticCompactSummaryCiphertext `gorm:"not null"`
	OwnerScope         string                            `gorm:"type:varchar(191);index"`
	ScopePolicy        string                            `gorm:"type:varchar(64);index"`
	UpstreamResponseID string                            `gorm:"type:varchar(191);index"`
	UserID             int                               `gorm:"index"`
	TokenID            int                               `gorm:"index"`
	Group              string                            `gorm:"column:group_name;type:varchar(191);index"`
	ChannelID          int                               `gorm:"index"`
	ChannelType        int                               `gorm:"index"`
	SourceInstance     string                            `gorm:"type:varchar(96);index"`
	StateHash          string                            `gorm:"type:varchar(64);index"`
	CreatedAt          int64                             `gorm:"type:bigint;index"`
	UpdatedAt          int64                             `gorm:"type:bigint"`
	LastAccessAt       int64                             `gorm:"type:bigint;index"`
	ExpiresAt          int64                             `gorm:"type:bigint;index"`
}

func SyntheticCompactOwnerScope(userID int, tokenID int, group string) string {
	group = strings.TrimSpace(group)
	if userID == 0 && tokenID == 0 && group == "" {
		return SyntheticCompactOwnerScopeUnbound
	}
	return fmt.Sprintf("user:%d/token:%d/group:%s", userID, tokenID, group)
}

func NormalizeSyntheticCompactStateKind(kind string) string {
	kind = strings.TrimSpace(kind)
	switch kind {
	case SyntheticCompactStateKindNativeOpaque:
		return SyntheticCompactStateKindNativeOpaque
	default:
		return SyntheticCompactStateKindSyntheticSummary
	}
}

func NormalizeSyntheticCompactScopePolicy(policy string, kind string) string {
	policy = strings.TrimSpace(policy)
	if policy != "" {
		return policy
	}
	if NormalizeSyntheticCompactStateKind(kind) == SyntheticCompactStateKindNativeOpaque {
		return SyntheticCompactScopePolicyStrict
	}
	return SyntheticCompactScopePolicyModelFlexible
}

func SaveSyntheticCompactStateRecord(ctx context.Context, record SyntheticCompactStateRecord) error {
	if DB == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	record.ID = strings.TrimSpace(record.ID)
	if record.ID == "" {
		return errors.New("synthetic compact state id is required")
	}
	if strings.TrimSpace(string(record.SummaryCiphertext)) == "" {
		return errors.New("synthetic compact state summary ciphertext is required")
	}
	now := time.Now().Unix()
	if record.CreatedAt == 0 {
		record.CreatedAt = now
	}
	PrepareSyntheticCompactStateRecord(&record, now)
	record.UpdatedAt = now
	return DB.WithContext(ctx).Save(&record).Error
}

func GetSyntheticCompactStateRecord(ctx context.Context, id string, now int64) (*SyntheticCompactStateRecord, bool, error) {
	if DB == nil {
		return nil, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, false, nil
	}
	if now == 0 {
		now = time.Now().Unix()
	}
	var record SyntheticCompactStateRecord
	err := DB.WithContext(ctx).
		Where("id = ? AND (expires_at = 0 OR expires_at > ?)", id, now).
		First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	shouldRefreshLastAccess := shouldRefreshSyntheticCompactLastAccess(record.LastAccessAt, now)
	shouldBackfillMetadata :=
		strings.TrimSpace(record.Kind) == "" ||
			strings.TrimSpace(record.ModelAtCreation) == "" ||
			strings.TrimSpace(record.OwnerScope) == "" ||
			strings.TrimSpace(record.ScopePolicy) == "" ||
			strings.TrimSpace(record.StateHash) == ""
	originalLastAccessAt := record.LastAccessAt
	PrepareSyntheticCompactStateRecord(&record, now)
	if shouldRefreshLastAccess || shouldBackfillMetadata {
		// Reads may repair legacy metadata, but last_access_at is still rate-limited to avoid write amplification.
		updates := map[string]interface{}{
			"kind":                 record.Kind,
			"model_at_creation":    record.ModelAtCreation,
			"owner_scope":          record.OwnerScope,
			"scope_policy":         record.ScopePolicy,
			"upstream_response_id": record.UpstreamResponseID,
			"source_instance":      record.SourceInstance,
			"state_hash":           record.StateHash,
		}
		if shouldRefreshLastAccess {
			updates["last_access_at"] = now
		}
		if err := DB.WithContext(ctx).
			Model(&SyntheticCompactStateRecord{}).
			Where("id = ?", record.ID).
			Updates(updates).Error; err != nil {
			common.SysError(fmt.Sprintf("failed to refresh synthetic compact state metadata: id=%s error=%v", record.ID, err))
			record.LastAccessAt = originalLastAccessAt
		} else if shouldRefreshLastAccess {
			record.LastAccessAt = now
		}
	}
	return &record, true, nil
}

func shouldRefreshSyntheticCompactLastAccess(lastAccessAt int64, now int64) bool {
	if lastAccessAt == 0 {
		return true
	}
	return now-lastAccessAt >= SyntheticCompactStateLastAccessUpdateIntervalSeconds
}

func PrepareSyntheticCompactStateRecord(record *SyntheticCompactStateRecord, now int64) {
	if record == nil {
		return
	}
	record.ID = strings.TrimSpace(record.ID)
	record.Kind = NormalizeSyntheticCompactStateKind(record.Kind)
	record.Model = strings.TrimSpace(record.Model)
	record.ModelAtCreation = strings.TrimSpace(record.ModelAtCreation)
	if record.ModelAtCreation == "" {
		record.ModelAtCreation = record.Model
	}
	if record.Model == "" {
		record.Model = record.ModelAtCreation
	}
	record.Group = strings.TrimSpace(record.Group)
	record.OwnerScope = strings.TrimSpace(record.OwnerScope)
	if record.OwnerScope == "" {
		record.OwnerScope = SyntheticCompactOwnerScope(record.UserID, record.TokenID, record.Group)
	}
	record.ScopePolicy = NormalizeSyntheticCompactScopePolicy(record.ScopePolicy, record.Kind)
	record.SourceInstance = strings.TrimSpace(record.SourceInstance)
	if record.LastAccessAt == 0 {
		record.LastAccessAt = now
	}
	record.StateHash = syntheticCompactStateRecordHash(*record)
}

func syntheticCompactStateRecordHash(record SyntheticCompactStateRecord) string {
	h := sha256.New()
	for _, part := range []string{
		strings.TrimSpace(record.ID),
		NormalizeSyntheticCompactStateKind(record.Kind),
		strings.TrimSpace(record.ModelAtCreation),
		strings.TrimSpace(record.OwnerScope),
		NormalizeSyntheticCompactScopePolicy(record.ScopePolicy, record.Kind),
		strings.TrimSpace(record.UpstreamResponseID),
		strings.TrimSpace(record.SourceInstance),
		strings.TrimSpace(string(record.SummaryCiphertext)),
		fmt.Sprintf("%d", record.CreatedAt),
		fmt.Sprintf("%d", record.ExpiresAt),
	} {
		h.Write([]byte{0})
		h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func PruneExpiredSyntheticCompactStateRecords(ctx context.Context, now int64) (int64, error) {
	if DB == nil {
		return 0, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if now == 0 {
		now = time.Now().Unix()
	}
	var totalDeleted int64
	for {
		ids := make([]string, 0, SyntheticCompactStatePruneBatchSize)
		var deleted int64
		err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&SyntheticCompactStateRecord{}).
				Where("expires_at > 0 AND expires_at <= ?", now).
				Order("expires_at, id").
				Limit(SyntheticCompactStatePruneBatchSize).
				Pluck("id", &ids).Error; err != nil {
				return err
			}
			if len(ids) == 0 {
				return nil
			}
			result := tx.Where("id IN ?", ids).Delete(&SyntheticCompactStateRecord{})
			deleted = result.RowsAffected
			return result.Error
		})
		if err != nil {
			return totalDeleted, err
		}
		if len(ids) == 0 {
			return totalDeleted, nil
		}
		if deleted == 0 {
			return totalDeleted, errors.New("expired synthetic compact state prune made no progress")
		}
		totalDeleted += deleted
	}
}
