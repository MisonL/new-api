package openai

import (
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

// The markers are spelled with Go escapes so this source carries no literal
// angle brackets; relay/common/think_tag.go documents the convention.
const (
	testThinkOpen  = "\x3cthink\x3e"
	testThinkClose = "\x3c/think\x3e"
)

func claudeThinkTagInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		// SendResponseCount 1 makes the converter emit message_start first.
		SendResponseCount: 1,
		ClaudeConvertInfo: &relaycommon.ClaudeConvertInfo{
			LastMessagesType: relaycommon.LastMessageTypeNone,
		},
	}
}

// sseDataPayloads returns the JSON payload of every "data: " line in an SSE
// body, so assertions can look at the decoded fields instead of the serialised
// bytes.
func sseDataPayloads(t *testing.T, body string) []string {
	t.Helper()
	payloads := make([]string, 0)
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "data: ") {
			payloads = append(payloads, strings.TrimPrefix(line, "data: "))
		}
	}
	return payloads
}

// TestHandleStreamFormatExtractsThinkTagsForClaudeClient is the wiring under
// test: HandleStreamFormat must move think tags out of the content string
// before the frame is converted, otherwise the Claude client receives the raw
// markers as answer text. The model name carries no reasoning marker, so the
// only thing that can open the thinking block is the extraction itself.
func TestHandleStreamFormatExtractsThinkTagsForClaudeClient(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	info := claudeThinkTagInfo()
	frame := dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl-thinktag",
		Model: "some-upstream-model",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				Content: common.GetPointer("here is the answer" + testThinkOpen + "\ninternal reasoning\n" + testThinkClose),
			},
		}},
	}
	raw, err := common.Marshal(frame)
	require.NoError(t, err)

	require.NoError(t, HandleStreamFormat(c, info, string(raw), false, false))

	var reasoning strings.Builder
	var text strings.Builder
	sawThinkingBlock := false
	for _, payload := range sseDataPayloads(t, recorder.Body.String()) {
		var event dto.ClaudeResponse
		if err := common.UnmarshalJsonStr(payload, &event); err != nil {
			continue
		}
		if event.Type == "content_block_start" && event.ContentBlock != nil && event.ContentBlock.Type == "thinking" {
			sawThinkingBlock = true
		}
		if event.Delta == nil {
			continue
		}
		switch event.Delta.Type {
		case "thinking_delta":
			reasoning.WriteString(*event.Delta.Thinking)
		case "text_delta":
			text.WriteString(*event.Delta.Text)
		}
	}

	require.True(t, sawThinkingBlock, "the extracted reasoning must open a thinking block")
	require.Equal(t, "\ninternal reasoning\n", reasoning.String())
	require.Equal(t, "here is the answer", text.String())
	require.NotContains(t, text.String(), testThinkOpen)
	require.NotContains(t, text.String(), testThinkClose)
	require.True(t, relaycommon.ThinkTagsExtracted(info))
}

// TestHandleStreamFormatLeavesThinkTagsForThinkingToContentChannel pins the
// opt-out: a channel configured for thinking_to_content wants the reasoning in
// content as tags, so extraction must not undo the operator's choice.
func TestHandleStreamFormatLeavesThinkTagsForThinkingToContentChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	info := claudeThinkTagInfo()
	info.RelayFormat = types.RelayFormatOpenAI
	frame := dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl-thinktag",
		Model: "some-upstream-model",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				Content: common.GetPointer("answer" + testThinkOpen + "reasoning" + testThinkClose),
			},
		}},
	}
	raw, err := common.Marshal(frame)
	require.NoError(t, err)

	require.NoError(t, HandleStreamFormat(c, info, string(raw), false, true))

	require.Nil(t, info.ThinkTagSplitter, "thinking_to_content must not allocate splitter state")

	payloads := sseDataPayloads(t, recorder.Body.String())
	require.Len(t, payloads, 1)
	var chunk dto.ChatCompletionsStreamResponse
	require.NoError(t, common.UnmarshalJsonStr(payloads[0], &chunk))
	require.Equal(t, "answer"+testThinkOpen+"reasoning"+testThinkClose, chunk.Choices[0].Delta.GetContentString())
}

