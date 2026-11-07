package claude

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel/openrouter"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relay/reasonmap"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/reasoning"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	WebSearchMaxUsesLow    = 1
	WebSearchMaxUsesMedium = 5
	WebSearchMaxUsesHigh   = 10
)

func stopReasonClaude2OpenAI(reason string) string {
	return reasonmap.ClaudeStopReasonToOpenAIFinishReason(reason)
}

func stopReasonClaude2OpenAIStrict(reason string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(reason))
	switch normalized {
	case "":
		return "stop", nil
	case "end_turn", "stop_sequence", "max_tokens", "tool_use", "refusal":
		return reasonmap.ClaudeStopReasonToOpenAIFinishReason(normalized), nil
	case "pause_turn":
		// Anthropic uses pause_turn for a resumable agent turn. Chat Completions
		// has no equivalent lifecycle state; stop is the least lossy terminal
		// representation and lets the client decide whether to continue.
		return constant.FinishReasonStop, nil
	case "model_context_window_exceeded":
		return constant.FinishReasonLength, nil
	default:
		return "", fmt.Errorf("Claude stop_reason %q is not supported by OpenAI Chat Completions", reason)
	}
}

func maybeMarkClaudeRefusal(c *gin.Context, stopReason string) {
	if c == nil {
		return
	}
	if strings.EqualFold(stopReason, "refusal") {
		common.SetContextKey(c, constant.ContextKeyAdminRejectReason, "claude_stop_reason=refusal")
	}
}

func buildClaudeMediaMessageFromFile(
	c *gin.Context,
	mediaMessage dto.MediaContent,
) (*dto.ClaudeMediaMessage, bool, error) {
	file := mediaMessage.GetFile()
	if file == nil {
		return nil, true, errors.New("Claude file content is missing file metadata")
	}
	if file.FileData == "" {
		if file.FileId == "" && file.FileUrl == "" {
			return nil, true, errors.New("Claude file content requires file_data, file_id, or file_url")
		}
		return nil, false, nil
	}

	mimeType, err := resolveClaudeFileMimeType(file)
	if err != nil {
		return nil, true, err
	}

	source := types.NewFileSourceFromData(file.FileData, mimeType)
	base64Data, normalizedMimeType, err := service.GetBase64Data(
		c,
		source,
		"formatting file for Claude",
	)
	if err != nil {
		return nil, true, fmt.Errorf("get file data failed: %w", err)
	}
	normalizedMimeType = normalizeClaudeMimeType(normalizedMimeType)
	if normalizedMimeType == "" || normalizedMimeType == "application/octet-stream" {
		return nil, true, fmt.Errorf("cannot determine MIME type for Claude file %q", file.FileName)
	}

	if strings.HasPrefix(normalizedMimeType, "text/") {
		decoded, err := base64.StdEncoding.DecodeString(base64Data)
		if err != nil {
			return nil, true, fmt.Errorf("decode text file failed: %w", err)
		}
		text := string(decoded)
		return &dto.ClaudeMediaMessage{
			Type: "text",
			Text: common.GetPointer[string](text),
		}, true, nil
	}

	claudeMediaMessage := &dto.ClaudeMediaMessage{
		Source: &dto.ClaudeMessageSource{
			Type:      "base64",
			MediaType: normalizedMimeType,
			Data:      base64Data,
		},
	}
	if strings.HasPrefix(normalizedMimeType, "application/pdf") {
		claudeMediaMessage.Type = "document"
		return claudeMediaMessage, true, nil
	}
	if strings.HasPrefix(normalizedMimeType, "image/") {
		claudeMediaMessage.Type = "image"
		return claudeMediaMessage, true, nil
	}

	return nil, true, fmt.Errorf("Claude does not support file MIME type %q", normalizedMimeType)
}

func resolveClaudeFileMimeType(file *dto.MessageFile) (string, error) {
	if file == nil {
		return "", errors.New("Claude file content is missing file metadata")
	}
	if mimeType := normalizeClaudeMimeType(file.MimeType); mimeType != "" && mimeType != "application/octet-stream" {
		return mimeType, nil
	}

	if ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(file.FileName)), "."); ext != "" {
		if mimeType := normalizeClaudeMimeType(service.GetMimeTypeByExtension(ext)); mimeType != "" && mimeType != "application/octet-stream" {
			return mimeType, nil
		}
	}

	if file.FileData != "" {
		encoded := file.FileData
		if comma := strings.Index(encoded, ","); comma >= 0 && strings.HasPrefix(strings.ToLower(strings.TrimSpace(encoded[:comma])), "data:") {
			encoded = encoded[comma+1:]
		}
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return "", fmt.Errorf("decode Claude file data for MIME detection failed: %w", err)
		}
		if mimeType := normalizeClaudeMimeType(http.DetectContentType(decoded)); mimeType != "" && mimeType != "application/octet-stream" {
			return mimeType, nil
		}
	}

	return "", fmt.Errorf("cannot determine MIME type for Claude file %q", file.FileName)
}

func normalizeClaudeMimeType(mimeType string) string {
	if index := strings.Index(mimeType, ";"); index >= 0 {
		mimeType = mimeType[:index]
	}
	return strings.ToLower(strings.TrimSpace(mimeType))
}

func buildClaudeMediaMessageFromSource(c *gin.Context, source types.FileSource, context string) (*dto.ClaudeMediaMessage, error) {
	if source == nil {
		return nil, fmt.Errorf("%s has no file source", context)
	}
	base64Data, mimeType, err := service.GetBase64Data(c, source, context)
	if err != nil {
		return nil, fmt.Errorf("get file data failed: %w", err)
	}
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	if mimeType == "" {
		return nil, fmt.Errorf("%s has no MIME type", context)
	}
	if strings.HasPrefix(mimeType, "text/") {
		decoded, err := base64.StdEncoding.DecodeString(base64Data)
		if err != nil {
			return nil, fmt.Errorf("decode text file failed: %w", err)
		}
		return &dto.ClaudeMediaMessage{Type: "text", Text: common.GetPointer[string](string(decoded))}, nil
	}
	if strings.HasPrefix(mimeType, "image/") {
		return &dto.ClaudeMediaMessage{
			Type:   "image",
			Source: &dto.ClaudeMessageSource{Type: "base64", MediaType: mimeType, Data: base64Data},
		}, nil
	}
	if mimeType == "application/pdf" {
		return &dto.ClaudeMediaMessage{
			Type:   "document",
			Source: &dto.ClaudeMessageSource{Type: "base64", MediaType: mimeType, Data: base64Data},
		}, nil
	}
	return nil, fmt.Errorf("Claude does not support MIME type %q", mimeType)
}

func hasNonEmptyToolCalls(message dto.Message) bool {
	toolCalls, err := message.ParseToolCallsStrict()
	return err == nil && len(toolCalls) > 0
}

func claudeUserContentBlocks(content any) ([]dto.ClaudeMediaMessage, error) {
	switch value := content.(type) {
	case nil:
		return make([]dto.ClaudeMediaMessage, 0), nil
	case string:
		return []dto.ClaudeMediaMessage{{
			Type: "text",
			Text: common.GetPointer(value),
		}}, nil
	case []dto.ClaudeMediaMessage:
		return append([]dto.ClaudeMediaMessage(nil), value...), nil
	case []any:
		blocks, err := common.Any2Type[[]dto.ClaudeMediaMessage](value)
		if err != nil {
			return nil, err
		}
		return blocks, nil
	default:
		return nil, fmt.Errorf("unsupported content type %T", content)
	}
}

