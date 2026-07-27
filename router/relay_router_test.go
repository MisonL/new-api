package router

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type countingRequestBody struct {
	reader io.Reader
	reads  int
}

func (b *countingRequestBody) Read(p []byte) (int, error) {
	b.reads++
	return b.reader.Read(p)
}

func (b *countingRequestBody) Close() error {
	return nil
}

func TestResponsesCompactIngressAliasesReachRelayAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, path := range []string{"/v1/responses/compact", "/responses/compact"} {
		t.Run(path, func(t *testing.T) {
			engine := gin.New()
			SetRelayRouter(engine)

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, path, nil)

			engine.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusUnauthorized, recorder.Code)
			require.Contains(t, recorder.Body.String(), "token.invalid")
		})
	}
}

func TestClaudeCountTokensRouteReachesRelayAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetRelayRouter(engine)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens?beta=true", nil)

	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.Contains(t, recorder.Body.String(), "token.invalid")
}

func TestLocalCodexResponsesCompactionRejectsUnauthenticatedWithoutReadingBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetRelayRouter(engine)

	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err := writer.Write([]byte(`{"model":"gpt-5.6-sol","input":"checkpoint"}`))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	body := &countingRequestBody{reader: bytes.NewReader(compressed.Bytes())}
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	request.Body = body
	request.ContentLength = -1
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Content-Encoding", "gzip")
	request.Header.Set("X-Codex-Turn-Metadata", `{"request_kind":"compaction","compaction":{"implementation":"responses"}}`)

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.Zero(t, body.reads)
	require.Equal(t, "gzip", request.Header.Get("Content-Encoding"))
}
