package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// 额度展示类型
const (
	QuotaDisplayTypeUSD    = "USD"
	QuotaDisplayTypeCNY    = "CNY"
	QuotaDisplayTypeTokens = "TOKENS"
	QuotaDisplayTypeCustom = "CUSTOM"
)

// GeneralSetting contains globally mutable operation settings exposed in the admin console.
type GeneralSetting struct {
	DocsLink                                          string `json:"docs_link"`
	LogFilterAutocompleteEnabled                      bool   `json:"log_filter_autocomplete_enabled"`
	PingIntervalEnabled                               bool   `json:"ping_interval_enabled"`
	PingIntervalSeconds                               int    `json:"ping_interval_seconds"`
	ResponsesStreamBootstrapRecoveryEnabled           bool   `json:"responses_stream_bootstrap_recovery_enabled"`
	ResponsesStreamBootstrapGracePeriodSeconds        int    `json:"responses_stream_bootstrap_grace_period_seconds"`
	ResponsesStreamBootstrapProbeIntervalMilliseconds int    `json:"responses_stream_bootstrap_probe_interval_milliseconds"`
	ResponsesStreamBootstrapPingIntervalSeconds       int    `json:"responses_stream_bootstrap_ping_interval_seconds"`
	ResponsesStreamBootstrapRetryableStatusCodes      []int  `json:"responses_stream_bootstrap_retryable_status_codes"`
	ChannelHealthEnabled                              bool   `json:"channel_health_enabled"`
	ChannelHealthCooldownSeconds                      int    `json:"channel_health_cooldown_seconds"`
	ChannelHealthProbeLeaseSeconds                    int    `json:"channel_health_probe_lease_seconds"`
	// 当前站点额度展示类型：USD / CNY / TOKENS
	QuotaDisplayType string `json:"quota_display_type"`
	// 自定义货币符号，用于 CUSTOM 展示类型
	CustomCurrencySymbol string `json:"custom_currency_symbol"`
	// 自定义货币与美元汇率（1 USD = X Custom）
	CustomCurrencyExchangeRate float64 `json:"custom_currency_exchange_rate"`
}

// DefaultResponsesBootstrapRetryableStatusCodes returns a copy of the default
// retryable status-code allowlist for responses bootstrap recovery.
func DefaultResponsesBootstrapRetryableStatusCodes() []int {
	return []int{
		401,
		403,
		408,
		429,
		500,
		502,
		503,
		504,
	}
}

// 默认配置
var generalSetting = GeneralSetting{
	DocsLink:                                          "https://github.com/MisonL/new-api/blob/main/README.md",
	LogFilterAutocompleteEnabled:                      true,
	PingIntervalEnabled:                               false,
	PingIntervalSeconds:                               60,
	ResponsesStreamBootstrapRecoveryEnabled:           false,
	ResponsesStreamBootstrapGracePeriodSeconds:        180,
	ResponsesStreamBootstrapProbeIntervalMilliseconds: 1000,
	ResponsesStreamBootstrapPingIntervalSeconds:       10,
	// ResponsesStreamBootstrapRetryableStatusCodes keeps 401/403 because in
	// multi-channel delivery these failures can be channel-specific, allowing
	// fallback to another channel or a refreshed credential before the first payload.
	ResponsesStreamBootstrapRetryableStatusCodes: DefaultResponsesBootstrapRetryableStatusCodes(),
	// Channel health cooling changes request routing and must be enabled explicitly
	// by an operator after its scope and failure classification are verified.
	ChannelHealthEnabled:           false,
	ChannelHealthCooldownSeconds:   DefaultChannelHealthCooldownSeconds,
	ChannelHealthProbeLeaseSeconds: DefaultChannelHealthProbeLeaseSeconds,
	QuotaDisplayType:               QuotaDisplayTypeUSD,
	CustomCurrencySymbol:           "¤",
	CustomCurrencyExchangeRate:     1.0,
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("general_setting", &generalSetting)
}

