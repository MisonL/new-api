package openaicompat

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

type ResponsesChatCompatibilityOptions struct {
	EnableCustomToolBridge bool
}

func isResponsesIncludeIgnoredInChatCompatibility(includeValue string) bool {
	switch includeValue {
	case "reasoning.encrypted_content":
		// This is an optional Responses side channel. Chat upstreams cannot emit it,
		// but clients such as Codex tolerate it being absent in compatibility mode.
		return true
	default:
		return false
	}
}
func rawMessageEnabled(raw []byte) bool {
	jsonType := common.GetJsonType(raw)
	return jsonType != "" && jsonType != "null" && jsonType != "unknown"
}

func rawMessageToString(raw []byte, fieldName string) (string, error) {
	if !rawMessageEnabled(raw) {
		return "", nil
	}
	if common.GetJsonType(raw) != "string" {
		return "", fmt.Errorf("%s must be a string", fieldName)
	}
	var out string
	if err := common.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("failed to parse %s: %w", fieldName, err)
	}
	return out, nil
}

func rawMessageToBoolPointer(raw []byte, fieldName string) (*bool, error) {
	if !rawMessageEnabled(raw) {
		return nil, nil
	}
	if common.GetJsonType(raw) != "boolean" {
		return nil, fmt.Errorf("%s must be a boolean", fieldName)
	}
	var out bool
	if err := common.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", fieldName, err)
	}
	return &out, nil
}

func validateResponsesIncludeForChatCompatibility(raw []byte) error {
	if !rawMessageEnabled(raw) {
		return nil
	}
	if common.GetJsonType(raw) != "array" {
		return fmt.Errorf("include must be an array")
	}

	var includeValues []string
	if err := common.Unmarshal(raw, &includeValues); err != nil {
		return fmt.Errorf("failed to parse include: %w", err)
	}
	for _, includeValue := range includeValues {
		normalized := strings.TrimSpace(includeValue)
		if normalized == "" {
			return fmt.Errorf("include contains an empty value")
		}
		if !isResponsesIncludeIgnoredInChatCompatibility(normalized) {
			return fmt.Errorf("include value %q is not supported in chat compatibility mode", normalized)
		}
	}
	return nil
}

func convertResponsesTextToChatResponseFormat(raw []byte) (*dto.ResponseFormat, error) {
	if !rawMessageEnabled(raw) {
		return nil, nil
	}
	if common.GetJsonType(raw) != "object" {
		return nil, fmt.Errorf("text must be an object")
	}

	var textObject map[string]any
	if err := common.Unmarshal(raw, &textObject); err != nil {
		return nil, fmt.Errorf("failed to parse text: %w", err)
	}

	formatAny, ok := textObject["format"]
	if !ok || formatAny == nil {
		return nil, nil
	}

	formatMap, ok := formatAny.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("text.format must be an object")
	}

	formatType := strings.TrimSpace(common.Interface2String(formatMap["type"]))
	if formatType == "" {
		return nil, fmt.Errorf("text.format.type is required")
	}

	responseFormat := &dto.ResponseFormat{
		Type: formatType,
	}

	if formatType != "json_schema" {
		return responseFormat, nil
	}

	schemaMap := make(map[string]any)
	for key, value := range formatMap {
		if key == "type" {
			continue
		}
		schemaMap[key] = value
	}
	if len(schemaMap) == 0 {
		return responseFormat, nil
	}

	schemaRaw, err := common.Marshal(schemaMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal json_schema: %w", err)
	}
	responseFormat.JsonSchema = schemaRaw
	return responseFormat, nil
}

