package types

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// Clients route on the HTTP status and show `type` to users, so `type` has to
// come from the OpenAI vocabulary. InitOpenAIError used to write the raw
// internal code there, which is how "type": "bad_response_status_code" reached
// callers -- a value that exists nowhere in the OpenAI API.
func TestStandardErrorTypeMapsToOpenAIVocabulary(t *testing.T) {
	cases := []struct {
		name       string
		code       ErrorCode
		statusCode int
		want       string
	}{
		{name: "bad response status code", code: ErrorCodeBadResponseStatusCode, statusCode: http.StatusNotFound, want: "not_found_error"},
		{name: "get channel failed", code: ErrorCodeGetChannelFailed, statusCode: http.StatusServiceUnavailable, want: "server_error"},
		{name: "model not found", code: ErrorCodeModelNotFound, statusCode: http.StatusBadRequest, want: "invalid_request_error"},
		{name: "invalid request", code: ErrorCodeInvalidRequest, statusCode: http.StatusBadRequest, want: "invalid_request_error"},
		{name: "unauthorized", code: ErrorCodeChannelInvalidKey, statusCode: http.StatusUnauthorized, want: "authentication_error"},
		{name: "forbidden quota", code: ErrorCodeInsufficientUserQuota, statusCode: http.StatusForbidden, want: "insufficient_quota"},
		{name: "forbidden other", code: ErrorCodeAccessDenied, statusCode: http.StatusForbidden, want: "permission_error"},
		{name: "rate limited", code: ErrorCodeChannelConcurrencyExceeded, statusCode: http.StatusTooManyRequests, want: "rate_limit_error"},
		{name: "upstream failure", code: ErrorCodeDoRequestFailed, statusCode: http.StatusInternalServerError, want: "server_error"},
		{name: "client error default", code: ErrorCodeBadRequestBody, statusCode: http.StatusUnprocessableEntity, want: "invalid_request_error"},
		// A 5xx is never ambiguous, so the code must not pull it into the 4xx
		// vocabulary. The distributor answers 503 with ErrorCodeModelNotFound
		// when a model exists but has no free channel; labelling that
		// "not_found_error" made a client that routes on `type` stop retrying a
		// transient capacity failure.
		{name: "model not found on 503", code: ErrorCodeModelNotFound, statusCode: http.StatusServiceUnavailable, want: "server_error"},
		{name: "model not found on 500", code: ErrorCodeModelNotFound, statusCode: http.StatusInternalServerError, want: "server_error"},
		{name: "access denied on 503", code: ErrorCodeAccessDenied, statusCode: http.StatusServiceUnavailable, want: "server_error"},
		{name: "model price error on 502", code: ErrorCodeModelPriceError, statusCode: http.StatusBadGateway, want: "server_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, standardErrorType(tc.code, tc.statusCode))
		})
	}
}

// The internal code must survive in `code`; only `type` is remapped. Existing
// callers and logs match on the code.
func TestInitOpenAIErrorKeepsInternalCode(t *testing.T) {
	err := InitOpenAIError(ErrorCodeBadResponseStatusCode, http.StatusNotFound)

	openAIErr := err.ToOpenAIError()
	require.Equal(t, ErrorCodeBadResponseStatusCode, openAIErr.Code)
	require.Equal(t, "not_found_error", openAIErr.Type)
	require.NotEqual(t, openAIErr.Code, openAIErr.Type)
}

func TestNewOpenAIErrorUsesStandardType(t *testing.T) {
	err := NewOpenAIError(
		http.ErrHandlerTimeout,
		ErrorCodeGetChannelFailed,
		http.StatusServiceUnavailable,
	)

	openAIErr := err.ToOpenAIError()
	require.Equal(t, ErrorCodeGetChannelFailed, openAIErr.Code)
	require.Equal(t, "server_error", openAIErr.Type)
}

// StandardizeErrorType reads the code as a tiebreaker, but the code on an
// internally built error is an ErrorCode (a distinct named type), not a string.
// A plain string assertion would silently no-op and read the code as empty.
func TestStandardizeErrorTypeReadsErrorCodeTiebreaker(t *testing.T) {
	cases := []struct {
		name       string
		code       any
		statusCode int
		want       string
	}{
		{name: "error code type", code: ErrorCodeModelNotFound, statusCode: http.StatusBadRequest, want: "invalid_request_error"},
		{name: "plain string", code: "model_not_found", statusCode: http.StatusBadRequest, want: "invalid_request_error"},
		{name: "nil code stays status-only", code: nil, statusCode: http.StatusBadRequest, want: "invalid_request_error"},
		{name: "nil code forbidden", code: nil, statusCode: http.StatusForbidden, want: "permission_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A foreign type forces the tiebreaker branch.
			got := StandardizeErrorType(OpenAIError{Type: "ForeignSDKType", Code: tc.code}, tc.statusCode)
			require.Equal(t, tc.want, got.Type)
		})
	}
}

// Every internal error path must reach clients with a standard type, not just
// the ones built by InitOpenAIError/NewOpenAIError. NewErrorWithStatusCode backs
// request validation, auth rejections and the distributor, and it used to emit
// the internal marker "new_api_error" as the client-facing type.
func TestNewErrorWithStatusCodeUsesStandardType(t *testing.T) {
	cases := []struct {
		name       string
		code       ErrorCode
		statusCode int
		want       string
	}{
		{name: "validation", code: ErrorCodeInvalidRequest, statusCode: http.StatusBadRequest, want: "invalid_request_error"},
		{name: "unauthorized", code: ErrorCodeChannelInvalidKey, statusCode: http.StatusUnauthorized, want: "authentication_error"},
		{name: "rate limited", code: ErrorCodeChannelConcurrencyExceeded, statusCode: http.StatusTooManyRequests, want: "rate_limit_error"},
		{name: "upstream", code: ErrorCodeDoRequestFailed, statusCode: http.StatusBadGateway, want: "server_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := NewErrorWithStatusCode(errors.New("boom"), tc.code, tc.statusCode)

			openAIErr := err.ToOpenAIError()
			require.Equal(t, tc.want, openAIErr.Type)
			// The internal code is this repository's stable contract and must not
			// move when the client-facing type changes.
			require.Equal(t, tc.code, openAIErr.Code)
			require.Equal(t, "boom", openAIErr.Message)
		})
	}
}

// A message set after construction (the relay path appends the request id) must
// not disturb the standardized type.
func TestSetMessageKeepsStandardType(t *testing.T) {
	err := NewErrorWithStatusCode(errors.New("boom"), ErrorCodeInvalidRequest, http.StatusBadRequest)
	err.SetMessage("boom (request id: abc)")

	openAIErr := err.ToOpenAIError()
	require.Equal(t, "invalid_request_error", openAIErr.Type)
	require.Equal(t, "boom (request id: abc)", openAIErr.Message)
}
