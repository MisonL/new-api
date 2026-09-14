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

// IsResponsesCompactPath reports whether path is one of the native Responses
// compaction endpoints exposed by this gateway.
func IsResponsesCompactPath(path string) bool {
	switch path {
	case "/v1/responses/compact", "/responses/compact":
		return true
	default:
		return false
	}
}

// IsCodexLocalResponsesCompactionRequest identifies the exact local Codex
// Responses compaction ingress without reading the request body.
func IsCodexLocalResponsesCompactionRequest(request *http.Request) bool {
	if request == nil || request.Method != http.MethodPost || request.URL == nil || request.URL.Path != "/v1/responses" {
		return false
	}

	metadata, ok := codexTurnMetadataFromRequest(request)
	if !ok {
		return false
	}
	return strings.EqualFold(metadata.RequestKind, "compaction") &&
		metadata.Compaction != nil &&
		strings.EqualFold(metadata.Compaction.Implementation, "responses")
}

// IsCodexOrdinaryResponsesRequest identifies an ordinary Codex Responses turn
// without reading its body. Codex emits x-codex-turn-metadata as the direct
// HTTP compatibility projection of its canonical client metadata. Control
// flows such as prewarm, memory, and remote compaction intentionally remain on
// their existing paths.
func IsCodexOrdinaryResponsesRequest(request *http.Request) bool {
	if request == nil || request.Method != http.MethodPost || request.URL == nil || request.URL.Path != "/v1/responses" {
		return false
	}
	metadata, ok := codexTurnMetadataFromRequest(request)
	return ok && metadata.RequestKind == "turn"
}

func codexTurnMetadataFromRequest(request *http.Request) (codexTurnMetadata, bool) {
	if request == nil {
		return codexTurnMetadata{}, false
	}
	rawMetadata := strings.TrimSpace(request.Header.Get("X-Codex-Turn-Metadata"))
	if rawMetadata == "" {
		return codexTurnMetadata{}, false
	}
	var metadata codexTurnMetadata
	if err := Unmarshal([]byte(rawMetadata), &metadata); err != nil {
		return codexTurnMetadata{}, false
	}
	return metadata, true
}

// IsResponsesCompactionIngressRequest identifies requests that need the
// authenticated, disk-backed Responses compaction ingress. Direct compact
// endpoints do not carry the local Codex turn metadata but can contain the
// same opaque encrypted state, so they must not bypass that protection.
func IsResponsesCompactionIngressRequest(request *http.Request) bool {
	if request == nil || request.Method != http.MethodPost || request.URL == nil {
		return false
	}
	if IsResponsesCompactPath(request.URL.Path) {
		return true
	}
	return IsCodexLocalResponsesCompactionRequest(request)
}

// IsResponsesOpaqueReplayIngressRequest identifies requests that must enter
// the authenticated disk-backed replay ingress before their JSON body can be
// inspected. Direct compact endpoints always qualify. Ordinary Responses only
// qualify when they are a Codex turn, avoiding a behavior change for unrelated
// clients and distinct Codex control flows.
func IsResponsesOpaqueReplayIngressRequest(request *http.Request) bool {
	return IsResponsesCompactionIngressRequest(request) || IsCodexOrdinaryResponsesRequest(request)
}
