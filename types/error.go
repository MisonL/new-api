package types

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

type OpenAIError struct {
	Message  string          `json:"message"`
	Type     string          `json:"type"`
	Param    string          `json:"param"`
	Code     any             `json:"code"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

func IsOpenAIRequestParameterError(openAIError *OpenAIError) bool {
	if openAIError == nil {
		return false
	}
	errorCode := strings.ToLower(strings.TrimSpace(fmt.Sprint(openAIError.Code)))
	switch errorCode {
	case "missing_required_parameter", "unknown_parameter", "unsupported_parameter", "invalid_type":
		return true
	}

	message := strings.ToLower(strings.Join([]string{
		openAIError.Message,
		openAIError.Param,
		errorCode,
	}, " "))
	for _, indicator := range []string{
		"missing required parameter",
		"missing_required_parameter",
		"unknown parameter",
		"unknown_parameter",
		"unsupported parameter",
		"unsupported_parameter",
		"invalid type for",
		"[invalid_type]",
	} {
		if strings.Contains(message, indicator) {
			return true
		}
	}
	return false
}

type ClaudeError struct {
	Type    string `json:"type,omitempty"`
	Message string `json:"message,omitempty"`
}

type ErrorType string

const (
	ErrorTypeNewAPIError     ErrorType = "new_api_error"
	ErrorTypeOpenAIError     ErrorType = "openai_error"
	ErrorTypeClaudeError     ErrorType = "claude_error"
	ErrorTypeMidjourneyError ErrorType = "midjourney_error"
	ErrorTypeGeminiError     ErrorType = "gemini_error"
	ErrorTypeRerankError     ErrorType = "rerank_error"
	ErrorTypeUpstreamError   ErrorType = "upstream_error"
)

type ErrorCode string

const (
	ErrorCodeInvalidRequest         ErrorCode = "invalid_request"
	ErrorCodeSensitiveWordsDetected ErrorCode = "sensitive_words_detected"
	ErrorCodeViolationFeeGrokCSAM   ErrorCode = "violation_fee.grok.csam"

	// new api error
	ErrorCodeCountTokenFailed   ErrorCode = "count_token_failed"
	ErrorCodeModelPriceError    ErrorCode = "model_price_error"
	ErrorCodeInvalidApiType     ErrorCode = "invalid_api_type"
	ErrorCodeJsonMarshalFailed  ErrorCode = "json_marshal_failed"
	ErrorCodeDoRequestFailed    ErrorCode = "do_request_failed"
	ErrorCodeGetChannelFailed   ErrorCode = "get_channel_failed"
	ErrorCodeGenRelayInfoFailed ErrorCode = "gen_relay_info_failed"

	// channel error
	ErrorCodeChannelNoAvailableKey         ErrorCode = "channel:no_available_key"
	ErrorCodeChannelParamOverrideInvalid   ErrorCode = "channel:param_override_invalid"
	ErrorCodeChannelHeaderOverrideInvalid  ErrorCode = "channel:header_override_invalid"
	ErrorCodeChannelModelMappedError       ErrorCode = "channel:model_mapped_error"
	ErrorCodeChannelAwsClientError         ErrorCode = "channel:aws_client_error"
	ErrorCodeChannelInvalidKey             ErrorCode = "channel:invalid_key"
	ErrorCodeChannelResponseTimeExceeded   ErrorCode = "channel:response_time_exceeded"
	ErrorCodeChannelConcurrencyExceeded    ErrorCode = "channel_concurrency_exceeded"
	ErrorCodeChannelConcurrencyUnavailable ErrorCode = "channel_concurrency_unavailable"

	// client request error
	ErrorCodeReadRequestBodyFailed ErrorCode = "read_request_body_failed"
	ErrorCodeConvertRequestFailed  ErrorCode = "convert_request_failed"
	ErrorCodeAccessDenied          ErrorCode = "access_denied"

	// request error
	ErrorCodeBadRequestBody ErrorCode = "bad_request_body"

	// response error
	ErrorCodeReadResponseBodyFailed       ErrorCode = "read_response_body_failed"
	ErrorCodeBadResponseStatusCode        ErrorCode = "bad_response_status_code"
	ErrorCodeBadResponse                  ErrorCode = "bad_response"
	ErrorCodeBadResponseBody              ErrorCode = "bad_response_body"
	ErrorCodeEmptyResponse                ErrorCode = "empty_response"
	ErrorCodeUpstreamRequestTooLarge      ErrorCode = "upstream_request_too_large"
	ErrorCodeUpstreamTransportInterrupted ErrorCode = "upstream_transport_interrupted"
	ErrorCodeAwsInvokeError               ErrorCode = "aws_invoke_error"
	ErrorCodeModelNotFound                ErrorCode = "model_not_found"
	ErrorCodePromptBlocked                ErrorCode = "prompt_blocked"

	// sql error
	ErrorCodeQueryDataError  ErrorCode = "query_data_error"
	ErrorCodeUpdateDataError ErrorCode = "update_data_error"

	// quota error
	ErrorCodeInsufficientUserQuota      ErrorCode = "insufficient_user_quota"
	ErrorCodePreConsumeTokenQuotaFailed ErrorCode = "pre_consume_token_quota_failed"
)

func UpstreamHTTPStatusErrorCode(statusCode int) (ErrorCode, bool) {
	switch statusCode {
	case http.StatusRequestEntityTooLarge:
		return ErrorCodeUpstreamRequestTooLarge, true
	default:
		return "", false
	}
}

func UpstreamHTTPStatusErrorMessage(errorCode ErrorCode, statusCode int) string {
	switch errorCode {
	case ErrorCodeUpstreamRequestTooLarge:
		return fmt.Sprintf("upstream request too large: status code %d", statusCode)
	default:
		return fmt.Sprintf("bad response status code %d", statusCode)
	}
}

func IsUpstreamTransportInterruptedError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, indicator := range []string{
		"unexpected eof",
		"server sent goaway",
		"http2: server sent goaway",
		"stream error: stream id",
		"response body closed",
		"connection reset by peer",
	} {
		if strings.Contains(message, indicator) {
			return true
		}
	}
	return false
}

type NewAPIError struct {
	Err        error
	RelayError any
	// cause keeps the original error available to errors.Is/errors.As while Err
	// remains safe to expose through legacy callers that access the exported
	// field directly.
	cause              error
	skipRetry          bool
	recordErrorLog     *bool
	errorType          ErrorType
	errorCode          ErrorCode
	StatusCode         int
	OriginalStatusCode int
	// RetryAfterSeconds carries an upstream Retry-After hint for routing and
	// health cooldown decisions. It is never exposed as a credential or body.
	RetryAfterSeconds int
	Metadata          json.RawMessage
}

// Unwrap enables errors.Is / errors.As to work with NewAPIError by exposing the underlying error.
func (e *NewAPIError) Unwrap() error {
	if e == nil {
		return nil
	}
	if e.cause != nil {
		return e.cause
	}
	return e.Err
}

func (e *NewAPIError) GetErrorCode() ErrorCode {
	if e == nil {
		return ""
	}
	return e.errorCode
}

func (e *NewAPIError) GetErrorType() ErrorType {
	if e == nil {
		return ""
	}
	return e.errorType
}

func (e *NewAPIError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		// fallback message when underlying error is missing
		return string(e.errorCode)
	}
	return common.MaskSensitiveInfo(e.Err.Error())
}

func (e *NewAPIError) ErrorWithStatusCode() string {
	if e == nil {
		return ""
	}
	msg := e.Error()
	if e.StatusCode == 0 {
		return msg
	}
	if msg == "" {
		return fmt.Sprintf("status_code=%d", e.StatusCode)
	}
	return fmt.Sprintf("status_code=%d, %s", e.StatusCode, msg)
}

func (e *NewAPIError) MaskSensitiveError() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return string(e.errorCode)
	}
	errStr := e.Err.Error()
	return common.MaskSensitiveInfo(errStr)
}

func (e *NewAPIError) MaskSensitiveErrorWithStatusCode() string {
	if e == nil {
		return ""
	}
	msg := e.MaskSensitiveError()
	if e.StatusCode == 0 {
		return msg
	}
	if msg == "" {
		return fmt.Sprintf("status_code=%d", e.StatusCode)
	}
	return fmt.Sprintf("status_code=%d, %s", e.StatusCode, msg)
}

func (e *NewAPIError) SetMessage(message string) {
	if e == nil {
		return
	}
	e.Err = safeError(errors.New(message))
}

func (e *NewAPIError) ToOpenAIError() OpenAIError {
	if e == nil {
		return OpenAIError{
			Message: "unknown error",
			Type:    string(ErrorTypeNewAPIError),
			Code:    "unknown_error",
		}
	}
	var result OpenAIError
	switch e.errorType {
	case ErrorTypeOpenAIError:
		if openAIError, ok := e.RelayError.(OpenAIError); ok {
			result = openAIError
		}
	case ErrorTypeClaudeError:
		if claudeError, ok := e.RelayError.(ClaudeError); ok {
			result = OpenAIError{
				Message: e.Error(),
				Type:    claudeError.Type,
				Param:   "",
				Code:    e.errorCode,
			}
		}
	default:
		result = OpenAIError{
			Message: e.Error(),
			Type:    string(e.errorType),
			Param:   "",
			Code:    e.errorCode,
		}
	}
	result = sanitizeOpenAIError(result)
	if result.Message == "" {
		result.Message = string(e.errorType)
	}
	return result
}

func (e *NewAPIError) ToClaudeError() ClaudeError {
	if e == nil {
		return ClaudeError{
			Message: "unknown error",
			Type:    string(ErrorTypeNewAPIError),
		}
	}
	var result ClaudeError
	switch e.errorType {
	case ErrorTypeOpenAIError:
		if openAIError, ok := e.RelayError.(OpenAIError); ok {
			result = ClaudeError{
				Message: e.Error(),
				Type:    fmt.Sprintf("%v", openAIError.Code),
			}
		}
	case ErrorTypeClaudeError:
		if claudeError, ok := e.RelayError.(ClaudeError); ok {
			result = claudeError
		}
	default:
		result = ClaudeError{
			Message: e.Error(),
			Type:    string(e.errorType),
		}
	}
	result.Message = common.MaskSensitiveInfo(result.Message)
	if result.Message == "" {
		result.Message = string(e.errorType)
	}
	return result
}

type NewAPIErrorOptions func(*NewAPIError)

func NewError(err error, errorCode ErrorCode, ops ...NewAPIErrorOptions) *NewAPIError {
	var newErr *NewAPIError
	// 保留深层传递的 new err
	if errors.As(err, &newErr) && newErr != nil {
		for _, op := range ops {
			op(newErr)
		}
		return newErr
	}
	e := &NewAPIError{
		Err:        safeError(err),
		cause:      err,
		RelayError: nil,
		errorType:  ErrorTypeNewAPIError,
		StatusCode: http.StatusInternalServerError,
		errorCode:  errorCode,
	}
	for _, op := range ops {
		op(e)
	}
	return e
}

func NewOpenAIError(err error, errorCode ErrorCode, statusCode int, ops ...NewAPIErrorOptions) *NewAPIError {
	var newErr *NewAPIError
	// 保留深层传递的 new err
	if errors.As(err, &newErr) && newErr != nil {
		if newErr.RelayError == nil {
			openaiError := OpenAIError{
				Message: newErr.Error(),
				Type:    string(errorCode),
				Code:    errorCode,
			}
			newErr.RelayError = openaiError
		}
		for _, op := range ops {
			op(newErr)
		}
		return newErr
	}
	openaiError := OpenAIError{
		Message: errorMessage(err),
		Type:    string(errorCode),
		Code:    errorCode,
	}
	return WithOpenAIError(openaiError, statusCode, ops...)
}

func InitOpenAIError(errorCode ErrorCode, statusCode int, ops ...NewAPIErrorOptions) *NewAPIError {
	openaiError := OpenAIError{
		Type: string(errorCode),
		Code: errorCode,
	}
	return WithOpenAIError(openaiError, statusCode, ops...)
}

func NewErrorWithStatusCode(err error, errorCode ErrorCode, statusCode int, ops ...NewAPIErrorOptions) *NewAPIError {
	e := &NewAPIError{
		Err:   safeError(err),
		cause: err,
		RelayError: OpenAIError{
			Message: safeErrorMessage(err),
			Type:    string(errorCode),
		},
		errorType:  ErrorTypeNewAPIError,
		StatusCode: statusCode,
		errorCode:  errorCode,
	}
	for _, op := range ops {
		op(e)
	}

	return e
}

func errorMessage(err error) string {
	if err == nil {
		return "unknown error"
	}
	return err.Error()
}

func safeErrorMessage(err error) string {
	return common.MaskSensitiveInfo(errorMessage(err))
}

func safeError(err error) error {
	return errors.New(safeErrorMessage(err))
}

func sanitizeOpenAIError(openAIError OpenAIError) OpenAIError {
	openAIError.Message = common.MaskSensitiveInfo(openAIError.Message)
	openAIError.Param = common.MaskSensitiveInfo(openAIError.Param)
	if len(openAIError.Metadata) > 0 {
		openAIError.Metadata = sanitizeErrorMetadata(openAIError.Metadata)
	}
	return openAIError
}

func sanitizeErrorMetadata(raw json.RawMessage) json.RawMessage {
	var value any
	if err := common.UnmarshalWithNumber(raw, &value); err != nil {
		// Invalid metadata is not useful as a structured response. Discard it
		// instead of risking an invalid or partially masked JSON payload.
		return nil
	}
	sanitized := sanitizeErrorValue(value)
	encoded, err := common.Marshal(sanitized)
	if err != nil {
		return nil
	}
	return json.RawMessage(encoded)
}

func sanitizeErrorValue(value any) any {
	switch typed := value.(type) {
	case string:
		return common.MaskSensitiveInfo(typed)
	case []any:
		for index := range typed {
			typed[index] = sanitizeErrorValue(typed[index])
		}
		return typed
	case map[string]any:
		sanitized := make(map[string]any, len(typed))
		for key, item := range typed {
			sanitized[common.MaskSensitiveInfo(key)] = sanitizeErrorValue(item)
		}
		return sanitized
	case json.Number:
		// Preserve the lexical JSON number. Decoding metadata into interface{}
		// with the default decoder would convert integers to float64 and can
		// silently round large IDs when the error is re-encoded.
		return typed
	default:
		return value
	}
}

func WithOpenAIError(openAIError OpenAIError, statusCode int, ops ...NewAPIErrorOptions) *NewAPIError {
	openAIError = sanitizeOpenAIError(openAIError)
	code, ok := openAIError.Code.(string)
	if !ok {
		if openAIError.Code != nil {
			code = fmt.Sprintf("%v", openAIError.Code)
		} else {
			code = "unknown_error"
		}
	}
	if openAIError.Type == "" {
		openAIError.Type = "upstream_error"
	}
	e := &NewAPIError{
		RelayError: openAIError,
		errorType:  ErrorTypeOpenAIError,
		StatusCode: statusCode,
		Err:        errors.New(openAIError.Message),
		errorCode:  ErrorCode(code),
	}
	// OpenRouter
	if len(openAIError.Metadata) > 0 {
		openAIError.Message = fmt.Sprintf("%s (%s)", openAIError.Message, openAIError.Metadata)
		e.Metadata = openAIError.Metadata
		e.RelayError = openAIError
		e.Err = errors.New(openAIError.Message)
	}
	for _, op := range ops {
		op(e)
	}
	return e
}

func WithClaudeError(claudeError ClaudeError, statusCode int, ops ...NewAPIErrorOptions) *NewAPIError {
	claudeError.Message = common.MaskSensitiveInfo(claudeError.Message)
	if claudeError.Type == "" {
		claudeError.Type = "upstream_error"
	}
	e := &NewAPIError{
		RelayError: claudeError,
		errorType:  ErrorTypeClaudeError,
		StatusCode: statusCode,
		Err:        errors.New(claudeError.Message),
		errorCode:  ErrorCode(claudeError.Type),
	}
	for _, op := range ops {
		op(e)
	}
	return e
}

func IsChannelError(err *NewAPIError) bool {
	if err == nil {
		return false
	}
	if strings.HasPrefix(string(err.errorCode), "channel:") {
		return true
	}
	// These capacity errors are kept as stable, user-facing error codes without
	// the legacy channel: prefix. They still belong to the channel error class
	// for retry, health, and observability handling.
	switch err.errorCode {
	case ErrorCodeChannelConcurrencyExceeded, ErrorCodeChannelConcurrencyUnavailable:
		return true
	default:
		return false
	}
}

func IsSkipRetryError(err *NewAPIError) bool {
	if err == nil {
		return false
	}

	return err.skipRetry
}

func ErrOptionWithSkipRetry() NewAPIErrorOptions {
	return func(e *NewAPIError) {
		e.skipRetry = true
	}
}

func ErrOptionWithNoRecordErrorLog() NewAPIErrorOptions {
	return func(e *NewAPIError) {
		e.recordErrorLog = common.GetPointer(false)
	}
}

func ErrOptionWithStatusCode(statusCode int) NewAPIErrorOptions {
	return func(e *NewAPIError) {
		e.StatusCode = statusCode
	}
}

func ErrOptionWithErrorCode(errorCode ErrorCode) NewAPIErrorOptions {
	return func(e *NewAPIError) {
		e.errorCode = errorCode
	}
}

func ErrOptionWithHideErrMsg(replaceStr string) NewAPIErrorOptions {
	return func(e *NewAPIError) {
		if common.DebugEnabled {
			fmt.Printf("ErrOptionWithHideErrMsg: %s, origin error: %s", replaceStr, common.MaskSensitiveInfo(errorMessage(e.Err)))
		}
		e.Err = safeError(errors.New(replaceStr))
	}
}

func IsRecordErrorLog(e *NewAPIError) bool {
	if e == nil {
		return false
	}
	if e.recordErrorLog == nil {
		// default to true if not set
		return true
	}
	return *e.recordErrorLog
}
