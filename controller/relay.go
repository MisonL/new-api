package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func relayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	switch info.RelayMode {
	case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
		err = relay.ImageHelper(c, info)
	case relayconstant.RelayModeAudioSpeech:
		fallthrough
	case relayconstant.RelayModeAudioTranslation:
		fallthrough
	case relayconstant.RelayModeAudioTranscription:
		err = relay.AudioHelper(c, info)
	case relayconstant.RelayModeRerank:
		err = relay.RerankHelper(c, info)
	case relayconstant.RelayModeEmbeddings:
		err = relay.EmbeddingHelper(c, info)
	case relayconstant.RelayModeResponses, relayconstant.RelayModeResponsesCompact:
		err = relay.ResponsesHelper(c, info)
	default:
		err = relay.TextHelper(c, info)
	}
	return err
}

func geminiRelayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	if strings.Contains(c.Request.URL.Path, "embed") {
		err = relay.GeminiEmbeddingHandler(c, info)
	} else {
		err = relay.GeminiHelper(c, info)
	}
	return err
}

// Relay handles OpenAI-compatible relay requests and applies channel retry logic.
func Relay(c *gin.Context, relayFormat types.RelayFormat) {

	requestId := c.GetString(common.RequestIdKey)
	userSetting, _ := common.GetContextKeyType[dto.UserSetting](c, constant.ContextKeyUserSetting)
	common.InitPayloadAudit(
		c,
		userSetting.RecordRequestContentLog,
		userSetting.RecordResponseContentLog && relayFormat != types.RelayFormatOpenAIRealtime,
	)
	//group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	//originalModel := common.GetContextKeyString(c, constant.ContextKeyOriginalModel)

	var (
		newAPIError *types.NewAPIError
		ws          *websocket.Conn
	)

	if relayFormat == types.RelayFormatOpenAIRealtime {
		var err error
		ws, err = upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			helper.WssError(c, ws, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry()).ToOpenAIError())
			return
		}
		defer ws.Close()
	}

	defer func() {
		if newAPIError != nil {
			logger.LogError(c, fmt.Sprintf("relay error: %s", common.LocalLogPreview(newAPIError.Error())))
			newAPIError.SetMessage(common.MessageWithRequestId(newAPIError.Error(), requestId))
			switch relayFormat {
			case types.RelayFormatOpenAIRealtime:
				helper.WssError(c, ws, newAPIError.ToOpenAIError())
			case types.RelayFormatClaude:
				c.JSON(newAPIError.StatusCode, gin.H{
					"type":  "error",
					"error": newAPIError.ToClaudeError(),
				})
			default:
				if service.ShouldWriteResponsesBootstrapStreamError(c) {
					helper.SetEventStreamHeaders(c)
					service.MarkResponsesBootstrapHeadersSent(c)
					if err := helper.OpenAIErrorEvent(c, newAPIError.ToOpenAIError()); err != nil {
						logger.LogError(c, fmt.Sprintf("write bootstrap stream error failed: %s", err.Error()))
					}
				} else {
					c.JSON(newAPIError.StatusCode, gin.H{
						"error": newAPIError.ToOpenAIError(),
					})
				}
			}
		}
	}()

	request, err := helper.GetAndValidateRequest(c, relayFormat)
	if err != nil {
		// Map "request body too large" to 413 so clients can handle it correctly
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
		} else {
			newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		return
	}

	relayInfo, err := relaycommon.GenRelayInfo(c, relayFormat, request, ws)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeGenRelayInfoFailed)
		return
	}
	service.EnsureResponsesBootstrapRecoveryState(c, relayInfo.IsStream)

	needSensitiveCheck := setting.ShouldCheckPromptSensitive()
	needCountToken := constant.CountToken
	// Avoid building huge CombineText (strings.Join) when token counting and sensitive check are both disabled.
	var meta *types.TokenCountMeta
	if needSensitiveCheck || needCountToken {
		meta = request.GetTokenCountMeta()
	} else {
		meta = fastTokenCountMetaForPricing(request)
	}

	if needSensitiveCheck && meta != nil {
		contains, words := service.CheckSensitiveText(meta.CombineText)
		if contains {
			logger.LogWarn(c, fmt.Sprintf("user sensitive words detected: %s", strings.Join(words, ", ")))
			newAPIError = types.NewError(err, types.ErrorCodeSensitiveWordsDetected)
			return
		}
	}

	tokens, err := service.EstimateRequestToken(c, meta, relayInfo)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeCountTokenFailed)
		return
	}

	relayInfo.SetEstimatePromptTokens(tokens)

	priceData, err := helper.ModelPriceHelper(c, relayInfo, tokens, meta)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
		return
	}

	// common.SetContextKey(c, constant.ContextKeyTokenCountMeta, meta)
	if priceData.FreeModel {
		logger.LogInfo(c, fmt.Sprintf("模型 %s 免费，跳过预扣费", relayInfo.OriginModelName))
	}

	defer func() {
		// Only return quota if downstream failed and quota was actually pre-consumed
		if newAPIError != nil {
			newAPIError = service.NormalizeViolationFeeError(newAPIError)
			if relayInfo.Billing != nil {
				relayInfo.Billing.Refund(c)
			}
			service.ChargeViolationFeeIfNeeded(c, relayInfo, newAPIError)
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:        c,
		TokenGroup: relayInfo.TokenGroup,
		ModelName:  relayInfo.OriginModelName,
		Retry:      common.GetPointer(0),
	}
	relayInfo.RetryIndex = 0
	relayInfo.LastError = nil

	for {
		retryParam.SetRetry(0)
		for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
			relayInfo.RetryIndex = retryParam.GetRetry()
			channel, channelErr := getChannel(c, relayInfo, retryParam)
			if channelErr != nil {
				logger.LogError(c, channelErr.Error())
				newAPIError = channelErr
				break
			}

			addUsedChannel(c, channel.Id)
			bodyStorage, bodyErr := common.GetBodyStorage(c)
			if bodyErr != nil {
				// Ensure consistent 413 for oversized bodies even when error occurs later (e.g., retry path)
				if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
					newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
				} else {
					newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
				}
				break
			}
			c.Request.Body = io.NopCloser(bodyStorage)

			if !priceData.FreeModel && relayInfo.Billing == nil {
				newAPIError = service.PreConsumeBilling(c, priceData.QuotaToPreConsume, relayInfo)
				if newAPIError != nil {
					break
				}
			}

			switch relayFormat {
			case types.RelayFormatOpenAIRealtime:
				newAPIError = relay.WssHelper(c, relayInfo)
			case types.RelayFormatClaude:
				newAPIError = relay.ClaudeHelper(c, relayInfo)
			case types.RelayFormatGemini:
				newAPIError = geminiRelayHandler(c, relayInfo)
			default:
				newAPIError = relayHandler(c, relayInfo)
			}

			if newAPIError == nil {
				relayInfo.LastError = nil
				return
			}

			newAPIError = service.NormalizeViolationFeeError(newAPIError)
			relayInfo.LastError = newAPIError

			if shouldFallbackResponsesCompactNativeContext(c, relayInfo, newAPIError) {
				fallbackSnapshot := snapshotResponsesCompactFallbackContext(c)
				c.Set("responses_compact_context_fallback_attempted", true)
				logger.LogWarn(c, fmt.Sprintf(
					"responses compact context fallback to synthetic summary: channel_id=%d status_code=%d error=%s",
					relayInfo.ChannelMeta.ChannelId,
					newAPIError.StatusCode,
					newAPIError.MaskSensitiveError(),
				))
				newAPIError = retryResponsesCompactSyntheticSummary(c, relayInfo, bodyStorage, newAPIError)
				if newAPIError == nil {
					relayInfo.LastError = nil
					return
				}
				restoreResponsesCompactFallbackContext(c, fallbackSnapshot)
				relayInfo.LastError = newAPIError
			}

			if shouldFallbackResponsesCompactAuto(c, relayInfo, newAPIError) {
				fallbackSnapshot := snapshotResponsesCompactFallbackContext(c)
				c.Set("responses_compact_auto_fallback_attempted", true)
				fallbackReason := newAPIError.MaskSensitiveErrorWithStatusCode()
				logger.LogWarn(c, fmt.Sprintf(
					"responses compact auto fallback to synthetic summary: channel_id=%d status_code=%d error=%s",
					relayInfo.ChannelMeta.ChannelId,
					newAPIError.StatusCode,
					newAPIError.MaskSensitiveError(),
				))
				newAPIError = retryResponsesCompactSyntheticSummary(c, relayInfo, bodyStorage, newAPIError)
				if newAPIError == nil {
					if err := model.MarkResponsesCompactAutoFallback(relayInfo.ChannelMeta.ChannelId, fallbackReason); err != nil {
						logger.LogError(c, fmt.Sprintf("mark responses compact auto fallback failed: %s", err.Error()))
					}
					relayInfo.LastError = nil
					return
				}
				restoreResponsesCompactFallbackContext(c, fallbackSnapshot)
				relayInfo.LastError = newAPIError
			}

			if shouldFallbackResponsesCompactSummaryModel(c, relayInfo, newAPIError) {
				fallbackSnapshot := snapshotResponsesCompactFallbackContext(c)
				newAPIError = retryResponsesCompactSummaryFallbackModels(c, relayInfo, bodyStorage, newAPIError)
				if newAPIError == nil {
					relayInfo.LastError = nil
					return
				}
				restoreResponsesCompactFallbackContext(c, fallbackSnapshot)
				relayInfo.LastError = newAPIError
			}

			channelError := *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan())
			if shouldSuppressBootstrapRecoveryAutoBan(c, newAPIError) {
				channelError.AutoBan = false
			}
			processChannelError(c, relayInfo, channelError, newAPIError)

			if !shouldRetry(c, newAPIError, common.RetryTimes-retryParam.GetRetry()) {
				break
			}
		}

		if !service.CanContinueResponsesBootstrapRecovery(c, newAPIError) {
			break
		}
		releaseBootstrapRecoveryBilling(c, relayInfo)
		waitErr, canceled := waitForResponsesBootstrapRecoveryProbe(c)
		if canceled {
			return
		}
		if waitErr != nil {
			newAPIError = waitErr
			break
		}
		if !service.CanContinueResponsesBootstrapRecovery(c, newAPIError) {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}
	if newAPIError != nil {
		gopool.Go(func() {
			perfmetrics.RecordRelaySample(relayInfo, false, 0)
		})
	}
}

