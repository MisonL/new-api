package ollama

import (
	"bytes"
	"errors"
	"fmt"
	"io"
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

type trackingReadCloser struct {
	reader    io.Reader
	closeSeen bool
}

func (r *trackingReadCloser) Read(p []byte) (int, error) {
	return r.reader.Read(p)
}

func (r *trackingReadCloser) Close() error {
	r.closeSeen = true
	return nil
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

func TestOllamaChatHandlerPreservesToolCallsAndReasoning(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := `{"model":"qwen3","message":{"role":"assistant","content":"","thinking":"plan","tool_calls":[{"function":{"name":"lookup","arguments":{"q":"status"}}}]},"done":true,"done_reason":"stop","prompt_eval_count":2,"eval_count":3}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}

	usage, apiErr := ollamaChatHandler(c, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "qwen3"}}, resp)

	require.Nil(t, apiErr)
	require.Equal(t, 5, usage.TotalTokens)
	var converted dto.OpenAITextResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &converted))
	require.Equal(t, "plan", converted.Choices[0].Message.GetReasoningContent())
	calls, err := converted.Choices[0].Message.ParseToolCallsStrict()
	require.NoError(t, err)
	require.Len(t, calls, 1)
	require.Equal(t, "call_0", calls[0].ID)
	require.Equal(t, "lookup", calls[0].Function.Name)
	require.Equal(t, "tool_calls", converted.Choices[0].FinishReason)
}

func TestOllamaStreamHandlerEmitsToolCallsBeforeDone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := strings.Join([]string{
		`{"model":"qwen3","message":{"role":"assistant","content":"answer","thinking":"plan"},"done":false}`,
		`{"model":"qwen3","message":{"role":"assistant","tool_calls":[{"function":{"name":"lookup","arguments":{"q":"status"}}}]},"done":true,"done_reason":"stop","prompt_eval_count":2,"eval_count":3}`,
	}, "\n")
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}

	usage, apiErr := ollamaStreamHandler(c, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "qwen3"}}, resp)

	require.Nil(t, apiErr)
	require.Equal(t, 5, usage.TotalTokens)
	output := recorder.Body.String()
	require.Contains(t, output, `"reasoning_content":"plan"`)
	require.Contains(t, output, `"tool_calls":[`)
	require.Contains(t, output, `"id":"call_0"`)
	require.Contains(t, output, `"finish_reason":"tool_calls"`)
	require.Contains(t, output, "[DONE]")
}

func TestOllamaStreamHandlerAggregatesSplitToolArgumentsAndDoneContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := strings.Join([]string{
		`{"model":"qwen3","message":{"role":"assistant","tool_calls":[{"function":{"name":"lookup","arguments":"{\"q\":\"st"}}]},"done":false}`,
		`{"model":"qwen3","message":{"role":"assistant","tool_calls":[{"function":{"name":"lookup","arguments":"atus\"}"}}]},"done":false}`,
		`{"model":"qwen3","message":{"role":"assistant","content":"tail"},"done":true,"done_reason":"stop","prompt_eval_count":2,"eval_count":3}`,
	}, "\n")
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}

	usage, apiErr := ollamaStreamHandler(c, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "qwen3"}}, resp)

	require.Nil(t, apiErr)
	require.Equal(t, 5, usage.TotalTokens)
	output := recorder.Body.String()
	require.Contains(t, output, `"content":"tail"`)
	require.Contains(t, output, `"arguments":"{\"q\":\"status\"}"`)
	require.Contains(t, output, `"finish_reason":"tool_calls"`)
}

func TestOllamaChatHandlerReturnsUpstreamHTTP400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader(`{"error":"invalid model"}`)), Header: make(http.Header)}

	usage, apiErr := ollamaChatHandler(c, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "qwen3"}}, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.Contains(t, apiErr.Error(), "invalid model")
}

func TestOllamaStreamHandlerClosesBodyBeforeNilInfoReturn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := &trackingReadCloser{reader: strings.NewReader(`{"message":{"content":"unused"},"done":true}`)}
	resp := &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header)}

	_, apiErr := ollamaStreamHandler(c, nil, resp)

	require.NotNil(t, apiErr)
	require.True(t, body.closeSeen)
}

func TestOllamaChatHandlerClosesBodyWhenReadFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := &trackingReadCloser{reader: failingReader{}}
	resp := &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header)}

	_, apiErr := ollamaChatHandler(c, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "qwen3"}}, resp)

	require.NotNil(t, apiErr)
	require.True(t, body.closeSeen)
}

func TestOllamaStreamHandlerClosesBodyOnUpstreamError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := &trackingReadCloser{reader: strings.NewReader(`{"error":"invalid model"}`)}
	resp := &http.Response{StatusCode: http.StatusBadRequest, Body: body, Header: make(http.Header)}

	_, apiErr := ollamaStreamHandler(c, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "qwen3"}}, resp)

	require.NotNil(t, apiErr)
	require.True(t, body.closeSeen)
}

func TestOllamaHandlersBoundErrorAndNonStreamingBodies(t *testing.T) {
	oversized := bytes.Repeat([]byte("x"), int(maxOllamaResponseBodyBytes+1))
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "qwen3"}}

	streamRecorder := httptest.NewRecorder()
	streamContext, _ := gin.CreateTestContext(streamRecorder)
	_, streamErr := ollamaStreamHandler(streamContext, info, &http.Response{
		StatusCode: http.StatusBadGateway,
		Body:       io.NopCloser(bytes.NewReader(oversized)),
		Header:     make(http.Header),
	})
	require.Error(t, streamErr)
	require.Contains(t, streamErr.Error(), "响应体超过")

	chatRecorder := httptest.NewRecorder()
	chatContext, _ := gin.CreateTestContext(chatRecorder)
	_, chatErr := ollamaChatHandler(chatContext, info, &http.Response{
		StatusCode: http.StatusBadGateway,
		Body:       io.NopCloser(bytes.NewReader(oversized)),
		Header:     make(http.Header),
	})
	require.Error(t, chatErr)
	require.Contains(t, chatErr.Error(), "响应体超过")

	embeddingRecorder := httptest.NewRecorder()
	embeddingContext, _ := gin.CreateTestContext(embeddingRecorder)
	_, embeddingErr := ollamaEmbeddingHandler(embeddingContext, info, &http.Response{
		StatusCode: http.StatusBadGateway,
		Body:       io.NopCloser(bytes.NewReader(oversized)),
		Header:     make(http.Header),
	})
	require.Error(t, embeddingErr)
	require.Contains(t, embeddingErr.Error(), "响应体超过")
}

func TestOllamaStreamHandlerAcceptsLargeJSONLine(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	largeContent := strings.Repeat("x", 128<<10)
	body := strings.Join([]string{
		fmt.Sprintf(`{"model":"qwen3","message":{"role":"assistant","content":"%s"},"done":false}`, largeContent),
		`{"model":"qwen3","message":{"role":"assistant"},"done":true,"done_reason":"stop"}`,
	}, "\n")

	usage, apiErr := ollamaStreamHandler(c, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "qwen3"}}, &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	})

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Contains(t, recorder.Body.String(), "[DONE]")
}

func TestOllamaLineScannerHasFiniteExplicitLimit(t *testing.T) {
	scanner := newOllamaLineScanner(strings.NewReader(strings.Repeat("x", maxOllamaStreamLineBytes+1)))
	require.False(t, scanner.Scan())
	require.Error(t, scanner.Err())
}

func TestMergeOllamaToolCallsRejectsSparseHugeIndex(t *testing.T) {
	index := maxOllamaToolCallIndex + 1
	err := mergeOllamaToolCalls(new([]ollamaToolCallAccumulator), []OllamaToolCall{{
		Function: OllamaToolCallFunction{
			Index:     &index,
			Name:      "lookup",
			Arguments: map[string]any{"q": "status"},
		},
	}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds maximum")
}

func TestConvertOllamaToolCallsCompactsSparseIndexes(t *testing.T) {
	firstIndex := 2
	lastIndex := 5
	accumulators := make([]ollamaToolCallAccumulator, 0)
	require.NoError(t, mergeOllamaToolCalls(&accumulators, []OllamaToolCall{
		{Function: OllamaToolCallFunction{Index: &firstIndex, Name: "lookup", Arguments: map[string]any{"q": "status"}}},
		{Function: OllamaToolCallFunction{Index: &lastIndex, Name: "write", Arguments: map[string]any{"value": "ok"}}},
	}))

	converted, err := convertOllamaToolCalls(accumulators)
	require.NoError(t, err)
	require.Len(t, converted, 2)
	require.Equal(t, "lookup", converted[0].Function.Name)
	require.NotNil(t, converted[0].Index)
	require.Equal(t, 0, *converted[0].Index)
	require.Equal(t, "write", converted[1].Function.Name)
	require.NotNil(t, converted[1].Index)
	require.Equal(t, 1, *converted[1].Index)
}
