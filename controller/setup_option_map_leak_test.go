package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// setupSetupControllerTestDB 调用 model.InitOptionMap()，该函数用
// `common.OptionMap = make(...)` 重建进程级配置表且不保存旧值；随后
// PostSetup 还会经 persistDerivedServerAddress 写入 ServerAddress。清理只
// 恢复 system_setting.ServerAddress 与数据库，进程级 OptionMap 便永久留下
// 本用例的 "http://127.0.0.1:13000"（newSetupContext 的请求 Host），后续
// 同包任何读 OptionMap["ServerAddress"] 的用例都会拿到它。
//
// 真实观测：-shuffle 下 TestHandleCustomOAuthCASStartRedirectsToCASLogin 的
// CAS service URL 变成 http://127.0.0.1:13000/oauth/acme-sso?state=...，而
// 不是它自己 httptest server 的地址。
func TestSetupControllerTestDBRestoresOptionMap(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = map[string]string{"ServerAddress": "https://before.example.com"}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
	})

	t.Run("inner", func(t *testing.T) {
		setupSetupControllerTestDB(t)
		// 模拟 PostSetup 落库后的内存侧写入。
		common.OptionMapRWMutex.Lock()
		common.OptionMap["ServerAddress"] = "http://127.0.0.1:13000"
		common.OptionMapRWMutex.Unlock()
	})

	common.OptionMapRWMutex.RLock()
	got := common.OptionMap["ServerAddress"]
	common.OptionMapRWMutex.RUnlock()
	require.Equal(t, "https://before.example.com", got,
		"the helper must restore the process-wide OptionMap it replaced")
}
