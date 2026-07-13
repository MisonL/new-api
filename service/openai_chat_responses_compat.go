package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service/openaicompat"
	"github.com/QuantumNous/new-api/setting/model_setting"
)

type ResponsesChatCompatibilityOptions = openaicompat.ResponsesChatCompatibilityOptions
type ResponsesChatCompatibilityToolState = openaicompat.ResponsesChatCompatibilityToolState

const responsesChatCompatibilityIDPrefix = "resp_newapi_chatcmp_"

var (
	ErrResponsesChatCompatibilityPreviousIDUnsupported    = errors.New("responses chat compatibility previous_response_id is unsupported")
	ErrResponsesChatCompatibilityToolBridgeRequestInvalid = errors.New("responses chat compatibility tool bridge request is invalid")
)

func NewResponsesChatCompatibilityID(requestID string) string {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		requestID = common.GetUUID()
	}
	return responsesChatCompatibilityIDPrefix + requestID
}

func IsResponsesChatCompatibilityReference(responseID string) bool {
	return strings.HasPrefix(strings.TrimSpace(responseID), responsesChatCompatibilityIDPrefix)
}

func ChatCompletionsRequestToResponsesRequest(req *dto.GeneralOpenAIRequest) (*dto.OpenAIResponsesRequest, error) {
	return openaicompat.ChatCompletionsRequestToResponsesRequest(req)
}

func ResponsesRequestToChatCompletionsRequest(req *dto.OpenAIResponsesRequest) (*dto.GeneralOpenAIRequest, error) {
	return openaicompat.ResponsesRequestToChatCompletionsRequest(req)
}

func ResponsesRequestToChatCompletionsRequestWithOptions(req *dto.OpenAIResponsesRequest, options ResponsesChatCompatibilityOptions) (*dto.GeneralOpenAIRequest, error) {
	return openaicompat.ResponsesRequestToChatCompletionsRequestWithOptions(req, options)
}

func ResponsesRequestToChatCompletionsRequestWithState(req *dto.OpenAIResponsesRequest, options ResponsesChatCompatibilityOptions) (*dto.GeneralOpenAIRequest, *ResponsesChatCompatibilityToolState, error) {
	return openaicompat.ResponsesRequestToChatCompletionsRequestWithState(req, options)
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
	if relayMode != relayconstant.RelayModeResponses {
		return nil, nil
	}
	if request != nil && IsResponsesChatCompatibilityReference(request.PreviousResponseID) {
		return nil, ErrResponsesChatCompatibilityPreviousIDUnsupported
	}
	if passThroughGlobal || channelSetting.PassThroughBodyEnabled {
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
	return openaicompat.FindProtocolConversionRulePolicyWithFilter(
		model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy,
		model_setting.ProtocolEndpointResponses,
		model_setting.ProtocolEndpointChatCompletions,
		channelID,
		channelType,
		model,
		func(rule *model_setting.ProtocolConversionRule) (bool, error) {
			if rule == nil || rule.Options == nil || !rule.Options.OnlyWhenToolBridgeRequired {
				return true, nil
			}
			return responsesRequestRequiresToolBridge(request)
		},
	)
}

func FindUnconditionalResponsesViaChatRule(channelID int, channelType int, model string) (*model_setting.ProtocolConversionRule, error) {
	return openaicompat.FindProtocolConversionRulePolicyWithFilter(
		model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy,
		model_setting.ProtocolEndpointResponses,
		model_setting.ProtocolEndpointChatCompletions,
		channelID,
		channelType,
		model,
		func(rule *model_setting.ProtocolConversionRule) (bool, error) {
			return rule == nil || rule.Options == nil || !rule.Options.OnlyWhenToolBridgeRequired, nil
		},
	)
}

func responsesRequestRequiresToolBridge(request *dto.OpenAIResponsesRequest) (bool, error) {
	if request == nil {
		return false, nil
	}

	tools, err := request.EffectiveTools()
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrResponsesChatCompatibilityToolBridgeRequestInvalid, err)
	}
	for index, tool := range tools {
		required, err := responsesToolRequiresBridge(tool)
		if err != nil {
			return false, fmt.Errorf("%w: tool[%d] %v", ErrResponsesChatCompatibilityToolBridgeRequestInvalid, index, err)
		}
		if required {
			return true, nil
		}
	}

	required, err := responsesInputRequiresToolBridge(request.Input)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrResponsesChatCompatibilityToolBridgeRequestInvalid, err)
	}
	if required {
		return true, nil
	}
	required, err = responsesToolChoiceRequiresBridge(request.ToolChoice)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrResponsesChatCompatibilityToolBridgeRequestInvalid, err)
	}
	return required, nil
}