// GetGeneralSetting returns the mutable singleton general settings object.
//
// Deprecated for new production reads: the returned pointer aliases a struct that
// config writes rewrite field by field, so reading fields through it races with a
// concurrent settings save. Use the field accessors below, or
// GetGeneralSettingSnapshot when several fields must be consistent with each
// other. Kept for tests that seed settings before exercising a code path.
func GetGeneralSetting() *GeneralSetting {
	return &generalSetting
}

// readGeneralSetting runs fn against the live general settings under the config
// manager read lock. Accessors must not hold a lock of their own here: the
// manager lock is the only one config writes take.
//
// When the module is somehow not registered (or delivers an unexpected type),
// nothing can be writing the struct, so reading the package instance directly
// is safe and keeps the accessors deterministic instead of silently returning
// zero values.
func readGeneralSetting(fn func(*GeneralSetting)) {
	delivered := false
	config.GlobalConfig.ReadLive("general_setting", func(cfg interface{}) {
		if setting, ok := cfg.(*GeneralSetting); ok && setting != nil {
			delivered = true
			fn(setting)
		}
	})
	if !delivered {
		fn(&generalSetting)
	}
}

// GetGeneralSettingSnapshot returns a copy of the current general settings under config-manager locking.
func GetGeneralSettingSnapshot() GeneralSetting {
	var snapshot GeneralSetting
	config.GlobalConfig.Read("general_setting", func(cfg interface{}) {
		if setting, ok := cfg.(*GeneralSetting); ok && setting != nil {
			snapshot = *setting
			if setting.ResponsesStreamBootstrapRetryableStatusCodes != nil {
				snapshot.ResponsesStreamBootstrapRetryableStatusCodes = append(
					[]int(nil),
					setting.ResponsesStreamBootstrapRetryableStatusCodes...,
				)
			}
		}
	})
	return snapshot
}

// IsCurrencyDisplay 是否以货币形式展示（美元或人民币）
func IsCurrencyDisplay() bool {
	var displayType string
	readGeneralSetting(func(setting *GeneralSetting) {
		displayType = setting.QuotaDisplayType
	})
	return displayType != QuotaDisplayTypeTokens
}

// IsCNYDisplay 是否以人民币展示
func IsCNYDisplay() bool {
	var displayType string
	readGeneralSetting(func(setting *GeneralSetting) {
		displayType = setting.QuotaDisplayType
	})
	return displayType == QuotaDisplayTypeCNY
}

// GetQuotaDisplayType 返回额度展示类型
func GetQuotaDisplayType() string {
	var displayType string
	readGeneralSetting(func(setting *GeneralSetting) {
		displayType = setting.QuotaDisplayType
	})
	return displayType
}

// GetCurrencySymbol 返回当前展示类型对应符号
func GetCurrencySymbol() string {
	var symbol string
	readGeneralSetting(func(setting *GeneralSetting) {
		switch setting.QuotaDisplayType {
		case QuotaDisplayTypeUSD:
			symbol = "$"
		case QuotaDisplayTypeCNY:
			symbol = "¥"
		case QuotaDisplayTypeCustom:
			symbol = setting.CustomCurrencySymbol
			if symbol == "" {
				symbol = "¤"
			}
		}
	})
	return symbol
}

// GetUsdToCurrencyRate 返回 1 USD = X <currency> 的 X（TOKENS 不适用）
func GetUsdToCurrencyRate(usdToCny float64) float64 {
	rate := float64(1)
	readGeneralSetting(func(setting *GeneralSetting) {
		switch setting.QuotaDisplayType {
		case QuotaDisplayTypeCNY:
			rate = usdToCny
		case QuotaDisplayTypeCustom:
			if setting.CustomCurrencyExchangeRate > 0 {
				rate = setting.CustomCurrencyExchangeRate
			}
		}
	})
	return rate
}