func releaseBootstrapRecoveryBilling(c *gin.Context, relayInfo *relaycommon.RelayInfo) {
	if relayInfo == nil || relayInfo.Billing == nil {
		return
	}
	relayInfo.ResetBillingMetadata(c)
}

func waitForResponsesBootstrapRecoveryProbe(c *gin.Context) (*types.NewAPIError, bool) {
	waitDuration, sendPing, ok := service.NextResponsesBootstrapWait(c, time.Now())
	if !ok {
		return nil, false
	}
	if sendPing {
		helper.SetEventStreamHeaders(c)
		service.MarkResponsesBootstrapHeadersSent(c)
		now := time.Now()
		if err := helper.PingData(c); err != nil {
			return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry()), false
		}
		service.MarkResponsesBootstrapPingSent(c, now)
	}
	timer := time.NewTimer(waitDuration)
	defer timer.Stop()
	select {
	case <-c.Request.Context().Done():
		if errors.Is(c.Request.Context().Err(), context.Canceled) {
			return nil, true
		}
		return types.NewOpenAIError(c.Request.Context().Err(), types.ErrorCodeDoRequestFailed, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry()), false
	case <-timer.C:
		return nil, false
	}
}

func shouldSuppressBootstrapRecoveryAutoBan(c *gin.Context, newAPIError *types.NewAPIError) bool {
	if newAPIError == nil {
		return false
	}
	if newAPIError.StatusCode != http.StatusUnauthorized && newAPIError.StatusCode != http.StatusForbidden {
		return false
	}
	return service.CanContinueResponsesBootstrapRecovery(c, newAPIError)
}

