package controller

import (
	"bytes"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
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

// 上游 envelope 的 success 翻译规则。此前 Success 是 bool 零值 false：不带
// success 字段的裸静态载荷会被判成失败，且 success=false + 空 message 会以
// 空错误串进入结果、收集侧按 Err=="" 判成成功——两头都是伪状态。
func TestUpstreamEnvelopeError(t *testing.T) {
	failed := false
	succeeded := true

	for _, tc := range []struct {
		name    string
		success *bool
		message string
		want    string
	}{
		{name: "missing success field treats as undeclared", success: nil, message: "", want: ""},
		{name: "explicit success", success: &succeeded, message: "", want: ""},
		{name: "explicit failure with message", success: &failed, message: "quota exhausted", want: "quota exhausted"},
		{
			name:    "explicit failure without message synthesizes one",
			success: &failed,
			message: "",
			want:    "upstream returned success=false without a message",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, upstreamEnvelopeError(tc.success, tc.message))
		})
	}
}

// 端到端：本地 HTTP 上游返回不带 success 字段的静态 ratio_config，/fetch 必须
// 把它当成功解析；success=false 且无 message 的上游必须报错而不是伪成功。
// 修复前两种都会进错误分支（bool 零值 false）或伪成功分支（空 Err）。
func TestFetchUpstreamRatiosEnvelopeSemantics(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 无 success 字段的静态 type1 数据
	staticData := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model_ratio":{"e2e-model":2},"completion_ratio":{"e2e-model":3}}`))
	}))
	defer staticData.Close()

	// 显式失败且无 message
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":false,"data":{},"message":""}`))
	}))
	defer failing.Close()

	perform := func(baseURL string) (*httptest.ResponseRecorder, map[string]any) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/ratio_sync/fetch",
			bytes.NewBufferString(fmt.Sprintf(`{"upstreams":[{"name":"probe","base_url":%q,"endpoint":"/api/ratio_config"}],"timeout":5}`, baseURL)))
		c.Request.Header.Set("Content-Type", "application/json")
		FetchUpstreamRatios(c)

		var out map[string]any
		require.NoError(t, common.UnmarshalJsonStr(w.Body.String(), &out))
		return w, out
	}

	// 静态数据（无 success 字段）：应解析为成功并给出差异表
	w, out := perform(staticData.URL)
	require.Equal(t, http.StatusOK, w.Code)
	results := out["data"].(map[string]any)["test_results"].([]any)
	require.NotEmpty(t, results)
	first := results[0].(map[string]any)
	require.Equal(t, "success", first["status"], "不带 success 字段的静态载荷不能被判失败")

	// 显式失败：必须有非空错误串
	w, out = perform(failing.URL)
	require.Equal(t, http.StatusOK, w.Code)
	results = out["data"].(map[string]any)["test_results"].([]any)
	require.NotEmpty(t, results)
	first = results[0].(map[string]any)
	require.Equal(t, "error", first["status"])
	require.NotEmpty(t, first["error"], "success=false 且 message 为空必须合成错误串")
}
