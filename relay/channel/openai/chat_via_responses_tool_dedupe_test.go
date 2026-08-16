package openai

import (
	"io"
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

func TestOaiResponsesToChatStreamHandlerDoesNotReemitCompletedFunctionCall(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"run_shell","arguments":""}}`,
		`data: {"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"command\":\"pwd\"}"}`,
		`data: {"type":"response.output_item.done","item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"run_shell","arguments":"{\"command\":\"pwd\"}"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_tool_once","object":"response","created_at":1710000000,"model":"gpt-5.6-sol","output":[{"id":"fc_1","type":"function_call","call_id":"call_1","name":"run_shell","arguments":"{\"command\":\"pwd\"}"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		RelayFormat:  types.RelayFormatOpenAI,
		StreamStatus: relaycommon.NewStreamStatus(),
		DisablePing:  true,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-5.6-sol",
		},
	}

	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 12, usage.PromptTokens)
	require.Equal(t, 3, usage.CompletionTokens)
	toolCalls := chatToolCallStreamChunks(t, recorder.Body.String())
	require.Len(t, toolCalls, 2)
	require.Equal(t, "call_1", toolCalls[0].ID)
	require.Equal(t, "run_shell", toolCalls[0].Function.Name)
	require.Empty(t, toolCalls[0].Function.Arguments)
	require.Equal(t, "call_1", toolCalls[1].ID)
	require.Empty(t, toolCalls[1].Function.Name)
	require.Equal(t, `{"command":"pwd"}`, toolCalls[1].Function.Arguments)
}

func chatToolCallStreamChunks(t *testing.T, output string) []dto.ToolCallResponse {
	t.Helper()
	toolCalls := make([]dto.ToolCallResponse, 0)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			continue
		}
		var chunk dto.ChatCompletionsStreamResponse
		require.NoError(t, common.Unmarshal([]byte(data), &chunk))
		for _, choice := range chunk.Choices {
			toolCalls = append(toolCalls, choice.Delta.ToolCalls...)
		}
	}
	return toolCalls
}
