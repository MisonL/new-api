package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	"github.com/QuantumNous/new-api/setting/model_setting"
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

func beginResponsesStreamRelayAttempt(c *gin.Context, info *relaycommon.RelayInfo) {
	if info == nil || !info.IsStream {
		return
	}
	// StreamStatus is attempt-scoped and uses sync.Once for its terminal
	// reason. A channel retry must not inherit the previous upstream stream's
	// interruption or completion state, so every streaming attempt gets a
	// fresh one -- not only the Responses formats.
	info.StreamStatus = relaycommon.NewStreamStatus()
	// 帧计数与 prelude 标记同样是 attempt 级状态：重试复用同一个 gin context，
	// 上一轮的内容帧不能带进本轮。标记先置为“不安全”，只有本轮渠道在入口
	// 显式声明“只发过前导帧”时才允许换渠道重试；不参与置位的自管渠道因此
	// 不会在写出内容后仍被判定可重试。
	info.ReceivedResponseCount = 0
	if c != nil {
		common.SetContextKey(c, constant.ContextKeyResponsesStreamPreludeRetrySafe, false)
	}
	if info.RelayMode != relayconstant.RelayModeResponses && info.RelayMode != relayconstant.RelayModeResponsesCompact {
		return
	}
	if info.ResponsesStreamObservation == nil {
		info.ResponsesStreamObservation = relaycommon.NewResponsesStreamObservation()
	}
	if info.ResponsesStreamAttemptStarted {
		info.ResponsesStreamObservation.BeginAttempt()
	}
	// A retry reuses the downstream gin context, but each upstream Responses
	// stream has its own sequence-number space. Do not let a failed stream's
	// high-water mark leak into the next stream's generated error event.
	if c != nil {
		common.SetContextKey(c, constant.ContextKeyResponsesStreamSequenceNumber, 0)
	}
	info.ResponsesStreamAttemptStarted = true
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

func finalizeRelayChannelHealthProbe(c *gin.Context, info *relaycommon.RelayInfo, succeeded bool) {
	if succeeded && relayRequestProducedNormalEnd(info) {
		service.MarkChannelHealthProbeSuccess(c, info)
		return
	}
	service.ReleaseChannelHealthProbe(c, info)
}

func relayRequestProducedNormalEnd(info *relaycommon.RelayInfo) bool {
	if info == nil || !info.IsStream {
		return true
	}
	if info.StreamStatus == nil {
		return false
	}
	return info.StreamStatus.IsSuccessfulEnd()
}

// Relay handles OpenAI-compatible relay requests and applies channel retry logic.
func Relay(c *gin.Context, relayFormat types.RelayFormat) {

	requestId := c.GetString(common.RequestIdKey)
	userSetting, _ := common.GetContextKeyType[dto.UserSetting](c, constant.ContextKeyUserSetting)
	//group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	//originalModel := common.GetContextKeyString(c, constant.ContextKeyOriginalModel)

	var (
		newAPIError *types.NewAPIError
		relayInfo   *relaycommon.RelayInfo
		ws          *websocket.Conn
	)

	if relayFormat == types.RelayFormatOpenAIRealtime {
		var err error
		ws, err = upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			helper.WssError(c, ws, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry()).ToOpenAIError())
			return
		}
		// gorilla 默认不限帧大小，不设上限时单帧即可撑爆堆。
		ws.SetReadLimit(relay.RealtimeMaxMessageBytes)
		defer ws.Close()
	}

	defer func() {
		if newAPIError != nil {
			logger.LogError(c, fmt.Sprintf("relay error: %s", common.LocalLogPreview(newAPIError.MaskSensitiveErrorWithStatusCode())))
			newAPIError.SetMessage(common.MessageWithRequestId(newAPIError.Error(), requestId))
			writeRelayErrorResponse(c, relayFormat, relayInfo, ws, newAPIError)
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
	opaqueResponsesReplay, err := detectAndMarkOpaqueResponsesReplay(c, request)
	if err != nil {
		newAPIError = types.NewErrorWithStatusCode(
			fmt.Errorf("detect opaque Responses encrypted replay: %w", err),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
		return
	}
	common.InitPayloadAudit(
		c,
		!opaqueResponsesReplay && userSetting.RecordRequestContentLog,
		!opaqueResponsesReplay && userSetting.RecordResponseContentLog && relayFormat != types.RelayFormatOpenAIRealtime,
	)

	relayInfo, err = relaycommon.GenRelayInfo(c, relayFormat, request, ws)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeGenRelayInfoFailed)
		return
	}
	defer func() {
		finalizeRelayChannelHealthProbe(c, relayInfo, newAPIError == nil)
	}()
	defer func() {
		finalizeResponsesRoutingTrace(c, newAPIError)
	}()
	// The selected channel lease is attempt-scoped. The loop releases it before
	// a retry; this final defer covers early returns and panics as well.
	defer service.ReleaseChannelConcurrency(c)
	service.EnsureResponsesBootstrapRecoveryState(c, relayInfo.IsStream)
	service.ApplyResponsesBootstrapRecoveryMetric(c, relayInfo)

	needSensitiveCheck := !opaqueResponsesReplay && setting.ShouldCheckPromptSensitive()
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
			service.ReleaseChannelConcurrency(c)
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
				logger.LogError(c, common.LocalLogPreview(channelErr.MaskSensitiveErrorWithStatusCode()))
				newAPIError = channelErr
				recordTerminalResponsesRoutingErrorLog(c, relayInfo, channelErr)
				break
			}

			attemptIndex := -1
			if shouldTrackResponsesRouting(relayInfo) {
				attemptIndex = beginResponsesRoutingAttempt(c, channel.Id)
			}
			bodyStorage, bodyErr := common.GetBodyStorage(c)
			if bodyErr != nil {
				// Ensure consistent 413 for oversized bodies even when error occurs later (e.g., retry path)
				if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
					newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
				} else {
					newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
				}
				completeResponsesRoutingAttempt(c, attemptIndex, newAPIError)
				break
			}
			c.Request.Body = io.NopCloser(bodyStorage)

			// Restore the client's own stream intent before this attempt runs.
			// The handlers below derive IsStream from the upstream response
			// (helper.ShouldTreatAsEventStream), and that derivation happens
			// before the status code is even inspected, so a failed channel that
			// answered with an SSE content type would otherwise leave IsStream
			// true for every later channel -- ShouldTreatAsEventStream returns
			// its requestedStream argument unchanged once it is true. A retry
			// would then run a JSON response through the streaming path. The
			// client's intent is recorded once at request setup and never
			// rewritten, so it is the value to restore.
			relayInfo.IsStream = common.GetContextKeyBool(c, constant.ContextKeyIsStream)

			if !priceData.FreeModel && relayInfo.Billing == nil {
				newAPIError = service.PreConsumeBilling(c, priceData.QuotaToPreConsume, relayInfo)
				if newAPIError != nil {
					completeResponsesRoutingAttempt(c, attemptIndex, newAPIError)
					break
				}
			}
			// Record an upstream attempt only after request-body preparation and
			// billing pre-consumption have succeeded. A locally rejected request
			// must not make this channel look as if it already received traffic.
			beginResponsesStreamRelayAttempt(c, relayInfo)
			addUsedChannel(c, channel.Id)
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
				completeResponsesRoutingAttempt(c, attemptIndex, nil)
				if !opaqueResponsesReplay {
					recordResponsesStreamOptionsIncludeUsageCapabilityObservation(c, relayInfo)
					recordResponsesToolCapabilityObservation(c, relayInfo)
				}
				relayInfo.LastError = nil
				return
			}

			newAPIError = service.NormalizeViolationFeeError(newAPIError)
			if !opaqueResponsesReplay {
				observeUpstreamRateLimitRetry(c, channel.Id, newAPIError)
			}
			relayInfo.LastError = newAPIError
			if !opaqueResponsesReplay {
				recordResponsesCapabilityObservation(c, relayInfo, newAPIError)
				recordChannelRequestBodyLimit(c, relayInfo, newAPIError)
			}

			if !opaqueResponsesReplay && shouldFallbackResponsesCompactNativeContext(c, relayInfo, newAPIError) {
				fallbackSnapshot := snapshotResponsesCompactFallbackContext(c)
				c.Set("responses_compact_context_fallback_attempted", true)
				service.MarkResponsesCompactFallbackAttempt(c, relayInfo, service.ResponsesCompactFallbackAttemptContext, nil)
				logger.LogWarn(c, fmt.Sprintf(
					"responses compact context fallback to synthetic summary: channel_id=%d status_code=%d error=%s",
					relayInfo.ChannelMeta.ChannelId,
					newAPIError.StatusCode,
					newAPIError.MaskSensitiveError(),
				))
				newAPIError = retryResponsesCompactSyntheticSummary(c, relayInfo, bodyStorage, newAPIError)
				if newAPIError == nil {
					completeResponsesRoutingAttempt(c, attemptIndex, nil)
					relayInfo.LastError = nil
					return
				}
				restoreResponsesCompactFallbackContext(c, relayInfo, fallbackSnapshot)
				relayInfo.LastError = newAPIError
			}

			if !opaqueResponsesReplay && shouldFallbackResponsesCompactPreviousResponseID(c, relayInfo, newAPIError) {
				fallbackSnapshot := snapshotResponsesCompactFallbackContext(c)
				c.Set("responses_compact_previous_response_id_fallback_attempted", true)
				service.MarkResponsesCompactFallbackAttempt(c, relayInfo, service.ResponsesCompactFallbackAttemptPreviousID, nil)
				setResponsesCompactVisibleOnly(c, true)
				logger.LogWarn(c, fmt.Sprintf(
					"responses compact previous_response_id fallback to synthetic visible-only: channel_id=%d status_code=%d error=%s",
					relayInfo.ChannelMeta.ChannelId,
					newAPIError.StatusCode,
					newAPIError.MaskSensitiveError(),
				))
				newAPIError = retryResponsesCompactSyntheticSummary(c, relayInfo, bodyStorage, newAPIError)
				if newAPIError == nil {
					completeResponsesRoutingAttempt(c, attemptIndex, nil)
					relayInfo.LastError = nil
					return
				}
				restoreResponsesCompactFallbackContext(c, relayInfo, fallbackSnapshot)
				relayInfo.LastError = newAPIError
			}

			if !opaqueResponsesReplay && shouldFallbackResponsesCompactAuto(c, relayInfo, newAPIError) {
				fallbackSnapshot := snapshotResponsesCompactFallbackContext(c)
				c.Set("responses_compact_auto_fallback_attempted", true)
				service.MarkResponsesCompactFallbackAttempt(c, relayInfo, service.ResponsesCompactFallbackAttemptAuto, nil)
				now := time.Now()
				fallbackReason := newAPIError.MaskSensitiveErrorWithStatusCode()
				service.MarkResponsesCompactNativeFallback(c, relayInfo, newAPIError.StatusCode, fallbackReason, now)
				logger.LogWarn(c, fmt.Sprintf(
					"responses compact auto fallback to synthetic summary: channel_id=%d status_code=%d error=%s",
					relayInfo.ChannelMeta.ChannelId,
					newAPIError.StatusCode,
					newAPIError.MaskSensitiveError(),
				))
				newAPIError = retryResponsesCompactSyntheticSummary(c, relayInfo, bodyStorage, newAPIError)
				if newAPIError == nil {
					completeResponsesRoutingAttempt(c, attemptIndex, nil)
					if err := model.MarkResponsesCompactAutoFallback(relayInfo.ChannelMeta.ChannelId, fallbackReason); err != nil {
						logger.LogError(c, fmt.Sprintf("mark responses compact auto fallback failed: %s", err.Error()))
					}
					relayInfo.LastError = nil
					return
				}
				restoreResponsesCompactFallbackContext(c, relayInfo, fallbackSnapshot)
				relayInfo.LastError = newAPIError
			}

			if !opaqueResponsesReplay && shouldFallbackRemoteResponsesCompaction(c, relayInfo, newAPIError) {
				fallbackSnapshot := snapshotResponsesCompactFallbackContext(c)
				common.SetContextKey(c, constant.ContextKeyResponsesRemoteCompactSyntheticFallback, true)
				fallbackReason := newAPIError.MaskSensitiveErrorWithStatusCode()
				service.MarkResponsesCompactNativeFallback(c, relayInfo, newAPIError.StatusCode, fallbackReason, time.Now())
				common.SetContextKey(c, constant.ContextKeyResponsesCompactRouteDecision, "remote_v2_synthetic_recovery")
				common.SetContextKey(c, constant.ContextKeyResponsesCompactFallbackReason, fallbackReason)
				logger.LogWarn(c, fmt.Sprintf(
					"remote responses compaction fallback to synthetic summary: channel_id=%d status_code=%d error=%s",
					relayInfo.ChannelMeta.ChannelId,
					newAPIError.StatusCode,
					newAPIError.MaskSensitiveError(),
				))
				newAPIError = retryResponsesCompactSyntheticSummary(c, relayInfo, bodyStorage, newAPIError)
				if newAPIError == nil {
					completeResponsesRoutingAttempt(c, attemptIndex, nil)
					relayInfo.LastError = nil
					return
				}
				restoreResponsesCompactFallbackContext(c, relayInfo, fallbackSnapshot)
				relayInfo.LastError = newAPIError
			}

			if !opaqueResponsesReplay && shouldFallbackResponsesCompactVisibleOnly(c, relayInfo, newAPIError) {
				fallbackSnapshot := snapshotResponsesCompactFallbackContext(c)
				newAPIError = retryResponsesCompactVisibleOnlySummary(c, relayInfo, bodyStorage, newAPIError)
				if newAPIError == nil {
					completeResponsesRoutingAttempt(c, attemptIndex, nil)
					relayInfo.LastError = nil
					return
				}
				restoreResponsesCompactFallbackContext(c, relayInfo, fallbackSnapshot)
				relayInfo.LastError = newAPIError
			}

			if !opaqueResponsesReplay && shouldFallbackResponsesCompactSummaryModel(c, relayInfo, newAPIError) {
				fallbackSnapshot := snapshotResponsesCompactFallbackContext(c)
				newAPIError = retryResponsesCompactSummaryFallbackModels(c, relayInfo, bodyStorage, newAPIError)
				if newAPIError == nil {
					completeResponsesRoutingAttempt(c, attemptIndex, nil)
					relayInfo.LastError = nil
					return
				}
				restoreResponsesCompactFallbackContext(c, relayInfo, fallbackSnapshot)
				relayInfo.LastError = newAPIError
			}

			recordRelayErrorAttempt(c, newAPIError)
			completeResponsesRoutingAttempt(c, attemptIndex, newAPIError)

			channelError := *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan())
			if opaqueResponsesReplay || shouldSuppressBootstrapRecoveryAutoBan(c, newAPIError) {
				channelError.AutoBan = false
			}
			// Record which channel this attempt ran against before the error is
			// logged, so the log attributes the failure to it rather than to the
			// channel the distributor selected before the retry loop started.
			common.SetContextKey(c, relayAttemptChannelIdKey, channel.Id)
			processChannelError(c, relayInfo, channelError, newAPIError)

			if !shouldRetry(c, newAPIError, common.RetryTimes-retryParam.GetRetry()) {
				break
			}
		}

		if !service.CanContinueResponsesBootstrapRecovery(c, newAPIError) {
			break
		}
		releaseBootstrapRecoveryBilling(c, relayInfo)
		waitErr, canceled := waitForResponsesBootstrapRecoveryProbe(c, relayInfo)
		if canceled {
			if relayInfo != nil && relayInfo.ResponsesBootstrapRecoveryAttempted {
				service.RecordResponsesBootstrapRecovery(
					relayInfo.OriginModelName,
					relayInfo.UsingGroup,
					false,
					relayInfo.ResponsesBootstrapRecoveryWaitMs,
				)
			}
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
		if recordSample := perfmetrics.CaptureRelaySample(relayInfo, false, 0); recordSample != nil {
			gopool.Go(recordSample)
		}
	}
}

func writeRelayErrorResponse(c *gin.Context, relayFormat types.RelayFormat, relayInfo *relaycommon.RelayInfo, ws *websocket.Conn, newAPIError *types.NewAPIError) {
	if c == nil || newAPIError == nil {
		return
	}
	// A streamed request whose upstream failed before the first frame still has
	// SSE headers on the writer. Writing a JSON body under text/event-stream
	// leaves the client parsing SSE and finding a bare object, so drop the SSE
	// headers first. Once a frame has been written the response really is SSE
	// and the error goes out as an SSE event instead (handled below).
	if !c.Writer.Written() {
		helper.ResetEventStreamHeaders(c)
	}
	switch relayFormat {
	case types.RelayFormatOpenAIRealtime:
		helper.WssError(c, ws, newAPIError.ToOpenAIError())
	case types.RelayFormatClaude:
		// 流已经开始后必须用 SSE 错误帧收尾：Content-Type 已是
		// text/event-stream，直接写裸 JSON 会被合规客户端当成未知字段行忽略。
		if relayInfo != nil && relayInfo.IsStream && c.Writer.Written() {
			helper.SetEventStreamHeaders(c)
			if err := helper.ClaudeErrorEvent(c, newAPIError.ToClaudeError()); err != nil {
				logger.LogError(c, fmt.Sprintf("write claude stream error failed: %s", err.Error()))
			}
			return
		}
		c.JSON(newAPIError.StatusCode, gin.H{
			"type":  "error",
			"error": newAPIError.ToClaudeError(),
		})
	default:
		if relayInfo != nil && relayInfo.IsStream &&
			(relayInfo.RelayMode == relayconstant.RelayModeResponses || relayInfo.RelayMode == relayconstant.RelayModeResponsesCompact) &&
			c.Writer.Written() {
			helper.SetEventStreamHeaders(c)
			if err := helper.OpenAIErrorEvent(c, newAPIError.ToOpenAIError()); err != nil {
				logger.LogError(c, fmt.Sprintf("write responses stream error failed: %s", err.Error()))
			}
			return
		}
		if relayInfo != nil && relayInfo.IsStream &&
			relayInfo.RelayMode == relayconstant.RelayModeChatCompletions &&
			relayFormat == types.RelayFormatOpenAI && c.Writer.Written() {
			helper.SetEventStreamHeaders(c)
			if err := helper.OpenAIChatErrorEvent(c, newAPIError.ToOpenAIError()); err != nil {
				logger.LogError(c, fmt.Sprintf("write chat completions stream error failed: %s", err.Error()))
			}
			helper.Done(c)
			return
		}
		if service.ShouldWriteResponsesBootstrapStreamError(c) {
			helper.SetEventStreamHeaders(c)
			service.MarkResponsesBootstrapHeadersSent(c)
			if err := helper.OpenAIErrorEvent(c, newAPIError.ToOpenAIError()); err != nil {
				logger.LogError(c, fmt.Sprintf("write bootstrap stream error failed: %s", err.Error()))
			}
			return
		}
		// Gemini 原生流（以及其它落到 default 的流式格式）同样不能追加裸
		// JSON；以 SSE data 帧下发，保持传输层合法。
		if relayInfo != nil && relayInfo.IsStream && c.Writer.Written() {
			helper.SetEventStreamHeaders(c)
			payload := gin.H{"error": newAPIError.ToOpenAIError()}
			jsonData, marshalErr := common.Marshal(payload)
			if marshalErr != nil {
				logger.LogError(c, fmt.Sprintf("write stream error failed: %s", marshalErr.Error()))
				return
			}
			if err := helper.StringData(c, string(jsonData)); err != nil {
				logger.LogError(c, fmt.Sprintf("write stream error failed: %s", err.Error()))
			}
			return
		}
		c.JSON(newAPIError.StatusCode, gin.H{
			"error": newAPIError.ToOpenAIError(),
		})
	}
}

