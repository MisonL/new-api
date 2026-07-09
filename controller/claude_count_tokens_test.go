package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCountClaudeTokensReturnsInputTokensWhenGlobalCountDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousCountToken := constant.CountToken
	constant.CountToken = false
	t.Cleanup(func() {
		constant.CountToken = previousCountToken
	})

	body := `{"model":"deepseek-v4-flash","max_tokens":1,"messages":[{"role":"user","content":"hello world"}]}`
	c, recorder := newClaudeCountTokensTestContext(body)

	CountClaudeTokens(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response claudeCountTokensResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Greater(t, response.InputTokens, 0)
	var raw map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &raw))
	require.Len(t, raw, 1)
	require.Contains(t, raw, "input_tokens")
}

func TestCountClaudeTokensRejectsTokenModelLimitMismatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"model":"deepseek-v4-flash","max_tokens":1,"messages":[{"role":"user","content":"hello world"}]}`
	c, recorder := newClaudeCountTokensTestContext(body)
	common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
	common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{
		"gpt-4o": true,
	})

	CountClaudeTokens(c)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), "not allowed")
}

func TestCountClaudeTokensAllowsMatchingTokenModelLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"model":"deepseek-v4-flash","max_tokens":1,"messages":[{"role":"user","content":"hello world"}]}`
	c, recorder := newClaudeCountTokensTestContext(body)
	common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
	common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{
		"deepseek-v4-flash": true,
	})

	CountClaudeTokens(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	response := decodeClaudeCountTokensResponse(t, recorder)
	require.Greater(t, response.InputTokens, 0)
}

func TestCountClaudeTokensAcceptsRequestWithoutMaxTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hello world"}]}`
	c, recorder := newClaudeCountTokensTestContext(body)

	CountClaudeTokens(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	response := decodeClaudeCountTokensResponse(t, recorder)
	require.Greater(t, response.InputTokens, 0)
}

func TestCountClaudeTokensCountsToolDefinitionsFromJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	baseBody := `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hello world"}]}`
	toolBody := `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hello world"}],"tools":[{"name":"lookup_order","description":"Find an order by id","input_schema":{"type":"object","properties":{"order_id":{"type":"string","description":"Order id"}},"required":["order_id"]}}]}`

	baseCtx, baseRecorder := newClaudeCountTokensTestContext(baseBody)
	CountClaudeTokens(baseCtx)
	require.Equal(t, http.StatusOK, baseRecorder.Code)
	baseResponse := decodeClaudeCountTokensResponse(t, baseRecorder)

	toolCtx, toolRecorder := newClaudeCountTokensTestContext(toolBody)
	CountClaudeTokens(toolCtx)
	require.Equal(t, http.StatusOK, toolRecorder.Code)
	toolResponse := decodeClaudeCountTokensResponse(t, toolRecorder)

	require.Greater(t, toolResponse.InputTokens, baseResponse.InputTokens)
}

func TestCountClaudeTokensCountsSystemAndContentBlocks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	baseBody := `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hello world"}]}`
	richBody := `{"model":"deepseek-v4-flash","system":[{"type":"text","text":"You answer with order status only."}],"messages":[{"role":"user","content":[{"type":"text","text":"hello world"},{"type":"tool_result","tool_use_id":"toolu_1","content":"order shipped"}]}]}`

	baseCtx, baseRecorder := newClaudeCountTokensTestContext(baseBody)
	CountClaudeTokens(baseCtx)
	require.Equal(t, http.StatusOK, baseRecorder.Code)
	baseResponse := decodeClaudeCountTokensResponse(t, baseRecorder)

	richCtx, richRecorder := newClaudeCountTokensTestContext(richBody)
	CountClaudeTokens(richCtx)
	require.Equal(t, http.StatusOK, richRecorder.Code)
	richResponse := decodeClaudeCountTokensResponse(t, richRecorder)

	require.Greater(t, richResponse.InputTokens, baseResponse.InputTokens)
}

