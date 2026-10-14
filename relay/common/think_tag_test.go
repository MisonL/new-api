package common

import (
	"sort"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"

	"github.com/stretchr/testify/require"
)

const (
	tOpen  = "\x3cthink\x3e"
	tClose = "\x3c/think\x3e"
)

// tCloseFull is the DeepSeek-V3 end marker: the solidus and the hyphens are
// FULLWIDTH VERTICAL LINE and BOX DRAWINGS LIGHT VERTICAL, not ASCII.
const tCloseFull = "\x3c｜▁end▁of▁thinking｜\x3e"

func TestSplitThinkTaggedContent(t *testing.T) {
	cases := []struct {
		name          string
		in            string
		wantVisible   string
		wantReasoning string
		wantFound     bool
	}{
		{"plain text", "just an answer", "just an answer", "", false},
		{"whole content wrapped", tOpen + "\n17*23=391\n" + tClose + "391", "391", "\n17*23=391\n", true},
		{"tag in the middle", "before " + tOpen + "\nhidden\n" + tClose + " after", "before  after", "\nhidden\n", true},
		{"two pairs", tOpen + "\na\n" + tClose + "m" + tOpen + "\nb\n" + tClose + "z", "mz", "\na\n\nb\n", true},
		{"prose word think is untouched", "I'm thinking about it", "I'm thinking about it", "", false},
		{"unterminated open is untouched", "say " + tOpen + "\ndangling", "say " + tOpen + "\ndangling", "", false},
		{"orphan close is untouched", "a " + tClose + " b", "a " + tClose + " b", "", false},
		{"pair then unterminated tail keeps tail", tOpen + "\na\n" + tClose + "z" + tOpen + "\ntrunc", "z" + tOpen + "\ntrunc", "\na\n", true},
		{
			"v3 close marker",
			tOpen + "\nreasoning\n" + tCloseFull + "answer",
			"answer",
			"\nreasoning\n",
			true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			visible, reasoning, found := SplitThinkTaggedContent(c.in)
			require.Equal(t, c.wantFound, found)
			require.Equal(t, c.wantVisible, visible)
			require.Equal(t, c.wantReasoning, reasoning)
		})
	}
}

// TestSplitThinkTaggedContentLosesNothing pins the property that matters for
// the relay: whatever the split decides, the client still receives every byte
// that was not a tag marker.
func TestSplitThinkTaggedContentLosesNothing(t *testing.T) {
	inputs := []string{
		"plain content",
		tOpen + "\nreasoning\n" + tClose + "answer",
		"before " + tOpen + "\nreasoning\n" + tClose + " after",
		tOpen + "\nonly reasoning, unterminated",
		tOpen + "\na\n" + tClose + "mid" + tOpen + "\nb\n" + tClose,
		"I'm thinking about it",
		"a " + tClose + " b",
	}
	for _, in := range inputs {
		visible, reasoning, found := SplitThinkTaggedContent(in)
		stripped := in
		if found {
			stripped = strings.ReplaceAll(stripped, tOpen, "")
			stripped = strings.ReplaceAll(stripped, tCloseFull, "")
			stripped = strings.ReplaceAll(stripped, tClose, "")
		}
		// The splitter reorders: for each pair it emits what came before the
		// marker as visible text and the wrapped text as reasoning, so the two
		// parts do not concatenate back in original order. Assert the property
		// that actually matters instead: the split loses and invents nothing.
		require.Equal(t, len(stripped), len(visible)+len(reasoning), "input %q changed length", in)
		require.Equal(t, sortedRunes(stripped), sortedRunes(visible+reasoning), "input %q changed content", in)
	}
}

// sortedRunes compares two strings as multisets of characters, which is the
// order-independent form of "the split moved text around but removed none".
func sortedRunes(s string) string {
	runes := []rune(s)
	sort.Slice(runes, func(i, j int) bool { return runes[i] < runes[j] })
	return string(runes)
}

