package openai

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
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

func TestAppendResponsesFallbackTextCapsMemory(t *testing.T) {
	var builder strings.Builder

	truncated := appendResponsesFallbackText(&builder, strings.Repeat("a", responsesFallbackTextMaxBytes+16))

	require.True(t, truncated)
	require.Equal(t, responsesFallbackTextMaxBytes, builder.Len())

	truncated = appendResponsesFallbackText(&builder, "b")

	require.True(t, truncated)
	require.Equal(t, responsesFallbackTextMaxBytes, builder.Len())
}

func TestAppendResponsesFallbackTextKeepsUTF8Boundary(t *testing.T) {
	var builder strings.Builder

	truncated := appendResponsesFallbackText(&builder, strings.Repeat("a", responsesFallbackTextMaxBytes-1))
	require.False(t, truncated)

	truncated = appendResponsesFallbackText(&builder, "中")

	require.True(t, truncated)
	require.Equal(t, responsesFallbackTextMaxBytes-1, builder.Len())
	require.Equal(t, strings.Repeat("a", responsesFallbackTextMaxBytes-1), builder.String())
}

func TestOaiResponsesHandlerCountsImageGenerationTool(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := `{
		"id":"resp_image_generation",
		"object":"response",
		"created_at":1710000000,
		"output":[{"type":"image_generation_call","quality":"low","size":"1024x1024"}],
		"tools":[{"type":"image_generation","quality":"low","size":"1024x1024"}],
		"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}
	}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				"image_generation": {ToolName: "image_generation"},
			},
		},
	}

	usage, err := OaiResponsesHandler(c, info, resp)

	require.Nil(t, err)
	require.Equal(t, 12, usage.PromptTokens)
	require.Equal(t, 3, usage.CompletionTokens)
	require.True(t, c.GetBool("image_generation_call"))
	require.Equal(t, "low", c.GetString("image_generation_call_quality"))
	require.Equal(t, "1024x1024", c.GetString("image_generation_call_size"))
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools["image_generation"].CallCount)
	require.Equal(t, "low", info.ResponsesUsageInfo.BuiltInTools["image_generation"].Quality)
	require.Equal(t, "1024x1024", info.ResponsesUsageInfo.BuiltInTools["image_generation"].Size)
}

func TestOaiResponsesHandlerCountsMultipleImageGenerationOutputs(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := `{
		"id":"resp_image_generation_multiple",
		"object":"response",
		"created_at":1710000000,
		"output":[
			{"type":"image_generation_call","quality":"low","size":"1024x1024"},
			{"type":"image_generation_call","quality":"low","size":"1024x1024"}
		],
		"tools":[{"type":"image_generation"}],
		"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}
	}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
	}

	usage, err := OaiResponsesHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 2, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].CallCount)
	require.Equal(t, "low", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].Quality)
	require.Equal(t, "1024x1024", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].Size)
	require.Equal(t, map[string]int{
		relaycommon.ImageGenerationCallKey("low", "1024x1024"): 2,
	}, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].ImageCalls)
}

func TestOaiResponsesHandlerCountsMixedImageGenerationSpecs(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := `{
		"id":"resp_image_generation_mixed",
		"object":"response",
		"created_at":1710000000,
		"output":[
			{"type":"image_generation_call","quality":"low","size":"1024x1024"},
			{"type":"image_generation_call","quality":"high","size":"1536x1024"}
		],
		"tools":[{"type":"image_generation"}],
		"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}
	}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
	}

	usage, err := OaiResponsesHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	imageTool := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration]
	require.Equal(t, 2, imageTool.CallCount)
	require.Equal(t, "low", imageTool.Quality)
	require.Equal(t, "1024x1024", imageTool.Size)
	require.Equal(t, map[string]int{
		relaycommon.ImageGenerationCallKey("low", "1024x1024"):  1,
		relaycommon.ImageGenerationCallKey("high", "1536x1024"): 1,
	}, imageTool.ImageCalls)
}

func TestOaiResponsesHandlerRegistersImageGenerationToolFromResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := `{
		"id":"resp_image_generation_dynamic",
		"object":"response",
		"created_at":1710000000,
		"output":[{"type":"image_generation_call","quality":"high","size":"1536x1024"}],
		"tools":[{"type":"image_generation","quality":"high","size":"1536x1024"}],
		"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}
	}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{},
		},
	}

	usage, err := OaiResponsesHandler(c, info, resp)

	require.Nil(t, err)
	require.Equal(t, 12, usage.PromptTokens)
	require.Equal(t, 3, usage.CompletionTokens)
	require.True(t, c.GetBool("image_generation_call"))
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools["image_generation"].CallCount)
	require.Equal(t, "high", info.ResponsesUsageInfo.BuiltInTools["image_generation"].Quality)
	require.Equal(t, "1536x1024", info.ResponsesUsageInfo.BuiltInTools["image_generation"].Size)
}

func TestOaiResponsesHandlerInitializesBuiltInToolsFromResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := `{
		"id":"resp_image_generation_lazy",
		"object":"response",
		"created_at":1710000000,
		"output":[{"type":"image_generation_call","quality":"medium","size":"1024x1536"}],
		"tools":[{"type":"image_generation","quality":"medium","size":"1024x1536"}],
		"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}
	}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{}

	usage, err := OaiResponsesHandler(c, info, resp)

	require.Nil(t, err)
	require.Equal(t, 12, usage.PromptTokens)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools["image_generation"].CallCount)
	require.Equal(t, "medium", info.ResponsesUsageInfo.BuiltInTools["image_generation"].Quality)
	require.Equal(t, "1024x1536", info.ResponsesUsageInfo.BuiltInTools["image_generation"].Size)
}

func TestOaiResponsesHandlerDoesNotCountWebSearchDefinitionAsCall(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := `{
		"id":"resp_web_search_definition_only",
		"object":"response",
		"created_at":1710000000,
		"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],
		"tools":[{"type":"web_search_preview","search_context_size":"high"}],
		"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}
	}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
	}

	usage, err := OaiResponsesHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.NotNil(t, info.ResponsesUsageInfo.BuiltInTools)
	require.Equal(t, 0, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
	require.Equal(t, "high", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].SearchContextSize)
	require.False(t, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].DefaultSearchSize)
}