func detectAndMarkOpaqueResponsesReplay(c *gin.Context, request dto.Request) (bool, error) {
	if c == nil {
		return false, nil
	}
	hasOpaqueReplay, err := requestHasOpaqueResponsesReplay(c, request)
	if err != nil {
		return false, err
	}
	if hasOpaqueReplay {
		common.SetContextKey(c, constant.ContextKeyResponsesOpaqueEncryptedReplay, true)
	}
	return hasOpaqueReplay, nil
}

func requestHasOpaqueResponsesReplay(c *gin.Context, request dto.Request) (bool, error) {
	switch typedRequest := request.(type) {
	case *dto.OpaqueOpenAIResponsesRequest:
		return typedRequest != nil && typedRequest.OpaqueResponsesMetadata().HasOpaqueEncryptedContent, nil
	case *dto.OpenAIResponsesRequest:
		if typedRequest == nil {
			return false, nil
		}
		return service.ResponsesInputHasOpaqueEncryptedContent(relaycommon.GinRequestContext(c), typedRequest.Input)
	case *dto.OpenAIResponsesCompactionRequest:
		if typedRequest == nil {
			return false, nil
		}
		return service.ResponsesInputHasOpaqueEncryptedContent(relaycommon.GinRequestContext(c), typedRequest.Input)
	default:
		return false, nil
	}
}

func releaseBootstrapRecoveryBilling(c *gin.Context, relayInfo *relaycommon.RelayInfo) {
	if relayInfo == nil || relayInfo.Billing == nil {
		return
	}
	relayInfo.ResetBillingMetadata(c)
}

func waitForResponsesBootstrapRecoveryProbe(c *gin.Context, relayInfo *relaycommon.RelayInfo) (*types.NewAPIError, bool) {
	waitDuration, sendPing, ok := service.NextResponsesBootstrapWait(c, time.Now())
	state, _ := service.GetResponsesBootstrapRecoveryState(c)
	if !ok {
		if state != nil {
			logger.LogInfo(c, fmt.Sprintf("responses bootstrap recovery probe deadline reached: attempts=%d waited_ms=%d", state.WaitAttempts, state.WaitDuration.Milliseconds()))
		}
		return nil, false
	}
	service.MarkResponsesBootstrapRecoveryMetric(c)
	service.ApplyResponsesBootstrapRecoveryMetric(c, relayInfo)
	if waitErr, canceled, done := responsesBootstrapProbeContextDone(c, state); done {
		return waitErr, canceled
	}
	if sendPing {
		helper.SetEventStreamHeaders(c)
		service.MarkResponsesBootstrapHeadersSent(c)
		now := time.Now()
		if err := helper.PingData(c); err != nil {
			return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry()), false
		}
		service.MarkResponsesBootstrapPingSent(c, now)
		if state != nil {
			logger.LogInfo(c, fmt.Sprintf("responses bootstrap recovery probe ping: attempts=%d waited_ms=%d", state.WaitAttempts, state.WaitDuration.Milliseconds()))
		}
	}
	timer := time.NewTimer(waitDuration)
	defer timer.Stop()
	select {
	case <-c.Request.Context().Done():
		waitErr, canceled, _ := responsesBootstrapProbeContextDone(c, state)
		return waitErr, canceled
	case <-timer.C:
		if waitErr, canceled, done := responsesBootstrapProbeContextDone(c, state); done {
			return waitErr, canceled
		}
		return nil, false
	}
}

func responsesBootstrapProbeContextDone(c *gin.Context, state *service.ResponsesBootstrapRecoveryState) (*types.NewAPIError, bool, bool) {
	err := c.Request.Context().Err()
	if err == nil {
		return nil, false, false
	}
	if state != nil {
		logger.LogInfo(c, fmt.Sprintf("responses bootstrap recovery probe request context done: attempts=%d waited_ms=%d error=%q", state.WaitAttempts, state.WaitDuration.Milliseconds(), err.Error()))
	}
	if errors.Is(err, context.Canceled) {
		return nil, true, true
	}
	return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry()), false, true
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

const relayInitialChannelConsumedKey = "relay_initial_channel_consumed"
const responsesCompactUnsafeCompactionInputKey = "responses_compact_unsafe_compaction_input"
const relayUpstreamErrorStateKey constant.ContextKey = "relay_upstream_error_state"

// relayAttemptChannelIdKey carries the channel of the attempt currently being
// processed. The distributor writes ContextKeyChannelId exactly once, before the
// retry loop, so it names the initially selected channel and goes stale as soon
// as a retry moves to another one. Error logs and routing trace readers use this
// key to attribute a failure to the channel that actually produced it.
const relayAttemptChannelIdKey constant.ContextKey = "relay_attempt_channel_id"

type relayUpstreamErrorState struct {
	lastModelNotFoundError *types.NewAPIError
	modelNotFoundAttempts  int
	otherFailures          int
}

func isDeterministicModelNotFoundError(err *types.NewAPIError) bool {
	if err == nil || (err.StatusCode != http.StatusBadRequest && err.StatusCode != http.StatusNotFound) {
		return false
	}
	if err.GetErrorType() != types.ErrorTypeOpenAIError {
		return false
	}
	if err.GetErrorCode() == types.ErrorCodeModelNotFound {
		return true
	}

	openAIError := err.ToOpenAIError()
	message := strings.Join(strings.Fields(strings.ToLower(strings.NewReplacer(
		"_", " ",
		"-", " ",
	).Replace(strings.Join([]string{
		err.Error(),
		openAIError.Message,
		fmt.Sprint(openAIError.Code),
	}, " ")))), " ")
	if !strings.Contains(message, "model") {
		return false
	}
	for _, indicator := range []string{
		"unknown model",
		"unrecognized model",
		"model does not exist",
		"no such model",
	} {
		if strings.Contains(message, indicator) {
			return true
		}
	}
	if strings.Contains(message, "not found") {
		for _, routeIndicator := range []string{"endpoint", "route", "url", "path", "method"} {
			if strings.Contains(message, routeIndicator) {
				return false
			}
		}
		return true
	}
	return false
}

func recordRelayErrorAttempt(c *gin.Context, err *types.NewAPIError) {
	if c == nil || err == nil {
		return
	}
	state, ok := common.GetContextKeyType[*relayUpstreamErrorState](c, relayUpstreamErrorStateKey)
	if !ok || state == nil {
		state = &relayUpstreamErrorState{}
		common.SetContextKey(c, relayUpstreamErrorStateKey, state)
	}
	if isDeterministicModelNotFoundError(err) {
		state.modelNotFoundAttempts++
		state.lastModelNotFoundError = err
		return
	}
	state.otherFailures++
}

func terminalRelayErrorAfterChannelExhaustion(c *gin.Context, fallback *types.NewAPIError) *types.NewAPIError {
	if c == nil {
		return fallback
	}
	state, ok := common.GetContextKeyType[*relayUpstreamErrorState](c, relayUpstreamErrorStateKey)
	if !ok || state == nil || state.modelNotFoundAttempts == 0 || state.otherFailures != 0 || state.lastModelNotFoundError == nil {
		return fallback
	}
	logger.LogInfo(c, fmt.Sprintf(
		"preserve deterministic upstream model_not_found after channel exhaustion: attempts=%d status_code=%d error=%s",
		state.modelNotFoundAttempts,
		state.lastModelNotFoundError.StatusCode,
		state.lastModelNotFoundError.MaskSensitiveError(),
	))
	return state.lastModelNotFoundError
}

func addUsedChannel(c *gin.Context, channelId int) {
	useChannel := c.GetStringSlice("use_channel")
	useChannel = append(useChannel, fmt.Sprintf("%d", channelId))
	c.Set("use_channel", useChannel)
}

