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
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 第十轮 relay 修复的回归测试组：
//  1. thinking_to_content 同帧 reasoning+content 时不得丢弃可见 content；
//  2. 纯 reasoning 流在终止处必须闭合 </think>；
//  3. include_usage=false 的合并型终帧必须剥离 usage 后仍下发（保 finish_reason）；
//  4. Claude 客户端下"带 usage 内容帧 + 空 choices 无 usage 帧"必须与转换器同口径
//     地排空缓冲（扣留字节不得丢失）；
//  5. Gemini 客户端截断流的终端帧必须补 finishReason（与 Claude 分支对称）。

func openAiEmittedFrames(t *testing.T, body string) []dto.ChatCompletionsStreamResponse {
	t.Helper()
	var frames []dto.ChatCompletionsStreamResponse
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" || payload == "" {
			continue
		}
		var frame dto.ChatCompletionsStreamResponse
		require.NoError(t, common.UnmarshalJsonStr(payload, &frame), "frame: %s", payload)
		frames = append(frames, frame)
	}
	return frames
}

func frameContents(frames []dto.ChatCompletionsStreamResponse) []string {
	out := make([]string, 0, len(frames))
	for _, frame := range frames {
		for _, choice := range frame.Choices {
			out = append(out, choice.Delta.GetContentString())
		}
	}
	return out
}

func containsContent(frames []dto.ChatCompletionsStreamResponse, want string) bool {
	for _, got := range frameContents(frames) {
		if strings.Contains(got, want) {
			return true
		}
	}
	return false
}

func newThinkingToContentInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		RelayFormat:         types.RelayFormatOpenAI,
		ChannelMeta:         &relaycommon.ChannelMeta{UpstreamModelName: "m"},
		ThinkingContentInfo: relaycommon.ThinkingContentInfo{IsFirstThinkingContent: true},
	}
}

func newStreamRecorder(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	return c, recorder
}

func TestThinkingToContentKeepsSameFrameVisibleContent(t *testing.T) {
	c, recorder := newStreamRecorder(t)
	info := newThinkingToContentInfo()

	require.NoError(t, HandleStreamFormat(c, info,
		`{"id":"x","model":"m","choices":[{"index":0,"delta":{"reasoning_content":"R1"}}]}`, false, true))
	require.NoError(t, HandleStreamFormat(c, info,
		`{"id":"x","model":"m","choices":[{"index":0,"delta":{"reasoning_content":"R2","content":"VISIBLE2"}}]}`, false, true))

	frames := openAiEmittedFrames(t, recorder.Body.String())
	require.True(t, containsContent(frames, "</think>"), "转场闭合标签应发出")
	require.True(t, containsContent(frames, "R2"), "第二轮推理应发出")
	require.True(t, containsContent(frames, "VISIBLE2"),
		"同帧可见 content 不得被推理覆盖丢弃，frames=%q", frameContents(frames))
}

func TestThinkingToContentClosesTagOnReasoningOnlyStream(t *testing.T) {
	c, recorder := newStreamRecorder(t)
	info := newThinkingToContentInfo()

	require.NoError(t, HandleStreamFormat(c, info,
		`{"id":"x","model":"m","choices":[{"index":0,"delta":{"reasoning_content":"R1"}}]}`, false, true))
	// 纯推理流直接结束：finish_reason 帧之前必须闭合 think 块。
	require.NoError(t, HandleStreamFormat(c, info,
		`{"id":"x","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"length"}]}`, false, true))

	frames := openAiEmittedFrames(t, recorder.Body.String())
	require.True(t, containsContent(frames, "</think>"),
		"纯推理流结束时必须闭合 </think>，否则客户端渲染未闭合 think 块，frames=%q", frameContents(frames))
}

func TestIncludeUsageFalseKeepsMergedTerminalFinishReason(t *testing.T) {
	c, recorder := newStreamRecorder(t)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		RelayMode:          relayconstant.RelayModeChatCompletions,
		RelayFormat:        types.RelayFormatOpenAI,
		ShouldIncludeUsage: false,
		IsStream:           true,
		DisablePing:        true,
		ChannelMeta:        &relaycommon.ChannelMeta{UpstreamModelName: "m"},
	}
	info.SetEstimatePromptTokens(10)
	body := "data: {\"id\":\"x\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n" +
		"data: {\"id\":\"x\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"length\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2,\"total_tokens\":3}}\n\n" +
		"data: [DONE]\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	_, apiErr := OaiStreamHandler(c, info, resp)
	require.Nil(t, apiErr)

	frames := openAiEmittedFrames(t, recorder.Body.String())
	foundFinish := false
	for _, frame := range frames {
		for _, choice := range frame.Choices {
			if choice.FinishReason != nil && *choice.FinishReason == "length" {
				foundFinish = true
			}
		}
	}
	require.True(t, foundFinish, "include_usage=false 时合并型终帧的 finish_reason 不得丢失")

	// 客户端显式退出 usage：补发的终帧不得携带 usage 对象。
	for _, frame := range frames {
		require.Nil(t, frame.Usage, "include_usage=false 的终帧必须剥离 usage")
	}
}

