package channel

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/dto"
)

// OpenAIMessageText converts an OpenAI message to the text-only representation
// used by legacy provider protocols. It deliberately rejects non-text content,
// malformed tool calls, and metadata which those protocols cannot express.
// Returning an error is preferable to silently forwarding an incomplete
// request.
func OpenAIMessageText(message dto.Message, protocol string) (string, error) {
	if message.Name != nil && strings.TrimSpace(*message.Name) != "" {
		return "", fmt.Errorf("message name is not supported by %s", protocol)
	}
	if message.Prefix != nil {
		return "", fmt.Errorf("message prefix is not supported by %s", protocol)
	}
	if reasoning := message.GetReasoningContent(); reasoning != "" {
		return "", fmt.Errorf("reasoning_content is not supported by %s", protocol)
	}
	toolCalls, err := message.ParseToolCallsStrict()
	if err != nil {
		return "", err
	}
	if len(toolCalls) > 0 {
		return "", fmt.Errorf("tool_calls are not supported by %s", protocol)
	}
	if strings.TrimSpace(message.ToolCallId) != "" {
		return "", fmt.Errorf("tool_call_id is not supported by %s", protocol)
	}

	parts, err := message.ParseContentStrict()
	if err != nil {
		return "", err
	}
	var text strings.Builder
	for index, part := range parts {
		if part.Type != dto.ContentTypeText {
			return "", fmt.Errorf("content[%d] type %q is not supported by %s", index, part.Type, protocol)
		}
		text.WriteString(part.Text)
	}
	return text.String(), nil
}

// OpenAIMessageRoleText validates a role and converts its content for a
// text-only provider. The role map contains the provider role for each
// accepted OpenAI role (for example, developer -> system).
func OpenAIMessageRoleText(message dto.Message, protocol string, roles map[string]string) (string, string, error) {
	role := strings.ToLower(strings.TrimSpace(message.Role))
	providerRole, ok := roles[role]
	if !ok {
		return "", "", fmt.Errorf("role %q is not supported by %s", message.Role, protocol)
	}
	text, err := OpenAIMessageText(message, protocol)
	if err != nil {
		return "", "", err
	}
	return providerRole, text, nil
}

// RejectUnsupportedTextRequestFields rejects request-level OpenAI features
// that a legacy text protocol cannot represent. Adapters may still map
// provider-specific supported fields before calling this helper.
func RejectUnsupportedTextRequestFields(request dto.GeneralOpenAIRequest, protocol string) error {
	if len(request.Tools) > 0 {
		return fmt.Errorf("tools are not supported by %s", protocol)
	}
	if request.ToolChoice != nil {
		return fmt.Errorf("tool_choice is not supported by %s", protocol)
	}
	if len(request.Functions) > 0 {
		return fmt.Errorf("functions are not supported by %s", protocol)
	}
	if len(request.FunctionCall) > 0 {
		return fmt.Errorf("function_call is not supported by %s", protocol)
	}
	if request.ResponseFormat != nil {
		return fmt.Errorf("response_format is not supported by %s", protocol)
	}
	if request.ReasoningEffort != "" || len(request.Reasoning) > 0 || len(request.Verbosity) > 0 {
		return fmt.Errorf("reasoning options are not supported by %s", protocol)
	}
	if request.Prefix != nil || request.Suffix != nil {
		return fmt.Errorf("prefix/suffix are not supported by %s", protocol)
	}
	if len(request.Modalities) > 0 || len(request.Audio) > 0 {
		return fmt.Errorf("audio or modalities are not supported by %s", protocol)
	}
	return nil
}
