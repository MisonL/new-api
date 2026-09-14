package openai

import "github.com/QuantumNous/new-api/types"

func responsesCompactOpenAIErrorHasContent(oaiError *types.OpenAIError) bool {
	return responsesOpenAIErrorHasContent(oaiError)
}

func responsesCompactOpenAIErrorStatus(statusCode int, oaiError *types.OpenAIError) int {
	return responsesOpenAIErrorStatus(statusCode, oaiError)
}