func TestOaiResponsesHandlerCountsWebSearchCallWithoutToolMetadata(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := `{
		"id":"resp_web_search_call_no_tools",
		"object":"response",
		"created_at":1710000000,
		"output":[{"type":"web_search_call"}],
		"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}
	}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{}

	usage, err := OaiResponsesHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	webSearchTool := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview]
	require.Equal(t, 1, webSearchTool.CallCount)
	require.Equal(t, "medium", webSearchTool.SearchContextSize)
	require.True(t, webSearchTool.DefaultSearchSize)
}

func TestOaiResponsesHandlerCountsWebSearchCallFromOutput(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := `{
		"id":"resp_web_search_call",
		"object":"response",
		"created_at":1710000000,
		"output":[{"type":"web_search_call"}],
		"tools":[{"type":"web_search_preview","search_context_size":"low"}],
		"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}
	}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
	}

	usage, err := OaiResponsesHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.NotNil(t, info.ResponsesUsageInfo.BuiltInTools)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
	require.Equal(t, "low", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].SearchContextSize)
}

func TestOaiResponsesHandlerCountsFileSearchCallFromOutput(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := `{
		"id":"resp_file_search_call",
		"object":"response",
		"created_at":1710000000,
		"output":[{"type":"file_search_call"}],
		"tools":[{"type":"file_search"}],
		"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}
	}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
	}

	usage, err := OaiResponsesHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.NotNil(t, info.ResponsesUsageInfo.BuiltInTools)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolFileSearch].CallCount)
}

func TestOaiResponsesHandlerCanonicalizesVersionedWebSearchPreviewTool(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := `{
		"id":"resp_web_search_versioned_tool",
		"object":"response",
		"created_at":1710000000,
		"output":[{"type":"web_search_call"}],
		"tools":[{"type":"web_search_preview_2025_03_11","search_context_size":"high"}],
		"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}
	}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
	}

	usage, err := OaiResponsesHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Len(t, info.ResponsesUsageInfo.BuiltInTools, 1)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
	require.Equal(t, "high", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].SearchContextSize)
}

func TestOaiResponsesHandlerIgnoresBuiltInToolsWithoutRelayInfo(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := `{
		"id":"resp_builtin_tools_without_info",
		"object":"response",
		"created_at":1710000000,
		"output":[
			{"type":"web_search_call"},
			{"type":"image_generation_call","quality":"low","size":"1024x1024"}
		],
		"tools":[
			{"type":"web_search_preview","search_context_size":"high"},
			{"type":"image_generation","quality":"low","size":"1024x1024"}
		],
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
	require.True(t, c.GetBool("image_generation_call"))
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
			UpstreamModelName: "custom-model",
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

func TestOaiResponsesToChatHandlerCountsBuiltInToolsFromResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := `{
		"id":"resp_chat_builtin_tools",
		"object":"response",
		"created_at":1710000000,
		"model":"gpt-5.5",
		"output":[
			{"type":"web_search_call"},
			{"type":"image_generation_call","quality":"low","size":"1024x1024"},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}
		],
		"tools":[
			{"type":"web_search_preview","search_context_size":"high"},
			{"type":"image_generation","quality":"low","size":"1024x1024"}
		],
		"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}
	}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAI,
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{},
		},
	}

	usage, err := OaiResponsesToChatHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 12, usage.PromptTokens)
	require.Equal(t, 3, usage.CompletionTokens)
	require.Contains(t, recorder.Body.String(), `"content":"ok"`)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
	require.Equal(t, "high", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].SearchContextSize)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].CallCount)
	require.Equal(t, "low", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].Quality)
	require.Equal(t, "1024x1024", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].Size)
	require.True(t, c.GetBool("image_generation_call"))
	require.Equal(t, "low", c.GetString("image_generation_call_quality"))
	require.Equal(t, "1024x1024", c.GetString("image_generation_call_size"))
}

