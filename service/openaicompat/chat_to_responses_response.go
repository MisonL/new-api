package openaicompat

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

func chatResponseCreatedAt(created any) int {
	switch v := created.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func buildResponsesUsageFromChat(resp *dto.OpenAITextResponse) *dto.Usage {
	if resp == nil {
		return &dto.Usage{}
	}
	usage := resp.Usage
	if usage.InputTokens == 0 {
		usage.InputTokens = usage.PromptTokens
	}
	if usage.OutputTokens == 0 {
		usage.OutputTokens = usage.CompletionTokens
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	if usage.InputTokensDetails == nil {
		usage.InputTokensDetails = &dto.InputTokenDetails{
			CachedTokens: usage.PromptTokensDetails.CachedTokens,
			ImageTokens:  usage.PromptTokensDetails.ImageTokens,
			AudioTokens:  usage.PromptTokensDetails.AudioTokens,
		}
	}
	return &usage
}

func ChatCompletionsResponseToResponsesResponse(resp *dto.OpenAITextResponse, id string) (*dto.OpenAIResponsesResponse, *dto.Usage, error) {
	if resp == nil {
		return nil, nil, fmt.Errorf("response is nil")
	}
	if len(resp.Choices) == 0 {
		return nil, nil, fmt.Errorf("response choices are empty")
	}
	if len(resp.Choices) > 1 {
		return nil, nil, fmt.Errorf("multiple choices are not supported in responses compatibility mode")
	}

	choice := resp.Choices[0]
	message := choice.Message
	output := make([]dto.ResponsesOutput, 0, 2)

	if message.Content != nil {
		if message.IsStringContent() {
			text := message.StringContent()
			if text != "" {
				output = append(output, dto.ResponsesOutput{
					Type:   "message",
					ID:     "msg_0",
					Status: "completed",
					Role:   "assistant",
					Content: []dto.ResponsesOutputContent{
						{
							Type: "output_text",
							Text: text,
						},
					},
				})
			}
		} else {
			parts := message.ParseContent()
			textParts := make([]dto.ResponsesOutputContent, 0, len(parts))
			for _, part := range parts {
				if part.Type != dto.ContentTypeText {
					return nil, nil, fmt.Errorf("assistant content type %q is not supported in responses compatibility mode", part.Type)
				}
				textParts = append(textParts, dto.ResponsesOutputContent{
					Type: "output_text",
					Text: part.Text,
				})
			}
			if len(textParts) > 0 {
				output = append(output, dto.ResponsesOutput{
					Type:    "message",
					ID:      "msg_0",
					Status:  "completed",
					Role:    "assistant",
					Content: textParts,
				})
			}
		}
	}

	for index, toolCall := range message.ParseToolCalls() {
		toolType := strings.TrimSpace(toolCall.Type)
		if toolType != "" && toolType != "function" {
			return nil, nil, fmt.Errorf("tool call type %q is not supported in responses compatibility mode", toolType)
		}
		name := strings.TrimSpace(toolCall.Function.Name)
		if name == "" {
			return nil, nil, fmt.Errorf("tool call name is required")
		}
		callID := strings.TrimSpace(toolCall.ID)
		if callID == "" {
			callID = fmt.Sprintf("call_%d", index)
		}
		output = append(output, dto.ResponsesOutput{
			Type:      "function_call",
			ID:        fmt.Sprintf("fc_%d", index),
			Status:    "completed",
			CallId:    callID,
			Name:      name,
			Arguments: toolCall.Function.Arguments,
		})
	}

	responseID := strings.TrimSpace(id)
	if responseID == "" {
		responseID = strings.TrimSpace(resp.Id)
	}
	if responseID == "" {
		responseID = "resp_" + common.GetUUID()
	}

	statusRaw, _ := common.Marshal("completed")
	usage := buildResponsesUsageFromChat(resp)

	out := &dto.OpenAIResponsesResponse{
		ID:        responseID,
		Object:    "response",
		CreatedAt: chatResponseCreatedAt(resp.Created),
		Status:    statusRaw,
		Model:     resp.Model,
		Output:    output,
		Usage:     usage,
	}

	return out, usage, nil
}
