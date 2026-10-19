package ollama

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

func openAIChatToOllamaChat(c *gin.Context, r *dto.GeneralOpenAIRequest) (*OllamaChatRequest, error) {
	if r == nil {
		return nil, fmt.Errorf("request is nil")
	}
	if err := validateOllamaChatRequest(*r); err != nil {
		return nil, err
	}
	chatReq := &OllamaChatRequest{
		Model:   r.Model,
		Stream:  lo.FromPtrOr(r.Stream, false),
		Options: map[string]any{},
		Think:   r.Think,
	}
	toolNamesByID := make(map[string]string)
	for messageIndex, message := range r.Messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		if role == "developer" {
			role = "system"
		}
		if role != "assistant" {
			continue
		}
		calls, err := message.ParseToolCallsStrict()
		if err != nil {
			return nil, fmt.Errorf("messages[%d]: %w", messageIndex, err)
		}
		for callIndex, call := range calls {
			id := strings.TrimSpace(call.ID)
			name := strings.TrimSpace(call.Function.Name)
			if id == "" || name == "" {
				continue
			}
			if existing, ok := toolNamesByID[id]; ok && existing != name {
				return nil, fmt.Errorf("messages[%d].tool_calls[%d].id %q maps to conflicting tool names", messageIndex, callIndex, id)
			}
			toolNamesByID[id] = name
		}
	}
	if r.ResponseFormat != nil {
		format, err := normalizeOllamaResponseFormat(r.ResponseFormat, "Chat")
		if err != nil {
			return nil, err
		}
		chatReq.Format = format
	}

	if err := addOllamaSamplingOptions(chatReq.Options, *r); err != nil {
		return nil, err
	}

	if len(r.Tools) > 0 {
		tools := make([]OllamaTool, 0, len(r.Tools))
		seenNames := make(map[string]struct{}, len(r.Tools))
		for toolIndex, t := range r.Tools {
			if strings.TrimSpace(t.Type) != "" && t.Type != "function" {
				return nil, fmt.Errorf("tools[%d].type %q is not supported by Ollama Chat", toolIndex, t.Type)
			}
			name := strings.TrimSpace(t.Function.Name)
			if name == "" {
				return nil, fmt.Errorf("tools[%d].function.name is required by Ollama Chat", toolIndex)
			}
			if _, exists := seenNames[name]; exists {
				return nil, fmt.Errorf("tools[%d].function.name %q is duplicated", toolIndex, name)
			}
			seenNames[name] = struct{}{}
			parameters, err := normalizeOllamaToolParameters(t.Function.Parameters)
			if err != nil {
				return nil, fmt.Errorf("tools[%d].function.parameters: %w", toolIndex, err)
			}
			tools = append(tools, OllamaTool{Type: "function", Function: OllamaToolFunction{Name: name, Description: t.Function.Description, Parameters: parameters}})
		}
		chatReq.Tools = tools
	}

	chatReq.Messages = make([]OllamaChatMessage, 0, len(r.Messages))
	for messageIndex, m := range r.Messages {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		if role == "developer" {
			role = "system"
		}
		if role != "system" && role != "user" && role != "assistant" && role != "tool" {
			return nil, fmt.Errorf("messages[%d].role %q is not supported by Ollama Chat", messageIndex, m.Role)
		}
		if err := validateOllamaMessageMetadata(m, role, messageIndex); err != nil {
			return nil, err
		}
		var textBuilder strings.Builder
		var images []string
		if m.IsStringContent() {
			textBuilder.WriteString(m.StringContent())
		} else {
			parts, err := m.ParseContentStrict()
			if err != nil {
				return nil, fmt.Errorf("messages[%d]: %w", messageIndex, err)
			}
			for partIndex, part := range parts {
				if part.Type == dto.ContentTypeImageURL {
					if role != "user" {
						return nil, fmt.Errorf("messages[%d].content[%d]: images are only supported in user messages by Ollama Chat", messageIndex, partIndex)
					}
					source := part.ToFileSource()
					if source == nil {
						return nil, fmt.Errorf("messages[%d].content[%d] image_url has no usable source", messageIndex, partIndex)
					}
					base64Data, _, err := service.GetBase64Data(c, source, "fetch image for ollama chat")
					if err != nil {
						return nil, err
					}
					if base64Data == "" {
						return nil, fmt.Errorf("messages[%d].content[%d] image_url resolved to empty data", messageIndex, partIndex)
					}
					images = append(images, base64Data)
				} else if part.Type == dto.ContentTypeText {
					textBuilder.WriteString(part.Text)
				} else {
					return nil, fmt.Errorf("messages[%d].content[%d] type %q is not supported by Ollama Chat", messageIndex, partIndex, part.Type)
				}
			}
		}
		if textBuilder.Len() == 0 && len(images) == 0 && role != "assistant" {
			return nil, fmt.Errorf("messages[%d]: content is required by Ollama Chat", messageIndex)
		}
		cm := OllamaChatMessage{Role: role, Content: textBuilder.String()}
		if len(images) > 0 {
			cm.Images = images
		}
		if role == "tool" {
			toolName := ""
			if m.Name != nil {
				toolName = strings.TrimSpace(*m.Name)
			}
			if toolCallID := strings.TrimSpace(m.ToolCallId); toolCallID != "" {
				mappedName := toolNamesByID[toolCallID]
				if mappedName != "" && toolName != "" && mappedName != toolName {
					return nil, fmt.Errorf("messages[%d].tool_call_id %q maps to conflicting tool names", messageIndex, toolCallID)
				}
				if toolName == "" {
					toolName = mappedName
				}
			}
			if toolName == "" {
				return nil, fmt.Errorf("messages[%d]: tool message requires name or a tool_call_id mapped from assistant tool_calls", messageIndex)
			}
			cm.ToolName = toolName
			cm.ToolCallID = strings.TrimSpace(m.ToolCallId)
		}
		if role == "assistant" && (m.ReasoningContent != nil || m.ThinkingContent != nil || m.Reasoning != nil) {
			thinking, err := common.Marshal(m.GetReasoningContent())
			if err != nil {
				return nil, fmt.Errorf("messages[%d].reasoning_content cannot be encoded: %w", messageIndex, err)
			}
			cm.Thinking = thinking
		}
		if m.ToolCalls != nil && len(m.ToolCalls) > 0 {
			parsed, err := m.ParseToolCallsStrict()
			if err != nil {
				return nil, fmt.Errorf("messages[%d]: %w", messageIndex, err)
			}
			if len(parsed) > 0 {
				calls := make([]OllamaToolCall, 0, len(parsed))
				for callIndex, tc := range parsed {
					if role != "assistant" {
						return nil, fmt.Errorf("messages[%d].tool_calls are only supported on assistant messages", messageIndex)
					}
					if strings.TrimSpace(tc.Type) != "" && tc.Type != "function" {
						return nil, fmt.Errorf("messages[%d].tool_calls[%d].type %q is not supported by Ollama Chat", messageIndex, callIndex, tc.Type)
					}
					if strings.TrimSpace(tc.Function.Name) == "" {
						return nil, fmt.Errorf("messages[%d].tool_calls[%d] function name is required", messageIndex, callIndex)
					}
					var args map[string]interface{}
					if tc.Function.Arguments != "" {
						if err := common.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
							return nil, fmt.Errorf("messages[%d].tool_calls[%d] arguments are invalid: %w", messageIndex, callIndex, err)
						}
						if args == nil {
							return nil, fmt.Errorf("messages[%d].tool_calls[%d] arguments must be a JSON object", messageIndex, callIndex)
						}
					}
					if args == nil {
						args = map[string]any{}
					}
					oc := OllamaToolCall{}
					oc.Function.Index = common.GetPointer(callIndex)
					oc.Function.Name = tc.Function.Name
					oc.Function.Arguments = args
					calls = append(calls, oc)
				}
				cm.ToolCalls = calls
			}
		}
		if role == "assistant" && textBuilder.Len() == 0 && len(images) == 0 && len(cm.ToolCalls) == 0 && cm.Thinking == nil {
			return nil, fmt.Errorf("messages[%d]: assistant message must contain content, reasoning, or tool_calls", messageIndex)
		}
		chatReq.Messages = append(chatReq.Messages, cm)
	}
	return chatReq, nil
}

