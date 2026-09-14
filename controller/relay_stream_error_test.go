package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestWriteRelayErrorResponseEmitsResponsesErrorAfterPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	_, err := c.Writer.Write([]byte("payload"))
	require.NoError(t, err)

	info := &relaycommon.RelayInfo{
		IsStream:  true,
		RelayMode: relayconstant.RelayModeResponses,
	}
	streamErr := types.NewOpenAIError(errors.New("upstream rate limit"), types.ErrorCodeUpstreamTransportInterrupted, http.StatusBadGateway)

	writeRelayErrorResponse(c, types.RelayFormatOpenAI, info, (*websocket.Conn)(nil), streamErr)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), "event: error")
	// common.Marshal is free to choose a stable but non-struct map key order;
	// validate the protocol fields without coupling the test to that order.
	require.Contains(t, recorder.Body.String(), `"type":"error"`)
	require.Contains(t, recorder.Body.String(), `"error":`)
	require.Contains(t, recorder.Body.String(), "upstream rate limit")
}

func TestWriteRelayErrorResponseEmitsChatErrorAfterPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	_, err := c.Writer.Write([]byte("data: {\"id\":\"chatcmpl-1\"}\n\n"))
	require.NoError(t, err)

	info := &relaycommon.RelayInfo{
		IsStream:  true,
		RelayMode: relayconstant.RelayModeChatCompletions,
	}
	streamErr := types.NewOpenAIError(errors.New("upstream rate limit"), types.ErrorCodeUpstreamTransportInterrupted, http.StatusBadGateway)

	writeRelayErrorResponse(c, types.RelayFormatOpenAI, info, (*websocket.Conn)(nil), streamErr)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "event: error")
	require.Contains(t, recorder.Body.String(), `data: {"error":`)
	require.Contains(t, recorder.Body.String(), "upstream rate limit")
	require.Contains(t, recorder.Body.String(), "data: [DONE]")
}
