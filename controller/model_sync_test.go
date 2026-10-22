package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// normalizeLocale 转小写在比较阶段是对的，但返回值会被直接拼进上游 i18n
// 路径。上游按大小写敏感目录名提供数据，返回小写会静默回退到英文数据，
// 而响应里仍回显请求的 locale。
func TestNormalizeLocaleReturnsCanonicalPathSegment(t *testing.T) {
	cases := map[string]string{
		"zh-CN": "zh-CN",
		"zh-cn": "zh-CN",
		"ZH-cn": "zh-CN",
		"zh-TW": "zh-TW",
		"zh-tw": "zh-TW",
		"en":    "en",
		"EN":    "en",
		"ja":    "ja",
		"ja-JP": "",
		"fr":    "",
		"":      "",
	}
	for input, want := range cases {
		got, ok := normalizeLocale(input)
		require.Equal(t, want, got, "normalizeLocale(%q)", input)
		require.Equal(t, want != "", ok, "normalizeLocale(%q) ok flag", input)
	}
}

// getUpstreamURLs 拼出的 URL 必须使用规范大小写。
func TestGetUpstreamURLsUsesCanonicalLocaleCase(t *testing.T) {
	modelsURL, vendorsURL := getUpstreamURLs("zh-CN")
	require.Contains(t, modelsURL, "/api/i18n/zh-CN/newapi/models.json")
	require.Contains(t, vendorsURL, "/api/i18n/zh-CN/newapi/vendors.json")

	modelsURL, _ = getUpstreamURLs("zh-cn")
	require.Contains(t, modelsURL, "/api/i18n/zh-CN/newapi/models.json",
		"lowercase input must still produce the canonical upstream segment")
}

type syncProbeModel struct {
	ModelName string `json:"model_name"`
}

func serveJSON(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func fetchProbeEnvelope(t *testing.T, server *httptest.Server) (*upstreamEnvelope[syncProbeModel], error) {
	t.Helper()
	// 每次用新 URL 避免 ETag 缓存把上一次的结果带进来。
	out := &upstreamEnvelope[syncProbeModel]{}
	err := fetchJSON(context.Background(), server.URL+"/models.json", out)
	return out, err
}

// 上游用 HTTP 200 + success:false 表达限流/拒绝。若只看状态码，同步会当作
// "空列表"继续执行并回报成功，运维看到的是成功而不是上游拒绝。
func TestFetchJSONRejectsExplicitSuccessFalse(t *testing.T) {
	server := serveJSON(t, `{"success":false,"message":"rate limited"}`, http.StatusOK)
	_, err := fetchProbeEnvelope(t, server)
	require.Error(t, err, "an explicit success:false envelope must surface as a fetch failure")
}

// 只有 message、没有数据和 success 字段时同样按失败处理。
func TestFetchJSONRejectsMessageOnlyEnvelope(t *testing.T) {
	server := serveJSON(t, `{"message":"service unavailable"}`, http.StatusOK)
	_, err := fetchProbeEnvelope(t, server)
	require.Error(t, err)
}

// 兼容性：不带 success 的静态数据与裸数组必须继续可用。
func TestFetchJSONAcceptsEnvelopeWithoutSuccessField(t *testing.T) {
	server := serveJSON(t, `{"data":[{"model_name":"gpt-4o"}]}`, http.StatusOK)
	out, err := fetchProbeEnvelope(t, server)
	require.NoError(t, err)
	require.Len(t, out.Data, 1)
	require.Equal(t, "gpt-4o", out.Data[0].ModelName)
}

func TestFetchJSONAcceptsBareArray(t *testing.T) {
	server := serveJSON(t, `[{"model_name":"claude-sonnet-5"}]`, http.StatusOK)
	out, err := fetchProbeEnvelope(t, server)
	require.NoError(t, err)
	require.Len(t, out.Data, 1)
	require.Equal(t, "claude-sonnet-5", out.Data[0].ModelName)
}

// 显式 success:true 的空列表是合法结果（上游确实没有模型）。
func TestFetchJSONAcceptsExplicitSuccessTrueEmptyList(t *testing.T) {
	server := serveJSON(t, `{"success":true,"data":[]}`, http.StatusOK)
	out, err := fetchProbeEnvelope(t, server)
	require.NoError(t, err)
	require.Empty(t, out.Data)
}

// success:false 但消息为空同样不能当成成功。
func TestFetchJSONRejectsSuccessFalseWithoutMessage(t *testing.T) {
	server := serveJSON(t, `{"success":false}`, http.StatusOK)
	_, err := fetchProbeEnvelope(t, server)
	require.Error(t, err)
	require.False(t, strings.Contains(err.Error(), "<nil>"))
}

// Model.Insert 会把调用方的零值写回库（先 Create 再用 Updates 覆盖 gorm 的
// default 标签）。同步创建时若不显式给 SyncOfficial=1，模型会以 0 落库，
// 而 0 的语义是"不参与官方同步"——它会立刻被本函数的跳过逻辑排除，之后
// 再也不会被更新，只能人工逐个改回。
func TestModelInsertPersistsSyncOfficialZeroFromStruct(t *testing.T) {
	db := setupHeaderPolicyControllerTestDB(t, &model.Model{}, &model.Vendor{})

	mi := &model.Model{ModelName: "probe-model", Status: 1}
	require.NoError(t, mi.Insert())

	var stored model.Model
	require.NoError(t, db.Where("model_name = ?", "probe-model").First(&stored).Error)
	require.Equal(t, 0, stored.SyncOfficial,
		"precondition: a zero-value struct field is persisted as 0, not replaced by the default tag")
}

// 创建路径必须显式给 SyncOfficial=1。
func TestSyncUpstreamModelsCreatedModelsParticipateInOfficialSync(t *testing.T) {
	db := setupHeaderPolicyControllerTestDB(t, &model.Model{}, &model.Vendor{})
	require.NoError(t, db.AutoMigrate(&model.Model{}, &model.Vendor{}))

	mi := &model.Model{
		ModelName:    "created-by-sync",
		Status:       1,
		SyncOfficial: 1,
	}
	require.NoError(t, mi.Insert())

	var stored model.Model
	require.NoError(t, db.Where("model_name = ?", "created-by-sync").First(&stored).Error)
	require.Equal(t, 1, stored.SyncOfficial,
		"models created by the official sync must stay eligible for future syncs")
}
