package dto

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
)

type responsesInputTokenMeta struct {
	texts []string
	files []*types.FileMeta
}

func responsesInputTokenCountMeta(input json.RawMessage) ([]string, []*types.FileMeta) {
	trimmed := bytes.TrimSpace(input)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}

	meta := responsesInputTokenMeta{}
	if common.GetJsonType(trimmed) == "string" {
		meta.appendText(common.JsonRawMessageToString(trimmed))
		return meta.texts, meta.files
	}
	if common.GetJsonType(trimmed) != "array" {
		return nil, nil
	}

	var items []any
	if err := common.Unmarshal(trimmed, &items); err != nil {
		return nil, nil
	}
	for _, item := range items {
		meta.appendInputValue(item)
	}
	return meta.texts, meta.files
}

func (m *responsesInputTokenMeta) appendInputValue(value any) {
	switch value := value.(type) {
	case string:
		m.appendText(value)
	case []any:
		for _, item := range value {
			m.appendInputValue(item)
		}
	case map[string]any:
		m.appendItem(value)
	}
}

func (m *responsesInputTokenMeta) appendItem(item map[string]any) {
	itemType := strings.TrimSpace(common.Interface2String(item["type"]))
	switch itemType {
	case "message":
		m.appendContent(item["content"])
	case "agent_message":
		m.appendText(common.Interface2String(item["author"]))
		m.appendText(common.Interface2String(item["recipient"]))
		m.appendContent(item["content"])
	case "reasoning":
		m.appendReasoning(item)
	case "input_text", "output_text", "text", "reasoning_text":
		m.appendText(common.Interface2String(item["text"]))
	case "refusal":
		m.appendText(common.Interface2String(item["refusal"]))
	case "input_image":
		m.appendImage(item)
	case "input_audio":
		m.appendAudio(item)
	case "input_file":
		m.appendFile(item)
	case "function_call":
		m.appendToolIdentity(item)
		m.appendJSON(item["arguments"])
	case "custom_tool_call":
		m.appendToolIdentity(item)
		m.appendJSON(item["input"])
	case "function_call_output", "custom_tool_call_output":
		m.appendToolOutput(item["output"])
	case "tool_search_call":
		m.appendJSON(item["arguments"])
	case "local_shell_call":
		m.appendLocalShellAction(item["action"])
	case "shell_call":
		m.appendShellAction(item["action"])
	case "computer_call":
		m.appendComputerAction(item["action"])
		m.appendComputerActions(item["actions"])
	case "web_search_call":
		m.appendWebSearchAction(item["action"])
	case "local_shell_call_output", "apply_patch_call_output":
		m.appendTextValue(item["output"])
	case "shell_call_output":
		m.appendShellOutput(item["output"])
	case "computer_call_output":
		m.appendComputerOutput(item["output"])
	case "apply_patch_call":
		m.appendApplyPatchOperation(item["operation"])
		m.appendTextValue(item["patch"])
	case "code_interpreter_call":
		m.appendTextValue(item["code"])
		m.appendCodeInterpreterOutputs(item["outputs"])
	case "mcp_approval_request":
		m.appendToolIdentity(item)
		m.appendJSON(item["arguments"])
	case "mcp_approval_response":
		m.appendTextValue(item["reason"])
	case "mcp_call":
		m.appendText(common.Interface2String(item["server_label"]))
		m.appendToolIdentity(item)
		m.appendJSON(item["arguments"])
		m.appendTextValue(item["output"])
		m.appendTextValue(item["error"])
	case "mcp_list_tools":
		m.appendJSON(item["tools"])
		m.appendTextValue(item["error"])
	case "file_search_call":
		m.appendTextValues(item["queries"])
		m.appendFileSearchResults(item["results"])
	case "multi_agent_call":
		m.appendTextValue(item["action"])
		m.appendToolOutput(item["output"])
	case "multi_agent_call_output":
		m.appendTextValue(item["action"])
		m.appendToolOutput(item["output"])
	case "program":
		m.appendTextValue(item["code"])
	case "program_output":
		m.appendTextValue(item["result"])
	case "image_generation_call":
		m.appendTextValue(item["revised_prompt"])
		m.appendGeneratedImage(item["result"])
	default:
		if content, exists := item["content"]; exists {
			m.appendContent(content)
		}
	}
}