func convertResponsesToolChoiceToChat(raw []byte, options ResponsesChatCompatibilityOptions) (any, error) {
	if !rawMessageEnabled(raw) {
		return nil, nil
	}

	switch common.GetJsonType(raw) {
	case "string":
		var mode string
		if err := common.Unmarshal(raw, &mode); err != nil {
			return nil, fmt.Errorf("failed to parse tool_choice: %w", err)
		}
		return mode, nil
	case "object":
		var toolChoice map[string]any
		if err := common.Unmarshal(raw, &toolChoice); err != nil {
			return nil, fmt.Errorf("failed to parse tool_choice: %w", err)
		}
		toolType := strings.TrimSpace(common.Interface2String(toolChoice["type"]))
		if toolType == dto.CustomType {
			if !options.EnableCustomToolBridge {
				return nil, fmt.Errorf("custom tool bridge is not enabled in chat compatibility mode")
			}
			name := extractResponsesToolName(toolChoice)
			if name == "" {
				return nil, fmt.Errorf("tool_choice custom name is required")
			}
			return map[string]any{
				"type": dto.CustomType,
				"custom": map[string]any{
					"name": name,
				},
			}, nil
		}
		if toolType != "function" {
			return nil, fmt.Errorf("tool_choice type %q is not supported in chat compatibility mode", toolType)
		}

		name := strings.TrimSpace(common.Interface2String(toolChoice["name"]))
		if name == "" {
			if functionMap, ok := toolChoice["function"].(map[string]any); ok {
				name = strings.TrimSpace(common.Interface2String(functionMap["name"]))
			}
		}
		if name == "" {
			return nil, fmt.Errorf("tool_choice function name is required")
		}
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": name,
			},
		}, nil
	default:
		return nil, fmt.Errorf("tool_choice type %q is not supported", common.GetJsonType(raw))
	}
}

func extractResponsesToolName(tool map[string]any) string {
	name := strings.TrimSpace(common.Interface2String(tool["name"]))
	if name != "" {
		return name
	}
	if functionMap, ok := tool["function"].(map[string]any); ok {
		name = strings.TrimSpace(common.Interface2String(functionMap["name"]))
		if name != "" {
			return name
		}
	}
	if customMap, ok := tool["custom"].(map[string]any); ok {
		return strings.TrimSpace(common.Interface2String(customMap["name"]))
	}
	return ""
}

func buildChatCustomToolPayload(tool map[string]any, name string) (common.RawMessage, error) {
	customPayload := make(map[string]any)
	if customMap, ok := tool["custom"].(map[string]any); ok {
		for key, value := range customMap {
			customPayload[key] = value
		}
	}
	customPayload["name"] = name
	for _, key := range []string{"description", "format"} {
		if value, ok := tool[key]; ok && value != nil {
			customPayload[key] = value
		}
	}
	return common.Marshal(customPayload)
}

func isResponsesToolIgnoredInChatCompatibility(toolType string) bool {
	switch toolType {
	case "web_search", dto.BuildInToolWebSearchPreview, dto.BuildInToolFileSearch:
		return true
	default:
		return false
	}
}

func convertResponsesToolsToChat(raw []byte, options ResponsesChatCompatibilityOptions) ([]dto.ToolCallRequest, error) {
	if !rawMessageEnabled(raw) {
		return nil, nil
	}
	if common.GetJsonType(raw) != "array" {
		return nil, fmt.Errorf("tools must be an array")
	}

	var rawTools []map[string]any
	if err := common.Unmarshal(raw, &rawTools); err != nil {
		return nil, fmt.Errorf("failed to parse tools: %w", err)
	}

	tools := make([]dto.ToolCallRequest, 0, len(rawTools))
	for _, tool := range rawTools {
		toolType := strings.TrimSpace(common.Interface2String(tool["type"]))
		if isResponsesToolIgnoredInChatCompatibility(toolType) {
			continue
		}
		if toolType == dto.CustomType {
			if !options.EnableCustomToolBridge {
				return nil, fmt.Errorf("custom tool bridge is not enabled in chat compatibility mode")
			}
			name := extractResponsesToolName(tool)
			if name == "" {
				return nil, fmt.Errorf("custom tool name is required")
			}
			customPayload, err := buildChatCustomToolPayload(tool, name)
			if err != nil {
				return nil, fmt.Errorf("failed to encode custom tool: %w", err)
			}
			tools = append(tools, dto.ToolCallRequest{
				Type:   dto.CustomType,
				Custom: customPayload,
			})
			continue
		}
		if toolType != "function" {
			return nil, fmt.Errorf("tool type %q is not supported in chat compatibility mode", toolType)
		}

		name := strings.TrimSpace(common.Interface2String(tool["name"]))
		if name == "" {
			return nil, fmt.Errorf("tool name is required")
		}

		tools = append(tools, dto.ToolCallRequest{
			Type: "function",
			Function: dto.FunctionRequest{
				Name:        name,
				Description: common.Interface2String(tool["description"]),
				Parameters:  tool["parameters"],
			},
		})
	}
	return tools, nil
}

