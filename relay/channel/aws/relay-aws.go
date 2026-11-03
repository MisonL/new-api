package aws

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"

	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	bedrockruntimeTypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/aws/smithy-go/auth/bearer"
	smithymiddleware "github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// getAwsErrorStatusCode extracts HTTP status code from AWS SDK error
func getAwsErrorStatusCode(err error) int {
	// Check for HTTP response error which contains status code
	var httpErr interface{ HTTPStatusCode() int }
	if errors.As(err, &httpErr) {
		return httpErr.HTTPStatusCode()
	}
	// Default to 500 if we can't determine the status code
	return http.StatusInternalServerError
}

// requestContextOf 取 gin 请求的 context。真实请求里 Request 永远非空；
// 仅为 gin 测试上下文（CreateTestContext 不设置 Request）保留空值兜底。
func requestContextOf(c *gin.Context) context.Context {
	if c == nil || c.Request == nil {
		return context.Background()
	}
	return c.Request.Context()
}

// newAwsInvokeContext 在调用方 context 上派生 relay 超时。AWS SDK 只认
// 传进去的 context：用 context.Background() 会让客户端断开、上游取消
// 都无法传到 Bedrock 调用，请求会一直挂到 SDK 自己的重试耗尽。
func newAwsInvokeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if common.RelayTimeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, time.Duration(common.RelayTimeout)*time.Second)
}

type awsRateLimitAwareRetryer struct {
	aws.Retryer
}

func (r awsRateLimitAwareRetryer) IsErrorRetryable(err error) bool {
	var httpErr interface{ HTTPStatusCode() int }
	if errors.As(err, &httpErr) && httpErr.HTTPStatusCode() == http.StatusTooManyRequests {
		return false
	}
	return r.Retryer.IsErrorRetryable(err)
}

func newAwsRateLimitAwareRetryer() aws.Retryer {
	return awsRateLimitAwareRetryer{Retryer: retry.NewStandard()}
}

func newAwsClient(c *gin.Context, info *relaycommon.RelayInfo) (*bedrockruntime.Client, error) {
	if info == nil || info.ChannelMeta == nil {
		return nil, errors.New("AWS relay info or channel metadata is nil")
	}
	var (
		httpClient *http.Client
		err        error
	)
	if info.ChannelSetting.Proxy != "" {
		httpClient, err = service.NewProxyHttpClient(info.ChannelSetting.Proxy)
		if err != nil {
			return nil, fmt.Errorf("new proxy http client failed: %w", err)
		}
	} else {
		httpClient = service.GetHttpClient()
	}

	awsSecret := strings.Split(info.ApiKey, "|")
	var apiOptions []func(*smithymiddleware.Stack) error
	isApiKey := info != nil && info.ChannelMeta != nil && info.ChannelOtherSettings.AwsKeyType == dto.AwsKeyTypeApiKey
	if isApiKey {
		headerOverride, resolveErr := channel.ResolveHeaderOverride(info, c)
		if resolveErr != nil {
			return nil, resolveErr
		}
		if len(headerOverride) > 0 {
			apiOptions = append(apiOptions, newAwsHeaderOverrideAPIOption(headerOverride))
		}
	}
	var client *bedrockruntime.Client
	switch len(awsSecret) {
	case 2:
		apiKey := awsSecret[0]
		region := awsSecret[1]
		client = bedrockruntime.New(bedrockruntime.Options{
			Region:                  region,
			BearerAuthTokenProvider: bearer.StaticTokenProvider{Token: bearer.Token{Value: apiKey}},
			HTTPClient:              httpClient,
			Retryer:                 newAwsRateLimitAwareRetryer(),
			APIOptions:              apiOptions,
		})
	case 3:
		ak := awsSecret[0]
		sk := awsSecret[1]
		region := awsSecret[2]
		client = bedrockruntime.New(bedrockruntime.Options{
			Region:      region,
			Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(ak, sk, "")),
			HTTPClient:  httpClient,
			Retryer:     newAwsRateLimitAwareRetryer(),
			APIOptions:  apiOptions,
		})
	default:
		return nil, errors.New("invalid aws secret key")
	}

	return client, nil
}

