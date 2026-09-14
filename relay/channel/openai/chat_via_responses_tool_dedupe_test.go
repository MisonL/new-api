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

func TestOaiResponsesToChatStreamHandlerNormalizesNamespacedInvalidToolName(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.added","item":{"id":"fc_namespace_invalid","type":"function_call","call_id":"call_namespace_invalid","namespace":"mcp.calendar/v1","name":"search.tool","arguments":""}}`,
		`data: {"type":"response.completed","response":{"id":"resp_namespace_invalid","object":"response","created_at":1710000000,"model":"gpt-5.6-luna","output":[{"id":"fc_namespace_invalid","type":"function_call","call_id":"call_namespace_invalid","namespace":"mcp.calendar/v1","name":"search.tool","arguments":"{\"q\":\"hello\"}"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-luna"},
	}

	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	calls := chatToolCallStreamChunks(t, recorder.Body.String())
	require.Len(t, calls, 2)
	want := dto.NormalizeToolName("mcp.calendar/v1__search.tool")
	require.Equal(t, want, calls[0].Function.Name)
	require.Regexp(t, `^[a-zA-Z0-9_-]+$`, calls[0].Function.Name)
	require.Equal(t, `{"q":"hello"}`, calls[1].Function.Arguments)
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

func TestOaiResponsesToChatStreamHandlerBuffersArgumentsBeforeOutputItemAdded(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := strings.Join([]string{
		`data: {"type":"response.function_call_arguments.delta","item_id":"fc_pre","output_index":0,"delta":"{\"command\":\"pwd\"}"}`,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"id":"fc_pre","type":"function_call","call_id":"call_pre","name":"run_shell","arguments":""}}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"id":"fc_pre","type":"function_call","call_id":"call_pre","name":"run_shell","arguments":"{\"command\":\"pwd\"}"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_tool_reordered","object":"response","created_at":1710000000,"model":"gpt-5.6-luna","output":[{"id":"fc_pre","type":"function_call","call_id":"call_pre","name":"run_shell","arguments":"{\"command\":\"pwd\"}"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
			UpstreamModelName: "gpt-5.6-luna",
		},
	}

	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	calls := chatToolCallStreamChunks(t, recorder.Body.String())
	require.Len(t, calls, 1)
	require.Equal(t, "call_pre", calls[0].ID)
	require.Equal(t, "run_shell", calls[0].Function.Name)
	require.Equal(t, `{"command":"pwd"}`, calls[0].Function.Arguments)
	require.NotContains(t, recorder.Body.String(), `"id":"fc_pre"`)
}

func TestOaiResponsesToChatStreamHandlerDoesNotDuplicateReasoningDoneDelta(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := strings.Join([]string{
		`data: {"type":"response.reasoning_text.delta","output_index":0,"delta":"first "}`,
		`data: {"type":"response.reasoning_text.delta","output_index":0,"delta":"thought"}`,
		`data: {"type":"response.reasoning_text.done","output_index":0,"delta":"thought"}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"id":"rs_reordered","type":"reasoning","status":"completed","content":[{"type":"reasoning_text","text":"first thought"}]}}`,
		`data: {"type":"response.output_text.delta","delta":"answer"}`,
		`data: {"type":"response.completed","response":{"id":"resp_reasoning_done_delta","object":"response","created_at":1710000000,"model":"deepseek-v4-flash","output":[{"id":"rs_reordered","type":"reasoning","status":"completed","content":[{"type":"reasoning_text","text":"first thought"}]},{"id":"msg_0","type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]}],"usage":{"input_tokens":12,"output_tokens":5,"total_tokens":17}}}`,
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
			UpstreamModelName: "deepseek-v4-flash",
		},
	}

	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	var reasoningDeltas []string
	for _, line := range strings.Split(recorder.Body.String(), "\n") {
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
			if reasoning := choice.Delta.GetReasoningContent(); reasoning != "" {
				reasoningDeltas = append(reasoningDeltas, reasoning)
			}
		}
	}
	require.Equal(t, []string{"first ", "thought"}, reasoningDeltas)
	require.NotContains(t, recorder.Body.String(), `"reasoning_content":"first thought"`)
}

