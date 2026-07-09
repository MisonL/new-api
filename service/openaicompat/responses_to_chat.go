package openaicompat

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/dto"
)

func ResponsesResponseToChatCompletionsResponse(resp *dto.OpenAIResponsesResponse, id string) (*dto.OpenAITextResponse, *dto.Usage, error) {
	if resp == nil {
		return nil, nil, errors.New("response is nil")
	}

	text := ExtractOutputTextFromResponses(resp)
	reasoningSummary := ExtractReasoningSummaryFromResponses(resp)

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
	if text == "" && len(resp.Output) > 0 {
		for _, out := range resp.Output {
			if out.Type != "function_call" {
				continue
			}
			name := strings.TrimSpace(out.Name)
			if name == "" {
				continue
			}
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
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

	msg := dto.Message{
		Role:    "assistant",
		Content: text,
	}
	if reasoningSummary != "" {
		msg.ReasoningContent = &reasoningSummary
	}
	if len(toolCalls) > 0 {
		msg.SetToolCalls(toolCalls)
		msg.Content = ""
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
			}
		}
	}
	return sb.String()
}
