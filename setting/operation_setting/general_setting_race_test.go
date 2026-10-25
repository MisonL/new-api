package operation_setting_test

import (
	"strconv"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// TestGeneralSettingAccessorsAreRaceFreeDuringConfigUpdate 覆盖热路径读取与分层
// 配置写入并发。
//
// 展示类型与自定义货币符号在计费日志路径上每次调用都要读（logger.FormatQuota），
// 而管理员保存设置或 SyncOptions 重载会经 config.ConfigManager.Update 改写同一个
// generalSetting 结构体。访问器不持模块锁时 -race 会报并发读写。
func TestGeneralSettingAccessorsAreRaceFreeDuringConfigUpdate(t *testing.T) {
	original := operation_setting.GetGeneralSettingSnapshot()
	t.Cleanup(func() {
		restore := map[string]string{
			"quota_display_type":            original.QuotaDisplayType,
			"custom_currency_symbol":        original.CustomCurrencySymbol,
			"custom_currency_exchange_rate": strconv.FormatFloat(original.CustomCurrencyExchangeRate, 'f', -1, 64),
		}
		if err := config.GlobalConfig.Update("general_setting", restore); err != nil {
			t.Fatalf("failed to restore general_setting: %v", err)
		}
	})

	const iterations = 300
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			display := operation_setting.QuotaDisplayTypeUSD
			if i%2 == 1 {
				display = operation_setting.QuotaDisplayTypeCustom
			}
			if err := config.GlobalConfig.Update("general_setting", map[string]string{
				"quota_display_type":            display,
				"custom_currency_symbol":        "$",
				"custom_currency_exchange_rate": "7.3",
			}); err != nil {
				t.Errorf("Update returned error: %v", err)
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = operation_setting.IsCurrencyDisplay()
			_ = operation_setting.IsCNYDisplay()
			_ = operation_setting.GetQuotaDisplayType()
			_ = operation_setting.GetCurrencySymbol()
			_ = operation_setting.GetUsdToCurrencyRate(7.3)
			_ = logger.FormatQuota(1000)
			_ = logger.LogQuota(1000)
		}
	}()

	wg.Wait()
}
