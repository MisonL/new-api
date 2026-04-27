package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

type Log struct {
	Id               int    `json:"id" gorm:"index:idx_created_at_id,priority:1;index:idx_user_id_id,priority:2;index:idx_logs_username_token_type_created_id,priority:5;index:idx_logs_username_model_type_created_id,priority:5;index:idx_logs_username_group_type_created_id,priority:5;index:idx_logs_model_group_type_created_id,priority:5;index:idx_logs_token_model_type_created_id,priority:5;index:idx_logs_channel_model_type_created_id,priority:5;index:idx_logs_token_group_type_created_id,priority:5;index:idx_logs_channel_group_type_created_id,priority:5;index:idx_logs_username_token_group_type_created_id,priority:6"`
	UserId           int    `json:"user_id" gorm:"index;index:idx_user_id_id,priority:1;index:idx_logs_user_created_at,priority:1;index:idx_logs_user_type_created_at,priority:1"`
	CreatedAt        int64  `json:"created_at" gorm:"bigint;index:idx_created_at_id,priority:2;index:idx_created_at_type;index:idx_logs_type_created_at,priority:2;index:idx_logs_user_created_at,priority:2;index:idx_logs_username_created_at,priority:2;index:idx_logs_token_created_at,priority:2;index:idx_logs_model_created_at,priority:2;index:idx_logs_channel_created_at,priority:2;index:idx_logs_group_created_at,priority:2;index:idx_logs_user_type_created_at,priority:3;index:idx_logs_username_type_created_at,priority:3;index:idx_logs_token_type_created_at,priority:3;index:idx_logs_model_type_created_at,priority:3;index:idx_logs_channel_type_created_at,priority:3;index:idx_logs_group_type_created_at,priority:3;index:idx_logs_username_token_type_created_id,priority:4;index:idx_logs_username_model_type_created_id,priority:4;index:idx_logs_username_group_type_created_id,priority:4;index:idx_logs_model_group_type_created_id,priority:4;index:idx_logs_token_model_type_created_id,priority:4;index:idx_logs_channel_model_type_created_id,priority:4;index:idx_logs_token_group_type_created_id,priority:4;index:idx_logs_channel_group_type_created_id,priority:4;index:idx_logs_username_token_group_type_created_id,priority:5"`
	Type             int    `json:"type" gorm:"index:idx_created_at_type;index:idx_logs_type_created_at,priority:1;index:idx_logs_user_type_created_at,priority:2;index:idx_logs_username_type_created_at,priority:2;index:idx_logs_token_type_created_at,priority:2;index:idx_logs_model_type_created_at,priority:2;index:idx_logs_channel_type_created_at,priority:2;index:idx_logs_group_type_created_at,priority:2;index:idx_logs_username_token_type_created_id,priority:3;index:idx_logs_username_model_type_created_id,priority:3;index:idx_logs_username_group_type_created_id,priority:3;index:idx_logs_model_group_type_created_id,priority:3;index:idx_logs_token_model_type_created_id,priority:3;index:idx_logs_channel_model_type_created_id,priority:3;index:idx_logs_token_group_type_created_id,priority:3;index:idx_logs_channel_group_type_created_id,priority:3;index:idx_logs_username_token_group_type_created_id,priority:4"`
	Content          string `json:"content"`
	Username         string `json:"username" gorm:"index;index:index_username_model_name,priority:2;index:idx_logs_username_created_at,priority:1;index:idx_logs_username_type_created_at,priority:1;index:idx_logs_username_token_type_created_id,priority:1;index:idx_logs_username_model_type_created_id,priority:1;index:idx_logs_username_group_type_created_id,priority:1;index:idx_logs_username_token_group_type_created_id,priority:1;default:''"`
	TokenName        string `json:"token_name" gorm:"index;index:idx_logs_token_created_at,priority:1;index:idx_logs_token_type_created_at,priority:1;index:idx_logs_username_token_type_created_id,priority:2;index:idx_logs_token_model_type_created_id,priority:1;index:idx_logs_token_group_type_created_id,priority:1;index:idx_logs_username_token_group_type_created_id,priority:2;default:''"`
	ModelName        string `json:"model_name" gorm:"index;index:index_username_model_name,priority:1;index:idx_logs_model_created_at,priority:1;index:idx_logs_model_type_created_at,priority:1;index:idx_logs_username_model_type_created_id,priority:2;index:idx_logs_model_group_type_created_id,priority:1;index:idx_logs_token_model_type_created_id,priority:2;index:idx_logs_channel_model_type_created_id,priority:2;default:''"`
	Quota            int    `json:"quota" gorm:"default:0"`
	PromptTokens     int    `json:"prompt_tokens" gorm:"default:0"`
	CompletionTokens int    `json:"completion_tokens" gorm:"default:0"`
	UseTime          int    `json:"use_time" gorm:"default:0"`
	IsStream         bool   `json:"is_stream"`
	ChannelId        int    `json:"channel" gorm:"index;index:idx_logs_channel_created_at,priority:1;index:idx_logs_channel_type_created_at,priority:1;index:idx_logs_channel_model_type_created_id,priority:1;index:idx_logs_channel_group_type_created_id,priority:1"`
	ChannelName      string `json:"channel_name" gorm:"->"`
	TokenId          int    `json:"token_id" gorm:"default:0;index"`
	Group            string `json:"group" gorm:"index;index:idx_logs_group_created_at,priority:1;index:idx_logs_group_type_created_at,priority:1;index:idx_logs_username_group_type_created_id,priority:2;index:idx_logs_model_group_type_created_id,priority:2;index:idx_logs_token_group_type_created_id,priority:2;index:idx_logs_channel_group_type_created_id,priority:2;index:idx_logs_username_token_group_type_created_id,priority:3"`
	Ip               string `json:"ip" gorm:"index;default:''"`
	RequestId        string `json:"request_id,omitempty" gorm:"type:varchar(64);index:idx_logs_request_id;default:''"`
	Other            string `json:"other"`
}