func validateOllamaChatRequest(r dto.GeneralOpenAIRequest) error {
	if r.Prompt != nil || r.Input != nil || r.Instruction != "" {
		return fmt.Errorf("prompt/input fields are not supported by Ollama Chat")
	}
	if r.Prefix != nil || r.Suffix != nil {
		return fmt.Errorf("prefix/suffix are not supported by Ollama Chat")
	}
	if r.N != nil && *r.N != 1 {
		return fmt.Errorf("Ollama Chat supports only one completion (n=1)")
	}
	if len(r.Functions) > 0 || len(r.FunctionCall) > 0 {
		return fmt.Errorf("legacy functions/function_call are not supported by Ollama Chat")
	}
	if r.ToolChoice != nil {
		return fmt.Errorf("tool_choice is not supported by Ollama Chat")
	}
	if r.ParallelTooCalls != nil {
		return fmt.Errorf("parallel_tool_calls is not supported by Ollama Chat")
	}
	if r.ReasoningEffort != "" || len(r.Verbosity) > 0 || len(r.Reasoning) > 0 {
		return fmt.Errorf("reasoning_effort, verbosity, and reasoning are not supported by Ollama Chat; use think")
	}
	if len(r.Modalities) > 0 || len(r.Audio) > 0 {
		return fmt.Errorf("audio/modalities are not supported by Ollama Chat")
	}
	if len(r.ExtraBody) > 0 || len(r.SearchParameters) > 0 || r.WebSearchOptions != nil || len(r.WebSearch) > 0 ||
		len(r.EnableSearch) > 0 || len(r.SearchDomainFilter) > 0 || len(r.SearchRecencyFilter) > 0 || r.ReturnImages != nil ||
		r.ReturnRelatedQuestions != nil || len(r.SearchMode) > 0 {
		return fmt.Errorf("provider-specific search and extra fields are not supported by Ollama Chat")
	}
	if len(r.EncodingFormat) > 0 || r.Dimensions != nil || len(r.Size) > 0 {
		return fmt.Errorf("embedding/image fields are not supported by Ollama Chat")
	}
	if len(r.ServiceTier) > 0 || r.LogProbs != nil || r.TopLogProbs != nil || len(r.SafetyIdentifier) > 0 || len(r.Store) > 0 ||
		r.PromptCacheKey != "" || len(r.PromptCacheRetention) > 0 || len(r.LogitBias) > 0 || len(r.Metadata) > 0 || len(r.Prediction) > 0 ||
		len(r.VlHighResolutionImages) > 0 || len(r.EnableThinking) > 0 || len(r.ChatTemplateKwargs) > 0 || len(r.THINKING) > 0 || len(r.ReasoningSplit) > 0 {
		return fmt.Errorf("request fields are not supported by Ollama Chat")
	}
	return validateOllamaThink(r.Think)
}

