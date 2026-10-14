package openaicompat

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/samber/lo"
)

func normalizeChatImageURLToString(v any) any {
	switch vv := v.(type) {
	case string:
		return vv
	case map[string]any:
		if url := common.Interface2String(vv["url"]); url != "" {
			return url
		}
		return v
	case dto.MessageImageUrl:
		if vv.Url != "" {
			return vv.Url
		}
		return v
	case *dto.MessageImageUrl:
		if vv != nil && vv.Url != "" {
			return vv.Url
		}
		return v
	default:
		return v
	}
}

func convertChatResponseFormatToResponsesText(reqFormat *dto.ResponseFormat) (json.RawMessage, error) {
	if reqFormat == nil || strings.TrimSpace(reqFormat.Type) == "" {
		return nil, nil
	}

	format := map[string]any{
		"type": reqFormat.Type,
	}

	if reqFormat.Type == "json_schema" && len(reqFormat.JsonSchema) > 0 {
		var chatSchema map[string]any
		if err := common.Unmarshal(reqFormat.JsonSchema, &chatSchema); err == nil {
			for key, value := range chatSchema {
				if key == "type" {
					continue
				}
				format[key] = value
			}

			if nested, ok := format["json_schema"].(map[string]any); ok {
				for key, value := range nested {
					if _, exists := format[key]; !exists {
						format[key] = value
					}
				}
				delete(format, "json_schema")
			}
		} else {
			return nil, fmt.Errorf("response_format json_schema must be a JSON object: %w", err)
		}
	}

	textRaw, err := common.Marshal(map[string]any{
		"format": format,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to encode response_format: %w", err)
	}
	return textRaw, nil
}

// ChatCompletionsToResponsesOptions controls fields that are only valid for
// specific Responses-compatible upstreams. In particular, DeepSeek accepts a
// reasoning input item containing reasoning_text parts, while native OpenAI
// Responses requires its own opaque reasoning context format.
type ChatCompletionsToResponsesOptions struct {
	PreserveReasoningContent bool
	EnableCustomToolBridge   bool
	// ToolNameRestorerOut, when non-nil, receives the alias registry built
	// during conversion. The caller attaches it to the request-scoped relay
	// info so the chat response path (OaiResponsesToChat*) can map an alias
	// the upstream echoes back to the client's original tool name.
	ToolNameRestorerOut **relaycommon.ResponsesToolNameRestorer
}

func parseChatToolCallsStrict(msg dto.Message, messageIndex int) ([]dto.ToolCallRequest, error) {
	if len(msg.ToolCalls) == 0 || string(msg.ToolCalls) == "null" {
		return nil, nil
	}
	var toolCalls []dto.ToolCallRequest
	if err := common.Unmarshal(msg.ToolCalls, &toolCalls); err != nil {
		return nil, fmt.Errorf("messages[%d].tool_calls must be an array: %w", messageIndex, err)
	}
	return toolCalls, nil
}

func convertChatToolCallsToResponses(msg dto.Message, messageIndex int, options ChatCompletionsToResponsesOptions, aliases *toolNameAliasRegistry) ([]map[string]any, error) {
	toolCalls, err := parseChatToolCallsStrict(msg, messageIndex)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(toolCalls))
	for callIndex, toolCall := range toolCalls {
		callID := strings.TrimSpace(toolCall.ID)
		if callID == "" {
			return nil, fmt.Errorf("messages[%d].tool_calls[%d].id is required", messageIndex, callIndex)
		}
		toolType := strings.TrimSpace(toolCall.Type)
		if toolType == "" {
			toolType = "function"
		}
		switch toolType {
		case "function":
			name, err := aliases.normalizeReference(toolCall.Function.Name)
			if err != nil {
				return nil, fmt.Errorf("messages[%d].tool_calls[%d].function.name: %w", messageIndex, callIndex, err)
			}
			items = append(items, map[string]any{
				"type":      "function_call",
				"call_id":   callID,
				"name":      name,
				"arguments": toolCall.Function.Arguments,
			})
		case dto.CustomType:
			if !options.EnableCustomToolBridge {
				return nil, fmt.Errorf("custom tool bridge is not enabled for messages[%d].tool_calls[%d]", messageIndex, callIndex)
			}
			name, input, err := parseChatCustomToolPayload(toolCall.Custom)
			if err != nil {
				return nil, fmt.Errorf("messages[%d].tool_calls[%d]: %w", messageIndex, callIndex, err)
			}
			name, err = aliases.normalizeReference(name)
			if err != nil {
				return nil, fmt.Errorf("messages[%d].tool_calls[%d]: %w", messageIndex, callIndex, err)
			}
			items = append(items, map[string]any{
				"type":    "custom_tool_call",
				"call_id": callID,
				"name":    name,
				"input":   input,
			})
		default:
			return nil, fmt.Errorf("messages[%d].tool_calls[%d].type %q is not supported in responses compatibility mode", messageIndex, callIndex, toolType)
		}
	}
	return items, nil
}

