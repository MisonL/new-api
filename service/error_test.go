package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestRelayErrorHandlerClassifiesHTML413(t *testing.T) {
	t.Parallel()

	resp := &http.Response{
		StatusCode: http.StatusRequestEntityTooLarge,
		Body:       io.NopCloser(strings.NewReader("<html><body>413 Request Entity Too Large</body></html>")),
	}

	err := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, err)
	require.Equal(t, http.StatusRequestEntityTooLarge, err.StatusCode)
	require.Equal(t, types.ErrorCodeUpstreamRequestTooLarge, err.GetErrorCode())
	require.Contains(t, err.Error(), "upstream request too large")
}

func TestRelayErrorHandlerClassifiesJSON413WithoutDroppingUpstreamMessage(t *testing.T) {
	t.Parallel()

	resp := &http.Response{
		StatusCode: http.StatusRequestEntityTooLarge,
		Body: io.NopCloser(strings.NewReader(`{
			"error": {
				"message": "context window exceeded",
				"type": "invalid_request_error",
				"code": "context_length_exceeded"
			}
		}`)),
	}

	err := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, err)
	require.Equal(t, http.StatusRequestEntityTooLarge, err.StatusCode)
	require.Equal(t, types.ErrorCodeUpstreamRequestTooLarge, err.GetErrorCode())
	require.Contains(t, err.Error(), "context window exceeded")
	require.Equal(t, "context_length_exceeded", err.ToOpenAIError().Code)
}

func TestResetStatusCode(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		statusCode       int
		statusCodeConfig string
		expectedCode     int
	}{
		{
			name:             "map string value",
			statusCode:       429,
			statusCodeConfig: `{"429":"503"}`,
			expectedCode:     503,
		},
		{
			name:             "map int value",
			statusCode:       429,
			statusCodeConfig: `{"429":503}`,
			expectedCode:     503,
		},
		{
			name:             "skip invalid string value",
			statusCode:       429,
			statusCodeConfig: `{"429":"bad-code"}`,
			expectedCode:     429,
		},
		{
			name:             "skip status code 200",
			statusCode:       200,
			statusCodeConfig: `{"200":503}`,
			expectedCode:     200,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			newAPIError := &types.NewAPIError{
				StatusCode: tc.statusCode,
			}
			ResetStatusCode(newAPIError, tc.statusCodeConfig)
			require.Equal(t, tc.expectedCode, newAPIError.StatusCode)
		})
	}
}