func validateOllamaMessageMetadata(message dto.Message, role string, messageIndex int) error {
	if message.Name != nil && strings.TrimSpace(*message.Name) != "" && role != "tool" {
		return fmt.Errorf("messages[%d].name is not supported by Ollama Chat", messageIndex)
	}
	if message.Prefix != nil {
		return fmt.Errorf("messages[%d].prefix is not supported by Ollama Chat", messageIndex)
	}
	if strings.TrimSpace(message.ToolCallId) != "" && role != "tool" {
		return fmt.Errorf("messages[%d].tool_call_id is only supported on tool messages by Ollama Chat", messageIndex)
	}
	if role != "assistant" && (message.ReasoningContent != nil || message.ThinkingContent != nil || message.Reasoning != nil) {
		return fmt.Errorf("messages[%d].reasoning_content is only supported on assistant messages by Ollama Chat", messageIndex)
	}
	return nil
}

func normalizeOllamaToolParameters(parameters any) (map[string]any, error) {
	if parameters == nil {
		return map[string]any{"type": "object"}, nil
	}
	var schema map[string]any
	data, err := common.Marshal(parameters)
	if err != nil {
		return nil, err
	}
	if err := common.Unmarshal(data, &schema); err != nil {
		return nil, err
	}
	if schema == nil {
		return nil, fmt.Errorf("schema must be a JSON object")
	}
	if schemaType, ok := schema["type"].(string); ok && schemaType != "object" {
		return nil, fmt.Errorf("schema.type must be object")
	}
	if _, ok := schema["type"]; !ok {
		schema["type"] = "object"
	}
	return schema, nil
}