var upgrader = websocket.Upgrader{
	Subprotocols: []string{"realtime"}, // WS 握手支持的协议，如果有使用 Sec-WebSocket-Protocol，则必须在此声明对应的 Protocol TODO add other protocol
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许跨域
	},
}

func addUsedChannel(c *gin.Context, channelId int) {
	useChannel := c.GetStringSlice("use_channel")
	useChannel = append(useChannel, fmt.Sprintf("%d", channelId))
	c.Set("use_channel", useChannel)
}

func fastTokenCountMetaForPricing(request dto.Request) *types.TokenCountMeta {
	if request == nil {
		return &types.TokenCountMeta{}
	}
	meta := &types.TokenCountMeta{
		TokenType: types.TokenTypeTokenizer,
	}
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		maxCompletionTokens := lo.FromPtrOr(r.MaxCompletionTokens, uint(0))
		maxTokens := lo.FromPtrOr(r.MaxTokens, uint(0))
		if maxCompletionTokens > maxTokens {
			meta.MaxTokens = int(maxCompletionTokens)
		} else {
			meta.MaxTokens = int(maxTokens)
		}
	case *dto.OpenAIResponsesRequest:
		meta.MaxTokens = int(lo.FromPtrOr(r.MaxOutputTokens, uint(0)))
	case *dto.ClaudeRequest:
		meta.MaxTokens = int(lo.FromPtr(r.MaxTokens))
	case *dto.ImageRequest:
		// Pricing for image requests depends on ImagePriceRatio; safe to compute even when CountToken is disabled.
		return r.GetTokenCountMeta()
	default:
		// Best-effort: leave CombineText empty to avoid large allocations.
	}
	return meta
}

