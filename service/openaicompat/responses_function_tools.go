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
			Name:        name,
			Description: common.Interface2String(tool["description"]),
			Parameters:  parameters,
			Strict:      strict,
		},
	}, nil
}

func validateResponsesCustomToolFormat(tool map[string]any) error {
	format, exists := tool["format"]
	if !exists || format == nil {
		return nil
	}
	formatMap, ok := format.(map[string]any)
	if !ok {
		return fmt.Errorf("custom tool format must be an object")
	}
	formatType := strings.TrimSpace(common.Interface2String(formatMap["type"]))
	if formatType == "" {
		return fmt.Errorf("custom tool format type is required")
	}
	if formatType != "text" {
		return fmt.Errorf("custom tool format type %q is not supported in chat compatibility mode", formatType)
	}
	return nil
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
