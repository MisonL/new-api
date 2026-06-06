package model

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

const SyntheticCompactStatePruneBatchSize = 1000

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
	ID                string                            `gorm:"primaryKey;type:varchar(96)"`
	Model             string                            `gorm:"type:varchar(191);index"`
	SummaryCiphertext SyntheticCompactSummaryCiphertext `gorm:"not null"`
	UserID            int                               `gorm:"index"`
	TokenID           int                               `gorm:"index"`
	Group             string                            `gorm:"column:group_name;type:varchar(191);index"`
	ChannelID         int                               `gorm:"index"`
	ChannelType       int                               `gorm:"index"`
	CreatedAt         int64                             `gorm:"type:bigint;index"`
	UpdatedAt         int64                             `gorm:"type:bigint"`
	ExpiresAt         int64                             `gorm:"type:bigint;index"`
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
	return &record, true, nil
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
