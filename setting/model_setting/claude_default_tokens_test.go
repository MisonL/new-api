package model_setting

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
)

// The accessor must be safe to call from many goroutines while a config save
// replaces DefaultMaxTokens. It used to repair the map in place, which the
// runtime turns into "fatal error: concurrent map writes" -- a process-level
// abort that gin.CustomRecovery cannot catch.
func TestGetClaudeSettingsConcurrentAccessIsRaceFree(t *testing.T) {
	// A saved config whose JSON has no "default" key.
	config.UpdateConfigFromMap(&claudeSettings, map[string]string{
		"default_max_tokens": `{"claude-3-5-sonnet": 4096}`,
	})

	const readers = 32
	var ready, goFlag int64
	var wg sync.WaitGroup
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			atomic.AddInt64(&ready, 1)
			for atomic.LoadInt64(&goFlag) == 0 {
			}
			// Every goroutine must both read the accessor and resolve a
			// fallback, which is where the old write happened.
			_ = GetClaudeSettings().GetDefaultMaxTokens("model-not-configured")
		}()
	}
	for atomic.LoadInt64(&ready) < readers {
	}
	atomic.StoreInt64(&goFlag, 1)
	wg.Wait()
}

// A saved config without a "default" key must still yield the documented
// fallback rather than 0 (which would make max_tokens invalid upstream).
func TestDefaultMaxTokensFallsBackWhenDefaultKeyIsAbsent(t *testing.T) {
	previous := claudeSettings.DefaultMaxTokens
	t.Cleanup(func() { claudeSettings.DefaultMaxTokens = previous })

	claudeSettings.DefaultMaxTokens = map[string]int{"claude-3-5-sonnet": 4096}

	if got := GetClaudeSettings().GetDefaultMaxTokens("unknown-model"); got != ClaudeDefaultMaxTokens {
		t.Fatalf("fallback = %d, want %d", got, ClaudeDefaultMaxTokens)
	}
	if got := GetClaudeSettings().GetDefaultMaxTokens("claude-3-5-sonnet"); got != 4096 {
		t.Fatalf("model-specific value = %d, want 4096", got)
	}

	// An explicit "default" entry still wins over the constant.
	claudeSettings.DefaultMaxTokens = map[string]int{"default": 1234}
	if got := GetClaudeSettings().GetDefaultMaxTokens("unknown-model"); got != 1234 {
		t.Fatalf("explicit default = %d, want 1234", got)
	}
}
