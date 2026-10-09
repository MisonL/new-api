package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/require"
)

func TestSSRFGuardedTransportDialsValidatedHostnameAddress(t *testing.T) {
	fetchSetting := system_setting.GetFetchSetting()
	previousSetting := *fetchSetting
	t.Cleanup(func() { *fetchSetting = previousSetting })
	*fetchSetting = system_setting.FetchSetting{
		EnableSSRFProtection:   true,
		AllowPrivateIp:         true,
		DomainFilterMode:       false,
		IpFilterMode:           false,
		ApplyIPFilterForDomain: true,
	}

	var dialAddress string
	base := &http.Transport{
		DialContext: func(_ context.Context, _ string, address string) (net.Conn, error) {
			dialAddress = address
			return nil, errors.New("stop dial")
		},
	}
	client := &http.Client{Transport: withSSRFProtectionTransport(base, true)}
	req, err := http.NewRequest(http.MethodGet, "http://localhost:12345/", nil)
	require.NoError(t, err)
	req = req.WithContext(common.WithSSRFValidation(req.Context()))

	_, err = client.Do(req)
	require.Error(t, err)
	require.NotEmpty(t, dialAddress)
	require.NotContains(t, strings.ToLower(dialAddress), "localhost")
}

func TestSSRFGuardedTransportSkipsFetchPolicyForUnmarkedRelayRequest(t *testing.T) {
	fetchSetting := system_setting.GetFetchSetting()
	previousSetting := *fetchSetting
	t.Cleanup(func() { *fetchSetting = previousSetting })
	*fetchSetting = system_setting.FetchSetting{
		EnableSSRFProtection: true,
		AllowedPorts:         []string{"443"},
	}

	var dialAddress string
	base := &http.Transport{
		DialContext: func(_ context.Context, _ string, address string) (net.Conn, error) {
			dialAddress = address
			return nil, errors.New("stop dial")
		},
	}
	client := &http.Client{Transport: withSSRFProtectionTransport(base, true)}
	req, err := http.NewRequest(http.MethodGet, "http://localhost:12345/", nil)
	require.NoError(t, err)

	_, err = client.Do(req)
	require.Error(t, err)
	require.Equal(t, "localhost:12345", dialAddress)
}

func TestSSRFGuardedTransportPreservesHostnameWhenIPPinningDisabled(t *testing.T) {
	fetchSetting := system_setting.GetFetchSetting()
	previousSetting := *fetchSetting
	t.Cleanup(func() { *fetchSetting = previousSetting })
	*fetchSetting = system_setting.FetchSetting{
		EnableSSRFProtection:   true,
		AllowPrivateIp:         true,
		DomainFilterMode:       false,
		IpFilterMode:           false,
		ApplyIPFilterForDomain: false,
	}

	var dialAddress string
	base := &http.Transport{
		DialContext: func(_ context.Context, _ string, address string) (net.Conn, error) {
			dialAddress = address
			return nil, errors.New("stop dial")
		},
	}
	client := &http.Client{Transport: withSSRFProtectionTransport(base, false)}
	req, err := http.NewRequest(http.MethodGet, "http://localhost:12345/", nil)
	require.NoError(t, err)
	req = req.WithContext(common.WithSSRFValidation(req.Context()))

	_, err = client.Do(req)
	require.Error(t, err)
	require.Equal(t, "localhost:12345", dialAddress)
}

func TestSSRFGuardedTransportRejectsSOCKSRemoteDNSPrivateResolutionWhenDomainIPFilterDisabled(t *testing.T) {
	fetchSetting := system_setting.GetFetchSetting()
	previousSetting := *fetchSetting
	t.Cleanup(func() { *fetchSetting = previousSetting })
	*fetchSetting = system_setting.FetchSetting{
		EnableSSRFProtection:   true,
		ApplyIPFilterForDomain: false,
		AllowedPorts:           []string{"12345"},
	}

	var dialAddress string
	base := &http.Transport{
		DialContext: func(_ context.Context, _ string, address string) (net.Conn, error) {
			dialAddress = address
			return nil, errors.New("stop dial")
		},
	}
	client := &http.Client{Transport: withSSRFProtectionTransport(base, true)}
	req, err := http.NewRequest(http.MethodGet, "http://localhost:12345/", nil)
	require.NoError(t, err)
	req = req.WithContext(common.WithSSRFValidation(req.Context()))

	_, err = client.Do(req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "private IP address not allowed")
	require.Empty(t, dialAddress)
}