func convertChatMediaPartToResponses(part dto.MediaContent, messageIndex int, contentIndex int) (map[string]any, error) {
	partType := strings.TrimSpace(part.Type)
	switch partType {
	case dto.ContentTypeText:
		return map[string]any{"type": "input_text", "text": part.Text}, nil
	case dto.ContentTypeImageURL:
		image := part.GetImageMedia()
		if image == nil || strings.TrimSpace(image.Url) == "" {
			return nil, fmt.Errorf("messages[%d].content[%d].image_url.url is required", messageIndex, contentIndex)
		}
		return map[string]any{"type": "input_image", "image_url": image.Url}, nil
	case dto.ContentTypeInputAudio:
		audio := part.GetInputAudio()
		if audio == nil || strings.TrimSpace(audio.Data) == "" || strings.TrimSpace(audio.Format) == "" {
			return nil, fmt.Errorf("messages[%d].content[%d].input_audio.data and format are required", messageIndex, contentIndex)
		}
		return map[string]any{
			"type": "input_audio",
			"input_audio": map[string]any{
				"data":   audio.Data,
				"format": audio.Format,
			},
		}, nil
	case dto.ContentTypeFile:
		file := part.GetFile()
		if file == nil {
			return nil, fmt.Errorf("messages[%d].content[%d].file is required", messageIndex, contentIndex)
		}
		fileObject := make(map[string]any)
		if file.FileId != "" {
			fileObject["file_id"] = file.FileId
		}
		if file.FileData != "" {
			fileObject["file_data"] = file.FileData
		}
		if file.FileUrl != "" {
			fileObject["file_url"] = file.FileUrl
		}
		if file.FileName != "" {
			fileObject["filename"] = file.FileName
		}
		if len(fileObject) == 0 {
			return nil, fmt.Errorf("messages[%d].content[%d].file requires file_id, file_data, or file_url", messageIndex, contentIndex)
		}
		return map[string]any{"type": "input_file", "file": fileObject}, nil
	case dto.ContentTypeVideoUrl:
		video := part.GetVideoUrl()
		if video == nil || strings.TrimSpace(video.Url) == "" {
			return nil, fmt.Errorf("messages[%d].content[%d].video_url.url is required", messageIndex, contentIndex)
		}
		return map[string]any{"type": "input_video", "video_url": video.Url}, nil
	default:
		return nil, fmt.Errorf("messages[%d].content[%d].type %q is not supported in responses compatibility mode", messageIndex, contentIndex, partType)
	}
}

func ChatCompletionsRequestToResponsesRequest(req *dto.GeneralOpenAIRequest) (*dto.OpenAIResponsesRequest, error) {
	return ChatCompletionsRequestToResponsesRequestWithOptions(req, ChatCompletionsToResponsesOptions{})
}

