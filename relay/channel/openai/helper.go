package openai

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
)

// 辅助函数
func HandleStreamFormat(c *gin.Context, info *relaycommon.RelayInfo, data string, forceFormat bool, thinkToContent bool) error {
	info.SendResponseCount++

	switch info.RelayFormat {
	case types.RelayFormatOpenAI:
		return sendStreamData(c, info, data, forceFormat, thinkToContent)
	case types.RelayFormatClaude:
		return handleClaudeFormat(c, data, info)
	case types.RelayFormatGemini:
		return handleGeminiFormat(c, data, info)
	}
	return nil
}

func handleClaudeFormat(c *gin.Context, data string, info *relaycommon.RelayInfo) error {
	// 提取必须发生在转换之前：模型名门控覆盖不到的上游会把 think 标签原样
	// 放进 content，转换后 Claude 客户端就把原始标记当成正文渲染。
	// 注意：这里只做逐帧提取，不能同时调 FinishThinkTagStream——该函数会
	// 把本帧已被 Extract 消费过的字节重喂进 splitter 并提前 flush，破坏
	// 多帧流的提取（见 06a063ef0 回归）。缓冲字节的释放只在流结束处的
	// HandleFinalResponseWithError 做一次。
	data = relaycommon.ExtractThinkTagsFromChatChunk(info, data)
	var streamResponse dto.ChatCompletionsStreamResponse
	if err := common.Unmarshal(common.StringToByteSlice(data), &streamResponse); err != nil {
		return err
	}

	if streamResponse.Usage != nil {
		info.ClaudeConvertInfo.Usage = streamResponse.Usage
	}
	claudeResponses, err := service.StreamResponseOpenAI2ClaudeWithError(&streamResponse, info)
	if err != nil {
		return err
	}
	for _, resp := range claudeResponses {
		helper.ClaudeData(c, *resp)
	}
	return nil
}

func handleGeminiFormat(c *gin.Context, data string, info *relaycommon.RelayInfo) error {
	// 与 Claude 路径对称：逐帧提取；flush 只在流结束处的
	// HandleFinalResponseWithError 做一次（理由见 handleClaudeFormat 注释）。
	data = relaycommon.ExtractThinkTagsFromChatChunk(info, data)
	var streamResponse dto.ChatCompletionsStreamResponse
	if err := common.Unmarshal(common.StringToByteSlice(data), &streamResponse); err != nil {
		logger.LogError(c, "failed to unmarshal stream response: "+err.Error())
		return err
	}

	geminiResponse, err := service.StreamResponseOpenAI2GeminiWithError(&streamResponse, info)
	if err != nil {
		return err
	}

	// 如果返回 nil，表示没有实际内容，跳过发送
	if geminiResponse == nil {
		return nil
	}

	geminiResponseStr, err := common.Marshal(geminiResponse)
	if err != nil {
		logger.LogError(c, "failed to marshal gemini response: "+err.Error())
		return err
	}

	// send gemini format response
	c.Render(-1, &common.CustomEvent{Data: "data: " + string(geminiResponseStr)})
	_ = helper.FlushWriter(c)
	return nil
}

func ProcessStreamResponse(streamResponse dto.ChatCompletionsStreamResponse, responseTextBuilder *strings.Builder, toolCount *int) error {
	for _, choice := range streamResponse.Choices {
		responseTextBuilder.WriteString(choice.Delta.GetContentString())
		responseTextBuilder.WriteString(choice.Delta.GetReasoningContent())
		if choice.Delta.ToolCalls != nil {
			if len(choice.Delta.ToolCalls) > *toolCount {
				*toolCount = len(choice.Delta.ToolCalls)
			}
			for _, tool := range choice.Delta.ToolCalls {
				responseTextBuilder.WriteString(tool.Function.Name)
				responseTextBuilder.WriteString(tool.Function.Arguments)
			}
		}
	}
	return nil
}

