package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

// FetchModels 的通用分支用裸 &http.Client{} 请求用户在请求体里提交的
// base_url，既不经过 service.GetHttpClient() 的 SSRF 防护，也不受
// RelayTimeout 约束。Gemini 分支走 FetchGeminiModels，用的是带防护的共享
// 客户端，所以缺口只在通用分支。
//
// 判定方式：把 SSRF 保护打开、域名设为白名单（不含 localhost），再让请求
// 打到本机临时服务器。受保护的客户端会在 DNS 校验阶段拒绝，裸客户端会直接
// 连上并把 models 返回给调用方。
func TestFetchModelsUsesSharedSSRFProtectedClient(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"leaked-model"}]}`))
	}))
	defer upstream.Close()

	fetchSetting := system_setting.GetFetchSetting()
	previous := *fetchSetting
	t.Cleanup(func() { *fetchSetting = previous })
	*fetchSetting = system_setting.FetchSetting{
		EnableSSRFProtection:   true,
		AllowPrivateIp:         false,
		DomainFilterMode:       true,
		DomainList:             nil,
		IpFilterMode:           true,
		ApplyIPFilterForDomain: true,
	}

	gin.SetMode(gin.TestMode)
	// 共享客户端由启动流程初始化；测试里显式建一次，才能验证它真的带防护。
	service.InitHttpClient()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := []byte(`{"base_url":"` + upstream.URL + `","type":0,"key":"probe"}`)
	c.Request, _ = http.NewRequest(http.MethodPost, "/api/channel/fetch_models", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	FetchModels(c)

	// 受保护客户端会拒绝回环地址；任何成功取回 models 的路径都说明绕过生效。
	if bytes.Contains(w.Body.Bytes(), []byte("leaked-model")) {
		t.Fatalf("FetchModels reached a blocked address and returned its body: %s", w.Body.String())
	}
}
