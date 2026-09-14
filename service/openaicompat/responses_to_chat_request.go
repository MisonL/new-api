package openaicompat

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

type ResponsesChatCompatibilityOptions struct {
	EnableCustomToolBridge bool
	toolState              *ResponsesChatCompatibilityToolState
}

const responsesToolTypeNamespace = "namespace"

// ResponsesRequestHasUnbridgeableNamespaceTools reports whether a request has
// a namespace declaration that the configured Responses-to-Chat bridge cannot
// convert. Malformed declarations suppress the capability skip so the normal
// converter can return the precise input error instead of turning bad input
// into a routing decision. String tools remain valid shorthand custom tools.
func ResponsesRequestHasUnbridgeableNamespaceTools(req *dto.OpenAIResponsesRequest, options ResponsesChatCompatibilityOptions) (bool, error) {
	if req == nil {
		return false, nil
	}

	tools, err := req.EffectiveTools()
	if err != nil {
		return false, err
	}

	var unbridgeable bool
	var malformed bool
	for _, rawTool := range tools {
		jsonType := common.GetJsonType(rawTool)
		if jsonType == "string" {
			continue
		}
		if jsonType != "object" {
			malformed = true
			continue
		}
		var tool map[string]any
		if err := common.Unmarshal(rawTool, &tool); err != nil {
			malformed = true
			continue
		}
		if strings.TrimSpace(common.Interface2String(tool["type"])) != responsesToolTypeNamespace {
			continue
		}

		namespace := extractResponsesToolName(tool)
		nestedTools, err := parseResponsesNamespaceTools(tool)
		if err != nil || namespace == "" {
			malformed = true
			continue
		}

		for _, nestedTool := range nestedTools {
			nestedType := strings.TrimSpace(common.Interface2String(nestedTool["type"]))
			switch nestedType {
			case "function":
				if _, err := convertResponsesFunctionToolToChat(nestedTool); err != nil {
					malformed = true
					continue
				}
			case dto.CustomType:
				if _, err := convertResponsesCustomToolToChat(nestedTool, namespace); err != nil {
					malformed = true
					continue
				}
			default:
				if nestedType == "" {
					malformed = true
					continue
				}
				unbridgeable = true
				continue
			}
			if !options.EnableCustomToolBridge {
				unbridgeable = true
			}
		}
	}

	return unbridgeable && !malformed, nil
}

type responsesNamespaceToolSelection struct {
	name string
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

func convertResponsesToolChoiceToChat(raw []byte, options ResponsesChatCompatibilityOptions) (any, *responsesNamespaceToolSelection, error) {
	if !rawMessageEnabled(raw) {
		return nil, nil, nil
	}

	switch common.GetJsonType(raw) {
	case "string":
		var mode string
		if err := common.Unmarshal(raw, &mode); err != nil {
			return nil, nil, fmt.Errorf("failed to parse tool_choice: %w", err)
		}
		return mode, nil, nil
	case "object":
		var toolChoice map[string]any
		if err := common.Unmarshal(raw, &toolChoice); err != nil {
			return nil, nil, fmt.Errorf("failed to parse tool_choice: %w", err)
		}
		toolType := strings.TrimSpace(common.Interface2String(toolChoice["type"]))
		if toolType == responsesToolTypeToolSearch {
			if !options.EnableCustomToolBridge {
				return nil, nil, fmt.Errorf("custom tool bridge is not enabled in chat compatibility mode")
			}
			return map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": responsesToolTypeToolSearch,
				},
			}, nil, nil
		}
		if toolType == dto.CustomType {
			if !options.EnableCustomToolBridge {
				return nil, nil, fmt.Errorf("custom tool bridge is not enabled in chat compatibility mode")
			}
			name := extractResponsesToolName(toolChoice)
			if name == "" {
				return nil, nil, fmt.Errorf("tool_choice custom name is required")
			}
			return map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": flattenResponsesNamespacedCustomToolName(extractResponsesToolNamespace(toolChoice), name),
				},
			}, nil, nil
		}
		if toolType == responsesToolTypeNamespace {
			if !options.EnableCustomToolBridge {
				return nil, nil, fmt.Errorf("custom tool bridge is not enabled in chat compatibility mode")
			}
			name := extractResponsesToolName(toolChoice)
			if name == "" {
				return nil, nil, fmt.Errorf("tool_choice namespace name is required")
			}
			return "required", &responsesNamespaceToolSelection{name: name}, nil
		}
		if toolType != "function" {
			return nil, nil, fmt.Errorf("tool_choice type %q is not supported in chat compatibility mode", toolType)
		}

		name := strings.TrimSpace(common.Interface2String(toolChoice["name"]))
		if name == "" {
			if functionMap, ok := toolChoice["function"].(map[string]any); ok {
				name = strings.TrimSpace(common.Interface2String(functionMap["name"]))
			}
		}
		if name == "" {
			return nil, nil, fmt.Errorf("tool_choice function name is required")
		}
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": dto.NormalizeToolName(name),
			},
		}, nil, nil
	default:
		return nil, nil, fmt.Errorf("tool_choice type %q is not supported", common.GetJsonType(raw))
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
	if namespace := strings.TrimSpace(common.Interface2String(tool[responsesToolTypeNamespace])); namespace != "" {
		return namespace
	}
	return ""
}

