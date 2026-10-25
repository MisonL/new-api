package config

import (
	"strconv"
	"sync"
	"testing"
)

type lockProbeConfig struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func newLockProbeManager() *ConfigManager {
	cm := NewConfigManager()
	cm.Register("probe", &lockProbeConfig{Name: "initial", Count: 0})
	return cm
}

// TestConfigManagerUpdateIsRaceFreeWithReaders 覆盖分层配置写入与读取的并发。
//
// 生产写路径（model/option.go handleConfigUpdate、SyncOptions 每 60 秒重载）用
// 反射逐字段改写注册结构体；读取路径包括快照克隆（Read）和热路径上的直接字段
// 读取（ReadLive）。三者必须串行化在同一模块锁上，否则 -race 报
// reflect.Value.Set 与 cloneConfigValue 并发写读。
func TestConfigManagerUpdateIsRaceFreeWithReaders(t *testing.T) {
	cm := newLockProbeManager()

	const iterations = 300
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if err := cm.Update("probe", map[string]string{
				"name":  "n" + strconv.Itoa(i),
				"count": strconv.Itoa(i),
			}); err != nil {
				t.Errorf("Update returned error: %v", err)
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			cm.Read("probe", func(cfg interface{}) {
				if probe, ok := cfg.(*lockProbeConfig); ok && probe != nil {
					_ = probe.Name
					_ = probe.Count
				}
			})
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			cm.ReadLive("probe", func(cfg interface{}) {
				if probe, ok := cfg.(*lockProbeConfig); ok && probe != nil {
					_ = probe.Name
					_ = probe.Count
				}
			})
		}
	}()

	wg.Wait()
}

func TestConfigManagerUpdateRejectsUnknownModule(t *testing.T) {
	cm := newLockProbeManager()

	err := cm.Update("missing", map[string]string{"name": "x"})
	if err == nil {
		t.Fatal("Update must fail for an unregistered module")
	}
}

func TestConfigManagerUpdateIsVisibleToReaders(t *testing.T) {
	cm := newLockProbeManager()

	if err := cm.Update("probe", map[string]string{"name": "written", "count": "7"}); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	cm.Read("probe", func(cfg interface{}) {
		probe, ok := cfg.(*lockProbeConfig)
		if !ok {
			t.Fatalf("Read delivered %T", cfg)
		}
		if probe.Name != "written" || probe.Count != 7 {
			t.Fatalf("clone did not observe the update, got %#v", probe)
		}
	})

	cm.ReadLive("probe", func(cfg interface{}) {
		probe, ok := cfg.(*lockProbeConfig)
		if !ok {
			t.Fatalf("ReadLive delivered %T", cfg)
		}
		if probe.Name != "written" || probe.Count != 7 {
			t.Fatalf("live value did not observe the update, got %#v", probe)
		}
	})
}
