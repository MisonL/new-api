package openaicompat

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

const (
	responsesToolTypeToolSearch = "tool_search"
	chatToolNameMaxLength       = 64
	responsesCustomToolPrefix   = "newapi_custom"
)

type ResponsesChatToolIdentity struct {
	Type      string
	Namespace string
	Name      string
}

type ResponsesChatCompatibilityToolState struct {
	byChatName          map[string]ResponsesChatToolIdentity
	definitionSignature map[string]string
	hasCustomTool       bool
}

func newResponsesChatCompatibilityToolState() *ResponsesChatCompatibilityToolState {
	return &ResponsesChatCompatibilityToolState{
		byChatName:          make(map[string]ResponsesChatToolIdentity),
		definitionSignature: make(map[string]string),
	}
}

func (s *ResponsesChatCompatibilityToolState) register(chatName string, identity ResponsesChatToolIdentity, definition any) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("responses chat tool state is required")
	}
	chatName = strings.TrimSpace(chatName)
	if chatName == "" || strings.TrimSpace(identity.Name) == "" {
		return false, fmt.Errorf("responses chat tool identity is incomplete")
	}
	signature, err := common.Marshal(definition)
	if err != nil {
		return false, fmt.Errorf("failed to encode chat tool %q: %w", chatName, err)
	}
	if previous, ok := s.byChatName[chatName]; ok {
		if previous == identity && s.definitionSignature[chatName] == string(signature) {
			return true, nil
		}
		return false, fmt.Errorf("chat tool name %q is duplicated with a conflicting definition", chatName)
	}
	s.byChatName[chatName] = identity
	s.definitionSignature[chatName] = string(signature)
	if identity.Type == dto.CustomType {
		s.hasCustomTool = true
	}
	return false, nil
}

func (s *ResponsesChatCompatibilityToolState) Resolve(chatName string) (ResponsesChatToolIdentity, bool) {
	if s == nil {
		return ResponsesChatToolIdentity{}, false
	}
	identity, ok := s.byChatName[strings.TrimSpace(chatName)]
	return identity, ok
}

func (s *ResponsesChatCompatibilityToolState) HasCustomTool() bool {
	return s != nil && s.hasCustomTool
}

func normalizeChatToolChoiceForTools(toolChoice any, tools []dto.ToolCallRequest) (any, error) {
	if toolChoice == nil {
		return nil, nil
	}
	mode, ok := toolChoice.(string)
	if !ok {
		return toolChoice, nil
	}

	switch strings.TrimSpace(mode) {
	case "auto", "none":
		if len(tools) == 0 {
			return nil, nil
		}
		return mode, nil
	case "required":
		if len(tools) == 0 {
			return nil, fmt.Errorf("tool_choice required requires at least one compatible tool")
		}
		return mode, nil
	default:
		return nil, fmt.Errorf("tool_choice %q is not supported in chat compatibility mode", mode)
	}
}

func flattenResponsesNamespaceToolName(namespace string, name string) string {
	return flattenResponsesChatToolName(namespace, name)
}

func flattenResponsesCustomToolName(name string) string {
	return flattenResponsesChatToolName(responsesCustomToolPrefix, name)
}

func flattenResponsesNamespacedCustomToolName(namespace string, name string) string {
	if strings.TrimSpace(namespace) == "" {
		return flattenResponsesCustomToolName(name)
	}
	return flattenResponsesChatToolName(responsesCustomToolPrefix, namespace, name)
}

func flattenResponsesChatToolName(parts ...string) string {
	trimmedParts := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmedParts = append(trimmedParts, strings.TrimSpace(part))
	}
	fullName := strings.Join(trimmedParts, "__")
	if len(fullName) <= chatToolNameMaxLength {
		return fullName
	}
	sum := sha256.Sum256([]byte(fullName))
	suffix := "__" + hex.EncodeToString(sum[:4])
	prefixLimit := chatToolNameMaxLength - len(suffix)
	var prefix strings.Builder
	for _, ch := range fullName {
		if prefix.Len()+len(string(ch)) > prefixLimit {
			break
		}
		prefix.WriteRune(ch)
	}
	return prefix.String() + suffix
}
