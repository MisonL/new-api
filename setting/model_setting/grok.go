package model_setting

import "github.com/QuantumNous/new-api/setting/config"

// GrokSettings defines Grok model configuration.
type GrokSettings struct {
	ViolationDeductionEnabled bool    `json:"violation_deduction_enabled"`
	ViolationDeductionAmount  float64 `json:"violation_deduction_amount"`
}

var defaultGrokSettings = GrokSettings{
	ViolationDeductionEnabled: true,
	ViolationDeductionAmount:  0.05,
}

var grokSettings = defaultGrokSettings

func init() {
	config.GlobalConfig.Register("grok", &grokSettings)
}

// GetGrokSettings 指针 getter 只保留给测试播种配置；生产代码读 readGrokSettings
// 或 GrokSettingsSnapshot，避免与写锁内的逐字段重写构成数据竞争。
func GetGrokSettings() *GrokSettings {
	return &grokSettings
}

// readGrokSettings 在配置管理器读锁内把当前配置交给 fn。
func readGrokSettings(fn func(*GrokSettings)) {
	if fn == nil {
		return
	}
	if !config.ReadLiveAs("grok", fn) {
		fn(&grokSettings)
	}
}

// GrokSettingsSnapshot 返回当前 Grok 配置的浅拷贝。
func GrokSettingsSnapshot() GrokSettings {
	if snapshot, ok := config.SnapshotOf[GrokSettings]("grok"); ok {
		return snapshot
	}
	return grokSettings
}
