package openai

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type chatToolCallState struct {
	outputIndex int
	itemID      string
	callID      string
	name        string
	arguments   strings.Builder
	sentAdded   bool
}

func marshalResponsesCompatArguments(arguments string) (common.RawMessage, error) {
	return common.Marshal(arguments)
}

func getResponsesCompatID(c *gin.Context) string {
	logID := c.GetString(common.RequestIdKey)
	if logID == "" {
		return "resp_" + common.GetUUID()
	}
	return "resp_" + logID
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

func OaiChatToResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	defer service.CloseResponseBodyGracefully(resp)

	var chatResp dto.OpenAITextResponse
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	if err := common.Unmarshal(body, &chatResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := chatResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	responsesID := getResponsesCompatID(c)
	responsesResp, usage, err := service.ChatCompletionsResponseToResponsesResponse(&chatResp, responsesID)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if usage == nil || usage.TotalTokens == 0 {
		text := ""
		if len(chatResp.Choices) > 0 {
			text = chatResp.Choices[0].Message.StringContent()
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
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	defer service.CloseResponseBodyGracefully(resp)

	responseID := getResponsesCompatID(c)
	model := info.UpstreamModelName
	createdAt := int(time.Now().Unix())

	statusInProgress, _ := common.Marshal("in_progress")
	statusCompleted, _ := common.Marshal("completed")

	usage := &dto.Usage{}
	textItemID := "msg_0"
	textOutputIndex := 0
	textAdded := false
	var textBuilder strings.Builder
	var usageBuilder strings.Builder
	var completedOutput []dto.ResponsesOutput
	toolCalls := make(map[int]*chatToolCallState)
	var streamErr *types.NewAPIError
	responseCreatedSent := false
	responseInProgressSent := false
	textContentPartAdded := false
	getToolOutputIndex := func(index int) int {
		if textAdded || textBuilder.Len() > 0 {
			return index + 1
		}
		return index
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

	sendTextItemAdded := func() bool {
		if textAdded {
			return true
		}
		if !sendResponseInProgress() {
			return false
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
		if !sendResponseInProgress() {
			return false
		}
		argumentsRaw, err := marshalResponsesCompatArguments(state.arguments.String())
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

	finalizeToolCalls := func() bool {
		for index := 0; index < len(toolCalls); index++ {
			state := toolCalls[index]
			if state == nil {
				continue
			}
			if !sendToolItemAdded(state) {
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
					Arguments: argumentsRaw,
				},
				OutputIndex: common.GetPointer(state.outputIndex),
			})
			if err != nil {
				streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
				return false
			}
			completedOutput = append(completedOutput, dto.ResponsesOutput{
				Type:      "function_call",
				ID:        state.itemID,
				Status:    "completed",
				CallId:    state.callID,
				Name:      state.name,
				Arguments: argumentsRaw,
			})
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
		completedOutput = append([]dto.ResponsesOutput{
			{
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
		}, completedOutput...)
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
			state, ok := toolCalls[index]
			if !ok {
				callID := strings.TrimSpace(toolCall.ID)
				if callID == "" {
					callID = fmt.Sprintf("call_%d", index)
				}
				state = &chatToolCallState{
					outputIndex: getToolOutputIndex(index),
					itemID:      fmt.Sprintf("fc_%d", index),
					callID:      callID,
				}
				toolCalls[index] = state
			}
			state.outputIndex = getToolOutputIndex(index)
			if strings.TrimSpace(toolCall.ID) != "" {
				state.callID = strings.TrimSpace(toolCall.ID)
			}
			if strings.TrimSpace(toolCall.Function.Name) != "" {
				state.name = strings.TrimSpace(toolCall.Function.Name)
			}
			if !sendToolItemAdded(state) {
				sr.Stop(streamErr)
				return
			}
			if toolCall.Function.Arguments != "" {
				state.arguments.WriteString(toolCall.Function.Arguments)
				usageBuilder.WriteString(toolCall.Function.Arguments)
				err := sendResponsesCompatEvent(c, "response.function_call_arguments.delta", dto.ResponsesStreamResponse{
					Delta:       toolCall.Function.Arguments,
					ItemID:      state.itemID,
					OutputIndex: common.GetPointer(state.outputIndex),
				})
				if err != nil {
					streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
					sr.Stop(streamErr)
					return
				}
			}
		}

		if choice.FinishReason != nil && *choice.FinishReason != "" {
			if !sendResponseInProgress() {
				sr.Stop(streamErr)
				return
			}
		}
	})

	if streamErr != nil {
		return nil, streamErr
	}

	if usage == nil || usage.TotalTokens == 0 {
		usage = buildResponsesCompatUsage(service.ResponseText2Usage(c, usageBuilder.String(), info.UpstreamModelName, info.GetEstimatePromptTokens()))
	}

	if !sendResponseInProgress() {
		return nil, streamErr
	}
	if !finalizeTextItem() {
		return nil, streamErr
	}
	if !finalizeToolCalls() {
		return nil, streamErr
	}

	err := sendResponsesCompatEvent(c, "response.completed", dto.ResponsesStreamResponse{
		Response: &dto.OpenAIResponsesResponse{
			ID:        responseID,
			Object:    "response",
			CreatedAt: createdAt,
			Status:    statusCompleted,
			Model:     model,
			Output:    completedOutput,
			Usage:     buildResponsesCompatUsage(usage),
		},
	})
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	return usage, nil
}
