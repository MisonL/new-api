package model_setting

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/require"
)

// 管理端保存设置走 ConfigManager.Update，在写锁内逐字段重写已注册的结构体。
// 请求热路径若直接读包级变量的字段（尤其是 slice/map），与写构成数据竞争：
// slice 头被替换时可读到越界长度。访问器必须在读锁内取快照。
func TestGlobalSettingsAccessorsDoNotRaceWithUpdate(t *testing.T) {
	// 交替写入等长/更短的列表，使 json 解码复用同一底层数组：快照持有者
	// 在锁外遍历旧切片时，必须不会与原地覆盖元素构成竞争。
	lists := []string{
		`["probe-1","probe-2","probe-3","probe-4","probe-5","probe-6","probe-7","probe-8"]`,
		`["probe-1","probe-2"]`,
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			_ = ShouldPreserveThinkingSuffix("probe-model")
			_ = GetRequestBodyLimitPolicy()
			snapshot := GlobalSettingsSnapshot()
			for _, word := range snapshot.ThinkingModelBlacklist {
				_ = word
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 300; i++ {
			_ = config.GlobalConfig.Update("global", map[string]string{
				"thinking_model_blacklist": lists[i%len(lists)],
			})
		}
	}()
	wg.Wait()
}

// 访问器必须看到 Update 之后的新值：修复不能靠把配置读成陈旧快照。
func TestGlobalSettingsAccessorSeesUpdatedBlacklist(t *testing.T) {
	previous := GetGlobalSettings().ThinkingModelBlacklist
	t.Cleanup(func() {
		raw, err := common.Marshal(previous)
		require.NoError(t, err)
		require.NoError(t, config.GlobalConfig.Update("global", map[string]string{
			"thinking_model_blacklist": string(raw),
		}))
	})

	require.NoError(t, config.GlobalConfig.Update("global", map[string]string{
		"thinking_model_blacklist": `["snapshot-probe-model"]`,
	}))

	require.True(t, ShouldPreserveThinkingSuffix("snapshot-probe-model"),
		"the accessor must observe the updated blacklist")
	require.False(t, ShouldPreserveThinkingSuffix("kimi-k2-thinking"),
		"the previous blacklist entries must be gone after replacement")
}
