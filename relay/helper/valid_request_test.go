package helper

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetAndValidateResponsesCompactionRequestRequiresInput(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "missing input",
			body: `{"model":"gpt-5.4"}`,
			want: "input is required",
		},
		{
			name: "null input",
			body: `{"model":"gpt-5.4","input":null}`,
			want: "input is required",
		},
		{
			name: "empty array input",
			body: `{"model":"gpt-5.4","input":[]}`,
			want: "input must contain at least one item",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader(tc.body))
			ctx.Request.Header.Set("Content-Type", "application/json")

			request, err := GetAndValidateResponsesCompactionRequest(ctx)
			require.Nil(t, request)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestGetAndValidateResponsesCompactionRequestAcceptsNonEmptyInput(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/responses/compact",
		strings.NewReader(`{"model":"gpt-5.4","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"compact"}]}]}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	request, err := GetAndValidateResponsesCompactionRequest(ctx)
	require.NoError(t, err)
	require.Equal(t, "gpt-5.4", request.Model)
	require.NotEmpty(t, request.Input)
}

func TestGetAndValidateResponsesCompactionRequestPreservesSub2APICompatibleFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := `{
		"model":"gpt-5.5",
		"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"compact"}]}],
		"instructions":"keep useful state",
		"tools":[{"type":"function","name":"edit"}],
		"parallel_tool_calls":true,
		"reasoning":{"effort":"high","summary":"auto"},
		"service_tier":"flex",
		"text":{"verbosity":"low"},
		"previous_response_id":"resp_previous",
		"store":false,
		"stream":true,
		"prompt_cache_key":"codex-cache-key"
	}`
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	request, err := GetAndValidateResponsesCompactionRequest(ctx)
	require.NoError(t, err)
	require.Equal(t, "gpt-5.5", request.Model)
	require.JSONEq(t, `[{"type":"function","name":"edit"}]`, string(request.Tools))
	require.JSONEq(t, `true`, string(request.ParallelToolCalls))
	require.NotNil(t, request.Reasoning)
	require.Equal(t, "high", request.Reasoning.Effort)
	require.Equal(t, "auto", request.Reasoning.Summary)
	require.Equal(t, "flex", request.ServiceTier)
	require.JSONEq(t, `{"verbosity":"low"}`, string(request.Text))
	require.JSONEq(t, `"codex-cache-key"`, string(request.PromptCacheKey))
	require.Equal(t, "resp_previous", request.PreviousResponseID)
	require.JSONEq(t, `false`, string(request.Store))
	require.NotNil(t, request.Stream)
	require.True(t, *request.Stream)

	responsesRequest := request.ToResponsesRequest()
	require.JSONEq(t, string(request.Input), string(responsesRequest.Input))
	require.JSONEq(t, string(request.Instructions), string(responsesRequest.Instructions))
	require.JSONEq(t, string(request.Tools), string(responsesRequest.Tools))
	require.JSONEq(t, string(request.ParallelToolCalls), string(responsesRequest.ParallelToolCalls))
	require.Equal(t, request.Reasoning, responsesRequest.Reasoning)
	require.Equal(t, "flex", responsesRequest.ServiceTier)
	require.JSONEq(t, string(request.Text), string(responsesRequest.Text))
	require.JSONEq(t, string(request.PromptCacheKey), string(responsesRequest.PromptCacheKey))
	require.Equal(t, "resp_previous", responsesRequest.PreviousResponseID)
	require.JSONEq(t, `false`, string(responsesRequest.Store))
	require.NotNil(t, responsesRequest.Stream)
	require.True(t, *responsesRequest.Stream)

	raw, err := common.Marshal(responsesRequest)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"service_tier":"flex"`)
	require.Contains(t, string(raw), `"prompt_cache_key":"codex-cache-key"`)
	require.Contains(t, string(raw), `"store":false`)
	require.Contains(t, string(raw), `"stream":true`)
}

func TestResponsesCompactionRequestKeepsKnownFieldsFilterableAfterConversion(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := `{
		"model":"gpt-5.5",
		"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"compact"}]}],
		"service_tier":"flex",
		"store":true,
		"safety_identifier":"user-123",
		"stream_options":{"include_obfuscation":false},
		"sub2api_session_id":"session-compact"
	}`
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	request, err := GetAndValidateResponsesCompactionRequest(ctx)
	require.NoError(t, err)
	responsesRequest := request.ToResponsesRequest()
	require.NotNil(t, responsesRequest)
	require.Contains(t, responsesRequest.Extra, "sub2api_session_id")

	raw, err := common.Marshal(responsesRequest)
	require.NoError(t, err)
	filtered, err := relaycommon.RemoveDisabledFields(raw, dto.ChannelOtherSettings{DisableStore: true}, false)
	require.NoError(t, err)

	require.NotContains(t, string(filtered), `"service_tier"`)
	require.NotContains(t, string(filtered), `"store"`)
	require.NotContains(t, string(filtered), `"safety_identifier"`)
	require.NotContains(t, string(filtered), `"include_obfuscation"`)
	require.Contains(t, string(filtered), `"sub2api_session_id":"session-compact"`)
}

func TestResponsesCompactionTokenMetaExcludesPromptCacheKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := `{
		"model":"gpt-5.5",
		"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"compact"}]}],
		"prompt_cache_key":"codex-cache-key"
	}`
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	request, err := GetAndValidateResponsesCompactionRequest(ctx)
	require.NoError(t, err)
	meta := request.GetTokenCountMeta()
	require.NotNil(t, meta)
	require.Contains(t, meta.CombineText, "compact")
	require.NotContains(t, meta.CombineText, "codex-cache-key")
}