func ChatCompletionsRequestToResponsesRequestWithOptions(req *dto.GeneralOpenAIRequest, options ChatCompletionsToResponsesOptions) (*dto.OpenAIResponsesRequest, error) {
	if req == nil {
		return nil, errors.New("request is nil")
	}
	if req.Model == "" {
		return nil, errors.New("model is required")
	}
	if lo.FromPtrOr(req.N, 1) > 1 {
		return nil, fmt.Errorf("n>1 is not supported in responses compatibility mode")
	}

	var instructionsParts []string
	inputItems := make([]map[string]any, 0, len(req.Messages))
	aliases := newToolNameAliasRegistry()
	// Register declarations before walking history so a generated alias can be
	// checked against every tool reference deterministically.
	for toolIndex, tool := range req.Tools {
		toolType := strings.TrimSpace(tool.Type)
		if toolType == "" {
			toolType = "function"
		}
		if toolType != "function" || strings.TrimSpace(tool.Function.Name) == "" {
			continue
		}
		if _, err := aliases.normalizeDeclaration(tool.Function.Name); err != nil {
			return nil, fmt.Errorf("tools[%d].function.name: %w", toolIndex, err)
		}
	}

	for messageIndex, msg := range req.Messages {
		role := strings.TrimSpace(msg.Role)
		if role == "" {
			return nil, fmt.Errorf("messages[%d].role is required", messageIndex)
		}

		if role == "tool" || role == "function" {
			callID := strings.TrimSpace(msg.ToolCallId)

			var output any
			if msg.Content == nil {
				output = ""
			} else if msg.IsStringContent() {
				output = msg.StringContent()
			} else {
				b, err := common.Marshal(msg.Content)
				if err != nil {
					return nil, fmt.Errorf("messages[%d].content cannot be encoded as tool output: %w", messageIndex, err)
				}
				output = string(b)
			}

			if callID == "" {
				return nil, fmt.Errorf("messages[%d].tool_call_id is required for tool output", messageIndex)
			}

			inputItems = append(inputItems, map[string]any{
				"type":    "function_call_output",
				"call_id": callID,
				"output":  output,
			})
			continue
		}

		// Prefer mapping system/developer messages into `instructions`.
		if role == "system" || role == "developer" {
			if msg.Content == nil {
				continue
			}
			if msg.IsStringContent() {
				if s := strings.TrimSpace(msg.StringContent()); s != "" {
					instructionsParts = append(instructionsParts, s)
				}
				continue
			}
			parts, parseErr := msg.ParseContentStrict()
			if parseErr != nil {
				return nil, fmt.Errorf("messages[%d]: %w", messageIndex, parseErr)
			}
			var sb strings.Builder
			for contentIndex, part := range parts {
				if part.Type == dto.ContentTypeText && strings.TrimSpace(part.Text) != "" {
					if sb.Len() > 0 {
						sb.WriteString("\n")
					}
					sb.WriteString(part.Text)
					continue
				}
				if part.Type != dto.ContentTypeText {
					return nil, fmt.Errorf("messages[%d].content[%d].type %q cannot be represented in Responses instructions", messageIndex, contentIndex, part.Type)
				}
			}
			if s := strings.TrimSpace(sb.String()); s != "" {
				instructionsParts = append(instructionsParts, s)
			}
			continue
		}
		if role != "user" && role != "assistant" {
			return nil, fmt.Errorf("messages[%d].role %q is not supported in responses compatibility mode", messageIndex, role)
		}

		item := map[string]any{
			"role": role,
		}
		if options.PreserveReasoningContent && role == "assistant" {
			if reasoning, ok := chatMessageReasoningContent(msg); ok {
				inputItems = append(inputItems, map[string]any{
					"type": "reasoning",
					"content": []map[string]any{{
						"type": "reasoning_text",
						"text": reasoning,
					}},
				})
			}
		}

		if msg.Content == nil {
			item["content"] = ""
			inputItems = append(inputItems, item)

			if role == "assistant" {
				calls, err := convertChatToolCallsToResponses(msg, messageIndex, options, aliases)
				if err != nil {
					return nil, err
				}
				inputItems = append(inputItems, calls...)
			}
			continue
		}

		if msg.IsStringContent() {
			item["content"] = msg.StringContent()
			inputItems = append(inputItems, item)

			if role == "assistant" {
				calls, err := convertChatToolCallsToResponses(msg, messageIndex, options, aliases)
				if err != nil {
					return nil, err
				}
				inputItems = append(inputItems, calls...)
			}
			continue
		}

		parts, parseErr := msg.ParseContentStrict()
		if parseErr != nil {
			return nil, fmt.Errorf("messages[%d]: %w", messageIndex, parseErr)
		}
		contentParts := make([]map[string]any, 0, len(parts))
		for contentIndex, part := range parts {
			convertedPart, err := convertChatMediaPartToResponses(part, messageIndex, contentIndex)
			if err != nil {
				return nil, err
			}
			if role == "assistant" {
				if convertedPart["type"] != "input_text" {
					return nil, fmt.Errorf("messages[%d].content[%d].type %q is not supported for assistant history in Responses compatibility mode", messageIndex, contentIndex, convertedPart["type"])
				}
				convertedPart["type"] = "output_text"
			}
			contentParts = append(contentParts, convertedPart)
		}
		item["content"] = contentParts
		inputItems = append(inputItems, item)

		if role == "assistant" {
			calls, err := convertChatToolCallsToResponses(msg, messageIndex, options, aliases)
			if err != nil {
				return nil, err
			}
			inputItems = append(inputItems, calls...)
		}
	}

	inputRaw, err := common.Marshal(inputItems)
	if err != nil {
		return nil, err
	}

	var instructionsRaw json.RawMessage
	if len(instructionsParts) > 0 {
		instructions := strings.Join(instructionsParts, "\n\n")
		instructionsRaw, err = common.Marshal(instructions)
		if err != nil {
			return nil, fmt.Errorf("failed to encode instructions: %w", err)
		}
	}

	var toolsRaw json.RawMessage
	if req.Tools != nil {
		tools := make([]map[string]any, 0, len(req.Tools))
		for toolIndex, tool := range req.Tools {
			toolType := strings.TrimSpace(tool.Type)
			if toolType == "" {
				toolType = "function"
			}
			if toolType != "function" {
				return nil, fmt.Errorf("tools[%d].type %q is not supported in Responses compatibility mode", toolIndex, toolType)
			}
			name, nameErr := aliases.normalizeDeclaration(tool.Function.Name)
			if nameErr != nil {
				return nil, fmt.Errorf("tools[%d].function.name: %w", toolIndex, nameErr)
			}
			toolObject := map[string]any{
				"type":        "function",
				"name":        name,
				"description": tool.Function.Description,
				"parameters":  tool.Function.Parameters,
			}
			if tool.Function.Strict != nil {
				toolObject["strict"] = *tool.Function.Strict
			}
			tools = append(tools, toolObject)
		}
		toolsRaw, err = common.Marshal(tools)
		if err != nil {
			return nil, fmt.Errorf("failed to encode tools: %w", err)
		}
	}

	var toolChoiceRaw json.RawMessage
	if req.ToolChoice != nil {
		switch v := req.ToolChoice.(type) {
		case string:
			normalizedChoice := strings.TrimSpace(v)
			switch normalizedChoice {
			case "auto", "none", "required":
			default:
				return nil, fmt.Errorf("tool_choice %q is not supported in Responses compatibility mode", v)
			}
			toolChoiceRaw, err = common.Marshal(normalizedChoice)
			if err != nil {
				return nil, fmt.Errorf("failed to encode tool_choice: %w", err)
			}
		default:
			var m map[string]any
			b, marshalErr := common.Marshal(v)
			if marshalErr != nil {
				return nil, fmt.Errorf("failed to encode tool_choice: %w", marshalErr)
			}
			if unmarshalErr := common.Unmarshal(b, &m); unmarshalErr != nil {
				return nil, fmt.Errorf("tool_choice must be a string or object: %w", unmarshalErr)
			}
			if m == nil {
				return nil, fmt.Errorf("tool_choice must be a string or object")
			}
			if t, _ := m["type"].(string); t == "function" {
				// Chat: {"type":"function","function":{"name":"..."}}
				// Responses: {"type":"function","name":"..."}
				if name, ok := m["name"].(string); ok && name != "" {
					name, err = aliases.normalizeReference(name)
					if err != nil {
						return nil, fmt.Errorf("tool_choice function name: %w", err)
					}
					toolChoiceRaw, err = common.Marshal(map[string]any{
						"type": "function",
						"name": name,
					})
				} else if fn, ok := m["function"].(map[string]any); ok {
					if name, ok := fn["name"].(string); ok && name != "" {
						name, err = aliases.normalizeReference(name)
						if err != nil {
							return nil, fmt.Errorf("tool_choice function name: %w", err)
						}
						toolChoiceRaw, err = common.Marshal(map[string]any{
							"type": "function",
							"name": name,
						})
					} else {
						return nil, fmt.Errorf("tool_choice function name is required")
					}
				} else {
					return nil, fmt.Errorf("tool_choice function name is required")
				}
			} else {
				return nil, fmt.Errorf("tool_choice type %q is not supported in Responses compatibility mode", m["type"])
			}
			if err != nil {
				return nil, fmt.Errorf("failed to encode tool_choice: %w", err)
			}
		}
	}

	var parallelToolCallsRaw json.RawMessage
	if req.ParallelTooCalls != nil {
		parallelToolCallsRaw, err = common.Marshal(*req.ParallelTooCalls)
		if err != nil {
			return nil, fmt.Errorf("failed to encode parallel_tool_calls: %w", err)
		}
	}

	textRaw, err := convertChatResponseFormatToResponsesText(req.ResponseFormat)
	if err != nil {
		return nil, err
	}

	maxOutputTokens := lo.FromPtrOr(req.MaxTokens, uint(0))
	maxCompletionTokens := lo.FromPtrOr(req.MaxCompletionTokens, uint(0))
	if maxCompletionTokens > maxOutputTokens {
		maxOutputTokens = maxCompletionTokens
	}
	// OpenAI Responses API rejects max_output_tokens < 16 when explicitly provided.
	//if maxOutputTokens > 0 && maxOutputTokens < 16 {
	//	maxOutputTokens = 16
	//}

	var topP *float64
	if req.TopP != nil {
		topP = common.GetPointer(lo.FromPtr(req.TopP))
	}

	out := &dto.OpenAIResponsesRequest{
		Model:             req.Model,
		Input:             inputRaw,
		Instructions:      instructionsRaw,
		Stream:            req.Stream,
		Temperature:       req.Temperature,
		Text:              textRaw,
		ToolChoice:        toolChoiceRaw,
		Tools:             toolsRaw,
		TopP:              topP,
		User:              req.User,
		ParallelToolCalls: parallelToolCallsRaw,
		Store:             req.Store,
		Metadata:          req.Metadata,
	}
	if req.MaxTokens != nil || req.MaxCompletionTokens != nil {
		out.MaxOutputTokens = lo.ToPtr(maxOutputTokens)
	}

	if req.ReasoningEffort != "" {
		out.Reasoning = &dto.Reasoning{
			Effort:  req.ReasoningEffort,
			Summary: "detailed",
		}
	}

	if options.ToolNameRestorerOut != nil {
		*options.ToolNameRestorerOut = relaycommon.NewResponsesToolNameRestorer(aliases.originalByAliasCopy())
	}

	return out, nil
}

func chatMessageReasoningContent(msg dto.Message) (string, bool) {
	if msg.ReasoningContent != nil {
		return *msg.ReasoningContent, true
	}
	if msg.ThinkingContent != nil {
		return *msg.ThinkingContent, true
	}
	if msg.Reasoning != nil {
		return *msg.Reasoning, true
	}
	return "", false
}
