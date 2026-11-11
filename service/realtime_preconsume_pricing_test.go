package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

// PreWssConsumeQuota 是 realtime 的逐 turn 直扣，其定价必须与终态结算
// PostWssConsumeQuota 同源：两者都读 relayInfo.PriceData 的冻结快照。
// 若逐 turn 另取实时倍率或忽略按次价格，逐 turn 累加与结算 delta 会互相打架，
// 未配置倍率的模型还会被 37.5 兜底倍率扣费。

// 订阅计费与钱包余额无关：订阅用户钱包为 0 时，逐 turn 直扣不得因钱包检查失败。
func TestPreWssConsumeQuota_SubscriptionBillingIgnoresWalletBalance(t *testing.T) {
	setupRealtimeBillingProbe(t)

	const userId = 11
	seedUser(t, userId, 0)
	seedToken(t, userId, userId, "realtime-probe-key-11", 1_000_000)

	planId := 980011
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id: planId, Title: "realtime-sub", DurationUnit: "month", DurationValue: 1,
		Enabled: true, TotalAmount: 1_000_000, QuotaResetPeriod: model.SubscriptionResetNever,
	}).Error)
	require.NoError(t, model.DB.Create(&model.UserSubscription{
		Id: userId, UserId: userId, PlanId: planId, AmountTotal: 1_000_000,
		Status: "active", StartTime: time.Now().Unix(), EndTime: time.Now().Add(24 * time.Hour).Unix(),
	}).Error)

	c, _ := newRealtimeProbeContext()
	info := newRealtimeProbeInfo(userId, userId, "realtime-probe-key-11")
	info.PriceData.ModelRatio = 1
	info.PriceData.GroupRatioInfo.GroupRatio = 1
	info.UserSetting.BillingPreference = "subscription_only"
	// 生产链路的 RequestId 由中间件填充，订阅预扣以它做幂等键。
	info.RequestId = "realtime-sub-probe-req-1"

	require.Nil(t, PreConsumeBilling(c, 500, info))
	require.NotNil(t, info.Billing)
	require.Equal(t, BillingSourceSubscription, info.BillingSource,
		"探针必须真的走订阅计费，否则钱包检查不会触发")

	require.NoError(t, PreWssConsumeQuota(c, info, realtimeTextTurn(1500)),
		"订阅计费不得因钱包余额为 0 而失败")

	require.Equal(t, int64(500+realtimeTurnQuota), getSubscriptionUsed(t, userId),
		"逐 turn 用量必须累加到订阅")
}

// 按次计费（model_price）模型：逐 turn 必须按价格扣费，而不是按倍率。
func TestPreWssConsumeQuota_UsesFrozenPriceDataForPerPriceModel(t *testing.T) {
	setupRealtimeBillingProbe(t)

	const userId = 12
	seedUser(t, userId, 1_000_000)
	seedToken(t, userId, userId, "realtime-probe-key-12", 1_000_000)

	c, _ := newRealtimeProbeContext()
	info := newRealtimeProbeInfo(userId, userId, "realtime-probe-key-12")
	info.PriceData.UsePrice = true
	info.PriceData.ModelPrice = 0.02
	info.PriceData.GroupRatioInfo.GroupRatio = 1

	require.NoError(t, PreWssConsumeQuota(c, info, realtimeTextTurn(1500)))

	wantQuota := int(0.02 * common.QuotaPerUnit)
	require.Equal(t, 1_000_000-wantQuota, readUserQuota(t, userId),
		"按次价格模型必须按 model_price 扣费，而不是回落成倍率计费")
}

// 自动分组：逐 turn 直扣必须使用请求开始时冻结的分组倍率。终态结算
// PostWssConsumeQuota 读的是 relayInfo.PriceData.GroupRatioInfo，若逐 turn
// 另按 ContextKeyAutoGroup 取实时配置，连接期间改倍率就会让逐 turn 的预留
// 上限与结算口径不同源——倍率调高后逐 turn 先按新价拒绝放行，连接被误断。
func TestPreWssConsumeQuota_IgnoresLiveAutoGroupRatio(t *testing.T) {
	setupRealtimeBillingProbe(t)

	const userId = 15
	seedUser(t, userId, 1_000_000)
	seedToken(t, userId, userId, "realtime-probe-key-15", 1_000_000)

	// 实时配置把 auto-live 的倍率改成 3，冻结快照仍是 1。
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"auto-live":3}`))

	c, _ := newRealtimeProbeContext()
	common.SetContextKey(c, constant.ContextKeyAutoGroup, "auto-live")
	info := newRealtimeProbeInfo(userId, userId, "realtime-probe-key-15")
	info.PriceData.ModelRatio = 2
	info.PriceData.GroupRatioInfo.GroupRatio = 1

	require.NoError(t, PreWssConsumeQuota(c, info, realtimeTextTurn(1500)))

	require.Equal(t, 1_000_000-1500*2, readUserQuota(t, userId),
		"逐 turn 直扣必须用冻结的 GroupRatioInfo，而不是实时 GetGroupRatio")
}

// 未配置倍率的模型：必须用定价快照里冻结的倍率，不能拿 GetModelRatio 的 37.5 兜底。
func TestPreWssConsumeQuota_UsesFrozenModelRatioNotFallback(t *testing.T) {
	setupRealtimeBillingProbe(t)

	const userId = 13
	seedUser(t, userId, 1_000_000)
	seedToken(t, userId, userId, "realtime-probe-key-13", 1_000_000)

	c, _ := newRealtimeProbeContext()
	info := newRealtimeProbeInfo(userId, userId, "realtime-probe-key-13")
	info.OriginModelName = "realtime-unpriced-probe-model"
	info.PriceData.ModelRatio = 2
	info.PriceData.GroupRatioInfo.GroupRatio = 1

	require.NoError(t, PreWssConsumeQuota(c, info, realtimeTextTurn(1500)))

	const wantQuota = 1500 * 2
	require.Equal(t, 1_000_000-wantQuota, readUserQuota(t, userId),
		"逐 turn 直扣必须使用冻结倍率，不得回落到 37.5 兜底")
}

// 终态结算必须用定价快照里冻结的补全倍率。连接期间管理员改倍率时，
// 若这里回查实时配置，同一请求的逐 turn 直扣与终态 delta 会按两套价格计算。
func TestPostWssConsumeQuota_UsesFrozenCompletionRatio(t *testing.T) {
	setupRealtimeBillingProbe(t)

	const userId = 16
	const initial = 1_000_000
	seedUser(t, userId, initial)
	seedToken(t, userId, userId, "realtime-probe-key-16", initial)

	// 实时配置把补全倍率改成 4；请求开始时冻结的是 1。
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(
		`{"`+realtimeDoubleChargeModel+`":4}`))

	c, _ := newRealtimeProbeContext()
	info := newRealtimeProbeInfo(userId, userId, "realtime-probe-key-16")
	info.PriceData.ModelRatio = 1
	info.PriceData.CompletionRatio = 1
	info.PriceData.GroupRatioInfo.GroupRatio = 1

	require.Nil(t, PreConsumeBilling(c, 0, info))
	PostWssConsumeQuota(c, info, info.OriginModelName, realtimeTextTurn(1500), "")

	// 冻结口径：输入 1000 + 输出 500 x 1 = 1500。
	require.Equal(t, initial-1500, readUserQuota(t, userId),
		"终态结算必须用冻结的 CompletionRatio，而不是实时配置")
}
