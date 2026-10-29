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
	"github.com/QuantumNous/new-api/setting/reasoning"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const maxResponsesCompatibilityRetries = 8

const opaqueOpenAIResponsesReplayValidationContextKey = "opaque_openai_responses_replay_validation"

type opaqueOpenAIResponsesReplayValidation struct {
	request *dto.OpaqueOpenAIResponsesRequest
	info    *relaycommon.RelayInfo
}

type managedResponsesRequestBody struct {
	io.Reader
	closer io.Closer
}

type responsesRequestBodyMetadata struct {
	size    int64
	getBody func() (io.ReadCloser, error)
	folded  []byte
}

func snapshotResponsesRequestBodyMetadata(info *relaycommon.RelayInfo) responsesRequestBodyMetadata {
	if info == nil {
		return responsesRequestBodyMetadata{}
	}
	return responsesRequestBodyMetadata{
		size:    info.UpstreamRequestBodySize,
		getBody: info.UpstreamRequestGetBody,
		folded:  append([]byte(nil), info.CodexFoldedBaseRequestBody...),
	}
}

func restoreResponsesRequestBodyMetadata(info *relaycommon.RelayInfo, metadata responsesRequestBodyMetadata) {
	if info == nil {
		return
	}
	info.UpstreamRequestBodySize = metadata.size
	info.UpstreamRequestGetBody = metadata.getBody
	info.CodexFoldedBaseRequestBody = append(info.CodexFoldedBaseRequestBody[:0], metadata.folded...)
}

func (body *managedResponsesRequestBody) Close() error {
	if body == nil || body.closer == nil {
		return nil
	}
	err := body.closer.Close()
	body.closer = nil
	return err
}

func closeManagedResponsesRequestBody(body io.Reader) {
	if managed, ok := body.(*managedResponsesRequestBody); ok {
		_ = managed.Close()
	}
}

func relayOpaqueOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.OpaqueOpenAIResponsesRequest) *types.NewAPIError {
	if err := validateOpaqueOpenAIResponsesReplay(c, info, request); err != nil {
		return err
	}
	if err := helper.ModelMappedHelper(c, info, request); err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)

	usage, newAPIError := executeOpaqueOpenAIResponsesRequest(c, info, adaptor, request)
	if newAPIError != nil {
		return newAPIError
	}
	return settleOpenAIResponsesUsage(c, info, usage)
}

