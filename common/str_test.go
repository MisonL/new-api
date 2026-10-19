package common

import (
	"strings"
	"testing"
)

func TestMaskSensitiveInfoRedactsStandaloneAndAuthTokens(t *testing.T) {
	input := "standalone sk-live-secret123 Bearer bearer-secret-123 Token token-secret-123"
	got := MaskSensitiveInfo(input)

	for _, secret := range []string{
		"sk-live-secret123",
		"bearer-secret-123",
		"token-secret-123",
	} {
		if strings.Contains(got, secret) {
			t.Fatalf("expected %q to be redacted in %q", secret, got)
		}
	}
	if !strings.Contains(got, "standalone ***") {
		t.Fatalf("expected standalone key marker, got %q", got)
	}
	if !strings.Contains(got, "Bearer ***") || !strings.Contains(got, "Token ***") {
		t.Fatalf("expected auth scheme markers, got %q", got)
	}
}

func TestMaskSensitiveInfoPreservesOrdinaryTokenProse(t *testing.T) {
	for _, input := range []string{
		"token has no model access; bearer is required",
		"token count 12",
		"token usage is 10",
		"Bearer token",
	} {
		if got := MaskSensitiveInfo(input); got != input {
			t.Fatalf("expected ordinary token prose to remain unchanged, got %q", got)
		}
	}
}

func TestMaskSensitiveInfoPreservesResponsesEventNames(t *testing.T) {
	input := "response.completed response.done response.error"
	if got := MaskSensitiveInfo(input); got != input {
		t.Fatalf("expected Responses event names to remain unchanged, got %q", got)
	}
}

func TestMaskSensitiveInfoRedactsShortAuthSchemeValues(t *testing.T) {
	input := "Basic x Bearer y Token z"
	got := MaskSensitiveInfo(input)

	if got != "Basic *** Bearer *** Token ***" {
		t.Fatalf("expected short auth values to be redacted, got %q", got)
	}
}

func TestMaskSensitiveInfoDoesNotMaskEmbeddedSkText(t *testing.T) {
	input := "disk-sk-test disk-sk_test disk-rk-test disk-sess_test should remain diagnostic identifiers"
	if got := MaskSensitiveInfo(input); got != input {
		t.Fatalf("expected embedded sk text to remain unchanged, got %q", got)
	}
}

func TestMaskSensitiveInfoRedactsAPIKeyVariantsAndUnderscoreKeys(t *testing.T) {
	input := `Api-Key: api-secret-1 api-key=api-secret-2 api_key:"api-secret-3" sk_api-secret rk_api-secret sess_api-secret`
	got := MaskSensitiveInfo(input)

	for _, secret := range []string{
		"api-secret-1",
		"api-secret-2",
		"api-secret-3",
		"sk_api-secret",
		"rk_api-secret",
		"sess_api-secret",
	} {
		if strings.Contains(got, secret) {
			t.Fatalf("expected %q to be redacted in %q", secret, got)
		}
	}
	if !strings.Contains(got, "Api-Key: ***") || !strings.Contains(got, "api-key=***") || !strings.Contains(got, "api_key:\"***\"") {
		t.Fatalf("expected API key labels and quoting to be preserved, got %q", got)
	}
}

func TestMaskSensitiveInfoRedactsIPLiteralHostsAndUnschemedQuerySecrets(t *testing.T) {
	input := "https://192.168.1.1:8443/path https://[2001:db8::1]:443/path api.example.com?token=query-secret 2001:db8::2"
	got := MaskSensitiveInfo(input)

	for _, secret := range []string{"192.168.1.1", "2001:db8::1", "api.example.com", "query-secret", "2001:db8::2"} {
		if strings.Contains(got, secret) {
			t.Fatalf("expected %q to be redacted in %q", secret, got)
		}
	}
	if !strings.Contains(got, "https://***:8443/") || !strings.Contains(got, "https://[***]:443/") {
		t.Fatalf("expected masked IP URL hosts with ports preserved, got %q", got)
	}
}

func TestIsAutoGroupIsCaseInsensitiveAndTrimmed(t *testing.T) {
	for _, value := range []string{"auto", "AUTO", "Auto", "  aUtO  "} {
		if !IsAutoGroup(value) {
			t.Fatalf("expected %q to be recognized as the reserved auto group", value)
		}
	}
	for _, value := range []string{"", "automatic", "auto-prod", "default"} {
		if IsAutoGroup(value) {
			t.Fatalf("did not expect %q to be recognized as the reserved auto group", value)
		}
	}
}

// DEBUG 日志会打印完整上游 URL。Vertex API-key 模式把渠道密钥放在 query
// (?key=AIza...)，渠道也可能在 userinfo 里带凭据，这两处都不能进日志。
// 其余部分（scheme/host/path）是排障需要的，保持原样。
func TestMaskURLCredentialsKeepsEndpointButDropsSecrets(t *testing.T) {
	cases := []struct {
		name string
		in   string
		leak []string
		want []string
	}{
		{
			name: "vertex key query",
			in:   "https://us-central1-aiplatform.googleapis.com/v1/projects/p/locations/us/models/gemini-2.5-pro:generateContent?key=AIzaSyD-abcdefghijklmnopqrstuvwxyz01234",
			leak: []string{"AIzaSyD-abcdefghijklmnopqrstuvwxyz01234"},
			want: []string{"us-central1-aiplatform.googleapis.com", "/v1/projects/p/locations/us/models/gemini-2.5-pro:generateContent", "key=***"},
		},
		{
			name: "userinfo and multiple query values",
			in:   "https://user:pass@example.com/v1/chat?api_key=secret-one&model=gpt-5.6-sol",
			leak: []string{"pass", "secret-one"},
			want: []string{"example.com", "/v1/chat", "api_key=***", "model=***"},
		},
		{
			name: "no query unchanged",
			in:   "https://api.openai.com/v1/chat/completions",
			want: []string{"https://api.openai.com/v1/chat/completions"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MaskURLCredentials(tc.in)
			for _, leak := range tc.leak {
				if strings.Contains(got, leak) {
					t.Fatalf("expected %q to be masked, got %q", leak, got)
				}
			}
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("expected %q to remain in %q", want, got)
				}
			}
		})
	}
}
