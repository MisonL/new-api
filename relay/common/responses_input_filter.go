package common

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	basecommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

var ErrResponsesEncryptedReasoningOnlyInput = errors.New("responses encrypted reasoning context is unsupported by this OpenAI-compatible channel and no other input items remain")

var responsesNamespaceParameterPattern = regexp.MustCompile(`^input\[(\d+)\]\.namespace$`)
var responsesArgumentsParameterPattern = regexp.MustCompile(`^input\[(\d+)\]\.arguments$`)
var responsesEncryptedContentParameterPattern = regexp.MustCompile(`^input\[(\d+)\](?:\.(content|output)\[(\d+)\])?\.encrypted_content$`)
var responsesNamespaceTextPattern = regexp.MustCompile(`input\[(\d+)\]\.namespace`)
var responsesArgumentsTextPattern = regexp.MustCompile(`input\[(\d+)\]\.arguments`)
var responsesEncryptedContentObjectParamTextPattern = regexp.MustCompile(`(?i)\[ObjectParam\]\s*\[\s*input\[(\d+)\](?:\.(content|output)\[(\d+)\])?\.encrypted_content\s*\]\s*\[(?:missing_required_parameter|unsupported_parameter|invalid_type)\]`)

type ResponsesInputStripResult struct {
	Input                   json.RawMessage
	EncryptedReasoningCount int
	RemainingCount          int
}

type ResponsesArgumentsNormalizeResult struct {
	NormalizedCount  int
	TargetNormalized bool
}

type responsesEncryptedContentInputPath struct {
	inputIndex  int
	nestedField string
	nestedIndex int
}

func (r ResponsesInputStripResult) RemovedCount() int {
	return r.EncryptedReasoningCount
}

func StripEncryptedReasoningFromResponsesRequest(request dto.OpenAIResponsesRequest) (dto.OpenAIResponsesRequest, ResponsesInputStripResult, error) {
	result, err := StripEncryptedReasoningFromResponsesInput(request.Input)
	if err != nil {
		return request, result, err
	}
	if result.RemovedCount() == 0 {
		return request, result, nil
	}
	if result.RemainingCount == 0 {
		return request, result, ErrResponsesEncryptedReasoningOnlyInput
	}
	request.Input = result.Input
	return request, result, nil
}

func StripEncryptedReasoningFromResponsesInput(input json.RawMessage) (ResponsesInputStripResult, error) {
	result := ResponsesInputStripResult{
		Input: input,
	}
	trimmedInput := bytes.TrimSpace(input)
	if len(trimmedInput) == 0 || trimmedInput[0] != '[' {
		return result, nil
	}

	var items []json.RawMessage
	if err := basecommon.Unmarshal(input, &items); err != nil {
		return result, err
	}

	filtered := make([]json.RawMessage, 0, len(items))
	for _, rawItem := range items {
		var item map[string]json.RawMessage
		if err := basecommon.Unmarshal(rawItem, &item); err != nil {
			filtered = append(filtered, rawItem)
			continue
		}
		if responsesItemType(item) == "reasoning" && responsesItemHasEncryptedContent(item) {
			result.EncryptedReasoningCount++
			continue
		}
		filtered = append(filtered, rawItem)
	}
	if result.RemovedCount() == 0 {
		result.RemainingCount = len(items)
		return result, nil
	}

	raw, err := basecommon.Marshal(filtered)
	if err != nil {
		return result, err
	}
	result.Input = json.RawMessage(raw)
	result.RemainingCount = len(filtered)
	return result, nil
}

func StripResponsesInputEncryptedContentItem(request dto.OpenAIResponsesRequest, param string, message string) (dto.OpenAIResponsesRequest, bool, error) {
	path, ok := responsesEncryptedContentInputPathFromError(param, message)
	if !ok {
		return request, false, nil
	}

	trimmedInput := bytes.TrimSpace(request.Input)
	if len(trimmedInput) == 0 || trimmedInput[0] != '[' {
		return request, false, nil
	}
	var items []json.RawMessage
	if err := basecommon.Unmarshal(request.Input, &items); err != nil {
		return request, false, err
	}
	if path.inputIndex < 0 || path.inputIndex >= len(items) {
		return request, false, nil
	}

	if path.nestedField == "" {
		strippedItems, removed, err := stripTopLevelResponsesEncryptedReasoning(items, path.inputIndex)
		if err != nil {
			return request, false, err
		}
		if !removed {
			return request, false, nil
		}
		return withResponsesInput(request, strippedItems)
	}

	strippedItem, removed, err := stripNestedResponsesEncryptedContent(items[path.inputIndex], path)
	if err != nil {
		return request, false, err
	}
	if !removed {
		return request, false, nil
	}
	items[path.inputIndex] = strippedItem
	return withResponsesInput(request, items)
}

