package dto

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// OpaqueOpenAIResponsesRequest represents a locally spooled Responses request
// that contains encrypted context. It intentionally holds only bounded routing
// metadata; the request body remains on disk for native replay.
type OpaqueOpenAIResponsesRequest struct {
	Model    string
	metadata common.ResponsesRequestMetadata
}

func NewOpaqueOpenAIResponsesRequest(metadata common.ResponsesRequestMetadata) *OpaqueOpenAIResponsesRequest {
	return &OpaqueOpenAIResponsesRequest{
		Model:    metadata.Model,
		metadata: cloneOpaqueResponsesRequestMetadata(metadata),
	}
}

func (r *OpaqueOpenAIResponsesRequest) IsOpaqueResponsesRequest() bool {
	return r != nil
}

func (r *OpaqueOpenAIResponsesRequest) OpaqueResponsesMetadata() common.ResponsesRequestMetadata {
	if r == nil {
		return common.ResponsesRequestMetadata{}
	}
	return cloneOpaqueResponsesRequestMetadata(r.metadata)
}

func (r *OpaqueOpenAIResponsesRequest) GetTokenCountMeta() *types.TokenCountMeta {
	if r == nil {
		return &types.TokenCountMeta{}
	}
	maxTokens := 0
	if r.metadata.MaxOutputTokens != nil {
		maxTokens = int(*r.metadata.MaxOutputTokens)
	}
	return &types.TokenCountMeta{MaxTokens: maxTokens}
}

func (r *OpaqueOpenAIResponsesRequest) IsStream(_ *gin.Context) bool {
	return r != nil && r.metadata.Stream != nil && *r.metadata.Stream
}

func (r *OpaqueOpenAIResponsesRequest) SetModelName(modelName string) {
	if r != nil && modelName != "" {
		r.Model = modelName
	}
}

func (r *OpaqueOpenAIResponsesRequest) MarshalJSON() ([]byte, error) {
	return nil, errors.New("opaque Responses request body must be replayed from storage")
}

func cloneOpaqueResponsesRequestMetadata(metadata common.ResponsesRequestMetadata) common.ResponsesRequestMetadata {
	return metadata.Clone()
}
