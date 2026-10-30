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

// GetLogRetentionSetting 返回当前日志保留配置的浅拷贝。直接返回包级变量的值
// 会与管理端 Update 的逐字段重写构成数据竞争；字段均为标量，浅拷贝即完整。
func GetLogRetentionSetting() LogRetentionSetting {
	if snapshot, ok := config.SnapshotOf[LogRetentionSetting](ConfigName); ok {
		return snapshot
	}
	return logRetentionSetting
}

func ValidateValue(key string, value string) error {
	_, err := NormalizeValue(key, value)
	return err
}

// NormalizeValue validates a value and returns its canonical string form.
//
// ValidateValue trims whitespace before checking, but the value that reaches
// updateConfigFromMap is the untrimmed one: strconv.ParseBool(" true ") fails,
// the field is left at its previous value, and the write silently does nothing
// while the API reports success. Returning the trimmed (and for booleans,
// canonical) form lets the caller persist what it actually validated.
func NormalizeValue(key string, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	switch key {
	case "enabled", "server_log_cleanup_enabled":
		enabled, err := strconv.ParseBool(trimmed)
		if err != nil {
			return "", errors.New("日志保留策略开关值不合法")
		}
		return strconv.FormatBool(enabled), nil
	case "run_interval_hours":
		return normalizeIntRange(trimmed, 1, maxRunIntervalHours, "日志保留执行间隔")
	case "batch_size":
		return normalizeIntRange(trimmed, 1, maxBatchSize, "日志保留批大小")
	case "max_batches":
		return normalizeIntRange(trimmed, 1, maxMaxBatches, "日志保留最大批次数")
	case "consume_retention_days",
		"error_retention_days",
		"system_retention_days",
		"manage_retention_days",
		"topup_retention_days",
		"refund_retention_days",
		"unknown_retention_days",
		"request_payload_retention_days",
		"response_payload_retention_days":
		return normalizeIntRange(trimmed, 0, maxRetentionDays, "日志保留天数")
	case "server_log_keep_files":
		return normalizeIntRange(trimmed, 0, maxServerKeepFiles, "运行日志保留文件数")
	case "server_log_keep_days":
		return normalizeIntRange(trimmed, 0, maxServerKeepDays, "运行日志保留天数")
	case "server_log_max_total_size_mb":
		return normalizeIntRange(trimmed, 0, maxServerSizeMB, "运行日志最大总大小")
	default:
		return trimmed, nil
	}
}

func normalizeIntRange(value string, min int, max int, name string) (string, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < min || parsed > max {
		return "", errors.New(name + "必须在 " + strconv.Itoa(min) + " 到 " + strconv.Itoa(max) + " 之间")
	}
	return strconv.Itoa(parsed), nil
}
