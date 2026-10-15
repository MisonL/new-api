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
	// 多帧流的提取（见 06a063ef0 回归）。缓冲字节的常规释放点在流结束处的
	// HandleFinalResponseWithError。
	//
	// 本帧自带 finish_reason（含 usage 的合并型上游）时，转换器处理完本帧
	// 即终止 message 生命周期。缓冲字节若留到 HandleFinalResponseWithError
	// 再发，就会落在 message_stop 之后——严格客户端在 message_stop 处结束
	// 迭代，字节实际丢失。因此转换本帧之前先把缓冲以无 finish_reason 的
	// 伪帧送出（转换器只开块消费 delta，不终止）。
	data = relaycommon.ExtractThinkTagsFromChatChunk(info, data)
	var streamResponse dto.ChatCompletionsStreamResponse
	if err := common.Unmarshal(common.StringToByteSlice(data), &streamResponse); err != nil {
		return err
	}

	if info.ClaudeConvertInfo != nil && !info.ClaudeConvertInfo.Done {
		doneChunk := len(streamResponse.Choices) > 0 &&
			streamResponse.Choices[0].FinishReason != nil &&
			*streamResponse.Choices[0].FinishReason != ""
		// 终端判定必须与转换器一致（convert.go：choices 为空且带 usage 才
		// 走 usage-only 终止分支）。上游会在流中间发空 choices 帧，若仅凭
		// len==0 就排空缓冲，未闭合 <think> 内的推理字节会按"未闭合规则"
		// 作为可见文本归还，且 splitter 复位后标签泄入正文。
		usageOnlyTerminal := len(streamResponse.Choices) == 0 && streamResponse.Usage != nil

		if doneChunk || usageOnlyTerminal {
			if visible, reasoning := relaycommon.FlushThinkTagStream(info); visible != "" || reasoning != "" {
				if doneChunk {
					// 字节并入终端帧 delta，单次转换走完终止状态机：恰好一个
					// message_start（SRC==1 时由终端帧发出）与一组终止事件。
					// 独立伪帧在 SRC==1 时会额外触发一次 message_start。
					mergeFlushIntoDelta(&streamResponse.Choices[0].Delta, visible, reasoning)
				} else {
					// usage-only 终端帧不消费 delta，字节以无 finish_reason 的
					// 伪帧在终端帧之前发出。能走到这里说明前面已有内容帧
					// （否则无缓冲可排空），SRC>=2，伪帧不会触发 message_start。
					flushResponses, flushErr := service.StreamResponseOpenAI2ClaudeWithError(
						flushThinkTagFrame(visible, reasoning), info)
					if flushErr != nil {
						return flushErr
					}
					for _, resp := range flushResponses {
						_ = helper.ClaudeData(c, *resp)
					}
				}
			}
		}
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

// flushThinkTagFrame builds the flush pseudo-frame for usage-only terminals:
// no finish_reason, so the converter consumes the delta (emitting content
// blocks) without closing the lifecycle — the terminal frame closes it once.
func flushThinkTagFrame(visible string, reasoning string) *dto.ChatCompletionsStreamResponse {
	delta := dto.ChatCompletionsStreamResponseChoiceDelta{}
	if reasoning != "" {
		delta.ReasoningContent = &reasoning
	}
	if visible != "" {
		delta.SetContentString(visible)
	}
	return &dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl-thinktag-flush",
		Model: "",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Delta: delta,
		}},
	}
}

// mergeFlushIntoDelta appends the flushed bytes onto a terminal frame's delta
// so the converter emits them in the same conversion pass that closes the
// lifecycle.
func mergeFlushIntoDelta(delta *dto.ChatCompletionsStreamResponseChoiceDelta, visible string, reasoning string) {
	if reasoning != "" {
		existing := delta.GetReasoningContent()
		merged := existing + reasoning
		delta.ReasoningContent = &merged
	}
	if visible != "" {
		delta.SetContentString(delta.GetContentString() + visible)
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
		// 流结束的一次性 flush。常规路径（终端帧带 finish_reason 或
		// usage-only）的缓冲字节已在 handleClaudeFormat 转换该帧之前送出，
		// 这里通常已无缓冲。仍可能剩缓冲的场景：上游流被截断、最后内容帧
		// 不带 finish_reason——此时生命周期未闭合，缓冲并入终端帧 delta 或
		// 以无 finish_reason 的伪帧发出，随后由终端帧闭合一次。伪帧绝不能
		// 带 finish_reason：那会额外闭合一次生命周期（重复 message_stop）
		// 并把 stop_reason 覆盖成 end_turn（tool-use 流回归）。
		//
		// 生命周期已闭合（Done=true）时 FlushThinkTagStream 已在帧前排空，
		// 此处不再产生任何事件，避免 message_stop 之后补发内容。
		//
		// 终端帧自身的 content 也必须过提取：主循环只对非最后一帧调
		// HandleStreamFormat，最后一帧直达这里，不提取则帧内 <think> 标签
		// 会原样作为正文发给客户端（并计入输出 token）。
		//
		// 终端帧不经 HandleStreamFormat，SendResponseCount 还停在最后一条
		// 普通帧上。转换器用 SRC==1 判定"发 message_start"，若不推进：
		// 首个普通帧已让 SRC==1 时，这里的 flush 伪帧与终端帧会被再当作
		// 首响，各多发一个 message_start；且 usage-only 终止分支被
		// SRC==1 分支遮蔽，流不终止。推进一次使终端帧按"下一条响应"处理。
		info.SendResponseCount++
		lastStreamData = relaycommon.ExtractThinkTagsFromChatChunk(info, lastStreamData)
		var terminalFrame dto.ChatCompletionsStreamResponse
		if err := common.Unmarshal(common.StringToByteSlice(lastStreamData), &terminalFrame); err != nil {
			return err
		}
		terminalIsUsageOnly := len(terminalFrame.Choices) == 0

		if !info.ClaudeConvertInfo.Done {
			if visible, reasoning := relaycommon.FlushThinkTagStream(info); visible != "" || reasoning != "" {
				if terminalIsUsageOnly {
					flushResponses, flushErr := service.StreamResponseOpenAI2ClaudeWithError(
						flushThinkTagFrame(visible, reasoning), info)
					if flushErr != nil {
						return flushErr
					}
					for _, resp := range flushResponses {
						_ = helper.ClaudeData(c, *resp)
					}
				} else {
					mergeFlushIntoDelta(&terminalFrame.Choices[0].Delta, visible, reasoning)
					merged, marshalErr := common.Marshal(terminalFrame)
					if marshalErr != nil {
						return marshalErr
					}
					lastStreamData = string(merged)
				}
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
		// 与 Claude 分支同：flush 字节在终端帧之前送出。伪帧不带
		// finish_reason，转换器只输出 parts，不发终止；生命周期由终端帧
		// 闭合一次。终端帧自身 content 同样先过提取（见 Claude 分支说明）。
		lastStreamData = relaycommon.ExtractThinkTagsFromChatChunk(info, lastStreamData)
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