func TestOaiResponsesToChatStreamHandlerMapsReasoningSummaryPart(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := strings.Join([]string{
		`data: {"type":"response.reasoning_summary_part.added","item_id":"rs_1","summary_index":0,"part":{"type":"summary_text","text":"first summary"}}`,
		`data: {"type":"response.reasoning_summary_part.done","item_id":"rs_1","summary_index":0,"part":{"type":"summary_text","text":"first summary"}}`,
		`data: {"type":"response.reasoning_summary_text.done"}`,
		`data: {"type":"response.reasoning_summary_part.done","item_id":"rs_1","summary_index":1,"part":{"type":"summary_text","text":"second summary"}}`,
		`data: {"type":"response.output_text.delta","delta":"answer"}`,
		`data: {"type":"response.completed","response":{"id":"resp_reasoning_summary","object":"response","created_at":1710000000,"model":"gpt-5.4-mini","usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
			UpstreamModelName: "gpt-5.4-mini",
		},
	}

	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.Equal(t, 12, usage.PromptTokens)
	require.Equal(t, 3, usage.CompletionTokens)
	output := recorder.Body.String()
	require.Contains(t, output, `"reasoning_content":"first summary"`)
	require.Contains(t, output, `"reasoning_content":"\n\nsecond summary"`)
	require.Equal(t, 1, strings.Count(output, `"reasoning_content":"first summary"`))
	require.Contains(t, output, `"content":"answer"`)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesChatCompatReasoningSummaryMapped))
}

func TestOaiResponsesToChatStreamHandlerMapsReasoningSummaryTextDelta(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := strings.Join([]string{
		`data: {"type":"response.reasoning_summary_text.delta","delta":"first summary"}`,
		`data: {"type":"response.reasoning_summary_text.done"}`,
		`data: {"type":"response.reasoning_summary_text.delta","delta":"second summary"}`,
		`data: {"type":"response.output_text.delta","delta":"answer"}`,
		`data: {"type":"response.completed","response":{"id":"resp_reasoning_summary_delta","object":"response","created_at":1710000000,"model":"gpt-5.4-mini","usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
			UpstreamModelName: "gpt-5.4-mini",
		},
	}

	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.Equal(t, 12, usage.PromptTokens)
	require.Equal(t, 3, usage.CompletionTokens)
	output := recorder.Body.String()
	require.Contains(t, output, `"reasoning_content":"first summary"`)
	require.Contains(t, output, `"reasoning_content":"\n\nsecond summary"`)
	require.Contains(t, output, `"content":"answer"`)
}

func TestOaiResponsesToChatStreamHandlerDoesNotDuplicateMixedReasoningSummaryEvents(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := strings.Join([]string{
		`data: {"type":"response.reasoning_summary_text.delta","delta":"first summary"}`,
		`data: {"type":"response.reasoning_summary_part.added","item_id":"rs_1","summary_index":0,"part":{"type":"summary_text","text":"first summary"}}`,
		`data: {"type":"response.output_text.delta","delta":"answer"}`,
		`data: {"type":"response.completed","response":{"id":"resp_reasoning_summary_mixed","object":"response","created_at":1710000000,"model":"gpt-5.4-mini","usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
			UpstreamModelName: "gpt-5.4-mini",
		},
	}

	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.Equal(t, 12, usage.PromptTokens)
	require.Equal(t, 3, usage.CompletionTokens)
	output := recorder.Body.String()
	require.Equal(t, 1, strings.Count(output, `"reasoning_content":"first summary"`))
	require.Contains(t, output, `"content":"answer"`)
}

func TestOaiResponsesToChatStreamHandlerKeepsTextDeltaAfterReasoningSummaryPartAdded(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := strings.Join([]string{
		`data: {"type":"response.reasoning_summary_part.added","item_id":"rs_1","summary_index":0,"part":{"type":"summary_text","text":"first summary"}}`,
		`data: {"type":"response.reasoning_summary_text.delta","delta":"first summary"}`,
		`data: {"type":"response.reasoning_summary_text.delta","delta":"second summary"}`,
		`data: {"type":"response.reasoning_summary_part.done","item_id":"rs_1","summary_index":0,"part":{"type":"summary_text","text":"first summary"}}`,
		`data: {"type":"response.output_text.delta","delta":"answer"}`,
		`data: {"type":"response.completed","response":{"id":"resp_reasoning_summary_mixed_text_delta","object":"response","created_at":1710000000,"model":"gpt-5.4-mini","usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
			UpstreamModelName: "gpt-5.4-mini",
		},
	}

	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.Equal(t, 12, usage.PromptTokens)
	require.Equal(t, 3, usage.CompletionTokens)
	output := recorder.Body.String()
	require.Contains(t, output, `"reasoning_content":"first summary"`)
	require.Contains(t, output, `"reasoning_content":"second summary"`)
	require.Equal(t, 1, strings.Count(output, `"reasoning_content":"first summary"`))
	require.Contains(t, output, `"content":"answer"`)
}

func TestOaiResponsesToChatStreamHandlerSeparatesNewReasoningSummaryParts(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := strings.Join([]string{
		`data: {"type":"response.reasoning_summary_part.added","item_id":"rs_1","summary_index":0,"part":{"type":"summary_text","text":"first summary"}}`,
		`data: {"type":"response.reasoning_summary_part.done","item_id":"rs_1","summary_index":0,"part":{"type":"summary_text","text":"first summary"}}`,
		`data: {"type":"response.reasoning_summary_part.done","item_id":"rs_2","summary_index":1,"part":{"type":"summary_text","text":"second summary"}}`,
		`data: {"type":"response.output_text.delta","delta":"answer"}`,
		`data: {"type":"response.completed","response":{"id":"resp_reasoning_summary_separator","object":"response","created_at":1710000000,"model":"gpt-5.4-mini","usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
			UpstreamModelName: "gpt-5.4-mini",
		},
	}

	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.Equal(t, 12, usage.PromptTokens)
	require.Equal(t, 3, usage.CompletionTokens)
	output := recorder.Body.String()
	require.Contains(t, output, `"reasoning_content":"first summary"`)
	require.Contains(t, output, `"reasoning_content":"\n\nsecond summary"`)
	require.Equal(t, 1, strings.Count(output, `"reasoning_content":"first summary"`))
	require.Contains(t, output, `"content":"answer"`)
}

func TestOaiResponsesToChatStreamHandlerCountsBuiltInToolsFromCompletedResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"ok"}`,
		`data: {"type":"response.completed","response":{"id":"resp_chat_stream_builtin_tools","object":"response","created_at":1710000000,"model":"gpt-5.5","output":[{"type":"web_search_call"},{"type":"image_generation_call","quality":"high","size":"1536x1024"}],"tools":[{"type":"web_search_preview","search_context_size":"low"},{"type":"image_generation","quality":"high","size":"1536x1024"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
			UpstreamModelName: "gpt-5.5",
		},
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{},
		},
	}

	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 12, usage.PromptTokens)
	require.Equal(t, 3, usage.CompletionTokens)
	require.Contains(t, recorder.Body.String(), `"content":"ok"`)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
	require.Equal(t, "low", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].SearchContextSize)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].CallCount)
	require.Equal(t, "high", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].Quality)
	require.Equal(t, "1536x1024", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].Size)
	require.True(t, c.GetBool("image_generation_call"))
	require.Equal(t, "high", c.GetString("image_generation_call_quality"))
	require.Equal(t, "1536x1024", c.GetString("image_generation_call_size"))
}

func TestOaiResponsesToChatStreamHandlerCountsImageGenerationOutputItemDone(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"type":"image_generation_call","quality":"low","size":"1024x1024"}}`,
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
			UpstreamModelName: "gpt-5.5",
		},
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
	}

	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	imageTool := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration]
	require.Equal(t, 1, imageTool.CallCount)
	require.Equal(t, "low", imageTool.Quality)
	require.Equal(t, "1024x1024", imageTool.Size)
	require.Equal(t, map[string]int{
		relaycommon.ImageGenerationCallKey("low", "1024x1024"): 1,
	}, imageTool.ImageCalls)
	require.True(t, c.GetBool("image_generation_call"))
	require.Equal(t, "low", c.GetString("image_generation_call_quality"))
	require.Equal(t, "1024x1024", c.GetString("image_generation_call_size"))
}

func TestOaiResponsesToChatStreamHandlerMarksContextCompactionItemDone(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"type":"context_compaction","encrypted_content":"opaque"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_chat_context_compact","object":"response","created_at":1710000000,"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
			UpstreamModelName: "gpt-5.5",
		},
	}

	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactionOutput))
}

func TestOaiResponsesHandlerMapsUsageDetails(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := `{
		"id":"resp_usage_details",
		"object":"response",
		"created_at":1710000000,
		"usage":{
			"input_tokens":10,
			"output_tokens":20,
			"total_tokens":30,
			"input_tokens_details":{"cached_tokens":3,"image_tokens":4,"audio_tokens":5},
			"output_tokens_details":{"reasoning_tokens":6,"image_tokens":7,"audio_tokens":8}
		}
	}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	usage, err := OaiResponsesHandler(c, nil, resp)

	require.Nil(t, err)
	require.Equal(t, 10, usage.PromptTokens)
	require.Equal(t, 20, usage.CompletionTokens)
	require.Equal(t, 30, usage.TotalTokens)
	require.Equal(t, 3, usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, 4, usage.PromptTokensDetails.ImageTokens)
	require.Equal(t, 5, usage.PromptTokensDetails.AudioTokens)
	require.Equal(t, 6, usage.CompletionTokenDetails.ReasoningTokens)
	require.Equal(t, 7, usage.CompletionTokenDetails.ImageTokens)
	require.Equal(t, 8, usage.CompletionTokenDetails.AudioTokens)
}

func TestOaiResponsesStreamHandlerMapsUsageDetails(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.completed","response":{"id":"resp_stream_usage_details","object":"response","created_at":1710000000,"usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30,"input_tokens_details":{"cached_tokens":3,"image_tokens":4,"audio_tokens":5},"output_tokens_details":{"reasoning_tokens":6,"image_tokens":7,"audio_tokens":8}}}}`,
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
			UpstreamModelName: "custom-model",
		},
		StreamStatus: relaycommon.NewStreamStatus(),
		DisablePing:  true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.Equal(t, 10, usage.PromptTokens)
	require.Equal(t, 20, usage.CompletionTokens)
	require.Equal(t, 30, usage.TotalTokens)
	require.Equal(t, 3, usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, 4, usage.PromptTokensDetails.ImageTokens)
	require.Equal(t, 5, usage.PromptTokensDetails.AudioTokens)
	require.Equal(t, 6, usage.CompletionTokenDetails.ReasoningTokens)
	require.Equal(t, 7, usage.CompletionTokenDetails.ImageTokens)
	require.Equal(t, 8, usage.CompletionTokenDetails.AudioTokens)
}

