package helper

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type opaqueValidationBodyStorage struct {
	readCalls int
}

type opaqueValidationRequestBody struct {
	readCalls int
}

func (b *opaqueValidationRequestBody) Read(_ []byte) (int, error) {
	b.readCalls++
	return 0, errors.New("opaque request HTTP body must not be decoded during validation")
}

func (b *opaqueValidationRequestBody) Close() error {
	return nil
}

func (s *opaqueValidationBodyStorage) Read(_ []byte) (int, error) {
	s.readCalls++
	return 0, errors.New("opaque request body must not be decoded during validation")
}

func (s *opaqueValidationBodyStorage) Seek(_ int64, _ int) (int64, error) {
	return 0, nil
}

func (s *opaqueValidationBodyStorage) Close() error {
	return nil
}

func (s *opaqueValidationBodyStorage) Bytes() ([]byte, error) {
	return nil, errors.New("opaque request body must not be materialized during validation")
}

func (s *opaqueValidationBodyStorage) Size() int64 {
	return 1024
}

func (s *opaqueValidationBodyStorage) IsDisk() bool {
	return true
}

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

func TestGetAndValidateResponsesRequestRejectsMalformedAdditionalTools(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/responses",
		strings.NewReader(`{"model":"gpt-5.6-sol","input":[{"type":"additional_tools","tools":{"type":"function","name":"lookup"}}]}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	request, err := GetAndValidateResponsesRequest(ctx)

	require.Nil(t, request)
	require.ErrorContains(t, err, "additional_tools.tools must be an array")
}

func TestGetAndValidateRequestUsesOpaqueResponsesMetadataWithoutDecodingBody(t *testing.T) {
	previousGinMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(previousGinMode) })

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	requestBody := &opaqueValidationRequestBody{}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request.Body = requestBody
	ctx.Request.Header.Set("Content-Type", "application/json")

	stream := true
	storage := &opaqueValidationBodyStorage{}
	ctx.Set(common.KeyBodyStorage, storage)
	common.SetResponsesRequestMetadata(ctx, common.ResponsesRequestMetadata{
		Model:                     "gpt-5.6-sol",
		ModelPresent:              true,
		InputPresent:              true,
		InputKind:                 common.ResponsesJSONValueKindArray,
		Stream:                    &stream,
		HasOpaqueEncryptedContent: true,
		BodySize:                  storage.Size(),
	})

	request, err := GetAndValidateRequest(ctx, types.RelayFormatOpenAIResponses)

	require.NoError(t, err)
	opaqueRequest, ok := request.(interface{ IsOpaqueResponsesRequest() bool })
	require.True(t, ok)
	require.True(t, opaqueRequest.IsOpaqueResponsesRequest())
	require.Zero(t, storage.readCalls)
	require.Zero(t, requestBody.readCalls)
}

func TestGetAndValidateRequestUsesOpaqueResponsesCompactMetadataWithoutDecodingBody(t *testing.T) {
	previousGinMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(previousGinMode) })

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	requestBody := &opaqueValidationRequestBody{}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	ctx.Request.Body = requestBody
	ctx.Request.Header.Set("Content-Type", "application/json")

	storage := &opaqueValidationBodyStorage{}
	ctx.Set(common.KeyBodyStorage, storage)
	common.SetResponsesRequestMetadata(ctx, common.ResponsesRequestMetadata{
		Model:                     "gpt-5.6-sol",
		ModelPresent:              true,
		InputPresent:              true,
		InputKind:                 common.ResponsesJSONValueKindArray,
		HasOpaqueEncryptedContent: true,
		HasCompactionItem:         true,
		BodySize:                  storage.Size(),
	})

	request, err := GetAndValidateRequest(ctx, types.RelayFormatOpenAIResponsesCompaction)

	require.NoError(t, err)
	opaqueRequest, ok := request.(interface{ IsOpaqueResponsesRequest() bool })
	require.True(t, ok)
	require.True(t, opaqueRequest.IsOpaqueResponsesRequest())
	require.Zero(t, storage.readCalls)
	require.Zero(t, requestBody.readCalls)
}

func TestGetAndValidateResponsesRequestAcceptsOfficialToolSearchContinuation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/responses",
		strings.NewReader(`{
			"model":"gpt-5.6-sol",
			"input":[
				{"type":"tool_search_call","id":"tsc_search","call_id":"call_search","execution":"client","arguments":"{\"query\":\"calendar\"}"},
				{"type":"tool_search_output","id":"tso_search","call_id":"call_search","status":"completed","execution":"client","tools":[
					{"type":"namespace","name":"mcp__calendar","tools":[{"type":"function","name":"create_event","defer_loading":true,"parameters":{"type":"object"}}]}
				]}
			]
		}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	request, err := GetAndValidateResponsesRequest(ctx)

	require.NoError(t, err)
	effectiveTools, err := request.EffectiveTools()
	require.NoError(t, err)
	require.Len(t, effectiveTools, 1)
	hasToolSearch, err := relaycommon.ResponsesRequestHasToolSearch(request)
	require.NoError(t, err)
	require.True(t, hasToolSearch)
	hasNamespaceTools, err := relaycommon.ResponsesRequestHasNamespaceTools(request)
	require.NoError(t, err)
	require.True(t, hasNamespaceTools)
}

func TestGetAndValidateResponsesRequestRejectsMalformedToolSearchOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/responses",
		strings.NewReader(`{
			"model":"gpt-5.6-sol",
			"input":[{"type":"tool_search_output","call_id":"call_search","status":"completed","execution":"client","tools":{"type":"function","name":"create_event"}}]
		}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	request, err := GetAndValidateResponsesRequest(ctx)

	require.Nil(t, request)
	require.ErrorContains(t, err, "tool_search_output.tools must be an array")
}

func TestGetAndValidateResponsesCompactionRequestRejectsMalformedAdditionalTools(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/responses/compact",
		strings.NewReader(`{"model":"gpt-5.6-sol","input":[{"type":"additional_tools","tools":{"type":"function","name":"lookup"}}]}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	request, err := GetAndValidateResponsesCompactionRequest(ctx)

	require.Nil(t, request)
	require.ErrorContains(t, err, "additional_tools.tools must be an array")
}

func TestGetAndValidateResponsesCompactionRequestAcceptsOfficialToolSearchContinuation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/responses/compact",
		strings.NewReader(`{
			"model":"gpt-5.6-sol-openai-compact",
			"input":[
				{"type":"tool_search_call","id":"tsc_search","call_id":"call_search","execution":"client","arguments":"{\"query\":\"calendar\"}"},
				{"type":"tool_search_output","id":"tso_search","call_id":"call_search","status":"completed","execution":"client","tools":[
					{"type":"namespace","name":"mcp__calendar","tools":[{"type":"function","name":"create_event","defer_loading":true,"parameters":{"type":"object"}}]}
				]}
			]
		}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	request, err := GetAndValidateResponsesCompactionRequest(ctx)

	require.NoError(t, err)
	converted := request.ToResponsesRequest()
	effectiveTools, err := converted.EffectiveTools()
	require.NoError(t, err)
	require.Len(t, effectiveTools, 1)
	hasNamespaceTools, err := relaycommon.ResponsesRequestHasNamespaceTools(converted)
	require.NoError(t, err)
	require.True(t, hasNamespaceTools)
}

func TestGetAndValidateResponsesCompactionRequestRejectsMalformedToolSearchOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/responses/compact",
		strings.NewReader(`{
			"model":"gpt-5.6-sol-openai-compact",
			"input":[{"type":"tool_search_output","call_id":"call_search","status":"completed","execution":"client","tools":{"type":"function","name":"create_event"}}]
		}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	request, err := GetAndValidateResponsesCompactionRequest(ctx)

	require.Nil(t, request)
	require.ErrorContains(t, err, "tool_search_output.tools must be an array")
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