// acquireSelectedChannelConcurrency reserves the selected channel only after
// all normal routing and compatibility filters have passed. It returns
// (false, nil) when a non-strict route may reselect another channel because
// this one is full. The excluded set is deliberately separate from
// use_channel: no upstream request was sent yet.
func acquireSelectedChannelConcurrency(c *gin.Context, channel *model.Channel, strict bool) (bool, *types.NewAPIError) {
	if channel == nil {
		return false, types.NewError(errors.New("channel is nil"), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if channel.MaxConcurrency == 0 {
		if maxConcurrency := common.GetContextKeyInt(c, constant.ContextKeyChannelMaxConcurrency); maxConcurrency != 0 {
			clone := *channel
			clone.MaxConcurrency = maxConcurrency
			channel = &clone
		}
	}
	lease, err := service.AcquireChannelConcurrency(c, channel)
	if err == nil {
		service.SetChannelConcurrencyLease(c, lease)
		return true, nil
	}
	if errors.Is(err, service.ErrChannelConcurrencyExceeded) && !strict {
		service.RecordChannelConcurrencyReselect()
		service.ExcludeChannelDueToConcurrency(c, channel.Id)
		service.ReleaseChannelHealthProbeForChannel(c, channel.Id)
		return false, nil
	}
	service.ReleaseChannelHealthProbeForChannel(c, channel.Id)
	statusCode := http.StatusServiceUnavailable
	errorCode := types.ErrorCodeChannelConcurrencyUnavailable
	message := fmt.Sprintf("channel #%d concurrency coordinator unavailable", channel.Id)
	if errors.Is(err, service.ErrChannelConcurrencyExceeded) {
		if strict {
			service.RecordChannelConcurrencyStrict429()
		}
		statusCode = http.StatusTooManyRequests
		errorCode = types.ErrorCodeChannelConcurrencyExceeded
		message = fmt.Sprintf("channel #%d concurrency limit reached", channel.Id)
	} else if errors.Is(err, service.ErrChannelConcurrencyInvalid) {
		statusCode = http.StatusInternalServerError
		errorCode = types.ErrorCodeGetChannelFailed
		message = fmt.Sprintf("channel #%d has an invalid concurrency limit", channel.Id)
	}
	return false, types.NewErrorWithStatusCode(errors.New(message), errorCode, statusCode, types.ErrOptionWithSkipRetry())
}

func channelConcurrencyExhaustedError(attempts int) *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		fmt.Errorf("all eligible channels reached their concurrency limit after %d attempts", attempts),
		types.ErrorCodeChannelConcurrencyExceeded,
		http.StatusTooManyRequests,
		types.ErrOptionWithSkipRetry(),
	)
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
	case *dto.OpenAIResponsesCompactionRequest:
		meta.MaxTokens = int(lo.FromPtrOr(r.MaxOutputTokens, uint(0)))
	case *dto.OpaqueOpenAIResponsesRequest:
		meta.MaxTokens = r.GetTokenCountMeta().MaxTokens
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
	if c != nil && retryParam != nil {
		common.SetContextKey(c, constant.ContextKeyChannelHeaderProfileAttempt, retryParam.GetRetry())
	}
	installChannelCandidatePreflight(c, info, retryParam)
	originalRetry := retryParam.GetRetry()
	skippedProtocolIncompatibleChannels := 0
	protocolIncompatiblePriorityAdvances := 0
	skippedProtocolIncompatibleInThisCall := false
	concurrencySkips := 0
	restoreRetryAfterProtocolIncompatibleSkip := func() {
		if protocolIncompatiblePriorityAdvances > 0 {
			retryParam.SetRetry(originalRetry)
			info.RetryIndex = originalRetry
		}
	}
	if shouldUseInitialSelectedChannel(c, info, retryParam) {
		c.Set(relayInitialChannelConsumedKey, true)
		channel := initialSelectedChannelSnapshot(c)
		if channel == nil {
			return nil, types.NewError(errors.New("initial selected channel context is unavailable"), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		if shouldTrackResponsesRouting(info) {
			recordResponsesRoutingCandidate(c, channel.Id)
		}
		if _, specified := c.Get("specific_channel_id"); !specified {
			decision := service.EvaluateChannelHealthSelectionForChannel(c, info.UsingGroup, info.OriginModelName, channel)
			if !decision.Allow {
				if !service.ExcludeChannelHealthSelectionForRoute(c, decision.Group, decision.ModelName, decision.Route) {
					return nil, types.NewError(errors.New("channel health selection could not exclude unavailable initial channel"), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
				}
				service.MarkChannelHealthSelectionRetryableForRoute(c, decision.Group, decision.ModelName, decision.Route)
			} else {
				skipChannel, skipErr := evaluateChannelCandidatePreflight(c, info, retryParam, channel)
				if skipErr != nil {
					service.ReleaseChannelHealthProbeForChannel(c, channel.Id)
					return nil, newResponsesCompactionInputError(skipErr)
				}
				if skipChannel {
					service.ReleaseChannelHealthProbeForChannel(c, channel.Id)
					addUsedChannel(c, channel.Id)
					skippedProtocolIncompatibleChannels++
					if isProtocolIncompatibleSkip(info.LastError) {
						skippedProtocolIncompatibleInThisCall = true
					}
				} else {
					// The distributor may have selected a concrete multi-key
					// credential after its pre-dispatch health check. Re-check
					// with the key index now present in the request context.
					decision := service.EvaluateChannelHealthSelectionForChannel(c, info.UsingGroup, info.OriginModelName, channel)
					if !decision.Allow {
						service.ReleaseChannelHealthProbeForChannel(c, channel.Id)
						if !service.ExcludeChannelHealthSelectionForRoute(c, decision.Group, decision.ModelName, decision.Route) {
							return nil, types.NewError(errors.New("channel health selection could not exclude credential-specific initial channel"), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
						}
						service.MarkChannelHealthSelectionRetryableForRoute(c, decision.Group, decision.ModelName, decision.Route)
						addUsedChannel(c, channel.Id)
						// The initial channel is excluded for this route. Fall through
						// to the normal retry selection loop below.
					} else {
						restoreRetryAfterProtocolIncompatibleSkip()
						strict := false
						if _, specified := c.Get("specific_channel_id"); specified {
							strict = true
						} else if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
							strict = true
						}
						acquired, concurrencyErr := acquireSelectedChannelConcurrency(c, channel, strict)
						if concurrencyErr != nil {
							return nil, concurrencyErr
						}
						if !acquired {
							concurrencySkips++
							if concurrencySkips >= service.ChannelConcurrencyReselectLimit() {
								return nil, channelConcurrencyExhaustedError(concurrencySkips)
							}
							// The initial candidate is outside the selector loop; its
							// capacity exclusion is carried into the loop below. It was
							// never sent upstream, so it must not enter use_channel.
						}
						if acquired {
							return channel, nil
						}
					}
				}
			}
		} else {
			skipChannel, skipErr := evaluateChannelCandidatePreflight(c, info, retryParam, channel)
			if skipErr != nil {
				service.ReleaseChannelHealthProbeForChannel(c, channel.Id)
				return nil, newResponsesCompactionInputError(skipErr)
			}
			if skipChannel {
				return nil, unsupportedResponsesToChatChannelError(channel, info.LastError)
			}
			restoreRetryAfterProtocolIncompatibleSkip()
			acquired, concurrencyErr := acquireSelectedChannelConcurrency(c, channel, true)
			if concurrencyErr != nil {
				return nil, concurrencyErr
			}
			if !acquired {
				return nil, channelConcurrencyExhaustedError(1)
			}
			return channel, nil
		}
	}
	for {
		info.RetryIndex = retryParam.GetRetry()
		channel, selectGroup, err := service.CacheGetRandomSatisfiedChannel(retryParam)

		info.PriceData.GroupRatioInfo = helper.HandleGroupRatio(c, info)

		if err != nil {
			restoreRetryAfterProtocolIncompatibleSkip()
			var preflightErr *model.CandidatePreflightError
			if errors.As(err, &preflightErr) {
				return nil, newResponsesCompactionInputError(preflightErr)
			}
			return nil, types.NewError(fmt.Errorf("获取分组 %s 下模型 %s 的可用渠道失败（retry）: %s", selectGroup, info.OriginModelName, err.Error()), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		if channel == nil {
			if protocolIncompatiblePriorityAdvances < skippedProtocolIncompatibleChannels {
				resetAutoGroupSelectionProgress(c)
				retryParam.IncreaseRetry()
				protocolIncompatiblePriorityAdvances++
				continue
			}
			if (skippedProtocolIncompatibleInThisCall || hasResponsesRoutingPreflightSkip(c)) && info.LastError != nil && isProtocolIncompatibleSkip(info.LastError) {
				restoreRetryAfterProtocolIncompatibleSkip()
				return nil, info.LastError
			}
			if concurrencySkips > 0 {
				restoreRetryAfterProtocolIncompatibleSkip()
				return nil, channelConcurrencyExhaustedError(concurrencySkips)
			}
			restoreRetryAfterProtocolIncompatibleSkip()
			return nil, terminalRelayErrorAfterChannelExhaustion(c, types.NewError(
				errors.New(formatNoAvailableChannelErrorMessage(selectGroup, info.OriginModelName, info.LastError)),
				types.ErrorCodeGetChannelFailed,
				types.ErrOptionWithSkipRetry(),
			))
		}
		if shouldTrackResponsesRouting(info) {
			recordResponsesRoutingCandidate(c, channel.Id)
		}
		skipChannel, skipErr := evaluateChannelCandidatePreflight(c, info, retryParam, channel)
		if skipErr != nil {
			service.ReleaseChannelHealthProbeForChannel(c, channel.Id)
			restoreRetryAfterProtocolIncompatibleSkip()
			return nil, newResponsesCompactionInputError(skipErr)
		}
		if skipChannel {
			service.ReleaseChannelHealthProbeForChannel(c, channel.Id)
			addUsedChannel(c, channel.Id)
			skippedProtocolIncompatibleChannels++
			if isProtocolIncompatibleSkip(info.LastError) {
				skippedProtocolIncompatibleInThisCall = true
			}
			continue
		}

		newAPIError := middleware.SetupContextForSelectedChannel(c, channel, info.OriginModelName)
		if newAPIError != nil {
			service.ReleaseChannelHealthProbeForChannel(c, channel.Id)
			if newAPIError.GetErrorCode() == types.ErrorCodeChannelNoAvailableKey {
				service.ExcludeChannelHealthSelection(c, selectGroup, info.OriginModelName, channel.Id)
				addUsedChannel(c, channel.Id)
				continue
			}
			restoreRetryAfterProtocolIncompatibleSkip()
			return nil, newAPIError
		}
		info.InitChannelMeta(c)
		// SetupContextForSelectedChannel chooses the concrete credential for a
		// multi-key channel. Re-evaluate the route with that key before allowing
		// the relay to send upstream; a cooldown for one key must not be treated
		// as a cooldown for the whole channel.
		if _, specified := c.Get("specific_channel_id"); !specified {
			decision := service.EvaluateChannelHealthSelectionForChannel(c, selectGroup, info.OriginModelName, channel)
			if !decision.Allow {
				service.ReleaseChannelHealthProbeForChannel(c, channel.Id)
				if !service.ExcludeChannelHealthSelectionForRoute(c, decision.Group, decision.ModelName, decision.Route) {
					restoreRetryAfterProtocolIncompatibleSkip()
					return nil, types.NewError(errors.New("channel health selection could not exclude credential-specific channel"), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
				}
				service.MarkChannelHealthSelectionRetryableForRoute(c, decision.Group, decision.ModelName, decision.Route)
				addUsedChannel(c, channel.Id)
				continue
			}
		}
		acquired, concurrencyErr := acquireSelectedChannelConcurrency(c, channel, false)
		if concurrencyErr != nil {
			restoreRetryAfterProtocolIncompatibleSkip()
			return nil, concurrencyErr
		}
		if !acquired {
			concurrencySkips++
			if concurrencySkips >= service.ChannelConcurrencyReselectLimit() {
				restoreRetryAfterProtocolIncompatibleSkip()
				return nil, channelConcurrencyExhaustedError(concurrencySkips)
			}
			// Capacity exclusion is request-local and distinct from the
			// upstream-attempt history in use_channel.
			continue
		}
		restoreRetryAfterProtocolIncompatibleSkip()
		return channel, nil
	}
}

type responsesCandidatePreflightKey struct {
	channelID    int
	attemptIndex int
}

type responsesCandidatePreflightResult struct {
	allowed bool
	err     error
}

type responsesCandidatePreflightMemo struct {
	base        func(*model.Channel) (bool, error)
	initialized bool
	results     map[responsesCandidatePreflightKey]responsesCandidatePreflightResult
}

// installChannelCandidatePreflight makes request-shape compatibility part of
// channel eligibility. The selector invokes this before priority and weight.
// Results are memoized per channel and retry attempt: a round-robin Header
// Profile selection is attempt-scoped, so reusing a previous attempt's result
// can approve or skip a channel whose actual profile has changed.
func installChannelCandidatePreflight(c *gin.Context, info *relaycommon.RelayInfo, retryParam *service.RetryParam) {
	if retryParam == nil || info == nil || !shouldTrackResponsesRouting(info) {
		return
	}
	compatibilityPreflight := func(channel *model.Channel) (bool, error) {
		skip, err := shouldSkipChannelForResponsesToChatCompatibility(c, info, channel)
		return !skip, err
	}
	var memo *responsesCandidatePreflightMemo
	if c != nil {
		memo, _ = common.GetContextKeyType[*responsesCandidatePreflightMemo](c, constant.ContextKeyResponsesCandidatePreflightCache)
		if memo == nil {
			memo = &responsesCandidatePreflightMemo{results: make(map[responsesCandidatePreflightKey]responsesCandidatePreflightResult)}
			common.SetContextKey(c, constant.ContextKeyResponsesCandidatePreflightCache, memo)
		}
	}
	if memo == nil {
		// A nil context is only used by low-level tests/callers. Preserve any
		// caller-provided preflight without introducing request-local state.
		previous := retryParam.CandidatePreflight
		retryParam.CandidatePreflight = func(channel *model.Channel) (bool, error) {
			if previous != nil {
				allowed, err := previous(channel)
				if err != nil || !allowed {
					return allowed, err
				}
			}
			return compatibilityPreflight(channel)
		}
		return
	}
	if !memo.initialized {
		memo.base = retryParam.CandidatePreflight
		memo.initialized = true
	}
	retryParam.CandidatePreflight = func(channel *model.Channel) (bool, error) {
		if channel == nil {
			return false, nil
		}
		key := responsesCandidatePreflightKey{channelID: channel.Id, attemptIndex: retryParam.GetRetry()}
		if result, ok := memo.results[key]; ok {
			return result.allowed, result.err
		}
		allowed := true
		var err error
		if memo.base != nil {
			allowed, err = memo.base(channel)
		}
		if err == nil && allowed {
			allowed, err = compatibilityPreflight(channel)
		}
		memo.results[key] = responsesCandidatePreflightResult{allowed: allowed, err: err}
		return allowed, err
	}
}

func evaluateChannelCandidatePreflight(c *gin.Context, info *relaycommon.RelayInfo, retryParam *service.RetryParam, channel *model.Channel) (bool, error) {
	if retryParam != nil && retryParam.CandidatePreflight != nil {
		allowed, err := retryParam.CandidatePreflight(channel)
		return !allowed, err
	}
	return shouldSkipChannelForResponsesToChatCompatibility(c, info, channel)
}

func hasResponsesRoutingPreflightSkip(c *gin.Context) bool {
	if c == nil {
		return false
	}
	skips, ok := common.GetContextKeyType[[]dto.ResponsesRoutingChannelSkip](c, constant.ContextKeyResponsesRoutingChannelSkips)
	return ok && len(skips) > 0
}

func resetAutoGroupSelectionProgress(c *gin.Context) {
	if c == nil {
		return
	}
	delete(c.Keys, string(constant.ContextKeyAutoGroupIndex))
	delete(c.Keys, string(constant.ContextKeyAutoGroupRetryIndex))
}

func shouldUseInitialSelectedChannel(c *gin.Context, info *relaycommon.RelayInfo, retryParam *service.RetryParam) bool {
	if c == nil || info == nil || retryParam == nil {
		return false
	}
	return info.ChannelMeta == nil &&
		retryParam.GetRetry() == 0 &&
		!c.GetBool(relayInitialChannelConsumedKey) &&
		common.GetContextKeyInt(c, constant.ContextKeyChannelId) > 0
}

// initialSelectedChannelSnapshot returns the exact channel selected by the
// distributor/setup path. Older internal callers may only install primitive
// context fields, so retain a cache/database compatibility lookup and a final
// context projection. The projection is deliberately never passed to setup:
// its key, model mapping, and settings have already been installed for this
// request and must not be replaced by a second live credential selection.
func initialSelectedChannelSnapshot(c *gin.Context) *model.Channel {
	if c == nil {
		return nil
	}
	if snapshot, ok := common.GetContextKeyType[*model.Channel](c, constant.ContextKeySelectedChannelSnapshot); ok && snapshot != nil && snapshot.Id > 0 {
		return snapshot.CloneForCache()
	}

	channelID := common.GetContextKeyInt(c, constant.ContextKeyChannelId)
	if channelID <= 0 {
		return nil
	}
	if channel, err := model.CacheGetChannel(channelID); err == nil && channel != nil {
		return channel
	}

	// The channel may have been deleted after Distributor completed setup. The
	// request still owns a valid, already-installed route context; preserve it
	// for the in-flight request instead of selecting a different channel.
	autoBan := 1
	if !common.GetContextKeyBool(c, constant.ContextKeyChannelAutoBan) {
		autoBan = 0
	}
	channel := &model.Channel{
		Id:             channelID,
		Type:           common.GetContextKeyInt(c, constant.ContextKeyChannelType),
		Name:           common.GetContextKeyString(c, constant.ContextKeyChannelName),
		Key:            common.GetContextKeyString(c, constant.ContextKeyChannelKey),
		Status:         common.ChannelStatusEnabled,
		AutoBan:        &autoBan,
		MaxConcurrency: common.GetContextKeyInt(c, constant.ContextKeyChannelMaxConcurrency),
	}
	if mapping := common.GetContextKeyString(c, constant.ContextKeyChannelModelMapping); mapping != "" {
		channel.ModelMapping = common.GetPointer(mapping)
	}
	if baseURL := common.GetContextKeyString(c, constant.ContextKeyChannelBaseUrl); baseURL != "" {
		channel.BaseURL = common.GetPointer(baseURL)
	}
	if common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey) {
		channel.ChannelInfo.IsMultiKey = true
		channel.ChannelInfo.MultiKeyPollingIndex = common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
	}
	return channel
}

func isProtocolIncompatibleSkip(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	return err.GetErrorCode() == types.ErrorCodeChannelModelMappedError &&
		err.StatusCode == http.StatusServiceUnavailable &&
		(errors.Is(err, errResponsesNativeCompactionUnsupported) ||
			errors.Is(err, errResponsesNativeToolIdentityUnsupported) ||
			errors.Is(err, relaycommon.ErrResponsesEncryptedReasoningReplayUnsupported) ||
			errors.Is(err, relaycommon.ErrResponsesOpaqueReplayParamOverrideUnsupported) ||
			errors.Is(err, errResponsesCodexDynamicHeadersUnsupported) ||
			errors.Is(err, errResponsesToChatIncompatible) ||
			errors.Is(err, errResponsesFunctionToolsUnsupported) ||
			errors.Is(err, errResponsesCustomToolsUnsupported) ||
			errors.Is(err, errResponsesNamespaceToolsUnsupported) ||
			errors.Is(err, errResponsesToolSearchUnsupported))
}

func shouldSkipChannelForResponsesToChatCompatibility(c *gin.Context, info *relaycommon.RelayInfo, channel *model.Channel) (bool, error) {
	if c == nil || c.Request == nil || info == nil || channel == nil ||
		(info.RelayMode != relayconstant.RelayModeResponses && info.RelayMode != relayconstant.RelayModeResponsesCompact) {
		return false, nil
	}
	var request *dto.OpenAIResponsesRequest
	var opaqueMetadata common.ResponsesRequestMetadata
	isOpaqueReplayRequest := false
	switch typedRequest := info.Request.(type) {
	case *dto.OpaqueOpenAIResponsesRequest:
		if typedRequest != nil {
			opaqueMetadata = typedRequest.OpaqueResponsesMetadata()
			isOpaqueReplayRequest = opaqueMetadata.HasOpaqueEncryptedContent
		}
	case *dto.OpenAIResponsesRequest:
		request = typedRequest
	case *dto.OpenAIResponsesCompactionRequest:
		if typedRequest != nil {
			request = typedRequest.ToResponsesRequest()
		}
	}
	if request == nil && !isOpaqueReplayRequest {
		return false, nil
	}
	if missingDynamicHeaders := missingResponsesCodexDynamicHeaders(c, info, channel); len(missingDynamicHeaders) > 0 {
		markResponsesCompatibilityChannelSkip(
			c,
			info,
			channel,
			responsesRoutingChannelSkipReasonUnsupportedCodexDynamicHeaders,
			unsupportedResponsesCodexDynamicHeadersChannelError(channel, missingDynamicHeaders),
		)
		return true, nil
	}
	hasOpaqueEncryptedContent := isOpaqueReplayRequest
	if !isOpaqueReplayRequest {
		if request == nil {
			return false, nil
		}
		var opaqueErr error
		hasOpaqueEncryptedContent, opaqueErr = service.ResponsesInputHasOpaqueEncryptedContent(c.Request.Context(), request.Input)
		if opaqueErr != nil {
			return false, opaqueErr
		}
	}
	channelOtherSettings := responsesCompactionChannelOtherSettings(c, info, channel)
	if isOpaqueReplayRequest {
		capability := channelOtherSettings.ResolveResponsesEncryptedReasoningReplayCapability(channel.Type)
		relaycommon.MarkResponsesEncryptedReasoningReplayDecision(c, capability)
		if capability == dto.ResponsesEncryptedReasoningReplayCapabilityUnsupported {
			markResponsesCompatibilityChannelSkip(
				c,
				info,
				channel,
				responsesRoutingChannelSkipReasonUnsupportedEncryptedReasoningReplay,
				relaycommon.NewResponsesEncryptedReasoningReplayUnsupportedError(channel.Id),
			)
			return true, nil
		}
		if info.RelayMode == relayconstant.RelayModeResponsesCompact &&
			!relaycommon.SupportsNativeOpaqueResponsesCompact(channel.Type, channelOtherSettings) {
			relaycommon.MarkResponsesEncryptedReasoningReplayRejected(c)
			markResponsesCompactionChannelSkip(c, info, channel)
			return true, nil
		}
		if opaqueMetadata.HasCompactionItem && responsesCompactionChannelCannotPassthroughItems(channelOtherSettings, channel.Type) {
			relaycommon.MarkResponsesEncryptedReasoningReplayRejected(c)
			markResponsesCompactionChannelSkip(c, info, channel)
			return true, nil
		}
		if err := relaycommon.ValidateOpaqueResponsesReplayParamOverride(opaqueResponsesReplayParamOverride(c, channel)); err != nil {
			relaycommon.MarkResponsesEncryptedReasoningReplayRejected(c)
			markResponsesCompatibilityChannelSkip(
				c,
				info,
				channel,
				responsesRoutingChannelSkipReasonOpaqueReplayParamOverride,
				relaycommon.NewResponsesOpaqueReplayParamOverrideUnsupportedError(channel.Id),
			)
			return true, nil
		}
		rule, err := service.FindUnconditionalResponsesViaChatRule(channel.Id, channel.Type, info.OriginModelName)
		if err != nil {
			return false, err
		}
		if rule != nil {
			relaycommon.MarkResponsesEncryptedReasoningReplayRejected(c)
			markResponsesCompatibilityChannelSkip(
				c,
				info,
				channel,
				responsesRoutingChannelSkipReasonUnsupportedEncryptedReasoningReplay,
				relaycommon.NewResponsesEncryptedReasoningReplayUnsupportedError(channel.Id),
			)
			return true, nil
		}
		return false, nil
	}
	if hasOpaqueEncryptedContent {
		capability := channelOtherSettings.ResolveResponsesEncryptedReasoningReplayCapability(channel.Type)
		relaycommon.MarkResponsesEncryptedReasoningReplayDecision(c, capability)
		if channelOtherSettings.ExplicitlyDisablesResponsesEncryptedReasoningReplay(channel.Type) {
			markResponsesCompatibilityChannelSkip(
				c,
				info,
				channel,
				responsesRoutingChannelSkipReasonUnsupportedEncryptedReasoningReplay,
				relaycommon.NewResponsesEncryptedReasoningReplayUnsupportedError(channel.Id),
			)
			return true, nil
		}
		if info.RelayMode == relayconstant.RelayModeResponsesCompact &&
			!relaycommon.SupportsNativeOpaqueResponsesCompact(channel.Type, channelOtherSettings) {
			markResponsesCompactionChannelSkip(c, info, channel)
			return true, nil
		}
		if info.RelayMode == relayconstant.RelayModeResponses {
			hasRemoteCompaction, err := service.HasRemoteResponsesCompactionInput(c.Request.Context(), *request)
			if err != nil {
				return false, err
			}
			if hasRemoteCompaction && responsesCompactionChannelCannotPassthroughItems(channelOtherSettings, channel.Type) {
				markResponsesCompactionChannelSkip(c, info, channel)
				return true, nil
			}
		}
		if err := relaycommon.ValidateOpaqueResponsesReplayParamOverride(opaqueResponsesReplayParamOverride(c, channel)); err != nil {
			relaycommon.MarkResponsesEncryptedReasoningReplayRejected(c)
			markResponsesCompatibilityChannelSkip(
				c,
				info,
				channel,
				responsesRoutingChannelSkipReasonOpaqueReplayParamOverride,
				relaycommon.NewResponsesOpaqueReplayParamOverrideUnsupportedError(channel.Id),
			)
			return true, nil
		}
	}
	if info.RelayMode == relayconstant.RelayModeResponsesCompact {
		return false, nil
	}
	if nativeToolIdentity := codexLiteNativeToolIdentityDecisionFor(c, request); nativeToolIdentity.required &&
		!(model_setting.GlobalSettingsSnapshot().PassThroughRequestEnabled || nativeCompactionChannelSetting(c, info, channel).PassThroughBodyEnabled) {
		rule, err := service.FindResponsesViaChatRule(
			c.Request.Context(),
			info.RelayMode,
			model_setting.GlobalSettingsSnapshot().PassThroughRequestEnabled,
			nativeCompactionChannelSetting(c, info, channel),
			channel.Id,
			channel.Type,
			info.OriginModelName,
			request,
			responsesCompactionChannelOtherSettings(c, info, channel),
		)
		if err != nil {
			return false, err
		}
		if rule != nil {
			reason := responsesRoutingChannelSkipReasonRequiresNativeResponsesToolIdentity
			if nativeToolIdentity.unparseable {
				reason = responsesRoutingChannelSkipReasonUnparseableNativeResponsesToolIdentity
			}
			markResponsesCompatibilityChannelSkip(
				c,
				info,
				channel,
				reason,
				unsupportedResponsesNativeToolIdentityChannelError(channel),
			)
			return true, nil
		}
	}
	hasFunctionTools, err := relaycommon.ResponsesRequestHasFunctionTools(request)
	if err != nil {
		return false, err
	}
	hasNamespaceTools, err := relaycommon.ResponsesRequestHasNamespaceTools(request)
	if err != nil {
		return false, err
	}
	hasToolSearch, err := relaycommon.ResponsesRequestHasToolSearch(request)
	if err != nil {
		return false, err
	}
	hasCustomTools, err := relaycommon.ResponsesRequestHasCustomTools(request)
	if err != nil {
		return false, err
	}
	functionToolsUnsupported := hasFunctionTools && channelOtherSettings.ExplicitlyDisablesResponsesFunctionTools()
	customToolsUnsupported := hasCustomTools && channelOtherSettings.ExplicitlyDisablesResponsesCustomTools()
	// Namespace and tool-search are not safely convertible through the generic
	// Chat bridge. Only an explicit unsupported capability is a channel-level
	// preflight skip. Unknown capability remains best-effort: the upstream may
	// support the request, and a real upstream error can still trigger retry and
	// automatic channel selection.
	namespaceToolsUnsupported := hasNamespaceTools &&
		channelOtherSettings.ResolveResponsesNamespaceToolsCapability(channel.Type) == dto.ResponsesToolCapabilityUnsupported
	toolSearchUnsupported := hasToolSearch &&
		channelOtherSettings.ResolveResponsesToolSearchCapability(channel.Type) == dto.ResponsesToolCapabilityUnsupported
	unsupportedTools := responsesUnsupportedToolCapabilities{
		function:   functionToolsUnsupported,
		custom:     customToolsUnsupported,
		namespace:  namespaceToolsUnsupported,
		toolSearch: toolSearchUnsupported,
	}
	if model_setting.GlobalSettingsSnapshot().PassThroughRequestEnabled ||
		nativeCompactionChannelSetting(c, info, channel).PassThroughBodyEnabled {
		if skipExplicitlyUnsupportedResponsesTools(c, info, channel, unsupportedTools) {
			return true, nil
		}
		return false, nil
	}
	// Opaque encrypted state must stay on a native Responses path once the
	// channel has passed the replay and compaction passthrough checks above.
	// A Responses-to-Chat rule is not a reason to divert this request: the
	// conversion cannot preserve the opaque item and would make a capable
	// native channel look incompatible.
	if hasOpaqueEncryptedContent {
		if skipExplicitlyUnsupportedResponsesTools(c, info, channel, unsupportedTools) {
			return true, nil
		}
		return false, nil
	}
	hasLocalCompactionTrigger := request.HasCompactionTrigger()
	hasRemoteCompaction, err := service.HasRemoteResponsesCompactionInput(c.Request.Context(), *request)
	if err != nil {
		return false, err
	}
	if hasRemoteCompaction {
		channelOtherSettings := responsesCompactionChannelOtherSettings(c, info, channel)
		if responsesCompactionChannelCannotPassthroughItems(channelOtherSettings, channel.Type) {
			markResponsesCompactionChannelSkip(c, info, channel)
			return true, nil
		}
		rule, err := service.FindUnconditionalResponsesViaChatRule(
			channel.Id,
			channel.Type,
			info.OriginModelName,
		)
		if err != nil {
			return false, err
		}
		if rule != nil && responsesCompactionRuleRequiresChannelSkip(c, info, rule, channel) {
			markResponsesCompactionChannelSkip(c, info, channel)
			return true, nil
		}
		if skipExplicitlyUnsupportedResponsesTools(c, info, channel, unsupportedTools) {
			return true, nil
		}
		return false, nil
	}
	hasLocalNativeOpaque, err := service.HasLocalNativeOpaqueCompactReference(c.Request.Context(), *request)
	if err != nil {
		return false, err
	}
	if hasLocalNativeOpaque {
		channelOtherSettings := responsesCompactionChannelOtherSettings(c, info, channel)
		if !channelOtherSettings.ResolveResponsesChannelCapability(channel.Type).SupportsRestPreviousResponseID {
			markResponsesCompactionChannelSkip(c, info, channel)
			return true, nil
		}
		if skipExplicitlyUnsupportedResponsesTools(c, info, channel, unsupportedTools) {
			return true, nil
		}
		return false, nil
	}
	if hasLocalCompactionTrigger {
		channelOtherSettings := responsesCompactionChannelOtherSettings(c, info, channel)
		if channelOtherSettings.HasResponsesProxyCompatibilityProfile() {
			markResponsesCompactionChannelSkip(c, info, channel)
			return true, nil
		}
		rule, err := service.FindUnconditionalResponsesViaChatRule(
			channel.Id,
			channel.Type,
			info.OriginModelName,
		)
		if err != nil {
			return false, err
		}
		if rule != nil && responsesLocalCompactionTriggerRuleRequiresChannelSkip(c, info, rule, channel) {
			markResponsesCompactionChannelSkip(c, info, channel)
			return true, nil
		}
		if skipExplicitlyUnsupportedResponsesTools(c, info, channel, unsupportedTools) {
			return true, nil
		}
		return false, nil
	}
	rule, err := service.FindResponsesViaChatRule(
		c.Request.Context(),
		info.RelayMode,
		model_setting.GlobalSettingsSnapshot().PassThroughRequestEnabled,
		nativeCompactionChannelSetting(c, info, channel),
		channel.Id,
		channel.Type,
		info.OriginModelName,
		request,
		responsesCompactionChannelOtherSettings(c, info, channel),
	)
	if err != nil {
		return false, err
	}
	if rule == nil {
		if skipExplicitlyUnsupportedResponsesTools(c, info, channel, unsupportedTools) {
			return true, nil
		}
		return false, nil
	}
	options := service.ResponsesViaChatRuleOptions(rule)
	compatibilityRequest, err := responsesToChatCompatibilityRequestForPrecheck(c, info, request)
	if err != nil {
		if errors.Is(err, service.ErrResponsesNativeOpaqueStateNotRestorable) {
			markResponsesCompactionChannelSkip(c, info, channel)
			return true, nil
		}
		return false, err
	}
	// A Responses-to-Chat bridge can represent function tools regardless of
	// the native Responses capability snapshot. Namespace, custom, and
	// tool-search declarations are bridgeable only when the explicit bridge
	// option is enabled. Defer namespace classification to the structural
	// helper below so malformed declarations still reach the normal validator.
	bridgeUnsupportedTools := unsupportedTools
	bridgeUnsupportedTools.function = false
	bridgeUnsupportedTools.namespace = false
	if options.EnableCustomToolBridge {
		bridgeUnsupportedTools.custom = false
		bridgeUnsupportedTools.toolSearch = false
	}
	if skipExplicitlyUnsupportedResponsesTools(c, info, channel, bridgeUnsupportedTools) {
		return true, nil
	}
	if hasNamespaceTools {
		unbridgeable, err := service.ResponsesRequestHasUnbridgeableNamespaceTools(compatibilityRequest, options)
		if err != nil {
			return false, err
		}
		if unbridgeable {
			skipResponsesNamespaceToolsWithoutBridge(c, info, channel)
			return true, nil
		}
	}
	if err := service.ValidateResponsesRequestToChatCompatibility(compatibilityRequest, options); err != nil {
		markResponsesCompatibilityChannelSkip(
			c,
			info,
			channel,
			responsesRoutingChannelSkipReasonResponsesToChatIncompatible,
			unsupportedResponsesToChatConversionError(channel, err),
		)
		return true, nil
	}
	return false, nil
}

type responsesUnsupportedToolCapabilities struct {
	function   bool
	custom     bool
	namespace  bool
	toolSearch bool
}

const (
	responsesRoutingChannelSkipReasonUnsupportedCompaction                  = "unsupported_compaction"
	responsesRoutingChannelSkipReasonUnsupportedEncryptedReasoningReplay    = "unsupported_encrypted_reasoning_replay"
	responsesRoutingChannelSkipReasonOpaqueReplayParamOverride              = "incompatible_opaque_replay_param_override"
	responsesRoutingChannelSkipReasonUnsupportedCodexDynamicHeaders         = "unsupported_codex_dynamic_headers"
	responsesRoutingChannelSkipReasonRequiresNativeResponsesToolIdentity    = "requires_native_responses_tool_identity"
	responsesRoutingChannelSkipReasonUnparseableNativeResponsesToolIdentity = "requires_native_responses_tool_identity_unparseable"
	responsesRoutingChannelSkipReasonUnsupportedFunctionTools               = "unsupported_function_tools"
	responsesRoutingChannelSkipReasonUnsupportedCustomTools                 = "unsupported_custom_tools"
	responsesRoutingChannelSkipReasonUnsupportedNamespaceTools              = "unsupported_namespace_tools"
	responsesRoutingChannelSkipReasonUnsupportedToolSearch                  = "unsupported_tool_search"
	responsesRoutingChannelSkipReasonResponsesToChatIncompatible            = "responses_to_chat_incompatible"
)

func skipExplicitlyUnsupportedResponsesTools(c *gin.Context, info *relaycommon.RelayInfo, channel *model.Channel, unsupported responsesUnsupportedToolCapabilities) bool {
	skipped := false
	var firstError *types.NewAPIError
	if unsupported.function {
		if skipUnsupportedResponsesFunctionTools(c, info, channel) {
			skipped = true
			if firstError == nil && info != nil {
				firstError = info.LastError
			}
		}
	}
	if unsupported.custom {
		if skipUnsupportedResponsesCustomTools(c, info, channel) {
			skipped = true
			if firstError == nil && info != nil {
				firstError = info.LastError
			}
		}
	}
	if unsupported.namespace {
		if skipUnsupportedResponsesNamespaceTools(c, info, channel) {
			skipped = true
			if firstError == nil && info != nil {
				firstError = info.LastError
			}
		}
	}
	if unsupported.toolSearch {
		if skipUnsupportedResponsesToolSearch(c, info, channel) {
			skipped = true
			if firstError == nil && info != nil {
				firstError = info.LastError
			}
		}
	}
	if firstError != nil && info != nil {
		info.LastError = firstError
	}
	return skipped
}

func missingResponsesCodexDynamicHeaders(c *gin.Context, info *relaycommon.RelayInfo, channel *model.Channel) []string {
	if c == nil || info == nil || channel == nil {
		return nil
	}
	settings := responsesCompactionChannelOtherSettings(c, info, channel)
	if settings.AllowMissingCodexDynamicRequestHeaders != nil && *settings.AllowMissingCodexDynamicRequestHeaders {
		// Channel-level opt-out. The routing preflight requires at least one
		// x-codex-* value because a Sub2API upstream refuses clients that cannot
		// prove Codex identity; a channel that does not sit behind such a gate
		// would otherwise be marked unusable for every non-Codex client even
		// though its upstream accepts the request. The runtime passthrough check
		// in the request path is unaffected: a Codex client whose metadata cannot
		// be forwarded still fails there.
		return nil
	}
	strategy := settings.HeaderProfileStrategy
	if strategy == nil || !strategy.Enabled {
		return nil
	}
	selection, err := service.PrepareChannelRuntimeHeaderProfileSelection(c, channel.Id, strategy, info.RetryIndex)
	if err != nil {
		return nil
	}
	profileID := selection.ProfileID
	paramOverride := cachedResponsesChannelParamOverride(c, channel)
	runtimeOverride := false
	if info.ChannelMeta != nil && info.ChannelId == channel.Id {
		paramOverride = info.ChannelMeta.ParamOverride
		runtimeOverride = true
	} else if common.GetContextKeyInt(c, constant.ContextKeyChannelId) == channel.Id {
		paramOverride = common.GetContextKeyStringMap(c, constant.ContextKeyChannelParamOverride)
		runtimeOverride = true
	}
	if !runtimeOverride {
		// Candidate preflight runs before SetupContextForSelectedChannel applies
		// the request-scoped affinity template to the candidate override.
		if merged, applied := service.PreviewChannelAffinityOverrideTemplate(c, paramOverride); applied {
			paramOverride = merged
		}
	}
	return relaycommon.MissingCodexDynamicRequestHeaders(profileID, paramOverride, info.RequestHeaders)
}

func skipUnsupportedResponsesNamespaceTools(c *gin.Context, info *relaycommon.RelayInfo, channel *model.Channel) bool {
	markResponsesCompatibilityChannelSkip(c, info, channel, responsesRoutingChannelSkipReasonUnsupportedNamespaceTools, unsupportedResponsesNamespaceToolsChannelError(channel))
	return true
}

func skipResponsesNamespaceToolsWithoutBridge(c *gin.Context, info *relaycommon.RelayInfo, channel *model.Channel) {
	channelID := 0
	if channel != nil {
		channelID = channel.Id
	}
	// Keep the terminal client error tied to the failed Responses-to-Chat
	// conversion while retaining the more actionable namespace-specific reason
	// in the routing trace. The selector uses info.LastError after all
	// candidates are skipped; exposing only the diagnostic sentinel here made a
	// global bridge rule look like a different channel capability contract.
	conversionErr := fmt.Errorf(
		"%w: channel #%d uses Responses-to-Chat compatibility without an enabled namespace tool bridge; enable the bridge or use a native Responses channel",
		errResponsesNamespaceToolsUnsupported,
		channelID,
	)
	markResponsesCompatibilityChannelSkip(
		c,
		info,
		channel,
		responsesRoutingChannelSkipReasonUnsupportedNamespaceTools,
		unsupportedResponsesToChatConversionError(channel, conversionErr),
	)
}

func skipUnsupportedResponsesFunctionTools(c *gin.Context, info *relaycommon.RelayInfo, channel *model.Channel) bool {
	markResponsesCompatibilityChannelSkip(c, info, channel, responsesRoutingChannelSkipReasonUnsupportedFunctionTools, unsupportedResponsesFunctionToolsChannelError(channel))
	return true
}

func skipUnsupportedResponsesCustomTools(c *gin.Context, info *relaycommon.RelayInfo, channel *model.Channel) bool {
	markResponsesCompatibilityChannelSkip(c, info, channel, responsesRoutingChannelSkipReasonUnsupportedCustomTools, unsupportedResponsesCustomToolsChannelError(channel))
	return true
}

func skipUnsupportedResponsesToolSearch(c *gin.Context, info *relaycommon.RelayInfo, channel *model.Channel) bool {
	markResponsesCompatibilityChannelSkip(c, info, channel, responsesRoutingChannelSkipReasonUnsupportedToolSearch, unsupportedResponsesToolSearchChannelError(channel))
	return true
}

func markResponsesCompactionChannelSkip(c *gin.Context, info *relaycommon.RelayInfo, channel *model.Channel) {
	common.SetContextKey(c, constant.ContextKeyResponsesCompactChannelSkip, "channel_skipped_unsupported_compaction")
	markResponsesCompatibilityChannelSkip(c, info, channel, responsesRoutingChannelSkipReasonUnsupportedCompaction, unsupportedNativeCompactionChannelError(channel))
}

func markResponsesCompatibilityChannelSkip(c *gin.Context, info *relaycommon.RelayInfo, channel *model.Channel, reason string, channelError *types.NewAPIError) {
	recordResponsesRoutingChannelSkip(c, channel, reason)
	if info != nil {
		info.LastError = channelError
	}
}

func opaqueResponsesReplayParamOverride(c *gin.Context, channel *model.Channel) map[string]interface{} {
	if channel == nil {
		return nil
	}
	if c != nil && common.GetContextKeyInt(c, constant.ContextKeyChannelId) == channel.Id {
		if paramOverride, ok := common.GetContextKeyType[map[string]interface{}](c, constant.ContextKeyChannelParamOverride); ok {
			return paramOverride
		}
	}
	paramOverride := channel.GetParamOverride()
	if merged, applied := service.PreviewChannelAffinityOverrideTemplate(c, paramOverride); applied {
		return merged
	}
	return paramOverride
}

func recordResponsesRoutingChannelSkip(c *gin.Context, channel *model.Channel, reason string) {
	if c == nil || channel == nil {
		return
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return
	}
	existing, _ := common.GetContextKeyType[[]dto.ResponsesRoutingChannelSkip](c, constant.ContextKeyResponsesRoutingChannelSkips)
	for _, skip := range existing {
		if skip.ChannelID == channel.Id && skip.Reason == reason {
			return
		}
	}
	skips := make([]dto.ResponsesRoutingChannelSkip, 0, len(existing)+1)
	skips = append(skips, existing...)
	skips = append(skips, dto.ResponsesRoutingChannelSkip{
		ChannelID: channel.Id,
		Reason:    reason,
	})
	recordResponsesRoutingPreflightSkip(c, dto.ResponsesRoutingChannelSkip{
		ChannelID: channel.Id,
		Reason:    reason,
	})
	common.SetContextKey(c, constant.ContextKeyResponsesRoutingChannelSkips, skips)
}

func responsesToChatCompatibilityRequestForPrecheck(c *gin.Context, info *relaycommon.RelayInfo, request *dto.OpenAIResponsesRequest) (*dto.OpenAIResponsesRequest, error) {
	if c == nil || request == nil {
		return request, nil
	}
	preparedRequest := request
	normalizedRequest, normalizationResult, err := service.NormalizeContextCompactionForResponsesRequest(c.Request.Context(), *request)
	if err != nil {
		return nil, err
	}
	if normalizationResult.ChangedCount() > 0 {
		preparedRequest = &normalizedRequest
	}
	hasLocalReference, err := service.HasLocalSyntheticCompactReferenceWithContext(c.Request.Context(), *preparedRequest)
	if err != nil || !hasLocalReference {
		return preparedRequest, err
	}
	converted, applied, visibleOnly, _, err := service.ApplySyntheticCompactStateOrVisibleOnlyWithInfo(
		c.Request.Context(),
		service.SyntheticCompactScopeFromSource(info),
		*preparedRequest,
	)
	if err != nil {
		return nil, err
	}
	if !applied && !visibleOnly {
		return preparedRequest, nil
	}
	return &converted, nil
}

func responsesLocalCompactionTriggerRuleRequiresChannelSkip(c *gin.Context, info *relaycommon.RelayInfo, rule *model_setting.ProtocolConversionRule, channel *model.Channel) bool {
	if rule == nil || channel == nil {
		return false
	}
	settings := responsesCompactionChannelOtherSettings(c, info, channel)
	if settings.HasResponsesProxyCompatibilityProfile() {
		return true
	}
	if rule.AllChannels {
		return !settings.ResolveResponsesChannelCapability(channel.Type).SupportsCompactionItemPassthrough
	}
	if len(rule.ChannelIDs) > 0 {
		for _, channelID := range rule.ChannelIDs {
			if channelID == channel.Id {
				return true
			}
		}
	}
	if !rule.AllChannels && len(rule.ChannelTypes) > 0 {
		for _, channelType := range rule.ChannelTypes {
			if channelType == channel.Type {
				return true
			}
		}
	}
	return false
}

func nativeCompactionChannelSetting(c *gin.Context, info *relaycommon.RelayInfo, channel *model.Channel) dto.ChannelSettings {
	if channel == nil {
		return dto.ChannelSettings{}
	}
	if info != nil && info.ChannelMeta != nil && info.ChannelId == channel.Id {
		return info.ChannelSetting
	}
	if c != nil {
		if settings, ok := common.GetContextKeyType[dto.ChannelSettings](c, constant.ContextKeyChannelSetting); ok {
			if common.GetContextKeyInt(c, constant.ContextKeyChannelId) == channel.Id {
				return settings
			}
		}
	}
	return cachedResponsesChannelSettings(c, channel)
}

var errResponsesNativeCompactionUnsupported = errors.New("responses native compaction unsupported")
var errResponsesNativeToolIdentityUnsupported = errors.New("responses native tool identity unsupported")
var errResponsesToChatIncompatible = errors.New("responses to chat conversion incompatible")
var errResponsesFunctionToolsUnsupported = errors.New("responses function tools unsupported")
var errResponsesCustomToolsUnsupported = errors.New("responses custom tools unsupported")
var errResponsesNamespaceToolsUnsupported = errors.New("responses namespace tools unsupported")
var errResponsesToolSearchUnsupported = errors.New("responses tool search unsupported")
var errResponsesCodexDynamicHeadersUnsupported = errors.New("responses Codex dynamic header passthrough unsupported")

func unsupportedResponsesNativeToolIdentityChannelError(channel *model.Channel) *types.NewAPIError {
	channelID := 0
	if channel != nil {
		channelID = channel.Id
	}
	return types.NewErrorWithStatusCode(
		fmt.Errorf("%w: channel #%d uses responses-to-chat compatibility and cannot preserve Codex Lite native namespace tool identity; choose a native Responses-capable channel", errResponsesNativeToolIdentityUnsupported, channelID),
		types.ErrorCodeChannelModelMappedError,
		http.StatusServiceUnavailable,
	)
}

func unsupportedResponsesCodexDynamicHeadersChannelError(channel *model.Channel, missingHeaders []string) *types.NewAPIError {
	channelID := 0
	if channel != nil {
		channelID = channel.Id
	}
	return types.NewErrorWithStatusCode(
		fmt.Errorf("%w: channel #%d requires unconditional pass_headers for Codex dynamic request headers: %s", errResponsesCodexDynamicHeadersUnsupported, channelID, strings.Join(missingHeaders, ", ")),
		types.ErrorCodeChannelModelMappedError,
		http.StatusServiceUnavailable,
	)
}

func unsupportedNativeCompactionChannelError(channel *model.Channel) *types.NewAPIError {
	channelID := 0
	if channel != nil {
		channelID = channel.Id
	}
	return types.NewErrorWithStatusCode(
		fmt.Errorf("%w: channel #%d uses responses-to-chat compatibility and cannot handle native compaction input; choose a native Responses-capable channel", errResponsesNativeCompactionUnsupported, channelID),
		types.ErrorCodeChannelModelMappedError,
		http.StatusServiceUnavailable,
	)
}

func unsupportedResponsesNamespaceToolsChannelError(channel *model.Channel) *types.NewAPIError {
	channelID := 0
	if channel != nil {
		channelID = channel.Id
	}
	return types.NewErrorWithStatusCode(
		fmt.Errorf("%w: channel #%d explicitly does not support namespace tools in Responses requests", errResponsesNamespaceToolsUnsupported, channelID),
		types.ErrorCodeChannelModelMappedError,
		http.StatusServiceUnavailable,
	)
}

func unsupportedResponsesFunctionToolsChannelError(channel *model.Channel) *types.NewAPIError {
	channelID := 0
	if channel != nil {
		channelID = channel.Id
	}
	return types.NewErrorWithStatusCode(
		fmt.Errorf("%w: channel #%d explicitly does not support function tools in Responses requests", errResponsesFunctionToolsUnsupported, channelID),
		types.ErrorCodeChannelModelMappedError,
		http.StatusServiceUnavailable,
	)
}

func unsupportedResponsesCustomToolsChannelError(channel *model.Channel) *types.NewAPIError {
	channelID := 0
	if channel != nil {
		channelID = channel.Id
	}
	return types.NewErrorWithStatusCode(
		fmt.Errorf("%w: channel #%d explicitly does not support custom tools in Responses requests", errResponsesCustomToolsUnsupported, channelID),
		types.ErrorCodeChannelModelMappedError,
		http.StatusServiceUnavailable,
	)
}

func unsupportedResponsesToolSearchChannelError(channel *model.Channel) *types.NewAPIError {
	channelID := 0
	if channel != nil {
		channelID = channel.Id
	}
	return types.NewErrorWithStatusCode(
		fmt.Errorf("%w: channel #%d explicitly does not support tool search in Responses requests", errResponsesToolSearchUnsupported, channelID),
		types.ErrorCodeChannelModelMappedError,
		http.StatusServiceUnavailable,
	)
}

func unsupportedResponsesToChatChannelError(channel *model.Channel, lastErr *types.NewAPIError) *types.NewAPIError {
	if lastErr != nil {
		return lastErr
	}
	return unsupportedResponsesToChatConversionError(channel, errors.New("request is not compatible with responses-to-chat compatibility mode"))
}

func unsupportedResponsesToChatConversionError(channel *model.Channel, err error) *types.NewAPIError {
	channelID := 0
	if channel != nil {
		channelID = channel.Id
	}
	return types.NewErrorWithStatusCode(
		fmt.Errorf("%w: channel #%d uses responses-to-chat compatibility and cannot safely convert this Responses request: %w", errResponsesToChatIncompatible, channelID, err),
		types.ErrorCodeChannelModelMappedError,
		http.StatusServiceUnavailable,
	)
}

func responsesCompactionRuleRequiresChannelSkip(c *gin.Context, info *relaycommon.RelayInfo, rule *model_setting.ProtocolConversionRule, channel *model.Channel) bool {
	if rule == nil || channel == nil {
		return false
	}
	settings := responsesCompactionChannelOtherSettings(c, info, channel)
	if settings.HasResponsesProxyCompatibilityProfile() {
		return true
	}
	if rule.AllChannels {
		return !settings.ResolveResponsesChannelCapability(channel.Type).SupportsCompactionItemPassthrough
	}
	if len(rule.ChannelIDs) > 0 {
		for _, channelID := range rule.ChannelIDs {
			if channelID == channel.Id {
				return true
			}
		}
	}
	if !rule.AllChannels && len(rule.ChannelTypes) > 0 {
		for _, channelType := range rule.ChannelTypes {
			if channelType == channel.Type {
				return true
			}
		}
	}
	return false
}

func responsesCompactionChannelCannotPassthroughItems(settings dto.ChannelOtherSettings, channelType int) bool {
	if settings.HasResponsesProxyCompatibilityProfile() {
		return true
	}
	return !settings.ResolveResponsesChannelCapability(channelType).SupportsCompactionItemPassthrough
}

func responsesCompactionChannelOtherSettings(c *gin.Context, info *relaycommon.RelayInfo, channel *model.Channel) dto.ChannelOtherSettings {
	if info != nil && info.ChannelMeta != nil && info.ChannelId == channel.Id {
		return info.ChannelOtherSettings
	}
	if c != nil {
		if settings, ok := common.GetContextKeyType[dto.ChannelOtherSettings](c, constant.ContextKeyChannelOtherSetting); ok {
			if common.GetContextKeyInt(c, constant.ContextKeyChannelId) == channel.Id {
				return settings
			}
		}
	}
	return cachedResponsesChannelOtherSettings(c, channel)
}

func newResponsesCompactionInputError(err error) *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		err,
		types.ErrorCodeInvalidRequest,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	)
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
	// Once the downstream request is canceled, retrying another upstream cannot
	// produce a usable response and may create unnecessary upstream traffic.
	if c == nil {
		return false
	}
	if c.Request != nil && c.Request.Context().Err() != nil {
		return false
	}
	if relaycommon.IsResponsesOpaqueEncryptedReplay(c) {
		relaycommon.MarkResponsesEncryptedReasoningReplayRetrySuppressed(c)
		return false
	}
	if isUnsafeReadResponseBodyRetry(c, openaiErr) {
		return false
	}
	if c != nil && c.Writer != nil && c.Writer.Written() &&
		!common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamPreludeRetrySafe) {
		return false
	}
	if service.IsUpstreamRateLimitRetryTerminal(c) {
		return false
	}
	if isPermanentUpstreamUnavailableError(openaiErr) {
		return false
	}
	// A deterministic model-not-found is the same answer from every channel, so
	// retrying only burns the budget and then lets the terminal classification
	// overwrite the original 404 with a get_channel_failed. Stop here and let the
	// caller surface the upstream's own error.
	if isDeterministicModelNotFoundError(openaiErr) {
		return false
	}
	// A content-policy or safety refusal is decided by the request body itself, so
	// replaying it against another channel gets the same refusal, burns the retry
	// budget, and cools every channel it touches. Deterministic, like the
	// model-not-found case above: stop and surface the upstream's own error.
	if isDeterministicContentPolicyRefusalError(openaiErr) {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) &&
		!shouldRetryDespiteChannelAffinity(c, openaiErr) &&
		!service.ShouldRetryAfterChannelHealthAuthenticationFailure(openaiErr) {
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
	if shouldRetryUpstreamRequestTooLargeForResponsesCompact(c, openaiErr) {
		return true
	}
	if isRetryableUpstreamTemporaryError(openaiErr) {
		return true
	}
	if isUpstreamTransportInterruptedRelayError(openaiErr) {
		return true
	}
	return operation_setting.ShouldRetryByStatusCode(code)
}

// Read failures must be classified at the relay boundary before a channel can
// be retried. A generic read error cannot prove that replay is safe.
func isUnsafeReadResponseBodyRetry(c *gin.Context, openaiErr *types.NewAPIError) bool {
	if openaiErr == nil || openaiErr.GetErrorCode() != types.ErrorCodeReadResponseBodyFailed {
		return false
	}
	if c == nil || c.Request == nil || c.Request.Context().Err() != nil {
		return true
	}
	if c.Writer != nil && c.Writer.Written() {
		return true
	}
	return !types.IsUpstreamTransportInterruptedError(openaiErr)
}

func observeUpstreamRateLimitRetry(c *gin.Context, channelID int, openaiErr *types.NewAPIError) {
	if openaiErr == nil || relaycommon.IsResponsesOpaqueEncryptedReplay(c) {
		return
	}
	statusCode := openaiErr.StatusCode
	if openaiErr.OriginalStatusCode != 0 {
		statusCode = openaiErr.OriginalStatusCode
	}
	if !isUpstreamRateLimitError(statusCode) {
		return
	}
	if openaiErr.GetErrorType() == types.ErrorTypeNewAPIError || types.IsChannelError(openaiErr) {
		return
	}
	service.ObserveUpstreamRateLimitRetry(c, channelID)
}

func recordResponsesCapabilityObservation(c *gin.Context, info *relaycommon.RelayInfo, err *types.NewAPIError) {
	if c == nil || info == nil || info.ChannelMeta == nil || err == nil || relaycommon.IsResponsesOpaqueEncryptedReplay(c) {
		return
	}
	if common.GetContextKeyBool(c, constant.ContextKeyResponsesCodexLocalCompaction413Recovery) {
		return
	}
	if shouldFallbackRemoteResponsesCompaction(c, info, err) {
		return
	}
	if !shouldRecordResponsesCapabilityObservation(info.RelayMode, err) {
		return
	}
	observation := dto.ResponsesCapabilityObservation{
		ObservedAt: time.Now().UTC().Unix(),
		StatusCode: err.StatusCode,
		ErrorCode:  string(err.GetErrorCode()),
		Reason:     common.LocalLogPreview(err.MaskSensitiveErrorWithStatusCode()),
	}
	settings, updateErr := model.MarkResponsesCapabilityObservation(info.ChannelMeta.ChannelId, observation)
	if updateErr != nil {
		logger.LogError(c, fmt.Sprintf("mark responses capability observation failed: %s", updateErr.Error()))
		return
	}
	if settings.ResponsesCapabilityRegistry == nil {
		return
	}
	info.ChannelOtherSettings = settings
	info.ChannelMeta.ChannelOtherSettings = settings
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, settings)
}

func recordChannelRequestBodyLimit(c *gin.Context, info *relaycommon.RelayInfo, err *types.NewAPIError) {
	if c == nil || info == nil || info.ChannelMeta == nil || err == nil || relaycommon.IsResponsesOpaqueEncryptedReplay(c) {
		return
	}
	if common.GetContextKeyBool(c, constant.ContextKeyResponsesCodexLocalCompaction413Recovery) {
		return
	}
	policy := model_setting.GetRequestBodyLimitPolicy()
	if !policy.Enabled {
		return
	}
	if err.StatusCode != http.StatusRequestEntityTooLarge {
		return
	}
	if shouldFallbackRemoteResponsesCompaction(c, info, err) {
		return
	}
	requestBodySize := common.GetContextKeyInt64(c, constant.ContextKeyRequestBodySize)
	if requestBodySize <= 0 {
		return
	}
	limit := dto.ChannelRequestBodyLimit{
		MaxBytes:   requestBodySize,
		ObservedAt: time.Now().UTC().Unix(),
		Source:     "upstream_413",
		Reason:     common.LocalLogPreview(err.MaskSensitiveErrorWithStatusCode()),
	}
	settings, updateErr := model.MarkChannelRequestBodyLimit(info.ChannelMeta.ChannelId, limit)
	if updateErr != nil {
		logger.LogError(c, fmt.Sprintf("mark channel request body limit failed: %s", updateErr.Error()))
		return
	}
	info.ChannelOtherSettings = settings
	info.ChannelMeta.ChannelOtherSettings = settings
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, settings)
}

func shouldRecordResponsesCapabilityObservation(relayMode int, err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	if relayMode == relayconstant.RelayModeResponsesCompact {
		return err.StatusCode == http.StatusBadRequest || err.StatusCode == http.StatusRequestEntityTooLarge
	}
	if relayMode != relayconstant.RelayModeResponses {
		return false
	}
	if err.StatusCode == http.StatusRequestEntityTooLarge {
		return true
	}
	if err.StatusCode != http.StatusBadRequest {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "compaction") ||
		strings.Contains(message, "responses/compact") ||
		strings.Contains(message, "chat compatibility mode") ||
		strings.Contains(message, "previous_response_id") ||
		strings.Contains(message, "previous response id")
}

func shouldRetryDespiteChannelAffinity(c *gin.Context, openaiErr *types.NewAPIError) bool {
	if openaiErr == nil {
		return false
	}
	return isUpstreamRateLimitError(openaiErr.StatusCode) ||
		service.ShouldRetryAfterChannelHealthAuthenticationFailure(openaiErr) ||
		isRetryableUpstreamTemporaryError(openaiErr) ||
		isUpstreamTransportInterruptedRelayError(openaiErr) ||
		isUpstreamGatewayTimeoutError(openaiErr.StatusCode) ||
		shouldRetryUpstreamRequestTooLargeForResponsesCompact(c, openaiErr)
}

func isUpstreamRateLimitError(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests
}

func isUpstreamGatewayTimeoutError(statusCode int) bool {
	return statusCode == http.StatusGatewayTimeout || statusCode == 524
}

func isPermanentUpstreamUnavailableError(openaiErr *types.NewAPIError) bool {
	if openaiErr == nil {
		return false
	}
	message := strings.ToLower(strings.Join(strings.Fields(openaiErr.Error()), " "))
	return strings.Contains(message, "permanently unavailable") ||
		strings.Contains(message, "permanent unavailable")
}

func isRetryableUpstreamTemporaryError(openaiErr *types.NewAPIError) bool {
	if openaiErr == nil {
		return false
	}
	message := strings.ToLower(strings.Join(strings.Fields(openaiErr.Error()), " "))
	if isRetryableModelCapacityMessage(message) {
		return true
	}
	if strings.Contains(message, "auth_not_found") &&
		strings.Contains(message, "no auth available") {
		return true
	}
	if strings.Contains(message, "upstream service temporarily unavailable") {
		return true
	}
	if !isPermanentUpstreamUnavailableError(openaiErr) &&
		strings.Contains(message, "service temporarily unavailable") {
		return true
	}
	if strings.Contains(message, "subscription temporarily unavailable") {
		return true
	}
	return strings.Contains(message, "订阅额度不足或暂不可用")
}

func isRetryableModelCapacityMessage(message string) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(message), " "))
	return strings.Contains(normalized, "selected model is at capacity") ||
		(strings.Contains(normalized, "model is at capacity") &&
			strings.Contains(normalized, "try a different model"))
}

