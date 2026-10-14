package common

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

// Think tags: some upstreams pack the model's reasoning into the assistant
// `content` string wrapped in think markers instead of returning it in
// `reasoning_content`. That is DeepSeek's convention, and aggregator channels
// that front DeepSeek pass it through unchanged.
//
// Anthropic Messages, OpenAI Chat Completions, Gemini and the Responses API
// each have a dedicated field for reasoning, and every mainstream AI coding
// tool reads that field: Claude Code renders type:"thinking" blocks, Codex CLI
// renders reasoning summaries, Gemini CLI renders thought parts, and the
// Cline/Roo/Continue family renders reasoning_content. None of them understands
// the raw tag, so it shows up as ordinary assistant text.
//
// The split below runs on the response path, before the upstream OpenAI frame
// is converted to the client's protocol, so the extracted reasoning lands in
// the dedicated field and the visible text loses only the tag markers.
//
// Rules, pinned by think_tag_test.go:
//
//   - Only a COMPLETE open/close pair changes the output. A lone open marker is
//     ordinary English ("I'm thinking about it") and must pass through
//     untouched, or non-tagging responses would be mangled.
//   - The reasoning text is never discarded: the caller places it in the
//     reasoning field of its protocol (thinking block, reasoning_content,
//     thought part), so a model that reasons out loud does not lose that
//     reasoning and degrade over a multi-turn conversation.
//   - Visible text outside the pair is preserved byte for byte. Unlike
//     service.splitThinkTaggedText, which reassembles whole request-history
//     messages and may trim, response content is a delta stream and trimming
//     would eat the spaces between fragments.
const (
	// thinkTagOpen opens the reasoning block DeepSeek-style upstreams emit
	// inside the assistant content string.
	thinkTagOpen = "\x3cthink\x3e"
	// thinkTagClose is the matching end marker, the DeepSeek-R1 form.
	thinkTagClose = "\x3c/think\x3e"
	// thinkTagCloseFull is the DeepSeek-V3 form of the end marker, which uses
	// U+FF5C and U+2581 instead of the ASCII solidus and hyphen.
	//
	// The markers are spelled with escapes on purpose: the source then carries
	// no literal angle brackets, which keeps the constants readable in editors
	// and diffs that would otherwise swallow them.
	thinkTagCloseFull = "\x3c｜▁end▁of▁thinking｜\x3e"
)

// thinkOpenTags and thinkCloseTags are the marker variants seen in the wild.
var (
	thinkOpenTags  = []string{thinkTagOpen}
	thinkCloseTags = []string{thinkTagCloseFull, thinkTagClose}
)

// SplitThinkTaggedContent splits a complete content string into the text the
// user should see and the reasoning that was wrapped in think tags.
//
// found reports whether at least one complete tag pair was consumed. When
// found is false the input is returned unchanged: an unterminated open tag
// keeps its raw text, because treating ordinary prose as the start of a tag
// would rewrite it.
func SplitThinkTaggedContent(content string) (visible string, reasoning string, found bool) {
	if !hasAnyTag(content, thinkOpenTags) {
		return content, "", false
	}

	var visibleBuilder strings.Builder
	var reasoningBuilder strings.Builder
	cursor := 0
	for {
		openIdx, openLen := indexAnyTag(content, cursor, thinkOpenTags)
		if openIdx < 0 {
			visibleBuilder.WriteString(content[cursor:])
			break
		}
		reasoningStart := openIdx + openLen
		closeIdx, closeLen := indexAnyTag(content, reasoningStart, thinkCloseTags)
		if closeIdx < 0 {
			// Unterminated: not a complete pair. Keep the raw text.
			visibleBuilder.WriteString(content[cursor:])
			break
		}
		visibleBuilder.WriteString(content[cursor:openIdx])
		reasoningBuilder.WriteString(content[reasoningStart:closeIdx])
		cursor = closeIdx + closeLen
		found = true
	}

	if !found {
		return content, "", false
	}
	return visibleBuilder.String(), reasoningBuilder.String(), true
}

