package dto

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type OpenAIResponsesCompactionRequest struct {
	Model              string          `json:"model"`
	Input              json.RawMessage `json:"input,omitempty"`
	Instructions       json.RawMessage `json:"instructions,omitempty"`
	Tools              json.RawMessage `json:"tools,omitempty"`
	ParallelToolCalls  json.RawMessage `json:"parallel_tool_calls,omitempty"`
	Reasoning          *Reasoning      `json:"reasoning,omitempty"`
	ServiceTier        string          `json:"service_tier,omitempty"`
	Text               json.RawMessage `json:"text,omitempty"`
	PromptCacheKey     json.RawMessage `json:"prompt_cache_key,omitempty"`
	PreviousResponseID string          `json:"previous_response_id,omitempty"`
}

func (r *OpenAIResponsesCompactionRequest) GetTokenCountMeta() *types.TokenCountMeta {
	var parts []string
	if len(r.Instructions) > 0 {
		parts = append(parts, string(r.Instructions))
	}
	if len(r.Input) > 0 {
		parts = append(parts, string(r.Input))
	}
	if len(r.Tools) > 0 {
		parts = append(parts, string(r.Tools))
	}
	if len(r.ParallelToolCalls) > 0 {
		parts = append(parts, string(r.ParallelToolCalls))
	}
	if r.Reasoning != nil {
		if raw, err := common.Marshal(r.Reasoning); err == nil {
			parts = append(parts, string(raw))
		}
	}
	if len(r.Text) > 0 {
		parts = append(parts, string(r.Text))
	}
	return &types.TokenCountMeta{
		CombineText: strings.Join(parts, "\n"),
	}
}

func (r *OpenAIResponsesCompactionRequest) ToResponsesRequest() *OpenAIResponsesRequest {
	if r == nil {
		return nil
	}
	return &OpenAIResponsesRequest{
		Model:              r.Model,
		Input:              r.Input,
		Instructions:       r.Instructions,
		Tools:              r.Tools,
		ParallelToolCalls:  r.ParallelToolCalls,
		Reasoning:          r.Reasoning,
		ServiceTier:        r.ServiceTier,
		Text:               r.Text,
		PromptCacheKey:     r.PromptCacheKey,
		PreviousResponseID: r.PreviousResponseID,
	}
}

func (r *OpenAIResponsesCompactionRequest) IsStream(c *gin.Context) bool {
	return false
}

func (r *OpenAIResponsesCompactionRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}
