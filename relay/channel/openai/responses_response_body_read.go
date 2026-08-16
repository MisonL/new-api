package openai

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func readResponsesNonStreamingResponseBody(c *gin.Context, body io.Reader) ([]byte, *types.NewAPIError) {
	responseBody, err := io.ReadAll(body)
	if err != nil {
		return nil, newResponsesNonStreamingResponseBodyReadError(c, err)
	}
	return responseBody, nil
}

func newResponsesNonStreamingResponseBodyReadError(c *gin.Context, err error) *types.NewAPIError {
	if isRetryableResponsesNonStreamingResponseBodyRead(c, err) {
		return types.NewOpenAIError(
			err,
			types.ErrorCodeUpstreamTransportInterrupted,
			http.StatusBadGateway,
		)
	}
	return types.NewOpenAIError(
		err,
		types.ErrorCodeReadResponseBodyFailed,
		http.StatusInternalServerError,
		types.ErrOptionWithSkipRetry(),
	)
}

func isRetryableResponsesNonStreamingResponseBodyRead(c *gin.Context, err error) bool {
	if err == nil || c == nil || c.Request == nil || c.Request.Context().Err() != nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		return false
	}
	return types.IsUpstreamTransportInterruptedError(err)
}
