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

func TestStreamTokenRelayModeUsesChatCompletionsForConvertedClaude(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode:              relayconstant.RelayModeUnknown,
		RelayFormat:            types.RelayFormatClaude,
		RequestConversionChain: []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAI},
	}

	require.Equal(t, relayconstant.RelayModeChatCompletions, streamTokenRelayMode(info))
}

func TestProcessTokensCountsConvertedClaudeOpenAIChatChunks(t *testing.T) {
	streamItems := []string{
		`{"id":"chatcmpl_deepseek","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hello"}}]}`,
		`{"id":"chatcmpl_deepseek","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"reasoning_content":" because"}}]}`,
		`{"id":"chatcmpl_deepseek","object":"chat.completion.chunk","choices":[{"index":0,"finish_reason":"stop"}]}`,
	}
	var responseTextBuilder strings.Builder
	var toolCount int

	err := processTokens(streamTokenRelayMode(&relaycommon.RelayInfo{
		RelayMode:              relayconstant.RelayModeUnknown,
		RelayFormat:            types.RelayFormatClaude,
		RequestConversionChain: []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAI},
	}), streamItems, &responseTextBuilder, &toolCount)

	require.NoError(t, err)
	require.Equal(t, "hello because", responseTextBuilder.String())
	require.Equal(t, 0, toolCount)
}

func TestStreamTokenRelayModeLeavesNativeCompletions(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeCompletions,
	}

	require.Equal(t, relayconstant.RelayModeCompletions, streamTokenRelayMode(info))
}

func TestProcessStreamResponseCountsReasoningAlias(t *testing.T) {
	reasoning := "thinking"
	streamResponse := dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					Reasoning: &reasoning,
				},
			},
		},
	}
	var responseTextBuilder strings.Builder
	var toolCount int

	require.NoError(t, ProcessStreamResponse(streamResponse, &responseTextBuilder, &toolCount))
	require.Equal(t, reasoning, responseTextBuilder.String())
}

func TestOaiStreamHandlerEstimatesUsageForConvertedClaudeWithoutUpstreamUsage(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	body := strings.Join([]string{
		`data: {"id":"chatcmpl_deepseek","object":"chat.completion.chunk","model":"deepseek-v4-flash","choices":[{"index":0,"delta":{"content":"hello world"}}]}`,
		`data: {"id":"chatcmpl_deepseek","object":"chat.completion.chunk","model":"deepseek-v4-flash","choices":[{"index":0,"finish_reason":"stop"}]}`,
		`data: [DONE]`,
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		RelayMode:              relayconstant.RelayModeUnknown,
		RelayFormat:            types.RelayFormatClaude,
		ShouldIncludeUsage:     false,
		IsStream:               true,
		DisablePing:            true,
		RequestConversionChain: []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAI},
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "deepseek-v4-flash",
		},
		ClaudeConvertInfo: &relaycommon.ClaudeConvertInfo{},
	}
	info.SetEstimatePromptTokens(10)

	usage, apiErr := OaiStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, 10, usage.PromptTokens)
	require.Greater(t, usage.CompletionTokens, 0)
	require.Equal(t, usage.PromptTokens+usage.CompletionTokens, usage.TotalTokens)
}