func TestThinkTagStreamSplitterMatchesNonStreaming(t *testing.T) {
	inputs := []string{
		tOpen + "\n17*23=391\n" + tClose + "391",
		"I'm thinking about it",
		"plain content",
		"prefix" + tOpen + "\npart1" + tClose + "mid" + tOpen + "\npart2" + tClose + "suffix",
		tOpen + "\nonly reasoning, unterminated",
		"orphan " + tClose + " close first",
		"I'm thinking about it",
		"a" + tOpen + "b" + tClose + "c" + tOpen + "d" + tClose + "e",
		"cost " + tOpen + "\nx\n" + tClose + "ok",
		tOpen + "\nv3\n" + tCloseFull + "tail",
	}
	for _, in := range inputs {
		wantVisible, wantReasoning, _ := SplitThinkTaggedContent(in)

		s := &ThinkTagStreamSplitter{}
		var vis, rea strings.Builder
		for _, r := range in {
			v, rr := s.Feed(string(r))
			vis.WriteString(v)
			rea.WriteString(rr)
		}
		v, rr := s.Flush()
		vis.WriteString(v)
		rea.WriteString(rr)

		require.Equal(t, wantVisible, vis.String(), "visible mismatch for %q", in)
		require.Equal(t, wantReasoning, rea.String(), "reasoning mismatch for %q", in)
	}
}

func TestThinkTagStreamSplitterChunkBoundaries(t *testing.T) {
	s := &ThinkTagStreamSplitter{}
	var vis, rea strings.Builder
	for _, delta := range []string{"say", " <th", "ink>\nhi\n<", "/think>done"} {
		v, r := s.Feed(delta)
		vis.WriteString(v)
		rea.WriteString(r)
	}
	v, r := s.Flush()
	vis.WriteString(v)
	rea.WriteString(r)

	// The space before the marker belongs to the answer, so it stays visible.
	require.Equal(t, "say done", vis.String())
	require.Equal(t, "\nhi\n", rea.String())
	require.True(t, s.SawTag())
}

func TestThinkTagStreamSplitterUnterminatedGoesBackToVisible(t *testing.T) {
	s := &ThinkTagStreamSplitter{}
	v1, r1 := s.Feed(" hello " + tOpen + "\npartial")
	require.Equal(t, " hello ", v1)
	require.Equal(t, "", r1, "in-tag text stays held until close or flush")

	v2, r2 := s.Flush()
	require.Equal(t, tOpen+"\npartial", v2, "unterminated open must return to visible")
	require.Equal(t, "", r2)
	require.False(t, s.SawTag())
}

func TestThinkTagStreamSplitterHoldsPartialOpenPrefix(t *testing.T) {
	s := &ThinkTagStreamSplitter{}
	v1, _ := s.Feed("cost ")
	require.Equal(t, "cost ", v1, "the open tag starts with a literal angle bracket, so no trailing byte is a tag prefix")

	v2, _ := s.Feed("<")
	require.Equal(t, "", v2, "a lone angle bracket could still open the tag")

	v3, r3 := s.Feed("think>\nsecret\n" + tClose + "ok")
	require.Equal(t, "ok", v3)
	require.Equal(t, "\nsecret\n", r3)

	v4, _ := s.Flush()
	require.Equal(t, "", v4)
	require.True(t, s.SawTag())
}

func chatChunkFrame(t *testing.T, content string) string {
	t.Helper()
	chunk := dto.ChatCompletionsStreamResponse{
		Id:     "chatcmpl-1",
		Object: "chat.completion.chunk",
		Model:  "some-model",
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: common.GetPointer(content)}},
		},
	}
	raw, err := common.Marshal(chunk)
	require.NoError(t, err)
	return string(raw)
}

func TestExtractThinkTagsFromChatChunk(t *testing.T) {
	info := &RelayInfo{}

	frame := chatChunkFrame(t, "answer "+tOpen+"\nreasoning\n"+tClose+" tail")
	out := ExtractThinkTagsFromChatChunk(info, frame)

	var chunk dto.ChatCompletionsStreamResponse
	require.NoError(t, common.UnmarshalJsonStr(out, &chunk))
	require.Len(t, chunk.Choices, 1)
	require.Equal(t, "answer  tail", chunk.Choices[0].Delta.GetContentString())
	require.Equal(t, "\nreasoning\n", chunk.Choices[0].Delta.GetReasoningContent())
	require.True(t, ThinkTagsExtracted(info))
}

func TestExtractThinkTagsFromChatChunkLeavesUntaggedFrameByteIdentical(t *testing.T) {
	info := &RelayInfo{}
	frame := chatChunkFrame(t, "just an answer")
	require.Equal(t, frame, ExtractThinkTagsFromChatChunk(info, frame))
	require.False(t, ThinkTagsExtracted(info))
}

