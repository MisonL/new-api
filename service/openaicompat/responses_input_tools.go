package openaicompat

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

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

func responsesToolOutputToChatContent(itemType string, item map[string]any) (string, error) {
	output, exists := item["output"]
	if !exists {
		return "", fmt.Errorf("%s output is required", itemType)
	}
	switch value := output.(type) {
	case string:
		return value, nil
	case []any:
		textSegments := make([]string, 0, len(value))
		for index, content := range value {
			contentItem, ok := content.(map[string]any)
			if !ok {
				return "", fmt.Errorf("%s output[%d] must be an object", itemType, index)
			}
			switch strings.TrimSpace(common.Interface2String(contentItem["type"])) {
			case "input_text":
				text, ok := contentItem["text"].(string)
				if !ok {
					return "", fmt.Errorf("%s output[%d].text must be a string", itemType, index)
				}
				if strings.TrimSpace(text) != "" {
					textSegments = append(textSegments, text)
				}
			case "input_image", "encrypted_content":
				return "", fmt.Errorf("%s output[%d] content type %q is not supported in chat compatibility mode", itemType, index, contentItem["type"])
			default:
				return "", fmt.Errorf("%s output[%d] content type %q is not supported in chat compatibility mode", itemType, index, contentItem["type"])
			}
		}
		return strings.Join(textSegments, "\n"), nil
	default:
		return "", fmt.Errorf("%s output must be a string or an array of input_text items in chat compatibility mode", itemType)
	}
}

