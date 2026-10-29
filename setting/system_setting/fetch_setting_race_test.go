package system_setting

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/require"
)

// SSRF 校验在每个出站请求上读取抓取设置（含多个 slice 字段），管理端保存
// 设置走 ConfigManager.Update。请求路径必须取锁内快照，否则与写入构成数据
// 竞争，被替换的 slice 头可能读到撕裂长度。
func TestFetchSettingSnapshotDoesNotRaceWithUpdate(t *testing.T) {
	previous, err := config.ConfigToMap(&defaultFetchSetting)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.Update("fetch_setting", previous))
	})

	var wg sync.WaitGroup
	wg.Add(2)
	// 交替写入等长/更短的列表，使 json 解码复用同一底层数组；快照持有者
	// 在锁外遍历旧切片时，必须不会与原地覆盖元素构成竞争。
	domainLists := []string{
		`["example.com","*.example.org","probe.example.net","probe.example.edu"]`,
		`["example.com","*.example.org"]`,
	}
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			snapshot := GetFetchSettingSnapshot()
			for _, domain := range snapshot.DomainList {
				_ = domain
			}
			for _, port := range snapshot.AllowedPorts {
				_ = port
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 300; i++ {
			_ = config.GlobalConfig.Update("fetch_setting", map[string]string{
				"domain_list":   domainLists[i%len(domainLists)],
				"ip_list":       `["10.0.0.0/8","192.168.0.0/16"]`,
				"allowed_ports": `["80","443","8080"]`,
			})
		}
	}()
	wg.Wait()
}

func TestFetchSettingSnapshotSeesUpdatedLists(t *testing.T) {
	previous, err := config.ConfigToMap(&defaultFetchSetting)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.Update("fetch_setting", previous))
	})

	require.NoError(t, config.GlobalConfig.Update("fetch_setting", map[string]string{
		"domain_list": `["snapshot-probe.example"]`,
	}))

	snapshot := GetFetchSettingSnapshot()
	require.Equal(t, []string{"snapshot-probe.example"}, snapshot.DomainList)
}
