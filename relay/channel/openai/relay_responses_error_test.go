package openai

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"


)

type responsesErrorReadCloser struct {
	err error
}

func (r responsesErrorReadCloser) Read(_ []byte) (int, error) {
	return 0, r.err
}

func (r responsesErrorReadCloser) Close() error {
	return nil
}

func TestOaiResponsesStreamHandlerPreservesTopLevelFailedContextError(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.failed","error":{"message":"context length exceeded","type":"invalid_request_error","code":"context_length_exceeded"}}`,
			``,
		}, "\n"))),
	}
	info := &relaycommon.RelayInfo{
		DisablePing:  true,
		StreamStatus: relaycommon.NewStreamStatus(),
	}

	usage, apiErr := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.Equal(t, types.ErrorCode("context_length_exceeded"), apiErr.GetErrorCode())
	require.Empty(t, recorder.Body.String())
}

func TestResponsesOpenAIErrorStatusPrioritizesRateLimitOverContextMessage(t *testing.T) {
	error := &types.OpenAIError{
		Message: "request too large for the current rate limit window; token limit reached",
		Type:    "rate_limit_error",
		Code:    "rate_limit_exceeded",
	}

	require.Equal(t, http.StatusTooManyRequests, responsesOpenAIErrorStatus(http.StatusOK, error))
	normalized := normalizeResponsesUpstreamOpenAIError(error)
	require.Equal(t, "rate_limit_error", normalized.Type)

	parameterShapedRateLimit := &types.OpenAIError{
		Message: "request too large while rate limited",
		Type:    "rate_limit_error",
		Code:    "invalid_type",
	}
	require.Equal(t, http.StatusTooManyRequests, responsesOpenAIErrorStatus(http.StatusOK, parameterShapedRateLimit))
}

func TestOaiResponsesStreamHandlerPreservesContextErrorAfterKeepalive(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			": keepalive",
			"",
			`data: {"type":"response.failed","error":{"message":"context length exceeded","type":"invalid_request_error","code":"context_length_exceeded"}}`,
			"",
		}, "\n"))),
	}
	info := &relaycommon.RelayInfo{
		DisablePing:  true,
		StreamStatus: relaycommon.NewStreamStatus(),
	}

	usage, apiErr := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.Equal(t, types.ErrorCode("context_length_exceeded"), apiErr.GetErrorCode())
	require.Empty(t, recorder.Body.String())
}

func TestOaiResponsesHandlerPreservesTerminalSSEError(t *testing.T) {
	tests := []struct {
		name       string
		prefix     string
		event      string
		wantStatus int
		wantCode   types.ErrorCode
	}{
		{
			name:       "failed context limit error",
			prefix:     "data: ",
			event:      `{"type":"response.failed","error":{"message":"context length exceeded","type":"invalid_request_error","code":"context_length_exceeded"}}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   types.ErrorCode("context_length_exceeded"),
		},
		{
			name:       "rate limit error",
			prefix:     "data: ",
			event:      `{"type":"response.error","error":{"message":"rate limited","type":"rate_limit_error","code":"rate_limit_exceeded"}}`,
			wantStatus: http.StatusTooManyRequests,
			wantCode:   types.ErrorCode("rate_limit_exceeded"),
		},
		{
			name:       "server error",
			prefix:     "data: ",
			event:      `{"type":"response.error","error":{"message":"upstream unavailable","type":"server_error","code":"server_error"}}`,
			wantStatus: http.StatusBadGateway,
			wantCode:   types.ErrorCode("server_error"),
		},
		{
			name:       "event field supplies failed type",
			prefix:     "event: response.failed\ndata: ",
			event:      `{"error":{"message":"context length exceeded","type":"invalid_request_error","code":"context_length_exceeded"}}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   types.ErrorCode("context_length_exceeded"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(tt.prefix + tt.event + "\n\n")),
			}

			usage, apiErr := OaiResponsesHandler(c, nil, resp)

			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			require.Equal(t, tt.wantStatus, apiErr.StatusCode)
			require.Equal(t, tt.wantCode, apiErr.GetErrorCode())
			require.Empty(t, recorder.Body.String())
		})
	}
}

