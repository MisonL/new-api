package service

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/openrouter"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/reasonmap"
	"github.com/samber/lo"
)

func ClaudeToOpenAIRequest(claudeRequest dto.ClaudeRequest, info *relaycommon.RelayInfo) (*dto.GeneralOpenAIRequest, error) {
	openAIRequest := dto.GeneralOpenAIRequest{
		Model:       claudeRequest.Model,
		Temperature: claudeRequest.Temperature,
	}
	if claudeRequest.MaxTokens != nil {
		openAIRequest.MaxTokens = lo.ToPtr(lo.FromPtr(claudeRequest.MaxTokens))
	}
	if claudeRequest.TopP != nil {
		openAIRequest.TopP = lo.ToPtr(lo.FromPtr(claudeRequest.TopP))
	}
	if claudeRequest.TopK != nil {
		openAIRequest.TopK = lo.ToPtr(lo.FromPtr(claudeRequest.TopK))
	}
	if claudeRequest.Stream != nil {
		openAIRequest.Stream = lo.ToPtr(lo.FromPtr(claudeRequest.Stream))
	}

	isOpenRouter := info != nil && info.ChannelType == constant.ChannelTypeOpenRouter

	if isOpenRouter {
		if effort := claudeRequest.GetEfforts(); effort != "" {
			effortBytes, _ := common.Marshal(effort)
			openAIRequest.Verbosity = effortBytes
		}
		if claudeRequest.Thinking != nil {
			var reasoning openrouter.RequestReasoning
			if claudeRequest.Thinking.Type == "enabled" {
				reasoning = openrouter.RequestReasoning{
					Enabled:   true,
					MaxTokens: claudeRequest.Thinking.GetBudgetTokens(),
				}
			} else if claudeRequest.Thinking.Type == "adaptive" {
				reasoning = openrouter.RequestReasoning{
					Enabled: true,
				}
			}
			reasoningJSON, err := common.Marshal(reasoning)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal reasoning: %w", err)
			}
			openAIRequest.Reasoning = reasoningJSON
		}
	} else {
		thinkingSuffix := "-thinking"
		if info != nil &&
			strings.HasSuffix(info.OriginModelName, thinkingSuffix) &&
			!strings.HasSuffix(openAIRequest.Model, thinkingSuffix) {
			openAIRequest.Model = openAIRequest.Model + thinkingSuffix
		}
		// output_config.effort 是 Claude 侧的推理努力声明，OpenAI 兼容上游
		// 用 reasoning_effort 承载同一语义，转发时不丢弃。
		if effort := claudeRequest.GetEfforts(); effort != "" {
			openAIRequest.ReasoningEffort = effort
		}
	}

	// Convert stop sequences
	if len(claudeRequest.StopSequences) == 1 {
		openAIRequest.Stop = claudeRequest.StopSequences[0]
	} else if len(claudeRequest.StopSequences) > 1 {
		openAIRequest.Stop = claudeRequest.StopSequences
	}

	// Convert tools. Claude built-in server tools (for example
	// web_search_20250305) do not have a faithful generic OpenAI function-tool
	// representation, so reject them instead of disguising them as functions.
	tools, err := parseClaudeToolsForOpenAI(claudeRequest.Tools)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Claude tools: %w", err)
	}
	openAITools := make([]dto.ToolCallRequest, 0)
	for index, claudeTool := range tools {
		if strings.TrimSpace(claudeTool.Name) == "" {
			return nil, fmt.Errorf("Claude tool[%d] name is required", index)
		}
		openAITool := dto.ToolCallRequest{
			Type: "function",
			Function: dto.FunctionRequest{
				Name:        claudeTool.Name,
				Description: claudeTool.Description,
				Parameters:  claudeTool.InputSchema,
			},
		}
		openAITools = append(openAITools, openAITool)
	}
	openAIRequest.Tools = openAITools

	// Convert messages
	openAIMessages := make([]dto.Message, 0)
	extractThinkTaggedHistory := shouldExtractThinkTaggedClaudeHistory(claudeRequest.Model, info)

	// Add system message if present
	if claudeRequest.System != nil {
		if claudeRequest.IsStringSystem() && claudeRequest.GetStringSystem() != "" {
			openAIMessage := dto.Message{
				Role: "system",
			}
			openAIMessage.SetStringContent(claudeRequest.GetStringSystem())
			openAIMessages = append(openAIMessages, openAIMessage)
		} else {
			systems, parseErr := common.Any2Type[[]dto.ClaudeMediaMessage](claudeRequest.System)
			if parseErr != nil {
				return nil, fmt.Errorf("failed to parse Claude system content: %w", parseErr)
			}
			if len(systems) > 0 {
				openAIMessage := dto.Message{
					Role: "system",
				}
				isOpenRouterClaude := isOpenRouter && strings.HasPrefix(info.UpstreamModelName, "anthropic/claude")
				if isOpenRouterClaude {
					systemMediaMessages := make([]dto.MediaContent, 0, len(systems))
					for systemIndex, system := range systems {
						if system.Type != "" && system.Type != "text" {
							return nil, fmt.Errorf("Claude system block[%d] type %q cannot be represented by OpenRouter", systemIndex, system.Type)
						}
						if system.Text == nil {
							return nil, fmt.Errorf("Claude system block[%d] text is required", systemIndex)
						}
						message := dto.MediaContent{
							Type:         "text",
							Text:         *system.Text,
							CacheControl: system.CacheControl,
						}
						systemMediaMessages = append(systemMediaMessages, message)
					}
					openAIMessage.SetMediaContent(systemMediaMessages)
				} else {
					systemStr := ""
					for systemIndex, system := range systems {
						if system.Type != "" && system.Type != "text" {
							return nil, fmt.Errorf("Claude system block[%d] type %q cannot be represented by OpenAI Chat Completions", systemIndex, system.Type)
						}
						if system.Text == nil {
							return nil, fmt.Errorf("Claude system block[%d] text is required", systemIndex)
						}
						systemStr += *system.Text
					}
					openAIMessage.SetStringContent(systemStr)
				}
				openAIMessages = append(openAIMessages, openAIMessage)
			}
		}
	}
	for messageIndex, claudeMessage := range claudeRequest.Messages {
		if claudeMessage.Role != "user" && claudeMessage.Role != "assistant" {
			return nil, fmt.Errorf("Claude messages[%d] role %q is not supported", messageIndex, claudeMessage.Role)
		}
		openAIMessage := dto.Message{
			Role: claudeMessage.Role,
		}

		//log.Printf("claudeMessage.Content: %v", claudeMessage.Content)
		if claudeMessage.IsStringContent() {
			contentText := claudeMessage.GetStringContent()
			if claudeMessage.Role == "assistant" && extractThinkTaggedHistory {
				visibleText, reasoningTexts, hasTaggedReasoning := splitThinkTaggedText(contentText)
				if hasTaggedReasoning {
					var reasoningBuilder strings.Builder
					for _, reasoningText := range reasoningTexts {
						appendTrimmedReasoningText(&reasoningBuilder, reasoningText)
					}
					if reasoningBuilder.Len() > 0 {
						thinking := reasoningBuilder.String()
						openAIMessage.ReasoningContent = &thinking
					}
					openAIMessage.SetStringContent(visibleText)
				} else {
					openAIMessage.SetStringContent(contentText)
				}
			} else {
				openAIMessage.SetStringContent(contentText)
			}
		} else {
			content, err := claudeMessage.ParseContent()
			if err != nil {
				return nil, err
			}
			contents := content
			var toolCalls []dto.ToolCallRequest
			var reasoningBuilder strings.Builder
			mediaMessages := make([]dto.MediaContent, 0, len(contents))
			previousText := ""
			previousTextWasThinkStripped := false

			emptyThinkingBlock := false
			for contentIndex, mediaMsg := range contents {
				switch mediaMsg.Type {
				case "text", "input_text":
					text := mediaMsg.GetText()
					textWasThinkStripped := false
					if claudeMessage.Role == "assistant" && extractThinkTaggedHistory {
						visibleText, reasoningTexts, hasTaggedReasoning := splitThinkTaggedText(text)
						if hasTaggedReasoning {
							for _, reasoningText := range reasoningTexts {
								appendTrimmedReasoningText(&reasoningBuilder, reasoningText)
							}
							text = visibleText
							textWasThinkStripped = true
						}
					}
					if textWasThinkStripped && text == "" {
						if previousText != "" {
							previousTextWasThinkStripped = true
						}
						continue
					}
					if previousText != "" && (previousTextWasThinkStripped || textWasThinkStripped) {
						if needsVisibleTextBoundarySpace(previousText, text) {
							text = " " + text
						}
					}
					message := dto.MediaContent{
						Type:         "text",
						Text:         text,
						CacheControl: mediaMsg.CacheControl,
					}
					mediaMessages = append(mediaMessages, message)
					previousText = text
					previousTextWasThinkStripped = textWasThinkStripped
				case "thinking":
					if claudeMessage.Role != "assistant" {
						break
					}
					if mediaMsg.Thinking != nil && *mediaMsg.Thinking != "" {
						appendReasoningText(&reasoningBuilder, *mediaMsg.Thinking)
						break
					}
					if strings.TrimSpace(mediaMsg.Signature) != "" {
						return nil, claudeOpaqueThinkingConversionError(messageIndex, contentIndex, "thinking")
					}
					emptyThinkingBlock = true
				case "redacted_thinking":
					if claudeMessage.Role != "assistant" {
						break
					}
					return nil, claudeOpaqueThinkingConversionError(messageIndex, contentIndex, "redacted_thinking")
				case "image":
					if mediaMsg.Source == nil {
						return nil, fmt.Errorf("Claude image block at messages[%d].content[%d] is missing source", messageIndex, contentIndex)
					}
					var mediaMessage dto.MediaContent
					sourceType := strings.ToLower(strings.TrimSpace(mediaMsg.Source.Type))
					switch sourceType {
					case "base64":
						data, ok := mediaMsg.Source.Data.(string)
						if !ok {
							return nil, fmt.Errorf("Claude image block at messages[%d].content[%d] data must be a string", messageIndex, contentIndex)
						}
						if strings.TrimSpace(mediaMsg.Source.MediaType) == "" || strings.TrimSpace(data) == "" {
							return nil, fmt.Errorf("Claude image block at messages[%d].content[%d] requires media_type and data", messageIndex, contentIndex)
						}
						if !strings.HasPrefix(strings.ToLower(mediaMsg.Source.MediaType), "image/") {
							return nil, fmt.Errorf("Claude image block at messages[%d].content[%d] has unsupported media_type %q", messageIndex, contentIndex, mediaMsg.Source.MediaType)
						}
						imageData := fmt.Sprintf("data:%s;base64,%s", mediaMsg.Source.MediaType, data)
						mediaMessage = dto.MediaContent{
							Type:     dto.ContentTypeImageURL,
							ImageUrl: &dto.MessageImageUrl{Url: imageData, MimeType: mediaMsg.Source.MediaType},
						}
					case "url":
						if strings.TrimSpace(mediaMsg.Source.Url) == "" {
							return nil, fmt.Errorf("Claude image block at messages[%d].content[%d] requires url", messageIndex, contentIndex)
						}
						mediaMessage = dto.MediaContent{
							Type:     dto.ContentTypeImageURL,
							ImageUrl: &dto.MessageImageUrl{Url: mediaMsg.Source.Url, MimeType: mediaMsg.Source.MediaType},
						}
					default:
						return nil, fmt.Errorf("Claude image block at messages[%d].content[%d] source type %q is not supported", messageIndex, contentIndex, mediaMsg.Source.Type)
					}
					mediaMessages = append(mediaMessages, mediaMessage)
				case "tool_use":
					if strings.TrimSpace(mediaMsg.Id) == "" || strings.TrimSpace(mediaMsg.Name) == "" {
						return nil, fmt.Errorf("Claude tool_use at messages[%d].content[%d] requires id and name", messageIndex, contentIndex)
					}
					toolInput := mediaMsg.Input
					if toolInput == nil {
						toolInput = map[string]interface{}{}
					}
					arguments, err := toJSONString(toolInput)
					if err != nil {
						return nil, fmt.Errorf("failed to encode Claude tool %s arguments: %w", mediaMsg.Name, err)
					}
					if common.GetJsonType([]byte(arguments)) == "null" {
						arguments = "{}"
					}
					toolCall := dto.ToolCallRequest{
						ID:   mediaMsg.Id,
						Type: "function",
						Function: dto.FunctionRequest{
							Name:      mediaMsg.Name,
							Arguments: arguments,
						},
					}
					toolCalls = append(toolCalls, toolCall)
				case "tool_result":
					if claudeMessage.Role != "user" {
						return nil, fmt.Errorf("Claude tool_result at messages[%d].content[%d] must be in a user message", messageIndex, contentIndex)
					}
					if strings.TrimSpace(mediaMsg.ToolUseId) == "" {
						return nil, fmt.Errorf("Claude tool_result at messages[%d].content[%d] requires tool_use_id", messageIndex, contentIndex)
					}
					// Preserve the order when a Claude user message interleaves text
					// and tool_result blocks. OpenAI requires a tool message after the
					// preceding assistant turn, so flush text seen before this result
					// before appending the tool message.
					if len(mediaMessages) > 0 {
						openAIMessage.SetMediaContent(mediaMessages)
						openAIMessages = append(openAIMessages, openAIMessage)
						openAIMessage = dto.Message{Role: claudeMessage.Role}
						mediaMessages = nil
					}
					// Add tool result as a separate message
					toolName := mediaMsg.Name
					if toolName == "" {
						toolName = claudeRequest.SearchToolNameByToolCallId(mediaMsg.ToolUseId)
					}
					if strings.TrimSpace(toolName) == "" {
						return nil, fmt.Errorf("Claude tool_result at messages[%d].content[%d] cannot resolve tool name for tool_use_id %q", messageIndex, contentIndex, mediaMsg.ToolUseId)
					}
					oaiToolMessage := dto.Message{
						Role:       "tool",
						Name:       &toolName,
						ToolCallId: mediaMsg.ToolUseId,
					}
					//oaiToolMessage.SetStringContent(*mediaMsg.GetMediaContent().Text)
					if mediaMsg.IsStringContent() {
						oaiToolMessage.SetStringContent(mediaMsg.GetStringContent())
					} else {
						mediaContents, parseErr := common.Any2Type[[]dto.ClaudeMediaMessage](mediaMsg.Content)
						if parseErr != nil {
							return nil, fmt.Errorf("failed to parse Claude tool result at messages[%d].content[%d]: %w", messageIndex, contentIndex, parseErr)
						}
						encodeJson, err := common.Marshal(mediaContents)
						if err != nil {
							return nil, fmt.Errorf("failed to encode Claude tool result: %w", err)
						}
						oaiToolMessage.SetStringContent(string(encodeJson))
					}
					openAIMessages = append(openAIMessages, oaiToolMessage)
				default:
					return nil, fmt.Errorf("Claude content block type %q at messages[%d].content[%d] is not supported", mediaMsg.Type, messageIndex, contentIndex)
				}
			}

			if len(toolCalls) > 0 {
				openAIMessage.SetToolCalls(toolCalls)
			}
			if reasoningBuilder.Len() > 0 {
				thinking := reasoningBuilder.String()
				openAIMessage.ReasoningContent = &thinking
			} else if emptyThinkingBlock && (len(toolCalls) > 0 || len(openAIRequest.Tools) > 0) && hasDeepSeekModelName(claudeRequest.Model, info) {
				// DeepSeek requires reasoning_content for prior assistant turns
				// whenever the request carries tools. An empty, non-opaque block has
				// no text to translate, but retaining the field avoids turning a
				// valid empty block into a missing field. Opaque blocks are rejected
				// above.
				emptyThinking := ""
				openAIMessage.ReasoningContent = &emptyThinking
			}

			if len(mediaMessages) > 0 {
				if len(toolCalls) > 0 {
					for contentIndex, mediaMessage := range mediaMessages {
						if mediaMessage.Type != dto.ContentTypeText {
							return nil, fmt.Errorf("Claude assistant content[%d] type %q cannot be represented alongside tool_use in OpenAI Chat Completions", contentIndex, mediaMessage.Type)
						}
					}
					if content := openAITextContentFromMediaMessages(mediaMessages); content != "" {
						openAIMessage.SetStringContent(content)
					}
				} else {
					openAIMessage.SetMediaContent(mediaMessages)
				}
			} else if len(toolCalls) > 0 || openAIMessage.GetReasoningContent() != "" {
				openAIMessage.SetStringContent("")
			}
		}
		parsedContent, parseErr := openAIMessage.ParseContentStrict()
		if parseErr != nil {
			return nil, fmt.Errorf("converted Claude messages[%d] content: %w", messageIndex, parseErr)
		}
		if len(parsedContent) > 0 || len(openAIMessage.ToolCalls) > 0 || openAIMessage.GetReasoningContent() != "" {
			openAIMessages = append(openAIMessages, openAIMessage)
		}
	}

	openAIRequest.Messages = openAIMessages

	return &openAIRequest, nil
}

