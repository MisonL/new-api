package common

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

func TestSSRFProtectionValidateURLAndResolveUsesOneValidatedDNSSnapshot(t *testing.T) {
	previousLookup := lookupIPForSSRF
	t.Cleanup(func() { lookupIPForSSRF = previousLookup })
	lookupCalls := 0
	lookupIPForSSRF = func(_ context.Context, host string) ([]net.IP, error) {
		lookupCalls++
		if host != "example.com" {
			t.Fatalf("unexpected lookup host: %s", host)
		}
		return []net.IP{net.ParseIP("8.8.8.8"), net.ParseIP("1.1.1.1")}, nil
	}

	protection := &SSRFProtection{
		DomainFilterMode:       false,
		IpFilterMode:           false,
		ApplyIPFilterForDomain: true,
	}
	ips, err := protection.ValidateURLAndResolve("https://example.com/path")
	if err != nil {
		t.Fatalf("ValidateURLAndResolve returned error: %v", err)
	}
	if lookupCalls != 1 {
		t.Fatalf("lookup count = %d, want 1", lookupCalls)
	}
	if len(ips) != 2 || ips[0].String() != "8.8.8.8" || ips[1].String() != "1.1.1.1" {
		t.Fatalf("resolved IPs = %v, want [8.8.8.8 1.1.1.1]", ips)
	}
}

func TestSSRFProtectionValidateURLAndResolveRejectsAnyDisallowedResolvedAddress(t *testing.T) {
	previousLookup := lookupIPForSSRF
	t.Cleanup(func() { lookupIPForSSRF = previousLookup })
	lookupIPForSSRF = func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8"), net.ParseIP("127.0.0.1")}, nil
	}

	protection := &SSRFProtection{
		DomainFilterMode:       false,
		IpFilterMode:           false,
		ApplyIPFilterForDomain: true,
	}
	_, err := protection.ValidateURLAndResolve("https://example.com/path")
	if err == nil || !strings.Contains(err.Error(), "private IP address not allowed") {
		t.Fatalf("ValidateURLAndResolve error = %v, want private IP rejection", err)
	}
}

func TestSSRFProtectionValidateURLAndResolveReportsDNSFailure(t *testing.T) {
	previousLookup := lookupIPForSSRF
	t.Cleanup(func() { lookupIPForSSRF = previousLookup })
	lookupIPForSSRF = func(context.Context, string) ([]net.IP, error) { return nil, errors.New("resolver unavailable") }

	protection := &SSRFProtection{DomainFilterMode: false, IpFilterMode: false, ApplyIPFilterForDomain: true}
	_, err := protection.ValidateURLAndResolve("https://example.com/path")
	if err == nil || !strings.Contains(err.Error(), "DNS resolution failed") {
		t.Fatalf("ValidateURLAndResolve error = %v, want DNS failure", err)
	}
}

func TestSSRFProtectionValidateURLAndResolveRejectsInvalidResolvedAddress(t *testing.T) {
	previousLookup := lookupIPForSSRF
	t.Cleanup(func() { lookupIPForSSRF = previousLookup })
	lookupIPForSSRF = func(context.Context, string) ([]net.IP, error) {
		return []net.IP{nil}, nil
	}

	protection := &SSRFProtection{
		DomainFilterMode:       false,
		IpFilterMode:           false,
		ApplyIPFilterForDomain: true,
	}
	_, err := protection.ValidateURLAndResolve("https://example.com/path")
	if err == nil || !strings.Contains(err.Error(), "invalid address") {
		t.Fatalf("ValidateURLAndResolve error = %v, want invalid resolved address rejection", err)
	}
}

func TestSSRFProtectionValidateURLRequiresAuthority(t *testing.T) {
	protection := &SSRFProtection{}

	tests := []struct {
		name string
		url  string
		want string
	}{
		{name: "scheme without authority", url: "http:///path", want: "authority and hostname are required"},
		{name: "empty authority", url: "https://", want: "authority and hostname are required"},
		{name: "port without hostname", url: "http://:80/path", want: "authority and hostname are required"},
		{name: "userinfo without hostname", url: "http://user@:80/path", want: "authority and hostname are required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := protection.ValidateURL(tt.url)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateURL(%q) error = %v, want substring %q", tt.url, err, tt.want)
			}
		})
	}
}

