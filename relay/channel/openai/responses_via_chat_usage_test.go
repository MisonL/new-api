package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// A chat upstream reports the output breakdown under completion_tokens_details,
// but a Responses client reads output_tokens_details. The bridge must emit the
// Responses spelling, otherwise the same model has a different usage shape on
// the bridged route than on a native Responses channel.
func TestBuildResponsesCompatUsageFillsOutputTokenDetails(t *testing.T) {
	usage := &dto.Usage{
		PromptTokens:     10,
		CompletionTokens: 40,
		TotalTokens:      50,
		CompletionTokenDetails: dto.OutputTokenDetails{
			TextTokens:      30,
			ReasoningTokens: 10,
		},
	}

	filled := buildResponsesCompatUsage(usage)

	require.NotNil(t, filled.OutputTokensDetails)
	require.Equal(t, 30, filled.OutputTokensDetails.TextTokens)
	require.Equal(t, 10, filled.OutputTokensDetails.ReasoningTokens)
	require.NotNil(t, filled.InputTokensDetails)
}

// An upstream that already speaks the Responses spelling must not be overwritten.
func TestBuildResponsesCompatUsageKeepsExistingOutputTokenDetails(t *testing.T) {
	existing := &dto.OutputTokenDetails{TextTokens: 7, ReasoningTokens: 3}
	usage := &dto.Usage{
		PromptTokens:        10,
		CompletionTokens:    40,
		TotalTokens:         50,
		OutputTokensDetails: existing,
		CompletionTokenDetails: dto.OutputTokenDetails{
			TextTokens: 999,
		},
	}

	filled := buildResponsesCompatUsage(usage)

	require.Same(t, existing, filled.OutputTokensDetails)
	require.Equal(t, 7, filled.OutputTokensDetails.TextTokens)
}

// End-to-end through the handler: the body the client receives carries both
// detail objects.
func TestOaiChatToResponsesHandlerEmitsBothUsageDetailObjects(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := `{"id":"c1","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	_, apiErr := OaiChatToResponsesHandler(c, nil, resp)

	require.Nil(t, apiErr)
	forwarded := recorder.Body.String()
	require.True(t, gjson.Get(forwarded, "usage.input_tokens_details").IsObject(), forwarded)
	require.True(t, gjson.Get(forwarded, "usage.output_tokens_details").IsObject(), forwarded)
}
