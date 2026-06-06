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

func TestOaiResponsesCompactionHandlerPassesValidCompactionOutput(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	body := `{
		"id":"resp_compact",
		"object":"response",
		"created_at":1710000000,
		"output":[{"type":"compaction","encrypted_content":"opaque"}],
		"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}
	}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	usage, err := OaiResponsesCompactionHandler(c, resp)

	require.Nil(t, err)
	require.Equal(t, 12, usage.PromptTokens)
	require.Equal(t, 3, usage.CompletionTokens)
	require.Equal(t, 15, usage.TotalTokens)
	require.JSONEq(t, body, recorder.Body.String())
}

func TestOaiResponsesCompactionHandlerRejectsMalformedCompactionOutput(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"id":"resp_bad_compact",
			"object":"response",
			"output":[{"type":"message","content":[{"type":"output_text","text":"not compact"}]}]
		}`)),
	}

	usage, err := OaiResponsesCompactionHandler(c, resp)

	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, http.StatusBadGateway, err.StatusCode)
	require.Equal(t, types.ErrorCodeBadResponseBody, err.GetErrorCode())
	require.Contains(t, err.Error(), "malformed compact output")
	require.Empty(t, recorder.Body.String())
}

func TestOaiResponsesCompactionHandlerRejectsInvalidJSONAsMalformedCompactOutput(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`not json`)),
	}

	usage, err := OaiResponsesCompactionHandler(c, resp)

	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, http.StatusBadGateway, err.StatusCode)
	require.Equal(t, types.ErrorCodeBadResponseBody, err.GetErrorCode())
	require.Contains(t, err.Error(), "malformed compact output")
	require.Empty(t, recorder.Body.String())
}

func TestOaiResponsesCompactionHandlerRejectsNonStringEncryptedContent(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"id":"resp_bad_compact",
			"object":"response",
			"output":[{"type":"compaction","encrypted_content":{"opaque":true}}]
		}`)),
	}

	usage, err := OaiResponsesCompactionHandler(c, resp)

	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, http.StatusBadGateway, err.StatusCode)
	require.Equal(t, types.ErrorCodeBadResponseBody, err.GetErrorCode())
	require.Contains(t, err.Error(), "compaction output has no encrypted content")
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
