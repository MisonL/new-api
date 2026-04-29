package hailuo

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/require"
)

func TestBuildVideoURLAppliesRuntimeHeaders(t *testing.T) {
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
