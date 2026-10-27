package openai

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel/openrouter"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"

	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func sendStreamData(c *gin.Context, info *relaycommon.RelayInfo, data string, forceFormat bool, thinkToContent bool) error {
	if data == "" {
		return nil
	}

	if !forceFormat && !thinkToContent {
		return helper.StringData(c, data)
	}

	var lastStreamResponse dto.ChatCompletionsStreamResponse
	if err := common.UnmarshalJsonStr(data, &lastStreamResponse); err != nil {
		return err
	}

	if !thinkToContent {
		return helper.ObjectData(c, lastStreamResponse)
	}

	hasThinkingContent := false
	hasContent := false
	hasFinishReason := false
	var thinkingContent strings.Builder
	for _, choice := range lastStreamResponse.Choices {
		if len(choice.Delta.GetReasoningContent()) > 0 {
			hasThinkingContent = true
			thinkingContent.WriteString(choice.Delta.GetReasoningContent())
		}
		if len(choice.Delta.GetContentString()) > 0 {
			hasContent = true
		}
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			hasFinishReason = true
		}
	}

	thinkingInfo := &info.ThinkingContentInfo

	if lastStreamResponse.Choices == nil || len(lastStreamResponse.Choices) == 0 {
		// 空 choices 帧（usage-only / 心跳）：没有正文与终止语义，原样透传。
		return helper.ObjectData(c, lastStreamResponse)
	}

	// Handle think to content conversion。
	//
	// 一帧可能同时携带 reasoning 与可见 content，交付顺序固定为：
	// 推理帧（进入/继续 think 块）→ 闭合帧（正文或终止词到来时）→ 主帧
	// （正文 + finish_reason）。闭合标签绝不落在一帧正文之后，正文也绝不
	// 被推理覆盖（第十轮修复：此前同帧正文被 SetContentString 丢弃，纯
	// 推理流的 think 块永不闭合）。
	if hasThinkingContent {
		thinkFrame := lastStreamResponse.Copy()
		prefix := ""
		if thinkingInfo.IsFirstThinkingContent {
			prefix = "<think>\n"
			thinkingInfo.IsFirstThinkingContent = false
		}
		thinkingInfo.HasSentThinkingContent = true
		for i := range thinkFrame.Choices {
			thinkFrame.Choices[i].Delta.SetContentString(prefix + thinkingContent.String())
			thinkFrame.Choices[i].Delta.ReasoningContent = nil
			thinkFrame.Choices[i].Delta.ThinkingContent = nil
			thinkFrame.Choices[i].Delta.Reasoning = nil
			thinkFrame.Choices[i].FinishReason = nil
		}
		if err := helper.ObjectData(c, thinkFrame); err != nil {
			return err
		}
	}

	if thinkingInfo.HasSentThinkingContent && !thinkingInfo.SendLastThinkingContent &&
		(hasContent || hasFinishReason) {
		closeFrame := lastStreamResponse.Copy()
		for i := range closeFrame.Choices {
			closeFrame.Choices[i].Delta.SetContentString("\n</think>\n")
			closeFrame.Choices[i].Delta.ReasoningContent = nil
			closeFrame.Choices[i].Delta.ThinkingContent = nil
			closeFrame.Choices[i].Delta.Reasoning = nil
			closeFrame.Choices[i].FinishReason = nil
		}
		thinkingInfo.SendLastThinkingContent = true
		if err := helper.ObjectData(c, closeFrame); err != nil {
			return err
		}
	}

	// 推理-only 帧（无正文、无 finish_reason）的内容已由推理帧交付，
	// 不再补发空帧。
	if hasThinkingContent && !hasContent && !hasFinishReason {
		return nil
	}

	for i := range lastStreamResponse.Choices {
		lastStreamResponse.Choices[i].Delta.ReasoningContent = nil
		lastStreamResponse.Choices[i].Delta.ThinkingContent = nil
		lastStreamResponse.Choices[i].Delta.Reasoning = nil
	}
	return helper.ObjectData(c, lastStreamResponse)
}

func OaiStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid response or response body")
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	defer service.CloseResponseBodyGracefully(resp)

	// 首个内容帧之前只可能发出 role/空 delta 帧；这段窗口内断流可以换渠道
	// 重试。内容帧到达时由 trackStreamPreludeCommit 清掉标记。
	common.SetContextKey(c, constant.ContextKeyResponsesStreamPreludeRetrySafe, true)

	model := info.UpstreamModelName
	var responseId string
	var createAt int64 = 0
	var systemFingerprint string
	var containStreamUsage bool
	var responseTextBuilder strings.Builder
	var toolCount int
	var usage = &dto.Usage{}
	var streamItems []string // store stream items
	var lastStreamData string
	var secondLastStreamData string // 存储倒数第二个stream data，用于音频模型
	var streamFrameError *types.OpenAIError

	// 检查是否为音频模型
	isAudioModel := strings.Contains(strings.ToLower(model), "audio")

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		// 上游可能在 HTTP 200 的 SSE 流内用 error 帧报告限流、配额或内容策略
		// 拒绝。该帧不能作为普通 chunk 转发给客户端，也不能让请求按成功结算。
		if frameError := streamErrorFrameWithContent(data); frameError != nil {
			streamFrameError = frameError
			info.StreamStatus.RecordError(frameErrorMessageForStatus(frameError))
			lastStreamData = "" // 丢弃错误帧本身，不作为内容转发
			sr.Stop(fmt.Errorf("%s", frameError.Message))
			return
		}
		if lastStreamData != "" {
			if err := HandleStreamFormat(c, info, lastStreamData, info.ChannelSetting.ForceFormat, info.ChannelSetting.ThinkingToContent); err != nil {
				common.SysLog("error handling stream format: " + err.Error())
				sr.Stop(err)
				return
			}
		}
		if len(data) > 0 {
			// 对音频模型，保存倒数第二个stream data
			if isAudioModel && lastStreamData != "" {
				secondLastStreamData = lastStreamData
			}

			lastStreamData = data
			streamItems = append(streamItems, data)
		}
	})
	if info.StreamStatus != nil && info.StreamStatus.EndReason == relaycommon.StreamEndReasonHandlerStop && info.StreamStatus.EndError != nil {
		return nil, types.NewOpenAIError(info.StreamStatus.EndError, types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
	}

	// 帧级错误在 HTTP 200 流内到达：按上游错误内容映射状态码返回，且不产出
	// 计费 usage（失败请求不能按成功结算）。
	if streamFrameError != nil {
		statusCode := frameErrorStatusCode(streamFrameError)
		return nil, types.WithOpenAIError(*streamFrameError, statusCode, types.ErrOptionWithSkipRetry())
	}

	// 最后一帧在扫描期间不会经过 HandleStreamFormat（它被压后到流结束才
	// 下发），这里补一次提交判定：末帧带内容时，中断就不能再重试。
	trackStreamPreludeCommit(c, lastStreamData)
	if newAPIError := retryableUpstreamStreamInterruptedError(c, info); newAPIError != nil {
		return nil, newAPIError
	}

	// 对音频模型，从倒数第二个stream data中提取usage信息
	if isAudioModel && secondLastStreamData != "" {
		var streamResp struct {
			Usage *dto.Usage `json:"usage"`
		}
		err := common.Unmarshal([]byte(secondLastStreamData), &streamResp)
		if err == nil && streamResp.Usage != nil && service.ValidUsage(streamResp.Usage) {
			usage = streamResp.Usage
			containStreamUsage = true

			if common.DebugEnabled {
				logger.LogDebug(c, fmt.Sprintf("Audio model usage extracted from second last SSE: PromptTokens=%d, CompletionTokens=%d, TotalTokens=%d, InputTokens=%d, OutputTokens=%d",
					usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens,
					usage.InputTokens, usage.OutputTokens))
			}
		}
	}

	// 处理最后的响应
	shouldSendLastResp := true
	processedLastStreamData, err := handleLastResponse(lastStreamData, &responseId, &createAt, &systemFingerprint, &model, &usage,
		&containStreamUsage, info, &shouldSendLastResp)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("error handling last response: %s, lastStreamData: [%s]", err.Error(), lastStreamData))
	}
	lastStreamData = processedLastStreamData

	if info.RelayFormat == types.RelayFormatOpenAI {
		if shouldSendLastResp {
			_ = sendStreamData(c, info, lastStreamData, info.ChannelSetting.ForceFormat, info.ChannelSetting.ThinkingToContent)
		}
	}

	// 处理token计算
	if err := processTokens(streamTokenRelayMode(info), streamItems, &responseTextBuilder, &toolCount); err != nil {
		logger.LogError(c, "error processing tokens: "+err.Error())
	}

	if !containStreamUsage {
		usage = service.ResponseText2Usage(c, responseTextBuilder.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		usage.CompletionTokens += toolCount * 7
	}

	applyUsagePostProcessing(info, usage, common.StringToByteSlice(lastStreamData))

	if err := HandleFinalResponseWithError(c, info, lastStreamData, responseId, createAt, model, systemFingerprint, usage, containStreamUsage); err != nil {
		return usage, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
	}

	return usage, nil
}