func claudeOpaqueThinkingConversionError(messageIndex int, contentIndex int, blockType string) error {
	return fmt.Errorf(
		"cannot convert Claude %s block at messages[%d].content[%d] to OpenAI-compatible reasoning_content: the block contains opaque thinking state that cannot be represented safely; route this request to a native Anthropic-compatible channel or request thinking display=\"summarized\"",
		blockType,
		messageIndex,
		contentIndex,
	)
}

func openAITextContentFromMediaMessages(messages []dto.MediaContent) string {
	var builder strings.Builder
	for _, message := range messages {
		if message.Type == "text" && message.Text != "" {
			builder.WriteString(message.Text)
		}
	}
	return builder.String()
}

func needsVisibleTextBoundarySpace(previous string, current string) bool {
	if previous == "" || current == "" {
		return false
	}
	if isTrailingWhitespace(previous) || isLeadingWhitespace(current) {
		return false
	}
	previousRune, previousOK := lastRune(previous)
	currentRune, currentOK := firstRune(current)
	if !previousOK || !currentOK {
		return false
	}
	return !isOpeningPunctuation(previousRune) && !isLeadingPunctuation(currentRune)
}

func isTrailingWhitespace(text string) bool {
	last, ok := lastRune(text)
	return ok && unicode.IsSpace(last)
}

func isLeadingWhitespace(text string) bool {
	first, ok := firstRune(text)
	return ok && unicode.IsSpace(first)
}

func firstRune(text string) (rune, bool) {
	for _, r := range text {
		return r, true
	}
	return 0, false
}

func lastRune(text string) (rune, bool) {
	var last rune
	ok := false
	for _, r := range text {
		last = r
		ok = true
	}
	return last, ok
}

func isOpeningPunctuation(r rune) bool {
	return strings.ContainsRune("([{", r)
}

func isLeadingPunctuation(r rune) bool {
	return unicode.IsPunct(r) || unicode.IsSymbol(r)
}

func appendReasoningText(builder *strings.Builder, text string) {
	if text == "" {
		return
	}
	if builder.Len() > 0 {
		builder.WriteString("\n")
	}
	builder.WriteString(text)
}

func appendTrimmedReasoningText(builder *strings.Builder, text string) {
	appendReasoningText(builder, strings.TrimSpace(text))
}

func shouldExtractThinkTaggedClaudeHistory(model string, info *relaycommon.RelayInfo) bool {
	return hasDeepSeekModelName(model, info)
}

func isDeepSeekModelName(model string) bool {
	return IsDeepSeekModelName(model)
}

// IsDeepSeekModelName reports whether a model name identifies a DeepSeek
// model, including provider-prefixed aliases such as
// deepseek-ai/DeepSeek-R1 and provider/deepseek-v4-flash.
//
// Model names are opaque strings at the routing boundary, so only provider- or
// path-like segments are inspected. This covers slash- and dot-qualified
// aliases emitted by gateways and model registries without treating embedded
// substrings such as "notdeepseek-model" as DeepSeek models.
func IsDeepSeekModelName(model string) bool {
	normalized := strings.TrimSpace(strings.ToLower(model))
	if normalized == "" {
		return false
	}
	for _, segment := range strings.FieldsFunc(normalized, func(r rune) bool {
		switch r {
		case '/', ':', '@', '|', '.':
			return true
		default:
			return false
		}
	}) {
		segment = strings.TrimSpace(segment)
		if segment == "deepseek" || strings.HasPrefix(segment, "deepseek-") || strings.HasPrefix(segment, "deepseek_") {
			return true
		}
	}
	return false
}

func shouldExposeReasoningAsClaudeThinking(model string, info *relaycommon.RelayInfo) bool {
	if relaycommon.ThinkTagsExtracted(info) {
		// 提取器已从 think 标签剥离出推理内容：这份数据本身就是思考，模型名
		// 无需再参与判断，否则提取结果会被这里丢掉。
		return true
	}
	return hasDeepSeekModelName(model, info)
}

func hasDeepSeekModelName(model string, info *relaycommon.RelayInfo) bool {
	return HasDeepSeekModelName(model, info)
}

// HasDeepSeekModelName checks the requested, origin, and resolved upstream
// model names. Routing may rewrite the public model name, so all available
// names must participate in the capability decision.
func HasDeepSeekModelName(model string, info *relaycommon.RelayInfo) bool {
	models := []string{model}
	if info != nil {
		models = append(models, info.OriginModelName)
		// UpstreamModelName is only meaningful after channel metadata is attached.
		if info.ChannelMeta != nil {
			models = append(models, info.UpstreamModelName)
		}
	}
	for _, name := range models {
		if IsDeepSeekModelName(name) {
			return true
		}
	}
	return false
}

func splitThinkTaggedText(text string) (string, []string, bool) {
	const startTag = "<think>"
	const endTag = "</think>"

	var visibleBuilder strings.Builder
	var reasoningTexts []string
	cursor := 0
	for {
		startRel := indexASCIIFold(text[cursor:], startTag)
		if startRel < 0 {
			appendVisibleThinkText(&visibleBuilder, text[cursor:])
			break
		}

		start := cursor + startRel
		reasoningStart := start + len(startTag)
		endRel := indexASCIIFold(text[reasoningStart:], endTag)
		if endRel < 0 {
			appendVisibleThinkText(&visibleBuilder, text[cursor:])
			break
		}

		end := reasoningStart + endRel
		appendVisibleThinkText(&visibleBuilder, text[cursor:start])
		reasoningTexts = append(reasoningTexts, text[reasoningStart:end])
		cursor = end + len(endTag)
	}

	if len(reasoningTexts) == 0 {
		return text, nil, false
	}
	visibleText := strings.TrimSpace(visibleBuilder.String())
	for _, reasoningText := range reasoningTexts {
		if strings.TrimSpace(reasoningText) != "" {
			return visibleText, reasoningTexts, true
		}
	}
	if visibleText == "" {
		return text, nil, false
	}
	return visibleText, reasoningTexts, true
}

