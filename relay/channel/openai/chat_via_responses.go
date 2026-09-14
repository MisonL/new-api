package openai

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func responsesStreamIndexKey(itemID string, idx *int) string {
	if itemID == "" {
		return ""
	}
	if idx == nil {
		return itemID
	}
	return fmt.Sprintf("%s:%d", itemID, *idx)
}

func stringDeltaFromPrefix(prev string, next string) string {
	if next == "" {
		return ""
	}
	if prev != "" && strings.HasPrefix(next, prev) {
		return next[len(prev):]
	}
	return next
}

const responsesReasoningTextFallbackKey = "__responses_reasoning_text__"

const responsesReasoningTextOutputIndexPrefix = "__responses_reasoning_text_output_"

const responsesOutputTextFallbackKey = "__responses_output_text__"

const responsesOutputTextOutputIndexPrefix = "__responses_output_text_output_"

func responsesReasoningTextOutputIndexKey(index *int) string {
	if index == nil {
		return ""
	}
	return fmt.Sprintf("%s%d", responsesReasoningTextOutputIndexPrefix, *index)
}

func responsesOutputTextOutputIndexKey(index *int) string {
	if index == nil {
		return ""
	}
	return fmt.Sprintf("%s%d", responsesOutputTextOutputIndexPrefix, *index)
}

// resolveResponsesReasoningTextKey reconciles providers that omit item_id on
// reasoning deltas but include it on the corresponding done/output item
// event. Moving the fallback snapshot prevents replaying the full reasoning
// text as a duplicate delta when the identifier appears later.
func resolveResponsesReasoningTextKey(states map[string]string, itemID string, outputIndex *int) string {
	key := strings.TrimSpace(itemID)
	if key == "" {
		key = responsesReasoningTextOutputIndexKey(outputIndex)
		if key == "" {
			return responsesReasoningTextFallbackKey
		}
	}
	if _, exists := states[key]; !exists {
		candidates := make([]string, 0, 2)
		if outputIndex != nil {
			if outputKey := responsesReasoningTextOutputIndexKey(outputIndex); outputKey != "" && outputKey != key {
				candidates = append(candidates, outputKey)
			}
		}
		candidates = append(candidates, responsesReasoningTextFallbackKey)
		if outputIndex == nil {
			var unresolvedKey string
			for candidate := range states {
				if strings.HasPrefix(candidate, responsesReasoningTextOutputIndexPrefix) {
					if unresolvedKey != "" {
						unresolvedKey = ""
						break
					}
					unresolvedKey = candidate
				}
			}
			if unresolvedKey != "" {
				candidates = append(candidates, unresolvedKey)
			}
		}
		for _, candidate := range candidates {
			if snapshot, ok := states[candidate]; ok {
				states[key] = snapshot
				delete(states, candidate)
				break
			}
		}
	}
	return key
}

// resolveResponsesOutputTextKey reconciles providers that omit item_id on
// output-text deltas but include it on the corresponding done/output item
// event. The terminal snapshot is authoritative for the completed item and
// must not be emitted a second time when it follows incremental deltas.
func resolveResponsesOutputTextKey(states map[string]string, itemID string, outputIndex *int) string {
	key := strings.TrimSpace(itemID)
	if key == "" {
		key = responsesOutputTextOutputIndexKey(outputIndex)
		if key == "" {
			return responsesOutputTextFallbackKey
		}
	}
	if _, exists := states[key]; !exists {
		candidates := make([]string, 0, 2)
		if outputIndex != nil {
			if outputKey := responsesOutputTextOutputIndexKey(outputIndex); outputKey != "" && outputKey != key {
				candidates = append(candidates, outputKey)
			}
		}
		candidates = append(candidates, responsesOutputTextFallbackKey)
		if outputIndex == nil {
			var unresolvedKey string
			for candidate := range states {
				if strings.HasPrefix(candidate, responsesOutputTextOutputIndexPrefix) {
					if unresolvedKey != "" {
						unresolvedKey = ""
						break
					}
					unresolvedKey = candidate
				}
			}
			if unresolvedKey != "" {
				candidates = append(candidates, unresolvedKey)
			}
		}
		for _, candidate := range candidates {
			if snapshot, ok := states[candidate]; ok {
				states[key] = snapshot
				delete(states, candidate)
				break
			}
		}
	}
	return key
}

