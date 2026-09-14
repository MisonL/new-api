package ollama

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/require"
)

func TestFetchOllamaVersionAppliesExtraHeaders(t *testing.T) {
	fetchSetting := system_setting.GetFetchSetting()
	previousSetting := *fetchSetting
	t.Cleanup(func() { *fetchSetting = previousSetting })
	// The fixture intentionally listens on loopback; production requests stay
	// behind the shared SSRF-guarded service client.
	fetchSetting.EnableSSRFProtection = false
	service.InitHttpClient()

	seen := make(chan struct {
		path          string
		userAgent     string
		authorization string
		host          string
		multi         []string
	}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- struct {
			path          string
			userAgent     string
			authorization string
			host          string
			multi         []string
		}{
			path:          r.URL.Path,
			userAgent:     r.Header.Get("User-Agent"),
			authorization: r.Header.Get("Authorization"),
			host:          r.Host,
			multi:         append([]string(nil), r.Header.Values("X-Multi")...),
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"0.11.0"}`))
	}))
	defer server.Close()

	headers := http.Header{}
	headers.Set("User-Agent", "Codex-Test")
	headers.Set("Host", "ollama.test")
	headers.Add("X-Multi", "one")
	headers.Add("X-Multi", "two")

	version, err := FetchOllamaVersion(server.URL, "sk-test", headers)
	require.NoError(t, err)
	require.Equal(t, "0.11.0", version)
	request := <-seen
	require.Equal(t, "/api/version", request.path)
	require.Equal(t, "Codex-Test", request.userAgent)
	require.Equal(t, "Bearer sk-test", request.authorization)
	require.Equal(t, "ollama.test", request.host)
	require.Equal(t, []string{"one", "two"}, request.multi)
}

func TestFetchOllamaModelsNormalizesBaseURLAndAppliesHeaders(t *testing.T) {
	fetchSetting := system_setting.GetFetchSetting()
	previousSetting := *fetchSetting
	t.Cleanup(func() { *fetchSetting = previousSetting })
	fetchSetting.EnableSSRFProtection = false
	service.InitHttpClient()

	seen := make(chan struct {
		path          string
		authorization string
		multi         []string
	}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- struct {
			path          string
			authorization string
			multi         []string
		}{
			path:          r.URL.Path,
			authorization: r.Header.Get("Authorization"),
			multi:         append([]string(nil), r.Header.Values("X-Multi")...),
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"models":[{"name":"llama3.1:8b","size":123,"details":{"family":"llama"}}]}`)
	}))
	defer server.Close()

	headers := http.Header{}
	headers.Add("X-Multi", "one")
	headers.Add("X-Multi", "two")
	models, err := FetchOllamaModels(server.URL+"///", "sk-test", headers)
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, "llama3.1:8b", models[0].Name)
	require.Equal(t, int64(123), models[0].Size)
	require.Equal(t, "llama", models[0].Details.Family)

	request := <-seen
	require.Equal(t, "/api/tags", request.path)
	require.Equal(t, "Bearer sk-test", request.authorization)
	require.Equal(t, []string{"one", "two"}, request.multi)
}

func TestFetchOllamaModelsReturnsNonOKAndOversizedErrors(t *testing.T) {
	fetchSetting := system_setting.GetFetchSetting()
	previousSetting := *fetchSetting
	t.Cleanup(func() { *fetchSetting = previousSetting })
	fetchSetting.EnableSSRFProtection = false
	service.InitHttpClient()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"upstream unavailable"}`))
	}))
	defer server.Close()
	oversizedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bytes.Repeat([]byte("x"), int(maxOllamaResponseBodyBytes+1)))
	}))
	defer oversizedServer.Close()

	_, err := FetchOllamaModels(server.URL, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "服务器返回错误 502")

	_, err = FetchOllamaModels(oversizedServer.URL, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "读取响应失败")
	require.Contains(t, err.Error(), "超过")
}

func TestNewOllamaRequestBindsDeadline(t *testing.T) {
	request, cancel, err := newOllamaRequest(http.MethodGet, "http://example.test/api/tags", nil, time.Second)
	require.NoError(t, err)
	t.Cleanup(cancel)
	deadline, ok := request.Context().Deadline()
	require.True(t, ok)
	require.WithinDuration(t, time.Now().Add(time.Second), deadline, 500*time.Millisecond)
}

func TestGetOllamaHTTPClientDoesNotShortenLongOperationTimeout(t *testing.T) {
	previousRelayTimeout := common.RelayTimeout
	t.Cleanup(func() {
		common.RelayTimeout = previousRelayTimeout
		service.InitHttpClient()
	})
	common.RelayTimeout = 5
	service.InitHttpClient()

	client, err := getOllamaHTTPClient(30 * time.Minute)
	require.NoError(t, err)
	require.Equal(t, 30*time.Minute, client.Timeout)
	require.Equal(t, 5*time.Second, service.GetHttpClient().Timeout)
}

func TestReadOllamaResponseBodyRejectsOversizedPayload(t *testing.T) {
	body := bytes.Repeat([]byte("x"), int(maxOllamaResponseBodyBytes+1))
	_, err := readOllamaResponseBody(bytes.NewReader(body))
	require.Error(t, err)
	require.Contains(t, err.Error(), "响应体超过")
}