func normalizeOllamaResponseFormat(format *dto.ResponseFormat, mode string) (any, error) {
	if format == nil {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(format.Type)) {
	case "", "text":
		return nil, nil
	case "json", "json_object":
		return "json", nil
	case "json_schema":
		if len(format.JsonSchema) == 0 {
			return nil, fmt.Errorf("response_format.json_schema is required")
		}
		var wrapper dto.FormatJsonSchema
		if err := common.Unmarshal(format.JsonSchema, &wrapper); err != nil {
			return nil, fmt.Errorf("response_format.json_schema is invalid: %w", err)
		}
		if len(wrapper.Strict) > 0 && common.GetJsonType(wrapper.Strict) != "null" {
			var strict bool
			if err := common.Unmarshal(wrapper.Strict, &strict); err != nil {
				return nil, fmt.Errorf("response_format.json_schema.strict must be boolean: %w", err)
			}
			if strict {
				return nil, fmt.Errorf("response_format.json_schema.strict=true cannot be represented by Ollama %s", mode)
			}
		}
		if wrapper.Schema != nil {
			schema, err := normalizeOllamaToolParameters(wrapper.Schema)
			if err != nil {
				return nil, fmt.Errorf("response_format.json_schema.schema: %w", err)
			}
			return schema, nil
		}
		var schema map[string]any
		if err := common.Unmarshal(format.JsonSchema, &schema); err != nil {
			return nil, fmt.Errorf("response_format.json_schema is invalid: %w", err)
		}
		if schema == nil {
			return nil, fmt.Errorf("response_format.json_schema must be a JSON object")
		}
		return normalizeOllamaToolParameters(schema)
	default:
		return nil, fmt.Errorf("response_format.type %q is not supported by Ollama %s", format.Type, mode)
	}
}

func validateOllamaThink(think []byte) error {
	if len(think) == 0 || common.GetJsonType(think) == "null" {
		return nil
	}
	switch common.GetJsonType(think) {
	case "boolean":
		return nil
	case "string":
		var level string
		if err := common.Unmarshal(think, &level); err != nil {
			return fmt.Errorf("think must be boolean or one of low, medium, high: %w", err)
		}
		switch strings.ToLower(strings.TrimSpace(level)) {
		case "low", "medium", "high":
			return nil
		default:
			return fmt.Errorf("think level %q is not supported by Ollama", level)
		}
	default:
		return fmt.Errorf("think must be boolean or one of low, medium, high")
	}
}

func addOllamaSamplingOptions(options map[string]any, request dto.GeneralOpenAIRequest) error {
	if request.Temperature != nil {
		options["temperature"] = *request.Temperature
	}
	if request.TopP != nil {
		options["top_p"] = *request.TopP
	}
	if request.TopK != nil {
		options["top_k"] = *request.TopK
	}
	if request.FrequencyPenalty != nil {
		options["frequency_penalty"] = *request.FrequencyPenalty
	}
	if request.PresencePenalty != nil {
		options["presence_penalty"] = *request.PresencePenalty
	}
	if request.Seed != nil {
		options["seed"] = int(*request.Seed)
	}
	if request.MaxTokens != nil && request.MaxCompletionTokens != nil && *request.MaxTokens != *request.MaxCompletionTokens {
		return fmt.Errorf("max_tokens and max_completion_tokens disagree")
	}
	if request.MaxCompletionTokens != nil {
		options["num_predict"] = int(*request.MaxCompletionTokens)
	} else if request.MaxTokens != nil {
		options["num_predict"] = int(*request.MaxTokens)
	}
	if request.Stop == nil {
		return nil
	}
	stop, err := normalizeOllamaStop(request.Stop)
	if err != nil {
		return err
	}
	options["stop"] = stop
	return nil
}

