package system_setting

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

type ThemeSettings struct {
	Frontend string `json:"frontend"`
}

var themeSettings = ThemeSettings{
	Frontend: "classic",
}

func init() {
	config.GlobalConfig.Register("theme", &themeSettings)
	syncThemeToCommon()
}

func syncThemeToCommon() {
	common.SetTheme(ThemeSettingsSnapshot().Frontend)
}

// GetThemeSettings 指针 getter 只保留给测试播种配置；生产代码读
// ThemeSettingsSnapshot，避免与写锁内的逐字段重写构成数据竞争。
func GetThemeSettings() *ThemeSettings {
	return &themeSettings
}

// ThemeSettingsSnapshot 返回当前主题配置的浅拷贝。
func ThemeSettingsSnapshot() ThemeSettings {
	if snapshot, ok := config.SnapshotOf[ThemeSettings]("theme"); ok {
		return snapshot
	}
	return themeSettings
}

// UpdateAndSyncTheme syncs the theme config to common after DB load.
func UpdateAndSyncTheme() {
	syncThemeToCommon()
}