func withResponsesInput(request dto.OpenAIResponsesRequest, items []json.RawMessage) (dto.OpenAIResponsesRequest, bool, error) {
	encodedInput, err := basecommon.Marshal(items)
	if err != nil {
		return request, false, fmt.Errorf("marshal responses input without encrypted context content: %w", err)
	}
	request.Input = encodedInput
	return request, true, nil
}

func stripTopLevelResponsesEncryptedReasoning(items []json.RawMessage, inputIndex int) ([]json.RawMessage, bool, error) {
	if len(items) == 1 {
		return items, false, nil
	}
	var item map[string]json.RawMessage
	if err := basecommon.Unmarshal(items[inputIndex], &item); err != nil {
		return items, false, err
	}
	if !responsesItemIsEncryptedReasoning(item) || !responsesItemHasEncryptedContent(item) {
		return items, false, nil
	}
	return append(items[:inputIndex], items[inputIndex+1:]...), true, nil
}

func stripNestedResponsesEncryptedContent(rawItem json.RawMessage, path responsesEncryptedContentInputPath) (json.RawMessage, bool, error) {
	var item map[string]json.RawMessage
	if err := basecommon.Unmarshal(rawItem, &item); err != nil {
		return rawItem, false, err
	}
	if !responsesItemSupportsEncryptedContent(item, path.nestedField) {
		return rawItem, false, nil
	}
	contentRaw, exists := item[path.nestedField]
	if !exists {
		return rawItem, false, nil
	}
	var content []json.RawMessage
	if err := basecommon.Unmarshal(contentRaw, &content); err != nil {
		return rawItem, false, err
	}
	if path.nestedIndex < 0 || path.nestedIndex >= len(content) || len(content) == 1 {
		return rawItem, false, nil
	}
	var contentItem map[string]json.RawMessage
	if err := basecommon.Unmarshal(content[path.nestedIndex], &contentItem); err != nil {
		return rawItem, false, err
	}
	if responsesItemType(contentItem) != "encrypted_content" || !responsesItemHasEncryptedContent(contentItem) {
		return rawItem, false, nil
	}
	content = append(content[:path.nestedIndex], content[path.nestedIndex+1:]...)
	encodedContent, err := basecommon.Marshal(content)
	if err != nil {
		return rawItem, false, fmt.Errorf("marshal responses nested content without encrypted context item: %w", err)
	}
	item[path.nestedField] = encodedContent
	encodedItem, err := basecommon.Marshal(item)
	if err != nil {
		return rawItem, false, fmt.Errorf("marshal responses input item without encrypted context content: %w", err)
	}
	return encodedItem, true, nil
}

func HasResponsesInputEncryptedContentPath(param string, message string) bool {
	_, ok := responsesEncryptedContentInputPathFromError(param, message)
	return ok
}

func responsesEncryptedContentInputPathFromError(param string, message string) (responsesEncryptedContentInputPath, bool) {
	trimmedParam := strings.TrimSpace(param)
	matches := responsesEncryptedContentParameterPattern.FindStringSubmatch(trimmedParam)
	if len(matches) == 0 && trimmedParam == "" {
		matches = responsesEncryptedContentObjectParamTextPattern.FindStringSubmatch(message)
	}
	if len(matches) != 4 {
		return responsesEncryptedContentInputPath{}, false
	}
	inputIndex, err := strconv.Atoi(matches[1])
	if err != nil {
		return responsesEncryptedContentInputPath{}, false
	}
	if matches[2] == "" {
		return responsesEncryptedContentInputPath{inputIndex: inputIndex}, true
	}
	nestedIndex, err := strconv.Atoi(matches[3])
	if err != nil {
		return responsesEncryptedContentInputPath{}, false
	}
	return responsesEncryptedContentInputPath{
		inputIndex:  inputIndex,
		nestedField: matches[2],
		nestedIndex: nestedIndex,
	}, true
}

