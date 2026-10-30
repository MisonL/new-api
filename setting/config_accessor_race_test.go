package setting

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/console_setting"
	"github.com/QuantumNous/new-api/setting/log_retention_setting"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
	"github.com/QuantumNous/new-api/setting/performance_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

// 管理端保存设置走 ConfigManager.Update，在写锁内按字段重写已注册结构体。
// 这些模块的访问器如果直接读包级变量，就与写入构成数据竞争；字符串字段是
// 两字（指针+长度），撕裂读可能拿到越界指针。
//
// 用例把每个模块公开的读取访问器与 Update 并发跑，在 -race 下必须无告警。
// 访问器是请求热路径实际调用的入口，因此覆盖它们等价于覆盖生产读取路径。
func TestSettingAccessorsDoNotRaceWithUpdate(t *testing.T) {
	cases := []struct {
		name string
		mod  string
		key  string
		val  string
		read func()
	}{
		{"claude", "claude", "thinking_adapter_enabled", "true", func() {
			snapshot := model_setting.ClaudeSettingsSnapshot()
			_ = snapshot.ThinkingAdapterEnabled
			_ = snapshot.GetDefaultMaxTokens("claude-probe")
		}},
		{"grok", "grok", "violation_deduction_amount", "0.09", func() {
			_ = model_setting.GrokSettingsSnapshot().ViolationDeductionAmount
		}},
		{"legal", "legal", "user_agreement", "probe-agreement", func() {
			_ = system_setting.LegalSettingsSnapshot().UserAgreement
		}},
		{"theme", "theme", "frontend", "default", func() {
			_ = system_setting.ThemeSettingsSnapshot().Frontend
		}},
		{"passkey", "passkey", "rp_id", "probe.example.com", func() {
			_ = system_setting.PasskeySettingsSnapshot().RPID
		}},
		{"discord", "discord", "client_id", "probe-client-id", func() {
			_ = system_setting.DiscordSettingsSnapshot().ClientId
		}},
		{"oidc", "oidc", "client_id", "probe-client-id", func() {
			_ = system_setting.OIDCSettingsSnapshot().ClientId
		}},
		{"console", "console_setting", "announcements", `[{"title":"probe"}]`, func() {
			_ = console_setting.ConsoleSettingSnapshot().Announcements
		}},
		{"token", "token_setting", "max_user_tokens", "2000", func() {
			_ = operation_setting.GetMaxUserTokens()
		}},
		{"checkin", "checkin_setting", "min_quota", "1234", func() {
			_ = operation_setting.IsCheckinEnabled()
			minQuota, maxQuota := operation_setting.GetCheckinQuotaRange()
			_ = minQuota + maxQuota
		}},
		{"monitor", "monitor_setting", "auto_test_channel_minutes", "9", func() {
			_ = operation_setting.MonitorSettingSnapshot().AutoTestChannelMinutes
		}},
		{"payment", "payment_setting", "amount_options", "[10,20]", func() {
			snapshot := operation_setting.PaymentSettingSnapshot()
			for _, amount := range snapshot.AmountOptions {
				_ = amount
			}
			for amount := range snapshot.AmountDiscount {
				_ = amount
			}
		}},
		{"quota", "quota_setting", "enable_free_model_pre_consume", "true", func() {
			_ = operation_setting.FreeModelPreConsumeEnabled()
		}},
		{"channel_affinity", "channel_affinity_setting", "enabled", "true", func() {
			snapshot := operation_setting.ChannelAffinitySettingSnapshot()
			_ = snapshot.Enabled
			for _, rule := range snapshot.Rules {
				_ = rule.Name
			}
		}},
		{"log_retention", "log_retention_setting", "run_interval_hours", "7", func() {
			_ = log_retention_setting.GetLogRetentionSetting().RunIntervalHours
		}},
		{"perf_metrics", "perf_metrics_setting", "flush_interval", "9", func() {
			_ = perf_metrics_setting.GetSetting().FlushInterval
			_ = perf_metrics_setting.GetBucketSeconds()
			_ = perf_metrics_setting.GetFlushIntervalMinutes()
		}},
		{"performance", "performance_setting", "monitor_enabled", "true", func() {
			_ = performance_setting.PerformanceSettingSnapshot().MonitorEnabled
		}},
		{"billing", "billing_setting", "billing_mode", `{"probe-model":"tiered_expr"}`, func() {
			_ = billing_setting.GetBillingMode("probe-model")
			_, _ = billing_setting.GetBillingExpr("probe-model")
		}},
		// group_ratio 的字段是 *types.RWMap，访问走 RWMap 自己的锁；一并放进来
		// 固定住"该模块不需要额外快照"这个结论，防止日后被误判为漏改。
		{"group_ratio", "group_ratio_setting", "group_special_usable_group", `{"vip":{"probe":"probe-group"}}`, func() {
			_ = ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup
		}},
		// tool_price_setting 的读取走原子指针索引，但索引重建读配置本身；重建
		// 与并发 Update 之间同样不能裸读包级结构体。
		{"tool_price", "tool_price_setting", "prices", `{"probe_tool":0.02}`, func() {
			_ = operation_setting.GetToolPrice("probe_tool")
			operation_setting.RebuildToolPriceIndex()
		}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// 恢复原配置，避免探针值泄漏到同包其它用例。
			previous := moduleConfigMap(t, tc.mod)
			t.Cleanup(func() { restoreModuleConfig(t, tc.mod, previous) })

			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				for i := 0; i < 2000; i++ {
					tc.read()
				}
			}()
			go func() {
				defer wg.Done()
				for i := 0; i < 300; i++ {
					if err := config.GlobalConfig.Update(tc.mod, map[string]string{tc.key: tc.val}); err != nil {
						t.Errorf("update %s: %v", tc.mod, err)
						return
					}
				}
			}()
			wg.Wait()
		})
	}
}