// streamErrorFrameWithContent reports whether an SSE data frame is an upstream
// error frame carrying real content. GetOpenAIError is non-nil for any present
// `error` key, so a content check is required: benign placeholder frames such
// as `{"error":{}}` or `{"error":{"message":""}}` must not abort a healthy
// stream, while a code-only frame is still a failure.
func streamErrorFrameWithContent(data string) *types.OpenAIError {
	var frame struct {
		Error *types.OpenAIError `json:"error"`
	}
	if !strings.Contains(data, `"error"`) {
		return nil
	}
	if err := common.Unmarshal([]byte(data), &frame); err != nil {
		return nil
	}
	if frame.Error == nil {
		return nil
	}
	if !frame.Error.HasContent() {
		return nil
	}
	return frame.Error
}

// frameErrorStatusCode maps an upstream error frame to a client status code.
// Rate-limit and quota codes get their canonical statuses; everything else
// surfaces as a bad gateway so the failure is attributed to upstream.
func frameErrorStatusCode(frameError *types.OpenAIError) int {
	if frameError == nil {
		return http.StatusBadGateway
	}
	code := strings.ToLower(fmt.Sprintf("%v", frameError.Code))
	errorType := strings.ToLower(frameError.Type)
	if code == "rate_limit_exceeded" || strings.Contains(errorType, "rate_limit") {
		return http.StatusTooManyRequests
	}
	if code == "insufficient_quota" || strings.Contains(errorType, "insufficient_quota") {
		return http.StatusPaymentRequired
	}
	return http.StatusBadGateway
}

func frameErrorMessageForStatus(frameError *types.OpenAIError) string {
	if frameError == nil {
		return ""
	}
	return fmt.Sprintf("upstream error frame: type=%s code=%s message=%s", frameError.Type, fmt.Sprintf("%v", frameError.Code), frameError.Message)
}

func retryableUpstreamStreamInterruptedError(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	// 判定逻辑与 Claude 渠道共用，见 relay/common/stream_interrupt.go。
	return relaycommon.RetryableUpstreamStreamInterruptedError(c, info)
}

func OpenaiHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	var simpleResponse dto.OpenAITextResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	if common.DebugEnabled {
		println("upstream response body:", string(responseBody))
	}
	// Unmarshal to simpleResponse
	if info.ChannelType == constant.ChannelTypeOpenRouter && info.ChannelOtherSettings.IsOpenRouterEnterprise() {
		// 尝试解析为 openrouter enterprise
		var enterpriseResponse openrouter.OpenRouterEnterpriseResponse
		err = common.Unmarshal(responseBody, &enterpriseResponse)
		if err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
		if enterpriseResponse.Success {
			responseBody = enterpriseResponse.Data
		} else {
			logger.LogError(c, fmt.Sprintf("openrouter enterprise response success=false, data: %s", enterpriseResponse.Data))
			return nil, types.NewOpenAIError(fmt.Errorf("openrouter response success=false"), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
	}

	err = common.Unmarshal(responseBody, &simpleResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	if oaiError := simpleResponse.GetOpenAIError(); oaiError.HasContent() {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	for _, choice := range simpleResponse.Choices {
		if choice.FinishReason == constant.FinishReasonContentFilter {
			common.SetContextKey(c, constant.ContextKeyAdminRejectReason, "openai_finish_reason=content_filter")
			break
		}
	}

	forceFormat := false
	if info.ChannelSetting.ForceFormat {
		forceFormat = true
	}

	usageModified := false
	if simpleResponse.Usage.PromptTokens == 0 {
		completionTokens := simpleResponse.Usage.CompletionTokens
		if completionTokens == 0 {
			for _, choice := range simpleResponse.Choices {
				ctkm := service.CountTextToken(choice.Message.StringContent()+choice.Message.GetReasoningContent(), info.UpstreamModelName)
				completionTokens += ctkm
			}
		}
		simpleResponse.Usage = dto.Usage{
			PromptTokens:     info.GetEstimatePromptTokens(),
			CompletionTokens: completionTokens,
			TotalTokens:      info.GetEstimatePromptTokens() + completionTokens,
		}
		usageModified = true
	}

	applyUsagePostProcessing(info, &simpleResponse.Usage, responseBody)

	switch info.RelayFormat {
	case types.RelayFormatOpenAI:
		if usageModified {
			var bodyMap map[string]interface{}
			err = common.Unmarshal(responseBody, &bodyMap)
			if err != nil {
				return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
			}
			bodyMap["usage"] = simpleResponse.Usage
			responseBody, _ = common.Marshal(bodyMap)
		}
		if forceFormat {
			responseBody, err = common.Marshal(simpleResponse)
			if err != nil {
				return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
			}
		} else {
			break
		}
	case types.RelayFormatClaude:
		claudeResp, conversionErr := service.ResponseOpenAI2ClaudeWithError(&simpleResponse, info)
		if conversionErr != nil {
			return nil, types.NewOpenAIError(conversionErr, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		claudeRespStr, err := common.Marshal(claudeResp)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
		}
		responseBody = claudeRespStr
	case types.RelayFormatGemini:
		geminiResp, conversionErr := service.ResponseOpenAI2GeminiWithError(&simpleResponse, info)
		if conversionErr != nil {
			return nil, types.NewOpenAIError(conversionErr, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		geminiRespStr, err := common.Marshal(geminiResp)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
		}
		responseBody = geminiRespStr
	}

	service.IOCopyBytesGracefully(c, resp, responseBody)

	return &simpleResponse.Usage, nil
}

func streamTTSResponse(c *gin.Context, resp *http.Response) {
	c.Writer.WriteHeaderNow()

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		logger.LogWarn(c, "streaming not supported")
		_, err := io.Copy(c.Writer, resp.Body)
		if err != nil {
			logger.LogWarn(c, err.Error())
		}
		return
	}

	buffer := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buffer)
		//logger.LogInfo(c, fmt.Sprintf("streamTTSResponse read %d bytes", n))
		if n > 0 {
			if _, writeErr := c.Writer.Write(buffer[:n]); writeErr != nil {
				logger.LogError(c, writeErr.Error())
				break
			}
			flusher.Flush()
		}
		if err != nil {
			if err != io.EOF {
				logger.LogError(c, err.Error())
			}
			break
		}
	}
}

func OpenaiRealtimeHandler(c *gin.Context, info *relaycommon.RelayInfo) (*types.NewAPIError, *dto.RealtimeUsage) {
	if info == nil || info.ClientWs == nil || info.TargetWs == nil {
		return types.NewError(fmt.Errorf("invalid websocket connection"), types.ErrorCodeBadResponse), nil
	}

	info.IsStream = true
	clientConn := info.ClientWs
	targetConn := info.TargetWs

	clientClosed := make(chan struct{})
	targetClosed := make(chan struct{})
	errChan := make(chan error, 2)

	usage := &dto.RealtimeUsage{}
	localUsage := &dto.RealtimeUsage{}
	sumUsage := &dto.RealtimeUsage{}

	gopool.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				errChan <- fmt.Errorf("panic in client reader: %v", r)
			}
		}()
		for {
			select {
			case <-c.Done():
				return
			default:
				_, message, err := clientConn.ReadMessage()
				if err != nil {
					if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
						errChan <- fmt.Errorf("error reading from client: %v", err)
					}
					close(clientClosed)
					return
				}

				realtimeEvent := &dto.RealtimeEvent{}
				err = common.Unmarshal(message, realtimeEvent)
				if err != nil {
					errChan <- fmt.Errorf("error unmarshalling message: %v", err)
					return
				}

				if realtimeEvent.Type == dto.RealtimeEventTypeSessionUpdate {
					if realtimeEvent.Session != nil {
						if realtimeEvent.Session.Tools != nil {
							info.RealtimeTools = realtimeEvent.Session.Tools
						}
					}
				}

				upstreamMessage, mappingErr := rewriteRealtimeClientModelFields(message, info)
				if mappingErr != nil {
					errChan <- fmt.Errorf("error mapping realtime model: %v", mappingErr)
					return
				}

				textToken, audioToken, err := service.CountTokenRealtime(info, *realtimeEvent, info.UpstreamModelName)
				if err != nil {
					errChan <- fmt.Errorf("error counting text token: %v", err)
					return
				}
				logger.LogInfo(c, fmt.Sprintf("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
				localUsage.TotalTokens += textToken + audioToken
				localUsage.InputTokens += textToken + audioToken
				localUsage.InputTokenDetails.TextTokens += textToken
				localUsage.InputTokenDetails.AudioTokens += audioToken

				err = helper.WssString(c, targetConn, string(upstreamMessage))
				if err != nil {
					errChan <- fmt.Errorf("error writing to target: %v", err)
					return
				}
			}
		}
	})

	gopool.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				errChan <- fmt.Errorf("panic in target reader: %v", r)
			}
		}()
		for {
			select {
			case <-c.Done():
				return
			default:
				_, message, err := targetConn.ReadMessage()
				if err != nil {
					if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
						errChan <- fmt.Errorf("error reading from target: %v", err)
					}
					close(targetClosed)
					return
				}
				info.SetFirstResponseTime()
				realtimeEvent := &dto.RealtimeEvent{}
				err = common.Unmarshal(message, realtimeEvent)
				if err != nil {
					errChan <- fmt.Errorf("error unmarshalling message: %v", err)
					return
				}

				if realtimeEvent.Type == dto.RealtimeEventTypeResponseDone {
					realtimeUsage := realtimeEvent.Response.Usage
					if realtimeUsage != nil {
						addRealtimeUsage(usage, realtimeUsage)
						err := preConsumeUsage(c, info, usage, sumUsage)
						// 无论扣费成功与否，本 turn 的用量都已并入 sumUsage，
						// 必须清空 usage；否则函数尾部的兜底 preConsumeUsage 会把
						// 同一 turn 再加一次，sumUsage 翻倍并按翻倍值终态结算。
						usage = &dto.RealtimeUsage{}
						if err != nil {
							errChan <- fmt.Errorf("error consume usage: %v", err)
							return
						}
						// 本次计费完成，清除
						localUsage = &dto.RealtimeUsage{}
					} else {
						textToken, audioToken, err := service.CountTokenRealtime(info, *realtimeEvent, info.UpstreamModelName)
						if err != nil {
							errChan <- fmt.Errorf("error counting text token: %v", err)
							return
						}
						logger.LogInfo(c, fmt.Sprintf("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
						localUsage.TotalTokens += textToken + audioToken
						info.IsFirstRequest = false
						localUsage.InputTokens += textToken + audioToken
						localUsage.InputTokenDetails.TextTokens += textToken
						localUsage.InputTokenDetails.AudioTokens += audioToken
						err = preConsumeUsage(c, info, localUsage, sumUsage)
						// 同 ResponseDone 分支：用量已并入 sumUsage，先清空再判断错误，
						// 避免尾部兜底重复累计。
						localUsage = &dto.RealtimeUsage{}
						if err != nil {
							errChan <- fmt.Errorf("error consume usage: %v", err)
							return
						}
						// print now usage
					}
					logger.LogInfo(c, fmt.Sprintf("realtime streaming sumUsage: %v", sumUsage))
					logger.LogInfo(c, fmt.Sprintf("realtime streaming localUsage: %v", localUsage))
					logger.LogInfo(c, fmt.Sprintf("realtime streaming localUsage: %v", localUsage))

				} else if realtimeEvent.Type == dto.RealtimeEventTypeSessionUpdated || realtimeEvent.Type == dto.RealtimeEventTypeSessionCreated {
					realtimeSession := realtimeEvent.Session
					if realtimeSession != nil {
						// update audio format
						info.InputAudioFormat = common.GetStringIfEmpty(realtimeSession.InputAudioFormat, info.InputAudioFormat)
						info.OutputAudioFormat = common.GetStringIfEmpty(realtimeSession.OutputAudioFormat, info.OutputAudioFormat)
					}
				} else {
					textToken, audioToken, err := service.CountTokenRealtime(info, *realtimeEvent, info.UpstreamModelName)
					if err != nil {
						errChan <- fmt.Errorf("error counting text token: %v", err)
						return
					}
					logger.LogInfo(c, fmt.Sprintf("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
					localUsage.TotalTokens += textToken + audioToken
					localUsage.OutputTokens += textToken + audioToken
					localUsage.OutputTokenDetails.TextTokens += textToken
					localUsage.OutputTokenDetails.AudioTokens += audioToken
				}

				err = helper.WssString(c, clientConn, string(message))
				if err != nil {
					errChan <- fmt.Errorf("error writing to client: %v", err)
					return
				}
			}
		}
	})

	select {
	case <-clientClosed:
	case <-targetClosed:
	case err := <-errChan:
		//return service.OpenAIErrorWrapper(err, "realtime_error", http.StatusInternalServerError), nil
		logger.LogError(c, "realtime error: "+err.Error())
	case <-c.Done():
	}

	if usage.TotalTokens != 0 {
		_ = preConsumeUsage(c, info, usage, sumUsage)
	}

	if localUsage.TotalTokens != 0 {
		_ = preConsumeUsage(c, info, localUsage, sumUsage)
	}

	// check usage total tokens, if 0, use local usage

	return nil, sumUsage
}

