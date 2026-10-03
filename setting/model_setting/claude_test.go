package model_setting

import (
	"net/http"
	"strings"
	"testing"
)

func TestClaudeSettingsWriteHeadersMergesConfiguredValuesIntoSingleHeader(t *testing.T) {
	settings := &ClaudeSettings{
		HeadersSettings: map[string]map[string][]string{
			"claude-3-7-sonnet-20250219-thinking": {
				"anthropic-beta": {
					"token-efficient-tools-2025-02-19",
				},
			},
		},
	}

	headers := http.Header{}
	headers.Set("anthropic-beta", "output-128k-2025-02-19")

	settings.WriteHeaders("claude-3-7-sonnet-20250219-thinking", &headers)

	got := headers.Values("anthropic-beta")
	if len(got) != 1 {
		t.Fatalf("expected a single merged header value, got %v", got)
	}
	expected := "output-128k-2025-02-19,token-efficient-tools-2025-02-19"
	if got[0] != expected {
		t.Fatalf("expected merged header %q, got %q", expected, got[0])
	}
}

func TestClaudeSettingsWriteHeadersDeduplicatesAcrossCommaSeparatedAndRepeatedValues(t *testing.T) {
	settings := &ClaudeSettings{
		HeadersSettings: map[string]map[string][]string{
			"claude-3-7-sonnet-20250219-thinking": {
				"anthropic-beta": {
					"token-efficient-tools-2025-02-19",
					"computer-use-2025-01-24",
				},
			},
		},
	}

	headers := http.Header{}
	headers.Add("anthropic-beta", "output-128k-2025-02-19, token-efficient-tools-2025-02-19")
	headers.Add("anthropic-beta", "token-efficient-tools-2025-02-19")

	settings.WriteHeaders("claude-3-7-sonnet-20250219-thinking", &headers)

	got := headers.Values("anthropic-beta")
	if len(got) != 1 {
		t.Fatalf("expected duplicate values to collapse into one header, got %v", got)
	}
	expected := "output-128k-2025-02-19,token-efficient-tools-2025-02-19,computer-use-2025-01-24"
	if got[0] != expected {
		t.Fatalf("expected deduplicated merged header %q, got %q", expected, got[0])
	}
}

// Credential headers must be replaced, never merged. Merge semantics are right
// for list-valued protocol headers such as anthropic-beta, where the caller and
// the operator both contribute values. They are wrong for a credential: joining
// the channel key with an operator-configured key produces a single malformed
// value that no upstream can authenticate, so a gateway that only reads
// Authorization would start rejecting requests that previously worked.
func TestClaudeSettingsWriteHeadersReplacesCredentialHeaders(t *testing.T) {
	settings := &ClaudeSettings{
		HeadersSettings: map[string]map[string][]string{
			"claude-opus-5": {
				"Authorization": {"Bearer operator-key"},
			},
		},
	}

	headers := http.Header{}
	headers.Set("Authorization", "Bearer channel-key")

	settings.WriteHeaders("claude-opus-5", &headers)

	got := headers.Values("Authorization")
	if len(got) != 1 {
		t.Fatalf("expected a single Authorization value, got %v", got)
	}
	if got[0] != "Bearer operator-key" {
		t.Fatalf("expected the configured credential to replace the channel one, got %q", got[0])
	}
	if strings.Contains(got[0], ",") {
		t.Fatalf("credential must not be comma-joined: %q", got[0])
	}
}

// The same applies to x-api-key, which is the legacy form of the same
// credential.
func TestClaudeSettingsWriteHeadersReplacesLegacyApiKeyHeader(t *testing.T) {
	settings := &ClaudeSettings{
		HeadersSettings: map[string]map[string][]string{
			"claude-opus-5": {
				"x-api-key": {"operator-key"},
			},
		},
	}

	headers := http.Header{}
	headers.Set("x-api-key", "channel-key")

	settings.WriteHeaders("claude-opus-5", &headers)

	got := headers.Values("x-api-key")
	if len(got) != 1 {
		t.Fatalf("expected a single x-api-key value, got %v", got)
	}
	if got[0] != "operator-key" {
		t.Fatalf("expected the configured key to replace the channel one, got %q", got[0])
	}
}