// don't use iota, avoid change log type value
const (
	LogTypeUnknown = 0
	LogTypeTopup   = 1
	LogTypeConsume = 2
	LogTypeManage  = 3
	LogTypeSystem  = 4
	LogTypeError   = 5
	LogTypeRefund  = 6
)

func formatUserLogs(logs []*Log, startIdx int) {
	for i := range logs {
		logs[i].ChannelName = ""
		var otherMap map[string]interface{}
		otherMap, _ = common.StrToMap(logs[i].Other)
		if otherMap != nil {
			// Remove admin-only debug fields.
			delete(otherMap, "admin_info")
			// delete(otherMap, "reject_reason")
			delete(otherMap, "stream_status")
		}
		logs[i].Other = common.MapToJsonStr(otherMap)
		logs[i].Id = startIdx + i + 1
	}
}

func GetLogByTokenId(tokenId int) (logs []*Log, err error) {
	err = LOG_DB.Model(&Log{}).Where("token_id = ?", tokenId).Order("id desc").Limit(common.MaxRecentItems).Find(&logs).Error
	formatUserLogs(logs, 0)
	return logs, err
}

func RecordLog(userId int, logType int, content string) {
	if logType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(userId, false)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      logType,
		Content:   content,
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		common.SysLog("failed to record log: " + err.Error())
	}
}

func RecordLogWithAdminInfo(userId int, logType int, content string, adminInfo map[string]interface{}) {
	if logType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(userId, false)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      logType,
		Content:   content,
	}
	if len(adminInfo) > 0 {
		log.Other = common.MapToJsonStr(map[string]interface{}{
			"admin_info": adminInfo,
		})
	}
	if err := LOG_DB.Create(log).Error; err != nil {
		common.SysLog("failed to record log: " + err.Error())
	}
}

func RecordTopupLog(userId int, content string, callerIp string, paymentMethod string, callbackPaymentMethod string) {
	username, _ := GetUsernameById(userId, false)
	adminInfo := map[string]interface{}{
		"server_ip":               common.GetIp(),
		"node_name":               common.NodeName,
		"caller_ip":               callerIp,
		"payment_method":          paymentMethod,
		"callback_payment_method": callbackPaymentMethod,
		"version":                 common.Version,
	}
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeTopup,
		Content:   content,
		Ip:        callerIp,
		Other: common.MapToJsonStr(map[string]interface{}{
			"admin_info": adminInfo,
		}),
	}
	if err := LOG_DB.Create(log).Error; err != nil {
		common.SysLog("failed to record topup log: " + err.Error())
	}
}