func TestResponsesSSETerminalErrorRecordsEventType(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	apiErr := responsesSSETerminalError(c, []byte("event: response.failed\ndata: {\"error\":{\"message\":\"bad\",\"type\":\"invalid_request_error\",\"param\":\"input\",\"code\":\"invalid_type\"}}\n\n"), http.StatusOK)
	require.NotNil(t, apiErr)
	require.Equal(t, "response.failed", common.GetContextKeyString(c, constant.ContextKeyResponsesErrorEvent))
	require.Equal(t, "input", apiErr.ToOpenAIError().Param)
}

func TestOaiResponsesHandlerNormalizesHTTP200ContextError(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`{
			"error": {
				"message": "context length exceeded",
				"type": "invalid_request_error",
				"code": "context_length_exceeded"
			}
		}`)),
	}

	usage, apiErr := OaiResponsesHandler(c, nil, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.Equal(t, types.ErrorCode("context_length_exceeded"), apiErr.GetErrorCode())
	require.Empty(t, recorder.Body.String())
}

func TestOaiResponsesHandlerRejectsNonTerminalSSEForNonStreamingRequest(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_non_stream\"}}\n\n",
		)),
	}

	usage, apiErr := OaiResponsesHandler(c, nil, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
	require.Equal(t, types.ErrorCodeBadResponseBody, apiErr.GetErrorCode())
	require.Empty(t, recorder.Body.String())
}

func TestResponsesSSETerminalErrorRequiresSSEPayloadPrefix(t *testing.T) {
	apiErr := responsesSSETerminalError(nil, []byte(strings.Join([]string{
		`{"unexpected":"payload"}`,
		`data: {"type":"response.error","error":{"message":"rate limited","type":"rate_limit_error","code":"rate_limit_exceeded"}}`,
	}, "\n")), http.StatusOK)

	require.Nil(t, apiErr)
}

func TestResponsesSSETerminalErrorAcceptsKeepAliveCommentPrefix(t *testing.T) {
	apiErr := responsesSSETerminalError(nil, []byte(strings.Join([]string{
		`: keepalive`,
		``,
		`event: response.failed`,
		`data: {"error":{"message":"context length exceeded","type":"invalid_request_error","code":"context_length_exceeded"}}`,
		``,
	}, "\n")), http.StatusOK)

	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.Equal(t, types.ErrorCode("context_length_exceeded"), apiErr.GetErrorCode())
}

func TestOaiResponsesCompactionHandlerPreservesTerminalSSEError(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.failed\",\"error\":{\"message\":\"context length exceeded\",\"type\":\"invalid_request_error\",\"code\":\"context_length_exceeded\"}}\n\n",
		)),
	}

	usage, apiErr := OaiResponsesCompactionHandler(c, nil, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.Equal(t, types.ErrorCode("context_length_exceeded"), apiErr.GetErrorCode())
	require.Empty(t, recorder.Body.String())
}

func TestOaiResponsesCompactionHandlerPreservesContextErrorAfterKeepalive(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			": keepalive",
			"",
			`data: {"type":"response.failed","error":{"message":"context length exceeded","type":"invalid_request_error","code":"context_length_exceeded"}}`,
			"",
		}, "\n"))),
	}

	usage, apiErr := OaiResponsesCompactionHandler(c, nil, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.Equal(t, types.ErrorCode("context_length_exceeded"), apiErr.GetErrorCode())
	require.Empty(t, recorder.Body.String())
}

func TestOaiSyntheticResponsesCompactionHandlerPreservesTerminalSSEError(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.failed\",\"error\":{\"message\":\"context length exceeded\",\"type\":\"invalid_request_error\",\"code\":\"context_length_exceeded\"}}\n\n",
		)),
	}

	usage, apiErr := OaiSyntheticResponsesCompactionHandler(c, nil, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.Equal(t, types.ErrorCode("context_length_exceeded"), apiErr.GetErrorCode())
	require.Empty(t, recorder.Body.String())
}

func TestOaiResponsesToChatHandlerPreservesTerminalSSEError(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.failed\",\"error\":{\"message\":\"context length exceeded\",\"type\":\"invalid_request_error\",\"code\":\"context_length_exceeded\"}}\n\n",
		)),
	}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"}}

	usage, apiErr := OaiResponsesToChatHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.Equal(t, types.ErrorCode("context_length_exceeded"), apiErr.GetErrorCode())
	require.Empty(t, recorder.Body.String())
}

