package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponsesCompactIngressAliasesReachRelayAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, path := range []string{"/v1/responses", "/responses", "/v1/responses/compact", "/responses/compact"} {
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

func TestResponsesWebSocketRoutesUseExplicitFailClosedHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetRelayRouter(engine)

	wantPaths := map[string]bool{
		"/v1/responses": false,
		"/responses":    false,
	}
	for _, route := range engine.Routes() {
		if route.Method != http.MethodGet {
			continue
		}
		if _, ok := wantPaths[route.Path]; !ok {
			continue
		}
		require.True(t, strings.HasSuffix(route.Handler, ".RelayResponsesWebSocketNotSupported"))
		wantPaths[route.Path] = true
	}

	for path, registered := range wantPaths {
		require.True(t, registered, "missing explicit Responses WebSocket route %s", path)
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
