package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 任务提交时用 ModelPriceHelperPerCall 生成定价快照，终态按 token 重算读的
// 就是这份快照。补全倍率必须在提交时冻结：任务可能运行数分钟到数小时，
// 轮询期再查配置会让同一笔任务的预扣与结算不同源。
func TestModelPriceHelperPerCallFreezesCompletionRatio(t *testing.T) {
	gin.SetMode(gin.TestMode)

	prevModel := ratio_setting.GetModelRatioCopy()
	prevCompletion := ratio_setting.GetCompletionRatioCopy()
	prevGroup := ratio_setting.GetGroupRatioCopy()
	t.Cleanup(func() {
		restore := func(name string, m map[string]float64, apply func(string) error) {
			payload, err := common.Marshal(m)
			require.NoError(t, err)
			require.NoError(t, apply(string(payload)), name)
		}
		restore("model_ratio", prevModel, ratio_setting.UpdateModelRatioByJSONString)
		restore("completion_ratio", prevCompletion, ratio_setting.UpdateCompletionRatioByJSONString)
		restore("group_ratio", prevGroup, ratio_setting.UpdateGroupRatioByJSONString)
	})

	const modelName = "task-percall-freeze-probe"
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"`+modelName+`":1.5}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"`+modelName+`":3.5}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName: modelName,
		UserGroup:       "default",
		UsingGroup:      "default",
	}

	priceData, err := ModelPriceHelperPerCall(ctx, info)
	require.NoError(t, err)
	require.False(t, priceData.UsePrice, "未配置按次价格时应走倍率计费")

	// 冻结后改配置不得影响已生成的快照。
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"`+modelName+`":9}`))
	require.Equal(t, 3.5, priceData.CompletionRatio,
		"按量计费的定价快照必须冻结补全倍率，供终态重算使用")
}
