package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

func syntheticCompactMarkers(input common.RawMessage) []string {
	if common.GetJsonType(input) != "array" {
		return nil
	}
	var items []common.RawMessage
	if err := common.Unmarshal(input, &items); err != nil {
		return nil
	}
	markers := make([]string, 0, 1)
	for _, rawItem := range items {
		item, ok := responsesInputObject(rawItem)
		if !ok {
			continue
		}
		if !relaycommon.IsResponsesCompactionItemType(rawStringField(item["type"])) {
			continue
		}
		if marker := rawStringField(item["encrypted_content"]); marker != "" {
			markers = append(markers, marker)
		}
	}
	return markers
}

func removeSyntheticCompactMarkers(ctx context.Context, input common.RawMessage) (common.RawMessage, error) {
	if common.GetJsonType(input) != "array" {
		return input, nil
	}
	var items []common.RawMessage
	if err := common.Unmarshal(input, &items); err != nil {
		// Malformed input stays opaque; callers only strip markers from parseable arrays.
		return input, nil
	}
	cleaned := make([]common.RawMessage, 0, len(items))
	for _, rawItem := range items {
		item, ok := responsesInputObject(rawItem)
		if !ok {
			cleaned = append(cleaned, rawItem)
			continue
		}
		if relaycommon.IsResponsesCompactionItemType(rawStringField(item["type"])) {
			if marker := rawStringField(item["encrypted_content"]); marker != "" {
				if _, ok, err := syntheticCompactIDFromMarker(ctx, marker); err != nil {
					return nil, err
				} else if ok {
					continue
				}
			}
		}
		cleaned = append(cleaned, rawItem)
	}
	data, err := common.Marshal(cleaned)
	if err != nil {
		return nil, fmt.Errorf("marshal synthetic compact marker-cleaned input: %w", err)
	}
	return data, nil
}

func normalizeResponsesInputItems(input common.RawMessage) []common.RawMessage {
	switch common.GetJsonType(input) {
	case "array":
		var items []common.RawMessage
		if err := common.Unmarshal(input, &items); err != nil {
			return nil
		}
		normalized := make([]common.RawMessage, 0, len(items))
		for _, rawItem := range items {
			if normalizedItem, ok := normalizeResponsesInputItem(rawItem); ok {
				normalized = append(normalized, normalizedItem)
			}
		}
		return normalized
	case "string":
		text := strings.TrimSpace(rawStringField(input))
		if text != "" {
			item, err := responseMessageInput("user", text)
			if err == nil {
				return []common.RawMessage{item}
			}
		}
	}
	return nil
}

func normalizeResponsesInputItem(rawItem common.RawMessage) (common.RawMessage, bool) {
	switch common.GetJsonType(rawItem) {
	case "object":
		return rawItem, true
	case "string":
		text := strings.TrimSpace(rawStringField(rawItem))
		if text == "" {
			return nil, false
		}
		item, err := responseMessageInput("user", text)
		if err != nil {
			return nil, false
		}
		return item, true
	default:
		return nil, false
	}
}

func visibleResponsesInputParts(input common.RawMessage) []string {
	switch common.GetJsonType(input) {
	case "string":
		text := rawStringField(input)
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return []string{"[input] " + strings.TrimSpace(text)}
	case "array":
		var items []common.RawMessage
		if err := common.Unmarshal(input, &items); err != nil {
			return nil
		}
		parts := make([]string, 0, len(items))
		for _, rawItem := range items {
			if text := rawVisibleResponsesInputItem(rawItem); text != "" {
				parts = append(parts, text)
			}
		}
		return parts
	default:
		return nil
	}
}

func rawVisibleResponsesInputItem(rawItem common.RawMessage) string {
	switch common.GetJsonType(rawItem) {
	case "string":
		text := strings.TrimSpace(rawStringField(rawItem))
		if text == "" {
			return ""
		}
		return "[input] " + text
	case "object":
		item, ok := responsesInputObject(rawItem)
		if !ok || relaycommon.IsResponsesCompactionItemType(rawStringField(item["type"])) {
			return ""
		}
		text := visibleResponsesItemText(item)
		if text == "" {
			return ""
		}
		label := rawStringField(item["role"])
		if label == "" {
			label = rawStringField(item["type"])
		}
		if label == "" {
			label = "input"
		}
		return fmt.Sprintf("[%s] %s", label, text)
	default:
		return ""
	}
}

