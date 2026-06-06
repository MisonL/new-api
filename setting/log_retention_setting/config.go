package log_retention_setting

import (
	"errors"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

const (
	ConfigName = "log_retention_setting"

	DefaultRunIntervalHours = 24
	DefaultBatchSize        = 500
	DefaultMaxBatches       = 200
	DefaultServerKeepFiles  = 30
	DefaultServerKeepDays   = 30

	maxRunIntervalHours = 24 * 30
	maxRetentionDays    = 3650
	maxBatchSize        = 5000
	maxMaxBatches       = 1000
	maxServerKeepFiles  = 10000
	maxServerKeepDays   = 3650
	maxServerSizeMB     = 1024 * 1024
)

type LogRetentionSetting struct {
	Enabled                      bool `json:"enabled"`
	RunIntervalHours             int  `json:"run_interval_hours"`
	BatchSize                    int  `json:"batch_size"`
	MaxBatches                   int  `json:"max_batches"`
	ConsumeRetentionDays         int  `json:"consume_retention_days"`
	ErrorRetentionDays           int  `json:"error_retention_days"`
	SystemRetentionDays          int  `json:"system_retention_days"`
	ManageRetentionDays          int  `json:"manage_retention_days"`
	TopupRetentionDays           int  `json:"topup_retention_days"`
	RefundRetentionDays          int  `json:"refund_retention_days"`
	UnknownRetentionDays         int  `json:"unknown_retention_days"`
	RequestPayloadRetentionDays  int  `json:"request_payload_retention_days"`
	ResponsePayloadRetentionDays int  `json:"response_payload_retention_days"`
	ServerLogCleanupEnabled      bool `json:"server_log_cleanup_enabled"`
	ServerLogKeepFiles           int  `json:"server_log_keep_files"`
	ServerLogKeepDays            int  `json:"server_log_keep_days"`
	ServerLogMaxTotalSizeMB      int  `json:"server_log_max_total_size_mb"`
}

var logRetentionSetting = LogRetentionSetting{
	Enabled:            false,
	RunIntervalHours:   DefaultRunIntervalHours,
	BatchSize:          DefaultBatchSize,
	MaxBatches:         DefaultMaxBatches,
	ServerLogKeepFiles: DefaultServerKeepFiles,
	ServerLogKeepDays:  DefaultServerKeepDays,
}

func init() {
	config.GlobalConfig.Register(ConfigName, &logRetentionSetting)
}

func GetLogRetentionSetting() LogRetentionSetting {
	return logRetentionSetting
}

func ValidateValue(key string, value string) error {
	trimmed := strings.TrimSpace(value)
	switch key {
	case "enabled", "server_log_cleanup_enabled":
		if _, err := strconv.ParseBool(trimmed); err != nil {
			return errors.New("日志保留策略开关值不合法")
		}
		return nil
	case "run_interval_hours":
		return validateIntRange(trimmed, 1, maxRunIntervalHours, "日志保留执行间隔")
	case "batch_size":
		return validateIntRange(trimmed, 1, maxBatchSize, "日志保留批大小")
	case "max_batches":
		return validateIntRange(trimmed, 1, maxMaxBatches, "日志保留最大批次数")
	case "consume_retention_days",
		"error_retention_days",
		"system_retention_days",
		"manage_retention_days",
		"topup_retention_days",
		"refund_retention_days",
		"unknown_retention_days",
		"request_payload_retention_days",
		"response_payload_retention_days":
		return validateIntRange(trimmed, 0, maxRetentionDays, "日志保留天数")
	case "server_log_keep_files":
		return validateIntRange(trimmed, 0, maxServerKeepFiles, "运行日志保留文件数")
	case "server_log_keep_days":
		return validateIntRange(trimmed, 0, maxServerKeepDays, "运行日志保留天数")
	case "server_log_max_total_size_mb":
		return validateIntRange(trimmed, 0, maxServerSizeMB, "运行日志最大总大小")
	default:
		return nil
	}
}

func validateIntRange(value string, min int, max int, name string) error {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < min || parsed > max {
		return errors.New(name + "必须在 " + strconv.Itoa(min) + " 到 " + strconv.Itoa(max) + " 之间")
	}
	return nil
}
