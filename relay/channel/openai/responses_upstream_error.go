package openai

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
)

func responsesOpenAIErrorHasContent(openAIError *types.OpenAIError) bool {
	return openAIError != nil &&
		(openAIError.Type != "" || openAIError.Message != "" || openAIError.Param != "" || openAIError.Code != nil)
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
	if isResponsesContextWindowError(&normalized) {
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

type responsesMissingUpstreamError struct{}

func (*responsesMissingUpstreamError) Error() string {
	return "upstream Responses error payload is missing"
}