func TestExtractThinkTagsFromChatChunkReassemblesAcrossFrames(t *testing.T) {
	info := &RelayInfo{}

	first := ExtractThinkTagsFromChatChunk(info, chatChunkFrame(t, "say "+tOpen+"\nrea"))
	second := ExtractThinkTagsFromChatChunk(info, chatChunkFrame(t, "soning\n"+tClose+"done"))
	_ = first

	var visible strings.Builder
	var reasoning strings.Builder
	for _, raw := range []string{first, second} {
		var chunk dto.ChatCompletionsStreamResponse
		require.NoError(t, common.UnmarshalJsonStr(raw, &chunk))
		visible.WriteString(chunk.Choices[0].Delta.GetContentString())
		reasoning.WriteString(chunk.Choices[0].Delta.GetReasoningContent())
	}

	require.Equal(t, "say done", visible.String())
	require.Equal(t, "\nreasoning\n", reasoning.String())
}

func TestFinishThinkTagStreamReleasesHeldBytes(t *testing.T) {
	info := &RelayInfo{}
	// A trailing partial open tag is withheld until the stream ends.
	ExtractThinkTagsFromChatChunk(info, chatChunkFrame(t, "answer <thi"))

	terminal := chatChunkFrame(t, "")
	finished := FinishThinkTagStream(info, terminal)

	var chunk dto.ChatCompletionsStreamResponse
	require.NoError(t, common.UnmarshalJsonStr(finished, &chunk))
	require.Equal(t, "<thi", chunk.Choices[0].Delta.GetContentString(), "held prefix must reach the client")

	// The finish step must be safe to repeat: it runs on the terminal frame in
	// more than one place as the frame travels to the client conversion.
	require.Equal(t, finished, FinishThinkTagStream(info, finished))
}

func TestFinishThinkTagStreamNoOpWithoutTags(t *testing.T) {
	info := &RelayInfo{}
	terminal := chatChunkFrame(t, "done")
	require.Equal(t, terminal, FinishThinkTagStream(info, terminal))
}

func TestFlushThinkTagStreamWithoutSplitter(t *testing.T) {
	visible, reasoning := FlushThinkTagStream(&RelayInfo{})
	require.Equal(t, "", visible)
	require.Equal(t, "", reasoning)
}

func TestExtractThinkTagsFromMessage(t *testing.T) {
	message := &dto.Message{
		Role:    "assistant",
		Content: "answer " + tOpen + "\nreasoning\n" + tClose + " tail",
	}
	require.True(t, ExtractThinkTagsFromMessage(message))
	require.Equal(t, "answer  tail", message.StringContent())
	require.Equal(t, "\nreasoning\n", message.GetReasoningContent())
}

func TestExtractThinkTagsFromMessageKeepsStructuredContent(t *testing.T) {
	message := &dto.Message{
		Role: "assistant",
		Content: []dto.MediaContent{
			{Type: dto.ContentTypeText, Text: "answer " + tOpen + "\nreasoning\n" + tClose},
		},
	}
	require.False(t, ExtractThinkTagsFromMessage(message), "array content must not be rewritten into a string")
}

func TestExtractThinkTagsFromMessageDoesNotDuplicateExistingReasoning(t *testing.T) {
	message := &dto.Message{
		Role:             "assistant",
		Content:          tOpen + "\nreasoning\n" + tClose + "answer",
		ReasoningContent: common.GetPointer("\nreasoning\n"),
	}
	require.True(t, ExtractThinkTagsFromMessage(message))
	require.Equal(t, "answer", message.StringContent())
	require.Equal(t, "\nreasoning\n", message.GetReasoningContent(), "upstream reasoning_content must not be doubled")
}

func TestExtractThinkTagsFromMessageLeavesUntaggedMessageAlone(t *testing.T) {
	message := &dto.Message{Role: "assistant", Content: "I'm thinking about it"}
	require.False(t, ExtractThinkTagsFromMessage(message))
	require.Equal(t, "I'm thinking about it", message.StringContent())

	prose := &dto.Message{Role: "assistant", Content: "I think the answer is 4"}
	require.False(t, ExtractThinkTagsFromMessage(prose), "the word think without the tag must not be treated as a tag")
}

func TestExtractThinkTagsFromTextResponseCoversEveryChoice(t *testing.T) {
	resp := &dto.OpenAITextResponse{
		Choices: []dto.OpenAITextResponseChoice{
			{Index: 0, Message: dto.Message{Role: "assistant", Content: tOpen + "\na\n" + tClose + "x"}},
			{Index: 1, Message: dto.Message{Role: "assistant", Content: "plain"}},
		},
	}
	require.True(t, ExtractThinkTagsFromTextResponse(resp))
	require.Equal(t, "x", resp.Choices[0].Message.StringContent())
	require.Equal(t, "\na\n", resp.Choices[0].Message.GetReasoningContent())
	require.Equal(t, "plain", resp.Choices[1].Message.StringContent())
}
