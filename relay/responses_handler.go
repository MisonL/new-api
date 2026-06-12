package relay

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func ResponsesHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {
	info.InitChannelMeta(c)
	if info.RelayMode == relayconstant.RelayModeResponsesCompact {
		switch info.ApiType {
		case appconstant.APITypeOpenAI, appconstant.APITypeCodex:
		default:
			return types.NewErrorWithStatusCode(
				fmt.Errorf("unsupported endpoint %q for api type %d", "/v1/responses/compact", info.ApiType),
				types.ErrorCodeInvalidRequest,
				http.StatusBadRequest,
				types.ErrOptionWithSkipRetry(),
			)
		}
	}

	var responsesReq *dto.OpenAIResponsesRequest
	switch req := info.Request.(type) {
	case *dto.OpenAIResponsesRequest:
		responsesReq = req
	case *dto.OpenAIResponsesCompactionRequest:
		responsesReq = req.ToResponsesRequest()
	default:
		return types.NewErrorWithStatusCode(
			fmt.Errorf("invalid request type, expected dto.OpenAIResponsesRequest or dto.OpenAIResponsesCompactionRequest, got %T", info.Request),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}

	request, err := common.DeepCopy(responsesReq)
	if err != nil {
		return types.NewError(fmt.Errorf("failed to copy request to GeneralOpenAIRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	err = helper.ModelMappedHelper(c, info, request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}
	applyResponsesCompactSummaryModelOverride(c, info, request)

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)
	passThroughGlobal := model_setting.GetGlobalSettings().PassThroughRequestEnabled
	conversionRule := findResponsesViaChatRule(info, passThroughGlobal, request)
	if conversionRule != nil {
		usage, newApiErr := responsesViaChat(c, info, adaptor, request, responsesViaChatOptionsFromRule(conversionRule))
		if newApiErr != nil {
			return newApiErr
		}

		if strings.HasPrefix(info.OriginModelName, "gpt-4o-audio") {
			service.PostAudioConsumeQuota(c, info, usage, "")
		} else {
			service.PostTextConsumeQuota(c, info, usage, nil)
		}
		return nil
	}

	usageDto, newAPIError := executeOpenAIResponsesRequest(c, info, adaptor, request, passThroughGlobal)
	if newAPIError != nil {
		return newAPIError
	}

	if info.RelayMode == relayconstant.RelayModeResponsesCompact {
		originModelName := info.OriginModelName
		originPriceData := info.PriceData
		normalizeResponsesCompactUsage(info, usageDto)

		_, err := helper.ModelPriceHelper(c, info, info.GetEstimatePromptTokens(), &types.TokenCountMeta{})
		if err != nil {
			info.OriginModelName = originModelName
			info.PriceData = originPriceData
			return types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithSkipRetry(), types.ErrOptionWithStatusCode(http.StatusBadRequest))
		}
		service.PostTextConsumeQuota(c, info, usageDto, nil)

		info.OriginModelName = originModelName
		info.PriceData = originPriceData
		return nil
	}

	if strings.HasPrefix(info.OriginModelName, "gpt-4o-audio") {
		service.PostAudioConsumeQuota(c, info, usageDto, "")
	} else {
		service.PostTextConsumeQuota(c, info, usageDto, nil)
	}
	return nil
}

func normalizeResponsesCompactUsage(info *relaycommon.RelayInfo, usage *dto.Usage) {
	if info == nil || usage == nil {
		return
	}
	estimatePromptTokens := info.GetEstimatePromptTokens()
	if usage.PromptTokens == 0 {
		usage.PromptTokens = usage.InputTokens
	}
	if usage.InputTokens == 0 {
		usage.InputTokens = usage.PromptTokens
	}
	if usage.PromptTokens == 0 && estimatePromptTokens > 0 {
		usage.PromptTokens = estimatePromptTokens
		usage.InputTokens = estimatePromptTokens
	}
	if usage.CompletionTokens == 0 {
		usage.CompletionTokens = usage.OutputTokens
	}
	if usage.OutputTokens == 0 {
		usage.OutputTokens = usage.CompletionTokens
	}
	totalTokens := usage.PromptTokens + usage.CompletionTokens
	if usage.TotalTokens == 0 || usage.TotalTokens < totalTokens {
		usage.TotalTokens = totalTokens
	}
}

func executeOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, adaptor relaychannel.Adaptor, request *dto.OpenAIResponsesRequest, passThroughGlobal bool) (*dto.Usage, *types.NewAPIError) {
	if request == nil {
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("responses request is required"),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	var requestBody io.Reader
	syntheticCompactReference := false
	if relaycommon.IsOpenAICompatibleResponses(info) {
		var err error
		syntheticCompactReference, err = service.HasLocalSyntheticCompactReferenceWithContext(c.Request.Context(), *request)
		if err != nil {
			return nil, newResponsesConvertRequestError(err)
		}
	}
	actualPassThroughBody := (passThroughGlobal || info.ChannelSetting.PassThroughBodyEnabled) &&
		!relaycommon.ShouldConvertResponsesRequest(info) &&
		!syntheticCompactReference
	if actualPassThroughBody {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
		}
		requestBody = common.ReaderOnly(storage)
	} else {
		convertedRequest, err := adaptor.ConvertOpenAIResponsesRequest(c, info, *request)
		if err != nil {
			return nil, newResponsesConvertRequestError(err)
		}
		relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)
		jsonData, err := common.Marshal(convertedRequest)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}

		// Converted requests always use filtering; raw pass-through is the only path that preserves user-controlled fields.
		jsonData, err = relaycommon.RemoveDisabledFields(jsonData, info.ChannelOtherSettings, actualPassThroughBody)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}

		// apply param override
		if len(info.ParamOverride) > 0 {
			jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
			if err != nil {
				return nil, newAPIErrorFromParamOverride(err)
			}
		}

		if common.DebugEnabled {
			println("requestBody: ", string(jsonData))
		}
		requestBody = bytes.NewBuffer(jsonData)
	}

	var httpResp *http.Response
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}

	statusCodeMappingStr := c.GetString("status_code_mapping")

	if resp != nil {
		httpResp = resp.(*http.Response)

		if httpResp.StatusCode != http.StatusOK {
			newAPIError := service.RelayErrorHandler(c.Request.Context(), httpResp, false)
			// reset status code 重置状态码
			service.ResetStatusCode(newAPIError, statusCodeMappingStr)
			return nil, newAPIError
		}
	}

	usage, newAPIError := adaptor.DoResponse(c, httpResp, info)
	if newAPIError != nil {
		// reset status code 重置状态码
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return nil, newAPIError
	}

	usageDto := usage.(*dto.Usage)
	return usageDto, nil
}

