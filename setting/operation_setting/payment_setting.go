package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

type PaymentSetting struct {
	AmountOptions  []int           `json:"amount_options"`
	AmountDiscount map[int]float64 `json:"amount_discount"` // 充值金额对应的折扣，例如 100 元 0.9 表示 100 元充值享受 9 折优惠
}

// 默认配置
var paymentSetting = PaymentSetting{
	AmountOptions:  []int{10, 20, 50, 100, 200, 500},
	AmountDiscount: map[int]float64{},
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("payment_setting", &paymentSetting)
}

// GetPaymentSetting 指针 getter 只保留给测试播种配置；生产代码读
// PaymentSettingSnapshot，避免与写锁内的逐字段重写构成数据竞争。
func GetPaymentSetting() *PaymentSetting {
	return &paymentSetting
}

// PaymentSettingSnapshot 返回当前支付配置的浅拷贝。slice/map 字段在写入时
// 整体替换，旧对象不再被改写，浅拷贝足以让调用方在锁外安全读取。
func PaymentSettingSnapshot() PaymentSetting {
	if snapshot, ok := config.SnapshotOf[PaymentSetting]("payment_setting"); ok {
		return snapshot
	}
	return paymentSetting
}