// The point of this test is the proxy endpoint, not the target: an HTTP proxy
// is dialed by its own address, so it must not be rewritten to a validated
// target IP. That requires a target the SSRF gate can validate without a
// resolver — an IP literal. Using the proxy hostname as the request URL (as an
// earlier version did) made the gate fail on DNS resolution before any dial, so
// the assertion could never observe the dial address at all.
func TestSSRFGuardedTransportDoesNotPinHTTPProxyEndpoint(t *testing.T) {
	fetchSetting := system_setting.GetFetchSetting()
	previousSetting := *fetchSetting
	t.Cleanup(func() { *fetchSetting = previousSetting })
	*fetchSetting = system_setting.FetchSetting{
		EnableSSRFProtection:   true,
		AllowPrivateIp:         true,
		DomainFilterMode:       false,
		IpFilterMode:           false,
		ApplyIPFilterForDomain: false,
	}

	proxyURL, err := url.Parse("http://proxy.example:8080")
	require.NoError(t, err)
	var dialAddress string
	base := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
		DialContext: func(_ context.Context, _ string, address string) (net.Conn, error) {
			dialAddress = address
			return nil, errors.New("stop dial")
		},
	}
	client := &http.Client{Transport: withSSRFProtectionTransport(base, true)}
	req, err := http.NewRequest(http.MethodGet, "http://203.0.113.10/resource", nil)
	require.NoError(t, err)
	req = req.WithContext(common.WithSSRFValidation(req.Context()))

	_, err = client.Do(req)
	require.Error(t, err)
	require.Equal(t, "proxy.example:8080", dialAddress)
}

func TestSSRFGuardedTransportUsesValidatedIPThroughHTTPProxy(t *testing.T) {
	fetchSetting := system_setting.GetFetchSetting()
	previousSetting := *fetchSetting
	t.Cleanup(func() { *fetchSetting = previousSetting })

	target := httptest.NewServer(http.NotFoundHandler())
	defer target.Close()
	targetURL, err := url.Parse(target.URL)
	require.NoError(t, err)
	*fetchSetting = system_setting.FetchSetting{
		EnableSSRFProtection:   true,
		AllowPrivateIp:         true,
		AllowedPorts:           []string{targetURL.Port()},
		ApplyIPFilterForDomain: false,
	}

	seenRequestURI := make(chan string, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// For an absolute-form request Go's server intentionally ignores the
		// Host header and derives r.Host from the request-target authority. The
		// request-target is therefore the reliable assertion for proxy routing.
		seenRequestURI <- r.RequestURI
		w.WriteHeader(http.StatusNoContent)
	}))
	defer proxy.Close()
	proxyURL, err := url.Parse(proxy.URL)
	require.NoError(t, err)

	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	client := &http.Client{Transport: withSSRFProtectionTransport(transport, true)}
	req, err := http.NewRequest(http.MethodGet, "http://localhost:"+targetURL.Port()+"/resource", nil)
	require.NoError(t, err)
	req = req.WithContext(common.WithSSRFValidation(req.Context()))
	resp, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.NotContains(t, <-seenRequestURI, "localhost")
}

func TestValidatedHTTPSProxyTargetKeepsTunnelRequestOriginForm(t *testing.T) {
	proxyURL, err := url.Parse("http://proxy.example:8080")
	require.NoError(t, err)
	transport := &http.Transport{}
	req, err := http.NewRequest(http.MethodGet, "https://example.com:443/resource", nil)
	require.NoError(t, err)

	proxyRequest, proxyTransport, err := requestForValidatedHTTPProxyTarget(transport, proxyURL, req, net.ParseIP("203.0.113.10"))
	require.NoError(t, err)
	require.Equal(t, "203.0.113.10:443", proxyRequest.URL.Host)
	require.Empty(t, proxyRequest.URL.Opaque)
	require.Equal(t, "example.com:443", proxyRequest.Host)
	require.NotNil(t, proxyTransport.TLSClientConfig)
	require.Equal(t, "example.com", proxyTransport.TLSClientConfig.ServerName)
}

func TestSOCKSRemoteDNSRedirectCarriesResolutionPolicy(t *testing.T) {
	fetchSetting := system_setting.GetFetchSetting()
	previousSetting := *fetchSetting
	t.Cleanup(func() { *fetchSetting = previousSetting })
	*fetchSetting = system_setting.FetchSetting{
		EnableSSRFProtection:   true,
		AllowPrivateIp:         true,
		AllowedPorts:           []string{"12345"},
		ApplyIPFilterForDomain: false,
	}

	req, err := http.NewRequest(http.MethodGet, "http://localhost:12345/resource", nil)
	require.NoError(t, err)
	req = req.WithContext(common.WithSSRFValidation(req.Context()))
	require.NoError(t, checkRedirectWithRemoteDNS(req, nil))
	require.True(t, common.UsesSSRFRemoteDNS(req.Context()))
}