func getChannel(c *gin.Context, info *relaycommon.RelayInfo, retryParam *service.RetryParam) (*model.Channel, *types.NewAPIError) {
	if info.ChannelMeta == nil && retryParam.GetRetry() == 0 {
		autoBan := c.GetBool("auto_ban")
		autoBanInt := 1
		if !autoBan {
			autoBanInt = 0
		}
		return &model.Channel{
			Id:      c.GetInt("channel_id"),
			Type:    c.GetInt("channel_type"),
			Name:    c.GetString("channel_name"),
			AutoBan: &autoBanInt,
		}, nil
	}
	channel, selectGroup, err := service.CacheGetRandomSatisfiedChannel(retryParam)

	info.PriceData.GroupRatioInfo = helper.HandleGroupRatio(c, info)

	if err != nil {
		return nil, types.NewError(fmt.Errorf("获取分组 %s 下模型 %s 的可用渠道失败（retry）: %s", selectGroup, info.OriginModelName, err.Error()), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if channel == nil {
		return nil, types.NewError(errors.New(formatNoAvailableChannelErrorMessage(selectGroup, info.OriginModelName, info.LastError)), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}

	newAPIError := middleware.SetupContextForSelectedChannel(c, channel, info.OriginModelName)
	if newAPIError != nil {
		return nil, newAPIError
	}
	info.InitChannelMeta(c)
	return channel, nil
}

func formatNoAvailableChannelErrorMessage(group string, model string, lastErr *types.NewAPIError) string {
	message := fmt.Sprintf("分组 %s 下模型 %s 的可用渠道不存在（retry）", group, model)
	if lastErr == nil {
		return message
	}
	return fmt.Sprintf("%s，上一错误：%s", message, lastErr.MaskSensitiveErrorWithStatusCode())
}

func shouldRetry(c *gin.Context, openaiErr *types.NewAPIError, retryTimes int) bool {
	if openaiErr == nil {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) && !isUpstreamRateLimitError(openaiErr.StatusCode) {
		return false
	}
	if types.IsChannelError(openaiErr) {
		return true
	}
	if types.IsSkipRetryError(openaiErr) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	code := openaiErr.StatusCode
	if code >= 200 && code < 300 {
		return false
	}
	if code < 100 || code > 599 {
		return true
	}
	if shouldRetryTimeoutForResponsesCompact(c, code) {
		return true
	}
	if operation_setting.IsAlwaysSkipRetryCode(openaiErr.GetErrorCode()) {
		return false
	}
	return operation_setting.ShouldRetryByStatusCode(code)
}

func isUpstreamRateLimitError(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests
}

func shouldRetryTimeoutForResponsesCompact(c *gin.Context, statusCode int) bool {
	if statusCode != http.StatusGatewayTimeout && statusCode != 524 {
		return false
	}
	return c.GetInt("relay_mode") == relayconstant.RelayModeResponsesCompact
}

func shouldFallbackResponsesCompactAuto(c *gin.Context, info *relaycommon.RelayInfo, err *types.NewAPIError) bool {
	if err == nil || info == nil || info.ChannelMeta == nil {
		return false
	}
	if c.GetBool("responses_compact_auto_fallback_attempted") {
		return false
	}
	if info.RelayMode != relayconstant.RelayModeResponsesCompact ||
		info.ChannelType != constant.ChannelTypeOpenAI ||
		!info.ChannelOtherSettings.IsAutoResponsesCompact() {
		return false
	}
	if info.ChannelOtherSettings.HasActiveResponsesCompactAutoFallback(time.Now()) {
		return false
	}
	switch err.StatusCode {
	case http.StatusBadRequest,
		http.StatusBadGateway,
		http.StatusNotFound,
		http.StatusMethodNotAllowed,
		http.StatusUnprocessableEntity,
		http.StatusNotImplemented:
		return isResponsesCompactNativeCompatibilityError(info, err)
	case http.StatusGatewayTimeout, 524:
		return true
	default:
		return false
	}
}

type requestBodyReadSeeker interface {
	io.Reader
	io.Seeker
}

func setResponsesCompactSyntheticMode(c *gin.Context, info *relaycommon.RelayInfo) {
	if info == nil || info.ChannelMeta == nil {
		return
	}
	settings := info.ChannelMeta.ChannelOtherSettings
	settings.ResponsesCompactMode = dto.ResponsesCompactModeSynthetic
	info.ChannelMeta.ChannelOtherSettings = settings
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, settings)
}

func restoreResponsesCompactMode(c *gin.Context, info *relaycommon.RelayInfo, settings dto.ChannelOtherSettings) {
	if info == nil || info.ChannelMeta == nil {
		return
	}
	info.ChannelMeta.ChannelOtherSettings = settings
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, settings)
}

type responsesCompactFallbackContextValue struct {
	exists bool
	value  any
}

var responsesCompactFallbackContextKeys = []string{
	"responses_compact_auto_fallback_attempted",
	"responses_compact_context_fallback_attempted",
	"responses_compact_summary_model_fallback_attempted",
	string(constant.ContextKeyResponsesCompactSummaryModel),
	string(constant.ContextKeyResponsesCompactSummaryModels),
}

func snapshotResponsesCompactFallbackContext(c *gin.Context) map[string]responsesCompactFallbackContextValue {
	snapshot := make(map[string]responsesCompactFallbackContextValue, len(responsesCompactFallbackContextKeys))
	if c == nil {
		return snapshot
	}
	for _, key := range responsesCompactFallbackContextKeys {
		value, exists := c.Get(key)
		snapshot[key] = responsesCompactFallbackContextValue{
			exists: exists,
			value:  value,
		}
	}
	return snapshot
}

func restoreResponsesCompactFallbackContext(c *gin.Context, snapshot map[string]responsesCompactFallbackContextValue) {
	if c == nil {
		return
	}
	for _, key := range responsesCompactFallbackContextKeys {
		entry, exists := snapshot[key]
		if exists && entry.exists {
			c.Set(key, entry.value)
			continue
		}
		if c.Keys != nil {
			delete(c.Keys, key)
		}
	}
}

func retryResponsesCompactSyntheticSummary(c *gin.Context, info *relaycommon.RelayInfo, bodyStorage requestBodyReadSeeker, triggerErr *types.NewAPIError) *types.NewAPIError {
	if info == nil || info.ChannelMeta == nil {
		return triggerErr
	}
	originalSettings := info.ChannelMeta.ChannelOtherSettings
	setResponsesCompactSyntheticMode(c, info)
	err := retryResponsesCompactWithBody(c, info, bodyStorage, triggerErr)
	if shouldFallbackResponsesCompactSummaryModel(c, info, err) {
		err = retryResponsesCompactSummaryFallbackModels(c, info, bodyStorage, err)
	}
	if err != nil {
		restoreResponsesCompactMode(c, info, originalSettings)
	}
	return err
}

func retryResponsesCompactSummaryFallbackModels(c *gin.Context, info *relaycommon.RelayInfo, bodyStorage requestBodyReadSeeker, triggerErr *types.NewAPIError) *types.NewAPIError {
	if info == nil || info.ChannelMeta == nil {
		return triggerErr
	}
	models := responsesCompactSummaryFallbackCandidates(c, info)
	if len(models) == 0 {
		return triggerErr
	}
	lastErr := triggerErr
	c.Set("responses_compact_summary_model_fallback_attempted", true)
	common.SetContextKey(c, constant.ContextKeyResponsesCompactSummaryModels, models)
	for _, fallbackModel := range models {
		common.SetContextKey(c, constant.ContextKeyResponsesCompactSummaryModel, fallbackModel)
		logger.LogWarn(c, fmt.Sprintf(
			"responses compact summary model fallback: channel_id=%d model=%s status_code=%d error=%s",
			info.ChannelMeta.ChannelId,
			fallbackModel,
			lastErr.StatusCode,
			lastErr.MaskSensitiveError(),
		))
		lastErr = retryResponsesCompactWithBody(c, info, bodyStorage, lastErr)
		if lastErr == nil {
			return nil
		}
		if !isResponsesCompactContextLengthError(lastErr) {
			common.SetContextKey(c, constant.ContextKeyResponsesCompactSummaryModel, "")
			return lastErr
		}
	}
	common.SetContextKey(c, constant.ContextKeyResponsesCompactSummaryModel, "")
	return lastErr
}

func responsesCompactSummaryFallbackCandidates(c *gin.Context, info *relaycommon.RelayInfo) []string {
	if info == nil || info.ChannelMeta == nil {
		return nil
	}
	originalModel := strings.TrimSpace(info.UpstreamModelName)
	if originalModel == "" {
		originalModel = strings.TrimSpace(info.OriginModelName)
	}
	currentSummaryModel := strings.TrimSpace(common.GetContextKeyString(c, constant.ContextKeyResponsesCompactSummaryModel))
	models := make([]string, 0)
	for _, model := range info.ChannelOtherSettings.ResponsesCompactSummaryFallbackModelsOrDefault() {
		if model == originalModel || model == currentSummaryModel {
			continue
		}
		models = append(models, model)
	}
	return models
}

func retryResponsesCompactWithBody(c *gin.Context, info *relaycommon.RelayInfo, bodyStorage requestBodyReadSeeker, triggerErr *types.NewAPIError) *types.NewAPIError {
	if _, seekErr := bodyStorage.Seek(0, io.SeekStart); seekErr != nil {
		wrappedErr := fmt.Errorf(
			"seek request body for responses compact fallback after upstream error (%s): %w",
			triggerErr.MaskSensitiveErrorWithStatusCode(),
			seekErr,
		)
		return types.NewErrorWithStatusCode(wrappedErr, types.ErrorCodeReadRequestBodyFailed, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
	}
	c.Request.Body = io.NopCloser(bodyStorage)
	err := relayHandler(c, info)
	if err == nil {
		return nil
	}
	return service.NormalizeViolationFeeError(err)
}

func shouldFallbackResponsesCompactNativeContext(c *gin.Context, info *relaycommon.RelayInfo, err *types.NewAPIError) bool {
	if err == nil || info == nil || info.ChannelMeta == nil {
		return false
	}
	if c.GetBool("responses_compact_context_fallback_attempted") {
		return false
	}
	if info.RelayMode != relayconstant.RelayModeResponsesCompact ||
		info.ChannelType != constant.ChannelTypeOpenAI ||
		!info.ChannelOtherSettings.HasNativeResponsesCompact() ||
		!info.ChannelOtherSettings.ResponsesCompactContextFallbackEnabled() {
		return false
	}
	return isResponsesCompactContextLengthError(err)
}

func shouldFallbackResponsesCompactSummaryModel(c *gin.Context, info *relaycommon.RelayInfo, err *types.NewAPIError) bool {
	if err == nil || info == nil || info.ChannelMeta == nil {
		return false
	}
	if c.GetBool("responses_compact_summary_model_fallback_attempted") {
		return false
	}
	if info.RelayMode != relayconstant.RelayModeResponsesCompact ||
		info.ChannelType != constant.ChannelTypeOpenAI ||
		!info.ChannelOtherSettings.HasSyntheticResponsesCompact() ||
		!info.ChannelOtherSettings.ResponsesCompactSummaryModelFallbackEnabled() ||
		len(responsesCompactSummaryFallbackCandidates(c, info)) == 0 {
		return false
	}
	return isResponsesCompactContextLengthError(err)
}

func isResponsesCompactContextLengthError(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	switch err.StatusCode {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
	default:
		return false
	}
	openAIError := err.ToOpenAIError()
	message := strings.ToLower(strings.Join([]string{
		err.Error(),
		openAIError.Message,
		fmt.Sprint(openAIError.Code),
	}, " "))
	normalized := normalizeResponsesCompactCompatibilityMessage(message)
	for _, indicator := range []string{
		"context length",
		"context window",
		"context limit",
		"context_length_exceeded",
		"maximum context",
		"max context",
		"too many tokens",
		"token limit",
		"input too large",
		"request too large",
		"payload too large",
	} {
		if strings.Contains(message, indicator) || strings.Contains(normalized, indicator) {
			return true
		}
	}
	return false
}

func isResponsesCompactNativeCompatibilityError(info *relaycommon.RelayInfo, err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	openAIError := err.ToOpenAIError()
	message := strings.ToLower(strings.Join([]string{
		err.Error(),
		openAIError.Message,
		fmt.Sprint(openAIError.Code),
	}, " "))
	normalized := normalizeResponsesCompactCompatibilityMessage(message)
	compactPathMentioned := strings.Contains(message, "responses/compact") || strings.Contains(normalized, "responses compact")
	malformedCompactOutput := strings.Contains(normalized, "malformed compact output")
	payloadCompatibility := isResponsesCompactNativePayloadCompatibilityError(normalized)
	genericEndpointStatus := isGenericResponsesCompactEndpointStatus(err.StatusCode)
	if payloadCompatibility && !responsesCompactRequestHasContextPayload(info) {
		return false
	}
	if !compactPathMentioned && !malformedCompactOutput && !payloadCompatibility && !genericEndpointStatus {
		return false
	}
	if isModelLookupError(normalized) {
		return false
	}
	if isRequestParameterError(normalized) {
		return false
	}
	if payloadCompatibility {
		return true
	}
	if malformedCompactOutput {
		return true
	}
	if genericEndpointStatus {
		return true
	}
	for _, indicator := range []string{
		"not supported",
		"unsupported",
		"not implemented",
		"no route",
		"endpoint",
		"cannot post",
		"cannot get",
		"method",
		"path",
		"route",
		"url",
		"not found",
		"unknown",
		"unrecognized",
	} {
		if strings.Contains(normalized, indicator) {
			return true
		}
	}
	return false
}

func isGenericResponsesCompactEndpointStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return true
	default:
		return false
	}
}

