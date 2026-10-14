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
	if usage.OutputTokensDetails == nil {
		usage.OutputTokensDetails = &dto.OutputTokenDetails{
			TextTokens:      usage.CompletionTokenDetails.TextTokens,
			AudioTokens:     usage.CompletionTokenDetails.AudioTokens,
			ImageTokens:     usage.CompletionTokenDetails.ImageTokens,
			ReasoningTokens: usage.CompletionTokenDetails.ReasoningTokens,
		}
	}
	return &usage
}

func responsesStatusFromChatFinishReason(finishReason string) (common.RawMessage, *dto.IncompleteDetails) {
	switch strings.ToLower(strings.TrimSpace(finishReason)) {
	case "length":
		return common.RawMessage(`"incomplete"`), &dto.IncompleteDetails{Reason: "max_output_tokens"}
	case "content_filter":
		return common.RawMessage(`"incomplete"`), &dto.IncompleteDetails{Reason: "content_filter"}
	default:
		return common.RawMessage(`"completed"`), nil
	}
}

func parseChatCustomToolPayload(raw []byte) (string, string, error) {
	if len(raw) == 0 {
		return "", "", fmt.Errorf("custom tool payload is required")
	}
	var custom map[string]any
	if err := common.Unmarshal(raw, &custom); err != nil {
		return "", "", fmt.Errorf("failed to parse custom tool payload: %w", err)
	}
	name := strings.TrimSpace(common.Interface2String(custom["name"]))
	if name == "" {
		return "", "", fmt.Errorf("custom tool name is required")
	}
	rawInput, exists := custom["input"]
	if !exists {
		return "", "", fmt.Errorf("custom tool input is required")
	}
	input, ok := rawInput.(string)
	if !ok {
		return "", "", fmt.Errorf("custom tool input must be a string")
	}
	return name, input, nil
}

func ParseChatCustomToolFunctionArguments(arguments string) (string, error) {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" {
		return "", fmt.Errorf("custom tool function arguments are required")
	}
	var payload map[string]any
	if err := common.Unmarshal([]byte(trimmed), &payload); err != nil {
		return "", fmt.Errorf("failed to parse custom tool function arguments: %w", err)
	}
	input, ok := payload["input"]
	if !ok {
		return "", fmt.Errorf("custom tool function arguments require input")
	}
	result, ok := input.(string)
	if !ok {
		return "", fmt.Errorf("custom tool function input must be a string")
	}
	return result, nil
}

func ChatCompletionsResponseToResponsesResponse(resp *dto.OpenAITextResponse, id string) (*dto.OpenAIResponsesResponse, *dto.Usage, error) {
	return ChatCompletionsResponseToResponsesResponseWithOptions(resp, id, ResponsesChatCompatibilityOptions{})
}

func ChatCompletionsResponseToResponsesResponseWithOptions(resp *dto.OpenAITextResponse, id string, options ResponsesChatCompatibilityOptions) (*dto.OpenAIResponsesResponse, *dto.Usage, error) {
	return ChatCompletionsResponseToResponsesResponseWithOptionsAndState(resp, id, options, nil)
}

