package openaicompat

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

func ResponsesResponseToChatCompletionsResponse(resp *dto.OpenAIResponsesResponse, id string) (*dto.OpenAITextResponse, *dto.Usage, error) {
	if resp == nil {
		return nil, nil, errors.New("response is nil")
	}
	if err := validateResponsesResponseForChat(resp); err != nil {
		return nil, nil, err
	}

	text := ExtractOutputTextFromResponses(resp)
	hasRefusal := responsesHasRefusal(resp)
	reasoningContent, hasReasoningContent := extractReasoningContentFromResponses(resp)
	reasoningSummary := ExtractReasoningSummaryFromResponses(resp)
	if !hasReasoningContent {
		reasoningContent = reasoningSummary
	}

	usage := &dto.Usage{}
	if resp.Usage != nil {
		if resp.Usage.InputTokens != 0 {
			usage.PromptTokens = resp.Usage.InputTokens
			usage.InputTokens = resp.Usage.InputTokens
		}
		if resp.Usage.OutputTokens != 0 {
			usage.CompletionTokens = resp.Usage.OutputTokens
			usage.OutputTokens = resp.Usage.OutputTokens
		}
		if resp.Usage.TotalTokens != 0 {
			usage.TotalTokens = resp.Usage.TotalTokens
		} else {
			usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
		}
		if resp.Usage.InputTokensDetails != nil {
			usage.PromptTokensDetails.CachedTokens = resp.Usage.InputTokensDetails.CachedTokens
			usage.PromptTokensDetails.ImageTokens = resp.Usage.InputTokensDetails.ImageTokens
			usage.PromptTokensDetails.AudioTokens = resp.Usage.InputTokensDetails.AudioTokens
		}
		outputDetails := resp.Usage.GetOutputTokenDetails()
		if outputDetails.ReasoningTokens != 0 {
			usage.CompletionTokenDetails.ReasoningTokens = outputDetails.ReasoningTokens
		}
		if outputDetails.ImageTokens != 0 {
			usage.CompletionTokenDetails.ImageTokens = outputDetails.ImageTokens
		}
		if outputDetails.AudioTokens != 0 {
			usage.CompletionTokenDetails.AudioTokens = outputDetails.AudioTokens
		}
	}

	created := resp.CreatedAt

	var toolCalls []dto.ToolCallResponse
	if len(resp.Output) > 0 {
		for _, out := range resp.Output {
			if out.Type != "function_call" {
				continue
			}
			name := flattenResponsesOutputToolName(out.Namespace, out.Name)
			callId := strings.TrimSpace(out.CallId)
			if callId == "" {
				callId = strings.TrimSpace(out.ID)
			}
			toolCalls = append(toolCalls, dto.ToolCallResponse{
				ID:   callId,
				Type: "function",
				Function: dto.FunctionResponse{
					Name:      name,
					Arguments: out.ArgumentsString(),
				},
			})
		}
	}

	finishReason := "stop"
	if hasRefusal {
		// Chat Completions does not define a refusal finish reason. The refusal
		// text remains in message content, so expose the standard terminal reason.
		finishReason = "stop"
	} else if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	} else if incompleteFinishReason, ok := responsesIncompleteFinishReason(resp); ok {
		finishReason = incompleteFinishReason
	}

	msg := dto.Message{
		Role:    "assistant",
		Content: text,
	}
	if hasReasoningContent || reasoningContent != "" {
		msg.ReasoningContent = &reasoningContent
	}
	if len(toolCalls) > 0 {
		msg.SetToolCalls(toolCalls)
	}

	out := &dto.OpenAITextResponse{
		Id:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   resp.Model,
		Choices: []dto.OpenAITextResponseChoice{
			{
				Index:        0,
				Message:      msg,
				FinishReason: finishReason,
			},
		},
		Usage: *usage,
	}

	return out, usage, nil
}