func TestOaiResponsesStreamHandlerCountsReasoningSummaryFallbackTokens(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.reasoning_summary_text.delta","delta":"reasoning only answer"}`,
		`data: {"type":"response.completed","response":{"id":"resp_reasoning_fallback","object":"response","created_at":1710000000}}`,
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
			UpstreamModelName: "custom-model",
		},
		StreamStatus: relaycommon.NewStreamStatus(),
		DisablePing:  true,
	}
	info.SetEstimatePromptTokens(7)

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.Equal(t, 7, usage.PromptTokens)
	require.Greater(t, usage.CompletionTokens, 0)
	require.Equal(t, usage.PromptTokens+usage.CompletionTokens, usage.TotalTokens)
}

func TestOaiResponsesStreamHandlerCompletedUsageOverridesFallbackWithZero(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.reasoning_summary_text.delta","delta":"fallback reasoning"}`,
		`data: {"type":"response.completed","response":{"id":"resp_zero_usage","object":"response","created_at":1710000000,"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0,"output_tokens_details":{"reasoning_tokens":0,"image_tokens":0,"audio_tokens":0}}}}`,
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
			UpstreamModelName: "custom-model",
		},
		StreamStatus: relaycommon.NewStreamStatus(),
		DisablePing:  true,
	}
	info.SetEstimatePromptTokens(7)

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.Equal(t, 7, usage.PromptTokens)
	require.Greater(t, usage.CompletionTokens, 0)
	require.Equal(t, usage.PromptTokens+usage.CompletionTokens, usage.TotalTokens)
	require.Zero(t, usage.CompletionTokenDetails.ReasoningTokens)
	require.Zero(t, usage.CompletionTokenDetails.ImageTokens)
	require.Zero(t, usage.CompletionTokenDetails.AudioTokens)
}