func validateOpaqueOpenAIResponsesReplay(c *gin.Context, info *relaycommon.RelayInfo, request *dto.OpaqueOpenAIResponsesRequest) *types.NewAPIError {
	if request == nil {
		return types.NewErrorWithStatusCode(
			errors.New("opaque Responses request is required"),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	metadata := request.OpaqueResponsesMetadata()
	if !metadata.ModelPresent || strings.TrimSpace(metadata.Model) == "" {
		return types.NewErrorWithStatusCode(
			errors.New("model is required"),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if !metadata.HasOpaqueEncryptedContent {
		return types.NewErrorWithStatusCode(
			errors.New("opaque Responses request is missing encrypted replay metadata"),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}

	capability := dto.ResponsesEncryptedReasoningReplayCapabilityUnknown
	if info != nil {
		capability = info.ChannelOtherSettings.ResolveResponsesEncryptedReasoningReplayCapability(info.ChannelType)
	}
	markResponsesEncryptedReasoningReplayDecision(c, capability)
	if capability == dto.ResponsesEncryptedReasoningReplayCapabilityUnsupported {
		channelID := 0
		if info != nil {
			channelID = info.ChannelId
		}
		return relaycommon.NewResponsesEncryptedReasoningReplayUnsupportedError(channelID)
	}
	if info != nil && info.RelayMode == relayconstant.RelayModeResponsesCompact &&
		!relaycommon.SupportsNativeOpaqueResponsesCompact(info.ChannelType, info.ChannelOtherSettings) {
		relaycommon.MarkResponsesEncryptedReasoningReplayRejected(c)
		return relaycommon.NewResponsesEncryptedReasoningReplayUnsupportedError(info.ChannelId)
	}
	if metadata.HasCompactionItem && info != nil &&
		(info.ChannelOtherSettings.HasResponsesProxyCompatibilityProfile() ||
			!info.ChannelOtherSettings.ResolveResponsesChannelCapability(info.ChannelType).SupportsCompactionItemPassthrough) {
		relaycommon.MarkResponsesEncryptedReasoningReplayRejected(c)
		return types.NewErrorWithStatusCode(
			errors.New("opaque Responses compaction item cannot be replayed by the selected upstream"),
			types.ErrorCodeChannelModelMappedError,
			http.StatusServiceUnavailable,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if c != nil {
		c.Set(opaqueOpenAIResponsesReplayValidationContextKey, opaqueOpenAIResponsesReplayValidation{
			request: request,
			info:    info,
		})
	}
	return nil
}

func hasValidatedOpaqueOpenAIResponsesReplay(c *gin.Context, info *relaycommon.RelayInfo, request *dto.OpaqueOpenAIResponsesRequest) bool {
	if c == nil {
		return false
	}
	value, exists := c.Get(opaqueOpenAIResponsesReplayValidationContextKey)
	if !exists {
		return false
	}
	validation, ok := value.(opaqueOpenAIResponsesReplayValidation)
	return ok && validation.request == request && validation.info == info
}

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
	case *dto.OpaqueOpenAIResponsesRequest:
		return relayOpaqueOpenAIResponsesRequest(c, info, req)
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
	if service.IsResponsesChatCompatibilityReference(responsesReq.PreviousResponseID) {
		return newResponsesConvertRequestError(service.ErrResponsesChatCompatibilityPreviousIDUnsupported)
	}
	hasOpaqueEncryptedContent, err := service.ResponsesInputHasOpaqueEncryptedContent(relaycommon.GinRequestContext(c), responsesReq.Input)
	if err != nil {
		return newResponsesConvertRequestError(err)
	}
	if hasOpaqueEncryptedContent {
		capability := info.ChannelOtherSettings.ResolveResponsesEncryptedReasoningReplayCapability(info.ChannelType)
		markResponsesEncryptedReasoningReplayDecision(c, capability)
		if capability == dto.ResponsesEncryptedReasoningReplayCapabilityUnsupported {
			return relaycommon.NewResponsesEncryptedReasoningReplayUnsupportedError(info.ChannelId)
		}
		if info.RelayMode == relayconstant.RelayModeResponsesCompact &&
			!relaycommon.SupportsNativeOpaqueResponsesCompact(info.ChannelType, info.ChannelOtherSettings) {
			markResponsesEncryptedReasoningReplayDecision(c, dto.ResponsesEncryptedReasoningReplayCapabilityUnsupported)
			return relaycommon.NewResponsesEncryptedReasoningReplayUnsupportedError(info.ChannelId)
		}
	}

	request, err := common.DeepCopy(responsesReq)
	if err != nil {
		return types.NewError(fmt.Errorf("failed to copy request to GeneralOpenAIRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	// 客户端携带 Responses/Chat 契约不接受的工具名时，归一化为确定性别名
	// 并把别名→原始名的映射挂到本次请求的 RelayInfo 上；响应路径
	// （OaiResponsesHandler / sendResponsesStreamData）据此把上游 echo 的
	// 别名还原为客户端原始名。无别名时 restorer 为空，响应原样转发。
	if !info.IsChannelTest {
		normalizedRequest, normalizeResult, restorer, err := relaycommon.NormalizeResponsesToolNamesWithRestorer(*request)
		if err != nil {
			return newResponsesConvertRequestError(err)
		}
		if normalizeResult.ChangedCount > 0 {
			*request = normalizedRequest
			info.ResponsesToolNameRestorer = restorer
		}
	}

	err = helper.ModelMappedHelper(c, info, request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}
	applyResponsesCompactSummaryModelOverride(c, info, request, hasOpaqueEncryptedContent)
	if !hasOpaqueEncryptedContent {
		requestWithLogicalEffort, applied := relaycommon.ApplyResponsesLogicalModelReasoningEffort(info, *request)
		if applied {
			request = &requestWithLogicalEffort
			info.ReasoningEffort = request.Reasoning.Effort
		}
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)
	passThroughGlobal := model_setting.GlobalSettingsSnapshot().PassThroughRequestEnabled
	compatibilityRequest, contextCompactionResult, err := prepareResponsesRequestForChatCompatibility(info, request, relaycommon.GinRequestContext(c))
	if err != nil {
		return newResponsesConvertRequestError(err)
	}
	conversionRule, err := findResponsesViaChatRule(relaycommon.GinRequestContext(c), info, passThroughGlobal, compatibilityRequest)
	if err != nil {
		return newResponsesConvertRequestError(err)
	}
	if conversionRule != nil {
		if contextCompactionResult.ChangedCount() > 0 {
			common.SysLog(fmt.Sprintf(
				"responses context compaction normalized before chat compatibility: channel_id=%d model=%s converted_items=%d dropped_markers=%d remaining_items=%d",
				info.ChannelId,
				info.OriginModelName,
				contextCompactionResult.ConvertedCount,
				contextCompactionResult.DroppedCount,
				contextCompactionResult.RemainingCount,
			))
		}
		usage, newApiErr := responsesViaChat(c, info, adaptor, compatibilityRequest, responsesViaChatOptionsFromRule(conversionRule))
		if newApiErr != nil {
			return newApiErr
		}

		if settleErr := settleOpenAIResponsesUsage(c, info, usage); settleErr != nil {
			return settleErr
		}
		return nil
	}

	usageDto, newAPIError := executeOpenAIResponsesRequest(c, info, adaptor, request, passThroughGlobal)
	if newAPIError != nil {
		return newAPIError
	}

	if info.RelayMode == relayconstant.RelayModeResponsesCompact {
		return settleOpenAIResponsesUsage(c, info, usageDto)
	}

	if settleErr := settleOpenAIResponsesUsage(c, info, usageDto); settleErr != nil {
		return settleErr
	}
	return nil
}

func prepareResponsesRequestForChatCompatibility(info *relaycommon.RelayInfo, request *dto.OpenAIResponsesRequest, contexts ...context.Context) (*dto.OpenAIResponsesRequest, relaycommon.ResponsesContextCompactionNormalizeResult, error) {
	result := relaycommon.ResponsesContextCompactionNormalizeResult{}
	if request == nil || !relaycommon.IsOpenAICompatibleResponses(info) {
		return request, result, nil
	}
	ctx := context.Background()
	if len(contexts) > 0 && contexts[0] != nil {
		ctx = contexts[0]
	}
	normalizedRequest, result, err := service.NormalizeContextCompactionForResponsesRequest(ctx, *request)
	if err != nil || result.ChangedCount() == 0 {
		return request, result, err
	}
	return &normalizedRequest, result, nil
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

func settleOpenAIResponsesUsage(c *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage) *types.NewAPIError {
	if info.RelayMode == relayconstant.RelayModeResponsesCompact {
		return settleResponsesCompactUsage(c, info, usage, func(consumedUsage *dto.Usage) {
			service.PostTextConsumeQuota(c, info, consumedUsage, nil)
		})
	}

	if strings.HasPrefix(info.OriginModelName, "gpt-4o-audio") {
		service.PostAudioConsumeQuota(c, info, usage, "")
	} else {
		service.PostTextConsumeQuota(c, info, usage, nil)
	}
	return nil
}

func settleResponsesCompactUsage(c *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage, consume func(*dto.Usage)) *types.NewAPIError {
	originModelName := info.OriginModelName
	originPriceData := info.PriceData
	defer func() {
		info.OriginModelName = originModelName
		info.PriceData = originPriceData
	}()

	normalizeResponsesCompactUsage(info, usage)
	if _, err := helper.ModelPriceHelper(c, info, info.GetEstimatePromptTokens(), &types.TokenCountMeta{}); err != nil {
		return types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithSkipRetry(), types.ErrOptionWithStatusCode(http.StatusBadRequest))
	}
	consume(usage)
	return nil
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
	validationRequest := *request
	if relaycommon.IsOpenAICompatibleResponses(info) {
		normalizedRequest, _, err := service.NormalizeContextCompactionForResponsesRequest(relaycommon.GinRequestContext(c), validationRequest)
		if err != nil {
			return nil, newResponsesConvertRequestError(err)
		}
		validationRequest = normalizedRequest
	}
	if _, err := service.HasRemoteResponsesCompactionInput(c.Request.Context(), validationRequest); err != nil {
		return nil, newResponsesConvertRequestError(err)
	}
	retryState := responsesCompatibilityRetryState{}
	var requestBody io.Reader
	var convertedRequest dto.OpenAIResponsesRequest
	if shouldPreemptivelyReduceCodexLocalCompactionInboundRequest(c, info, *request) {
		originalRequest := *request
		originalRequestBytes := common.GetContextKeyInt64(c, appconstant.ContextKeyRequestBodySize)
		var preemptiveErr *types.NewAPIError
		requestBody, convertedRequest, retryState, preemptiveErr = startCodexLocalCompactionPreemptiveBodyReduction(
			c,
			info,
			adaptor,
			originalRequest,
			originalRequestBytes,
		)
		if preemptiveErr != nil {
			return nil, preemptiveErr
		}
	} else {
		var buildErr *types.NewAPIError
		requestBody, convertedRequest, buildErr = buildOpenAIResponsesRequestBody(c, info, adaptor, request, passThroughGlobal)
		if buildErr != nil {
			return nil, buildErr
		}
		if shouldPreemptivelyReduceCodexLocalCompactionRequest(c, info, convertedRequest) {
			originalRequest := convertedRequest
			originalRequestBytes := info.UpstreamRequestBodySize
			currentBodyMetadata := snapshotResponsesRequestBodyMetadata(info)
			recoveryBody, recoveredRequest, recoveryState, preemptiveErr := startCodexLocalCompactionPreemptiveBodyReduction(
				c,
				info,
				adaptor,
				originalRequest,
				originalRequestBytes,
			)
			if preemptiveErr != nil {
				restoreResponsesRequestBodyMetadata(info, currentBodyMetadata)
				closeManagedResponsesRequestBody(requestBody)
				return nil, preemptiveErr
			}
			closeManagedResponsesRequestBody(requestBody)
			requestBody = recoveryBody
			convertedRequest = recoveredRequest
			retryState = recoveryState
		}
	}
	defer func() { closeManagedResponsesRequestBody(requestBody) }()
	for {
		usage, newAPIError := doOpenAIResponsesRequest(c, info, adaptor, requestBody, doOpenAIResponsesRequestOptions{
			SkipStatusCodeMapping: true,
		})
		if newAPIError == nil {
			return usage, nil
		}
		if retryState.codexLocalCompaction413Attempted && isUpstreamRequestTooLargeError(newAPIError) {
			if retryState.codexLocalCompaction413Stage == service.CodexLocalCompaction413RecoveryStageRichVisible {
				if c != nil && c.Writer != nil && c.Writer.Written() {
					resetResponsesStatusCode(c, newAPIError)
					return nil, newAPIError
				}
				currentBodyMetadata := snapshotResponsesRequestBodyMetadata(info)
				recoveryBody, recoveredRequest, recoveryInfo, recoveryErr := buildCodexLocalCompaction413RecoveryBody(
					c,
					info,
					adaptor,
					retryState.codexLocalCompaction413OriginalRequest,
					service.CodexLocalCompaction413RecoveryStageBoundedVisible,
				)
				recoveryInfo.OriginalRequestBytes = retryState.codexLocalCompaction413Info.OriginalRequestBytes
				if recoveryErr == nil {
					if retryState.codexLocalCompaction413Preemptive {
						markCodexLocalCompactionPreemptiveBodyReductionStarted(c, info, recoveryInfo)
					} else {
						markCodexLocalCompaction413RecoveryStarted(c, info, recoveryInfo)
					}
					closeManagedResponsesRequestBody(requestBody)
					requestBody = recoveryBody
					convertedRequest = recoveredRequest
					retryState.codexLocalCompaction413Info = recoveryInfo
					retryState.codexLocalCompaction413Stage = recoveryInfo.Stage
					retryState.attempts++
					if info.StreamStatus != nil {
						info.StreamStatus = relaycommon.NewStreamStatus()
					}
					continue
				}
				restoreResponsesRequestBodyMetadata(info, currentBodyMetadata)
				common.SysLog(fmt.Sprintf(
					"codex local compaction 413 bounded recovery unavailable: channel_id=%d model=%s original_input_bytes=%d recovery_input_bytes=%d recovery_request_bytes=%d reason=%s",
					info.ChannelId,
					info.OriginModelName,
					recoveryInfo.OriginalInputBytes,
					recoveryInfo.RecoveredInputBytes,
					recoveryInfo.RecoveredRequestBytes,
					recoveryErr.Error(),
				))
			}
			return nil, finishCodexLocalCompaction413Recovery(c, info, newAPIError, retryState.codexLocalCompaction413Info)
		}
		if shouldRecoverCodexLocalCompactionAfter413(c, info, convertedRequest, newAPIError) {
			originalRequest := convertedRequest
			originalRequestBytes := info.UpstreamRequestBodySize
			currentBodyMetadata := snapshotResponsesRequestBodyMetadata(info)
			markCodexLocalCompactionBodyReductionSource(c, codexLocalCompactionBodyReductionSourceUpstream413)
			recoveryBody, recoveredRequest, recoveryInfo, recoveryErr := buildCodexLocalCompaction413RecoveryBody(
				c,
				info,
				adaptor,
				originalRequest,
				service.CodexLocalCompaction413RecoveryStageRichVisible,
			)
			recoveryInfo.OriginalRequestBytes = originalRequestBytes
			if recoveryErr != nil {
				restoreResponsesRequestBodyMetadata(info, currentBodyMetadata)
				common.SysLog(fmt.Sprintf(
					"codex local compaction 413 recovery unavailable: channel_id=%d model=%s original_request_bytes=%d original_input_bytes=%d reason=%s",
					info.ChannelId,
					info.OriginModelName,
					originalRequestBytes,
					len(convertedRequest.Input),
					recoveryErr.Error(),
				))
				return nil, finishCodexLocalCompaction413Recovery(c, info, newAPIError, recoveryInfo)
			}
			markCodexLocalCompaction413RecoveryStarted(c, info, recoveryInfo)
			closeManagedResponsesRequestBody(requestBody)
			requestBody = recoveryBody
			convertedRequest = recoveredRequest
			retryState.codexLocalCompaction413Attempted = true
			retryState.codexLocalCompaction413Info = recoveryInfo
			retryState.codexLocalCompaction413Stage = recoveryInfo.Stage
			retryState.codexLocalCompaction413OriginalRequest = originalRequest
			retryState.codexLocalCompaction413Preemptive = false
			if info.StreamStatus != nil {
				info.StreamStatus = relaycommon.NewStreamStatus()
			}
			continue
		}
		if c.Writer.Written() && !common.GetContextKeyBool(c, appconstant.ContextKeyResponsesStreamPreludeRetrySafe) {
			markResponsesEncryptedContextRetryBlockedAfterStreamEvent(c, info, newAPIError)
			resetResponsesStatusCode(c, newAPIError)
			return nil, newAPIError
		}
		if retryState.attempts >= maxResponsesCompatibilityRetries {
			resetResponsesStatusCode(c, newAPIError)
			return nil, newAPIError
		}

		retryBody, retryRequest, retried, retryErr := buildOpenAIResponsesCompatibilityRetry(c, info, adaptor, convertedRequest, newAPIError, &retryState)
		if retryErr != nil {
			return nil, retryErr
		}
		if !retried {
			resetResponsesStatusCode(c, newAPIError)
			return nil, newAPIError
		}
		closeManagedResponsesRequestBody(requestBody)
		requestBody = retryBody
		convertedRequest = retryRequest
		retryState.attempts++
		if info.StreamStatus != nil {
			info.StreamStatus = relaycommon.NewStreamStatus()
		}
	}
}

// executeOpaqueOpenAIResponsesRequest forwards an opaque disk-backed body once.
// It intentionally does not enter compatibility conversion or retry handling.
func executeOpaqueOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, adaptor relaychannel.Adaptor, request *dto.OpaqueOpenAIResponsesRequest) (*dto.Usage, *types.NewAPIError) {
	if !hasValidatedOpaqueOpenAIResponsesReplay(c, info, request) {
		if err := validateOpaqueOpenAIResponsesReplay(c, info, request); err != nil {
			return nil, err
		}
	}
	requestBody, err := buildOpaqueOpenAIResponsesReplayBody(c, info, request)
	if err != nil {
		return nil, err
	}
	defer closeManagedResponsesRequestBody(requestBody)
	return doOpenAIResponsesRequest(c, info, adaptor, requestBody)
}

func buildCodexLocalCompaction413RecoveryBody(c *gin.Context, info *relaycommon.RelayInfo, adaptor relaychannel.Adaptor, request dto.OpenAIResponsesRequest, stage service.CodexLocalCompaction413RecoveryStage) (io.Reader, dto.OpenAIResponsesRequest, service.CodexLocalCompaction413RecoveryInfo, error) {
	recoveredRequest, recoveryInfo, err := service.BuildCodexLocalCompaction413RecoveryRequestForStage(request, stage)
	if err != nil {
		return nil, dto.OpenAIResponsesRequest{}, recoveryInfo, err
	}
	recoveryBody, convertedRequest, recoveryBuildErr := buildOpenAIResponsesRequestBody(c, info, adaptor, &recoveredRequest, false, buildResponsesRequestBodyOptions{
		ForceConvertedBody: true,
	})
	if recoveryBuildErr != nil {
		return nil, dto.OpenAIResponsesRequest{}, recoveryInfo, recoveryBuildErr
	}

	// A channel adaptor may add fields while converting the recovery request.
	// Check the final serialized body, not only the pre-conversion request, so
	// the rich stage cannot send another body that the upstream will reject.
	if stage == service.CodexLocalCompaction413RecoveryStageRichVisible &&
		info != nil && info.UpstreamRequestBodySize > int64(service.CodexLocalCompaction413RecoverySafeBodyMaxBytes) {
		common.SysLog(fmt.Sprintf(
			"codex local compaction 413 rich recovery body exceeded safe limit: channel_id=%d model=%s recovery_request_bytes=%d safe_body_bytes=%d; switching to bounded stage",
			info.ChannelId,
			info.OriginModelName,
			info.UpstreamRequestBodySize,
			service.CodexLocalCompaction413RecoverySafeBodyMaxBytes,
		))
		closeManagedResponsesRequestBody(recoveryBody)
		return buildCodexLocalCompaction413RecoveryBody(
			c,
			info,
			adaptor,
			request,
			service.CodexLocalCompaction413RecoveryStageBoundedVisible,
		)
	}
	if info != nil {
		recoveryInfo.RecoveredRequestBytes = int(info.UpstreamRequestBodySize)
		if info.UpstreamRequestBodySize > int64(service.CodexLocalCompaction413RecoverySafeBodyMaxBytes) {
			closeManagedResponsesRequestBody(recoveryBody)
			return nil, dto.OpenAIResponsesRequest{}, recoveryInfo, fmt.Errorf(
				"codex local compaction recovery body exceeds safe limit: request_bytes=%d safe_body_bytes=%d",
				info.UpstreamRequestBodySize,
				service.CodexLocalCompaction413RecoverySafeBodyMaxBytes,
			)
		}
	}
	return recoveryBody, convertedRequest, recoveryInfo, nil
}

func markCodexLocalCompaction413RecoveryStarted(c *gin.Context, info *relaycommon.RelayInfo, recoveryInfo service.CodexLocalCompaction413RecoveryInfo) {
	common.SetContextKey(c, appconstant.ContextKeyResponsesCodexLocalCompaction413Recovery, true)
	markCodexLocalCompactionBodyReductionMetrics(c, recoveryInfo)
	common.SysLog(fmt.Sprintf(
		"codex local compaction 413 recovery started: channel_id=%d model=%s stage=%s original_request_bytes=%d original_input_bytes=%d recovery_input_bytes=%d recovery_request_bytes=%d visible_parts=%d instructions_preserved=%t visible_input_truncated=%t embedded_media_omitted=%t tool_payload_omitted=%t",
		info.ChannelId,
		info.OriginModelName,
		recoveryInfo.Stage,
		recoveryInfo.OriginalRequestBytes,
		recoveryInfo.OriginalInputBytes,
		recoveryInfo.RecoveredInputBytes,
		recoveryInfo.RecoveredRequestBytes,
		recoveryInfo.VisiblePartCount,
		recoveryInfo.InstructionsPreserved,
		recoveryInfo.VisibleInputTruncated,
		recoveryInfo.EmbeddedMediaOmitted,
		recoveryInfo.ToolPayloadOmitted,
	))
}

const (
	codexLocalCompactionBodyReductionSourceUpstream413    = "upstream_413"
	codexLocalCompactionBodyReductionSourcePreemptiveSize = "preemptive_size"
)

func shouldPreemptivelyReduceCodexLocalCompactionRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) bool {
	if info == nil {
		return false
	}
	return shouldPreemptivelyReduceCodexLocalCompactionForBodySize(c, info, request, info.UpstreamRequestBodySize)
}

func shouldPreemptivelyReduceCodexLocalCompactionInboundRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) bool {
	return shouldPreemptivelyReduceCodexLocalCompactionForBodySize(
		c,
		info,
		request,
		common.GetContextKeyInt64(c, appconstant.ContextKeyRequestBodySize),
	)
}

func shouldPreemptivelyReduceCodexLocalCompactionForBodySize(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest, bodySize int64) bool {
	if c == nil || info == nil || info.ChannelMeta == nil ||
		bodySize <= int64(service.CodexLocalCompaction413RecoverySafeBodyMaxBytes) ||
		info.RelayMode != relayconstant.RelayModeResponses || !info.IsStream ||
		!service.IsCodexLocalResponsesCompaction(c) || request.HasCompactionTrigger() ||
		!request.IsStream(c) || c.Writer.Written() {
		return false
	}
	hasOpaqueEncryptedContent, err := service.ResponsesInputHasOpaqueEncryptedContent(relaycommon.GinRequestContext(c), request.Input)
	if err != nil || hasOpaqueEncryptedContent {
		return false
	}
	return relaycommon.IsOpenAICompatibleResponses(info) || info.ChannelType == appconstant.ChannelTypeCodex
}

func isUpstreamRequestTooLargeError(err *types.NewAPIError) bool {
	return err != nil && err.StatusCode == http.StatusRequestEntityTooLarge &&
		err.GetErrorCode() == types.ErrorCodeUpstreamRequestTooLarge
}

func markCodexLocalCompactionBodyReductionSource(c *gin.Context, source string) {
	if source == "" {
		return
	}
	common.SetContextKey(c, appconstant.ContextKeyResponsesCodexLocalCompactionBodyReductionSource, source)
}

func markCodexLocalCompactionPreemptiveBodyReductionStarted(c *gin.Context, info *relaycommon.RelayInfo, recoveryInfo service.CodexLocalCompaction413RecoveryInfo) {
	markCodexLocalCompactionBodyReductionSource(c, codexLocalCompactionBodyReductionSourcePreemptiveSize)
	markCodexLocalCompactionBodyReductionMetrics(c, recoveryInfo)
	common.SysLog(fmt.Sprintf(
		"codex local compaction preemptive body reduction started: channel_id=%d model=%s stage=%s original_request_bytes=%d original_input_bytes=%d recovery_input_bytes=%d recovery_request_bytes=%d visible_parts=%d instructions_preserved=%t visible_input_truncated=%t embedded_media_omitted=%t tool_payload_omitted=%t",
		info.ChannelId,
		info.OriginModelName,
		recoveryInfo.Stage,
		recoveryInfo.OriginalRequestBytes,
		recoveryInfo.OriginalInputBytes,
		recoveryInfo.RecoveredInputBytes,
		recoveryInfo.RecoveredRequestBytes,
		recoveryInfo.VisiblePartCount,
		recoveryInfo.InstructionsPreserved,
		recoveryInfo.VisibleInputTruncated,
		recoveryInfo.EmbeddedMediaOmitted,
		recoveryInfo.ToolPayloadOmitted,
	))
}

func markCodexLocalCompactionBodyReductionMetrics(c *gin.Context, recoveryInfo service.CodexLocalCompaction413RecoveryInfo) {
	if recoveryInfo.OriginalRequestBytes > 0 {
		common.SetContextKey(c, appconstant.ContextKeyResponsesCodexLocalCompactionOriginalBodyBytes, recoveryInfo.OriginalRequestBytes)
	}
	if recoveryInfo.RecoveredRequestBytes > 0 {
		common.SetContextKey(c, appconstant.ContextKeyResponsesCodexLocalCompactionReducedBodyBytes, int64(recoveryInfo.RecoveredRequestBytes))
	}
}

func newCodexLocalCompactionPreemptiveBodyReductionError(originalRequestBytes int64, err error) *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		fmt.Errorf(
			"codex local compaction request body exceeds safe upstream limit (%d bytes) and could not be reduced: %w",
			originalRequestBytes,
			err,
		),
		types.ErrorCodeUpstreamRequestTooLarge,
		http.StatusRequestEntityTooLarge,
		types.ErrOptionWithSkipRetry(),
	)
}

func markResponsesEncryptedContextRetryBlockedAfterStreamEvent(c *gin.Context, info *relaycommon.RelayInfo, newAPIError *types.NewAPIError) {
	if !shouldRetryResponsesWithoutEncryptedReasoning(info, newAPIError) {
		return
	}
	eventType := common.GetContextKeyString(c, appconstant.ContextKeyResponsesStreamFirstCommittedEvent)
	if eventType == "" {
		eventType = "unknown"
	}
	common.SetContextKey(c, appconstant.ContextKeyResponsesEncryptedContextRetryBlockedAfterEvent, eventType)
	common.SysLog(fmt.Sprintf(
		"responses encrypted context retry blocked after committed stream event: channel_id=%d model=%s event_type=%s original_error=%s",
		info.ChannelId,
		info.OriginModelName,
		eventType,
		newAPIError.MaskSensitiveError(),
	))
}

func shouldRecoverCodexLocalCompactionAfter413(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest, err *types.NewAPIError) bool {
	if c == nil || info == nil || info.ChannelMeta == nil || err == nil ||
		info.RelayMode != relayconstant.RelayModeResponses || !info.IsStream ||
		!service.IsCodexLocalResponsesCompaction(c) || request.HasCompactionTrigger() ||
		!request.IsStream(c) || c.Writer.Written() {
		return false
	}
	hasOpaqueEncryptedContent, parseErr := service.ResponsesInputHasOpaqueEncryptedContent(relaycommon.GinRequestContext(c), request.Input)
	if parseErr != nil || hasOpaqueEncryptedContent {
		return false
	}
	if !relaycommon.IsOpenAICompatibleResponses(info) && info.ChannelType != appconstant.ChannelTypeCodex {
		return false
	}
	return isUpstreamRequestTooLargeError(err)
}

func finishCodexLocalCompaction413Recovery(c *gin.Context, info *relaycommon.RelayInfo, err *types.NewAPIError, recoveryInfo service.CodexLocalCompaction413RecoveryInfo) *types.NewAPIError {
	if err == nil {
		return nil
	}
	common.SetContextKey(c, appconstant.ContextKeyResponsesCodexLocalCompaction413Recovery, true)
	common.SetContextKey(c, appconstant.ContextKeyResponsesCodexLocalCompaction413RecoveryFailed, true)
	common.SysLog(fmt.Sprintf(
		"codex local compaction 413 recovery failed: channel_id=%d model=%s stage=%s original_request_bytes=%d original_input_bytes=%d recovery_input_bytes=%d recovery_request_bytes=%d status_code=%d error=%s",
		info.ChannelId,
		info.OriginModelName,
		recoveryInfo.Stage,
		recoveryInfo.OriginalRequestBytes,
		recoveryInfo.OriginalInputBytes,
		recoveryInfo.RecoveredInputBytes,
		recoveryInfo.RecoveredRequestBytes,
		err.StatusCode,
		err.MaskSensitiveError(),
	))
	resetResponsesStatusCode(c, err)
	return types.NewError(err, err.GetErrorCode(), types.ErrOptionWithSkipRetry())
}

type responsesCompatibilityRetryState struct {
	attempts                               int
	encryptedContentParameterAttempted     bool
	streamOptionsIncludeUsageAttempted     bool
	argumentsObjectAttempted               bool
	argumentsStringAttempted               bool
	codexLocalCompaction413Attempted       bool
	codexLocalCompaction413Info            service.CodexLocalCompaction413RecoveryInfo
	codexLocalCompaction413Stage           service.CodexLocalCompaction413RecoveryStage
	codexLocalCompaction413OriginalRequest dto.OpenAIResponsesRequest
	codexLocalCompaction413Preemptive      bool
}

func startCodexLocalCompactionPreemptiveBodyReduction(c *gin.Context, info *relaycommon.RelayInfo, adaptor relaychannel.Adaptor, originalRequest dto.OpenAIResponsesRequest, originalRequestBytes int64) (io.Reader, dto.OpenAIResponsesRequest, responsesCompatibilityRetryState, *types.NewAPIError) {
	recoveryBody, recoveredRequest, recoveryInfo, recoveryErr := buildCodexLocalCompaction413RecoveryBody(
		c,
		info,
		adaptor,
		originalRequest,
		service.CodexLocalCompaction413RecoveryStageRichVisible,
	)
	if recoveryErr != nil {
		common.SysLog(fmt.Sprintf(
			"codex local compaction preemptive body reduction unavailable: channel_id=%d model=%s original_request_bytes=%d safe_body_bytes=%d reason=%s",
			info.ChannelId,
			info.OriginModelName,
			originalRequestBytes,
			service.CodexLocalCompaction413RecoverySafeBodyMaxBytes,
			recoveryErr.Error(),
		))
		return nil, dto.OpenAIResponsesRequest{}, responsesCompatibilityRetryState{}, newCodexLocalCompactionPreemptiveBodyReductionError(originalRequestBytes, recoveryErr)
	}
	recoveryInfo.OriginalRequestBytes = originalRequestBytes
	markCodexLocalCompactionPreemptiveBodyReductionStarted(c, info, recoveryInfo)
	return recoveryBody, recoveredRequest, responsesCompatibilityRetryState{
		codexLocalCompaction413Attempted:       true,
		codexLocalCompaction413Info:            recoveryInfo,
		codexLocalCompaction413Stage:           recoveryInfo.Stage,
		codexLocalCompaction413OriginalRequest: originalRequest,
		codexLocalCompaction413Preemptive:      true,
	}, nil
}

func buildOpenAIResponsesCompatibilityRetry(c *gin.Context, info *relaycommon.RelayInfo, adaptor relaychannel.Adaptor, convertedRequest dto.OpenAIResponsesRequest, newAPIError *types.NewAPIError, retryState *responsesCompatibilityRetryState) (io.Reader, dto.OpenAIResponsesRequest, bool, *types.NewAPIError) {
	hasOpaqueEncryptedContent, err := service.ResponsesInputHasOpaqueEncryptedContent(relaycommon.GinRequestContext(c), convertedRequest.Input)
	if err != nil {
		return nil, convertedRequest, false, newResponsesConvertRequestError(err)
	}
	if hasOpaqueEncryptedContent {
		return nil, convertedRequest, false, nil
	}
	if shouldRetryResponsesWithoutEncryptedReasoning(info, newAPIError) {
		if isResponsesEncryptedContentParameterError(newAPIError.ToOpenAIError()) {
			if retryState.encryptedContentParameterAttempted {
				return nil, convertedRequest, false, nil
			}
			retryState.encryptedContentParameterAttempted = true
		}
		return buildResponsesWithoutEncryptedReasoningRetry(c, info, adaptor, convertedRequest, newAPIError)
	}
	if shouldRetryResponsesWithoutStreamOptionsIncludeUsage(info, newAPIError) &&
		!retryState.streamOptionsIncludeUsageAttempted {
		retryState.streamOptionsIncludeUsageAttempted = true
		return buildResponsesWithoutStreamOptionsIncludeUsageRetry(c, info, adaptor, convertedRequest, newAPIError)
	}
	if shouldRetryResponsesWithoutNamespaceHistory(info, newAPIError) {
		return buildResponsesWithoutNamespaceHistoryRetry(c, info, adaptor, convertedRequest, newAPIError)
	}
	if shouldRetryResponsesWithObjectArguments(info, newAPIError) && !retryState.argumentsObjectAttempted {
		retryState.argumentsObjectAttempted = true
		return buildResponsesWithObjectArgumentsRetry(c, info, adaptor, convertedRequest, newAPIError)
	}
	if shouldRetryResponsesWithStringArguments(info, newAPIError) && !retryState.argumentsStringAttempted {
		retryState.argumentsStringAttempted = true
		return buildResponsesWithStringArgumentsRetry(c, info, adaptor, convertedRequest, newAPIError)
	}
	return nil, convertedRequest, false, nil
}

func buildResponsesWithoutEncryptedReasoningRetry(c *gin.Context, info *relaycommon.RelayInfo, adaptor relaychannel.Adaptor, convertedRequest dto.OpenAIResponsesRequest, newAPIError *types.NewAPIError) (io.Reader, dto.OpenAIResponsesRequest, bool, *types.NewAPIError) {
	openAIError := newAPIError.ToOpenAIError()
	removedEncryptedReasoning := 0
	remainingItems := 0
	removedTarget := false
	retryMode := "generic_reasoning"
	var strippedRequest dto.OpenAIResponsesRequest
	var stripErr error
	if isResponsesEncryptedContentParameterError(openAIError) {
		retryMode = "targeted_item"
		strippedRequest, removedTarget, stripErr = relaycommon.StripResponsesInputEncryptedContentItem(convertedRequest, openAIError.Param, openAIError.Message)
		if stripErr != nil {
			return nil, convertedRequest, false, nil
		}
		if !removedTarget {
			target, validToolOutput, targetErr := relaycommon.ValidResponsesToolOutputEncryptedContentTarget(convertedRequest, openAIError.Param, openAIError.Message)
			if targetErr != nil || !validToolOutput {
				return nil, convertedRequest, false, nil
			}
			strippedRequest = convertedRequest
			common.SysLog(fmt.Sprintf(
				"responses valid tool output replay: channel_id=%d model=%s input_index=%d item_type=%s original_error=%s",
				info.ChannelId,
				info.OriginModelName,
				target.InputIndex,
				target.ItemType,
				newAPIError.MaskSensitiveError(),
			))
		}
	} else {
		var result relaycommon.ResponsesInputStripResult
		strippedRequest, result, stripErr = relaycommon.StripEncryptedReasoningFromResponsesRequest(convertedRequest)
		if stripErr != nil || result.RemovedCount() == 0 {
			return nil, convertedRequest, false, nil
		}
		removedEncryptedReasoning = result.EncryptedReasoningCount
		remainingItems = result.RemainingCount
	}
	common.SetContextKey(c, appconstant.ContextKeyResponsesEncryptedContextRetry, true)
	common.SysLog(fmt.Sprintf(
		"responses encrypted context retry: channel_id=%d model=%s mode=%s targeted_item=%t encrypted_reasoning_items=%d remaining_items=%d original_error=%s",
		info.ChannelId,
		info.OriginModelName,
		retryMode,
		removedTarget,
		removedEncryptedReasoning,
		remainingItems,
		newAPIError.MaskSensitiveError(),
	))
	retryBody, _, retryBuildErr := buildOpenAIResponsesRequestBody(c, info, adaptor, &strippedRequest, false, buildResponsesRequestBodyOptions{
		ForceConvertedBody: true,
	})
	if retryBuildErr != nil {
		return nil, convertedRequest, false, retryBuildErr
	}
	return retryBody, strippedRequest, true, nil
}

func shouldRetryResponsesWithoutNamespaceHistory(info *relaycommon.RelayInfo, err *types.NewAPIError) bool {
	if info == nil || err == nil || !relaycommon.IsOpenAICompatibleResponses(info) || err.StatusCode != http.StatusBadRequest {
		return false
	}
	openAIError := err.ToOpenAIError()
	if _, ok := relaycommon.ResponsesNamespaceHistoryIndexFromError(openAIError.Param, openAIError.Message); !ok {
		return false
	}
	errorCode := strings.ToLower(fmt.Sprintf("%v", openAIError.Code))
	if errorCode == "unknown_parameter" {
		return true
	}
	errorMessage := strings.ToLower(openAIError.Message)
	return strings.Contains(errorMessage, "unknown parameter")
}

func buildResponsesWithoutNamespaceHistoryRetry(c *gin.Context, info *relaycommon.RelayInfo, adaptor relaychannel.Adaptor, convertedRequest dto.OpenAIResponsesRequest, newAPIError *types.NewAPIError) (io.Reader, dto.OpenAIResponsesRequest, bool, *types.NewAPIError) {
	openAIError := newAPIError.ToOpenAIError()
	itemIndex, ok := relaycommon.ResponsesNamespaceHistoryIndexFromError(openAIError.Param, openAIError.Message)
	if !ok {
		return nil, convertedRequest, false, nil
	}
	strippedRequest, removed, stripErr := relaycommon.StripNamespaceFromResponsesInputHistory(convertedRequest, itemIndex)
	if stripErr != nil || !removed {
		return nil, convertedRequest, false, nil
	}
	common.SetContextKey(c, appconstant.ContextKeyResponsesNamespaceHistoryRetry, true)
	common.SysLog(fmt.Sprintf(
		"responses namespace history retry: channel_id=%d model=%s input_index=%d original_error=%s",
		info.ChannelId,
		info.OriginModelName,
		itemIndex,
		newAPIError.MaskSensitiveError(),
	))
	retryBody, _, retryBuildErr := buildOpenAIResponsesRequestBody(c, info, adaptor, &strippedRequest, false, buildResponsesRequestBodyOptions{
		ForceConvertedBody: true,
	})
	if retryBuildErr != nil {
		return nil, convertedRequest, false, retryBuildErr
	}
	return retryBody, strippedRequest, true, nil
}

func shouldRetryResponsesWithObjectArguments(info *relaycommon.RelayInfo, err *types.NewAPIError) bool {
	if info == nil || err == nil || !relaycommon.IsOpenAICompatibleResponses(info) || err.StatusCode != http.StatusBadRequest {
		return false
	}
	openAIError := err.ToOpenAIError()
	if _, ok := relaycommon.ResponsesArgumentsHistoryIndexFromError(openAIError.Param, openAIError.Message); !ok {
		return false
	}
	errorMessage := strings.ToLower(openAIError.Message)
	return strings.Contains(errorMessage, "expected an object") && strings.Contains(errorMessage, "got a string")
}

func shouldRetryResponsesWithStringArguments(info *relaycommon.RelayInfo, err *types.NewAPIError) bool {
	if info == nil || err == nil || !relaycommon.IsOpenAICompatibleResponses(info) || err.StatusCode != http.StatusBadRequest {
		return false
	}
	openAIError := err.ToOpenAIError()
	if _, ok := relaycommon.ResponsesArgumentsHistoryIndexFromError(openAIError.Param, openAIError.Message); !ok {
		return false
	}
	errorMessage := strings.ToLower(openAIError.Message)
	return strings.Contains(errorMessage, "expected a string") && strings.Contains(errorMessage, "got an object")
}

func buildResponsesWithObjectArgumentsRetry(c *gin.Context, info *relaycommon.RelayInfo, adaptor relaychannel.Adaptor, convertedRequest dto.OpenAIResponsesRequest, newAPIError *types.NewAPIError) (io.Reader, dto.OpenAIResponsesRequest, bool, *types.NewAPIError) {
	openAIError := newAPIError.ToOpenAIError()
	itemIndex, ok := relaycommon.ResponsesArgumentsHistoryIndexFromError(openAIError.Param, openAIError.Message)
	if !ok {
		return nil, convertedRequest, false, nil
	}
	normalizedRequest, result, normalizeErr := relaycommon.NormalizeStringObjectArgumentsFromResponsesInputHistory(convertedRequest, itemIndex)
	if normalizeErr != nil || !result.TargetNormalized {
		return nil, convertedRequest, false, nil
	}
	common.SetContextKey(c, appconstant.ContextKeyResponsesArgumentsHistoryRetry, true)
	common.SysLog(fmt.Sprintf(
		"responses arguments history retry: channel_id=%d model=%s input_index=%d normalized_items=%d original_error=%s",
		info.ChannelId,
		info.OriginModelName,
		itemIndex,
		result.NormalizedCount,
		newAPIError.MaskSensitiveError(),
	))
	retryBody, _, retryBuildErr := buildOpenAIResponsesRequestBody(c, info, adaptor, &normalizedRequest, false, buildResponsesRequestBodyOptions{
		ForceConvertedBody: true,
	})
	if retryBuildErr != nil {
		return nil, convertedRequest, false, retryBuildErr
	}
	return retryBody, normalizedRequest, true, nil
}

func buildResponsesWithStringArgumentsRetry(c *gin.Context, info *relaycommon.RelayInfo, adaptor relaychannel.Adaptor, convertedRequest dto.OpenAIResponsesRequest, newAPIError *types.NewAPIError) (io.Reader, dto.OpenAIResponsesRequest, bool, *types.NewAPIError) {
	openAIError := newAPIError.ToOpenAIError()
	itemIndex, ok := relaycommon.ResponsesArgumentsHistoryIndexFromError(openAIError.Param, openAIError.Message)
	if !ok {
		return nil, convertedRequest, false, nil
	}
	stringifiedRequest, result, stringifyErr := relaycommon.StringifyObjectArgumentsFromResponsesInputHistory(convertedRequest, itemIndex)
	if stringifyErr != nil || !result.TargetNormalized {
		return nil, convertedRequest, false, nil
	}
	common.SetContextKey(c, appconstant.ContextKeyResponsesArgumentsHistoryRetry, true)
	common.SysLog(fmt.Sprintf(
		"responses arguments history string retry: channel_id=%d model=%s input_index=%d stringified_items=%d original_error=%s",
		info.ChannelId,
		info.OriginModelName,
		itemIndex,
		result.NormalizedCount,
		newAPIError.MaskSensitiveError(),
	))
	retryBody, _, retryBuildErr := buildOpenAIResponsesRequestBody(c, info, adaptor, &stringifiedRequest, false, buildResponsesRequestBodyOptions{
		ForceConvertedBody: true,
	})
	if retryBuildErr != nil {
		return nil, convertedRequest, false, retryBuildErr
	}
	return retryBody, stringifiedRequest, true, nil
}

type doOpenAIResponsesRequestOptions struct {
	SkipStatusCodeMapping bool
}

type buildResponsesRequestBodyOptions struct {
	ForceConvertedBody bool
}

func buildOpenAIResponsesRequestBody(c *gin.Context, info *relaycommon.RelayInfo, adaptor relaychannel.Adaptor, request *dto.OpenAIResponsesRequest, passThroughGlobal bool, options ...buildResponsesRequestBodyOptions) (io.Reader, dto.OpenAIResponsesRequest, *types.NewAPIError) {
	var requestBody io.Reader
	info.UpstreamRequestBodySize = 0
	info.UpstreamRequestGetBody = nil
	info.CodexFoldedBaseRequestBody = nil
	convertedResponsesRequest := *request
	hasOpaqueEncryptedContent, err := service.ResponsesInputHasOpaqueEncryptedContent(relaycommon.GinRequestContext(c), request.Input)
	if err != nil {
		return nil, convertedResponsesRequest, newResponsesConvertRequestError(err)
	}
	if hasOpaqueEncryptedContent {
		return buildOpaqueOpenAIResponsesRequestBody(c, info, request)
	}
	streamOptionsIncludeUsageCapabilityDisabled := relaycommon.IsOpenAICompatibleResponses(info) &&
		info.ChannelOtherSettings.ExplicitlyDisablesResponsesStreamOptionsIncludeUsage()
	streamOptionsIncludeUsageCapabilityStripped := false
	if streamOptionsIncludeUsageCapabilityDisabled {
		strippedRequest, stripped := stripResponsesStreamOptionsIncludeUsage(*request)
		if stripped {
			request = &strippedRequest
			convertedResponsesRequest = strippedRequest
			streamOptionsIncludeUsageCapabilityStripped = true
			markResponsesStreamOptionsIncludeUsageCapabilityStripped(c, info)
		}
	}
	contextCompactionNormalized := false
	if relaycommon.IsOpenAICompatibleResponses(info) {
		normalizedRequest, result, err := service.NormalizeContextCompactionForResponsesRequest(relaycommon.GinRequestContext(c), *request)
		if err != nil {
			return nil, convertedResponsesRequest, newResponsesConvertRequestError(err)
		}
		if result.ChangedCount() > 0 {
			request = &normalizedRequest
			convertedResponsesRequest = normalizedRequest
			contextCompactionNormalized = true
			common.SysLog(fmt.Sprintf(
				"responses context compaction normalized for upstream API: channel_id=%d model=%s converted_items=%d dropped_markers=%d remaining_items=%d",
				info.ChannelId,
				info.OriginModelName,
				result.ConvertedCount,
				result.DroppedCount,
				result.RemainingCount,
			))
		}
	}
	normalizedInput, normalizeResult, normalizeErr := relaycommon.NormalizeResponsesInputItemIDs(request.Input)
	if normalizeErr != nil {
		return nil, convertedResponsesRequest, newResponsesConvertRequestError(normalizeErr)
	}
	inputItemIDsNormalized := normalizeResult.RemovedCount > 0
	if inputItemIDsNormalized {
		normalizedRequest := *request
		normalizedRequest.Input = normalizedInput
		request = &normalizedRequest
		convertedResponsesRequest = normalizedRequest
		common.SetContextKey(c, appconstant.ContextKeyResponsesInputItemIDNormalizationRemovedCount, normalizeResult.RemovedCount)
		common.SysLog(fmt.Sprintf(
			"responses input item ids normalized before upstream forwarding: channel_id=%d model=%s removed_count=%d",
			info.ChannelId,
			info.OriginModelName,
			normalizeResult.RemovedCount,
		))
	}
	reasoningContextRequiresConversion := responsesReasoningContextRequiresConversion(c, info, request)
	syntheticCompactReference := false
	nativeOpaqueReference := service.IsNativeOpaqueCompactReference(request.PreviousResponseID)
	if relaycommon.IsOpenAICompatibleResponses(info) {
		var err error
		syntheticCompactReference, err = service.HasLocalSyntheticCompactReferenceWithContext(c.Request.Context(), *request)
		if err != nil {
			return nil, convertedResponsesRequest, newResponsesConvertRequestError(err)
		}
	}
	requestWithLogicalEffort, logicalEffortApplied := relaycommon.ApplyResponsesLogicalModelReasoningEffort(info, *request)
	if logicalEffortApplied {
		request = &requestWithLogicalEffort
		convertedResponsesRequest = requestWithLogicalEffort
	}
	reasoningEffortSuffix, _ := reasoning.ParseOpenAIReasoningEffortFromModelSuffix(request.Model)
	if info != nil && request.Reasoning != nil && request.Reasoning.Effort != "" {
		info.ReasoningEffort = request.Reasoning.Effort
	}
	reasoningContextStripped := false
	if request.Reasoning != nil && len(bytes.TrimSpace(request.Reasoning.Context)) > 0 {
		capabilityRequest := relaycommon.ApplyResponsesReasoningContextCapability(c, info, *request)
		request = &capabilityRequest
		convertedResponsesRequest = capabilityRequest
		reasoningContextStripped = capabilityRequest.Reasoning == nil || len(bytes.TrimSpace(capabilityRequest.Reasoning.Context)) == 0
	}
	reasoningContextRequiresConvertedBody := reasoningContextStripped || logicalEffortApplied || reasoningEffortSuffix != ""
	bodyPreservingParamOverride := relaycommon.ValidateBytePreservingParamOverride(info.ParamOverride) == nil
	responsesLiteRequest := c != nil && c.Request != nil && strings.EqualFold(strings.TrimSpace(c.Request.Header.Get("X-OpenAI-Internal-Codex-Responses-Lite")), "true")
	actualPassThroughBody := (passThroughGlobal || info.ChannelSetting.PassThroughBodyEnabled) &&
		!relaycommon.ShouldConvertResponsesRequest(info) &&
		!responsesLiteRequest &&
		!inputItemIDsNormalized &&
		!reasoningContextRequiresConversion &&
		!reasoningContextRequiresConvertedBody &&
		bodyPreservingParamOverride &&
		!streamOptionsIncludeUsageCapabilityStripped &&
		!contextCompactionNormalized &&
		!common.GetContextKeyBool(c, appconstant.ContextKeyResponsesRemoteCompactSyntheticFallback) &&
		(!syntheticCompactReference || nativeOpaqueReference)
	if len(options) > 0 && options[0].ForceConvertedBody {
		actualPassThroughBody = false
	}
	if actualPassThroughBody {
		if len(info.ParamOverride) > 0 {
			if err := relaycommon.ApplyBytePreservingHeaderParamOverrideWithRelayInfo(info); err != nil {
				return nil, convertedResponsesRequest, newAPIErrorFromParamOverride(err)
			}
		}
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
				info.UpstreamRequestBodySize = int64(len(body))
				captureCodexFoldedBaseRequestBody(info, body)
			}
		}
		if requestBody == nil {
			var passThroughErr *types.NewAPIError
			requestBody, passThroughErr = buildOpenAIResponsesPassThroughBody(c, info, *request)
			if passThroughErr != nil {
				return nil, convertedResponsesRequest, passThroughErr
			}
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

		if len(info.ParamOverride) > 0 {
			jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
			if err != nil {
				return nil, convertedResponsesRequest, newAPIErrorFromParamOverride(err)
			}
		}
		if streamOptionsIncludeUsageCapabilityDisabled {
			var stripped bool
			jsonData, stripped, err = stripResponsesStreamOptionsIncludeUsageFromJSON(jsonData)
			if err != nil {
				return nil, convertedResponsesRequest, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}
			if stripped {
				markResponsesStreamOptionsIncludeUsageCapabilityStripped(c, info)
			}
		}

		if common.DebugEnabled {
			println("requestBody: ", string(jsonData))
		}
		body, size, getBody, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
		if err != nil {
			return nil, convertedResponsesRequest, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		requestBody = &managedResponsesRequestBody{Reader: body, closer: closer}
		info.UpstreamRequestBodySize = size
		info.UpstreamRequestGetBody = getBody
		captureCodexFoldedBaseRequestBody(info, jsonData)
	}
	return requestBody, convertedResponsesRequest, nil
}

// buildOpenAIResponsesPassThroughBody preserves the client body while changing
// only the top-level model token selected by channel mapping.
func buildOpenAIResponsesPassThroughBody(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (io.Reader, *types.NewAPIError) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
	}
	modelLocation, err := common.LocateTopLevelJSONString(storage, "model")
	if err != nil {
		return nil, newResponsesConvertRequestError(fmt.Errorf("locate model for Responses pass-through: %w", err))
	}
	upstreamModel := strings.TrimSpace(request.Model)
	if upstreamModel == "" {
		return nil, newResponsesConvertRequestError(errors.New("responses pass-through model is required"))
	}
	if modelLocation.Value == upstreamModel {
		if info != nil {
			info.UpstreamRequestBodySize = storage.Size()
			info.UpstreamRequestGetBody = func() (io.ReadCloser, error) {
				return common.NewBodyStorageReader(storage)
			}
			if err := captureCodexFoldedBaseRequestBodyFromStorage(info, storage, info.UpstreamRequestGetBody); err != nil {
				return nil, types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
			}
		}
		return common.ReaderOnly(storage), nil
	}
	requestBody, err := common.NewTopLevelJSONStringReplacementReader(storage, storage.Size(), modelLocation, upstreamModel)
	if err != nil {
		return nil, newResponsesConvertRequestError(fmt.Errorf("replace model for Responses pass-through: %w", err))
	}
	if info != nil {
		info.UpstreamRequestBodySize = requestBody.ContentLength()
		info.UpstreamRequestGetBody = func() (io.ReadCloser, error) {
			reader, err := common.NewTopLevelJSONStringReplacementReader(storage, storage.Size(), modelLocation, upstreamModel)
			if err != nil {
				return nil, err
			}
			return io.NopCloser(reader), nil
		}
		if err := captureCodexFoldedBaseRequestBodyFromStorage(info, storage, info.UpstreamRequestGetBody); err != nil {
			return nil, types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
		}
	}
	return requestBody, nil
}

func captureCodexFoldedBaseRequestBody(info *relaycommon.RelayInfo, body []byte) {
	if !codexFoldedBaseRequestBodyCaptureEnabled(info) || len(body) == 0 {
		return
	}
	info.CodexFoldedBaseRequestBody = append(info.CodexFoldedBaseRequestBody[:0], body...)
}

func codexFoldedBaseRequestBodyCaptureEnabled(info *relaycommon.RelayInfo) bool {
	return info != nil && info.IsStream &&
		info.RelayMode == relayconstant.RelayModeResponses &&
		info.ChannelType == appconstant.ChannelTypeCodex &&
		info.ChannelOtherSettings.CodexFoldedSSEEnabled
}

func captureCodexFoldedBaseRequestBodyFromReader(info *relaycommon.RelayInfo, getBody func() (io.ReadCloser, error)) error {
	if !codexFoldedBaseRequestBodyCaptureEnabled(info) {
		return nil
	}
	if getBody == nil {
		return errors.New("Codex folded SSE request body replay is unavailable")
	}
	reader, err := getBody()
	if err != nil {
		return err
	}
	if reader == nil {
		return errors.New("Codex folded SSE request body replay returned a nil reader")
	}
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	captureCodexFoldedBaseRequestBody(info, body)
	return nil
}

func captureCodexFoldedBaseRequestBodyFromStorage(info *relaycommon.RelayInfo, storage common.BodyStorage, getBody func() (io.ReadCloser, error)) error {
	if !codexFoldedBaseRequestBodyCaptureEnabled(info) {
		return nil
	}
	if storage == nil {
		return errors.New("Codex folded SSE request body storage is unavailable")
	}
	if err := captureCodexFoldedBaseRequestBodyFromReader(info, getBody); err != nil {
		return err
	}
	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("reset Codex folded SSE request body storage: %w", err)
	}
	return nil
}

func responsesReasoningContextRequiresConversion(c *gin.Context, info *relaycommon.RelayInfo, request *dto.OpenAIResponsesRequest) bool {
	if request == nil || request.Reasoning == nil || len(bytes.TrimSpace(request.Reasoning.Context)) == 0 {
		return false
	}

	decision := relaycommon.ResolveResponsesReasoningContextCapabilityDecision(info, request.Model)
	if c != nil {
		common.SetContextKey(c, appconstant.ContextKeyResponsesReasoningContextAction, decision.Action)
	}
	if !relaycommon.IsOpenAICompatibleResponses(info) {
		return true
	}
	return decision.Capability == dto.ResponsesReasoningContextCapabilityUnsupported
}

func buildOpaqueOpenAIResponsesRequestBody(c *gin.Context, info *relaycommon.RelayInfo, request *dto.OpenAIResponsesRequest) (io.Reader, dto.OpenAIResponsesRequest, *types.NewAPIError) {
	convertedRequest := *request
	capability := dto.ResponsesEncryptedReasoningReplayCapabilityUnknown
	if info != nil {
		capability = info.ChannelOtherSettings.ResolveResponsesEncryptedReasoningReplayCapability(info.ChannelType)
	}
	relaycommon.MarkResponsesEncryptedReasoningReplayDecision(c, capability)
	if info != nil && len(info.ParamOverride) > 0 {
		if err := relaycommon.ApplyHeaderOnlyParamOverrideWithRelayInfo(info); err != nil {
			return nil, convertedRequest, newAPIErrorFromParamOverride(err)
		}
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, convertedRequest, types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
	}
	normalizedInput, normalizeResult, normalizeErr := relaycommon.NormalizeResponsesInputItemIDs(request.Input)
	if normalizeErr != nil {
		return nil, convertedRequest, newResponsesConvertRequestError(normalizeErr)
	}
	streamOptionsIncludeUsageCapabilityDisabled := relaycommon.IsOpenAICompatibleResponses(info) &&
		info.ChannelOtherSettings.ExplicitlyDisablesResponsesStreamOptionsIncludeUsage()
	reasoningContextRequiresConversion := responsesReasoningContextRequiresConversion(c, info, request)
	if normalizeResult.RemovedCount == 0 &&
		!streamOptionsIncludeUsageCapabilityDisabled &&
		!reasoningContextRequiresConversion {
		requestBody, bodyErr := buildOpaqueResponsesStorageBody(c, info, request.Model, nil)
		if bodyErr != nil {
			return nil, convertedRequest, bodyErr
		}
		return requestBody, convertedRequest, nil
	}
	rawBody, err := storage.Bytes()
	if err != nil {
		return nil, convertedRequest, types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
	}
	if normalizeResult.RemovedCount > 0 {
		rawBody, err = sjson.SetRawBytes(rawBody, "input", normalizedInput)
		if err != nil {
			return nil, convertedRequest, newResponsesConvertRequestError(fmt.Errorf("normalize responses input item ids in opaque replay body: %w", err))
		}
		convertedRequest.Input = normalizedInput
		common.SetContextKey(c, appconstant.ContextKeyResponsesInputItemIDNormalizationRemovedCount, normalizeResult.RemovedCount)
		common.SysLog(fmt.Sprintf(
			"responses invalid input item ids removed on opaque replay: channel_id=%d model=%s count=%d",
			info.ChannelId,
			info.OriginModelName,
			normalizeResult.RemovedCount,
		))
	}
	if streamOptionsIncludeUsageCapabilityDisabled {
		var stripped bool
		rawBody, stripped, err = stripResponsesStreamOptionsIncludeUsageFromJSON(rawBody)
		if err != nil {
			return nil, convertedRequest, newResponsesConvertRequestError(fmt.Errorf("strip stream_options.include_usage from opaque replay: %w", err))
		}
		if stripped {
			convertedRequest, _ = stripResponsesStreamOptionsIncludeUsage(convertedRequest)
			markResponsesStreamOptionsIncludeUsageCapabilityStripped(c, info)
		}
	}
	rawBody, convertedRequest, apiErr := applyOpaqueResponsesReasoningContextCapability(c, info, convertedRequest, rawBody)
	if apiErr != nil {
		return nil, convertedRequest, apiErr
	}
	upstreamModel := strings.TrimSpace(request.Model)
	if upstreamModel == "" {
		return nil, convertedRequest, newResponsesConvertRequestError(errors.New("responses opaque encrypted replay model is required"))
	}
	rawModel := gjson.GetBytes(rawBody, "model")
	if !rawModel.Exists() || rawModel.Type != gjson.String || rawModel.String() != upstreamModel {
		rawBody, err = sjson.SetBytes(rawBody, "model", upstreamModel)
		if err != nil {
			return nil, convertedRequest, newResponsesConvertRequestError(fmt.Errorf("set model for responses opaque encrypted replay: %w", err))
		}
	}
	body, size, getBody, closer, err := relaycommon.NewOutboundJSONBody(rawBody)
	if err != nil {
		return nil, convertedRequest, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	info.UpstreamRequestBodySize = size
	info.UpstreamRequestGetBody = getBody
	captureCodexFoldedBaseRequestBody(info, rawBody)
	return &managedResponsesRequestBody{Reader: body, closer: closer}, convertedRequest, nil
}

func applyOpaqueResponsesReasoningContextCapability(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest, rawBody []byte) ([]byte, dto.OpenAIResponsesRequest, *types.NewAPIError) {
	if request.Reasoning == nil || len(bytes.TrimSpace(request.Reasoning.Context)) == 0 {
		return rawBody, request, nil
	}

	decision := relaycommon.ResolveResponsesReasoningContextCapabilityDecision(info, request.Model)
	if c != nil {
		common.SetContextKey(c, appconstant.ContextKeyResponsesReasoningContextAction, decision.Action)
	}
	if decision.Capability != dto.ResponsesReasoningContextCapabilityUnsupported {
		return rawBody, request, nil
	}

	strippedRequest, stripped := relaycommon.StripResponsesReasoningContext(request)
	if !stripped {
		return rawBody, request, nil
	}
	updatedBody, err := sjson.DeleteBytes(rawBody, "reasoning.context")
	if err != nil {
		return rawBody, request, newResponsesConvertRequestError(fmt.Errorf("strip reasoning.context from opaque responses body: %w", err))
	}
	if bytes.Equal(rawBody, updatedBody) {
		return rawBody, request, newResponsesConvertRequestError(errors.New("opaque responses body does not contain reasoning.context"))
	}
	common.SysLog(fmt.Sprintf(
		"responses reasoning context stripped from opaque replay by channel capability: channel_id=%d model=%s profile=%s",
		decision.ChannelID,
		decision.Model,
		decision.Profile,
	))
	return updatedBody, strippedRequest, nil
}

func buildOpaqueOpenAIResponsesReplayBody(c *gin.Context, info *relaycommon.RelayInfo, request *dto.OpaqueOpenAIResponsesRequest) (io.Reader, *types.NewAPIError) {
	if request == nil {
		return nil, types.NewErrorWithStatusCode(
			errors.New("opaque Responses request is required"),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
	}
	metadata := request.OpaqueResponsesMetadata()
	if metadata.BodySize != storage.Size() {
		return nil, newResponsesConvertRequestError(fmt.Errorf(
			"opaque Responses replay body size mismatch: metadata=%d storage=%d",
			metadata.BodySize,
			storage.Size(),
		))
	}
	upstreamModel := strings.TrimSpace(request.Model)
	if upstreamModel == "" {
		return nil, newResponsesConvertRequestError(errors.New("responses opaque encrypted replay model is required"))
	}
	if info != nil && len(info.ParamOverride) > 0 {
		if err := relaycommon.ApplyHeaderOnlyParamOverrideWithRelayInfo(info); err != nil {
			return nil, newAPIErrorFromParamOverride(err)
		}
	}
	return buildOpaqueResponsesStorageBody(c, info, upstreamModel, &metadata)
}

func buildOpaqueResponsesStorageBody(c *gin.Context, info *relaycommon.RelayInfo, upstreamModel string, metadata *common.ResponsesRequestMetadata) (io.Reader, *types.NewAPIError) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
	}
	if metadata != nil && metadata.BodySize != storage.Size() {
		return nil, newResponsesConvertRequestError(fmt.Errorf(
			"opaque Responses replay body size mismatch: metadata=%d storage=%d",
			metadata.BodySize,
			storage.Size(),
		))
	}
	location := common.TopLevelJSONStringLocation{}
	if metadata != nil {
		location = metadata.ModelLocation
	}
	if location.End <= location.Start {
		location, err = common.LocateTopLevelJSONString(storage, "model")
		if err != nil {
			return nil, newResponsesConvertRequestError(fmt.Errorf("locate model for opaque Responses replay: %w", err))
		}
	}
	if location.Value == strings.TrimSpace(upstreamModel) {
		requestBody, err := common.NewSizedReader(storage, storage.Size())
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
		}
		if info != nil {
			info.UpstreamRequestBodySize = storage.Size()
			info.UpstreamRequestGetBody = func() (io.ReadCloser, error) {
				return common.NewBodyStorageReader(storage)
			}
			if err := captureCodexFoldedBaseRequestBodyFromStorage(info, storage, info.UpstreamRequestGetBody); err != nil {
				return nil, types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
			}
		}
		return requestBody, nil
	}
	requestBody, err := common.NewTopLevelJSONStringReplacementReader(storage, storage.Size(), location, strings.TrimSpace(upstreamModel))
	if err != nil {
		return nil, newResponsesConvertRequestError(fmt.Errorf("replace model for opaque Responses replay: %w", err))
	}
	if info != nil {
		info.UpstreamRequestBodySize = requestBody.ContentLength()
		info.UpstreamRequestGetBody = func() (io.ReadCloser, error) {
			reader, err := common.NewTopLevelJSONStringReplacementReader(storage, storage.Size(), location, strings.TrimSpace(upstreamModel))
			if err != nil {
				return nil, err
			}
			return io.NopCloser(reader), nil
		}
		if err := captureCodexFoldedBaseRequestBodyFromStorage(info, storage, info.UpstreamRequestGetBody); err != nil {
			return nil, types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
		}
	}
	return requestBody, nil
}

func doOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, adaptor relaychannel.Adaptor, requestBody io.Reader, options ...doOpenAIResponsesRequestOptions) (*dto.Usage, *types.NewAPIError) {
	var httpResp *http.Response
	relaycommon.MarkResponsesEncryptedReasoningReplayAttempt(c)
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}

	skipStatusCodeMapping := len(options) > 0 && options[0].SkipStatusCodeMapping

	if resp != nil {
		relaycommon.MarkResponsesEncryptedReasoningReplayForwarded(c)
		httpResp = resp.(*http.Response)

		if httpResp.StatusCode != http.StatusOK && !shouldParseCodexFoldedSSEStatus(info, httpResp) {
			newAPIError := service.RelayErrorHandler(c.Request.Context(), httpResp, false)
			if !skipStatusCodeMapping {
				resetResponsesStatusCode(c, newAPIError)
			}
			return nil, newAPIError
		}
	}

	usage, newAPIError := adaptor.DoResponse(c, httpResp, info)
	if newAPIError != nil {
		if !skipStatusCodeMapping {
			resetResponsesStatusCode(c, newAPIError)
		}
		return nil, newAPIError
	}

	usageDto := usage.(*dto.Usage)
	return usageDto, nil
}

