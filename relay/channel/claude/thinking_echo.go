package claude

import (
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
)

// ensureDeepSeekThinkingEcho restores the thinking block that DeepSeek's
// Anthropic-compatible endpoint requires on the last assistant turn of a tool
// loop. A turn served by another protocol reaches the client without one, and
// the upstream rejects the follow-up request with "The `content[].thinking` in
// the thinking mode must be passed back to the API". An empty block satisfies
// the check (the upstream accepts it with or without a signature) and matches
// what the upstream itself returns when the thinking display is omitted, so no
// reasoning content is fabricated. Requests that already carry a thinking block
// are left untouched.
//
// The gate uses the upstream model name only: the rule is enforced by the
// upstream that receives the request, and injecting an unsigned block into a
// real Anthropic model (where signatures are mandatory) would be rejected.
func ensureDeepSeekThinkingEcho(request *dto.ClaudeRequest) {
	if request == nil || len(request.Messages) == 0 {
		return
	}
	if !service.IsDeepSeekModelName(request.Model) {
		return
	}
	for i := len(request.Messages) - 1; i >= 0; i-- {
		message := &request.Messages[i]
		if message.Role != "assistant" {
			continue
		}
		message.Content = prependEmptyThinkingBeforeToolUse(message.Content)
		return
	}
}

func prependEmptyThinkingBeforeToolUse(content any) any {
	switch blocks := content.(type) {
	case []any:
		hasToolUse := false
		for _, raw := range blocks {
			block, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			switch block["type"] {
			case "thinking", "redacted_thinking":
				return content
			case "tool_use":
				hasToolUse = true
			}
		}
		if !hasToolUse {
			return content
		}
		return append([]any{map[string]any{"type": "thinking", "thinking": ""}}, blocks...)
	case []dto.ClaudeMediaMessage:
		hasToolUse := false
		for _, block := range blocks {
			switch block.Type {
			case "thinking", "redacted_thinking":
				return content
			case "tool_use":
				hasToolUse = true
			}
		}
		if !hasToolUse {
			return content
		}
		empty := ""
		return append([]dto.ClaudeMediaMessage{{Type: "thinking", Thinking: &empty}}, blocks...)
	}
	return content
}
