package system_setting

import "github.com/QuantumNous/new-api/setting/config"

type DiscordSettings struct {
	Enabled      bool   `json:"enabled"`
	ClientId     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// 默认配置
var defaultDiscordSettings = DiscordSettings{}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("discord", &defaultDiscordSettings)
}

// GetDiscordSettings 指针 getter 只保留给测试播种配置；生产代码读
// DiscordSettingsSnapshot，避免与写锁内的逐字段重写构成数据竞争。
func GetDiscordSettings() *DiscordSettings {
	return &defaultDiscordSettings
}

// DiscordSettingsSnapshot 返回当前 Discord OAuth 配置的浅拷贝。
func DiscordSettingsSnapshot() DiscordSettings {
	if snapshot, ok := config.SnapshotOf[DiscordSettings]("discord"); ok {
		return snapshot
	}
	return defaultDiscordSettings
}
