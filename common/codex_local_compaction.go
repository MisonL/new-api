package common

import (
	"net/http"
	"strings"
)

type codexTurnMetadata struct {
	RequestKind string                          `json:"request_kind"`
	Compaction  *codexCompactionMetadataPayload `json:"compaction"`
}

type codexCompactionMetadataPayload struct {
	Implementation string `json:"implementation"`
}

// IsCodexLocalResponsesCompactionRequest identifies the exact local Codex
// Responses compaction ingress without reading the request body.
func IsCodexLocalResponsesCompactionRequest(request *http.Request) bool {
	if request == nil || request.Method != http.MethodPost || request.URL == nil || request.URL.Path != "/v1/responses" {
		return false
	}

	rawMetadata := strings.TrimSpace(request.Header.Get("X-Codex-Turn-Metadata"))
	if rawMetadata == "" {
		return false
	}
	var metadata codexTurnMetadata
	if err := Unmarshal([]byte(rawMetadata), &metadata); err != nil {
		return false
	}
	return metadata.RequestKind == "compaction" &&
		metadata.Compaction != nil &&
		metadata.Compaction.Implementation == "responses"
}