func responsesCompactRequestHasContextPayload(info *relaycommon.RelayInfo) bool {
	if info == nil {
		return false
	}
	switch req := info.Request.(type) {
	case *dto.OpenAIResponsesCompactionRequest:
		return responsesRequestHasContextPayload(req.PreviousResponseID, req.Input)
	case *dto.OpenAIResponsesRequest:
		return responsesRequestHasContextPayload(req.PreviousResponseID, req.Input)
	default:
		return false
	}
}

func responsesRequestHasContextPayload(previousResponseID string, input common.RawMessage) bool {
	if strings.TrimSpace(previousResponseID) != "" {
		return true
	}
	if common.GetJsonType(input) != "array" {
		return false
	}
	var items []common.RawMessage
	if err := common.Unmarshal(input, &items); err != nil {
		return false
	}
	for _, rawItem := range items {
		var item map[string]common.RawMessage
		if err := common.Unmarshal(rawItem, &item); err != nil {
			continue
		}
		switch responsesCompactInputItemType(item) {
		case "compaction":
			return true
		case "reasoning":
			if responsesCompactInputHasEncryptedContent(item) {
				return true
			}
		}
	}
	return false
}

func responsesCompactInputItemType(item map[string]common.RawMessage) string {
	rawType := item["type"]
	if len(rawType) == 0 {
		return ""
	}
	var itemType string
	if err := common.Unmarshal(rawType, &itemType); err != nil {
		return ""
	}
	return itemType
}

