package relay

import (
	"bytes"
	"context"
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
	conversionRule, err := findResponsesViaChatRule(relaycommon.GinRequestContext(c), info, passThroughGlobal, request)
	if err != nil {
		return newResponsesConvertRequestError(err)
	}
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
	requestBody, convertedRequest, err := buildOpenAIResponsesRequestBody(c, info, adaptor, request, passThroughGlobal)
	if err != nil {
		return nil, err
	}

	usage, newAPIError := doOpenAIResponsesRequest(c, info, adaptor, requestBody)
	if newAPIError == nil {
		return usage, nil
	}

	if !shouldRetryResponsesWithoutEncryptedReasoning(info, newAPIError) {
		return nil, newAPIError
	}
	strippedRequest, result, stripErr := relaycommon.StripEncryptedReasoningFromResponsesRequest(convertedRequest)
	if stripErr != nil {
		return nil, newAPIError
	}
	if result.RemovedCount() == 0 {
		return nil, newAPIError
	}
	common.SetContextKey(c, appconstant.ContextKeyResponsesEncryptedContextRetry, true)
	common.SysLog(fmt.Sprintf(
		"responses encrypted reasoning retry: channel_id=%d model=%s encrypted_reasoning_items=%d remaining_items=%d original_error=%s",
		info.ChannelId,
		info.OriginModelName,
		result.EncryptedReasoningCount,
		result.RemainingCount,
		newAPIError.MaskSensitiveError(),
	))
	retryBody, _, retryBuildErr := buildOpenAIResponsesRequestBody(c, info, adaptor, &strippedRequest, false, buildResponsesRequestBodyOptions{
		ForceConvertedBody: true,
	})
	if retryBuildErr != nil {
		return nil, retryBuildErr
	}
	return doOpenAIResponsesRequest(c, info, adaptor, retryBody)
}

type buildResponsesRequestBodyOptions struct {
	ForceConvertedBody bool
}

func buildOpenAIResponsesRequestBody(c *gin.Context, info *relaycommon.RelayInfo, adaptor relaychannel.Adaptor, request *dto.OpenAIResponsesRequest, passThroughGlobal bool, options ...buildResponsesRequestBodyOptions) (io.Reader, dto.OpenAIResponsesRequest, *types.NewAPIError) {
	var requestBody io.Reader
	convertedResponsesRequest := *request
	syntheticCompactReference := false
	nativeOpaqueReference := service.IsNativeOpaqueCompactReference(request.PreviousResponseID)
	if relaycommon.IsOpenAICompatibleResponses(info) {
		var err error
		syntheticCompactReference, err = service.HasLocalSyntheticCompactReferenceWithContext(c.Request.Context(), *request)
		if err != nil {
			return nil, convertedResponsesRequest, newResponsesConvertRequestError(err)
		}
	}
	actualPassThroughBody := (passThroughGlobal || info.ChannelSetting.PassThroughBodyEnabled) &&
		!relaycommon.ShouldConvertResponsesRequest(info) &&
		(!syntheticCompactReference || nativeOpaqueReference)
	if len(options) > 0 && options[0].ForceConvertedBody {
		actualPassThroughBody = false
	}
	if actualPassThroughBody {
		if nativeOpaqueReference {
			restoredRequest, restored, applyInfo, err := service.RestoreNativeOpaquePreviousResponseID(
				c.Request.Context(),
				service.SyntheticCompactScopeFromSource(info),
				*request,
			)
			service.SetSyntheticCompactApplyInfo(c, applyInfo)
			if err != nil {
				return nil, convertedResponsesRequest, newResponsesConvertRequestError(err)
			}
			if restored {
				convertedResponsesRequest = restoredRequest
				body, err := common.Marshal(restoredRequest)
				if err != nil {
					return nil, convertedResponsesRequest, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
				}
				requestBody = bytes.NewReader(body)
			}
		}
		if requestBody == nil {
			storage, err := common.GetBodyStorage(c)
			if err != nil {
				return nil, convertedResponsesRequest, types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
			}
			requestBody = common.ReaderOnly(storage)
		}
	} else {
		if nativeOpaqueReference {
			restoredRequest, restored, applyInfo, err := service.RestoreNativeOpaquePreviousResponseID(
				c.Request.Context(),
				service.SyntheticCompactScopeFromSource(info),
				*request,
			)
			service.SetSyntheticCompactApplyInfo(c, applyInfo)
			if err != nil {
				return nil, convertedResponsesRequest, newResponsesConvertRequestError(err)
			}
			if restored {
				request = &restoredRequest
				convertedResponsesRequest = restoredRequest
			}
		}
		convertedRequest, err := adaptor.ConvertOpenAIResponsesRequest(c, info, *request)
		if err != nil {
			return nil, convertedResponsesRequest, newResponsesConvertRequestError(err)
		}
		if converted, ok := convertedRequest.(dto.OpenAIResponsesRequest); ok {
			convertedResponsesRequest = converted
		}
		relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)
		jsonData, err := common.Marshal(convertedRequest)
		if err != nil {
			return nil, convertedResponsesRequest, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}

		// Converted requests always use filtering; raw pass-through is the only path that preserves user-controlled fields.
		jsonData, err = relaycommon.RemoveDisabledFields(jsonData, info.ChannelOtherSettings, actualPassThroughBody)
		if err != nil {
			return nil, convertedResponsesRequest, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}

		// apply param override
		if len(info.ParamOverride) > 0 {
			jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
			if err != nil {
				return nil, convertedResponsesRequest, newAPIErrorFromParamOverride(err)
			}
		}

		if common.DebugEnabled {
			println("requestBody: ", string(jsonData))
		}
		requestBody = bytes.NewBuffer(jsonData)
	}
	return requestBody, convertedResponsesRequest, nil
}

func doOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, adaptor relaychannel.Adaptor, requestBody io.Reader) (*dto.Usage, *types.NewAPIError) {
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

func shouldRetryResponsesWithoutEncryptedReasoning(info *relaycommon.RelayInfo, err *types.NewAPIError) bool {
	if info == nil || err == nil || !relaycommon.IsOpenAICompatibleResponses(info) {
		return false
	}
	if err.StatusCode != http.StatusBadRequest {
		return false
	}
	errorCode := strings.ToLower(string(err.GetErrorCode()))
	if errorCode == "invalid_encrypted_content" || errorCode == "thinking_signature_invalid" {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "encrypted content") &&
		(strings.Contains(message, "could not be verified") ||
			strings.Contains(message, "could not be decrypted or parsed"))
}

func newResponsesConvertRequestError(err error) *types.NewAPIError {
	options := []types.NewAPIErrorOptions{types.ErrOptionWithSkipRetry()}
	if errors.Is(err, service.ErrSyntheticCompactStateNotFound) ||
		errors.Is(err, service.ErrSyntheticCompactRequiresVisibleInput) ||
		errors.Is(err, service.ErrSyntheticCompactStateScopeMismatch) ||
		errors.Is(err, service.ErrSyntheticCompactMultipleMarkers) ||
		errors.Is(err, service.ErrResponsesRESTPreviousIDUnsupported) ||
		errors.Is(err, service.ErrResponsesNativeOpaqueStateNotRestorable) {
		options = append(options, types.ErrOptionWithStatusCode(http.StatusBadRequest))
	}
	if errors.Is(err, service.ErrResponsesCompactionContentRequired) {
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
	rule, err := findResponsesViaChatRule(context.Background(), info, passThroughGlobal, nil)
	return err == nil && rule != nil
}

func findResponsesViaChatRule(ctx context.Context, info *relaycommon.RelayInfo, passThroughGlobal bool, request *dto.OpenAIResponsesRequest) (*model_setting.ProtocolConversionRule, error) {
	if info == nil {
		return nil, nil
	}
	return service.FindResponsesViaChatRule(
		ctx,
		info.RelayMode,
		passThroughGlobal,
		info.ChannelSetting,
		info.ChannelId,
		info.ChannelType,
		info.OriginModelName,
		request,
	)
}

func responsesViaChatOptionsFromRule(rule *model_setting.ProtocolConversionRule) service.ResponsesChatCompatibilityOptions {
	return service.ResponsesViaChatRuleOptions(rule)
}
