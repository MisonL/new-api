package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"

	"github.com/gin-gonic/gin"
)

func TestOaiResponsesHandlerMarksCompactionOutput(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := `{
		"id":"resp_compact_v2",
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

	usage, err := OaiResponsesHandler(c, nil, resp)

	require.Nil(t, err)
	require.Equal(t, 12, usage.PromptTokens)
	require.Equal(t, 3, usage.CompletionTokens)
	require.Equal(t, 15, usage.TotalTokens)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactionOutput))
	require.JSONEq(t, body, recorder.Body.String())
}

func TestOaiResponsesStreamHandlerMarksContextCompactionItemDone(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"type":"context_compaction","encrypted_content":"opaque"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_context_compact","object":"response","created_at":1710000000,"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-5.5",
		},
		StreamStatus: relaycommon.NewStreamStatus(),
		DisablePing:  true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.Equal(t, 12, usage.PromptTokens)
	require.Equal(t, 3, usage.CompletionTokens)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactionOutput))
}

func TestOaiResponsesHandlerAllowsNonStringCompactionEncryptedContent(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := `{
		"id":"resp_compact_v2",
		"object":"response",
		"created_at":1710000000,
		"output":[{"type":"compaction","encrypted_content":{"opaque":true}}],
		"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}
	}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	usage, err := OaiResponsesHandler(c, nil, resp)

	require.Nil(t, err)
	require.Equal(t, 12, usage.PromptTokens)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactionOutput))
	require.JSONEq(t, body, recorder.Body.String())
}

func TestOaiResponsesHandlerMarksContextCompactionOutput(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := `{
		"id":"resp_context_compact",
		"object":"response",
		"created_at":1710000000,
		"output":[{"type":"context_compaction","encrypted_content":"opaque"}],
		"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}
	}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	usage, err := OaiResponsesHandler(c, nil, resp)

	require.Nil(t, err)
	require.Equal(t, 12, usage.PromptTokens)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactionOutput))
	require.JSONEq(t, body, recorder.Body.String())
}