func RecordErrorLog(c *gin.Context, userId int, channelId int, modelName string, tokenName string, content string, tokenId int, useTimeSeconds int,
	isStream bool, group string, other map[string]interface{}) {
	logger.LogInfo(c, fmt.Sprintf("record error log: userId=%d, channelId=%d, modelName=%s, tokenName=%s, content=%s", userId, channelId, modelName, tokenName, content))
	username := c.GetString("username")
	requestId := c.GetString(common.RequestIdKey)
	other = common.AppendPayloadAuditFields(c, other)
	otherStr := common.MapToJsonStr(other)
	// 判断是否需要记录 IP
	needRecordIp := false
	if settingMap, err := GetUserSetting(userId, false); err == nil {
		if settingMap.RecordIpLog {
			needRecordIp = true
		}
	}
	log := &Log{
		UserId:           userId,
		Username:         username,
		CreatedAt:        common.GetTimestamp(),
		Type:             LogTypeError,
		Content:          content,
		PromptTokens:     0,
		CompletionTokens: 0,
		TokenName:        tokenName,
		ModelName:        modelName,
		Quota:            0,
		ChannelId:        channelId,
		TokenId:          tokenId,
		UseTime:          useTimeSeconds,
		IsStream:         isStream,
		Group:            group,
		Ip: func() string {
			if needRecordIp {
				return c.ClientIP()
			}
			return ""
		}(),
		RequestId: requestId,
		Other:     otherStr,
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		logger.LogError(c, "failed to record log: "+err.Error())
	}
}

type RecordConsumeLogParams struct {
	ChannelId        int                    `json:"channel_id"`
	PromptTokens     int                    `json:"prompt_tokens"`
	CompletionTokens int                    `json:"completion_tokens"`
	ModelName        string                 `json:"model_name"`
	TokenName        string                 `json:"token_name"`
	Quota            int                    `json:"quota"`
	Content          string                 `json:"content"`
	TokenId          int                    `json:"token_id"`
	UseTimeSeconds   int                    `json:"use_time_seconds"`
	IsStream         bool                   `json:"is_stream"`
	Group            string                 `json:"group"`
	Other            map[string]interface{} `json:"other"`
}

func RecordConsumeLog(c *gin.Context, userId int, params RecordConsumeLogParams) {
	if !common.LogConsumeEnabled {
		return
	}
	if common.DebugEnabled {
		logger.LogDebug(c, "record consume log: userId=%d, params=%s", userId, common.GetJsonString(params))
	}
	username := c.GetString("username")
	requestId := c.GetString(common.RequestIdKey)
	params.Other = common.AppendPayloadAuditFields(c, params.Other)
	otherStr := common.MapToJsonStr(params.Other)
	// 判断是否需要记录 IP
	needRecordIp := false
	if settingMap, err := GetUserSetting(userId, false); err == nil {
		if settingMap.RecordIpLog {
			needRecordIp = true
		}
	}
	log := &Log{
		UserId:           userId,
		Username:         username,
		CreatedAt:        common.GetTimestamp(),
		Type:             LogTypeConsume,
		Content:          params.Content,
		PromptTokens:     params.PromptTokens,
		CompletionTokens: params.CompletionTokens,
		TokenName:        params.TokenName,
		ModelName:        params.ModelName,
		Quota:            params.Quota,
		ChannelId:        params.ChannelId,
		TokenId:          params.TokenId,
		UseTime:          params.UseTimeSeconds,
		IsStream:         params.IsStream,
		Group:            params.Group,
		Ip: func() string {
			if needRecordIp {
				return c.ClientIP()
			}
			return ""
		}(),
		RequestId: requestId,
		Other:     otherStr,
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		logger.LogError(c, "failed to record log: "+err.Error())
	}
	if common.DataExportEnabled {
		gopool.Go(func() {
			LogQuotaData(userId, username, params.ModelName, params.Quota, common.GetTimestamp(), params.PromptTokens+params.CompletionTokens)
		})
	}
}

type RecordTaskBillingLogParams struct {
	UserId    int
	LogType   int
	Content   string
	ChannelId int
	ModelName string
	Quota     int
	TokenId   int
	Group     string
	Other     map[string]interface{}
}

func RecordTaskBillingLog(params RecordTaskBillingLogParams) {
	if params.LogType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(params.UserId, false)
	tokenName := ""
	if params.TokenId > 0 {
		if token, err := GetTokenById(params.TokenId); err == nil {
			tokenName = token.Name
		}
	}
	log := &Log{
		UserId:    params.UserId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      params.LogType,
		Content:   params.Content,
		TokenName: tokenName,
		ModelName: params.ModelName,
		Quota:     params.Quota,
		ChannelId: params.ChannelId,
		TokenId:   params.TokenId,
		Group:     params.Group,
		Other:     common.MapToJsonStr(params.Other),
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		common.SysLog("failed to record task billing log: " + err.Error())
	}
}