// normalizeClaudeInputSchema 把工具参数规范成可直接下发上游的 JSON Schema。
// 返回值是原始字节而非 map：JSON 编码 map 会按字典序重排 properties，而
// structured output 按 schema 里列出的顺序填充字段，重排会改变模型行为。
func normalizeClaudeInputSchema(parameters any, toolName string) (json.RawMessage, error) {
	if parameters == nil {
		return json.RawMessage(`{"type":"object"}`), nil
	}

	var raw json.RawMessage
	switch typed := parameters.(type) {
	case json.RawMessage:
		raw = typed
	case []byte:
		raw = typed
	default:
		encoded, err := common.Marshal(parameters)
		if err != nil {
			return nil, fmt.Errorf("tool %s parameters must be a JSON object: %w", toolName, err)
		}
		raw = encoded
	}

	if common.GetJsonType(raw) != "object" {
		return nil, fmt.Errorf("tool %s parameters must be a JSON object", toolName)
	}

	// 只在完全缺少 type 时补 "type":"object"。已有 type 必须原样是字符串
	// "object"：null 或 array/string 之类既不是合法的工具参数 schema，也不能
	// 靠文本拼接"覆盖"——拼出来的 {"type":"object","type":null,...} 是重复键，
	// JSON 解码取后者，客户端显式写的值反而胜出。
	var probe struct {
		Type json.RawMessage `json:"type"`
	}
	if err := common.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("tool %s parameters must be a JSON object: %w", toolName, err)
	}
	if duplicates, err := countTopLevelKey(raw, "type"); err != nil {
		return nil, fmt.Errorf("tool %s parameters must be a JSON object: %w", toolName, err)
	} else if duplicates > 1 {
		return nil, fmt.Errorf("tool %s parameters.type must appear exactly once", toolName)
	}
	if len(probe.Type) == 0 {
		return injectSchemaObjectType(raw, toolName)
	}
	if common.GetJsonType(probe.Type) != "string" {
		return nil, fmt.Errorf("tool %s parameters.type must be a string, got %s",
			toolName, common.GetJsonType(probe.Type))
	}
	var typeName string
	if err := common.Unmarshal(probe.Type, &typeName); err != nil {
		return nil, fmt.Errorf("tool %s parameters.type must be a string: %w", toolName, err)
	}
	if typeName != "object" {
		return nil, fmt.Errorf("tool %s parameters.type must be \"object\", got %q", toolName, typeName)
	}
	return raw, nil
}

// countTopLevelKey 统计 JSON 对象顶层出现指定键的次数。重复键本身就让文档
// 有歧义（不同解析器取首次或末次），所以补 type 前后都要能发现它。
func countTopLevelKey(raw json.RawMessage, key string) (int, error) {
	decoder := common.NewJsonDecoderUseNumber(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return 0, err
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return 0, errors.New("schema must be a JSON object")
	}
	count := 0
	depth := 0
	for decoder.More() || depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			return 0, err
		}
		switch value := token.(type) {
		case json.Delim:
			if value == '{' || value == '[' {
				depth++
			} else {
				depth--
			}
		case string:
			if depth == 0 && value == key {
				count++
				// 跳过该键的值，避免把值里的同名键也数进来。
				var skip json.RawMessage
				if err := decoder.Decode(&skip); err != nil {
					return 0, err
				}
			}
		}
	}
	return count, nil
}

// injectSchemaObjectType 在保留原始字节顺序的前提下，向 schema 对象头部插入
// "type":"object"。走 map 再编码会丢失 properties 顺序，这里只做文本拼接；
// 调用方已确认原文没有 type 键，因此拼出来的文档不会出现重复键。
func injectSchemaObjectType(raw json.RawMessage, toolName string) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "{}" {
		return json.RawMessage(`{"type":"object"}`), nil
	}
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return nil, fmt.Errorf("tool %s parameters must be a JSON object", toolName)
	}
	body := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
	if body == "" {
		return json.RawMessage(`{"type":"object"}`), nil
	}
	return json.RawMessage(`{"type":"object",` + body + `}`), nil
}

// claudeOutputFormatFromResponseFormat maps an OpenAI response_format onto the
// Claude equivalent: output_config.format, whose schema is the raw JSON Schema.
// Claude has no OpenAI-style name/strict envelope, so the wrapper is unwrapped
// rather than forwarded. A text format produces no constraint at all.
func claudeOutputFormatFromResponseFormat(format *dto.ResponseFormat) (map[string]any, error) {
	formatType := strings.ToLower(strings.TrimSpace(format.Type))
	switch formatType {
	case "", "text":
		return nil, nil
	case "json_object":
		// Claude has no schema-free JSON mode, so accept any JSON object.
		return map[string]any{
			"type":   "json_schema",
			"schema": map[string]any{"type": "object"},
		}, nil
	case "json_schema":
		if len(format.JsonSchema) == 0 {
			return nil, errors.New("response_format.json_schema is required")
		}
		var wrapper dto.FormatJsonSchema
		if err := common.Unmarshal(format.JsonSchema, &wrapper); err != nil {
			return nil, fmt.Errorf("response_format.json_schema is invalid: %w", err)
		}
		schema, err := normalizeClaudeInputSchema(wrapper.Schema, "response_format.json_schema")
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"type":   "json_schema",
			"schema": schema,
		}, nil
	default:
		return nil, fmt.Errorf("response_format.type %q is not supported by Claude", format.Type)
	}
}

// mergeClaudeOutputConfig folds a format into the existing output_config so it
// survives alongside the reasoning effort, which is written to the same object.
func mergeClaudeOutputConfig(existing json.RawMessage, format map[string]any) (json.RawMessage, error) {
	if format == nil {
		return existing, nil
	}
	merged := map[string]any{}
	if len(existing) > 0 {
		if err := common.Unmarshal(existing, &merged); err != nil {
			return nil, fmt.Errorf("output_config is invalid: %w", err)
		}
	}
	merged["format"] = format
	encoded, err := common.Marshal(merged)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(encoded), nil
}