func processTokens(relayMode int, streamItems []string, responseTextBuilder *strings.Builder, toolCount *int) error {
	streamResp := "[" + strings.Join(streamItems, ",") + "]"

	switch relayMode {
	case relayconstant.RelayModeChatCompletions:
		return processChatCompletions(streamResp, streamItems, responseTextBuilder, toolCount)
	case relayconstant.RelayModeCompletions:
		return processCompletions(streamResp, streamItems, responseTextBuilder)
	}
	return nil
}

func streamTokenRelayMode(info *relaycommon.RelayInfo) int {
	if info == nil {
		return relayconstant.RelayModeUnknown
	}
	switch info.RelayMode {
	case relayconstant.RelayModeChatCompletions, relayconstant.RelayModeCompletions:
		return info.RelayMode
	}
	if info.GetFinalRequestRelayFormat() == types.RelayFormatOpenAI {
		return relayconstant.RelayModeChatCompletions
	}
	return info.RelayMode
}

func processChatCompletions(streamResp string, streamItems []string, responseTextBuilder *strings.Builder, toolCount *int) error {
	var streamResponses []dto.ChatCompletionsStreamResponse
	if err := json.Unmarshal(common.StringToByteSlice(streamResp), &streamResponses); err != nil {
		// 一次性解析失败，逐个解析
		common.SysLog("error unmarshalling stream response: " + err.Error())
		for _, item := range streamItems {
			var streamResponse dto.ChatCompletionsStreamResponse
			if err := json.Unmarshal(common.StringToByteSlice(item), &streamResponse); err != nil {
				return err
			}
			if err := ProcessStreamResponse(streamResponse, responseTextBuilder, toolCount); err != nil {
				common.SysLog("error processing stream response: " + err.Error())
			}
		}
		return nil
	}

	// 批量处理所有响应
	for _, streamResponse := range streamResponses {
		for _, choice := range streamResponse.Choices {
			responseTextBuilder.WriteString(choice.Delta.GetContentString())
			responseTextBuilder.WriteString(choice.Delta.GetReasoningContent())
			if choice.Delta.ToolCalls != nil {
				if len(choice.Delta.ToolCalls) > *toolCount {
					*toolCount = len(choice.Delta.ToolCalls)
				}
				for _, tool := range choice.Delta.ToolCalls {
					responseTextBuilder.WriteString(tool.Function.Name)
					responseTextBuilder.WriteString(tool.Function.Arguments)
				}
			}
		}
	}
	return nil
}

func processCompletions(streamResp string, streamItems []string, responseTextBuilder *strings.Builder) error {
	var streamResponses []dto.CompletionsStreamResponse
	if err := json.Unmarshal(common.StringToByteSlice(streamResp), &streamResponses); err != nil {
		// 一次性解析失败，逐个解析
		common.SysLog("error unmarshalling stream response: " + err.Error())
		for _, item := range streamItems {
			var streamResponse dto.CompletionsStreamResponse
			if err := json.Unmarshal(common.StringToByteSlice(item), &streamResponse); err != nil {
				continue
			}
			for _, choice := range streamResponse.Choices {
				responseTextBuilder.WriteString(choice.Text)
			}
		}
		return nil
	}

	// 批量处理所有响应
	for _, streamResponse := range streamResponses {
		for _, choice := range streamResponse.Choices {
			responseTextBuilder.WriteString(choice.Text)
		}
	}
	return nil
}

