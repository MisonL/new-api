package ollama

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/require"
)

// 模型发现走渠道管理路径，请求用 context.Background() 构造，不携带 relay
// 上下文。共享客户端只校验显式标记 fetch-URL 边界的请求，因此 fetch_models
// 分支必须打上 WithSSRFValidation，否则管理员可控的 base URL 可指向内网，
// 该接口就成了内网探测器。
func TestFetchOllamaModelsRejectsBlockedDestination(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"leaked-model"}]}`))
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
	service.InitHttpClient()

	models, err := FetchOllamaModels(common.WithSSRFValidation(context.Background()), upstream.URL, "probe-key")

	require.Error(t, err, "a blocked destination must not return models")
	require.Empty(t, models)
}