// rewriteRealtimeClientModelFields enforces the selected route for the model
// fields accepted by session.update. Realtime normally carries the model in
// the handshake query, but compatible providers may also inspect a top-level
// or nested session.model field. Unknown event fields and events without a
// model are preserved; in particular, a no-model event returns its original
// bytes without a marshal/unmarshal round-trip.
func rewriteRealtimeClientModelFields(message []byte, info *relaycommon.RelayInfo) ([]byte, error) {
	if info == nil || strings.TrimSpace(info.UpstreamModelName) == "" {
		return message, nil
	}
	var event map[string]common.RawMessage
	if err := common.Unmarshal(message, &event); err != nil {
		return nil, err
	}
	var eventType string
	if rawType, ok := event["type"]; ok {
		_ = common.Unmarshal(rawType, &eventType)
	}
	if eventType != dto.RealtimeEventTypeSessionUpdate {
		return message, nil
	}

	changed := false
	if rawModel, ok := event["model"]; ok {
		updated, err := replaceRealtimeModelField(rawModel, info.UpstreamModelName)
		if err != nil {
			return nil, err
		}
		if updated != nil {
			event["model"] = updated
			changed = true
		}
	}
	if rawSession, ok := event["session"]; ok {
		var session map[string]common.RawMessage
		if err := common.Unmarshal(rawSession, &session); err == nil && session != nil {
			if rawModel, modelOK := session["model"]; modelOK {
				updated, err := replaceRealtimeModelField(rawModel, info.UpstreamModelName)
				if err != nil {
					return nil, err
				}
				if updated != nil {
					session["model"] = updated
					rawSession, err = common.Marshal(session)
					if err != nil {
						return nil, err
					}
					event["session"] = rawSession
					changed = true
				}
			}
		}
	}
	if !changed {
		return message, nil
	}
	return common.Marshal(event)
}

