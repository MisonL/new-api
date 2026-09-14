package openai

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func newResponsesReadResponseBodyError(err error) *types.NewAPIError {
	if types.IsUpstreamTransportInterruptedError(err) {
		return types.NewOpenAIError(err, types.ErrorCodeUpstreamTransportInterrupted, http.StatusBadGateway)
	}
	return types.NewOpenAIError(
		err,
		types.ErrorCodeReadResponseBodyFailed,
		http.StatusInternalServerError,
		types.ErrOptionWithSkipRetry(),
	)
}

func responsesOpenAIErrorStatus(statusCode int, openAIError *types.OpenAIError) int {
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return statusCode
	}
	if openAIError == nil {
		return http.StatusBadGateway
	}
	errorType := strings.ToLower(strings.TrimSpace(openAIError.Type))
	errorCode := strings.ToLower(strings.TrimSpace(fmt.Sprint(openAIError.Code)))
	if strings.Contains(errorType, "rate_limit") || strings.Contains(errorCode, "rate_limit") {
		return http.StatusTooManyRequests
	}
	if types.IsOpenAIRequestParameterError(openAIError) {
		return http.StatusBadRequest
	}
	errorMessage := strings.ToLower(strings.TrimSpace(openAIError.Message))
	if isResponsesContextWindowError(openAIError) {
		return http.StatusBadRequest
	}
	switch {
	case strings.Contains(errorType, "authentication") || strings.Contains(errorCode, "invalid_api_key"):
		return http.StatusUnauthorized
	case strings.Contains(errorType, "permission") || strings.Contains(errorCode, "permission"):
		return http.StatusForbidden
	case strings.Contains(errorType, "invalid_request") ||
		strings.Contains(errorType, "validation") ||
		errorCode == "unknown_parameter" ||
		errorCode == "invalid_type" ||
		strings.Contains(errorMessage, "unknown parameter") ||
		strings.Contains(errorMessage, "invalid type for"):
		return http.StatusBadRequest
	default:
		return http.StatusBadGateway
	}
}

func responsesStreamTerminalOpenAIError(streamResponse *dto.ResponsesStreamResponse) *types.OpenAIError {
	if streamResponse == nil {
		return nil
	}
	if streamResponse.Error != nil && strings.TrimSpace(streamResponse.Error.Message) != "" {
		return streamResponse.Error
	}
	if streamResponse.Response == nil {
		return nil
	}
	openAIError := streamResponse.Response.GetOpenAIError()
	if openAIError == nil || strings.TrimSpace(openAIError.Message) == "" {
		return nil
	}
	return openAIError
}

func responsesSSETerminalError(c *gin.Context, responseBody []byte, statusCode int) *types.NewAPIError {
	trimmedResponseBody := bytes.TrimSpace(responseBody)
	if !bytes.HasPrefix(trimmedResponseBody, []byte("data:")) &&
		!bytes.HasPrefix(trimmedResponseBody, []byte("event:")) &&
		!bytes.HasPrefix(trimmedResponseBody, []byte(":")) {
		return nil
	}

	scanner := bufio.NewScanner(bytes.NewReader(responseBody))
	scanner.Buffer(make([]byte, 1024), len(responseBody)+1)
	var eventType string
	dataLines := make([]string, 0, 1)

	parseEvent := func() *types.NewAPIError {
		if len(dataLines) == 0 {
			return nil
		}
		data := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		if strings.TrimSpace(data) == "[DONE]" {
			return nil
		}
		var streamResponse dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			return nil
		}
		if strings.TrimSpace(streamResponse.Type) == "" {
			streamResponse.Type = eventType
		}
		switch streamResponse.Type {
		case "response.error", "error", "response.failed":
			if c != nil {
				common.SetContextKey(c, constant.ContextKeyResponsesErrorEvent, streamResponse.Type)
			}
			openAIError := responsesStreamTerminalOpenAIError(&streamResponse)
			if openAIError == nil {
				return types.NewOpenAIError(
					errors.New("upstream responses SSE terminal event has no valid error"),
					types.ErrorCodeBadResponse,
					http.StatusBadGateway,
				)
			}
			return types.WithOpenAIError(*openAIError, responsesOpenAIErrorStatus(statusCode, openAIError))
		default:
			return nil
		}
	}

	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if newAPIError := parseEvent(); newAPIError != nil {
				return newAPIError
			}
			eventType = ""
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	return parseEvent()
}
