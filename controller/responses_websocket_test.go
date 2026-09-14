package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRelayResponsesWebSocketNotSupportedFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	c.Request.Header.Set("Connection", "Upgrade")
	c.Request.Header.Set("Upgrade", "websocket")

	RelayResponsesWebSocketNotSupported(c)

	require.True(t, c.IsAborted())
	require.Equal(t, http.StatusNotImplemented, recorder.Code)
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	require.Empty(t, recorder.Header().Get("Upgrade"))

	var body struct {
		Error types.OpenAIError `json:"error"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, "invalid_request_error", body.Error.Type)
	require.Equal(t, responsesWebSocketUnsupportedCode, body.Error.Code)
	require.Contains(t, body.Error.Message, "POST /v1/responses")
}