// ThinkTagStreamSplitter is the streaming form of SplitThinkTaggedContent.
//
// Delta boundaries are adversarial: a tag can straddle two upstream deltas
// ("<thi" / "nk>"), and until the closing tag appears there is no way to tell a
// real tag from prose. So the splitter:
//
//   - withholds a trailing fragment that could still turn into the open tag;
//   - once the open tag is seen, withholds everything until the close tag
//     completes the pair, then releases the inner text as reasoning (thinking
//     arrives as one block when the tag closes, not incrementally);
//   - if the stream ends while still inside an open tag, Flush returns the
//     original bytes as visible text, matching SplitThinkTaggedContent's
//     "unterminated is not a tag" rule.
//
// When a Feed call returns both parts, the caller must emit reasoning before
// visible: within one tag pair the reasoning always precedes the text after it.
type ThinkTagStreamSplitter struct {
	pending  []byte
	searched int
	inTag    bool
	sawTag   bool
}

// SawTag reports whether at least one complete think tag pair has been seen.
func (s *ThinkTagStreamSplitter) SawTag() bool {
	return s.sawTag
}

// HasPending reports whether bytes are still held back awaiting a later delta.
func (s *ThinkTagStreamSplitter) HasPending() bool {
	return len(s.pending) > 0
}

// Feed consumes one upstream content delta and returns the parts that are
// ready to emit. Either return value may be empty.
func (s *ThinkTagStreamSplitter) Feed(delta string) (visible string, reasoning string) {
	if delta == "" {
		return "", ""
	}
	s.pending = append(s.pending, delta...)

	var visibleBuilder strings.Builder
	var reasoningBuilder strings.Builder
	for {
		if s.inTag {
			closeIdx, closeLen := indexAnyTagBytes(s.pending, s.searched, thinkCloseTags)
			if closeIdx >= 0 {
				reasoningBuilder.WriteString(string(s.pending[:closeIdx]))
				s.pending = s.pending[closeIdx+closeLen:]
				s.inTag = false
				s.sawTag = true
				s.searched = 0
				continue
			}
			// Complete pair has not arrived yet: hold everything as in-tag.
			s.searched = closeSearchFloor(len(s.pending), longestTagLen(thinkCloseTags))
			break
		}

		openIdx, openLen := indexAnyTagBytes(s.pending, s.searched, thinkOpenTags)
		if openIdx >= 0 {
			visibleBuilder.WriteString(string(s.pending[:openIdx]))
			s.pending = s.pending[openIdx+openLen:]
			s.inTag = true
			s.searched = 0
			continue
		}

		// Outside a tag: emit everything that cannot still become an open tag.
		hold := partialAnyTagSuffixLen(s.pending, thinkOpenTags)
		if hold == len(s.pending) {
			// The whole pending buffer is a tag prefix; wait for more bytes.
			s.searched = 0
			break
		}
		visibleBuilder.WriteString(string(s.pending[:len(s.pending)-hold]))
		s.pending = s.pending[len(s.pending)-hold:]
		// Scan the withheld suffix from its start next time: the marker may
		// begin inside those bytes, and searched is an offset into the buffer
		// that survives, not into the bytes that were already emitted.
		s.searched = 0
		break
	}

	return visibleBuilder.String(), reasoningBuilder.String()
}

// Flush releases anything still held back at end of stream. Held bytes are
// always released as visible text: an unterminated open tag was never a real
// pair, so its bytes belong to the answer.
func (s *ThinkTagStreamSplitter) Flush() (visible string, reasoning string) {
	if len(s.pending) == 0 {
		return "", ""
	}
	held := string(s.pending)
	s.pending = nil
	s.searched = 0
	if s.inTag {
		// Stream ended inside an open tag: the pair never completed. The open
		// marker itself was consumed on entry and is re-emitted here so the
		// visible text stays byte-for-byte identical to the upstream content.
		s.inTag = false
		return thinkTagOpen + held, ""
	}
	return held, ""
}

// SplitDelta extracts think tags from one streaming chat delta in place,
// moving the reasoning into the delta's reasoning_content field. It reports
// whether the delta changed, so the caller can skip re-serialising untouched
// frames.
func (s *ThinkTagStreamSplitter) SplitDelta(d *dto.ChatCompletionsStreamResponseChoiceDelta) bool {
	if d == nil {
		return false
	}
	content := d.GetContentString()
	if content == "" && !s.HasPending() {
		return false
	}
	visible, reasoning := s.Feed(content)
	if visible == content && reasoning == "" {
		return false
	}
	d.SetContentString(visible)
	appendReasoningWithoutDuplication(d, reasoning)
	return true
}