func responsesCompactInputHasEncryptedContent(item map[string]common.RawMessage) bool {
	raw := strings.TrimSpace(string(item["encrypted_content"]))
	if raw == "" || raw == "null" {
		return false
	}
	var encryptedContent string
	if err := common.Unmarshal(item["encrypted_content"], &encryptedContent); err == nil {
		return strings.TrimSpace(encryptedContent) != ""
	}
	return true
}

func isResponsesCompactNativePayloadCompatibilityError(normalized string) bool {
	if strings.Contains(strings.ReplaceAll(normalized, " ", ""), "请求包含不允许的内容") {
		return true
	}
	for _, indicator := range []string{
		"request contains disallowed content",
		"request contains not allowed content",
		"payload contains disallowed content",
		"payload contains not allowed content",
		"input contains disallowed content",
		"input contains not allowed content",
		"content is not allowed",
		"content not allowed",
		"disallowed content",
	} {
		if strings.Contains(normalized, indicator) {
			return true
		}
	}
	return false
}

func normalizeResponsesCompactCompatibilityMessage(message string) string {
	normalized := strings.NewReplacer(
		"_", " ",
		"-", " ",
		"/", " ",
		".", " ",
	).Replace(message)
	return strings.Join(strings.Fields(normalized), " ")
}

func isRequestParameterError(normalized string) bool {
	for _, indicator := range []string{
		"unsupported parameter",
		"unknown parameter",
		"unrecognized parameter",
		"invalid parameter",
	} {
		if strings.Contains(normalized, indicator) {
			return true
		}
	}
	return false
}

func isModelLookupError(normalized string) bool {
	if !strings.Contains(normalized, "model") {
		return false
	}
	for _, indicator := range []string{
		"not found",
		"unknown model",
		"unrecognized model",
		"model does not exist",
		"model does not exists",
		"no such model",
		"invalid model",
		"model not available",
		"model unavailable",
		"not available",
		"unavailable",
	} {
		if strings.Contains(normalized, indicator) {
			return true
		}
	}
	return false
}

