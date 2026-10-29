package model_setting

import (
	"github.com/QuantumNous/new-api/setting/config"
)

// GeminiSettings defines Gemini model configuration. 注意bool要以enabled结尾才可以生效编辑
type GeminiSettings struct {
	SafetySettings                        map[string]string `json:"safety_settings"`
	VersionSettings                       map[string]string `json:"version_settings"`
	SupportedImagineModels                []string          `json:"supported_imagine_models"`
	ThinkingAdapterEnabled                bool              `json:"thinking_adapter_enabled"`
	ThinkingAdapterBudgetTokensPercentage float64           `json:"thinking_adapter_budget_tokens_percentage"`
	FunctionCallThoughtSignatureEnabled   bool              `json:"function_call_thought_signature_enabled"`
	RemoveFunctionResponseIdEnabled       bool              `json:"remove_function_response_id_enabled"`
}

// 默认配置
var defaultGeminiSettings = GeminiSettings{
	SafetySettings: map[string]string{
		"default": "OFF",
	},
	VersionSettings: map[string]string{
		"default":        "v1beta",
		"gemini-1.0-pro": "v1",
	},
	SupportedImagineModels: []string{
		"gemini-2.0-flash-exp-image-generation",
		"gemini-2.0-flash-exp",
		"gemini-3-pro-image",
		"gemini-3-pro-image-preview",
		"gemini-2.5-flash-image",
		"gemini-3.1-flash-image",
		"gemini-3.1-flash-lite-image",
		"gemini-3.1-flash-image-preview",
		"gemini-3.1-flash-lite-image-preview",
	},
	ThinkingAdapterEnabled:                false,
	ThinkingAdapterBudgetTokensPercentage: 0.6,
	FunctionCallThoughtSignatureEnabled:   true,
	RemoveFunctionResponseIdEnabled:       true,
}

// 全局实例
var geminiSettings = defaultGeminiSettings

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("gemini", &geminiSettings)
}

// GetGeminiSettings 获取Gemini配置
func GetGeminiSettings() *GeminiSettings {
	return &geminiSettings
}

// readGeminiSettings 在配置管理器读锁内把当前配置交给 fn；直接读包级变量
// 与管理端 Update 构成数据竞争。写入方发布全新的 map/slice，旧对象替换后
// 不再被改写，复制切片头或 map 引用后在锁外读取是安全的。
func readGeminiSettings(fn func(*GeminiSettings)) {
	if fn == nil {
		return
	}
	applied := config.GlobalConfig.ReadLive("gemini", func(cfg interface{}) {
		if settings, ok := cfg.(*GeminiSettings); ok && settings != nil {
			fn(settings)
		}
	})
	if !applied {
		fn(&geminiSettings)
	}
}

// GeminiSettingsSnapshot 返回当前 Gemini 配置的浅拷贝。
func GeminiSettingsSnapshot() GeminiSettings {
	var snapshot GeminiSettings
	readGeminiSettings(func(settings *GeminiSettings) {
		snapshot = *settings
	})
	return snapshot
}

// GetGeminiSafetySetting 获取安全设置
func GetGeminiSafetySetting(key string) string {
	value := ""
	readGeminiSettings(func(settings *GeminiSettings) {
		if v, ok := settings.SafetySettings[key]; ok {
			value = v
			return
		}
		value = settings.SafetySettings["default"]
	})
	return value
}

// GetGeminiVersionSetting 获取版本设置
func GetGeminiVersionSetting(key string) string {
	value := ""
	readGeminiSettings(func(settings *GeminiSettings) {
		if v, ok := settings.VersionSettings[key]; ok {
			value = v
			return
		}
		value = settings.VersionSettings["default"]
	})
	return value
}

func IsGeminiModelSupportImagine(model string) bool {
	found := false
	readGeminiSettings(func(settings *GeminiSettings) {
		for _, v := range settings.SupportedImagineModels {
			if v == model {
				found = true
				return
			}
		}
	})
	return found
}
