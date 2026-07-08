package service

import (
	"context"
	"strings"

	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service/openaicompat"
	"github.com/QuantumNous/new-api/setting/model_setting"
)

type ResponsesChatCompatibilityOptions = openaicompat.ResponsesChatCompatibilityOptions

func ChatCompletionsRequestToResponsesRequest(req *dto.GeneralOpenAIRequest) (*dto.OpenAIResponsesRequest, error) {
	return openaicompat.ChatCompletionsRequestToResponsesRequest(req)
}

func ResponsesRequestToChatCompletionsRequest(req *dto.OpenAIResponsesRequest) (*dto.GeneralOpenAIRequest, error) {
	return openaicompat.ResponsesRequestToChatCompletionsRequest(req)
}

func ResponsesRequestToChatCompletionsRequestWithOptions(req *dto.OpenAIResponsesRequest, options ResponsesChatCompatibilityOptions) (*dto.GeneralOpenAIRequest, error) {
	return openaicompat.ResponsesRequestToChatCompletionsRequestWithOptions(req, options)
}

func ValidateResponsesRequestToChatCompatibility(req *dto.OpenAIResponsesRequest, options ResponsesChatCompatibilityOptions) error {
	_, err := openaicompat.ResponsesRequestToChatCompletionsRequestWithOptions(req, options)
	return err
}

func ResponsesViaChatRuleOptions(rule *model_setting.ProtocolConversionRule) ResponsesChatCompatibilityOptions {
	if rule == nil || rule.Options == nil {
		return ResponsesChatCompatibilityOptions{}
	}
	return ResponsesChatCompatibilityOptions{
		EnableCustomToolBridge: rule.Options.EnableCustomToolBridge,
	}
}

func FindResponsesViaChatRule(ctx context.Context, relayMode int, passThroughGlobal bool, channelSetting dto.ChannelSettings, channelID int, channelType int, model string, request *dto.OpenAIResponsesRequest) (*model_setting.ProtocolConversionRule, error) {
	if relayMode != relayconstant.RelayModeResponses ||
		passThroughGlobal ||
		channelSetting.PassThroughBodyEnabled {
		return nil, nil
	}
	if request != nil {
		previousResponseID := strings.TrimSpace(request.PreviousResponseID)
		if request.HasCompactionTrigger() {
			return nil, nil
		}
		if previousResponseID != "" {
			if IsNativeOpaqueCompactReference(previousResponseID) {
				return nil, nil
			}
			_, ok, err := syntheticCompactIDFromReference(ctx, previousResponseID)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, nil
			}
		}
		hasRemoteCompaction, err := HasRemoteResponsesCompactionInput(ctx, *request)
		if err != nil {
			return nil, err
		}
		if hasRemoteCompaction {
			return nil, nil
		}
	}
	return FindProtocolConversionRuleGlobal(
		model_setting.ProtocolEndpointResponses,
		model_setting.ProtocolEndpointChatCompletions,
		channelID,
		channelType,
		model,
	), nil
}

func ResponsesResponseToChatCompletionsResponse(resp *dto.OpenAIResponsesResponse, id string) (*dto.OpenAITextResponse, *dto.Usage, error) {
	return openaicompat.ResponsesResponseToChatCompletionsResponse(resp, id)
}

func ChatCompletionsResponseToResponsesResponse(resp *dto.OpenAITextResponse, id string) (*dto.OpenAIResponsesResponse, *dto.Usage, error) {
	return openaicompat.ChatCompletionsResponseToResponsesResponse(resp, id)
}

func ChatCompletionsResponseToResponsesResponseWithOptions(resp *dto.OpenAITextResponse, id string, options ResponsesChatCompatibilityOptions) (*dto.OpenAIResponsesResponse, *dto.Usage, error) {
	return openaicompat.ChatCompletionsResponseToResponsesResponseWithOptions(resp, id, options)
}

func ExtractOutputTextFromResponses(resp *dto.OpenAIResponsesResponse) string {
	return openaicompat.ExtractOutputTextFromResponses(resp)
}

func ExtractReasoningSummaryFromResponses(resp *dto.OpenAIResponsesResponse) string {
	return openaicompat.ExtractReasoningSummaryFromResponses(resp)
}
