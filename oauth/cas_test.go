package oauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

func TestCASProviderBuildLoginURLIncludesServiceAndFlags(t *testing.T) {
	provider := NewCASProvider(&model.CustomOAuthProvider{
		Name:         "CAS SSO",
		Slug:         "cas-sso",
		CASServerURL: "https://cas.example.com/cas",
		Renew:        true,
		Gateway:      true,
	})

	loginURL, err := provider.BuildLoginURL("https://newapi.example.com/oauth/cas-sso?state=abc")
	if err != nil {
		t.Fatalf("expected cas login url build to succeed, got %v", err)
	}
	if !strings.HasPrefix(loginURL, "https://cas.example.com/cas/login?") {
		t.Fatalf("unexpected cas login url: %q", loginURL)
	}
	if !strings.Contains(loginURL, "service=https%3A%2F%2Fnewapi.example.com%2Foauth%2Fcas-sso%3Fstate%3Dabc") {
		t.Fatalf("expected service query param in cas login url, got %q", loginURL)
	}
	if !strings.Contains(loginURL, "renew=true") {
		t.Fatalf("expected renew=true in cas login url, got %q", loginURL)
	}
	if !strings.Contains(loginURL, "gateway=true") {
		t.Fatalf("expected gateway=true in cas login url, got %q", loginURL)
	}
}

func TestCASProviderResolveIdentityParsesValidationResponseAndDoesNotElevateInvalidMappings(t *testing.T) {
	handlerErrors := newAsyncHandlerErrors()
	defer handlerErrors.check(t)

	validationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("ticket"); got != "ST-123" {
			handlerErrors.failRequest(w, http.StatusBadRequest, "expected ticket ST-123, got %q", got)
			return
		}
		if got := r.URL.Query().Get("service"); got != "https://newapi.example.com/oauth/cas-sso?state=abc" {
			handlerErrors.failRequest(w, http.StatusBadRequest, "unexpected service url %q", got)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas">
  <cas:authenticationSuccess>
    <cas:user>cas-user-1</cas:user>
    <cas:attributes>
      <cas:loginid>cas-user</cas:loginid>
      <cas:userName>CAS User</cas:userName>
      <cas:mailbox>cas-user@example.com</cas:mailbox>
      <cas:group>engineering</cas:group>
      <cas:role>root-role</cas:role>
    </cas:attributes>
  </cas:authenticationSuccess>
</cas:serviceResponse>`))
	}))
	defer validationServer.Close()

	provider := NewCASProvider(&model.CustomOAuthProvider{
		Name:             "CAS SSO",
		Slug:             "cas-sso",
		CASServerURL:     "https://cas.example.com/cas",
		ValidateURL:      validationServer.URL,
		UserIdField:      "authenticationSuccess.user",
		UsernameField:    "authenticationSuccess.attributes.loginid",
		DisplayNameField: "authenticationSuccess.attributes.userName",
		EmailField:       "authenticationSuccess.attributes.mailbox",
		GroupField:       "authenticationSuccess.attributes.group",
		GroupMapping:     `{"engineering":"missing-group"}`,
		RoleField:        "authenticationSuccess.attributes.role",
		RoleMapping:      `{"root-role":"root"}`,
	})

	identity, err := provider.ResolveIdentityFromTicket(
		context.Background(),
		"ST-123",
		"https://newapi.example.com/oauth/cas-sso?state=abc",
	)
	if err != nil {
		t.Fatalf("expected cas identity resolution to succeed, got %v", err)
	}
	if identity.User.ProviderUserID != "cas-user-1" {
		t.Fatalf("unexpected provider user id: %q", identity.User.ProviderUserID)
	}
	if identity.User.Username != "cas-user" || identity.User.DisplayName != "CAS User" || identity.User.Email != "cas-user@example.com" {
		t.Fatalf("unexpected identity payload: %+v", identity.User)
	}
	if identity.Group != "" {
		t.Fatalf("expected invalid group mapping to be ignored, got %q", identity.Group)
	}
	if identity.Role != common.RoleGuestUser {
		t.Fatalf("expected invalid root promotion to be ignored and keep role unset, got %d", identity.Role)
	}
}

func TestCASProviderResolveIdentitySanitizesErrorResponseBody(t *testing.T) {
	validationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"bad_gateway","access_token":"secret-access-token","password":"super-secret-password","details":"` + strings.Repeat("x", 260) + `"}`))
	}))
	defer validationServer.Close()

	provider := NewCASProvider(&model.CustomOAuthProvider{
		Name:         "CAS SSO",
		Slug:         "cas-sso",
		CASServerURL: "https://cas.example.com/cas",
		ValidateURL:  validationServer.URL,
	})

	_, err := provider.ResolveIdentityFromTicket(
		context.Background(),
		"ST-123",
		"https://newapi.example.com/oauth/cas-sso?state=abc",
	)
	if err == nil {
		t.Fatal("expected cas identity resolution to fail on upstream 502")
	}

	var oauthErr *OAuthError
	if !errors.As(err, &oauthErr) {
		t.Fatalf("expected OAuthError, got %T", err)
	}
	if !strings.Contains(oauthErr.RawError, "502 Bad Gateway") {
		t.Fatalf("expected status in raw error, got %q", oauthErr.RawError)
	}
	if strings.Contains(oauthErr.RawError, "secret-access-token") {
		t.Fatalf("expected access token to be redacted, got %q", oauthErr.RawError)
	}
	if strings.Contains(oauthErr.RawError, "super-secret-password") {
		t.Fatalf("expected password to be redacted, got %q", oauthErr.RawError)
	}
	if !strings.Contains(oauthErr.RawError, "[redacted]") {
		t.Fatalf("expected redaction marker in raw error, got %q", oauthErr.RawError)
	}
	if !strings.Contains(oauthErr.RawError, "...(truncated)") {
		t.Fatalf("expected truncation marker in raw error, got %q", oauthErr.RawError)
	}
}

