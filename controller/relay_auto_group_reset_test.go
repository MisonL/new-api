package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 自动分组跨组重选时，resetAutoGroupSelectionProgress 必须连同当前分组一起
// 清掉。HandleGroupRatio 只在上下文里已有 auto_group 时才改写
// relayInfo.UsingGroup，残留的旧分组会让下一次选择的新分组倍率永远不生效，
// 结算继续按旧分组的倍率计费。
func TestResetAutoGroupSelectionProgressClearsSelectedGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(ctx, constant.ContextKeyAutoGroup, "group-a")
	common.SetContextKey(ctx, constant.ContextKeyAutoGroupIndex, 1)
	common.SetContextKey(ctx, constant.ContextKeyAutoGroupRetryIndex, 2)

	resetAutoGroupSelectionProgress(ctx)

	_, hasGroup := common.GetContextKey(ctx, constant.ContextKeyAutoGroup)
	require.False(t, hasGroup, "跨组重选必须清掉已选定的分组")
	_, hasIndex := common.GetContextKey(ctx, constant.ContextKeyAutoGroupIndex)
	require.False(t, hasIndex)
	_, hasRetryIndex := common.GetContextKey(ctx, constant.ContextKeyAutoGroupRetryIndex)
	require.False(t, hasRetryIndex)
}
