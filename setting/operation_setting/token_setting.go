package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// TokenSetting 令牌相关配置
type TokenSetting struct {
	MaxUserTokens int `json:"max_user_tokens"` // 每用户最大令牌数量
}

// 默认配置
var tokenSetting = TokenSetting{
	MaxUserTokens: 1000, // 默认每用户最多 1000 个令牌
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("token_setting", &tokenSetting)
}

// GetTokenSetting 指针 getter 只保留给测试播种配置；生产代码读
// GetMaxUserTokens，它走锁内快照，避免与写锁内的逐字段重写构成数据竞争。
func GetTokenSetting() *TokenSetting {
	return &tokenSetting
}

// GetMaxUserTokens 获取每用户最大令牌数量
func GetMaxUserTokens() int {
	if snapshot, ok := config.SnapshotOf[TokenSetting]("token_setting"); ok {
		return snapshot.MaxUserTokens
	}
	return tokenSetting.MaxUserTokens
}