func indexASCIIFold(text string, pattern string) int {
	if pattern == "" {
		return 0
	}
	patternLen := len(pattern)
	for index := 0; index+patternLen <= len(text); index++ {
		if equalASCIIFold(text[index:index+patternLen], pattern) {
			return index
		}
	}
	return -1
}

func equalASCIIFold(text string, pattern string) bool {
	if len(text) != len(pattern) {
		return false
	}
	for index := range pattern {
		if toLowerASCII(text[index]) != toLowerASCII(pattern[index]) {
			return false
		}
	}
	return true
}

func toLowerASCII(value byte) byte {
	if value >= 'A' && value <= 'Z' {
		return value + ('a' - 'A')
	}
	return value
}

func appendVisibleThinkText(builder *strings.Builder, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if builder.Len() > 0 {
		builder.WriteString(" ")
	}
	builder.WriteString(text)
}

func generateStopBlock(index int) *dto.ClaudeResponse {
	return &dto.ClaudeResponse{
		Type:  "content_block_stop",
		Index: common.GetPointer[int](index),
	}
}

func buildClaudeUsageFromOpenAIUsage(oaiUsage *dto.Usage) *dto.ClaudeUsage {
	if oaiUsage == nil {
		return nil
	}
	cacheCreation5m, cacheCreation1h := NormalizeCacheCreationSplit(
		oaiUsage.PromptTokensDetails.CachedCreationTokens,
		oaiUsage.ClaudeCacheCreation5mTokens,
		oaiUsage.ClaudeCacheCreation1hTokens,
	)
	usage := &dto.ClaudeUsage{
		InputTokens:              oaiUsage.PromptTokens,
		OutputTokens:             oaiUsage.CompletionTokens,
		CacheCreationInputTokens: oaiUsage.PromptTokensDetails.CachedCreationTokens,
		CacheReadInputTokens:     oaiUsage.PromptTokensDetails.CachedTokens,
	}
	if cacheCreation5m > 0 || cacheCreation1h > 0 {
		usage.CacheCreation = &dto.ClaudeCacheCreationUsage{
			Ephemeral5mInputTokens: cacheCreation5m,
			Ephemeral1hInputTokens: cacheCreation1h,
		}
	}
	return usage
}

func NormalizeCacheCreationSplit(totalTokens int, tokens5m int, tokens1h int) (int, int) {
	remainder := lo.Max([]int{totalTokens - tokens5m - tokens1h, 0})
	return tokens5m + remainder, tokens1h
}

func ensureClaudeToolCallState(info *relaycommon.RelayInfo) {
	if info == nil || info.ClaudeConvertInfo == nil {
		return
	}
	if info.ClaudeConvertInfo.ToolCallStarted == nil {
		info.ClaudeConvertInfo.ToolCallStarted = make(map[int]bool)
	}
	if info.ClaudeConvertInfo.ToolCallIDs == nil {
		info.ClaudeConvertInfo.ToolCallIDs = make(map[int]string)
	}
	if info.ClaudeConvertInfo.ToolCallNames == nil {
		info.ClaudeConvertInfo.ToolCallNames = make(map[int]string)
	}
	if info.ClaudeConvertInfo.ToolCallPendingArgs == nil {
		info.ClaudeConvertInfo.ToolCallPendingArgs = make(map[int]string)
	}
}

func resetClaudeToolCallState(info *relaycommon.RelayInfo) {
	if info == nil || info.ClaudeConvertInfo == nil {
		return
	}
	info.ClaudeConvertInfo.ToolCallBaseIndex = 0
	info.ClaudeConvertInfo.ToolCallMaxIndexOffset = 0
	info.ClaudeConvertInfo.ToolCallStarted = nil
	info.ClaudeConvertInfo.ToolCallIDs = nil
	info.ClaudeConvertInfo.ToolCallNames = nil
	info.ClaudeConvertInfo.ToolCallPendingArgs = nil
}

func appendClaudeToolDelta(responses *[]*dto.ClaudeResponse, blockIndex int, arguments string) {
	if arguments == "" {
		return
	}
	idx := blockIndex
	*responses = append(*responses, &dto.ClaudeResponse{
		Index: &idx,
		Type:  "content_block_delta",
		Delta: &dto.ClaudeMediaMessage{
			Type:        "input_json_delta",
			PartialJson: &arguments,
		},
	})
}

func StreamResponseOpenAI2Claude(openAIResponse *dto.ChatCompletionsStreamResponse, info *relaycommon.RelayInfo) []*dto.ClaudeResponse {
	responses, _ := StreamResponseOpenAI2ClaudeWithError(openAIResponse, info)
	return responses
}

// StreamResponseOpenAI2ClaudeWithError validates finish reasons before
// emitting Anthropic SSE blocks. The legacy wrapper is kept for callers that
// cannot return an error, while relay handlers use this variant to avoid
// forwarding an invalid Claude stop_reason.
func StreamResponseOpenAI2ClaudeWithError(openAIResponse *dto.ChatCompletionsStreamResponse, info *relaycommon.RelayInfo) ([]*dto.ClaudeResponse, error) {
	if openAIResponse == nil {
		return nil, fmt.Errorf("OpenAI stream response is nil")
	}
	for choiceIndex, choice := range openAIResponse.Choices {
		if choice.FinishReason == nil {
			continue
		}
		if _, err := stopReasonOpenAI2ClaudeStrict(*choice.FinishReason); err != nil {
			return nil, fmt.Errorf("choice[%d]: %w", choiceIndex, err)
		}
	}
	return streamResponseOpenAI2Claude(openAIResponse, info), nil
}

