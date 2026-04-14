package model

import "testing"

func TestValidateCASProviderRequiresServerURL(t *testing.T) {
	provider := &CustomOAuthProvider{
		Name: "CAS SSO",
		Slug: "cas-sso",
		Kind: CustomOAuthProviderKindCAS,
	}

	if err := validateCustomOAuthProvider(provider); err == nil {
		t.Fatal("expected cas provider without cas_server_url to fail")
	}
}

func TestCASProviderSupportsBrowserLoginWhenConfigured(t *testing.T) {
	provider := &CustomOAuthProvider{
		Kind:         CustomOAuthProviderKindCAS,
		Enabled:      true,
		CASServerURL: "https://cas.example.com/cas",
	}

	if !provider.SupportsBrowserLogin() {
		t.Fatal("expected cas provider to support browser login when enabled and configured")
	}
}

func TestValidateCASProviderDefaultsIdentityFieldMappings(t *testing.T) {
	provider := &CustomOAuthProvider{
		Name:         "CAS SSO",
		Slug:         "cas-sso",
		Kind:         CustomOAuthProviderKindCAS,
		CASServerURL: "https://cas.example.com/cas",
	}

	if err := validateCustomOAuthProvider(provider); err != nil {
		t.Fatalf("expected cas provider validation to succeed, got %v", err)
	}
	if provider.UserIdField != "authenticationSuccess.user" {
		t.Fatalf("expected default cas user_id_field, got %q", provider.UserIdField)
	}
	if provider.UsernameField != "preferred_username" {
		t.Fatalf("expected default username_field to remain preferred_username, got %q", provider.UsernameField)
	}
}

func TestCASProviderBuildsDerivedURLs(t *testing.T) {
	provider := &CustomOAuthProvider{
		CASServerURL: "https://cas.example.com/cas/login",
	}

	if got := provider.GetCASLoginURL(); got != "https://cas.example.com/cas/login" {
		t.Fatalf("unexpected cas login url: %q", got)
	}
	if got := provider.GetCASValidateURL(); got != "https://cas.example.com/cas/serviceValidate" {
		t.Fatalf("unexpected cas validate url: %q", got)
	}
	if got := provider.GetCASServiceURL("https://newapi.example.com/oauth/cas-sso?state=abc"); got != "https://newapi.example.com/oauth/cas-sso?state=abc" {
		t.Fatalf("unexpected cas service url: %q", got)
	}
}

func TestCASProviderServiceURLPreservesConfiguredURLAndInjectsState(t *testing.T) {
	provider := &CustomOAuthProvider{
		CASServerURL: "https://cas.example.com/cas",
		ServiceURL:   "https://sso.example.com/custom/callback?from=cas",
	}

	got := provider.GetCASServiceURL("https://newapi.example.com/oauth/cas-sso?state=abc123")
	expected := "https://sso.example.com/custom/callback?from=cas&state=abc123"
	if got != expected {
		t.Fatalf("expected configured service url with merged state %q, got %q", expected, got)
	}
}
