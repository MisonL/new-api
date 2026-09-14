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

// parseResponsesReasoningContent decodes DeepSeek's documented Responses
// reasoning item shape. DeepSeek uses content[] with reasoning_text parts and
// merges the item into the adjacent assistant message. The returned boolean
// distinguishes an explicit empty reasoning item from an absent item so an
// empty reasoning_content value is not silently dropped on a tool turn.
func parseResponsesReasoningContent(item map[string]any) (string, bool, error) {
	if item == nil {
		return "", false, fmt.Errorf("reasoning item is required")
	}
	rawContent, ok := item["content"]
	if !ok || rawContent == nil {
		return "", false, fmt.Errorf("reasoning content is required")
	}
	parts, ok := rawContent.([]any)
	if !ok {
		return "", false, fmt.Errorf("reasoning content must be an array")
	}

	var builder strings.Builder
	for index, rawPart := range parts {
		part, ok := rawPart.(map[string]any)
		if !ok {
			return "", false, fmt.Errorf("reasoning content[%d] must be an object", index)
		}
		partType := strings.TrimSpace(common.Interface2String(part["type"]))
		if partType != "reasoning_text" {
			return "", false, fmt.Errorf("reasoning content[%d] type %q is not supported in chat compatibility mode", index, partType)
		}
		text, ok := part["text"].(string)
		if !ok {
			return "", false, fmt.Errorf("reasoning content[%d].text must be a string", index)
		}
		builder.WriteString(text)
	}
	return builder.String(), true, nil
}

func appendMessageReasoningContent(message *dto.Message, text string) {
	if message == nil {
		return
	}
	if message.ReasoningContent != nil {
		*message.ReasoningContent += text
		message.ThinkingContent = nil
		message.Reasoning = nil
		return
	}
	if message.ThinkingContent != nil {
		combined := *message.ThinkingContent + text
		message.ReasoningContent = &combined
		message.ThinkingContent = nil
		message.Reasoning = nil
		return
	}
	if message.Reasoning != nil {
		combined := *message.Reasoning + text
		message.ReasoningContent = &combined
		message.Reasoning = nil
		return
	}
	reasoning := text
	message.ReasoningContent = &reasoning
}

func messageHasReasoningContent(message *dto.Message) bool {
	return message != nil && (message.ReasoningContent != nil || message.ThinkingContent != nil || message.Reasoning != nil)
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
		text, ok := part["text"].(string)
		if !ok {
			return dto.MediaContent{}, fmt.Errorf("%s.text must be a string", partType)
		}
		return dto.MediaContent{Type: dto.ContentTypeText, Text: text}, nil
	case "input_image":
		image := part["image_url"]
		if image == nil {
			image = part["image"]
		}
		if image == nil {
			return dto.MediaContent{}, fmt.Errorf("input_image.image_url is required")
		}
		imageObject := map[string]any{}
		switch value := image.(type) {
		case string:
			imageObject["url"] = value
		case map[string]any:
			for key, item := range value {
				imageObject[key] = item
			}
		default:
			return dto.MediaContent{}, fmt.Errorf("input_image.image_url must be a string or object")
		}
		if strings.TrimSpace(common.Interface2String(imageObject["url"])) == "" {
			return dto.MediaContent{}, fmt.Errorf("input_image.image_url.url is required")
		}
		return dto.MediaContent{Type: dto.ContentTypeImageURL, ImageUrl: imageObject}, nil
	case "input_audio":
		audio := part["input_audio"]
		if audio == nil {
			return dto.MediaContent{}, fmt.Errorf("input_audio.input_audio is required")
		}
		audioObject, ok := audio.(map[string]any)
		if !ok {
			return dto.MediaContent{}, fmt.Errorf("input_audio.input_audio must be an object")
		}
		data, dataOK := audioObject["data"].(string)
		format, formatOK := audioObject["format"].(string)
		if !dataOK || strings.TrimSpace(data) == "" || !formatOK || strings.TrimSpace(format) == "" {
			return dto.MediaContent{}, fmt.Errorf("input_audio.input_audio.data and format are required")
		}
		return dto.MediaContent{Type: dto.ContentTypeInputAudio, InputAudio: map[string]any{"data": data, "format": format}}, nil
	case "input_file":
		file := part["file"]
		fileObject := map[string]any{}
		if file != nil {
			var ok bool
			fileObject, ok = file.(map[string]any)
			if !ok {
				return dto.MediaContent{}, fmt.Errorf("input_file.file must be an object")
			}
			fileObject = cloneStringAnyMap(fileObject)
		}
		for _, key := range []string{"file_id", "file_data", "file_url", "filename"} {
			if value, exists := part[key]; exists && value != nil {
				fileObject[key] = value
			}
		}
		if strings.TrimSpace(common.Interface2String(fileObject["file_id"])) == "" &&
			strings.TrimSpace(common.Interface2String(fileObject["file_data"])) == "" &&
			strings.TrimSpace(common.Interface2String(fileObject["file_url"])) == "" {
			return dto.MediaContent{}, fmt.Errorf("input_file requires file_id, file_data, or file_url")
		}
		return dto.MediaContent{Type: dto.ContentTypeFile, File: fileObject}, nil
	case "input_video":
		video := part["video_url"]
		if video == nil {
			return dto.MediaContent{}, fmt.Errorf("input_video.video_url is required")
		}
		videoURL := ""
		switch value := video.(type) {
		case string:
			videoURL = value
		case map[string]any:
			videoURL = common.Interface2String(value["url"])
		default:
			return dto.MediaContent{}, fmt.Errorf("input_video.video_url must be a string or object")
		}
		if strings.TrimSpace(videoURL) == "" {
			return dto.MediaContent{}, fmt.Errorf("input_video.video_url.url is required")
		}
		return dto.MediaContent{Type: dto.ContentTypeVideoUrl, VideoUrl: map[string]any{"url": videoURL}}, nil
	default:
		return dto.MediaContent{}, fmt.Errorf("input content type %q is not supported in chat compatibility mode", partType)
	}
}

func cloneStringAnyMap(value map[string]any) map[string]any {
	cloned := make(map[string]any, len(value))
	for key, item := range value {
		cloned[key] = item
	}
	return cloned
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

func effectiveResponsesTools(req *dto.OpenAIResponsesRequest) (common.RawMessage, error) {
	tools, err := req.EffectiveTools()
	if err != nil {
		return nil, err
	}
	return marshalResponsesTools(tools)
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
		} else {
			name = dto.NormalizeToolName(name)
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
	if message.StringContent() == "" {
		if messageHasReasoningContent(&message) {
			appendMessageReasoningContent(last, message.GetReasoningContent())
		}
		lastCalls := last.ParseToolCalls()
		last.SetToolCalls(append(lastCalls, calls...))
		return
	}
	lastCalls := last.ParseToolCalls()
	if len(lastCalls) == 0 {
		*messages = append(*messages, message)
		return
	}
	last.SetToolCalls(append(lastCalls, calls...))
}
