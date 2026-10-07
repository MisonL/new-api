package xunfei

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// https://console.xfyun.cn/services/cbm
// https://www.xfyun.cn/doc/spark/Web.html

func requestOpenAI2Xunfei(request dto.GeneralOpenAIRequest, xunfeiAppId string, domain string) (*XunfeiChatRequest, error) {
	if err := channel.RejectUnsupportedTextRequestFields(request, "讯飞 Spark"); err != nil {
		return nil, err
	}
	if request.TopP != nil || request.FrequencyPenalty != nil || request.PresencePenalty != nil || request.Stop != nil ||
		request.N != nil || request.StreamOptions != nil || len(request.ExtraBody) > 0 || len(request.WebSearch) > 0 ||
		len(request.EnableSearch) > 0 {
		return nil, fmt.Errorf("request contains sampling or stop options that are not supported by 讯飞 Spark")
	}
	messages := make([]XunfeiMessage, 0, len(request.Messages))
	shouldCovertSystemMessage := !strings.HasSuffix(request.Model, "3.5")
	roles := map[string]string{
		"system":    "system",
		"developer": "system",
		"user":      "user",
		"assistant": "assistant",
	}
	seenConversation := false
	for messageIndex, message := range request.Messages {
		role, text, err := channel.OpenAIMessageRoleText(message, "讯飞 Spark", roles)
		if err != nil {
			return nil, fmt.Errorf("messages[%d]: %w", messageIndex, err)
		}
		if role == "system" && seenConversation {
			return nil, fmt.Errorf("messages[%d]: system/developer messages must be first for 讯飞 Spark", messageIndex)
		}
		if role == "system" && shouldCovertSystemMessage {
			messages = append(messages, XunfeiMessage{
				Role:    "user",
				Content: text,
			})
			messages = append(messages, XunfeiMessage{
				Role:    "assistant",
				Content: "Okay",
			})
		} else {
			messages = append(messages, XunfeiMessage{
				Role:    role,
				Content: text,
			})
		}
		if role != "system" {
			seenConversation = true
		}
	}
	xunfeiRequest := XunfeiChatRequest{}
	xunfeiRequest.Header.AppId = xunfeiAppId
	xunfeiRequest.Parameter.Chat.Domain = domain
	xunfeiRequest.Parameter.Chat.Temperature = request.Temperature
	xunfeiRequest.Parameter.Chat.TopK = lo.FromPtrOr(request.TopK, 0)
	xunfeiRequest.Parameter.Chat.MaxTokens = request.GetMaxTokens()
	xunfeiRequest.Payload.Message.Text = messages
	return &xunfeiRequest, nil
}

func responseXunfei2OpenAI(response *XunfeiChatResponse) *dto.OpenAITextResponse {
	if len(response.Payload.Choices.Text) == 0 {
		response.Payload.Choices.Text = []XunfeiChatResponseTextItem{
			{
				Content: "",
			},
		}
	}
	choice := dto.OpenAITextResponseChoice{
		Index: 0,
		Message: dto.Message{
			Role:    "assistant",
			Content: response.Payload.Choices.Text[0].Content,
		},
		FinishReason: constant.FinishReasonStop,
	}
	fullTextResponse := dto.OpenAITextResponse{
		Object:  "chat.completion",
		Created: common.GetTimestamp(),
		Choices: []dto.OpenAITextResponseChoice{choice},
		Usage:   response.Payload.Usage.Text,
	}
	return &fullTextResponse
}

func streamResponseXunfei2OpenAI(xunfeiResponse *XunfeiChatResponse) *dto.ChatCompletionsStreamResponse {
	if len(xunfeiResponse.Payload.Choices.Text) == 0 {
		xunfeiResponse.Payload.Choices.Text = []XunfeiChatResponseTextItem{
			{
				Content: "",
			},
		}
	}
	var choice dto.ChatCompletionsStreamResponseChoice
	choice.Delta.SetContentString(xunfeiResponse.Payload.Choices.Text[0].Content)
	if xunfeiResponse.Payload.Choices.Status == 2 {
		choice.FinishReason = &constant.FinishReasonStop
	}
	response := dto.ChatCompletionsStreamResponse{
		Object:  "chat.completion.chunk",
		Created: common.GetTimestamp(),
		Model:   "SparkDesk",
		Choices: []dto.ChatCompletionsStreamResponseChoice{choice},
	}
	return &response
}

