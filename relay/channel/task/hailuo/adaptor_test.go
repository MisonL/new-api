package hailuo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/require"
)

func allowLocalHailuoTestServer(t *testing.T) {
	t.Helper()
	setting := system_setting.GetFetchSetting()
	previous := *setting
	t.Cleanup(func() { *setting = previous })
	*setting = system_setting.FetchSetting{
		EnableSSRFProtection:   true,
		AllowPrivateIp:         true,
		AllowedPorts:           []string{"1-65535"},
		ApplyIPFilterForDomain: true,
	}
}

func TestConvertToRequestPayloadKeepsMappedModelWhenMetadataContainsModel(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "MiniMax-Hailuo-2.3",
		},
	}
	req := &relaycommon.TaskSubmitReq{
		Prompt: "a test scene",
		Metadata: map[string]interface{}{
			"model":      "attacker-selected-model",
			"resolution": "1080P",
		},
	}

	got, err := adaptor.convertToRequestPayload(req, info)

	require.NoError(t, err)
	require.Equal(t, "MiniMax-Hailuo-2.3", got.Model)
	require.Equal(t, "1080P", got.Resolution)
}

func TestBuildVideoURLAppliesRuntimeHeaders(t *testing.T) {
	allowLocalHailuoTestServer(t)
	service.InitHttpClient()

	seen := make(chan struct {
		path          string
		fileID        string
		userAgent     string
		authorization string
		multi         []string
	}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- struct {
			path          string
			fileID        string
			userAgent     string
			authorization string
			multi         []string
		}{
			path:          r.URL.Path,
			fileID:        r.URL.Query().Get("file_id"),
			userAgent:     r.Header.Get("User-Agent"),
			authorization: r.Header.Get("Authorization"),
			multi:         append([]string(nil), r.Header.Values("X-Multi")...),
		}
		_, _ = w.Write([]byte(`{"base_resp":{"status_code":0},"file":{"download_url":"https://cdn.example/video.mp4"}}`))
	}))
	defer server.Close()

	headers := http.Header{}
	headers.Set("User-Agent", "Codex-Test")
	headers.Add("X-Multi", "one")
	headers.Add("X-Multi", "two")

	adaptor := &TaskAdaptor{
		apiKey:  "sk-test",
		baseURL: server.URL,
	}

	require.Equal(t, "https://cdn.example/video.mp4", adaptor.buildVideoURL("", "file-1", "sk-test", headers))
	var request struct {
		path          string
		fileID        string
		userAgent     string
		authorization string
		multi         []string
	}
	select {
	case request = <-seen:
	case <-time.After(time.Second):
		require.Fail(t, "upstream request was not observed")
	}
	require.Equal(t, "/v1/files/retrieve", request.path)
	require.Equal(t, "file-1", request.fileID)
	require.Equal(t, "Codex-Test", request.userAgent)
	require.Equal(t, "Bearer sk-test", request.authorization)
	require.Equal(t, []string{"one", "two"}, request.multi)
}

func TestParseTaskResultReturnsErrorWhenFileLookupFails(t *testing.T) {
	allowLocalHailuoTestServer(t)
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"upstream unavailable"}`))
	}))
	defer server.Close()

	adaptor := &TaskAdaptor{apiKey: "sk-test", baseURL: server.URL}
	_, err := adaptor.ParseTaskResult([]byte(`{"task_id":"task-1","status":"success","file_id":"file-1","base_resp":{"status_code":0}}`), "sk-test")

	require.Error(t, err)
	require.Contains(t, err.Error(), "status 502")
}

func TestParseTaskResultRejectsEmptyDownloadURL(t *testing.T) {
	allowLocalHailuoTestServer(t)
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"base_resp":{"status_code":0},"file":{"download_url":""}}`))
	}))
	defer server.Close()

	adaptor := &TaskAdaptor{apiKey: "sk-test", baseURL: server.URL}
	_, err := adaptor.ParseTaskResultWithContext(context.Background(), server.URL, "sk-test", []byte(`{"task_id":"task-1","status":"success","file_id":"file-1","base_resp":{"status_code":0}}`), "")

	require.Error(t, err)
	require.Contains(t, err.Error(), "empty download URL")
}