func processChannelError(c *gin.Context, relayInfo *relaycommon.RelayInfo, channelError types.ChannelError, err *types.NewAPIError) {
	logger.LogError(c, fmt.Sprintf("channel error (channel #%d, status code: %d): %s", channelError.ChannelId, err.StatusCode, common.LocalLogPreview(err.Error())))
	// 不要使用context获取渠道信息，异步处理时可能会出现渠道信息不一致的情况
	// do not use context to get channel info, there may be inconsistent channel info when processing asynchronously
	if service.ShouldDisableChannel(err) && channelError.AutoBan {
		gopool.Go(func() {
			service.DisableChannel(channelError, err.ErrorWithStatusCode())
		})
	}

	if constant.ErrorLogEnabled && types.IsRecordErrorLog(err) {
		// 保存错误日志到mysql中
		userId := c.GetInt("id")
		tokenName := c.GetString("token_name")
		modelName := c.GetString("original_model")
		tokenId := c.GetInt("token_id")
		userGroup := c.GetString("group")
		channelId := c.GetInt("channel_id")
		other := make(map[string]interface{})
		if c.Request != nil && c.Request.URL != nil {
			other["request_path"] = c.Request.URL.Path
		}
		other["error_type"] = err.GetErrorType()
		other["error_code"] = err.GetErrorCode()
		other["status_code"] = err.StatusCode
		other["channel_id"] = channelId
		other["channel_name"] = c.GetString("channel_name")
		other["channel_type"] = c.GetInt("channel_type")
		adminInfo := make(map[string]interface{})
		adminInfo["use_channel"] = c.GetStringSlice("use_channel")
		isMultiKey := common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey)
		if isMultiKey {
			adminInfo["is_multi_key"] = true
			adminInfo["multi_key_index"] = common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
		}
		service.AppendChannelAffinityAdminInfo(c, adminInfo)
		other["admin_info"] = adminInfo
		service.AppendRequestHeaderPolicyInfo(c, other)
		startTime := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
		if startTime.IsZero() {
			startTime = time.Now()
		}
		useTimeSeconds := int(relaycommon.SafeElapsedSeconds(startTime, time.Now()))
		contentParts, other := service.AppendResponsesCompactLogInfo(c, relayInfo, []string{err.MaskSensitiveErrorWithStatusCode()}, other, time.Now())
		model.RecordErrorLog(
			c,
			userId,
			channelId,
			modelName,
			tokenName,
			strings.Join(contentParts, ", "),
			tokenId,
			useTimeSeconds,
			common.GetContextKeyBool(c, constant.ContextKeyIsStream),
			userGroup,
			other,
		)
	}

}

// RelayMidjourney handles Midjourney proxy relay requests.
func RelayMidjourney(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatMjProxy, nil, nil)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"description": fmt.Sprintf("failed to generate relay info: %s", err.Error()),
			"type":        "upstream_error",
			"code":        4,
		})
		return
	}

	var mjErr *dto.MidjourneyResponse
	switch relayInfo.RelayMode {
	case relayconstant.RelayModeMidjourneyNotify:
		mjErr = relay.RelayMidjourneyNotify(c)
	case relayconstant.RelayModeMidjourneyTaskFetch, relayconstant.RelayModeMidjourneyTaskFetchByCondition:
		mjErr = relay.RelayMidjourneyTask(c, relayInfo.RelayMode)
	case relayconstant.RelayModeMidjourneyTaskImageSeed:
		mjErr = relay.RelayMidjourneyTaskImageSeed(c)
	case relayconstant.RelayModeSwapFace:
		mjErr = relay.RelaySwapFace(c, relayInfo)
	default:
		mjErr = relay.RelayMidjourneySubmit(c, relayInfo)
	}
	//err = relayMidjourneySubmit(c, relayMode)
	log.Println(mjErr)
	if mjErr != nil {
		statusCode := http.StatusBadRequest
		if mjErr.Code == 30 {
			mjErr.Result = "当前分组负载已饱和，请稍后再试，或升级账户以提升服务质量。"
			statusCode = http.StatusTooManyRequests
		}
		c.JSON(statusCode, gin.H{
			"description": fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result),
			"type":        "upstream_error",
			"code":        mjErr.Code,
		})
		channelId := c.GetInt("channel_id")
		logger.LogError(c, fmt.Sprintf("relay error (channel #%d, status code %d): %s", channelId, statusCode, fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result)))
	}
}

// RelayNotImplemented responds with a standardized "not implemented" error.
func RelayNotImplemented(c *gin.Context) {
	err := types.OpenAIError{
		Message: "API not implemented",
		Type:    "new_api_error",
		Param:   "",
		Code:    "api_not_implemented",
	}
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": err,
	})
}

// RelayNotFound responds with a standardized invalid URL error.
func RelayNotFound(c *gin.Context) {
	err := types.OpenAIError{
		Message: fmt.Sprintf("Invalid URL (%s %s)", c.Request.Method, c.Request.URL.Path),
		Type:    "invalid_request_error",
		Param:   "",
		Code:    "",
	}
	c.JSON(http.StatusNotFound, gin.H{
		"error": err,
	})
}