func stringifyResponsesOutput(value any) (string, error) {
	switch v := value.(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	default:
		raw, err := common.Marshal(v)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
}

func convertResponsesContentPartToChat(part map[string]any) (dto.MediaContent, error) {
	partType := strings.TrimSpace(common.Interface2String(part["type"]))
	switch partType {
	case "input_text", "output_text", dto.ContentTypeText:
		return dto.MediaContent{
			Type: dto.ContentTypeText,
			Text: common.Interface2String(part["text"]),
		}, nil
	case "input_image":
		image := part["image_url"]
		if image == nil {
			image = part["image"]
		}
		if image == nil {
			return dto.MediaContent{}, fmt.Errorf("input_image.image_url is required")
		}
		return dto.MediaContent{
			Type:     dto.ContentTypeImageURL,
			ImageUrl: image,
		}, nil
	case "input_audio":
		audio := part["input_audio"]
		if audio == nil {
			return dto.MediaContent{}, fmt.Errorf("input_audio.input_audio is required")
		}
		return dto.MediaContent{
			Type:       dto.ContentTypeInputAudio,
			InputAudio: audio,
		}, nil
	case "input_file":
		file := part["file"]
		if file == nil {
			file = part["file_url"]
		}
		if file == nil {
			return dto.MediaContent{}, fmt.Errorf("input_file.file is required")
		}
		return dto.MediaContent{
			Type: dto.ContentTypeFile,
			File: file,
		}, nil
	case "input_video":
		video := part["video_url"]
		if video == nil {
			return dto.MediaContent{}, fmt.Errorf("input_video.video_url is required")
		}
		return dto.MediaContent{
			Type:     dto.ContentTypeVideoUrl,
			VideoUrl: video,
		}, nil
	default:
		return dto.MediaContent{}, fmt.Errorf("input content type %q is not supported in chat compatibility mode", partType)
	}
}

func flushPendingUserMessage(messages *[]dto.Message, pending []dto.MediaContent) []dto.MediaContent {
	if len(pending) == 0 {
		return pending
	}
	message := dto.Message{Role: "user"}
	if len(pending) == 1 && pending[0].Type == dto.ContentTypeText {
		message.SetStringContent(pending[0].Text)
	} else {
		message.SetMediaContent(append([]dto.MediaContent(nil), pending...))
	}
	*messages = append(*messages, message)
	return pending[:0]
}

func convertResponsesInputToChatMessages(raw []byte, options ResponsesChatCompatibilityOptions) ([]dto.Message, error) {
	if !rawMessageEnabled(raw) {
		return nil, nil
	}

	switch common.GetJsonType(raw) {
	case "string":
		text, err := rawMessageToString(raw, "input")
		if err != nil {
			return nil, err
		}
		return []dto.Message{
			{
				Role:    "user",
				Content: text,
			},
		}, nil
	case "array":
	default:
		return nil, fmt.Errorf("input type %q is not supported in chat compatibility mode", common.GetJsonType(raw))
	}

	var items []map[string]any
	if err := common.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("failed to parse input: %w", err)
	}

	messages := make([]dto.Message, 0, len(items))
	pendingUserParts := make([]dto.MediaContent, 0)

	for _, item := range items {
		role := strings.TrimSpace(common.Interface2String(item["role"]))
		itemType := strings.TrimSpace(common.Interface2String(item["type"]))

		if role != "" || itemType == "message" {
			pendingUserParts = flushPendingUserMessage(&messages, pendingUserParts)

			if role == "" {
				role = "user"
			}

			message := dto.Message{Role: role}
			content := item["content"]
			if content == nil {
				message.SetStringContent("")
				messages = append(messages, message)
				continue
			}

			switch typedContent := content.(type) {
			case string:
				message.SetStringContent(typedContent)
			case []any:
				parts := make([]dto.MediaContent, 0, len(typedContent))
				for _, partAny := range typedContent {
					partMap, ok := partAny.(map[string]any)
					if !ok {
						return nil, fmt.Errorf("message content item must be an object")
					}
					part, err := convertResponsesContentPartToChat(partMap)
					if err != nil {
						return nil, err
					}
					parts = append(parts, part)
				}
				message.SetMediaContent(parts)
			default:
				return nil, fmt.Errorf("message content type %T is not supported in chat compatibility mode", content)
			}
			messages = append(messages, message)
			continue
		}

		switch itemType {
		case "function_call":
			pendingUserParts = flushPendingUserMessage(&messages, pendingUserParts)

			callID := strings.TrimSpace(common.Interface2String(item["call_id"]))
			name := strings.TrimSpace(common.Interface2String(item["name"]))
			if callID == "" || name == "" {
				return nil, fmt.Errorf("function_call requires call_id and name")
			}
			arguments, err := stringifyResponsesOutput(item["arguments"])
			if err != nil {
				return nil, fmt.Errorf("failed to encode function_call arguments: %w", err)
			}

			message := dto.Message{Role: "assistant", Content: ""}
			message.SetToolCalls([]dto.ToolCallRequest{
				{
					ID:   callID,
					Type: "function",
					Function: dto.FunctionRequest{
						Name:      name,
						Arguments: arguments,
					},
				},
			})
			messages = append(messages, message)
		case "custom_tool_call":
			if !options.EnableCustomToolBridge {
				return nil, fmt.Errorf("custom tool bridge is not enabled in chat compatibility mode")
			}
			pendingUserParts = flushPendingUserMessage(&messages, pendingUserParts)

			callID := strings.TrimSpace(common.Interface2String(item["call_id"]))
			name := strings.TrimSpace(common.Interface2String(item["name"]))
			if callID == "" || name == "" {
				return nil, fmt.Errorf("custom_tool_call requires call_id and name")
			}

			customPayload := map[string]any{
				"name": name,
			}
			if input, ok := item["input"]; ok {
				customPayload["input"] = input
			}
			customRaw, err := common.Marshal(customPayload)
			if err != nil {
				return nil, fmt.Errorf("failed to encode custom_tool_call: %w", err)
			}

			message := dto.Message{Role: "assistant", Content: ""}
			message.SetToolCalls([]dto.ToolCallRequest{
				{
					ID:     callID,
					Type:   dto.CustomType,
					Custom: customRaw,
				},
			})
			messages = append(messages, message)
		case "function_call_output":
			pendingUserParts = flushPendingUserMessage(&messages, pendingUserParts)

			callID := strings.TrimSpace(common.Interface2String(item["call_id"]))
			if callID == "" {
				return nil, fmt.Errorf("function_call_output requires call_id")
			}
			output, err := stringifyResponsesOutput(item["output"])
			if err != nil {
				return nil, fmt.Errorf("failed to encode function_call_output: %w", err)
			}
			messages = append(messages, dto.Message{
				Role:       "tool",
				Content:    output,
				ToolCallId: callID,
			})
		case "custom_tool_call_output":
			if !options.EnableCustomToolBridge {
				return nil, fmt.Errorf("custom tool bridge is not enabled in chat compatibility mode")
			}
			pendingUserParts = flushPendingUserMessage(&messages, pendingUserParts)

			callID := strings.TrimSpace(common.Interface2String(item["call_id"]))
			if callID == "" {
				return nil, fmt.Errorf("custom_tool_call_output requires call_id")
			}
			output, err := stringifyResponsesOutput(item["output"])
			if err != nil {
				return nil, fmt.Errorf("failed to encode custom_tool_call_output: %w", err)
			}
			messages = append(messages, dto.Message{
				Role:       "tool",
				Content:    output,
				ToolCallId: callID,
			})
		case "input_text", "input_image", "input_audio", "input_file", "input_video":
			part, err := convertResponsesContentPartToChat(item)
			if err != nil {
				return nil, err
			}
			pendingUserParts = append(pendingUserParts, part)
		default:
			return nil, fmt.Errorf("input item type %q is not supported in chat compatibility mode", itemType)
		}
	}

	pendingUserParts = flushPendingUserMessage(&messages, pendingUserParts)
	return messages, nil
}