// String 类型断言辅助：OpenAIError.Code 是 any，可能是 string、ErrorCode 或
// 上游的数字/对象 code。统一转小写字符串再匹配。
func openAIErrorCodeString(code any) string {
	lowered := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", code)))
	return strings.ToLower(strings.Join(strings.Fields(lowered), " "))
}

// isContentPolicyRefusalMessage 覆盖最常见的确定性内容策略/安全拒绝形态。
// 普通化后的 message 与 code 一起参与匹配。
func isContentPolicyRefusalMessage(normalized, code string) bool {
	for _, marker := range []string{
		"content_policy_violation",
		"content policy violation",
		"content_policy",
		"content policy",
		"content filter",
		"content_filter",
		"safety system",
		"prompt is blocked",
	} {
		if strings.Contains(code, marker) || strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

// isDeterministicContentPolicyRefusalError 判定一条上游错误是否是内容策略/安全
// 拒绝。这类拒绝由请求内容本身决定：把同一个 prompt 换到任何渠道都会得到同样
// 的拒绝，重试只会白跑预算，并让每个被尝试的渠道各记一次健康冷却。因此在
// shouldRetry 里统一判为不可重试。
//
// 判定放在控制器而不是某个协议 handler：非流式、OpenAI SSE 帧、Responses 事件
// 等所有路径最终都汇聚到 shouldRetry，这里一处判定对全部协议生效。
func isDeterministicContentPolicyRefusalError(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	openAIError := err.ToOpenAIError()
	if openAIError.Type != "" {
		typeLower := strings.ToLower(strings.Join(strings.Fields(openAIError.Type), " "))
		for _, marker := range []string{
			"content_policy_violation",
			"content policy violation",
			"content_filter",
			"content filter",
		} {
			if strings.Contains(typeLower, marker) {
				return true
			}
		}
	}
	code := openAIErrorCodeString(openAIError.Code)
	message := strings.ToLower(strings.Join(strings.Fields(openAIError.Message), " "))
	return isContentPolicyRefusalMessage(message, code)
}

func isUpstreamTransportInterruptedRelayError(openaiErr *types.NewAPIError) bool {
	if openaiErr == nil {
		return false
	}
	if openaiErr.GetErrorCode() == types.ErrorCodeUpstreamTransportInterrupted {
		return true
	}
	return types.IsUpstreamTransportInterruptedError(openaiErr)
}

func shouldRetryUpstreamRequestTooLargeForResponsesCompact(c *gin.Context, err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	if err.StatusCode != http.StatusRequestEntityTooLarge {
		return false
	}
	if err.GetErrorCode() != types.ErrorCodeUpstreamRequestTooLarge {
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
	if responsesCompactSyntheticFallbackAttemptedForChannel(c, info.ChannelMeta.ChannelId) {
		return false
	}
	if info.RelayMode != relayconstant.RelayModeResponsesCompact ||
		info.ChannelType != constant.ChannelTypeOpenAI ||
		!info.ChannelOtherSettings.IsAutoResponsesCompact() ||
		info.ChannelOtherSettings.HasResponsesProxyCompatibilityProfile() {
		return false
	}
	if info.ChannelOtherSettings.HasActiveResponsesCompactAutoFallback(time.Now()) {
		return false
	}
	if responsesCompactRequestHasUnsafeCompactionInput(c, info) {
		return false
	}
	switch err.StatusCode {
	case http.StatusBadRequest,
		http.StatusNotFound,
		http.StatusMethodNotAllowed,
		http.StatusUnprocessableEntity,
		http.StatusNotImplemented:
		return isResponsesCompactNativeCompatibilityError(info, err)
	case http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
		524:
		return true
	default:
		return false
	}
}

func shouldFallbackRemoteResponsesCompaction(c *gin.Context, info *relaycommon.RelayInfo, err *types.NewAPIError) bool {
	if c == nil || err == nil || info == nil || info.ChannelMeta == nil {
		return false
	}
	if c.Writer != nil && c.Writer.Written() &&
		!common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamPreludeRetrySafe) {
		return false
	}
	if common.GetContextKeyBool(c, constant.ContextKeyResponsesRemoteCompactSyntheticFallback) ||
		responsesCompactSyntheticFallbackAttemptedForChannel(c, info.ChannelMeta.ChannelId) {
		return false
	}
	if info.RelayMode != relayconstant.RelayModeResponses ||
		info.ChannelType != constant.ChannelTypeOpenAI ||
		info.ChannelOtherSettings.HasDisabledResponsesCompact() {
		return false
	}
	request, ok := info.Request.(*dto.OpenAIResponsesRequest)
	if !ok || request == nil || !request.HasCompactionTrigger() || !info.IsStream ||
		!requestHeaderHasFeature(c.Request, "X-Codex-Beta-Features", "remote_compaction_v2") {
		return false
	}
	if responsesCompactRequestHasUnsafeCompactionInput(c, info) {
		return false
	}
	switch err.StatusCode {
	case http.StatusRequestEntityTooLarge:
		return err.GetErrorCode() == types.ErrorCodeUpstreamRequestTooLarge
	case http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
		524:
		return true
	default:
		return false
	}
}

func requestHeaderHasFeature(request *http.Request, headerName string, target string) bool {
	if request == nil {
		return false
	}
	for _, value := range request.Header.Values(headerName) {
		for _, feature := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(feature), target) {
				return true
			}
		}
	}
	return false
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
	info.ChannelOtherSettings = settings
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, settings)
}

