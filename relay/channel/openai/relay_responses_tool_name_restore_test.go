package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const (
	responsesRestoreOriginal = "mcp.calendar/search.v1"
	responsesRestoreAlias    = "newapi_tool_mcp_calendar_search_v1_898acec239"
)

// buildRestorerForTest runs the real request-side normalization so the alias
// under test is the one the gateway actually generates, not a hand-written
// constant that could drift from dto.NormalizeToolName.
func buildRestorerForTest(t *testing.T) *relaycommon.ResponsesToolNameRestorer {
	t.Helper()
	request := dto.OpenAIResponsesRequest{
		Model: "gpt-5.4",
		Tools: []byte(`[{"type":"function","name":"` + responsesRestoreOriginal + `","parameters":{}}]`),
	}
	_, result, restorer, err := relaycommon.NormalizeResponsesToolNamesWithRestorer(request)
	require.NoError(t, err)
	require.Positive(t, result.ChangedCount)
	require.True(t, restorer.HasAliases())
	return restorer
}

func newResponsesRestoreContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	return ctx, recorder
}

func newResponsesRestoreInfo(restorer *relaycommon.ResponsesToolNameRestorer) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		RelayMode:                 relayconstant.RelayModeResponses,
		RelayFormat:               types.RelayFormatOpenAIResponses,
		ResponsesToolNameRestorer: restorer,
		ChannelMeta:               &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.4"},
	}
}

// Non-streaming: the body written to the client must carry the original name.
func TestOaiResponsesHandlerRestoresToolNames(t *testing.T) {
	ctx, recorder := newResponsesRestoreContext(t)
	info := newResponsesRestoreInfo(buildRestorerForTest(t))
	body := `{"id":"resp_1","object":"response","output":[{"type":"function_call","name":"` +
		responsesRestoreAlias + `","call_id":"call_1","arguments":"{}"}],"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	_, apiErr := OaiResponsesHandler(ctx, info, resp)
	require.Nil(t, apiErr)

	written := recorder.Body.String()
	require.Contains(t, written, responsesRestoreOriginal)
	require.NotContains(t, written, responsesRestoreAlias)
	// Fields the gateway does not model must survive the byte-level rewrite.
	require.Contains(t, written, `"call_id":"call_1"`)
}

// Streaming: the frame written to the client must carry the original name.
func TestOaiResponsesStreamHandlerRestoresToolNames(t *testing.T) {
	ctx, recorder := newResponsesRestoreContext(t)
	info := newResponsesRestoreInfo(buildRestorerForTest(t))
	info.IsStream = true
	frames := []string{
		`{"type":"response.output_item.added","item":{"type":"function_call","name":"` + responsesRestoreAlias + `","call_id":"call_1"}}`,
		`{"type":"response.function_call_arguments.delta","name":"` + responsesRestoreAlias + `","delta":"{}"}`,
		`{"type":"response.completed","response":{"id":"resp_1","output":[{"type":"function_call","name":"` + responsesRestoreAlias + `"}]}}`,
	}
	var builder strings.Builder
	for _, frame := range frames {
		builder.WriteString("event: message\ndata: " + frame + "\n\n")
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(builder.String())),
	}

	_, apiErr := OaiResponsesStreamHandler(ctx, info, resp)
	require.Nil(t, apiErr)

	written := recorder.Body.String()
	require.Contains(t, written, responsesRestoreOriginal)
	require.NotContains(t, written, responsesRestoreAlias)
}