func replaceRealtimeModelField(rawModel common.RawMessage, upstreamModel string) (common.RawMessage, error) {
	var modelName string
	if err := common.Unmarshal(rawModel, &modelName); err != nil || strings.TrimSpace(modelName) == "" {
		return nil, nil
	}
	if strings.TrimSpace(modelName) == strings.TrimSpace(upstreamModel) {
		return nil, nil
	}
	return common.Marshal(strings.TrimSpace(upstreamModel))
}

// addRealtimeUsage aggregates one realtime turn's usage into a session total.
// The tiered billing expression can price cache creation, image, and audio
// tokens separately (service/tiered_settle.go reads those detail fields), so
// every detail field a turn reports must survive the aggregation; a field
// dropped here reads as zero in the expression and its tokens are billed at
// the text rate.
func addRealtimeUsage(dst, src *dto.RealtimeUsage) {
	if dst == nil || src == nil {
		return
	}
	dst.TotalTokens += src.TotalTokens
	dst.InputTokens += src.InputTokens
	dst.OutputTokens += src.OutputTokens
	dst.InputTokenDetails.CachedTokens += src.InputTokenDetails.CachedTokens
	dst.InputTokenDetails.CachedCreationTokens += src.InputTokenDetails.CachedCreationTokens
	dst.InputTokenDetails.TextTokens += src.InputTokenDetails.TextTokens
	dst.InputTokenDetails.AudioTokens += src.InputTokenDetails.AudioTokens
	dst.InputTokenDetails.ImageTokens += src.InputTokenDetails.ImageTokens
	dst.OutputTokenDetails.TextTokens += src.OutputTokenDetails.TextTokens
	dst.OutputTokenDetails.AudioTokens += src.OutputTokenDetails.AudioTokens
	dst.OutputTokenDetails.ImageTokens += src.OutputTokenDetails.ImageTokens
}

