package gemini

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

// 与 Ollama 分支同一边界：模型发现请求不带 relay 上下文，必须显式标记
// fetch-URL 边界，否则 SSRF 防护对该分支完全不生效。
func TestFetchGeminiModelsRejectsBlockedDestination(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"models/leaked-model"}]}`))
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

	models, err := FetchGeminiModels(common.WithSSRFValidation(context.Background()), upstream.URL, "probe-key", "")

	require.Error(t, err, "a blocked destination must not return models")
	require.Empty(t, models)
}
