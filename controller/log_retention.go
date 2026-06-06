package controller

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/log_retention_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

const (
	logRetentionInitialDelay         = 5 * time.Minute
	logRetentionDisabledPollInterval = 5 * time.Minute
)

var logRetentionRunMu sync.Mutex

type LogRetentionExecuteRequest struct {
	Preview bool `json:"preview"`
}

type LogRetentionPolicyResult struct {
	LogType       int    `json:"type"`
	Name          string `json:"name"`
	RetentionDays int    `json:"retention_days"`
	Cutoff        int64  `json:"cutoff"`
	Matched       int64  `json:"matched"`
	Deleted       int64  `json:"deleted"`
}

type PayloadRetentionResult struct {
	Field         string `json:"field"`
	RetentionDays int    `json:"retention_days"`
	Cutoff        int64  `json:"cutoff"`
	Matched       int64  `json:"matched"`
	Updated       int64  `json:"updated"`
}

type ServerLogRetentionResult struct {
	Enabled      bool     `json:"enabled"`
	LogDir       string   `json:"log_dir"`
	MatchedFiles int      `json:"matched_files"`
	DeletedFiles int      `json:"deleted_files"`
	FreedBytes   int64    `json:"freed_bytes"`
	FailedFiles  []string `json:"failed_files"`
}

type LogRetentionResult struct {
	Preview        bool                       `json:"preview"`
	StartedAt      int64                      `json:"started_at"`
	FinishedAt     int64                      `json:"finished_at"`
	DatabaseLogs   []LogRetentionPolicyResult `json:"database_logs"`
	Payloads       []PayloadRetentionResult   `json:"payloads"`
	ServerLogFiles ServerLogRetentionResult   `json:"server_log_files"`
}

type logRetentionPolicy struct {
	logType int
	name    string
	days    int
}

func normalizeRetentionBatchSize(value int) int {
	if value <= 0 {
		return log_retention_setting.DefaultBatchSize
	}
	if value > 5000 {
		return 5000
	}
	return value
}

func normalizeRetentionMaxBatches(value int) int {
	if value <= 0 {
		return log_retention_setting.DefaultMaxBatches
	}
	if value > 1000 {
		return 1000
	}
	return value
}

func normalizeRetentionRunInterval(value int) time.Duration {
	if value <= 0 {
		value = log_retention_setting.DefaultRunIntervalHours
	}
	return time.Duration(value) * time.Hour
}

func logRetentionSleepDuration(setting log_retention_setting.LogRetentionSetting) time.Duration {
	interval := normalizeRetentionRunInterval(setting.RunIntervalHours)
	if setting.Enabled || interval < logRetentionDisabledPollInterval {
		return interval
	}
	return logRetentionDisabledPollInterval
}

func logRetentionPolicies(setting log_retention_setting.LogRetentionSetting) []logRetentionPolicy {
	return []logRetentionPolicy{
		{logType: model.LogTypeUnknown, name: "unknown", days: setting.UnknownRetentionDays},
		{logType: model.LogTypeTopup, name: "topup", days: setting.TopupRetentionDays},
		{logType: model.LogTypeConsume, name: "consume", days: setting.ConsumeRetentionDays},
		{logType: model.LogTypeManage, name: "manage", days: setting.ManageRetentionDays},
		{logType: model.LogTypeSystem, name: "system", days: setting.SystemRetentionDays},
		{logType: model.LogTypeError, name: "error", days: setting.ErrorRetentionDays},
		{logType: model.LogTypeRefund, name: "refund", days: setting.RefundRetentionDays},
	}
}

func retentionCutoff(now time.Time, days int) int64 {
	return now.AddDate(0, 0, -days).Unix()
}

func executeLogRetention(ctx context.Context, preview bool, setting log_retention_setting.LogRetentionSetting) (LogRetentionResult, error) {
	logRetentionRunMu.Lock()
	defer logRetentionRunMu.Unlock()

	now := time.Now()
	result := LogRetentionResult{
		Preview:    preview,
		StartedAt:  now.Unix(),
		FinishedAt: now.Unix(),
	}

	batchSize := normalizeRetentionBatchSize(setting.BatchSize)
	maxBatches := normalizeRetentionMaxBatches(setting.MaxBatches)
	for _, policy := range logRetentionPolicies(setting) {
		if policy.days <= 0 {
			continue
		}
		cutoff := retentionCutoff(now, policy.days)
		item := LogRetentionPolicyResult{
			LogType:       policy.logType,
			Name:          policy.name,
			RetentionDays: policy.days,
			Cutoff:        cutoff,
		}
		filter := model.LogFilter{
			LogType:      policy.logType,
			LogTypeSet:   policy.logType == model.LogTypeUnknown,
			EndTimestamp: cutoff,
		}
		count, err := model.CountLogsByFilter(filter)
		if err != nil {
			return result, err
		}
		item.Matched = count
		if !preview && count > 0 {
			deleted, err := model.DeleteLogsByFilterBatches(ctx, filter, batchSize, maxBatches)
			if err != nil {
				return result, err
			}
			item.Deleted = deleted
		}
		result.DatabaseLogs = append(result.DatabaseLogs, item)
	}

	payloads, err := executePayloadRetention(ctx, preview, setting, now, batchSize)
	if err != nil {
		return result, err
	}
	result.Payloads = payloads

	serverLogResult, err := executeServerLogRetention(preview, setting, now)
	if err != nil {
		return result, err
	}
	result.ServerLogFiles = serverLogResult
	result.FinishedAt = time.Now().Unix()
	return result, nil
}