func normalizeOllamaStop(value any) ([]string, error) {
	appendStop := func(result []string, index int, stop string) ([]string, error) {
		if stop == "" {
			return nil, fmt.Errorf("stop[%d] must not be empty", index)
		}
		return append(result, stop), nil
	}
	switch value := value.(type) {
	case string:
		result, err := appendStop(nil, 0, value)
		if err != nil {
			return nil, fmt.Errorf("stop: %w", err)
		}
		return result, nil
	case []string:
		result := make([]string, 0, len(value))
		for index, stop := range value {
			var err error
			result, err = appendStop(result, index, stop)
			if err != nil {
				return nil, err
			}
		}
		return result, nil
	case []any:
		result := make([]string, 0, len(value))
		for index, item := range value {
			stop, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("stop[%d] must be a string", index)
			}
			var err error
			result, err = appendStop(result, index, stop)
			if err != nil {
				return nil, err
			}
		}
		return result, nil
	default:
		return nil, fmt.Errorf("stop must be a string or string array")
	}
}

// openAIToGenerate converts OpenAI completions request to Ollama generate
func openAIToGenerate(c *gin.Context, r *dto.GeneralOpenAIRequest) (*OllamaGenerateRequest, error) {
	if r == nil {
		return nil, fmt.Errorf("request is nil")
	}
	if err := validateOllamaGenerateRequest(*r); err != nil {
		return nil, err
	}
	gen := &OllamaGenerateRequest{
		Model:   r.Model,
		Stream:  lo.FromPtrOr(r.Stream, false),
		Options: map[string]any{},
		Think:   r.Think,
	}
	// Prompt may be in r.Prompt (string or []any)
	if r.Prompt != nil {
		switch v := r.Prompt.(type) {
		case string:
			gen.Prompt = v
		case []any:
			var sb strings.Builder
			for index, it := range v {
				if s, ok := it.(string); ok {
					sb.WriteString(s)
				} else {
					return nil, fmt.Errorf("prompt[%d] must be a string", index)
				}
			}
			gen.Prompt = sb.String()
		default:
			return nil, fmt.Errorf("prompt must be a string or string array")
		}
	}
	if r.Suffix != nil {
		if s, ok := r.Suffix.(string); ok {
			gen.Suffix = s
		} else {
			return nil, fmt.Errorf("suffix must be a string")
		}
	}
	if r.ResponseFormat != nil {
		format, err := normalizeOllamaResponseFormat(r.ResponseFormat, "Generate")
		if err != nil {
			return nil, err
		}
		gen.Format = format
	}
	if err := addOllamaSamplingOptions(gen.Options, *r); err != nil {
		return nil, err
	}
	return gen, nil
}

func validateOllamaGenerateRequest(r dto.GeneralOpenAIRequest) error {
	if len(r.Messages) > 0 {
		return fmt.Errorf("messages are not supported by Ollama Generate; use prompt")
	}
	if r.Input != nil || r.Instruction != "" {
		return fmt.Errorf("input/instruction fields are not supported by Ollama Generate")
	}
	if r.Prefix != nil {
		return fmt.Errorf("prefix is not supported by Ollama Generate")
	}
	if r.N != nil && *r.N != 1 {
		return fmt.Errorf("Ollama Generate supports only one completion (n=1)")
	}
	if len(r.Functions) > 0 || len(r.FunctionCall) > 0 || len(r.Tools) > 0 || r.ToolChoice != nil || r.ParallelTooCalls != nil {
		return fmt.Errorf("tools and function calling are not supported by Ollama Generate")
	}
	if r.ReasoningEffort != "" || len(r.Verbosity) > 0 || len(r.Reasoning) > 0 {
		return fmt.Errorf("reasoning_effort, verbosity, and reasoning are not supported by Ollama Generate; use think")
	}
	if len(r.Modalities) > 0 || len(r.Audio) > 0 {
		return fmt.Errorf("audio/modalities are not supported by Ollama Generate")
	}
	if len(r.ExtraBody) > 0 || len(r.SearchParameters) > 0 || r.WebSearchOptions != nil || len(r.WebSearch) > 0 ||
		len(r.EnableSearch) > 0 || len(r.SearchDomainFilter) > 0 || len(r.SearchRecencyFilter) > 0 || r.ReturnImages != nil ||
		r.ReturnRelatedQuestions != nil || len(r.SearchMode) > 0 {
		return fmt.Errorf("provider-specific search and extra fields are not supported by Ollama Generate")
	}
	if len(r.EncodingFormat) > 0 || r.Dimensions != nil || len(r.Size) > 0 {
		return fmt.Errorf("embedding/image fields are not supported by Ollama Generate")
	}
	if len(r.ServiceTier) > 0 || r.LogProbs != nil || r.TopLogProbs != nil || len(r.SafetyIdentifier) > 0 || len(r.Store) > 0 ||
		r.PromptCacheKey != "" || len(r.PromptCacheRetention) > 0 || len(r.LogitBias) > 0 || len(r.Metadata) > 0 || len(r.Prediction) > 0 ||
		len(r.VlHighResolutionImages) > 0 || len(r.EnableThinking) > 0 || len(r.ChatTemplateKwargs) > 0 || len(r.THINKING) > 0 || len(r.ReasoningSplit) > 0 {
		return fmt.Errorf("request fields are not supported by Ollama Generate")
	}
	return validateOllamaThink(r.Think)
}

