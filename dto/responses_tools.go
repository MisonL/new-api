package dto

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

func (r *OpenAIResponsesRequest) EffectiveTools() ([]common.RawMessage, error) {
	if r == nil {
		return nil, nil
	}

	tools, err := parseResponsesToolsField(r.Tools, "tools", false)
	if err != nil {
		return nil, err
	}
	if common.GetJsonType(r.Input) != "array" {
		return tools, nil
	}

	var inputItems []common.RawMessage
	if err := common.Unmarshal(r.Input, &inputItems); err != nil {
		return nil, fmt.Errorf("failed to parse input for additional_tools: %w", err)
	}
	for _, rawItem := range inputItems {
		if common.GetJsonType(rawItem) != "object" {
			continue
		}
		var item struct {
			Type  string            `json:"type"`
			Tools common.RawMessage `json:"tools"`
		}
		if err := common.Unmarshal(rawItem, &item); err != nil {
			return nil, fmt.Errorf("failed to parse input item for additional_tools: %w", err)
		}
		if strings.TrimSpace(item.Type) != "additional_tools" {
			continue
		}
		additionalTools, err := parseResponsesToolsField(item.Tools, "additional_tools.tools", true)
		if err != nil {
			return nil, err
		}
		tools = append(tools, additionalTools...)
	}
	return tools, nil
}

func parseResponsesToolsField(raw common.RawMessage, fieldName string, required bool) ([]common.RawMessage, error) {
	jsonType := common.GetJsonType(raw)
	if jsonType == "unknown" || jsonType == "null" {
		if required {
			return nil, fmt.Errorf("%s is required", fieldName)
		}
		return nil, nil
	}
	if jsonType != "array" {
		return nil, fmt.Errorf("%s must be an array", fieldName)
	}
	var tools []common.RawMessage
	if err := common.Unmarshal(raw, &tools); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", fieldName, err)
	}
	return tools, nil
}