func responsesInputObject(rawItem common.RawMessage) (map[string]common.RawMessage, bool) {
	if common.GetJsonType(rawItem) != "object" {
		return nil, false
	}
	var item map[string]common.RawMessage
	if err := common.Unmarshal(rawItem, &item); err != nil {
		return nil, false
	}
	return item, true
}

func visibleResponsesItemText(item map[string]common.RawMessage) string {
	if text := visibleResponsesToolItemText(item); text != "" {
		return text
	}
	if isResponsesToolLikeType(rawStringField(item["type"])) {
		return ""
	}
	if text := rawStringField(item["text"]); text != "" {
		return strings.TrimSpace(text)
	}
	if output := rawVisibleResponsesField(item["output"]); output != "" {
		return strings.TrimSpace(output)
	}
	if input := rawVisibleResponsesField(item["input"]); input != "" {
		return strings.TrimSpace(input)
	}
	if arguments := rawVisibleResponsesField(item["arguments"]); arguments != "" {
		return arguments
	}
	content := item["content"]
	switch common.GetJsonType(content) {
	case "string":
		return strings.TrimSpace(rawStringField(content))
	case "array":
		var rawParts []common.RawMessage
		if err := common.Unmarshal(content, &rawParts); err != nil {
			return ""
		}
		parts := make([]map[string]common.RawMessage, 0, len(rawParts))
		for _, rawPart := range rawParts {
			if part, ok := responsesInputObject(rawPart); ok {
				parts = append(parts, part)
			}
		}
		if len(parts) > syntheticCompactVisibleContentPartsMax {
			common.SysLog(fmt.Sprintf("synthetic compact visible content parts truncated: original=%d max=%d", len(parts), syntheticCompactVisibleContentPartsMax))
			parts = parts[:syntheticCompactVisibleContentPartsMax]
		}
		texts := make([]string, 0, len(parts))
		for _, part := range parts {
			partType := rawStringField(part["type"])
			switch partType {
			case "input_text", "output_text", "text":
				if text := strings.TrimSpace(rawStringField(part["text"])); text != "" {
					texts = append(texts, text)
				}
			case "function_call", "custom_tool_call", "function_call_output", "custom_tool_call_output":
				if text := visibleResponsesToolItemText(part); text != "" {
					texts = append(texts, text)
				}
			}
		}
		return strings.Join(texts, "\n")
	default:
		return ""
	}
}

func visibleResponsesToolItemText(item map[string]common.RawMessage) string {
	itemType := rawStringField(item["type"])
	if !isKnownResponsesToolItemType(itemType) {
		return ""
	}
	metadata := make([]string, 0, 2)
	if name := rawStringField(item["name"]); name != "" {
		metadata = append(metadata, "name="+name)
	}
	if callID := rawStringField(item["call_id"]); callID != "" {
		metadata = append(metadata, "call_id="+callID)
	}
	payloads := make([]string, 0, 3)
	for _, field := range []string{"output", "input", "arguments"} {
		if value := rawVisibleResponsesField(item[field]); value != "" {
			payloads = append(payloads, field+"="+value)
		}
	}
	parts := append(metadata, payloads...)
	return strings.Join(parts, "\n")
}

func isKnownResponsesToolItemType(itemType string) bool {
	switch itemType {
	case "function_call", "custom_tool_call", "function_call_output", "custom_tool_call_output":
		return true
	default:
		return false
	}
}

func isResponsesToolLikeType(itemType string) bool {
	return strings.HasSuffix(itemType, "_call") || strings.HasSuffix(itemType, "_call_output")
}

func rawVisibleResponsesField(raw common.RawMessage) string {
	if value := rawStringField(raw); value != "" {
		return strings.TrimSpace(value)
	}
	if value := strings.TrimSpace(string(raw)); value != "" && value != "null" {
		return value
	}
	return ""
}

func rawStringField(raw common.RawMessage) string {
	if len(raw) == 0 || common.GetJsonType(raw) != "string" {
		return ""
	}
	var value string
	if err := common.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}
