package helper

import (
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 预扣费换算越界得到 MinInt64 时，预扣会变成"加钱"（负额度即退款方向）。
// 这里走 ModelPriceHelper 的按次价格分支，验证换算入口本身饱和。
func TestPreConsumedQuotaConversionNeverGoesNegative(t *testing.T) {
	ratio_setting.InitRatioSettings()

	const model = "price-overflow-probe"
	previousPrices := ratio_setting.ModelPrice2JSONString()
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"`+model+`":1.18e21}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousPrices))
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set("group", "default")

	info := &relaycommon.RelayInfo{
		OriginModelName: model,
		UserGroup:       "default",
		UsingGroup:      "default",
	}

	priceData, err := ModelPriceHelper(ctx, info, 1, &types.TokenCountMeta{})
	require.NoError(t, err)
	require.True(t, priceData.UsePrice)

	require.Positive(t, priceData.QuotaToPreConsume,
		"an overflowing price must not pre-consume a negative (refund) quota")
	require.Equal(t, math.MaxInt64, priceData.QuotaToPreConsume)
}