func executePayloadRetention(ctx context.Context, preview bool, setting log_retention_setting.LogRetentionSetting, now time.Time, batchSize int) ([]PayloadRetentionResult, error) {
	policies := []struct {
		field string
		days  int
	}{
		{field: "request", days: setting.RequestPayloadRetentionDays},
		{field: "response", days: setting.ResponsePayloadRetentionDays},
	}
	results := make([]PayloadRetentionResult, 0, len(policies))
	for _, policy := range policies {
		if policy.days <= 0 {
			continue
		}
		cutoff := retentionCutoff(now, policy.days)
		item := PayloadRetentionResult{
			Field:         policy.field,
			RetentionDays: policy.days,
			Cutoff:        cutoff,
		}
		filter := model.LogFilter{EndTimestamp: cutoff}
		countRequest := policy.field == "request"
		countResponse := policy.field == "response"
		count, err := model.CountLogsWithPayloadAuditFieldsByFilter(filter, countRequest, countResponse)
		if err != nil {
			return results, err
		}
		item.Matched = count
		if !preview && count > 0 {
			updated, err := model.ClearPayloadAuditFieldsByFilterBatches(ctx, filter, countRequest, countResponse, batchSize, normalizeRetentionMaxBatches(setting.MaxBatches))
			if err != nil {
				return results, err
			}
			item.Updated = updated
		}
		results = append(results, item)
	}
	return results, nil
}

func executeServerLogRetention(preview bool, setting log_retention_setting.LogRetentionSetting, now time.Time) (ServerLogRetentionResult, error) {
	result := ServerLogRetentionResult{
		Enabled: setting.ServerLogCleanupEnabled,
		LogDir:  *common.LogDir,
	}
	if !setting.ServerLogCleanupEnabled || *common.LogDir == "" {
		return result, nil
	}

	files, err := getLogFiles()
	if err != nil {
		return result, err
	}
	toDelete := selectServerLogFilesForRetention(files, setting, now, logger.GetCurrentLogPath())
	result.MatchedFiles = len(toDelete)
	if preview {
		return result, nil
	}

	for _, file := range toDelete {
		fullPath := filepath.Join(*common.LogDir, file.Name)
		if err := os.Remove(fullPath); err != nil {
			result.FailedFiles = append(result.FailedFiles, file.Name)
			continue
		}
		result.DeletedFiles++
		result.FreedBytes += file.Size
	}
	return result, nil
}

func selectServerLogFilesForRetention(files []LogFileInfo, setting log_retention_setting.LogRetentionSetting, now time.Time, activeLogPath string) []LogFileInfo {
	if len(files) == 0 {
		return nil
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name > files[j].Name
	})

	selected := make(map[string]LogFileInfo)
	addFile := func(file LogFileInfo) {
		fullPath := filepath.Join(*common.LogDir, file.Name)
		if fullPath == activeLogPath {
			return
		}
		selected[file.Name] = file
	}

	if setting.ServerLogKeepFiles > 0 {
		for i, file := range files {
			if i >= setting.ServerLogKeepFiles {
				addFile(file)
			}
		}
	}
	if setting.ServerLogKeepDays > 0 {
		cutoff := now.AddDate(0, 0, -setting.ServerLogKeepDays)
		for _, file := range files {
			if file.ModTime.Before(cutoff) {
				addFile(file)
			}
		}
	}
	if setting.ServerLogMaxTotalSizeMB > 0 {
		maxBytes := int64(setting.ServerLogMaxTotalSizeMB) * 1024 * 1024
		var total int64
		for _, file := range files {
			total += file.Size
		}
		if total > maxBytes {
			oldestFirst := append([]LogFileInfo(nil), files...)
			sort.Slice(oldestFirst, func(i, j int) bool {
				return oldestFirst[i].Name < oldestFirst[j].Name
			})
			for _, file := range oldestFirst {
				if total <= maxBytes {
					break
				}
				fullPath := filepath.Join(*common.LogDir, file.Name)
				if fullPath == activeLogPath {
					continue
				}
				addFile(file)
				total -= file.Size
			}
		}
	}

	toDelete := make([]LogFileInfo, 0, len(selected))
	for _, file := range selected {
		toDelete = append(toDelete, file)
	}
	sort.Slice(toDelete, func(i, j int) bool {
		return toDelete[i].Name < toDelete[j].Name
	})
	return toDelete
}

func ExecuteLogRetention(c *gin.Context) {
	var req LogRetentionExecuteRequest
	if c.Request.Body != nil {
		if err := common.DecodeJson(c.Request.Body, &req); err != nil {
			common.ApiErrorMsg(c, "invalid request")
			return
		}
	}
	result, err := executeLogRetention(c.Request.Context(), req.Preview, log_retention_setting.GetLogRetentionSetting())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !req.Preview {
		common.SysLog(fmt.Sprintf("log retention executed: database=%d payloads=%d server_files=%d",
			len(result.DatabaseLogs), len(result.Payloads), result.ServerLogFiles.DeletedFiles))
	}
	common.ApiSuccess(c, result)
}

func StartLogRetentionTask() {
	gopool.Go(func() {
		common.SleepBeforeMaintenanceLoop(logRetentionInitialDelay)
		for {
			setting := log_retention_setting.GetLogRetentionSetting()
			sleepDuration := logRetentionSleepDuration(setting)
			if common.IsMasterNode && setting.Enabled {
				result, err := executeLogRetention(context.Background(), false, setting)
				if err != nil {
					common.SysError("log retention task failed: " + err.Error())
				} else {
					common.SysLog(fmt.Sprintf("log retention task completed: database=%d payloads=%d server_files=%d",
						len(result.DatabaseLogs), len(result.Payloads), result.ServerLogFiles.DeletedFiles))
				}
			}
			time.Sleep(sleepDuration)
		}
	})
}
