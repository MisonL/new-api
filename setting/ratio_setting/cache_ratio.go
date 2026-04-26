package ratio_setting

import (
	"github.com/QuantumNous/new-api/types"
)

var defaultCacheRatio = map[string]float64{
	"deepseek-v3p1":                       0.5,
	"deepseek-v3p2":                       0.5,
	"gemini-3-flash-preview":              0.1,
	"gemini-3-pro-preview":                0.1,
	"gemini-3.1-pro-preview":              0.1,
	"glm-4p7":                             0.5,
	"glm-5":                               0.2,
	"gpt-4":                               0.5,
	"gpt-oss-20b":                         4.0 / 7.0,
	"gpt-oss-120b":                        1.0 / 15.0,
	"o1":                                  0.5,
	"o1-2024-12-17":                       0.5,
	"o1-preview-2024-09-12":               0.5,
	"o1-preview":                          0.5,
	"o1-mini-2024-09-12":                  0.5,
	"o1-mini":                             0.5,
	"o3-mini":                             0.5,
	"o3-mini-2025-01-31":                  0.5,
	"gpt-4o-2024-11-20":                   0.5,
	"gpt-4o-2024-08-06":                   0.5,
	"gpt-4o":                              0.5,
	"gpt-4o-mini-2024-07-18":              0.5,
	"gpt-4o-mini":                         0.5,
	"gpt-4o-realtime-preview":             0.5,
	"gpt-4o-mini-realtime-preview":        0.5,
	"gpt-4.5-preview":                     0.5,
	"gpt-4.5-preview-2025-02-27":          0.5,
	"gpt-4.1":                             0.25,
	"gpt-4.1-mini":                        0.25,
	"gpt-4.1-nano":                        0.25,
	"gpt-5":                               0.1,
	"gpt-5-2025-08-07":                    0.1,
	"gpt-5-chat-latest":                   0.1,
	"gpt-5-mini":                          0.1,
	"gpt-5-mini-2025-08-07":               0.1,
	"gpt-5-nano":                          0.1,
	"gpt-5-nano-2025-08-07":               0.1,
	"deepseek-chat":                       0.25,
	"deepseek-reasoner":                   0.25,
	"deepseek-coder":                      0.25,
	"kimi-k2-thinking":                    0.25,
	"kimi-k2p5":                           1.0 / 6.0,
	"llama-v3p3-70b-instruct":             0.5,
	"minimax-m2p5":                        0.1,
	"qwen3-8b":                            0.5,
	"qwen3-vl-30b-a3b-thinking":           7.0 / 15.0,
	"claude-3-sonnet-20240229":            0.1,
	"claude-3-opus-20240229":              0.1,
	"claude-3-haiku-20240307":             0.1,
	"claude-3-5-haiku-20241022":           0.1,
	"claude-haiku-4-5-20251001":           0.1,
	"claude-3-5-sonnet-20240620":          0.1,
	"claude-3-5-sonnet-20241022":          0.1,
	"claude-3-7-sonnet-20250219":          0.1,
	"claude-3-7-sonnet-20250219-thinking": 0.1,
	"claude-sonnet-4-20250514":            0.1,
	"claude-sonnet-4-20250514-thinking":   0.1,
	"claude-opus-4-20250514":              0.1,
	"claude-opus-4-20250514-thinking":     0.1,
	"claude-opus-4-1-20250805":            0.1,
	"claude-opus-4-1-20250805-thinking":   0.1,
	"claude-sonnet-4-5-20250929":          0.1,
	"claude-sonnet-4-5-20250929-thinking": 0.1,
	"claude-opus-4-5-20251101":            0.1,
	"claude-opus-4-5-20251101-thinking":   0.1,
	"claude-opus-4-6":                     0.1,
	"claude-opus-4-6-thinking":            0.1,
	"claude-opus-4-6-max":                 0.1,
	"claude-opus-4-6-high":                0.1,
	"claude-opus-4-6-medium":              0.1,
	"claude-opus-4-6-low":                 0.1,
	"claude-opus-4-7":                     0.1,
	"claude-opus-4-7-thinking":            0.1,
	"claude-opus-4-7-max":                 0.1,
	"claude-opus-4-7-xhigh":               0.1,
	"claude-opus-4-7-high":                0.1,
	"claude-opus-4-7-medium":              0.1,
	"claude-opus-4-7-low":                 0.1,
}