func TestCASProviderResolveIdentityRejectsAuthenticationFailure(t *testing.T) {
	validationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas">
  <cas:authenticationFailure code="INVALID_TICKET">
    service mismatch
  </cas:authenticationFailure>
</cas:serviceResponse>`))
	}))
	defer validationServer.Close()

	provider := NewCASProvider(&model.CustomOAuthProvider{
		Name:         "CAS SSO",
		Slug:         "cas-sso",
		CASServerURL: "https://cas.example.com/cas",
		ValidateURL:  validationServer.URL,
	})

	_, err := provider.ResolveIdentityFromTicket(
		context.Background(),
		"ST-FAIL-1",
		"https://newapi.example.com/oauth/cas-sso?state=abc",
	)
	if err == nil {
		t.Fatal("expected cas authentication failure to be rejected")
	}
	if !strings.Contains(err.Error(), "INVALID_TICKET") || !strings.Contains(err.Error(), "service mismatch") {
		t.Fatalf("expected failure reason to include cas authentication failure details, got %v", err)
	}
}

func TestCASProviderResolveIdentityRejectsMissingExternalUserID(t *testing.T) {
	validationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas">
  <cas:authenticationSuccess>
    <cas:attributes>
      <cas:loginid>cas-user</cas:loginid>
    </cas:attributes>
  </cas:authenticationSuccess>
</cas:serviceResponse>`))
	}))
	defer validationServer.Close()

	provider := NewCASProvider(&model.CustomOAuthProvider{
		Name:         "CAS SSO",
		Slug:         "cas-sso",
		CASServerURL: "https://cas.example.com/cas",
		ValidateURL:  validationServer.URL,
		UserIdField:  "authenticationSuccess.user",
	})

	_, err := provider.ResolveIdentityFromTicket(
		context.Background(),
		"ST-MISSING-USER",
		"https://newapi.example.com/oauth/cas-sso?state=abc",
	)
	if err == nil {
		t.Fatal("expected cas response without external user id to fail")
	}
	if !strings.Contains(err.Error(), "external user id") {
		t.Fatalf("expected missing external user id error, got %v", err)
	}
}