func streamResponseOpenAI2Claude(openAIResponse *dto.ChatCompletionsStreamResponse, info *relaycommon.RelayInfo) []*dto.ClaudeResponse {
	if info.ClaudeConvertInfo.Done {
		return nil
	}
	allowClaudeThinking := shouldExposeReasoningAsClaudeThinking(openAIResponse.Model, info)

	var claudeResponses []*dto.ClaudeResponse
	// stopOpenBlocks emits the required content_block_stop event(s) for the currently open block(s)
	// according to Anthropic's SSE streaming state machine:
	// content_block_start -> content_block_delta* -> content_block_stop (per index).
	//
	// For text/thinking, there is at most one open block at info.ClaudeConvertInfo.Index.
	// For tools, OpenAI tool_calls can stream multiple parallel tool_use blocks (indexed from 0),
	// so we may have multiple open blocks and must stop each one explicitly.
	stopOpenBlocks := func() {
		switch info.ClaudeConvertInfo.LastMessagesType {
		case relaycommon.LastMessageTypeText, relaycommon.LastMessageTypeThinking:
			claudeResponses = append(claudeResponses, generateStopBlock(info.ClaudeConvertInfo.Index))
		case relaycommon.LastMessageTypeTools:
			ensureClaudeToolCallState(info)
			base := info.ClaudeConvertInfo.ToolCallBaseIndex
			for offset := 0; offset <= info.ClaudeConvertInfo.ToolCallMaxIndexOffset; offset++ {
				blockIndex := base + offset
				if info.ClaudeConvertInfo.ToolCallStarted[blockIndex] {
					claudeResponses = append(claudeResponses, generateStopBlock(blockIndex))
				}
			}
		}
	}
	// stopOpenBlocksAndAdvance closes the currently open block(s) and advances the content block index
	// to the next available slot for subsequent content_block_start events.
	//
	// This prevents invalid streams where a content_block_delta (e.g. thinking_delta) is emitted for an
	// index whose active content_block type is different (the typical cause of "Mismatched content block type").
	stopOpenBlocksAndAdvance := func() {
		if info.ClaudeConvertInfo.LastMessagesType == relaycommon.LastMessageTypeNone {
			return
		}
		stopOpenBlocks()
		switch info.ClaudeConvertInfo.LastMessagesType {
		case relaycommon.LastMessageTypeTools:
			info.ClaudeConvertInfo.Index = info.ClaudeConvertInfo.ToolCallBaseIndex + info.ClaudeConvertInfo.ToolCallMaxIndexOffset + 1
			resetClaudeToolCallState(info)
		default:
			info.ClaudeConvertInfo.Index++
		}
		info.ClaudeConvertInfo.LastMessagesType = relaycommon.LastMessageTypeNone
	}
	appendReasoningDelta := func(reasoning string) {
		if !allowClaudeThinking {
			return
		}
		if reasoning == "" {
			return
		}
		if info.ClaudeConvertInfo.LastMessagesType != relaycommon.LastMessageTypeThinking {
			stopOpenBlocksAndAdvance()
			idx := info.ClaudeConvertInfo.Index
			claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
				Index: &idx,
				Type:  "content_block_start",
				ContentBlock: &dto.ClaudeMediaMessage{
					Type:     "thinking",
					Thinking: common.GetPointer[string](""),
				},
			})
		}
		info.ClaudeConvertInfo.LastMessagesType = relaycommon.LastMessageTypeThinking
		idx := info.ClaudeConvertInfo.Index
		claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
			Index: &idx,
			Type:  "content_block_delta",
			Delta: &dto.ClaudeMediaMessage{
				Type:     "thinking_delta",
				Thinking: &reasoning,
			},
		})
	}
	appendTextDelta := func(textContent string) {
		if textContent == "" {
			return
		}
		if info.ClaudeConvertInfo.LastMessagesType != relaycommon.LastMessageTypeText {
			stopOpenBlocksAndAdvance()
			idx := info.ClaudeConvertInfo.Index
			claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
				Index: &idx,
				Type:  "content_block_start",
				ContentBlock: &dto.ClaudeMediaMessage{
					Type: "text",
					Text: common.GetPointer[string](""),
				},
			})
		}
		info.ClaudeConvertInfo.LastMessagesType = relaycommon.LastMessageTypeText
		idx := info.ClaudeConvertInfo.Index
		claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
			Index: &idx,
			Type:  "content_block_delta",
			Delta: &dto.ClaudeMediaMessage{
				Type: "text_delta",
				Text: common.GetPointer[string](textContent),
			},
		})
	}
	appendToolCalls := func(toolCalls []dto.ToolCallResponse) {
		if len(toolCalls) == 0 {
			return
		}
		if info.ClaudeConvertInfo.LastMessagesType != relaycommon.LastMessageTypeTools {
			stopOpenBlocksAndAdvance()
			info.ClaudeConvertInfo.ToolCallBaseIndex = info.ClaudeConvertInfo.Index
			info.ClaudeConvertInfo.ToolCallMaxIndexOffset = 0
			ensureClaudeToolCallState(info)
		}
		info.ClaudeConvertInfo.LastMessagesType = relaycommon.LastMessageTypeTools
		ensureClaudeToolCallState(info)

		base := info.ClaudeConvertInfo.ToolCallBaseIndex
		maxOffset := info.ClaudeConvertInfo.ToolCallMaxIndexOffset
		for i, toolCall := range toolCalls {
			offset := i
			if toolCall.Index != nil {
				offset = *toolCall.Index
			}
			if offset > maxOffset {
				maxOffset = offset
			}
			blockIndex := base + offset
			if toolCall.ID != "" {
				info.ClaudeConvertInfo.ToolCallIDs[blockIndex] = toolCall.ID
			}
			if toolCall.Function.Name != "" {
				info.ClaudeConvertInfo.ToolCallNames[blockIndex] = toolCall.Function.Name
			}

			if !info.ClaudeConvertInfo.ToolCallStarted[blockIndex] {
				if toolCall.Function.Arguments != "" {
					info.ClaudeConvertInfo.ToolCallPendingArgs[blockIndex] += toolCall.Function.Arguments
				}
				name := info.ClaudeConvertInfo.ToolCallNames[blockIndex]
				if name == "" {
					continue
				}
				id := info.ClaudeConvertInfo.ToolCallIDs[blockIndex]
				if id == "" {
					id = fmt.Sprintf("call_%d", blockIndex)
				}
				idx := blockIndex
				claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
					Index: &idx,
					Type:  "content_block_start",
					ContentBlock: &dto.ClaudeMediaMessage{
						Id:    id,
						Type:  "tool_use",
						Name:  name,
						Input: map[string]interface{}{},
					},
				})
				info.ClaudeConvertInfo.ToolCallStarted[blockIndex] = true
				appendClaudeToolDelta(&claudeResponses, blockIndex, info.ClaudeConvertInfo.ToolCallPendingArgs[blockIndex])
				delete(info.ClaudeConvertInfo.ToolCallPendingArgs, blockIndex)
				continue
			}

			appendClaudeToolDelta(&claudeResponses, blockIndex, toolCall.Function.Arguments)
		}
		info.ClaudeConvertInfo.ToolCallMaxIndexOffset = maxOffset
		info.ClaudeConvertInfo.Index = base + maxOffset
	}
	if info.SendResponseCount == 1 {
		msg := &dto.ClaudeMediaMessage{
			Id:    openAIResponse.Id,
			Model: openAIResponse.Model,
			Type:  "message",
			Role:  "assistant",
			Usage: &dto.ClaudeUsage{
				InputTokens:  info.GetEstimatePromptTokens(),
				OutputTokens: 0,
			},
		}
		msg.SetContent(make([]any, 0))
		claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
			Type:    "message_start",
			Message: msg,
		})
		//claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
		//	Type: "ping",
		//})
		// 判断首个响应是否存在内容（非标准的 OpenAI 响应）
		if len(openAIResponse.Choices) > 0 {
			delta := openAIResponse.Choices[0].Delta
			appendReasoningDelta(delta.GetReasoningContent())
			appendTextDelta(delta.GetContentString())
			appendToolCalls(delta.ToolCalls)
		}

		// 如果首块就带 finish_reason，需要立即发送停止块
		if len(openAIResponse.Choices) > 0 && openAIResponse.Choices[0].FinishReason != nil && *openAIResponse.Choices[0].FinishReason != "" {
			info.FinishReason = *openAIResponse.Choices[0].FinishReason
			stopOpenBlocks()
			oaiUsage := openAIResponse.Usage
			if oaiUsage == nil {
				oaiUsage = info.ClaudeConvertInfo.Usage
			}
			if oaiUsage != nil {
				claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
					Type:  "message_delta",
					Usage: buildClaudeUsageFromOpenAIUsage(oaiUsage),
					Delta: &dto.ClaudeMediaMessage{
						StopReason: common.GetPointer[string](stopReasonOpenAI2Claude(info.FinishReason)),
					},
				})
			}
			claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
				Type: "message_stop",
			})
			info.ClaudeConvertInfo.Done = true
		}
		return claudeResponses
	}

	if len(openAIResponse.Choices) == 0 {
		// Some OpenAI-compatible upstreams end with a usage-only SSE chunk.
		oaiUsage := openAIResponse.Usage
		if oaiUsage == nil {
			oaiUsage = info.ClaudeConvertInfo.Usage
		}
		if oaiUsage != nil {
			stopOpenBlocks()
			stopReason := stopReasonOpenAI2Claude(info.FinishReason)
			if stopReason == "" {
				stopReason = "end_turn"
			}
			claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
				Type:  "message_delta",
				Usage: buildClaudeUsageFromOpenAIUsage(oaiUsage),
				Delta: &dto.ClaudeMediaMessage{
					StopReason: common.GetPointer[string](stopReason),
				},
			})
			claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
				Type: "message_stop",
			})
			info.ClaudeConvertInfo.Done = true
		}
		return claudeResponses
	} else {
		chosenChoice := openAIResponse.Choices[0]
		doneChunk := chosenChoice.FinishReason != nil && *chosenChoice.FinishReason != ""
		if doneChunk {
			info.FinishReason = *chosenChoice.FinishReason
		}

		appendReasoningDelta(chosenChoice.Delta.GetReasoningContent())
		appendTextDelta(chosenChoice.Delta.GetContentString())
		appendToolCalls(chosenChoice.Delta.ToolCalls)

		if doneChunk && openAIResponse.Usage == nil && info.ClaudeConvertInfo.Usage == nil {
			// Some upstreams emit finish_reason with the final delta first,
			// then send usage in a later usage-only chunk.
			return claudeResponses
		}

		if doneChunk || info.ClaudeConvertInfo.Done {
			stopOpenBlocks()
			oaiUsage := openAIResponse.Usage
			if oaiUsage == nil {
				oaiUsage = info.ClaudeConvertInfo.Usage
			}
			if oaiUsage != nil {
				claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
					Type:  "message_delta",
					Usage: buildClaudeUsageFromOpenAIUsage(oaiUsage),
					Delta: &dto.ClaudeMediaMessage{
						StopReason: common.GetPointer[string](stopReasonOpenAI2Claude(info.FinishReason)),
					},
				})
			}
			claudeResponses = append(claudeResponses, &dto.ClaudeResponse{
				Type: "message_stop",
			})
			info.ClaudeConvertInfo.Done = true
			return claudeResponses
		}
	}

	return claudeResponses
}

func ResponseOpenAI2Claude(openAIResponse *dto.OpenAITextResponse, info *relaycommon.RelayInfo) *dto.ClaudeResponse {
	response, _ := ResponseOpenAI2ClaudeWithError(openAIResponse, info)
	return response
}

func ResponseOpenAI2ClaudeWithError(openAIResponse *dto.OpenAITextResponse, info *relaycommon.RelayInfo) (*dto.ClaudeResponse, error) {
	if openAIResponse == nil {
		return nil, fmt.Errorf("OpenAI response is nil")
	}
	if len(openAIResponse.Choices) > 1 {
		return nil, fmt.Errorf("multiple choices are not supported by Claude")
	}
	var stopReason string
	contents := make([]dto.ClaudeMediaMessage, 0)
	allowClaudeThinking := shouldExposeReasoningAsClaudeThinking(openAIResponse.Model, info)
	claudeResponse := &dto.ClaudeResponse{
		Id:    openAIResponse.Id,
		Type:  "message",
		Role:  "assistant",
		Model: openAIResponse.Model,
	}
	for _, choice := range openAIResponse.Choices {
		mappedStopReason, stopErr := stopReasonOpenAI2ClaudeStrict(choice.FinishReason)
		if stopErr != nil {
			return nil, stopErr
		}
		stopReason = mappedStopReason
		if choice.Message.Content != nil && !choice.Message.IsStringContent() {
			parts, parseErr := choice.Message.ParseContentStrict()
			if parseErr != nil {
				return nil, fmt.Errorf("choice[%d]: %w", choice.Index, parseErr)
			}
			for contentIndex, part := range parts {
				if part.Type != dto.ContentTypeText {
					return nil, fmt.Errorf("choice[%d] content[%d] type %q is not supported by Claude", choice.Index, contentIndex, part.Type)
				}
			}
		}
		toolCalls, parseErr := choice.Message.ParseToolCallsStrict()
		if parseErr != nil {
			return nil, fmt.Errorf("choice[%d]: %w", choice.Index, parseErr)
		}
		// 上游可能把推理以 think 标签内嵌在可见文本里；非流式也走一次提取，
		// 与流式 splitter 行为对齐（见 TestThinkTaggedResponseOpenAI2ClaudeKeepsReasoning）。
		taggedText := choice.Message.StringContent()
		tagUnlock := false
		if tagVisible, tagReasoning, tagFound := relaycommon.SplitThinkTaggedContent(taggedText); tagFound {
			choice.Message.Content = tagVisible
			// 与流式 appendReasoningWithoutDuplication 对齐：已有推理内容时
			// 按包含关系判重后追加，上游双写 reasoning_content 与 think 标签
			// 且内容一致时不输出两份推理；tag 内为空不引入孤立换行。
			if existing := choice.Message.GetReasoningContent(); existing != "" {
				merged := existing
				if tagReasoning != "" && !strings.Contains(existing, tagReasoning) {
					merged += "\n" + tagReasoning
				}
				choice.Message.ReasoningContent = &merged
			} else {
				choice.Message.ReasoningContent = &tagReasoning
			}
			// 提取到完整标签对本身就证明这块内容是推理，直接解锁 thinking 块，
			// 不再依赖模型名门控。
			tagUnlock = true
		}
		if allowClaudeThinking || tagUnlock {
			reasoningContent := choice.Message.GetReasoningContent()
			preserveEmptyReasoning := reasoningContent == "" &&
				(choice.Message.ReasoningContent != nil || choice.Message.ThinkingContent != nil || choice.Message.Reasoning != nil) &&
				len(toolCalls) > 0
			if reasoningContent != "" || preserveEmptyReasoning {
				claudeContent := dto.ClaudeMediaMessage{}
				claudeContent.Type = "thinking"
				claudeContent.Thinking = common.GetPointer[string](reasoningContent)
				contents = append(contents, claudeContent)
			}
		}
		if textContent := choice.Message.StringContent(); textContent != "" || choice.FinishReason != "tool_calls" {
			claudeContent := dto.ClaudeMediaMessage{}
			claudeContent.Type = "text"
			claudeContent.SetText(textContent)
			contents = append(contents, claudeContent)
		}
		for _, toolUse := range toolCalls {
			if strings.TrimSpace(toolUse.ID) == "" || strings.TrimSpace(toolUse.Function.Name) == "" {
				return nil, fmt.Errorf("choice[%d] tool call requires id and function name", choice.Index)
			}
			claudeContent := dto.ClaudeMediaMessage{}
			claudeContent.Type = "tool_use"
			claudeContent.Id = toolUse.ID
			claudeContent.Name = toolUse.Function.Name
			mapParams := make(map[string]interface{})
			arguments := strings.TrimSpace(toolUse.Function.Arguments)
			if arguments != "" {
				if err := common.Unmarshal([]byte(arguments), &mapParams); err != nil {
					return nil, fmt.Errorf("choice[%d] tool %q arguments must be a JSON object: %w", choice.Index, toolUse.Function.Name, err)
				}
			}
			claudeContent.Input = mapParams
			contents = append(contents, claudeContent)
		}
	}
	claudeResponse.Content = contents
	claudeResponse.StopReason = stopReason
	claudeResponse.Usage = buildClaudeUsageFromOpenAIUsage(&openAIResponse.Usage)

	return claudeResponse, nil
}

func stopReasonOpenAI2Claude(reason string) string {
	return reasonmap.OpenAIFinishReasonToClaudeStopReason(reason)
}

