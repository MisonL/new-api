package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
)

func TestDeriveCustomOAuthBaseURLFromRequestUsesTrustedForwardedHeaders(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	previousEnabled := common.OptionMap[customOAuthTrustForwardedHeadersOption]
	previousCIDRs := common.OptionMap[customOAuthTrustedProxyCIDRsOption]
	common.OptionMap[customOAuthTrustForwardedHeadersOption] = "true"
	common.OptionMap[customOAuthTrustedProxyCIDRsOption] = "127.0.0.1/32"
	common.OptionMapRWMutex.Unlock()
	defer func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap[customOAuthTrustForwardedHeadersOption] = previousEnabled
		common.OptionMap[customOAuthTrustedProxyCIDRsOption] = previousCIDRs
		common.OptionMapRWMutex.Unlock()
	}()

	req := httptest.NewRequest("GET", "http://internal:3000/api/status", nil)
	req.Host = "internal:3000"
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("X-Forwarded-Host", "public.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Prefix", "/gateway")

	got := deriveCustomOAuthBaseURLFromRequest(req)
	want := "https://public.example.com/gateway"
	if got != want {
		t.Fatalf("deriveCustomOAuthBaseURLFromRequest() = %q, want %q", got, want)
	}
}

func TestDeriveCustomOAuthBaseURLFromRequestIgnoresUntrustedForwardedHeaders(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	previousEnabled := common.OptionMap[customOAuthTrustForwardedHeadersOption]
	previousCIDRs := common.OptionMap[customOAuthTrustedProxyCIDRsOption]
	common.OptionMap[customOAuthTrustForwardedHeadersOption] = "true"
	common.OptionMap[customOAuthTrustedProxyCIDRsOption] = "10.0.0.0/8"
	common.OptionMapRWMutex.Unlock()
	defer func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap[customOAuthTrustForwardedHeadersOption] = previousEnabled
		common.OptionMap[customOAuthTrustedProxyCIDRsOption] = previousCIDRs
		common.OptionMapRWMutex.Unlock()
	}()

	req := httptest.NewRequest("GET", "http://internal:3000/api/status", nil)
	req.Host = "internal:3000"
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("X-Forwarded-Host", "public.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")

	got := deriveCustomOAuthBaseURLFromRequest(req)
	want := "http://internal:3000"
	if got != want {
		t.Fatalf("deriveCustomOAuthBaseURLFromRequest() = %q, want %q", got, want)
	}
}