func TestOaiResponsesToChatStreamHandlerPreservesTopLevelFailedContextError(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.failed\",\"error\":{\"message\":\"context length exceeded\",\"type\":\"invalid_request_error\",\"code\":\"context_length_exceeded\"}}\n\n",
		)),
	}
	info := &relaycommon.RelayInfo{
		DisablePing:  true,
		StreamStatus: relaycommon.NewStreamStatus(),
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"},
	}

	usage, apiErr := OaiResponsesToChatStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.Equal(t, types.ErrorCode("context_length_exceeded"), apiErr.GetErrorCode())
	require.Empty(t, recorder.Body.String())
}

func TestOaiResponsesHandlerClassifiesInterruptedBodyReadAsTransportFailure(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       responsesErrorReadCloser{err: io.ErrUnexpectedEOF},
	}

	usage, apiErr := OaiResponsesHandler(c, nil, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	require.Equal(t, types.ErrorCodeUpstreamTransportInterrupted, apiErr.GetErrorCode())
	require.False(t, types.IsSkipRetryError(apiErr))
	require.Empty(t, recorder.Body.String())
}

func TestOaiResponsesHandlerKeepsNonTransportBodyReadFailureExplicit(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       responsesErrorReadCloser{err: errors.New("response reader failed")},
	}

	usage, apiErr := OaiResponsesHandler(c, nil, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
	require.Equal(t, types.ErrorCodeReadResponseBodyFailed, apiErr.GetErrorCode())
	require.True(t, types.IsSkipRetryError(apiErr))
	require.Empty(t, recorder.Body.String())
}

func TestOaiResponsesHandlerClassifiesHTTP2InternalErrorAsTransportFailure(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       responsesErrorReadCloser{err: errors.New("stream error: stream ID 37; INTERNAL_ERROR; received from peer")},
	}

	usage, apiErr := OaiResponsesHandler(c, nil, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	require.Equal(t, types.ErrorCodeUpstreamTransportInterrupted, apiErr.GetErrorCode())
	require.False(t, types.IsSkipRetryError(apiErr))
	require.Empty(t, recorder.Body.String())
}

func TestOaiResponsesCompactionHandlerClassifiesInterruptedBodyReadAsTransportFailure(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       responsesErrorReadCloser{err: io.ErrUnexpectedEOF},
	}

	usage, apiErr := OaiResponsesCompactionHandler(c, nil, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	require.Equal(t, types.ErrorCodeUpstreamTransportInterrupted, apiErr.GetErrorCode())
	require.False(t, types.IsSkipRetryError(apiErr))
	require.Empty(t, recorder.Body.String())
}

func TestOaiResponsesCompactionHandlerKeepsNonTransportBodyReadFailureExplicit(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       responsesErrorReadCloser{err: errors.New("response reader failed")},
	}

	usage, apiErr := OaiResponsesCompactionHandler(c, nil, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
	require.Equal(t, types.ErrorCodeReadResponseBodyFailed, apiErr.GetErrorCode())
	require.True(t, types.IsSkipRetryError(apiErr))
	require.Empty(t, recorder.Body.String())
}

func TestOaiSyntheticResponsesCompactionHandlerClassifiesInterruptedBodyReadAsTransportFailure(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       responsesErrorReadCloser{err: io.ErrUnexpectedEOF},
	}

	usage, apiErr := OaiSyntheticResponsesCompactionHandler(c, nil, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	require.Equal(t, types.ErrorCodeUpstreamTransportInterrupted, apiErr.GetErrorCode())
	require.False(t, types.IsSkipRetryError(apiErr))
	require.Empty(t, recorder.Body.String())
}

func TestOaiResponsesToChatHandlerClassifiesInterruptedBodyReadAsTransportFailure(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       responsesErrorReadCloser{err: io.ErrUnexpectedEOF},
	}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"}}

	usage, apiErr := OaiResponsesToChatHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	require.Equal(t, types.ErrorCodeUpstreamTransportInterrupted, apiErr.GetErrorCode())
	require.False(t, types.IsSkipRetryError(apiErr))
	require.Empty(t, recorder.Body.String())
}