// TestHandleStreamFormatFlushesSplitterOnTerminalFrame is the flush wiring
// regression: a stream truncated inside an open think block must still deliver
// the buffered reasoning instead of dropping it, and the trailing bytes held by
// the splitter must reach the client. Before the fix FinishThinkTagStream had
// no production caller, so any withheld bytes were silently lost.
func TestHandleStreamFormatFlushesSplitterOnTerminalFrame(t *testing.T) {
	gin.SetMode(gin.TestMode)

	run := func(t *testing.T, relayFormat types.RelayFormat) {
		t.Helper()
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)

		info := claudeThinkTagInfo()
		info.RelayFormat = relayFormat

		first := dto.ChatCompletionsStreamResponse{
			Id:    "chatcmpl-truncated",
			Model: "some-upstream-model",
			Choices: []dto.ChatCompletionsStreamResponseChoice{{
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					Content: common.GetPointer("answer " + testThinkOpen + "\ntruncated reasoning"),
				},
			}},
		}
		raw, err := common.Marshal(first)
		require.NoError(t, err)
		require.NoError(t, HandleStreamFormat(c, info, string(raw), false, false))

		// 终端帧：delta 为空，只有 finish_reason。
		fr := "length"
		terminal := dto.ChatCompletionsStreamResponse{
			Id:    "chatcmpl-truncated",
			Model: "some-upstream-model",
			Choices: []dto.ChatCompletionsStreamResponseChoice{{
				Delta:        dto.ChatCompletionsStreamResponseChoiceDelta{},
				FinishReason: &fr,
			}},
		}
		raw, err = common.Marshal(terminal)
		require.NoError(t, err)
		require.NoError(t, HandleStreamFormat(c, info, string(raw), false, false))
		// flush 接线在 HandleFinalResponseWithError：流结束时一次性释放。
		require.NoError(t, HandleFinalResponseWithError(c, info, string(raw),
			"chatcmpl-truncated", 0, "some-upstream-model", "", &dto.Usage{}, false))

		var recovered string
		for _, payload := range sseDataPayloads(t, recorder.Body.String()) {
			if relayFormat == types.RelayFormatGemini {
				var gemini dto.GeminiChatResponse
				if err := common.UnmarshalJsonStr(payload, &gemini); err == nil {
					for _, candidate := range gemini.Candidates {
						for _, part := range candidate.Content.Parts {
							recovered += part.Text
						}
					}
				}
				continue
			}
			var event dto.ClaudeResponse
			if err := common.UnmarshalJsonStr(payload, &event); err != nil {
				continue
			}
			if event.Delta != nil && event.Delta.Thinking != nil {
				recovered += *event.Delta.Thinking
			}
			if event.Delta != nil && event.Delta.Text != nil {
				recovered += *event.Delta.Text
			}
		}
		// Flush 把未闭合标记按"不是标签"规则以原始字节归还（与
		// SplitThinkTaggedContent 的语义一致）。关键是内容不能丢：推理或
		// 原始字节二者必有一处承载被缓冲的文本。
		require.Contains(t, recovered, "\ntruncated reasoning",
			"bytes buffered by the splitter must reach the client, not be dropped")
	}

	t.Run("claude", func(t *testing.T) { run(t, types.RelayFormatClaude) })
	t.Run("gemini", func(t *testing.T) { run(t, types.RelayFormatGemini) })
}

// TestHandleStreamFormatKeepsPerFrameExtractionAcrossFrames is the regression
// for the 06a063ef0 follow-up: an earlier attempt called FinishThinkTagStream
// on every frame, which re-fed each frame's already-extracted bytes into the
// splitter, leaked the raw open marker as content and broke extraction for the
// rest of the stream. Per-frame handling must only extract; the flush is
// terminal-only (HandleFinalResponseWithError).
func TestHandleStreamFormatKeepsPerFrameExtractionAcrossFrames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	info := claudeThinkTagInfo()

	// 帧尾停留在 open 块内部：内容必须被 withheld，原始标记不得外泄。
	first := dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl-multiframe",
		Model: "some-upstream-model",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				Content: common.GetPointer("a" + testThinkOpen + "held1"),
			},
		}},
	}
	raw, err := common.Marshal(first)
	require.NoError(t, err)
	require.NoError(t, HandleStreamFormat(c, info, string(raw), false, false))

	second := dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl-multiframe",
		Model: "some-upstream-model",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				Content: common.GetPointer("held2" + testThinkClose + "b"),
			},
		}},
	}
	raw, err = common.Marshal(second)
	require.NoError(t, err)
	require.NoError(t, HandleStreamFormat(c, info, string(raw), false, false))

	var thinking string
	var text string
	for _, payload := range sseDataPayloads(t, recorder.Body.String()) {
		var event dto.ClaudeResponse
		if err := common.UnmarshalJsonStr(payload, &event); err != nil {
			continue
		}
		if event.Delta != nil && event.Delta.Thinking != nil {
			thinking += *event.Delta.Thinking
		}
		if event.Delta != nil && event.Delta.Text != nil {
			text += *event.Delta.Text
		}
	}
	require.Equal(t, "ab", text, "bytes before the open marker and after the close marker are visible")
	require.Equal(t, "held1held2", thinking, "reasoning reassembles across frames")
	require.NotContains(t, text, testThinkOpen, "the raw marker must not leak as content")
	require.NotContains(t, text, "held1", "withheld in-tag bytes must not leak as content")
}
