package relay

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	openaichannel "github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func applySystemPromptIfNeeded(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) error {
	if info == nil || request == nil {
		return nil
	}
	if info.ChannelSetting.SystemPrompt == "" {
		return nil
	}

	systemRole := request.GetSystemRoleName()

	containSystemPrompt := false
	for _, message := range request.Messages {
		if message.Role == systemRole {
			containSystemPrompt = true
			break
		}
	}
	if !containSystemPrompt {
		systemMessage := dto.Message{
			Role:    systemRole,
			Content: info.ChannelSetting.SystemPrompt,
		}
		request.Messages = append([]dto.Message{systemMessage}, request.Messages...)
		return nil
	}

	if !info.ChannelSetting.SystemPromptOverride {
		return nil
	}

	common.SetContextKey(c, constant.ContextKeySystemPromptOverride, true)
	for i, message := range request.Messages {
		if message.Role != systemRole {
			continue
		}
		if message.IsStringContent() {
			request.Messages[i].SetStringContent(info.ChannelSetting.SystemPrompt + "\n" + message.StringContent())
			return nil
		}
		contents, err := message.ParseContentStrict()
		if err != nil {
			return fmt.Errorf("cannot apply system prompt to messages[%d]: %w", i, err)
		}
		contents = append([]dto.MediaContent{
			{
				Type: dto.ContentTypeText,
				Text: info.ChannelSetting.SystemPrompt,
			},
		}, contents...)
		request.Messages[i].Content = contents
		return nil
	}
	return nil
}

func chatCompletionsViaResponses(c *gin.Context, info *relaycommon.RelayInfo, adaptor channel.Adaptor, request *dto.GeneralOpenAIRequest) (*dto.Usage, *types.NewAPIError) {
	chatJSON, err := common.Marshal(request)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}

	chatJSON, err = relaycommon.RemoveDisabledFields(chatJSON, info.ChannelOtherSettings, false)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}

	if len(info.ParamOverride) > 0 {
		chatJSON, err = relaycommon.ApplyParamOverrideWithRelayInfo(chatJSON, info)
		if err != nil {
			return nil, newAPIErrorFromParamOverride(err)
		}
	}

	var overriddenChatReq dto.GeneralOpenAIRequest
	if err := common.Unmarshal(chatJSON, &overriddenChatReq); err != nil {
		return nil, types.NewError(err, types.ErrorCodeChannelParamOverrideInvalid, types.ErrOptionWithSkipRetry())
	}

	var toolNameRestorer *relaycommon.ResponsesToolNameRestorer
	responsesReq, err := service.ChatCompletionsRequestToResponsesRequestWithOptions(&overriddenChatReq, service.ChatCompletionsToResponsesOptions{
		PreserveReasoningContent: shouldPreservePlainTextReasoningForResponses(info, &overriddenChatReq),
		ToolNameRestorerOut:      &toolNameRestorer,
	})
	if err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	// 上游 Responses echo 的工具别名经此映射还原为客户端原始名（响应侧
	// OaiResponsesToChat* 读取）；无别名时为 nil，响应原样透传。
	info.ResponsesToolNameRestorer = toolNameRestorer
	info.AppendRequestConversion(types.RelayFormatOpenAIResponses)

	savedRelayMode := info.RelayMode
	savedRequestURLPath := info.RequestURLPath
	previousBridgeMarker, hadBridgeMarker := common.GetContextKey(c, constant.ContextKeyResponsesToolCapabilityObservationBridged)
	defer func() {
		info.RelayMode = savedRelayMode
		info.RequestURLPath = savedRequestURLPath
		if hadBridgeMarker {
			common.SetContextKey(c, constant.ContextKeyResponsesToolCapabilityObservationBridged, previousBridgeMarker)
		} else {
			common.SetContextKey(c, constant.ContextKeyResponsesToolCapabilityObservationBridged, false)
		}
	}()

	common.SetContextKey(c, constant.ContextKeyResponsesToolCapabilityObservationBridged, true)
	info.RelayMode = relayconstant.RelayModeResponses
	info.RequestURLPath = "/v1/responses"

	convertedRequest, err := adaptor.ConvertOpenAIResponsesRequest(c, info, *responsesReq)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)

	jsonData, err := common.Marshal(convertedRequest)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}

	jsonData, err = relaycommon.RemoveDisabledFields(jsonData, info.ChannelOtherSettings, false)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}

	var requestBody io.Reader = bytes.NewBuffer(jsonData)

	var httpResp *http.Response
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	if resp == nil {
		return nil, types.NewOpenAIError(nil, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	statusCodeMappingStr := c.GetString("status_code_mapping")

	httpResp = resp.(*http.Response)
	info.IsStream = helper.ShouldTreatAsEventStream(httpResp, info.IsStream)
	if httpResp.StatusCode != http.StatusOK {
		newApiErr := service.RelayErrorHandler(c.Request.Context(), httpResp, false)
		service.ResetStatusCode(newApiErr, statusCodeMappingStr)
		return nil, newApiErr
	}

	if info.IsStream {
		usage, newApiErr := openaichannel.OaiResponsesToChatStreamHandler(c, info, httpResp)
		if newApiErr != nil {
			service.ResetStatusCode(newApiErr, statusCodeMappingStr)
			return nil, newApiErr
		}
		return usage, nil
	}

	usage, newApiErr := openaichannel.OaiResponsesToChatHandler(c, info, httpResp)
	if newApiErr != nil {
		service.ResetStatusCode(newApiErr, statusCodeMappingStr)
		return nil, newApiErr
	}
	return usage, nil
}

// shouldPreservePlainTextReasoningForResponses enables DeepSeek's documented
// plain-text reasoning input item without sending that extension to native
// OpenAI Responses endpoints, which use opaque reasoning context instead.
func shouldPreservePlainTextReasoningForResponses(info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) bool {
	if request == nil {
		return false
	}

	channelType := 0
	profile := dto.ResponsesUpstreamProfile("")
	if info != nil && info.ChannelMeta != nil {
		channelType = info.ChannelType
		profile = info.ChannelOtherSettings.NormalizedResponsesUpstreamProfile()
	}
	if profile == dto.ResponsesUpstreamProfileOfficialOpenAI {
		return false
	}
	if channelType == constant.ChannelTypeDeepSeek {
		return true
	}

	models := []string{request.Model}
	if info != nil {
		models = append(models, info.UpstreamModelName, info.OriginModelName)
	}
	for _, model := range models {
		if service.IsDeepSeekModelName(model) {
			return true
		}
	}
	return false
}