func requestOpenAI2Embeddings(r dto.EmbeddingRequest) *OllamaEmbeddingRequest {
	opts := map[string]any{}
	if r.Temperature != nil {
		opts["temperature"] = *r.Temperature
	}
	if r.TopP != nil {
		opts["top_p"] = lo.FromPtr(r.TopP)
	}
	if r.FrequencyPenalty != nil {
		opts["frequency_penalty"] = lo.FromPtr(r.FrequencyPenalty)
	}
	if r.PresencePenalty != nil {
		opts["presence_penalty"] = lo.FromPtr(r.PresencePenalty)
	}
	if r.Seed != nil {
		opts["seed"] = int(lo.FromPtr(r.Seed))
	}
	dimensions := lo.FromPtrOr(r.Dimensions, 0)
	if r.Dimensions != nil {
		opts["dimensions"] = dimensions
	}
	input := r.ParseInput()
	if len(input) == 1 {
		return &OllamaEmbeddingRequest{Model: r.Model, Input: input[0], Options: opts, Dimensions: dimensions}
	}
	return &OllamaEmbeddingRequest{Model: r.Model, Input: input, Options: opts, Dimensions: dimensions}
}

func ollamaEmbeddingHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("ollama embedding response is empty"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	var oResp OllamaEmbeddingResponse
	body, err := readOllamaResponseBody(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	service.CloseResponseBodyGracefully(resp)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, types.NewOpenAIError(fmt.Errorf("ollama returned HTTP %d: %s", resp.StatusCode, common.LocalUpstreamErrorPreview(string(body))), types.ErrorCodeBadResponseBody, resp.StatusCode)
	}
	if err = common.Unmarshal(body, &oResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oResp.Error != "" {
		return nil, types.NewOpenAIError(fmt.Errorf("ollama error: %s", oResp.Error), types.ErrorCodeBadResponseBody, resp.StatusCode)
	}
	data := make([]dto.OpenAIEmbeddingResponseItem, 0, len(oResp.Embeddings))
	for i, emb := range oResp.Embeddings {
		data = append(data, dto.OpenAIEmbeddingResponseItem{Index: i, Object: "embedding", Embedding: emb})
	}
	usage := &dto.Usage{PromptTokens: oResp.PromptEvalCount, CompletionTokens: 0, TotalTokens: oResp.PromptEvalCount}
	embResp := &dto.OpenAIEmbeddingResponse{Object: "list", Data: data, Model: info.UpstreamModelName, Usage: *usage}
	out, _ := common.Marshal(embResp)
	service.IOCopyBytesGracefully(c, resp, out)
	return usage, nil
}

func FetchOllamaModels(baseURL, apiKey string, extraHeaders ...http.Header) ([]OllamaModel, error) {
	url, err := ollamaAPIURL(baseURL, "/api/tags")
	if err != nil {
		return nil, err
	}

	client, err := getOllamaHTTPClient(30 * time.Second)
	if err != nil {
		return nil, err
	}
	request, cancel, err := newOllamaRequest(http.MethodGet, url, nil, 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}
	defer cancel()

	// Ollama 通常不需要 Bearer token，但为了兼容性保留
	if apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}
	applyFetchOllamaExtraHeaders(request, extraHeaders...)

	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, readErr := readOllamaResponseBody(response.Body)
		if readErr != nil {
			return nil, fmt.Errorf("读取错误响应失败: %v", readErr)
		}
		return nil, fmt.Errorf("服务器返回错误 %d: %s", response.StatusCode, common.LocalUpstreamErrorPreview(string(body)))
	}

	var tagsResponse OllamaTagsResponse
	body, err := readOllamaResponseBody(response.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}

	err = common.Unmarshal(body, &tagsResponse)
	if err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	return tagsResponse.Models, nil
}