func stopReasonOpenAI2ClaudeStrict(reason string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(reason))
	switch normalized {
	case "":
		return "end_turn", nil
	case "stop", "stop_sequence", "length", "max_tokens", "content_filter", "tool_calls":
		return reasonmap.OpenAIFinishReasonToClaudeStopReason(normalized), nil
	case "function_call":
		return "tool_use", nil
	default:
		return "", fmt.Errorf("OpenAI finish_reason %q is not supported by Claude Messages", reason)
	}
}

func toJSONString(v interface{}) (string, error) {
	b, err := common.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func parseGeminiTools(raw common.RawMessage) ([]dto.GeminiChatTool, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || common.GetJsonType(raw) == "null" {
		return nil, nil
	}

	switch common.GetJsonType(raw) {
	case "array":
		var tools []dto.GeminiChatTool
		if err := common.Unmarshal(raw, &tools); err != nil {
			return nil, fmt.Errorf("failed to parse Gemini tools: %w", err)
		}
		return tools, nil
	case "object":
		var tool dto.GeminiChatTool
		if err := common.Unmarshal(raw, &tool); err != nil {
			return nil, fmt.Errorf("failed to parse Gemini tool: %w", err)
		}
		return []dto.GeminiChatTool{tool}, nil
	default:
		return nil, fmt.Errorf("Gemini tools must be an object or array")
	}
}

func parseClaudeToolsForOpenAI(raw any) ([]dto.Tool, error) {
	if raw == nil {
		return nil, nil
	}
	rawJSON, err := common.Marshal(raw)
	if err != nil {
		return nil, err
	}
	if common.GetJsonType(rawJSON) == "null" {
		return nil, nil
	}
	if common.GetJsonType(rawJSON) != "array" {
		return nil, fmt.Errorf("Claude tools must be an array")
	}
	var rawTools []common.RawMessage
	if err := common.Unmarshal(rawJSON, &rawTools); err != nil {
		return nil, err
	}
	tools := make([]dto.Tool, 0, len(rawTools))
	for index, rawTool := range rawTools {
		if common.GetJsonType(rawTool) != "object" {
			return nil, fmt.Errorf("Claude tools[%d] must be an object", index)
		}
		var toolMap map[string]any
		if err := common.Unmarshal(rawTool, &toolMap); err != nil {
			return nil, err
		}
		toolType := strings.TrimSpace(common.Interface2String(toolMap["type"]))
		if toolType != "" && toolType != "custom" {
			return nil, fmt.Errorf("Claude built-in tool type %q cannot be converted to an OpenAI function tool", toolType)
		}
		var tool dto.Tool
		if err := common.Unmarshal(rawTool, &tool); err != nil {
			return nil, err
		}
		tool.Name = strings.TrimSpace(tool.Name)
		if tool.Name == "" {
			return nil, fmt.Errorf("Claude tools[%d] name is required", index)
		}
		if tool.InputSchema == nil {
			tool.InputSchema = map[string]interface{}{"type": "object"}
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

func geminiFunctionArgumentsToObject(arguments string) map[string]interface{} {
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

func geminiFunctionArgumentsToObjectStrict(arguments string) (map[string]interface{}, error) {
	if strings.TrimSpace(arguments) == "" {
		return map[string]interface{}{}, nil
	}
	var args map[string]interface{}
	if err := common.Unmarshal([]byte(arguments), &args); err != nil {
		return nil, fmt.Errorf("Gemini functionCall arguments must be a JSON object: %w", err)
	}
	if args == nil {
		return map[string]interface{}{}, nil
	}
	return args, nil
}

func GeminiToOpenAIRequest(geminiRequest *dto.GeminiChatRequest, info *relaycommon.RelayInfo) (*dto.GeneralOpenAIRequest, error) {
	if geminiRequest == nil {
		return nil, fmt.Errorf("Gemini request is nil")
	}
	if info == nil {
		return nil, fmt.Errorf("relay info is required for Gemini conversion")
	}
	openaiRequest := &dto.GeneralOpenAIRequest{
		Model:  info.UpstreamModelName,
		Stream: lo.ToPtr(info.IsStream),
	}
	if len(geminiRequest.GenerationConfig.ResponseModalities) > 0 {
		for _, modality := range geminiRequest.GenerationConfig.ResponseModalities {
			if strings.ToUpper(strings.TrimSpace(modality)) != "TEXT" {
				return nil, fmt.Errorf("Gemini response modality %q cannot be represented by OpenAI Chat Completions", modality)
			}
		}
	}
	if geminiJSONFieldEnabled(geminiRequest.GenerationConfig.SpeechConfig) {
		return nil, fmt.Errorf("Gemini speechConfig cannot be represented by OpenAI Chat Completions")
	}
	if geminiJSONFieldEnabled(geminiRequest.GenerationConfig.ImageConfig) {
		return nil, fmt.Errorf("Gemini imageConfig cannot be represented by OpenAI Chat Completions")
	}
	if geminiRequest.GenerationConfig.ResponseMimeType != "" {
		if geminiRequest.GenerationConfig.ResponseMimeType != "application/json" {
			return nil, fmt.Errorf("Gemini responseMimeType %q cannot be represented by OpenAI Chat Completions", geminiRequest.GenerationConfig.ResponseMimeType)
		}
		openaiRequest.ResponseFormat = &dto.ResponseFormat{Type: "json_object"}
	}
	if geminiRequest.GenerationConfig.ResponseSchema != nil || geminiJSONFieldEnabled(geminiRequest.GenerationConfig.ResponseJsonSchema) {
		var schema any = geminiRequest.GenerationConfig.ResponseSchema
		if schema == nil {
			var decoded any
			if err := common.Unmarshal(geminiRequest.GenerationConfig.ResponseJsonSchema, &decoded); err != nil {
				return nil, fmt.Errorf("failed to parse Gemini responseJsonSchema: %w", err)
			}
			schema = decoded
		}
		schemaBytes, schemaErr := common.Marshal(schema)
		if schemaErr != nil {
			return nil, fmt.Errorf("failed to encode Gemini response schema: %w", schemaErr)
		}
		if common.GetJsonType(schemaBytes) != "object" {
			return nil, fmt.Errorf("Gemini response schema must be a JSON object")
		}
		schemaRaw, err := common.Marshal(map[string]any{
			"name":   "response",
			"schema": schema,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to encode Gemini response schema: %w", err)
		}
		openaiRequest.ResponseFormat = &dto.ResponseFormat{Type: "json_schema", JsonSchema: schemaRaw}
	}

	// 转换 messages。Gemini 的 functionCall/functionResponse 通过显式 id
	// 关联；没有 id 的旧客户端则按同名调用的先后顺序关联。
	var messages []dto.Message
	pendingCallIDsByName := make(map[string][]string)
	callNamesByID := make(map[string]string)
	callCounter := 0
	for contentIndex, content := range geminiRequest.Contents {
		role, err := convertGeminiRoleToOpenAI(content.Role)
		if err != nil {
			return nil, fmt.Errorf("Gemini contents[%d]: %w", contentIndex, err)
		}
		message := dto.Message{
			Role: role,
		}

		var mediaContents []dto.MediaContent
		var toolCalls []dto.ToolCallRequest
		var reasoningBuilder strings.Builder
		reasoningSeen := false
		flushMessage := func() {
			if len(toolCalls) > 0 {
				message.SetToolCalls(toolCalls)
			}
			if len(mediaContents) == 1 && mediaContents[0].Type == "text" {
				message.Content = mediaContents[0].Text
			} else if len(mediaContents) > 0 {
				message.SetMediaContent(mediaContents)
			}
			if reasoningSeen {
				reasoning := reasoningBuilder.String()
				message.ReasoningContent = &reasoning
			}
			if len(mediaContents) > 0 || len(toolCalls) > 0 || reasoningSeen {
				messages = append(messages, message)
			}
			message = dto.Message{Role: role}
			mediaContents = nil
			toolCalls = nil
			reasoningBuilder.Reset()
			reasoningSeen = false
		}
		for partIndex, part := range content.Parts {
			if err := validateGeminiRequestPartForOpenAI(part, contentIndex, partIndex); err != nil {
				return nil, err
			}
			switch {
			case part.FunctionCall != nil:
				callID := strings.TrimSpace(part.FunctionCall.ID)
				if callID == "" {
					for {
						callCounter++
						callID = fmt.Sprintf("call_%d", callCounter)
						if _, exists := callNamesByID[callID]; !exists {
							break
						}
					}
				}
				if _, exists := callNamesByID[callID]; exists {
					return nil, fmt.Errorf("gemini functionCall id %q is duplicated", callID)
				}
				functionName := strings.TrimSpace(part.FunctionCall.FunctionName)
				if functionName == "" {
					return nil, fmt.Errorf("gemini functionCall name is required")
				}
				arguments, err := geminiFunctionCallArgumentsToJSONString(part.FunctionCall.Arguments)
				if err != nil {
					return nil, fmt.Errorf("Gemini contents[%d].parts[%d] functionCall %s: %w", contentIndex, partIndex, functionName, err)
				}
				toolCalls = append(toolCalls, dto.ToolCallRequest{
					ID:   callID,
					Type: "function",
					Function: dto.FunctionRequest{
						Name:      functionName,
						Arguments: arguments,
					},
				})
				pendingCallIDsByName[functionName] = append(pendingCallIDsByName[functionName], callID)
				callNamesByID[callID] = functionName
			case part.FunctionResponse != nil:
				// A Gemini content may legally mix functionResponse and text parts.
				// OpenAI Chat Completions has no inline function-response part, so
				// flush the content accumulated before this response and emit the
				// tool message at this exact position instead of appending all tool
				// messages after the content (which would reorder functionResponse,
				// text into text, functionResponse).
				flushMessage()
				functionResponse := part.FunctionResponse
				callID, err := geminiFunctionResponseID(functionResponse.ID)
				if err != nil {
					return nil, err
				}
				if callID != "" {
					expectedName, knownCall := callNamesByID[callID]
					if !knownCall {
						return nil, fmt.Errorf("gemini functionResponse references unknown functionCall id %q", callID)
					}
					if strings.TrimSpace(functionResponse.Name) != "" && strings.TrimSpace(functionResponse.Name) != expectedName {
						return nil, fmt.Errorf("gemini functionResponse name %q does not match functionCall %q for id %q", functionResponse.Name, expectedName, callID)
					}
				}
				functionName := strings.TrimSpace(functionResponse.Name)
				if functionName == "" && callID != "" {
					functionName = callNamesByID[callID]
				}
				if functionName == "" {
					return nil, fmt.Errorf("gemini functionResponse name is required")
				}
				if callID == "" {
					pending := pendingCallIDsByName[functionName]
					if len(pending) == 0 {
						return nil, fmt.Errorf("cannot match gemini functionResponse %q to a functionCall id", functionName)
					}
					callID = pending[0]
					pendingCallIDsByName[functionName] = pending[1:]
				} else {
					pending := pendingCallIDsByName[functionName]
					matched := false
					for i, pendingID := range pending {
						if pendingID == callID {
							pendingCallIDsByName[functionName] = append(pending[:i], pending[i+1:]...)
							matched = true
							break
						}
					}
					if !matched {
						return nil, fmt.Errorf("gemini functionResponse for id %q has no pending functionCall", callID)
					}
				}
				result, err := toJSONString(functionResponse.Response)
				if err != nil {
					return nil, fmt.Errorf("failed to encode Gemini functionResponse %s: %w", functionName, err)
				}
				toolMessage := dto.Message{Role: "tool", ToolCallId: callID}
				toolMessage.SetStringContent(result)
				messages = append(messages, toolMessage)
			case part.Thought:
				reasoningSeen = true
				if part.Text != "" {
					if reasoningBuilder.Len() > 0 {
						reasoningBuilder.WriteString("\n")
					}
					reasoningBuilder.WriteString(part.Text)
				}
			case part.Text != "":
				mediaContents = append(mediaContents, dto.MediaContent{Type: "text", Text: part.Text})
			case part.InlineData != nil:
				media, err := geminiInlineDataToOpenAIMedia(part.InlineData)
				if err != nil {
					return nil, fmt.Errorf("Gemini contents[%d].parts[%d]: %w", contentIndex, partIndex, err)
				}
				mediaContents = append(mediaContents, media)
			case part.FileData != nil:
				media, err := geminiFileDataToOpenAIMedia(part.FileData)
				if err != nil {
					return nil, fmt.Errorf("Gemini contents[%d].parts[%d]: %w", contentIndex, partIndex, err)
				}
				mediaContents = append(mediaContents, media)
			case part.ExecutableCode != nil, part.CodeExecutionResult != nil,
				len(part.ThoughtSignature) > 0, len(part.MediaResolution) > 0, len(part.VideoMetadata) > 0:
				return nil, fmt.Errorf("Gemini contents[%d].parts[%d] contains a field that cannot be represented by OpenAI Chat Completions", contentIndex, partIndex)
			default:
				return nil, fmt.Errorf("Gemini contents[%d].parts[%d] has no supported content", contentIndex, partIndex)
			}
		}

		flushMessage()
	}

	openaiRequest.Messages = messages

	if geminiRequest.GenerationConfig.Temperature != nil {
		openaiRequest.Temperature = geminiRequest.GenerationConfig.Temperature
	}
	if geminiRequest.GenerationConfig.TopP != nil {
		openaiRequest.TopP = lo.ToPtr(*geminiRequest.GenerationConfig.TopP)
	}
	if geminiRequest.GenerationConfig.TopK != nil {
		topK := *geminiRequest.GenerationConfig.TopK
		if topK < 0 || topK != float64(int(topK)) {
			return nil, fmt.Errorf("Gemini topK must be a non-negative integer")
		}
		openaiRequest.TopK = lo.ToPtr(int(topK))
	}
	if geminiRequest.GenerationConfig.MaxOutputTokens != nil && *geminiRequest.GenerationConfig.MaxOutputTokens > 0 {
		openaiRequest.MaxTokens = lo.ToPtr(*geminiRequest.GenerationConfig.MaxOutputTokens)
	}
	// OpenAI Chat Completions accepts at most four stop sequences. Refuse a
	// fifth sequence instead of silently truncating it.
	if len(geminiRequest.GenerationConfig.StopSequences) > 0 {
		if len(geminiRequest.GenerationConfig.StopSequences) > 4 {
			return nil, fmt.Errorf("Gemini stopSequences has %d entries; OpenAI supports at most 4", len(geminiRequest.GenerationConfig.StopSequences))
		}
		for index, stop := range geminiRequest.GenerationConfig.StopSequences {
			if strings.TrimSpace(stop) == "" {
				return nil, fmt.Errorf("Gemini stopSequences[%d] must not be empty", index)
			}
		}
		openaiRequest.Stop = append([]string(nil), geminiRequest.GenerationConfig.StopSequences...)
	}
	if geminiRequest.GenerationConfig.CandidateCount != nil {
		if *geminiRequest.GenerationConfig.CandidateCount <= 0 {
			return nil, fmt.Errorf("Gemini candidateCount must be a positive integer")
		}
		openaiRequest.N = lo.ToPtr(*geminiRequest.GenerationConfig.CandidateCount)
	}
	if geminiRequest.GenerationConfig.PresencePenalty != nil {
		openaiRequest.PresencePenalty = lo.ToPtr(float64(*geminiRequest.GenerationConfig.PresencePenalty))
	}
	if geminiRequest.GenerationConfig.FrequencyPenalty != nil {
		openaiRequest.FrequencyPenalty = lo.ToPtr(float64(*geminiRequest.GenerationConfig.FrequencyPenalty))
	}

	// 转换工具调用
	geminiTools, err := parseGeminiTools(geminiRequest.Tools)
	if err != nil {
		return nil, err
	}
	if len(geminiTools) > 0 {
		var tools []dto.ToolCallRequest
		for toolIndex, tool := range geminiTools {
			if tool.GoogleSearch != nil || tool.GoogleSearchRetrieval != nil || tool.CodeExecution != nil || tool.URLContext != nil {
				return nil, fmt.Errorf("Gemini tools[%d] contains a server-side tool that cannot be represented by OpenAI Chat Completions", toolIndex)
			}
			if tool.FunctionDeclarations != nil {
				functionDeclarations, err := common.Any2Type[[]dto.FunctionRequest](tool.FunctionDeclarations)
				if err != nil {
					return nil, fmt.Errorf("failed to parse Gemini tools[%d].functionDeclarations: %w", toolIndex, err)
				}
				for functionIndex, function := range functionDeclarations {
					if strings.TrimSpace(function.Name) == "" {
						return nil, fmt.Errorf("Gemini tools[%d].functionDeclarations[%d].name is required", toolIndex, functionIndex)
					}
					openAITool := dto.ToolCallRequest{
						Type: "function",
						Function: dto.FunctionRequest{
							Name:        function.Name,
							Description: function.Description,
							Parameters:  function.Parameters,
						},
					}
					tools = append(tools, openAITool)
				}
			}
		}
		if len(tools) > 0 {
			openaiRequest.Tools = tools
		}
	}
	if geminiRequest.ToolConfig != nil {
		toolChoice, err := geminiToolConfigToOpenAIToolChoice(geminiRequest.ToolConfig)
		if err != nil {
			return nil, err
		}
		openaiRequest.ToolChoice = toolChoice
	}

	// gemini system instructions
	if geminiRequest.SystemInstructions != nil {
		if geminiRequest.SystemInstructions.Role != "" && geminiRequest.SystemInstructions.Role != "system" {
			return nil, fmt.Errorf("Gemini systemInstruction role %q is not supported", geminiRequest.SystemInstructions.Role)
		}
		systemParts := make([]dto.MediaContent, 0, len(geminiRequest.SystemInstructions.Parts))
		for index, part := range geminiRequest.SystemInstructions.Parts {
			if part.Thought || part.FunctionCall != nil || part.FunctionResponse != nil || part.ExecutableCode != nil || part.CodeExecutionResult != nil || part.InlineData != nil || part.FileData != nil {
				return nil, fmt.Errorf("Gemini systemInstruction part[%d] is not supported by OpenAI Chat Completions", index)
			}
			switch {
			case part.Text != "":
				systemParts = append(systemParts, dto.MediaContent{Type: dto.ContentTypeText, Text: part.Text})
			default:
				return nil, fmt.Errorf("Gemini systemInstruction part[%d] has no supported content", index)
			}
		}
		systemMessage := dto.Message{Role: "system"}
		if len(systemParts) == 1 && systemParts[0].Type == dto.ContentTypeText {
			systemMessage.SetStringContent(systemParts[0].Text)
		} else {
			systemMessage.SetMediaContent(systemParts)
		}
		openaiRequest.Messages = append([]dto.Message{systemMessage}, openaiRequest.Messages...)
	}

	return openaiRequest, nil
}

func geminiFunctionResponseID(raw common.RawMessage) (string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || common.GetJsonType(raw) == "null" {
		return "", nil
	}
	if common.GetJsonType(raw) != "string" {
		return "", fmt.Errorf("gemini functionResponse id must be a string")
	}
	return common.JsonRawMessageToString(raw), nil
}

func validateGeminiRequestPartForOpenAI(part dto.GeminiPart, contentIndex int, partIndex int) error {
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
		return fmt.Errorf("Gemini contents[%d].parts[%d] contains multiple content fields", contentIndex, partIndex)
	}
	if part.Thought && (part.InlineData != nil || part.FunctionCall != nil || part.FunctionResponse != nil || part.FileData != nil || part.ExecutableCode != nil || part.CodeExecutionResult != nil) {
		return fmt.Errorf("Gemini contents[%d].parts[%d] thought cannot be combined with non-text content", contentIndex, partIndex)
	}
	if len(part.ThoughtSignature) > 0 && common.GetJsonType(part.ThoughtSignature) != "null" {
		return fmt.Errorf("Gemini contents[%d].parts[%d].thoughtSignature cannot be represented by OpenAI Chat Completions", contentIndex, partIndex)
	}
	if len(part.MediaResolution) > 0 && common.GetJsonType(part.MediaResolution) != "null" {
		return fmt.Errorf("Gemini contents[%d].parts[%d].mediaResolution cannot be represented by OpenAI Chat Completions", contentIndex, partIndex)
	}
	if len(part.VideoMetadata) > 0 && common.GetJsonType(part.VideoMetadata) != "null" {
		return fmt.Errorf("Gemini contents[%d].parts[%d].videoMetadata cannot be represented by OpenAI Chat Completions", contentIndex, partIndex)
	}
	if functionResponse := part.FunctionResponse; functionResponse != nil {
		if len(functionResponse.WillContinue) > 0 && common.GetJsonType(functionResponse.WillContinue) != "null" {
			return fmt.Errorf("Gemini contents[%d].parts[%d].functionResponse.willContinue cannot be represented by OpenAI Chat Completions", contentIndex, partIndex)
		}
		if len(functionResponse.Scheduling) > 0 && common.GetJsonType(functionResponse.Scheduling) != "null" {
			return fmt.Errorf("Gemini contents[%d].parts[%d].functionResponse.scheduling cannot be represented by OpenAI Chat Completions", contentIndex, partIndex)
		}
		if len(functionResponse.Parts) > 0 && common.GetJsonType(functionResponse.Parts) != "null" {
			return fmt.Errorf("Gemini contents[%d].parts[%d].functionResponse.parts cannot be represented by OpenAI Chat Completions", contentIndex, partIndex)
		}
	}
	return nil
}

func convertGeminiRoleToOpenAI(geminiRole string) (string, error) {
	switch strings.TrimSpace(geminiRole) {
	case "user":
		return "user", nil
	case "model":
		return "assistant", nil
	case "function":
		return "function", nil
	case "":
		// Gemini omits role on some first-turn requests. Treat an omitted role
		// as user, but never reinterpret an unknown role as user.
		return "user", nil
	default:
		return "", fmt.Errorf("role %q is not supported by OpenAI Chat Completions", geminiRole)
	}
}

func geminiFunctionCallArgumentsToJSONString(arguments any) (string, error) {
	if arguments == nil {
		return "{}", nil
	}
	if raw, ok := arguments.(common.RawMessage); ok {
		arguments = raw
	}
	if raw, ok := arguments.([]byte); ok {
		arguments = common.RawMessage(raw)
	}
	if raw, ok := arguments.(common.RawMessage); ok {
		if strings.TrimSpace(string(raw)) == "" || common.GetJsonType(raw) == "null" {
			return "{}", nil
		}
		if common.GetJsonType(raw) != "object" {
			return "", fmt.Errorf("arguments must be a JSON object")
		}
		return string(raw), nil
	}
	if text, ok := arguments.(string); ok {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			return "{}", nil
		}
		var object map[string]interface{}
		if err := common.Unmarshal([]byte(trimmed), &object); err != nil || object == nil {
			return "", fmt.Errorf("arguments must be a JSON object")
		}
		return trimmed, nil
	}
	encoded, err := common.Marshal(arguments)
	if err != nil {
		return "", fmt.Errorf("arguments must be a JSON object: %w", err)
	}
	if common.GetJsonType(encoded) != "object" {
		return "", fmt.Errorf("arguments must be a JSON object")
	}
	return string(encoded), nil
}

func geminiInlineDataToOpenAIMedia(data *dto.GeminiInlineData) (dto.MediaContent, error) {
	if data == nil || strings.TrimSpace(data.MimeType) == "" || strings.TrimSpace(data.Data) == "" {
		return dto.MediaContent{}, fmt.Errorf("inlineData requires mimeType and data")
	}
	mimeType := strings.ToLower(strings.TrimSpace(data.MimeType))
	dataURL := fmt.Sprintf("data:%s;base64,%s", data.MimeType, data.Data)
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return dto.MediaContent{Type: dto.ContentTypeImageURL, ImageUrl: &dto.MessageImageUrl{Url: dataURL, Detail: "auto", MimeType: data.MimeType}}, nil
	case strings.HasPrefix(mimeType, "audio/"):
		format := strings.TrimPrefix(mimeType, "audio/")
		if format == "" {
			return dto.MediaContent{}, fmt.Errorf("inlineData audio MIME type is invalid")
		}
		return dto.MediaContent{Type: dto.ContentTypeInputAudio, InputAudio: &dto.MessageInputAudio{Data: data.Data, Format: format}}, nil
	case strings.HasPrefix(mimeType, "video/"):
		return dto.MediaContent{Type: dto.ContentTypeVideoUrl, VideoUrl: &dto.MessageVideoUrl{Url: dataURL}}, nil
	default:
		return dto.MediaContent{Type: dto.ContentTypeFile, File: &dto.MessageFile{FileData: data.Data, MimeType: data.MimeType}}, nil
	}
}

func geminiFileDataToOpenAIMedia(data *dto.GeminiFileData) (dto.MediaContent, error) {
	if data == nil || strings.TrimSpace(data.FileUri) == "" {
		return dto.MediaContent{}, fmt.Errorf("fileData requires fileUri")
	}
	mimeType := strings.ToLower(strings.TrimSpace(data.MimeType))
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return dto.MediaContent{Type: dto.ContentTypeImageURL, ImageUrl: &dto.MessageImageUrl{Url: data.FileUri, Detail: "auto", MimeType: data.MimeType}}, nil
	case strings.HasPrefix(mimeType, "video/"):
		return dto.MediaContent{Type: dto.ContentTypeVideoUrl, VideoUrl: &dto.MessageVideoUrl{Url: data.FileUri}}, nil
	default:
		return dto.MediaContent{Type: dto.ContentTypeFile, File: &dto.MessageFile{FileUrl: data.FileUri, MimeType: data.MimeType}}, nil
	}
}

func geminiToolConfigToOpenAIToolChoice(config *dto.ToolConfig) (any, error) {
	if config == nil {
		return nil, nil
	}
	if config.RetrievalConfig != nil {
		return nil, fmt.Errorf("Gemini retrieval tool configuration cannot be represented by OpenAI Chat Completions")
	}
	if config.IncludeServerSideToolInvocations != nil {
		return nil, fmt.Errorf("Gemini includeServerSideToolInvocations cannot be represented by OpenAI Chat Completions")
	}
	if config.FunctionCallingConfig == nil {
		return nil, nil
	}
	functionConfig := config.FunctionCallingConfig
	mode := strings.ToUpper(strings.TrimSpace(string(functionConfig.Mode)))
	switch mode {
	case "AUTO":
		if len(functionConfig.AllowedFunctionNames) > 0 {
			return nil, fmt.Errorf("Gemini functionCallingConfig.allowedFunctionNames is only supported with mode ANY")
		}
		return "auto", nil
	case "NONE":
		if len(functionConfig.AllowedFunctionNames) > 0 {
			return nil, fmt.Errorf("Gemini functionCallingConfig.allowedFunctionNames is only supported with mode ANY")
		}
		return "none", nil
	case "ANY":
		if len(functionConfig.AllowedFunctionNames) == 0 {
			return "required", nil
		}
		if len(functionConfig.AllowedFunctionNames) != 1 || strings.TrimSpace(functionConfig.AllowedFunctionNames[0]) == "" {
			return nil, fmt.Errorf("Gemini functionCallingConfig.allowedFunctionNames must contain exactly one non-empty name for OpenAI Chat Completions")
		}
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": strings.TrimSpace(functionConfig.AllowedFunctionNames[0]),
			},
		}, nil
	case "":
		return nil, fmt.Errorf("Gemini functionCallingConfig.mode is required")
	default:
		return nil, fmt.Errorf("Gemini functionCallingConfig.mode %q is not supported by OpenAI Chat Completions", functionConfig.Mode)
	}
}

