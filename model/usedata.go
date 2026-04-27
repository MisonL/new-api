package model

import (
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// QuotaData 柱状图数据
type QuotaData struct {
	Id        int    `json:"id"`
	UserID    int    `json:"user_id" gorm:"index;index:idx_qdt_user_created_at,priority:1"`
	Username  string `json:"username" gorm:"index:idx_qdt_model_user_name,priority:2;index:idx_qdt_username_created_at,priority:1;size:64;default:''"`
	ModelName string `json:"model_name" gorm:"index:idx_qdt_model_user_name,priority:1;index:idx_qdt_created_model,priority:2;size:64;default:''"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;index:idx_qdt_created_at,priority:2;index:idx_qdt_user_created_at,priority:2;index:idx_qdt_username_created_at,priority:2;index:idx_qdt_created_model,priority:1"`
	TokenUsed int    `json:"token_used" gorm:"default:0"`
	Count     int    `json:"count" gorm:"default:0"`
	Quota     int    `json:"quota" gorm:"default:0"`
}

// ChannelQuotaData represents dashboard chart data aggregated by channel and hour bucket.
type ChannelQuotaData struct {
	ChannelId   int    `json:"channel_id" gorm:"column:channel_id"`
	ChannelName string `json:"channel_name" gorm:"-"`
	CreatedAt   int64  `json:"created_at" gorm:"column:created_at"`
	TokenUsed   int    `json:"token_used" gorm:"column:token_used"`
	Count       int    `json:"count" gorm:"column:count"`
	Quota       int    `json:"quota" gorm:"column:quota"`
}

const (
	DashboardTimeHour = "hour"
	DashboardTimeDay  = "day"
	DashboardTimeWeek = "week"

	dashboardDayGranularityThresholdSeconds  int64 = 7 * 24 * 60 * 60
	dashboardWeekGranularityThresholdSeconds int64 = 30 * 24 * 60 * 60
	dashboardPostgresWorkMem                       = "64MB"
)

// NormalizeDashboardTimeGranularity keeps dashboard aggregation inputs bounded.
func NormalizeDashboardTimeGranularity(defaultTime string) string {
	switch defaultTime {
	case DashboardTimeDay, DashboardTimeWeek:
		return defaultTime
	default:
		return DashboardTimeHour
	}
}

// NormalizeDashboardTimeGranularityForRange prevents large custom ranges from returning too many chart buckets.
func NormalizeDashboardTimeGranularityForRange(defaultTime string, startTime int64, endTime int64) string {
	normalizedTime := NormalizeDashboardTimeGranularity(defaultTime)
	if endTime <= startTime {
		return normalizedTime
	}

	rangeSeconds := endTime - startTime
	if rangeSeconds > dashboardWeekGranularityThresholdSeconds {
		return DashboardTimeWeek
	}
	if rangeSeconds > dashboardDayGranularityThresholdSeconds && normalizedTime == DashboardTimeHour {
		return DashboardTimeDay
	}
	return normalizedTime
}

func dashboardGranularitySeconds(defaultTime string) int64 {
	switch NormalizeDashboardTimeGranularity(defaultTime) {
	case DashboardTimeDay:
		return 86400
	case DashboardTimeWeek:
		return 604800
	default:
		return 3600
	}
}

func getDashboardBucketExpression(db *gorm.DB, column string, defaultTime string) string {
	seconds := dashboardGranularitySeconds(defaultTime)
	switch db.Dialector.Name() {
	case common.DatabaseTypePostgreSQL:
		return fmt.Sprintf("((%s / %d) * %d)", column, seconds, seconds)
	case common.DatabaseTypeMySQL:
		return fmt.Sprintf("CAST(FLOOR(%s / %d) * %d AS SIGNED)", column, seconds, seconds)
	default:
		return fmt.Sprintf("CAST(%s / %d AS INTEGER) * %d", column, seconds, seconds)
	}
}

func runDashboardAggregateQuery(db *gorm.DB, scan func(*gorm.DB) error) error {
	if db.Dialector.Name() != common.DatabaseTypePostgreSQL {
		return scan(db)
	}

	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL work_mem = '" + dashboardPostgresWorkMem + "'").Error; err != nil {
			return err
		}
		return scan(tx)
	})
}

func UpdateQuotaData() {
	for {
		if common.DataExportEnabled {
			common.SysLog("正在更新数据看板数据...")
			SaveQuotaDataCache()
		}
		time.Sleep(time.Duration(common.DataExportInterval) * time.Minute)
	}
}

var CacheQuotaData = make(map[string]*QuotaData)
var CacheQuotaDataLock = sync.Mutex{}

