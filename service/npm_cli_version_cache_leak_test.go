package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// loadRecordedNpmCLIVersionOptions 只在 OptionMap 里的键**存在**时同步。
// 键被删除后函数立刻 return，进程级 npmCliVersionCache 里那些条目永远
// 留着，于是同一个进程里只要有一个用例播种过版本记录下来，后续任何
// 解析 CLI 请求头模板的地方都会拿到那份残留版本，而不是 "latest"。
//
// 真实观测：controller 包的 TestBuildFetchModelsHeadersAppliesHeaderProfileStrategy
// 在 -shuffle 下会被别的用例播种的 1.2.3 覆盖，UA 从
// "codex-tui/0.142.4 (...)" 变成 "codex-tui/1.2.3 (...)" 而失败。
func TestLoadRecordedNpmCLIVersionOptionsDropsEntriesWhenOptionKeyCleared(t *testing.T) {
	resetNpmCLIVersionCacheForTest()
	t.Cleanup(resetNpmCLIVersionCacheForTest)

	previousOptionMap := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() { common.OptionMap = previousOptionMap })

	recorded := map[string]any{
		"packages": map[string]any{
			"@openai/codex": map[string]any{
				"fetched_at":     time.Unix(1000, 0).UTC(),
				"latest_version": "1.2.3",
				"options": []map[string]any{
					{
						"value":            "latest",
						"label":            "latest (1.2.3)",
						"is_latest":        true,
						"resolved_version": "1.2.3",
					},
				},
			},
		},
	}
	payload, err := common.Marshal(recorded)
	require.NoError(t, err)

	common.OptionMap[npmCliVersionRecordedOptionsKey] = string(payload)
	loadRecordedNpmCLIVersionOptions()

	entry, ok := getCachedNpmCLIVersionOptionsEntry("@openai/codex")
	require.True(t, ok, "seeding the option must populate the cache")
	require.Equal(t, "1.2.3", entry.latestVersion)

	// 模拟测试清理：键被移除。
	delete(common.OptionMap, npmCliVersionRecordedOptionsKey)
	loadRecordedNpmCLIVersionOptions()

	entry, ok = getCachedNpmCLIVersionOptionsEntry("@openai/codex")
	require.False(t, ok, "clearing the option must clear the processed cache entry")
	require.Empty(t, entry.latestVersion)
}