func handleLastResponse(lastStreamData string, responseId *string, createAt *int64,
	systemFingerprint *string, model *string, usage **dto.Usage,
	containStreamUsage *bool, info *relaycommon.RelayInfo,
	shouldSendLastResp *bool) error {

	var lastStreamResponse dto.ChatCompletionsStreamResponse
	if err := common.Unmarshal(common.StringToByteSlice(lastStreamData), &lastStreamResponse); err != nil {
		return err
	}

	*responseId = lastStreamResponse.Id
	*createAt = lastStreamResponse.Created
	*systemFingerprint = lastStreamResponse.GetSystemFingerprint()
	*model = lastStreamResponse.Model

	if service.ValidUsage(lastStreamResponse.Usage) {
		*containStreamUsage = true
		*usage = lastStreamResponse.Usage
		if !info.ShouldIncludeUsage {
			*shouldSendLastResp = lo.SomeBy(lastStreamResponse.Choices, func(choice dto.ChatCompletionsStreamResponseChoice) bool {
				return choice.Delta.GetContentString() != "" || choice.Delta.GetReasoningContent() != ""
			})
		}
	}

	return nil
}

func HandleFinalResponse(c *gin.Context, info *relaycommon.RelayInfo, lastStreamData string,
	responseId string, createAt int64, model string, systemFingerprint string,
	usage *dto.Usage, containStreamUsage bool) {
	_ = HandleFinalResponseWithError(c, info, lastStreamData, responseId, createAt, model, systemFingerprint, usage, containStreamUsage)
}

// flushThinkTagFrame builds a chat frame carrying the bytes the think-tag
// splitter still held at end of stream. The finish_reason is required so the
// downstream converters treat it as a terminal frame and run their full
// closing state machine instead of swallowing the bytes.
func flushThinkTagFrame(visible string, reasoning string) *dto.ChatCompletionsStreamResponse {
	delta := dto.ChatCompletionsStreamResponseChoiceDelta{}
	if reasoning != "" {
		delta.ReasoningContent = &reasoning
	}
	if visible != "" {
		delta.SetContentString(visible)
	}
	finishReason := "stop"
	return &dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl-thinktag-flush",
		Model: "",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta:        delta,
			FinishReason: &finishReason,
		}},
	}
}