// ThinkTagsExtracted reports whether extraction ran and found a complete pair
// while converting this request. Protocol converters that normally gate
// thinking blocks on the model name consult it so that tag-extracted reasoning
// still reaches the client instead of being dropped.
func ThinkTagsExtracted(info *RelayInfo) bool {
	return info != nil && info.ThinkTagSplitter != nil && info.ThinkTagSplitter.SawTag()
}

// ExtractThinkTagsFromChatChunk rewrites one raw upstream chat-completions frame
// so any think tags in a choice's content move into reasoning_content. It
// returns the frame unchanged when nothing was extracted, keeping the fast path
// free of a re-serialisation. info carries the per-request splitter state, so a
// tag split across frames still reassembles.
//
// The cheap prefilter below exists only to keep untagged traffic off the JSON
// parse; it must never skip a frame that could carry a marker. Matching the
// ASCII word "think" is not enough: a frame can end inside the marker
// ("...<thi"), and those bytes have to reach the splitter or the reassembly
// loses them.
func ExtractThinkTagsFromChatChunk(info *RelayInfo, data string) string {
	if info == nil || data == "" {
		return data
	}
	if info.ThinkTagSplitter == nil && !mayCarryThinkTag(data) {
		return data
	}
	splitter := info.ThinkTagSplitter
	if splitter == nil {
		splitter = &ThinkTagStreamSplitter{}
		info.ThinkTagSplitter = splitter
	}

	var chunk dto.ChatCompletionsStreamResponse
	if err := common.UnmarshalJsonStr(data, &chunk); err != nil {
		return data
	}
	changed := false
	for i := range chunk.Choices {
		if splitter.SplitDelta(&chunk.Choices[i].Delta) {
			changed = true
		}
	}
	if !changed {
		return data
	}
	out, err := common.Marshal(chunk)
	if err != nil {
		return data
	}
	return string(out)
}

// FlushThinkTagStream releases any think-tag bytes still held back at end of
// stream. Callers place the parts in their protocol's reasoning and content
// fields; empty values mean the stream ended on a tag boundary, or that no tag
// was ever seen and there is nothing to release.
func FlushThinkTagStream(info *RelayInfo) (visible string, reasoning string) {
	if info == nil || info.ThinkTagSplitter == nil {
		return "", ""
	}
	return info.ThinkTagSplitter.Flush()
}

// FinishThinkTagStream processes the terminal chat frame of a stream: it feeds
// the frame's own content through the splitter and releases anything still held
// back, so no visible byte is lost when the stream ends mid-tag or mid-prefix.
// The returned frame is safe to hand to the client conversion unchanged when
// nothing was extracted. It is idempotent, so a frame may pass through it more
// than once without duplicating text.
func FinishThinkTagStream(info *RelayInfo, data string) string {
	if info == nil || info.ThinkTagSplitter == nil {
		return data
	}
	splitter := info.ThinkTagSplitter

	var chunk dto.ChatCompletionsStreamResponse
	if err := common.UnmarshalJsonStr(data, &chunk); err != nil {
		return data
	}
	changed := false
	for i := range chunk.Choices {
		if splitter.SplitDelta(&chunk.Choices[i].Delta) {
			changed = true
		}
	}

	visible, reasoning := splitter.Flush()
	if visible != "" || reasoning != "" {
		if len(chunk.Choices) == 0 {
			chunk.Choices = []dto.ChatCompletionsStreamResponseChoice{{Index: 0}}
		}
		delta := &chunk.Choices[0].Delta
		appendReasoningWithoutDuplication(delta, reasoning)
		if visible != "" {
			delta.SetContentString(delta.GetContentString() + visible)
		}
		changed = true
	}

	if !changed {
		return data
	}
	out, err := common.Marshal(chunk)
	if err != nil {
		return data
	}
	return string(out)
}

// ExtractThinkTagsFromTextResponse rewrites every choice of a non-streaming
// chat response in place and reports whether any message changed.
func ExtractThinkTagsFromTextResponse(resp *dto.OpenAITextResponse) bool {
	if resp == nil {
		return false
	}
	changed := false
	for i := range resp.Choices {
		if ExtractThinkTagsFromMessage(&resp.Choices[i].Message) {
			changed = true
		}
	}
	return changed
}

