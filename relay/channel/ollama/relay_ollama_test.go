package ollama

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFetchOllamaVersionAppliesExtraHeaders(t *testing.T) {
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