func logQuotaDataCache(userId int, username string, modelName string, quota int, createdAt int64, tokenUsed int) {
	key := fmt.Sprintf("%d-%s-%s-%d", userId, username, modelName, createdAt)
	quotaData, ok := CacheQuotaData[key]
	if ok {
		quotaData.Count += 1
		quotaData.Quota += quota
		quotaData.TokenUsed += tokenUsed
	} else {
		quotaData = &QuotaData{
			UserID:    userId,
			Username:  username,
			ModelName: modelName,
			CreatedAt: createdAt,
			Count:     1,
			Quota:     quota,
			TokenUsed: tokenUsed,
		}
	}
	CacheQuotaData[key] = quotaData
}

func LogQuotaData(userId int, username string, modelName string, quota int, createdAt int64, tokenUsed int) {
	// 只精确到小时
	createdAt = createdAt - (createdAt % 3600)

	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()
	logQuotaDataCache(userId, username, modelName, quota, createdAt, tokenUsed)
}

func SaveQuotaDataCache() {
	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()
	size := len(CacheQuotaData)
	// 如果缓存中有数据，就保存到数据库中
	// 1. 先查询数据库中是否有数据
	// 2. 如果有数据，就更新数据
	// 3. 如果没有数据，就插入数据
	for _, quotaData := range CacheQuotaData {
		quotaDataDB := &QuotaData{}
		DB.Table("quota_data").Where("user_id = ? and username = ? and model_name = ? and created_at = ?",
			quotaData.UserID, quotaData.Username, quotaData.ModelName, quotaData.CreatedAt).First(quotaDataDB)
		if quotaDataDB.Id > 0 {
			//quotaDataDB.Count += quotaData.Count
			//quotaDataDB.Quota += quotaData.Quota
			//DB.Table("quota_data").Save(quotaDataDB)
			increaseQuotaData(quotaData.UserID, quotaData.Username, quotaData.ModelName, quotaData.Count, quotaData.Quota, quotaData.CreatedAt, quotaData.TokenUsed)
		} else {
			DB.Table("quota_data").Create(quotaData)
		}
	}
	CacheQuotaData = make(map[string]*QuotaData)
	common.SysLog(fmt.Sprintf("保存数据看板数据成功，共保存%d条数据", size))
}

func increaseQuotaData(userId int, username string, modelName string, count int, quota int, createdAt int64, tokenUsed int) {
	err := DB.Table("quota_data").Where("user_id = ? and username = ? and model_name = ? and created_at = ?",
		userId, username, modelName, createdAt).Updates(map[string]interface{}{
		"count":      gorm.Expr("count + ?", count),
		"quota":      gorm.Expr("quota + ?", quota),
		"token_used": gorm.Expr("token_used + ?", tokenUsed),
	}).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("increaseQuotaData error: %s", err))
	}
}

func getQuotaDataGroupedByModel(baseQuery *gorm.DB, defaultTime string) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	bucketExpression := getDashboardBucketExpression(DB, "created_at", defaultTime)
	err = runDashboardAggregateQuery(baseQuery, func(tx *gorm.DB) error {
		return tx.Select(
			fmt.Sprintf(
				"model_name, COALESCE(sum(count), 0) as count, COALESCE(sum(quota), 0) as quota, COALESCE(sum(token_used), 0) as token_used, %s as created_at",
				bucketExpression,
			),
		).
			Group(fmt.Sprintf("model_name, %s", bucketExpression)).
			Find(&quotaDatas).Error
	})
	return quotaDatas, err
}

func GetQuotaDataByUsername(username string, startTime int64, endTime int64, defaultTime string) (quotaData []*QuotaData, err error) {
	baseQuery := DB.Table("quota_data").
		Where("username = ? and created_at >= ? and created_at <= ?", username, startTime, endTime)
	return getQuotaDataGroupedByModel(baseQuery, defaultTime)
}

func GetQuotaDataByUserId(userId int, startTime int64, endTime int64, defaultTime string) (quotaData []*QuotaData, err error) {
	baseQuery := DB.Table("quota_data").
		Where("user_id = ? and created_at >= ? and created_at <= ?", userId, startTime, endTime)
	return getQuotaDataGroupedByModel(baseQuery, defaultTime)
}

func GetQuotaDataGroupByUser(startTime int64, endTime int64, defaultTime string) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	bucketExpression := getDashboardBucketExpression(DB, "created_at", defaultTime)
	err = runDashboardAggregateQuery(DB.Table("quota_data"), func(tx *gorm.DB) error {
		return tx.Select(
			fmt.Sprintf(
				"username, %s as created_at, COALESCE(sum(count), 0) as count, COALESCE(sum(quota), 0) as quota, COALESCE(sum(token_used), 0) as token_used",
				bucketExpression,
			),
		).
			Where("created_at >= ? and created_at <= ?", startTime, endTime).
			Group(fmt.Sprintf("username, %s", bucketExpression)).
			Find(&quotaDatas).Error
	})
	return quotaDatas, err
}