// ExtractThinkTagsFromMessage rewrites a non-streaming assistant message in
// place, moving think tags from content into reasoning_content. It reports
// whether the message changed. Structured (non-string) content is left alone:
// rewriting it would change the message's JSON shape rather than just its text.
func ExtractThinkTagsFromMessage(message *dto.Message) bool {
	if message == nil || !message.IsStringContent() {
		return false
	}
	content := message.StringContent()
	if !hasAnyTag(content, thinkOpenTags) {
		return false
	}
	visible, reasoning, found := SplitThinkTaggedContent(content)
	if !found {
		return false
	}
	message.SetStringContent(visible)
	if reasoning != "" {
		existing := message.GetReasoningContent()
		if !strings.Contains(existing, reasoning) {
			message.ReasoningContent = common.GetPointer[string](existing + reasoning)
		}
	}
	return true
}

// appendReasoningWithoutDuplication appends extracted tag reasoning to a
// delta's reasoning_content unless that reasoning is already present, which
// happens when an upstream reports the same reasoning in reasoning_content and
// also leaves the tags in content.
func appendReasoningWithoutDuplication(d *dto.ChatCompletionsStreamResponseChoiceDelta, reasoning string) {
	if d == nil || reasoning == "" {
		return
	}
	existing := d.GetReasoningContent()
	if strings.Contains(existing, reasoning) {
		return
	}
	d.SetReasoningContent(existing + reasoning)
}

// mayCarryThinkTag reports whether data could contain a think marker or a
// prefix of one. A marker always contains "<", which reaches us either
// literally (raw upstream JSON) or as the JSON escape for it (a frame this
// relay re-serialised through common.Marshal, which escapes "<").
func mayCarryThinkTag(data string) bool {
	return strings.Contains(data, "<") || strings.Contains(data, "\\u003c")
}

// hasAnyTag reports whether s contains any of the markers.
func hasAnyTag(s string, tags []string) bool {
	for _, tag := range tags {
		if strings.Contains(s, tag) {
			return true
		}
	}
	return false
}

// indexAnyTag returns the earliest match of any tag in s starting at offset.
func indexAnyTag(s string, offset int, tags []string) (index int, length int) {
	best := -1
	bestLen := 0
	for _, tag := range tags {
		rel := strings.Index(s[offset:], tag)
		if rel < 0 {
			continue
		}
		abs := offset + rel
		if best < 0 || abs < best || (abs == best && len(tag) > bestLen) {
			best = abs
			bestLen = len(tag)
		}
	}
	return best, bestLen
}

// indexAnyTagBytes is indexAnyTag over a byte buffer, so the streaming splitter
// can keep its pending buffer as bytes without an extra string copy per scan.
func indexAnyTagBytes(b []byte, offset int, tags []string) (index int, length int) {
	if offset > len(b) {
		return -1, 0
	}
	s := string(b[offset:])
	rel, length := indexAnyTag(s, 0, tags)
	if rel < 0 {
		return -1, 0
	}
	return offset + rel, length
}

// longestTagLen returns the length of the longest marker, the worst-case
// look-back window for a straddling match.
func longestTagLen(tags []string) int {
	longest := 0
	for _, tag := range tags {
		if len(tag) > longest {
			longest = len(tag)
		}
	}
	return longest
}

// closeSearchFloor is the earliest byte a close tag can start given that only
// the last (tagLen-1) bytes can straddle the next delta.
func closeSearchFloor(pendingLen int, tagLen int) int {
	floor := pendingLen - (tagLen - 1)
	if floor < 0 {
		return 0
	}
	return floor
}

// partialAnyTagSuffixLen returns the length of the longest suffix of b that is
// a proper prefix of any tag, i.e. how many trailing bytes must be withheld
// because the next delta could complete the tag.
func partialAnyTagSuffixLen(b []byte, tags []string) int {
	best := 0
	for _, tag := range tags {
		max := len(tag) - 1
		if len(b) < max {
			max = len(b)
		}
		for n := max; n > best; n-- {
			if string(b[len(b)-n:]) == tag[:n] {
				best = n
				break
			}
		}
	}
	return best
}
