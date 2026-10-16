package openai

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
)

func responsesOpenAIErrorHasContent(openAIError *types.OpenAIError) bool {
	return openAIError.HasContent()
}

func responsesOpenAIErrorHasMessage(openAIError *types.OpenAIError) bool {
	return openAIError != nil && strings.TrimSpace(openAIError.Message) != ""
}

// responsesFailedEventOpenAIError accepts both Responses terminal failure forms
// used by upstreams: response.error and top-level error.
func responsesFailedEventOpenAIError(event dto.ResponsesStreamResponse) *types.OpenAIError {
	if responsesOpenAIErrorHasMessage(event.Error) {
		return event.Error
	}
	if event.Response == nil {
		return nil
	}
	if openAIError := event.Response.GetOpenAIError(); responsesOpenAIErrorHasMessage(openAIError) {
		return openAIError
	}
	return nil
}

func normalizeResponsesUpstreamOpenAIError(openAIError *types.OpenAIError) *types.OpenAIError {
	if openAIError == nil {
		return nil
	}
	normalized := *openAIError
	if isResponsesContextWindowError(&normalized) && !isResponsesRateLimitError(&normalized) {
		normalized.Type = "invalid_request_error"
	}
	return &normalized
}

func newResponsesUpstreamOpenAIError(statusCode int, openAIError *types.OpenAIError) *types.NewAPIError {
	normalized := normalizeResponsesUpstreamOpenAIError(openAIError)
	if normalized == nil {
		return types.NewOpenAIError(
			&responsesMissingUpstreamError{},
			types.ErrorCodeBadResponse,
			http.StatusBadGateway,
		)
	}
	return types.WithOpenAIError(*normalized, responsesUpstreamOpenAIErrorStatus(statusCode, normalized))
}

func responsesUpstreamOpenAIErrorStatus(statusCode int, openAIError *types.OpenAIError) int {
	return responsesOpenAIErrorStatus(statusCode, openAIError)
}

func isResponsesRateLimitError(openAIError *types.OpenAIError) bool {
	if openAIError == nil {
		return false
	}
	errorType := strings.ToLower(strings.TrimSpace(openAIError.Type))
	errorCode := strings.ToLower(strings.TrimSpace(fmt.Sprint(openAIError.Code)))
	return strings.Contains(errorType, "rate_limit") || strings.Contains(errorCode, "rate_limit")
}

func isResponsesContextWindowError(openAIError *types.OpenAIError) bool {
	if openAIError == nil {
		return false
	}
	contextError := strings.ToLower(strings.Join([]string{
		openAIError.Message,
		openAIError.Type,
		fmt.Sprint(openAIError.Code),
	}, " "))
	for _, indicator := range []string{
		"context length",
		"context window",
		"context limit",
		"context_length_exceeded",
		"context_too_large",
		"input exceeds",
		"input too large",
		"maximum context",
		"max context",
		"payload too large",
		"request too large",
		"string_above_max_length",
		"too many tokens",
		"token limit",
	} {
		if strings.Contains(contextError, indicator) {
			return true
		}
	}
	return false
}

type responsesMissingUpstreamError struct{}

func (*responsesMissingUpstreamError) Error() string {
	return "upstream Responses error payload is missing"
}
