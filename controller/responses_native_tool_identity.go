package controller

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
)

const (
	codexResponsesLiteHeader                       = "x-openai-internal-codex-responses-lite"
	codexResponsesLiteClientMetadataKey            = "client_metadata"
	codexResponsesLiteTurnMetadataKey              = "x-codex-turn-metadata"
	codexResponsesLiteNativeToolIdentityContextKey = "responses_codex_lite_native_tool_identity"
)

type responsesNativeToolIdentity struct {
	namespace string
	name      string
}

type codexLiteNativeToolIdentityDecision struct {
	request     *dto.OpenAIResponsesRequest
	required    bool
	unparseable bool
}

func codexLiteNativeToolIdentityDecisionFor(c *gin.Context, request *dto.OpenAIResponsesRequest) codexLiteNativeToolIdentityDecision {
	if c == nil || c.Request == nil || request == nil || !isCodexResponsesLiteRequest(c) {
		return codexLiteNativeToolIdentityDecision{}
	}
	if cached, ok := c.Get(codexResponsesLiteNativeToolIdentityContextKey); ok {
		if decision, ok := cached.(codexLiteNativeToolIdentityDecision); ok && decision.request == request {
			return decision
		}
	}

	decision := codexLiteNativeToolIdentityRequired(request)
	decision.request = request
	c.Set(codexResponsesLiteNativeToolIdentityContextKey, decision)
	return decision
}

func isCodexResponsesLiteRequest(c *gin.Context) bool {
	return c != nil && c.Request != nil && strings.EqualFold(
		strings.TrimSpace(c.Request.Header.Get(codexResponsesLiteHeader)),
		"true",
	)
}

func codexLiteNativeToolIdentityRequired(request *dto.OpenAIResponsesRequest) codexLiteNativeToolIdentityDecision {
	declared, err := codexLiteDeclaredNamespaceToolIdentities(request)
	if err != nil {
		return codexLiteNativeToolIdentityDecision{required: true, unparseable: true}
	}
	metadata, err := codexLiteCodeModeToolIdentities(request)
	if err != nil {
		if len(declared) == 0 {
			return codexLiteNativeToolIdentityDecision{}
		}
		return codexLiteNativeToolIdentityDecision{required: true, unparseable: true}
	}
	if len(declared) == 0 || len(metadata) == 0 {
		return codexLiteNativeToolIdentityDecision{}
	}
	developerText, err := codexLiteDeveloperInstructionText(request)
	if err != nil {
		return codexLiteNativeToolIdentityDecision{required: true, unparseable: true}
	}
	if developerText == "" {
		return codexLiteNativeToolIdentityDecision{}
	}
	for identity := range metadata {
		if !declared[identity] {
			continue
		}
		if developerTextReferencesCodexNativeTool(developerText, identity) {
			return codexLiteNativeToolIdentityDecision{required: true}
		}
	}
	return codexLiteNativeToolIdentityDecision{}
}

func codexNativeToolInvocation(identity responsesNativeToolIdentity) string {
	return "functions." + identity.namespace + "." + identity.name
}

func developerTextReferencesCodexNativeTool(text string, identity responsesNativeToolIdentity) bool {
	invocation := codexNativeToolInvocation(identity)
	searchFrom := 0
	for searchFrom < len(text) {
		relativeIndex := strings.Index(text[searchFrom:], invocation)
		if relativeIndex < 0 {
			return false
		}
		index := searchFrom + relativeIndex
		end := index + len(invocation)
		if isCodexNativeToolInvocationBoundary(text, index, end) {
			return true
		}
		searchFrom = index + 1
	}
	return false
}