func responsesItemType(item map[string]json.RawMessage) string {
	rawType := item["type"]
	if len(rawType) == 0 {
		return ""
	}
	var itemType string
	if err := basecommon.Unmarshal(rawType, &itemType); err != nil {
		return ""
	}
	return itemType
}

func responsesItemHasEncryptedContent(item map[string]json.RawMessage) bool {
	raw := bytes.TrimSpace(item["encrypted_content"])
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var encryptedContent string
	if err := basecommon.Unmarshal(raw, &encryptedContent); err == nil {
		return encryptedContent != ""
	}
	return false
}

func responsesItemIsEncryptedReasoning(item map[string]json.RawMessage) bool {
	return responsesItemType(item) == "reasoning"
}

func responsesItemSupportsEncryptedContent(item map[string]json.RawMessage, nestedField string) bool {
	switch nestedField {
	case "content":
		return responsesItemType(item) == "agent_message"
	case "output":
		switch responsesItemType(item) {
		case "function_call_output", "custom_tool_call_output":
			return true
		}
	}
	return false
}

func ResponsesRequestHasNamespaceTools(request *dto.OpenAIResponsesRequest) (bool, error) {
	if request == nil {
		return false, nil
	}
	tools, err := request.EffectiveTools()
	if err != nil {
		return false, err
	}
	for _, rawTool := range tools {
		var tool map[string]json.RawMessage
		if err := basecommon.Unmarshal(rawTool, &tool); err != nil {
			continue
		}
		if responsesItemType(tool) == "namespace" {
			return true, nil
		}
	}
	return false, nil
}

func StripNamespaceFromResponsesInputHistory(request dto.OpenAIResponsesRequest, itemIndex int) (dto.OpenAIResponsesRequest, bool, error) {
	trimmedInput := bytes.TrimSpace(request.Input)
	if itemIndex < 0 || len(trimmedInput) == 0 || trimmedInput[0] != '[' {
		return request, false, nil
	}

	var items []json.RawMessage
	if err := basecommon.Unmarshal(request.Input, &items); err != nil {
		return request, false, err
	}
	if itemIndex >= len(items) {
		return request, false, nil
	}
	var item map[string]json.RawMessage
	if err := basecommon.Unmarshal(items[itemIndex], &item); err != nil {
		return request, false, err
	}
	if _, exists := item["namespace"]; !exists {
		return request, false, nil
	}
	delete(item, "namespace")
	encodedItem, err := basecommon.Marshal(item)
	if err != nil {
		return request, false, fmt.Errorf("marshal responses input item %d without namespace: %w", itemIndex, err)
	}
	items[itemIndex] = encodedItem
	encodedInput, err := basecommon.Marshal(items)
	if err != nil {
		return request, false, fmt.Errorf("marshal responses input without namespace history: %w", err)
	}
	request.Input = encodedInput
	return request, true, nil
}

func ResponsesNamespaceHistoryIndex(param string) (int, bool) {
	matches := responsesNamespaceParameterPattern.FindStringSubmatch(strings.TrimSpace(param))
	if len(matches) != 2 {
		return 0, false
	}
	index, err := strconv.Atoi(matches[1])
	return index, err == nil
}

func ResponsesNamespaceHistoryIndexFromError(param string, message string) (int, bool) {
	if index, ok := ResponsesNamespaceHistoryIndex(param); ok {
		return index, true
	}
	return responsesHistoryIndexFromText(responsesNamespaceTextPattern, message)
}

func ResponsesArgumentsHistoryIndex(param string) (int, bool) {
	matches := responsesArgumentsParameterPattern.FindStringSubmatch(strings.TrimSpace(param))
	if len(matches) != 2 {
		return 0, false
	}
	index, err := strconv.Atoi(matches[1])
	return index, err == nil
}

