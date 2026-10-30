package system_setting

import "github.com/QuantumNous/new-api/setting/config"

type OIDCSettings struct {
	Enabled               bool   `json:"enabled"`
	ClientId              string `json:"client_id"`
	ClientSecret          string `json:"client_secret"`
	WellKnown             string `json:"well_known"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserInfoEndpoint      string `json:"user_info_endpoint"`
}

// 默认配置
var defaultOIDCSettings = OIDCSettings{}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("oidc", &defaultOIDCSettings)
}

// GetOIDCSettings 指针 getter 只保留给测试播种配置；生产代码读
// OIDCSettingsSnapshot，避免与写锁内的逐字段重写构成数据竞争。
func GetOIDCSettings() *OIDCSettings {
	return &defaultOIDCSettings
}

// OIDCSettingsSnapshot 返回当前 OIDC 配置的浅拷贝。
func OIDCSettingsSnapshot() OIDCSettings {
	if snapshot, ok := config.SnapshotOf[OIDCSettings]("oidc"); ok {
		return snapshot
	}
	return defaultOIDCSettings
}
