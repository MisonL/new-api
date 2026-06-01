package openai

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/types"
)

func responsesCompactOpenAIErrorHasContent(oaiError *types.OpenAIError) bool {
	return oaiError != nil &&
		(oaiError.Type != "" || oaiError.Message != "" || oaiError.Param != "" || oaiError.Code != nil)
}

func responsesCompactOpenAIErrorStatus(statusCode int, oaiError *types.OpenAIError) int {
	if statusCode != http.StatusOK {
		return statusCode
	}
	message := strings.ToLower(strings.Join([]string{
		oaiError.Message,
		oaiError.Type,
		fmt.Sprint(oaiError.Code),
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
		if strings.Contains(message, indicator) {
			return http.StatusBadRequest
		}
	}
	return http.StatusBadGateway
}