var defaultCreateCacheRatio = map[string]float64{
	"claude-3-sonnet-20240229":            1.25,
	"claude-3-opus-20240229":              1.25,
	"claude-3-haiku-20240307":             1.25,
	"claude-3-5-haiku-20241022":           1.25,
	"claude-haiku-4-5-20251001":           1.25,
	"claude-3-5-sonnet-20240620":          1.25,
	"claude-3-5-sonnet-20241022":          1.25,
	"claude-3-7-sonnet-20250219":          1.25,
	"claude-3-7-sonnet-20250219-thinking": 1.25,
	"claude-sonnet-4-20250514":            1.25,
	"claude-sonnet-4-20250514-thinking":   1.25,
	"claude-opus-4-20250514":              1.25,
	"claude-opus-4-20250514-thinking":     1.25,
	"claude-opus-4-1-20250805":            1.25,
	"claude-opus-4-1-20250805-thinking":   1.25,
	"claude-sonnet-4-5-20250929":          1.25,
	"claude-sonnet-4-5-20250929-thinking": 1.25,
	"claude-opus-4-5-20251101":            1.25,
	"claude-opus-4-5-20251101-thinking":   1.25,
	"claude-opus-4-6":                     1.25,
	"claude-opus-4-6-thinking":            1.25,
	"claude-opus-4-6-max":                 1.25,
	"claude-opus-4-6-high":                1.25,
	"claude-opus-4-6-medium":              1.25,
	"claude-opus-4-6-low":                 1.25,
	"claude-opus-4-7":                     1.25,
	"claude-opus-4-7-thinking":            1.25,
	"claude-opus-4-7-max":                 1.25,
	"claude-opus-4-7-xhigh":               1.25,
	"claude-opus-4-7-high":                1.25,
	"claude-opus-4-7-medium":              1.25,
	"claude-opus-4-7-low":                 1.25,
}

//var defaultCreateCacheRatio = map[string]float64{}

var cacheRatioMap = types.NewRWMap[string, float64]()
var createCacheRatioMap = types.NewRWMap[string, float64]()

// GetCacheRatioMap returns a copy of the cache ratio map
func GetCacheRatioMap() map[string]float64 {
	return cacheRatioMap.ReadAll()
}

// CacheRatio2JSONString converts the cache ratio map to a JSON string
func CacheRatio2JSONString() string {
	return cacheRatioMap.MarshalJSONString()
}

// CreateCacheRatio2JSONString converts the create cache ratio map to a JSON string
func CreateCacheRatio2JSONString() string {
	return createCacheRatioMap.MarshalJSONString()
}

// UpdateCacheRatioByJSONString updates the cache ratio map from a JSON string
func UpdateCacheRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonStringWithCallback(cacheRatioMap, jsonStr, InvalidateExposedDataCache)
}

// UpdateCreateCacheRatioByJSONString updates the create cache ratio map from a JSON string
func UpdateCreateCacheRatioByJSONString(jsonStr string) error {
	return types.LoadFromJsonStringWithCallback(createCacheRatioMap, jsonStr, InvalidateExposedDataCache)
}

// GetCacheRatio returns the cache ratio for a model
func GetCacheRatio(name string) (float64, bool) {
	name = FormatMatchingModelName(name)
	if ratio, ok, _ := getCompactAwareMapValue(cacheRatioMap, name); ok {
		return ratio, true
	}
	return 1, false // Default to 1 if not found
}

func GetCreateCacheRatio(name string) (float64, bool) {
	name = FormatMatchingModelName(name)
	if ratio, ok, _ := getCompactAwareMapValue(createCacheRatioMap, name); ok {
		return ratio, true
	}
	return 1.25, false // Default to 1.25 if not found
}

func GetCacheRatioCopy() map[string]float64 {
	return cacheRatioMap.ReadAll()
}

func GetCreateCacheRatioCopy() map[string]float64 {
	return createCacheRatioMap.ReadAll()
}
