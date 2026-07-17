package relay

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func shouldRetryResponsesWithoutStreamOptionsIncludeUsage(info *relaycommon.RelayInfo, err *types.NewAPIError) bool {
	if info == nil || err == nil || !relaycommon.IsOpenAICompatibleResponses(info) || err.StatusCode != http.StatusBadRequest {
		return false
	}
	openAIError := err.ToOpenAIError()
	if !isResponsesStreamOptionsIncludeUsageParameter(openAIError.Param, openAIError.Message) {
		return false
	}
	errorCode := strings.ToLower(fmt.Sprint(openAIError.Code))
	if errorCode == "unknown_parameter" || errorCode == "unsupported_parameter" {
		return true
	}
	errorMessage := strings.ToLower(openAIError.Message)
	return strings.Contains(errorMessage, "unknown parameter") || strings.Contains(errorMessage, "unsupported parameter")
}

func isResponsesStreamOptionsIncludeUsageParameter(parameter string, message string) bool {
	const target = "stream_options.include_usage"
	parameter = strings.ToLower(strings.TrimSpace(parameter))
	if parameter != "" {
		return parameter == target
	}
	message = strings.ToLower(message)
	return strings.Contains(message, "'"+target+"'") ||
		strings.Contains(message, "["+target+"]") ||
		responsesErrorMessageHasExactParameter(message, target)
}

func responsesErrorMessageHasExactParameter(message string, target string) bool {
	for _, prefix := range []string{"unknown parameter:", "unsupported parameter:"} {
		remaining := message
		for {
			index := strings.Index(remaining, prefix)
			if index < 0 {
				break
			}
			remaining = remaining[index+len(prefix):]
			fields := strings.Fields(remaining)
			if len(fields) > 0 {
				parameter := strings.Trim(fields[0], "\\\"'`[](){}.,:;!?")
				if parameter == target {
					return true
				}
			}
		}
	}
	return false
}

func buildResponsesWithoutStreamOptionsIncludeUsageRetry(c *gin.Context, info *relaycommon.RelayInfo, adaptor relaychannel.Adaptor, convertedRequest dto.OpenAIResponsesRequest, newAPIError *types.NewAPIError) (io.Reader, dto.OpenAIResponsesRequest, bool, *types.NewAPIError) {
	strippedRequest, stripped := stripResponsesStreamOptionsIncludeUsage(convertedRequest)
	if !stripped {
		return nil, convertedRequest, false, nil
	}

	common.SetContextKey(c, appconstant.ContextKeyResponsesStreamOptionsIncludeUsageRetry, true)
	common.SetContextKey(c, appconstant.ContextKeyResponsesStreamOptionsIncludeUsageRetryChannelID, info.ChannelId)
	common.SysLog(fmt.Sprintf(
		"responses stream options include_usage retry: channel_id=%d model=%s original_error=%s",
		info.ChannelId,
		info.OriginModelName,
		newAPIError.MaskSensitiveError(),
	))
	retryBody, _, retryBuildErr := buildOpenAIResponsesRequestBody(c, info, adaptor, &strippedRequest, false, buildResponsesRequestBodyOptions{
		ForceConvertedBody: true,
	})
	if retryBuildErr != nil {
		return nil, convertedRequest, false, retryBuildErr
	}
	return retryBody, strippedRequest, true, nil
}

func stripResponsesStreamOptionsIncludeUsage(request dto.OpenAIResponsesRequest) (dto.OpenAIResponsesRequest, bool) {
	if request.StreamOptions == nil || !request.StreamOptions.HasIncludeUsageField() {
		return request, false
	}

	strippedRequest := request
	streamOptions := *request.StreamOptions
	streamOptions.RemoveIncludeUsage()
	if !streamOptions.IncludeObfuscation && len(streamOptions.Extra) == 0 {
		strippedRequest.StreamOptions = nil
	} else {
		strippedRequest.StreamOptions = &streamOptions
	}
	return strippedRequest, true
}

func stripResponsesStreamOptionsIncludeUsageFromJSON(jsonData []byte) ([]byte, bool, error) {
	var request map[string]common.RawMessage
	if err := common.Unmarshal(jsonData, &request); err != nil {
		return jsonData, false, err
	}
	streamOptionsData, ok := request["stream_options"]
	if !ok {
		return jsonData, false, nil
	}
	var streamOptions map[string]common.RawMessage
	if err := common.Unmarshal(streamOptionsData, &streamOptions); err != nil {
		return jsonData, false, err
	}
	if _, ok := streamOptions["include_usage"]; !ok {
		return jsonData, false, nil
	}
	delete(streamOptions, "include_usage")
	if len(streamOptions) == 0 {
		delete(request, "stream_options")
	} else {
		normalizedStreamOptions, err := common.Marshal(streamOptions)
		if err != nil {
			return jsonData, false, err
		}
		request["stream_options"] = normalizedStreamOptions
	}
	strippedData, err := common.Marshal(request)
	if err != nil {
		return jsonData, false, err
	}
	return strippedData, true, nil
}

func markResponsesStreamOptionsIncludeUsageCapabilityStripped(c *gin.Context, info *relaycommon.RelayInfo) {
	if c == nil || info == nil {
		return
	}
	if common.GetContextKeyBool(c, appconstant.ContextKeyResponsesStreamOptionsCapabilityStripped) &&
		common.GetContextKeyInt(c, appconstant.ContextKeyResponsesStreamOptionsCapabilityStrippedChannelID) == info.ChannelId {
		return
	}
	common.SetContextKey(c, appconstant.ContextKeyResponsesStreamOptionsCapabilityStripped, true)
	common.SetContextKey(c, appconstant.ContextKeyResponsesStreamOptionsCapabilityStrippedChannelID, info.ChannelId)
	common.SysLog(fmt.Sprintf(
		"responses stream options include_usage stripped by channel capability: channel_id=%d model=%s",
		info.ChannelId,
		info.OriginModelName,
	))
}