func extractResponsesToolNamespace(tool map[string]any) string {
	return strings.TrimSpace(common.Interface2String(tool["namespace"]))
}

func convertResponsesCustomToolToChat(tool map[string]any, namespace string) (dto.ToolCallRequest, error) {
	name := extractResponsesToolName(tool)
	if name == "" {
		return dto.ToolCallRequest{}, fmt.Errorf("custom tool name is required")
	}
	formatGuidance, err := responsesCustomToolFormatGuidance(tool)
	if err != nil {
		return dto.ToolCallRequest{}, err
	}
	description := strings.TrimSpace(common.Interface2String(tool["description"]))
	if description == "" {
		description = "Execute the custom tool with its complete input in the input field."
	} else {
		description += " Provide the complete custom tool input in the input field."
	}
	description += formatGuidance
	return dto.ToolCallRequest{
		Type: "function",
		Function: dto.FunctionRequest{
			Name:        flattenResponsesNamespacedCustomToolName(namespace, name),
			Description: description,
			Parameters: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"input": map[string]any{
						"type":        "string",
						"description": "Complete input for the custom tool.",
					},
				},
				"required": []string{"input"},
			},
		},
	}, nil
}

func responsesToolSearchProxyChatTool() dto.ToolCallRequest {
	return dto.ToolCallRequest{
		Type: "function",
		Function: dto.FunctionRequest{
			Name:        responsesToolTypeToolSearch,
			Description: "Search and load tools, plugins, connectors, and namespaces for the current task.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search query for tools or connectors to load.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of tool groups to return.",
					},
				},
				"required": []string{"query"},
			},
		},
	}
}

func parseResponsesNamespaceTools(tool map[string]any) ([]map[string]any, error) {
	nestedToolsAny, ok := tool["tools"]
	if !ok || nestedToolsAny == nil {
		return nil, fmt.Errorf("namespace tool tools is required")
	}
	nestedToolsRaw, err := common.Marshal(nestedToolsAny)
	if err != nil {
		return nil, fmt.Errorf("failed to encode namespace tools: %w", err)
	}
	var nestedTools []map[string]any
	if err := common.Unmarshal(nestedToolsRaw, &nestedTools); err != nil {
		return nil, fmt.Errorf("failed to parse namespace tools: %w", err)
	}
	if len(nestedTools) == 0 {
		return nil, fmt.Errorf("namespace tool tools must not be empty")
	}
	return nestedTools, nil
}