func geminiJSONFieldEnabled(raw common.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return false
	}
	switch common.GetJsonType([]byte(trimmed)) {
	case "object":
		var object map[string]any
		if err := common.Unmarshal([]byte(trimmed), &object); err != nil {
			return true
		}
		return len(object) > 0
	case "array":
		var array []common.RawMessage
		if err := common.Unmarshal([]byte(trimmed), &array); err != nil {
			return true
		}
		return len(array) > 0
	default:
		return true
	}
}

func extractTextFromGeminiParts(parts []dto.GeminiPart) string {
	var texts []string
	for _, part := range parts {
		if part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// ResponseOpenAI2Gemini 将 OpenAI 响应转换为 Gemini 格式
func ResponseOpenAI2Gemini(openAIResponse *dto.OpenAITextResponse, info *relaycommon.RelayInfo) *dto.GeminiChatResponse {
	response, _ := ResponseOpenAI2GeminiWithError(openAIResponse, info)
	return response
}

func ResponseOpenAI2GeminiWithError(openAIResponse *dto.OpenAITextResponse, info *relaycommon.RelayInfo) (*dto.GeminiChatResponse, error) {
	if openAIResponse == nil {
		return nil, fmt.Errorf("OpenAI response is nil")
	}
	geminiResponse := &dto.GeminiChatResponse{
		Candidates: make([]dto.GeminiChatCandidate, 0, len(openAIResponse.Choices)),
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:     openAIResponse.PromptTokens,
			CandidatesTokenCount: openAIResponse.CompletionTokens,
			TotalTokenCount:      openAIResponse.PromptTokens + openAIResponse.CompletionTokens,
		},
	}

	for _, choice := range openAIResponse.Choices {
		candidate := dto.GeminiChatCandidate{
			Index:         int64(choice.Index),
			SafetyRatings: []dto.GeminiChatSafetyRating{},
		}
		toolCalls, parseErr := choice.Message.ParseToolCallsStrict()
		if parseErr != nil {
			return nil, fmt.Errorf("choice[%d]: %w", choice.Index, parseErr)
		}

		finishReason, err := openAIFinishReasonToGemini(choice.FinishReason)
		if err != nil {
			return nil, fmt.Errorf("choice[%d]: %w", choice.Index, err)
		}
		candidate.FinishReason = &finishReason

		// 转换消息内容
		content := dto.GeminiChatContent{
			Role:  "model",
			Parts: make([]dto.GeminiPart, 0),
		}

		// OpenAI stores reasoning, visible text, and tool calls in separate
		// fields. Gemini can represent all three parts in one candidate, so do
		// not let tool calls suppress either form of text.
		if choice.Message.ReasoningContent != nil || choice.Message.ThinkingContent != nil || choice.Message.Reasoning != nil {
			content.Parts = append(content.Parts, dto.GeminiPart{
				Thought: true,
				Text:    choice.Message.GetReasoningContent(),
			})
		}
		if choice.Message.Content != nil && !choice.Message.IsStringContent() {
			parts, parseErr := choice.Message.ParseContentStrict()
			if parseErr != nil {
				return nil, fmt.Errorf("choice[%d] content: %w", choice.Index, parseErr)
			}
			mediaParts, err := openAIMediaContentToGemini(parts)
			if err != nil {
				return nil, fmt.Errorf("choice[%d] content: %w", choice.Index, err)
			}
			content.Parts = append(content.Parts, mediaParts...)
		} else if textContent := choice.Message.StringContent(); textContent != "" {
			// 上游可能把推理以 think 标签内嵌在可见文本里；转为 thought part
			// 并从可见文本中移除，与流式路径行为一致。无标签时原样透传。
			if visible, reasoning, found := relaycommon.SplitThinkTaggedContent(textContent); found {
				if reasoning != "" {
					content.Parts = append(content.Parts, dto.GeminiPart{Thought: true, Text: reasoning})
				}
				textContent = visible
			}
			if textContent != "" {
				content.Parts = append(content.Parts, dto.GeminiPart{Text: textContent})
			}
		}
		for _, toolCall := range toolCalls {
			if strings.TrimSpace(toolCall.ID) == "" || strings.TrimSpace(toolCall.Function.Name) == "" {
				return nil, fmt.Errorf("choice[%d] tool call requires id and function name", choice.Index)
			}
			if toolCall.Type != "" && toolCall.Type != "function" {
				return nil, fmt.Errorf("choice[%d] tool call type %q is not supported by Gemini", choice.Index, toolCall.Type)
			}
			args, err := geminiFunctionArgumentsToObjectStrict(toolCall.Function.Arguments)
			if err != nil {
				return nil, fmt.Errorf("choice[%d] tool %q arguments: %w", choice.Index, toolCall.Function.Name, err)
			}
			content.Parts = append(content.Parts, dto.GeminiPart{
				FunctionCall: &dto.FunctionCall{
					ID:           toolCall.ID,
					FunctionName: toolCall.Function.Name,
					Arguments:    args,
				},
			})
		}

		candidate.Content = content
		geminiResponse.Candidates = append(geminiResponse.Candidates, candidate)
	}

	return geminiResponse, nil
}

func openAIFinishReasonToGemini(reason string) (string, error) {
	switch strings.TrimSpace(reason) {
	case "", "stop", "tool_calls", "function_call":
		return "STOP", nil
	case "length":
		return "MAX_TOKENS", nil
	case "content_filter":
		return "SAFETY", nil
	default:
		return "", fmt.Errorf("finish_reason %q is not supported by Gemini", reason)
	}
}

func openAIMediaContentToGemini(messages []dto.MediaContent) ([]dto.GeminiPart, error) {
	parts := make([]dto.GeminiPart, 0, len(messages))
	for index, message := range messages {
		switch message.Type {
		case dto.ContentTypeText:
			if message.Text != "" {
				parts = append(parts, dto.GeminiPart{Text: message.Text})
			}
		case dto.ContentTypeImageURL:
			image := message.GetImageMedia()
			if image == nil || strings.TrimSpace(image.Url) == "" {
				return nil, fmt.Errorf("content[%d] image_url.url is required", index)
			}
			part, err := openAIURLToGeminiMedia(image.Url, image.MimeType, "image")
			if err != nil {
				return nil, fmt.Errorf("content[%d]: %w", index, err)
			}
			parts = append(parts, part)
		case dto.ContentTypeInputAudio:
			audio := message.GetInputAudio()
			if audio == nil || strings.TrimSpace(audio.Data) == "" || strings.TrimSpace(audio.Format) == "" {
				return nil, fmt.Errorf("content[%d] input_audio.data and format are required", index)
			}
			parts = append(parts, dto.GeminiPart{InlineData: &dto.GeminiInlineData{
				MimeType: "audio/" + strings.TrimPrefix(strings.ToLower(audio.Format), "audio/"),
				Data:     audio.Data,
			}})
		case dto.ContentTypeFile:
			file := message.GetFile()
			if file == nil {
				return nil, fmt.Errorf("content[%d] file is required", index)
			}
			if file.FileData != "" {
				if strings.TrimSpace(file.MimeType) == "" {
					return nil, fmt.Errorf("content[%d] file mime_type is required when file_data is used", index)
				}
				parts = append(parts, dto.GeminiPart{InlineData: &dto.GeminiInlineData{MimeType: file.MimeType, Data: file.FileData}})
			} else if strings.TrimSpace(file.FileUrl) != "" {
				parts = append(parts, dto.GeminiPart{FileData: &dto.GeminiFileData{MimeType: file.MimeType, FileUri: file.FileUrl}})
			} else {
				return nil, fmt.Errorf("content[%d] file requires file_data or file_url", index)
			}
		case dto.ContentTypeVideoUrl:
			video := message.GetVideoUrl()
			if video == nil || strings.TrimSpace(video.Url) == "" {
				return nil, fmt.Errorf("content[%d] video_url.url is required", index)
			}
			parts = append(parts, dto.GeminiPart{FileData: &dto.GeminiFileData{FileUri: video.Url}})
		default:
			return nil, fmt.Errorf("content[%d] type %q is not supported by Gemini", index, message.Type)
		}
	}
	return parts, nil
}

func openAIURLToGeminiMedia(url string, mimeType string, kind string) (dto.GeminiPart, error) {
	if strings.HasPrefix(strings.ToLower(url), "data:") {
		format, data, err := DecodeBase64FileData(url)
		if err != nil {
			return dto.GeminiPart{}, fmt.Errorf("invalid %s data URL: %w", kind, err)
		}
		return dto.GeminiPart{InlineData: &dto.GeminiInlineData{MimeType: format, Data: data}}, nil
	}
	return dto.GeminiPart{FileData: &dto.GeminiFileData{MimeType: mimeType, FileUri: url}}, nil
}

// StreamResponseOpenAI2Gemini 将 OpenAI 流式响应转换为 Gemini 格式。
// 保留此无错误返回的包装函数以兼容旧调用方；新的中继路径应使用
// StreamResponseOpenAI2GeminiWithError，以避免把不可表达的数据静默改写。
func StreamResponseOpenAI2Gemini(openAIResponse *dto.ChatCompletionsStreamResponse, info *relaycommon.RelayInfo) *dto.GeminiChatResponse {
	response, _ := StreamResponseOpenAI2GeminiWithError(openAIResponse, info)
	return response
}

func ensureGeminiConvertInfo(info *relaycommon.RelayInfo) *relaycommon.GeminiConvertInfo {
	if info == nil {
		return &relaycommon.GeminiConvertInfo{}
	}
	if info.GeminiConvertInfo == nil {
		info.GeminiConvertInfo = &relaycommon.GeminiConvertInfo{}
	}
	if info.GeminiConvertInfo.ToolCalls == nil {
		info.GeminiConvertInfo.ToolCalls = make(map[int]map[int]*relaycommon.GeminiToolCallState)
	}
	return info.GeminiConvertInfo
}

func geminiToolCallStateForChoice(convertInfo *relaycommon.GeminiConvertInfo, choiceIndex int, toolIndex int) *relaycommon.GeminiToolCallState {
	if convertInfo.ToolCalls == nil {
		convertInfo.ToolCalls = make(map[int]map[int]*relaycommon.GeminiToolCallState)
	}
	byIndex := convertInfo.ToolCalls[choiceIndex]
	if byIndex == nil {
		byIndex = make(map[int]*relaycommon.GeminiToolCallState)
		convertInfo.ToolCalls[choiceIndex] = byIndex
	}
	state := byIndex[toolIndex]
	if state == nil {
		state = &relaycommon.GeminiToolCallState{}
		byIndex[toolIndex] = state
	}
	return state
}

func geminiToolCallArguments(raw string) (map[string]interface{}, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, false
	}
	var args map[string]interface{}
	if common.Unmarshal([]byte(trimmed), &args) != nil {
		// A streamed OpenAI tool call may contain an incomplete JSON prefix.
		// Defer the decision until the call is complete, where the caller can
		// return a precise conversion error instead of corrupting the payload.
		return nil, false
	}
	if args == nil {
		return map[string]interface{}{}, true
	}
	return args, true
}