func newAwsHeaderOverrideAPIOption(headers map[string]string) func(*smithymiddleware.Stack) error {
	return func(stack *smithymiddleware.Stack) error {
		return stack.Finalize.Add(smithymiddleware.FinalizeMiddlewareFunc("new-api-header-override", func(ctx context.Context, in smithymiddleware.FinalizeInput, next smithymiddleware.FinalizeHandler) (smithymiddleware.FinalizeOutput, smithymiddleware.Metadata, error) {
			request, ok := in.Request.(*smithyhttp.Request)
			if !ok {
				return smithymiddleware.FinalizeOutput{}, smithymiddleware.Metadata{}, fmt.Errorf("AWS request has unexpected transport type %T", in.Request)
			}
			if err := applyAwsHeaderOverrides(request, headers); err != nil {
				return smithymiddleware.FinalizeOutput{}, smithymiddleware.Metadata{}, err
			}
			return next.HandleFinalize(ctx, in)
		}), smithymiddleware.After)
	}
}

func applyAwsHeaderOverrides(request *smithyhttp.Request, headers map[string]string) error {
	if request == nil || request.Request == nil {
		return errors.New("AWS request is nil while applying header overrides")
	}
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
		if strings.EqualFold(key, "Host") {
			request.Host = value
		}
	}
	return nil
}

func doAwsClientRequest(c *gin.Context, info *relaycommon.RelayInfo, a *Adaptor, requestBody io.Reader) (any, error) {
	if info == nil || info.ChannelMeta == nil {
		return nil, types.NewError(errors.New("AWS relay info or channel metadata is nil"), types.ErrorCodeBadRequestBody)
	}
	if a == nil {
		return nil, types.NewError(errors.New("AWS adaptor is nil"), types.ErrorCodeBadRequestBody)
	}
	awsCli, err := newAwsClient(c, info)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeChannelAwsClientError)
	}
	a.AwsClient = awsCli

	if a.ClientMode == ClientModeApiKey {
		return doAwsConverseRequest(c, info, a, requestBody)
	}

	// 获取对应的AWS模型ID
	awsModelId := getAwsModelID(info.UpstreamModelName)

	awsRegionPrefix := getAwsRegionPrefix(awsCli.Options().Region)
	canCrossRegion := awsModelCanCrossRegion(awsModelId, awsRegionPrefix)
	if canCrossRegion {
		awsModelId = awsModelCrossRegion(awsModelId, awsRegionPrefix)
	}

	// init empty request.header
	requestHeader := http.Header{}
	if err := a.SetupRequestHeader(c, &requestHeader, info); err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadRequestBody)
	}
	headerOverride, err := channel.ResolveHeaderOverride(info, c)
	if err != nil {
		return nil, err
	}
	for key, value := range headerOverride {
		requestHeader.Set(key, value)
	}

	if isNovaModel(awsModelId) {
		var novaReq *NovaRequest
		err = common.DecodeJson(requestBody, &novaReq)
		if err != nil {
			return nil, types.NewError(errors.Wrap(err, "decode nova request fail"), types.ErrorCodeBadRequestBody)
		}

		// 使用InvokeModel API，但使用Nova格式的请求体
		awsReq := &bedrockruntime.InvokeModelInput{
			ModelId:     aws.String(awsModelId),
			Accept:      aws.String("application/json"),
			ContentType: aws.String("application/json"),
		}

		reqBody, err := common.Marshal(novaReq)
		if err != nil {
			return nil, types.NewError(errors.Wrap(err, "marshal nova request"), types.ErrorCodeBadResponseBody)
		}
		awsReq.Body = reqBody
		a.AwsReq = awsReq
		return nil, nil
	} else {
		awsClaudeReq, err := formatRequest(requestBody, requestHeader)
		if err != nil {
			return nil, types.NewError(errors.Wrap(err, "format aws request fail"), types.ErrorCodeBadRequestBody)
		}

		if info.IsStream {
			awsReq := &bedrockruntime.InvokeModelWithResponseStreamInput{
				ModelId:     aws.String(awsModelId),
				Accept:      aws.String("application/json"),
				ContentType: aws.String("application/json"),
			}
			awsReq.Body, err = buildAwsRequestBody(c, info, awsClaudeReq)
			if err != nil {
				return nil, types.NewError(errors.Wrap(err, "marshal aws request fail"), types.ErrorCodeBadRequestBody)
			}
			a.AwsReq = awsReq
			return nil, nil
		} else {
			awsReq := &bedrockruntime.InvokeModelInput{
				ModelId:     aws.String(awsModelId),
				Accept:      aws.String("application/json"),
				ContentType: aws.String("application/json"),
			}
			awsReq.Body, err = buildAwsRequestBody(c, info, awsClaudeReq)
			if err != nil {
				return nil, types.NewError(errors.Wrap(err, "marshal aws request fail"), types.ErrorCodeBadRequestBody)
			}
			a.AwsReq = awsReq
			return nil, nil
		}
	}
}