func restoreResponsesCompactMode(c *gin.Context, info *relaycommon.RelayInfo, settings dto.ChannelOtherSettings) {
	if info == nil || info.ChannelMeta == nil {
		return
	}
	info.ChannelMeta.ChannelOtherSettings = settings
	info.ChannelOtherSettings = settings
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, settings)
}

type responsesCompactFallbackContextValue struct {
	exists bool
	value  any
}

const responsesCompactFallbackChannelIDKey = "responses_compact_fallback_snapshot_channel_id"

var responsesCompactFallbackContextKeys = []string{
	string(constant.ContextKeyChannelOtherSetting),
	"responses_compact_auto_fallback_attempted",
	"responses_compact_context_fallback_attempted",
	string(constant.ContextKeyResponsesCompactCodexContextPruned),
	"responses_compact_previous_response_id_fallback_attempted",
	"responses_compact_synthetic_fallback_channel_id",
	string(constant.ContextKeyResponsesCompactSummaryModelFallbackAttempted),
	string(constant.ContextKeyResponsesCompactVisibleOnlyFallbackAttempted),
	string(constant.ContextKeyResponsesCompactStateLookup),
	string(constant.ContextKeyResponsesCompactStateScopeResult),
	string(constant.ContextKeyResponsesCompactMarkerKind),
	string(constant.ContextKeyResponsesCompactRouteDecision),
	string(constant.ContextKeyResponsesCompactFallbackReason),
	string(constant.ContextKeyResponsesCompactChannelSkip),
	string(constant.ContextKeyResponsesRoutingChannelSkips),
	string(constant.ContextKeyResponsesCompactStateRestored),
	string(constant.ContextKeyResponsesCompactModelChanged),
	string(constant.ContextKeyResponsesCompactStateHash),
	string(constant.ContextKeyResponsesPreviousIDAction),
	string(constant.ContextKeyResponsesCompactSummaryModel),
	string(constant.ContextKeyResponsesCompactSummaryModels),
	string(constant.ContextKeyResponsesCompactVisibleOnly),
	string(constant.ContextKeyResponsesCompactNativeFallback),
	string(constant.ContextKeyResponsesRemoteCompactSyntheticFallback),
}

