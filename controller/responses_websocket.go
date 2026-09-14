package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const responsesWebSocketUnsupportedCode = "responses_websocket_not_supported"

// RelayResponsesWebSocketNotSupported rejects Responses WebSocket handshakes
// without upgrading or selecting an upstream channel.
func RelayResponsesWebSocketNotSupported(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": types.OpenAIError{
			Message: "Responses WebSocket mode is not supported by this gateway; use POST /v1/responses over HTTP or SSE",
			Type:    "invalid_request_error",
			Param:   "",
			Code:    responsesWebSocketUnsupportedCode,
		},
	})
	c.Abort()
}
