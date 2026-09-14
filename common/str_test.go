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
