package operation_setting

import (
	"os"
	"strconv"

	"github.com/QuantumNous/new-api/setting/config"
)

type MonitorSetting struct {
	AutoTestChannelEnabled bool    `json:"auto_test_channel_enabled"`
	AutoTestChannelMinutes float64 `json:"auto_test_channel_minutes"`
}

// 默认配置
var monitorSetting = MonitorSetting{
	AutoTestChannelEnabled: false,
	AutoTestChannelMinutes: 10,
}

// channelTestFrequencyOverride 返回 CHANNEL_TEST_FREQUENCY 的覆盖值（分钟），
// 未设置或非法时返回 0。原来每次 getter 调用都解析并就地写包级结构体，两个
// 并发读者之间就会互相写同一字段；现在解析结果只施加在读取快照上。
//
// 每次调用都读环境变量而不是在包 init 里缓存：.env 由 main() 加载，晚于包
// init，缓存会让写在 .env 里的值失效。
func channelTestFrequencyOverride() float64 {
	raw := os.Getenv("CHANNEL_TEST_FREQUENCY")
	if raw == "" {
		return 0
	}
	frequency, err := strconv.Atoi(raw)
	if err != nil || frequency <= 0 {
		return 0
	}
	return float64(frequency)
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("monitor_setting", &monitorSetting)
}

// GetMonitorSetting 指针 getter 只保留给测试播种配置；生产代码读
// MonitorSettingSnapshot，避免与写锁内的逐字段重写构成数据竞争。
func GetMonitorSetting() *MonitorSetting {
	return &monitorSetting
}

// MonitorSettingSnapshot 返回当前渠道监控配置的浅拷贝，并叠加启动期的环境变量覆盖。
func MonitorSettingSnapshot() MonitorSetting {
	snapshot, ok := config.SnapshotOf[MonitorSetting]("monitor_setting")
	if !ok {
		snapshot = monitorSetting
	}
	if frequency := channelTestFrequencyOverride(); frequency > 0 {
		snapshot.AutoTestChannelEnabled = true
		snapshot.AutoTestChannelMinutes = frequency
	}
	return snapshot
}