func TestSSRFProtectionValidateURLAcceptsHTTPAuthority(t *testing.T) {
	previousLookup := lookupIPForSSRF
	t.Cleanup(func() { lookupIPForSSRF = previousLookup })
	lookupCalls := 0
	lookupIPForSSRF = func(context.Context, string) ([]net.IP, error) {
		lookupCalls++
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	}

	protection := &SSRFProtection{ApplyIPFilterForDomain: false}
	ips, err := protection.ValidateURLAndResolve("https://example.com/path")
	if err != nil {
		t.Fatalf("ValidateURL with a valid authority returned error: %v", err)
	}
	if lookupCalls != 1 {
		t.Fatalf("lookup count = %d, want 1", lookupCalls)
	}
	if len(ips) != 1 || ips[0].String() != "8.8.8.8" {
		t.Fatalf("resolved IPs = %v, want [8.8.8.8]", ips)
	}
}

func TestSSRFProtectionValidateURLRejectsPrivateAddressWhenDomainIPFilterDisabled(t *testing.T) {
	previousLookup := lookupIPForSSRF
	t.Cleanup(func() { lookupIPForSSRF = previousLookup })
	lookupIPForSSRF = func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}

	protection := &SSRFProtection{ApplyIPFilterForDomain: false}
	_, err := protection.ValidateURLAndResolve("https://example.com/path")
	if err == nil || !strings.Contains(err.Error(), "private IP address not allowed") {
		t.Fatalf("ValidateURL error = %v, want private IP rejection", err)
	}
}

func TestSSRFProtectionValidateURLRejectsOutOfRangePorts(t *testing.T) {
	protection := &SSRFProtection{}
	for _, url := range []string{
		"https://example.com:0/path",
		"https://example.com:65536/path",
	} {
		t.Run(url, func(t *testing.T) {
			err := protection.ValidateURL(url)
			if err == nil || !strings.Contains(err.Error(), "must be 1-65535") {
				t.Fatalf("ValidateURL(%q) error = %v, want out-of-range port error", url, err)
			}
		})
	}
}

func TestSSRFProtectionDomainMatchingCanonicalizesCaseAndTrailingDot(t *testing.T) {
	list := []string{"blocked.example.com.", "*.Example.org."}

	if !isDomainListed("BLOCKED.EXAMPLE.COM.", list) {
		t.Fatal("exact domain with a trailing dot should match")
	}
	if !isDomainListed("child.example.org.", list) {
		t.Fatal("wildcard domain with a trailing dot should match")
	}
	if isDomainListed("allowed.example.net.", list) {
		t.Fatal("unlisted domain should not match")
	}
}

func TestSSRFProtectionValidateURLAndResolveCanonicalizesLookupHost(t *testing.T) {
	previousLookup := lookupIPForSSRF
	t.Cleanup(func() { lookupIPForSSRF = previousLookup })
	lookupIPForSSRF = func(_ context.Context, host string) ([]net.IP, error) {
		if host != "example.com" {
			t.Fatalf("lookup host = %q, want canonical host", host)
		}
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	}

	protection := &SSRFProtection{DomainFilterMode: false, IpFilterMode: false, ApplyIPFilterForDomain: true}
	if _, err := protection.ValidateURLAndResolve("https://EXAMPLE.COM./result"); err != nil {
		t.Fatalf("ValidateURLAndResolve returned error: %v", err)
	}
}

func TestSSRFProtectionValidateURLAndResolveContextCancelsDNSLookup(t *testing.T) {
	previousLookup := lookupIPForSSRF
	t.Cleanup(func() { lookupIPForSSRF = previousLookup })
	lookupIPForSSRF = func(ctx context.Context, _ string) ([]net.IP, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	protection := &SSRFProtection{DomainFilterMode: false, IpFilterMode: false, ApplyIPFilterForDomain: true}
	_, err := protection.ValidateURLAndResolveContext(ctx, "https://example.com/path")
	if err == nil || !strings.Contains(err.Error(), "DNS resolution failed") {
		t.Fatalf("ValidateURLAndResolveContext error = %v, want cancelled DNS failure", err)
	}
}