func moduleConfigMap(t *testing.T, module string) map[string]string {
	t.Helper()
	registered := config.GlobalConfig.Get(module)
	if registered == nil {
		t.Fatalf("config module %q is not registered", module)
	}
	configMap, err := config.ConfigToMap(registered)
	if err != nil {
		t.Fatalf("snapshot config %q: %v", module, err)
	}
	return configMap
}

func restoreModuleConfig(t *testing.T, module string, configMap map[string]string) {
	t.Helper()
	if err := config.GlobalConfig.Update(module, configMap); err != nil {
		t.Fatalf("restore config %q: %v", module, err)
	}
}

// 快照访问器必须看到 Update 之后的新值。加锁不能靠读一份陈旧副本通过：那样
// 管理端改完设置，请求热路径永远拿到旧值，是比竞争更难发现的错误。
func TestSettingAccessorsSeeUpdatedSettings(t *testing.T) {
	cases := []struct {
		name  string
		mod   string
		key   string
		val   string
		check func(t *testing.T)
	}{
		{"token", "token_setting", "max_user_tokens", "2468", func(t *testing.T) {
			if got := operation_setting.GetMaxUserTokens(); got != 2468 {
				t.Fatalf("GetMaxUserTokens() = %d, want 2468", got)
			}
		}},
		{"oidc", "oidc", "client_id", "fresh-client-id", func(t *testing.T) {
			if got := system_setting.OIDCSettingsSnapshot().ClientId; got != "fresh-client-id" {
				t.Fatalf("OIDCSettingsSnapshot().ClientId = %q, want fresh-client-id", got)
			}
		}},
		{"quota", "quota_setting", "enable_free_model_pre_consume", "false", func(t *testing.T) {
			if operation_setting.FreeModelPreConsumeEnabled() {
				t.Fatalf("FreeModelPreConsumeEnabled() = true after disabling it")
			}
		}},
		{"billing", "billing_setting", "billing_mode", `{"fresh-probe":"tiered_expr"}`, func(t *testing.T) {
			if got := billing_setting.GetBillingMode("fresh-probe"); got != billing_setting.BillingModeTieredExpr {
				t.Fatalf("GetBillingMode(fresh-probe) = %q, want %q", got, billing_setting.BillingModeTieredExpr)
			}
		}},
		{"console", "console_setting", "announcements", `[{"title":"fresh"}]`, func(t *testing.T) {
			if got := console_setting.ConsoleSettingSnapshot().Announcements; got != `[{"title":"fresh"}]` {
				t.Fatalf("ConsoleSettingSnapshot().Announcements = %q", got)
			}
		}},
		{"perf_metrics", "perf_metrics_setting", "flush_interval", "17", func(t *testing.T) {
			if got := perf_metrics_setting.GetFlushIntervalMinutes(); got != 17 {
				t.Fatalf("GetFlushIntervalMinutes() = %d, want 17", got)
			}
		}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			previous := moduleConfigMap(t, tc.mod)
			t.Cleanup(func() { restoreModuleConfig(t, tc.mod, previous) })

			if err := config.GlobalConfig.Update(tc.mod, map[string]string{tc.key: tc.val}); err != nil {
				t.Fatalf("update %s: %v", tc.mod, err)
			}
			tc.check(t)
		})
	}
}
