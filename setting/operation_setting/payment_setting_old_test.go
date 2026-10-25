package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestUpdatePayMethodsByJsonStringKeepsExistingDataOnParseError 确认解析失败
// 不会把 PayMethods 换成空切片：支付方式配置一旦被坏 JSON 清空，前端收银台
// 会在接口报错的同时失去全部支付入口。
func TestUpdatePayMethodsByJsonStringKeepsExistingDataOnParseError(t *testing.T) {
	before := PayMethods2JsonString()

	err := UpdatePayMethodsByJsonString(`[{"name":"x",`)
	require.Error(t, err)
	require.Equal(t, before, PayMethods2JsonString(), "a failed parse must not touch PayMethods")
}
