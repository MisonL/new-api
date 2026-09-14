package types

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestIsUpstreamTransportInterruptedError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "unexpected eof sentinel",
			err:  io.ErrUnexpectedEOF,
			want: true,
		},
		{
			name: "http2 goaway",
			err:  errors.New("http2: server sent GOAWAY and closed the connection"),
			want: true,
		},
		{
			name: "unexpected eof message",
			err:  errors.New("unexpected EOF"),
			want: true,
		},
		{
			name: "connection reset by peer",
			err:  errors.New("read tcp 127.0.0.1:1->127.0.0.1:2: connection reset by peer"),
			want: true,
		},
		{
			name: "http2 response body closed",
			err:  errors.New("http2: response body closed"),
			want: true,
		},
		{
			name: "http2 stream internal error",
			err:  errors.New("stream error: stream ID 37; INTERNAL_ERROR; received from peer"),
			want: true,
		},
		{
			name: "http2 stream internal error lowercase",
			err:  errors.New("http2: stream error: stream ID 41; internal_error; received from peer"),
			want: true,
		},
		{
			name: "upstream internal error mentioning stream",
			err:  errors.New(`{"error":{"type":"internal_error","message":"stream request failed"}}`),
			want: false,
		},
		{
			name: "dial timeout",
			err:  errors.New("dial tcp timeout"),
			want: false,
		},
		{
			name: "nil",
			err:  nil,
			want: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.want, IsUpstreamTransportInterruptedError(tc.err))
		})
	}
}

func TestIsChannelErrorRecognizesConcurrencyErrorCodes(t *testing.T) {
	for _, code := range []ErrorCode{
		ErrorCodeChannelConcurrencyExceeded,
		ErrorCodeChannelConcurrencyUnavailable,
	} {
		err := NewError(errors.New("channel concurrency error"), code)
		require.True(t, IsChannelError(err), "error code %q should be classified as a channel error", code)
	}
}

func TestNewAPIErrorExposesOnlyMaskedErrorTextButPreservesCause(t *testing.T) {
	cause := errors.New(`upstream request failed: https://api.example.com/v1?token=query-secret`)
	err := NewErrorWithStatusCode(cause, ErrorCodeBadResponseStatusCode, 502)

	require.ErrorIs(t, err, cause)
	require.NotContains(t, err.Err.Error(), "api.example.com")
	require.NotContains(t, err.Err.Error(), "query-secret")
	require.NotContains(t, err.Error(), "api.example.com")
	require.NotContains(t, err.Error(), "query-secret")
}

func TestWithOpenAIErrorRedactsMessageParamAndMetadata(t *testing.T) {
	err := WithOpenAIError(OpenAIError{
		Message:  "upstream failed: Authorization: Bearer bearer-secret-123",
		Param:    "https://api.example.com/v1?token=param-secret",
		Code:     "server_error",
		Metadata: common.RawMessage(`{"url":"https://metadata.example.com/?api_key=metadata-secret","nested":"sk-live-secret123","https://metadata.example.com/?token=key-secret":"value"}`),
	}, 502)

	for _, value := range []string{
		"bearer-secret-123",
		"api.example.com",
		"param-secret",
		"metadata.example.com",
		"metadata-secret",
		"sk-live-secret123",
		"key-secret",
	} {
		require.NotContains(t, err.Error(), value)
		require.NotContains(t, err.Err.Error(), value)
	}

	encoded, marshalErr := common.Marshal(err.ToOpenAIError())
	require.NoError(t, marshalErr)
	for _, value := range []string{"bearer-secret-123", "api.example.com", "param-secret", "metadata.example.com", "metadata-secret", "sk-live-secret123", "key-secret"} {
		require.False(t, strings.Contains(string(encoded), value), "serialized error leaked %q: %s", value, encoded)
	}
}

func TestWithOpenAIErrorPreservesLargeMetadataNumbers(t *testing.T) {
	err := WithOpenAIError(OpenAIError{
		Message:  "upstream failed",
		Metadata: common.RawMessage(`{"request_id":9007199254740993,"nested":{"sequence":9223372036854775807}}`),
	}, http.StatusBadGateway)

	encoded, marshalErr := common.Marshal(err.ToOpenAIError())
	require.NoError(t, marshalErr)
	require.Contains(t, string(encoded), `9007199254740993`)
	require.Contains(t, string(encoded), `9223372036854775807`)
}