func TestOaiResponsesStreamHandlerKeepsCompletedZeroUsageWithoutOutput(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.completed","response":{"id":"resp_zero_usage","object":"response","created_at":1710000000,"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`,
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
			UpstreamModelName: "custom-model",
		},
		StreamStatus: relaycommon.NewStreamStatus(),
		DisablePing:  true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.Zero(t, usage.PromptTokens)
	require.Zero(t, usage.CompletionTokens)
	require.Zero(t, usage.TotalTokens)
}

func TestOaiResponsesStreamHandlerCountsImageGenerationTool(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.completed","response":{"id":"resp_image_generation_stream","object":"response","created_at":1710000000,"output":[{"type":"image_generation_call","quality":"medium","size":"1024x1024"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolImageGeneration: {
					ToolName: dto.BuildInToolImageGeneration,
				},
			},
		},
		StreamStatus: relaycommon.NewStreamStatus(),
		DisablePing:  true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].CallCount)
	require.True(t, c.GetBool("image_generation_call"))
	require.Equal(t, "medium", c.GetString("image_generation_call_quality"))
	require.Equal(t, "1024x1024", c.GetString("image_generation_call_size"))
	require.Equal(t, "medium", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].Quality)
	require.Equal(t, "1024x1024", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].Size)
}

func TestOaiResponsesStreamHandlerUpdatesImageGenerationToolSpecFromFirstOutput(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.completed","response":{"id":"resp_image_generation_stream_actual_spec","object":"response","created_at":1710000000,"output":[{"type":"image_generation_call","quality":"high","size":"1536x1024"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolImageGeneration: {
					ToolName: dto.BuildInToolImageGeneration,
					Quality:  "low",
					Size:     "1024x1024",
				},
			},
		},
		StreamStatus: relaycommon.NewStreamStatus(),
		DisablePing:  true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	imageTool := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration]
	require.Equal(t, 1, imageTool.CallCount)
	require.Equal(t, "high", imageTool.Quality)
	require.Equal(t, "1536x1024", imageTool.Size)
	require.Equal(t, map[string]int{
		relaycommon.ImageGenerationCallKey("high", "1536x1024"): 1,
	}, imageTool.ImageCalls)
}

func TestOaiResponsesStreamHandlerRegistersImageGenerationToolFromResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.completed","response":{"id":"resp_image_generation_stream_dynamic","object":"response","created_at":1710000000,"output":[{"type":"image_generation_call","quality":"high","size":"1536x1024"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.NotNil(t, info.ResponsesUsageInfo.BuiltInTools)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].CallCount)
	require.Equal(t, "high", c.GetString("image_generation_call_quality"))
	require.Equal(t, "1536x1024", c.GetString("image_generation_call_size"))
	require.Equal(t, "high", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].Quality)
	require.Equal(t, "1536x1024", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].Size)
}

func TestOaiResponsesStreamHandlerCountsImageGenerationToolOnce(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	completed := `data: {"type":"response.completed","response":{"id":"resp_image_generation_stream_repeat","object":"response","created_at":1710000000,"output":[{"type":"image_generation_call","quality":"medium","size":"1024x1024"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`
	body := strings.Join([]string{
		completed,
		completed,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].CallCount)
	require.Equal(t, "medium", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].Quality)
	require.Equal(t, "1024x1024", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].Size)
}

func TestOaiResponsesStreamHandlerCountsImageGenerationOutputItemDone(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"type":"image_generation_call","quality":"low","size":"1024x1024"}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	imageTool := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration]
	require.Equal(t, 1, imageTool.CallCount)
	require.Equal(t, "low", imageTool.Quality)
	require.Equal(t, "1024x1024", imageTool.Size)
	require.Equal(t, map[string]int{
		relaycommon.ImageGenerationCallKey("low", "1024x1024"): 1,
	}, imageTool.ImageCalls)
	require.True(t, c.GetBool("image_generation_call"))
	require.Equal(t, "low", c.GetString("image_generation_call_quality"))
	require.Equal(t, "1024x1024", c.GetString("image_generation_call_size"))
}

func TestOaiResponsesStreamHandlerDeduplicatesImageGenerationDoneOnlyByID(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"id":"ig_1","type":"image_generation_call","quality":"low","size":"1024x1024"}}`,
		`data: {"type":"response.output_item.done","item":{"id":"ig_1","type":"image_generation_call","quality":"low","size":"1024x1024"}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	imageTool := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration]
	require.Equal(t, 1, imageTool.CallCount)
	require.Equal(t, map[string]int{
		relaycommon.ImageGenerationCallKey("low", "1024x1024"): 1,
	}, imageTool.ImageCalls)
}

func TestOaiResponsesStreamHandlerCountsNoIDImageGenerationDoneOnlyEvents(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"type":"image_generation_call","quality":"low","size":"1024x1024"}}`,
		`data: {"type":"response.output_item.done","item":{"type":"image_generation_call","quality":"low","size":"1024x1024"}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	imageTool := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration]
	require.Equal(t, 2, imageTool.CallCount)
	require.Equal(t, map[string]int{
		relaycommon.ImageGenerationCallKey("low", "1024x1024"): 2,
	}, imageTool.ImageCalls)
}

func TestOaiResponsesStreamHandlerDoesNotDoubleCountImageGenerationDoneAndCompleted(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"type":"image_generation_call","quality":"low","size":"1024x1024"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_image_generation_stream_done_completed","object":"response","created_at":1710000000,"output":[{"type":"image_generation_call","quality":"low","size":"1024x1024"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	imageTool := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration]
	require.Equal(t, 1, imageTool.CallCount)
	require.Equal(t, map[string]int{
		relaycommon.ImageGenerationCallKey("low", "1024x1024"): 1,
	}, imageTool.ImageCalls)
}

func TestOaiResponsesStreamHandlerDoesNotDoubleCountImageGenerationDuplicateDoneAfterCompleted(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"type":"image_generation_call","quality":"low","size":"1024x1024"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_image_generation_stream_duplicate_done_after_completed","object":"response","created_at":1710000000,"output":[{"type":"image_generation_call","quality":"low","size":"1024x1024"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
		`data: {"type":"response.output_item.done","item":{"type":"image_generation_call","quality":"low","size":"1024x1024"}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	imageTool := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration]
	require.Equal(t, 1, imageTool.CallCount)
	require.Equal(t, map[string]int{
		relaycommon.ImageGenerationCallKey("low", "1024x1024"): 1,
	}, imageTool.ImageCalls)
}

func TestOaiResponsesStreamHandlerDoesNotDoubleCountImageGenerationDuplicateDoneBeforeCompleted(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"type":"image_generation_call","quality":"low","size":"1024x1024"}}`,
		`data: {"type":"response.output_item.done","item":{"type":"image_generation_call","quality":"low","size":"1024x1024"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_image_generation_stream_duplicate_done_before_completed","object":"response","created_at":1710000000,"output":[{"type":"image_generation_call","quality":"low","size":"1024x1024"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	imageTool := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration]
	require.Equal(t, 1, imageTool.CallCount)
	require.Equal(t, map[string]int{
		relaycommon.ImageGenerationCallKey("low", "1024x1024"): 1,
	}, imageTool.ImageCalls)
}

func TestOaiResponsesStreamHandlerDeduplicatesImageGenerationBySpecWhenCompletedOrderDiffers(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"type":"image_generation_call","quality":"high","size":"1536x1024"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_image_generation_stream_mixed_order","object":"response","created_at":1710000000,"output":[{"type":"image_generation_call","quality":"low","size":"1024x1024"},{"type":"image_generation_call","quality":"high","size":"1536x1024"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	imageTool := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration]
	require.Equal(t, 2, imageTool.CallCount)
	require.Equal(t, map[string]int{
		relaycommon.ImageGenerationCallKey("low", "1024x1024"):  1,
		relaycommon.ImageGenerationCallKey("high", "1536x1024"): 1,
	}, imageTool.ImageCalls)
}

func TestOaiResponsesStreamHandlerDeduplicatesImageGenerationDoneAfterCompletedBySpec(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.completed","response":{"id":"resp_image_generation_stream_completed_first","object":"response","created_at":1710000000,"output":[{"type":"image_generation_call","quality":"low","size":"1024x1024"},{"type":"image_generation_call","quality":"high","size":"1536x1024"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
		`data: {"type":"response.output_item.done","item":{"type":"image_generation_call","quality":"high","size":"1536x1024"}}`,
		`data: {"type":"response.output_item.done","item":{"type":"image_generation_call","quality":"low","size":"1024x1024"}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	imageTool := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration]
	require.Equal(t, 2, imageTool.CallCount)
	require.Equal(t, map[string]int{
		relaycommon.ImageGenerationCallKey("low", "1024x1024"):  1,
		relaycommon.ImageGenerationCallKey("high", "1536x1024"): 1,
	}, imageTool.ImageCalls)
}

func TestOaiResponsesStreamHandlerDeduplicatesImageGenerationCompletedWithMixedDoneIDs(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.completed","response":{"id":"resp_image_generation_stream_mixed_done_ids","object":"response","created_at":1710000000,"output":[{"id":"ig_1","type":"image_generation_call","quality":"low","size":"1024x1024"},{"id":"ig_2","type":"image_generation_call","quality":"low","size":"1024x1024"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
		`data: {"type":"response.output_item.done","item":{"type":"image_generation_call","quality":"low","size":"1024x1024"}}`,
		`data: {"type":"response.output_item.done","item":{"id":"ig_1","type":"image_generation_call","quality":"low","size":"1024x1024"}}`,
		`data: {"type":"response.output_item.done","item":{"id":"ig_2","type":"image_generation_call","quality":"low","size":"1024x1024"}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	imageTool := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration]
	require.Equal(t, 2, imageTool.CallCount)
	require.Equal(t, map[string]int{
		relaycommon.ImageGenerationCallKey("low", "1024x1024"): 2,
	}, imageTool.ImageCalls)
}

func TestOaiResponsesStreamHandlerReclassifiesImageGenerationDoneWithoutSpec(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"id":"ig_1","type":"image_generation_call"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_image_generation_stream_reclassify","object":"response","created_at":1710000000,"output":[{"id":"ig_1","type":"image_generation_call","quality":"high","size":"1536x1024"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolImageGeneration: {
					ToolName: dto.BuildInToolImageGeneration,
					Quality:  "low",
					Size:     "1024x1024",
				},
			},
		},
		StreamStatus: relaycommon.NewStreamStatus(),
		DisablePing:  true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	imageTool := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration]
	require.Equal(t, 1, imageTool.CallCount)
	require.Equal(t, map[string]int{
		relaycommon.ImageGenerationCallKey("high", "1536x1024"): 1,
	}, imageTool.ImageCalls)
	require.Equal(t, "high", c.GetString("image_generation_call_quality"))
	require.Equal(t, "1536x1024", c.GetString("image_generation_call_size"))
}

