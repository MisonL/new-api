package gemini

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/require"
)

func TestFetchTaskUsesPersistedMappedModelVersion(t *testing.T) {
	allowLocalGeminiTestServer(t)
	service.InitHttpClient()
	seen := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"done":false}`))
	}))
	defer server.Close()

	upstreamName := "projects/demo/locations/us/models/gemini-1.0-pro/operations/op-1"
	resp, err := (&TaskAdaptor{}).FetchTask(server.URL, "test-key", map[string]any{
		"task_id": taskcommon.EncodeLocalTaskID(upstreamName),
		"model":   "gemini-1.0-pro",
	}, "")
	require.NoError(t, err)
	require.NotNil(t, resp)
	_ = resp.Body.Close()

	select {
	case path := <-seen:
		require.Equal(t, "/v1/projects/demo/locations/us/models/gemini-1.0-pro/operations/op-1", path)
	case <-time.After(time.Second):
		t.Fatal("upstream request was not observed")
	}
}

func TestFetchTaskDefaultsToConfiguredGeminiVersion(t *testing.T) {
	allowLocalGeminiTestServer(t)
	service.InitHttpClient()
	seen := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"done":false}`))
	}))
	defer server.Close()

	upstreamName := "operations/op-2"
	resp, err := (&TaskAdaptor{}).FetchTask(server.URL, "test-key", map[string]any{
		"task_id": taskcommon.EncodeLocalTaskID(upstreamName),
	}, "")
	require.NoError(t, err)
	require.NotNil(t, resp)
	_ = resp.Body.Close()

	select {
	case path := <-seen:
		require.Equal(t, "/v1beta/operations/op-2", path)
	case <-time.After(time.Second):
		t.Fatal("upstream request was not observed")
	}
}

func allowLocalGeminiTestServer(t *testing.T) {
	t.Helper()
	setting := system_setting.GetFetchSetting()
	previous := *setting
	t.Cleanup(func() { *setting = previous })
	*setting = system_setting.FetchSetting{
		EnableSSRFProtection:   true,
		AllowPrivateIp:         true,
		DomainFilterMode:       false,
		IpFilterMode:           false,
		AllowedPorts:           []string{"1-65535"},
		ApplyIPFilterForDomain: true,
	}
}
