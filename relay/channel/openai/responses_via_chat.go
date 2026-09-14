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

func getResponsesCompatID(c *gin.Context) string {
	logID := c.GetString(common.RequestIdKey)
	return service.NewResponsesChatCompatibilityID(logID)
}

func sendResponsesCompatEvent(c *gin.Context, eventType string, event dto.ResponsesStreamResponse) error {
	event.Type = eventType
	jsonData, err := common.Marshal(event)
	if err != nil {
		return err
	}
	helper.ResponseChunkData(c, event, string(jsonData))
	return nil
}

func sendResponsesCompatRawEvent(c *gin.Context, eventType string, payload any) error {
	jsonData, err := common.Marshal(payload)
	if err != nil {
		return err
	}
	helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: eventType}, string(jsonData))
	return nil
}

func buildResponsesCompatUsage(usage *dto.Usage) *dto.Usage {
	if usage == nil {
		return &dto.Usage{}
	}
	copied := *usage
	if copied.InputTokens == 0 {
		copied.InputTokens = copied.PromptTokens
	}
	if copied.OutputTokens == 0 {
		copied.OutputTokens = copied.CompletionTokens
	}
	if copied.TotalTokens == 0 {
		copied.TotalTokens = copied.InputTokens + copied.OutputTokens
	}
	if copied.InputTokensDetails == nil {
		copied.InputTokensDetails = &dto.InputTokenDetails{
			CachedTokens: copied.PromptTokensDetails.CachedTokens,
			ImageTokens:  copied.PromptTokensDetails.ImageTokens,
			AudioTokens:  copied.PromptTokensDetails.AudioTokens,
		}
	}
	return &copied
}

type responsesCompatReasoningState struct {
	itemID           string
	outputIndex      int
	builder          strings.Builder
	contentPartAdded bool
	finalized        bool
}

func OaiChatToResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	return OaiChatToResponsesHandlerWithOptions(c, info, resp, service.ResponsesChatCompatibilityOptions{})
}

func OaiChatToResponsesHandlerWithOptions(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response, options service.ResponsesChatCompatibilityOptions) (*dto.Usage, *types.NewAPIError) {
	return OaiChatToResponsesHandlerWithOptionsAndState(c, info, resp, options, nil)
}

func OaiChatToResponsesHandlerWithOptionsAndState(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response, options service.ResponsesChatCompatibilityOptions, toolState *service.ResponsesChatCompatibilityToolState) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	defer service.CloseResponseBodyGracefully(resp)

	var chatResp dto.OpenAITextResponse
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, newResponsesReadResponseBodyError(err)
	}
	if newAPIError := responsesSSETerminalError(c, body, resp.StatusCode); newAPIError != nil {
		return nil, newAPIError
	}
	if err := common.Unmarshal(body, &chatResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := chatResp.GetOpenAIError(); responsesOpenAIErrorHasContent(oaiError) {
		common.SetContextKey(c, constant.ContextKeyResponsesErrorEvent, "json")
		return nil, types.WithOpenAIError(*oaiError, responsesOpenAIErrorStatus(resp.StatusCode, oaiError))
	}

	responsesID := getResponsesCompatID(c)
	responsesResp, usage, err := service.ChatCompletionsResponseToResponsesResponseWithOptionsAndState(&chatResp, responsesID, options, toolState)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if usage == nil || usage.TotalTokens == 0 {
		text := ""
		if len(chatResp.Choices) > 0 {
			text = chatResp.Choices[0].Message.StringContent()
			reasoning := chatResp.Choices[0].Message.GetReasoningContent()
			if reasoning != "" {
				if text != "" {
					text += "\n\n"
				}
				text += reasoning
			}
		}
		usage = service.ResponseText2Usage(c, text, info.UpstreamModelName, info.GetEstimatePromptTokens())
		responsesResp.Usage = buildResponsesCompatUsage(usage)
	}

	responseBody, err := common.Marshal(responsesResp)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}
	service.IOCopyBytesGracefully(c, resp, responseBody)
	return usage, nil
}

func OaiChatToResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	return OaiChatToResponsesStreamHandlerWithOptions(c, info, resp, service.ResponsesChatCompatibilityOptions{})
}

func OaiChatToResponsesStreamHandlerWithOptions(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response, options service.ResponsesChatCompatibilityOptions) (*dto.Usage, *types.NewAPIError) {
	return OaiChatToResponsesStreamHandlerWithOptionsAndState(c, info, resp, options, nil)
}