func newResponsesConvertRequestError(err error) *types.NewAPIError {
	options := []types.NewAPIErrorOptions{types.ErrOptionWithSkipRetry()}
	if errors.Is(err, service.ErrSyntheticCompactStateNotFound) ||
		errors.Is(err, service.ErrSyntheticCompactRequiresVisibleInput) ||
		errors.Is(err, service.ErrSyntheticCompactStateScopeMismatch) ||
		errors.Is(err, service.ErrSyntheticCompactMultipleMarkers) ||
		errors.Is(err, service.ErrResponsesRESTPreviousIDUnsupported) {
		options = append(options, types.ErrOptionWithStatusCode(http.StatusBadRequest))
	}
	return types.NewError(err, types.ErrorCodeConvertRequestFailed, options...)
}

func applyResponsesCompactSummaryModelOverride(c *gin.Context, info *relaycommon.RelayInfo, request *dto.OpenAIResponsesRequest) {
	if c == nil || request == nil || !relaycommon.IsSyntheticOpenAICompatibleResponsesCompact(info) {
		return
	}
	model := strings.TrimSpace(common.GetContextKeyString(c, appconstant.ContextKeyResponsesCompactSummaryModel))
	if model == "" {
		return
	}
	request.SetModelName(model)
	info.UpstreamModelName = model
}

func shouldRouteResponsesViaChat(info *relaycommon.RelayInfo, passThroughGlobal bool) bool {
	return findResponsesViaChatRule(info, passThroughGlobal, nil) != nil
}

func findResponsesViaChatRule(info *relaycommon.RelayInfo, passThroughGlobal bool, request *dto.OpenAIResponsesRequest) *model_setting.ProtocolConversionRule {
	if info == nil ||
		info.RelayMode != relayconstant.RelayModeResponses ||
		passThroughGlobal ||
		info.ChannelSetting.PassThroughBodyEnabled ||
		request.HasCompactionTrigger() {
		return nil
	}
	return service.FindProtocolConversionRuleGlobal(
		model_setting.ProtocolEndpointResponses,
		model_setting.ProtocolEndpointChatCompletions,
		info.ChannelId,
		info.ChannelType,
		info.OriginModelName,
	)
}

func responsesViaChatOptionsFromRule(rule *model_setting.ProtocolConversionRule) service.ResponsesChatCompatibilityOptions {
	if rule == nil || rule.Options == nil {
		return service.ResponsesChatCompatibilityOptions{}
	}
	return service.ResponsesChatCompatibilityOptions{
		EnableCustomToolBridge: rule.Options.EnableCustomToolBridge,
	}
}
