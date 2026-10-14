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
