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
