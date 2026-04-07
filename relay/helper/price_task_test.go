package helper

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelPriceHelperPerCall_UsesConfiguredPrice(t *testing.T) {
	ratio_setting.InitRatioSettings()
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"test-task-price":0.4}`))

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		OriginModelName: "test-task-price",
		UsingGroup:      "default",
	}

	priceData, err := ModelPriceHelperPerCall(c, info)
	require.NoError(t, err)

	assert.True(t, priceData.UsePrice)
	assert.Equal(t, 0.4, priceData.ModelPrice)
	assert.Equal(t, int(0.4*common.QuotaPerUnit), priceData.Quota)
	assert.Equal(t, 0.0, priceData.ModelRatio)
}

func TestModelPriceHelperPerCall_UsesModelRatioWhenPriceMissing(t *testing.T) {
	ratio_setting.InitRatioSettings()
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"test-task-ratio":0.3}`))

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		OriginModelName: "test-task-ratio",
		UsingGroup:      "default",
	}

	priceData, err := ModelPriceHelperPerCall(c, info)
	require.NoError(t, err)

	assert.False(t, priceData.UsePrice)
	assert.Equal(t, -1.0, priceData.ModelPrice)
	assert.Equal(t, 0.3, priceData.ModelRatio)
	assert.Equal(t, int(0.3/2*common.QuotaPerUnit), priceData.Quota)
}