func (m *responsesInputTokenMeta) appendContent(content any) {
	switch content := content.(type) {
	case string:
		m.appendText(content)
	case []any:
		for _, part := range content {
			m.appendInputValue(part)
		}
	case map[string]any:
		m.appendItem(content)
	}
}

func (m *responsesInputTokenMeta) appendReasoning(item map[string]any) {
	m.appendReasoningSummary(item["summary"])
	if content, exists := item["content"]; exists {
		m.appendContent(content)
	}
}

func (m *responsesInputTokenMeta) appendReasoningSummary(summary any) {
	switch summary := summary.(type) {
	case string:
		m.appendText(summary)
	case []any:
		for _, partAny := range summary {
			part, ok := partAny.(map[string]any)
			if !ok || strings.TrimSpace(common.Interface2String(part["type"])) != "summary_text" {
				continue
			}
			m.appendText(common.Interface2String(part["text"]))
		}
	}
}

func (m *responsesInputTokenMeta) appendToolOutput(output any) {
	switch output := output.(type) {
	case string:
		m.appendText(output)
	case []any:
		for _, part := range output {
			m.appendToolOutputPart(part)
		}
	case map[string]any:
		m.appendToolOutputPart(output)
	}
}

func (m *responsesInputTokenMeta) appendToolOutputPart(part any) {
	item, ok := part.(map[string]any)
	if !ok {
		return
	}
	switch strings.TrimSpace(common.Interface2String(item["type"])) {
	case "input_text", "output_text", "text", "refusal", "input_image", "input_audio", "input_file":
		m.appendItem(item)
	case "logs":
		m.appendTextValue(item["logs"])
	case "image":
		m.appendImageURL(common.Interface2String(item["url"]), "")
	case "computer_screenshot":
		imageURL, detail := responsesInputURLAndDetail(item["image_url"])
		m.appendImageURL(imageURL, detail)
	case "encrypted_content":
		return
	}
}

func (m *responsesInputTokenMeta) appendImage(item map[string]any) {
	imageURL, detail := responsesInputURLAndDetail(item["image_url"])
	m.appendImageURL(imageURL, detail)
}

func (m *responsesInputTokenMeta) appendImageURL(imageURL string, detail string) {
	if imageURL == "" {
		return
	}
	m.files = append(m.files, &types.FileMeta{
		FileType: types.FileTypeImage,
		Source:   types.NewFileSourceFromData(imageURL, ""),
		Detail:   detail,
	})
}

func (m *responsesInputTokenMeta) appendAudio(item map[string]any) {
	audioData, mimeType := responsesInputAudioSource(item)
	if audioData == "" {
		return
	}
	m.files = append(m.files, &types.FileMeta{
		FileType: types.FileTypeAudio,
		Source:   types.NewFileSourceFromData(audioData, mimeType),
	})
}

func (m *responsesInputTokenMeta) appendFile(item map[string]any) {
	fileData := responsesInputFileSource(item)
	if fileData != "" {
		m.files = append(m.files, &types.FileMeta{
			FileType: types.FileTypeFile,
			Source:   types.NewFileSourceFromData(fileData, ""),
		})
		return
	}
	if responsesInputFileID(item) == "" {
		return
	}
	// File IDs are valid upstream references, but the gateway has no authorized
	// source to fetch their bytes. Preserve the existing generic-file estimate
	// without treating the opaque ID as inline file data.
	m.files = append(m.files, &types.FileMeta{
		FileType: types.FileTypeFile,
	})
}

func (m *responsesInputTokenMeta) appendToolIdentity(item map[string]any) {
	m.appendText(common.Interface2String(item["namespace"]))
	m.appendText(common.Interface2String(item["name"]))
}

func (m *responsesInputTokenMeta) appendLocalShellAction(action any) {
	item, ok := action.(map[string]any)
	if !ok {
		return
	}
	m.appendTextValues(item["command"])
}

func (m *responsesInputTokenMeta) appendShellAction(action any) {
	item, ok := action.(map[string]any)
	if !ok {
		return
	}
	m.appendTextValues(item["commands"])
}

func (m *responsesInputTokenMeta) appendComputerAction(action any) {
	item, ok := action.(map[string]any)
	if !ok {
		return
	}
	m.appendTextValue(item["text"])
	m.appendTextValue(item["query"])
}

func (m *responsesInputTokenMeta) appendComputerActions(actions any) {
	items, ok := actions.([]any)
	if !ok {
		return
	}
	for _, action := range items {
		m.appendComputerAction(action)
	}
}