func shouldParseCodexFoldedSSEStatus(info *relaycommon.RelayInfo, resp *http.Response) bool {
	return info != nil && info.IsStream &&
		info.RelayMode == relayconstant.RelayModeResponses &&
		info.ChannelType == appconstant.ChannelTypeCodex &&
		info.ChannelOtherSettings.CodexFoldedSSEEnabled &&
		resp != nil && strings.HasPrefix(strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type"))), "text/event-stream")
}

func resetResponsesStatusCode(c *gin.Context, newAPIError *types.NewAPIError) {
	if c == nil || newAPIError == nil {
		return
	}
	service.ResetStatusCode(newAPIError, c.GetString("status_code_mapping"))
}

func shouldRetryResponsesWithoutEncryptedReasoning(info *relaycommon.RelayInfo, err *types.NewAPIError) bool {
	if info == nil || err == nil || !relaycommon.IsOpenAICompatibleResponses(info) {
		return false
	}
	openAIError := err.ToOpenAIError()
	if isResponsesEncryptedContentParameterError(openAIError) &&
		relaycommon.HasResponsesInputEncryptedContentPath(openAIError.Param, openAIError.Message) &&
		(err.StatusCode == http.StatusBadRequest || err.StatusCode == http.StatusBadGateway) {
		return true
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

func isResponsesEncryptedContentParameterError(openAIError types.OpenAIError) bool {
	if !types.IsOpenAIRequestParameterError(&openAIError) {
		return false
	}
	value := strings.ToLower(strings.Join([]string{
		openAIError.Message,
		openAIError.Param,
		fmt.Sprint(openAIError.Code),
	}, " "))
	return strings.Contains(value, "encrypted_content") || strings.Contains(value, "encrypted content")
}

func newResponsesConvertRequestError(err error) *types.NewAPIError {
	options := []types.NewAPIErrorOptions{types.ErrOptionWithSkipRetry()}
	if errors.Is(err, service.ErrSyntheticCompactStateNotFound) ||
		errors.Is(err, service.ErrSyntheticCompactRequiresVisibleInput) ||
		errors.Is(err, service.ErrSyntheticCompactStateScopeMismatch) ||
		errors.Is(err, service.ErrSyntheticCompactMultipleMarkers) ||
		errors.Is(err, service.ErrResponsesRESTPreviousIDUnsupported) ||
		errors.Is(err, service.ErrResponsesNativeOpaqueStateNotRestorable) ||
		errors.Is(err, service.ErrResponsesChatCompatibilityPreviousIDUnsupported) ||
		errors.Is(err, service.ErrResponsesChatCompatibilityToolBridgeRequestInvalid) ||
		errors.Is(err, relaycommon.ErrResponsesContextCompactionOnlyInput) ||
		errors.Is(err, relaycommon.ErrResponsesContextCompactionInvalid) {
		options = append(options, types.ErrOptionWithStatusCode(http.StatusBadRequest))
	}
	if errors.Is(err, service.ErrResponsesCompactionContentRequired) {
		options = append(options, types.ErrOptionWithStatusCode(http.StatusBadRequest))
	}
	if errors.Is(err, service.ErrProtocolConversionUnsupported) {
		options = append(options, types.ErrOptionWithStatusCode(http.StatusBadRequest))
	}
	return types.NewError(err, types.ErrorCodeConvertRequestFailed, options...)
}

func markResponsesEncryptedReasoningReplayDecision(c *gin.Context, capability dto.ResponsesEncryptedReasoningReplayCapability) {
	relaycommon.MarkResponsesEncryptedReasoningReplayDecision(c, capability)
}

func applyResponsesCompactSummaryModelOverride(c *gin.Context, info *relaycommon.RelayInfo, request *dto.OpenAIResponsesRequest, hasOpaqueEncryptedContent bool) {
	if c == nil || request == nil || hasOpaqueEncryptedContent || !shouldApplyResponsesCompactSummaryModelOverride(info) {
		return
	}
	model := strings.TrimSpace(common.GetContextKeyString(c, appconstant.ContextKeyResponsesCompactSummaryModel))
	if model == "" {
		return
	}
	request.SetModelName(model)
	info.UpstreamModelName = model
}

func shouldApplyResponsesCompactSummaryModelOverride(info *relaycommon.RelayInfo) bool {
	if info == nil || info.ChannelMeta == nil || info.RelayMode != relayconstant.RelayModeResponsesCompact {
		return false
	}
	if relaycommon.IsSyntheticOpenAICompatibleResponsesCompact(info) {
		return true
	}
	return info.ChannelType == appconstant.ChannelTypeCodex
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
		info.ChannelOtherSettings,
	)
}

func responsesViaChatOptionsFromRule(rule *model_setting.ProtocolConversionRule) service.ResponsesChatCompatibilityOptions {
	return service.ResponsesViaChatRuleOptions(rule)
}
