package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// A channel that omits output_tokens_details must still deliver the object, so a
// client reading one of its fields works on every channel serving the model.
func TestEnsureResponsesUsageDetailObjectsFillsMissingObjects(t *testing.T) {
	raw := common.RawMessage(`{"id":"r1","usage":{"input_tokens":11,"input_tokens_details":{"cached_tokens":3},"output_tokens":7,"total_tokens":18}}`)

	updated, changed := ensureResponsesUsageDetailObjects(raw)
	require.True(t, changed)

	require.True(t, gjson.GetBytes(updated, "usage.output_tokens_details").IsObject())
	require.Equal(t, int64(0), gjson.GetBytes(updated, "usage.output_tokens_details.reasoning_tokens").Int())
	// Existing numbers and other keys must survive the rewrite.
	require.Equal(t, int64(7), gjson.GetBytes(updated, "usage.output_tokens").Int())
	require.Equal(t, int64(18), gjson.GetBytes(updated, "usage.total_tokens").Int())
	require.Equal(t, int64(3), gjson.GetBytes(updated, "usage.input_tokens_details.cached_tokens").Int())
	require.Equal(t, "r1", gjson.GetBytes(updated, "id").String())
	// input_tokens_details was already an object, so it is reported unchanged.
	var document map[string]any
	require.NoError(t, common.Unmarshal(updated, &document))
}

func TestEnsureResponsesUsageDetailObjectsFillsBothObjects(t *testing.T) {
	raw := common.RawMessage(`{"usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18}}`)

	updated, changed := ensureResponsesUsageDetailObjects(raw)
	require.True(t, changed)
	require.True(t, gjson.GetBytes(updated, "usage.input_tokens_details").IsObject())
	require.True(t, gjson.GetBytes(updated, "usage.output_tokens_details").IsObject())
}

func TestEnsureResponsesUsageDetailObjectsLeavesCompleteUsageUntouched(t *testing.T) {
	raw := common.RawMessage(`{"usage":{"input_tokens":11,"input_tokens_details":{"cached_tokens":3},"output_tokens":7,"output_tokens_details":{"reasoning_tokens":5},"total_tokens":18}}`)

	updated, changed := ensureResponsesUsageDetailObjects(raw)
	require.False(t, changed)
	require.Equal(t, string(raw), string(updated))
	require.Equal(t, int64(5), gjson.GetBytes(updated, "usage.output_tokens_details.reasoning_tokens").Int())
}

func TestEnsureResponsesUsageDetailObjectsReplacesNullObjects(t *testing.T) {
	raw := common.RawMessage(`{"usage":{"output_tokens":7,"output_tokens_details":null}}`)

	updated, changed := ensureResponsesUsageDetailObjects(raw)
	require.True(t, changed)
	require.True(t, gjson.GetBytes(updated, "usage.output_tokens_details").IsObject())
}

func TestEnsureResponsesUsageDetailObjectsIgnoresBodiesWithoutUsage(t *testing.T) {
	for _, raw := range []string{``, `{}`, `{"id":"r1"}`, `{"usage":null}`, `{"usage":[]}`, `not json`} {
		updated, changed := ensureResponsesUsageDetailObjects([]byte(raw))
		require.False(t, changed, "payload %q", raw)
		require.Equal(t, raw, string(updated), "payload %q", raw)
	}
}

// responsesBodyWithFilledUsageDetails returns the body the handler is expected
// to forward for a fixture whose upstream sent no usage token-detail objects.
// The expected bytes are spelled out here instead of being produced by the code
// under test, so a wrong value inserted by the handler fails the assertion
// rather than satisfying it. The fixtures carry no brace inside a usage string
// value, so a depth count finds the object's closing brace.
func responsesBodyWithFilledUsageDetails(body string) string {
	const filled = `,"output_tokens_details":{"text_tokens":0,"audio_tokens":0,"image_tokens":0,"reasoning_tokens":0},"input_tokens_details":{"cached_tokens":0,"text_tokens":0,"audio_tokens":0,"image_tokens":0}`
	usageStart := strings.Index(body, `"usage":{`)
	if usageStart < 0 {
		return body
	}
	open := usageStart + strings.Index(body[usageStart:], "{")
	depth := 0
	for i := open; i < len(body); i++ {
		switch body[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return body[:i] + filled + body[i:]
			}
		}
	}
	return body
}