func convertResponsesContentPartToChat(part map[string]any) (dto.MediaContent, error) {
	partType := strings.TrimSpace(common.Interface2String(part["type"]))
	switch partType {
	case "input_text", "output_text", dto.ContentTypeText:
		return dto.MediaContent{Type: dto.ContentTypeText, Text: common.Interface2String(part["text"])}, nil
	case "input_image":
		image := part["image_url"]
		if image == nil {
			image = part["image"]
		}
		if image == nil {
			return dto.MediaContent{}, fmt.Errorf("input_image.image_url is required")
		}
		return dto.MediaContent{Type: dto.ContentTypeImageURL, ImageUrl: image}, nil
	case "input_audio":
		audio := part["input_audio"]
		if audio == nil {
			return dto.MediaContent{}, fmt.Errorf("input_audio.input_audio is required")
		}
		return dto.MediaContent{Type: dto.ContentTypeInputAudio, InputAudio: audio}, nil
	case "input_file":
		file := part["file"]
		if file == nil {
			file = part["file_url"]
		}
		if file == nil {
			return dto.MediaContent{}, fmt.Errorf("input_file.file is required")
		}
		return dto.MediaContent{Type: dto.ContentTypeFile, File: file}, nil
	case "input_video":
		video := part["video_url"]
		if video == nil {
			return dto.MediaContent{}, fmt.Errorf("input_video.video_url is required")
		}
		return dto.MediaContent{Type: dto.ContentTypeVideoUrl, VideoUrl: video}, nil
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

func parseResponsesToolSearchOutputTools(item map[string]any) ([]common.RawMessage, error) {
	callID := strings.TrimSpace(common.Interface2String(item["call_id"]))
	if callID == "" {
		return nil, fmt.Errorf("tool_search_output requires call_id")
	}
	if strings.TrimSpace(common.Interface2String(item["status"])) != "completed" {
		return nil, fmt.Errorf("tool_search_output status must be completed")
	}
	if strings.TrimSpace(common.Interface2String(item["execution"])) != "client" {
		return nil, fmt.Errorf("tool_search_output execution must be client")
	}
	tools, ok := item["tools"]
	if !ok {
		return nil, fmt.Errorf("tool_search_output requires tools")
	}
	toolsRaw, err := common.Marshal(tools)
	if err != nil {
		return nil, fmt.Errorf("failed to encode tool_search_output tools: %w", err)
	}
	if common.GetJsonType(toolsRaw) != "array" {
		return nil, fmt.Errorf("tool_search_output tools must be an array")
	}
	var parsedTools []common.RawMessage
	if err := common.Unmarshal(toolsRaw, &parsedTools); err != nil {
		return nil, fmt.Errorf("failed to parse tool_search_output tools: %w", err)
	}
	return parsedTools, nil
}

func collectResponsesToolSearchOutputTools(raw common.RawMessage) ([]common.RawMessage, error) {
	if common.GetJsonType(raw) != "array" {
		return nil, nil
	}
	var inputItems []map[string]any
	if err := common.Unmarshal(raw, &inputItems); err != nil {
		return nil, fmt.Errorf("failed to parse input for tool_search_output: %w", err)
	}
	var tools []common.RawMessage
	for index, item := range inputItems {
		if strings.TrimSpace(common.Interface2String(item["type"])) != "tool_search_output" {
			continue
		}
		outputTools, err := parseResponsesToolSearchOutputTools(item)
		if err != nil {
			return nil, fmt.Errorf("input[%d] %w", index, err)
		}
		tools = append(tools, outputTools...)
	}
	return tools, nil
}

func effectiveResponsesTools(req *dto.OpenAIResponsesRequest) (common.RawMessage, error) {
	tools, err := req.EffectiveTools()
	if err != nil {
		return nil, err
	}
	loadedTools, err := collectResponsesToolSearchOutputTools(req.Input)
	if err != nil {
		return nil, err
	}
	return marshalResponsesTools(append(tools, loadedTools...))
}

func convertResponsesToolInputItem(itemType string, item map[string]any, options ResponsesChatCompatibilityOptions) (dto.Message, bool, error) {
	switch itemType {
	case "function_call":
		callID := strings.TrimSpace(common.Interface2String(item["call_id"]))
		name := strings.TrimSpace(common.Interface2String(item["name"]))
		if callID == "" || name == "" {
			return dto.Message{}, true, fmt.Errorf("function_call requires call_id and name")
		}
		if namespace := strings.TrimSpace(common.Interface2String(item["namespace"])); namespace != "" {
			name = flattenResponsesNamespaceToolName(namespace, name)
		}
		arguments, err := stringifyResponsesOutput(item["arguments"])
		if err != nil {
			return dto.Message{}, true, fmt.Errorf("failed to encode function_call arguments: %w", err)
		}
		message := dto.Message{Role: "assistant", Content: ""}
		message.SetToolCalls([]dto.ToolCallRequest{{
			ID:   callID,
			Type: "function",
			Function: dto.FunctionRequest{
				Name:      name,
				Arguments: arguments,
			},
		}})
		return message, true, nil
	case "custom_tool_call":
		if !options.EnableCustomToolBridge {
			return dto.Message{}, true, fmt.Errorf("custom tool bridge is not enabled in chat compatibility mode")
		}
		callID := strings.TrimSpace(common.Interface2String(item["call_id"]))
		name := strings.TrimSpace(common.Interface2String(item["name"]))
		if callID == "" || name == "" {
			return dto.Message{}, true, fmt.Errorf("custom_tool_call requires call_id and name")
		}
		rawInput, ok := item["input"]
		if !ok {
			return dto.Message{}, true, fmt.Errorf("custom_tool_call input is required")
		}
		input, ok := rawInput.(string)
		if !ok {
			return dto.Message{}, true, fmt.Errorf("custom_tool_call input must be a string")
		}
		arguments, err := common.Marshal(map[string]any{"input": input})
		if err != nil {
			return dto.Message{}, true, fmt.Errorf("failed to encode custom_tool_call arguments: %w", err)
		}
		message := dto.Message{Role: "assistant", Content: ""}
		message.SetToolCalls([]dto.ToolCallRequest{{
			ID:   callID,
			Type: "function",
			Function: dto.FunctionRequest{
				Name:      flattenResponsesNamespacedCustomToolName(extractResponsesToolNamespace(item), name),
				Arguments: string(arguments),
			},
		}})
		return message, true, nil
	case "tool_search_call":
		if !options.EnableCustomToolBridge {
			return dto.Message{}, true, fmt.Errorf("custom tool bridge is not enabled in chat compatibility mode")
		}
		callID := strings.TrimSpace(common.Interface2String(item["call_id"]))
		if callID == "" {
			return dto.Message{}, true, fmt.Errorf("tool_search_call requires call_id")
		}
		arguments, err := stringifyResponsesOutput(item["arguments"])
		if err != nil {
			return dto.Message{}, true, fmt.Errorf("failed to encode tool_search_call arguments: %w", err)
		}
		if _, err := responsesToolSearchArguments(arguments); err != nil {
			return dto.Message{}, true, err
		}
		message := dto.Message{Role: "assistant", Content: ""}
		message.SetToolCalls([]dto.ToolCallRequest{{
			ID:   callID,
			Type: "function",
			Function: dto.FunctionRequest{
				Name:      responsesToolTypeToolSearch,
				Arguments: arguments,
			},
		}})
		return message, true, nil
	case "function_call_output", "custom_tool_call_output":
		if itemType == "custom_tool_call_output" && !options.EnableCustomToolBridge {
			return dto.Message{}, true, fmt.Errorf("custom tool bridge is not enabled in chat compatibility mode")
		}
		callID := strings.TrimSpace(common.Interface2String(item["call_id"]))
		if callID == "" {
			return dto.Message{}, true, fmt.Errorf("%s requires call_id", itemType)
		}
		output, err := responsesToolOutputToChatContent(itemType, item)
		if err != nil {
			return dto.Message{}, true, err
		}
		return dto.Message{Role: "tool", Content: output, ToolCallId: callID}, true, nil
	case "tool_search_output":
		if !options.EnableCustomToolBridge {
			return dto.Message{}, true, fmt.Errorf("custom tool bridge is not enabled in chat compatibility mode")
		}
		tools, err := parseResponsesToolSearchOutputTools(item)
		if err != nil {
			return dto.Message{}, true, err
		}
		toolsRaw, err := common.Marshal(tools)
		if err != nil {
			return dto.Message{}, true, fmt.Errorf("failed to encode tool_search_output tools: %w", err)
		}
		return dto.Message{Role: "tool", Content: string(toolsRaw), ToolCallId: strings.TrimSpace(common.Interface2String(item["call_id"]))}, true, nil
	default:
		return dto.Message{}, false, nil
	}
}

func appendResponsesToolMessage(messages *[]dto.Message, message dto.Message) {
	if message.Role != "assistant" {
		*messages = append(*messages, message)
		return
	}

	calls := message.ParseToolCalls()
	if len(calls) == 0 || len(*messages) == 0 {
		*messages = append(*messages, message)
		return
	}

	last := &(*messages)[len(*messages)-1]
	if last.Role != "assistant" {
		*messages = append(*messages, message)
		return
	}
	lastCalls := last.ParseToolCalls()
	if len(lastCalls) == 0 {
		*messages = append(*messages, message)
		return
	}
	last.SetToolCalls(append(lastCalls, calls...))
}
