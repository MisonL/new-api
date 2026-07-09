package dto

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// OpenAIResponsesCompactionRequest mirrors OpenAIResponsesRequest for compact ingress.
// Keep this struct and ToResponsesRequest in sync when adding Responses fields.
type OpenAIResponsesCompactionRequest struct {
	Model                string                     `json:"model"`
	Input                json.RawMessage            `json:"input,omitempty"`
	Include              json.RawMessage            `json:"include,omitempty"`
	Conversation         json.RawMessage            `json:"conversation,omitempty"`
	ContextManagement    json.RawMessage            `json:"context_management,omitempty"`
	Instructions         json.RawMessage            `json:"instructions,omitempty"`
	MaxOutputTokens      *uint                      `json:"max_output_tokens,omitempty"`
	TopLogProbs          *int                       `json:"top_logprobs,omitempty"`
	Metadata             json.RawMessage            `json:"metadata,omitempty"`
	ParallelToolCalls    json.RawMessage            `json:"parallel_tool_calls,omitempty"`
	PreviousResponseID   string                     `json:"previous_response_id,omitempty"`
	Reasoning            *Reasoning                 `json:"reasoning,omitempty"`
	ServiceTier          string                     `json:"service_tier,omitempty"`
	Store                json.RawMessage            `json:"store,omitempty"`
	PromptCacheKey       json.RawMessage            `json:"prompt_cache_key,omitempty"`
	PromptCacheRetention json.RawMessage            `json:"prompt_cache_retention,omitempty"`
	SafetyIdentifier     json.RawMessage            `json:"safety_identifier,omitempty"`
	Stream               *bool                      `json:"stream,omitempty"`
	StreamOptions        *StreamOptions             `json:"stream_options,omitempty"`
	Temperature          *float64                   `json:"temperature,omitempty"`
	Text                 json.RawMessage            `json:"text,omitempty"`
	ToolChoice           json.RawMessage            `json:"tool_choice,omitempty"`
	Tools                json.RawMessage            `json:"tools,omitempty"`
	TopP                 *float64                   `json:"top_p,omitempty"`
	Truncation           json.RawMessage            `json:"truncation,omitempty"`
	User                 json.RawMessage            `json:"user,omitempty"`
	MaxToolCalls         *uint                      `json:"max_tool_calls,omitempty"`
	Prompt               json.RawMessage            `json:"prompt,omitempty"`
	EnableThinking       json.RawMessage            `json:"enable_thinking,omitempty"`
	Preset               json.RawMessage            `json:"preset,omitempty"`
	Extra                map[string]json.RawMessage `json:"-"`
}

var openAIResponsesCompactionRequestKnownFields = sync.OnceValue(func() map[string]struct{} {
	return GetJSONFieldNames(reflect.TypeOf(OpenAIResponsesCompactionRequest{}))
})

func (r *OpenAIResponsesCompactionRequest) UnmarshalJSON(data []byte) error {
	var rawMap map[string]json.RawMessage
	if err := common.Unmarshal(data, &rawMap); err != nil {
		return err
	}

	type Alias OpenAIResponsesCompactionRequest
	var known Alias
	if err := common.Unmarshal(data, &known); err != nil {
		return err
	}
	*r = OpenAIResponsesCompactionRequest(known)

	r.Extra = collectUnknownJSONFields(rawMap, openAIResponsesCompactionRequestKnownFields())
	return nil
}

func (r OpenAIResponsesCompactionRequest) MarshalJSON() ([]byte, error) {
	type Alias OpenAIResponsesCompactionRequest
	alias := Alias(r)
	base, err := common.Marshal(alias)
	if err != nil {
		return nil, err
	}
	return marshalWithExtraJSONFields(base, r.Extra)
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
		Model:                r.Model,
		Input:                r.Input,
		Include:              r.Include,
		Conversation:         r.Conversation,
		ContextManagement:    r.ContextManagement,
		Instructions:         r.Instructions,
		MaxOutputTokens:      clonePtr(r.MaxOutputTokens),
		TopLogProbs:          clonePtr(r.TopLogProbs),
		Metadata:             r.Metadata,
		ParallelToolCalls:    r.ParallelToolCalls,
		PreviousResponseID:   r.PreviousResponseID,
		Reasoning:            clonePtr(r.Reasoning),
		ServiceTier:          r.ServiceTier,
		Store:                r.Store,
		PromptCacheKey:       r.PromptCacheKey,
		PromptCacheRetention: r.PromptCacheRetention,
		SafetyIdentifier:     r.SafetyIdentifier,
		Stream:               clonePtr(r.Stream),
		StreamOptions:        clonePtr(r.StreamOptions),
		Temperature:          clonePtr(r.Temperature),
		Text:                 r.Text,
		ToolChoice:           r.ToolChoice,
		Tools:                r.Tools,
		TopP:                 clonePtr(r.TopP),
		Truncation:           r.Truncation,
		User:                 r.User,
		MaxToolCalls:         clonePtr(r.MaxToolCalls),
		Prompt:               r.Prompt,
		EnableThinking:       r.EnableThinking,
		Preset:               r.Preset,
		Extra:                cloneRawMessageMap(r.Extra),
	}
}

func clonePtr[T any](value *T) *T {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func (r *OpenAIResponsesCompactionRequest) IsStream(c *gin.Context) bool {
	if r == nil || r.Stream == nil {
		return false
	}
	return *r.Stream
}

func (r *OpenAIResponsesCompactionRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}
