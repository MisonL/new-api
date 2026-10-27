package gemini

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/reasoning"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

// https://cloud.google.com/vertex-ai/generative-ai/docs/model-reference/inference?hl=zh-cn#blob
var geminiSupportedMimeTypes = map[string]bool{
	"application/pdf": true,
	"audio/mpeg":      true,
	"audio/mp3":       true,
	"audio/wav":       true,
	"image/png":       true,
	"image/jpeg":      true,
	"image/jpg":       true, // support old image/jpeg
	"image/webp":      true,
	"image/heic":      true,
	"image/heif":      true,
	"text/plain":      true,
	"video/mov":       true,
	"video/mpeg":      true,
	"video/mp4":       true,
	"video/mpg":       true,
	"video/avi":       true,
	"video/wmv":       true,
	"video/mpegps":    true,
	"video/flv":       true,
}

const thoughtSignatureBypassValue = "context_engineering_is_the_way_to_go"

// Gemini 允许的思考预算范围
const (
	pro25MinBudget       = 128
	pro25MaxBudget       = 32768
	flash25MaxBudget     = 24576
	flash25LiteMinBudget = 512
	flash25LiteMaxBudget = 24576
)

func isNew25ProModel(modelName string) bool {
	return strings.HasPrefix(modelName, "gemini-2.5-pro") &&
		!strings.HasPrefix(modelName, "gemini-2.5-pro-preview-05-06") &&
		!strings.HasPrefix(modelName, "gemini-2.5-pro-preview-03-25")
}

func is25FlashLiteModel(modelName string) bool {
	return strings.HasPrefix(modelName, "gemini-2.5-flash-lite")
}

// clampThinkingBudget 根据模型名称将预算限制在允许的范围内
func clampThinkingBudget(modelName string, budget int) int {
	isNew25Pro := isNew25ProModel(modelName)
	is25FlashLite := is25FlashLiteModel(modelName)

	if is25FlashLite {
		if budget < flash25LiteMinBudget {
			return flash25LiteMinBudget
		}
		if budget > flash25LiteMaxBudget {
			return flash25LiteMaxBudget
		}
	} else if isNew25Pro {
		if budget < pro25MinBudget {
			return pro25MinBudget
		}
		if budget > pro25MaxBudget {
			return pro25MaxBudget
		}
	} else { // 其他模型
		if budget < 0 {
			return 0
		}
		if budget > flash25MaxBudget {
			return flash25MaxBudget
		}
	}
	return budget
}

// "effort": "high" - Allocates a large portion of tokens for reasoning (approximately 80% of max_tokens)
// "effort": "medium" - Allocates a moderate portion of tokens (approximately 50% of max_tokens)
// "effort": "low" - Allocates a smaller portion of tokens (approximately 20% of max_tokens)
// "effort": "minimal" - Allocates a minimal portion of tokens (approximately 5% of max_tokens)
func clampThinkingBudgetByEffort(modelName string, effort string) int {
	isNew25Pro := isNew25ProModel(modelName)
	is25FlashLite := is25FlashLiteModel(modelName)

	maxBudget := 0
	if is25FlashLite {
		maxBudget = flash25LiteMaxBudget
	}
	if isNew25Pro {
		maxBudget = pro25MaxBudget
	} else {
		maxBudget = flash25MaxBudget
	}
	switch effort {
	case "high":
		maxBudget = maxBudget * 80 / 100
	case "medium":
		maxBudget = maxBudget * 50 / 100
	case "low":
		maxBudget = maxBudget * 20 / 100
	case "minimal":
		maxBudget = maxBudget * 5 / 100
	}
	return clampThinkingBudget(modelName, maxBudget)
}

func ThinkingAdaptor(geminiRequest *dto.GeminiChatRequest, info *relaycommon.RelayInfo, oaiRequest ...dto.GeneralOpenAIRequest) {
	if model_setting.GetGeminiSettings().ThinkingAdapterEnabled {
		modelName := info.UpstreamModelName
		isNew25Pro := strings.HasPrefix(modelName, "gemini-2.5-pro") &&
			!strings.HasPrefix(modelName, "gemini-2.5-pro-preview-05-06") &&
			!strings.HasPrefix(modelName, "gemini-2.5-pro-preview-03-25")

		if strings.Contains(modelName, "-thinking-") {
			parts := strings.SplitN(modelName, "-thinking-", 2)
			if len(parts) == 2 && parts[1] != "" {
				if budgetTokens, err := strconv.Atoi(parts[1]); err == nil {
					clampedBudget := clampThinkingBudget(modelName, budgetTokens)
					geminiRequest.GenerationConfig.ThinkingConfig = &dto.GeminiThinkingConfig{
						ThinkingBudget:  common.GetPointer(clampedBudget),
						IncludeThoughts: true,
					}
				}
			}
		} else if strings.HasSuffix(modelName, "-thinking") {
			unsupportedModels := []string{
				"gemini-2.5-pro-preview-05-06",
				"gemini-2.5-pro-preview-03-25",
			}
			isUnsupported := false
			for _, unsupportedModel := range unsupportedModels {
				if strings.HasPrefix(modelName, unsupportedModel) {
					isUnsupported = true
					break
				}
			}

			if isUnsupported {
				geminiRequest.GenerationConfig.ThinkingConfig = &dto.GeminiThinkingConfig{
					IncludeThoughts: true,
				}
			} else {
				geminiRequest.GenerationConfig.ThinkingConfig = &dto.GeminiThinkingConfig{
					IncludeThoughts: true,
				}
				if geminiRequest.GenerationConfig.MaxOutputTokens != nil && *geminiRequest.GenerationConfig.MaxOutputTokens > 0 {
					budgetTokens := model_setting.GetGeminiSettings().ThinkingAdapterBudgetTokensPercentage * float64(*geminiRequest.GenerationConfig.MaxOutputTokens)
					clampedBudget := clampThinkingBudget(modelName, int(budgetTokens))
					geminiRequest.GenerationConfig.ThinkingConfig.ThinkingBudget = common.GetPointer(clampedBudget)
				} else {
					if len(oaiRequest) > 0 {
						// 如果有reasoningEffort参数，则根据其值设置思考预算
						geminiRequest.GenerationConfig.ThinkingConfig.ThinkingBudget = common.GetPointer(clampThinkingBudgetByEffort(modelName, oaiRequest[0].ReasoningEffort))
					}
				}
			}
		} else if strings.HasSuffix(modelName, "-nothinking") {
			if !isNew25Pro {
				geminiRequest.GenerationConfig.ThinkingConfig = &dto.GeminiThinkingConfig{
					ThinkingBudget: common.GetPointer(0),
				}
			}
		} else if _, level, ok := reasoning.TrimEffortSuffix(info.UpstreamModelName); ok && level != "" {
			geminiRequest.GenerationConfig.ThinkingConfig = &dto.GeminiThinkingConfig{
				IncludeThoughts: true,
				ThinkingLevel:   level,
			}
			info.ReasoningEffort = level
		}
	}
}