func snapshotResponsesCompactFallbackContext(c *gin.Context) map[string]responsesCompactFallbackContextValue {
	snapshot := make(map[string]responsesCompactFallbackContextValue, len(responsesCompactFallbackContextKeys)+1)
	if c == nil {
		return snapshot
	}
	channelID := common.GetContextKeyInt(c, constant.ContextKeyChannelId)
	if channelID != 0 {
		snapshot[responsesCompactFallbackChannelIDKey] = responsesCompactFallbackContextValue{
			exists: true,
			value:  channelID,
		}
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

func restoreResponsesCompactFallbackContext(c *gin.Context, info *relaycommon.RelayInfo, snapshot map[string]responsesCompactFallbackContextValue) {
	if c == nil {
		return
	}
	for _, key := range responsesCompactFallbackContextKeys {
		entry, exists := snapshot[key]
		if key == string(constant.ContextKeyResponsesCompactNativeFallback) && (!exists || !entry.exists) {
			if _, currentExists := c.Get(key); currentExists {
				continue
			}
		}
		if key == string(constant.ContextKeyChannelOtherSetting) &&
			!responsesCompactFallbackSnapshotMatchesContextChannel(c, snapshot) {
			continue
		}
		if exists && entry.exists {
			c.Set(key, entry.value)
			continue
		}
		if c.Keys != nil {
			delete(c.Keys, key)
		}
	}
	restoreResponsesCompactFallbackRelayInfo(info, snapshot)
	restoreResponsesCompactVisibleOnlyRequestContext(c, snapshot)
}

func restoreResponsesCompactFallbackRelayInfo(info *relaycommon.RelayInfo, snapshot map[string]responsesCompactFallbackContextValue) {
	if info == nil || info.ChannelMeta == nil {
		return
	}
	entry, exists := snapshot[string(constant.ContextKeyChannelOtherSetting)]
	if !exists || !entry.exists {
		return
	}
	settings, ok := entry.value.(dto.ChannelOtherSettings)
	if !ok {
		return
	}
	if !responsesCompactFallbackSnapshotMatchesChannel(info, snapshot) {
		return
	}
	info.ChannelMeta.ChannelOtherSettings = settings
	info.ChannelOtherSettings = settings
}

func responsesCompactFallbackSnapshotMatchesChannel(info *relaycommon.RelayInfo, snapshot map[string]responsesCompactFallbackContextValue) bool {
	channelID, ok := responsesCompactFallbackSnapshotChannelID(snapshot)
	if !ok {
		return true
	}
	return channelID == info.ChannelMeta.ChannelId
}

func responsesCompactFallbackSnapshotMatchesContextChannel(c *gin.Context, snapshot map[string]responsesCompactFallbackContextValue) bool {
	channelID, ok := responsesCompactFallbackSnapshotChannelID(snapshot)
	if !ok {
		return true
	}
	if c == nil {
		return false
	}
	return channelID == common.GetContextKeyInt(c, constant.ContextKeyChannelId)
}

func responsesCompactFallbackSnapshotChannelID(snapshot map[string]responsesCompactFallbackContextValue) (int, bool) {
	entry, exists := snapshot[responsesCompactFallbackChannelIDKey]
	if !exists || !entry.exists {
		return 0, false
	}
	channelID, ok := entry.value.(int)
	return channelID, ok
}

func restoreResponsesCompactVisibleOnlyRequestContext(c *gin.Context, snapshot map[string]responsesCompactFallbackContextValue) {
	if c == nil || c.Request == nil {
		return
	}
	entry, exists := snapshot[string(constant.ContextKeyResponsesCompactVisibleOnly)]
	enabled := false
	if exists && entry.exists {
		if value, ok := entry.value.(bool); ok {
			enabled = value
		}
	}
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), constant.ContextKeyResponsesCompactVisibleOnly, enabled))
}

func markResponsesCompactSyntheticFallbackAttempted(c *gin.Context, info *relaycommon.RelayInfo) {
	if c == nil || info == nil || info.ChannelMeta == nil {
		return
	}
	c.Set("responses_compact_synthetic_fallback_channel_id", info.ChannelMeta.ChannelId)
}

func responsesCompactSyntheticFallbackAttemptedForChannel(c *gin.Context, channelID int) bool {
	if c == nil || channelID == 0 {
		return false
	}
	return c.GetInt("responses_compact_synthetic_fallback_channel_id") == channelID
}

func retryResponsesCompactSyntheticSummary(c *gin.Context, info *relaycommon.RelayInfo, bodyStorage requestBodyReadSeeker, triggerErr *types.NewAPIError) *types.NewAPIError {
	if info == nil || info.ChannelMeta == nil {
		return triggerErr
	}
	markResponsesCompactSyntheticFallbackAttempted(c, info)
	originalSettings := info.ChannelMeta.ChannelOtherSettings
	setResponsesCompactSyntheticMode(c, info)
	err := retryResponsesCompactWithBody(c, info, bodyStorage, triggerErr)
	if shouldFallbackResponsesCompactVisibleOnly(c, info, err) {
		err = retryResponsesCompactVisibleOnlySummary(c, info, bodyStorage, err)
	}
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
	common.SetContextKey(c, constant.ContextKeyResponsesCompactSummaryModelFallbackAttempted, true)
	common.SetContextKey(c, constant.ContextKeyResponsesCompactSummaryModels, models)
	service.MarkResponsesCompactFallbackAttempt(c, info, service.ResponsesCompactFallbackAttemptSummaryModel, models)
	for _, fallbackModel := range models {
		common.SetContextKey(c, constant.ContextKeyResponsesCompactSummaryModel, fallbackModel)
		setResponsesCompactVisibleOnly(c, true)
		logger.LogWarn(c, fmt.Sprintf(
			"responses compact summary model fallback: channel_id=%d model=%s visible_only=true status_code=%d error=%s",
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

func retryResponsesCompactVisibleOnlySummary(c *gin.Context, info *relaycommon.RelayInfo, bodyStorage requestBodyReadSeeker, triggerErr *types.NewAPIError) *types.NewAPIError {
	if info == nil || info.ChannelMeta == nil {
		return triggerErr
	}
	common.SetContextKey(c, constant.ContextKeyResponsesCompactVisibleOnlyFallbackAttempted, true)
	service.MarkResponsesCompactFallbackAttempt(c, info, service.ResponsesCompactFallbackAttemptVisibleOnly, nil)
	setResponsesCompactVisibleOnly(c, true)
	logger.LogWarn(c, fmt.Sprintf(
		"responses compact visible-only fallback: channel_id=%d status_code=%d error=%s",
		info.ChannelMeta.ChannelId,
		triggerErr.StatusCode,
		triggerErr.MaskSensitiveError(),
	))
	return retryResponsesCompactWithBody(c, info, bodyStorage, triggerErr)
}

func setResponsesCompactVisibleOnly(c *gin.Context, enabled bool) {
	if c == nil || c.Request == nil {
		return
	}
	common.SetContextKey(c, constant.ContextKeyResponsesCompactVisibleOnly, enabled)
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), constant.ContextKeyResponsesCompactVisibleOnly, enabled))
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
	beginResponsesStreamRelayAttempt(c, info)
	err := relayHandler(c, info)
	if err == nil {
		return nil
	}
	return service.NormalizeViolationFeeError(err)
}

func shouldFallbackResponsesCompactVisibleOnly(c *gin.Context, info *relaycommon.RelayInfo, err *types.NewAPIError) bool {
	if err == nil || info == nil || info.ChannelMeta == nil {
		return false
	}
	if common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactVisibleOnlyFallbackAttempted) {
		return false
	}
	if responsesCompactVisibleOnlyEnabled(c) {
		return false
	}
	if info.RelayMode != relayconstant.RelayModeResponsesCompact ||
		info.ChannelType != constant.ChannelTypeOpenAI ||
		!info.ChannelOtherSettings.HasSyntheticResponsesCompact() {
		return false
	}
	if responsesCompactRequestHasUnsafeCompactionInput(c, info) {
		return false
	}
	return isResponsesCompactContextLengthError(err)
}

func responsesCompactVisibleOnlyEnabled(c *gin.Context) bool {
	if c == nil {
		return false
	}
	if common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactVisibleOnly) {
		return true
	}
	if c.Request == nil {
		return false
	}
	value, ok := c.Request.Context().Value(constant.ContextKeyResponsesCompactVisibleOnly).(bool)
	return ok && value
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
	if responsesCompactRequestHasUnsafeCompactionInput(c, info) {
		return false
	}
	return isResponsesCompactContextLengthError(err)
}

func shouldFallbackResponsesCompactPreviousResponseID(c *gin.Context, info *relaycommon.RelayInfo, err *types.NewAPIError) bool {
	if err == nil || info == nil || info.ChannelMeta == nil {
		return false
	}
	if c.GetBool("responses_compact_previous_response_id_fallback_attempted") {
		return false
	}
	if info.RelayMode != relayconstant.RelayModeResponsesCompact ||
		info.ChannelType != constant.ChannelTypeOpenAI {
		return false
	}
	if !responsesCompactRequestHasLocalSyntheticReference(c, info) {
		return false
	}
	if responsesCompactRequestHasUnsafeCompactionInput(c, info) {
		return false
	}
	return isResponsesCompactPreviousResponseIDUnsupportedError(err)
}

func responsesCompactRequestHasLocalSyntheticReference(c *gin.Context, info *relaycommon.RelayInfo) bool {
	if info == nil {
		return false
	}
	var req dto.OpenAIResponsesRequest
	switch request := info.Request.(type) {
	case *dto.OpenAIResponsesCompactionRequest:
		if request == nil {
			return false
		}
		req = *request.ToResponsesRequest()
	case *dto.OpenAIResponsesRequest:
		if request == nil {
			return false
		}
		req = *request
	default:
		return false
	}
	hasReference, err := service.HasLocalSyntheticCompactReferenceWithContext(relaycommon.GinRequestContext(c), req)
	return err == nil && hasReference
}

