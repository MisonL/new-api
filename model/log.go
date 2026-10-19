package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

type Log struct {
	Id                int               `json:"id" gorm:"index:idx_logs_created_at_id,priority:2;index:idx_logs_upstream_request_created_at_id,priority:3"`
	UserId            int               `json:"user_id" gorm:"index:idx_logs_user_created_at,priority:1;index:idx_logs_user_type_created_at,priority:1"`
	CreatedAt         int64             `json:"created_at" gorm:"bigint;index:idx_logs_created_at_id,priority:1;index:idx_created_at_type;index:idx_logs_type_created_at,priority:2;index:idx_logs_user_created_at,priority:2;index:idx_logs_username_created_at,priority:2;index:idx_logs_token_created_at,priority:2;index:idx_logs_model_created_at,priority:2;index:idx_logs_channel_created_at,priority:2;index:idx_logs_group_created_at,priority:2;index:idx_logs_request_id_created_at,priority:2;index:idx_logs_upstream_request_created_at_id,priority:2;index:idx_logs_user_type_created_at,priority:3;index:idx_logs_username_type_created_at,priority:3;index:idx_logs_token_type_created_at,priority:3;index:idx_logs_model_type_created_at,priority:3;index:idx_logs_channel_type_created_at,priority:3;index:idx_logs_group_type_created_at,priority:3"`
	Type              int               `json:"type" gorm:"index:idx_created_at_type;index:idx_logs_type_created_at,priority:1"`
	Content           string            `json:"content"`
	Username          string            `json:"username" gorm:"index:index_username_model_name,priority:2;index:idx_logs_username_created_at,priority:1;index:idx_logs_username_type_created_at,priority:1;default:''"`
	TokenName         string            `json:"token_name" gorm:"index:idx_logs_token_created_at,priority:1;index:idx_logs_token_type_created_at,priority:1;default:''"`
	ModelName         string            `json:"model_name" gorm:"index:index_username_model_name,priority:1;index:idx_logs_model_created_at,priority:1;index:idx_logs_model_type_created_at,priority:1;default:''"`
	Quota             int               `json:"quota" gorm:"default:0"`
	PromptTokens      int               `json:"prompt_tokens" gorm:"default:0"`
	CompletionTokens  int               `json:"completion_tokens" gorm:"default:0"`
	UseTime           int               `json:"use_time" gorm:"default:0"`
	IsStream          bool              `json:"is_stream"`
	ChannelId         int               `json:"channel" gorm:"index:idx_logs_channel_created_at,priority:1;index:idx_logs_channel_type_created_at,priority:1"`
	ChannelName       string            `json:"channel_name" gorm:"->"`
	ChannelDetail     *LogChannelDetail `json:"channel_detail,omitempty" gorm:"-"`
	TokenId           int               `json:"token_id" gorm:"default:0;index:idx_logs_token_id"`
	Group             string            `json:"group" gorm:"index:idx_logs_group_created_at,priority:1;index:idx_logs_group_type_created_at,priority:1"`
	Ip                string            `json:"ip" gorm:"default:''"`
	RequestId         string            `json:"request_id,omitempty" gorm:"type:varchar(64);index:idx_logs_request_id_created_at,priority:1;default:''"`
	UpstreamRequestId string            `json:"upstream_request_id,omitempty" gorm:"type:varchar(128);index:idx_logs_upstream_request_created_at_id,priority:1;default:''"`
	Other             string            `json:"other"`
}

type LogChannelDetail struct {
	Id           int    `json:"id"`
	Name         string `json:"name,omitempty"`
	Type         int    `json:"type,omitempty"`
	TypeName     string `json:"type_name,omitempty"`
	Status       int    `json:"status,omitempty"`
	BaseURL      string `json:"base_url,omitempty"`
	Group        string `json:"group,omitempty"`
	Tag          string `json:"tag,omitempty"`
	TestModel    string `json:"test_model,omitempty"`
	ResponseTime int    `json:"response_time,omitempty"`
	Priority     *int64 `json:"priority,omitempty"`
	Weight       *uint  `json:"weight,omitempty"`
	AutoBan      *int   `json:"auto_ban,omitempty"`
	ModelsCount  int    `json:"models_count,omitempty"`
	IsMultiKey   bool   `json:"is_multi_key,omitempty"`
	MultiKeySize int    `json:"multi_key_size,omitempty"`
}