// Setting safety to the lowest possible values since Gemini is already powerless enough
func CovertOpenAI2Gemini(c *gin.Context, textRequest dto.GeneralOpenAIRequest, info *relaycommon.RelayInfo) (*dto.GeminiChatRequest, error) {

	geminiRequest := dto.GeminiChatRequest{
		Contents: make([]dto.GeminiChatContent, 0, len(textRequest.Messages)),
		GenerationConfig: dto.GeminiChatGenerationConfig{
			Temperature: textRequest.Temperature,
		},
	}

	if textRequest.TopP != nil {
		geminiRequest.GenerationConfig.TopP = common.GetPointer(*textRequest.TopP)
	}
	if textRequest.TopK != nil {
		geminiTopK := float64(*textRequest.TopK)
		geminiRequest.GenerationConfig.TopK = common.GetPointer(geminiTopK)
	}
	if textRequest.PresencePenalty != nil {
		geminiPenalty := float32(*textRequest.PresencePenalty)
		geminiRequest.GenerationConfig.PresencePenalty = common.GetPointer(geminiPenalty)
	}
	if textRequest.FrequencyPenalty != nil {
		geminiPenalty := float32(*textRequest.FrequencyPenalty)
		geminiRequest.GenerationConfig.FrequencyPenalty = common.GetPointer(geminiPenalty)
	}
	if textRequest.N != nil {
		geminiRequest.GenerationConfig.CandidateCount = common.GetPointer(*textRequest.N)
	}

	if maxTokens := textRequest.GetMaxTokens(); maxTokens > 0 {
		geminiRequest.GenerationConfig.MaxOutputTokens = common.GetPointer(maxTokens)
	}

	if textRequest.Seed != nil {
		geminiSeed := int64(lo.FromPtr(textRequest.Seed))
		geminiRequest.GenerationConfig.Seed = common.GetPointer(geminiSeed)
	}

	channelType := 0
	upstreamModelName := textRequest.Model
	if info != nil {
		channelType = info.ChannelType
		if strings.TrimSpace(info.UpstreamModelName) != "" {
			upstreamModelName = info.UpstreamModelName
		}
	}
	attachThoughtSignature := (channelType == constant.ChannelTypeGemini ||
		channelType == constant.ChannelTypeVertexAi) &&
		model_setting.GetGeminiSettings().FunctionCallThoughtSignatureEnabled

	if model_setting.IsGeminiModelSupportImagine(upstreamModelName) {
		geminiRequest.GenerationConfig.ResponseModalities = []string{
			"TEXT",
			"IMAGE",
		}
	}
	stopSequences, err := parseStopSequences(textRequest.Stop)
	if err != nil {
		return nil, err
	}
	if len(stopSequences) > 0 {
		// Gemini supports up to 5 stop sequences. Refuse overflow rather than
		// silently dropping user-provided termination conditions.
		if len(stopSequences) > 5 {
			return nil, fmt.Errorf("OpenAI stop supports at most 5 sequences for Gemini, got %d", len(stopSequences))
		}
		geminiRequest.GenerationConfig.StopSequences = stopSequences
	}

	adaptorWithExtraBody := false

	// patch extra_body
	if len(textRequest.ExtraBody) > 0 {
		var extraBody map[string]interface{}
		if err := common.Unmarshal(textRequest.ExtraBody, &extraBody); err != nil {
			return nil, fmt.Errorf("invalid extra body: %w", err)
		}

		// eg. {"google":{"thinking_config":{"thinking_budget":5324,"include_thoughts":true}}}
		if googleBody, ok := extraBody["google"].(map[string]interface{}); ok {
			if !strings.HasSuffix(upstreamModelName, "-nothinking") {
				adaptorWithExtraBody = true
				// check error param name like thinkingConfig, should be thinking_config
				if _, hasErrorParam := googleBody["thinkingConfig"]; hasErrorParam {
					return nil, errors.New("extra_body.google.thinkingConfig is not supported, use extra_body.google.thinking_config instead")
				}

				if thinkingConfig, ok := googleBody["thinking_config"].(map[string]interface{}); ok {
					// check error param name like thinkingBudget, should be thinking_budget
					if _, hasErrorParam := thinkingConfig["thinkingBudget"]; hasErrorParam {
						return nil, errors.New("extra_body.google.thinking_config.thinkingBudget is not supported, use extra_body.google.thinking_config.thinking_budget instead")
					}
					var hasThinkingConfig bool
					var tempThinkingConfig dto.GeminiThinkingConfig

					if thinkingBudget, exists := thinkingConfig["thinking_budget"]; exists {
						switch v := thinkingBudget.(type) {
						case float64:
							budgetInt := int(v)
							tempThinkingConfig.ThinkingBudget = common.GetPointer(budgetInt)
							if budgetInt > 0 {
								// 有正数预算
								tempThinkingConfig.IncludeThoughts = true
							} else {
								// 存在但为0或负数，禁用思考
								tempThinkingConfig.IncludeThoughts = false
							}
							hasThinkingConfig = true
						default:
							return nil, errors.New("extra_body.google.thinking_config.thinking_budget must be an integer")
						}
					}

					if includeThoughts, exists := thinkingConfig["include_thoughts"]; exists {
						if v, ok := includeThoughts.(bool); ok {
							tempThinkingConfig.IncludeThoughts = v
							hasThinkingConfig = true
						} else {
							return nil, errors.New("extra_body.google.thinking_config.include_thoughts must be a boolean")
						}
					}
					if thinkingLevel, exists := thinkingConfig["thinking_level"]; exists {
						if v, ok := thinkingLevel.(string); ok {
							tempThinkingConfig.ThinkingLevel = v
							hasThinkingConfig = true
						} else {
							return nil, errors.New("extra_body.google.thinking_config.thinking_level must be a string")
						}
					}

					if hasThinkingConfig {
						// 避免 panic: 仅在获得配置时分配，防止后续赋值时空指针
						if geminiRequest.GenerationConfig.ThinkingConfig == nil {
							geminiRequest.GenerationConfig.ThinkingConfig = &tempThinkingConfig
						} else {
							// 如果已分配，则合并内容
							if tempThinkingConfig.ThinkingBudget != nil {
								geminiRequest.GenerationConfig.ThinkingConfig.ThinkingBudget = tempThinkingConfig.ThinkingBudget
							}
							geminiRequest.GenerationConfig.ThinkingConfig.IncludeThoughts = tempThinkingConfig.IncludeThoughts
							if tempThinkingConfig.ThinkingLevel != "" {
								geminiRequest.GenerationConfig.ThinkingConfig.ThinkingLevel = tempThinkingConfig.ThinkingLevel
							}
						}
					}
				}
			}

			// check error param name like imageConfig, should be image_config
			if _, hasErrorParam := googleBody["imageConfig"]; hasErrorParam {
				return nil, errors.New("extra_body.google.imageConfig is not supported, use extra_body.google.image_config instead")
			}

			if imageConfig, ok := googleBody["image_config"].(map[string]interface{}); ok {
				// check error param name like aspectRatio, should be aspect_ratio
				if _, hasErrorParam := imageConfig["aspectRatio"]; hasErrorParam {
					return nil, errors.New("extra_body.google.image_config.aspectRatio is not supported, use extra_body.google.image_config.aspect_ratio instead")
				}
				// check error param name like imageSize, should be image_size
				if _, hasErrorParam := imageConfig["imageSize"]; hasErrorParam {
					return nil, errors.New("extra_body.google.image_config.imageSize is not supported, use extra_body.google.image_config.image_size instead")
				}

				// convert snake_case to camelCase for Gemini API
				geminiImageConfig := make(map[string]interface{})
				if aspectRatio, ok := imageConfig["aspect_ratio"]; ok {
					geminiImageConfig["aspectRatio"] = aspectRatio
				}
				if imageSize, ok := imageConfig["image_size"]; ok {
					geminiImageConfig["imageSize"] = imageSize
				}

				if len(geminiImageConfig) > 0 {
					imageConfigBytes, err := common.Marshal(geminiImageConfig)
					if err != nil {
						return nil, fmt.Errorf("failed to marshal image_config: %w", err)
					}
					geminiRequest.GenerationConfig.ImageConfig = imageConfigBytes
				}
			}
		}
	}

	if !adaptorWithExtraBody {
		ThinkingAdaptor(&geminiRequest, info, textRequest)
	}

	safetySettings := make([]dto.GeminiChatSafetySettings, 0, len(SafetySettingList))
	for _, category := range SafetySettingList {
		safetySettings = append(safetySettings, dto.GeminiChatSafetySettings{
			Category:  category,
			Threshold: model_setting.GetGeminiSafetySetting(category),
		})
	}
	geminiRequest.SafetySettings = safetySettings

	// openaiContent.FuncToToolCalls()
	if textRequest.Tools != nil {
		functions := make([]dto.FunctionRequest, 0, len(textRequest.Tools))
		googleSearch := false
		codeExecution := false
		urlContext := false
		seenFunctionNames := make(map[string]struct{}, len(textRequest.Tools))
		for toolIndex, tool := range textRequest.Tools {
			toolType := strings.TrimSpace(tool.Type)
			if toolType != "" && toolType != "function" {
				return nil, fmt.Errorf("tools[%d] type %q is not supported by Gemini", toolIndex, tool.Type)
			}
			toolName := strings.TrimSpace(tool.Function.Name)
			if toolName == "" {
				return nil, fmt.Errorf("tools[%d] function name is required", toolIndex)
			}
			if _, exists := seenFunctionNames[toolName]; exists {
				return nil, fmt.Errorf("tools[%d] function name %q is duplicated", toolIndex, toolName)
			}
			seenFunctionNames[toolName] = struct{}{}
			tool.Function.Name = toolName
			if tool.Function.Name == "googleSearch" {
				googleSearch = true
				continue
			}
			if tool.Function.Name == "codeExecution" {
				codeExecution = true
				continue
			}
			if tool.Function.Name == "urlContext" {
				urlContext = true
				continue
			}
			if tool.Function.Parameters != nil {
				params, ok := tool.Function.Parameters.(map[string]interface{})
				if ok {
					if props, hasProps := params["properties"].(map[string]interface{}); hasProps && len(props) == 0 {
						tool.Function.Parameters = nil
					}
				}
			}
			// Clean the parameters before appending.
			cleanedParams := cleanFunctionParameters(tool.Function.Parameters)
			if cleanedParams != nil {
				encodedParams, marshalErr := common.Marshal(cleanedParams)
				if marshalErr != nil {
					return nil, fmt.Errorf("tools[%d] function %q parameters cannot be encoded: %w", toolIndex, toolName, marshalErr)
				}
				if common.GetJsonType(encodedParams) != "object" {
					return nil, fmt.Errorf("tools[%d] function %q parameters must be a JSON object", toolIndex, toolName)
				}
			}
			tool.Function.Parameters = cleanedParams
			functions = append(functions, tool.Function)
		}
		geminiTools := geminiRequest.GetTools()
		if codeExecution {
			geminiTools = append(geminiTools, dto.GeminiChatTool{CodeExecution: make(map[string]string)})
		}
		if googleSearch {
			geminiTools = append(geminiTools, dto.GeminiChatTool{GoogleSearch: make(map[string]string)})
		}
		if urlContext {
			geminiTools = append(geminiTools, dto.GeminiChatTool{URLContext: make(map[string]string)})
		}
		if len(functions) > 0 {
			geminiTools = append(geminiTools, dto.GeminiChatTool{FunctionDeclarations: functions})
		}
		geminiRequest.SetTools(geminiTools)
	}

	// [NEW] Convert OpenAI tool_choice to Gemini toolConfig.functionCallingConfig
	// Mapping: "auto" -> "AUTO", "none" -> "NONE", "required" -> "ANY"
	// Object format: {"type": "function", "function": {"name": "xxx"}} -> "ANY" + allowedFunctionNames
	// Do this independently of Tools so a supplied choice is never silently dropped.
	if textRequest.ToolChoice != nil {
		geminiRequest.ToolConfig, err = convertToolChoiceToGeminiConfig(textRequest.ToolChoice)
		if err != nil {
			return nil, err
		}
	}

	if textRequest.ResponseFormat != nil && (textRequest.ResponseFormat.Type == "json_schema" || textRequest.ResponseFormat.Type == "json_object") {
		geminiRequest.GenerationConfig.ResponseMimeType = "application/json"

		if len(textRequest.ResponseFormat.JsonSchema) > 0 {
			// 先将json.RawMessage解析
			var jsonSchema dto.FormatJsonSchema
			if err := common.Unmarshal(textRequest.ResponseFormat.JsonSchema, &jsonSchema); err == nil {
				cleanedSchema := removeAdditionalPropertiesWithDepth(jsonSchema.Schema, 0)
				geminiRequest.GenerationConfig.ResponseSchema = cleanedSchema
			}
		}
	}
	tool_call_ids := make(map[string]string)
	toolCallCounter := 0
	var system_content []string
	//shouldAddDummyModelMessage := false
	for messageIndex, message := range textRequest.Messages {
		role := strings.TrimSpace(message.Role)
		if role == "system" || role == "developer" {
			contentParts, parseErr := message.ParseContentStrict()
			if parseErr != nil {
				return nil, fmt.Errorf("messages[%d]: %w", messageIndex, parseErr)
			}
			for contentIndex, part := range contentParts {
				if part.Type != dto.ContentTypeText {
					return nil, fmt.Errorf("messages[%d].content[%d] type %q is not supported in Gemini systemInstruction", messageIndex, contentIndex, part.Type)
				}
				system_content = append(system_content, part.Text)
			}
			continue
		} else if role == "tool" || role == "function" {
			if len(geminiRequest.Contents) == 0 || geminiRequest.Contents[len(geminiRequest.Contents)-1].Role == "model" {
				geminiRequest.Contents = append(geminiRequest.Contents, dto.GeminiChatContent{
					Role: "user",
				})
			}
			var parts = &geminiRequest.Contents[len(geminiRequest.Contents)-1].Parts
			name := ""
			if message.Name != nil {
				name = strings.TrimSpace(*message.Name)
			}
			if name == "" {
				if val, exists := tool_call_ids[message.ToolCallId]; exists {
					name = val
				}
			}
			if name == "" {
				return nil, fmt.Errorf("tool response function name is required for tool call %q", message.ToolCallId)
			}
			contentParts, parseErr := message.ParseContentStrict()
			if parseErr != nil {
				return nil, fmt.Errorf("messages[%d]: %w", messageIndex, parseErr)
			}
			var contentBuilder strings.Builder
			for contentIndex, part := range contentParts {
				if part.Type != dto.ContentTypeText {
					return nil, fmt.Errorf("messages[%d].content[%d] type %q is not supported in Gemini functionResponse", messageIndex, contentIndex, part.Type)
				}
				contentBuilder.WriteString(part.Text)
			}
			contentStr := contentBuilder.String()
			if message.IsStringContent() {
				contentStr = message.StringContent()
			}
			var contentMap map[string]interface{}

			// 1. 尝试解析为 JSON 对象
			if err := common.Unmarshal([]byte(contentStr), &contentMap); err != nil {
				// 2. 如果失败，尝试解析为 JSON 数组
				var contentSlice []interface{}
				if err := common.Unmarshal([]byte(contentStr), &contentSlice); err == nil {
					// 如果是数组，包装成对象
					contentMap = map[string]interface{}{"result": contentSlice}
				} else {
					// 3. 如果再次失败，作为纯文本处理
					contentMap = map[string]interface{}{"content": contentStr}
				}
			}
			if contentMap == nil {
				contentMap = map[string]interface{}{"content": nil}
			}

			functionResp := &dto.GeminiFunctionResponse{
				Name:     name,
				Response: contentMap,
			}
			if strings.TrimSpace(message.ToolCallId) != "" {
				functionResp.ID, _ = common.Marshal(message.ToolCallId)
			}

			*parts = append(*parts, dto.GeminiPart{
				FunctionResponse: functionResp,
			})
			continue
		}
		if role != "user" && role != "assistant" && role != "model" {
			return nil, fmt.Errorf("messages[%d] role %q is not supported by Gemini", messageIndex, message.Role)
		}
		var parts []dto.GeminiPart
		content := dto.GeminiChatContent{
			Role: role,
		}
		shouldAttachThoughtSignature := attachThoughtSignature && (role == "assistant" || role == "model")
		signatureAttached := false
		// isToolCall := false
		if role == "assistant" || role == "model" {
			if message.ReasoningContent != nil || message.ThinkingContent != nil || message.Reasoning != nil {
				thoughtPart := dto.GeminiPart{
					Text:    message.GetReasoningContent(),
					Thought: true,
				}
				if shouldAttachThoughtSignature {
					thoughtPart.ThoughtSignature = json.RawMessage(strconv.Quote(thoughtSignatureBypassValue))
				}
				parts = append(parts, thoughtPart)
			}
		}
		toolCalls, parseToolErr := message.ParseToolCallsStrict()
		if parseToolErr != nil {
			return nil, fmt.Errorf("messages[%d]: %w", messageIndex, parseToolErr)
		}
		if len(toolCalls) > 0 {
			// message.Role = "model"
			// isToolCall = true
			for callIndex, call := range toolCalls {
				if strings.TrimSpace(call.Type) != "" && strings.TrimSpace(call.Type) != "function" {
					return nil, fmt.Errorf("messages[%d].tool_calls[%d] type %q is not supported by Gemini", messageIndex, callIndex, call.Type)
				}
				callName := strings.TrimSpace(call.Function.Name)
				if callName == "" {
					return nil, fmt.Errorf("messages[%d].tool_calls[%d] function name is required", messageIndex, callIndex)
				}
				toolCallCounter++
				callID := strings.TrimSpace(call.ID)
				if callID == "" {
					callID = fmt.Sprintf("call_%d", toolCallCounter)
				}
				args := map[string]interface{}{}
				if call.Function.Arguments != "" {
					if common.Unmarshal([]byte(call.Function.Arguments), &args) != nil {
						return nil, fmt.Errorf("invalid arguments for function %s, args: %s", call.Function.Name, call.Function.Arguments)
					}
				}
				toolCall := dto.GeminiPart{
					FunctionCall: &dto.FunctionCall{
						ID:           callID,
						FunctionName: callName,
						Arguments:    args,
					},
				}
				if shouldAttachThoughtSignature && !signatureAttached && hasFunctionCallContent(toolCall.FunctionCall) && len(toolCall.ThoughtSignature) == 0 {
					toolCall.ThoughtSignature = json.RawMessage(strconv.Quote(thoughtSignatureBypassValue))
					signatureAttached = true
				}
				parts = append(parts, toolCall)
				tool_call_ids[callID] = callName
			}
		}

		openaiContent, parseContentErr := message.ParseContentStrict()
		if parseContentErr != nil {
			return nil, fmt.Errorf("messages[%d]: %w", messageIndex, parseContentErr)
		}
		for contentIndex, part := range openaiContent {
			if part.Type == dto.ContentTypeText {
				if part.Text == "" {
					continue
				}
				// check markdown image ![image](data:image/jpeg;base64,xxxxxxxxxxxx)
				// 使用字符串查找而非正则，避免大文本性能问题
				text := part.Text
				hasMarkdownImage := false
				for {
					// 快速检查是否包含 markdown 图片标记
					startIdx := strings.Index(text, "![")
					if startIdx == -1 {
						break
					}
					// 找到 ](
					bracketIdx := strings.Index(text[startIdx:], "](data:")
					if bracketIdx == -1 {
						break
					}
					bracketIdx += startIdx
					// 找到闭合的 )
					closeIdx := strings.Index(text[bracketIdx+2:], ")")
					if closeIdx == -1 {
						break
					}
					closeIdx += bracketIdx + 2

					hasMarkdownImage = true
					// 添加图片前的文本
					if startIdx > 0 {
						textBefore := text[:startIdx]
						if textBefore != "" {
							parts = append(parts, dto.GeminiPart{
								Text: textBefore,
							})
						}
					}
					// 提取 data URL (从 "](" 后面开始，到 ")" 之前)
					dataUrl := text[bracketIdx+2 : closeIdx]
					format, base64String, err := service.DecodeBase64FileData(dataUrl)
					if err != nil {
						return nil, fmt.Errorf("decode markdown base64 image data failed: %s", err.Error())
					}
					imgPart := dto.GeminiPart{
						InlineData: &dto.GeminiInlineData{
							MimeType: format,
							Data:     base64String,
						},
					}
					if shouldAttachThoughtSignature {
						imgPart.ThoughtSignature = json.RawMessage(strconv.Quote(thoughtSignatureBypassValue))
					}
					parts = append(parts, imgPart)
					// 继续处理剩余文本
					text = text[closeIdx+1:]
				}
				// 添加剩余文本；如果没有找到图片则保留完整原始文本。
				if hasMarkdownImage {
					if text != "" {
						parts = append(parts, dto.GeminiPart{Text: text})
					}
				} else {
					parts = append(parts, dto.GeminiPart{
						Text: part.Text,
					})
				}
			} else {
				source := part.ToFileSource()
				if source == nil {
					return nil, fmt.Errorf("messages[%d].content[%d] type %q has no usable file source", messageIndex, contentIndex, part.Type)
				}
				base64Data, mimeType, err := service.GetBase64Data(c, source, "formatting image for Gemini")
				if err != nil {
					return nil, fmt.Errorf("get file data from '%s' failed: %w", source.GetIdentifier(), err)
				}

				// 校验 MimeType 是否在 Gemini 支持的白名单中
				if _, ok := geminiSupportedMimeTypes[strings.ToLower(mimeType)]; !ok {
					return nil, fmt.Errorf("mime type is not supported by Gemini: '%s', url: '%s', supported types are: %v", mimeType, source.GetIdentifier(), getSupportedMimeTypesList())
				}

				parts = append(parts, dto.GeminiPart{
					InlineData: &dto.GeminiInlineData{
						MimeType: mimeType,
						Data:     base64Data,
					},
				})
			}
		}

		// 如果需要附加签名但还没有附加（没有 tool_calls 或 tool_calls 为空），
		// 则在第一个文本 part 上附加 thoughtSignature
		if shouldAttachThoughtSignature && !signatureAttached && len(parts) > 0 {
			for i := range parts {
				if parts[i].Text != "" {
					parts[i].ThoughtSignature = json.RawMessage(strconv.Quote(thoughtSignatureBypassValue))
					break
				}
			}
		}

		content.Parts = parts

		// there's no assistant role in gemini and API shall vomit if Role is not user or model
		if content.Role == "assistant" {
			content.Role = "model"
		}
		if len(content.Parts) > 0 {
			geminiRequest.Contents = append(geminiRequest.Contents, content)
		}
	}

	if len(system_content) > 0 {
		geminiRequest.SystemInstructions = &dto.GeminiChatContent{
			Parts: []dto.GeminiPart{
				{
					Text: strings.Join(system_content, "\n"),
				},
			},
		}
	}

	return &geminiRequest, nil
}