func isCodexNativeToolInvocationBoundary(text string, start, end int) bool {
	if start > 0 {
		previous, _ := utf8.DecodeLastRuneInString(text[:start])
		if isCodexNativeToolPathCharacter(previous) {
			return false
		}
	}
	if end < len(text) {
		next, size := utf8.DecodeRuneInString(text[end:])
		if next == '.' {
			if end+size >= len(text) {
				return true
			}
			afterDot, _ := utf8.DecodeRuneInString(text[end+size:])
			return !isCodexNativeToolPathCharacter(afterDot)
		}
		if isCodexNativeToolPathCharacter(next) {
			return false
		}
	}
	return true
}

func isCodexNativeToolPathCharacter(value rune) bool {
	return unicode.IsLetter(value) || unicode.IsDigit(value) || unicode.IsMark(value) ||
		value == '_' || value == '-' || value == '.'
}

func codexLiteCodeModeToolIdentities(request *dto.OpenAIResponsesRequest) (map[responsesNativeToolIdentity]bool, error) {
	if request == nil || len(request.Extra) == 0 {
		return nil, nil
	}
	rawClientMetadata, exists := request.Extra[codexResponsesLiteClientMetadataKey]
	if !exists {
		return nil, nil
	}
	if common.GetJsonType(rawClientMetadata) != "object" {
		return nil, fmt.Errorf("Codex Lite client metadata must be an object")
	}
	var clientMetadata map[string]common.RawMessage
	if err := common.Unmarshal(rawClientMetadata, &clientMetadata); err != nil {
		return nil, fmt.Errorf("decode Codex Lite client metadata: %w", err)
	}
	rawTurnMetadata, exists := clientMetadata[codexResponsesLiteTurnMetadataKey]
	if !exists {
		return nil, nil
	}
	if common.GetJsonType(rawTurnMetadata) != "string" {
		return nil, fmt.Errorf("Codex Lite turn metadata must be a string")
	}
	var turnMetadataJSON string
	if err := common.Unmarshal(rawTurnMetadata, &turnMetadataJSON); err != nil {
		return nil, fmt.Errorf("decode Codex Lite turn metadata: %w", err)
	}
	var turnMetadata struct {
		CodeModeToolNames map[string]struct {
			Name      string  `json:"name"`
			Namespace *string `json:"namespace"`
		} `json:"code_mode_tool_names"`
	}
	if err := common.Unmarshal([]byte(turnMetadataJSON), &turnMetadata); err != nil {
		return nil, fmt.Errorf("decode Codex Lite code mode tool metadata: %w", err)
	}
	identities := make(map[responsesNativeToolIdentity]bool, len(turnMetadata.CodeModeToolNames))
	for _, tool := range turnMetadata.CodeModeToolNames {
		if tool.Namespace == nil {
			continue
		}
		identity := responsesNativeToolIdentity{
			namespace: strings.TrimSpace(*tool.Namespace),
			name:      strings.TrimSpace(tool.Name),
		}
		if identity.namespace != "" && identity.name != "" {
			identities[identity] = true
		}
	}
	return identities, nil
}

func codexLiteDeclaredNamespaceToolIdentities(request *dto.OpenAIResponsesRequest) (map[responsesNativeToolIdentity]bool, error) {
	if request == nil {
		return nil, nil
	}
	tools, err := request.EffectiveTools()
	if err != nil {
		return nil, fmt.Errorf("decode Codex Lite tools: %w", err)
	}
	identities := make(map[responsesNativeToolIdentity]bool)
	for _, rawTool := range tools {
		if err := appendCodexLiteNamespaceToolIdentity(rawTool, identities); err != nil {
			return nil, err
		}
	}
	return identities, nil
}

