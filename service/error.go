package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/types"
)

// ErrProtocolConversionUnsupported identifies a request/response conversion
// that cannot be represented faithfully by the selected protocol. Callers
// should surface this as a client error instead of retrying or forwarding a
// payload using a different schema.
var ErrProtocolConversionUnsupported = errors.New("protocol conversion unsupported")

func MidjourneyErrorWrapper(code int, desc string) *dto.MidjourneyResponse {
	return &dto.MidjourneyResponse{
		Code:        code,
		Description: desc,
	}
}

func MidjourneyErrorWithStatusCodeWrapper(code int, desc string, statusCode int) *dto.MidjourneyResponseWithStatusCode {
	return &dto.MidjourneyResponseWithStatusCode{
		StatusCode: statusCode,
		Response:   *MidjourneyErrorWrapper(code, desc),
	}
}

//// OpenAIErrorWrapper wraps an error into an OpenAIErrorWithStatusCode
//func OpenAIErrorWrapper(err error, code string, statusCode int) *dto.OpenAIErrorWithStatusCode {
//	text := err.Error()
//	lowerText := strings.ToLower(text)
//	if !strings.HasPrefix(lowerText, "get file base64 from url") && !strings.HasPrefix(lowerText, "mime type is not supported") {
//		if strings.Contains(lowerText, "post") || strings.Contains(lowerText, "dial") || strings.Contains(lowerText, "http") {
//			common.SysLog(fmt.Sprintf("error: %s", text))
//			text = "请求上游地址失败"
//		}
//	}
//	openAIError := dto.OpenAIError{
//		Message: text,
//		Type:    "new_api_error",
//		Code:    code,
//	}
//	return &dto.OpenAIErrorWithStatusCode{
//		Error:      openAIError,
//		StatusCode: statusCode,
//	}
//}
//
//func OpenAIErrorWrapperLocal(err error, code string, statusCode int) *dto.OpenAIErrorWithStatusCode {
//	openaiErr := OpenAIErrorWrapper(err, code, statusCode)
//	openaiErr.LocalError = true
//	return openaiErr
//}

// anthropicErrorTypeName maps an HTTP status onto the Anthropic error type
// vocabulary that Claude clients switch on.
func anthropicErrorTypeName(statusCode int) string {
	switch {
	case statusCode == http.StatusTooManyRequests:
		return "rate_limit_error"
	case statusCode == http.StatusRequestTimeout:
		return "timeout_error"
	case statusCode == http.StatusNotFound:
		return "not_found_error"
	case statusCode == http.StatusRequestEntityTooLarge:
		return "request_too_large"
	case statusCode >= 400 && statusCode < 500:
		return "invalid_request_error"
	case statusCode >= 500:
		return "api_error"
	}
	return "api_error"
}

func ClaudeErrorWrapper(err error, code string, statusCode int) *dto.ClaudeErrorWithStatusCode {
	if err == nil {
		err = errors.New("upstream request failed")
	}
	text := common.MaskSensitiveInfo(err.Error())
	lowerText := strings.ToLower(text)
	if !strings.HasPrefix(lowerText, "get file base64 from url") {
		if strings.Contains(lowerText, "post") || strings.Contains(lowerText, "dial") || strings.Contains(lowerText, "http") {
			common.SysLog(fmt.Sprintf("error: %s", common.LocalLogPreview(text)))
			text = "请求上游地址失败"
		}
	}
	claudeError := types.ClaudeError{
		Message: text,
		// Claude 客户端按 Anthropic 错误词汇分发：5xx 是 api_error，
		// 429 是 rate_limit_error 等。内部标记 new_api_error 不属于任何
		// Anthropic 客户端的 switch。
		Type: anthropicErrorTypeName(statusCode),
	}
	return &dto.ClaudeErrorWithStatusCode{
		Error:      claudeError,
		StatusCode: statusCode,
	}
}

func ClaudeErrorWrapperLocal(err error, code string, statusCode int) *dto.ClaudeErrorWithStatusCode {
	claudeErr := ClaudeErrorWrapper(err, code, statusCode)
	claudeErr.LocalError = true
	return claudeErr
}

