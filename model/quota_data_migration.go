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
	return migrateQuotaDataUniqueIndexWithExecutor(gormPrefillGroupMigrationExecutor{db: DB})
}

// migrateQuotaDataUniqueIndexWithExecutor 在传入执行器指向的库上完成"先合并
// 重复桶、再建唯一索引"。所有读写都必须走 executor.DB()：写死全局 DB 时，
// 传入替身或另一个库就会改错库——目标库的重复桶没被合并，随后的建索引必然
// 失败，而全局库里无关的行反而被改写。
func migrateQuotaDataUniqueIndexWithExecutor(executor prefillGroupMigrationExecutor) error {
	if !executor.HasTable(&QuotaData{}) {
		return executor.AutoMigrate(&QuotaData{})
	}
	if executor.HasIndex(&QuotaData{}, quotaDataBucketIndex) {
		return executor.AutoMigrate(&QuotaData{})
	}

	db := executor.DB()

	// 合并查询读取全部模型列。旧库可能还没有 token_used（晚于本迁移引入的
	// 模型版本），缺列会让合并直接报错、InitDB 中断，而补列的 AutoMigrate
	// 排在所有合并之后，永远执行不到。先按需补列，再扫描合并。
	for _, column := range []string{"user_id", "username", "model_name", "created_at", "token_used", "count", "quota"} {
		if executor.HasColumn(&QuotaData{}, column) {
			continue
		}
		if err := executor.AddColumn(&QuotaData{}, column); err != nil {
			return fmt.Errorf("failed to add quota_data.%s: %w", column, err)
		}
	}

	var duplicates []struct {
		UserID    int
		Username  string
		ModelName string
		CreatedAt int64
	}
	query := db.Table("quota_data").
		Select("user_id, username, model_name, created_at").
		Group("user_id, username, model_name, created_at").
		Having("COUNT(*) > 1")
	if err := query.Scan(&duplicates).Error; err != nil {
		return fmt.Errorf("failed to scan duplicate quota_data buckets: %w", err)
	}
	for _, bucket := range duplicates {
		if err := mergeQuotaDataBucket(db, bucket.UserID, bucket.Username, bucket.ModelName, bucket.CreatedAt); err != nil {
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
func mergeQuotaDataBucket(db *gorm.DB, userId int, username, modelName string, createdAt int64) error {
	return db.Transaction(func(tx *gorm.DB) error {
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