func ResponsesArgumentsHistoryIndexFromError(param string, message string) (int, bool) {
	if index, ok := ResponsesArgumentsHistoryIndex(param); ok {
		return index, true
	}
	return responsesHistoryIndexFromText(responsesArgumentsTextPattern, message)
}

func responsesHistoryIndexFromText(pattern *regexp.Regexp, text string) (int, bool) {
	matches := pattern.FindStringSubmatch(text)
	if len(matches) != 2 {
		return 0, false
	}
	index, err := strconv.Atoi(matches[1])
	return index, err == nil
}

func NormalizeStringObjectArgumentsFromResponsesInputHistory(request dto.OpenAIResponsesRequest, targetIndex int) (dto.OpenAIResponsesRequest, ResponsesArgumentsNormalizeResult, error) {
	return transformResponsesInputHistoryArguments(request, targetIndex, "normalized", normalizeResponsesStringObjectArguments)
}

func StringifyObjectArgumentsFromResponsesInputHistory(request dto.OpenAIResponsesRequest, targetIndex int) (dto.OpenAIResponsesRequest, ResponsesArgumentsNormalizeResult, error) {
	return transformResponsesInputHistoryArguments(request, targetIndex, "stringified", stringifyResponsesObjectArguments)
}

func transformResponsesInputHistoryArguments(request dto.OpenAIResponsesRequest, targetIndex int, action string, transform func(json.RawMessage) (json.RawMessage, bool)) (dto.OpenAIResponsesRequest, ResponsesArgumentsNormalizeResult, error) {
	result := ResponsesArgumentsNormalizeResult{}
	trimmedInput := bytes.TrimSpace(request.Input)
	if targetIndex < 0 || len(trimmedInput) == 0 || trimmedInput[0] != '[' {
		return request, result, nil
	}

	var items []json.RawMessage
	if err := basecommon.Unmarshal(request.Input, &items); err != nil {
		return request, result, err
	}
	if targetIndex >= len(items) {
		return request, result, nil
	}

	for index, rawItem := range items {
		var item map[string]json.RawMessage
		if err := basecommon.Unmarshal(rawItem, &item); err != nil {
			continue
		}
		rawArguments, exists := item["arguments"]
		if !exists {
			continue
		}
		transformedArguments, ok := transform(rawArguments)
		if !ok {
			continue
		}
		item["arguments"] = transformedArguments
		encodedItem, err := basecommon.Marshal(item)
		if err != nil {
			return request, result, fmt.Errorf("marshal responses input item %d with %s arguments: %w", index, action, err)
		}
		items[index] = encodedItem
		result.NormalizedCount++
		if index == targetIndex {
			result.TargetNormalized = true
		}
	}
	if result.NormalizedCount == 0 {
		return request, result, nil
	}
	encodedInput, err := basecommon.Marshal(items)
	if err != nil {
		return request, result, fmt.Errorf("marshal responses input with %s arguments: %w", action, err)
	}
	request.Input = encodedInput
	return request, result, nil
}

func normalizeResponsesStringObjectArguments(raw json.RawMessage) (json.RawMessage, bool) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, false
	}
	var argumentsString string
	if err := basecommon.Unmarshal(raw, &argumentsString); err != nil {
		return nil, false
	}
	trimmedArguments := bytes.TrimSpace([]byte(argumentsString))
	if len(trimmedArguments) == 0 {
		trimmedArguments = []byte(`{}`)
	}
	if trimmedArguments[0] != '{' {
		return nil, false
	}
	var object map[string]json.RawMessage
	if err := basecommon.Unmarshal(trimmedArguments, &object); err != nil {
		return nil, false
	}
	encoded, err := basecommon.Marshal(object)
	if err != nil {
		return nil, false
	}
	return encoded, true
}

func stringifyResponsesObjectArguments(raw json.RawMessage) (json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, false
	}
	var object map[string]json.RawMessage
	if err := basecommon.Unmarshal(trimmed, &object); err != nil {
		return nil, false
	}
	encodedObject, err := basecommon.Marshal(object)
	if err != nil {
		return nil, false
	}
	encodedString, err := basecommon.Marshal(string(encodedObject))
	if err != nil {
		return nil, false
	}
	return encodedString, true
}