func responsesToolRequiresBridge(raw common.RawMessage) (bool, error) {
	switch common.GetJsonType(raw) {
	case "string":
		var name string
		if err := common.Unmarshal(raw, &name); err != nil {
			return false, fmt.Errorf("failed to parse custom tool name: %w", err)
		}
		if strings.TrimSpace(name) == "" {
			return false, fmt.Errorf("custom tool name is required")
		}
		return true, nil
	case "object":
	default:
		return false, fmt.Errorf("must be an object or string")
	}
	var tool map[string]any
	if err := common.Unmarshal(raw, &tool); err != nil {
		return false, fmt.Errorf("failed to parse: %w", err)
	}
	switch strings.TrimSpace(common.Interface2String(tool["type"])) {
	case "custom", "namespace", "tool_search":
		return true, nil
	default:
		return false, nil
	}
}

func responsesInputRequiresToolBridge(raw common.RawMessage) (bool, error) {
	if common.GetJsonType(raw) != "array" {
		return false, nil
	}
	var items []common.RawMessage
	if err := common.Unmarshal(raw, &items); err != nil {
		return false, fmt.Errorf("failed to parse input: %w", err)
	}
	for _, rawItem := range items {
		if common.GetJsonType(rawItem) != "object" {
			continue
		}
		var item struct {
			Type      string `json:"type"`
			Namespace string `json:"namespace"`
		}
		if err := common.Unmarshal(rawItem, &item); err != nil {
			return false, fmt.Errorf("failed to parse input item: %w", err)
		}
		switch strings.TrimSpace(item.Type) {
		case "custom_tool_call", "custom_tool_call_output", "tool_search_call", "tool_search_output":
			return true, nil
		case "function_call", "function_call_output":
			if strings.TrimSpace(item.Namespace) != "" {
				return true, nil
			}
		}
	}
	return false, nil
}

func responsesToolChoiceRequiresBridge(raw common.RawMessage) (bool, error) {
	if common.GetJsonType(raw) == "unknown" || common.GetJsonType(raw) == "null" || common.GetJsonType(raw) == "string" {
		return false, nil
	}
	if common.GetJsonType(raw) != "object" {
		return false, fmt.Errorf("tool_choice must be a string or object")
	}
	var choice map[string]any
	if err := common.Unmarshal(raw, &choice); err != nil {
		return false, fmt.Errorf("failed to parse tool_choice: %w", err)
	}
	switch strings.TrimSpace(common.Interface2String(choice["type"])) {
	case "custom", "namespace", "tool_search":
		return true, nil
	case "":
		return false, fmt.Errorf("tool_choice.type is required")
	default:
		return false, nil
	}
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

func ChatCompletionsResponseToResponsesResponseWithOptionsAndState(resp *dto.OpenAITextResponse, id string, options ResponsesChatCompatibilityOptions, toolState *ResponsesChatCompatibilityToolState) (*dto.OpenAIResponsesResponse, *dto.Usage, error) {
	return openaicompat.ChatCompletionsResponseToResponsesResponseWithOptionsAndState(resp, id, options, toolState)
}

func ParseResponsesCustomToolFunctionArguments(arguments string) (string, error) {
	return openaicompat.ParseChatCustomToolFunctionArguments(arguments)
}

func ResponsesChatCompatibilityHasCustomTool(toolState *ResponsesChatCompatibilityToolState) bool {
	return toolState.HasCustomTool()
}

func ExtractOutputTextFromResponses(resp *dto.OpenAIResponsesResponse) string {
	return openaicompat.ExtractOutputTextFromResponses(resp)
}

func ExtractReasoningSummaryFromResponses(resp *dto.OpenAIResponsesResponse) string {
	return openaicompat.ExtractReasoningSummaryFromResponses(resp)
}
