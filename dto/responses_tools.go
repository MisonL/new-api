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
	signature := string(r.Tools) + "\x00" + string(r.Input)
	if r.effectiveToolsCacheReady && r.effectiveToolsCacheSignature == signature {
		return r.effectiveToolsCache, r.effectiveToolsCacheErr
	}
	tools, err := r.effectiveToolsUncached()
	r.effectiveToolsCacheSignature = signature
	r.effectiveToolsCache = tools
	r.effectiveToolsCacheErr = err
	r.effectiveToolsCacheReady = true
	return tools, err
}

func (r *OpenAIResponsesRequest) effectiveToolsUncached() ([]common.RawMessage, error) {
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
			Type      string            `json:"type"`
			CallID    string            `json:"call_id"`
			Status    string            `json:"status"`
			Execution string            `json:"execution"`
			Tools     common.RawMessage `json:"tools"`
		}
		if err := common.Unmarshal(rawItem, &item); err != nil {
			return nil, fmt.Errorf("failed to parse input item for additional_tools: %w", err)
		}
		switch strings.TrimSpace(item.Type) {
		case "additional_tools":
			additionalTools, err := parseResponsesToolsField(item.Tools, "additional_tools.tools", true)
			if err != nil {
				return nil, err
			}
			tools = append(tools, additionalTools...)
		case "tool_search_output":
			execution := strings.TrimSpace(item.Execution)
			if execution == "server" {
				// Server-executed history is not a client-side tool declaration.
				// Keep it in input, but do not charge or route it as an effective tool.
				continue
			}
			if execution != "" && execution != "client" {
				return nil, fmt.Errorf("tool_search_output.execution must be client or server")
			}
			if strings.TrimSpace(item.CallID) == "" {
				// Older server history may omit execution and call_id. It cannot
				// prove a client-loaded tool set, so leave it out of the effective set.
				if execution == "" {
					continue
				}
				return nil, fmt.Errorf("tool_search_output.call_id is required for client execution")
			}
			if strings.TrimSpace(item.Status) != "completed" {
				return nil, fmt.Errorf("tool_search_output.status must be completed")
			}
			loadedTools, err := parseResponsesToolsField(item.Tools, "tool_search_output.tools", true)
			if err != nil {
				return nil, err
			}
			tools = append(tools, loadedTools...)
		}
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
