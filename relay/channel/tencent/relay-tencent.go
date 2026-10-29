package tencent

import (
	"bufio"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
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

	"github.com/gin-gonic/gin"
)

// https://cloud.tencent.com/document/product/1729/97732

func requestOpenAI2Tencent(request dto.GeneralOpenAIRequest) (*TencentChatRequest, error) {
	if err := channel.RejectUnsupportedTextRequestFields(request, "Tencent Hunyuan"); err != nil {
		return nil, err
	}
	if request.MaxTokens != nil || request.MaxCompletionTokens != nil || request.Stop != nil ||
		request.FrequencyPenalty != nil || request.PresencePenalty != nil || request.N != nil ||
		request.Seed != nil || request.StreamOptions != nil || len(request.ExtraBody) > 0 || len(request.WebSearch) > 0 ||
		len(request.EnableSearch) > 0 {
		return nil, fmt.Errorf("request contains options that are not supported by Tencent Hunyuan")
	}
	if len(request.Messages) == 0 {
		return nil, fmt.Errorf("at least one message is required by Tencent Hunyuan")
	}
	if len(request.Messages) > 40 {
		return nil, fmt.Errorf("Tencent Hunyuan supports at most 40 messages, got %d", len(request.Messages))
	}

	roles := map[string]string{
		"system":    "system",
		"developer": "system",
		"user":      "user",
		"assistant": "assistant",
	}
	messages := make([]*TencentMessage, 0, len(request.Messages))
	var systemText strings.Builder
	seenConversation := false
	nextRole := "user"
	seenUser := false
	for messageIndex, message := range request.Messages {
		role, text, err := channel.OpenAIMessageRoleText(message, "Tencent Hunyuan", roles)
		if err != nil {
			return nil, fmt.Errorf("messages[%d]: %w", messageIndex, err)
		}
		if role == "system" {
			if seenConversation {
				return nil, fmt.Errorf("messages[%d]: system/developer messages must be first for Tencent Hunyuan", messageIndex)
			}
			if systemText.Len() > 0 {
				systemText.WriteString("\n")
			}
			systemText.WriteString(text)
			continue
		}
		seenConversation = true
		if role != nextRole {
			return nil, fmt.Errorf("messages[%d]: expected %s message, got %s for Tencent Hunyuan", messageIndex, nextRole, role)
		}
		if text == "" {
			return nil, fmt.Errorf("messages[%d]: content must not be empty for Tencent Hunyuan", messageIndex)
		}
		messages = append(messages, &TencentMessage{Content: text, Role: role})
		if role == "user" {
			seenUser = true
			nextRole = "assistant"
		} else {
			nextRole = "user"
		}
	}
	if !seenUser {
		return nil, fmt.Errorf("at least one user message is required by Tencent Hunyuan")
	}
	if nextRole != "assistant" {
		return nil, fmt.Errorf("final message must have role user for Tencent Hunyuan")
	}
	if systemText.Len() > 0 {
		messages = append([]*TencentMessage{{Role: "system", Content: systemText.String()}}, messages...)
	}
	var req = TencentChatRequest{
		Stream:   request.Stream,
		Messages: messages,
		Model:    &request.Model,
	}
	if request.TopP != nil {
		req.TopP = request.TopP
	}
	req.Temperature = request.Temperature
	return &req, nil
}

func responseTencent2OpenAI(response *TencentChatResponse) *dto.OpenAITextResponse {
	fullTextResponse := dto.OpenAITextResponse{
		Id:      response.Id,
		Object:  "chat.completion",
		Created: common.GetTimestamp(),
		Usage: dto.Usage{
			PromptTokens:     response.Usage.PromptTokens,
			CompletionTokens: response.Usage.CompletionTokens,
			TotalTokens:      response.Usage.TotalTokens,
		},
	}
	if len(response.Choices) > 0 {
		choice := dto.OpenAITextResponseChoice{
			Index: 0,
			Message: dto.Message{
				Role:    "assistant",
				Content: response.Choices[0].Messages.Content,
			},
			FinishReason: response.Choices[0].FinishReason,
		}
		fullTextResponse.Choices = append(fullTextResponse.Choices, choice)
	}
	return &fullTextResponse
}

func streamResponseTencent2OpenAI(TencentResponse *TencentChatResponse) *dto.ChatCompletionsStreamResponse {
	response := dto.ChatCompletionsStreamResponse{
		Object:  "chat.completion.chunk",
		Created: common.GetTimestamp(),
		Model:   "tencent-hunyuan",
	}
	if len(TencentResponse.Choices) > 0 {
		var choice dto.ChatCompletionsStreamResponseChoice
		choice.Delta.SetContentString(TencentResponse.Choices[0].Delta.Content)
		if TencentResponse.Choices[0].FinishReason == "stop" {
			choice.FinishReason = &constant.FinishReasonStop
		}
		response.Choices = append(response.Choices, choice)
	}
	return &response
}

func tencentStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	var responseText string
	scanner := bufio.NewScanner(resp.Body)
	scanner.Split(bufio.ScanLines)
	// 与共享扫描器同一行上限：裸 NewScanner 的 token 上限是 64KiB，上游一帧
	// 超过它就会停在 ErrTooLong，尾包 FinishReason=stop 再也读不到。
	scanner.Buffer(make([]byte, helper.InitialScannerBufferSize), helper.GetScannerBufferSize())
	sawFinish := false

	helper.SetEventStreamHeaders(c)

	for scanner.Scan() {
		data := scanner.Text()
		if len(data) < 5 || !strings.HasPrefix(data, "data:") {
			continue
		}
		data = strings.TrimPrefix(data, "data:")

		var tencentResponse TencentChatResponse
		err := common.Unmarshal([]byte(data), &tencentResponse)
		if err != nil {
			common.SysLog("error unmarshalling stream response: " + err.Error())
			continue
		}

		// 尾包以 FinishReason=stop 标记流结束（见 dto.go 注释）。
		if len(tencentResponse.Choices) > 0 && tencentResponse.Choices[0].FinishReason == "stop" {
			sawFinish = true
		}

		response := streamResponseTencent2OpenAI(&tencentResponse)
		if len(response.Choices) != 0 {
			responseText += response.Choices[0].Delta.GetContentString()
		}

		err = helper.ObjectData(c, response)
		if err != nil {
			common.SysLog(err.Error())
		}
	}

	if err := scanner.Err(); err != nil {
		common.SysLog("error reading stream: " + err.Error())
	}

	helper.RecordStreamEnd(c, info, sawFinish)
	helper.Done(c)

	service.CloseResponseBodyGracefully(resp)

	return service.ResponseText2Usage(c, responseText, info.UpstreamModelName, info.GetEstimatePromptTokens()), nil
}

func tencentHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	var tencentSb TencentChatResponseSB
	defer service.CloseResponseBodyGracefully(resp)
	responseBody, err := common.ReadAllWithLimit(resp.Body, common.UpstreamJSONResponseLimit)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	err = common.Unmarshal(responseBody, &tencentSb)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if tencentSb.Response.Error.Code != 0 {
		return nil, types.WithOpenAIError(types.OpenAIError{
			Message: tencentSb.Response.Error.Message,
			Code:    tencentSb.Response.Error.Code,
		}, resp.StatusCode)
	}
	fullTextResponse := responseTencent2OpenAI(&tencentSb.Response)
	jsonResponse, err := common.Marshal(fullTextResponse)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	service.IOCopyBytesGracefully(c, resp, jsonResponse)
	return &fullTextResponse.Usage, nil
}

func parseTencentConfig(config string) (appId int64, secretId string, secretKey string, err error) {
	parts := strings.Split(config, "|")
	if len(parts) != 3 {
		err = errors.New("invalid tencent config")
		return
	}
	appId, err = strconv.ParseInt(parts[0], 10, 64)
	secretId = parts[1]
	secretKey = parts[2]
	return
}

func sha256hex(s string) string {
	b := sha256.Sum256([]byte(s))
	return hex.EncodeToString(b[:])
}

func hmacSha256(s, key string) string {
	hashed := hmac.New(sha256.New, []byte(key))
	hashed.Write([]byte(s))
	return string(hashed.Sum(nil))
}

func getTencentSign(req TencentChatRequest, adaptor *Adaptor, secId, secKey string) string {
	// build canonical request string
	host := "hunyuan.tencentcloudapi.com"
	httpRequestMethod := "POST"
	canonicalURI := "/"
	canonicalQueryString := ""
	canonicalHeaders := fmt.Sprintf("content-type:%s\nhost:%s\nx-tc-action:%s\n",
		"application/json", host, strings.ToLower(adaptor.Action))
	signedHeaders := "content-type;host;x-tc-action"
	payload, _ := common.Marshal(req)
	hashedRequestPayload := sha256hex(string(payload))
	canonicalRequest := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		httpRequestMethod,
		canonicalURI,
		canonicalQueryString,
		canonicalHeaders,
		signedHeaders,
		hashedRequestPayload)
	// build string to sign
	algorithm := "TC3-HMAC-SHA256"
	requestTimestamp := strconv.FormatInt(adaptor.Timestamp, 10)
	timestamp, _ := strconv.ParseInt(requestTimestamp, 10, 64)
	t := time.Unix(timestamp, 0).UTC()
	// must be the format 2006-01-02, ref to package time for more info
	date := t.Format("2006-01-02")
	credentialScope := fmt.Sprintf("%s/%s/tc3_request", date, "hunyuan")
	hashedCanonicalRequest := sha256hex(canonicalRequest)
	string2sign := fmt.Sprintf("%s\n%s\n%s\n%s",
		algorithm,
		requestTimestamp,
		credentialScope,
		hashedCanonicalRequest)

	// sign string
	secretDate := hmacSha256(date, "TC3"+secKey)
	secretService := hmacSha256("hunyuan", secretDate)
	secretKey := hmacSha256("tc3_request", secretService)
	signature := hex.EncodeToString([]byte(hmacSha256(string2sign, secretKey)))

	// build authorization
	authorization := fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		algorithm,
		secId,
		credentialScope,
		signedHeaders,
		signature)
	return authorization
}