func flushGeminiToolCallState(
	convertInfo *relaycommon.GeminiConvertInfo,
	choiceIndex int,
	parts *[]dto.GeminiPart,
	final bool,
) error {
	byIndex := convertInfo.ToolCalls[choiceIndex]
	if len(byIndex) == 0 {
		return nil
	}
	indices := make([]int, 0, len(byIndex))
	for index := range byIndex {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	for _, toolIndex := range indices {
		state := byIndex[toolIndex]
		if state == nil {
			return fmt.Errorf("choice[%d] tool call[%d] state is nil", choiceIndex, toolIndex)
		}
		if strings.TrimSpace(state.ID) == "" || strings.TrimSpace(state.Name) == "" {
			if final {
				return fmt.Errorf("choice[%d] tool call[%d] requires id and function name", choiceIndex, toolIndex)
			}
			continue
		}
		args, complete := geminiToolCallArguments(state.PendingArgs)
		if !complete {
			if final {
				return fmt.Errorf("choice[%d] tool %q arguments must be a complete JSON object", choiceIndex, state.Name)
			}
			continue
		}
		if state.Emitted {
			continue
		}
		*parts = append(*parts, dto.GeminiPart{
			FunctionCall: &dto.FunctionCall{
				ID:           state.ID,
				FunctionName: state.Name,
				Arguments:    args,
			},
		})
		state.Emitted = true
	}
	if final {
		delete(convertInfo.ToolCalls, choiceIndex)
	}
	return nil
}

func StreamResponseOpenAI2GeminiWithError(openAIResponse *dto.ChatCompletionsStreamResponse, info *relaycommon.RelayInfo) (*dto.GeminiChatResponse, error) {
	if openAIResponse == nil {
		return nil, fmt.Errorf("OpenAI stream response is nil")
	}

	// 检查是否有实际内容或结束标志。主要针对 OpenAI 流响应开头的空数据。
	hasContent := false
	hasFinishReason := false
	for _, choice := range openAIResponse.Choices {
		if len(choice.Delta.GetContentString()) > 0 || choice.Delta.GetReasoningContent() != "" ||
			(choice.Delta.ToolCalls != nil && len(choice.Delta.ToolCalls) > 0) {
			hasContent = true
		}
		if choice.FinishReason != nil {
			hasFinishReason = true
		}
	}
	if !hasContent && !hasFinishReason && openAIResponse.Usage == nil {
		return nil, nil
	}

	promptTokens := 0
	if info != nil {
		promptTokens = info.GetEstimatePromptTokens()
	}
	geminiResponse := &dto.GeminiChatResponse{
		Candidates: make([]dto.GeminiChatCandidate, 0, len(openAIResponse.Choices)),
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:     promptTokens,
			CandidatesTokenCount: 0,
			TotalTokenCount:      promptTokens,
		},
	}

	if openAIResponse.Usage != nil {
		geminiResponse.UsageMetadata.PromptTokenCount = openAIResponse.Usage.PromptTokens
		geminiResponse.UsageMetadata.CandidatesTokenCount = openAIResponse.Usage.CompletionTokens
		geminiResponse.UsageMetadata.TotalTokenCount = openAIResponse.Usage.TotalTokens
	}

	convertInfo := ensureGeminiConvertInfo(info)
	for _, choice := range openAIResponse.Choices {
		candidate := dto.GeminiChatCandidate{
			Index:         int64(choice.Index),
			SafetyRatings: []dto.GeminiChatSafetyRating{},
		}

		if choice.FinishReason != nil {
			finishReason, err := openAIFinishReasonToGemini(*choice.FinishReason)
			if err != nil {
				return nil, fmt.Errorf("choice[%d]: %w", choice.Index, err)
			}
			candidate.FinishReason = &finishReason
		}

		content := dto.GeminiChatContent{
			Role:  "model",
			Parts: make([]dto.GeminiPart, 0),
		}

		if choice.Delta.ReasoningContent != nil || choice.Delta.ThinkingContent != nil || choice.Delta.Reasoning != nil {
			content.Parts = append(content.Parts, dto.GeminiPart{
				Thought: true,
				Text:    choice.Delta.GetReasoningContent(),
			})
		}
		if textContent := choice.Delta.GetContentString(); textContent != "" {
			content.Parts = append(content.Parts, dto.GeminiPart{Text: textContent})
		}
		for callPosition, toolCall := range choice.Delta.ToolCalls {
			if toolCall.Type != nil {
				declaredType, ok := toolCall.Type.(string)
				if !ok {
					return nil, fmt.Errorf("choice[%d] tool call type must be a string, got %T", choice.Index, toolCall.Type)
				}
				declaredType = strings.TrimSpace(declaredType)
				if declaredType != "" && declaredType != "function" {
					return nil, fmt.Errorf("choice[%d] tool call type %q is not supported by Gemini", choice.Index, declaredType)
				}
			}
			toolIndex := callPosition
			if toolCall.Index != nil {
				if *toolCall.Index < 0 {
					return nil, fmt.Errorf("choice[%d] tool call index must not be negative", choice.Index)
				}
				toolIndex = *toolCall.Index
			} else if len(choice.Delta.ToolCalls) > 1 {
				return nil, fmt.Errorf("choice[%d] parallel tool calls require an index in every stream chunk", choice.Index)
			}

			byIndex := convertInfo.ToolCalls[choice.Index]
			stateExists := byIndex != nil && byIndex[toolIndex] != nil
			state := geminiToolCallStateForChoice(convertInfo, choice.Index, toolIndex)
			if !stateExists && (strings.TrimSpace(toolCall.ID) == "" || strings.TrimSpace(toolCall.Function.Name) == "") {
				return nil, fmt.Errorf("choice[%d] tool call requires id and function name in the first stream chunk", choice.Index)
			}
			if toolCall.ID != "" {
				if state.ID != "" && state.ID != toolCall.ID {
					return nil, fmt.Errorf("choice[%d] tool call[%d] id changed during stream", choice.Index, toolIndex)
				}
				state.ID = toolCall.ID
			}
			if toolCall.Function.Name != "" {
				if state.Name != "" && state.Name != toolCall.Function.Name {
					return nil, fmt.Errorf("choice[%d] tool call[%d] function name changed during stream", choice.Index, toolIndex)
				}
				state.Name = toolCall.Function.Name
			}
			if state.Emitted && strings.TrimSpace(toolCall.Function.Arguments) != "" {
				return nil, fmt.Errorf("choice[%d] tool %q emitted before its argument stream completed", choice.Index, state.Name)
			}
			state.PendingArgs += toolCall.Function.Arguments
		}
		final := choice.FinishReason != nil
		if err := flushGeminiToolCallState(convertInfo, choice.Index, &content.Parts, final); err != nil {
			return nil, err
		}

		if len(content.Parts) > 0 || choice.FinishReason != nil {
			candidate.Content = content
			geminiResponse.Candidates = append(geminiResponse.Candidates, candidate)
		}
	}
	if len(geminiResponse.Candidates) == 0 && openAIResponse.Usage == nil {
		return nil, nil
	}

	return geminiResponse, nil
}