func RequestOpenAI2ClaudeMessage(c *gin.Context, textRequest dto.GeneralOpenAIRequest) (*dto.ClaudeRequest, error) {
	// The Claude Messages API returns exactly one completion. Dropping n>1
	// silently made the gateway answer with a single choice for a request that
	// asked for several, so the mismatch is reported instead. Other adaptors
	// that cannot represent multiple choices reject it the same way.
	if textRequest.N != nil && *textRequest.N > 1 {
		return nil, fmt.Errorf("n>1 is not supported by the Claude Messages API")
	}

	claudeTools := make([]any, 0, len(textRequest.Tools))

	// 一次转换取一份配置快照，避免 max_tokens 与 thinking 预算读到两代配置。
	claudeSettings := model_setting.ClaudeSettingsSnapshot()

	for _, tool := range textRequest.Tools {
		toolType := strings.TrimSpace(tool.Type)
		if toolType != "" && toolType != "function" {
			return nil, fmt.Errorf("tool type %q is not supported by Claude", tool.Type)
		}
		toolName := strings.TrimSpace(tool.Function.Name)
		if toolName == "" {
			return nil, errors.New("tool name is required")
		}
		inputSchema, err := normalizeClaudeInputSchema(tool.Function.Parameters, toolName)
		if err != nil {
			return nil, err
		}
		claudeTools = append(claudeTools, &dto.Tool{
			Name:        toolName,
			Description: tool.Function.Description,
			InputSchema: inputSchema,
		})
	}

	// Web search tool
	// https://docs.anthropic.com/en/docs/agents-and-tools/tool-use/web-search-tool
	if textRequest.WebSearchOptions != nil {
		webSearchTool := dto.ClaudeWebSearchTool{
			Type: "web_search_20250305",
			Name: "web_search",
		}

		// 处理 user_location
		if textRequest.WebSearchOptions.UserLocation != nil {
			anthropicUserLocation := &dto.ClaudeWebSearchUserLocation{
				Type: "approximate", // 固定为 "approximate"
			}

			// 解析 UserLocation JSON
			var userLocationMap map[string]interface{}
			if err := common.Unmarshal(textRequest.WebSearchOptions.UserLocation, &userLocationMap); err != nil {
				return nil, fmt.Errorf("web_search_options.user_location must be a JSON object: %w", err)
			}
			// 检查是否有 approximate 字段
			if approximateData, ok := userLocationMap["approximate"].(map[string]interface{}); ok {
				if timezone, ok := approximateData["timezone"].(string); ok && timezone != "" {
					anthropicUserLocation.Timezone = timezone
				}
				if country, ok := approximateData["country"].(string); ok && country != "" {
					anthropicUserLocation.Country = country
				}
				if region, ok := approximateData["region"].(string); ok && region != "" {
					anthropicUserLocation.Region = region
				}
				if city, ok := approximateData["city"].(string); ok && city != "" {
					anthropicUserLocation.City = city
				}
			}

			webSearchTool.UserLocation = anthropicUserLocation
		}

		// 处理 search_context_size 转换为 max_uses
		if textRequest.WebSearchOptions.SearchContextSize != "" {
			switch textRequest.WebSearchOptions.SearchContextSize {
			case "low":
				webSearchTool.MaxUses = WebSearchMaxUsesLow
			case "medium":
				webSearchTool.MaxUses = WebSearchMaxUsesMedium
			case "high":
				webSearchTool.MaxUses = WebSearchMaxUsesHigh
			}
		}

		claudeTools = append(claudeTools, &webSearchTool)
	}

	claudeRequest := dto.ClaudeRequest{
		Model:         textRequest.Model,
		StopSequences: nil,
		Temperature:   textRequest.Temperature,
		Tools:         claudeTools,
	}
	if maxTokens := textRequest.GetMaxTokens(); maxTokens > 0 {
		claudeRequest.MaxTokens = common.GetPointer(maxTokens)
	}
	if textRequest.TopP != nil {
		claudeRequest.TopP = common.GetPointer(*textRequest.TopP)
	}
	if textRequest.TopK != nil {
		claudeRequest.TopK = common.GetPointer(*textRequest.TopK)
	}
	if textRequest.IsStream(nil) {
		claudeRequest.Stream = common.GetPointer(true)
	}

	// 处理 tool_choice 和 parallel_tool_calls
	if textRequest.ToolChoice != nil || textRequest.ParallelTooCalls != nil {
		claudeToolChoice, choiceErr := mapToolChoice(textRequest.ToolChoice, textRequest.ParallelTooCalls)
		if choiceErr != nil {
			return nil, choiceErr
		}
		if claudeToolChoice != nil {
			claudeRequest.ToolChoice = claudeToolChoice
		}
	}

	if claudeRequest.MaxTokens == nil || *claudeRequest.MaxTokens == 0 {
		defaultMaxTokens := uint(claudeSettings.GetDefaultMaxTokens(textRequest.Model))
		claudeRequest.MaxTokens = &defaultMaxTokens
	}

	if baseModel, effortLevel, ok := reasoning.TrimEffortSuffix(textRequest.Model); ok && effortLevel != "" &&
		(strings.HasPrefix(textRequest.Model, "claude-opus-4-6") || strings.HasPrefix(textRequest.Model, "claude-opus-4-7")) {
		claudeRequest.Model = baseModel
		claudeRequest.Thinking = &dto.Thinking{
			Type: "adaptive",
		}
		claudeRequest.OutputConfig = json.RawMessage(fmt.Sprintf(`{"effort":"%s"}`, effortLevel))
		if strings.HasPrefix(baseModel, "claude-opus-4-7") {
			// Opus 4.7 rejects non-default temperature/top_p/top_k with 400
			// and defaults display to "omitted"; restore the 4.6 visible summary.
			claudeRequest.Thinking.Display = "summarized"
			claudeRequest.Temperature = nil
			claudeRequest.TopP = nil
			claudeRequest.TopK = nil
		} else {
			claudeRequest.TopP = nil
			claudeRequest.Temperature = common.GetPointer[float64](1.0)
		}
	} else if claudeSettings.ThinkingAdapterEnabled &&
		strings.HasSuffix(textRequest.Model, "-thinking") {

		trimmedModel := strings.TrimSuffix(textRequest.Model, "-thinking")
		if strings.HasPrefix(trimmedModel, "claude-opus-4-7") {
			// Opus 4.7 rejects thinking.type="enabled"; use adaptive at high effort.
			claudeRequest.Thinking = &dto.Thinking{Type: "adaptive", Display: "summarized"}
			claudeRequest.OutputConfig = json.RawMessage(`{"effort":"high"}`)
			claudeRequest.Temperature = nil
			claudeRequest.TopP = nil
			claudeRequest.TopK = nil
		} else {
			// 因为BudgetTokens 必须大于1024
			if claudeRequest.MaxTokens == nil || *claudeRequest.MaxTokens < 1280 {
				claudeRequest.MaxTokens = common.GetPointer[uint](1280)
			}

			// BudgetTokens 为 max_tokens 的 80%
			claudeRequest.Thinking = &dto.Thinking{
				Type:         "enabled",
				BudgetTokens: common.GetPointer[int](int(float64(*claudeRequest.MaxTokens) * claudeSettings.ThinkingAdapterBudgetTokensPercentage)),
			}
			// Extended thinking requires default sampling knobs except temperature=1.0.
			// See Anthropic extended-thinking important considerations.
			claudeRequest.TopP = nil
			claudeRequest.Temperature = common.GetPointer[float64](1.0)
		}
		if !model_setting.ShouldPreserveThinkingSuffix(textRequest.Model) {
			claudeRequest.Model = trimmedModel
		}
	}

	if textRequest.ReasoningEffort != "" {
		switch strings.ToLower(strings.TrimSpace(textRequest.ReasoningEffort)) {
		case "low":
			claudeRequest.Thinking = &dto.Thinking{
				Type:         "enabled",
				BudgetTokens: common.GetPointer[int](1280),
			}
		case "medium":
			claudeRequest.Thinking = &dto.Thinking{
				Type:         "enabled",
				BudgetTokens: common.GetPointer[int](2048),
			}
		case "high":
			claudeRequest.Thinking = &dto.Thinking{
				Type:         "enabled",
				BudgetTokens: common.GetPointer[int](4096),
			}
		default:
			return nil, fmt.Errorf("reasoning_effort %q is not supported by Claude", textRequest.ReasoningEffort)
		}
	}

	// 指定了 reasoning 参数,覆盖 budgetTokens
	if textRequest.Reasoning != nil {
		var reasoning openrouter.RequestReasoning
		if err := common.Unmarshal(textRequest.Reasoning, &reasoning); err != nil {
			return nil, err
		}

		budgetTokens := reasoning.MaxTokens
		if budgetTokens > 0 {
			claudeRequest.Thinking = &dto.Thinking{
				Type:         "enabled",
				BudgetTokens: &budgetTokens,
			}
		}
	}

	if textRequest.Stop != nil {
		// stop maybe string/array string, convert to array string
		switch stop := textRequest.Stop.(type) {
		case string:
			claudeRequest.StopSequences = []string{stop}
		case []string:
			claudeRequest.StopSequences = append([]string(nil), stop...)
		case []interface{}:
			stopSequences := make([]string, 0, len(stop))
			for index, item := range stop {
				value, ok := item.(string)
				if !ok {
					return nil, fmt.Errorf("stop[%d] must be a string", index)
				}
				stopSequences = append(stopSequences, value)
			}
			claudeRequest.StopSequences = stopSequences
		default:
			return nil, fmt.Errorf("stop must be a string or an array of strings")
		}
	}
	formatMessages := make([]dto.Message, 0)
	lastMessage := dto.Message{
		Role: "tool",
	}
	for i, message := range textRequest.Messages {
		if _, err := message.ParseToolCallsStrict(); err != nil {
			return nil, fmt.Errorf("messages[%d]: %w", i, err)
		}
		if message.Role == "" {
			textRequest.Messages[i].Role = "user"
			message.Role = "user"
		}
		if message.Role == "function" {
			message.Role = "tool"
		}
		if message.Role != "system" && message.Role != "developer" &&
			message.Role != "user" && message.Role != "assistant" && message.Role != "tool" {
			return nil, fmt.Errorf("message role %q is not supported by Claude", message.Role)
		}
		fmtMessage := dto.Message{
			Role:             message.Role,
			Content:          message.Content,
			ReasoningContent: message.ReasoningContent,
			ThinkingContent:  message.ThinkingContent,
			Reasoning:        message.Reasoning,
		}
		if message.Role == "tool" {
			fmtMessage.ToolCallId = message.ToolCallId
		}
		if message.Role == "assistant" && hasNonEmptyToolCalls(message) {
			fmtMessage.ToolCalls = message.ToolCalls
		}
		hasReasoningOrToolCalls := fmtMessage.Role == "assistant" &&
			(fmtMessage.GetReasoningContent() != "" || hasNonEmptyToolCalls(fmtMessage))
		lastHasReasoningOrToolCalls := lastMessage.Role == "assistant" &&
			(lastMessage.GetReasoningContent() != "" || hasNonEmptyToolCalls(lastMessage))
		if lastMessage.Role == message.Role &&
			lastMessage.Role != "tool" &&
			!hasReasoningOrToolCalls &&
			!lastHasReasoningOrToolCalls {
			if lastMessage.IsStringContent() && message.IsStringContent() {
				fmtMessage.SetStringContent(strings.Trim(fmt.Sprintf("%s %s", lastMessage.StringContent(), message.StringContent()), "\""))
				// delete last message
				formatMessages = formatMessages[:len(formatMessages)-1]
			}
		}
		if (fmtMessage.Content == nil || (fmtMessage.IsStringContent() && fmtMessage.StringContent() == "")) &&
			!hasReasoningOrToolCalls {
			fmtMessage.SetStringContent("...")
		}
		formatMessages = append(formatMessages, fmtMessage)
		lastMessage = fmtMessage
	}

	claudeMessages := make([]dto.ClaudeMessage, 0)
	isFirstMessage := true
	// 初始化system消息数组，用于累积多个system消息
	var systemMessages []dto.ClaudeMediaMessage

	for _, message := range formatMessages {
		if message.Role == "system" || message.Role == "developer" {
			// 根据Claude API规范，system字段使用数组格式更有通用性
			if message.IsStringContent() {
				if text := message.StringContent(); text != "" {
					systemMessages = append(systemMessages, dto.ClaudeMediaMessage{
						Type: "text",
						Text: common.GetPointer[string](text),
					})
				}
			} else {
				// Claude system blocks are text-only. Do not silently drop media.
				content, err := message.ParseContentStrict()
				if err != nil {
					return nil, fmt.Errorf("Claude system content: %w", err)
				}
				for _, ctx := range content {
					if ctx.Type != dto.ContentTypeText {
						return nil, fmt.Errorf("Claude system content type %q is not supported", ctx.Type)
					}
					if ctx.Text != "" {
						systemMessages = append(systemMessages, dto.ClaudeMediaMessage{
							Type: "text",
							Text: common.GetPointer[string](ctx.Text),
						})
					}
				}
			}
		} else {
			if isFirstMessage {
				isFirstMessage = false
				if message.Role != "user" {
					// fix: first message is assistant, add user message
					claudeMessage := dto.ClaudeMessage{
						Role: "user",
						Content: []dto.ClaudeMediaMessage{
							{
								Type: "text",
								Text: common.GetPointer[string]("..."),
							},
						},
					}
					claudeMessages = append(claudeMessages, claudeMessage)
				}
			}
			claudeMessage := dto.ClaudeMessage{
				Role: message.Role,
			}
			hasAssistantReasoning := message.Role == "assistant" && message.GetReasoningContent() != ""
			if message.Role == "tool" {
				if strings.TrimSpace(message.ToolCallId) == "" {
					return nil, errors.New("tool message requires tool_call_id")
				}
				if len(claudeMessages) > 0 && claudeMessages[len(claudeMessages)-1].Role == "user" {
					lastMessage := claudeMessages[len(claudeMessages)-1]
					lastContent, err := claudeUserContentBlocks(lastMessage.Content)
					if err != nil {
						return nil, fmt.Errorf("cannot append tool result to previous user message: %w", err)
					}
					lastMessage.Content = append(lastContent, dto.ClaudeMediaMessage{
						Type:      "tool_result",
						ToolUseId: message.ToolCallId,
						Content:   message.Content,
					})
					claudeMessages[len(claudeMessages)-1] = lastMessage
					continue
				} else {
					claudeMessage.Role = "user"
					claudeMessage.Content = []dto.ClaudeMediaMessage{
						{
							Type:      "tool_result",
							ToolUseId: message.ToolCallId,
							Content:   message.Content,
						},
					}
				}
			} else if message.IsStringContent() && !hasNonEmptyToolCalls(message) && !hasAssistantReasoning {
				text := message.StringContent()
				if text == "" {
					text = "..."
				}
				claudeMessage.Content = text
			} else {
				claudeMediaMessages := make([]dto.ClaudeMediaMessage, 0)
				if message.Role == "assistant" {
					if reasoningContent := message.GetReasoningContent(); reasoningContent != "" {
						claudeMediaMessages = append(claudeMediaMessages, dto.ClaudeMediaMessage{
							Type:     "thinking",
							Thinking: common.GetPointer[string](reasoningContent),
						})
					} else if hasNonEmptyToolCalls(message) && service.IsDeepSeekModelName(textRequest.Model) {
						// OpenAI 协议客户端（Codex 等）的工具循环历史不带 reasoning，
						// 而 DeepSeek 的 Anthropic 端点要求 tool_use 轮次回传 thinking
						// 块。补一个空块满足该要求，不伪造任何推理文本。
						claudeMediaMessages = append(claudeMediaMessages, dto.ClaudeMediaMessage{
							Type:     "thinking",
							Thinking: common.GetPointer[string](""),
						})
					}
				}
				content, err := message.ParseContentStrict()
				if err != nil {
					return nil, fmt.Errorf("Claude message content: %w", err)
				}
				for _, mediaMessage := range content {
					switch mediaMessage.Type {
					case "text":
						if mediaMessage.Text != "" {
							claudeMediaMessages = append(claudeMediaMessages, dto.ClaudeMediaMessage{
								Type: "text",
								Text: common.GetPointer[string](mediaMessage.Text),
							})
						}
					case dto.ContentTypeFile:
						claudeMediaMessage, handled, err := buildClaudeMediaMessageFromFile(c, mediaMessage)
						if err != nil {
							return nil, err
						}
						if handled {
							if claudeMediaMessage != nil {
								claudeMediaMessages = append(claudeMediaMessages, *claudeMediaMessage)
							}
							continue
						}
						mediaSource := mediaMessage.ToFileSource()
						claudeMediaMessage, err = buildClaudeMediaMessageFromSource(c, mediaSource, "formatting file for Claude")
						if err != nil {
							return nil, err
						}
						claudeMediaMessages = append(claudeMediaMessages, *claudeMediaMessage)
					case dto.ContentTypeImageURL:
						mediaSource := mediaMessage.ToFileSource()
						claudeMediaMessage, err := buildClaudeMediaMessageFromSource(c, mediaSource, "formatting image for Claude")
						if err != nil {
							return nil, err
						}
						if claudeMediaMessage.Type != "image" {
							return nil, fmt.Errorf("Claude image content resolved to unsupported type %q", claudeMediaMessage.Type)
						}
						claudeMediaMessages = append(claudeMediaMessages, *claudeMediaMessage)
					case dto.ContentTypeInputAudio, dto.ContentTypeVideoUrl:
						return nil, fmt.Errorf("Claude content type %q is not supported", mediaMessage.Type)
					default:
						return nil, fmt.Errorf("Claude content type %q is not supported", mediaMessage.Type)
					}
				}

				if toolCalls, err := message.ParseToolCallsStrict(); err != nil {
					return nil, fmt.Errorf("Claude message tool_calls: %w", err)
				} else if len(toolCalls) > 0 {
					for _, toolCall := range toolCalls {
						if strings.TrimSpace(toolCall.ID) == "" || strings.TrimSpace(toolCall.Function.Name) == "" {
							return nil, errors.New("tool call requires id and function name")
						}
						if strings.TrimSpace(toolCall.Type) != "" && toolCall.Type != "function" {
							return nil, fmt.Errorf("tool call type %q is not supported by Claude", toolCall.Type)
						}
						inputObj := make(map[string]any)
						arguments := strings.TrimSpace(toolCall.Function.Arguments)
						if arguments != "" {
							if err := common.Unmarshal([]byte(arguments), &inputObj); err != nil {
								return nil, fmt.Errorf("invalid arguments for function %s: %w", toolCall.Function.Name, err)
							}
						}
						claudeMediaMessages = append(claudeMediaMessages, dto.ClaudeMediaMessage{
							Type:  "tool_use",
							Id:    toolCall.ID,
							Name:  toolCall.Function.Name,
							Input: inputObj,
						})
					}
				}
				claudeMessage.Content = claudeMediaMessages
			}
			claudeMessages = append(claudeMessages, claudeMessage)
		}
	}

	// 设置累积的system消息
	if len(systemMessages) > 0 {
		claudeRequest.System = systemMessages
	}

	// response_format lands in the same output_config object the reasoning
	// effort uses, so it is merged after the effort branches above have run.
	if textRequest.ResponseFormat != nil {
		claudeFormat, formatErr := claudeOutputFormatFromResponseFormat(textRequest.ResponseFormat)
		if formatErr != nil {
			return nil, formatErr
		}
		mergedConfig, mergeErr := mergeClaudeOutputConfig(claudeRequest.OutputConfig, claudeFormat)
		if mergeErr != nil {
			return nil, mergeErr
		}
		claudeRequest.OutputConfig = mergedConfig
	}

	claudeRequest.Prompt = ""
	claudeRequest.Messages = claudeMessages
	return &claudeRequest, nil
}

