package openai

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func OaiSyntheticResponsesCompactionHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)
	compactResp, usage, newAPIError := buildSyntheticResponsesCompaction(c, info, resp)
	if newAPIError != nil {
		return nil, newAPIError
	}
	body, err := common.Marshal(compactResp)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}
	service.IOCopyBytesGracefully(c, resp, body)
	return usage, nil
}

type syntheticRemoteResponsesCompactionResponse struct {
	ID        string              `json:"id"`
	Object    string              `json:"object"`
	CreatedAt int                 `json:"created_at"`
	Status    string              `json:"status"`
	Model     string              `json:"model"`
	Output    []common.RawMessage `json:"output"`
	Usage     *dto.Usage          `json:"usage"`
}

type syntheticRemoteResponsesCompactionEvent struct {
	Type        string                                      `json:"type"`
	Response    *syntheticRemoteResponsesCompactionResponse `json:"response,omitempty"`
	Item        common.RawMessage                           `json:"item,omitempty"`
	OutputIndex *int                                        `json:"output_index,omitempty"`
}

func OaiSyntheticRemoteResponsesCompactionHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)
	compactResp, usage, newAPIError := buildSyntheticResponsesCompaction(c, info, resp)
	if newAPIError != nil {
		return nil, newAPIError
	}
	var rawOutput []common.RawMessage
	if err := common.Unmarshal(compactResp.Output, &rawOutput); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	var output []dto.ResponsesOutput
	if err := common.Unmarshal(compactResp.Output, &output); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if len(rawOutput) != 1 {
		return nil, types.NewOpenAIError(errors.New("synthetic remote compaction output must contain exactly one item"), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if err := validateRemoteResponsesCompactionOutput(output); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	model := ""
	if info != nil {
		model = info.OriginModelName
	}
	response := &syntheticRemoteResponsesCompactionResponse{
		ID:        compactResp.ID,
		Object:    compactResp.Object,
		CreatedAt: compactResp.CreatedAt,
		Status:    "completed",
		Model:     model,
		Output:    rawOutput,
		Usage:     compactResp.Usage,
	}
	common.SetContextKey(c, constant.ContextKeyResponsesCompactionOutput, true)
	if info == nil || !info.IsStream {
		body, err := common.Marshal(response)
		if err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
		}
		service.IOCopyBytesGracefully(c, resp, body)
		return usage, nil
	}
	helper.SetEventStreamHeaders(c)
	outputIndex := 0
	events := []syntheticRemoteResponsesCompactionEvent{
		{Type: dto.ResponsesOutputTypeItemDone, Item: rawOutput[0], OutputIndex: &outputIndex},
		{Type: "response.completed", Response: response},
	}
	for _, event := range events {
		data, err := common.Marshal(event)
		if err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
		}
		sendResponsesStreamData(c, dto.ResponsesStreamResponse{Type: event.Type}, string(data))
	}
	return usage, nil
}

func buildSyntheticResponsesCompaction(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.OpenAIResponsesCompactionResponse, *dto.Usage, *types.NewAPIError) {

	responseBody, readErr := readResponsesNonStreamingResponseBody(c, resp.Body)
	if readErr != nil {
		return nil, nil, readErr
	}

	var responsesResp dto.OpenAIResponsesResponse
	if err := common.Unmarshal(responseBody, &responsesResp); err != nil {
		if openAIError, found, sseErr := responsesSSETerminalError(responseBody); sseErr != nil {
			return nil, nil, types.NewOpenAIError(sseErr, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		} else if found {
			return nil, nil, newResponsesUpstreamOpenAIError(resp.StatusCode, openAIError)
		}
		return nil, nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := responsesResp.GetOpenAIError(); responsesCompactOpenAIErrorHasContent(oaiError) {
		return nil, nil, newResponsesUpstreamOpenAIError(resp.StatusCode, oaiError)
	}

	model := ""
	if info != nil {
		model = info.OriginModelName
	}
	if model == "" {
		model = responsesResp.Model
	}
	if model == "" {
		return nil, nil, types.NewOpenAIError(errors.New("model name is required"), types.ErrorCodeInvalidRequest, http.StatusBadRequest)
	}
	compactResp, usage, err := service.BuildSyntheticCompactResponse(relaycommon.GinRequestContext(c), syntheticCompactScopeFromRelayInfo(info), model, responsesResp)
	if err != nil {
		return nil, nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	return compactResp, usage, nil
}