func responsesCompactRequestHasUnsafeCompactionInput(c *gin.Context, info *relaycommon.RelayInfo) bool {
	if info == nil {
		return false
	}
	if c != nil {
		if cached, exists := c.Get(responsesCompactUnsafeCompactionInputKey); exists {
			if unsafe, ok := cached.(bool); ok {
				return unsafe
			}
		}
	}
	var req dto.OpenAIResponsesRequest
	switch request := info.Request.(type) {
	case *dto.OpenAIResponsesCompactionRequest:
		if request == nil {
			return false
		}
		req = *request.ToResponsesRequest()
	case *dto.OpenAIResponsesRequest:
		if request == nil {
			return false
		}
		req = *request
	default:
		return false
	}
	hasOpaqueEncryptedContent, opaqueErr := service.ResponsesInputHasOpaqueEncryptedContent(relaycommon.GinRequestContext(c), req.Input)
	if opaqueErr != nil {
		logger.LogError(c, fmt.Sprintf("detect opaque responses encrypted reasoning replay failed: %s", opaqueErr.Error()))
	}
	hasRemote, remoteErr := service.HasRemoteResponsesCompactionInput(relaycommon.GinRequestContext(c), req)
	if remoteErr != nil {
		logger.LogError(c, fmt.Sprintf("detect remote responses compaction input failed: %s", remoteErr.Error()))
	}
	unsafe := opaqueErr != nil || hasOpaqueEncryptedContent || remoteErr != nil || hasRemote
	if c != nil {
		c.Set(responsesCompactUnsafeCompactionInputKey, unsafe)
	}
	return unsafe
}

func shouldFallbackResponsesCompactSummaryModel(c *gin.Context, info *relaycommon.RelayInfo, err *types.NewAPIError) bool {
	if err == nil || info == nil || info.ChannelMeta == nil {
		return false
	}
	if common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactSummaryModelFallbackAttempted) {
		return false
	}
	remoteSyntheticRecovery := info.RelayMode == relayconstant.RelayModeResponses &&
		common.GetContextKeyBool(c, constant.ContextKeyResponsesRemoteCompactSyntheticFallback)
	if (info.RelayMode != relayconstant.RelayModeResponsesCompact && !remoteSyntheticRecovery) ||
		!responsesCompactSummaryModelFallbackSupported(c, info) ||
		!info.ChannelOtherSettings.ResponsesCompactSummaryModelFallbackEnabled() ||
		len(responsesCompactSummaryFallbackCandidates(c, info)) == 0 {
		return false
	}
	if responsesCompactRequestHasUnsafeCompactionInput(c, info) {
		return false
	}
	return isResponsesCompactContextLengthError(err) || isResponsesCompactSyntheticServerError(c, info, err)
}

func isResponsesCompactSyntheticServerError(c *gin.Context, info *relaycommon.RelayInfo, err *types.NewAPIError) bool {
	if info == nil || err == nil || !responsesCompactSyntheticRecoveryActive(c, info) {
		return false
	}
	switch err.StatusCode {
	case http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
		524:
		return true
	default:
		return false
	}
}

func responsesCompactSyntheticRecoveryActive(c *gin.Context, info *relaycommon.RelayInfo) bool {
	return info != nil && (info.ChannelOtherSettings.HasSyntheticResponsesCompact() ||
		common.GetContextKeyBool(c, constant.ContextKeyResponsesRemoteCompactSyntheticFallback))
}

func responsesCompactSummaryModelFallbackSupported(c *gin.Context, info *relaycommon.RelayInfo) bool {
	if info == nil || info.ChannelMeta == nil {
		return false
	}
	if info.ChannelType == constant.ChannelTypeCodex {
		return true
	}
	return info.ChannelType == constant.ChannelTypeOpenAI && responsesCompactSyntheticRecoveryActive(c, info)
}

func isResponsesCompactPreviousResponseIDUnsupportedError(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, service.ErrResponsesRESTPreviousIDUnsupported) {
		return true
	}
	openAIError := err.ToOpenAIError()
	message := strings.ToLower(strings.Join([]string{
		err.Error(),
		openAIError.Message,
		fmt.Sprint(openAIError.Code),
	}, " "))
	normalized := normalizeResponsesCompactCompatibilityMessage(message)
	for _, indicator := range []string{
		"previous response id is only supported on responses websocket v2",
		"previous response id is not supported",
		"previous response id unsupported",
	} {
		if strings.Contains(message, indicator) || strings.Contains(normalized, indicator) {
			return true
		}
	}
	return false
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
		itemType := responsesCompactInputItemType(item)
		if relaycommon.IsResponsesCompactionItemType(itemType) {
			return true
		}
		switch itemType {
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
	if relaycommon.IsResponsesOpaqueEncryptedReplay(c) {
		channelError.AutoBan = false
	}
	logger.LogError(c, fmt.Sprintf("channel error (channel #%d, status code: %d): %s", channelError.ChannelId, err.StatusCode, common.LocalLogPreview(err.MaskSensitiveErrorWithStatusCode())))
	if service.ShouldExcludeChannelForRetry(err) {
		group := ""
		modelName := ""
		if relayInfo != nil {
			group = relayInfo.UsingGroup
			modelName = relayInfo.OriginModelName
		}
		if c != nil {
			if strings.TrimSpace(group) == "" {
				group = common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
				if strings.TrimSpace(group) == "" {
					group = c.GetString("group")
				}
			}
			if strings.TrimSpace(modelName) == "" {
				modelName = common.GetContextKeyString(c, constant.ContextKeyOriginalModel)
				if strings.TrimSpace(modelName) == "" {
					modelName = c.GetString("original_model")
				}
			}
		}
		if relayInfo != nil && relayInfo.ChannelHealthRoute != nil {
			service.ExcludeChannelHealthSelectionForRoute(c, group, modelName, *relayInfo.ChannelHealthRoute)
		} else {
			service.ExcludeChannelHealthSelection(c, group, modelName, channelError.ChannelId)
		}
	}
	healthFailure := service.ShouldSuppressChannelAutoBanForHealth(c, relayInfo, err)
	if healthFailure || isProtocolIncompatibleSkip(err) {
		channelError.AutoBan = false
	}
	if healthFailure {
		service.ObserveChannelHealthFailure(c, relayInfo, err)
	} else {
		service.ReleaseChannelHealthProbe(c, relayInfo)
	}
	// 不要使用context获取渠道信息，异步处理时可能会出现渠道信息不一致的情况
	// do not use context to get channel info, there may be inconsistent channel info when processing asynchronously
	if service.ShouldDisableChannel(err) && channelError.AutoBan {
		gopool.Go(func() {
			service.DisableChannel(channelError, err.ErrorWithStatusCode())
		})
	}

	recordRelayErrorLog(c, relayInfo, err)

}

func recordTerminalResponsesRoutingErrorLog(c *gin.Context, relayInfo *relaycommon.RelayInfo, err *types.NewAPIError) {
	if c == nil || relayInfo == nil || err == nil {
		return
	}
	channelSkips, ok := common.GetContextKeyType[[]dto.ResponsesRoutingChannelSkip](c, constant.ContextKeyResponsesRoutingChannelSkips)
	if (!ok || len(channelSkips) == 0) && !service.HasChannelHealthRoutingAudit(c) {
		return
	}
	finalizeResponsesRoutingTrace(c, err)
	recordRelayErrorLog(c, relayInfo, err)
}

func recordRelayErrorLog(c *gin.Context, relayInfo *relaycommon.RelayInfo, err *types.NewAPIError) {
	if c == nil || err == nil || !constant.ErrorLogEnabled || !types.IsRecordErrorLog(err) {
		return
	}
	userId := c.GetInt("id")
	tokenName := c.GetString("token_name")
	modelName := c.GetString("original_model")
	tokenId := c.GetInt("token_id")
	userGroup := c.GetString("group")
	channelId := responsesRoutingAttributedChannelID(c, c.GetInt("channel_id"))
	other := make(map[string]interface{})
	if c.Request != nil && c.Request.URL != nil {
		other["request_path"] = c.Request.URL.Path
	}
	other["error_type"] = err.GetErrorType()
	other["error_code"] = err.GetErrorCode()
	other["status_code"] = err.StatusCode
	other["error_category"] = classifyRelayErrorCategory(c, relayInfo, err)
	openAIError := err.ToOpenAIError()
	if errorType := sanitizeRelayErrorField(openAIError.Type); errorType != "" {
		other["upstream_error_type"] = errorType
	}
	if errorCode := sanitizeRelayErrorField(fmt.Sprint(openAIError.Code)); errorCode != "" && errorCode != "<nil>" {
		other["upstream_error_code"] = errorCode
	}
	if errorParam := sanitizeRelayErrorField(openAIError.Param); errorParam != "" {
		other["error_param"] = errorParam
	}
	if errorEvent := common.GetContextKeyString(c, constant.ContextKeyResponsesErrorEvent); errorEvent != "" {
		other["error_event"] = normalizeRelayErrorEvent(errorEvent)
	}
	other["channel_id"] = channelId
	other["channel_name"] = c.GetString("channel_name")
	other["channel_type"] = c.GetInt("channel_type")
	adminInfo := make(map[string]interface{})
	appendChannelErrorAdminInfo(c, adminInfo)
	service.AppendChannelHealthRoutingAdminInfo(c, adminInfo)
	other["admin_info"] = adminInfo
	service.AppendRequestHeaderPolicyInfo(c, other)
	service.AppendResponsesRelayInfo(c, relayInfo, other)
	service.AppendResponsesRequestShape(c, relayInfo, other)
	service.AppendStreamStatusForLog(relayInfo, other)
	startTime := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
	if startTime.IsZero() {
		startTime = time.Now()
	}
	useTimeSeconds := int(relaycommon.SafeElapsedSeconds(startTime, time.Now()))
	service.MarkResponsesCompactErrorLog(c, true)
	contentParts, other := service.AppendResponsesCompactLogInfo(c, relayInfo, []string{err.MaskSensitiveErrorWithStatusCode()}, other, time.Now())
	service.MarkResponsesCompactErrorLog(c, false)
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

func sanitizeRelayErrorField(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
	// error_param is populated from upstream-controlled protocol fields. Always
	// pass it through the common masker: a credential can appear in an
	// unschemed host/query value that has no obvious auth marker.
	value = common.MaskSensitiveInfo(value)
	if len([]rune(value)) > 128 {
		value = string([]rune(value)[:128])
	}
	return strings.TrimSpace(value)
}

// normalizeRelayErrorEvent keeps the error-log event dimension bounded to
// values produced by the relay itself. The context value is request-scoped but
// can still be populated by an adapter or future integration; storing an
// arbitrary event name would make logs high-cardinality and could expose
// upstream payload fragments. Unknown values are intentionally collapsed.
func normalizeRelayErrorEvent(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "json", "sse", "response.error", "error", "response.failed":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "unknown"
	}
}

const relayCloudflareTimeoutStatusCode = 524

func classifyRelayErrorCategory(c *gin.Context, relayInfo *relaycommon.RelayInfo, err *types.NewAPIError) string {
	if err == nil {
		return "local_error"
	}
	if c != nil && c.Request != nil {
		if requestErr := c.Request.Context().Err(); errors.Is(requestErr, context.Canceled) {
			return "client_canceled"
		}
	}
	if errors.Is(err, context.Canceled) {
		return "client_canceled"
	}
	if types.IsChannelError(err) {
		if err.GetErrorCode() == types.ErrorCodeChannelModelMappedError {
			return "local_capability"
		}
		return "local_channel"
	}
	if relayInfo != nil && relayInfo.StreamStatus != nil {
		switch relayInfo.StreamStatus.EndReason {
		case relaycommon.StreamEndReasonClientGone:
			if relayInfo.StreamStatus.IsCanceled() {
				return "client_canceled"
			}
		case relaycommon.StreamEndReasonTimeout, relaycommon.StreamEndReasonFirstDataTimeout:
			return "upstream_timeout"
		case relaycommon.StreamEndReasonUpstreamInterrupted:
			return "upstream_transport"
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || err.StatusCode == http.StatusRequestTimeout || err.StatusCode == http.StatusGatewayTimeout || err.StatusCode == relayCloudflareTimeoutStatusCode {
		return "upstream_timeout"
	}
	if isUpstreamRelayErrorCode(err.GetErrorCode()) {
		if err.StatusCode >= http.StatusBadRequest && err.StatusCode < http.StatusInternalServerError {
			return "upstream_4xx"
		}
		if err.StatusCode >= http.StatusInternalServerError {
			return "upstream_5xx"
		}
	}
	if err.GetErrorCode() == types.ErrorCodeUpstreamTransportInterrupted {
		return "upstream_transport"
	}
	if isLocalRelayErrorCode(err.GetErrorCode()) || err.GetErrorType() == types.ErrorTypeNewAPIError {
		return "local_error"
	}
	if err.StatusCode >= http.StatusBadRequest && err.StatusCode < http.StatusInternalServerError {
		return "upstream_4xx"
	}
	if err.StatusCode >= http.StatusInternalServerError {
		return "upstream_5xx"
	}
	return "local_error"
}

func isUpstreamRelayErrorCode(code types.ErrorCode) bool {
	switch code {
	case types.ErrorCodeBadResponseStatusCode,
		types.ErrorCodeReadResponseBodyFailed,
		types.ErrorCodeBadResponse,
		types.ErrorCodeBadResponseBody,
		types.ErrorCodeEmptyResponse:
		return true
	default:
		return false
	}
}

func isLocalRelayErrorCode(code types.ErrorCode) bool {
	switch code {
	case types.ErrorCodeInvalidRequest,
		types.ErrorCodeSensitiveWordsDetected,
		types.ErrorCodeCountTokenFailed,
		types.ErrorCodeModelPriceError,
		types.ErrorCodeInvalidApiType,
		types.ErrorCodeJsonMarshalFailed,
		types.ErrorCodeDoRequestFailed,
		types.ErrorCodeGetChannelFailed,
		types.ErrorCodeGenRelayInfoFailed,
		types.ErrorCodeReadRequestBodyFailed,
		types.ErrorCodeConvertRequestFailed,
		types.ErrorCodeAccessDenied,
		types.ErrorCodeBadRequestBody,
		types.ErrorCodeQueryDataError,
		types.ErrorCodeUpdateDataError,
		types.ErrorCodeInsufficientUserQuota,
		types.ErrorCodePreConsumeTokenQuotaFailed:
		return true
	default:
		return false
	}
}

func appendChannelErrorAdminInfo(c *gin.Context, adminInfo map[string]interface{}) {
	if c == nil || adminInfo == nil {
		return
	}
	adminInfo["use_channel"] = c.GetStringSlice("use_channel")
	if common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey) {
		adminInfo["is_multi_key"] = true
		adminInfo["multi_key_index"] = common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
	}
	service.AppendChannelAffinityAdminInfo(c, adminInfo)
	service.AppendUpstreamRateLimitRetryAdminInfo(c, adminInfo)
}

// RelayMidjourney handles Midjourney proxy relay requests.
func RelayMidjourney(c *gin.Context) {
	var relayInfo *relaycommon.RelayInfo
	var genRelayInfoErr error
	var mjErr *dto.MidjourneyResponse
	defer func() {
		// Midjourney handlers own health success/failure finalization after they
		// inspect the upstream HTTP and provider response. The controller only
		// releases a probe that was reserved before a handler returned; it must
		// never infer success from a nil handler error.
		if relayInfo != nil && relayInfo.ChannelMeta != nil {
			service.ReleaseChannelHealthProbe(c, relayInfo)
		} else if genRelayInfoErr != nil || mjErr != nil {
			// Distributor may reserve a probe before RelayInfo initialization.
			// Release that attempt-scoped probe on local setup/validation failure.
			service.ReleaseChannelHealthProbeForChannel(c, c.GetInt("channel_id"))
		}
		service.ReleaseChannelConcurrency(c)
	}()

	relayInfo, genRelayInfoErr = relaycommon.GenRelayInfo(c, types.RelayFormatMjProxy, nil, nil)

	if genRelayInfoErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"description": fmt.Sprintf("failed to generate relay info: %s", common.MaskSensitiveInfo(genRelayInfoErr.Error())),
			"type":        "upstream_error",
			"code":        4,
		})
		return
	}

	switch relayInfo.RelayMode {
	case relayconstant.RelayModeMidjourneyNotify:
		mjErr = relay.RelayMidjourneyNotify(c)
	case relayconstant.RelayModeMidjourneyTaskFetch, relayconstant.RelayModeMidjourneyTaskFetchByCondition:
		mjErr = relay.RelayMidjourneyTask(c, relayInfo.RelayMode)
	case relayconstant.RelayModeMidjourneyTaskImageSeed:
		mjErr = relay.RelayMidjourneyTaskImageSeed(c, relayInfo)
	case relayconstant.RelayModeSwapFace:
		mjErr = relay.RelaySwapFace(c, relayInfo)
	default:
		mjErr = relay.RelayMidjourneySubmit(c, relayInfo)
	}
	//err = relayMidjourneySubmit(c, relayMode)
	if mjErr != nil {
		statusCode := http.StatusBadRequest
		if mjErr.Code == 30 {
			mjErr.Result = "当前分组负载已饱和，请稍后再试，或升级账户以提升服务质量。"
			statusCode = http.StatusTooManyRequests
		} else if mjErr.Code == 31 {
			statusCode = http.StatusServiceUnavailable
		}
		c.JSON(statusCode, gin.H{
			"description": common.MaskSensitiveInfo(fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result)),
			"type":        "upstream_error",
			"code":        mjErr.Code,
		})
		channelId := c.GetInt("channel_id")
		logger.LogError(c, fmt.Sprintf("relay error (channel #%d, status code %d): %s", channelId, statusCode, common.MaskSensitiveInfo(fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result))))
	}
}

