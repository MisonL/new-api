package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// FetchModels 直接对用户可控的 base_url 发起请求并把响应体交给 JSON 解码。
// 上游可以返回任意大小的响应体：没有上限时每个并发请求都会把整份响应读进
// 内存。仓库为模型列表准备的小信封上限（common.UpstreamSmallResponseLimit）
// 必须在这里生效，并且超限要显式失败，不能截断后当成完整列表解析。

func withFetchModelsTestSSRFSetting(t *testing.T) {
	t.Helper()
	fetchSetting := system_setting.GetFetchSetting()
	previous := *fetchSetting
	t.Cleanup(func() { *fetchSetting = previous })
	*fetchSetting = system_setting.FetchSetting{
		EnableSSRFProtection: true,
		AllowPrivateIp:       true,
		AllowedPorts:         []string{},
	}
	service.InitHttpClient()
}

func newFetchModelsContext(t *testing.T, baseURL string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := `{"base_url":"` + baseURL + `","type":0,"key":"sk-test"}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/channel/fetch_models/0", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, recorder
}

func TestFetchModelsRejectsOversizedUpstreamBody(t *testing.T) {
	setupHeaderPolicyControllerTestDB(t)
	withFetchModelsTestSSRFSetting(t)

	// 让合法 JSON 的单个 id 超过上限：解析前就必须拒绝。
	oversized := strings.Repeat("x", int(common.UpstreamSmallResponseLimit)+1024)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"` + oversized + `"}]}`))
	}))
	t.Cleanup(server.Close)

	c, recorder := newFetchModelsContext(t, server.URL)
	FetchModels(c)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success,
		"超过大小上限的模型列表必须显式失败：%s", recorder.Body.String())
	require.Contains(t, response.Message, "exceeds",
		"错误信息必须说明响应体超限，实际：%s", response.Message)
}

// 反向对照：正常大小的模型列表不得被上限误伤。
func TestFetchModelsAcceptsNormalUpstreamBody(t *testing.T) {
	setupHeaderPolicyControllerTestDB(t)
	withFetchModelsTestSSRFSetting(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o"},{"id":"gpt-4o-mini"}]}`))
	}))
	t.Cleanup(server.Close)

	c, recorder := newFetchModelsContext(t, server.URL)
	FetchModels(c)

	var response struct {
		Success bool     `json:"success"`
		Data    []string `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, recorder.Body.String())
	require.Equal(t, []string{"gpt-4o", "gpt-4o-mini"}, response.Data)
}
