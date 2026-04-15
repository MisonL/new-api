package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func GetAllLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	requestId := c.Query("request_id")
	logs, total, err := model.GetAllLogs(logType, startTimestamp, endTimestamp, modelName, username, tokenName, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), channel, group, requestId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
	return
}

func GetUserLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	userId := c.GetInt("id")
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	group := c.Query("group")
	requestId := c.Query("request_id")
	logs, total, err := model.GetUserLogs(userId, logType, startTimestamp, endTimestamp, modelName, tokenName, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), group, requestId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
	return
}

// Deprecated: SearchAllLogs 已废弃，前端未使用该接口。
func SearchAllLogs(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": false,
		"message": "该接口已废弃",
	})
}

// Deprecated: SearchUserLogs 已废弃，前端未使用该接口。
func SearchUserLogs(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": false,
		"message": "该接口已废弃",
	})
}

func GetLogByKey(c *gin.Context) {
	tokenId := c.GetInt("token_id")
	if tokenId == 0 {
		c.JSON(200, gin.H{
			"success": false,
			"message": "无效的令牌",
		})
		return
	}
	logs, err := model.GetLogByTokenId(tokenId)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(200, gin.H{
		"success": true,
		"message": "",
		"data":    logs,
	})
}

func GetLogsStat(c *gin.Context) {
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	tokenName := c.Query("token_name")
	username := c.Query("username")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	stat, err := model.SumUsedQuota(logType, startTimestamp, endTimestamp, modelName, username, tokenName, channel, group)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	//tokenNum := model.SumUsedToken(logType, startTimestamp, endTimestamp, modelName, username, "")
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"quota": stat.Quota,
			"rpm":   stat.Rpm,
			"tpm":   stat.Tpm,
		},
	})
	return
}

func GetLogsSelfStat(c *gin.Context) {
	username := c.GetString("username")
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	quotaNum, err := model.SumUsedQuota(logType, startTimestamp, endTimestamp, modelName, username, tokenName, channel, group)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	//tokenNum := model.SumUsedToken(logType, startTimestamp, endTimestamp, modelName, username, tokenName)
	c.JSON(200, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"quota": quotaNum.Quota,
			"rpm":   quotaNum.Rpm,
			"tpm":   quotaNum.Tpm,
			//"token": tokenNum,
		},
	})
	return
}

func DeleteHistoryLogs(c *gin.Context) {
	targetTimestamp, _ := strconv.ParseInt(c.Query("target_timestamp"), 10, 64)
	if targetTimestamp == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "target timestamp is required",
		})
		return
	}
	count, err := model.DeleteOldLog(c.Request.Context(), targetTimestamp, 100)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    count,
	})
	return
}

type BatchDeleteLogsRequest struct {
	LogType        int    `json:"type"`
	StartTimestamp int64  `json:"start_timestamp"`
	EndTimestamp   int64  `json:"end_timestamp"`
	Username       string `json:"username,omitempty"`
	TokenName      string `json:"token_name,omitempty"`
	ModelName      string `json:"model_name,omitempty"`
	Channel        int    `json:"channel,omitempty"`
	Group          string `json:"group,omitempty"`
	RequestID      string `json:"request_id,omitempty"`
}

type ClearLogPayloadRequest struct {
	ClearRequestContent  bool `json:"clear_request_content"`
	ClearResponseContent bool `json:"clear_response_content"`
}

func DeleteLog(c *gin.Context) {
	logID, _ := strconv.Atoi(c.Param("id"))
	if logID <= 0 {
		common.ApiErrorMsg(c, "invalid log id")
		return
	}
	rowsAffected, err := model.DeleteLogByID(logID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if rowsAffected == 0 {
		common.ApiErrorMsg(c, "log not found")
		return
	}
	common.ApiSuccess(c, gin.H{"deleted": 1})
}

func DeleteUserLog(c *gin.Context) {
	logID, _ := strconv.Atoi(c.Param("id"))
	if logID <= 0 {
		common.ApiErrorMsg(c, "invalid log id")
		return
	}
	if err := model.DeleteUserLogByID(c.GetInt("id"), logID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiErrorMsg(c, "log not found")
			return
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"deleted": 1})
}

func BatchDeleteLogs(c *gin.Context) {
	var req BatchDeleteLogsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "invalid request")
		return
	}
	if !hasScopedLogFilter(req) {
		common.ApiErrorMsg(c, "at least one filter is required")
		return
	}
	count, err := model.DeleteLogsByFilter(c.Request.Context(), model.LogFilter{
		LogType:        req.LogType,
		StartTimestamp: req.StartTimestamp,
		EndTimestamp:   req.EndTimestamp,
		ModelName:      req.ModelName,
		Username:       req.Username,
		TokenName:      req.TokenName,
		Channel:        req.Channel,
		Group:          req.Group,
		RequestId:      req.RequestID,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"deleted": count})
}

func BatchDeleteUserLogs(c *gin.Context) {
	var req BatchDeleteLogsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "invalid request")
		return
	}
	if !hasScopedLogFilter(req) {
		common.ApiErrorMsg(c, "at least one filter is required")
		return
	}
	count, err := model.DeleteLogsByFilter(c.Request.Context(), model.LogFilter{
		UserId:         c.GetInt("id"),
		LogType:        req.LogType,
		StartTimestamp: req.StartTimestamp,
		EndTimestamp:   req.EndTimestamp,
		ModelName:      req.ModelName,
		TokenName:      req.TokenName,
		Channel:        req.Channel,
		Group:          req.Group,
		RequestId:      req.RequestID,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"deleted": count})
}

func ClearUserLogPayloads(c *gin.Context) {
	var req ClearLogPayloadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "invalid request")
		return
	}
	if !req.ClearRequestContent && !req.ClearResponseContent {
		common.ApiErrorMsg(c, "nothing to clear")
		return
	}
	count, err := model.ClearPayloadAuditFieldsByFilter(c.Request.Context(), model.LogFilter{
		UserId: c.GetInt("id"),
	}, req.ClearRequestContent, req.ClearResponseContent, 200)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"updated": count})
}

func hasScopedLogFilter(req BatchDeleteLogsRequest) bool {
	return req.StartTimestamp != 0 ||
		req.EndTimestamp != 0 ||
		req.Username != "" ||
		req.TokenName != "" ||
		req.ModelName != "" ||
		req.Channel != 0 ||
		req.Group != "" ||
		req.RequestID != "" ||
		req.LogType != 0
}