func StreamResponseClaude2OpenAI(claudeResponse *dto.ClaudeResponse) *dto.ChatCompletionsStreamResponse {
	response, _ := StreamResponseClaude2OpenAIWithError(claudeResponse)
	return response
}

func StreamResponseClaude2OpenAIWithError(claudeResponse *dto.ClaudeResponse) (*dto.ChatCompletionsStreamResponse, error) {
	if claudeResponse == nil {
		return nil, errors.New("Claude stream response is nil")
	}
	var response dto.ChatCompletionsStreamResponse
	response.Object = "chat.completion.chunk"
	response.Model = claudeResponse.Model
	response.Choices = make([]dto.ChatCompletionsStreamResponseChoice, 0)
	tools := make([]dto.ToolCallResponse, 0)
	fcIdx := 0
	if claudeResponse.Index != nil {
		fcIdx = *claudeResponse.Index
	}
	var choice dto.ChatCompletionsStreamResponseChoice
	if claudeResponse.Type == "message_start" {
		if claudeResponse.Message != nil {
			response.Id = claudeResponse.Message.Id
			response.Model = claudeResponse.Message.Model
		}
		//claudeUsage = &claudeResponse.Message.Usage
		choice.Delta.SetContentString("")
		choice.Delta.Role = "assistant"
	} else if claudeResponse.Type == "content_block_start" {
		if claudeResponse.ContentBlock != nil {
			// 如果是文本块，尽可能发送首段文本（若存在）
			switch claudeResponse.ContentBlock.Type {
			case "text":
				if claudeResponse.ContentBlock.Text != nil {
					choice.Delta.SetContentString(*claudeResponse.ContentBlock.Text)
				}
			case "tool_use":
				if strings.TrimSpace(claudeResponse.ContentBlock.Id) == "" || strings.TrimSpace(claudeResponse.ContentBlock.Name) == "" {
					return nil, errors.New("Claude tool_use content block requires id and name")
				}
				tools = append(tools, dto.ToolCallResponse{
					Index: common.GetPointer(fcIdx),
					ID:    claudeResponse.ContentBlock.Id,
					Type:  "function",
					Function: dto.FunctionResponse{
						Name:      claudeResponse.ContentBlock.Name,
						Arguments: "",
					},
				})
			case "thinking":
				// The OpenAI Chat Completions stream has no block-start event for
				// reasoning. The following thinking_delta carries the content.
			case "redacted_thinking":
				return nil, errors.New("Claude redacted_thinking cannot be represented as OpenAI reasoning_content")
			default:
				return nil, fmt.Errorf("Claude content block type %q is not supported by OpenAI Chat Completions", claudeResponse.ContentBlock.Type)
			}
		} else {
			return nil, errors.New("Claude content_block_start is missing content_block")
		}
	} else if claudeResponse.Type == "content_block_delta" {
		if claudeResponse.Delta != nil {
			choice.Delta.Content = claudeResponse.Delta.Text
			switch claudeResponse.Delta.Type {
			case "input_json_delta":
				if claudeResponse.Delta.PartialJson == nil {
					return nil, errors.New("Claude input_json_delta is missing partial_json")
				}
				tools = append(tools, dto.ToolCallResponse{
					Type:  "function",
					Index: common.GetPointer(fcIdx),
					Function: dto.FunctionResponse{
						Arguments: *claudeResponse.Delta.PartialJson,
					},
				})
			case "signature_delta":
				// Claude signatures are opaque integrity metadata, not visible reasoning.
				// OpenAI-compatible chat chunks have no field that can carry them safely.
				return nil, nil
			case "thinking_delta":
				choice.Delta.ReasoningContent = claudeResponse.Delta.Thinking
			case "text_delta":
				if claudeResponse.Delta.Text == nil {
					return nil, errors.New("Claude text_delta is missing text")
				}
			default:
				return nil, fmt.Errorf("Claude delta type %q is not supported by OpenAI Chat Completions", claudeResponse.Delta.Type)
			}
		} else {
			return nil, errors.New("Claude content_block_delta is missing delta")
		}
	} else if claudeResponse.Type == "message_delta" {
		if claudeResponse.Delta != nil && claudeResponse.Delta.StopReason != nil {
			finishReason, err := stopReasonClaude2OpenAIStrict(*claudeResponse.Delta.StopReason)
			if err != nil {
				return nil, err
			}
			if finishReason != "null" {
				choice.FinishReason = &finishReason
			}
		}
		//claudeUsage = &claudeResponse.Usage
	} else if claudeResponse.Type == "message_stop" {
		return nil, nil
	} else if claudeResponse.Type == "content_block_stop" || claudeResponse.Type == "ping" {
		return nil, nil
	} else {
		return nil, fmt.Errorf("Claude stream event type %q is not supported by OpenAI Chat Completions", claudeResponse.Type)
	}
	if len(tools) > 0 {
		choice.Delta.Content = nil // compatible with other OpenAI derivative applications, like LobeOpenAICompatibleFactory ...
		choice.Delta.ToolCalls = tools
	}
	response.Choices = append(response.Choices, choice)

	return &response, nil
}

