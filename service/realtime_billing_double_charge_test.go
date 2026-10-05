package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// realtime 计费会经过三个独立阶段：controller 的通用预扣（建立 BillingSession）、
// 每个 turn 的 PreWssConsumeQuota 直扣、以及收尾的 PostWssConsumeQuota 结算。
// 只有当逐 turn 直扣与终态结算不会互相抵消时，总扣款才等于实际用量。
// 直扣走 PostConsumeQuota（绕开 Billing），结算走 BillingSession.Settle
// （按 delta = 实际 - 预扣 增量补扣），两者都写同一个钱包列，因此是两次真实扣款。

const realtimeDoubleChargeModel = "realtime-double-charge-probe"

// 固定倍率，让额度数字可手算：modelRatio=1、completionRatio=1、groupRatio=1。
// 一个纯文本 turn：输入 1000 + 输出 500 => 1500。
const realtimeTurnQuota = 1500

func setupRealtimeBillingProbe(t *testing.T) {
	t.Helper()
	truncate(t)
	gin.SetMode(gin.TestMode)

	// commonKeyCol 由 model.initCol() 懒初始化；service 包的 TestMain 建库时
	// 未经过该路径，若不触发，GetTokenByKey 会生成 "WHERE  = ?" 的语法错误。
	model.KeyColumn()

	// 只声明本探针用到的键，避免依赖全局默认表的加载时机。
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"`+realtimeDoubleChargeModel+`":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"`+realtimeDoubleChargeModel+`":1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
}

func newRealtimeProbeContext() (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/v1/realtime", nil)
	return c, w
}

// tokenKey 必须与 tokens.key 一致（生产里 middleware 存的是裸 key，不含 sk- 前缀），
// 因为 PreConsumeTokenQuota 直接按该值查库、不做前缀裁剪。
func newRealtimeProbeInfo(userId, tokenId int, tokenKey string) *relaycommon.RelayInfo {
	info := &relaycommon.RelayInfo{
		UserId:          userId,
		TokenId:         tokenId,
		TokenKey:        tokenKey,
		OriginModelName: realtimeDoubleChargeModel,
		UserGroup:       "default",
		UsingGroup:      "default",
		StartTime:       time.Now(),
	}
	// ChannelMeta 以指针内嵌，必须初始化，UpstreamModelName 才可读。
	info.ChannelMeta = &relaycommon.ChannelMeta{UpstreamModelName: realtimeDoubleChargeModel}
	return info
}

func readUserQuota(t *testing.T, id int) int {
	t.Helper()
	var quota int
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", id).Select("quota").Scan(&quota).Error)
	return quota
}

func readTokenRemainQuota(t *testing.T, id int) int {
	t.Helper()
	var remain int
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", id).Select("remain_quota").Scan(&remain).Error)
	return remain
}

func realtimeTextTurn(total int) *dto.RealtimeUsage {
	return &dto.RealtimeUsage{
		TotalTokens:        total,
		InputTokens:        1000,
		OutputTokens:       500,
		InputTokenDetails:  dto.InputTokenDetails{TextTokens: 1000},
		OutputTokenDetails: dto.OutputTokenDetails{TextTokens: 500},
	}
}

// TestPreWssConsumeQuota_DebitsImmediately 钉住逐 turn 直扣本身：
// 单次 PreWssConsumeQuota 之后，钱包与令牌余额必须真的减少，
// 后续对 BillingSession 的断言才有意义（否则「双扣」可能只是记账虚影）。
func TestPreWssConsumeQuota_DebitsImmediately(t *testing.T) {
	setupRealtimeBillingProbe(t)

	seedUser(t, 1, 1_000_000)
	seedToken(t, 1, 1, "realtime-probe-key", 1_000_000)

	c, _ := newRealtimeProbeContext()
	info := newRealtimeProbeInfo(1, 1, "realtime-probe-key")

	require.NoError(t, PreWssConsumeQuota(c, info, realtimeTextTurn(1500)))

	require.Equal(t, 1_000_000-realtimeTurnQuota, readUserQuota(t, 1),
		"PreWssConsumeQuota 必须真实扣减钱包")
	require.Equal(t, 1_000_000-realtimeTurnQuota, readTokenRemainQuota(t, 1),
		"PreWssConsumeQuota 必须真实扣减令牌额度")
}

