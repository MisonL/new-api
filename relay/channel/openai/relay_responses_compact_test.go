package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"

	"github.com/gin-gonic/gin"
)

func TestOaiResponsesCompactionHandlerNormalizesHTTP200ErrorBody(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"error": {
				"message": "Your input exceeds the context window of this model. Please adjust your input and try again.",
				"type": "invalid_request_error",
				"code": "context_too_large"
			}
		}`)),
	}

	usage, err := OaiResponsesCompactionHandler(c, resp)

	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, http.StatusBadRequest, err.StatusCode)
	require.Equal(t, "context_too_large", err.ToOpenAIError().Code)
	require.Empty(t, recorder.Body.String())
}

func TestResponsesCompactOpenAIErrorStatus(t *testing.T) {
	t.Parallel()

	require.Equal(t, http.StatusBadRequest, responsesCompactOpenAIErrorStatus(http.StatusOK, &types.OpenAIError{
		Message: "string_above_max_length for input item",
		Type:    "invalid_request_error",
	}))
	require.Equal(t, http.StatusBadGateway, responsesCompactOpenAIErrorStatus(http.StatusOK, &types.OpenAIError{
		Message: "provider returned malformed compact output",
		Type:    "server_error",
	}))
	require.Equal(t, http.StatusTooManyRequests, responsesCompactOpenAIErrorStatus(http.StatusTooManyRequests, &types.OpenAIError{
		Message: "rate limited",
		Type:    "rate_limit_error",
	}))
}