func ResponseClaude2OpenAI(claudeResponse *dto.ClaudeResponse) *dto.OpenAITextResponse {
	response, _ := ResponseClaude2OpenAIWithError(claudeResponse)
	return response
}

func ResponseClaude2OpenAIWithError(claudeResponse *dto.ClaudeResponse) (*dto.OpenAITextResponse, error) {
	if claudeResponse == nil {
		return nil, errors.New("Claude response is nil")
	}
	choices := make([]dto.OpenAITextResponseChoice, 0)
	fullTextResponse := dto.OpenAITextResponse{
		Id:      fmt.Sprintf("chatcmpl-%s", common.GetUUID()),
		Object:  "chat.completion",
		Created: common.GetTimestamp(),
	}
	var responseText strings.Builder
	var responseThinking strings.Builder
	tools := make([]dto.ToolCallResponse, 0)

	fullTextResponse.Id = claudeResponse.Id
	for _, message := range claudeResponse.Content {
		switch message.Type {
		case "tool_use":
			if strings.TrimSpace(message.Id) == "" || strings.TrimSpace(message.Name) == "" {
				return nil, errors.New("Claude tool_use response requires id and name")
			}
			args, err := claudeToolInputToJSONString(message.Input)
			if err != nil {
				return nil, fmt.Errorf("failed to encode Claude tool_use arguments: %w", err)
			}
			tools = append(tools, dto.ToolCallResponse{
				ID:   message.Id,
				Type: "function", // compatible with other OpenAI derivative applications
				Function: dto.FunctionResponse{
					Name:      message.Name,
					Arguments: args,
				},
			})
		case "thinking":
			if message.Thinking != nil && *message.Thinking != "" {
				appendClaudeThinkingText(&responseThinking, *message.Thinking)
			}
		case "redacted_thinking":
			return nil, errors.New("Claude redacted_thinking cannot be represented as OpenAI reasoning_content")
		case "text":
			responseText.WriteString(message.GetText())
		case "", "message_delta":
			return nil, fmt.Errorf("Claude response content block type %q is not supported", message.Type)
		default:
			return nil, fmt.Errorf("Claude response content block type %q is not supported", message.Type)
		}
	}
	choice := dto.OpenAITextResponseChoice{
		Index: 0,
		Message: dto.Message{
			Role: "assistant",
		},
	}
	finishReason, err := stopReasonClaude2OpenAIStrict(claudeResponse.StopReason)
	if err != nil {
		return nil, err
	}
	choice.FinishReason = finishReason
	choice.Message.SetStringContent(responseText.String())
	if responseThinking.Len() > 0 {
		thinkingContent := responseThinking.String()
		choice.Message.ReasoningContent = &thinkingContent
	}
	if len(tools) > 0 {
		choice.Message.SetToolCalls(tools)
	}
	fullTextResponse.Model = claudeResponse.Model
	choices = append(choices, choice)
	fullTextResponse.Choices = choices
	return &fullTextResponse, nil
}

