package performance_setting

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

// PerformanceSetting 性能设置配置
type PerformanceSetting struct {
	// DiskCacheEnabled 是否启用磁盘缓存（磁盘换内存）
	DiskCacheEnabled bool `json:"disk_cache_enabled"`
	// DiskCacheThresholdMB 触发磁盘缓存的请求体大小阈值（MB）
	DiskCacheThresholdMB int `json:"disk_cache_threshold_mb"`
	// DiskCacheMaxSizeMB 磁盘缓存最大总大小（MB）
	DiskCacheMaxSizeMB int `json:"disk_cache_max_size_mb"`
	// DiskCachePath 磁盘缓存目录
	DiskCachePath string `json:"disk_cache_path"`

	// MonitorEnabled 是否启用性能监控
	MonitorEnabled bool `json:"monitor_enabled"`
	// MonitorCPUThreshold CPU 使用率阈值（%）
	MonitorCPUThreshold int `json:"monitor_cpu_threshold"`
	// MonitorMemoryThreshold 内存使用率阈值（%）
	MonitorMemoryThreshold int `json:"monitor_memory_threshold"`
	// MonitorDiskThreshold 磁盘使用率阈值（%）
	MonitorDiskThreshold int `json:"monitor_disk_threshold"`
}

// 默认配置
var performanceSetting = PerformanceSetting{
	DiskCacheEnabled:     false,
	DiskCacheThresholdMB: 10,   // 超过 10MB 使用磁盘缓存
	DiskCacheMaxSizeMB:   1024, // 最大 1GB 磁盘缓存
	DiskCachePath:        "",   // 空表示使用系统临时目录

	MonitorEnabled:         true,
	MonitorCPUThreshold:    90,
	MonitorMemoryThreshold: 90,
	MonitorDiskThreshold:   95,
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("performance_setting", &performanceSetting)
	// 同步初始配置到 common 包
	syncToCommon()
}

// syncToCommon 将配置同步到 common 包。UpdateAndSync 在写锁释放之后调用，
// 此时另一个并发 Update 可能正在改写包级结构体，所以这里取锁内快照。
func syncToCommon() {
	settings := PerformanceSettingSnapshot()
	common.SetDiskCacheConfig(common.DiskCacheConfig{
		Enabled:     settings.DiskCacheEnabled,
		ThresholdMB: settings.DiskCacheThresholdMB,
		MaxSizeMB:   settings.DiskCacheMaxSizeMB,
		Path:        settings.DiskCachePath,
	})

	common.SetPerformanceMonitorConfig(common.PerformanceMonitorConfig{
		Enabled:         settings.MonitorEnabled,
		CPUThreshold:    settings.MonitorCPUThreshold,
		MemoryThreshold: settings.MonitorMemoryThreshold,
		DiskThreshold:   settings.MonitorDiskThreshold,
	})
}

// GetPerformanceSetting 指针 getter 只保留给测试播种配置；生产代码读
// PerformanceSettingSnapshot，避免与写锁内的逐字段重写构成数据竞争。
func GetPerformanceSetting() *PerformanceSetting {
	return &performanceSetting
}

// PerformanceSettingSnapshot 返回当前性能配置的浅拷贝。
func PerformanceSettingSnapshot() PerformanceSetting {
	if snapshot, ok := config.SnapshotOf[PerformanceSetting]("performance_setting"); ok {
		return snapshot
	}
	return performanceSetting
}

// UpdateAndSync 更新配置并同步到 common 包
// 当配置从数据库加载后，需要调用此函数同步
func UpdateAndSync() {
	syncToCommon()
}

// GetCacheStats 获取缓存统计信息（代理到 common 包）
func GetCacheStats() common.DiskCacheStats {
	return common.GetDiskCacheStats()
}

// ResetStats 重置统计信息
func ResetStats() {
	common.ResetDiskCacheStats()
}
