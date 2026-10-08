package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
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

// TestRealtimeBilling_TieredSettleChargesAudioSubcategory 是 §3.1 的回归：
// realtime 收尾结算在 tiered_expr 模式下必须把 audio/cached 细分传给表达式，
// 而不是只传总量。表达式 ai*12 + ao*50 若拿到 AI=0/AO=0 会少收音频费用。
// 语料：输入 1000（其中音频 900、缓存 100）+ 输出 200（其中音频 150），
// 表达式 tier("base", p * 1 + c * 1 + cr * 0.5 + ai * 12 + ao * 50)。
// P = 1000-900-100 = 0；C = 200-150 = 50。
// cost = 0*1 + 50*1 + 100*0.5 + 900*12 + 150*50 = 18400
// quota = 18400 / 1e6 * QuotaPerUnit(500000) = 9200。
func TestRealtimeBilling_TieredSettleChargesAudioSubcategory(t *testing.T) {
	setupRealtimeBillingProbe(t)

	const userQuota = 1_000_000
	seedUser(t, 5, userQuota)
	seedToken(t, 5, 5, "realtime-probe-key-5", userQuota)

	c, _ := newRealtimeProbeContext()
	info := newRealtimeProbeInfo(5, 5, "realtime-probe-key-5")
	info.PriceData.ModelRatio = 1
	info.PriceData.GroupRatioInfo.GroupRatio = 1

	// tiered 快照：与生产预扣路径同构（makeRelayInfo 会算 EstimatedQuotaAfterGroup）。
	const expr = `tier("base", p * 1 + c * 1 + cr * 0.5 + ai * 12 + ao * 50)`
	snapInfo := makeRelayInfo(expr, 1.0, 0, 0)
	info.TieredBillingSnapshot = snapInfo.TieredBillingSnapshot
	info.FinalPreConsumedQuota = snapInfo.FinalPreConsumedQuota

	// 预扣 0（信任额度旁路），使结算 delta 恰为实际用量。
	require.Nil(t, PreConsumeBilling(c, 0, info))

	usage := &dto.RealtimeUsage{
		TotalTokens:  1200,
		InputTokens:  1000,
		OutputTokens: 200,
		InputTokenDetails: dto.InputTokenDetails{
			CachedTokens: 100,
			TextTokens:   0,
			AudioTokens:  900,
		},
		OutputTokenDetails: dto.OutputTokenDetails{
			TextTokens:  50,
			AudioTokens: 150,
		},
	}

	PostWssConsumeQuota(c, info, realtimeDoubleChargeModel, usage, "")

	const wantQuota = 9200
	require.Equal(t, userQuota-wantQuota, readUserQuota(t, 5),
		"tiered 结算必须按细分变量计费（音频 900*12 + 150*50 等），而不是只按总量")
	require.Equal(t, userQuota-wantQuota, readTokenRemainQuota(t, 5),
		"令牌余额必须与钱包一致")
}

// TestRealtimeBilling_TrustedUserStillChargedOnce 钉住信任额度旁路分支。
// trusted 时 Reserve 直接返回 nil（不预扣），收尾 Settle 必须按全额扣一次；
// 若修复让 trusted 用户变成零扣款，本用例失败。
func TestRealtimeBilling_TrustedUserStillChargedOnce(t *testing.T) {
	setupRealtimeBillingProbe(t)

	// common.GetTrustQuota() = 10 * QuotaPerUnit，是固定值，无 setter。
	// 用户与令牌额度都必须高于它，shouldTrust 才为真。
	trustQuota := common.GetTrustQuota()
	userQuota := trustQuota * 2

	seedUser(t, 4, userQuota)
	seedToken(t, 4, 4, "realtime-probe-key-4", userQuota)

	c, _ := newRealtimeProbeContext()
	c.Set("token_quota", userQuota)

	info := newRealtimeProbeInfo(4, 4, "realtime-probe-key-4")
	info.PriceData.ModelRatio = 1
	info.PriceData.GroupRatioInfo.GroupRatio = 1

	require.Nil(t, PreConsumeBilling(c, 500, info))
	require.Equal(t, userQuota, readUserQuota(t, 4),
		"信任用户预扣 0，余额不应变化（必须真的走 trusted 分支）")

	turn := realtimeTextTurn(1500)
	require.NoError(t, PreWssConsumeQuota(c, info, turn))

	PostWssConsumeQuota(c, info, realtimeDoubleChargeModel, turn, "")

	require.Equal(t, userQuota-realtimeTurnQuota, readUserQuota(t, 4),
		"信任用户必须被按实际用量扣费一次，不得零扣也不得双扣")
}
