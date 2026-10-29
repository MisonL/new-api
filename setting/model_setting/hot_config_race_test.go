package model_setting

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/require"
)

// 管理端保存 Gemini/Qwen 设置走 ConfigManager.Update，在写锁内重写已注册的
// 结构体。请求热路径读取 map/slice 字段必须在读锁内取快照，否则与写入构成
// 数据竞争，map 并发读写还会直接触发 runtime 致命错误。
func TestGeminiAccessorsDoNotRaceWithUpdate(t *testing.T) {
	previous, err := config.ConfigToMap(&geminiSettings)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.Update("gemini", previous))
	})

	var wg sync.WaitGroup
	wg.Add(2)
	// 交替写入等长/更短的列表，使 json 解码复用同一底层数组；快照持有者
	// 在锁外遍历旧切片时，必须不会与原地覆盖元素构成竞争。
	imagineLists := []string{
		`["gemini-2.0-flash-exp","probe-image-model","probe-image-model-2","probe-image-model-3"]`,
		`["gemini-2.0-flash-exp","probe-image-model"]`,
	}
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			_ = GetGeminiSafetySetting("probe-model")
			_ = GetGeminiVersionSetting("probe-model")
			_ = IsGeminiModelSupportImagine("gemini-2.0-flash-exp")
			snapshot := GeminiSettingsSnapshot()
			for _, name := range snapshot.SupportedImagineModels {
				_ = name
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 300; i++ {
			_ = config.GlobalConfig.Update("gemini", map[string]string{
				"safety_settings":          `{"default":"BLOCK_NONE","probe-model":"OFF"}`,
				"version_settings":         `{"default":"v1beta","probe-model":"v1"}`,
				"supported_imagine_models": imagineLists[i%len(imagineLists)],
			})
		}
	}()
	wg.Wait()
}

// 快照访问器必须看到 Update 之后的新值，修复不能靠读陈旧副本。
func TestGeminiAccessorSeesUpdatedSettings(t *testing.T) {
	previous, err := config.ConfigToMap(&geminiSettings)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.Update("gemini", previous))
	})

	require.NoError(t, config.GlobalConfig.Update("gemini", map[string]string{
		"safety_settings":          `{"default":"OFF","snapshot-probe":"BLOCK_NONE"}`,
		"supported_imagine_models": `["snapshot-probe-image"]`,
	}))

	require.Equal(t, "BLOCK_NONE", GetGeminiSafetySetting("snapshot-probe"))
	snapshot := GeminiSettingsSnapshot()
	require.Equal(t, "BLOCK_NONE", snapshot.SafetySettings["snapshot-probe"])
	require.True(t, IsGeminiModelSupportImagine("snapshot-probe-image"))
	require.False(t, IsGeminiModelSupportImagine("gemini-2.0-flash-exp"),
		"the previous imagine list entries must be gone after replacement")
}

func TestQwenAccessorDoesNotRaceWithUpdate(t *testing.T) {
	previous, err := config.ConfigToMap(&qwenSettings)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.Update("qwen", previous))
	})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			_ = IsSyncImageModel("qwen-image-edit")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 300; i++ {
			_ = config.GlobalConfig.Update("qwen", map[string]string{
				"sync_image_models": `["z-image","qwen-image","probe-sync-model"]`,
			})
		}
	}()
	wg.Wait()

	require.NoError(t, config.GlobalConfig.Update("qwen", map[string]string{
		"sync_image_models": `["snapshot-only-model"]`,
	}))
	require.True(t, IsSyncImageModel("snapshot-only-model"))
	require.False(t, IsSyncImageModel("qwen-image-edit"),
		"the previous sync list entries must be gone after replacement")
}
