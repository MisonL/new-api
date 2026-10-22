package controller

import (
	"math"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// OpenRouter 的 pricing 字段是字符串。strconv.ParseFloat 对 "NaN"/"Infinity"
// 返回 nil error，而 NaN 与任何值比较都是 false，因此能绕过"负值跳过"与
// "prompt<=0 跳过"两道过滤，最后让整个 /fetch 响应无法序列化（json 不支持
// NaN/Inf），所有渠道的结果一起丢失。
func TestConvertOpenRouterToRatioDataSkipsNonFinitePrices(t *testing.T) {
	body := `{"data":[
		{"id":"nan-model","pricing":{"prompt":"NaN","completion":"0.001"}},
		{"id":"inf-model","pricing":{"prompt":"Infinity","completion":"0.001"}},
		{"id":"neg-inf-model","pricing":{"prompt":"-Infinity","completion":"0.001"}},
		{"id":"ok-model","pricing":{"prompt":"0.000003","completion":"0.000015"}}
	]}`

	converted, err := convertOpenRouterToRatioData(strings.NewReader(body))
	require.NoError(t, err)

	ratios, ok := converted["model_ratio"].(map[string]any)
	require.True(t, ok, "model_ratio must be present")
	require.Contains(t, ratios, "ok-model")
	require.NotContains(t, ratios, "nan-model")
	require.NotContains(t, ratios, "inf-model")
	require.NotContains(t, ratios, "neg-inf-model")

	for name, value := range ratios {
		ratio, ok := value.(float64)
		require.True(t, ok, "ratio for %s must be float64", name)
		require.False(t, ratio != ratio, "ratio for %s must not be NaN", name)
		require.False(t, ratio > 1e300 || ratio < -1e300, "ratio for %s must be finite", name)
	}
}

// NaN 的 cache 价格同样会污染 cache_ratio。
func TestConvertOpenRouterToRatioDataSkipsNonFiniteCachePrice(t *testing.T) {
	body := `{"data":[
		{"id":"m1","pricing":{"prompt":"0.000003","completion":"0.000015","input_cache_read":"NaN"}}
	]}`

	converted, err := convertOpenRouterToRatioData(strings.NewReader(body))
	require.NoError(t, err)

	if cacheRatios, ok := converted["cache_ratio"].(map[string]any); ok {
		require.NotContains(t, cacheRatios, "m1")
	}
}

// type1 数据原样进差异表，负值与 NaN/Inf 必须在源头剔除。
func TestSanitizeType1RatioDataDropsInvalidNumbers(t *testing.T) {
	input := map[string]any{
		"model_ratio": map[string]any{
			"good":     1.5,
			"negative": -5.0,
			"nan":      math.NaN(),
			"inf":      math.Inf(1),
			"stringy":  "1.25",
		},
		"completion_ratio": map[string]any{"good": 4.0},
		"billing_mode":     map[string]any{"good": "tiered_expr"},
	}

	cleaned := sanitizeType1RatioData(input)

	ratios := cleaned["model_ratio"].(map[string]any)
	require.Contains(t, ratios, "good")
	require.Contains(t, ratios, "stringy", "non-numeric entries keep the existing permissive behaviour")
	require.NotContains(t, ratios, "negative")
	require.NotContains(t, ratios, "nan")
	require.NotContains(t, ratios, "inf")

	// 非数值字段整体保留。
	require.Equal(t, "tiered_expr", cleaned["billing_mode"].(map[string]any)["good"])
}

// 清理后的数据必须能序列化，否则整个 /fetch 响应会变成空体。
func TestSanitizeType1RatioDataIsSerializable(t *testing.T) {
	input := map[string]any{
		"model_ratio": map[string]any{
			"nan": math.NaN(),
			"inf": math.Inf(-1),
			"ok":  2.0,
		},
	}
	cleaned := sanitizeType1RatioData(input)
	_, err := common.Marshal(cleaned)
	require.NoError(t, err, "sanitized data must be JSON-serializable")
}