type LogFilter struct {
	LogType        int
	StartTimestamp int64
	EndTimestamp   int64
	ModelName      string
	Username       string
	TokenName      string
	Channel        int
	Group          string
	RequestId      string
	UserId         int
}

func applyModelNameFilter(tx *gorm.DB, column string, modelName string) (*gorm.DB, error) {
	modelNamePattern, err := sanitizeLikePattern(modelName)
	if err != nil {
		return nil, err
	}
	if strings.Contains(modelNamePattern, "%") {
		return tx.Where(column+" LIKE ? ESCAPE '!'", modelNamePattern), nil
	}
	return tx.Where(column+" = ?", modelName), nil
}

func applyLogFilters(tx *gorm.DB, filter LogFilter) (*gorm.DB, error) {
	if filter.UserId > 0 {
		tx = tx.Where("logs.user_id = ?", filter.UserId)
	}
	if filter.LogType != LogTypeUnknown {
		tx = tx.Where("logs.type = ?", filter.LogType)
	}
	if filter.ModelName != "" {
		nextTx, err := applyModelNameFilter(tx, "logs.model_name", filter.ModelName)
		if err != nil {
			return nil, err
		}
		tx = nextTx
	}
	if filter.Username != "" {
		tx = tx.Where("logs.username = ?", filter.Username)
	}
	if filter.TokenName != "" {
		tx = tx.Where("logs.token_name = ?", filter.TokenName)
	}
	if filter.RequestId != "" {
		tx = tx.Where("logs.request_id = ?", filter.RequestId)
	}
	if filter.StartTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", filter.StartTimestamp)
	}
	if filter.EndTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", filter.EndTimestamp)
	}
	if filter.Channel != 0 {
		tx = tx.Where("logs.channel_id = ?", filter.Channel)
	}
	if filter.Group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", filter.Group)
	}
	return tx, nil
}

func GetAllLogs(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, startIdx int, num int, channel int, group string, requestId string) (logs []*Log, total int64, err error) {
	tx, err := applyLogFilters(LOG_DB, LogFilter{
		LogType:        logType,
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
		ModelName:      modelName,
		Username:       username,
		TokenName:      tokenName,
		Channel:        channel,
		Group:          group,
		RequestId:      requestId,
	})
	if err != nil {
		return nil, 0, err
	}
	err = tx.Model(&Log{}).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}
	err = tx.Order("logs.created_at desc, logs.id desc").Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		return nil, 0, err
	}

	channelIds := types.NewSet[int]()
	for _, log := range logs {
		if log.ChannelId != 0 {
			channelIds.Add(log.ChannelId)
		}
	}

	if channelIds.Len() > 0 {
		var channels []struct {
			Id   int    `gorm:"column:id"`
			Name string `gorm:"column:name"`
		}
		if common.MemoryCacheEnabled {
			// Cache get channel
			for _, channelId := range channelIds.Items() {
				if cacheChannel, err := CacheGetChannel(channelId); err == nil {
					channels = append(channels, struct {
						Id   int    `gorm:"column:id"`
						Name string `gorm:"column:name"`
					}{
						Id:   channelId,
						Name: cacheChannel.Name,
					})
				}
			}
		} else {
			// Bulk query channels from DB
			if err = DB.Table("channels").Select("id, name").Where("id IN ?", channelIds.Items()).Find(&channels).Error; err != nil {
				return logs, total, err
			}
		}
		channelMap := make(map[int]string, len(channels))
		for _, channel := range channels {
			channelMap[channel.Id] = channel.Name
		}
		for i := range logs {
			logs[i].ChannelName = channelMap[logs[i].ChannelId]
		}
	}

	return logs, total, err
}

const logSearchCountLimit = 10000

