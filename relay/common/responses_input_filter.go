package common

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	basecommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

var ErrResponsesEncryptedReasoningOnlyInput = errors.New("responses encrypted reasoning context is unsupported by this OpenAI-compatible channel and no other input items remain")

var responsesNamespaceParameterPattern = regexp.MustCompile(`^input\[\d+\]\.namespace$`)

type ResponsesInputStripResult struct {
	Input                   json.RawMessage
	EncryptedReasoningCount int
	RemainingCount          int
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

func ResponsesRequestHasNamespaceTools(request *dto.OpenAIResponsesRequest) (bool, error) {
	if request == nil {
		return false, nil
	}
	trimmedTools := bytes.TrimSpace(request.Tools)
	if len(trimmedTools) == 0 || trimmedTools[0] != '[' {
		return false, nil
	}
	var tools []json.RawMessage
	if err := basecommon.Unmarshal(request.Tools, &tools); err != nil {
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

func StripNamespaceFromResponsesInputHistory(request dto.OpenAIResponsesRequest) (dto.OpenAIResponsesRequest, int, error) {
	trimmedInput := bytes.TrimSpace(request.Input)
	if len(trimmedInput) == 0 || trimmedInput[0] != '[' {
		return request, 0, nil
	}

	var items []json.RawMessage
	if err := basecommon.Unmarshal(request.Input, &items); err != nil {
		return request, 0, err
	}
	removedCount := 0
	for index, rawItem := range items {
		var item map[string]json.RawMessage
		if err := basecommon.Unmarshal(rawItem, &item); err != nil {
			continue
		}
		if _, exists := item["namespace"]; !exists {
			continue
		}
		delete(item, "namespace")
		encodedItem, err := basecommon.Marshal(item)
		if err != nil {
			return request, removedCount, fmt.Errorf("marshal responses input item %d without namespace: %w", index, err)
		}
		items[index] = encodedItem
		removedCount++
	}
	if removedCount == 0 {
		return request, 0, nil
	}
	encodedInput, err := basecommon.Marshal(items)
	if err != nil {
		return request, removedCount, fmt.Errorf("marshal responses input without namespace history: %w", err)
	}
	request.Input = encodedInput
	return request, removedCount, nil
}

func IsResponsesNamespaceHistoryParameter(param string) bool {
	return responsesNamespaceParameterPattern.MatchString(string(bytes.TrimSpace([]byte(param))))
}