func ChatCompletionsResponseToResponsesResponseWithOptionsAndState(resp *dto.OpenAITextResponse, id string, options ResponsesChatCompatibilityOptions, toolState *ResponsesChatCompatibilityToolState) (*dto.OpenAIResponsesResponse, *dto.Usage, error) {
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
	output := make([]dto.ResponsesOutput, 0, 3)

	if message.ReasoningContent != nil || message.ThinkingContent != nil || message.Reasoning != nil {
		reasoning := message.GetReasoningContent()
		output = append(output, dto.ResponsesOutput{
			Type:   "reasoning",
			ID:     "rs_0",
			Status: "completed",
			Content: []dto.ResponsesOutputContent{{
				Type: "reasoning_text",
				Text: reasoning,
			}},
		})
	}

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
			parts, parseErr := message.ParseContentStrict()
			if parseErr != nil {
				return nil, nil, fmt.Errorf("assistant content: %w", parseErr)
			}
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

	toolCalls, parseErr := message.ParseToolCallsStrict()
	if parseErr != nil {
		return nil, nil, fmt.Errorf("assistant tool_calls: %w", parseErr)
	}
	for index, toolCall := range toolCalls {
		toolType := strings.TrimSpace(toolCall.Type)
		name := strings.TrimSpace(toolCall.Function.Name)
		callID := strings.TrimSpace(toolCall.ID)
		if callID == "" {
			callID = fmt.Sprintf("call_%d", index)
		}
		identity, hasIdentity := toolState.Resolve(name)
		if hasIdentity && identity.Type == dto.CustomType {
			if toolType != "" && toolType != "function" {
				return nil, nil, fmt.Errorf("custom tool bridge received unsupported chat tool type %q", toolType)
			}
			input, err := ParseChatCustomToolFunctionArguments(toolCall.Function.Arguments)
			if err != nil {
				return nil, nil, err
			}
			output = append(output, dto.ResponsesOutput{
				Type:      "custom_tool_call",
				ID:        fmt.Sprintf("ctc_%d", index),
				Status:    "completed",
				CallId:    callID,
				Name:      identity.Name,
				Namespace: identity.Namespace,
				Input:     input,
			})
			continue
		}
		if toolType == dto.CustomType {
			if !options.EnableCustomToolBridge {
				return nil, nil, fmt.Errorf("custom tool bridge is not enabled in responses compatibility mode")
			}
			name, input, err := parseChatCustomToolPayload(toolCall.Custom)
			if err != nil {
				return nil, nil, err
			}
			namespace := ""
			if identity, ok := toolState.Resolve(name); ok {
				name = identity.Name
				namespace = identity.Namespace
			}
			output = append(output, dto.ResponsesOutput{
				Type:      "custom_tool_call",
				ID:        fmt.Sprintf("ctc_%d", index),
				Status:    "completed",
				CallId:    callID,
				Name:      name,
				Namespace: namespace,
				Input:     input,
			})
			continue
		}
		if toolType != "" && toolType != "function" {
			return nil, nil, fmt.Errorf("tool call type %q is not supported in responses compatibility mode", toolType)
		}
		if name == "" {
			return nil, nil, fmt.Errorf("tool call name is required")
		}
		if identity, ok := toolState.Resolve(name); ok && identity.Type == responsesToolTypeToolSearch {
			argumentsRaw, err := responsesToolSearchArguments(toolCall.Function.Arguments)
			if err != nil {
				return nil, nil, err
			}
			output = append(output, dto.ResponsesOutput{
				Type:      "tool_search_call",
				ID:        fmt.Sprintf("tsc_%d", index),
				Status:    "completed",
				CallId:    callID,
				Execution: "client",
				Arguments: argumentsRaw,
			})
			continue
		}
		namespace := ""
		if identity, ok := toolState.Resolve(name); ok {
			name = identity.Name
			namespace = identity.Namespace
		}
		argumentsRaw, err := common.Marshal(toolCall.Function.Arguments)
		if err != nil {
			return nil, nil, fmt.Errorf("tool call arguments marshal failed: %w", err)
		}
		output = append(output, dto.ResponsesOutput{
			Type:      "function_call",
			ID:        fmt.Sprintf("fc_%d", index),
			Status:    "completed",
			CallId:    callID,
			Name:      name,
			Namespace: namespace,
			Arguments: argumentsRaw,
		})
	}

	responseID := strings.TrimSpace(id)
	if responseID == "" {
		responseID = strings.TrimSpace(resp.Id)
	}
	if responseID == "" {
		responseID = "resp_" + common.GetUUID()
	}

	statusRaw, incompleteDetails := responsesStatusFromChatFinishReason(choice.FinishReason)
	usage := buildResponsesUsageFromChat(resp)

	out := &dto.OpenAIResponsesResponse{
		ID:                responseID,
		Object:            "response",
		CreatedAt:         chatResponseCreatedAt(resp.Created),
		Status:            statusRaw,
		IncompleteDetails: incompleteDetails,
		Model:             resp.Model,
		Output:            output,
		Usage:             usage,
	}

	return out, usage, nil
}

func responsesToolSearchArguments(arguments string) (common.RawMessage, error) {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" {
		return nil, fmt.Errorf("tool_search arguments are required")
	}
	raw := common.RawMessage(trimmed)
	if !common.ValidJson(raw) || common.GetJsonType(raw) != "object" {
		return nil, fmt.Errorf("tool_search arguments must be a JSON object")
	}
	var payload map[string]any
	if err := common.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse tool_search arguments: %w", err)
	}
	if strings.TrimSpace(common.Interface2String(payload["query"])) == "" {
		return nil, fmt.Errorf("tool_search arguments require query")
	}
	return raw, nil
}