// buildAwsRequestBody prepares the payload for AWS requests, applying passthrough rules when enabled.
func buildAwsRequestBody(c *gin.Context, info *relaycommon.RelayInfo, awsClaudeReq any) ([]byte, error) {
	if model_setting.GlobalSettingsSnapshot().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return nil, errors.Wrap(err, "get request body for pass-through fail")
		}
		body, err := storage.Bytes()
		if err != nil {
			return nil, errors.Wrap(err, "get request body bytes fail")
		}
		var data map[string]interface{}
		if err := common.Unmarshal(body, &data); err != nil {
			return nil, errors.Wrap(err, "pass-through unmarshal request body fail")
		}
		delete(data, "model")
		delete(data, "stream")
		return common.Marshal(data)
	}
	return common.Marshal(awsClaudeReq)
}

func getAwsRegionPrefix(awsRegionId string) string {
	parts := strings.Split(awsRegionId, "-")
	regionPrefix := ""
	if len(parts) > 0 {
		regionPrefix = parts[0]
	}
	return regionPrefix
}

func awsModelCanCrossRegion(awsModelId, awsRegionPrefix string) bool {
	regionSet, exists := awsModelCanCrossRegionMap[awsModelId]
	if !exists {
		return false
	}
	if regionSet[awsRegionPrefix] {
		return true
	}
	// AWS uses both "ap" (the region id prefix, for example
	// ap-southeast-1) and "apac" (the inference-profile prefix). Keep
	// the lookup tolerant of either spelling so Nova and Claude models use
	// the same cross-region routing rule.
	if awsRegionPrefix == "ap" {
		return regionSet["apac"]
	}
	if awsRegionPrefix == "apac" {
		return regionSet["ap"]
	}
	return false
}

func awsModelCrossRegion(awsModelId, awsRegionPrefix string) string {
	modelPrefix, find := awsRegionCrossModelPrefixMap[awsRegionPrefix]
	if !find && awsRegionPrefix == "apac" {
		modelPrefix, find = awsRegionCrossModelPrefixMap["ap"]
	}
	if !find {
		return awsModelId
	}
	return modelPrefix + "." + awsModelId
}

func getAwsModelID(requestModel string) string {
	if awsModelIDName, ok := awsModelIDMap[requestModel]; ok {
		return awsModelIDName
	}
	return requestModel
}