func preConsumeUsage(ctx *gin.Context, info *relaycommon.RelayInfo, usage *dto.RealtimeUsage, totalUsage *dto.RealtimeUsage) error {
	if usage == nil || totalUsage == nil {
		return fmt.Errorf("invalid usage pointer")
	}

	addRealtimeUsage(totalUsage, usage)
	// clear usage
	err := service.PreWssConsumeQuota(ctx, info, usage)
	return err
}

func OpenaiHandlerWithUsage(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	var usageResp dto.SimpleResponse
	err = common.Unmarshal(responseBody, &usageResp)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	// 写入新的 response body
	service.IOCopyBytesGracefully(c, resp, responseBody)

	// Once we've written to the client, we should not return errors anymore
	// because the upstream has already consumed resources and returned content
	// We should still perform billing even if parsing fails
	// format
	if usageResp.InputTokens > 0 {
		usageResp.PromptTokens += usageResp.InputTokens
	}
	if usageResp.OutputTokens > 0 {
		usageResp.CompletionTokens += usageResp.OutputTokens
	}
	if usageResp.InputTokensDetails != nil {
		usageResp.PromptTokensDetails.ImageTokens += usageResp.InputTokensDetails.ImageTokens
		usageResp.PromptTokensDetails.TextTokens += usageResp.InputTokensDetails.TextTokens
	}
	applyUsagePostProcessing(info, &usageResp.Usage, responseBody)
	return &usageResp.Usage, nil
}