// validateResponsesResponseForChat rejects response items that have no
// faithful Chat Completions representation. A native Responses response can
// contain built-in tools, encrypted reasoning state, or other output item
// types; silently skipping those items would make the client observe a false
// successful answer.
func validateResponsesResponseForChat(resp *dto.OpenAIResponsesResponse) error {
	if resp == nil {
		return errors.New("response is nil")
	}

	status := strings.ToLower(strings.TrimSpace(common.JsonRawMessageToString(resp.Status)))
	switch status {
	case "", "completed", "incomplete":
	case "failed":
		return errors.New("Responses response status \"failed\" cannot be represented as a Chat Completions response")
	default:
		return fmt.Errorf("Responses response status %q cannot be represented as a Chat Completions response", status)
	}

	for index, out := range resp.Output {
		if err := ValidateResponsesOutputItemForChat(&out, index); err != nil {
			return err
		}
	}
	return nil
}

// ValidateResponsesOutputItemForChat validates one complete Responses output
// item before it is projected into a Chat Completions response. Stream
// `output_item.added` events may be partial and should be checked only after
// their corresponding `output_item.done` event or terminal response snapshot.
func ValidateResponsesOutputItemForChat(out *dto.ResponsesOutput, index int) error {
	if out == nil {
		return errors.New("Responses output item is nil")
	}
	label := "Responses output item"
	if index >= 0 {
		label = fmt.Sprintf("Responses output[%d]", index)
	}

	switch out.Type {
	case "message":
		if role := strings.TrimSpace(out.Role); role != "" && role != "assistant" {
			return fmt.Errorf("%s message role %q is not supported in chat compatibility mode", label, role)
		}
		for contentIndex, part := range out.Content {
			partType := strings.TrimSpace(part.Type)
			if partType != "" && partType != "output_text" && partType != "refusal" {
				return fmt.Errorf("%s.content[%d] type %q is not supported in chat compatibility mode", label, contentIndex, partType)
			}
		}
	case "reasoning":
		for contentIndex, part := range out.Content {
			partType := strings.TrimSpace(part.Type)
			if partType != "" && partType != "reasoning_text" && partType != "summary_text" {
				return fmt.Errorf("%s.content[%d] reasoning type %q is not supported in chat compatibility mode", label, contentIndex, partType)
			}
		}
		for summaryIndex, part := range out.Summary {
			partType := strings.TrimSpace(part.Type)
			if partType != "" && partType != "summary_text" {
				return fmt.Errorf("%s.summary[%d] type %q is not supported in chat compatibility mode", label, summaryIndex, partType)
			}
		}
	case "function_call":
		if strings.TrimSpace(out.Name) == "" {
			return fmt.Errorf("%s function_call name is required", label)
		}
		if strings.TrimSpace(out.CallId) == "" && strings.TrimSpace(out.ID) == "" {
			return fmt.Errorf("%s function_call requires call_id or id", label)
		}
		if len(out.Arguments) > 0 {
			argumentsType := common.GetJsonType(out.Arguments)
			if argumentsType != "string" && argumentsType != "object" && argumentsType != "array" {
				return fmt.Errorf("%s function_call arguments must be a JSON string or JSON object", label)
			}
		}
	case dto.BuildInCallWebSearchCall, dto.BuildInCallFileSearchCall, dto.ResponsesOutputTypeImageGenerationCall:
		// Built-in tool calls are accounted for separately by the relay layer.
		// They have no faithful Chat Completions message representation, so
		// they are intentionally omitted while the surrounding text is kept.
	case dto.ResponsesOutputTypeCompaction, dto.ResponsesOutputTypeCompactionSummary, dto.ResponsesOutputTypeContextCompaction:
		// Compaction output is state for the next Responses turn, not a Chat
		// message. The relay records its presence and omits it from the Chat
		// payload, matching the streaming compatibility path.
	case "":
		return fmt.Errorf("%s type is required", label)
	default:
		return fmt.Errorf("%s type %q is not supported in chat compatibility mode", label, out.Type)
	}
	return nil
}