func ResponsesRequestToChatCompletionsRequest(req *dto.OpenAIResponsesRequest) (*dto.GeneralOpenAIRequest, error) {
	return ResponsesRequestToChatCompletionsRequestWithOptions(req, ResponsesChatCompatibilityOptions{})
}

func ResponsesRequestToChatCompletionsRequestWithOptions(req *dto.OpenAIResponsesRequest, options ResponsesChatCompatibilityOptions) (*dto.GeneralOpenAIRequest, error) {
	if req == nil {
		return nil, fmt.Errorf("request is nil")
	}
	if strings.TrimSpace(req.Model) == "" {
		return nil, fmt.Errorf("model is required")
	}
	if strings.TrimSpace(req.PreviousResponseID) != "" {
		return nil, fmt.Errorf("previous_response_id is not supported in chat compatibility mode")
	}
	if err := validateResponsesIncludeForChatCompatibility(req.Include); err != nil {
		return nil, err
	}
	if rawMessageEnabled(req.Conversation) {
		return nil, fmt.Errorf("conversation is not supported in chat compatibility mode")
	}
	if rawMessageEnabled(req.ContextManagement) {
		return nil, fmt.Errorf("context_management is not supported in chat compatibility mode")
	}
	if rawMessageEnabled(req.Truncation) {
		return nil, fmt.Errorf("truncation is not supported in chat compatibility mode")
	}
	if req.MaxToolCalls != nil {
		return nil, fmt.Errorf("max_tool_calls is not supported in chat compatibility mode")
	}
	if rawMessageEnabled(req.Prompt) {
		return nil, fmt.Errorf("prompt is not supported in chat compatibility mode")
	}
	if rawMessageEnabled(req.Preset) {
		return nil, fmt.Errorf("preset is not supported in chat compatibility mode")
	}

	out := &dto.GeneralOpenAIRequest{
		Model:                req.Model,
		Stream:               req.Stream,
		StreamOptions:        req.StreamOptions,
		Temperature:          req.Temperature,
		TopP:                 req.TopP,
		User:                 req.User,
		Store:                req.Store,
		PromptCacheRetention: req.PromptCacheRetention,
		SafetyIdentifier:     req.SafetyIdentifier,
		Metadata:             req.Metadata,
		EnableThinking:       req.EnableThinking,
	}
	if req.MaxOutputTokens != nil {
		out.MaxTokens = common.GetPointer(*req.MaxOutputTokens)
	}
	if req.TopLogProbs != nil {
		out.TopLogProbs = common.GetPointer(*req.TopLogProbs)
		out.LogProbs = common.GetPointer(true)
	}
	if req.Reasoning != nil {
		out.ReasoningEffort = strings.TrimSpace(req.Reasoning.Effort)
	}
	if strings.TrimSpace(req.ServiceTier) != "" {
		serviceTierRaw, marshalErr := common.Marshal(req.ServiceTier)
		if marshalErr != nil {
			return nil, fmt.Errorf("failed to encode service_tier: %w", marshalErr)
		}
		out.ServiceTier = serviceTierRaw
	}

	promptCacheKey, err := rawMessageToString(req.PromptCacheKey, "prompt_cache_key")
	if err != nil {
		return nil, err
	}
	out.PromptCacheKey = promptCacheKey

	parallelToolCalls, err := rawMessageToBoolPointer(req.ParallelToolCalls, "parallel_tool_calls")
	if err != nil {
		return nil, err
	}
	out.ParallelTooCalls = parallelToolCalls

	toolChoice, err := convertResponsesToolChoiceToChat(req.ToolChoice, options)
	if err != nil {
		return nil, err
	}
	out.ToolChoice = toolChoice

	tools, err := convertResponsesToolsToChat(req.Tools, options)
	if err != nil {
		return nil, err
	}
	out.Tools = tools

	responseFormat, err := convertResponsesTextToChatResponseFormat(req.Text)
	if err != nil {
		return nil, err
	}
	out.ResponseFormat = responseFormat

	messages, err := convertResponsesInputToChatMessages(req.Input, options)
	if err != nil {
		return nil, err
	}
	out.Messages = messages

	instructions, err := rawMessageToString(req.Instructions, "instructions")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(instructions) != "" {
		systemRole := out.GetSystemRoleName()
		out.Messages = append([]dto.Message{
			{
				Role:    systemRole,
				Content: instructions,
			},
		}, out.Messages...)
	}

	return out, nil
}
