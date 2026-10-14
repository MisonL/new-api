package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

// The think-tag markers are spelled with Go escapes so this source carries no
// literal angle brackets; relay/common/think_tag.go documents the convention.
const (
	testThinkOpen  = "\x3cthink\x3e"
	testThinkClose = "\x3c/think\x3e"
)

// nonReasoningModel carries no model-name marker, so the name-based gate emits
// no thinking block for it on its own.
const nonReasoningModel = "some-upstream-model"

func taggedContent(visible string, reasoning string) string {
	if reasoning == "" {
		return visible
	}
	return visible + testThinkOpen + reasoning + testThinkClose
}

// feedThroughExtraction runs the relay-level extraction that the stream
// handlers perform (HandleStreamFormat and the Responses compatibility paths)
// and returns the frame the converter actually receives.
func feedThroughExtraction(t *testing.T, info *relaycommon.RelayInfo, frame *dto.ChatCompletionsStreamResponse) *dto.ChatCompletionsStreamResponse {
	t.Helper()
	raw, err := common.Marshal(frame)
	require.NoError(t, err)
	rewritten := relaycommon.ExtractThinkTagsFromChatChunk(info, string(raw))
	var out dto.ChatCompletionsStreamResponse
	require.NoError(t, common.UnmarshalJsonStr(rewritten, &out))
	return &out
}

func streamFrame(content string, finishReason string) *dto.ChatCompletionsStreamResponse {
	frame := &dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl-thinktag",
		Model: nonReasoningModel,
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: testPtr(content)},
		}},
	}
	if finishReason != "" {
		frame.Choices[0].FinishReason = testPtr(finishReason)
	}
	return frame
}

// collectedClaudeStream reports the thinking and text deltas a Claude client
// would render, failing when any block sequence is malformed.
func collectedClaudeStream(t *testing.T, responses []*dto.ClaudeResponse) (thinking string, text string) {
	t.Helper()
	requireClaudeStreamBlocksWellFormed(t, responses)
	for _, response := range responses {
		if response.Delta == nil {
			continue
		}
		switch response.Delta.Type {
		case "thinking_delta":
			thinking += *response.Delta.Thinking
		case "text_delta":
			text += *response.Delta.Text
		}
	}
	return thinking, text
}

// TestThinkTaggedUpstreamBecomesClaudeThinkingBlock is the reported symptom: a
// think tag arriving on the Claude path used to be forwarded verbatim as answer
// text, so the client printed the raw markers instead of rendering a thinking
// block.
func TestThinkTaggedUpstreamBecomesClaudeThinkingBlock(t *testing.T) {
	info := newClaudeStreamInfo(1)

	frame := feedThroughExtraction(t, info, streamFrame(taggedContent("here is the answer", "\ninternal reasoning\n"), "stop"))
	responses := StreamResponseOpenAI2Claude(frame, info)

	thinking, text := collectedClaudeStream(t, responses)
	require.Equal(t, "\ninternal reasoning\n", thinking, "the wrapped text must be delivered as reasoning")
	require.Equal(t, "here is the answer", text, "the markers must not reach the visible answer")
	require.NotContains(t, text, testThinkOpen)
	require.NotContains(t, text, testThinkClose)
}

// TestThinkTaggedStreamAcrossFramesMatchesNonStreaming covers what the
// non-streaming converter cannot see: the tag straddles two upstream frames, so
// only the streaming splitter can reassemble it.
func TestThinkTaggedStreamAcrossFramesMatchesNonStreaming(t *testing.T) {
	info := newClaudeStreamInfo(1)

	// The pair is split across frames on purpose, so it cannot be built with
	// taggedContent: the open marker arrives first, the close marker later.
	first := feedThroughExtraction(t, info, streamFrame(testThinkOpen+"\ninternal rea", ""))
	require.Empty(t, first.Choices[0].Delta.GetReasoningContent(), "an incomplete pair stays withheld")
	require.Empty(t, first.Choices[0].Delta.GetContentString(), "nothing before the open marker is visible yet")

	second := feedThroughExtraction(t, info, streamFrame("soning\n"+testThinkClose, "stop"))
	responses := StreamResponseOpenAI2Claude(second, info)

	thinking, text := collectedClaudeStream(t, responses)
	require.Equal(t, "\ninternal reasoning\n", thinking)
	require.Equal(t, "", text)
	require.True(t, relaycommon.ThinkTagsExtracted(info))
}

// TestThinkTaggedResponseOpenAI2ClaudeKeepsReasoning is the non-streaming
// sibling: the whole tagged content arrives in one response and must still be
// exposed as a thinking block for a model the name-based gate does not cover.
func TestThinkTaggedResponseOpenAI2ClaudeKeepsReasoning(t *testing.T) {
	info := newClaudeStreamInfo(1)
	response := &dto.OpenAITextResponse{
		Id:    "chatcmpl-thinktag",
		Model: nonReasoningModel,
		Choices: []dto.OpenAITextResponseChoice{{
			Message:      dto.Message{Role: "assistant", Content: taggedContent("the answer", "\ninternal reasoning\n")},
			FinishReason: "stop",
		}},
	}

	claudeResponse, err := ResponseOpenAI2ClaudeWithError(response, info)
	require.NoError(t, err)
	require.Len(t, claudeResponse.Content, 2)
	require.Equal(t, "thinking", claudeResponse.Content[0].Type)
	require.Equal(t, "\ninternal reasoning\n", *claudeResponse.Content[0].Thinking)
	require.Equal(t, "text", claudeResponse.Content[1].Type)
	require.Equal(t, "the answer", *claudeResponse.Content[1].Text)
}

// TestUntaggedUpstreamDoesNotGainAThinkingBlock guards the other direction: an
// ordinary answer for the same model must not grow a thinking block.
func TestUntaggedUpstreamDoesNotGainAThinkingBlock(t *testing.T) {
	info := newClaudeStreamInfo(1)

	frame := feedThroughExtraction(t, info, streamFrame("just an answer", "stop"))
	responses := StreamResponseOpenAI2Claude(frame, info)

	thinking, text := collectedClaudeStream(t, responses)
	require.Equal(t, "", thinking)
	require.Equal(t, "just an answer", text)
	require.False(t, relaycommon.ThinkTagsExtracted(info))
}

// TestThinkTagExtractionIsWhatOpensTheThinkingBlock pins the causal link the
// fix depends on: the same tagged frame, converted without the relay-level
// extraction, still emits the raw markers as answer text and no thinking
// block, because the model name carries no reasoning marker. That is the
// reported symptom, and it is why HandleStreamFormat has to run the splitter
// before conversion.
func TestThinkTagExtractionIsWhatOpensTheThinkingBlock(t *testing.T) {
	content := taggedContent("the answer", "\ninternal reasoning\n")

	withoutExtraction := newClaudeStreamInfo(1)
	raw := StreamResponseOpenAI2Claude(streamFrame(content, "stop"), withoutExtraction)
	thinking, text := collectedClaudeStream(t, raw)
	require.Equal(t, "", thinking, "no extraction means no thinking block for this model")
	require.Equal(t, content, text, "the raw markers are forwarded as answer text")

	withExtraction := newClaudeStreamInfo(1)
	extracted := feedThroughExtraction(t, withExtraction, streamFrame(content, "stop"))
	thinking, text = collectedClaudeStream(t, StreamResponseOpenAI2Claude(extracted, withExtraction))
	require.Equal(t, "\ninternal reasoning\n", thinking)
	require.Equal(t, "the answer", text)
}