func claudeToolInputToJSONString(input any) (string, error) {
	if input == nil {
		return "{}", nil
	}
	raw, err := common.Marshal(input)
	if err != nil {
		return "", err
	}
	if common.GetJsonType(raw) != "object" {
		return "", fmt.Errorf("tool input must be a JSON object")
	}
	return string(raw), nil
}

func appendClaudeThinkingText(builder *strings.Builder, text string) {
	if text == "" {
		return
	}
	if builder.Len() > 0 {
		builder.WriteString("\n")
	}
	builder.WriteString(text)
}

type ClaudeResponseInfo struct {
	ResponseId   string
	Created      int64
	Model        string
	ResponseText strings.Builder
	Usage        *dto.Usage
	Done         bool
	// SawMessageStop 记录上游发来过协议终止事件 message_stop。Anthropic 不发送
	// OpenAI 风格的 `data: [DONE]`，扫描器在干净 EOF 时若未见 [DONE] 会把流判成
	// 上游中断；处理器见到 message_stop 时据此标记正常结束。
	SawMessageStop bool
}

func cacheCreationTokensForOpenAIUsage(usage *dto.Usage) int {
	if usage == nil {
		return 0
	}
	splitCacheCreationTokens := usage.ClaudeCacheCreation5mTokens + usage.ClaudeCacheCreation1hTokens
	if splitCacheCreationTokens == 0 {
		return usage.PromptTokensDetails.CachedCreationTokens
	}
	if usage.PromptTokensDetails.CachedCreationTokens > splitCacheCreationTokens {
		return usage.PromptTokensDetails.CachedCreationTokens
	}
	return splitCacheCreationTokens
}

func buildOpenAIStyleUsageFromClaudeUsage(usage *dto.Usage) dto.Usage {
	if usage == nil {
		return dto.Usage{}
	}
	clone := *usage
	clone.ClaudeCacheCreation5mTokens, clone.ClaudeCacheCreation1hTokens = service.NormalizeCacheCreationSplit(
		usage.PromptTokensDetails.CachedCreationTokens,
		usage.ClaudeCacheCreation5mTokens,
		usage.ClaudeCacheCreation1hTokens,
	)
	cacheCreationTokens := cacheCreationTokensForOpenAIUsage(usage)
	totalInputTokens := usage.PromptTokens + usage.PromptTokensDetails.CachedTokens + cacheCreationTokens
	clone.PromptTokens = totalInputTokens
	clone.InputTokens = totalInputTokens
	clone.TotalTokens = totalInputTokens + usage.CompletionTokens
	clone.UsageSemantic = "openai"
	clone.UsageSource = "anthropic"
	return clone
}

func buildMessageDeltaPatchUsage(claudeResponse *dto.ClaudeResponse, claudeInfo *ClaudeResponseInfo) *dto.ClaudeUsage {
	usage := &dto.ClaudeUsage{}
	if claudeResponse != nil && claudeResponse.Usage != nil {
		*usage = *claudeResponse.Usage
	}

	if claudeInfo == nil || claudeInfo.Usage == nil {
		return usage
	}

	if usage.InputTokens == 0 && claudeInfo.Usage.PromptTokens > 0 {
		usage.InputTokens = claudeInfo.Usage.PromptTokens
	}
	if usage.CacheReadInputTokens == 0 && claudeInfo.Usage.PromptTokensDetails.CachedTokens > 0 {
		usage.CacheReadInputTokens = claudeInfo.Usage.PromptTokensDetails.CachedTokens
	}
	if usage.CacheCreationInputTokens == 0 && claudeInfo.Usage.PromptTokensDetails.CachedCreationTokens > 0 {
		usage.CacheCreationInputTokens = claudeInfo.Usage.PromptTokensDetails.CachedCreationTokens
	}
	cacheCreation5m := 0
	cacheCreation1h := 0
	if usage.CacheCreation != nil {
		cacheCreation5m = usage.CacheCreation.Ephemeral5mInputTokens
		cacheCreation1h = usage.CacheCreation.Ephemeral1hInputTokens
	} else {
		cacheCreation5m = claudeInfo.Usage.ClaudeCacheCreation5mTokens
		cacheCreation1h = claudeInfo.Usage.ClaudeCacheCreation1hTokens
	}
	cacheCreation5m, cacheCreation1h = service.NormalizeCacheCreationSplit(
		usage.CacheCreationInputTokens,
		cacheCreation5m,
		cacheCreation1h,
	)
	if usage.CacheCreation == nil && (cacheCreation5m > 0 || cacheCreation1h > 0) {
		usage.CacheCreation = &dto.ClaudeCacheCreationUsage{}
	}
	if usage.CacheCreation != nil {
		usage.CacheCreation.Ephemeral5mInputTokens = cacheCreation5m
		usage.CacheCreation.Ephemeral1hInputTokens = cacheCreation1h
	}
	return usage
}

func shouldSkipClaudeMessageDeltaUsagePatch(info *relaycommon.RelayInfo) bool {
	if model_setting.GlobalSettingsSnapshot().PassThroughRequestEnabled {
		return true
	}
	if info == nil {
		return false
	}
	return info.ChannelSetting.PassThroughBodyEnabled
}

func patchClaudeMessageDeltaUsageData(data string, usage *dto.ClaudeUsage) string {
	if data == "" || usage == nil {
		return data
	}

	data = setMessageDeltaUsageInt(data, "usage.input_tokens", usage.InputTokens)
	data = setMessageDeltaUsageInt(data, "usage.cache_read_input_tokens", usage.CacheReadInputTokens)
	data = setMessageDeltaUsageInt(data, "usage.cache_creation_input_tokens", usage.CacheCreationInputTokens)

	if usage.CacheCreation != nil {
		data = setMessageDeltaUsageInt(data, "usage.cache_creation.ephemeral_5m_input_tokens", usage.CacheCreation.Ephemeral5mInputTokens)
		data = setMessageDeltaUsageInt(data, "usage.cache_creation.ephemeral_1h_input_tokens", usage.CacheCreation.Ephemeral1hInputTokens)
	}

	return data
}

func setMessageDeltaUsageInt(data string, path string, localValue int) string {
	if localValue <= 0 {
		return data
	}

	upstreamValue := gjson.Get(data, path)
	if upstreamValue.Exists() && upstreamValue.Int() > 0 {
		return data
	}

	patchedData, err := sjson.Set(data, path, localValue)
	if err != nil {
		return data
	}
	return patchedData
}