// A credential header is matched case-insensitively: http.Header canonicalizes
// keys, so a config written as "authorization" must hit the same branch.
func TestClaudeSettingsWriteHeadersReplacesCredentialHeadersCaseInsensitively(t *testing.T) {
	settings := &ClaudeSettings{
		HeadersSettings: map[string]map[string][]string{
			"claude-opus-5": {
				"authorization": {"Bearer operator-key"},
			},
		},
	}

	headers := http.Header{}
	headers.Set("Authorization", "Bearer channel-key")

	settings.WriteHeaders("claude-opus-5", &headers)

	got := headers.Values("Authorization")
	if len(got) != 1 || got[0] != "Bearer operator-key" {
		t.Fatalf("expected case-insensitive credential replacement, got %v", got)
	}
}

// Non-credential headers keep the merge behaviour: the existing tests above
// pin that, and this asserts the two branches coexist.
func TestClaudeSettingsWriteHeadersStillMergesNonCredentialHeaders(t *testing.T) {
	settings := &ClaudeSettings{
		HeadersSettings: map[string]map[string][]string{
			"claude-opus-5": {
				"anthropic-beta": {"operator-beta"},
			},
		},
	}

	headers := http.Header{}
	headers.Set("anthropic-beta", "client-beta")

	settings.WriteHeaders("claude-opus-5", &headers)

	got := headers.Values("anthropic-beta")
	if len(got) != 1 {
		t.Fatalf("expected a single merged value, got %v", got)
	}
	if got[0] != "client-beta,operator-beta" {
		t.Fatalf("expected non-credential headers to keep merging, got %q", got[0])
	}
}

// Authorization and x-api-key are two forms of the same Anthropic credential,
// and an Anthropic-native request carries both (SetAnthropicAuthHeaders). An
// operator overriding just one of them must not leave the request presenting two
// different principals depending on which header the upstream reads: overriding
// Authorization moves x-api-key with it.
func TestClaudeSettingsWriteHeadersKeepsAnthropicCredentialPairInSyncFromAuthorization(t *testing.T) {
	settings := &ClaudeSettings{
		HeadersSettings: map[string]map[string][]string{
			"claude-opus-5": {
				"Authorization": {"Bearer operator-key"},
			},
		},
	}

	headers := http.Header{}
	headers.Set("x-api-key", "channel-key")
	headers.Set("Authorization", "Bearer channel-key")

	settings.WriteHeaders("claude-opus-5", &headers)

	if got := headers.Get("Authorization"); got != "Bearer operator-key" {
		t.Fatalf("expected Authorization to carry the operator credential, got %q", got)
	}
	if got := headers.Get("x-api-key"); got != "operator-key" {
		t.Fatalf("expected x-api-key to follow Authorization to the same credential, got %q", got)
	}
}

// The reverse direction: overriding the legacy x-api-key form rewrites
// Authorization to Bearer of the same token.
func TestClaudeSettingsWriteHeadersKeepsAnthropicCredentialPairInSyncFromApiKey(t *testing.T) {
	settings := &ClaudeSettings{
		HeadersSettings: map[string]map[string][]string{
			"claude-opus-5": {
				"x-api-key": {"operator-key"},
			},
		},
	}

	headers := http.Header{}
	headers.Set("x-api-key", "channel-key")
	headers.Set("Authorization", "Bearer channel-key")

	settings.WriteHeaders("claude-opus-5", &headers)

	if got := headers.Get("x-api-key"); got != "operator-key" {
		t.Fatalf("expected x-api-key to carry the operator credential, got %q", got)
	}
	if got := headers.Get("Authorization"); got != "Bearer operator-key" {
		t.Fatalf("expected Authorization to follow x-api-key to the same credential, got %q", got)
	}
}