// parseStopSequences 解析停止序列，支持字符串或字符串数组。
// 返回错误而不是跳过非法元素，避免跨协议转换时改变用户的停止条件。
func parseStopSequences(stop any) ([]string, error) {
	if stop == nil {
		return nil, nil
	}

	switch v := stop.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil, fmt.Errorf("stop must not be empty")
		}
		return []string{v}, nil
	case []string:
		sequences := append([]string(nil), v...)
		for index, sequence := range sequences {
			if strings.TrimSpace(sequence) == "" {
				return nil, fmt.Errorf("stop[%d] must not be empty", index)
			}
		}
		return sequences, nil
	case []interface{}:
		sequences := make([]string, 0, len(v))
		for index, item := range v {
			str, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("stop[%d] must be a string", index)
			}
			if strings.TrimSpace(str) == "" {
				return nil, fmt.Errorf("stop[%d] must not be empty", index)
			}
			sequences = append(sequences, str)
		}
		return sequences, nil
	default:
		return nil, fmt.Errorf("stop must be a string or an array of strings")
	}
}

func hasFunctionCallContent(call *dto.FunctionCall) bool {
	if call == nil {
		return false
	}
	if strings.TrimSpace(call.FunctionName) != "" {
		return true
	}

	switch v := call.Arguments.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(v) != ""
	case map[string]interface{}:
		return len(v) > 0
	case []interface{}:
		return len(v) > 0
	default:
		return true
	}
}