func TestCountClaudeTokensCountsThinkingBlocks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	baseBody := `{"model":"deepseek-v4-flash","messages":[{"role":"assistant","content":[{"type":"text","text":"short"}]}]}`
	thinkingBody := `{"model":"deepseek-v4-flash","messages":[{"role":"assistant","content":[{"type":"text","text":"short"},{"type":"thinking","thinking":"expanded private reasoning for token counting","signature":"sig-thinking"},{"type":"redacted_thinking","data":"opaque-redacted-thinking-data","signature":"sig-redacted"}]}]}`

	baseCtx, baseRecorder := newClaudeCountTokensTestContext(baseBody)
	CountClaudeTokens(baseCtx)
	require.Equal(t, http.StatusOK, baseRecorder.Code)
	baseResponse := decodeClaudeCountTokensResponse(t, baseRecorder)

	thinkingCtx, thinkingRecorder := newClaudeCountTokensTestContext(thinkingBody)
	CountClaudeTokens(thinkingCtx)
	require.Equal(t, http.StatusOK, thinkingRecorder.Code)
	thinkingResponse := decodeClaudeCountTokensResponse(t, thinkingRecorder)

	require.Greater(t, thinkingResponse.InputTokens, baseResponse.InputTokens)
}

func TestCountClaudeTokensRejectsMissingTokenModelLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hello world"}]}`
	c, recorder := newClaudeCountTokensTestContext(body)
	common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)

	CountClaudeTokens(c)

	requireClaudeErrorResponse(t, recorder, http.StatusForbidden)
	require.Contains(t, recorder.Body.String(), "token has no model access")
}

func TestCountClaudeTokensRejectsInvalidTokenModelLimitContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hello world"}]}`
	c, recorder := newClaudeCountTokensTestContext(body)
	common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
	common.SetContextKey(c, constant.ContextKeyTokenModelLimit, "bad context value")

	CountClaudeTokens(c)

	requireClaudeErrorResponse(t, recorder, http.StatusForbidden)
	require.Contains(t, recorder.Body.String(), "configuration is invalid")
}

func TestCountClaudeTokensRejectsInvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, recorder := newClaudeCountTokensTestContext(`{"model":`)

	CountClaudeTokens(c)

	requireClaudeErrorResponse(t, recorder, http.StatusBadRequest)
}

func TestCountClaudeTokensRejectsMissingModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"messages":[{"role":"user","content":"hello world"}]}`
	c, recorder := newClaudeCountTokensTestContext(body)

	CountClaudeTokens(c)

	requireClaudeErrorResponse(t, recorder, http.StatusBadRequest)
	require.Contains(t, recorder.Body.String(), "field model is required")
}

func TestWriteClaudeCountTokensErrorKeepsRequestTooLargeStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousMaxRequestBodyMB := constant.MaxRequestBodyMB
	constant.MaxRequestBodyMB = 1
	t.Cleanup(func() {
		constant.MaxRequestBodyMB = previousMaxRequestBodyMB
	})
	c, recorder := newClaudeCountTokensTestContext(strings.Repeat("x", 1<<20+1))

	CountClaudeTokens(c)

	requireClaudeErrorResponse(t, recorder, http.StatusRequestEntityTooLarge)
	require.Contains(t, recorder.Body.String(), "request body too large")
}

func decodeClaudeCountTokensResponse(t *testing.T, recorder *httptest.ResponseRecorder) claudeCountTokensResponse {
	t.Helper()
	var response claudeCountTokensResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func requireClaudeErrorResponse(t *testing.T, recorder *httptest.ResponseRecorder, statusCode int) {
	t.Helper()
	require.Equal(t, statusCode, recorder.Code)
	var raw map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &raw))
	require.Equal(t, "error", raw["type"])
	errorBody, ok := raw["error"].(map[string]any)
	require.True(t, ok)
	require.NotEmpty(t, errorBody["type"])
	require.NotEmpty(t, errorBody["message"])
}

func newClaudeCountTokensTestContext(body string) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens?beta=true", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, recorder
}