func HandleFinalResponseWithError(c *gin.Context, info *relaycommon.RelayInfo, lastStreamData string,
	responseId string, createAt int64, model string, systemFingerprint string,
	usage *dto.Usage, containStreamUsage bool) error {

	switch info.RelayFormat {
	case types.RelayFormatOpenAI:
		if info.ShouldIncludeUsage && !containStreamUsage {
			response := helper.GenerateFinalUsageResponse(responseId, createAt, model, *usage)
			response.SetSystemFingerprint(systemFingerprint)
			helper.ObjectData(c, response)
		}
		helper.Done(c)
		return nil

	case types.RelayFormatClaude:
		// 流结束的一次性 flush：splitter 缓冲的字节（截断在 think 块内、帧尾
		// 未闭合标记前缀等场景）作为独立伪帧发送，带 finish_reason 保证转换器
		// 走完整终止状态机。不能附着到终端帧本身：include_usage 时终端帧的
		// choices 为空，附着会产生一个无 finish_reason 的伪造 choice，转换器
		// 既不消费字节也不发终止事件，流就此悬挂。
		if visible, reasoning := relaycommon.FlushThinkTagStream(info); visible != "" || reasoning != "" {
			// 上游最后一个内容帧可能已自带 usage+finish_reason，转换器处理它
			// 时就终止了 message 生命周期（Done=true）。flush 伪帧必须绕过该
			// gate 才能把缓冲字节送出；转换后恢复，让终端帧保持被拦截。
			wasDone := info.ClaudeConvertInfo.Done
			info.ClaudeConvertInfo.Done = false
			flushResponses, flushErr := service.StreamResponseOpenAI2ClaudeWithError(
				flushThinkTagFrame(visible, reasoning), info)
			info.ClaudeConvertInfo.Done = wasDone
			if flushErr != nil {
				return flushErr
			}
			for _, resp := range flushResponses {
				_ = helper.ClaudeData(c, *resp)
			}
		}
		var streamResponse dto.ChatCompletionsStreamResponse
		if err := common.Unmarshal(common.StringToByteSlice(lastStreamData), &streamResponse); err != nil {
			return err
		}

		info.ClaudeConvertInfo.Usage = usage

		claudeResponses, err := service.StreamResponseOpenAI2ClaudeWithError(&streamResponse, info)
		if err != nil {
			return err
		}
		for _, resp := range claudeResponses {
			_ = helper.ClaudeData(c, *resp)
		}
		info.ClaudeConvertInfo.Done = true
		return nil

	case types.RelayFormatGemini:
		// 与 Claude 分支同：flush 字节作为独立伪帧（带 finish_reason）发送。
		if visible, reasoning := relaycommon.FlushThinkTagStream(info); visible != "" || reasoning != "" {
			geminiFlush, geminiErr := service.StreamResponseOpenAI2GeminiWithError(
				flushThinkTagFrame(visible, reasoning), info)
			if geminiErr != nil {
				return geminiErr
			}
			if geminiFlush != nil {
				geminiStr, marshalErr := common.Marshal(geminiFlush)
				if marshalErr != nil {
					return marshalErr
				}
				c.Render(-1, &common.CustomEvent{Data: "data: " + string(geminiStr)})
				_ = helper.FlushWriter(c)
			}
		}
		var streamResponse dto.ChatCompletionsStreamResponse
		if err := common.Unmarshal(common.StringToByteSlice(lastStreamData), &streamResponse); err != nil {
			return err
		}

		// 这里处理的是 openai 最后一个流响应，其 delta 为空，有 finish_reason 字段
		// 因此相比较于 google 官方的流响应，由 openai 转换而来会多一个 parts 为空，finishReason 为 STOP 的响应
		// 而包含最后一段文本输出的响应（倒数第二个）的 finishReason 为 null
		// 暂不知是否有程序会不兼容。

		geminiResponse, err := service.StreamResponseOpenAI2GeminiWithError(&streamResponse, info)
		if err != nil {
			return err
		}

		// openai 流响应开头的空数据
		if geminiResponse == nil {
			return nil
		}

		geminiResponseStr, err := common.Marshal(geminiResponse)
		if err != nil {
			return err
		}

		// 发送最终的 Gemini 响应
		c.Render(-1, &common.CustomEvent{Data: "data: " + string(geminiResponseStr)})
		_ = helper.FlushWriter(c)
		return nil
	}
	return nil
}

// isOpaqueFutureEventData reports whether an SSE frame carries the opaque
// future-extension markers. Frames that carry them are preserved verbatim for
// forward compatibility, so any rewrite must skip them.
func isOpaqueFutureEventData(data string) bool {
	return strings.Contains(data, `"future_event"`) ||
		strings.Contains(data, `"future_response"`) ||
		strings.Contains(data, `"future_item"`)
}

func sendResponsesStreamData(c *gin.Context, info *relaycommon.RelayInfo, streamResponse dto.ResponsesStreamResponse, data string) {
	if data == "" {
		return
	}
	// 上游 echo 回归一化的工具别名时，转发前换回客户端原始名；无别名
	// 或无 restorer 时逐字节原样转发。
	if restorer := responsesToolNameRestorer(info); restorer != nil {
		if restored, changed := restorer.RestoreRawJSON(common.StringToByteSlice(data)); changed {
			data = string(restored)
		}
	}
	// usage 嵌在 response.usage 的 terminal 事件也要补齐 token-detail 对象，
	// 与非流式路径保持一致的客户端可见形状。计费用已解码 struct，不受影响。
	// 带 future_* 标记的 opaque 负载承诺逐字节保留，不参与任何改写。
	if !isOpaqueFutureEventData(data) {
		if filled, changed := ensureUsageDetailObjectsAt(common.StringToByteSlice(data), "response.usage"); changed {
			data = string(filled)
		}
	}
	common.SetContextKey(c, appconstant.ContextKeyResponsesStreamPreludeRetrySafe, false)
	helper.ResponseChunkData(c, streamResponse, data)
}