// Helper function to get a list of supported MIME types for error messages
func getSupportedMimeTypesList() []string {
	keys := make([]string, 0, len(geminiSupportedMimeTypes))
	for k := range geminiSupportedMimeTypes {
		keys = append(keys, k)
	}
	return keys
}

var geminiOpenAPISchemaAllowedFields = map[string]struct{}{
	"anyOf":            {},
	"default":          {},
	"description":      {},
	"enum":             {},
	"example":          {},
	"format":           {},
	"items":            {},
	"maxItems":         {},
	"maxLength":        {},
	"maxProperties":    {},
	"maximum":          {},
	"minItems":         {},
	"minLength":        {},
	"minProperties":    {},
	"minimum":          {},
	"nullable":         {},
	"pattern":          {},
	"properties":       {},
	"propertyOrdering": {},
	"required":         {},
	"title":            {},
	"type":             {},
}

const geminiFunctionSchemaMaxDepth = 64

// cleanFunctionParameters recursively removes unsupported fields from Gemini function parameters.
func cleanFunctionParameters(params interface{}) interface{} {
	return cleanFunctionParametersWithDepth(params, 0)
}

func cleanFunctionParametersWithDepth(params interface{}, depth int) interface{} {
	if params == nil {
		return nil
	}

	if depth >= geminiFunctionSchemaMaxDepth {
		return cleanFunctionParametersShallow(params)
	}

	switch v := params.(type) {
	case json.RawMessage:
		if jsonType := common.GetJsonType(v); jsonType == "object" || jsonType == "array" {
			var decoded interface{}
			if err := common.Unmarshal(v, &decoded); err == nil {
				return cleanFunctionParametersWithDepth(decoded, depth)
			}
		}
		return params
	case map[string]interface{}:
		// Keep only Gemini-supported OpenAPI schema subset fields (per official SDK Schema).
		cleanedMap := make(map[string]interface{}, len(v))
		for k, val := range v {
			if _, ok := geminiOpenAPISchemaAllowedFields[k]; ok {
				cleanedMap[k] = val
			}
		}

		normalizeGeminiSchemaTypeAndNullable(cleanedMap)

		// Clean properties
		if props, ok := cleanedMap["properties"].(map[string]interface{}); ok && props != nil {
			cleanedProps := make(map[string]interface{})
			for propName, propValue := range props {
				cleanedProps[propName] = cleanFunctionParametersWithDepth(propValue, depth+1)
			}
			cleanedMap["properties"] = cleanedProps
		}

		// Recursively clean items in arrays
		if items, ok := cleanedMap["items"].(map[string]interface{}); ok && items != nil {
			cleanedMap["items"] = cleanFunctionParametersWithDepth(items, depth+1)
		}
		// OpenAPI tuple-style items is not supported by Gemini SDK Schema; keep first to avoid API rejection.
		if itemsArray, ok := cleanedMap["items"].([]interface{}); ok && len(itemsArray) > 0 {
			cleanedMap["items"] = cleanFunctionParametersWithDepth(itemsArray[0], depth+1)
		}

		// Recursively clean anyOf
		if nested, ok := cleanedMap["anyOf"].([]interface{}); ok && nested != nil {
			cleanedNested := make([]interface{}, len(nested))
			for i, item := range nested {
				cleanedNested[i] = cleanFunctionParametersWithDepth(item, depth+1)
			}
			cleanedMap["anyOf"] = cleanedNested
		}

		return cleanedMap

	case []interface{}:
		// Handle arrays of schemas
		cleanedArray := make([]interface{}, len(v))
		for i, item := range v {
			cleanedArray[i] = cleanFunctionParametersWithDepth(item, depth+1)
		}
		return cleanedArray

	default:
		// Not a map or array, return as is (e.g., could be a primitive)
		return params
	}
}

func cleanFunctionParametersShallow(params interface{}) interface{} {
	switch v := params.(type) {
	case json.RawMessage:
		var decoded interface{}
		if err := common.Unmarshal(v, &decoded); err == nil {
			return cleanFunctionParametersShallow(decoded)
		}
		return params
	case map[string]interface{}:
		cleanedMap := make(map[string]interface{}, len(v))
		for k, val := range v {
			if _, ok := geminiOpenAPISchemaAllowedFields[k]; ok {
				cleanedMap[k] = val
			}
		}
		normalizeGeminiSchemaTypeAndNullable(cleanedMap)
		// Stop recursion and avoid retaining huge nested structures.
		delete(cleanedMap, "properties")
		delete(cleanedMap, "items")
		delete(cleanedMap, "anyOf")
		return cleanedMap
	case []interface{}:
		// Prefer an empty list over deep recursion on attacker-controlled inputs.
		return []interface{}{}
	default:
		return params
	}
}

func normalizeGeminiSchemaTypeAndNullable(schema map[string]interface{}) {
	rawType, ok := schema["type"]
	if !ok || rawType == nil {
		return
	}

	normalize := func(t string) (string, bool) {
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "object":
			return "OBJECT", false
		case "array":
			return "ARRAY", false
		case "string":
			return "STRING", false
		case "integer":
			return "INTEGER", false
		case "number":
			return "NUMBER", false
		case "boolean":
			return "BOOLEAN", false
		case "null":
			return "", true
		default:
			return t, false
		}
	}

	switch t := rawType.(type) {
	case string:
		normalized, isNull := normalize(t)
		if isNull {
			schema["nullable"] = true
			delete(schema, "type")
			return
		}
		schema["type"] = normalized
	case []interface{}:
		nullable := false
		var chosen string
		for _, item := range t {
			if s, ok := item.(string); ok {
				normalized, isNull := normalize(s)
				if isNull {
					nullable = true
					continue
				}
				if chosen == "" {
					chosen = normalized
				}
			}
		}
		if nullable {
			schema["nullable"] = true
		}
		if chosen != "" {
			schema["type"] = chosen
		} else {
			delete(schema, "type")
		}
	}
}

func removeAdditionalPropertiesWithDepth(schema interface{}, depth int) interface{} {
	if depth >= 5 {
		return schema
	}

	v, ok := schema.(map[string]interface{})
	if !ok || len(v) == 0 {
		return schema
	}
	// 删除所有的title字段
	delete(v, "title")
	delete(v, "$schema")
	// 如果type不为object和array，则直接返回
	if typeVal, exists := v["type"]; !exists || (typeVal != "object" && typeVal != "array") {
		return schema
	}
	switch v["type"] {
	case "object":
		delete(v, "additionalProperties")
		// 处理 properties
		if properties, ok := v["properties"].(map[string]interface{}); ok {
			for key, value := range properties {
				properties[key] = removeAdditionalPropertiesWithDepth(value, depth+1)
			}
		}
		for _, field := range []string{"allOf", "anyOf", "oneOf"} {
			if nested, ok := v[field].([]interface{}); ok {
				for i, item := range nested {
					nested[i] = removeAdditionalPropertiesWithDepth(item, depth+1)
				}
			}
		}
	case "array":
		if items, ok := v["items"].(map[string]interface{}); ok {
			v["items"] = removeAdditionalPropertiesWithDepth(items, depth+1)
		}
	}

	return v
}

