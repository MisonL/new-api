package service

import (
	"bytes"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

var responsesRequestShapeTypes = map[string]struct{}{
	"additional_tools":        {},
	"agent_message":           {},
	"compaction":              {},
	"compaction_trigger":      {},
	"computer_call":           {},
	"computer_call_output":    {},
	"custom_tool_call":        {},
	"custom_tool_call_output": {},
	"file_search_call":        {},
	"function_call":           {},
	"function_call_output":    {},
	"image_generation_call":   {},
	"item_reference":          {},
	"local_shell_call":        {},
	"local_shell_call_output": {},
	"message":                 {},
	"mcp_approval_request":    {},
	"mcp_approval_response":   {},
	"mcp_call":                {},
	"mcp_list_tools":          {},
	"reasoning":               {},
	"shell_call":              {},
	"shell_call_output":       {},
	"tool_search_call":        {},
	"tool_search_output":      {},
	"web_search_call":         {},
}

var responsesToolShapeTypes = map[string]struct{}{
	"computer_use_preview": {},
	"custom":               {},
	"file_search":          {},
	"function":             {},
	"image_generation":     {},
	"local_shell":          {},
	"mcp":                  {},
	"namespace":            {},
	"shell":                {},
	"tool_search":          {},
	"web_search":           {},
	"web_search_preview":   {},
}

func appendResponsesRequestShape(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, other map[string]interface{}) {
	if relayInfo == nil || other == nil {
		return
	}
	request := responsesRequestForShape(relayInfo.Request)
	if request == nil {
		return
	}

	shape := map[string]interface{}{
		"input_json_type":          common.GetJsonType(request.Input),
		"has_previous_response_id": strings.TrimSpace(request.PreviousResponseID) != "",
		"has_compaction_trigger":   request.HasCompactionTrigger(),
		"has_include":              hasJSONValue(request.Include),
		"has_tool_choice":          hasJSONValue(request.ToolChoice),
		"has_parallel_tool_calls":  hasJSONValue(request.ParallelToolCalls),
		"has_store":                hasJSONValue(request.Store),
	}
	if ctx != nil {
		if bodyBytes := common.GetContextKeyInt64(ctx, constant.ContextKeyRequestBodySize); bodyBytes > 0 {
			shape["body_bytes"] = bodyBytes
		}
	}
	if request.Stream != nil {
		shape["stream"] = *request.Stream
	}
	appendResponsesReasoningShape(request, shape)
	appendResponsesInputShape(request, shape)
	appendResponsesToolsShape(request, shape)
	other["responses_request_shape"] = shape
}

func responsesRequestForShape(request interface{}) *dto.OpenAIResponsesRequest {
	switch value := request.(type) {
	case *dto.OpenAIResponsesRequest:
		return value
	case *dto.OpenAIResponsesCompactionRequest:
		if value == nil {
			return nil
		}
		return value.ToResponsesRequest()
	default:
		return nil
	}
}

func appendResponsesReasoningShape(request *dto.OpenAIResponsesRequest, shape map[string]interface{}) {
	if request.Reasoning == nil {
		shape["has_reasoning"] = false
		return
	}
	shape["has_reasoning"] = true
	shape["has_reasoning_context"] = hasJSONValue(request.Reasoning.Context)
	shape["has_reasoning_mode"] = hasJSONValue(request.Reasoning.Mode)
}

func appendResponsesInputShape(request *dto.OpenAIResponsesRequest, shape map[string]interface{}) {
	if common.GetJsonType(request.Input) != "array" {
		return
	}
	var items []common.RawMessage
	if err := common.Unmarshal(request.Input, &items); err != nil {
		shape["input_parse_error"] = true
		return
	}
	shape["input_item_count"] = len(items)
	itemTypes := make(map[string]int)
	for _, rawItem := range items {
		itemType, rawTools := responsesShapeItem(rawItem)
		itemTypes[redactResponsesShapeType(itemType, responsesRequestShapeTypes)]++
		markResponsesFeature(itemType, shape)
		if itemType == "additional_tools" {
			shape["has_additional_tools"] = true
			if count, ok := responsesShapeArrayCount(rawTools); ok {
				shape["additional_tool_count"] = responsesShapeInt(shape["additional_tool_count"]) + count
			}
		}
	}
	if len(itemTypes) > 0 {
		shape["input_item_types"] = itemTypes
	}
}

func responsesShapeItem(raw common.RawMessage) (string, common.RawMessage) {
	if common.GetJsonType(raw) != "object" {
		return "", nil
	}
	var item struct {
		Type  string            `json:"type"`
		Tools common.RawMessage `json:"tools"`
	}
	if err := common.Unmarshal(raw, &item); err != nil {
		return "", nil
	}
	return strings.TrimSpace(item.Type), item.Tools
}

func appendResponsesToolsShape(request *dto.OpenAIResponsesRequest, shape map[string]interface{}) {
	if count, ok := responsesShapeArrayCount(request.Tools); ok {
		shape["top_level_tool_count"] = count
	}
	tools, err := request.EffectiveTools()
	if err != nil {
		shape["tool_parse_error"] = true
		return
	}
	shape["effective_tool_count"] = len(tools)
	toolTypes := make(map[string]int)
	formatTypes := make(map[string]int)
	grammarSyntaxes := make(map[string]int)
	for _, rawTool := range tools {
		toolType, formatType, syntax := responsesShapeTool(rawTool)
		redactedType := redactResponsesShapeType(toolType, responsesToolShapeTypes)
		toolTypes[redactedType]++
		markResponsesFeature(toolType, shape)
		if formatType != "" {
			formatTypes[redactResponsesFormatType(formatType)]++
		}
		if syntax != "" {
			grammarSyntaxes[redactResponsesGrammarSyntax(syntax)]++
		}
	}
	if len(toolTypes) > 0 {
		shape["effective_tool_types"] = toolTypes
	}
	if len(formatTypes) > 0 {
		shape["custom_tool_format_types"] = formatTypes
	}
	if len(grammarSyntaxes) > 0 {
		shape["custom_tool_grammar_syntaxes"] = grammarSyntaxes
	}
	markResponsesToolChoiceFeature(request.ToolChoice, shape)
}

func responsesShapeTool(raw common.RawMessage) (string, string, string) {
	if common.GetJsonType(raw) == "string" {
		return "custom", "", ""
	}
	if common.GetJsonType(raw) != "object" {
		return "", "", ""
	}
	var tool map[string]common.RawMessage
	if err := common.Unmarshal(raw, &tool); err != nil {
		return "", "", ""
	}
	toolType := responsesShapeString(tool["type"])
	if toolType != "custom" || common.GetJsonType(tool["format"]) != "object" {
		return toolType, "", ""
	}
	var format map[string]common.RawMessage
	if err := common.Unmarshal(tool["format"], &format); err != nil {
		return toolType, "", ""
	}
	return toolType, responsesShapeString(format["type"]), responsesShapeString(format["syntax"])
}

func markResponsesToolChoiceFeature(raw common.RawMessage, shape map[string]interface{}) {
	if common.GetJsonType(raw) != "object" {
		return
	}
	var choice map[string]common.RawMessage
	if err := common.Unmarshal(raw, &choice); err != nil {
		return
	}
	markResponsesFeature(responsesShapeString(choice["type"]), shape)
}

func markResponsesFeature(itemType string, shape map[string]interface{}) {
	switch itemType {
	case "function", "function_call", "function_call_output":
		shape["has_function_tools"] = true
	case "custom", "custom_tool_call", "custom_tool_call_output":
		shape["has_custom_tools"] = true
	case "namespace":
		shape["has_namespace_tools"] = true
	case "tool_search", "tool_search_call", "tool_search_output":
		shape["has_tool_search"] = true
	}
}

func responsesShapeArrayCount(raw common.RawMessage) (int, bool) {
	if common.GetJsonType(raw) != "array" {
		return 0, false
	}
	var values []common.RawMessage
	if err := common.Unmarshal(raw, &values); err != nil {
		return 0, false
	}
	return len(values), true
}

func responsesShapeString(raw common.RawMessage) string {
	var value string
	if err := common.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func responsesShapeInt(value interface{}) int {
	result, _ := value.(int)
	return result
}

func redactResponsesShapeType(value string, allowed map[string]struct{}) string {
	if _, ok := allowed[value]; ok {
		return value
	}
	return "other"
}

func redactResponsesFormatType(value string) string {
	switch value {
	case "grammar", "text":
		return value
	default:
		return "other"
	}
}

func redactResponsesGrammarSyntax(value string) string {
	switch value {
	case "lark", "regex":
		return value
	default:
		return "other"
	}
}

func hasJSONValue(raw common.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}