func (m *responsesInputTokenMeta) appendWebSearchAction(action any) {
	item, ok := action.(map[string]any)
	if !ok {
		return
	}
	m.appendTextValue(item["query"])
	m.appendTextValues(item["queries"])
	m.appendTextValue(item["pattern"])
}

func (m *responsesInputTokenMeta) appendShellOutput(output any) {
	items, ok := output.([]any)
	if !ok {
		return
	}
	for _, outputItem := range items {
		item, ok := outputItem.(map[string]any)
		if !ok {
			continue
		}
		m.appendTextValue(item["stdout"])
		m.appendTextValue(item["stderr"])
	}
}

func (m *responsesInputTokenMeta) appendComputerOutput(output any) {
	item, ok := output.(map[string]any)
	if !ok {
		return
	}
	imageURL, detail := responsesInputURLAndDetail(item["image_url"])
	m.appendImageURL(imageURL, detail)
}

func (m *responsesInputTokenMeta) appendApplyPatchOperation(operation any) {
	item, ok := operation.(map[string]any)
	if !ok {
		return
	}
	m.appendTextValue(item["path"])
	m.appendTextValue(item["diff"])
}

func (m *responsesInputTokenMeta) appendCodeInterpreterOutputs(outputs any) {
	items, ok := outputs.([]any)
	if !ok {
		return
	}
	for _, output := range items {
		m.appendToolOutputPart(output)
	}
}

func (m *responsesInputTokenMeta) appendFileSearchResults(results any) {
	items, ok := results.([]any)
	if !ok {
		return
	}
	for _, result := range items {
		item, ok := result.(map[string]any)
		if !ok {
			continue
		}
		m.appendTextValue(item["filename"])
		m.appendTextValue(item["text"])
	}
}

func (m *responsesInputTokenMeta) appendGeneratedImage(result any) {
	imageData := common.Interface2String(result)
	if imageData == "" {
		return
	}
	m.files = append(m.files, &types.FileMeta{
		FileType: types.FileTypeImage,
		Source:   types.NewFileSourceFromData(imageData, ""),
	})
}

func (m *responsesInputTokenMeta) appendJSON(value any) {
	if value == nil {
		return
	}
	if stringValue, ok := value.(string); ok {
		m.appendText(stringValue)
		return
	}
	raw, err := common.Marshal(value)
	if err != nil {
		return
	}
	m.appendText(string(raw))
}

func (m *responsesInputTokenMeta) appendTextValue(value any) {
	stringValue, ok := value.(string)
	if !ok {
		return
	}
	m.appendText(stringValue)
}

func (m *responsesInputTokenMeta) appendTextValues(value any) {
	values, ok := value.([]any)
	if !ok {
		return
	}
	for _, value := range values {
		m.appendTextValue(value)
	}
}

func (m *responsesInputTokenMeta) appendText(value string) {
	if strings.TrimSpace(value) != "" {
		m.texts = append(m.texts, value)
	}
}

func responsesInputAudioSource(item map[string]any) (string, string) {
	if audio, ok := item["input_audio"].(map[string]any); ok {
		audioData := common.Interface2String(audio["data"])
		if audioData == "" {
			return "", ""
		}
		mimeType := ""
		if audioFormat := common.Interface2String(audio["format"]); audioFormat != "" {
			mimeType = "audio/" + audioFormat
		}
		return audioData, mimeType
	}
	return common.Interface2String(item["audio_url"]), ""
}

func responsesInputFileSource(item map[string]any) string {
	for _, field := range []string{"file_url", "file_data"} {
		if value := common.Interface2String(item[field]); value != "" {
			return value
		}
	}
	file, exists := item["file"]
	if !exists {
		return ""
	}
	if fileObject, ok := file.(map[string]any); ok {
		for _, field := range []string{"file_url", "file_data", "url"} {
			if value := common.Interface2String(fileObject[field]); value != "" {
				return value
			}
		}
		return ""
	}
	return common.Interface2String(file)
}

func responsesInputFileID(item map[string]any) string {
	if fileID := common.Interface2String(item["file_id"]); fileID != "" {
		return fileID
	}
	fileObject, ok := item["file"].(map[string]any)
	if !ok {
		return ""
	}
	return common.Interface2String(fileObject["file_id"])
}