func TestOaiResponsesStreamHandlerReclassifiesImageGenerationCompletedWithoutSpecFromDone(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"type":"image_generation_call","quality":"high","size":"1536x1024"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_image_generation_stream_completed_without_spec","object":"response","created_at":1710000000,"output":[{"id":"ig_1","type":"image_generation_call"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	imageTool := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration]
	require.Equal(t, 1, imageTool.CallCount)
	require.Equal(t, map[string]int{
		relaycommon.ImageGenerationCallKey("high", "1536x1024"): 1,
	}, imageTool.ImageCalls)
}

func TestConsumeResponsesImageGenerationCallDoesNotUseAmbiguousKnownSpecForIDOnly(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	addResponsesImageGenerationCall(c, responsesObservedImageGenerationCallsKey, responsesImageGenerationCallSpec{
		Quality: "high",
		Size:    "1536x1024",
	})
	addResponsesImageGenerationCall(c, responsesObservedImageGenerationCallsKey, responsesImageGenerationCallSpec{
		Quality: "low",
		Size:    "1024x1024",
	})

	_, ok := consumeResponsesImageGenerationCall(c, responsesObservedImageGenerationCallsKey, responsesImageGenerationCallSpec{
		ID: "ig_ambiguous",
	})

	require.False(t, ok)
}

func TestConsumeResponsesImageGenerationCallDoesNotUseAmbiguousEmptySpecForKnownSpec(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	addResponsesImageGenerationCall(c, responsesCompletedImageGenerationCallsKey, responsesImageGenerationCallSpec{
		ID: "ig_1",
	})
	addResponsesImageGenerationCall(c, responsesCompletedImageGenerationCallsKey, responsesImageGenerationCallSpec{
		ID: "ig_2",
	})

	_, ok := consumeResponsesImageGenerationCall(c, responsesCompletedImageGenerationCallsKey, responsesImageGenerationCallSpec{
		Quality: "high",
		Size:    "1536x1024",
	})

	require.False(t, ok)
}

func TestConsumeResponsesImageGenerationCallRemembersKnownSpecForEmptyID(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	addResponsesImageGenerationCall(c, responsesCompletedImageGenerationCallsKey, responsesImageGenerationCallSpec{
		ID: "ig_1",
	})

	observed, ok := consumeResponsesImageGenerationCall(c, responsesCompletedImageGenerationCallsKey, responsesImageGenerationCallSpec{
		Quality: "high",
		Size:    "1536x1024",
	})
	require.True(t, ok)
	require.Equal(t, responsesImageGenerationCallSpec{ID: "ig_1"}, observed)

	observed, ok = consumeResponsesImageGenerationCall(c, responsesCompletedImageGenerationCallsKey, responsesImageGenerationCallSpec{
		ID:      "ig_1",
		Quality: "high",
		Size:    "1536x1024",
	})
	require.True(t, ok)
	require.Equal(t, responsesImageGenerationCallSpec{
		ID:      "ig_1",
		Quality: "high",
		Size:    "1536x1024",
	}, observed)

	observed, ok = consumeResponsesImageGenerationCall(c, responsesCompletedImageGenerationCallsKey, responsesImageGenerationCallSpec{
		ID:      "ig_1",
		Quality: "high",
		Size:    "1536x1024",
	})
	require.True(t, ok)
	require.Equal(t, responsesImageGenerationCallSpec{
		ID:      "ig_1",
		Quality: "high",
		Size:    "1536x1024",
	}, observed)
}

func TestOaiResponsesStreamHandlerCountsWebSearchPreviewTool(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"type":"web_search_call"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_web_search_stream","object":"response","created_at":1710000000,"output":[{"type":"web_search_call"}],"tools":[{"type":"web_search_preview","search_context_size":"medium"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolWebSearchPreview: {
					ToolName:          dto.BuildInToolWebSearchPreview,
					SearchContextSize: "high",
				},
			},
		},
		StreamStatus: relaycommon.NewStreamStatus(),
		DisablePing:  true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.NotNil(t, info.ResponsesUsageInfo.BuiltInTools)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
	require.Equal(t, "high", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].SearchContextSize)
}

func TestOaiResponsesStreamHandlerDeduplicatesWebSearchDoneOnlyByID(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"id":"ws_1","type":"web_search_call"}}`,
		`data: {"type":"response.output_item.done","item":{"id":"ws_1","type":"web_search_call"}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
}

func TestOaiResponsesStreamHandlerCountsNoIDWebSearchDoneOnlyEvents(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"type":"web_search_call"}}`,
		`data: {"type":"response.output_item.done","item":{"type":"web_search_call"}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 2, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
}

func TestOaiResponsesStreamHandlerCountsWebSearchPreviewFromCompletedOutput(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.completed","response":{"id":"resp_web_search_stream_completed","object":"response","created_at":1710000000,"output":[{"type":"web_search_call"}],"tools":[{"type":"web_search_preview","search_context_size":"low"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.NotNil(t, info.ResponsesUsageInfo.BuiltInTools)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
	require.Equal(t, "low", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].SearchContextSize)
}

func TestOaiResponsesStreamHandlerCountsMultipleWebSearchPreviewItems(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"type":"web_search_call"}}`,
		`data: {"type":"response.output_item.done","item":{"type":"web_search_call"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_web_search_stream_multiple","object":"response","created_at":1710000000,"output":[{"type":"web_search_call"},{"type":"web_search_call"}],"tools":[{"type":"web_search_preview","search_context_size":"medium"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.NotNil(t, info.ResponsesUsageInfo.BuiltInTools)
	require.Equal(t, 2, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
	require.Equal(t, "medium", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].SearchContextSize)
}

func TestOaiResponsesStreamHandlerDoesNotDoubleCountWebSearchDuplicateDoneAfterCompleted(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"type":"web_search_call"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_web_search_stream_duplicate_done_after_completed","object":"response","created_at":1710000000,"output":[{"type":"web_search_call"}],"tools":[{"type":"web_search_preview","search_context_size":"medium"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
		`data: {"type":"response.output_item.done","item":{"type":"web_search_call"}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
}

func TestOaiResponsesStreamHandlerDoesNotDoubleCountWebSearchDuplicateDoneBeforeCompleted(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"type":"web_search_call"}}`,
		`data: {"type":"response.output_item.done","item":{"type":"web_search_call"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_web_search_stream_duplicate_done_before_completed","object":"response","created_at":1710000000,"output":[{"type":"web_search_call"}],"tools":[{"type":"web_search_preview","search_context_size":"medium"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
}

func TestOaiResponsesStreamHandlerCountsFileSearchCall(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"type":"file_search_call"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_file_search_stream","object":"response","created_at":1710000000,"output":[{"type":"file_search_call"}],"tools":[{"type":"file_search"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.NotNil(t, info.ResponsesUsageInfo.BuiltInTools)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolFileSearch].CallCount)
}

func TestOaiResponsesStreamHandlerDeduplicatesFileSearchDoneOnlyByID(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"id":"fs_1","type":"file_search_call"}}`,
		`data: {"type":"response.output_item.done","item":{"id":"fs_1","type":"file_search_call"}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolFileSearch].CallCount)
}

func TestOaiResponsesStreamHandlerCountsNoIDFileSearchDoneOnlyEvents(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"type":"file_search_call"}}`,
		`data: {"type":"response.output_item.done","item":{"type":"file_search_call"}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 2, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolFileSearch].CallCount)
}

func TestOaiResponsesStreamHandlerDoesNotDoubleCountFileSearchDuplicateDoneAfterCompleted(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"type":"file_search_call"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_file_search_stream_duplicate_done_after_completed","object":"response","created_at":1710000000,"output":[{"type":"file_search_call"}],"tools":[{"type":"file_search"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
		`data: {"type":"response.output_item.done","item":{"type":"file_search_call"}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolFileSearch].CallCount)
}

