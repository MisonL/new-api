package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

type QuotaSetting struct {
	EnableFreeModelPreConsume bool `json:"enable_free_model_pre_consume"` // 是否对免费模型启用预消耗
}

// 默认配置
var quotaSetting = QuotaSetting{
	EnableFreeModelPreConsume: true,
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("quota_setting", &quotaSetting)
}

// GetQuotaSetting 指针 getter 只保留给测试播种配置；生产代码读
// FreeModelPreConsumeEnabled，避免与写锁内的逐字段重写构成数据竞争。
func GetQuotaSetting() *QuotaSetting {
	return &quotaSetting
}

// FreeModelPreConsumeEnabled 返回是否对免费模型启用预消耗。该读取落在中继
// 计价热路径上，必须走锁内快照。
func FreeModelPreConsumeEnabled() bool {
	if snapshot, ok := config.SnapshotOf[QuotaSetting]("quota_setting"); ok {
		return snapshot.EnableFreeModelPreConsume
	}
	return quotaSetting.EnableFreeModelPreConsume
}