func convertResponsesNamespaceToolToChat(tool map[string]any, toolState *ResponsesChatCompatibilityToolState) ([]dto.ToolCallRequest, error) {
	namespace := extractResponsesToolName(tool)
	if namespace == "" {
		return nil, fmt.Errorf("namespace tool name is required")
	}
	namespaceDescription := strings.TrimSpace(common.Interface2String(tool["description"]))
	nestedTools, err := parseResponsesNamespaceTools(tool)
	if err != nil {
		return nil, err
	}
	out := make([]dto.ToolCallRequest, 0, len(nestedTools))
	for _, nestedTool := range nestedTools {
		nestedType := strings.TrimSpace(common.Interface2String(nestedTool["type"]))

		var (
			chatTool     dto.ToolCallRequest
			identityType string
			originalName string
		)
		switch nestedType {
		case "function":
			chatTool, err = convertResponsesFunctionToolToChat(nestedTool)
			if err != nil {
				return nil, err
			}
			originalName = extractResponsesToolName(nestedTool)
			chatTool.Function.Name = flattenResponsesNamespaceToolName(namespace, originalName)
			identityType = "function"
		case dto.CustomType:
			chatTool, err = convertResponsesCustomToolToChat(nestedTool, namespace)
			if err != nil {
				return nil, err
			}
			originalName = extractResponsesToolName(nestedTool)
			identityType = dto.CustomType
		default:
			return nil, fmt.Errorf("namespace tool type %q is not supported in chat compatibility mode", nestedType)
		}
		chatTool.Function.Description = withResponsesNamespaceDescription(chatTool.Function.Description, namespaceDescription)
		duplicate, err := toolState.register(chatTool.Function.Name, ResponsesChatToolIdentity{
			Type:      identityType,
			Namespace: namespace,
			Name:      originalName,
		}, chatTool)
		if err != nil {
			return nil, err
		}
		if duplicate {
			continue
		}
		out = append(out, chatTool)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("namespace tool has no compatible entries")
	}
	return out, nil
}

func convertResponsesToolsToChat(raw []byte, options ResponsesChatCompatibilityOptions, namespaceSelection *responsesNamespaceToolSelection, toolState *ResponsesChatCompatibilityToolState) ([]dto.ToolCallRequest, error) {
	if !rawMessageEnabled(raw) {
		if namespaceSelection != nil {
			return nil, fmt.Errorf("tool_choice namespace %q requires matching namespace tools", namespaceSelection.name)
		}
		return nil, nil
	}
	if common.GetJsonType(raw) != "array" {
		return nil, fmt.Errorf("tools must be an array")
	}

	var rawTools []common.RawMessage
	if err := common.Unmarshal(raw, &rawTools); err != nil {
		return nil, fmt.Errorf("failed to parse tools: %w", err)
	}

	tools := make([]dto.ToolCallRequest, 0, len(rawTools))
	for index, rawTool := range rawTools {
		tool := make(map[string]any)
		switch common.GetJsonType(rawTool) {
		case "string":
			var name string
			if err := common.Unmarshal(rawTool, &name); err != nil {
				return nil, fmt.Errorf("failed to parse tools[%d]: %w", index, err)
			}
			if strings.TrimSpace(name) == "" {
				return nil, fmt.Errorf("tools[%d] custom name is required", index)
			}
			tool["type"] = dto.CustomType
			tool["name"] = name
		case "object":
			if err := common.Unmarshal(rawTool, &tool); err != nil {
				return nil, fmt.Errorf("failed to parse tools[%d]: %w", index, err)
			}
		default:
			return nil, fmt.Errorf("tools[%d] must be an object or string", index)
		}
		toolType := strings.TrimSpace(common.Interface2String(tool["type"]))
		if toolType == responsesToolTypeNamespace {
			if namespaceSelection != nil && extractResponsesToolName(tool) != namespaceSelection.name {
				continue
			}
			if !options.EnableCustomToolBridge {
				return nil, fmt.Errorf("custom tool bridge is not enabled in chat compatibility mode")
			}
			namespaceTools, err := convertResponsesNamespaceToolToChat(tool, toolState)
			if err != nil {
				return nil, err
			}
			tools = append(tools, namespaceTools...)
			continue
		}
		if namespaceSelection != nil {
			continue
		}
		if toolType == responsesToolTypeToolSearch {
			if !options.EnableCustomToolBridge {
				return nil, fmt.Errorf("custom tool bridge is not enabled in chat compatibility mode")
			}
			chatTool := responsesToolSearchProxyChatTool()
			duplicate, err := toolState.register(responsesToolTypeToolSearch, ResponsesChatToolIdentity{
				Type: responsesToolTypeToolSearch,
				Name: responsesToolTypeToolSearch,
			}, chatTool)
			if err != nil {
				return nil, err
			}
			if !duplicate {
				tools = append(tools, chatTool)
			}
			continue
		}
		if toolType == dto.CustomType {
			if !options.EnableCustomToolBridge {
				return nil, fmt.Errorf("custom tool bridge is not enabled in chat compatibility mode")
			}
			namespace := extractResponsesToolNamespace(tool)
			chatTool, err := convertResponsesCustomToolToChat(tool, namespace)
			if err != nil {
				return nil, err
			}
			name := extractResponsesToolName(tool)
			duplicate, err := toolState.register(chatTool.Function.Name, ResponsesChatToolIdentity{Type: dto.CustomType, Namespace: namespace, Name: name}, tool)
			if err != nil {
				return nil, err
			}
			if !duplicate {
				tools = append(tools, chatTool)
			}
			continue
		}
		if toolType != "function" {
			return nil, fmt.Errorf("tool type %q is not supported in chat compatibility mode", toolType)
		}

		chatTool, err := convertResponsesFunctionToolToChat(tool)
		if err != nil {
			return nil, err
		}
		duplicate, err := toolState.register(chatTool.Function.Name, ResponsesChatToolIdentity{
			Type: "function",
			Name: extractResponsesToolName(tool),
		}, chatTool)
		if err != nil {
			return nil, err
		}
		if !duplicate {
			tools = append(tools, chatTool)
		}
	}
	if namespaceSelection != nil && len(tools) == 0 {
		return nil, fmt.Errorf("tool_choice namespace %q has no compatible tools in chat compatibility mode", namespaceSelection.name)
	}
	return tools, nil
}