func FormatClaudeResponseInfo(claudeResponse *dto.ClaudeResponse, oaiResponse *dto.ChatCompletionsStreamResponse, claudeInfo *ClaudeResponseInfo) bool {
	if claudeInfo == nil {
		return false
	}
	if claudeInfo.Usage == nil {
		claudeInfo.Usage = &dto.Usage{}
	}
	if claudeResponse.Type == "message_start" {
		if claudeResponse.Message != nil {
			claudeInfo.ResponseId = claudeResponse.Message.Id
			claudeInfo.Model = claudeResponse.Message.Model
		}

		// message_start, 获取usage
		if claudeResponse.Message != nil && claudeResponse.Message.Usage != nil {
			claudeInfo.Usage.PromptTokens = claudeResponse.Message.Usage.InputTokens
			claudeInfo.Usage.UsageSemantic = "anthropic"
			claudeInfo.Usage.PromptTokensDetails.CachedTokens = claudeResponse.Message.Usage.CacheReadInputTokens
			claudeInfo.Usage.PromptTokensDetails.CachedCreationTokens = claudeResponse.Message.Usage.CacheCreationInputTokens
			claudeInfo.Usage.ClaudeCacheCreation5mTokens = claudeResponse.Message.Usage.GetCacheCreation5mTokens()
			claudeInfo.Usage.ClaudeCacheCreation1hTokens = claudeResponse.Message.Usage.GetCacheCreation1hTokens()
			claudeInfo.Usage.CompletionTokens = claudeResponse.Message.Usage.OutputTokens
		}
	} else if claudeResponse.Type == "content_block_delta" {
		if claudeResponse.Delta != nil {
			if claudeResponse.Delta.Text != nil {
				claudeInfo.ResponseText.WriteString(*claudeResponse.Delta.Text)
			}
			if claudeResponse.Delta.Thinking != nil {
				claudeInfo.ResponseText.WriteString(*claudeResponse.Delta.Thinking)
			}
		}
	} else if claudeResponse.Type == "message_delta" {
		// 最终的usage获取
		if claudeResponse.Usage != nil {
			claudeInfo.Usage.UsageSemantic = "anthropic"
			if claudeResponse.Usage.InputTokens > 0 {
				// 不叠加，只取最新的
				claudeInfo.Usage.PromptTokens = claudeResponse.Usage.InputTokens
			}
			if claudeResponse.Usage.CacheReadInputTokens > 0 {
				claudeInfo.Usage.PromptTokensDetails.CachedTokens = claudeResponse.Usage.CacheReadInputTokens
			}
			if claudeResponse.Usage.CacheCreationInputTokens > 0 {
				claudeInfo.Usage.PromptTokensDetails.CachedCreationTokens = claudeResponse.Usage.CacheCreationInputTokens
			}
			if cacheCreation5m := claudeResponse.Usage.GetCacheCreation5mTokens(); cacheCreation5m > 0 {
				claudeInfo.Usage.ClaudeCacheCreation5mTokens = cacheCreation5m
			}
			if cacheCreation1h := claudeResponse.Usage.GetCacheCreation1hTokens(); cacheCreation1h > 0 {
				claudeInfo.Usage.ClaudeCacheCreation1hTokens = cacheCreation1h
			}
			if claudeResponse.Usage.OutputTokens > 0 {
				claudeInfo.Usage.CompletionTokens = claudeResponse.Usage.OutputTokens
			}
			claudeInfo.Usage.TotalTokens = claudeInfo.Usage.PromptTokens + claudeInfo.Usage.CompletionTokens
		}

		// 判断是否完整
		claudeInfo.Done = true
	} else if claudeResponse.Type == "content_block_start" {
	} else {
		return false
	}
	if oaiResponse != nil {
		oaiResponse.Id = claudeInfo.ResponseId
		oaiResponse.Created = claudeInfo.Created
		oaiResponse.Model = claudeInfo.Model
	}
	return true
}

func HandleStreamResponseData(c *gin.Context, info *relaycommon.RelayInfo, claudeInfo *ClaudeResponseInfo, data string) *types.NewAPIError {
	var claudeResponse dto.ClaudeResponse
	err := common.UnmarshalJsonStr(data, &claudeResponse)
	if err != nil {
		common.SysLog("error unmarshalling stream response: " + err.Error())
		return types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	if claudeError := claudeResponse.GetClaudeError(); claudeError != nil && claudeError.HasContent() {
		return types.WithClaudeError(*claudeError, http.StatusInternalServerError)
	}
	if claudeResponse.Type == "message_stop" {
		claudeInfo.SawMessageStop = true
	}
	if claudeResponse.StopReason != "" {
		maybeMarkClaudeRefusal(c, claudeResponse.StopReason)
	}
	if claudeResponse.Delta != nil && claudeResponse.Delta.StopReason != nil {
		maybeMarkClaudeRefusal(c, *claudeResponse.Delta.StopReason)
	}
	if info.RelayFormat == types.RelayFormatClaude {
		FormatClaudeResponseInfo(&claudeResponse, nil, claudeInfo)

		if claudeResponse.Type == "message_start" {
			// message_start, 获取usage
			if claudeResponse.Message != nil {
				info.UpstreamModelName = claudeResponse.Message.Model
			}
			if common.GetContextKeyBool(c, constant.ContextKeyClaudeStreamPreludeSent) {
				// 重试尝试：客户端已经收到上一次尝试的 message_start。
				// 再发一次会让 Anthropic SDK 重建消息状态，这里只保留
				// usage 解析，不重复转发。
				return nil
			}
			common.SetContextKey(c, constant.ContextKeyClaudeStreamPreludeSent, true)
			// 只有生命周期帧时，中断仍可安全换渠道重试；首个内容帧到达
			// 后由下面的分支清掉这个标记。
			common.SetContextKey(c, constant.ContextKeyResponsesStreamPreludeRetrySafe, true)
		} else if claudeResponse.Type == "content_block_start" || claudeResponse.Type == "content_block_delta" {
			// 内容已下发，重放会重复内容，不能再重试。
			common.SetContextKey(c, constant.ContextKeyResponsesStreamPreludeRetrySafe, false)
		} else if claudeResponse.Type == "message_delta" {
			// 确保 message_delta 的 usage 包含完整的 input_tokens 和 cache 相关字段
			// 解决 AWS Bedrock 等上游返回的 message_delta 缺少这些字段的问题
			if !shouldSkipClaudeMessageDeltaUsagePatch(info) {
				data = patchClaudeMessageDeltaUsageData(data, buildMessageDeltaPatchUsage(&claudeResponse, claudeInfo))
			}
		}
		helper.ClaudeChunkData(c, claudeResponse, data)
	} else if info.RelayFormat == types.RelayFormatOpenAI {
		response, conversionErr := StreamResponseClaude2OpenAIWithError(&claudeResponse)
		if conversionErr != nil {
			return types.NewOpenAIError(conversionErr, types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
		}

		if !FormatClaudeResponseInfo(&claudeResponse, response, claudeInfo) {
			return nil
		}

		// 与 RelayFormatClaude 分支同样的 prelude 判定：Claude 渠道也会服务
		// OpenAI 格式客户端，缺了这段，上游只发 message_start 后断流会被当成
		// 「内容已提交」，既不换渠道重试，又按 prompt token 计费，客户端拿到空回复。
		if claudeResponse.Type == "message_start" {
			if common.GetContextKeyBool(c, constant.ContextKeyClaudeStreamPreludeSent) {
				// 重试尝试：角色帧已下发过，不重复转发；usage 解析已在上面完成。
				return nil
			}
			common.SetContextKey(c, constant.ContextKeyClaudeStreamPreludeSent, true)
			common.SetContextKey(c, constant.ContextKeyResponsesStreamPreludeRetrySafe, true)
		} else if claudeResponse.Type == "content_block_start" || claudeResponse.Type == "content_block_delta" {
			common.SetContextKey(c, constant.ContextKeyResponsesStreamPreludeRetrySafe, false)
		}

		if response != nil {
			err = helper.ObjectData(c, response)
			if err != nil {
				logger.LogError(c, "send_stream_response_failed: "+err.Error())
			}
		}
	}
	return nil
}

func HandleStreamFinalResponse(c *gin.Context, info *relaycommon.RelayInfo, claudeInfo *ClaudeResponseInfo) {
	// Claude 的 input_tokens 不含 cache 读/写，全缓存 prompt 的输入是显式 0；
	// 只有 input 与 cache 全为 0 才算输入缺失。输出同理：仅在确实流出了内容
	// 而 token 计数为 0 时才回填估算，拒答等无输出的显式 0 保持原值。
	missingInputUsage := buildOpenAIStyleUsageFromClaudeUsage(claudeInfo.Usage).PromptTokens == 0
	missingOutputUsage := claudeInfo.Usage.CompletionTokens == 0 && claudeInfo.ResponseText.Len() > 0
	if missingInputUsage || missingOutputUsage || !claudeInfo.Done {
		if common.DebugEnabled {
			common.SysLog("claude response usage is not complete, maybe upstream error")
		}
		// 只补缺失字段，不整份覆盖，保留 message_start 已拿到的 cache 字段
		fallback := service.ResponseText2Usage(c, claudeInfo.ResponseText.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		if missingOutputUsage ||
			(!claudeInfo.Done && fallback.CompletionTokens > claudeInfo.Usage.CompletionTokens) {
			claudeInfo.Usage.CompletionTokens = fallback.CompletionTokens
		}
		if missingInputUsage {
			claudeInfo.Usage.PromptTokens = fallback.PromptTokens
		}
		claudeInfo.Usage.TotalTokens = claudeInfo.Usage.PromptTokens + claudeInfo.Usage.CompletionTokens
	}
	if claudeInfo.Usage != nil {
		claudeInfo.Usage.UsageSemantic = "anthropic"
	}

	if info.RelayFormat == types.RelayFormatClaude {
		//
	} else if info.RelayFormat == types.RelayFormatOpenAI {
		if info.ShouldIncludeUsage {
			openAIUsage := buildOpenAIStyleUsageFromClaudeUsage(claudeInfo.Usage)
			response := helper.GenerateFinalUsageResponse(claudeInfo.ResponseId, claudeInfo.Created, info.UpstreamModelName, openAIUsage)
			err := helper.ObjectData(c, response)
			if err != nil {
				common.SysLog("send final response failed: " + err.Error())
			}
		}
		helper.Done(c)
	}
}

func ClaudeStreamHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	claudeInfo := &ClaudeResponseInfo{
		ResponseId:   helper.GetResponseID(c),
		Created:      common.GetTimestamp(),
		Model:        info.UpstreamModelName,
		ResponseText: strings.Builder{},
		Usage:        &dto.Usage{},
	}
	var err *types.NewAPIError
	// 与其它渠道一致，入口先认为「尚未提交内容」：客户端此时最多只收到 ping，
	// 上游在首个数据帧前断流仍可安全换渠道。首个 message_start / 内容帧到达后
	// 由 HandleStreamResponseData 细化。缺了这行，ping 先写出会让 Written() 为真
	// 而标记为假，一次本可重试的中断会被挡成 502。
	common.SetContextKey(c, constant.ContextKeyResponsesStreamPreludeRetrySafe, true)
	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		err = HandleStreamResponseData(c, info, claudeInfo, data)
		if err != nil {
			sr.Stop(err)
			return
		}
		if claudeInfo.SawMessageStop {
			// message_stop 是 Anthropic 协议的最后一个事件，与 OpenAI 的
			// [DONE] 等价：标记正常结束并停止扫描，避免扫描器的 EOF 兜底
			// 把健康流判成 upstream_transport_interrupted。
			sr.Done()
		}
	})
	if err != nil {
		return nil, err
	}

	// 上游在首个内容帧之前断流（GOAWAY / unexpected EOF）时，此前只会
	// 静默补一个 message_stop，客户端拿到空回复却按成功计费。这里改成
	// 可重试错误：客户端尚未收到任何内容，换渠道重放是安全的。
	if newAPIError := relaycommon.RetryableUpstreamStreamInterruptedError(c, info); newAPIError != nil {
		return nil, newAPIError
	}

	HandleStreamFinalResponse(c, info, claudeInfo)
	return claudeInfo.Usage, nil
}

