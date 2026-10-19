package palm

import (
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func requestOpenAI2PaLM(request dto.GeneralOpenAIRequest) (*PaLMChatRequest, error) {
	if err := channel.RejectUnsupportedTextRequestFields(request, "Google PaLM"); err != nil {
		return nil, err
	}
	if request.MaxTokens != nil || request.MaxCompletionTokens != nil || request.Stop != nil ||
		request.FrequencyPenalty != nil || request.PresencePenalty != nil || request.Seed != nil ||
		request.StreamOptions != nil || len(request.ExtraBody) > 0 || len(request.WebSearch) > 0 ||
		len(request.EnableSearch) > 0 {
		return nil, fmt.Errorf("request contains options that are not supported by Google PaLM")
	}

	roles := map[string]string{
		"system":    "system",
		"developer": "system",
		"user":      "user",
		"assistant": "assistant",
	}
	paLMRequest := &PaLMChatRequest{Prompt: PaLMPrompt{Messages: make([]PaLMChatMessage, 0, len(request.Messages))}}
	seenConversation := false
	seenUser := false
	for messageIndex, message := range request.Messages {
		role, text, err := channel.OpenAIMessageRoleText(message, "Google PaLM", roles)
		if err != nil {
			return nil, fmt.Errorf("messages[%d]: %w", messageIndex, err)
		}
		if role == "system" {
			if seenConversation {
				return nil, fmt.Errorf("messages[%d]: system/developer messages must be first for Google PaLM", messageIndex)
			}
			if paLMRequest.Prompt.Context != "" {
				paLMRequest.Prompt.Context += "\n"
			}
			paLMRequest.Prompt.Context += text
			continue
		}
		seenConversation = true
		if role == "user" {
			seenUser = true
		}
		paLMRequest.Prompt.Messages = append(paLMRequest.Prompt.Messages, PaLMChatMessage{Author: role, Content: text})
	}
	if len(paLMRequest.Prompt.Messages) == 0 && request.Prompt != nil {
		prompt, ok := request.Prompt.(string)
		if !ok {
			return nil, fmt.Errorf("prompt must be a string for Google PaLM")
		}
		paLMRequest.Prompt.Messages = append(paLMRequest.Prompt.Messages, PaLMChatMessage{Author: "user", Content: prompt})
		seenUser = true
	}
	if len(paLMRequest.Prompt.Messages) == 0 || !seenUser {
		return nil, fmt.Errorf("at least one user message is required by Google PaLM")
	}
	if len(paLMRequest.Prompt.Messages) > 1 {
		for index, message := range paLMRequest.Prompt.Messages {
			if index%2 == 0 && message.Author != "user" || index%2 == 1 && message.Author != "assistant" {
				return nil, fmt.Errorf("messages[%d]: turns must alternate user and assistant for Google PaLM", index)
			}
		}
		if paLMRequest.Prompt.Messages[len(paLMRequest.Prompt.Messages)-1].Author != "user" {
			return nil, fmt.Errorf("final message must have role user for Google PaLM")
		}
	}

	if request.Temperature != nil {
		value := *request.Temperature
		paLMRequest.Temperature = &value
	}
	if request.TopP != nil {
		value := *request.TopP
		paLMRequest.TopP = &value
	}
	if request.TopK != nil {
		if *request.TopK < 0 {
			return nil, fmt.Errorf("top_k must not be negative for Google PaLM")
		}
		value := uint(*request.TopK)
		paLMRequest.TopK = &value
	}
	if request.N != nil {
		if *request.N < 1 || *request.N > 8 {
			return nil, fmt.Errorf("n must be between 1 and 8 for Google PaLM")
		}
		value := *request.N
		paLMRequest.CandidateCount = &value
	}
	return paLMRequest, nil
}

// https://developers.generativeai.google/api/rest/generativelanguage/models/generateMessage#request-body
// https://developers.generativeai.google/api/rest/generativelanguage/models/generateMessage#response-body

func responsePaLM2OpenAI(response *PaLMChatResponse) *dto.OpenAITextResponse {
	fullTextResponse := dto.OpenAITextResponse{
		Choices: make([]dto.OpenAITextResponseChoice, 0, len(response.Candidates)),
	}
	for i, candidate := range response.Candidates {
		choice := dto.OpenAITextResponseChoice{
			Index: i,
			Message: dto.Message{
				Role:    "assistant",
				Content: candidate.Content,
			},
			FinishReason: "stop",
		}
		fullTextResponse.Choices = append(fullTextResponse.Choices, choice)
	}
	return &fullTextResponse
}

func streamResponsePaLM2OpenAI(palmResponse *PaLMChatResponse) *dto.ChatCompletionsStreamResponse {
	var choice dto.ChatCompletionsStreamResponseChoice
	if len(palmResponse.Candidates) > 0 {
		choice.Delta.SetContentString(palmResponse.Candidates[0].Content)
	}
	choice.FinishReason = &constant.FinishReasonStop
	var response dto.ChatCompletionsStreamResponse
	response.Object = "chat.completion.chunk"
	response.Model = "palm2"
	response.Choices = []dto.ChatCompletionsStreamResponseChoice{choice}
	return &response
}

func palmStreamHandler(c *gin.Context, resp *http.Response) (*types.NewAPIError, string) {
	responseText := ""
	responseId := helper.GetResponseID(c)
	createdTime := common.GetTimestamp()
	dataChan := make(chan string)
	stopChan := make(chan bool)
	go func() {
		responseBody, err := io.ReadAll(resp.Body)
		if err != nil {
			common.SysLog("error reading stream response: " + err.Error())
			stopChan <- true
			return
		}
		service.CloseResponseBodyGracefully(resp)
		var palmResponse PaLMChatResponse
		err = common.Unmarshal(responseBody, &palmResponse)
		if err != nil {
			common.SysLog("error unmarshalling stream response: " + err.Error())
			stopChan <- true
			return
		}
		fullTextResponse := streamResponsePaLM2OpenAI(&palmResponse)
		fullTextResponse.Id = responseId
		fullTextResponse.Created = createdTime
		if len(palmResponse.Candidates) > 0 {
			responseText = palmResponse.Candidates[0].Content
		}
		jsonResponse, err := common.Marshal(fullTextResponse)
		if err != nil {
			common.SysLog("error marshalling stream response: " + err.Error())
			stopChan <- true
			return
		}
		dataChan <- string(jsonResponse)
		stopChan <- true
	}()
	helper.SetEventStreamHeaders(c)
	c.Stream(func(w io.Writer) bool {
		select {
		case data := <-dataChan:
			c.Render(-1, &common.CustomEvent{Data: "data: " + data})
			return true
		case <-stopChan:
			c.Render(-1, &common.CustomEvent{Data: "data: [DONE]"})
			return false
		}
	})
	service.CloseResponseBodyGracefully(resp)
	return nil, responseText
}

func palmHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	service.CloseResponseBodyGracefully(resp)
	var palmResponse PaLMChatResponse
	err = common.Unmarshal(responseBody, &palmResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if palmResponse.Error.Code != 0 || len(palmResponse.Candidates) == 0 {
		return nil, types.WithOpenAIError(types.OpenAIError{
			Message: palmResponse.Error.Message,
			Type:    palmResponse.Error.Status,
			Param:   "",
			Code:    palmResponse.Error.Code,
		}, resp.StatusCode)
	}
	fullTextResponse := responsePaLM2OpenAI(&palmResponse)
	usage := service.ResponseText2Usage(c, palmResponse.Candidates[0].Content, info.UpstreamModelName, info.GetEstimatePromptTokens())
	fullTextResponse.Usage = *usage
	jsonResponse, err := common.Marshal(fullTextResponse)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	service.IOCopyBytesGracefully(c, resp, jsonResponse)
	return usage, nil
}