func awsHandler(c *gin.Context, info *relaycommon.RelayInfo, a *Adaptor) (*types.NewAPIError, *dto.Usage) {

	ctx, cancel := newAwsInvokeContext(requestContextOf(c))
	defer cancel()

	if !service.StartUpstreamRateLimitRetryFollowup(c) {
		return service.NewUpstreamRateLimitRetryExhaustedError(), nil
	}
	awsResp, err := a.AwsClient.InvokeModel(ctx, a.AwsReq.(*bedrockruntime.InvokeModelInput))
	if err != nil {
		statusCode := getAwsErrorStatusCode(err)
		return types.NewOpenAIError(errors.Wrap(err, "InvokeModel"), types.ErrorCodeAwsInvokeError, statusCode), nil
	}

	claudeInfo := &claude.ClaudeResponseInfo{
		ResponseId:   helper.GetResponseID(c),
		Created:      common.GetTimestamp(),
		Model:        info.UpstreamModelName,
		ResponseText: strings.Builder{},
		Usage:        &dto.Usage{},
	}

	// 复制上游 Content-Type 到客户端响应头
	if awsResp.ContentType != nil && *awsResp.ContentType != "" {
		c.Writer.Header().Set("Content-Type", *awsResp.ContentType)
	}

	handlerErr := claude.HandleClaudeResponseData(c, info, claudeInfo, nil, awsResp.Body)
	if handlerErr != nil {
		return handlerErr, nil
	}
	return nil, claudeInfo.Usage
}

func awsStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, a *Adaptor) (*types.NewAPIError, *dto.Usage) {
	ctx, cancel := newAwsInvokeContext(requestContextOf(c))
	defer cancel()

	if !service.StartUpstreamRateLimitRetryFollowup(c) {
		return service.NewUpstreamRateLimitRetryExhaustedError(), nil
	}
	awsResp, err := a.AwsClient.InvokeModelWithResponseStream(ctx, a.AwsReq.(*bedrockruntime.InvokeModelWithResponseStreamInput))
	if err != nil {
		statusCode := getAwsErrorStatusCode(err)
		return types.NewOpenAIError(errors.Wrap(err, "InvokeModelWithResponseStream"), types.ErrorCodeAwsInvokeError, statusCode), nil
	}
	stream := awsResp.GetStream()
	defer stream.Close()

	claudeInfo := &claude.ClaudeResponseInfo{
		ResponseId:   helper.GetResponseID(c),
		Created:      common.GetTimestamp(),
		Model:        info.UpstreamModelName,
		ResponseText: strings.Builder{},
		Usage:        &dto.Usage{},
	}

	// 与 ClaudeStreamHandler 一致：入口先认为「尚未提交内容」。客户端此时
	// 最多只收到 ping，上游在首个数据帧前断流仍可安全换渠道。首个
	// message_start / 内容帧到达后由 HandleStreamResponseData 细化。
	common.SetContextKey(c, constant.ContextKeyResponsesStreamPreludeRetrySafe, true)
	// ReceivedResponseCount 是 attempt 级计数（StreamScannerHandler 在每次
	// 扫描结束时按本轮计数赋值）。重试复用同一个 RelayInfo，这里必须先从零
	// 开始，否则上一轮的帧数会累积到本轮。
	info.ReceivedResponseCount = 0

	for event := range stream.Events() {
		switch v := event.(type) {
		case *bedrockruntimeTypes.ResponseStreamMemberChunk:
			info.SetFirstResponseTime()
			// 自管扫描器不经过 StreamScannerHandler，需自行累计，供
			// RetryableUpstreamStreamInterruptedError 判断下游是否已收到内容。
			info.ReceivedResponseCount++
			respErr := claude.HandleStreamResponseData(c, info, claudeInfo, string(v.Value.Bytes))
			if respErr != nil {
				return respErr, nil
			}
		case *bedrockruntimeTypes.UnknownUnionMember:
			fmt.Println("unknown tag:", v.Tag)
			return types.NewError(errors.New("unknown response type"), types.ErrorCodeInvalidRequest), nil
		default:
			fmt.Println("union is nil or unknown type")
			return types.NewError(errors.New("nil or unknown response type"), types.ErrorCodeInvalidRequest), nil
		}
	}

	if newAPIError := finishAwsStream(c, info, claudeInfo, stream.Err()); newAPIError != nil {
		return newAPIError, claudeInfo.Usage
	}
	claude.HandleStreamFinalResponse(c, info, claudeInfo)
	return nil, claudeInfo.Usage
}

