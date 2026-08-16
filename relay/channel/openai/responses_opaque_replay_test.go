package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"

	"github.com/gin-gonic/gin"
)

func TestOaiResponsesHandlerPreservesOpaqueFutureResponseBytes(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := `{
		"id":"resp_opaque_future",
		"object":"response",
		"output":[{"type":"reasoning","encrypted_content":{"version":2,"payload":[1,2,3]},"future_item":{"enabled":true}}],
		"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15},
		"future_top_level":{"enabled":true}
	}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	usage, apiErr := OaiResponsesHandler(c, nil, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, body, recorder.Body.String())
}

func TestOaiResponsesStreamHandlerPreservesOpaqueFutureDataPayloads(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	itemData := `{"type":"response.output_item.done","item":{"type":"reasoning","encrypted_content":{"version":2},"future_item":{"enabled":true}},"future_event":{"sequence":1}}`
	completedData := `{"type":"response.completed","response":{"id":"resp_opaque_future","object":"response","output":[{"type":"reasoning","encrypted_content":{"version":2},"future_item":{"enabled":true}}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15},"future_response":{"enabled":true}},"future_event":{"sequence":2}}`
	body := strings.Join([]string{
		"data: " + itemData,
		"data: " + completedData,
		"data: [DONE]",
		"",
	}, "\n")
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
	require.Contains(t, recorder.Body.String(), "data: "+itemData)
	require.Contains(t, recorder.Body.String(), "data: "+completedData)
}