// ExtractReasoningContentFromResponses returns DeepSeek's plain-text
// reasoning_text parts exactly as received. Unlike a reasoning summary, this
// value is replay state and must not be trimmed or joined with separators.
func ExtractReasoningContentFromResponses(resp *dto.OpenAIResponsesResponse) string {
	text, _ := extractReasoningContentFromResponses(resp)
	return text
}

func extractReasoningContentFromResponses(resp *dto.OpenAIResponsesResponse) (string, bool) {
	if resp == nil {
		return "", false
	}

	var builder strings.Builder
	var hasReasoningText bool
	for _, out := range resp.Output {
		if out.Type != "reasoning" {
			continue
		}
		for _, part := range out.Content {
			if part.Type == "reasoning_text" {
				hasReasoningText = true
				builder.WriteString(part.Text)
			}
		}
	}
	return builder.String(), hasReasoningText
}

func responsesIncompleteFinishReason(resp *dto.OpenAIResponsesResponse) (string, bool) {
	if resp == nil || !strings.EqualFold(strings.TrimSpace(common.JsonRawMessageToString(resp.Status)), "incomplete") {
		return "", false
	}
	if resp.IncompleteDetails != nil && strings.EqualFold(resp.IncompleteDetails.EffectiveReason(), "content_filter") {
		return "content_filter", true
	}
	return "length", true
}

func ExtractReasoningSummaryFromResponses(resp *dto.OpenAIResponsesResponse) string {
	if resp == nil {
		return ""
	}

	var sb strings.Builder
	for _, out := range resp.Output {
		if out.Type != "reasoning" {
			continue
		}
		for _, part := range out.Summary {
			if part.Type != "" && part.Type != "summary_text" {
				continue
			}
			appendReasoningSummaryText(&sb, part.Text)
		}
		for _, c := range out.Content {
			if c.Type != "" && c.Type != "summary_text" {
				continue
			}
			appendReasoningSummaryText(&sb, c.Text)
		}
	}
	if sb.Len() > 0 {
		return sb.String()
	}

	if resp.Reasoning == nil || isReasoningSummaryConfigValue(resp.Reasoning.Summary) {
		return ""
	}
	return strings.TrimSpace(resp.Reasoning.Summary)
}

func appendReasoningSummaryText(sb *strings.Builder, text string) {
	if sb == nil || strings.TrimSpace(text) == "" {
		return
	}
	if sb.Len() > 0 {
		sb.WriteString("\n\n")
	}
	sb.WriteString(text)
}

func isReasoningSummaryConfigValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto", "concise", "detailed", "none":
		return true
	default:
		return false
	}
}

func ExtractOutputTextFromResponses(resp *dto.OpenAIResponsesResponse) string {
	if resp == nil || len(resp.Output) == 0 {
		return ""
	}

	var sb strings.Builder

	// Prefer assistant message outputs.
	for _, out := range resp.Output {
		if out.Type != "message" {
			continue
		}
		if out.Role != "" && out.Role != "assistant" {
			continue
		}
		for _, c := range out.Content {
			if c.Type == "output_text" && c.Text != "" {
				sb.WriteString(c.Text)
			} else if c.Type == "refusal" && c.Refusal != "" {
				sb.WriteString(c.Refusal)
			}
		}
	}
	if sb.Len() > 0 {
		return sb.String()
	}
	for _, out := range resp.Output {
		if out.Type == "reasoning" {
			continue
		}
		for _, c := range out.Content {
			if c.Text != "" {
				sb.WriteString(c.Text)
			} else if c.Refusal != "" {
				sb.WriteString(c.Refusal)
			}
		}
	}
	return sb.String()
}

func responsesHasRefusal(resp *dto.OpenAIResponsesResponse) bool {
	if resp == nil {
		return false
	}
	for _, out := range resp.Output {
		if out.Type != "message" {
			continue
		}
		for _, content := range out.Content {
			if content.Type == "refusal" && strings.TrimSpace(content.Refusal) != "" {
				return true
			}
		}
	}
	return false
}