func HandleClaudeResponseData(c *gin.Context, info *relaycommon.RelayInfo, claudeInfo *ClaudeResponseInfo, httpResp *http.Response, data []byte) *types.NewAPIError {
	var claudeResponse dto.ClaudeResponse
	err := common.Unmarshal(data, &claudeResponse)
	if err != nil {
		return types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	if claudeError := claudeResponse.GetClaudeError(); claudeError != nil && claudeError.HasContent() {
		return types.WithClaudeError(*claudeError, http.StatusInternalServerError)
	}
	maybeMarkClaudeRefusal(c, claudeResponse.StopReason)
	if claudeInfo.Usage == nil {
		claudeInfo.Usage = &dto.Usage{}
	}
	if claudeResponse.Usage != nil {
		claudeInfo.Usage.PromptTokens = claudeResponse.Usage.InputTokens
		claudeInfo.Usage.CompletionTokens = claudeResponse.Usage.OutputTokens
		claudeInfo.Usage.TotalTokens = claudeResponse.Usage.InputTokens + claudeResponse.Usage.OutputTokens
		claudeInfo.Usage.UsageSemantic = "anthropic"
		claudeInfo.Usage.PromptTokensDetails.CachedTokens = claudeResponse.Usage.CacheReadInputTokens
		claudeInfo.Usage.PromptTokensDetails.CachedCreationTokens = claudeResponse.Usage.CacheCreationInputTokens
		claudeInfo.Usage.ClaudeCacheCreation5mTokens = claudeResponse.Usage.GetCacheCreation5mTokens()
		claudeInfo.Usage.ClaudeCacheCreation1hTokens = claudeResponse.Usage.GetCacheCreation1hTokens()
	}
	var responseData []byte
	switch info.RelayFormat {
	case types.RelayFormatOpenAI:
		openaiResponse, conversionErr := ResponseClaude2OpenAIWithError(&claudeResponse)
		if conversionErr != nil {
			return types.NewOpenAIError(conversionErr, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		openaiResponse.Usage = buildOpenAIStyleUsageFromClaudeUsage(claudeInfo.Usage)
		responseData, err = common.Marshal(openaiResponse)
		if err != nil {
			return types.NewError(err, types.ErrorCodeBadResponseBody)
		}
	case types.RelayFormatClaude:
		responseData = data
	}

	if claudeResponse.Usage != nil && claudeResponse.Usage.ServerToolUse != nil && claudeResponse.Usage.ServerToolUse.WebSearchRequests > 0 {
		c.Set("claude_web_search_requests", claudeResponse.Usage.ServerToolUse.WebSearchRequests)
	}

	service.IOCopyBytesGracefully(c, httpResp, responseData)
	return nil
}

func ClaudeHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	claudeInfo := &ClaudeResponseInfo{
		ResponseId:   helper.GetResponseID(c),
		Created:      common.GetTimestamp(),
		Model:        info.UpstreamModelName,
		ResponseText: strings.Builder{},
		Usage:        &dto.Usage{},
	}
	// Claude non-stream responses are JSON envelopes; bound the read so a
	// provider cannot force unbounded allocation.
	const maxClaudeResponseBodyBytes int64 = 64 << 20
	responseBody, err := common.ReadAllWithLimit(resp.Body, maxClaudeResponseBodyBytes)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	if common.DebugEnabled {
		println("responseBody: ", string(responseBody))
	}
	handleErr := HandleClaudeResponseData(c, info, claudeInfo, resp, responseBody)
	if handleErr != nil {
		return nil, handleErr
	}
	return claudeInfo.Usage, nil
}

func mapToolChoice(toolChoice any, parallelToolCalls *bool) (*dto.ClaudeToolChoice, error) {
	var claudeToolChoice *dto.ClaudeToolChoice

	// 处理 tool_choice 字符串值
	if toolChoiceStr, ok := toolChoice.(string); ok {
		switch strings.TrimSpace(toolChoiceStr) {
		case "auto":
			claudeToolChoice = &dto.ClaudeToolChoice{
				Type: "auto",
			}
		case "required":
			claudeToolChoice = &dto.ClaudeToolChoice{
				Type: "any",
			}
		case "none":
			claudeToolChoice = &dto.ClaudeToolChoice{
				Type: "none",
			}
		default:
			return nil, fmt.Errorf("tool_choice %q is not supported by Claude", toolChoiceStr)
		}
	} else if toolChoiceMap, ok := toolChoice.(map[string]interface{}); ok {
		// 处理 tool_choice 对象值
		if rawType, exists := toolChoiceMap["type"]; exists && strings.TrimSpace(common.Interface2String(rawType)) != "function" {
			return nil, fmt.Errorf("tool_choice type %q is not supported by Claude", rawType)
		}
		if function, ok := toolChoiceMap["function"].(map[string]interface{}); ok {
			toolName := strings.TrimSpace(common.Interface2String(function["name"]))
			if toolName == "" {
				return nil, errors.New("tool_choice function name is required")
			}
			claudeToolChoice = &dto.ClaudeToolChoice{
				Type: "tool",
				Name: toolName,
			}
		} else {
			return nil, errors.New("tool_choice function object is required")
		}
	} else if toolChoice != nil {
		return nil, fmt.Errorf("tool_choice type %T is not supported by Claude", toolChoice)
	}

	// 处理 parallel_tool_calls
	if parallelToolCalls != nil {
		if claudeToolChoice == nil {
			// 如果没有 tool_choice，但有 parallel_tool_calls，创建默认的 auto 类型
			claudeToolChoice = &dto.ClaudeToolChoice{
				Type: "auto",
			}
		}

		// Anthropic schema: tool_choice.type=none does not accept extra fields.
		// When tools are disabled, parallel_tool_calls is irrelevant, so we drop it.
		if claudeToolChoice.Type != "none" {
			// 如果 parallel_tool_calls 为 true，则 disable_parallel_tool_use 为 false
			claudeToolChoice.DisableParallelToolUse = !*parallelToolCalls
		}
	}

	return claudeToolChoice, nil
}