func TestOaiResponsesStreamHandlerDoesNotDoubleCountFileSearchDuplicateDoneBeforeCompleted(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"type":"file_search_call"}}`,
		`data: {"type":"response.output_item.done","item":{"type":"file_search_call"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_file_search_stream_duplicate_done_before_completed","object":"response","created_at":1710000000,"output":[{"type":"file_search_call"}],"tools":[{"type":"file_search"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolFileSearch].CallCount)
}

func TestOaiResponsesStreamHandlerDoesNotDoubleCountFileSearchWhenCompletedArrivesFirst(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.completed","response":{"id":"resp_file_search_stream_out_of_order","object":"response","created_at":1710000000,"output":[{"type":"file_search_call"},{"type":"file_search_call"}],"tools":[{"type":"file_search"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
		`data: {"type":"response.output_item.done","item":{"type":"file_search_call"}}`,
		`data: {"type":"response.output_item.done","item":{"type":"file_search_call"}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.NotNil(t, info.ResponsesUsageInfo.BuiltInTools)
	require.Equal(t, 2, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolFileSearch].CallCount)
}

func TestOaiResponsesStreamHandlerCountsCompletedFileSearchDeltaAfterObservedDone(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"type":"file_search_call"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_file_search_stream_delta","object":"response","created_at":1710000000,"output":[{"type":"file_search_call"},{"type":"file_search_call"}],"tools":[{"type":"file_search"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.NotNil(t, info.ResponsesUsageInfo.BuiltInTools)
	require.Equal(t, 2, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolFileSearch].CallCount)
}

func TestOaiResponsesStreamHandlerMergesWebSearchPreviewToolMetadata(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"type":"web_search_call"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_web_search_stream_merge","object":"response","created_at":1710000000,"output":[{"type":"web_search_call"}],"tools":[{"type":"web_search_preview","search_context_size":"high"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.NotNil(t, info.ResponsesUsageInfo.BuiltInTools)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
	require.Equal(t, "high", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].SearchContextSize)
}

func TestOaiResponsesStreamHandlerCanonicalizesVersionedWebSearchPreviewTool(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"type":"web_search_call"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_web_search_stream_versioned","object":"response","created_at":1710000000,"output":[{"type":"web_search_call"}],"tools":[{"type":"web_search_preview_2025_03_11","search_context_size":"high"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Len(t, info.ResponsesUsageInfo.BuiltInTools, 1)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
	require.Equal(t, "high", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].SearchContextSize)
}

func TestOaiResponsesStreamHandlerPreservesExplicitWebSearchPreviewSize(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"type":"web_search_call"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_web_search_stream_explicit","object":"response","created_at":1710000000,"output":[{"type":"web_search_call"}],"tools":[{"type":"web_search_preview"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolWebSearchPreview: {
					ToolName:          dto.BuildInToolWebSearchPreview,
					SearchContextSize: "high",
				},
			},
		},
		StreamStatus: relaycommon.NewStreamStatus(),
		DisablePing:  true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
	require.Equal(t, "high", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].SearchContextSize)
	require.False(t, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].DefaultSearchSize)
}

func TestOaiResponsesStreamHandlerDoesNotCountCompletedToolsAsCalls(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.completed","response":{"id":"resp_web_search_stream_metadata","object":"response","created_at":1710000000,"tools":[{"type":"web_search_preview","search_context_size":"high"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.NotNil(t, info.ResponsesUsageInfo.BuiltInTools)
	require.Equal(t, 0, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
	require.Equal(t, "high", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].SearchContextSize)
}

func TestOaiResponsesStreamHandlerIgnoresOrdinaryCompletedToolDefinitions(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.completed","response":{"id":"resp_tool_defs","object":"response","created_at":1710000000,"tools":[{"type":"function"},{"type":"custom"},{"type":"tool_search"},{"type":"namespace"},{"type":"file_search"},{"type":"web_search","search_context_size":"low"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
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
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{},
		StreamStatus:       relaycommon.NewStreamStatus(),
		DisablePing:        true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Len(t, info.ResponsesUsageInfo.BuiltInTools, 2)
	require.Equal(t, 0, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
	require.Equal(t, "low", info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].SearchContextSize)
	require.Equal(t, 0, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolFileSearch].CallCount)
}

func TestOaiResponsesStreamHandlerReturnsRetryableErrorBeforeFirstWrite(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("")),
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-5.5",
		},
		StreamStatus: relaycommon.NewStreamStatus(),
		DisablePing:  true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, types.ErrorCodeUpstreamTransportInterrupted, err.GetErrorCode())
	require.Equal(t, http.StatusBadGateway, err.StatusCode)
	require.Contains(t, err.Error(), "stream disconnected before completion")
	require.False(t, c.Writer.Written())
	require.Empty(t, recorder.Body.String())
}

func TestRetryableUpstreamStreamInterruptedAllowsRetryAfterKeepaliveOnly(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	_, writeErr := c.Writer.Write([]byte(": PING\n\n"))
	require.NoError(t, writeErr)
	require.True(t, c.Writer.Written())

	info := &relaycommon.RelayInfo{
		StreamStatus: relaycommon.NewStreamStatus(),
	}
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonUpstreamInterrupted, io.ErrUnexpectedEOF)

	err := retryableUpstreamStreamInterruptedError(c, info)

	require.NotNil(t, err)
	require.Equal(t, types.ErrorCodeUpstreamTransportInterrupted, err.GetErrorCode())
	require.Equal(t, http.StatusBadGateway, err.StatusCode)
}

func TestRetryableUpstreamStreamInterruptedDoesNotRetryAfterUpstreamChunk(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := &relaycommon.RelayInfo{
		ReceivedResponseCount: 1,
		StreamStatus:          relaycommon.NewStreamStatus(),
	}
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonUpstreamInterrupted, io.ErrUnexpectedEOF)

	err := retryableUpstreamStreamInterruptedError(c, info)

	require.Nil(t, err)
	require.False(t, c.Writer.Written())
}

func TestOaiResponsesStreamHandlerDoesNotRecordAffinityBeforeCompleted(t *testing.T) {
	service.ClearChannelAffinityCacheAll()
	t.Cleanup(func() {
		service.ClearChannelAffinityCacheAll()
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	encryptedContent := fmt.Sprintf("gAAAA-stream-interrupt-%d", time.Now().UnixNano())
	body := strings.Join([]string{
		fmt.Sprintf(`data: {"type":"response.output_item.done","item":{"type":"reasoning","encrypted_content":"%s"}}`, encryptedContent),
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         203,
			UpstreamModelName: "gpt-5.5",
		},
		StreamStatus: relaycommon.NewStreamStatus(),
		DisablePing:  true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, relaycommon.StreamEndReasonUpstreamInterrupted, info.StreamStatus.EndReason)

	hitCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	hitCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{
		"model":"gpt-5.5",
		"input":[{"type":"reasoning","encrypted_content":"%s"}]
	}`, encryptedContent)))
	hitCtx.Request.Header.Set("Content-Type", "application/json")
	_, storageErr := common.GetBodyStorage(hitCtx)
	require.NoError(t, storageErr)

	channelID, found := service.GetPreferredChannelByAffinity(hitCtx, "gpt-5.5", "default")
	require.False(t, found)
	require.Equal(t, 0, channelID)
}

