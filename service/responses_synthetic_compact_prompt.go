package service

import (
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

const syntheticCompactSummaryPrompt = "You are performing a CONTEXT CHECKPOINT COMPACTION. Create a handoff summary for another LLM that will resume the task.\nInclude:\n- Current progress and key decisions made\n- Important context, constraints, or user preferences\n- What remains to be done (clear next steps)\n- Any critical data, examples, or references needed to continue\nBe concise, structured, and focused on helping the next LLM seamlessly continue the work.\nDo not invent facts. Return only the compact summary text."
const syntheticCompactPreviousResponsePrompt = "Use the existing previous_response_id context as the source of truth for the compaction. Create the handoff summary from the conversation available in that response chain. Return only the compact summary text."
const syntheticCompactResumeDirective = "Another language model produced the compact summary above. Use it to build on the work that has already been done and avoid duplicating work. If post-compact input is only repeated setup or repository instructions from the client, treat it as background and continue the latest pending task from the summary. If post-compact input contains a new explicit user request, answer that request using the summary as context."

func syntheticCompactRecoveredContextText(summary string) string {
	return syntheticCompactRecoveredSummaryText(summary) + "\n\n" + syntheticCompactResumeDirective
}

func syntheticCompactRecoveredSummaryText(summary string) string {
	return "Another language model started to solve this problem and produced a compact handoff summary. Use this to build on the work that has already been done and avoid duplicating work. Here is the summary produced by the other language model, use the information in this summary to assist with your own analysis:\n\n" + strings.TrimSpace(summary)
}

func responsesOutputText(outputs []dto.ResponsesOutput) string {
	texts := make([]string, 0, len(outputs))
	for _, output := range outputs {
		for _, content := range output.Content {
			if content.Type != "output_text" {
				continue
			}
			if text := strings.TrimSpace(content.Text); text != "" {
				texts = append(texts, text)
			}
		}
	}
	return strings.Join(texts, "\n")
}

func buildSyntheticCompactSummaryUserText(visibleParts []string, state *SyntheticCompactState) string {
	sections := make([]string, 0, 2)
	if state != nil && strings.TrimSpace(state.Summary) != "" {
		sections = append(sections, "Previous synthetic summary:\n"+strings.TrimSpace(state.Summary))
	}
	if len(visibleParts) > 0 {
		sections = append(sections, "Visible conversation to compact:\n"+limitSyntheticCompactVisibleParts(visibleParts, syntheticCompactVisibleTextMax))
	}
	return strings.Join(sections, "\n\n")
}

func limitSyntheticCompactVisibleParts(visibleParts []string, maxBytes int) string {
	visibleText := strings.TrimSpace(strings.Join(visibleParts, "\n"))
	if visibleText == "" {
		return ""
	}
	if maxBytes <= 0 || len(visibleText) <= maxBytes {
		return visibleText
	}
	tail := visibleText[len(visibleText)-maxBytes:]
	tail = trimToRuneBoundary(tail)
	return "[truncated earlier visible input]\n" + strings.TrimSpace(tail)
}

func limitSyntheticCompactPreviousVisibleParts(visibleParts []string) []string {
	visibleText := limitSyntheticCompactVisibleParts(visibleParts, syntheticCompactPreviousVisibleTextMax)
	if visibleText == "" {
		return nil
	}
	return []string{visibleText}
}

func syntheticCompactPromptInput(systemText string, userText string) (common.RawMessage, error) {
	systemItem, err := responseMessageInput("developer", systemText)
	if err != nil {
		return nil, err
	}
	userItem, err := responseMessageInput("user", userText)
	if err != nil {
		return nil, err
	}
	return common.Marshal([]common.RawMessage{systemItem, userItem})
}

func responseMessageInput(role string, text string) (common.RawMessage, error) {
	parts := splitSyntheticCompactTextParts(text)
	content := make([]map[string]string, 0, len(parts))
	for _, part := range parts {
		content = append(content, map[string]string{
			"type": "input_text",
			"text": part,
		})
	}
	item := map[string]any{
		"type":    "message",
		"role":    role,
		"content": content,
	}
	return common.Marshal(item)
}

func splitSyntheticCompactTextParts(text string) []string {
	if text == "" {
		return nil
	}
	if len(text) <= syntheticCompactTextPartMax {
		return []string{text}
	}
	parts := make([]string, 0, len(text)/syntheticCompactTextPartMax+1)
	for start := 0; start < len(text); {
		end := start + syntheticCompactTextPartMax
		if end >= len(text) {
			parts = append(parts, text[start:])
			break
		}
		originalEnd := end
		lowerBound := originalEnd - utf8.UTFMax
		if lowerBound < start {
			lowerBound = start
		}
		for end > lowerBound && !utf8.RuneStart(text[end]) {
			end--
		}
		if !utf8.RuneStart(text[end]) {
			// Invalid continuation-byte runs have no nearby rune boundary; hard split to keep runtime linear.
			end = originalEnd
		}
		parts = append(parts, text[start:end])
		start = end
	}
	return parts
}

func trimToRuneBoundary(text string) string {
	for len(text) > 0 && !utf8.RuneStart(text[0]) {
		text = text[1:]
	}
	return text
}