func dashboardLogColumns() []string {
	return []string{
		"id",
		"user_id",
		"created_at",
		"type",
		"username",
		"token_name",
		"model_name",
		"quota",
		"prompt_tokens",
		"completion_tokens",
		"use_time",
		"is_stream",
		"channel_id",
		"token_id",
		logGroupCol,
		"request_id",
		"upstream_request_id",
	}
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

func countChannelModels(models string) int {
	count := 0
	for _, modelName := range strings.Split(models, ",") {
		if strings.TrimSpace(modelName) != "" {
			count++
		}
	}
	return count
}

func buildLogChannelDetail(channel *Channel) *LogChannelDetail {
	if channel == nil {
		return nil
	}

	detail := &LogChannelDetail{
		Id:           channel.Id,
		Name:         channel.Name,
		Type:         channel.Type,
		TypeName:     constant.GetChannelTypeName(channel.Type),
		Status:       channel.Status,
		Group:        channel.Group,
		ResponseTime: channel.ResponseTime,
		Priority:     channel.Priority,
		Weight:       channel.Weight,
		AutoBan:      channel.AutoBan,
		ModelsCount:  countChannelModels(channel.Models),
		IsMultiKey:   channel.ChannelInfo.IsMultiKey,
		MultiKeySize: channel.ChannelInfo.MultiKeySize,
	}
	if channel.TestModel != nil {
		detail.TestModel = *channel.TestModel
	}
	if channel.BaseURL != nil {
		detail.BaseURL = *channel.BaseURL
	}
	if channel.Tag != nil {
		detail.Tag = *channel.Tag
	}
	return detail
}

func formatUserLogs(logs []*Log, startIdx int) {
	for i := range logs {
		logs[i].ChannelId = 0
		logs[i].ChannelName = ""
		logs[i].ChannelDetail = nil
		logs[i].Content = redactUserLogContent(logs[i].Content)
		var otherMap map[string]interface{}
		otherMap, _ = common.StrToMap(logs[i].Other)
		if otherMap != nil {
			if summary := buildUserResponsesRoutingSummary(otherMap); summary != nil {
				otherMap["responses_routing_summary"] = summary
			}
			// Remove admin-only debug fields.
			delete(otherMap, "admin_info")
			delete(otherMap, "stream_status")
			delete(otherMap, "error_category")
			delete(otherMap, "upstream_error_type")
			delete(otherMap, "upstream_error_code")
			delete(otherMap, "error_param")
			delete(otherMap, "error_event")
			delete(otherMap, "upstream_request_path")
			delete(otherMap, "channel_capability_snapshot")
			// 渠道身份、参数覆盖审计原文、请求头策略审计和渠道拦截原因都只在
			// 管理员视图中展示；前端靠 isAdmin 隐藏，但原始 other 仍会下发。
			delete(otherMap, "channel_id")
			delete(otherMap, "channel_name")
			delete(otherMap, "channel_type")
			delete(otherMap, "po")
			delete(otherMap, "request_header_policy")
			delete(otherMap, "header_policy_mode")
			delete(otherMap, "request_header_policy_mode")
			delete(otherMap, "reject_reason")
			delete(otherMap, "request_conversion")
			for key := range otherMap {
				if strings.HasPrefix(key, "responses_") && key != "responses_compact_mode" && key != "responses_routing_summary" {
					delete(otherMap, key)
				}
			}
		}
		logs[i].Other = common.MapToJsonStr(otherMap)
		logs[i].Id = startIdx + i + 1
	}
}

var userVisibleResponsesRoutingReasons = map[string]struct{}{
	"unsupported_compaction":                 {},
	"unsupported_encrypted_reasoning_replay": {},
	"unsupported_codex_dynamic_headers":      {},
	"unsupported_function_tools":             {},
	"unsupported_custom_tools":               {},
	"unsupported_namespace_tools":            {},
	"unsupported_tool_search":                {},
	"responses_to_chat_incompatible":         {},
}

var userVisibleResponsesRoutingOutcomes = map[string]struct{}{
	"success":               {},
	"error":                 {},
	"rate_limited":          {},
	"authentication_failed": {},
	"channel_unavailable":   {},
}

func buildUserResponsesRoutingSummary(other map[string]interface{}) *dto.ResponsesRoutingUserSummary {
	if other == nil {
		return nil
	}
	trace, _ := other["responses_routing_trace"].(map[string]interface{})
	summary := &dto.ResponsesRoutingUserSummary{}
	if trace != nil {
		summary.AttemptCount = userResponsesRoutingListLength(trace["attempted_channels"])
		summary.PreflightSkipCount = userResponsesRoutingListLength(trace["preflight_skipped"])
		if outcome, ok := trace["terminal_reason"].(string); ok {
			outcome = strings.TrimSpace(outcome)
			if _, allowed := userVisibleResponsesRoutingOutcomes[outcome]; allowed {
				summary.Outcome = outcome
			}
		}
	}
	if summary.PreflightSkipCount == 0 {
		summary.PreflightSkipCount = userResponsesRoutingListLength(other["responses_routing_channel_skips"])
	}
	summary.Reasons = userResponsesRoutingReasons(trace, other["responses_routing_channel_skips"])
	summary.AutomaticReroute = summary.PreflightSkipCount > 0 || summary.AttemptCount > 1
	if summary.Outcome == "" && !summary.AutomaticReroute && len(summary.Reasons) == 0 {
		return nil
	}
	return summary
}

func userResponsesRoutingListLength(value interface{}) int {
	switch values := value.(type) {
	case []interface{}:
		return len(values)
	case []map[string]interface{}:
		return len(values)
	default:
		return 0
	}
}

func userResponsesRoutingReasons(trace map[string]interface{}, rawSkips interface{}) []string {
	seen := make(map[string]struct{})
	reasons := make([]string, 0)
	appendReason := func(raw interface{}) {
		reason, ok := raw.(string)
		if !ok {
			return
		}
		reason = strings.TrimSpace(reason)
		if _, allowed := userVisibleResponsesRoutingReasons[reason]; !allowed {
			return
		}
		if _, exists := seen[reason]; exists {
			return
		}
		seen[reason] = struct{}{}
		reasons = append(reasons, reason)
	}
	appendReasonsFromList := func(raw interface{}) {
		switch values := raw.(type) {
		case []interface{}:
			for _, value := range values {
				if item, ok := value.(map[string]interface{}); ok {
					appendReason(item["reason"])
				}
			}
		case []map[string]interface{}:
			for _, item := range values {
				appendReason(item["reason"])
			}
		}
	}
	if trace != nil {
		appendReasonsFromList(trace["preflight_skipped"])
	}
	appendReasonsFromList(rawSkips)
	return reasons
}

func redactUserLogContent(content string) string {
	const responsesCompactLogPrefix = "Responses Compact "
	index := strings.Index(content, responsesCompactLogPrefix)
	if index <= 0 {
		return content
	}
	return strings.TrimSuffix(strings.TrimSpace(content[:index]), ",")
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

func upstreamRequestIdFromLogContext(c *gin.Context, other map[string]interface{}) string {
	if id := c.GetString(common.UpstreamRequestIdKey); id != "" {
		return id
	}
	if other == nil {
		return ""
	}
	if id, ok := other["upstream_request_id"].(string); ok {
		return id
	}
	return ""
}

func shouldRecordLogIP(c *gin.Context, userId int) bool {
	if c != nil {
		userSetting, ok := common.GetContextKeyType[dto.UserSetting](c, constant.ContextKeyUserSetting)
		if ok {
			return userSetting.RecordIpLog
		}
	}
	settingMap, err := GetUserSetting(userId, false)
	if err != nil {
		// Privacy-safe fallback: when the setting cannot be loaded, do not store the client IP.
		common.SysError(fmt.Sprintf("failed to get user setting for IP log decision: user_id=%d err=%v", userId, err))
		return false
	}
	return settingMap.RecordIpLog
}

func RecordErrorLog(c *gin.Context, userId int, channelId int, modelName string, tokenName string, content string, tokenId int, useTimeSeconds int,
	isStream bool, group string, other map[string]interface{}) {
	logger.LogInfo(c, fmt.Sprintf("record error log: userId=%d, channelId=%d, modelName=%s, tokenName=%s, content=%s", userId, channelId, modelName, tokenName, common.LocalLogPreview(content)))
	username := c.GetString("username")
	requestId := c.GetString(common.RequestIdKey)
	other = common.AppendPayloadAuditFields(c, other)
	upstreamRequestId := upstreamRequestIdFromLogContext(c, other)
	otherStr := common.MapToJsonStr(other)
	needRecordIp := shouldRecordLogIP(c, userId)
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
		RequestId:         requestId,
		UpstreamRequestId: upstreamRequestId,
		Other:             otherStr,
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
	upstreamRequestId := upstreamRequestIdFromLogContext(c, params.Other)
	otherStr := common.MapToJsonStr(params.Other)
	needRecordIp := shouldRecordLogIP(c, userId)
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
		RequestId:         requestId,
		UpstreamRequestId: upstreamRequestId,
		Other:             otherStr,
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
	LogType           int
	LogTypeSet        bool
	StartTimestamp    int64
	EndTimestamp      int64
	ModelName         string
	ModelNameEmpty    bool
	Username          string
	UsernameLike      bool
	TokenName         string
	Channel           int
	Group             string
	RequestId         string
	UpstreamRequestId string
	UserId            int
}

func applyExplicitLogTextFilter(tx *gorm.DB, column string, value string) (*gorm.DB, error) {
	if value == "" {
		return tx, nil
	}
	if strings.Contains(value, "%") {
		pattern, err := sanitizeLikePattern(value)
		if err != nil {
			return nil, err
		}
		return tx.Where(column+" LIKE ? ESCAPE '!'", pattern), nil
	}
	return tx.Where(column+" = ?", value), nil
}

func applyModelNameFilter(tx *gorm.DB, column string, modelName string) (*gorm.DB, error) {
	return applyExplicitLogTextFilter(tx, column, modelName)
}

func applyUsernameFilter(tx *gorm.DB, column string, username string, allowWildcard bool) (*gorm.DB, error) {
	if username == "" {
		return tx, nil
	}
	if allowWildcard {
		return applyExplicitLogTextFilter(tx, column, username)
	}
	return tx.Where(column+" = ?", username), nil
}

func applyLogFilters(tx *gorm.DB, filter LogFilter) (*gorm.DB, error) {
	if filter.UserId > 0 {
		tx = tx.Where("logs.user_id = ?", filter.UserId)
	}
	if filter.LogType != LogTypeUnknown || filter.LogTypeSet {
		tx = tx.Where("logs.type = ?", filter.LogType)
	}
	if filter.ModelNameEmpty {
		tx = tx.Where("logs.model_name = ?", "")
	} else if filter.ModelName != "" {
		nextTx, err := applyModelNameFilter(tx, "logs.model_name", filter.ModelName)
		if err != nil {
			return nil, err
		}
		tx = nextTx
	}
	if filter.Username != "" {
		nextTx, err := applyUsernameFilter(tx, "logs.username", filter.Username, filter.UsernameLike)
		if err != nil {
			return nil, err
		}
		tx = nextTx
	}
	if filter.TokenName != "" {
		tx = tx.Where("logs.token_name = ?", filter.TokenName)
	}
	if filter.RequestId != "" {
		tx = tx.Where("logs.request_id = ?", filter.RequestId)
	}
	if filter.UpstreamRequestId != "" {
		tx = tx.Where("logs.upstream_request_id = ?", filter.UpstreamRequestId)
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

const logSearchCountLimit = 10000

func countLogsBounded(tx *gorm.DB) (int64, error) {
	var total int64
	limited := tx.Session(&gorm.Session{}).
		Model(&Log{}).
		Select("logs.id").
		Limit(logSearchCountLimit + 1)
	err := LOG_DB.Session(&gorm.Session{NewDB: true}).
		Table("(?) AS bounded_logs", limited).
		Count(&total).Error
	if total > logSearchCountLimit {
		total = logSearchCountLimit
	}
	return total, err
}

func normalizeLogPagination(startIdx int, num int) (int, int) {
	if startIdx < 0 {
		startIdx = 0
	}
	if num <= 0 {
		num = common.ItemsPerPage
	}
	return startIdx, num
}

func findLogsFastPage(tx *gorm.DB, startIdx int, num int) (logs []*Log, total int64, err error) {
	startIdx, num = normalizeLogPagination(startIdx, num)
	fetchLimit := num + 1
	err = tx.Order("logs.created_at desc, logs.id desc").Limit(fetchLimit).Offset(startIdx).Find(&logs).Error
	if err != nil {
		return nil, 0, err
	}
	hasNext := len(logs) > num
	if hasNext {
		logs = logs[:num]
		return logs, int64(startIdx + num + 1), nil
	}
	return logs, int64(startIdx + len(logs)), nil
}

func GetAllLogs(logType int, startTimestamp int64, endTimestamp int64, modelName string, modelNameEmpty bool, username string, tokenName string, startIdx int, num int, channel int, group string, requestId string, upstreamRequestId string, fastPage bool, compact bool) (logs []*Log, total int64, err error) {
	startIdx, num = normalizeLogPagination(startIdx, num)
	tx, err := applyLogFilters(LOG_DB, LogFilter{
		LogType:           logType,
		StartTimestamp:    startTimestamp,
		EndTimestamp:      endTimestamp,
		ModelName:         modelName,
		ModelNameEmpty:    modelNameEmpty,
		Username:          username,
		UsernameLike:      true,
		TokenName:         tokenName,
		Channel:           channel,
		Group:             group,
		RequestId:         requestId,
		UpstreamRequestId: upstreamRequestId,
	})
	if err != nil {
		return nil, 0, err
	}
	if compact {
		tx = tx.Select(dashboardLogColumns())
	}
	if fastPage {
		logs, total, err = findLogsFastPage(tx, startIdx, num)
		if err != nil {
			return nil, 0, err
		}
	} else {
		total, err = countLogsBounded(tx)
		if err != nil {
			return nil, 0, err
		}
		err = tx.Order("logs.created_at desc, logs.id desc").Limit(num).Offset(startIdx).Find(&logs).Error
		if err != nil {
			return nil, 0, err
		}
	}

	channelIds := types.NewSet[int]()
	for _, log := range logs {
		if log.ChannelId != 0 {
			channelIds.Add(log.ChannelId)
		}
	}

	if channelIds.Len() > 0 && !compact {
		var channels []Channel
		missingChannelIds := types.NewSet[int]()
		if common.MemoryCacheEnabled {
			// Cache get channel
			for _, channelId := range channelIds.Items() {
				if cacheChannel, err := CacheGetChannel(channelId); err == nil {
					channels = append(channels, *cacheChannel)
				} else {
					missingChannelIds.Add(channelId)
				}
			}
		} else {
			for _, channelId := range channelIds.Items() {
				missingChannelIds.Add(channelId)
			}
		}
		if missingChannelIds.Len() > 0 {
			var dbChannels []Channel
			if err = DB.Model(&Channel{}).
				Select([]string{"id", "name", "type", "status", "base_url", commonGroupCol, "tag", "models", "test_model", "response_time", "priority", "weight", "auto_ban", "channel_info"}).
				Where("id IN ?", missingChannelIds.Items()).
				Find(&dbChannels).Error; err != nil {
				return logs, total, err
			}
			channels = append(channels, dbChannels...)
		}
		channelMap := make(map[int]string, len(channels))
		channelDetailMap := make(map[int]*LogChannelDetail, len(channels))
		for i := range channels {
			channelMap[channels[i].Id] = channels[i].Name
			channelDetailMap[channels[i].Id] = buildLogChannelDetail(&channels[i])
		}
		for i := range logs {
			logs[i].ChannelName = channelMap[logs[i].ChannelId]
			logs[i].ChannelDetail = channelDetailMap[logs[i].ChannelId]
		}
	}

	return logs, total, err
}

func GetUserLogs(userId int, logType int, startTimestamp int64, endTimestamp int64, modelName string, modelNameEmpty bool, tokenName string, startIdx int, num int, group string, requestId string, upstreamRequestId string, fastPage bool, compact bool) (logs []*Log, total int64, err error) {
	startIdx, num = normalizeLogPagination(startIdx, num)
	tx, err := applyLogFilters(LOG_DB, LogFilter{
		UserId:            userId,
		LogType:           logType,
		StartTimestamp:    startTimestamp,
		EndTimestamp:      endTimestamp,
		ModelName:         modelName,
		ModelNameEmpty:    modelNameEmpty,
		TokenName:         tokenName,
		Group:             group,
		RequestId:         requestId,
		UpstreamRequestId: upstreamRequestId,
	})
	if err != nil {
		return nil, 0, err
	}
	if compact {
		tx = tx.Select(dashboardLogColumns())
	}
	if fastPage {
		logs, total, err = findLogsFastPage(tx, startIdx, num)
		if err != nil {
			common.SysError("failed to fast search user logs: " + err.Error())
			return nil, 0, errors.New("查询日志失败")
		}
	} else {
		total, err = countLogsBounded(tx)
		if err != nil {
			common.SysError("failed to count user logs: " + err.Error())
			return nil, 0, errors.New("查询日志失败")
		}
		err = tx.Order("logs.created_at desc, logs.id desc").Limit(num).Offset(startIdx).Find(&logs).Error
		if err != nil {
			common.SysError("failed to search user logs: " + err.Error())
			return nil, 0, errors.New("查询日志失败")
		}
	}

	formatUserLogs(logs, startIdx)
	return logs, total, err
}

type Stat struct {
	Quota            int      `json:"quota"`
	Rpm              int      `json:"rpm"`
	Tpm              int      `json:"tpm"`
	InputTokens      int      `json:"input_tokens"`
	CacheReadTokens  int      `json:"cache_read_tokens"`
	CacheWriteTokens int      `json:"cache_write_tokens"`
	OutputTokens     int      `json:"output_tokens"`
	CacheRate        *float64 `json:"cache_rate"`
}

type logTokenStats struct {
	InputTokens          uint64
	CacheReadTokens      uint64
	CacheWriteTokens     uint64
	OutputTokens         uint64
	CacheRateNumerator   uint64
	CacheRateDenominator uint64
}

const logTokenStatsBatchSize = 512

type logTokenStatsTotals struct {
	InputTokens  int64 `gorm:"column:input_tokens"`
	OutputTokens int64 `gorm:"column:output_tokens"`
}

type logTokenStatsRow struct {
	Id           int            `gorm:"column:id"`
	PromptTokens sql.NullInt64  `gorm:"column:prompt_tokens"`
	Other        sql.NullString `gorm:"column:other"`
}

func normalizeLogTokenTotal(value int64) uint64 {
	if value <= 0 {
		return 0
	}
	return uint64(value)
}

func saturatingAddLogTokenCount(left uint64, right uint64) uint64 {
	if ^uint64(0)-left < right {
		return ^uint64(0)
	}
	return left + right
}

func logTokenCountToInt(value uint64) int {
	maxInt := uint64(^uint(0) >> 1)
	if value > maxInt {
		return int(maxInt)
	}
	return int(value)
}

func normalizeLogTokenFloat(number float64) uint64 {
	if number <= 0 || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0
	}
	// float64(^uint64(0)) rounds to 2^64, so this comparison also avoids
	// converting an out-of-range float to uint64.
	if number >= float64(^uint64(0)) {
		return ^uint64(0)
	}
	return uint64(number)
}

func logOtherNumber(value interface{}) (float64, bool) {
	switch value := value.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int8:
		return float64(value), true
	case int16:
		return float64(value), true
	case int32:
		return float64(value), true
	case int64:
		return float64(value), true
	case uint:
		return float64(value), true
	case uint8:
		return float64(value), true
	case uint16:
		return float64(value), true
	case uint32:
		return float64(value), true
	case uint64:
		return float64(value), true
	case common.JsonNumber:
		parsed, err := strconv.ParseFloat(value.String(), 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func logOtherTokenCount(value interface{}) uint64 {
	switch value := value.(type) {
	case int:
		if value > 0 {
			return uint64(value)
		}
		return 0
	case int8:
		if value > 0 {
			return uint64(value)
		}
		return 0
	case int16:
		if value > 0 {
			return uint64(value)
		}
		return 0
	case int32:
		if value > 0 {
			return uint64(value)
		}
		return 0
	case int64:
		if value > 0 {
			return uint64(value)
		}
		return 0
	case uint:
		return uint64(value)
	case uint8:
		return uint64(value)
	case uint16:
		return uint64(value)
	case uint32:
		return uint64(value)
	case uint64:
		return value
	}
	number, ok := logOtherNumber(value)
	if !ok {
		return 0
	}
	return normalizeLogTokenFloat(number)
}

type logCacheTelemetry struct {
	CacheRead       interface{} `json:"cache_tokens"`
	CacheWrite      interface{} `json:"cache_write_tokens"`
	CacheCreation   interface{} `json:"cache_creation_tokens"`
	CacheCreation5m interface{} `json:"cache_creation_tokens_5m"`
	CacheCreation1h interface{} `json:"cache_creation_tokens_1h"`
	Semantic        interface{} `json:"usage_semantic"`
	Claude          interface{} `json:"claude"`
}

func logOtherCacheWriteTokens(other logCacheTelemetry) uint64 {
	if normalized := logOtherTokenCount(other.CacheWrite); normalized > 0 {
		return normalized
	}
	cacheCreation5m := logOtherTokenCount(other.CacheCreation5m)
	cacheCreation1h := logOtherTokenCount(other.CacheCreation1h)
	if cacheCreation5m > 0 || cacheCreation1h > 0 {
		return saturatingAddLogTokenCount(cacheCreation5m, cacheCreation1h)
	}
	return logOtherTokenCount(other.CacheCreation)
}

func logOtherUsesAnthropicCacheRate(other logCacheTelemetry) bool {
	semantic, _ := other.Semantic.(string)
	return strings.EqualFold(strings.TrimSpace(semantic), "anthropic") || other.Claude == true
}

func logOtherHasCacheReadTelemetry(other logCacheTelemetry) bool {
	count, ok := logOtherNumber(other.CacheRead)
	return ok && count >= 0 && !math.IsNaN(count) && !math.IsInf(count, 0)
}

func sumLogTokenStats(ctx context.Context, filter LogFilter) (stats logTokenStats, err error) {
	// Prompt and completion totals are ordinary numeric columns. Let the
	// database aggregate them instead of transferring and iterating every log
	// row in the application process.
	totalsQuery := LOG_DB.WithContext(ctx).Table("logs").Select(
		`COALESCE(SUM(CASE WHEN COALESCE(logs.prompt_tokens, 0) > 0 THEN logs.prompt_tokens ELSE 0 END), 0) AS input_tokens,
		 COALESCE(SUM(CASE WHEN COALESCE(logs.completion_tokens, 0) > 0 THEN logs.completion_tokens ELSE 0 END), 0) AS output_tokens`,
	)
	totalsQuery, err = applyLogFilters(totalsQuery, filter)
	if err != nil {
		return stats, err
	}
	var totals logTokenStatsTotals
	if err := totalsQuery.Scan(&totals).Error; err != nil {
		return stats, err
	}
	stats.InputTokens = normalizeLogTokenTotal(totals.InputTokens)
	stats.OutputTokens = normalizeLogTokenTotal(totals.OutputTokens)

	// Cache telemetry is stored in the legacy JSON `other` column, so it still
	// needs decoding. Restrict that pass to rows containing a cache field and
	// fetch bounded keyset batches so a large log table never creates one
	// unbounded result set or holds a long-lived cursor.
	lastID := 0
	for {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		telemetryQuery := LOG_DB.WithContext(ctx).Table("logs").Select("logs.id, logs.prompt_tokens, logs.other")
		telemetryQuery, err = applyLogFilters(telemetryQuery, filter)
		if err != nil {
			return stats, err
		}
		telemetryQuery = telemetryQuery.
			Where("logs.other LIKE ? ESCAPE '!'", "%cache!_%").
			Where("logs.id > ?", lastID).
			Order("logs.id ASC").
			Limit(logTokenStatsBatchSize)

		var rows []logTokenStatsRow
		if err := telemetryQuery.Scan(&rows).Error; err != nil {
			return stats, err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			inputTokens := normalizeLogTokenTotal(row.PromptTokens.Int64)
			var otherMap logCacheTelemetry
			if err := common.UnmarshalJsonStr(row.Other.String, &otherMap); err != nil {
				otherMap = logCacheTelemetry{}
			}
			cacheReadTokens := logOtherTokenCount(otherMap.CacheRead)
			cacheWriteTokens := logOtherCacheWriteTokens(otherMap)
			stats.CacheReadTokens = saturatingAddLogTokenCount(stats.CacheReadTokens, cacheReadTokens)
			stats.CacheWriteTokens = saturatingAddLogTokenCount(stats.CacheWriteTokens, cacheWriteTokens)

			cacheRateDenominator := inputTokens
			if logOtherUsesAnthropicCacheRate(otherMap) {
				cacheRateDenominator = saturatingAddLogTokenCount(cacheRateDenominator, cacheReadTokens)
			}
			if cacheRateDenominator > 0 && logOtherHasCacheReadTelemetry(otherMap) {
				stats.CacheRateNumerator = saturatingAddLogTokenCount(stats.CacheRateNumerator, cacheReadTokens)
				stats.CacheRateDenominator = saturatingAddLogTokenCount(stats.CacheRateDenominator, cacheRateDenominator)
			}
		}
		nextID := rows[len(rows)-1].Id
		if nextID <= lastID {
			return stats, errors.New("日志统计游标未前进")
		}
		lastID = nextID
		if len(rows) < logTokenStatsBatchSize {
			break
		}
	}
	return stats, nil
}

func sumUsedQuota(ctx context.Context, filter LogFilter) (stat Stat, err error) {
	if err := ctx.Err(); err != nil {
		return stat, err
	}
	recentCutoff := time.Now().Add(-60 * time.Second).Unix()
	tx := LOG_DB.WithContext(ctx).Table("logs").Select(
		`COALESCE(SUM(quota), 0) quota,
		COALESCE(SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END), 0) rpm,
		COALESCE(SUM(CASE WHEN created_at >= ? THEN COALESCE(prompt_tokens, 0) + COALESCE(completion_tokens, 0) ELSE 0 END), 0) tpm`,
		recentCutoff,
		recentCutoff,
	)

	filter.LogType = LogTypeConsume
	tx, err = applyLogFilters(tx, filter)
	if err != nil {
		return stat, err
	}

	if err := tx.Scan(&stat).Error; err != nil {
		common.SysError("failed to query log stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}

	tokenStats, err := sumLogTokenStats(ctx, filter)
	if err != nil {
		common.SysError("failed to query log token stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}
	stat.InputTokens = logTokenCountToInt(tokenStats.InputTokens)
	stat.CacheReadTokens = logTokenCountToInt(tokenStats.CacheReadTokens)
	stat.CacheWriteTokens = logTokenCountToInt(tokenStats.CacheWriteTokens)
	stat.OutputTokens = logTokenCountToInt(tokenStats.OutputTokens)
	if tokenStats.CacheRateDenominator > 0 {
		rate := float64(tokenStats.CacheRateNumerator) / float64(tokenStats.CacheRateDenominator)
		stat.CacheRate = &rate
	}

	return stat, nil
}

func SumUsedQuota(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, channel int, group string) (stat Stat, err error) {
	return SumUsedQuotaByFilter(LogFilter{
		LogType:        logType,
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
		ModelName:      modelName,
		Username:       username,
		TokenName:      tokenName,
		Channel:        channel,
		Group:          group,
	})
}

func SumUsedQuotaWithWildcardUsername(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, channel int, group string) (stat Stat, err error) {
	return SumUsedQuotaByFilter(LogFilter{
		LogType:        logType,
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
		ModelName:      modelName,
		Username:       username,
		UsernameLike:   true,
		TokenName:      tokenName,
		Channel:        channel,
		Group:          group,
	})
}

func SumUsedQuotaByFilter(filter LogFilter) (stat Stat, err error) {
	return SumUsedQuotaByFilterContext(context.Background(), filter)
}

func SumUsedQuotaByFilterContext(ctx context.Context, filter LogFilter) (Stat, error) {
	return sumUsedQuota(ctx, filter)
}

func SumUsedToken(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string) (token int) {
	// 错误在这里被丢弃（历史上该函数没有调用方）。方言不兼容会同样得到 0，
	// 所以 sumUsedTokenQuery 单独暴露给测试，让测试能在同一查询上取错误。
	_ = sumUsedTokenQuery(LOG_DB, startTimestamp, endTimestamp, modelName, username, tokenName).
		Scan(&token).Error
	return token
}

// sumUsedTokenQuery 构造 SumUsedToken 的聚合查询。
//
// COALESCE 是三个方言的共同函数；ifnull 只有 MySQL 和 SQLite 提供，
// PostgreSQL 会报 function ifnull(numeric, integer) does not exist。
// 同文件的 sumLogTokenStats 已经使用 COALESCE。
func sumUsedTokenQuery(db *gorm.DB, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string) *gorm.DB {
	tx := db.Table("logs").Select("COALESCE(sum(prompt_tokens),0) + COALESCE(sum(completion_tokens),0)")
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
	return tx.Where("type = ?", LogTypeConsume)
}

func DeleteOldLog(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	var total int64 = 0

	// SQLite does not accept DELETE ... LIMIT, and gorm silently drops the
	// Limit clause when the dialect cannot render it, so a single statement
	// would remove every expired row at once — the opposite of the bounded
	// batches the caller is asking for. Deleting by id ranges works on all
	// three dialects and keeps every DELETE statement within the limit.
	for {
		if nil != ctx.Err() {
			return total, ctx.Err()
		}

		var batchIds []int64
		if err := LOG_DB.Model(&Log{}).
			Where("created_at < ?", targetTimestamp).
			Order("id").
			Limit(limit).
			Pluck("id", &batchIds).Error; err != nil {
			return total, err
		}
		if len(batchIds) == 0 {
			break
		}

		result := LOG_DB.Where("id IN ?", batchIds).Delete(&Log{})
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

func CountLogsByFilter(filter LogFilter) (int64, error) {
	tx, err := applyLogFilters(LOG_DB.Model(&Log{}), filter)
	if err != nil {
		return 0, err
	}
	var count int64
	err = tx.Count(&count).Error
	return count, err
}

func CountLogsWithPayloadAuditFieldsByFilter(filter LogFilter, countRequest bool, countResponse bool) (int64, error) {
	if !countRequest && !countResponse {
		return 0, nil
	}
	tx, err := applyLogFilters(LOG_DB.Model(&Log{}), filter)
	if err != nil {
		return 0, err
	}
	tx = applyPayloadAuditFieldFilter(tx, countRequest, countResponse)
	var count int64
	err = tx.Count(&count).Error
	return count, err
}

func applyPayloadAuditFieldFilter(tx *gorm.DB, countRequest bool, countResponse bool) *gorm.DB {
	if countRequest && countResponse {
		return tx.Where("other LIKE ? OR other LIKE ?", `%"request_content":%`, `%"response_content":%`)
	}
	if countRequest {
		return tx.Where("other LIKE ?", `%"request_content":%`)
	}
	return tx.Where("other LIKE ?", `%"response_content":%`)
}

func DeleteLogsByFilterBatches(ctx context.Context, filter LogFilter, batchSize int, maxBatches int) (int64, error) {
	if batchSize <= 0 {
		batchSize = 100
	}
	if maxBatches == 0 {
		maxBatches = 1
	}

	var total int64
	for batch := 0; maxBatches < 0 || batch < maxBatches; batch++ {
		if err := ctx.Err(); err != nil {
			return total, err
		}

		scopedDB := LOG_DB.WithContext(ctx)
		tx, err := applyLogFilters(scopedDB.Model(&Log{}), filter)
		if err != nil {
			return total, err
		}

		var ids []int
		if err := tx.Select("logs.id").Order("logs.id asc").Limit(batchSize).Pluck("logs.id", &ids).Error; err != nil {
			return total, err
		}
		if len(ids) == 0 {
			break
		}

		result := scopedDB.Where("id IN ?", ids).Delete(&Log{})
		if result.Error != nil {
			return total, result.Error
		}
		if result.RowsAffected == 0 {
			return total, errors.New("log deletion made no progress")
		}
		total += result.RowsAffected
		if len(ids) < batchSize {
			break
		}
	}

	return total, nil
}

const payloadAuditClearAllBatches = -1

func ClearPayloadAuditFieldsByFilter(ctx context.Context, filter LogFilter, clearRequest bool, clearResponse bool, batchSize int) (int64, error) {
	return ClearPayloadAuditFieldsByFilterBatches(ctx, filter, clearRequest, clearResponse, batchSize, payloadAuditClearAllBatches)
}

func ClearPayloadAuditFieldsByFilterBatches(ctx context.Context, filter LogFilter, clearRequest bool, clearResponse bool, batchSize int, maxBatches int) (int64, error) {
	if !clearRequest && !clearResponse {
		return 0, nil
	}
	if batchSize <= 0 {
		batchSize = 200
	}
	if maxBatches == 0 {
		maxBatches = 1
	}

	var total int64
	lastID := 0
	for batch := 0; maxBatches < 0 || batch < maxBatches; batch++ {
		if err := ctx.Err(); err != nil {
			return total, err
		}

		scopedDB := LOG_DB.WithContext(ctx)
		tx, err := applyLogFilters(scopedDB.Model(&Log{}), filter)
		if err != nil {
			return total, err
		}
		tx = applyPayloadAuditFieldFilter(tx, clearRequest, clearResponse)

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
			if err := scopedDB.Model(&Log{}).Where("id = ?", log.Id).Update("other", common.MapToJsonStr(otherMap)).Error; err != nil {
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
