package cloudflare

import (
	"bufio"
	"fmt"
	"net/http"
	"strings"

	basecommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
)

func convertCf2CompletionsRequest(textRequest dto.GeneralOpenAIRequest) (*CfRequest, error) {
	if err := channel.RejectUnsupportedTextRequestFields(textRequest, "Cloudflare Completions"); err != nil {
		return nil, err
	}
	if len(textRequest.Messages) > 0 {
		return nil, fmt.Errorf("messages are not supported by Cloudflare Completions; use prompt")
	}
	if textRequest.TopP != nil || textRequest.TopK != nil || textRequest.FrequencyPenalty != nil ||
		textRequest.PresencePenalty != nil || textRequest.N != nil || textRequest.Stop != nil ||
		textRequest.StreamOptions != nil || len(textRequest.ExtraBody) > 0 {
		return nil, fmt.Errorf("request contains options that are not supported by Cloudflare Completions")
	}
	var prompt string
	switch value := textRequest.Prompt.(type) {
	case string:
		prompt = value
	case []any:
		if len(value) != 1 {
			return nil, fmt.Errorf("prompt must be a single string for Cloudflare Completions")
		}
		var ok bool
		prompt, ok = value[0].(string)
		if !ok {
			return nil, fmt.Errorf("prompt[0] must be a string for Cloudflare Completions")
		}
	case nil:
		return nil, fmt.Errorf("prompt is required for Cloudflare Completions")
	default:
		return nil, fmt.Errorf("prompt must be a string for Cloudflare Completions")
	}
	return &CfRequest{
		Prompt:      prompt,
		MaxTokens:   textRequest.GetMaxTokens(),
		Stream:      lo.FromPtrOr(textRequest.Stream, false),
		Temperature: textRequest.Temperature,
	}, nil
}

func cfStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*types.NewAPIError, *dto.Usage) {
	scanner := bufio.NewScanner(resp.Body)
	scanner.Split(bufio.ScanLines)
	// 与共享扫描器同一行上限：裸 NewScanner 的 token 上限是 64KiB，上游一帧
	// 超过它就会停在 ErrTooLong，[DONE] 再也读不到，健康流被判成中断。
	scanner.Buffer(make([]byte, helper.InitialScannerBufferSize), helper.GetScannerBufferSize())

	helper.SetEventStreamHeaders(c)
	id := helper.GetResponseID(c)
	var responseText string
	isFirst := true
	sawDone := false

	for scanner.Scan() {
		data := scanner.Text()
		if len(data) < len("data:") || !strings.HasPrefix(data, "data:") {
			continue
		}
		// SSE 规范允许 data: 后不带空格；与共享扫描器一致（stream_scanner.go:343），
		// 否则这类帧会解析失败并被静默丢弃。
		data = strings.TrimSpace(data[len("data:"):])

		if data == "[DONE]" {
			sawDone = true
			break
		}

		var response dto.ChatCompletionsStreamResponse
		err := basecommon.Unmarshal([]byte(data), &response)
		if err != nil {
			logger.LogError(c, "error_unmarshalling_stream_response: "+err.Error())
			continue
		}
		for _, choice := range response.Choices {
			choice.Delta.Role = "assistant"
			responseText += choice.Delta.GetContentString()
		}
		response.Id = id
		response.Model = info.UpstreamModelName
		err = helper.ObjectData(c, response)
		if isFirst {
			isFirst = false
			info.SetFirstResponseTime()
		}
		if err != nil {
			logger.LogError(c, "error_rendering_stream_response: "+err.Error())
		} else {
			// 记录已下发的响应帧数：重试判定依赖它区分"首帧之前断流"
			// （可以换渠道重放）与"已经吐出内容"（重放会重复内容）。
			info.ReceivedResponseCount++
		}
	}

	scanErr := scanner.Err()
	helper.RecordStreamEnd(c, info, sawDone && scanErr == nil)
	if scanErr != nil {
		// 上游传输中断不能被吞成成功：此前只记一行日志，随后照常发送 [DONE]，
		// 客户端据此认为流已正常完成。首帧之前断流返回可重试错误；已吐出内容
		// 时不可重试，但同样不得以 [DONE] 收尾，改为显式错误。
		logger.LogError(c, "error_scanning_stream_response: "+scanErr.Error())
		service.CloseResponseBodyGracefully(resp)
		if newAPIError := relaycommon.RetryableUpstreamStreamInterruptedError(c, info); newAPIError != nil {
			return newAPIError, nil
		}
		return types.NewError(
			fmt.Errorf("cloudflare stream interrupted after content was delivered: %w", scanErr),
			types.ErrorCodeBadResponseBody,
		), nil
	}
	usage := service.ResponseText2Usage(c, responseText, info.UpstreamModelName, info.GetEstimatePromptTokens())
	if info.ShouldIncludeUsage {
		response := helper.GenerateFinalUsageResponse(id, info.StartTime.Unix(), info.UpstreamModelName, *usage)
		err := helper.ObjectData(c, response)
		if err != nil {
			logger.LogError(c, "error_rendering_final_usage_response: "+err.Error())
		}
	}
	helper.Done(c)

	service.CloseResponseBodyGracefully(resp)

	return nil, usage
}

func cfHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*types.NewAPIError, *dto.Usage) {
	defer service.CloseResponseBodyGracefully(resp)
	responseBody, err := basecommon.ReadAllWithLimit(resp.Body, basecommon.UpstreamJSONResponseLimit)
	if err != nil {
		return types.NewError(err, types.ErrorCodeBadResponseBody), nil
	}
	var response dto.TextResponse
	err = basecommon.Unmarshal(responseBody, &response)
	if err != nil {
		return types.NewError(err, types.ErrorCodeBadResponseBody), nil
	}
	response.Model = info.UpstreamModelName
	var responseText string
	for _, choice := range response.Choices {
		responseText += choice.Message.StringContent()
	}
	usage := service.ResponseText2Usage(c, responseText, info.UpstreamModelName, info.GetEstimatePromptTokens())
	response.Usage = *usage
	response.Id = helper.GetResponseID(c)
	jsonResponse, err := basecommon.Marshal(response)
	if err != nil {
		return types.NewError(err, types.ErrorCodeBadResponseBody), nil
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	_, _ = c.Writer.Write(jsonResponse)
	return nil, usage
}

func cfSTTHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*types.NewAPIError, *dto.Usage) {
	var cfResp CfAudioResponse
	defer service.CloseResponseBodyGracefully(resp)
	responseBody, err := basecommon.ReadAllWithLimit(resp.Body, basecommon.UpstreamJSONResponseLimit)
	if err != nil {
		return types.NewError(err, types.ErrorCodeBadResponseBody), nil
	}
	err = basecommon.Unmarshal(responseBody, &cfResp)
	if err != nil {
		return types.NewError(err, types.ErrorCodeBadResponseBody), nil
	}

	audioResp := &dto.AudioResponse{
		Text: cfResp.Result.Text,
	}

	jsonResponse, err := basecommon.Marshal(audioResp)
	if err != nil {
		return types.NewError(err, types.ErrorCodeBadResponseBody), nil
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	_, _ = c.Writer.Write(jsonResponse)

	usage := service.ResponseText2Usage(c, cfResp.Result.Text, info.UpstreamModelName, info.GetEstimatePromptTokens())
	return nil, usage
}
