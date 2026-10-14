package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// An upstream error body without a code field leaves OpenAIError.Code as a nil
// interface. Rendering it with %v produced the literal "<nil>" as the Claude
// error type, which the client cannot interpret.
func TestToClaudeErrorFallsBackWhenOpenAICodeIsNil(t *testing.T) {
	err := WithOpenAIError(OpenAIError{
		Message: "upstream rejected the request",
		Type:    "invalid_request_error",
	}, 400)

	require.Nil(t, err.ToOpenAIError().Code)

	claudeErr := err.ToClaudeError()
	require.NotEqual(t, "<nil>", claudeErr.Type)
	require.Equal(t, string(err.GetErrorCode()), claudeErr.Type)
	require.Equal(t, "upstream rejected the request", claudeErr.Message)
}

// A present upstream code must still reach the client unchanged.
func TestToClaudeErrorKeepsPresentOpenAICode(t *testing.T) {
	err := WithOpenAIError(OpenAIError{
		Message: "model not found",
		Type:    "invalid_request_error",
		Code:    "model_not_found",
	}, 404)

	claudeErr := err.ToClaudeError()
	require.Equal(t, "model_not_found", claudeErr.Type)
}