func unescapeString(s string) (string, error) {
	var result []rune
	escaped := false
	i := 0

	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:]) // 正确解码UTF-8字符
		if r == utf8.RuneError {
			return "", fmt.Errorf("invalid UTF-8 encoding")
		}

		if escaped {
			// 如果是转义符后的字符，检查其类型
			switch r {
			case '"':
				result = append(result, '"')
			case '\\':
				result = append(result, '\\')
			case '/':
				result = append(result, '/')
			case 'b':
				result = append(result, '\b')
			case 'f':
				result = append(result, '\f')
			case 'n':
				result = append(result, '\n')
			case 'r':
				result = append(result, '\r')
			case 't':
				result = append(result, '\t')
			case '\'':
				result = append(result, '\'')
			default:
				// 如果遇到一个非法的转义字符，直接按原样输出
				result = append(result, '\\', r)
			}
			escaped = false
		} else {
			if r == '\\' {
				escaped = true // 记录反斜杠作为转义符
			} else {
				result = append(result, r)
			}
		}
		i += size // 移动到下一个字符
	}

	return string(result), nil
}
func unescapeMapOrSlice(data interface{}) interface{} {
	switch v := data.(type) {
	case map[string]interface{}:
		for k, val := range v {
			v[k] = unescapeMapOrSlice(val)
		}
	case []interface{}:
		for i, val := range v {
			v[i] = unescapeMapOrSlice(val)
		}
	case string:
		if unescaped, err := unescapeString(v); err != nil {
			return v
		} else {
			return unescaped
		}
	}
	return data
}

func getResponseToolCall(item *dto.GeminiPart, fallbackID ...string) *dto.ToolCallResponse {
	call, _ := getResponseToolCallWithError(item, fallbackID...)
	return call
}

// geminiToolCallContentFingerprint derives a stable identity from a function
// call's content, so the same call synthesises the same ID on every chunk while
// two different calls never collide. Sorting is unnecessary: common.Marshal
// emits map keys in sorted order, so equal content always yields equal bytes.
func geminiToolCallContentFingerprint(call *dto.FunctionCall) string {
	if call == nil {
		return "nil"
	}
	arguments := call.Arguments
	if arguments == nil {
		arguments = map[string]interface{}{}
	}
	encoded, err := common.Marshal(arguments)
	if err != nil {
		// Unencodable arguments still need a distinct ID per call; fall back to
		// the name so the two cases cannot collapse into one.
		return "name:" + call.FunctionName
	}
	sum := sha256.Sum256(append([]byte(call.FunctionName+"\x00"), encoded...))
	return hex.EncodeToString(sum[:8])
}

func getResponseToolCallWithError(item *dto.GeminiPart, fallbackID ...string) (*dto.ToolCallResponse, error) {
	if item == nil || item.FunctionCall == nil {
		return nil, errors.New("Gemini functionCall is missing")
	}
	if strings.TrimSpace(item.FunctionCall.FunctionName) == "" {
		return nil, errors.New("Gemini functionCall name is required")
	}
	arguments := item.FunctionCall.Arguments
	if arguments == nil {
		arguments = map[string]interface{}{}
	}
	argsBytes, err := common.Marshal(arguments)

	if err != nil {
		return nil, fmt.Errorf("encode arguments: %w", err)
	}
	if common.GetJsonType(argsBytes) != "object" {
		return nil, errors.New("Gemini functionCall arguments must be a JSON object")
	}
	callID := strings.TrimSpace(item.FunctionCall.ID)
	if callID == "" {
		for _, id := range fallbackID {
			if strings.TrimSpace(id) != "" {
				callID = strings.TrimSpace(id)
				break
			}
		}
		if callID == "" {
			callID = fmt.Sprintf("call_%s", common.GetUUID())
		}
	}
	return &dto.ToolCallResponse{
		ID:   callID,
		Type: "function",
		Function: dto.FunctionResponse{
			Arguments: string(argsBytes),
			Name:      item.FunctionCall.FunctionName,
		},
	}, nil
}

// geminiResponseHasFinishReason reports whether any candidate in the chunk
// carries a finishReason. Gemini marks the end of a generation this way and
// never sends OpenAI's `data: [DONE]`, so the stream scanner needs this signal
// to classify the stream as a normal end.
func geminiResponseHasFinishReason(resp *dto.GeminiChatResponse) bool {
	if resp == nil {
		return false
	}
	for _, candidate := range resp.Candidates {
		if candidate.FinishReason != nil && strings.TrimSpace(*candidate.FinishReason) != "" {
			return true
		}
	}
	return false
}

func geminiFinishReasonToOpenAI(reason string) (*string, error) {
	var mapped string
	switch strings.TrimSpace(reason) {
	case "STOP":
		mapped = constant.FinishReasonStop
	case "MAX_TOKENS":
		mapped = constant.FinishReasonLength
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "OTHER":
		mapped = constant.FinishReasonContentFilter
	default:
		return nil, fmt.Errorf("Gemini finishReason %q is not supported by OpenAI Chat Completions", reason)
	}
	return &mapped, nil
}

func openAIFunctionArgumentsToGemini(arguments string) map[string]interface{} {
	args := make(map[string]interface{})
	if strings.TrimSpace(arguments) == "" {
		return args
	}
	if err := common.Unmarshal([]byte(arguments), &args); err != nil {
		// Gemini functionCall.args must be an object. Keep malformed upstream
		// arguments inside a valid object instead of emitting invalid JSON.
		return map[string]interface{}{"arguments": arguments}
	}
	if args == nil {
		return map[string]interface{}{}
	}
	return args
}

func buildUsageFromGeminiMetadata(metadata dto.GeminiUsageMetadata, fallbackPromptTokens int) dto.Usage {
	promptTokens := metadata.PromptTokenCount + metadata.ToolUsePromptTokenCount
	if promptTokens <= 0 && fallbackPromptTokens > 0 {
		promptTokens = fallbackPromptTokens
	}

	usage := dto.Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: metadata.CandidatesTokenCount + metadata.ThoughtsTokenCount,
		TotalTokens:      metadata.TotalTokenCount,
	}
	usage.CompletionTokenDetails.ReasoningTokens = metadata.ThoughtsTokenCount
	usage.PromptTokensDetails.CachedTokens = metadata.CachedContentTokenCount

	for _, detail := range metadata.PromptTokensDetails {
		if detail.Modality == "AUDIO" {
			usage.PromptTokensDetails.AudioTokens += detail.TokenCount
		} else if detail.Modality == "IMAGE" || detail.Modality == "VIDEO" {
			usage.PromptTokensDetails.ImageTokens += detail.TokenCount
		} else if detail.Modality == "TEXT" {
			usage.PromptTokensDetails.TextTokens += detail.TokenCount
		}
	}
	for _, detail := range metadata.ToolUsePromptTokensDetails {
		if detail.Modality == "AUDIO" {
			usage.PromptTokensDetails.AudioTokens += detail.TokenCount
		} else if detail.Modality == "IMAGE" || detail.Modality == "VIDEO" {
			usage.PromptTokensDetails.ImageTokens += detail.TokenCount
		} else if detail.Modality == "TEXT" {
			usage.PromptTokensDetails.TextTokens += detail.TokenCount
		}
	}
	for _, detail := range metadata.CandidatesTokensDetails {
		switch detail.Modality {
		case "IMAGE":
			usage.CompletionTokenDetails.ImageTokens += detail.TokenCount
		case "VIDEO":
			usage.CompletionTokenDetails.ImageTokens += detail.TokenCount
		case "AUDIO":
			usage.CompletionTokenDetails.AudioTokens += detail.TokenCount
		case "TEXT":
			usage.CompletionTokenDetails.TextTokens += detail.TokenCount
		}
	}

	if usage.TotalTokens > 0 && usage.CompletionTokens <= 0 {
		usage.CompletionTokens = usage.TotalTokens - usage.PromptTokens
	}

	if usage.PromptTokens > 0 && usage.PromptTokensDetails.TextTokens == 0 &&
		usage.PromptTokensDetails.AudioTokens == 0 && usage.PromptTokensDetails.ImageTokens == 0 {
		usage.PromptTokensDetails.TextTokens = usage.PromptTokens
	}

	return usage
}

func responseGeminiChat2OpenAI(c *gin.Context, response *dto.GeminiChatResponse) *dto.OpenAITextResponse {
	converted, _ := responseGeminiChat2OpenAIWithError(c, response)
	return converted
}