func TestOaiResponsesToChatStreamHandlerMakesPreCompletionInterruptionsExplicit(t *testing.T) {
	tests := []struct {
		name          string
		body          string
		wantCode      types.ErrorCode
		wantSkipRetry bool
		wantOutput    bool
		wantRetrySafe bool
	}{
		{
			name:          "empty upstream body",
			body:          "",
			wantCode:      types.ErrorCodeUpstreamTransportInterrupted,
			wantSkipRetry: false,
			wantOutput:    false,
			wantRetrySafe: true,
		},
		{
			name: "tool call before upstream interruption",
			body: strings.Join([]string{
				`data: {"type":"response.output_item.added","item":{"id":"fc_interrupted","type":"function_call","call_id":"call_interrupted","name":"lookup","arguments":"{}"}}`,
				``,
			}, "\n"),
			wantCode:      types.ErrorCodeUpstreamTransportInterrupted,
			wantSkipRetry: true,
			wantOutput:    true,
			wantRetrySafe: false,
		},
		{
			name: "done marker without response completed",
			body: strings.Join([]string{
				`data: {"type":"response.output_text.delta","delta":"partial"}`,
				`data: [DONE]`,
				``,
			}, "\n"),
			wantCode:      types.ErrorCodeBadResponseBody,
			wantSkipRetry: false,
			wantOutput:    true,
			wantRetrySafe: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(tt.body)),
			}
			info := &relaycommon.RelayInfo{
				DisablePing:  true,
				RelayFormat:  types.RelayFormatOpenAI,
				StreamStatus: relaycommon.NewStreamStatus(),
				ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"},
			}

			usage, apiErr := OaiResponsesToChatStreamHandler(c, info, resp)

			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
			require.Equal(t, tt.wantCode, apiErr.GetErrorCode())
			require.Equal(t, tt.wantSkipRetry, types.IsSkipRetryError(apiErr))
			require.Equal(t, tt.wantOutput, recorder.Body.Len() > 0)
			require.Equal(t, tt.wantRetrySafe, common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamPreludeRetrySafe))
			require.NotContains(t, recorder.Body.String(), `"finish_reason":"stop"`)
			require.NotContains(t, recorder.Body.String(), `"finish_reason":"tool_calls"`)
			require.NotContains(t, recorder.Body.String(), "[DONE]")
		})
	}
}

func TestOaiResponsesToChatStreamHandlerAcceptsCompletedEventWithoutDoneMarker(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_completed\",\"model\":\"gpt-5.6-sol\",\"usage\":{\"input_tokens\":3,\"output_tokens\":2,\"total_tokens\":5}}}\n\n",
		)),
	}
	info := &relaycommon.RelayInfo{
		DisablePing:  true,
		RelayFormat:  types.RelayFormatOpenAI,
		StreamStatus: relaycommon.NewStreamStatus(),
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"},
	}

	usage, apiErr := OaiResponsesToChatStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, 3, usage.PromptTokens)
	require.Equal(t, 2, usage.CompletionTokens)
	require.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
	require.Contains(t, recorder.Body.String(), `"finish_reason":"stop"`)
	require.Contains(t, recorder.Body.String(), "[DONE]")
}

func TestOaiResponsesToChatStreamHandlerRejectsOutputItemDoneBeforeTerminalResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.output_item.done","item":{"id":"fc_done","type":"function_call","call_id":"call_done","name":"lookup","arguments":"{}"}}`,
			``,
		}, "\n"))),
	}
	info := &relaycommon.RelayInfo{
		DisablePing:  true,
		RelayFormat:  types.RelayFormatOpenAI,
		StreamStatus: relaycommon.NewStreamStatus(),
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"},
	}

	usage, apiErr := OaiResponsesToChatStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	require.Equal(t, types.ErrorCodeUpstreamTransportInterrupted, apiErr.GetErrorCode())
	require.Contains(t, apiErr.Error(), "stream disconnected before completion")
	require.NotContains(t, recorder.Body.String(), `"finish_reason":"tool_calls"`)
	require.NotContains(t, recorder.Body.String(), "[DONE]")
}

func TestOaiResponsesToChatStreamHandlerRejectsOutputItemDoneFollowedByDoneMarker(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.output_item.done","item":{"id":"fc_done_marker","type":"function_call","call_id":"call_done_marker","name":"lookup","arguments":"{}"}}`,
			`data: [DONE]`,
			``,
		}, "\n"))),
	}
	info := &relaycommon.RelayInfo{
		DisablePing:  true,
		RelayFormat:  types.RelayFormatOpenAI,
		StreamStatus: relaycommon.NewStreamStatus(),
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-luna"},
	}

	usage, apiErr := OaiResponsesToChatStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	require.Equal(t, types.ErrorCodeBadResponseBody, apiErr.GetErrorCode())
	require.Contains(t, apiErr.Error(), "ended before response.completed")
	require.NotContains(t, recorder.Body.String(), `"finish_reason":"tool_calls"`)
	require.NotContains(t, recorder.Body.String(), "[DONE]")
}

