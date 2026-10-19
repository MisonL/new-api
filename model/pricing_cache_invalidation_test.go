package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

// 倍率类配置更新后必须让 /api/pricing 的进程内快照立即失效。
//
// pricingMap 有一分钟 TTL，管理员改完 ModelRatio 后，前端定价页在最长一分钟内
// 仍展示旧倍率，而计费早已按新值执行。旧式 updateOptionMap 分支只更新
// ratio_setting 的内存 map，从未调用 InvalidatePricingCache；只有分层配置的
// billing_setting 分支做了失效。
func TestModelRatioOptionUpdateInvalidatesPricingCache(t *testing.T) {
	db := setupPricingEndpointTypesTestDB(t)
	require.NoError(t, db.AutoMigrate(&Option{}))

	require.NoError(t, db.Create(&Channel{
		Id: 7201, Name: "pricing-cache-channel", Key: "sk-a", Type: 1, Status: 1, Group: "default",
	}).Error)
	require.NoError(t, db.Create(&Ability{
		Group: "default", Model: "pricing-cache-model", ChannelId: 7201, Enabled: true,
	}).Error)

	previousOptionMap := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() { common.OptionMap = previousOptionMap })

	ratio_setting.InitRatioSettings()
	t.Cleanup(ratio_setting.InitRatioSettings)
	InvalidatePricingCache()

	require.NoError(t, UpdateOption("ModelRatio", `{"pricing-cache-model":1.5}`))

	// 第一次读取会把 1.5 写入快照。
	require.InDelta(t, 1.5, pricingModelRatio(t, "pricing-cache-model"), 1e-9)

	// 第二次修改必须立刻反映，而不是等 TTL 过期。
	require.NoError(t, UpdateOption("ModelRatio", `{"pricing-cache-model":2.5}`))
	require.InDelta(t, 2.5, pricingModelRatio(t, "pricing-cache-model"), 1e-9,
		"ModelRatio 更新后 /api/pricing 快照必须立即失效")
}

func pricingModelRatio(t *testing.T, modelName string) float64 {
	t.Helper()
	for _, pricing := range GetPricing() {
		if pricing.ModelName == modelName {
			return pricing.ModelRatio
		}
	}
	t.Fatalf("model %q not found in pricing snapshot", modelName)
	return 0
}
