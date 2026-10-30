package system_setting

import "github.com/QuantumNous/new-api/setting/config"

type LegalSettings struct {
	UserAgreement string `json:"user_agreement"`
	PrivacyPolicy string `json:"privacy_policy"`
}

var defaultLegalSettings = LegalSettings{
	UserAgreement: "",
	PrivacyPolicy: "",
}

func init() {
	config.GlobalConfig.Register("legal", &defaultLegalSettings)
}

// GetLegalSettings 指针 getter 只保留给测试播种配置；生产代码读
// readLegalSettings 或 LegalSettingsSnapshot，避免与写锁内的逐字段重写竞争。
func GetLegalSettings() *LegalSettings {
	return &defaultLegalSettings
}

// readLegalSettings 在配置管理器读锁内把当前配置交给 fn。
func readLegalSettings(fn func(*LegalSettings)) {
	if fn == nil {
		return
	}
	if !config.ReadLiveAs("legal", fn) {
		fn(&defaultLegalSettings)
	}
}

// LegalSettingsSnapshot 返回当前法律条款配置的浅拷贝。
func LegalSettingsSnapshot() LegalSettings {
	if snapshot, ok := config.SnapshotOf[LegalSettings]("legal"); ok {
		return snapshot
	}
	// 未注册时没有任何写入方能改这个对象，直接读包级变量不会构成竞争。
	return defaultLegalSettings
}