func applyFetchOllamaExtraHeaders(request *http.Request, extraHeaders ...http.Header) {
	if request == nil {
		return
	}
	for _, headers := range extraHeaders {
		service.ApplyRuntimeRequestHeaders(request, headers)
	}
}

// 拉取 Ollama 模型 (非流式)
func PullOllamaModel(baseURL, apiKey, modelName string, extraHeaders ...http.Header) error {
	url, err := ollamaAPIURL(baseURL, "/api/pull")
	if err != nil {
		return err
	}

	pullRequest := OllamaPullRequest{
		Name:   modelName,
		Stream: false, // 非流式，简化处理
	}

	requestBody, err := common.Marshal(pullRequest)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %v", err)
	}

	client, err := getOllamaHTTPClient(30 * time.Minute)
	if err != nil {
		return err
	}
	request, cancel, err := newOllamaRequest(http.MethodPost, url, strings.NewReader(string(requestBody)), 30*time.Minute)
	if err != nil {
		return fmt.Errorf("创建请求失败: %v", err)
	}
	defer cancel()

	request.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}
	applyFetchOllamaExtraHeaders(request, extraHeaders...)

	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("请求失败: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := readOllamaResponseBody(response.Body)
		return fmt.Errorf("拉取模型失败 %d: %s", response.StatusCode, common.LocalUpstreamErrorPreview(string(body)))
	}

	return nil
}

// 流式拉取 Ollama 模型 (支持进度回调)
func PullOllamaModelStream(baseURL, apiKey, modelName string, progressCallback func(OllamaPullResponse), extraHeaders ...http.Header) error {
	url, err := ollamaAPIURL(baseURL, "/api/pull")
	if err != nil {
		return err
	}

	pullRequest := OllamaPullRequest{
		Name:   modelName,
		Stream: true, // 启用流式
	}

	requestBody, err := common.Marshal(pullRequest)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %v", err)
	}

	client, err := getOllamaHTTPClient(time.Hour)
	if err != nil {
		return err
	}
	request, cancel, err := newOllamaRequest(http.MethodPost, url, strings.NewReader(string(requestBody)), time.Hour)
	if err != nil {
		return fmt.Errorf("创建请求失败: %v", err)
	}
	defer cancel()

	request.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}
	applyFetchOllamaExtraHeaders(request, extraHeaders...)

	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("请求失败: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := readOllamaResponseBody(response.Body)
		return fmt.Errorf("拉取模型失败 %d: %s", response.StatusCode, common.LocalUpstreamErrorPreview(string(body)))
	}

	// 读取流式响应
	scanner := newOllamaLineScanner(response.Body)
	successful := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var pullResponse OllamaPullResponse
		if err := common.Unmarshal([]byte(line), &pullResponse); err != nil {
			return fmt.Errorf("解析流式响应失败: %w", err)
		}

		if progressCallback != nil {
			progressCallback(pullResponse)
		}

		// 检查是否出现错误或完成
		if strings.EqualFold(pullResponse.Status, "error") {
			return fmt.Errorf("拉取模型失败: %s", strings.TrimSpace(line))
		}
		if strings.EqualFold(pullResponse.Status, "success") {
			successful = true
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("读取流式响应失败: %v", err)
	}

	if !successful {
		return fmt.Errorf("拉取模型未完成: 未收到成功状态")
	}

	return nil
}