func applyUsagePostProcessing(info *relaycommon.RelayInfo, usage *dto.Usage, responseBody []byte) {
	if info == nil || usage == nil {
		return
	}

	switch info.ChannelType {
	case constant.ChannelTypeDeepSeek:
		if usage.PromptTokensDetails.CachedTokens == 0 && usage.PromptCacheHitTokens != 0 {
			usage.PromptTokensDetails.CachedTokens = usage.PromptCacheHitTokens
		}
	case constant.ChannelTypeZhipu_v4:
		// 智普的cached_tokens在标准位置: usage.prompt_tokens_details.cached_tokens
		if usage.PromptTokensDetails.CachedTokens == 0 {
			if usage.InputTokensDetails != nil && usage.InputTokensDetails.CachedTokens > 0 {
				usage.PromptTokensDetails.CachedTokens = usage.InputTokensDetails.CachedTokens
			} else if cachedTokens, ok := extractCachedTokensFromBody(responseBody); ok {
				usage.PromptTokensDetails.CachedTokens = cachedTokens
			} else if usage.PromptCacheHitTokens > 0 {
				usage.PromptTokensDetails.CachedTokens = usage.PromptCacheHitTokens
			}
		}
	case constant.ChannelTypeMoonshot:
		// Moonshot的cached_tokens在非标准位置: choices[].usage.cached_tokens
		if usage.PromptTokensDetails.CachedTokens == 0 {
			if usage.InputTokensDetails != nil && usage.InputTokensDetails.CachedTokens > 0 {
				usage.PromptTokensDetails.CachedTokens = usage.InputTokensDetails.CachedTokens
			} else if cachedTokens, ok := extractMoonshotCachedTokensFromBody(responseBody); ok {
				usage.PromptTokensDetails.CachedTokens = cachedTokens
			} else if cachedTokens, ok := extractCachedTokensFromBody(responseBody); ok {
				usage.PromptTokensDetails.CachedTokens = cachedTokens
			} else if usage.PromptCacheHitTokens > 0 {
				usage.PromptTokensDetails.CachedTokens = usage.PromptCacheHitTokens
			}
		}
	case constant.ChannelTypeOpenAI:
		if usage.PromptTokensDetails.CachedTokens == 0 {
			if cachedTokens, ok := extractLlamaCachedTokensFromBody(responseBody); ok {
				usage.PromptTokensDetails.CachedTokens = cachedTokens
			}
		}
	}
}

func extractCachedTokensFromBody(body []byte) (int, bool) {
	if len(body) == 0 {
		return 0, false
	}

	var payload struct {
		Usage struct {
			PromptTokensDetails struct {
				CachedTokens *int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			CachedTokens         *int `json:"cached_tokens"`
			PromptCacheHitTokens *int `json:"prompt_cache_hit_tokens"`
		} `json:"usage"`
	}

	if err := common.Unmarshal(body, &payload); err != nil {
		return 0, false
	}

	if payload.Usage.PromptTokensDetails.CachedTokens != nil {
		return *payload.Usage.PromptTokensDetails.CachedTokens, true
	}
	if payload.Usage.CachedTokens != nil {
		return *payload.Usage.CachedTokens, true
	}
	if payload.Usage.PromptCacheHitTokens != nil {
		return *payload.Usage.PromptCacheHitTokens, true
	}
	return 0, false
}

// extractMoonshotCachedTokensFromBody 从Moonshot的非标准位置提取cached_tokens
// Moonshot的流式响应格式: {"choices":[{"usage":{"cached_tokens":111}}]}
func extractMoonshotCachedTokensFromBody(body []byte) (int, bool) {
	if len(body) == 0 {
		return 0, false
	}

	var payload struct {
		Choices []struct {
			Usage struct {
				CachedTokens *int `json:"cached_tokens"`
			} `json:"usage"`
		} `json:"choices"`
	}

	if err := common.Unmarshal(body, &payload); err != nil {
		return 0, false
	}

	// 遍历choices查找cached_tokens
	for _, choice := range payload.Choices {
		if choice.Usage.CachedTokens != nil && *choice.Usage.CachedTokens > 0 {
			return *choice.Usage.CachedTokens, true
		}
	}

	return 0, false
}

// extractLlamaCachedTokensFromBody 从llama.cpp的非标准位置提取cache_n
func extractLlamaCachedTokensFromBody(body []byte) (int, bool) {
	if len(body) == 0 {
		return 0, false
	}

	var payload struct {
		Timings struct {
			CachedTokens *int `json:"cache_n"`
		} `json:"timings"`
	}

	if err := common.Unmarshal(body, &payload); err != nil {
		return 0, false
	}

	if payload.Timings.CachedTokens == nil {
		return 0, false
	}
	return *payload.Timings.CachedTokens, true
}