func TestOaiResponsesToChatStreamHandlerMapsIncompleteTerminalReasons(t *testing.T) {
	tests := []struct {
		name          string
		terminalEvent string
		wantFinish    string
	}{
		{
			name:          "max output tokens",
			terminalEvent: `{"type":"response.incomplete","response":{"id":"resp_incomplete_length","model":"gpt-5.6-sol","incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}}`,
			wantFinish:    "length",
		},
		{
			name:          "content filter",
			terminalEvent: `{"type":"response.incomplete","response":{"id":"resp_incomplete_filter","model":"gpt-5.6-sol","incomplete_details":{"reason":"content_filter"},"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}}`,
			wantFinish:    "content_filter",
		},
		{
			name:          "legacy content filter reason",
			terminalEvent: `{"type":"response.incomplete","response":{"id":"resp_incomplete_legacy_filter","model":"gpt-5.6-luna","incomplete_details":{"reasoning":"content_filter"},"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}}`,
			wantFinish:    "content_filter",
		},
		{
			name:          "event type overrides conflicting response status",
			terminalEvent: `{"type":"response.incomplete","response":{"id":"resp_incomplete_conflicting_status","model":"gpt-5.6-sol","status":"completed","incomplete_details":{"reason":"content_filter"},"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}}`,
			wantFinish:    "content_filter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body: io.NopCloser(strings.NewReader(strings.Join([]string{
					`data: {"type":"response.output_text.delta","delta":"partial"}`,
					"data: " + tt.terminalEvent,
					``,
				}, "\n"))),
			}
			info := &relaycommon.RelayInfo{
				DisablePing:  true,
				RelayFormat:  types.RelayFormatOpenAI,
				StreamStatus: relaycommon.NewStreamStatus(),
				ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"},
			}

			usage, apiErr := OaiResponsesToChatStreamHandler(c, info, resp)

			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			require.Equal(t, 3, usage.PromptTokens)
			require.Equal(t, 2, usage.CompletionTokens)
			require.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
			require.Contains(t, recorder.Body.String(), `"content":"partial"`)
			require.Contains(t, recorder.Body.String(), `"finish_reason":"`+tt.wantFinish+`"`)
			require.Contains(t, recorder.Body.String(), "[DONE]")
		})
	}
}

func TestOaiResponsesToChatStreamHandlerMapsIncompleteWithoutResponseObject(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.output_text.delta","delta":"partial"}`,
			`data: {"type":"response.incomplete"}`,
			``,
		}, "\n"))),
	}
	info := &relaycommon.RelayInfo{
		DisablePing:  true,
		RelayFormat:  types.RelayFormatOpenAI,
		StreamStatus: relaycommon.NewStreamStatus(),
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"},
	}

	usage, apiErr := OaiResponsesToChatStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
	require.Contains(t, recorder.Body.String(), `"finish_reason":"length"`)
	require.Contains(t, recorder.Body.String(), "[DONE]")
}

func TestOaiResponsesToChatStreamHandlerAcceptsResponseDoneWithoutDoneMarker(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.output_text.delta","delta":"done"}`,
			`data: {"type":"response.done","response":{"id":"resp_done","model":"gpt-5.6-sol","status":"completed","usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}}`,
			``,
		}, "\n"))),
	}
	info := &relaycommon.RelayInfo{
		DisablePing:  true,
		RelayFormat:  types.RelayFormatOpenAI,
		StreamStatus: relaycommon.NewStreamStatus(),
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"},
	}

	usage, apiErr := OaiResponsesToChatStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
	require.Contains(t, recorder.Body.String(), `"finish_reason":"stop"`)
	require.Contains(t, recorder.Body.String(), "[DONE]")
}

func TestOaiResponsesStreamHandlerAcceptsIncompleteAndDoneTerminalEventsWithoutDoneMarker(t *testing.T) {
	tests := []struct {
		name          string
		terminalEvent string
		wantType      string
	}{
		{
			name:          "incomplete",
			terminalEvent: `{"type":"response.incomplete","response":{"id":"resp_native_incomplete","model":"gpt-5.6-sol","incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}}`,
			wantType:      "response.incomplete",
		},
		{
			name:          "done",
			terminalEvent: `{"type":"response.done","response":{"id":"resp_native_done","model":"gpt-5.6-sol","status":"completed","usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}}`,
			wantType:      "response.done",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader("data: " + tt.terminalEvent + "\n\n")),
			}
			info := &relaycommon.RelayInfo{
				DisablePing:  true,
				StreamStatus: relaycommon.NewStreamStatus(),
				ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"},
			}

			usage, apiErr := OaiResponsesStreamHandler(c, info, resp)

			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			require.Equal(t, 3, usage.PromptTokens)
			require.Equal(t, 2, usage.CompletionTokens)
			require.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
			require.Contains(t, recorder.Body.String(), `"type":"`+tt.wantType+`"`)
		})
	}
}

