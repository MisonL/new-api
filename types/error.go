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

// HasContent reports whether the error object carries real failure
// information. GetOpenAIError is non-nil for any present `error` key, so a
// content check is required: placeholder frames such as `{"error":{}}` or
// `{"error":{"message":""}}` must not abort a healthy stream, while a
// code-only or message-only frame is a failure. Every entry point (non-stream
// OpenaiHandler, the streaming guard, the Responses guards) checks this same
// predicate so one upstream shape is never a failure in one path and success
// in another.
func (e *OpenAIError) HasContent() bool {
	return e != nil &&
		(e.Type != "" || e.Message != "" || e.Param != "" || e.Code != nil)
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

// HasContent reports whether the error carries any usable detail.
// Upstreams emit both the canonical form (with "type") and a bare
// {"code","message"} form; GetClaudeError only fills Type when the frame
// has a "type" key, so a guard that checks Type alone drops the latter.
// An empty error object decodes to a non-nil but all-empty ClaudeError and
// must not count as an error.
func (c ClaudeError) HasContent() bool {
	return c.Type != "" || c.Message != ""
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

// Standard OpenAI error `type` values. Clients route on the HTTP status and
// surface `type` to users, so it has to come from a fixed vocabulary rather
// than from this gateway's internal error codes.
const (
	standardErrorTypeInvalidRequest    = "invalid_request_error"
	standardErrorTypeAuthentication    = "authentication_error"
	standardErrorTypePermission        = "permission_error"
	standardErrorTypeNotFound          = "not_found_error"
	standardErrorTypeRateLimit         = "rate_limit_error"
	standardErrorTypeServer            = "server_error"
	standardErrorTypeInsufficientQuota = "insufficient_quota"
)

// standardErrorTypes is the set of `type` values clients already handle. A value
// outside it is one this gateway or a foreign upstream invented.
var standardErrorTypes = map[string]struct{}{
	standardErrorTypeInvalidRequest:    {},
	standardErrorTypeAuthentication:    {},
	standardErrorTypePermission:        {},
	standardErrorTypeNotFound:          {},
	standardErrorTypeRateLimit:         {},
	standardErrorTypeServer:            {},
	standardErrorTypeInsufficientQuota: {},
}

// standardErrorType maps an internal error code and HTTP status onto the OpenAI
// error `type` vocabulary. InitOpenAIError and NewOpenAIError previously wrote
// the raw internal code into Type, so a client saw
// "type": "bad_response_status_code" -- a value that exists nowhere in the
// OpenAI API. The status code is the primary signal; the code only breaks ties
// where the status alone is ambiguous.
// StandardErrorTypeForStatus exposes the status-driven mapping for callers
// outside this package (middleware abort helpers) that have no error code.
func StandardErrorTypeForStatus(statusCode int) string {
	return standardErrorType("", statusCode)
}

// StandardErrorTypeName exposes the full code+status mapping for callers
// outside this package that carry an error code.
func StandardErrorTypeName(code ErrorCode, statusCode int) string {
	return standardErrorType(code, statusCode)
}

func standardErrorType(code ErrorCode, statusCode int) string {
	switch statusCode {
	case http.StatusUnauthorized:
		return standardErrorTypeAuthentication
	case http.StatusForbidden:
		if code == ErrorCodeInsufficientUserQuota || code == ErrorCodePreConsumeTokenQuotaFailed {
			return standardErrorTypeInsufficientQuota
		}
		return standardErrorTypePermission
	case http.StatusNotFound:
		return standardErrorTypeNotFound
	case http.StatusTooManyRequests:
		return standardErrorTypeRateLimit
	}
	if statusCode >= 400 && statusCode < 500 {
		return standardErrorTypeInvalidRequest
	}
	// A 5xx status is a gateway-side or upstream failure, and the code below
	// only carries 4xx meanings: mapping it here would label a 503 "no free
	// channel right now" as not_found_error, which a client that routes on
	// `type` reads as "the model does not exist" and stops retrying. 5xx is
	// never ambiguous, so it needs no tiebreaker.
	if statusCode >= 500 {
		return standardErrorTypeServer
	}
	switch code {
	case ErrorCodeModelNotFound:
		return standardErrorTypeNotFound
	case ErrorCodeInsufficientUserQuota, ErrorCodePreConsumeTokenQuotaFailed:
		return standardErrorTypeInsufficientQuota
	case ErrorCodeAccessDenied, ErrorCodeChannelInvalidKey:
		return standardErrorTypePermission
	case ErrorCodeModelPriceError, ErrorCodeSensitiveWordsDetected:
		return standardErrorTypeInvalidRequest
	}
	return standardErrorTypeServer
}

// StandardizeErrorType rewrites an OpenAIError's `type` field onto the OpenAI
// vocabulary using its own code as a tiebreaker. RelayErrorHandler uses it for
// errors built from an upstream body, where the upstream may supply a `type`
// from a different vocabulary entirely (for example a Python SDK class name).
// The message and code are left untouched.
func StandardizeErrorType(openAIError OpenAIError, statusCode int) OpenAIError {
	// An upstream that already speaks the standard vocabulary is left alone; only
	// foreign values (an internal error code, a Python SDK class name, another
	// provider's wording) are replaced.
	if _, ok := standardErrorTypes[openAIError.Type]; ok {
		return openAIError
	}
	// ErrorCode is a distinct named type, so a plain assertion to string fails
	// for the errors this gateway builds itself (NewOpenAIError and friends set
	// Code to an ErrorCode). Without this case the tiebreaker silently no-ops
	// and the code is read as empty. A nil Code stays empty rather than being
	// rendered, so the caller falls through to the status-only mapping.
	var code string
	switch typed := openAIError.Code.(type) {
	case nil:
		code = ""
	case string:
		code = typed
	case ErrorCode:
		code = string(typed)
	default:
		// An upstream may send a numeric or object code.
		code = fmt.Sprint(typed)
	}
	openAIError.Type = standardErrorType(ErrorCode(code), statusCode)
	return openAIError
}

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
		// An internally built error carries no upstream `type` of its own, so its
		// type comes from the standard vocabulary. Emitting the internal marker
		// ("new_api_error" and friends) leaked this gateway's own taxonomy into a
		// field clients switch on, next to real OpenAI values from other paths.
		result = OpenAIError{
			Message: e.Error(),
			Type:    standardErrorType(e.errorCode, e.StatusCode),
			Param:   "",
			Code:    e.errorCode,
		}
	}
	result = sanitizeOpenAIError(result)
	if result.Message == "" {
		// Fall back to the human-readable error, not to the internal type name.
		// The type fallback produced "message": "openai_error" for errors built
		// from an upstream body that carried no message of its own.
		result.Message = e.Error()
	}
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
			// OpenAIError.Code is `any` and stays nil when the upstream error body
			// carries no code field. Rendering it with %v produced the literal
			// "<nil>" as the client-facing Claude error type. Fall back to this
			// gateway's own error code, matching what ToOpenAIError does.
			claudeType := strings.TrimSpace(fmt.Sprintf("%v", openAIError.Code))
			if claudeType == "" || claudeType == "<nil>" {
				claudeType = string(e.errorCode)
			}
			result = ClaudeError{
				Message: e.Error(),
				Type:    claudeType,
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
				Type:    standardErrorType(errorCode, statusCode),
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
		Type:    standardErrorType(errorCode, statusCode),
		Code:    errorCode,
	}
	return WithOpenAIError(openaiError, statusCode, ops...)
}

func InitOpenAIError(errorCode ErrorCode, statusCode int, ops ...NewAPIErrorOptions) *NewAPIError {
	openaiError := OpenAIError{
		Type: standardErrorType(errorCode, statusCode),
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