func responseGeminiChat2OpenAIWithError(c *gin.Context, response *dto.GeminiChatResponse) (*dto.OpenAITextResponse, error) {
	if response == nil {
		return nil, errors.New("Gemini response is nil")
	}
	fullTextResponse := dto.OpenAITextResponse{
		Id:      helper.GetResponseID(c),
		Object:  "chat.completion",
		Created: common.GetTimestamp(),
		Choices: make([]dto.OpenAITextResponseChoice, 0, len(response.Candidates)),
	}
	for _, candidate := range response.Candidates {
		choice := dto.OpenAITextResponseChoice{
			Index: int(candidate.Index),
			Message: dto.Message{
				Role:    "assistant",
				Content: "",
			},
			FinishReason: constant.FinishReasonStop,
		}
		hasToolCalls := false
		if len(candidate.Content.Parts) > 0 {
			var texts []string
			var reasoningBuilder strings.Builder
			reasoningSeen := false
			var toolCalls []dto.ToolCallResponse
			toolCallOrdinal := 0
			for partIndex, part := range candidate.Content.Parts {
				if err := validateGeminiResponsePartForOpenAI(part, candidate.Index, partIndex); err != nil {
					return nil, err
				}
				if part.InlineData != nil {
					if strings.TrimSpace(part.InlineData.MimeType) == "" || strings.TrimSpace(part.InlineData.Data) == "" {
						return nil, fmt.Errorf("candidate[%d] inlineData requires mimeType and data", candidate.Index)
					}
					// OpenAI Chat Completions has no generic output media part. Images
					// remain observable as a data URL; other modalities must fail
					// instead of being mislabeled as plain text.
					if strings.HasPrefix(strings.ToLower(part.InlineData.MimeType), "image/") {
						imgText := "![image](data:" + part.InlineData.MimeType + ";base64," + part.InlineData.Data + ")"
						texts = append(texts, imgText)
					} else {
						return nil, fmt.Errorf("candidate[%d] inlineData MIME type %q cannot be represented by OpenAI Chat Completions", candidate.Index, part.InlineData.MimeType)
					}
				} else if part.FunctionCall != nil {
					choice.FinishReason = constant.FinishReasonToolCalls
					fallbackID := fmt.Sprintf("call_gemini_%d_%d", candidate.Index, toolCallOrdinal)
					toolCallOrdinal++
					call, err := getResponseToolCallWithError(&part, fallbackID)
					if err != nil {
						return nil, fmt.Errorf("candidate[%d] functionCall: %w", candidate.Index, err)
					}
					toolCalls = append(toolCalls, *call)
				} else if part.Thought {
					reasoningSeen = true
					if part.Text != "" {
						if reasoningBuilder.Len() > 0 {
							reasoningBuilder.WriteString("\n")
						}
						reasoningBuilder.WriteString(part.Text)
					}
				} else {
					if part.ExecutableCode != nil {
						texts = append(texts, "```"+part.ExecutableCode.Language+"\n"+part.ExecutableCode.Code+"\n```")
					} else if part.CodeExecutionResult != nil {
						texts = append(texts, "```output\n"+part.CodeExecutionResult.Output+"\n```")
					} else {
						// 过滤掉空行
						if part.Text != "\n" {
							texts = append(texts, part.Text)
						}
					}
				}
			}
			if len(toolCalls) > 0 {
				choice.Message.SetToolCalls(toolCalls)
				hasToolCalls = true
			}
			if reasoningSeen {
				reasoning := reasoningBuilder.String()
				choice.Message.ReasoningContent = &reasoning
			}
			choice.Message.SetStringContent(strings.Join(texts, "\n"))

		}
		if candidate.FinishReason != nil {
			mappedFinishReason, err := geminiFinishReasonToOpenAI(*candidate.FinishReason)
			if err != nil {
				return nil, fmt.Errorf("candidate[%d]: %w", candidate.Index, err)
			}
			choice.FinishReason = *mappedFinishReason
		}
		if hasToolCalls {
			choice.FinishReason = constant.FinishReasonToolCalls
		}

		fullTextResponse.Choices = append(fullTextResponse.Choices, choice)
	}
	return &fullTextResponse, nil
}

func streamResponseGeminiChat2OpenAI(geminiResponse *dto.GeminiChatResponse) (*dto.ChatCompletionsStreamResponse, bool) {
	response, isStop, _ := streamResponseGeminiChat2OpenAIWithError(geminiResponse)
	return response, isStop
}

func validateGeminiResponsePartForOpenAI(part dto.GeminiPart, candidateIndex int64, partIndex int) error {
	coreFields := 0
	if part.Text != "" {
		coreFields++
	}
	if part.InlineData != nil {
		coreFields++
	}
	if part.FunctionCall != nil {
		coreFields++
	}
	if part.FunctionResponse != nil {
		coreFields++
	}
	if part.FileData != nil {
		coreFields++
	}
	if part.ExecutableCode != nil {
		coreFields++
	}
	if part.CodeExecutionResult != nil {
		coreFields++
	}
	if coreFields > 1 {
		return fmt.Errorf("candidate[%d] part[%d] contains multiple content fields", candidateIndex, partIndex)
	}
	if part.Thought && (part.InlineData != nil || part.FunctionCall != nil || part.FunctionResponse != nil || part.FileData != nil || part.ExecutableCode != nil || part.CodeExecutionResult != nil) {
		return fmt.Errorf("candidate[%d] part[%d] thought cannot be combined with non-text content", candidateIndex, partIndex)
	}
	if coreFields == 0 {
		if part.Thought {
			return nil
		}
		return fmt.Errorf("candidate[%d] part[%d] has no supported content", candidateIndex, partIndex)
	}
	if part.FileData != nil {
		return fmt.Errorf("candidate[%d] fileData cannot be represented by OpenAI Chat Completions", candidateIndex)
	}
	if part.FunctionResponse != nil {
		return fmt.Errorf("candidate[%d] functionResponse cannot be represented by OpenAI Chat Completions", candidateIndex)
	}
	return nil
}

func streamResponseGeminiChat2OpenAIWithError(geminiResponse *dto.GeminiChatResponse) (*dto.ChatCompletionsStreamResponse, bool, error) {
	if geminiResponse == nil {
		return nil, false, errors.New("Gemini stream response is nil")
	}
	choices := make([]dto.ChatCompletionsStreamResponseChoice, 0, len(geminiResponse.Candidates))
	isStop := false
	for _, candidate := range geminiResponse.Candidates {
		if candidate.FinishReason != nil && *candidate.FinishReason == "STOP" {
			isStop = true
			candidate.FinishReason = nil
		}
		choice := dto.ChatCompletionsStreamResponseChoice{
			Index: int(candidate.Index),
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				//Role: "assistant",
			},
		}
		var texts []string
		var reasoningBuilder strings.Builder
		isTools := false
		reasoningSeen := false
		toolCallOrdinal := 0
		if candidate.FinishReason != nil {
			mappedFinishReason, err := geminiFinishReasonToOpenAI(*candidate.FinishReason)
			if err != nil {
				return nil, false, fmt.Errorf("candidate[%d]: %w", candidate.Index, err)
			}
			choice.FinishReason = mappedFinishReason
		}
		for partIndex, part := range candidate.Content.Parts {
			if err := validateGeminiResponsePartForOpenAI(part, candidate.Index, partIndex); err != nil {
				return nil, false, err
			}
			if part.InlineData != nil {
				if strings.HasPrefix(strings.ToLower(strings.TrimSpace(part.InlineData.MimeType)), "image/") {
					imgText := "![image](data:" + part.InlineData.MimeType + ";base64," + part.InlineData.Data + ")"
					texts = append(texts, imgText)
				} else {
					return nil, false, fmt.Errorf("candidate[%d] inlineData MIME type %q cannot be represented by OpenAI Chat Completions", candidate.Index, part.InlineData.MimeType)
				}
			} else if part.FunctionCall != nil {
				isTools = true
				// 合成 ID 必须同时满足两件事：
				//  1) 同一个 functionCall 若在多个 chunk 里重复出现，ID 要一致——下游
				//     GeminiChatStreamHandler 按 ID 归并 tool call index，ID 变了客户端
				//     会看到重复的调用；
				//  2) 不同调用即使落在相同位置，ID 也必须不同——candidate.Index 通常恒为
				//     0 且 toolCallOrdinal 每个 chunk 都从 0 开始，仅用位置会让两个不同
				//     调用都得到 call_gemini_0_0，下游就会把第二个当成第一个的续传。
				// 因此身份取自「内容」（函数名 + 参数），而不是位置。Gemini 的
				// functionCall 必须带完整 JSON 对象参数（见 getResponseToolCallWithError
				// 的校验），不会被拆到多个 chunk，所以内容相同即可视为同一次调用。
				fallbackID := fmt.Sprintf("call_gemini_%d_%d_%s",
					candidate.Index, toolCallOrdinal, geminiToolCallContentFingerprint(part.FunctionCall))
				toolCallOrdinal++
				call, err := getResponseToolCallWithError(&part, fallbackID)
				if err != nil {
					return nil, false, fmt.Errorf("candidate[%d] functionCall: %w", candidate.Index, err)
				}
				call.SetIndex(len(choice.Delta.ToolCalls))
				choice.Delta.ToolCalls = append(choice.Delta.ToolCalls, *call)

			} else if part.Thought {
				reasoningSeen = true
				if part.Text != "" {
					if reasoningBuilder.Len() > 0 {
						reasoningBuilder.WriteString("\n")
					}
					reasoningBuilder.WriteString(part.Text)
				}
			} else {
				if part.ExecutableCode != nil {
					texts = append(texts, "```"+part.ExecutableCode.Language+"\n"+part.ExecutableCode.Code+"\n```\n")
				} else if part.CodeExecutionResult != nil {
					texts = append(texts, "```output\n"+part.CodeExecutionResult.Output+"\n```\n")
				} else {
					if part.Text != "\n" {
						texts = append(texts, part.Text)
					}
				}
			}
		}
		if reasoningSeen {
			choice.Delta.SetReasoningContent(reasoningBuilder.String())
		}
		if len(texts) > 0 {
			choice.Delta.SetContentString(strings.Join(texts, "\n"))
		}
		if isTools {
			choice.FinishReason = &constant.FinishReasonToolCalls
		}
		choices = append(choices, choice)
	}

	var response dto.ChatCompletionsStreamResponse
	response.Object = "chat.completion.chunk"
	response.Choices = choices
	return &response, isStop, nil
}

func handleStream(c *gin.Context, info *relaycommon.RelayInfo, resp *dto.ChatCompletionsStreamResponse) error {
	streamData, err := common.Marshal(resp)
	if err != nil {
		return fmt.Errorf("failed to marshal stream response: %w", err)
	}
	err = openai.HandleStreamFormat(c, info, string(streamData), info.ChannelSetting.ForceFormat, info.ChannelSetting.ThinkingToContent)
	if err != nil {
		return fmt.Errorf("failed to handle stream format: %w", err)
	}
	return nil
}