func TestOaiResponsesToChatStreamHandlerEmitsFinalFunctionCallArgumentsDoneSuffix(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.added","output_index":0,"item":{"id":"fc_done","type":"function_call","call_id":"call_done","name":"lookup","arguments":""}}`,
		`data: {"type":"response.function_call_arguments.delta","item_id":"fc_done","output_index":0,"delta":"{\"q\":"}`,
		`data: {"type":"response.function_call_arguments.done","item_id":"fc_done","output_index":0,"call_id":"call_done","name":"lookup","arguments":"{\"q\":\"status\"}"}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"id":"fc_done","type":"function_call","call_id":"call_done","name":"lookup","arguments":"{\"q\":\"status\"}"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_function_arguments_done","object":"response","created_at":1710000000,"model":"gpt-5.6-luna","output":[{"id":"fc_done","type":"function_call","call_id":"call_done","name":"lookup","arguments":"{\"q\":\"status\"}"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
			UpstreamModelName: "gpt-5.6-luna",
		},
	}

	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	calls := chatToolCallStreamChunks(t, recorder.Body.String())
	require.Len(t, calls, 3)
	require.Equal(t, "call_done", calls[0].ID)
	require.Equal(t, "lookup", calls[0].Function.Name)
	require.Empty(t, calls[0].Function.Arguments)
	require.Equal(t, "call_done", calls[1].ID)
	require.Empty(t, calls[1].Function.Name)
	require.Equal(t, `{"q":`, calls[1].Function.Arguments)
	require.Equal(t, "call_done", calls[2].ID)
	require.Empty(t, calls[2].Function.Name)
	require.Equal(t, `"status"}`, calls[2].Function.Arguments)
	require.Equal(t, 0, strings.Count(recorder.Body.String(), `"arguments":"{\"q\":\"status\"}"`))
}

func TestOaiResponsesToChatStreamHandlerPreservesRepeatedArgumentFragments(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.added","output_index":0,"item":{"id":"fc_repeat","type":"function_call","call_id":"call_repeat","name":"lookup","arguments":""}}`,
		`data: {"type":"response.function_call_arguments.delta","item_id":"fc_repeat","output_index":0,"delta":"{\"q\":\"a"}`,
		`data: {"type":"response.function_call_arguments.delta","item_id":"fc_repeat","output_index":0,"delta":"a\"}"}`,
		`data: {"type":"response.completed","response":{"id":"resp_repeat","object":"response","created_at":1710000000,"model":"gpt-5.6-luna","output":[{"id":"fc_repeat","type":"function_call","call_id":"call_repeat","name":"lookup","arguments":"{\"q\":\"aa\"}"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
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
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-luna"},
	}

	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	calls := chatToolCallStreamChunks(t, recorder.Body.String())
	require.Len(t, calls, 3)
	require.Equal(t, `{"q":"aa"}`, calls[1].Function.Arguments+calls[2].Function.Arguments)
}

func TestOaiResponsesToChatStreamHandlerRejectsUnsupportedCustomToolOutput(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.added","output_index":0,"item":{"id":"ctc_1","type":"custom_tool_call","call_id":"call_custom","name":"apply_patch","input":"*** Begin Patch"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_custom_unsupported","object":"response","created_at":1710000000,"model":"gpt-5.6-luna","output":[{"id":"ctc_1","type":"custom_tool_call","call_id":"call_custom","name":"apply_patch","input":"*** Begin Patch"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-luna"},
	}

	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, http.StatusBadGateway, err.StatusCode)
	require.Contains(t, err.Error(), "custom_tool_call")
	require.Empty(t, recorder.Body.String())
}

func TestOaiResponsesToChatStreamHandlerEmitsMessageTextFromOutputItemDone(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"id":"msg_done","type":"message","role":"assistant","content":[{"type":"output_text","text":"only terminal text"}]}}`,
		`data: {"type":"response.completed","response":{"id":"resp_message_done","object":"response","created_at":1710000000,"model":"gpt-5.6-luna","output":[{"id":"msg_done","type":"message","role":"assistant","content":[{"type":"output_text","text":"only terminal text"}]}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-luna"},
	}

	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	output := recorder.Body.String()
	require.Equal(t, 1, strings.Count(output, `"content":"only terminal text"`))
	require.Contains(t, output, `"finish_reason":"stop"`)
}

func TestOaiResponsesToChatStreamHandlerDoesNotDuplicateOutputTextSnapshot(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"first"}`,
		`data: {"type":"response.output_text.done","item_id":"msg_snapshot","output_index":0,"text":"first"}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"id":"msg_snapshot","type":"message","role":"assistant","content":[{"type":"output_text","text":"first"}]}}`,
		`data: {"type":"response.completed","response":{"id":"resp_message_snapshot","object":"response","created_at":1710000000,"model":"gpt-5.6-luna","output":[{"id":"msg_snapshot","type":"message","role":"assistant","content":[{"type":"output_text","text":"first"}]}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-luna"},
	}

	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	output := recorder.Body.String()
	require.Equal(t, 1, strings.Count(output, `"content":"first"`))
}

func TestOaiResponsesToChatStreamHandlerEmitsContentPartDoneTextWhenDeltaMissing(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := strings.Join([]string{
		`data: {"type":"response.content_part.done","item_id":"msg_part","output_index":0,"part":{"type":"output_text","text":"part snapshot"}}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"id":"msg_part","type":"message","role":"assistant","content":[{"type":"output_text","text":"part snapshot"}]}}`,
		`data: {"type":"response.completed","response":{"id":"resp_content_part_done","object":"response","created_at":1710000000,"model":"gpt-5.6-luna","output":[{"id":"msg_part","type":"message","role":"assistant","content":[{"type":"output_text","text":"part snapshot"}]}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.6-luna"},
	}

	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	output := recorder.Body.String()
	require.Equal(t, 1, strings.Count(output, `"content":"part snapshot"`))
}
