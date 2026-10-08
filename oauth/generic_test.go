package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
)

// The token endpoint is configured per-provider by whoever administers the
// directory, not by this gateway. Its response must be read with an explicit
// byte limit so a hostile or broken endpoint cannot grow the process heap
// without bound; the failure must surface as an error rather than a truncated
// token.
func TestGenericOAuthExchangeTokenRejectsOversizedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, maxGenericOAuthResponseBodyBytes+1))
	}))
	defer srv.Close()

	provider := NewGenericOAuthProvider(&model.CustomOAuthProvider{
		Name:          "Generic",
		Slug:          "generic-oversize-test",
		TokenEndpoint: srv.URL,
		UserIdField:   "sub",
	})

	token, err := provider.ExchangeToken(context.Background(), "code", nil)
	if token != nil {
		t.Fatalf("expected no token, got %+v", token)
	}
	if err == nil {
		t.Fatal("expected an oversized-body error, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected an explicit oversize error, got %q", err.Error())
	}
}

// Control case: a token response within the limit still parses, so the bound
// cannot break a legitimate exchange.
func TestGenericOAuthExchangeTokenAcceptsBodyWithinLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"ok","token_type":"Bearer"}`))
	}))
	defer srv.Close()

	provider := NewGenericOAuthProvider(&model.CustomOAuthProvider{
		Name:          "Generic",
		Slug:          "generic-within-limit-test",
		TokenEndpoint: srv.URL,
	})

	token, err := provider.ExchangeToken(context.Background(), "code", nil)
	if err != nil {
		t.Fatalf("expected exchange to succeed, got %v", err)
	}
	if token.AccessToken != "ok" {
		t.Fatalf("expected access token ok, got %q", token.AccessToken)
	}
}

// Same bound on the userinfo path: the provider's response is read into memory
// for JSON parsing and access-policy evaluation, so it needs the same ceiling.
func TestGenericOAuthGetUserInfoRejectsOversizedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, maxGenericOAuthResponseBodyBytes+1))
	}))
	defer srv.Close()

	provider := NewGenericOAuthProvider(&model.CustomOAuthProvider{
		Name:             "Generic",
		Slug:             "generic-userinfo-oversize-test",
		UserInfoEndpoint: srv.URL,
		UserIdField:      "sub",
	})

	user, err := provider.GetUserInfo(context.Background(), &OAuthToken{
		AccessToken: "token",
		TokenType:   "Bearer",
	})
	if user != nil {
		t.Fatalf("expected no user, got %+v", user)
	}
	if err == nil {
		t.Fatal("expected an oversized-body error, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected an explicit oversize error, got %q", err.Error())
	}
}
