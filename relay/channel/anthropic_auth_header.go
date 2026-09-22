package channel

import (
	"net/http"
)

// AnthropicAuthHeaderNames are the header names an Anthropic-native request
// carries its credential in. Authorization is the canonical form; x-api-key is
// the legacy fallback the Messages API still accepts in its place.
const (
	AnthropicAuthorizationHeaderName = "Authorization"
	AnthropicApiKeyHeaderName        = "x-api-key"
	AnthropicVersionHeaderName       = "anthropic-version"
)

// DefaultAnthropicVersion is the Messages API version sent when the caller
// does not specify one.
const DefaultAnthropicVersion = "2023-06-01"

// SetAnthropicAuthHeaders writes the Anthropic credential headers for an
// Anthropic-native request onto header.
//
// The Messages API documents Authorization: Bearer as the canonical credential
// and x-api-key as a legacy fallback consulted only when Authorization is
// absent. Third-party Anthropic-compatible gateways often implement only the
// bearer form, so both are emitted. Both carry the same token, so the
// authenticated principal cannot depend on which header an upstream reads.
//
// This is the single implementation shared by relay traffic (the claude
// adaptor) and the channel "fetch models" path (controller), so the two cannot
// drift apart.
func SetAnthropicAuthHeaders(header http.Header, token string) {
	header.Set(AnthropicApiKeyHeaderName, token)
	header.Set(AnthropicAuthorizationHeaderName, "Bearer "+token)
}

// AnthropicAuthHeaders returns a new header set carrying the Anthropic
// credential headers for token, plus the default Messages API version. It is
// the header-construction form used by callers that build a fresh header set
// rather than filling an existing one.
func AnthropicAuthHeaders(token string) http.Header {
	header := http.Header{}
	SetAnthropicAuthHeaders(header, token)
	header.Set(AnthropicVersionHeaderName, DefaultAnthropicVersion)
	return header
}