// RelayTaskFetch handles task-query relay requests.
func RelayTaskFetch(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &dto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}
	if taskErr := relay.RelayTaskFetch(c, relayInfo.RelayMode); taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

// RelayTask handles task submission and follow-up relay requests.
func RelayTask(c *gin.Context) {
	userSetting, _ := common.GetContextKeyType[dto.UserSetting](c, constant.ContextKeyUserSetting)
	common.InitPayloadAudit(
		c,
		userSetting.RecordRequestContentLog,
		userSetting.RecordResponseContentLog,
	)
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &dto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}

	if taskErr := relay.ResolveOriginTask(c, relayInfo); taskErr != nil {
		respondTaskError(c, taskErr)
		return
	}

	var result *relay.TaskSubmitResult
	var taskErr *dto.TaskError
	defer func() {
		if taskErr != nil && relayInfo.Billing != nil {
			relayInfo.Billing.Refund(c)
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:        c,
		TokenGroup: relayInfo.TokenGroup,
		ModelName:  relayInfo.OriginModelName,
		Retry:      common.GetPointer(0),
	}

	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		var channel *model.Channel

		if lockedCh, ok := relayInfo.LockedChannel.(*model.Channel); ok && lockedCh != nil {
			channel = lockedCh
			if retryParam.GetRetry() > 0 {
				if setupErr := middleware.SetupContextForSelectedChannel(c, channel, relayInfo.OriginModelName); setupErr != nil {
					taskErr = service.TaskErrorWrapperLocal(setupErr.Err, "setup_locked_channel_failed", http.StatusInternalServerError)
					break
				}
			}
		} else {
			var channelErr *types.NewAPIError
			channel, channelErr = getChannel(c, relayInfo, retryParam)
			if channelErr != nil {
				logger.LogError(c, channelErr.Error())
				taskErr = service.TaskErrorWrapperLocal(channelErr.Err, "get_channel_failed", http.StatusInternalServerError)
				break
			}
		}

		addUsedChannel(c, channel.Id)
		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusRequestEntityTooLarge)
			} else {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusBadRequest)
			}
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)

		result, taskErr = relay.RelayTaskSubmit(c, relayInfo)
		if taskErr == nil {
			break
		}

		if !taskErr.LocalError {
			processChannelError(c,
				relayInfo,
				*types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey,
					common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()),
				types.NewOpenAIError(taskErr.Error, types.ErrorCodeBadResponseStatusCode, taskErr.StatusCode))
		}

		if !shouldRetryTaskRelay(c, channel.Id, taskErr, common.RetryTimes-retryParam.GetRetry()) {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}

	// ── 成功：结算 + 日志 + 插入任务 ──
	if taskErr == nil {
		if settleErr := service.SettleBilling(c, relayInfo, result.Quota); settleErr != nil {
			common.SysError("settle task billing error: " + settleErr.Error())
		}
		service.LogTaskConsumption(c, relayInfo)

		task := model.InitTask(result.Platform, relayInfo)
		task.PrivateData.UpstreamTaskID = result.UpstreamTaskID
		task.PrivateData.BillingSource = relayInfo.BillingSource
		task.PrivateData.SubscriptionId = relayInfo.SubscriptionId
		task.PrivateData.TokenId = relayInfo.TokenId
		task.PrivateData.BillingContext = &model.TaskBillingContext{
			ModelPrice:      relayInfo.PriceData.ModelPrice,
			GroupRatio:      relayInfo.PriceData.GroupRatioInfo.GroupRatio,
			ModelRatio:      relayInfo.PriceData.ModelRatio,
			OtherRatios:     relayInfo.PriceData.OtherRatios,
			OriginModelName: relayInfo.OriginModelName,
			PerCallBilling:  common.StringsContains(constant.TaskPricePatches, relayInfo.OriginModelName) || relayInfo.PriceData.UsePrice,
		}
		task.Quota = result.Quota
		task.Data = result.TaskData
		task.Action = relayInfo.Action
		if insertErr := task.Insert(); insertErr != nil {
			common.SysError("insert task error: " + insertErr.Error())
		}
	}

	if taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

// respondTaskError 统一输出 Task 错误响应（含 429 限流提示改写）
func respondTaskError(c *gin.Context, taskErr *dto.TaskError) {
	if taskErr.StatusCode == http.StatusTooManyRequests {
		taskErr.Message = "当前分组上游负载已饱和，请稍后再试"
	}
	c.JSON(taskErr.StatusCode, taskErr)
}

func shouldRetryTaskRelay(c *gin.Context, channelId int, taskErr *dto.TaskError, retryTimes int) bool {
	if taskErr == nil {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) && !isUpstreamRateLimitError(taskErr.StatusCode) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	if taskErr.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if taskErr.StatusCode == 307 {
		return true
	}
	if taskErr.StatusCode/100 == 5 {
		// 超时不重试
		if operation_setting.IsAlwaysSkipRetryStatusCode(taskErr.StatusCode) {
			return false
		}
		return true
	}
	if taskErr.StatusCode == http.StatusBadRequest {
		return false
	}
	if taskErr.StatusCode == 408 {
		// azure处理超时不重试
		return false
	}
	if taskErr.LocalError {
		return false
	}
	if taskErr.StatusCode/100 == 2 {
		return false
	}
	return true
}
