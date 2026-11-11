package service

import (
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

// 按次定价（model_price）模型的终态结算必须按 model_price 收费。
// 修复前 PostWssConsumeQuota / PostAudioConsumeQuota 构造 QuotaInfo 时漏传
// ModelPrice，calculateAudioQuota 的按次分支用 0 价格算出 0，SettleBilling(0)
// 再把整笔预扣当作"多扣"退回，账面净扣为 0。

func TestPostWssConsumeQuota_ChargesPerPriceModel(t *testing.T) {
	setupRealtimeBillingProbe(t)

	const userId = 21
	const initial = 1_000_000
	const modelPrice = 0.02
	preConsume := int(modelPrice * common.QuotaPerUnit)

	seedUser(t, userId, initial)
	seedToken(t, userId, userId, "realtime-percall-key-21", initial)

	c, _ := newRealtimeProbeContext()
	info := newRealtimeProbeInfo(userId, userId, "realtime-percall-key-21")
	// 与 ModelPriceHelperPerCall 同形：按次计费只填 UsePrice/ModelPrice，
	// 不冻结完成/音频倍率，RatiosFrozen 保持 false。
	info.PriceData.UsePrice = true
	info.PriceData.ModelPrice = modelPrice
	info.PriceData.RatiosFrozen = false

	require.Nil(t, PreConsumeBilling(c, preConsume, info))
	require.Equal(t, initial-preConsume, readUserQuota(t, userId))

	PostWssConsumeQuota(c, info, info.OriginModelName, realtimeTextTurn(1500), "")

	require.Equal(t, initial-preConsume, readUserQuota(t, userId),
		"按次定价的 realtime 结算必须保留按次价格，不得归零退款")
}

func TestPostAudioConsumeQuota_ChargesPerPriceModel(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	model.KeyColumn()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))

	const userId = 22
	const initial = 1_000_000
	const modelPrice = 0.3
	const audioModel = "audio-percall-probe"
	preConsume := int(modelPrice * common.QuotaPerUnit)

	seedUser(t, userId, initial)
	seedToken(t, userId, userId, "audio-percall-key-22", initial)

	c, _ := newRealtimeProbeContext()
	info := &relaycommon.RelayInfo{
		UserId:          userId,
		TokenId:         userId,
		TokenKey:        "audio-percall-key-22",
		OriginModelName: audioModel,
		UserGroup:       "default",
		UsingGroup:      "default",
		StartTime:       time.Now(),
	}
	info.ChannelMeta = &relaycommon.ChannelMeta{UpstreamModelName: audioModel}
	// 与 ModelPriceHelperPerCall 同形：按次计费只填 UsePrice/ModelPrice/分组倍率，
	// 不冻结完成/音频倍率。
	info.PriceData.UsePrice = true
	info.PriceData.ModelPrice = modelPrice
	info.PriceData.GroupRatioInfo.GroupRatio = 1

	require.Nil(t, PreConsumeBilling(c, preConsume, info))

	usage := &dto.Usage{
		PromptTokens:           10,
		CompletionTokens:       100,
		TotalTokens:            110,
		PromptTokensDetails:    dto.InputTokenDetails{TextTokens: 10},
		CompletionTokenDetails: dto.OutputTokenDetails{AudioTokens: 100},
	}
	PostAudioConsumeQuota(c, info, usage, "")

	require.Equal(t, initial-preConsume, readUserQuota(t, userId),
		"按次定价的音频结算必须保留按次价格，不得归零退款")
}