// A request that carries only one form is not given the other: the fix keeps an
// existing pair coherent, it does not add a header the request did not have.
func TestClaudeSettingsWriteHeadersDoesNotIntroduceMissingCredentialSibling(t *testing.T) {
	settings := &ClaudeSettings{
		HeadersSettings: map[string]map[string][]string{
			"claude-opus-5": {
				"Authorization": {"Bearer operator-key"},
			},
		},
	}

	headers := http.Header{}
	headers.Set("Authorization", "Bearer channel-key")

	settings.WriteHeaders("claude-opus-5", &headers)

	if got := headers.Get("Authorization"); got != "Bearer operator-key" {
		t.Fatalf("expected Authorization to carry the operator credential, got %q", got)
	}
	if values := headers.Values("x-api-key"); len(values) != 0 {
		t.Fatalf("expected no x-api-key to be introduced, got %v", values)
	}
}

// The credential is matched case-insensitively across both forms, so a config
// written with a lower-case key still keeps the pair in sync.
func TestClaudeSettingsWriteHeadersSyncsAnthropicCredentialPairCaseInsensitively(t *testing.T) {
	settings := &ClaudeSettings{
		HeadersSettings: map[string]map[string][]string{
			"claude-opus-5": {
				"authorization": {"Bearer operator-key"},
			},
		},
	}

	headers := http.Header{}
	headers.Set("x-api-key", "channel-key")
	headers.Set("Authorization", "Bearer channel-key")

	settings.WriteHeaders("claude-opus-5", &headers)

	if got := headers.Get("x-api-key"); got != "operator-key" {
		t.Fatalf("expected lower-case Authorization config to sync x-api-key, got %q", got)
	}
}

// Authorization is the canonical form, so when both are configured the pair
// resolves to it and the outcome does not depend on map iteration order.
func TestClaudeSettingsWriteHeadersPrefersAuthorizationWhenBothFormsConfigured(t *testing.T) {
	settings := &ClaudeSettings{
		HeadersSettings: map[string]map[string][]string{
			"claude-opus-5": {
				"Authorization": {"Bearer canonical-key"},
				"x-api-key":     {"legacy-key"},
			},
		},
	}

	for i := 0; i < 50; i++ {
		headers := http.Header{}
		headers.Set("x-api-key", "channel-key")
		headers.Set("Authorization", "Bearer channel-key")

		settings.WriteHeaders("claude-opus-5", &headers)

		if got := headers.Get("Authorization"); got != "Bearer canonical-key" {
			t.Fatalf("iteration %d: expected Authorization to win, got %q", i, got)
		}
		if got := headers.Get("x-api-key"); got != "canonical-key" {
			t.Fatalf("iteration %d: expected x-api-key to match Authorization, got %q", i, got)
		}
	}
}

// Other credential headers (proxy-authorization and friends) are not part of the
// Anthropic pair and keep the plain replace behaviour.
func TestClaudeSettingsWriteHeadersReplacesNonAnthropicCredentialWithoutSiblings(t *testing.T) {
	settings := &ClaudeSettings{
		HeadersSettings: map[string]map[string][]string{
			"claude-opus-5": {
				"Proxy-Authorization": {"Bearer proxy-key"},
			},
		},
	}

	headers := http.Header{}
	headers.Set("Proxy-Authorization", "Bearer channel-key")
	headers.Set("Authorization", "Bearer channel-key")

	settings.WriteHeaders("claude-opus-5", &headers)

	if got := headers.Get("Proxy-Authorization"); got != "Bearer proxy-key" {
		t.Fatalf("expected proxy credential to be replaced, got %q", got)
	}
	if got := headers.Get("Authorization"); got != "Bearer channel-key" {
		t.Fatalf("expected Authorization to be untouched, got %q", got)
	}
}