func buildXunfeiAuthUrl(hostUrl string, apiKey, apiSecret string) string {
	HmacWithShaToBase64 := func(algorithm, data, key string) string {
		mac := hmac.New(sha256.New, []byte(key))
		mac.Write([]byte(data))
		encodeData := mac.Sum(nil)
		return base64.StdEncoding.EncodeToString(encodeData)
	}
	ul, err := url.Parse(hostUrl)
	if err != nil {
		fmt.Println(err)
	}
	date := time.Now().UTC().Format(time.RFC1123)
	signString := []string{"host: " + ul.Host, "date: " + date, "GET " + ul.Path + " HTTP/1.1"}
	sign := strings.Join(signString, "\n")
	sha := HmacWithShaToBase64("hmac-sha256", sign, apiSecret)
	authUrl := fmt.Sprintf("hmac username=\"%s\", algorithm=\"%s\", headers=\"%s\", signature=\"%s\"", apiKey,
		"hmac-sha256", "host date request-line", sha)
	authorization := base64.StdEncoding.EncodeToString([]byte(authUrl))
	v := url.Values{}
	v.Add("host", ul.Host)
	v.Add("date", date)
	v.Add("authorization", authorization)
	callUrl := hostUrl + "?" + v.Encode()
	return callUrl
}

// xunfeiStreamHandler 的 domain/authUrl 由调用方预先构建：handler 需要 info 来把
// 终止原因写入 StreamStatus，而测试需要用本地 WebSocket 上游替换
// wss://spark-api.xf-yun.com，两者都要求这两个参数外显而不是在 handler 内重建。
func xunfeiStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, textRequest dto.GeneralOpenAIRequest, appId string, domain string, authUrl string) (*dto.Usage, *types.NewAPIError) {
	dataChan, stopChan, requestErr := xunfeiMakeRequest(c, textRequest, domain, authUrl, appId)
	if requestErr != nil {
		return nil, requestErr
	}
	helper.SetEventStreamHeaders(c)
	var usage dto.Usage
	c.Stream(func(w io.Writer) bool {
		select {
		case xunfeiResponse := <-dataChan:
			usage.PromptTokens += xunfeiResponse.Payload.Usage.Text.PromptTokens
			usage.CompletionTokens += xunfeiResponse.Payload.Usage.Text.CompletionTokens
			usage.TotalTokens += xunfeiResponse.Payload.Usage.Text.TotalTokens
			response := streamResponseXunfei2OpenAI(&xunfeiResponse)
			jsonResponse, err := json.Marshal(response)
			if err != nil {
				common.SysLog("error marshalling stream response: " + err.Error())
				return true
			}
			c.Render(-1, &common.CustomEvent{Data: "data: " + string(jsonResponse)})
			return true
		case res := <-stopChan:
			helper.RecordStreamEnd(c, info, res.endedNormally)
			c.Render(-1, &common.CustomEvent{Data: "data: [DONE]"})
			return false
		case <-relaycommon.GinRequestContext(c).Done():
			return false
		}
	})
	return &usage, nil
}

func xunfeiHandler(c *gin.Context, textRequest dto.GeneralOpenAIRequest, appId string, apiSecret string, apiKey string) (*dto.Usage, *types.NewAPIError) {
	domain, authUrl := getXunfeiAuthUrl(c, apiKey, apiSecret, textRequest.Model)
	dataChan, stopChan, requestErr := xunfeiMakeRequest(c, textRequest, domain, authUrl, appId)
	if requestErr != nil {
		return nil, requestErr
	}
	var usage dto.Usage
	var content string
	var xunfeiResponse XunfeiChatResponse
	stop := false
	for !stop {
		select {
		case xunfeiResponse = <-dataChan:
			if len(xunfeiResponse.Payload.Choices.Text) == 0 {
				continue
			}
			content += xunfeiResponse.Payload.Choices.Text[0].Content
			usage.PromptTokens += xunfeiResponse.Payload.Usage.Text.PromptTokens
			usage.CompletionTokens += xunfeiResponse.Payload.Usage.Text.CompletionTokens
			usage.TotalTokens += xunfeiResponse.Payload.Usage.Text.TotalTokens
		case <-stopChan:
			stop = true
		case <-relaycommon.GinRequestContext(c).Done():
			stop = true
		}
	}
	if len(xunfeiResponse.Payload.Choices.Text) == 0 {
		xunfeiResponse.Payload.Choices.Text = []XunfeiChatResponseTextItem{
			{
				Content: "",
			},
		}
	}
	xunfeiResponse.Payload.Choices.Text[0].Content = content

	response := responseXunfei2OpenAI(&xunfeiResponse)
	jsonResponse, err := json.Marshal(response)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	_, _ = c.Writer.Write(jsonResponse)
	return &usage, nil
}

// xunfeiStreamResult 是 reader goroutine 的一次性收尾信号：endedNormally 表示
// 收到 payload.status==2 的终止帧后才关闭连接，直接断开（读错误、无终止帧）为 false。
// 消费者收到该信号时才写 StreamStatus；发送发生在写入之后，构成 happens-before。
type xunfeiStreamResult struct {
	endedNormally bool
}

