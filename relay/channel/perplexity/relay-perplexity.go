package perplexity

import "github.com/QuantumNous/new-api/dto"

func requestOpenAI2Perplexity(request dto.GeneralOpenAIRequest) *dto.GeneralOpenAIRequest {
	req := request
	req.Messages = append([]dto.Message(nil), request.Messages...)
	if request.MaxTokens != nil || request.MaxCompletionTokens != nil {
		maxTokens := request.GetMaxTokens()
		req.MaxTokens = &maxTokens
		req.MaxCompletionTokens = nil
	}
	return &req
}