func GetAllQuotaDates(startTime int64, endTime int64, username string, defaultTime string) (quotaData []*QuotaData, err error) {
	if username != "" {
		return GetQuotaDataByUsername(username, startTime, endTime, defaultTime)
	}
	baseQuery := DB.Table("quota_data").
		Where("created_at >= ? and created_at <= ?", startTime, endTime)
	return getQuotaDataGroupedByModel(baseQuery, defaultTime)
}

func attachChannelNames(channelData []*ChannelQuotaData) error {
	if len(channelData) == 0 {
		return nil
	}

	channelIds := make(map[int]struct{})
	for _, item := range channelData {
		if item.ChannelId != 0 {
			channelIds[item.ChannelId] = struct{}{}
		}
	}

	if len(channelIds) == 0 {
		return nil
	}

	ids := make([]int, 0, len(channelIds))
	for id := range channelIds {
		ids = append(ids, id)
	}

	channelNames := make(map[int]string, len(ids))
	for _, id := range ids {
		if common.MemoryCacheEnabled {
			if channel, err := CacheGetChannel(id); err == nil && channel != nil {
				channelNames[id] = channel.Name
			}
		}
	}

	if len(channelNames) < len(ids) {
		missingIDs := make([]int, 0, len(ids)-len(channelNames))
		for _, id := range ids {
			if _, ok := channelNames[id]; !ok {
				missingIDs = append(missingIDs, id)
			}
		}
		if len(missingIDs) == 0 {
			goto assignChannelNames
		}
		type channelRow struct {
			Id   int    `gorm:"column:id"`
			Name string `gorm:"column:name"`
		}
		rows := make([]channelRow, 0, len(missingIDs))
		if err := DB.Table("channels").Select("id, name").Where("id IN ?", missingIDs).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			channelNames[row.Id] = row.Name
		}
	}

assignChannelNames:
	for _, item := range channelData {
		if name, ok := channelNames[item.ChannelId]; ok && name != "" {
			item.ChannelName = name
			continue
		}
		if item.ChannelId == 0 {
			continue
		}
		item.ChannelName = fmt.Sprintf("Channel #%d", item.ChannelId)
	}
	return nil
}

// getChannelQuotaData aggregates usage logs by channel and hour bucket for dashboard charts.
func getChannelQuotaData(baseQuery *gorm.DB, defaultTime string) (channelData []*ChannelQuotaData, err error) {
	var channelDatas []*ChannelQuotaData
	bucketExpression := getDashboardBucketExpression(LOG_DB, "created_at", defaultTime)
	err = runDashboardAggregateQuery(baseQuery, func(tx *gorm.DB) error {
		return tx.Select(
			fmt.Sprintf(
				"channel_id, COALESCE(sum(quota), 0) as quota, COALESCE(sum(prompt_tokens), 0) + COALESCE(sum(completion_tokens), 0) as token_used, count(*) as count, %s as created_at",
				bucketExpression,
			),
		).
			Group(fmt.Sprintf("channel_id, %s", bucketExpression)).
			Find(&channelDatas).Error
	})
	if err != nil {
		return nil, err
	}
	if err = attachChannelNames(channelDatas); err != nil {
		return nil, err
	}
	return channelDatas, nil
}

// GetChannelQuotaDataByUsername returns dashboard channel aggregates for the selected admin filter.
func GetChannelQuotaDataByUsername(username string, startTime int64, endTime int64, defaultTime string) (channelData []*ChannelQuotaData, err error) {
	baseQuery := LOG_DB.Table("logs").
		Where("type = ?", LogTypeConsume).
		Where("username = ? AND created_at >= ? AND created_at <= ?", username, startTime, endTime)
	return getChannelQuotaData(baseQuery, defaultTime)
}

// GetChannelQuotaDataByUserId returns dashboard channel aggregates for a single user.
func GetChannelQuotaDataByUserId(userId int, startTime int64, endTime int64, defaultTime string) (channelData []*ChannelQuotaData, err error) {
	baseQuery := LOG_DB.Table("logs").
		Where("type = ?", LogTypeConsume).
		Where("user_id = ? AND created_at >= ? AND created_at <= ?", userId, startTime, endTime)
	return getChannelQuotaData(baseQuery, defaultTime)
}

// GetAllChannelQuotaData returns dashboard channel aggregates across all users or one username.
func GetAllChannelQuotaData(startTime int64, endTime int64, username string, defaultTime string) (channelData []*ChannelQuotaData, err error) {
	if username != "" {
		return GetChannelQuotaDataByUsername(username, startTime, endTime, defaultTime)
	}
	baseQuery := LOG_DB.Table("logs").
		Where("type = ?", LogTypeConsume).
		Where("created_at >= ? AND created_at <= ?", startTime, endTime)
	return getChannelQuotaData(baseQuery, defaultTime)
}