func TestClaudeMidStreamUsageFrameFlushesHeldBytes(t *testing.T) {
	c, recorder := newStreamRecorder(t)
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		ClaudeConvertInfo: &relaycommon.ClaudeConvertInfo{
			LastMessagesType: relaycommon.LastMessageTypeNone,
		},
	}
	// f1：带 usage 的内容帧，splitter 扣住未闭合标签后的字节。
	require.NoError(t, HandleStreamFormat(c, info,
		`{"id":"p","model":"m","choices":[{"index":0,"delta":{"content":"safe<think>hold"}}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`, false, false))
	// f2：空 choices、无本帧 usage——转换器会按累积 usage 终止，helper 必须同口径
	// 排空缓冲，否则扣留字节永久丢失。
	require.NoError(t, HandleStreamFormat(c, info, `{"id":"p","model":"m","choices":[]}`, false, false))

	body := recorder.Body.String()
	require.Contains(t, body, "hold", "空 choices 无 usage 帧触发终止前必须排空扣留字节：%s", body)
	require.Contains(t, body, "message_stop")
}

// 反向守卫：从未出现 usage 的空 choices 帧不得被判为终端（否则上游的心跳/占位
// 空帧会提前闭合生命周期，后续真实内容全部丢弃）。
func TestClaudeBlankChoicesWithoutUsageIsNotTerminal(t *testing.T) {
	c, recorder := newStreamRecorder(t)
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		ClaudeConvertInfo: &relaycommon.ClaudeConvertInfo{
			LastMessagesType: relaycommon.LastMessageTypeNone,
		},
	}
	require.NoError(t, HandleStreamFormat(c, info,
		`{"id":"p","model":"m","choices":[{"index":0,"delta":{"content":"A"}}]}`, false, false))
	require.NoError(t, HandleStreamFormat(c, info, `{"id":"p","model":"m","choices":[]}`, false, false))
	require.False(t, info.ClaudeConvertInfo.Done, "无 usage 的空帧不得闭合生命周期")
	require.NoError(t, HandleStreamFormat(c, info,
		`{"id":"p","model":"m","choices":[{"index":0,"delta":{"content":"B"}}]}`, false, false))

	body := recorder.Body.String()
	require.Contains(t, body, `"text":"B"`, "空帧之后的真实内容必须继续下发：%s", body)
	require.NotContains(t, body, "message_stop", "此时不应终止")
}

func TestGeminiTruncatedTerminalFrameGetsFinishReason(t *testing.T) {
	c, recorder := newStreamRecorder(t)
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatGemini,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "m"},
	}
	require.NoError(t, HandleStreamFormat(c, info,
		`{"id":"x","model":"m","choices":[{"index":0,"delta":{"content":"A"}}]}`, false, false))
	last := `{"id":"x","model":"m","choices":[{"index":0,"delta":{"content":"B"}}]}`
	usage := &dto.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3}
	require.NoError(t, HandleFinalResponseWithError(c, info, last, "x", 0, "m", "", usage, false))

	var geminiFrames []dto.GeminiChatResponse
	for _, line := range strings.Split(recorder.Body.String(), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var frame dto.GeminiChatResponse
		require.NoError(t, common.UnmarshalJsonStr(strings.TrimPrefix(line, "data: "), &frame))
		geminiFrames = append(geminiFrames, frame)
	}
	require.NotEmpty(t, geminiFrames, "截断流必须仍有输出：%s", recorder.Body.String())

	lastFrame := geminiFrames[len(geminiFrames)-1]
	require.NotEmpty(t, lastFrame.Candidates)
	finishReason := lastFrame.Candidates[0].FinishReason
	require.NotNil(t, finishReason, "截断流的终端帧必须补 finishReason，与 Claude 分支对称")
	require.NotEmpty(t, *finishReason)
}