func GetUserLogs(userId int, logType int, startTimestamp int64, endTimestamp int64, modelName string, tokenName string, startIdx int, num int, group string, requestId string) (logs []*Log, total int64, err error) {
	tx, err := applyLogFilters(LOG_DB, LogFilter{
		UserId:         userId,
		LogType:        logType,
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
		ModelName:      modelName,
		TokenName:      tokenName,
		Group:          group,
		RequestId:      requestId,
	})
	if err != nil {
		return nil, 0, err
	}
	err = tx.Model(&Log{}).Limit(logSearchCountLimit).Count(&total).Error
	if err != nil {
		common.SysError("failed to count user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}
	err = tx.Order("logs.created_at desc, logs.id desc").Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		common.SysError("failed to search user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}

	formatUserLogs(logs, startIdx)
	return logs, total, err
}

type Stat struct {
	Quota int `json:"quota"`
	Rpm   int `json:"rpm"`
	Tpm   int `json:"tpm"`
}

func SumUsedQuota(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, channel int, group string) (stat Stat, err error) {
	recentCutoff := time.Now().Add(-60 * time.Second).Unix()
	tx := LOG_DB.Table("logs").Select(
		`COALESCE(SUM(quota), 0) quota,
		COALESCE(SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END), 0) rpm,
		COALESCE(SUM(CASE WHEN created_at >= ? THEN COALESCE(prompt_tokens, 0) + COALESCE(completion_tokens, 0) ELSE 0 END), 0) tpm`,
		recentCutoff,
		recentCutoff,
	)

	if username != "" {
		tx = tx.Where("username = ?", username)
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if modelName != "" {
		nextTx, err := applyModelNameFilter(tx, "model_name", modelName)
		if err != nil {
			return stat, err
		}
		tx = nextTx
	}
	if channel != 0 {
		tx = tx.Where("channel_id = ?", channel)
	}
	if group != "" {
		tx = tx.Where(logGroupCol+" = ?", group)
	}

	tx = tx.Where("type = ?", LogTypeConsume)

	if err := tx.Scan(&stat).Error; err != nil {
		common.SysError("failed to query log stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}

	return stat, nil
}

func SumUsedToken(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string) (token int) {
	tx := LOG_DB.Table("logs").Select("ifnull(sum(prompt_tokens),0) + ifnull(sum(completion_tokens),0)")
	if username != "" {
		tx = tx.Where("username = ?", username)
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if modelName != "" {
		tx = tx.Where("model_name = ?", modelName)
	}
	tx.Where("type = ?", LogTypeConsume).Scan(&token)
	return token
}

func DeleteOldLog(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	var total int64 = 0

	for {
		if nil != ctx.Err() {
			return total, ctx.Err()
		}

		result := LOG_DB.Where("created_at < ?", targetTimestamp).Limit(limit).Delete(&Log{})
		if nil != result.Error {
			return total, result.Error
		}

		total += result.RowsAffected

		if result.RowsAffected < int64(limit) {
			break
		}
	}

	return total, nil
}

func DeleteLogByID(logID int) (int64, error) {
	result := LOG_DB.Delete(&Log{}, logID)
	return result.RowsAffected, result.Error
}

func DeleteUserLogByID(userID int, logID int) error {
	result := LOG_DB.Where("id = ? AND user_id = ?", logID, userID).Delete(&Log{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func DeleteLogsByFilter(ctx context.Context, filter LogFilter) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	tx, err := applyLogFilters(LOG_DB, filter)
	if err != nil {
		return 0, err
	}
	result := tx.Delete(&Log{})
	return result.RowsAffected, result.Error
}

func ClearPayloadAuditFieldsByFilter(ctx context.Context, filter LogFilter, clearRequest bool, clearResponse bool, batchSize int) (int64, error) {
	if !clearRequest && !clearResponse {
		return 0, nil
	}
	if batchSize <= 0 {
		batchSize = 200
	}

	var total int64
	lastID := 0
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}

		tx, err := applyLogFilters(LOG_DB.Model(&Log{}), filter)
		if err != nil {
			return total, err
		}

		var logs []Log
		err = tx.Where("id > ?", lastID).Order("id asc").Limit(batchSize).Find(&logs).Error
		if err != nil {
			return total, err
		}
		if len(logs) == 0 {
			break
		}

		for _, log := range logs {
			lastID = log.Id
			otherMap, err := common.StrToMap(log.Other)
			if err != nil || otherMap == nil {
				continue
			}
			changed := false
			if clearRequest {
				changed = deletePayloadKeys(otherMap, "request") || changed
			}
			if clearResponse {
				changed = deletePayloadKeys(otherMap, "response") || changed
			}
			if !changed {
				continue
			}
			if err := LOG_DB.Model(&Log{}).Where("id = ?", log.Id).Update("other", common.MapToJsonStr(otherMap)).Error; err != nil {
				return total, err
			}
			total++
		}
	}

	return total, nil
}

func deletePayloadKeys(otherMap map[string]interface{}, prefix string) bool {
	keys := []string{
		prefix + "_content",
		prefix + "_content_type",
		prefix + "_content_bytes",
		prefix + "_content_truncated",
		prefix + "_content_omitted",
	}
	changed := false
	for _, key := range keys {
		if _, ok := otherMap[key]; ok {
			delete(otherMap, key)
			changed = true
		}
	}
	return changed
}
