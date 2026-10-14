package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// /v1/completions answered a plain-text upstream 404 with
// "message":"openai_error","type":"bad_response_status_code". The message was an
// internal sentinel and the type came from this gateway's own error-code
// vocabulary, so the caller learned nothing actionable.
func TestRelayErrorHandlerPlainTextBodyGetsActionableError(t *testing.T) {
	t.Parallel()

	resp := &http.Response{
		StatusCode: http.StatusNotFound,
		Body:       io.NopCloser(strings.NewReader("404 page not found")),
	}

	err := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, err)
	require.Equal(t, http.StatusNotFound, err.StatusCode)

	openAIErr := err.ToOpenAIError()
	require.Equal(t, "not_found_error", openAIErr.Type)
	require.NotEqual(t, "openai_error", openAIErr.Message)
	require.NotEqual(t, "bad_response_status_code", openAIErr.Type)
	require.Contains(t, openAIErr.Message, "404")
}

// An upstream that speaks a different error vocabulary must not leak its `type`
// through to the client.
func TestRelayErrorHandlerStandardizesUpstreamErrorType(t *testing.T) {
	t.Parallel()

	body := `{"error":{"message":"litellm.ServiceUnavailableError: OpenAIException - no available server","type":"internal_server_error","code":"503"}}`
	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	err := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, err)
	openAIErr := err.ToOpenAIError()
	require.Equal(t, "server_error", openAIErr.Type)
	// The upstream message text is preserved; only the type vocabulary changes.
	require.Contains(t, openAIErr.Message, "no available server")
}

// A structured upstream error keeps its own code.
func TestRelayErrorHandlerKeepsUpstreamCodeWithStandardType(t *testing.T) {
	t.Parallel()

	body := `{"error":{"message":"The model does not exist","type":"invalid_request_error","code":"model_not_found"}}`
	resp := &http.Response{
		StatusCode: http.StatusNotFound,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	err := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, err)
	openAIErr := err.ToOpenAIError()
	require.Equal(t, "model_not_found", openAIErr.Code)
	require.Equal(t, "invalid_request_error", openAIErr.Type)
}

// ClaudeErrorWrapper builds an Anthropic-surface error body. Its type must come
// from the Anthropic vocabulary; the internal "new_api_error" marker appears in
// no Anthropic client's switch statement.
func TestClaudeErrorWrapperUsesAnthropicType(t *testing.T) {
	err := ClaudeErrorWrapper(errors.New("upstream refused the request"), "", http.StatusBadGateway)

	require.NotNil(t, err)
	require.Equal(t, "api_error", err.Error.Type)
	require.NotEqual(t, "new_api_error", err.Error.Type)
	require.Equal(t, http.StatusBadGateway, err.StatusCode)
}
