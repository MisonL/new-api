package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 部分早退分支（不支持的渠道类型、用户缓存读取失败）返回的 testResult 不带
// context。testAllChannels 的启用/禁用分支若直接解引用它，会 panic 并中断
// 整个巡检循环：后续渠道不再被测试，自动巡检开启时每个周期都在同一处中断。
func TestChannelKeyFromTestResultHandlesMissingContext(t *testing.T) {
	require.NotPanics(t, func() {
		key := channelKeyFromTestResult(testResult{})
		require.Empty(t, key)
	}, "a test result without a gin context must not panic the channel test loop")
}
