package model

import (
	"fmt"

	"gorm.io/gorm"
)

// quotaDataBucketIndex is the unique index that makes one hourly dashboard
// bucket a single row. Before it existed, the flush path did "select then
// insert": two processes flushing the same bucket concurrently both saw no
// row and both inserted, and every later UPDATE matched both rows, doubling
// the dashboard numbers.
const quotaDataBucketIndex = "uk_qdt_bucket"

// migrateQuotaDataUniqueIndex merges pre-existing duplicate buckets and then
// creates the unique index. A deployment that has been running without the
// index may already hold duplicates; creating the index on that data would
// fail and abort startup, so the duplicates are summed into the oldest row
// first.
func migrateQuotaDataUniqueIndex() error {
	dialect := ""
	if DB != nil && DB.Dialector != nil {
		dialect = DB.Dialector.Name()
	}
	return migrateQuotaDataUniqueIndexWithExecutor(gormPrefillGroupMigrationExecutor{db: DB}, dialect)
}

func migrateQuotaDataUniqueIndexWithExecutor(executor prefillGroupMigrationExecutor, dialect string) error {
	if !executor.HasTable(&QuotaData{}) {
		return executor.AutoMigrate(&QuotaData{})
	}
	if executor.HasIndex(&QuotaData{}, quotaDataBucketIndex) {
		return nil
	}

	var duplicates []struct {
		UserID    int
		Username  string
		ModelName string
		CreatedAt int64
	}
	query := DB.Table("quota_data").
		Select("user_id, username, model_name, created_at").
		Group("user_id, username, model_name, created_at").
		Having("COUNT(*) > 1")
	if err := query.Scan(&duplicates).Error; err != nil {
		return fmt.Errorf("failed to scan duplicate quota_data buckets: %w", err)
	}
	for _, bucket := range duplicates {
		if err := mergeQuotaDataBucket(bucket.UserID, bucket.Username, bucket.ModelName, bucket.CreatedAt); err != nil {
			return fmt.Errorf("failed to merge duplicate quota_data bucket (%d,%s,%s,%d): %w",
				bucket.UserID, bucket.Username, bucket.ModelName, bucket.CreatedAt, err)
		}
	}
	if err := executor.AutoMigrate(&QuotaData{}); err != nil {
		return fmt.Errorf("failed to create quota_data unique index: %w", err)
	}
	return nil
}

// mergeQuotaDataBucket sums every duplicate row of one bucket into the row with
// the smallest id, then deletes the rest.
func mergeQuotaDataBucket(userId int, username, modelName string, createdAt int64) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var rows []QuotaData
		if err := tx.Where("user_id = ? AND username = ? AND model_name = ? AND created_at = ?",
			userId, username, modelName, createdAt).
			Order("id").
			Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) < 2 {
			return nil
		}
		count, quota, tokenUsed := 0, 0, 0
		ids := make([]int, 0, len(rows)-1)
		for index, row := range rows {
			count += row.Count
			quota += row.Quota
			tokenUsed += row.TokenUsed
			if index > 0 {
				ids = append(ids, row.Id)
			}
		}
		if err := tx.Model(&QuotaData{}).Where("id = ?", rows[0].Id).Updates(map[string]interface{}{
			"count":      count,
			"quota":      quota,
			"token_used": tokenUsed,
		}).Error; err != nil {
			return err
		}
		return tx.Where("id IN ?", ids).Delete(&QuotaData{}).Error
	})
}
