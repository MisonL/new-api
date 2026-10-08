package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
)

type responsesResponseBodyReadErrorReader struct {
	err error
}

func (r responsesResponseBodyReadErrorReader) Read([]byte) (int, error) {
	return 0, r.err
}

func TestResponsesNonStreamingHandlersClassifyResponseBodyReadErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handlers := []struct {
		name    string
		handler func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)
	}{
		{name: "responses", handler: OaiResponsesHandler},
		{name: "native compact", handler: OaiResponsesCompactionHandler},
		{name: "synthetic compact", handler: OaiSyntheticResponsesCompactionHandler},
		{name: "synthetic remote compact", handler: OaiSyntheticRemoteResponsesCompactionHandler},
		{name: "chat to responses", handler: OaiChatToResponsesHandler},
		{name: "responses to chat", handler: OaiResponsesToChatHandler},
	}
	testCases := []struct {
		name       string
		requestCtx context.Context
		readErr    error
		wantCode   types.ErrorCode
		wantStatus int
		wantSkip   bool
	}{
		{
			name:       "upstream transport interruption",
			requestCtx: context.Background(),
			readErr:    io.ErrUnexpectedEOF,
			wantCode:   types.ErrorCodeUpstreamTransportInterrupted,
			wantStatus: http.StatusBadGateway,
		},
		{
			name:       "http2 stream interruption",
			requestCtx: context.Background(),
			readErr:    http2.StreamError{StreamID: 1, Code: http2.ErrCodeInternal},
			wantCode:   types.ErrorCodeUpstreamTransportInterrupted,
			wantStatus: http.StatusBadGateway,
		},
		{
			name:       "http2 goaway interruption",
			requestCtx: context.Background(),
			readErr:    http2.GoAwayError{ErrCode: http2.ErrCodeInternal},
			wantCode:   types.ErrorCodeUpstreamTransportInterrupted,
			wantStatus: http.StatusBadGateway,
		},
		{
			name:       "client cancellation",
			requestCtx: cancelledContext(),
			readErr:    context.Canceled,
			wantCode:   types.ErrorCodeReadResponseBodyFailed,
			wantStatus: http.StatusInternalServerError,
			wantSkip:   true,
		},
		{
			name:       "request deadline",
			requestCtx: deadlineExceededContext(),
			readErr:    context.DeadlineExceeded,
			wantCode:   types.ErrorCodeReadResponseBodyFailed,
			wantStatus: http.StatusInternalServerError,
			wantSkip:   true,
		},
		{
			name:       "response body limit",
			requestCtx: context.Background(),
			readErr:    &http.MaxBytesError{Limit: 1},
			wantCode:   types.ErrorCodeReadResponseBodyFailed,
			wantStatus: http.StatusInternalServerError,
			wantSkip:   true,
		},
		{
			name:       "unknown reader failure",
			requestCtx: context.Background(),
			readErr:    errors.New("reader failed without transport evidence"),
			wantCode:   types.ErrorCodeReadResponseBodyFailed,
			wantStatus: http.StatusInternalServerError,
			wantSkip:   true,
		},
	}

	for _, handler := range handlers {
		handler := handler
		for _, testCase := range testCases {
			testCase := testCase
			t.Run(handler.name+"/"+testCase.name, func(t *testing.T) {
				recorder := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(recorder)
				ctx.Request = httptest.NewRequestWithContext(testCase.requestCtx, http.MethodPost, "/v1/responses", nil)
				resp := &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(responsesResponseBodyReadErrorReader{
						err: testCase.readErr,
					}),
				}

				usage, apiErr := handler.handler(ctx, nil, resp)

				require.Nil(t, usage)
				require.NotNil(t, apiErr)
				require.Equal(t, testCase.wantCode, apiErr.GetErrorCode())
				require.Equal(t, testCase.wantStatus, apiErr.StatusCode)
				require.Equal(t, testCase.wantSkip, types.IsSkipRetryError(apiErr))
				require.Empty(t, recorder.Body.String())
			})
		}
	}
}

func TestResponsesNonStreamingResponseBodyReadRequiresActiveRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name string
		ctx  *gin.Context
	}{
		{name: "nil context"},
		{name: "missing request", ctx: new(gin.Context)},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			apiErr := newResponsesNonStreamingResponseBodyReadError(testCase.ctx, io.ErrUnexpectedEOF)

			require.Equal(t, types.ErrorCodeReadResponseBodyFailed, apiErr.GetErrorCode())
			require.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
			require.True(t, types.IsSkipRetryError(apiErr))
		})
	}
}

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func deadlineExceededContext() context.Context {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	cancel()
	return ctx
}

// zeroByteReader yields an endless stream of zero bytes without allocating a
// backing buffer, so an oversized body can be simulated cheaply.
type zeroByteReader struct{}

func (zeroByteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// A non-streaming Responses body arrives from an upstream this gateway does
// not control. A response that never ends must fail the read explicitly rather
// than grow the process heap, and the failure must stay classified as a
// non-retryable read failure: the same upstream would answer the same way.
func TestReadResponsesNonStreamingResponseBodyRejectsOversizedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	body, apiErr := readResponsesNonStreamingResponseBody(
		ctx,
		io.LimitReader(zeroByteReader{}, maxResponsesNonStreamingBodyBytes+1),
	)

	require.Nil(t, body)
	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeReadResponseBodyFailed, apiErr.GetErrorCode())
	require.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
	require.True(t, types.IsSkipRetryError(apiErr))
	require.Contains(t, apiErr.Error(), "exceeds")
}

// Control case: a body within the limit is returned untouched, so the bound
// cannot silently truncate or reject a legitimate response.
func TestReadResponsesNonStreamingResponseBodyAcceptsBodyWithinLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	body, apiErr := readResponsesNonStreamingResponseBody(ctx, strings.NewReader(`{"id":"resp_1"}`))

	require.Nil(t, apiErr)
	require.Equal(t, `{"id":"resp_1"}`, string(body))
}