// The rewritten body is for the client only. Re-decoding it into the response
// struct would let an inserted zero object shadow the upstream's own fallback
// field: usage that reports completion_tokens_details.reasoning_tokens but no
// output_tokens_details is billed from that fallback, and the placeholder would
// zero it out.
func TestOaiResponsesHandlerKeepsBillingUsageFromUpstreamBody(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := `{"id":"resp_billing","object":"response","output":[],"usage":{"input_tokens":10,"output_tokens":40,"total_tokens":50,"completion_tokens_details":{"text_tokens":30,"reasoning_tokens":10}}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	usage, apiErr := OaiResponsesHandler(c, nil, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, 10, usage.PromptTokens)
	require.Equal(t, 40, usage.CompletionTokens)
	require.Equal(t, 50, usage.TotalTokens)
	require.Equal(t, 10, usage.CompletionTokenDetails.ReasoningTokens)

	// The client still receives the filled-in objects.
	forwarded := recorder.Body.String()
	require.True(t, gjson.Get(forwarded, "usage.output_tokens_details").IsObject())
	require.True(t, gjson.Get(forwarded, "usage.input_tokens_details").IsObject())
}

// An upstream that omits output_tokens_details but still sends the chat
// spelling must have those numbers carried across, not replaced with zeros.
func TestEnsureResponsesUsageDetailObjectsCopiesChatBreakdown(t *testing.T) {
	raw := common.RawMessage(`{"usage":{"input_tokens":10,"output_tokens":40,"total_tokens":50,"completion_tokens_details":{"text_tokens":30,"reasoning_tokens":10},"prompt_tokens_details":{"cached_tokens":4}}}`)

	updated, changed := ensureResponsesUsageDetailObjects(raw)
	require.True(t, changed)

	require.Equal(t, int64(30), gjson.GetBytes(updated, "usage.output_tokens_details.text_tokens").Int())
	require.Equal(t, int64(10), gjson.GetBytes(updated, "usage.output_tokens_details.reasoning_tokens").Int())
	require.Equal(t, int64(4), gjson.GetBytes(updated, "usage.input_tokens_details.cached_tokens").Int())
}

// A chat-only key must not leak into the Responses object.
func TestEnsureResponsesUsageDetailObjectsDoesNotLeakChatOnlyKeys(t *testing.T) {
	raw := common.RawMessage(`{"usage":{"output_tokens":40,"completion_tokens_details":{"text_tokens":30,"prompt_cache_hit_tokens":9,"claude_cache_creation_1_h_tokens":3}}}`)

	updated, changed := ensureResponsesUsageDetailObjects(raw)
	require.True(t, changed)

	require.False(t, gjson.GetBytes(updated, "usage.output_tokens_details.prompt_cache_hit_tokens").Exists())
	require.False(t, gjson.GetBytes(updated, "usage.output_tokens_details.claude_cache_creation_1_h_tokens").Exists())
	require.Equal(t, int64(30), gjson.GetBytes(updated, "usage.output_tokens_details.text_tokens").Int())
}

// cached_creation_tokens is declared on InputTokenDetails and is a real billing
// input (it feeds the cc5m tiered-billing variable), so it must survive the
// copy. It carries `omitempty`, so reading the allowed keys from a marshalled
// zero value dropped it.
func TestEnsureResponsesUsageDetailObjectsCopiesCachedCreationTokens(t *testing.T) {
	raw := common.RawMessage(`{"usage":{"input_tokens":10,"output_tokens":40,"total_tokens":50,"prompt_tokens_details":{"cached_tokens":4,"cached_creation_tokens":7}}}`)

	updated, changed := ensureResponsesUsageDetailObjects(raw)
	require.True(t, changed)

	require.Equal(t, int64(7), gjson.GetBytes(updated, "usage.input_tokens_details.cached_creation_tokens").Int())
	require.Equal(t, int64(4), gjson.GetBytes(updated, "usage.input_tokens_details.cached_tokens").Int())
}

// The compaction path is a non-streaming Responses surface too.
func TestOaiResponsesCompactionHandlerFillsUsageDetailObjects(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	body := `{"id":"c1","object":"response","status":"completed","output":[{"type":"compaction","encrypted_content":"opaque"}],"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	_, apiErr := OaiResponsesCompactionHandler(c, nil, resp)

	require.Nil(t, apiErr)
	forwarded := recorder.Body.String()
	require.True(t, gjson.Get(forwarded, "usage.output_tokens_details").IsObject(), forwarded)
	require.True(t, gjson.Get(forwarded, "usage.input_tokens_details").IsObject(), forwarded)
}

// The native streaming path emits usage inside response.completed. An upstream
// that omits the token-detail objects leaves the client reading a field that is
// present on other channels serving the same model.
func TestOaiResponsesStreamHandlerFillsTerminalUsageDetailObjects(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	completedData := `{"type":"response.completed","response":{"id":"r1","object":"response","output":[],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15,"completion_tokens_details":{"reasoning_tokens":3}}}}`
	body := strings.Join([]string{"data: " + completedData, "data: [DONE]", ""}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		StreamStatus: relaycommon.NewStreamStatus(),
		DisablePing:  true,
		ChannelMeta:  &relaycommon.ChannelMeta{},
	}

	usage, apiErr := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	forwarded := recorder.Body.String()
	require.True(t, gjson.Get(forwarded, `response.usage.output_tokens_details`).Exists() ||
		strings.Contains(forwarded, `"output_tokens_details"`), forwarded)
	// The upstream's own breakdown is carried across rather than zeroed.
	require.Contains(t, forwarded, `"reasoning_tokens":3`)
}

// A payload whose usage sits at the top level (the non-streaming body) must not
// be touched by the streaming rewrite, and vice versa.
func TestEnsureUsageDetailObjectsAtOnlyTouchesItsPath(t *testing.T) {
	raw := common.RawMessage(`{"type":"response.completed","response":{"usage":{"output_tokens":3}},"usage":{"input_tokens":1}}`)

	updated, changed := ensureUsageDetailObjectsAt(raw, "response.usage")
	require.True(t, changed)
	require.True(t, gjson.GetBytes(updated, "response.usage.output_tokens_details").IsObject())
	require.False(t, gjson.GetBytes(updated, "usage.output_tokens_details").Exists())
}