func appendCodexLiteNamespaceToolIdentity(rawTool common.RawMessage, identities map[responsesNativeToolIdentity]bool) error {
	if common.GetJsonType(rawTool) != "object" {
		return fmt.Errorf("Codex Lite tool must be an object")
	}
	var namespaceTool struct {
		Type  string            `json:"type"`
		Name  string            `json:"name"`
		Tools common.RawMessage `json:"tools"`
	}
	if err := common.Unmarshal(rawTool, &namespaceTool); err != nil {
		return fmt.Errorf("decode Codex Lite tool: %w", err)
	}
	if strings.TrimSpace(namespaceTool.Type) != "namespace" {
		return nil
	}
	namespace := strings.TrimSpace(namespaceTool.Name)
	if namespace == "" || common.GetJsonType(namespaceTool.Tools) != "array" {
		return fmt.Errorf("Codex Lite namespace tool is malformed")
	}
	var nestedTools []common.RawMessage
	if err := common.Unmarshal(namespaceTool.Tools, &nestedTools); err != nil {
		return fmt.Errorf("decode Codex Lite namespace tools: %w", err)
	}
	for _, rawNestedTool := range nestedTools {
		if common.GetJsonType(rawNestedTool) != "object" {
			return fmt.Errorf("Codex Lite namespace tool entry must be an object")
		}
		var nestedTool struct {
			Name string `json:"name"`
		}
		if err := common.Unmarshal(rawNestedTool, &nestedTool); err != nil {
			return fmt.Errorf("decode Codex Lite namespace tool entry: %w", err)
		}
		name := strings.TrimSpace(nestedTool.Name)
		if name == "" {
			return fmt.Errorf("Codex Lite namespace tool entry is missing name")
		}
		identities[responsesNativeToolIdentity{namespace: namespace, name: name}] = true
	}
	return nil
}

func codexLiteDeveloperInstructionText(request *dto.OpenAIResponsesRequest) (string, error) {
	if request == nil {
		return "", nil
	}
	inputType := common.GetJsonType(request.Input)
	if inputType != "array" {
		if inputType == "unknown" {
			return "", fmt.Errorf("Codex Lite input cannot be decoded")
		}
		return "", nil
	}
	var inputItems []common.RawMessage
	if err := common.Unmarshal(request.Input, &inputItems); err != nil {
		return "", fmt.Errorf("decode Codex Lite input: %w", err)
	}
	texts := make([]string, 0)
	for _, rawItem := range inputItems {
		if common.GetJsonType(rawItem) != "object" {
			continue
		}
		var item struct {
			Type    string            `json:"type"`
			Role    string            `json:"role"`
			Content common.RawMessage `json:"content"`
		}
		if err := common.Unmarshal(rawItem, &item); err != nil {
			return "", fmt.Errorf("decode Codex Lite input item: %w", err)
		}
		if strings.TrimSpace(item.Type) != "message" || strings.TrimSpace(item.Role) != "developer" {
			continue
		}
		contentTexts, err := codexLiteInputContentText(item.Content)
		if err != nil {
			return "", err
		}
		texts = append(texts, contentTexts...)
	}
	return strings.Join(texts, "\n"), nil
}

func codexLiteInputContentText(rawContent common.RawMessage) ([]string, error) {
	switch common.GetJsonType(rawContent) {
	case "string":
		var text string
		if err := common.Unmarshal(rawContent, &text); err != nil {
			return nil, fmt.Errorf("decode Codex Lite developer content: %w", err)
		}
		if strings.TrimSpace(text) != "" {
			return []string{text}, nil
		}
	case "array":
		var items []common.RawMessage
		if err := common.Unmarshal(rawContent, &items); err != nil {
			return nil, fmt.Errorf("decode Codex Lite developer content: %w", err)
		}
		texts := make([]string, 0, len(items))
		for _, rawItem := range items {
			if common.GetJsonType(rawItem) != "object" {
				continue
			}
			var item struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if err := common.Unmarshal(rawItem, &item); err != nil {
				return nil, fmt.Errorf("decode Codex Lite developer content item: %w", err)
			}
			if strings.TrimSpace(item.Type) == "input_text" && strings.TrimSpace(item.Text) != "" {
				texts = append(texts, item.Text)
			}
		}
		return texts, nil
	case "object", "number", "boolean", "unknown":
		return nil, fmt.Errorf("Codex Lite developer content must be a string or array")
	}
	return nil, nil
}
