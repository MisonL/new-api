package openaicompat

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

const (
	responsesToolTypeToolSearch = "tool_search"
	responsesCustomToolPrefix   = "newapi_custom"
)

// toolNameAliasRegistry tracks the names used while converting a Chat
// Completions request to Responses. A normalized alias must not silently
// represent two different client names, including the edge case where a
// valid client name happens to equal an alias generated for an invalid one.
type toolNameAliasRegistry struct {
	originalByAlias map[string]string
	aliasByOriginal map[string]string
}

func newToolNameAliasRegistry() *toolNameAliasRegistry {
	return &toolNameAliasRegistry{
		originalByAlias: make(map[string]string),
		aliasByOriginal: make(map[string]string),
	}
}

func (r *toolNameAliasRegistry) normalizeDeclaration(name string) (string, error) {
	original := strings.TrimSpace(name)
	if original == "" {
		return "", fmt.Errorf("tool name is required")
	}
	if alias, exists := r.aliasByOriginal[original]; exists {
		return alias, nil
	}
	alias := dto.NormalizeToolName(original)
	if previous, exists := r.originalByAlias[alias]; exists && previous != original {
		return "", fmt.Errorf("tool name %q normalizes to %q, conflicting with %q", original, alias, previous)
	}
	r.originalByAlias[alias] = original
	r.aliasByOriginal[original] = alias
	return alias, nil
}

// originalByAliasCopy returns a copy of the alias-to-original mapping for
// callers that need to build a response-side restorer after conversion ends.
func (r *toolNameAliasRegistry) originalByAliasCopy() map[string]string {
	out := make(map[string]string, len(r.originalByAlias))
	for alias, original := range r.originalByAlias {
		out[alias] = original
	}
	return out
}

func (r *toolNameAliasRegistry) normalizeReference(name string) (string, error) {
	original := strings.TrimSpace(name)
	if original == "" {
		return "", fmt.Errorf("tool name is required")
	}
	if alias, exists := r.aliasByOriginal[original]; exists {
		return alias, nil
	}
	if _, exists := r.originalByAlias[original]; exists {
		return original, nil
	}
	return r.normalizeDeclaration(original)
}

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

func (s *ResponsesChatCompatibilityToolState) ChatNameFor(identity ResponsesChatToolIdentity) (string, bool) {
	if s == nil {
		return "", false
	}
	for chatName, registered := range s.byChatName {
		if registered == identity {
			return chatName, true
		}
	}
	return "", false
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
	return dto.NormalizeToolName(strings.Join(trimmedParts, "__"))
}

// flattenResponsesOutputToolName produces the Chat-visible name for a
// Responses function call. Namespaced calls must use the same flattened alias
// as their declarations and history; unnamespaced calls are normalized on the
// boundary so an upstream cannot emit a name rejected by Chat contracts.
func flattenResponsesOutputToolName(namespace string, name string) string {
	if strings.TrimSpace(namespace) != "" {
		return flattenResponsesNamespaceToolName(namespace, name)
	}
	return dto.NormalizeToolName(name)
}