// 删除 Ollama 模型
func DeleteOllamaModel(baseURL, apiKey, modelName string, extraHeaders ...http.Header) error {
	url, err := ollamaAPIURL(baseURL, "/api/delete")
	if err != nil {
		return err
	}

	deleteRequest := OllamaDeleteRequest{
		Name: modelName,
	}

	requestBody, err := common.Marshal(deleteRequest)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %v", err)
	}

	client, err := getOllamaHTTPClient(30 * time.Second)
	if err != nil {
		return err
	}
	request, cancel, err := newOllamaRequest(http.MethodDelete, url, strings.NewReader(string(requestBody)), 30*time.Second)
	if err != nil {
		return fmt.Errorf("创建请求失败: %v", err)
	}
	defer cancel()

	request.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}
	applyFetchOllamaExtraHeaders(request, extraHeaders...)

	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("请求失败: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := readOllamaResponseBody(response.Body)
		return fmt.Errorf("删除模型失败 %d: %s", response.StatusCode, common.LocalUpstreamErrorPreview(string(body)))
	}

	return nil
}

func FetchOllamaVersion(baseURL, apiKey string, extraHeaders ...http.Header) (string, error) {
	url, err := ollamaAPIURL(baseURL, "/api/version")
	if err != nil {
		return "", err
	}

	client, err := getOllamaHTTPClient(10 * time.Second)
	if err != nil {
		return "", err
	}
	request, cancel, err := newOllamaRequest(http.MethodGet, url, nil, 10*time.Second)
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %v", err)
	}
	defer cancel()

	if apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}
	applyFetchOllamaExtraHeaders(request, extraHeaders...)

	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("请求失败: %v", err)
	}
	defer response.Body.Close()

	body, err := readOllamaResponseBody(response.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %v", err)
	}

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("查询版本失败 %d: %s", response.StatusCode, common.LocalUpstreamErrorPreview(string(body)))
	}

	var versionResp struct {
		Version string `json:"version"`
	}

	if err := common.Unmarshal(body, &versionResp); err != nil {
		return "", fmt.Errorf("解析响应失败: %v", err)
	}

	if versionResp.Version == "" {
		return "", fmt.Errorf("未返回版本信息")
	}

	return versionResp.Version, nil
}

const maxOllamaResponseBodyBytes int64 = 8 << 20

func getOllamaHTTPClient(operationTimeout time.Duration) (*http.Client, error) {
	client, err := service.GetHttpClientWithProxy("")
	if err != nil {
		return nil, fmt.Errorf("获取 Ollama HTTP 客户端失败: %w", err)
	}
	if client == nil {
		return nil, fmt.Errorf("Ollama HTTP 客户端尚未初始化")
	}
	clientCopy := *client
	if operationTimeout > 0 && clientCopy.Timeout > 0 && clientCopy.Timeout < operationTimeout {
		clientCopy.Timeout = operationTimeout
	}
	return &clientCopy, nil
}

func newOllamaRequest(method, url string, body io.Reader, timeout time.Duration) (*http.Request, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	request, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return request, cancel, nil
}

func readOllamaResponseBody(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maxOllamaResponseBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxOllamaResponseBodyBytes {
		return nil, fmt.Errorf("Ollama 响应体超过 %d 字节限制", maxOllamaResponseBodyBytes)
	}
	return body, nil
}

// ollamaAPIURL joins the channel base URL and an Ollama endpoint without
// allowing a trailing slash in the configured base URL to create a different
// upstream path. Keeping this in one helper makes model discovery, pull,
// delete, version, and relay requests use the same URL semantics.
func ollamaAPIURL(baseURL, endpoint string) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", fmt.Errorf("baseURL 为空")
	}
	endpoint = "/" + strings.TrimLeft(strings.TrimSpace(endpoint), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		if err == nil {
			err = fmt.Errorf("URL 缺少 scheme 或 host")
		}
		return "", fmt.Errorf("baseURL 无效: %w", err)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + endpoint
	return parsed.String(), nil
}
