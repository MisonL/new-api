package helper

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// A token limit above MaxInt32/2 is added to the pre-consume token estimate.
// Cast to int, a value such as 2^64-600 becomes negative, the estimate goes
// negative, the pre-consume quota is not positive, and PreConsumeQuota is
// skipped entirely while the request is still relayed and charged at
// settlement. Every request shape that feeds the estimate must reject it.
const oversizedTokenLimit = uint(math.MaxUint64 - 599)

func newValidationContext(t *testing.T, path, body string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	return ctx
}

func TestGetAndValidateTextRequestRejectsOversizedTokenLimits(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "max_completion_tokens",
			body: fmt.Sprintf(`{"model":"gpt-5.4","max_completion_tokens":%d,"messages":[{"role":"user","content":"hi"}]}`, oversizedTokenLimit),
			want: "max_completion_tokens is invalid",
		},
		{
			name: "max_tokens",
			body: fmt.Sprintf(`{"model":"gpt-5.4","max_tokens":%d,"messages":[{"role":"user","content":"hi"}]}`, oversizedTokenLimit),
			want: "max_tokens is invalid",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request, err := GetAndValidateTextRequest(newValidationContext(t, "/v1/chat/completions", tc.body), 0)
			require.Nil(t, request)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestGetAndValidateClaudeRequestRejectsOversizedMaxTokens(t *testing.T) {
	body := fmt.Sprintf(`{"model":"claude-sonnet-5","max_tokens":%d,"messages":[{"role":"user","content":"hi"}]}`, oversizedTokenLimit)
	request, err := GetAndValidateClaudeRequest(newValidationContext(t, "/v1/messages", body))
	require.Nil(t, request)
	require.ErrorContains(t, err, "max_tokens is invalid")
}

func TestGetAndValidateResponsesRequestRejectsOversizedMaxOutputTokens(t *testing.T) {
	body := fmt.Sprintf(`{"model":"gpt-5.4","max_output_tokens":%d,"input":"hi"}`, oversizedTokenLimit)
	request, err := GetAndValidateResponsesRequest(newValidationContext(t, "/v1/responses", body))
	require.Nil(t, request)
	require.ErrorContains(t, err, "max_output_tokens is invalid")
}

func TestGetAndValidateGeminiRequestRejectsOversizedMaxOutputTokens(t *testing.T) {
	body := fmt.Sprintf(`{"contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"maxOutputTokens":%d}}`, oversizedTokenLimit)
	request, err := GetAndValidateGeminiRequest(newValidationContext(t, "/v1beta/models/gemini-3.1-pro:generateContent", body))
	require.Nil(t, request)
	require.ErrorContains(t, err, "maxOutputTokens is invalid")
}

// A limit at or below the bound must still be accepted: the check must not
// reject ordinary large limits.
func TestTokenLimitValidationAcceptsValuesAtTheBound(t *testing.T) {
	atBound := uint(math.MaxInt32 / 2)
	body := fmt.Sprintf(`{"model":"gpt-5.4","max_completion_tokens":%d,"messages":[{"role":"user","content":"hi"}]}`, atBound)
	request, err := GetAndValidateTextRequest(newValidationContext(t, "/v1/chat/completions", body), 0)
	require.NoError(t, err)
	require.NotNil(t, request)
}
