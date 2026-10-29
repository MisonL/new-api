package relay

import (
	"math"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

// 任务计费的 OtherRatios 来自 EstimateBilling，其中 sora 的 seconds 直接取自
// 客户端请求且没有上限。裸 int(float64(...)) 越界会回绕成 MinInt64：负额度
// 的预扣被 PreConsume 的 amount<=0 直接放行，最终落到退款方向。
func TestApplyOtherRatiosToQuotaSaturatesInsteadOfGoingNegative(t *testing.T) {
	quota := applyOtherRatiosToQuota(1000, map[string]float64{"seconds": 1e17})

	require.Positive(t, quota, "an overflowing task ratio must not produce a negative (refund) quota")
	require.Equal(t, math.MaxInt64, quota)
}

func TestRecalcQuotaFromRatiosSaturatesInsteadOfGoingNegative(t *testing.T) {
	info := &relaycommon.RelayInfo{}
	info.PriceData.Quota = 1000

	quota := recalcQuotaFromRatios(info, map[string]float64{"seconds": 1e17})

	require.Positive(t, quota, "an overflowing recalculated ratio must not produce a negative quota")
	require.Equal(t, math.MaxInt64, quota)
}

// 除数是极小值时同样会越界，必须走同一饱和路径。
func TestRecalcQuotaFromRatiosSaturatesOnTinyDivisor(t *testing.T) {
	info := &relaycommon.RelayInfo{}
	info.PriceData.Quota = 1000
	info.PriceData.OtherRatios = map[string]float64{"seconds": 1e-300}

	quota := recalcQuotaFromRatios(info, map[string]float64{"seconds": 1})

	require.Positive(t, quota)
	require.Equal(t, math.MaxInt64, quota)
}

// 正常倍率下的结果必须保持不变。
func TestApplyOtherRatiosToQuotaKeepsNormalRatios(t *testing.T) {
	quota := applyOtherRatiosToQuota(1000, map[string]float64{"seconds": 4, "size": 1})

	require.Equal(t, 4000, quota)
}