func handleFinalStream(c *gin.Context, info *relaycommon.RelayInfo, resp *dto.ChatCompletionsStreamResponse) error {
	streamData, err := common.Marshal(resp)
	if err != nil {
		return fmt.Errorf("failed to marshal stream response: %w", err)
	}
	return openai.HandleFinalResponseWithError(c, info, string(streamData), resp.Id, resp.Created, resp.Model, resp.GetSystemFingerprint(), resp.Usage, false)
}

func geminiStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response, callback func(data string, geminiResponse *dto.GeminiChatResponse) bool) (*dto.Usage, *types.NewAPIError) {
	captureGeminiResponseHeaders(info, resp)
	// 首个内容分片之前只可能收到元数据/空候选；这段窗口内断流可以换渠道
	// 重试。内容分片到达后由下方循环清掉标记。
	common.SetContextKey(c, constant.ContextKeyResponsesStreamPreludeRetrySafe, true)
	var usage = &dto.Usage{}
	var imageCount int
	responseText := strings.Builder{}

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		var geminiResponse dto.GeminiChatResponse
		if err := common.UnmarshalJsonStr(data, &geminiResponse); err != nil {
			sr.Stop(fmt.Errorf("unmarshal: %w", err))
			return
		}

		if len(geminiResponse.Candidates) == 0 && geminiResponse.PromptFeedback != nil && geminiResponse.PromptFeedback.BlockReason != nil {
			common.SetContextKey(c, constant.ContextKeyAdminRejectReason, fmt.Sprintf("gemini_block_reason=%s", *geminiResponse.PromptFeedback.BlockReason))
		}

		// 统计图片数量
		for _, candidate := range geminiResponse.Candidates {
			for _, part := range candidate.Content.Parts {
				if part.InlineData != nil && part.InlineData.MimeType != "" {
					imageCount++
				}
				if part.Text != "" {
					responseText.WriteString(part.Text)
				}
				if part.Text != "" || part.InlineData != nil || part.FunctionCall != nil {
					// 内容已下发，重放会重复内容，不能再重试。
					common.SetContextKey(c, constant.ContextKeyResponsesStreamPreludeRetrySafe, false)
				}
			}
		}

		// 更新使用量统计
		if geminiResponse.UsageMetadata.TotalTokenCount != 0 {
			mappedUsage := buildUsageFromGeminiMetadata(geminiResponse.UsageMetadata, info.GetEstimatePromptTokens())
			*usage = mappedUsage
		}

		// 在回调之前判断终止信号：回调链会改写 response（例如 STOP 会被清空），
		// 之后再看就晚了。
		hasFinishReason := geminiResponseHasFinishReason(&geminiResponse)
		if !callback(data, &geminiResponse) {
			sr.Stop(fmt.Errorf("gemini callback stopped"))
			return
		}
		// Gemini 以候选分片携带 finishReason 作为生成终止信号，且从不发送
		// OpenAI 风格的 `data: [DONE]`。这里只标记正常结束、不停扫描器：
		// 终止分片之后仍可能跟随仅带 usageMetadata 的分片，提前停止会漏计用量。
		if hasFinishReason {
			sr.MarkDone()
		}
	})

	// 上游在首个内容分片之前断流时，此前直接按成功返回空回复。与 OpenAI /
	// Claude 侧一致：尚未提交内容就报可重试错误，让控制器换渠道。
	if newAPIError := relaycommon.RetryableUpstreamStreamInterruptedError(c, info); newAPIError != nil {
		return nil, newAPIError
	}

	if imageCount != 0 {
		if usage.CompletionTokens == 0 {
			usage.CompletionTokens = imageCount * 1400
		}
	}

	if usage.CompletionTokens <= 0 {
		if info.ReceivedResponseCount > 0 {
			usage = service.ResponseText2Usage(c, responseText.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		} else {
			usage = &dto.Usage{}
		}
	}

	return usage, nil
}

func GeminiChatStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	id := helper.GetResponseID(c)
	createAt := common.GetTimestamp()
	finishReason := constant.FinishReasonStop
	toolCallIndexByChoice := make(map[int]map[string]int)
	nextToolCallIndexByChoice := make(map[int]int)
	var conversionErr error

	usage, err := geminiStreamHandler(c, info, resp, func(data string, geminiResponse *dto.GeminiChatResponse) bool {
		response, isStop, convertErr := streamResponseGeminiChat2OpenAIWithError(geminiResponse)
		if convertErr != nil {
			conversionErr = convertErr
			return false
		}

		response.Id = id
		response.Created = createAt
		response.Model = info.UpstreamModelName
		if response.IsToolCall() {
			finishReason = constant.FinishReasonToolCalls
			if info.RelayFormat == types.RelayFormatClaude {
				for choiceIdx := range response.Choices {
					response.Choices[choiceIdx].FinishReason = nil
				}
			}
		}
		for choiceIdx := range response.Choices {
			choiceKey := response.Choices[choiceIdx].Index
			for toolIdx := range response.Choices[choiceIdx].Delta.ToolCalls {
				tool := &response.Choices[choiceIdx].Delta.ToolCalls[toolIdx]
				if tool.ID == "" {
					continue
				}
				m := toolCallIndexByChoice[choiceKey]
				if m == nil {
					m = make(map[string]int)
					toolCallIndexByChoice[choiceKey] = m
				}
				if idx, ok := m[tool.ID]; ok {
					tool.SetIndex(idx)
					continue
				}
				idx := nextToolCallIndexByChoice[choiceKey]
				nextToolCallIndexByChoice[choiceKey] = idx + 1
				m[tool.ID] = idx
				tool.SetIndex(idx)
			}
		}

		logger.LogDebug(c, fmt.Sprintf("info.SendResponseCount = %d", info.SendResponseCount))
		if info.SendResponseCount == 0 {
			// send first response
			emptyResponse := helper.GenerateStartEmptyResponse(id, createAt, info.UpstreamModelName, nil)
			if response.IsToolCall() {
				if len(emptyResponse.Choices) > 0 && len(response.Choices) > 0 {
					toolCalls := response.Choices[0].Delta.ToolCalls
					copiedToolCalls := make([]dto.ToolCallResponse, len(toolCalls))
					for idx := range toolCalls {
						copiedToolCalls[idx] = toolCalls[idx]
						copiedToolCalls[idx].Function.Arguments = ""
					}
					emptyResponse.Choices[0].Delta.ToolCalls = copiedToolCalls
				}
				finishReason = constant.FinishReasonToolCalls
				err := handleStream(c, info, emptyResponse)
				if err != nil {
					conversionErr = err
					return false
				}

				response.ClearToolCalls()
				if response.IsFinished() {
					response.Choices[0].FinishReason = nil
				}
			} else {
				err := handleStream(c, info, emptyResponse)
				if err != nil {
					conversionErr = err
					return false
				}
			}
		}

		err := handleStream(c, info, response)
		if err != nil {
			conversionErr = err
			return false
		}
		if isStop {
			if info.RelayFormat != types.RelayFormatClaude {
				if err := handleStream(c, info, helper.GenerateStopResponse(id, createAt, info.UpstreamModelName, finishReason)); err != nil {
					conversionErr = err
					return false
				}
			}
		}
		return true
	})

	if conversionErr != nil {
		// Keep the usage accumulated before the malformed chunk. The outer relay
		// settles this partial usage before returning the error, so the pre-charge
		// is not incorrectly refunded after the upstream has already consumed
		// tokens.
		return usage, types.NewOpenAIError(conversionErr, types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
	}
	if err != nil {
		return usage, err
	}

	response := helper.GenerateFinalUsageResponse(id, createAt, info.UpstreamModelName, *usage)
	if info.RelayFormat == types.RelayFormatClaude && info.ClaudeConvertInfo != nil && !info.ClaudeConvertInfo.Done {
		response = helper.GenerateStopResponse(id, createAt, info.UpstreamModelName, finishReason)
		response.Usage = usage
	}
	handleErr := handleFinalStream(c, info, response)
	if handleErr != nil {
		return usage, types.NewOpenAIError(handleErr, types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
	}
	return usage, nil
}

func GeminiChatHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	captureGeminiResponseHeaders(info, resp)
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	service.CloseResponseBodyGracefully(resp)
	if common.DebugEnabled {
		println(string(responseBody))
	}
	var geminiResponse dto.GeminiChatResponse
	err = common.Unmarshal(responseBody, &geminiResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if len(geminiResponse.Candidates) == 0 {
		usage := buildUsageFromGeminiMetadata(geminiResponse.UsageMetadata, info.GetEstimatePromptTokens())

		var newAPIError *types.NewAPIError
		if geminiResponse.PromptFeedback != nil && geminiResponse.PromptFeedback.BlockReason != nil {
			common.SetContextKey(c, constant.ContextKeyAdminRejectReason, fmt.Sprintf("gemini_block_reason=%s", *geminiResponse.PromptFeedback.BlockReason))
			newAPIError = types.NewOpenAIError(
				errors.New("request blocked by Gemini API: "+*geminiResponse.PromptFeedback.BlockReason),
				types.ErrorCodePromptBlocked,
				http.StatusBadRequest,
			)
		} else {
			common.SetContextKey(c, constant.ContextKeyAdminRejectReason, "gemini_empty_candidates")
			newAPIError = types.NewOpenAIError(
				errors.New("empty response from Gemini API"),
				types.ErrorCodeEmptyResponse,
				http.StatusInternalServerError,
			)
		}

		service.ResetStatusCode(newAPIError, c.GetString("status_code_mapping"))

		switch info.RelayFormat {
		case types.RelayFormatClaude:
			c.JSON(newAPIError.StatusCode, gin.H{
				"type":  "error",
				"error": newAPIError.ToClaudeError(),
			})
		default:
			c.JSON(newAPIError.StatusCode, gin.H{
				"error": newAPIError.ToOpenAIError(),
			})
		}
		return &usage, nil
	}
	fullTextResponse, conversionErr := responseGeminiChat2OpenAIWithError(c, &geminiResponse)
	if conversionErr != nil {
		return nil, types.NewOpenAIError(conversionErr, types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
	}
	fullTextResponse.Model = info.UpstreamModelName
	usage := buildUsageFromGeminiMetadata(geminiResponse.UsageMetadata, info.GetEstimatePromptTokens())

	fullTextResponse.Usage = usage

	switch info.RelayFormat {
	case types.RelayFormatOpenAI:
		responseBody, err = common.Marshal(fullTextResponse)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
		}
	case types.RelayFormatClaude:
		claudeResp, conversionErr := service.ResponseOpenAI2ClaudeWithError(fullTextResponse, info)
		if conversionErr != nil {
			return nil, types.NewOpenAIError(conversionErr, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		claudeRespStr, err := common.Marshal(claudeResp)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
		}
		responseBody = claudeRespStr
	case types.RelayFormatGemini:
		break
	}

	service.IOCopyBytesGracefully(c, resp, responseBody)

	return &usage, nil
}

func GeminiEmbeddingHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	captureGeminiResponseHeaders(info, resp)
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, types.NewOpenAIError(readErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	var geminiResponse dto.GeminiBatchEmbeddingResponse
	if jsonErr := common.Unmarshal(responseBody, &geminiResponse); jsonErr != nil {
		return nil, types.NewOpenAIError(jsonErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	// convert to openai format response
	openAIResponse := dto.OpenAIEmbeddingResponse{
		Object: "list",
		Data:   make([]dto.OpenAIEmbeddingResponseItem, 0, len(geminiResponse.Embeddings)),
		Model:  info.UpstreamModelName,
	}

	for i, embedding := range geminiResponse.Embeddings {
		openAIResponse.Data = append(openAIResponse.Data, dto.OpenAIEmbeddingResponseItem{
			Object:    "embedding",
			Embedding: embedding.Values,
			Index:     i,
		})
	}

	// calculate usage
	// https://ai.google.dev/gemini-api/docs/pricing?hl=zh-cn#text-embedding-004
	// Google has not yet clarified how embedding models will be billed
	// refer to openai billing method to use input tokens billing
	// https://platform.openai.com/docs/guides/embeddings#what-are-embeddings
	usage := service.ResponseText2Usage(c, "", info.UpstreamModelName, info.GetEstimatePromptTokens())
	openAIResponse.Usage = *usage

	jsonResponse, jsonErr := common.Marshal(openAIResponse)
	if jsonErr != nil {
		return nil, types.NewOpenAIError(jsonErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	service.IOCopyBytesGracefully(c, resp, jsonResponse)
	return usage, nil
}

func GeminiImageHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	captureGeminiResponseHeaders(info, resp)
	responseBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, types.NewOpenAIError(readErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	var geminiResponse dto.GeminiImageResponse
	if jsonErr := common.Unmarshal(responseBody, &geminiResponse); jsonErr != nil {
		return nil, types.NewOpenAIError(jsonErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	if len(geminiResponse.Predictions) == 0 {
		return nil, types.NewOpenAIError(errors.New("no images generated"), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	// convert to openai format response
	openAIResponse := dto.ImageResponse{
		Created: common.GetTimestamp(),
		Data:    make([]dto.ImageData, 0, len(geminiResponse.Predictions)),
	}

	for _, prediction := range geminiResponse.Predictions {
		if prediction.RaiFilteredReason != "" {
			continue // skip filtered image
		}
		openAIResponse.Data = append(openAIResponse.Data, dto.ImageData{
			B64Json: prediction.BytesBase64Encoded,
		})
	}

	jsonResponse, jsonErr := common.Marshal(openAIResponse)
	if jsonErr != nil {
		return nil, types.NewError(jsonErr, types.ErrorCodeBadResponseBody)
	}

	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	_, _ = c.Writer.Write(jsonResponse)

	// https://github.com/google-gemini/cookbook/blob/719a27d752aac33f39de18a8d3cb42a70874917e/quickstarts/Counting_Tokens.ipynb
	// each image has fixed 258 tokens
	const imageTokens = 258
	generatedImages := len(openAIResponse.Data)

	usage := &dto.Usage{
		PromptTokens:     imageTokens * generatedImages, // each generated image has fixed 258 tokens
		CompletionTokens: 0,                             // image generation does not calculate completion tokens
		TotalTokens:      imageTokens * generatedImages,
	}

	return usage, nil
}

type GeminiModelsResponse struct {
	Models        []dto.GeminiModel `json:"models"`
	NextPageToken string            `json:"nextPageToken"`
}

func FetchGeminiModels(baseURL, apiKey, proxyURL string, extraHeaders ...http.Header) ([]string, error) {
	client, err := service.GetHttpClientWithProxy(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("创建HTTP客户端失败: %v", err)
	}

	allModels := make([]string, 0)
	nextPageToken := ""
	maxPages := 100 // Safety limit to prevent infinite loops

	for page := 0; page < maxPages; page++ {
		url := fmt.Sprintf("%s/v1beta/models", baseURL)
		if nextPageToken != "" {
			url = fmt.Sprintf("%s?pageToken=%s", url, nextPageToken)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("创建请求失败: %v", err)
		}

		request.Header.Set("x-goog-api-key", apiKey)
		applyFetchGeminiExtraHeaders(request, extraHeaders...)

		response, err := client.Do(request)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("请求失败: %v", err)
		}

		if response.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			cancel()
			return nil, fmt.Errorf("服务器返回错误 %d: %s", response.StatusCode, common.LocalUpstreamErrorPreview(string(body)))
		}

		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		cancel()
		if err != nil {
			return nil, fmt.Errorf("读取响应失败: %v", err)
		}

		var modelsResponse GeminiModelsResponse
		if err = common.Unmarshal(body, &modelsResponse); err != nil {
			return nil, fmt.Errorf("解析响应失败: %v", err)
		}

		for _, model := range modelsResponse.Models {
			modelNameValue, ok := model.Name.(string)
			if !ok {
				continue
			}
			modelName := strings.TrimPrefix(modelNameValue, "models/")
			allModels = append(allModels, modelName)
		}

		nextPageToken = modelsResponse.NextPageToken
		if nextPageToken == "" {
			break
		}
	}

	return allModels, nil
}

func applyFetchGeminiExtraHeaders(request *http.Request, extraHeaders ...http.Header) {
	for _, headers := range extraHeaders {
		service.ApplyRuntimeRequestHeaders(request, headers)
	}
}

// convertToolChoiceToGeminiConfig converts OpenAI tool_choice to Gemini toolConfig
// OpenAI tool_choice values:
//   - "auto": Let the model decide (default)
//   - "none": Don't call any tools
//   - "required": Must call at least one tool
//   - {"type": "function", "function": {"name": "xxx"}}: Call specific function
//
// Gemini functionCallingConfig.mode values:
//   - "AUTO": Model decides whether to call functions
//   - "NONE": Model won't call functions
//   - "ANY": Model must call at least one function
func convertToolChoiceToGeminiConfig(toolChoice any) (*dto.ToolConfig, error) {
	if toolChoice == nil {
		return nil, nil
	}

	// Handle string values: "auto", "none", "required"
	if toolChoiceStr, ok := toolChoice.(string); ok {
		config := &dto.ToolConfig{
			FunctionCallingConfig: &dto.FunctionCallingConfig{},
		}
		switch strings.ToLower(strings.TrimSpace(toolChoiceStr)) {
		case "auto":
			config.FunctionCallingConfig.Mode = "AUTO"
		case "none":
			config.FunctionCallingConfig.Mode = "NONE"
		case "required":
			config.FunctionCallingConfig.Mode = "ANY"
		default:
			return nil, fmt.Errorf("unsupported OpenAI tool_choice %q for Gemini", toolChoiceStr)
		}
		return config, nil
	}

	// Handle object value: {"type": "function", "function": {"name": "xxx"}}
	if toolChoiceMap, ok := toolChoice.(map[string]interface{}); ok {
		if strings.TrimSpace(common.Interface2String(toolChoiceMap["type"])) == "function" {
			config := &dto.ToolConfig{
				FunctionCallingConfig: &dto.FunctionCallingConfig{
					Mode: "ANY",
				},
			}
			// Extract function name if specified
			function, ok := toolChoiceMap["function"].(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("OpenAI tool_choice function object is required for Gemini")
			}
			name := strings.TrimSpace(common.Interface2String(function["name"]))
			if name == "" {
				return nil, fmt.Errorf("OpenAI tool_choice function name is required for Gemini")
			}
			config.FunctionCallingConfig.AllowedFunctionNames = []string{name}
			return config, nil
		}
		return nil, fmt.Errorf("OpenAI tool_choice type %q is not supported by Gemini", toolChoiceMap["type"])
	}

	return nil, fmt.Errorf("OpenAI tool_choice type %T is not supported by Gemini", toolChoice)
}

func captureGeminiResponseHeaders(info *relaycommon.RelayInfo, resp *http.Response) {
	if info == nil || resp == nil {
		return
	}
	info.UpstreamRequestId = resp.Header.Get("X-Goog-Request-Id")
	info.UpstreamUsageMetadata = resp.Header.Get("X-Goog-Usage-Metadata")
}