func xunfeiMakeRequest(c *gin.Context, textRequest dto.GeneralOpenAIRequest, domain, authUrl, appId string) (chan XunfeiChatResponse, chan xunfeiStreamResult, *types.NewAPIError) {
	data, conversionErr := requestOpenAI2Xunfei(textRequest, appId, domain)
	if conversionErr != nil {
		return nil, nil, types.NewError(conversionErr, types.ErrorCodeInvalidRequest)
	}

	d := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
	}
	if !service.StartUpstreamRateLimitRetryFollowup(c) {
		return nil, nil, service.NewUpstreamRateLimitRetryExhaustedError()
	}
	header := http.Header{}
	service.ApplyDefaultUserAgentHeader(header)
	requestContext := relaycommon.GinRequestContext(c)
	conn, resp, err := d.DialContext(requestContext, authUrl, header)
	if err != nil {
		if resp != nil {
			if resp.Body != nil {
				_ = resp.Body.Close()
			}
			return nil, nil, types.NewOpenAIError(
				fmt.Errorf("xunfei websocket handshake failed: %w, status: %d", err, resp.StatusCode),
				types.ErrorCodeDoRequestFailed,
				resp.StatusCode,
			)
		}
		return nil, nil, types.NewError(err, types.ErrorCodeDoRequestFailed)
	}
	if resp == nil {
		return nil, nil, types.NewError(fmt.Errorf("xunfei websocket handshake returned no response"), types.ErrorCodeDoRequestFailed)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
		return nil, nil, types.NewOpenAIError(
			fmt.Errorf("xunfei websocket handshake returned status: %d", resp.StatusCode),
			types.ErrorCodeDoRequestFailed,
			resp.StatusCode,
		)
	}

	err = conn.WriteJSON(data)
	if err != nil {
		_ = conn.Close()
		return nil, nil, types.NewError(err, types.ErrorCodeDoRequestFailed)
	}

	dataChan := make(chan XunfeiChatResponse)
	stopChan := make(chan xunfeiStreamResult, 1)
	stopOnCancel := make(chan struct{})
	go func() {
		select {
		case <-requestContext.Done():
			_ = conn.Close()
		case <-stopOnCancel:
		}
	}()
	go func() {
		defer close(stopOnCancel)
		defer func() {
			conn.Close()
		}()
		endedNormally := false
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				if requestContext.Err() == nil {
					common.SysLog("error reading stream response: " + err.Error())
				}
				break
			}
			var response XunfeiChatResponse
			err = json.Unmarshal(msg, &response)
			if err != nil {
				common.SysLog("error unmarshalling stream response: " + err.Error())
				break
			}
			select {
			case dataChan <- response:
			case <-requestContext.Done():
				return
			}
			if response.Payload.Choices.Status == 2 {
				// 讯飞协议的终止帧：payload.status==2 是「正常收尾」的唯一信号，
				// 与读错误区分开，否则健康流会被记成上游中断。
				endedNormally = true
				if err != nil {
					common.SysLog("error closing websocket connection: " + err.Error())
				}
				break
			}
		}
		select {
		case stopChan <- xunfeiStreamResult{endedNormally: endedNormally}:
		default:
		}
	}()

	return dataChan, stopChan, nil
}

func apiVersion2domain(apiVersion string) string {
	switch apiVersion {
	case "v1.1":
		return "lite"
	case "v2.1":
		return "generalv2"
	case "v3.1":
		return "generalv3"
	case "v3.5":
		return "generalv3.5"
	case "v4.0":
		return "4.0Ultra"
	}
	return "general" + apiVersion
}

func getXunfeiAuthUrl(c *gin.Context, apiKey string, apiSecret string, modelName string) (string, string) {
	apiVersion := getAPIVersion(c, modelName)
	domain := apiVersion2domain(apiVersion)
	authUrl := buildXunfeiAuthUrl(fmt.Sprintf("wss://spark-api.xf-yun.com/%s/chat", apiVersion), apiKey, apiSecret)
	return domain, authUrl
}

func getAPIVersion(c *gin.Context, modelName string) string {
	query := c.Request.URL.Query()
	apiVersion := query.Get("api-version")
	if apiVersion != "" {
		return apiVersion
	}
	parts := strings.Split(modelName, "-")
	if len(parts) == 2 {
		apiVersion = parts[1]
		return apiVersion

	}
	apiVersion = c.GetString("api_version")
	if apiVersion != "" {
		return apiVersion
	}
	apiVersion = "v1.1"
	common.SysLog("api_version not found, using default: " + apiVersion)
	return apiVersion
}