func migrateResponsesToolCallState(
	oldID string,
	newID string,
	toolCallArgsByID map[string]string,
	toolCallNameByID map[string]string,
	toolCallNameSent map[string]bool,
	toolCallIndexByID map[string]int,
) {
	oldID = strings.TrimSpace(oldID)
	newID = strings.TrimSpace(newID)
	if oldID == "" || newID == "" || oldID == newID {
		return
	}
	if _, exists := toolCallArgsByID[newID]; !exists {
		if value, ok := toolCallArgsByID[oldID]; ok {
			toolCallArgsByID[newID] = value
		}
	}
	if _, exists := toolCallNameByID[newID]; !exists {
		if value, ok := toolCallNameByID[oldID]; ok {
			toolCallNameByID[newID] = value
		}
	}
	if _, exists := toolCallNameSent[newID]; !exists {
		if value, ok := toolCallNameSent[oldID]; ok {
			toolCallNameSent[newID] = value
		}
	}
	if _, exists := toolCallIndexByID[newID]; !exists {
		if value, ok := toolCallIndexByID[oldID]; ok {
			toolCallIndexByID[newID] = value
		}
	}
	delete(toolCallArgsByID, oldID)
	delete(toolCallNameByID, oldID)
	delete(toolCallNameSent, oldID)
	delete(toolCallIndexByID, oldID)
}

func reasoningTextDoneDelta(prev, snapshot, delta string) (string, string) {
	if snapshot != "" {
		return snapshot, stringDeltaFromPrefix(prev, snapshot)
	}
	if delta == "" {
		return prev, ""
	}
	if prev == "" {
		return delta, delta
	}
	if strings.HasPrefix(delta, prev) {
		return delta, delta[len(prev):]
	}
	if strings.HasSuffix(prev, delta) {
		return prev, ""
	}
	return prev + delta, delta
}

func outputTextFromResponsesOutput(item *dto.ResponsesOutput) string {
	if item == nil || item.Type != "message" {
		return ""
	}
	var builder strings.Builder
	for _, part := range item.Content {
		if part.Type == "output_text" {
			builder.WriteString(part.Text)
		}
	}
	return builder.String()
}

func OaiResponsesToChatHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	defer service.CloseResponseBodyGracefully(resp)

	var responsesResp dto.OpenAIResponsesResponse
	body, readErr := readResponsesNonStreamingResponseBody(c, resp.Body)
	if readErr != nil {
		return nil, readErr
	}
	if newAPIError := responsesSSETerminalError(c, body, resp.StatusCode); newAPIError != nil {
		return nil, newAPIError
	}

	if err := common.Unmarshal(body, &responsesResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	if oaiError := responsesResp.GetOpenAIError(); responsesOpenAIErrorHasContent(oaiError) {
		common.SetContextKey(c, constant.ContextKeyResponsesErrorEvent, "json")
		return nil, types.WithOpenAIError(*oaiError, responsesOpenAIErrorStatus(resp.StatusCode, oaiError))
	}

	chatId := helper.GetResponseID(c)
	chatResp, usage, err := service.ResponsesResponseToChatCompletionsResponse(&responsesResp, chatId)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	if usage == nil || usage.TotalTokens == 0 {
		text := service.ExtractOutputTextFromResponses(&responsesResp)
		reasoningContent := service.ExtractReasoningContentFromResponses(&responsesResp)
		if reasoningContent == "" {
			reasoningContent = service.ExtractReasoningSummaryFromResponses(&responsesResp)
		}
		if reasoningContent != "" {
			// Fallback token estimation counts the returned assistant content, including
			// reasoning text that is mapped into the chat-compatible payload.
			if text != "" {
				text += "\n\n" + reasoningContent
			} else {
				text = reasoningContent
			}
		}
		usage = service.ResponseText2Usage(c, text, info.UpstreamModelName, info.GetEstimatePromptTokens())
		chatResp.Usage = *usage
	}

	var responseBody []byte
	switch info.RelayFormat {
	case types.RelayFormatClaude:
		claudeResp, conversionErr := service.ResponseOpenAI2ClaudeWithError(chatResp, info)
		if conversionErr != nil {
			return nil, types.NewOpenAIError(conversionErr, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		responseBody, err = common.Marshal(claudeResp)
	case types.RelayFormatGemini:
		geminiResp, conversionErr := service.ResponseOpenAI2GeminiWithError(chatResp, info)
		if conversionErr != nil {
			return nil, types.NewOpenAIError(conversionErr, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		responseBody, err = common.Marshal(geminiResp)
	default:
		responseBody, err = common.Marshal(chatResp)
	}
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}

	recordResponsesBuiltInToolUsage(c, info, &responsesResp, responsesOutputToolCallCounts{})
	if responsesResp.HasImageGenerationCall() {
		c.Set("image_generation_call", true)
		c.Set("image_generation_call_quality", responsesResp.GetQuality())
		c.Set("image_generation_call_size", responsesResp.GetSize())
	}
	if responsesResp.HasCompactionOutput() {
		common.SetContextKey(c, constant.ContextKeyResponsesCompactionOutput, true)
	}
	if len(chatResp.Choices) > 0 && chatResp.Choices[0].Message.GetReasoningContent() != "" {
		common.SetContextKey(c, constant.ContextKeyResponsesChatCompatReasoningSummaryMapped, true)
	}

	service.IOCopyBytesGracefully(c, resp, responseBody)
	return usage, nil
}

func OaiResponsesToChatStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	defer service.CloseResponseBodyGracefully(resp)
	common.SetContextKey(c, constant.ContextKeyResponsesStreamPreludeRetrySafe, true)
	common.SetContextKey(c, constant.ContextKeyResponsesStreamFirstCommittedEvent, "")

	responseId := helper.GetResponseID(c)
	createAt := time.Now().Unix()
	model := info.UpstreamModelName

	var (
		usage       = &dto.Usage{}
		outputText  strings.Builder
		usageText   strings.Builder
		sentStart   bool
		sentStop    bool
		sawToolCall bool
		completed   bool
		streamErr   *types.NewAPIError
	)

	toolCallIndexByID := make(map[string]int)
	toolCallNameByID := make(map[string]string)
	toolCallArgsByID := make(map[string]string)
	toolCallNameSent := make(map[string]bool)
	toolCallCanonicalIDByItemID := make(map[string]string)
	toolCallCanonicalIDByOutputIndex := make(map[int]string)
	pendingToolCallArgsByItemID := make(map[string]string)
	pendingToolCallArgsByOutputIndex := make(map[int]string)
	hasSentReasoningSummary := false
	needsReasoningSummarySeparator := false
	reasoningSummaryTextByKey := make(map[string]string)
	reasoningSummarySource := ""
	reasoningTextByKey := make(map[string]string)
	outputTextByKey := make(map[string]string)
	outputTextFinalizedByKey := make(map[string]bool)

	if info.RelayFormat == types.RelayFormatClaude && info.ClaudeConvertInfo == nil {
		info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{LastMessagesType: relaycommon.LastMessageTypeNone}
	}

	sendChatChunk := func(chunk *dto.ChatCompletionsStreamResponse) bool {
		if chunk == nil {
			return true
		}
		markCommitted := func() {
			markResponsesStreamFirstCommittedEvent(c, "chat.completion.chunk")
			common.SetContextKey(c, constant.ContextKeyResponsesStreamPreludeRetrySafe, false)
		}
		markCommittedIfWritten := func() {
			if c != nil && c.Writer != nil && c.Writer.Written() {
				markCommitted()
			}
		}
		if info.RelayFormat == types.RelayFormatOpenAI {
			if err := helper.ObjectData(c, chunk); err != nil {
				markCommittedIfWritten()
				streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
				return false
			}
			markCommitted()
			return true
		}

		chunkData, err := common.Marshal(chunk)
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
			return false
		}
		if err := HandleStreamFormat(c, info, string(chunkData), false, false); err != nil {
			markCommittedIfWritten()
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
		markCommitted()
		return true
	}

	sendStartIfNeeded := func() bool {
		if sentStart {
			return true
		}
		if !sendChatChunk(helper.GenerateStartEmptyResponse(responseId, createAt, model, nil)) {
			return false
		}
		sentStart = true
		return true
	}

	sendReasoningSummaryDelta := func(delta string) bool {
		if delta == "" {
			return true
		}
		if needsReasoningSummarySeparator {
			if strings.HasPrefix(delta, "\n\n") {
				needsReasoningSummarySeparator = false
			} else if strings.HasPrefix(delta, "\n") {
				delta = "\n" + delta
				needsReasoningSummarySeparator = false
			} else {
				delta = "\n\n" + delta
				needsReasoningSummarySeparator = false
			}
		}
		if !sendStartIfNeeded() {
			return false
		}

		usageText.WriteString(delta)
		chunk := &dto.ChatCompletionsStreamResponse{
			Id:      responseId,
			Object:  "chat.completion.chunk",
			Created: createAt,
			Model:   model,
			Choices: []dto.ChatCompletionsStreamResponseChoice{
				{
					Index: 0,
					Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
						ReasoningContent: &delta,
					},
				},
			},
		}
		if !sendChatChunk(chunk) {
			return false
		}
		hasSentReasoningSummary = true
		common.SetContextKey(c, constant.ContextKeyResponsesChatCompatReasoningSummaryMapped, true)
		return true
	}

	sendReasoningTextDelta := func(delta string) bool {
		if delta == "" {
			return true
		}
		if !sendStartIfNeeded() {
			return false
		}
		usageText.WriteString(delta)
		chunk := &dto.ChatCompletionsStreamResponse{
			Id:      responseId,
			Object:  "chat.completion.chunk",
			Created: createAt,
			Model:   model,
			Choices: []dto.ChatCompletionsStreamResponseChoice{{
				Index: 0,
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					ReasoningContent: &delta,
				},
			}},
		}
		if !sendChatChunk(chunk) {
			return false
		}
		common.SetContextKey(c, constant.ContextKeyResponsesChatCompatReasoningSummaryMapped, true)
		return true
	}

	sendOutputTextDelta := func(delta string) bool {
		if delta == "" {
			return true
		}
		if !sendStartIfNeeded() {
			return false
		}
		outputText.WriteString(delta)
		usageText.WriteString(delta)
		chunk := &dto.ChatCompletionsStreamResponse{
			Id:      responseId,
			Object:  "chat.completion.chunk",
			Created: createAt,
			Model:   model,
			Choices: []dto.ChatCompletionsStreamResponseChoice{{
				Index: 0,
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					Content: &delta,
				},
			}},
		}
		return sendChatChunk(chunk)
	}

	reasoningTextFromOutput := func(item *dto.ResponsesOutput) string {
		if item == nil || item.Type != "reasoning" {
			return ""
		}
		var builder strings.Builder
		for _, part := range item.Content {
			if part.Type == "reasoning_text" {
				builder.WriteString(part.Text)
			}
		}
		return builder.String()
	}

	emitReasoningTextSnapshot := func(key string, next string) bool {
		prev := reasoningTextByKey[key]
		reasoningTextByKey[key] = next
		return sendReasoningTextDelta(stringDeltaFromPrefix(prev, next))
	}

	emitReasoningTextDone := func(key string, snapshot string, delta string) bool {
		next, outputDelta := reasoningTextDoneDelta(reasoningTextByKey[key], snapshot, delta)
		reasoningTextByKey[key] = next
		return sendReasoningTextDelta(outputDelta)
	}

	emitOutputTextSnapshot := func(key string, snapshot string) bool {
		if key == "" {
			key = responsesOutputTextFallbackKey
		}
		prev := outputTextByKey[key]
		next, outputDelta := reasoningTextDoneDelta(prev, snapshot, "")
		outputTextByKey[key] = next
		if snapshot != "" {
			outputTextFinalizedByKey[key] = true
		}
		return sendOutputTextDelta(outputDelta)
	}

	sendToolCallDelta := func(callID string, name string, argsDelta string) bool {
		if callID == "" {
			return true
		}
		if !sendStartIfNeeded() {
			return false
		}

		idx, ok := toolCallIndexByID[callID]
		if !ok {
			idx = len(toolCallIndexByID)
			toolCallIndexByID[callID] = idx
		}
		if name != "" {
			// Responses tool names are stricter than some upstream Chat/agent
			// implementations. Normalize names at the last boundary before they
			// are emitted as Chat Completions, including calls that were not
			// present in the original request and therefore have no alias state.
			toolCallNameByID[callID] = dto.NormalizeToolName(name)
		}
		if toolCallNameByID[callID] != "" {
			name = toolCallNameByID[callID]
		}
		sendName := name != "" && !toolCallNameSent[callID]
		if !sendName && argsDelta == "" {
			return true
		}

		tool := dto.ToolCallResponse{
			ID:   callID,
			Type: "function",
			Function: dto.FunctionResponse{
				Arguments: argsDelta,
			},
		}
		tool.SetIndex(idx)
		if sendName {
			tool.Function.Name = name
			toolCallNameSent[callID] = true
		}

		chunk := &dto.ChatCompletionsStreamResponse{
			Id:      responseId,
			Object:  "chat.completion.chunk",
			Created: createAt,
			Model:   model,
			Choices: []dto.ChatCompletionsStreamResponseChoice{
				{
					Index: 0,
					Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
						ToolCalls: []dto.ToolCallResponse{tool},
					},
				},
			},
		}
		if !sendChatChunk(chunk) {
			return false
		}
		sawToolCall = true

		// Include tool call data in the local builder for fallback token estimation.
		if tool.Function.Name != "" {
			usageText.WriteString(tool.Function.Name)
		}
		if argsDelta != "" {
			usageText.WriteString(argsDelta)
		}
		return true
	}

	recordFunctionCall := func(item *dto.ResponsesOutput, terminal bool, outputIndex *int) bool {
		if item == nil || item.Type != "function_call" {
			return true
		}

		itemID := strings.TrimSpace(item.ID)
		callID := strings.TrimSpace(item.CallId)
		if callID == "" {
			callID = itemID
		}
		if callID == "" {
			return true
		}
		previousCallID := ""
		if itemID != "" {
			previousCallID = toolCallCanonicalIDByItemID[itemID]
			toolCallCanonicalIDByItemID[itemID] = callID
		}
		if outputIndex != nil {
			if previousByIndex := toolCallCanonicalIDByOutputIndex[*outputIndex]; previousCallID == "" {
				previousCallID = previousByIndex
			}
			toolCallCanonicalIDByOutputIndex[*outputIndex] = callID
		}
		migrateResponsesToolCallState(previousCallID, callID, toolCallArgsByID, toolCallNameByID, toolCallNameSent, toolCallIndexByID)

		name := strings.TrimSpace(item.Name)
		if name != "" {
			if namespace := strings.TrimSpace(item.Namespace); namespace != "" {
				name = dto.NormalizeToolName(namespace + "__" + name)
			} else {
				name = dto.NormalizeToolName(name)
			}
		}
		newArgs := item.ArgumentsString()
		prevArgs, hasPreviousArgs := toolCallArgsByID[callID]
		argsDelta := ""
		pendingArgs := ""
		if itemID != "" {
			pendingArgs = pendingToolCallArgsByItemID[itemID]
			delete(pendingToolCallArgsByItemID, itemID)
		}
		if pendingArgs == "" && outputIndex != nil {
			pendingArgs = pendingToolCallArgsByOutputIndex[*outputIndex]
			delete(pendingToolCallArgsByOutputIndex, *outputIndex)
		}
		if pendingArgs != "" && (!hasPreviousArgs || prevArgs == "") {
			// Argument deltas can arrive before output_item.added on some
			// compatibility providers. They have not been sent yet, so emit the
			// buffered prefix exactly once and reconcile any terminal snapshot.
			switch {
			case newArgs == "":
				argsDelta = pendingArgs
				toolCallArgsByID[callID] = pendingArgs
			case strings.HasPrefix(newArgs, pendingArgs):
				argsDelta = newArgs
				toolCallArgsByID[callID] = newArgs
			default:
				argsDelta = pendingArgs
				toolCallArgsByID[callID] = pendingArgs
			}
		} else if newArgs != "" {
			switch {
			case !hasPreviousArgs || prevArgs == "":
				argsDelta = newArgs
				toolCallArgsByID[callID] = newArgs
			case strings.HasPrefix(newArgs, prevArgs):
				argsDelta = newArgs[len(prevArgs):]
				toolCallArgsByID[callID] = newArgs
			case !terminal:
				argsDelta = newArgs
				toolCallArgsByID[callID] = newArgs
			}
		}

		return sendToolCallDelta(callID, name, argsDelta)
	}

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		if streamErr != nil {
			sr.Stop(streamErr)
			return
		}

		var streamResp dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResp); err != nil {
			logger.LogError(c, "failed to unmarshal responses stream event: "+err.Error())
			sr.Error(err)
			return
		}
		if info != nil {
			if info.ResponsesStreamObservation != nil {
				info.ResponsesStreamObservation.Observe(streamResp)
			}
		}

		switch streamResp.Type {
		case "response.created":
			if streamResp.Response != nil {
				if streamResp.Response.Model != "" {
					model = streamResp.Response.Model
				}
				if streamResp.Response.CreatedAt != 0 {
					createAt = int64(streamResp.Response.CreatedAt)
				}
			}

		case "response.reasoning_summary_text.delta":
			if reasoningSummarySource == "part" {
				break
			}
			reasoningSummarySource = "text_delta"
			if !sendReasoningSummaryDelta(streamResp.Delta) {
				sr.Stop(streamErr)
				return
			}

		case "response.reasoning_summary_text.done":
			if hasSentReasoningSummary {
				needsReasoningSummarySeparator = true
			}

		case "response.reasoning_summary_part.added":
			key := responsesStreamIndexKey(strings.TrimSpace(streamResp.ItemID), streamResp.SummaryIndex)
			if key == "" || streamResp.Part == nil {
				break
			}
			if streamResp.Part.Type != "" && streamResp.Part.Type != "summary_text" {
				break
			}

		case "response.reasoning_summary_part.done":
			if reasoningSummarySource == "text_delta" {
				break
			}
			key := responsesStreamIndexKey(strings.TrimSpace(streamResp.ItemID), streamResp.SummaryIndex)
			if key == "" || streamResp.Part == nil {
				break
			}
			if streamResp.Part.Type != "" && streamResp.Part.Type != "summary_text" {
				break
			}
			reasoningSummarySource = "part"
			prev := reasoningSummaryTextByKey[key]
			next := streamResp.Part.Text
			reasoningSummaryTextByKey[key] = next
			delta := stringDeltaFromPrefix(prev, next)
			if prev == "" && hasSentReasoningSummary && delta != "" {
				needsReasoningSummarySeparator = true
			}
			if !sendReasoningSummaryDelta(delta) {
				sr.Stop(streamErr)
				return
			}

		case "response.reasoning_text.delta":
			key := resolveResponsesReasoningTextKey(reasoningTextByKey, streamResp.ItemID, streamResp.OutputIndex)
			reasoningTextByKey[key] += streamResp.Delta
			if !sendReasoningTextDelta(streamResp.Delta) {
				sr.Stop(streamErr)
				return
			}

		case "response.reasoning_text.done":
			key := resolveResponsesReasoningTextKey(reasoningTextByKey, streamResp.ItemID, streamResp.OutputIndex)
			if !emitReasoningTextDone(key, streamResp.Text, streamResp.Delta) {
				sr.Stop(streamErr)
				return
			}

		case "response.output_text.delta":
			key := resolveResponsesOutputTextKey(outputTextByKey, streamResp.ItemID, streamResp.OutputIndex)
			if !outputTextFinalizedByKey[key] && streamResp.Delta != "" {
				outputTextByKey[key] += streamResp.Delta
				if !sendOutputTextDelta(streamResp.Delta) {
					sr.Stop(streamErr)
					return
				}
			}

		case "response.output_text.done":
			key := resolveResponsesOutputTextKey(outputTextByKey, streamResp.ItemID, streamResp.OutputIndex)
			if !emitOutputTextSnapshot(key, streamResp.Text) {
				sr.Stop(streamErr)
				return
			}

		case "response.content_part.done":
			if streamResp.Part != nil && (streamResp.Part.Type == "" || streamResp.Part.Type == "output_text") {
				key := resolveResponsesOutputTextKey(outputTextByKey, streamResp.ItemID, streamResp.OutputIndex)
				if !emitOutputTextSnapshot(key, streamResp.Part.Text) {
					sr.Stop(streamErr)
					return
				}
			}

		case "response.output_item.added", "response.output_item.done":
			if streamResp.Item == nil {
				break
			}
			if streamResp.Item.Type != "reasoning" &&
				streamResp.Item.Type != "function_call" &&
				streamResp.Item.Type != "message" &&
				streamResp.Item.Type != dto.BuildInCallWebSearchCall &&
				streamResp.Item.Type != dto.BuildInCallFileSearchCall &&
				streamResp.Item.Type != dto.ResponsesOutputTypeImageGenerationCall &&
				streamResp.Item.Type != dto.ResponsesOutputTypeCompaction &&
				streamResp.Item.Type != dto.ResponsesOutputTypeCompactionSummary &&
				streamResp.Item.Type != dto.ResponsesOutputTypeContextCompaction {
				streamErr = types.NewOpenAIError(
					fmt.Errorf("Responses output item type %q is not supported in Chat Completions compatibility mode", streamResp.Item.Type),
					types.ErrorCodeBadResponseBody,
					http.StatusBadGateway,
				)
				sr.Stop(streamErr)
				return
			}
			if streamResp.Type == "response.output_item.done" {
				if err := service.ValidateResponsesOutputItemForChat(streamResp.Item, -1); err != nil {
					streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
					sr.Stop(streamErr)
					return
				}
			}
			if streamResp.Item.Type == "reasoning" {
				if streamResp.Type == "response.output_item.done" {
					key := resolveResponsesReasoningTextKey(reasoningTextByKey, streamResp.Item.ID, streamResp.OutputIndex)
					if !emitReasoningTextSnapshot(key, reasoningTextFromOutput(streamResp.Item)) {
						sr.Stop(streamErr)
						return
					}
				}
				break
			}
			if streamResp.Item.Type == "message" && streamResp.Type == "response.output_item.done" {
				key := resolveResponsesOutputTextKey(outputTextByKey, streamResp.Item.ID, streamResp.OutputIndex)
				if !emitOutputTextSnapshot(key, outputTextFromResponsesOutput(streamResp.Item)) {
					sr.Stop(streamErr)
					return
				}
			}
			if streamResp.Type == "response.output_item.done" {
				if !markResponsesCompactionOutputContext(c, streamResp.Item) {
					countResponsesOutputItemDone(c, info, streamResp.Item)
				}
			}
			if !recordFunctionCall(streamResp.Item, streamResp.Type == "response.output_item.done", streamResp.OutputIndex) {
				sr.Stop(streamErr)
				return
			}

		case "response.function_call_arguments.delta":
			itemID := strings.TrimSpace(streamResp.ItemID)
			callID := toolCallCanonicalIDByItemID[itemID]
			if callID == "" && streamResp.OutputIndex != nil {
				callID = toolCallCanonicalIDByOutputIndex[*streamResp.OutputIndex]
			}
			if callID == "" {
				if itemID != "" {
					pendingToolCallArgsByItemID[itemID] += streamResp.Delta
				} else if streamResp.OutputIndex != nil {
					pendingToolCallArgsByOutputIndex[*streamResp.OutputIndex] += streamResp.Delta
				}
				break
			}
			if callID == "" {
				break
			}
			argsDelta := streamResp.Delta
			previousArgs := toolCallArgsByID[callID]
			if argsDelta == "" {
				break
			}
			if previousArgs != "" && strings.HasPrefix(argsDelta, previousArgs) {
				argsDelta = argsDelta[len(previousArgs):]
			}
			if argsDelta == "" {
				break
			}
			toolCallArgsByID[callID] = previousArgs + argsDelta
			if !sendToolCallDelta(callID, "", argsDelta) {
				sr.Stop(streamErr)
				return
			}

		case "response.function_call_arguments.done":
			itemID := strings.TrimSpace(streamResp.ItemID)
			callID := strings.TrimSpace(streamResp.CallId)
			previousCallID := ""
			if itemID != "" {
				previousCallID = toolCallCanonicalIDByItemID[itemID]
				if callID == "" {
					callID = previousCallID
				}
				if callID != "" {
					toolCallCanonicalIDByItemID[itemID] = callID
				}
			}
			if streamResp.OutputIndex != nil {
				previousByIndex := toolCallCanonicalIDByOutputIndex[*streamResp.OutputIndex]
				if previousCallID == "" {
					previousCallID = previousByIndex
				}
				if callID == "" {
					callID = previousByIndex
				}
				if callID != "" {
					toolCallCanonicalIDByOutputIndex[*streamResp.OutputIndex] = callID
				}
			}
			if callID != "" {
				migrateResponsesToolCallState(previousCallID, callID, toolCallArgsByID, toolCallNameByID, toolCallNameSent, toolCallIndexByID)
			}
			if callID == "" {
				if itemID != "" {
					pendingToolCallArgsByItemID[itemID] = streamResp.Arguments
				} else if streamResp.OutputIndex != nil {
					pendingToolCallArgsByOutputIndex[*streamResp.OutputIndex] = streamResp.Arguments
				}
				break
			}
			if streamResp.Name != "" {
				if !sendToolCallDelta(callID, streamResp.Name, "") {
					sr.Stop(streamErr)
					return
				}
			}
			previousArgs := toolCallArgsByID[callID]
			finalArgs := streamResp.Arguments
			if finalArgs == "" {
				break
			}
			argsDelta := stringDeltaFromPrefix(previousArgs, finalArgs)
			toolCallArgsByID[callID] = finalArgs
			if argsDelta != "" {
				if !sendToolCallDelta(callID, "", argsDelta) {
					sr.Stop(streamErr)
					return
				}
			}

		case "response.completed", "response.done", "response.incomplete":
			if streamResp.Type == "response.incomplete" {
				streamResp.Response = newIncompleteResponsesTerminalResponse(streamResp.Response)
			}
			if streamResp.Response != nil {
				if streamResp.Response.Model != "" {
					model = streamResp.Response.Model
				}
				if streamResp.Response.CreatedAt != 0 {
					createAt = int64(streamResp.Response.CreatedAt)
				}
				if streamResp.Response.Usage != nil {
					if streamResp.Response.Usage.InputTokens != 0 {
						usage.PromptTokens = streamResp.Response.Usage.InputTokens
						usage.InputTokens = streamResp.Response.Usage.InputTokens
					}
					if streamResp.Response.Usage.OutputTokens != 0 {
						usage.CompletionTokens = streamResp.Response.Usage.OutputTokens
						usage.OutputTokens = streamResp.Response.Usage.OutputTokens
					}
					if streamResp.Response.Usage.TotalTokens != 0 {
						usage.TotalTokens = streamResp.Response.Usage.TotalTokens
					} else {
						usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
					}
					if streamResp.Response.Usage.InputTokensDetails != nil {
						usage.PromptTokensDetails.CachedTokens = streamResp.Response.Usage.InputTokensDetails.CachedTokens
						usage.PromptTokensDetails.ImageTokens = streamResp.Response.Usage.InputTokensDetails.ImageTokens
						usage.PromptTokensDetails.AudioTokens = streamResp.Response.Usage.InputTokensDetails.AudioTokens
					}
					outputDetails := streamResp.Response.Usage.GetOutputTokenDetails()
					if outputDetails.ReasoningTokens != 0 {
						usage.CompletionTokenDetails.ReasoningTokens = outputDetails.ReasoningTokens
					}
					if outputDetails.ImageTokens != 0 {
						usage.CompletionTokenDetails.ImageTokens = outputDetails.ImageTokens
					}
					if outputDetails.AudioTokens != 0 {
						usage.CompletionTokenDetails.AudioTokens = outputDetails.AudioTokens
					}
				}
				recordResponsesBuiltInToolUsage(c, info, streamResp.Response, observedResponsesOutputToolCallCounts(c))
				if streamResp.Response.HasImageGenerationCall() {
					if !c.GetBool("image_generation_call") {
						c.Set("image_generation_call", true)
						c.Set("image_generation_call_quality", streamResp.Response.GetQuality())
						c.Set("image_generation_call_size", streamResp.Response.GetSize())
					}
				}
				if streamResp.Response.HasCompactionOutput() {
					common.SetContextKey(c, constant.ContextKeyResponsesCompactionOutput, true)
				}
				for index := range streamResp.Response.Output {
					if err := service.ValidateResponsesOutputItemForChat(&streamResp.Response.Output[index], index); err != nil {
						streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
						sr.Stop(streamErr)
						return
					}
					itemType := streamResp.Response.Output[index].Type
					if itemType != "reasoning" && itemType != "function_call" && itemType != "message" &&
						itemType != dto.BuildInCallWebSearchCall && itemType != dto.BuildInCallFileSearchCall &&
						itemType != dto.ResponsesOutputTypeImageGenerationCall &&
						itemType != dto.ResponsesOutputTypeCompaction &&
						itemType != dto.ResponsesOutputTypeCompactionSummary &&
						itemType != dto.ResponsesOutputTypeContextCompaction {
						streamErr = types.NewOpenAIError(
							fmt.Errorf("Responses output item type %q is not supported in Chat Completions compatibility mode", itemType),
							types.ErrorCodeBadResponseBody,
							http.StatusBadGateway,
						)
						sr.Stop(streamErr)
						return
					}
					if streamResp.Response.Output[index].Type == "reasoning" {
						outputIndex := index
						key := resolveResponsesReasoningTextKey(reasoningTextByKey, streamResp.Response.Output[index].ID, &outputIndex)
						if !emitReasoningTextSnapshot(key, reasoningTextFromOutput(&streamResp.Response.Output[index])) {
							sr.Stop(streamErr)
							return
						}
						continue
					}
					if streamResp.Response.Output[index].Type == "message" {
						outputIndex := index
						key := resolveResponsesOutputTextKey(outputTextByKey, streamResp.Response.Output[index].ID, &outputIndex)
						if !emitOutputTextSnapshot(key, outputTextFromResponsesOutput(&streamResp.Response.Output[index])) {
							sr.Stop(streamErr)
							return
						}
						continue
					}
					outputIndex := index
					if !recordFunctionCall(&streamResp.Response.Output[index], true, &outputIndex) {
						sr.Stop(streamErr)
						return
					}
				}
			}

			if !sendStartIfNeeded() {
				sr.Stop(streamErr)
				return
			}
			if !sentStop {
				if info.RelayFormat == types.RelayFormatClaude && info.ClaudeConvertInfo != nil {
					info.ClaudeConvertInfo.Usage = usage
				}
				finishReason := responsesChatFinishReason(streamResp.Response, sawToolCall)
				stop := helper.GenerateStopResponse(responseId, createAt, model, finishReason)
				if !sendChatChunk(stop) {
					sr.Stop(streamErr)
					return
				}
				sentStop = true
			}
			completed = true
			sr.Done()

		case "response.error", "error", "response.failed":
			common.SetContextKey(c, constant.ContextKeyResponsesErrorEvent, streamResp.Type)
			oaiErr := responsesStreamTerminalOpenAIError(&streamResp)
			if oaiErr == nil {
				streamErr = types.NewOpenAIError(fmt.Errorf("responses stream error: %s", streamResp.Type), types.ErrorCodeBadResponse, http.StatusBadGateway)
			} else {
				streamErr = types.WithOpenAIError(*oaiErr, responsesOpenAIErrorStatus(resp.StatusCode, oaiErr))
			}
			sr.Stop(streamErr)
			return

		default:
		}
	})

	if streamErr != nil {
		return nil, streamErr
	}
	if newAPIError := retryableUpstreamStreamInterruptedError(c, info); newAPIError != nil {
		return nil, newAPIError
	}
	if info != nil && info.StreamStatus != nil &&
		info.StreamStatus.EndReason == relaycommon.StreamEndReasonUpstreamInterrupted {
		err := info.StreamStatus.EndError
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		return nil, types.NewOpenAIError(
			err,
			types.ErrorCodeUpstreamTransportInterrupted,
			http.StatusBadGateway,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if !completed {
		return nil, types.NewOpenAIError(
			fmt.Errorf("upstream responses stream ended before response.completed"),
			types.ErrorCodeBadResponseBody,
			http.StatusBadGateway,
		)
	}

	if usage.TotalTokens == 0 {
		usage = service.ResponseText2Usage(c, usageText.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
	}

	if !sentStart {
		if !sendChatChunk(helper.GenerateStartEmptyResponse(responseId, createAt, model, nil)) {
			return nil, streamErr
		}
	}
	if !sentStop {
		if info.RelayFormat == types.RelayFormatClaude && info.ClaudeConvertInfo != nil {
			info.ClaudeConvertInfo.Usage = usage
		}
		finishReason := responsesChatFinishReason(nil, sawToolCall)
		stop := helper.GenerateStopResponse(responseId, createAt, model, finishReason)
		if !sendChatChunk(stop) {
			return nil, streamErr
		}
	}
	if info.RelayFormat == types.RelayFormatOpenAI && info.ShouldIncludeUsage && usage != nil {
		if err := helper.ObjectData(c, helper.GenerateFinalUsageResponse(responseId, createAt, model, *usage)); err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
		}
	}

	if info.RelayFormat == types.RelayFormatOpenAI {
		helper.Done(c)
	}
	return usage, nil
}

func newIncompleteResponsesTerminalResponse(response *dto.OpenAIResponsesResponse) *dto.OpenAIResponsesResponse {
	if response == nil {
		response = &dto.OpenAIResponsesResponse{}
	}
	response.Status = common.RawMessage(`"incomplete"`)
	return response
}

func responsesChatFinishReason(response *dto.OpenAIResponsesResponse, sawToolCall bool) string {
	if sawToolCall {
		return "tool_calls"
	}
	if response != nil && strings.EqualFold(strings.TrimSpace(common.JsonRawMessageToString(response.Status)), "incomplete") {
		if response.IncompleteDetails != nil && strings.EqualFold(response.IncompleteDetails.EffectiveReason(), "content_filter") {
			return "content_filter"
		}
		return "length"
	}
	return "stop"
}
