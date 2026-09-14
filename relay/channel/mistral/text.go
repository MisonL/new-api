package mistral

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

var mistralToolCallIdRegexp = regexp.MustCompile("^[a-zA-Z0-9]{9}$")

func requestOpenAI2Mistral(request *dto.GeneralOpenAIRequest) (*dto.GeneralOpenAIRequest, error) {
	if request == nil {
		return nil, fmt.Errorf("request is nil")
	}
	messages := make([]dto.Message, 0, len(request.Messages))
	idMap := make(map[string]string)
	for messageIndex, message := range request.Messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		if role == "developer" {
			role = "system"
		}
		switch role {
		case "system", "user", "assistant", "tool":
			message.Role = role
		default:
			return nil, fmt.Errorf("messages[%d].role %q is not supported by Mistral Chat", messageIndex, message.Role)
		}
		if role == "tool" && strings.TrimSpace(message.ToolCallId) == "" {
			return nil, fmt.Errorf("messages[%d].tool_call_id is required for Mistral tool messages", messageIndex)
		}

		// 1. tool_calls.id
		toolCalls, err := message.ParseToolCallsStrict()
		if err != nil {
			return nil, fmt.Errorf("messages[%d]: %w", messageIndex, err)
		}
		if toolCalls != nil {
			for i := range toolCalls {
				if message.Role != "assistant" {
					return nil, fmt.Errorf("messages[%d].tool_calls are only supported on assistant messages", messageIndex)
				}
				if strings.TrimSpace(toolCalls[i].Type) != "" && toolCalls[i].Type != "function" {
					return nil, fmt.Errorf("messages[%d].tool_calls[%d].type %q is not supported by Mistral Chat", messageIndex, i, toolCalls[i].Type)
				}
				if strings.TrimSpace(toolCalls[i].Function.Name) == "" {
					return nil, fmt.Errorf("messages[%d].tool_calls[%d].function.name is required", messageIndex, i)
				}
				if strings.TrimSpace(toolCalls[i].ID) == "" {
					return nil, fmt.Errorf("messages[%d].tool_calls[%d].id is required", messageIndex, i)
				}
				if strings.TrimSpace(toolCalls[i].Function.Arguments) == "" {
					return nil, fmt.Errorf("messages[%d].tool_calls[%d].function.arguments is required", messageIndex, i)
				}
				var arguments map[string]any
				if err := common.Unmarshal([]byte(toolCalls[i].Function.Arguments), &arguments); err != nil || arguments == nil {
					if err == nil {
						err = fmt.Errorf("arguments must be a JSON object")
					}
					return nil, fmt.Errorf("messages[%d].tool_calls[%d].function.arguments is invalid: %w", messageIndex, i, err)
				}
				if !mistralToolCallIdRegexp.MatchString(toolCalls[i].ID) {
					if newId, ok := idMap[toolCalls[i].ID]; ok {
						toolCalls[i].ID = newId
					} else {
						newId, err := common.GenerateRandomCharsKey(9)
						if err != nil {
							return nil, fmt.Errorf("messages[%d].tool_calls[%d] id normalization failed: %w", messageIndex, i, err)
						}
						idMap[toolCalls[i].ID] = newId
						toolCalls[i].ID = newId
					}
				}
			}
			message.SetToolCalls(toolCalls)
		}

		// 2. tool_call_id
		if message.ToolCallId != "" {
			if newId, ok := idMap[message.ToolCallId]; ok {
				message.ToolCallId = newId
			} else {
				if !mistralToolCallIdRegexp.MatchString(message.ToolCallId) {
					newId, err := common.GenerateRandomCharsKey(9)
					if err != nil {
						return nil, fmt.Errorf("messages[%d].tool_call_id normalization failed: %w", messageIndex, err)
					}
					idMap[message.ToolCallId] = newId
					message.ToolCallId = newId
				}
			}
		}

		mediaMessages, err := message.ParseContentStrict()
		if err != nil {
			return nil, fmt.Errorf("messages[%d]: %w", messageIndex, err)
		}
		if message.Role == "assistant" && message.ToolCalls != nil && message.Content == "" {
			mediaMessages = []dto.MediaContent{}
		}
		for j, mediaMessage := range mediaMessages {
			switch mediaMessage.Type {
			case dto.ContentTypeText:
			case dto.ContentTypeImageURL:
				imageUrl := mediaMessage.GetImageMedia()
				if imageUrl == nil || strings.TrimSpace(imageUrl.Url) == "" {
					return nil, fmt.Errorf("messages[%d].content[%d].image_url.url is required", messageIndex, j)
				}
				mediaMessage.ImageUrl = imageUrl.Url
				mediaMessages[j] = mediaMessage
			default:
				return nil, fmt.Errorf("messages[%d].content[%d] type %q is not supported by Mistral Chat", messageIndex, j, mediaMessage.Type)
			}
		}
		message.SetMediaContent(mediaMessages)
		messages = append(messages, message)
	}
	out := *request
	out.Messages = messages
	if request.MaxTokens != nil || request.MaxCompletionTokens != nil {
		maxTokens := request.GetMaxTokens()
		out.MaxTokens = &maxTokens
		out.MaxCompletionTokens = nil
	}
	return &out, nil
}