// TestRealtimeBilling_NoDoubleChargeAcrossPreConsumeTurnDebitAndSettle 是本文件的核心回归：
// 走完整的四段时序（预扣 -> 逐 turn 直扣 -> 收尾结算），
// 断言总扣款恰好等于实际用量，而不是两倍。
//
// 修复前该断言失败：净扣款 = 预扣 + Σturn直扣 + (实际 - 预扣) = 实际 + Σturn直扣。
func TestRealtimeBilling_NoDoubleChargeAcrossPreConsumeTurnDebitAndSettle(t *testing.T) {
	setupRealtimeBillingProbe(t)

	const userQuota = 1_000_000
	const tokenQuota = 1_000_000
	// 预扣额度取 500，与 real-time 未走 CountToken 时的估算一致。
	const preConsume = 500

	seedUser(t, 2, userQuota)
	seedToken(t, 2, 2, "realtime-probe-key-2", tokenQuota)

	c, _ := newRealtimeProbeContext()
	info := newRealtimeProbeInfo(2, 2, "realtime-probe-key-2")
	info.PriceData.ModelRatio = 1
	info.PriceData.GroupRatioInfo.GroupRatio = 1

	// 阶段 1：controller 通用预扣，建立 BillingSession。
	apiErr := PreConsumeBilling(c, preConsume, info)
	require.Nil(t, apiErr, "预扣必须成功")
	require.NotNil(t, info.Billing, "预扣必须建立 BillingSession")
	require.Equal(t, userQuota-preConsume, readUserQuota(t, 2))

	// 阶段 2：逐 turn 直扣。三个 turn 各 1500，且 sumUsage 同步累加到 4500。
	sumUsage := &dto.RealtimeUsage{}
	for i := 0; i < 3; i++ {
		turn := realtimeTextTurn(1500)
		sumUsage.TotalTokens += turn.TotalTokens
		sumUsage.InputTokens += turn.InputTokens
		sumUsage.OutputTokens += turn.OutputTokens
		sumUsage.InputTokenDetails.TextTokens += turn.InputTokenDetails.TextTokens
		sumUsage.OutputTokenDetails.TextTokens += turn.OutputTokenDetails.TextTokens
		require.NoError(t, PreWssConsumeQuota(c, info, turn))
	}
	const actualQuota = 3 * realtimeTurnQuota // 4500

	// 阶段 3：收尾结算。这是当前唯一会写日志的地方，日志额度即 actualQuota。
	PostWssConsumeQuota(c, info, realtimeDoubleChargeModel, sumUsage, "")

	wantUser := userQuota - actualQuota
	require.Equal(t, wantUser, readUserQuota(t, 2),
		"realtime 全流程总扣款必须等于实际用量 %d，不得叠加逐 turn 直扣", actualQuota)
	require.Equal(t, tokenQuota-actualQuota, readTokenRemainQuota(t, 2),
		"realtime 全流程令牌总扣款必须等于实际用量 %d", actualQuota)
}

// TestRealtimeBilling_TurnDebitAndSettleMustNotBothDebitWallet 单独钉住归因：
// 即使忽略预扣，逐 turn 直扣与终态结算也不得对同一份用量各扣一次。
func TestRealtimeBilling_TurnDebitAndSettleMustNotBothDebitWallet(t *testing.T) {
	setupRealtimeBillingProbe(t)

	const userQuota = 1_000_000
	seedUser(t, 3, userQuota)
	seedToken(t, 3, 3, "realtime-probe-key-3", userQuota)

	c, _ := newRealtimeProbeContext()
	info := newRealtimeProbeInfo(3, 3, "realtime-probe-key-3")
	info.PriceData.ModelRatio = 1
	info.PriceData.GroupRatioInfo.GroupRatio = 1

	// 预扣 0（信任额度旁路），使结算的 delta 恰为实际用量，便于隔离观察。
	require.Nil(t, PreConsumeBilling(c, 0, info))
	require.Equal(t, userQuota, readUserQuota(t, 3), "预扣 0 不得改动余额")

	turn := realtimeTextTurn(1500)
	require.NoError(t, PreWssConsumeQuota(c, info, turn))
	afterTurn := readUserQuota(t, 3)
	require.Equal(t, userQuota-realtimeTurnQuota, afterTurn, "逐 turn 应扣一次")

	PostWssConsumeQuota(c, info, realtimeDoubleChargeModel, turn, "")

	require.Equal(t, userQuota-realtimeTurnQuota, readUserQuota(t, 3),
		"终态结算不得再次扣减已被逐 turn 扣过的同一份用量")
}
