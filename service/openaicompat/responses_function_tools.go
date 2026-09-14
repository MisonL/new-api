package openaicompat

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

func parseResponsesFunctionToolStrict(tool map[string]any) (*bool, error) {
	value, exists := tool["strict"]
	if !exists || value == nil {
		return nil, nil
	}
	strict, ok := value.(bool)
	if !ok {
		return nil, fmt.Errorf("tool strict must be a boolean")
	}
	return &strict, nil
}

func convertResponsesFunctionToolToChat(tool map[string]any) (dto.ToolCallRequest, error) {
	name := strings.TrimSpace(common.Interface2String(tool["name"]))
	if name == "" {
		return dto.ToolCallRequest{}, fmt.Errorf("tool name is required")
	}
	strict, err := parseResponsesFunctionToolStrict(tool)
	if err != nil {
		return dto.ToolCallRequest{}, err
	}
	parameters := tool["parameters"]
	if parameters == nil {
		parameters = tool["input_schema"]
	}
	return dto.ToolCallRequest{
		Type: "function",
		Function: dto.FunctionRequest{
			Name:        dto.NormalizeToolName(name),
			Description: common.Interface2String(tool["description"]),
			Parameters:  parameters,
			Strict:      strict,
		},
	}, nil
}

func responsesCustomToolFormatGuidance(tool map[string]any) (string, error) {
	format, exists := tool["format"]
	if !exists || format == nil {
		return "", nil
	}
	formatMap, ok := format.(map[string]any)
	if !ok {
		return "", fmt.Errorf("custom tool format must be an object")
	}
	formatType := strings.TrimSpace(common.Interface2String(formatMap["type"]))
	if formatType == "" {
		return "", fmt.Errorf("custom tool format type is required")
	}
	switch formatType {
	case "text":
		return "", nil
	case "grammar":
		return responsesLarkGrammarGuidance(formatMap)
	default:
		return "", fmt.Errorf("custom tool format type %q is not supported in chat compatibility mode", formatType)
	}
}

func responsesLarkGrammarGuidance(format map[string]any) (string, error) {
	rawSyntax, exists := format["syntax"]
	if !exists || rawSyntax == nil {
		return "", fmt.Errorf("custom tool grammar syntax is required")
	}
	syntax, ok := rawSyntax.(string)
	if !ok {
		return "", fmt.Errorf("custom tool grammar syntax must be a string")
	}
	syntax = strings.TrimSpace(syntax)
	if syntax == "" {
		return "", fmt.Errorf("custom tool grammar syntax is required")
	}
	if syntax != "lark" {
		return "", fmt.Errorf("custom tool grammar syntax %q is not supported in chat compatibility mode", syntax)
	}
	rawDefinition, exists := format["definition"]
	if !exists || rawDefinition == nil {
		return "", fmt.Errorf("custom tool grammar definition is required")
	}
	definition, ok := rawDefinition.(string)
	if !ok {
		return "", fmt.Errorf("custom tool grammar definition must be a string")
	}
	definition = strings.TrimSpace(definition)
	if definition == "" {
		return "", fmt.Errorf("custom tool grammar definition is required")
	}
	return " Complete input must satisfy the following Lark grammar:\n" + definition, nil
}

func withResponsesNamespaceDescription(description string, namespaceDescription string) string {
	namespaceDescription = strings.TrimSpace(namespaceDescription)
	if namespaceDescription == "" {
		return description
	}
	description = strings.TrimSpace(description)
	if description == "" {
		return namespaceDescription
	}
	return namespaceDescription + " " + description
}
