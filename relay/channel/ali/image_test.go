package ali

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAliTaskHTTPStatusRetryClassification(t *testing.T) {
	tests := []struct {
		statusCode int
		retryable  bool
	}{
		{statusCode: http.StatusBadRequest, retryable: false},
		{statusCode: http.StatusUnauthorized, retryable: false},
		{statusCode: http.StatusRequestTimeout, retryable: true},
		{statusCode: http.StatusTooEarly, retryable: true},
		{statusCode: http.StatusTooManyRequests, retryable: true},
		{statusCode: http.StatusBadGateway, retryable: true},
	}

	for _, tt := range tests {
		err := &aliTaskPollError{
			err:        errors.New("poll failed"),
			statusCode: tt.statusCode,
			retryable:  isAliTaskHTTPStatusRetryable(tt.statusCode),
		}
		require.Equal(t, tt.retryable, isAliTaskPollRetryable(err), "status %d", tt.statusCode)
	}
}

func TestAliTaskPollRetryClassificationKeepsWrappedTransportErrors(t *testing.T) {
	err := &aliTaskPollError{err: errors.New("connection reset"), retryable: true}
	require.True(t, isAliTaskPollRetryable(errors.Join(errors.New("wrapper"), err)))
	require.ErrorIs(t, err, err.err)
}