func TestValidatedHTTPProxyFallsBackToNextValidatedIPAndReplaysBody(t *testing.T) {
	var seenBodies [][]byte
	var seenURIs []string
	handled := make(chan struct{}, 2)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read proxy request body: %v", err)
			return
		}
		seenBodies = append(seenBodies, body)
		seenURIs = append(seenURIs, r.RequestURI)
		handled <- struct{}{}
		if strings.Contains(r.RequestURI, "203.0.113.10") {
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Errorf("proxy server does not support hijacking")
				return
			}
			conn, _, err := hijacker.Hijack()
			if err != nil {
				t.Errorf("hijack proxy request: %v", err)
				return
			}
			_ = conn.Close()
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer proxy.Close()
	proxyURL, err := url.Parse(proxy.URL)
	require.NoError(t, err)
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	body := []byte(`{"prompt":"retry me"}`)
	req, err := http.NewRequest(http.MethodPut, "http://example.test/resource?retry=true", bytes.NewReader(body))
	require.NoError(t, err)

	resp, err := roundTripValidatedHTTPProxyTargets(transport, proxyURL, req, []net.IP{
		net.ParseIP("203.0.113.10"),
		net.ParseIP("203.0.113.11"),
	})
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	<-handled
	<-handled
	require.Len(t, seenBodies, 2)
	require.Equal(t, body, seenBodies[0])
	require.Equal(t, body, seenBodies[1])
	require.Contains(t, seenURIs[0], "retry=true")
	require.Contains(t, seenURIs[1], "retry=true")
}

func TestValidatedHTTPProxyDoesNotReplayNonIdempotentRequest(t *testing.T) {
	requests := 0
	handled := make(chan struct{}, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		handled <- struct{}{}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Errorf("proxy server does not support hijacking")
			return
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			t.Errorf("hijack proxy request: %v", err)
			return
		}
		_ = conn.Close()
	}))
	defer proxy.Close()
	proxyURL, err := url.Parse(proxy.URL)
	require.NoError(t, err)
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	req, err := http.NewRequest(http.MethodPost, "http://example.test/resource", bytes.NewReader([]byte(`{"side_effect":true}`)))
	require.NoError(t, err)

	_, err = roundTripValidatedHTTPProxyTargets(transport, proxyURL, req, []net.IP{
		net.ParseIP("203.0.113.10"),
		net.ParseIP("203.0.113.11"),
	})
	<-handled
	require.Error(t, err)
	require.Equal(t, 1, requests)
}

func TestValidatedHTTPProxyDoesNotReplayNonReusableBody(t *testing.T) {
	requests := 0
	handled := make(chan struct{}, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		handled <- struct{}{}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Errorf("proxy server does not support hijacking")
			return
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			t.Errorf("hijack proxy request: %v", err)
			return
		}
		_ = conn.Close()
	}))
	defer proxy.Close()
	proxyURL, err := url.Parse(proxy.URL)
	require.NoError(t, err)
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	req, err := http.NewRequest(http.MethodPost, "http://example.test/resource", io.NopCloser(strings.NewReader("one-shot")))
	require.NoError(t, err)

	_, err = roundTripValidatedHTTPProxyTargets(transport, proxyURL, req, []net.IP{
		net.ParseIP("203.0.113.10"),
		net.ParseIP("203.0.113.11"),
	})
	<-handled
	require.Error(t, err)
	require.Equal(t, 1, requests)
}

func TestSSRFGuardedTransportReenablesPinningAfterProxyMarker(t *testing.T) {
	fetchSetting := system_setting.GetFetchSetting()
	previousSetting := *fetchSetting
	t.Cleanup(func() { *fetchSetting = previousSetting })
	*fetchSetting = system_setting.FetchSetting{
		EnableSSRFProtection:   true,
		AllowPrivateIp:         true,
		DomainFilterMode:       false,
		IpFilterMode:           false,
		ApplyIPFilterForDomain: true,
	}

	var dialAddress string
	base := &http.Transport{
		DialContext: func(_ context.Context, _ string, address string) (net.Conn, error) {
			dialAddress = address
			return nil, errors.New("stop dial")
		},
	}
	client := &http.Client{Transport: withSSRFProtectionTransport(base, true)}
	req, err := http.NewRequest(http.MethodGet, "http://localhost:12345/", nil)
	require.NoError(t, err)
	ctx := common.WithSSRFValidation(req.Context())
	ctx = common.WithValidatedURLIPSPinningDisabled(ctx)
	req = req.WithContext(ctx)

	_, err = client.Do(req)
	require.Error(t, err)
	require.NotEmpty(t, dialAddress)
	require.NotContains(t, strings.ToLower(dialAddress), "localhost")
}
