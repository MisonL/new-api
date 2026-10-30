package perf_metrics_setting

import "github.com/QuantumNous/new-api/setting/config"

type PerfMetricsSetting struct {
	Enabled       bool   `json:"enabled"`
	FlushInterval int    `json:"flush_interval"`
	BucketTime    string `json:"bucket_time"`
	RetentionDays int    `json:"retention_days"`
}

var perfMetricsSetting = PerfMetricsSetting{
	Enabled:       true,
	FlushInterval: 5,
	BucketTime:    "hour",
	RetentionDays: 0,
}

func init() {
	config.GlobalConfig.Register("perf_metrics_setting", &perfMetricsSetting)
}

// GetSetting 返回当前性能指标配置的浅拷贝。直接返回包级变量的值会与管理端
// Update 的逐字段重写构成数据竞争；字段均为标量，浅拷贝即完整。
func GetSetting() PerfMetricsSetting {
	if snapshot, ok := config.SnapshotOf[PerfMetricsSetting]("perf_metrics_setting"); ok {
		return snapshot
	}
	return perfMetricsSetting
}

func GetBucketSeconds() int64 {
	switch GetSetting().BucketTime {
	case "minute":
		return 60
	case "5min":
		return 300
	case "hour":
		return 3600
	default:
		return 3600
	}
}

func GetFlushIntervalMinutes() int {
	flushInterval := GetSetting().FlushInterval
	if flushInterval < 1 {
		return 1
	}
	return flushInterval
}