func validateChatToolChoice(toolChoice any, tools []dto.ToolCallRequest) error {
	choice, ok := toolChoice.(map[string]any)
	if !ok {
		return nil
	}
	function, ok := choice["function"].(map[string]any)
	if !ok {
		return nil
	}
	name := strings.TrimSpace(common.Interface2String(function["name"]))
	if name == "" {
		return fmt.Errorf("tool_choice function name is required")
	}
	for _, tool := range tools {
		if tool.Type == "function" && tool.Function.Name == name {
			return nil
		}
	}
	return fmt.Errorf("tool_choice function %q is not available in chat compatibility mode", name)
}

func marshalResponsesTools(tools []common.RawMessage) (common.RawMessage, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	raw, err := common.Marshal(tools)
	if err != nil {
		return nil, fmt.Errorf("failed to encode effective tools: %w", err)
	}
	return raw, nil
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
	var pendingReasoning *string
	lastMeaningfulWasAssistant := false

	for _, item := range items {
		role := strings.TrimSpace(common.Interface2String(item["role"]))
		itemType := strings.TrimSpace(common.Interface2String(item["type"]))
		if itemType == "additional_tools" {
			continue
		}

		if itemType == "reasoning" {
			reasoning, _, err := parseResponsesReasoningContent(item)
			if err != nil {
				return nil, err
			}
			if lastMeaningfulWasAssistant && len(messages) > 0 && messages[len(messages)-1].Role == "assistant" {
				appendMessageReasoningContent(&messages[len(messages)-1], reasoning)
			} else if pendingReasoning == nil {
				pendingReasoning = &reasoning
			} else {
				*pendingReasoning += reasoning
			}
			// A reasoning item before an assistant is pending; a reasoning item
			// after an assistant remains adjacent to that assistant so another
			// reasoning item can be merged without losing ordering.
			if !(lastMeaningfulWasAssistant && len(messages) > 0 && messages[len(messages)-1].Role == "assistant") {
				lastMeaningfulWasAssistant = false
			}
			continue
		}

		if role != "" || itemType == "message" {
			pendingUserParts = flushPendingUserMessage(&messages, pendingUserParts)

			if role == "" {
				role = "user"
			}
			if role != "user" && role != "assistant" && role != "system" && role != "developer" {
				return nil, fmt.Errorf("message role %q is not supported in chat compatibility mode", role)
			}
			if pendingReasoning != nil && role != "assistant" {
				// Chat Completions has no standalone reasoning item. If the next
				// Responses item is not an assistant message, discard the pending
				// reasoning rather than rejecting an otherwise representable history.
				pendingReasoning = nil
			}

			message := dto.Message{Role: role}
			if pendingReasoning != nil {
				appendMessageReasoningContent(&message, *pendingReasoning)
				pendingReasoning = nil
			}
			content := item["content"]
			if content == nil {
				message.SetStringContent("")
				messages = append(messages, message)
				lastMeaningfulWasAssistant = role == "assistant"
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
			lastMeaningfulWasAssistant = role == "assistant"
			continue
		}

		toolMessage, handled, err := convertResponsesToolInputItem(itemType, item, options)
		if handled {
			pendingUserParts = flushPendingUserMessage(&messages, pendingUserParts)
			if err != nil {
				return nil, err
			}
			if pendingReasoning != nil {
				if toolMessage.Role != "assistant" {
					pendingReasoning = nil
				} else {
					appendMessageReasoningContent(&toolMessage, *pendingReasoning)
					pendingReasoning = nil
				}
			}
			appendResponsesToolMessage(&messages, toolMessage)
			lastMeaningfulWasAssistant = toolMessage.Role == "assistant"
			continue
		}

		switch itemType {
		case "input_text", "input_image", "input_audio", "input_file", "input_video":
			if pendingReasoning != nil {
				pendingReasoning = nil
			}
			part, err := convertResponsesContentPartToChat(item)
			if err != nil {
				return nil, err
			}
			pendingUserParts = append(pendingUserParts, part)
			lastMeaningfulWasAssistant = false
		default:
			return nil, fmt.Errorf("input item type %q is not supported in chat compatibility mode", itemType)
		}
	}
	// A trailing reasoning item cannot be represented in Chat Completions.
	// Drop it after the complete input has been validated.

	pendingUserParts = flushPendingUserMessage(&messages, pendingUserParts)
	return messages, nil
}

func ResponsesRequestToChatCompletionsRequest(req *dto.OpenAIResponsesRequest) (*dto.GeneralOpenAIRequest, error) {
	return ResponsesRequestToChatCompletionsRequestWithOptions(req, ResponsesChatCompatibilityOptions{})
}

func ResponsesRequestToChatCompletionsRequestWithState(req *dto.OpenAIResponsesRequest, options ResponsesChatCompatibilityOptions) (*dto.GeneralOpenAIRequest, *ResponsesChatCompatibilityToolState, error) {
	toolState := newResponsesChatCompatibilityToolState()
	options.toolState = toolState
	chatRequest, err := ResponsesRequestToChatCompletionsRequestWithOptions(req, options)
	if err != nil {
		return nil, nil, err
	}
	return chatRequest, toolState, nil
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
	if req.Reasoning != nil {
		if rawMessageEnabled(req.Reasoning.Context) {
			return nil, fmt.Errorf("reasoning.context is not supported in chat compatibility mode")
		}
		if rawMessageEnabled(req.Reasoning.Mode) {
			return nil, fmt.Errorf("reasoning.mode is not supported in chat compatibility mode")
		}
		if len(req.Reasoning.Extra) > 0 {
			return nil, fmt.Errorf("reasoning extensions are not supported in chat compatibility mode")
		}
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

	toolChoice, namespaceSelection, err := convertResponsesToolChoiceToChat(req.ToolChoice, options)
	if err != nil {
		return nil, err
	}

	effectiveTools, err := effectiveResponsesTools(req)
	if err != nil {
		return nil, err
	}
	toolState := options.toolState
	if toolState == nil {
		toolState = newResponsesChatCompatibilityToolState()
	}
	options.toolState = toolState
	tools, err := convertResponsesToolsToChat(effectiveTools, options, namespaceSelection, toolState)
	if err != nil {
		return nil, err
	}
	out.Tools = tools
	if err := validateChatToolChoice(toolChoice, out.Tools); err != nil {
		return nil, err
	}
	toolChoice, err = normalizeChatToolChoiceForTools(toolChoice, out.Tools)
	if err != nil {
		return nil, err
	}
	out.ToolChoice = toolChoice

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