func RelayErrorHandler(ctx context.Context, resp *http.Response, showBodyWhenFail bool) (newApiErr *types.NewAPIError) {
	if resp == nil {
		return types.NewOpenAIError(
			errors.New("upstream response is nil"),
			types.ErrorCodeEmptyResponse,
			http.StatusBadGateway,
		)
	}
	retryAfterSeconds := parseRetryAfterSeconds(resp)
	defer func() {
		if newApiErr != nil {
			newApiErr.RetryAfterSeconds = retryAfterSeconds
		}
	}()
	statusErrorCode := types.ErrorCodeBadResponseStatusCode
	if code, ok := types.UpstreamHTTPStatusErrorCode(resp.StatusCode); ok {
		statusErrorCode = code
	}
	statusMessage := types.UpstreamHTTPStatusErrorMessage(statusErrorCode, resp.StatusCode)
	newApiErr = types.InitOpenAIError(statusErrorCode, resp.StatusCode)

	if resp.Body == nil {
		newApiErr = types.NewOpenAIError(
			fmt.Errorf("upstream response body is nil (status %d)", resp.StatusCode),
			types.ErrorCodeEmptyResponse,
			resp.StatusCode,
		)
		return
	}
	defer CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	var errResponse dto.GeneralErrorResponse
	responseBodyText := string(responseBody)
	responseBodyPreview := common.LocalLogPreview(common.MaskSensitiveInfo(responseBodyText))
	buildErrWithBody := func(message string) error {
		message = common.LocalLogPreview(common.MaskSensitiveInfo(message))
		if message == "" {
			return fmt.Errorf("%s, body: %s", statusMessage, responseBodyPreview)
		}
		return fmt.Errorf("%s, message: %s, body: %s", statusMessage, message, responseBodyPreview)
	}

	err = common.Unmarshal(responseBody, &errResponse)
	if err != nil {
		if showBodyWhenFail {
			newApiErr.Err = buildErrWithBody("")
		} else {
			logger.LogError(ctx, fmt.Sprintf("%s, body: %s", statusMessage, responseBodyPreview))
			newApiErr.Err = errors.New(statusMessage)
		}
		return
	}

	if common.GetJsonType(errResponse.Error) == "object" {
		// General format error (OpenAI, Anthropic, Gemini, etc.)
		oaiError := errResponse.TryToOpenAIError()
		if oaiError != nil {
			// 上游可能带来自家词汇的 type（如 internal_server_error、Python SDK
			// 类名）。这里统一映射到 OpenAI 标准词汇，消息与 code 不动。
			standardized := types.StandardizeErrorType(*oaiError, resp.StatusCode)
			oaiError = &standardized
			options := make([]types.NewAPIErrorOptions, 0, 3)
			if statusErrorCode != types.ErrorCodeBadResponseStatusCode {
				options = append(options, types.ErrOptionWithErrorCode(statusErrorCode))
			}
			if resp.StatusCode >= http.StatusInternalServerError && types.IsOpenAIRequestParameterError(oaiError) {
				options = append(options, types.ErrOptionWithStatusCode(http.StatusBadRequest), types.ErrOptionWithSkipRetry())
			}
			newApiErr = types.WithOpenAIError(*oaiError, resp.StatusCode, options...)
			if showBodyWhenFail {
				newApiErr.Err = buildErrWithBody(newApiErr.Error())
			}
			return
		}
	}
	newApiErr = types.NewOpenAIError(errors.New(errResponse.ToMessage()), statusErrorCode, resp.StatusCode)
	if showBodyWhenFail {
		newApiErr.Err = buildErrWithBody(newApiErr.Error())
	}
	return
}

func parseRetryAfterSeconds(resp *http.Response) int {
	if resp == nil || resp.Header == nil {
		return 0
	}
	value := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return seconds
	}
	if retryAt, err := http.ParseTime(value); err == nil {
		remaining := time.Until(retryAt)
		if remaining > 0 {
			seconds := int((remaining + time.Second - 1) / time.Second)
			if seconds > 0 {
				return seconds
			}
		}
	}
	return 0
}

func ResetStatusCode(newApiErr *types.NewAPIError, statusCodeMappingStr string) {
	if newApiErr == nil {
		return
	}
	if statusCodeMappingStr == "" || statusCodeMappingStr == "{}" {
		return
	}
	statusCodeMapping := make(map[string]any)
	err := common.Unmarshal([]byte(statusCodeMappingStr), &statusCodeMapping)
	if err != nil {
		return
	}
	if newApiErr.StatusCode == http.StatusOK {
		return
	}
	codeStr := strconv.Itoa(newApiErr.StatusCode)
	if value, ok := statusCodeMapping[codeStr]; ok {
		intCode, ok := parseStatusCodeMappingValue(value)
		if !ok {
			return
		}
		if newApiErr.OriginalStatusCode == 0 {
			newApiErr.OriginalStatusCode = newApiErr.StatusCode
		}
		newApiErr.StatusCode = intCode
	}
}

func parseStatusCodeMappingValue(value any) (int, bool) {
	switch v := value.(type) {
	case string:
		if v == "" {
			return 0, false
		}
		statusCode, err := strconv.Atoi(v)
		if err != nil {
			return 0, false
		}
		return statusCode, true
	case float64:
		if v != math.Trunc(v) {
			return 0, false
		}
		return int(v), true
	case int:
		return v, true
	case json.Number:
		statusCode, err := strconv.Atoi(v.String())
		if err != nil {
			return 0, false
		}
		return statusCode, true
	default:
		return 0, false
	}
}

func TaskErrorWrapperLocal(err error, code string, statusCode int) *dto.TaskError {
	openaiErr := TaskErrorWrapper(err, code, statusCode)
	openaiErr.LocalError = true
	return openaiErr
}

func TaskErrorWrapper(err error, code string, statusCode int) *dto.TaskError {
	if err == nil {
		err = errors.New("task request failed")
	}
	text := common.MaskSensitiveInfo(err.Error())
	lowerText := strings.ToLower(text)
	if strings.Contains(lowerText, "post") || strings.Contains(lowerText, "dial") || strings.Contains(lowerText, "http") {
		common.SysLog(fmt.Sprintf("error: %s", common.LocalLogPreview(text)))
	}
	//避免暴露内部错误
	taskError := &dto.TaskError{
		Code:       code,
		Message:    text,
		StatusCode: statusCode,
		Error:      err,
	}

	return taskError
}

// TaskErrorFromAPIError 将 PreConsumeBilling 返回的 NewAPIError 转换为 TaskError。
func TaskErrorFromAPIError(apiErr *types.NewAPIError) *dto.TaskError {
	if apiErr == nil {
		return nil
	}
	return &dto.TaskError{
		Code:       string(apiErr.GetErrorCode()),
		Message:    common.MaskSensitiveInfo(apiErr.Error()),
		StatusCode: apiErr.StatusCode,
		Error:      apiErr.Err,
	}
}
