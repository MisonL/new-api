package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// CheckinSetting 签到功能配置
type CheckinSetting struct {
	Enabled  bool `json:"enabled"`   // 是否启用签到功能
	MinQuota int  `json:"min_quota"` // 签到最小额度奖励
	MaxQuota int  `json:"max_quota"` // 签到最大额度奖励
}

// 默认配置
var checkinSetting = CheckinSetting{
	Enabled:  false, // 默认关闭
	MinQuota: 1000,  // 默认最小额度 1000 (约 0.002 USD)
	MaxQuota: 10000, // 默认最大额度 10000 (约 0.02 USD)
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("checkin_setting", &checkinSetting)
}

// GetCheckinSetting 指针 getter 只保留给测试播种配置；生产代码读
// CheckinSettingSnapshot，避免与写锁内的逐字段重写构成数据竞争。
func GetCheckinSetting() *CheckinSetting {
	return &checkinSetting
}

// CheckinSettingSnapshot 返回当前签到配置的浅拷贝。
func CheckinSettingSnapshot() CheckinSetting {
	if snapshot, ok := config.SnapshotOf[CheckinSetting]("checkin_setting"); ok {
		return snapshot
	}
	return checkinSetting
}

// IsCheckinEnabled 是否启用签到功能
func IsCheckinEnabled() bool {
	return CheckinSettingSnapshot().Enabled
}

// GetCheckinQuotaRange 获取签到额度范围
func GetCheckinQuotaRange() (min, max int) {
	snapshot := CheckinSettingSnapshot()
	return snapshot.MinQuota, snapshot.MaxQuota
}