func TestOaiResponsesHandlerAllowsNonStreamingIncompleteResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := `{
		"id":"resp_incomplete_non_stream",
		"object":"response",
		"status":"incomplete",
		"incomplete_details":{"reason":"max_output_tokens"},
		"output":[{"id":"msg_partial","type":"message","role":"assistant","content":[{"type":"output_text","text":"partial"}]}],
		"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}
	}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	usage, apiErr := OaiResponsesHandler(c, nil, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, 3, usage.PromptTokens)
	require.Equal(t, 2, usage.CompletionTokens)
	require.JSONEq(t, body, stripUsageDetailObjects(recorder.Body.String()))
}

func TestOaiResponsesToChatHandlerMapsNonStreamingIncompleteResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"id":"resp_incomplete_chat",
			"object":"response",
			"status":"incomplete",
			"incomplete_details":{"reason":"content_filter"},
			"model":"gpt-5.6-sol",
			"output":[{"id":"msg_partial","type":"message","role":"assistant","content":[{"type":"output_text","text":"partial"}]}],
			"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}
		}`)),
	}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-sol"}}

	usage, apiErr := OaiResponsesToChatHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, 3, usage.PromptTokens)
	require.Equal(t, 2, usage.CompletionTokens)
	require.Contains(t, recorder.Body.String(), `"content":"partial"`)
	require.Contains(t, recorder.Body.String(), `"finish_reason":"content_filter"`)
}

func TestOaiResponsesHandlerRejectsIncompleteRemoteCompactionResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"id":"resp_incomplete_remote_compaction",
			"object":"response",
			"status":"incomplete",
			"incomplete_details":{"reason":"max_output_tokens"},
			"output":[{"type":"compaction","encrypted_content":"opaque"}],
			"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}
		}`)),
	}
	info := &relaycommon.RelayInfo{
		Request: &dto.OpenAIResponsesRequest{Input: []byte(`[{"type":"compaction_trigger"}]`)},
	}

	usage, apiErr := OaiResponsesHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	require.Contains(t, apiErr.Error(), "remote responses compaction response is incomplete")
	require.Empty(t, recorder.Body.String())
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactionOutput))
}

func TestOaiResponsesStreamHandlerRejectsNonCompletedRemoteCompactionTerminalEvents(t *testing.T) {
	tests := []struct {
		name          string
		terminalEvent string
		wantError     string
	}{
		{
			name:          "incomplete",
			terminalEvent: `{"type":"response.incomplete","response":{"id":"resp_incomplete_remote_compaction","status":"incomplete","output":[{"type":"compaction","encrypted_content":"opaque"}],"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}}`,
			wantError:     "instead of response.completed",
		},
		{
			name:          "done",
			terminalEvent: `{"type":"response.done","response":{"id":"resp_done_remote_compaction","status":"completed","output":[{"type":"compaction","encrypted_content":"opaque"}],"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}}`,
			wantError:     "instead of response.completed",
		},
		{
			name:          "completed event with incomplete response status",
			terminalEvent: `{"type":"response.completed","response":{"id":"resp_conflicting_remote_compaction","status":"incomplete","output":[{"type":"compaction","encrypted_content":"opaque"}],"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}}`,
			wantError:     "remote responses compaction response is incomplete",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader("data: " + tt.terminalEvent + "\n\n")),
			}
			info := &relaycommon.RelayInfo{
				Request:      &dto.OpenAIResponsesRequest{Input: []byte(`[{"type":"compaction_trigger"}]`)},
				StreamStatus: relaycommon.NewStreamStatus(),
				DisablePing:  true,
			}

			usage, apiErr := OaiResponsesStreamHandler(c, info, resp)

			require.Nil(t, usage)
			require.NotNil(t, apiErr)
			require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
			require.Contains(t, apiErr.Error(), tt.wantError)
			require.Empty(t, recorder.Body.String())
			require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactionOutput))
		})
	}
}