// RelayNotImplemented responds with a standardized "not implemented" error.
func RelayNotImplemented(c *gin.Context) {
	err := types.OpenAIError{
		Message: "API not implemented",
		Type:    types.StandardizeErrorType(types.OpenAIError{Code: "api_not_implemented"}, http.StatusNotImplemented).Type,
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
	// API/relay 404 必须显式禁缓存：全局 Cache 中间件给非根路径打的是
	// max-age=604800（一周），客户端或中间缓存会把"路由不存在"这个答案缓存
	// 一周，路由上线后仍拿旧 404（第十轮运行态发现）。
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate, private, max-age=0")
	c.JSON(http.StatusNotFound, gin.H{
		"error": err,
	})
}

// RelayTaskFetch handles task-query relay requests.
func RelayTaskFetch(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		respondTaskError(c, service.TaskErrorWrapperLocal(err, "gen_relay_info_failed", http.StatusInternalServerError))
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
		respondTaskError(c, service.TaskErrorWrapperLocal(err, "gen_relay_info_failed", http.StatusInternalServerError))
		return
	}

	if taskErr := relay.ResolveOriginTask(c, relayInfo); taskErr != nil {
		respondTaskError(c, taskErr)
		return
	}
	// Remix/continuation requests intentionally skip channel selection in the
	// distributor because the origin task owns the route. Once ResolveOriginTask
	// restores that immutable requested model, enforce the token allow-list before
	// any provider request can be submitted. Query/content endpoints never enter
	// this handler, so they remain model-less at the middleware boundary.
	if accessErr := middleware.ValidateTokenModelAccess(c, relayInfo.OriginModelName); accessErr != nil {
		code := "token_model_forbidden"
		if errors.Is(accessErr, middleware.ErrTokenModelAccessMissing) {
			code = "token_no_model_access"
		}
		respondTaskError(c, service.TaskErrorWrapperLocal(accessErr, code, http.StatusForbidden))
		return
	}

	var result *relay.TaskSubmitResult
	var taskErr *dto.TaskError
	defer func() {
		finalizeRelayChannelHealthProbe(c, relayInfo, taskErr == nil)
		if taskErr != nil && relayInfo.Billing != nil && !taskErr.NoRefund {
			relayInfo.Billing.Refund(c)
		}
	}()
	// Task submissions use a locked channel for remix/continuation requests and
	// therefore bypass the normal selector. Keep the same attempt-scoped lease
	// lifecycle as synchronous relay requests.
	defer service.ReleaseChannelConcurrency(c)

	retryParam := &service.RetryParam{
		Ctx:        c,
		TokenGroup: relayInfo.TokenGroup,
		ModelName:  relayInfo.OriginModelName,
		Retry:      common.GetPointer(0),
	}

	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		relayInfo.RetryIndex = retryParam.GetRetry()
		common.SetContextKey(c, constant.ContextKeyChannelHeaderProfileAttempt, relayInfo.RetryIndex)
		var channel *model.Channel

		if lockedCh, ok := relayInfo.LockedChannel.(*model.Channel); ok && lockedCh != nil {
			channel = lockedCh
			if relayInfo.TaskRelayInfo != nil && relayInfo.TaskRelayInfo.OriginRouteMappingMissing {
				taskErr = service.TaskErrorWrapperLocal(
					service.ErrTaskRouteMappingMissing,
					"task_route_mapping_missing",
					http.StatusBadGateway,
				)
				break
			}
			var setupErr *types.NewAPIError
			originSnapshot, snapshotErr := relay.TaskRouteSnapshotForContinuation(relayInfo)
			if snapshotErr != nil {
				taskErr = service.TaskErrorWrapperLocal(snapshotErr, "task_route_snapshot_invalid", http.StatusInternalServerError)
				break
			}
			if originSnapshot != nil && originSnapshot.SchemaVersion >= model.TaskRouteSnapshotHeaderPolicySchemaVersion {
				persistedCredential := ""
				if relayInfo.TaskRelayInfo != nil {
					persistedCredential = relayInfo.TaskRelayInfo.OriginRouteCredential
				}
				setupErr = middleware.SetupContextForSelectedChannelWithRouteSnapshotAndCredential(c, channel, relayInfo.OriginModelName, originSnapshot, persistedCredential)
			} else if effectiveModel := strings.TrimSpace(relayInfo.OriginRouteEffectiveModel); effectiveModel != "" {
				setupErr = middleware.SetupContextForSelectedChannelWithEffectiveModel(c, channel, relayInfo.OriginModelName, effectiveModel)
			} else {
				setupErr = middleware.SetupContextForSelectedChannel(c, channel, relayInfo.OriginModelName)
			}
			if setupErr != nil {
				taskErr = service.TaskErrorWrapperLocal(setupErr.Err, "setup_locked_channel_failed", http.StatusInternalServerError)
				break
			}
			acquired, concurrencyErr := acquireSelectedChannelConcurrency(c, channel, true)
			if concurrencyErr != nil {
				taskErr = service.TaskErrorWrapperLocal(concurrencyErr.Err, "channel_concurrency_failed", concurrencyErr.StatusCode)
				break
			}
			if !acquired {
				taskErr = service.TaskErrorWrapperLocal(errors.New("locked channel concurrency limit reached"), "channel_concurrency_exceeded", http.StatusTooManyRequests)
				break
			}
		} else {
			var channelErr *types.NewAPIError
			channel, channelErr = getChannel(c, relayInfo, retryParam)
			if channelErr != nil {
				logger.LogError(c, channelErr.MaskSensitiveErrorWithStatusCode())
				taskErr = taskErrorFromChannelSelectionError(channelErr)
				break
			}
		}

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
		// The channel is now ready for the actual task submission. Local body
		// failures above are not upstream attempts and must not pollute
		// use_channel.
		addUsedChannel(c, channel.Id)

		result, taskErr = relay.RelayTaskSubmit(c, relayInfo)
		if taskErr == nil {
			break
		}

		lockedRateLimit := shouldStopLockedTaskUpstreamRateLimitRetry(relayInfo, taskErr)
		if !taskErr.LocalError {
			if isUpstreamRateLimitError(taskErr.StatusCode) {
				service.ObserveUpstreamRateLimitRetry(c, channel.Id)
			}
			if lockedRateLimit {
				service.TerminateUpstreamRateLimitRetry(c)
			}
			processChannelError(c,
				relayInfo,
				*types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey,
					common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()),
				types.NewOpenAIError(taskErr.Error, types.ErrorCodeBadResponseStatusCode, taskErr.StatusCode))
		}
		if lockedRateLimit {
			break
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

	// A 2xx submission whose response does not expose a usable upstream task ID
	// is intentionally non-retryable: the provider may already have accepted the
	// job. Persist a terminal, zero-quota task using the same route snapshot so
	// the public task ID remains queryable and operators can reconcile the
	// ambiguous provider submission without creating a duplicate job.
	if taskErr != nil && taskErr.NoRefund {
		finalizeAmbiguousTaskBilling(c, relayInfo, result)
		if persistErr := persistAmbiguousTaskSubmission(c, relayInfo, result, taskErr); persistErr != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("persist ambiguous task submission failed: %s", common.MaskSensitiveInfo(persistErr.Error())))
		}
	}

	// ── 成功：先持久化任务，再结算、记录并提交响应 ──
	if taskErr == nil {
		if result == nil || result.RouteSnapshot == nil {
			taskErr = service.TaskErrorWrapperLocal(errors.New("task route snapshot is unavailable after submission"), "build_task_route_snapshot_failed", http.StatusInternalServerError)
		}
		if taskErr == nil {
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
				RequestId:       relayInfo.RequestId,
			}
			task.Quota = result.Quota
			task.Data = result.TaskData
			task.Action = relayInfo.Action
			// RelayTaskSubmit built this snapshot before contacting the provider;
			// persist exactly that immutable identity with the task.
			task.PrivateData.RouteSnapshot = result.RouteSnapshot
			// The task row is also used by asynchronous billing. Persist the same
			// concrete group as the route snapshot instead of leaving "auto" in a
			// field that later quota reconciliation interprets as a real group.
			task.Group = strings.TrimSpace(result.RouteSnapshot.Group)
			if insertErr := task.Insert(); insertErr != nil {
				// The provider has already accepted the job. A local persistence
				// failure must not trigger a second upstream submission or refund the
				// pre-consumed quota. Keep the public response safe and correlate the
				// failure with the generated task/channel/request identifiers in logs.
				common.SysError(fmt.Sprintf("insert task error after upstream acceptance task_id=%s channel_id=%d request_id=%s: %s", task.TaskID, task.ChannelId, relayInfo.RequestId, insertErr.Error()))
				taskErr = service.TaskErrorWrapperLocal(
					fmt.Errorf("task persistence failed after upstream acceptance (public_task_id=%s)", task.TaskID),
					"task_persist_failed",
					http.StatusInternalServerError,
				)
				taskErr.NoRefund = true
				finalizeAmbiguousTaskBilling(c, relayInfo, result)
				// The first insert crossed the provider-acceptance boundary but
				// failed locally. Make the same reconciliation attempt used for
				// other ambiguous submissions so operators retain the public task
				// ID, route snapshot and upstream correlation when the database
				// failure is transient. A second failure is logged and never
				// reopens retry or refund behavior.
				if persistErr := persistAmbiguousTaskSubmission(c, relayInfo, result, taskErr); persistErr != nil {
					logger.LogError(c.Request.Context(), fmt.Sprintf("persist task persistence failure reconciliation failed: %s", common.MaskSensitiveInfo(persistErr.Error())))
				}
			} else {
				// The upstream task is now durable. A settlement failure must not
				// turn this into a retryable submission error, otherwise clients may
				// submit the same provider task twice. Keep the task queryable and
				// leave an explicit operator-visible error in the log.
				var settleErr error
				if settleErr = service.SettleBilling(c, relayInfo, result.Quota); settleErr != nil {
					common.SysError("settle task billing error after durable insert: " + settleErr.Error())
				}
				service.LogTaskConsumption(c, relayInfo, settleErr)
				if result.Response != nil {
					if flushErr := result.Response.FlushTo(c.Writer); flushErr != nil {
						// The task and billing state are already durable; do not
						// invoke the refund path merely because the client socket
						// closed while the response was being written.
						common.SysError("flush task submit response failed after durable insert: " + flushErr.Error())
					}
				}
			}
		}
	}

	if taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

// finalizeAmbiguousTaskBilling closes the request-scoped billing session after
// the provider acceptance boundary has been crossed. The task row deliberately
// keeps Quota=0 so maintenance cannot auto-refund a job whose provider outcome
// is unknown, while the normal settlement and consumption log retain the real
// charge for reconciliation. Failures are operator-visible and never reopen a
// retry/refund path.
func finalizeAmbiguousTaskBilling(c *gin.Context, info *relaycommon.RelayInfo, result *relay.TaskSubmitResult) {
	if c == nil || info == nil {
		return
	}
	actualQuota := info.PriceData.Quota
	if result != nil {
		actualQuota = result.Quota
	}
	if actualQuota < 0 {
		actualQuota = 0
	}
	info.PriceData.Quota = actualQuota
	var settleErr error
	if info.Billing != nil {
		if settleErr = service.SettleBilling(c, info, actualQuota); settleErr != nil {
			common.SysError(fmt.Sprintf("settle ambiguous task billing failed task_id=%s channel_id=%d request_id=%s: %s", info.PublicTaskID, info.ChannelId, info.RequestId, settleErr.Error()))
		}
	}
	if c.Request != nil {
		service.LogTaskConsumption(c, info, settleErr)
	}
}

// persistAmbiguousTaskSubmission records a provider submission that crossed
// the upstream acceptance boundary but could not be associated with a provider
// task ID locally. The row is terminal and has zero quota so the maintenance
// poller cannot later interpret it as a normal task and issue an automatic
// refund. The captured route snapshot and redacted response body are retained
// only for operator reconciliation; no second upstream request is attempted.
func persistAmbiguousTaskSubmission(c *gin.Context, info *relaycommon.RelayInfo, result *relay.TaskSubmitResult, taskErr *dto.TaskError) error {
	if info == nil {
		return errors.New("relay info is nil")
	}
	if result == nil {
		return errors.New("partial task submission result is unavailable")
	}
	if result.RouteSnapshot == nil {
		return errors.New("task route snapshot is unavailable")
	}

	task := model.InitTask(result.Platform, info)
	task.PrivateData.UpstreamTaskID = strings.TrimSpace(result.UpstreamTaskID)
	task.PrivateData.BillingSource = info.BillingSource
	task.PrivateData.SubscriptionId = info.SubscriptionId
	task.PrivateData.TokenId = info.TokenId
	task.PrivateData.BillingContext = &model.TaskBillingContext{
		ModelPrice:      info.PriceData.ModelPrice,
		GroupRatio:      info.PriceData.GroupRatioInfo.GroupRatio,
		ModelRatio:      info.PriceData.ModelRatio,
		OtherRatios:     info.PriceData.OtherRatios,
		OriginModelName: info.OriginModelName,
		PerCallBilling:  common.StringsContains(constant.TaskPricePatches, info.OriginModelName) || info.PriceData.UsePrice,
	}
	task.PrivateData.RouteSnapshot = result.RouteSnapshot
	task.Quota = 0
	task.Data = redactAmbiguousTaskData(result.TaskData)
	task.Action = info.Action
	task.Group = strings.TrimSpace(result.RouteSnapshot.Group)
	task.Status = model.TaskStatusFailure
	task.Progress = "100%"
	task.FinishTime = time.Now().Unix()
	task.FailReason = ambiguousTaskFailureReason(taskErr)

	if err := task.Insert(); err != nil {
		return fmt.Errorf("insert ambiguous task %s: %w", task.TaskID, err)
	}
	logger.LogWarn(c.Request.Context(), fmt.Sprintf("ambiguous upstream task submission persisted: task_id=%s channel_id=%d upstream_task_id=%s", task.TaskID, task.ChannelId, task.PrivateData.UpstreamTaskID))
	return nil
}

const maxAmbiguousTaskDataBytes = 1 << 20

func redactAmbiguousTaskData(body []byte) []byte {
	if len(body) == 0 {
		return nil
	}
	if len(body) > maxAmbiguousTaskDataBytes {
		return omittedAmbiguousTaskData(len(body))
	}
	redacted := service.RedactVideoResponseBody(body)
	var payload any
	if common.Unmarshal(redacted, &payload) != nil || len(redacted) > maxAmbiguousTaskDataBytes {
		return omittedAmbiguousTaskData(len(body))
	}
	return redacted
}

func omittedAmbiguousTaskData(size int) []byte {
	fallback, err := common.Marshal(map[string]any{
		"response_body_omitted": true,
		"response_body_bytes":   size,
	})
	if err == nil {
		return fallback
	}
	return nil
}

func ambiguousTaskFailureReason(taskErr *dto.TaskError) string {
	reason := "ambiguous_task_submission"
	if taskErr != nil && strings.TrimSpace(taskErr.Message) != "" {
		reason += ": " + strings.TrimSpace(taskErr.Message)
	}
	reason = common.MaskSensitiveInfo(reason)
	reason = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, reason)
	const maxReasonRunes = 512
	runes := []rune(reason)
	if len(runes) > maxReasonRunes {
		runes = runes[:maxReasonRunes]
	}
	return strings.TrimSpace(string(runes))
}

// taskErrorFromChannelSelectionError keeps selector-level capacity semantics
// visible to task clients. In particular, an ordinary task route that cannot
// find a free channel must remain a 429 instead of being flattened into a 500.
func taskErrorFromChannelSelectionError(channelErr *types.NewAPIError) *dto.TaskError {
	if channelErr == nil {
		return nil
	}

	statusCode := channelErr.StatusCode
	if statusCode < http.StatusBadRequest || statusCode > 599 {
		statusCode = http.StatusInternalServerError
	}

	code := "get_channel_failed"
	switch channelErr.GetErrorCode() {
	case types.ErrorCodeChannelConcurrencyExceeded, types.ErrorCodeChannelConcurrencyUnavailable:
		code = string(channelErr.GetErrorCode())
	}

	err := channelErr.Err
	if err == nil {
		err = channelErr
	}
	return service.TaskErrorWrapperLocal(err, code, statusCode)
}

func shouldStopLockedTaskUpstreamRateLimitRetry(info *relaycommon.RelayInfo, taskErr *dto.TaskError) bool {
	if info == nil || info.TaskRelayInfo == nil || taskErr == nil || taskErr.LocalError || !isUpstreamRateLimitError(taskErr.StatusCode) {
		return false
	}
	lockedChannel, ok := info.LockedChannel.(*model.Channel)
	return ok && lockedChannel != nil
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
	if taskErr.NoRefund {
		// The upstream may already have accepted the asynchronous job. Retrying
		// would risk creating a duplicate provider task even if a future adapter
		// reports the ambiguity as a non-local error.
		return false
	}
	if service.IsUpstreamRateLimitRetryTerminal(c) {
		return false
	}
	if taskErr.LocalError {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) &&
		!isUpstreamRateLimitError(taskErr.StatusCode) &&
		!isRetryableModelCapacityMessage(taskErr.Message) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	if taskErr.StatusCode >= 200 && taskErr.StatusCode < 300 {
		return false
	}
	if isRetryableModelCapacityMessage(taskErr.Message) {
		return true
	}
	if taskErr.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if taskErr.StatusCode == 307 {
		return true
	}
	if taskErr.StatusCode/100 == 5 {
		return true
	}
	if taskErr.StatusCode/100 == 2 {
		return false
	}
	// 其余非 2xx（含 400、408、4xx）统一允许换渠道重试：与主中继路径一致，
	// 单条渠道的 4xx 往往只是这条线路或这个 key 的问题。
	return true
}