func OaiChatToResponsesStreamHandlerWithOptionsAndState(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response, options service.ResponsesChatCompatibilityOptions, toolState *service.ResponsesChatCompatibilityToolState) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	defer service.CloseResponseBodyGracefully(resp)

	responseID := getResponsesCompatID(c)
	model := info.UpstreamModelName
	createdAt := int(time.Now().Unix())

	statusInProgress, _ := common.Marshal("in_progress")
	statusCompleted, _ := common.Marshal("completed")
	statusIncomplete, _ := common.Marshal("incomplete")

	usage := &dto.Usage{}
	textItemID := "msg_0"
	textOutputIndex := -1
	textAdded := false
	var textBuilder strings.Builder
	var usageBuilder strings.Builder
	completedOutput := make(map[int]dto.ResponsesOutput)
	nextOutputIndex := 0
	reasoningStates := make([]*responsesCompatReasoningState, 0, 1)
	var activeReasoning *responsesCompatReasoningState
	toolCalls := make(map[int]*chatToolCallState)
	var streamErr *types.NewAPIError
	finishReason := ""
	responseCreatedSent := false
	responseInProgressSent := false
	textContentPartAdded := false
	allocateOutputIndex := func() int {
		outputIndex := nextOutputIndex
		nextOutputIndex++
		return outputIndex
	}

	sendResponseCreated := func() bool {
		if responseCreatedSent {
			return true
		}
		err := sendResponsesCompatEvent(c, "response.created", dto.ResponsesStreamResponse{
			Response: &dto.OpenAIResponsesResponse{
				ID:        responseID,
				Object:    "response",
				CreatedAt: createdAt,
				Status:    statusInProgress,
				Model:     model,
			},
		})
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
		responseCreatedSent = true
		return true
	}

	sendResponseInProgress := func() bool {
		if responseInProgressSent {
			return true
		}
		if !sendResponseCreated() {
			return false
		}
		err := sendResponsesCompatEvent(c, "response.in_progress", dto.ResponsesStreamResponse{
			Response: &dto.OpenAIResponsesResponse{
				ID:        responseID,
				Object:    "response",
				CreatedAt: createdAt,
				Status:    statusInProgress,
				Model:     model,
			},
		})
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
		responseInProgressSent = true
		return true
	}

	finalizeReasoningState := func(state *responsesCompatReasoningState) bool {
		if state == nil || state.finalized {
			return true
		}
		if err := sendResponsesCompatRawEvent(c, "response.reasoning_text.done", map[string]any{
			"type":         "response.reasoning_text.done",
			"item_id":      state.itemID,
			"output_index": state.outputIndex,
			"text":         state.builder.String(),
		}); err != nil {
			streamErr = types.NewOpenAIError(fmt.Errorf("failed to emit reasoning text done event"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
		if state.contentPartAdded {
			if err := sendResponsesCompatRawEvent(c, "response.content_part.done", map[string]any{
				"type":          "response.content_part.done",
				"item_id":       state.itemID,
				"output_index":  state.outputIndex,
				"content_index": 0,
				"part": map[string]any{
					"type": "reasoning_text",
					"text": state.builder.String(),
				},
			}); err != nil {
				streamErr = types.NewOpenAIError(fmt.Errorf("failed to emit reasoning content part done event"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
				return false
			}
		}
		if err := sendResponsesCompatEvent(c, dto.ResponsesOutputTypeItemDone, dto.ResponsesStreamResponse{
			Item: &dto.ResponsesOutput{
				Type:   "reasoning",
				ID:     state.itemID,
				Status: "completed",
				Content: []dto.ResponsesOutputContent{{
					Type: "reasoning_text",
					Text: state.builder.String(),
				}},
			},
			OutputIndex: common.GetPointer(state.outputIndex),
		}); err != nil {
			streamErr = types.NewOpenAIError(fmt.Errorf("failed to emit reasoning output item done event"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
		completedOutput[state.outputIndex] = dto.ResponsesOutput{
			Type:   "reasoning",
			ID:     state.itemID,
			Status: "completed",
			Content: []dto.ResponsesOutputContent{{
				Type: "reasoning_text",
				Text: state.builder.String(),
			}},
		}
		state.finalized = true
		if activeReasoning == state {
			activeReasoning = nil
		}
		return true
	}

	finalizeActiveReasoning := func() bool {
		return finalizeReasoningState(activeReasoning)
	}

	sendReasoningItemAdded := func() bool {
		if activeReasoning != nil && !activeReasoning.finalized {
			return true
		}
		if !sendResponseInProgress() {
			return false
		}
		state := &responsesCompatReasoningState{
			itemID:      fmt.Sprintf("rs_%d", len(reasoningStates)),
			outputIndex: allocateOutputIndex(),
		}
		reasoningStates = append(reasoningStates, state)
		if err := sendResponsesCompatEvent(c, dto.ResponsesOutputTypeItemAdded, dto.ResponsesStreamResponse{
			Item: &dto.ResponsesOutput{
				Type:   "reasoning",
				ID:     state.itemID,
				Status: "in_progress",
				Content: []dto.ResponsesOutputContent{{
					Type: "reasoning_text",
				}},
			},
			OutputIndex:  common.GetPointer(state.outputIndex),
			ContentIndex: common.GetPointer(0),
		}); err != nil {
			streamErr = types.NewOpenAIError(fmt.Errorf("failed to emit reasoning output item added event"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
		if err := sendResponsesCompatRawEvent(c, "response.content_part.added", map[string]any{
			"type":          "response.content_part.added",
			"item_id":       state.itemID,
			"output_index":  state.outputIndex,
			"content_index": 0,
			"part": map[string]any{
				"type": "reasoning_text",
				"text": "",
			},
		}); err != nil {
			streamErr = types.NewOpenAIError(fmt.Errorf("failed to emit reasoning content part added event"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
		state.contentPartAdded = true
		activeReasoning = state
		return true
	}

	sendReasoningDelta := func(delta string) bool {
		if delta == "" {
			return true
		}
		if !sendReasoningItemAdded() {
			return false
		}
		activeReasoning.builder.WriteString(delta)
		usageBuilder.WriteString(delta)
		if err := sendResponsesCompatEvent(c, "response.reasoning_text.delta", dto.ResponsesStreamResponse{
			Delta:       delta,
			ItemID:      activeReasoning.itemID,
			OutputIndex: common.GetPointer(activeReasoning.outputIndex),
		}); err != nil {
			streamErr = types.NewOpenAIError(fmt.Errorf("failed to emit reasoning text delta event"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
		return true
	}

	sendTextItemAdded := func() bool {
		if textAdded {
			return true
		}
		if !finalizeActiveReasoning() {
			return false
		}
		if !sendResponseInProgress() {
			return false
		}
		if textOutputIndex < 0 {
			textOutputIndex = allocateOutputIndex()
		}
		err := sendResponsesCompatEvent(c, dto.ResponsesOutputTypeItemAdded, dto.ResponsesStreamResponse{
			Item: &dto.ResponsesOutput{
				Type:   "message",
				ID:     textItemID,
				Status: "in_progress",
				Role:   "assistant",
				Content: []dto.ResponsesOutputContent{
					{Type: "output_text"},
				},
			},
			OutputIndex:  common.GetPointer(textOutputIndex),
			ContentIndex: common.GetPointer(0),
		})
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
		textAdded = true
		err = sendResponsesCompatRawEvent(c, "response.content_part.added", map[string]any{
			"type":          "response.content_part.added",
			"item_id":       textItemID,
			"output_index":  textOutputIndex,
			"content_index": 0,
			"part": map[string]any{
				"type": "output_text",
				"text": "",
			},
		})
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
		textContentPartAdded = true
		return true
	}

	sendToolItemAdded := func(state *chatToolCallState) bool {
		if state == nil || state.sentAdded {
			return true
		}
		if strings.TrimSpace(state.name) == "" {
			return true
		}
		if !finalizeActiveReasoning() {
			return false
		}
		if !sendResponseInProgress() {
			return false
		}
		if state.outputIndex < 0 {
			state.outputIndex = allocateOutputIndex()
		}
		if state.toolType == dto.CustomType {
			err := sendResponsesCompatEvent(c, dto.ResponsesOutputTypeItemAdded, dto.ResponsesStreamResponse{
				Item: &dto.ResponsesOutput{
					Type:      "custom_tool_call",
					ID:        state.itemID,
					Status:    "in_progress",
					CallId:    state.callID,
					Name:      state.name,
					Namespace: state.namespace,
					Input:     "",
				},
				OutputIndex: common.GetPointer(state.outputIndex),
			})
			if err != nil {
				streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
				return false
			}
			state.sentAdded = true
			return true
		}
		if state.toolType == "tool_search" {
			err := sendResponsesCompatEvent(c, dto.ResponsesOutputTypeItemAdded, dto.ResponsesStreamResponse{
				Item: &dto.ResponsesOutput{
					Type:      "tool_search_call",
					ID:        state.itemID,
					Status:    "in_progress",
					CallId:    state.callID,
					Execution: "client",
				},
				OutputIndex: common.GetPointer(state.outputIndex),
			})
			if err != nil {
				streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
				return false
			}
			state.sentAdded = true
			return true
		}
		argumentsRaw, err := marshalResponsesCompatArguments("")
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
		err = sendResponsesCompatEvent(c, dto.ResponsesOutputTypeItemAdded, dto.ResponsesStreamResponse{
			Item: &dto.ResponsesOutput{
				Type:      "function_call",
				ID:        state.itemID,
				Status:    "in_progress",
				CallId:    state.callID,
				Name:      state.name,
				Namespace: state.namespace,
				Arguments: argumentsRaw,
			},
			OutputIndex: common.GetPointer(state.outputIndex),
		})
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
		state.sentAdded = true
		return true
	}

	sendPendingFunctionArguments := func(state *chatToolCallState) bool {
		if state == nil || state.toolType != "function" || !state.sentAdded {
			return true
		}
		arguments := state.arguments.String()
		if state.argumentsPublished >= len(arguments) {
			return true
		}
		delta := arguments[state.argumentsPublished:]
		err := sendResponsesCompatEvent(c, "response.function_call_arguments.delta", dto.ResponsesStreamResponse{
			Delta:       delta,
			ItemID:      state.itemID,
			OutputIndex: common.GetPointer(state.outputIndex),
		})
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
		state.argumentsPublished = len(arguments)
		return true
	}

	sendCustomToolInputEvents := func(state *chatToolCallState) bool {
		input := state.input.String()
		err := sendResponsesCompatRawEvent(c, "response.custom_tool_call_input.delta", map[string]any{
			"type":         "response.custom_tool_call_input.delta",
			"item_id":      state.itemID,
			"output_index": state.outputIndex,
			"delta":        input,
		})
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
		err = sendResponsesCompatRawEvent(c, "response.custom_tool_call_input.done", map[string]any{
			"type":         "response.custom_tool_call_input.done",
			"item_id":      state.itemID,
			"output_index": state.outputIndex,
			"call_id":      state.callID,
			"name":         state.name,
			"input":        input,
		})
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
		return true
	}

	finalizeToolCalls := func() bool {
		for _, state := range orderedChatToolCallStates(toolCalls) {
			if strings.TrimSpace(state.name) == "" {
				streamErr = types.NewOpenAIError(fmt.Errorf("chat tool call name is required"), types.ErrorCodeBadResponse, http.StatusBadGateway)
				return false
			}
			if !sendToolItemAdded(state) {
				return false
			}
			if state.toolType == dto.CustomType {
				if state.chatToolType != dto.CustomType {
					input, err := service.ParseResponsesCustomToolFunctionArguments(state.arguments.String())
					if err != nil {
						streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusBadGateway)
						return false
					}
					state.input.Reset()
					state.input.WriteString(input)
				} else if !state.customInputSeen {
					streamErr = types.NewOpenAIError(fmt.Errorf("custom tool input is required"), types.ErrorCodeBadResponse, http.StatusBadGateway)
					return false
				}
				if !sendCustomToolInputEvents(state) {
					return false
				}
				err := sendResponsesCompatEvent(c, dto.ResponsesOutputTypeItemDone, dto.ResponsesStreamResponse{
					Item: &dto.ResponsesOutput{
						Type:      "custom_tool_call",
						ID:        state.itemID,
						Status:    "completed",
						CallId:    state.callID,
						Name:      state.name,
						Namespace: state.namespace,
						Input:     state.input.String(),
					},
					OutputIndex: common.GetPointer(state.outputIndex),
				})
				if err != nil {
					streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
					return false
				}
				completedOutput[state.outputIndex] = dto.ResponsesOutput{
					Type:      "custom_tool_call",
					ID:        state.itemID,
					Status:    "completed",
					CallId:    state.callID,
					Name:      state.name,
					Namespace: state.namespace,
					Input:     state.input.String(),
				}
				continue
			}
			if state.toolType == "tool_search" {
				argumentsRaw, err := responsesToolSearchArgumentsRaw(state.arguments.String())
				if err != nil {
					streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusBadGateway)
					return false
				}
				err = sendResponsesCompatEvent(c, dto.ResponsesOutputTypeItemDone, dto.ResponsesStreamResponse{
					Item: &dto.ResponsesOutput{
						Type:      "tool_search_call",
						ID:        state.itemID,
						Status:    "completed",
						CallId:    state.callID,
						Execution: "client",
						Arguments: argumentsRaw,
					},
					OutputIndex: common.GetPointer(state.outputIndex),
				})
				if err != nil {
					streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
					return false
				}
				completedOutput[state.outputIndex] = dto.ResponsesOutput{
					Type:      "tool_search_call",
					ID:        state.itemID,
					Status:    "completed",
					CallId:    state.callID,
					Execution: "client",
					Arguments: argumentsRaw,
				}
				continue
			}
			if !sendPendingFunctionArguments(state) {
				return false
			}
			argumentsRaw, err := marshalResponsesCompatArguments(state.arguments.String())
			if err != nil {
				streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
				return false
			}
			err = sendResponsesCompatRawEvent(c, "response.function_call_arguments.done", map[string]any{
				"type":         "response.function_call_arguments.done",
				"item_id":      state.itemID,
				"output_index": state.outputIndex,
				"arguments":    state.arguments.String(),
			})
			if err != nil {
				streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
				return false
			}
			err = sendResponsesCompatEvent(c, dto.ResponsesOutputTypeItemDone, dto.ResponsesStreamResponse{
				Item: &dto.ResponsesOutput{
					Type:      "function_call",
					ID:        state.itemID,
					Status:    "completed",
					CallId:    state.callID,
					Name:      state.name,
					Namespace: state.namespace,
					Arguments: argumentsRaw,
				},
				OutputIndex: common.GetPointer(state.outputIndex),
			})
			if err != nil {
				streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
				return false
			}
			completedOutput[state.outputIndex] = dto.ResponsesOutput{
				Type:      "function_call",
				ID:        state.itemID,
				Status:    "completed",
				CallId:    state.callID,
				Name:      state.name,
				Namespace: state.namespace,
				Arguments: argumentsRaw,
			}
		}
		return true
	}

	finalizeTextItem := func() bool {
		if !textAdded {
			return true
		}
		if textContentPartAdded {
			err := sendResponsesCompatRawEvent(c, "response.output_text.done", map[string]any{
				"type":          "response.output_text.done",
				"item_id":       textItemID,
				"output_index":  textOutputIndex,
				"content_index": 0,
				"text":          textBuilder.String(),
			})
			if err != nil {
				streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
				return false
			}
			err = sendResponsesCompatRawEvent(c, "response.content_part.done", map[string]any{
				"type":          "response.content_part.done",
				"item_id":       textItemID,
				"output_index":  textOutputIndex,
				"content_index": 0,
				"part": map[string]any{
					"type": "output_text",
					"text": textBuilder.String(),
				},
			})
			if err != nil {
				streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
				return false
			}
		}
		err := sendResponsesCompatEvent(c, dto.ResponsesOutputTypeItemDone, dto.ResponsesStreamResponse{
			Item: &dto.ResponsesOutput{
				Type:   "message",
				ID:     textItemID,
				Status: "completed",
				Role:   "assistant",
				Content: []dto.ResponsesOutputContent{
					{
						Type: "output_text",
						Text: textBuilder.String(),
					},
				},
			},
			OutputIndex:  common.GetPointer(textOutputIndex),
			ContentIndex: common.GetPointer(0),
		})
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
		completedOutput[textOutputIndex] = dto.ResponsesOutput{
			Type:   "message",
			ID:     textItemID,
			Status: "completed",
			Role:   "assistant",
			Content: []dto.ResponsesOutputContent{
				{
					Type: "output_text",
					Text: textBuilder.String(),
				},
			},
		}
		return true
	}

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		if streamErr != nil {
			sr.Stop(streamErr)
			return
		}
		if data == "" || data == "[DONE]" {
			return
		}

		var errorResp dto.OpenAITextResponse
		if err := common.UnmarshalJsonStr(data, &errorResp); err == nil {
			if oaiError := errorResp.GetOpenAIError(); responsesOpenAIErrorHasContent(oaiError) {
				common.SetContextKey(c, constant.ContextKeyResponsesErrorEvent, "sse")
				statusCode := responsesViaChatStreamErrorStatus(resp.StatusCode, oaiError, c.Writer.Written())
				streamErr = types.WithOpenAIError(*oaiError, statusCode)
				sr.Stop(streamErr)
				return
			}
		}

		var chunk dto.ChatCompletionsStreamResponse
		if err := common.UnmarshalJsonStr(data, &chunk); err != nil {
			logger.LogError(c, "failed to unmarshal chat stream event: "+err.Error())
			sr.Error(err)
			return
		}

		if chunk.Model != "" {
			model = chunk.Model
		}
		if chunk.Created != 0 {
			createdAt = int(chunk.Created)
		}
		if chunk.Usage != nil {
			usage = buildResponsesCompatUsage(chunk.Usage)
		}

		if len(chunk.Choices) == 0 {
			return
		}

		choice := chunk.Choices[0]
		if choice.Delta.Role != "" {
			if !sendResponseInProgress() {
				sr.Stop(streamErr)
			}
		}

		if reasoningDelta := choice.Delta.GetReasoningContent(); reasoningDelta != "" {
			if !sendReasoningDelta(reasoningDelta) {
				sr.Stop(streamErr)
				return
			}
		}

		contentDelta := choice.Delta.GetContentString()
		if contentDelta != "" {
			if !sendTextItemAdded() {
				sr.Stop(streamErr)
				return
			}
			textBuilder.WriteString(contentDelta)
			usageBuilder.WriteString(contentDelta)
			err := sendResponsesCompatEvent(c, "response.output_text.delta", dto.ResponsesStreamResponse{
				Delta:        contentDelta,
				ItemID:       textItemID,
				OutputIndex:  common.GetPointer(textOutputIndex),
				ContentIndex: common.GetPointer(0),
			})
			if err != nil {
				streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
				sr.Stop(streamErr)
				return
			}
		}

		for _, toolCall := range choice.Delta.ToolCalls {
			index := 0
			if toolCall.Index != nil {
				index = *toolCall.Index
			}
			declaredToolType := strings.TrimSpace(common.Interface2String(toolCall.Type))
			toolType := declaredToolType
			if toolType == "" {
				toolType = "function"
			}
			state, ok := toolCalls[index]
			if !ok {
				callID := strings.TrimSpace(toolCall.ID)
				if callID == "" {
					callID = fmt.Sprintf("call_%d", index)
				}
				itemPrefix := "fc"
				if toolType == dto.CustomType {
					itemPrefix = "ctc"
				}
				state = &chatToolCallState{
					outputIndex:  -1,
					itemID:       fmt.Sprintf("%s_%d", itemPrefix, index),
					callID:       callID,
					toolType:     toolType,
					chatToolType: toolType,
				}
				toolCalls[index] = state
			}
			if declaredToolType != "" {
				state.chatToolType = declaredToolType
				if state.toolType != dto.CustomType && state.toolType != "tool_search" {
					state.toolType = declaredToolType
				}
			}
			if strings.TrimSpace(toolCall.ID) != "" {
				state.callID = strings.TrimSpace(toolCall.ID)
			}
			if state.toolType == dto.CustomType && state.chatToolType == dto.CustomType {
				if !options.EnableCustomToolBridge {
					streamErr = types.NewOpenAIError(
						fmt.Errorf("custom tool bridge is not enabled in responses compatibility mode"),
						types.ErrorCodeBadResponse,
						http.StatusInternalServerError,
					)
					sr.Stop(streamErr)
					return
				}
				name, inputDelta, inputSeen, err := parseChatCustomStreamDelta(toolCall.Custom)
				if err != nil {
					streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
					sr.Stop(streamErr)
					return
				}
				if name != "" {
					state.name = name
					applyResponsesChatToolIdentity(state, toolState)
				}
				if inputSeen {
					state.customInputSeen = true
					state.input.WriteString(inputDelta)
					if inputDelta != "" {
						usageBuilder.WriteString(inputDelta)
					}
				}
				if strings.TrimSpace(state.name) != "" && !sendToolItemAdded(state) {
					sr.Stop(streamErr)
					return
				}
				continue
			}
			if strings.TrimSpace(toolCall.Function.Name) != "" {
				state.name = strings.TrimSpace(toolCall.Function.Name)
				applyResponsesChatToolIdentity(state, toolState)
			}
			if toolCall.Function.Arguments != "" {
				state.arguments.WriteString(toolCall.Function.Arguments)
				usageBuilder.WriteString(toolCall.Function.Arguments)
			}
			if strings.TrimSpace(state.name) == "" && service.ResponsesChatCompatibilityHasCustomTool(toolState) {
				continue
			}
			if !sendToolItemAdded(state) {
				sr.Stop(streamErr)
				return
			}
			if !sendPendingFunctionArguments(state) {
				sr.Stop(streamErr)
				return
			}
		}

		if choice.FinishReason != nil && *choice.FinishReason != "" {
			finishReason = strings.TrimSpace(*choice.FinishReason)
			if !sendResponseInProgress() {
				sr.Stop(streamErr)
				return
			}
		}
	})

	if streamErr != nil {
		return nil, streamErr
	}
	if streamErr := responsesViaChatStreamInterruptedError(c, info); streamErr != nil {
		return nil, streamErr
	}

	if usage == nil || usage.TotalTokens == 0 {
		usage = buildResponsesCompatUsage(service.ResponseText2Usage(c, usageBuilder.String(), info.UpstreamModelName, info.GetEstimatePromptTokens()))
	}

	if !sendResponseInProgress() {
		return nil, streamErr
	}
	for _, state := range reasoningStates {
		if !finalizeReasoningState(state) {
			return nil, streamErr
		}
	}
	if !finalizeTextItem() {
		return nil, streamErr
	}
	if !finalizeToolCalls() {
		return nil, streamErr
	}
	orderedOutput := make([]dto.ResponsesOutput, 0, len(completedOutput))
	for outputIndex := 0; outputIndex < nextOutputIndex; outputIndex++ {
		output, ok := completedOutput[outputIndex]
		if !ok {
			return nil, types.NewOpenAIError(
				fmt.Errorf("missing completed response output at index %d", outputIndex),
				types.ErrorCodeBadResponse,
				http.StatusInternalServerError,
			)
		}
		orderedOutput = append(orderedOutput, output)
	}

	terminalEventType := "response.completed"
	terminalStatus := statusCompleted
	var incompleteDetails *dto.IncompleteDetails
	switch strings.ToLower(finishReason) {
	case "length":
		terminalEventType = "response.incomplete"
		terminalStatus = statusIncomplete
		incompleteDetails = &dto.IncompleteDetails{Reason: "max_output_tokens"}
	case "content_filter":
		terminalEventType = "response.incomplete"
		terminalStatus = statusIncomplete
		incompleteDetails = &dto.IncompleteDetails{Reason: "content_filter"}
	}
	err := sendResponsesCompatEvent(c, terminalEventType, dto.ResponsesStreamResponse{
		Response: &dto.OpenAIResponsesResponse{
			ID:                responseID,
			Object:            "response",
			CreatedAt:         createdAt,
			Status:            terminalStatus,
			IncompleteDetails: incompleteDetails,
			Model:             model,
			Output:            orderedOutput,
			Usage:             buildResponsesCompatUsage(usage),
		},
	})
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	return usage, nil
}

func responsesViaChatStreamErrorStatus(upstreamStatus int, openAIError *types.OpenAIError, downstreamStarted bool) int {
	if downstreamStarted {
		return upstreamStatus
	}
	return responsesOpenAIErrorStatus(upstreamStatus, openAIError)
}

func responsesViaChatStreamInterruptedError(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	if retryable := retryableUpstreamStreamInterruptedError(c, info); retryable != nil {
		return retryable
	}
	if info == nil || info.StreamStatus == nil ||
		info.StreamStatus.EndReason != relaycommon.StreamEndReasonUpstreamInterrupted {
		return nil
	}
	err := info.StreamStatus.EndError
	if err == nil {
		err = io.ErrUnexpectedEOF
	}
	return types.NewOpenAIError(
		err,
		types.ErrorCodeUpstreamTransportInterrupted,
		http.StatusBadGateway,
		types.ErrOptionWithSkipRetry(),
	)
}