func TestOaiResponsesStreamHandlerRecordsAffinityAfterCompleted(t *testing.T) {
	service.ClearChannelAffinityCacheAll()
	t.Cleanup(func() {
		service.ClearChannelAffinityCacheAll()
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	encryptedContent := fmt.Sprintf("gAAAA-stream-complete-%d", time.Now().UnixNano())
	body := strings.Join([]string{
		fmt.Sprintf(`data: {"type":"response.completed","response":{"id":"resp_affinity","object":"response","created_at":1710000000,"model":"gpt-5.5","output":[{"type":"reasoning","encrypted_content":"%s"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`, encryptedContent),
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
			ChannelId:         203,
			UpstreamModelName: "gpt-5.5",
		},
		StreamStatus: relaycommon.NewStreamStatus(),
		DisablePing:  true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)

	hitCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	hitCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{
		"model":"gpt-5.5",
		"input":[{"type":"reasoning","encrypted_content":"%s"}]
	}`, encryptedContent)))
	hitCtx.Request.Header.Set("Content-Type", "application/json")
	_, storageErr := common.GetBodyStorage(hitCtx)
	require.NoError(t, storageErr)

	channelID, found := service.GetPreferredChannelByAffinity(hitCtx, "gpt-5.5", "default")
	require.True(t, found)
	require.Equal(t, 203, channelID)
}

func TestOaiResponsesHandlerSkipsAffinityAfterEncryptedContextRetry(t *testing.T) {
	service.ClearChannelAffinityCacheAll()
	t.Cleanup(func() {
		service.ClearChannelAffinityCacheAll()
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyResponsesEncryptedContextRetry, true)
	encryptedContent := fmt.Sprintf("gAAAA-encrypted-retry-%d", time.Now().UnixNano())
	body := fmt.Sprintf(`{
		"id":"resp_encrypted_retry",
		"object":"response",
		"created_at":1710000000,
		"model":"gpt-5.5",
		"output":[{"type":"reasoning","encrypted_content":"%s"}],
		"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}
	}`, encryptedContent)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId: 203,
		},
	}

	usage, err := OaiResponsesHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)

	hitCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	hitCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{
		"model":"gpt-5.5",
		"input":[{"type":"reasoning","encrypted_content":"%s"}]
	}`, encryptedContent)))
	hitCtx.Request.Header.Set("Content-Type", "application/json")
	_, storageErr := common.GetBodyStorage(hitCtx)
	require.NoError(t, storageErr)

	channelID, found := service.GetPreferredChannelByAffinity(hitCtx, "gpt-5.5", "default")
	require.False(t, found)
	require.Equal(t, 0, channelID)
}

func TestOaiResponsesStreamHandlerSkipsAffinityAfterEncryptedContextRetry(t *testing.T) {
	service.ClearChannelAffinityCacheAll()
	t.Cleanup(func() {
		service.ClearChannelAffinityCacheAll()
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyResponsesEncryptedContextRetry, true)
	encryptedContent := fmt.Sprintf("gAAAA-stream-encrypted-retry-%d", time.Now().UnixNano())
	body := strings.Join([]string{
		fmt.Sprintf(`data: {"type":"response.completed","response":{"id":"resp_stream_encrypted_retry","object":"response","created_at":1710000000,"model":"gpt-5.5","output":[{"type":"reasoning","encrypted_content":"%s"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`, encryptedContent),
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
			ChannelId:         203,
			UpstreamModelName: "gpt-5.5",
		},
		StreamStatus: relaycommon.NewStreamStatus(),
		DisablePing:  true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)

	hitCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	hitCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{
		"model":"gpt-5.5",
		"input":[{"type":"reasoning","encrypted_content":"%s"}]
	}`, encryptedContent)))
	hitCtx.Request.Header.Set("Content-Type", "application/json")
	_, storageErr := common.GetBodyStorage(hitCtx)
	require.NoError(t, storageErr)

	channelID, found := service.GetPreferredChannelByAffinity(hitCtx, "gpt-5.5", "default")
	require.False(t, found)
	require.Equal(t, 0, channelID)
}

func TestOaiResponsesStreamHandlerTreatsCompletedWithoutDoneAsNormalEnd(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		`data: {"type":"response.completed","response":{"id":"resp_partial","object":"response","created_at":1710000000,"usage":{"input_tokens":8,"output_tokens":1,"total_tokens":9}}}`,
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
	require.NotNil(t, usage)
	require.Equal(t, 8, usage.PromptTokens)
	require.Equal(t, 1, usage.CompletionTokens)
	require.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
	require.True(t, info.StreamStatus.IsNormalEnd())
	require.Nil(t, info.StreamStatus.EndError)
	require.True(t, c.Writer.Written())
	require.Contains(t, recorder.Body.String(), "response.output_text.delta")
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
