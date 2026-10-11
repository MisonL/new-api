package service

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// Gemini conversion must emit at most one thought part per candidate and must
// not emit an empty thought part, and non-stream conversion must move think
// tags out of the visible text. Regression for the duplicate-injection defect
// where two independent branches each appended GetReasoningContent().
func TestGeminiConversionEmitsSingleThoughtPart(t *testing.T) {
	splitter := &relaycommon.ThinkTagStreamSplitter{}
	splitter.Feed("<think>seed</think>") // SawTag() becomes true for the request
	info := &relaycommon.RelayInfo{ThinkTagSplitter: splitter}
	if !relaycommon.ThinkTagsExtracted(info) {
		t.Fatal("precondition: splitter should report a seen tag")
	}

	reasoning := "R2"
	content := "more text"
	stream := &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index: 0,
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				ReasoningContent: &reasoning,
				Content:          &content,
			},
		}},
	}
	got, err := StreamResponseOpenAI2GeminiWithError(stream, info)
	if err != nil {
		t.Fatalf("stream convert: %v", err)
	}
	thoughts := 0
	for _, p := range got.Candidates[0].Content.Parts {
		if p.Thought {
			thoughts++
			if p.Text == "" {
				t.Errorf("empty thought part emitted")
			}
		}
	}
	if thoughts != 1 {
		t.Fatalf("stream: got %d thought parts, want 1", thoughts)
	}

	// A later content-only frame must not inject an empty thought part just
	// because a tag was seen earlier in the request.
	plain := "plain"
	late := &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index: 0,
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: &plain},
		}},
	}
	gotLate, err := StreamResponseOpenAI2GeminiWithError(late, info)
	if err != nil {
		t.Fatalf("stream convert 2: %v", err)
	}
	for _, p := range gotLate.Candidates[0].Content.Parts {
		if p.Thought {
			t.Fatalf("content-only frame emitted a thought part with text %q", p.Text)
		}
	}

	// Non-stream: the tag-wrapped reasoning must reach the client as a thought
	// part and must be removed from the visible text.
	nonStream := &dto.OpenAITextResponse{
		Choices: []dto.OpenAITextResponseChoice{{
			Index:   0,
			Message: dto.Message{Content: "hello <think>R1</think> world"},
		}},
	}
	gotNon, err := ResponseOpenAI2GeminiWithError(nonStream, &relaycommon.RelayInfo{})
	if err != nil {
		t.Fatalf("non-stream convert: %v", err)
	}
	parts := gotNon.Candidates[0].Content.Parts
	nonThoughts := 0
	var visible string
	for _, p := range parts {
		if p.Thought {
			nonThoughts++
			if p.Text == "" {
				t.Errorf("non-stream: empty thought part emitted")
			}
		} else {
			visible += p.Text
		}
	}
	if nonThoughts != 1 {
		t.Fatalf("non-stream: got %d thought parts, want 1", nonThoughts)
	}
	if want := "hello  world"; visible != want {
		t.Errorf("non-stream visible text = %q, want %q", visible, want)
	}
}