// finishAwsStream 收尾 AKSK InvokeModelWithResponseStream 的自管扫描循环。
// 与 converse 的 finishConverseStream 同理：handler 本体需要真实的
// *bedrockruntime.Client，无法在单测里构造，终止判定提取出来直接驱动。
//
// 判据跟随 ClaudeStreamHandler：见过 message_stop 即正常结束；未见终止标记
// 则按上游中断处理，只有在客户端尚未收到内容帧（prelude 阶段）时才转成可
// 重试错误，内容帧已下发则退回部分成功，避免重放产生重复内容。
func finishAwsStream(c *gin.Context, info *relaycommon.RelayInfo, claudeInfo *claude.ClaudeResponseInfo, streamErr error) *types.NewAPIError {
	if info != nil && info.StreamStatus != nil && streamErr != nil && !claudeInfo.SawMessageStop {
		// 保留 SDK 事件流的真实错误，RecordStreamEnd 只在 EndReason 为空时
		// 写入通用错误，这里先写可以保住根因。
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonUpstreamInterrupted,
			errors.Wrap(streamErr, "AWS InvokeModelWithResponseStream"))
	}
	helper.RecordStreamEnd(c, info, claudeInfo.SawMessageStop)
	return relaycommon.RetryableUpstreamStreamInterruptedError(c, info)
}

// Nova模型处理函数
func handleNovaRequest(c *gin.Context, info *relaycommon.RelayInfo, a *Adaptor) (*types.NewAPIError, *dto.Usage) {

	ctx, cancel := newAwsInvokeContext(requestContextOf(c))
	defer cancel()

	if !service.StartUpstreamRateLimitRetryFollowup(c) {
		return service.NewUpstreamRateLimitRetryExhaustedError(), nil
	}
	awsResp, err := a.AwsClient.InvokeModel(ctx, a.AwsReq.(*bedrockruntime.InvokeModelInput))
	if err != nil {
		statusCode := getAwsErrorStatusCode(err)
		return types.NewOpenAIError(errors.Wrap(err, "InvokeModel"), types.ErrorCodeAwsInvokeError, statusCode), nil
	}

	// 解析Nova响应
	var novaResp struct {
		Output struct {
			Message struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		} `json:"output"`
		Usage struct {
			InputTokens  int `json:"inputTokens"`
			OutputTokens int `json:"outputTokens"`
			TotalTokens  int `json:"totalTokens"`
		} `json:"usage"`
	}

	if err := common.Unmarshal(awsResp.Body, &novaResp); err != nil {
		return types.NewError(errors.Wrap(err, "unmarshal nova response"), types.ErrorCodeBadResponseBody), nil
	}

	// 构造OpenAI格式响应
	if len(novaResp.Output.Message.Content) == 0 {
		return types.NewError(errors.New("Nova response contains no message content"), types.ErrorCodeBadResponseBody), nil
	}
	var responseText strings.Builder
	for _, content := range novaResp.Output.Message.Content {
		responseText.WriteString(content.Text)
	}
	response := dto.OpenAITextResponse{
		Id:      helper.GetResponseID(c),
		Object:  "chat.completion",
		Created: common.GetTimestamp(),
		Model:   info.UpstreamModelName,
		Choices: []dto.OpenAITextResponseChoice{{
			Index: 0,
			Message: dto.Message{
				Role:    "assistant",
				Content: responseText.String(),
			},
			FinishReason: "stop",
		}},
		Usage: dto.Usage{
			PromptTokens:     novaResp.Usage.InputTokens,
			CompletionTokens: novaResp.Usage.OutputTokens,
			TotalTokens:      novaResp.Usage.TotalTokens,
		},
	}

	c.JSON(http.StatusOK, response)
	return nil, &response.Usage
}
