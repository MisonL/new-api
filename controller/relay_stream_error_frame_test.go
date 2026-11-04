package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relay/helper"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 流已经开始（已写出 SSE 帧）后再写错误：Claude 与 Gemini 分支此前直接
// c.JSON，Content-Type 仍是 text/event-stream，追加的裸 JSON 不带 data:
// 前缀，合规 SSE 客户端会忽略，错误信号丢失。
func TestWriteRelayErrorResponseUsesSSEFrameAfterStreamStarted(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newStartedStream := func() (*gin.Context, *httptest.ResponseRecorder) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
		helper.SetEventStreamHeaders(c)
		_, err := c.Writer.WriteString("event: message_start\ndata: {}\n\n")
		require.NoError(t, err)
		require.True(t, c.Writer.Written())
		return c, recorder
	}
	apiErr := types.NewOpenAIError(errors.New("upstream exploded"), types.ErrorCodeBadResponse, http.StatusBadGateway)

	t.Run("claude", func(t *testing.T) {
		c, recorder := newStartedStream()
		writeRelayErrorResponse(c, types.RelayFormatClaude, &relaycommon.RelayInfo{IsStream: true}, nil, apiErr)
		body := recorder.Body.String()
		require.Contains(t, body, "event: error", "Claude 客户端只认 SSE 错误帧")
		require.Contains(t, body, "data: ")
		require.False(t, strings.HasPrefix(strings.TrimSpace(body), "{\"type\":\"error\""),
			"不能追加裸 JSON")
	})

	t.Run("default-format-stream", func(t *testing.T) {
		c, recorder := newStartedStream()
		writeRelayErrorResponse(c, types.RelayFormatGemini, &relaycommon.RelayInfo{IsStream: true}, nil, apiErr)
		body := recorder.Body.String()
		require.Contains(t, body, "data: ", "流式响应里的错误必须以 SSE data 帧下发")
	})

	t.Run("non-stream-keeps-json", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
		writeRelayErrorResponse(c, types.RelayFormatClaude, &relaycommon.RelayInfo{IsStream: false}, nil, apiErr)
		require.Contains(t, recorder.Body.String(), "\"error\"")
	})
}
