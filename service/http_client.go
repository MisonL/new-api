package service

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"golang.org/x/net/proxy"
)

var (
	httpClient      *http.Client
	proxyClientLock sync.Mutex
	proxyClients    = make(map[string]*http.Client)
)

// DefaultUserAgent is used when an upstream request has no explicit client identity.
// Keep it stable and browser-shaped without exposing the gateway implementation name.
// Chromium uses this compatibility platform token on both Intel and Apple Silicon
// Macs; the actual ARM signal is carried by UA Client Hints.
const (
	DefaultUserAgent        = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.7922.138 Safari/537.36"
	DefaultUserAgentArch    = `"arm"`
	DefaultUserAgentBitness = `"64"`
)

// ApplyDefaultUserAgent preserves every explicit User-Agent, including an explicitly
// empty header, and only fills the value that net/http would otherwise synthesize.
func ApplyDefaultUserAgent(req *http.Request) {
	if req == nil {
		return
	}
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	ApplyDefaultUserAgentHeader(req.Header)
}

// ApplyDefaultUserAgentHeader applies DefaultUserAgent to protocol handshakes
// that are represented by an http.Header instead of an http.Request.
func ApplyDefaultUserAgentHeader(headers http.Header) {
	if headers == nil {
		return
	}
	if values, exists := headers["User-Agent"]; exists {
		for name := range headers {
			if name != "User-Agent" && strings.EqualFold(name, "User-Agent") {
				delete(headers, name)
			}
		}
		headers["User-Agent"] = values
		return
	}
	for name, values := range headers {
		if !strings.EqualFold(name, "User-Agent") {
			continue
		}
		delete(headers, name)
		headers["User-Agent"] = values
		return
	}
	headers.Set("User-Agent", DefaultUserAgent)
	// On Apple Silicon Chrome, ARM is exposed as the high-entropy UA Client Hint
	// value "arm", not as "arm64" in the traditional User-Agent string.
	headers.Set("Sec-CH-UA-Arch", DefaultUserAgentArch)
	headers.Set("Sec-CH-UA-Bitness", DefaultUserAgentBitness)
}

type defaultUserAgentTransport struct {
	base http.RoundTripper
}

func (t defaultUserAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("nil HTTP request")
	}
	if _, exists := req.Header["User-Agent"]; exists {
		return t.base.RoundTrip(req)
	}
	requestCopy := req.Clone(req.Context())
	ApplyDefaultUserAgent(requestCopy)
	return t.base.RoundTrip(requestCopy)
}

func (t defaultUserAgentTransport) CloseIdleConnections() {
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func withDefaultUserAgentTransport(transport http.RoundTripper) http.RoundTripper {
	if transport == nil {
		transport = http.DefaultTransport
	}
	return defaultUserAgentTransport{base: transport}
}

// ssrfGuardedTransport validates only requests that explicitly opt into the
// user-controlled fetch URL boundary. Direct connections and SOCKS requests pin
// to the validated addresses; HTTP proxies receive a validated IP target.
type ssrfGuardedTransport struct {
	base            *http.Transport
	pinValidatedIPs bool
}

func (t ssrfGuardedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil {
		return nil, fmt.Errorf("nil HTTP request or URL")
	}
	if !common.RequiresSSRFValidation(req.Context()) {
		return t.base.RoundTrip(req)
	}
	fetchSetting := system_setting.GetFetchSettingSnapshot()
	if !fetchSetting.EnableSSRFProtection {
		return t.base.RoundTrip(req)
	}
	validatedIPs, err := common.ValidateURLWithFetchSettingAndResolveContextOptions(
		req.Context(),
		req.URL.String(),
		fetchSetting.EnableSSRFProtection,
		fetchSetting.AllowPrivateIp,
		fetchSetting.DomainFilterMode,
		fetchSetting.IpFilterMode,
		fetchSetting.DomainList,
		fetchSetting.IpList,
		fetchSetting.AllowedPorts,
		fetchSetting.ApplyIPFilterForDomain,
		true,
	)
	if err != nil {
		return nil, fmt.Errorf("SSRF destination validation failed: %w", err)
	}
	proxyURL, proxyErr := proxyURLForRequest(t.base, req)
	if proxyErr != nil {
		return nil, fmt.Errorf("SSRF proxy selection failed: %w", proxyErr)
	}
	if proxyURL != nil && len(validatedIPs) > 0 {
		return roundTripValidatedHTTPProxyTargets(t.base, proxyURL, req, validatedIPs)
	}
	shouldPin := t.pinValidatedIPs
	if !shouldPin || len(validatedIPs) == 0 || net.ParseIP(req.URL.Hostname()) != nil {
		return t.base.RoundTrip(req)
	}
	requestCopy := req.Clone(common.WithValidatedURLIPs(req.Context(), req.URL.Hostname(), validatedIPs))
	return t.base.RoundTrip(requestCopy)
}

func proxyURLForRequest(transport *http.Transport, req *http.Request) (*url.URL, error) {
	if transport == nil || transport.Proxy == nil {
		return nil, nil
	}
	return transport.Proxy(req)
}

// requestForValidatedHTTPProxyTarget makes an HTTP proxy connect to the
// address that passed SSRF validation instead of resolving the original
// hostname on the proxy host. Host and TLS SNI remain the original hostname
// so virtual hosting and certificate verification continue to work.
func requestForValidatedHTTPProxyTarget(transport *http.Transport, proxyURL *url.URL, req *http.Request, ip net.IP) (*http.Request, *http.Transport, error) {
	if transport == nil || proxyURL == nil || req == nil || req.URL == nil || ip == nil {
		return nil, nil, fmt.Errorf("invalid validated proxy request")
	}
	requestCopy := req.Clone(common.WithValidatedURLIPSPinningDisabled(req.Context()))
	originalHost := req.URL.Host
	originalHostname := req.URL.Hostname()
	port := req.URL.Port()
	if port == "" {
		if req.URL.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	requestCopy.URL.Host = net.JoinHostPort(ip.String(), port)
	// net/http uses Request.Host for both the Host header and the authority in
	// an absolute-form HTTP proxy request. Keep the original Host for virtual
	// host routing, but put the validated address in URL.Opaque so the proxy
	// request line cannot switch back to the unvalidated hostname. HTTPS proxy
	// requests use CONNECT and must retain an empty Opaque value so the request
	// inside the tunnel remains origin-form.
	if req.URL.Scheme == "http" {
		proxyTargetPath := requestCopy.URL.EscapedPath()
		if proxyTargetPath == "" {
			proxyTargetPath = "/"
		}
		if requestCopy.URL.ForceQuery || requestCopy.URL.RawQuery != "" {
			proxyTargetPath += "?" + requestCopy.URL.RawQuery
		}
		requestCopy.URL.Opaque = "//" + requestCopy.URL.Host + proxyTargetPath
	}
	if requestCopy.Host == "" {
		requestCopy.Host = originalHost
	}
	transportCopy := transport.Clone()
	transportCopy.Proxy = http.ProxyURL(proxyURL)
	// This transport is created for one validated target only and is never put
	// in the shared client cache. Disable keep-alives so a response that is not
	// fully consumed cannot leave an idle connection owned by an unreachable
	// transport clone.
	transportCopy.DisableKeepAlives = true
	if req.URL.Scheme == "https" {
		var tlsConfig *tls.Config
		if transportCopy.TLSClientConfig != nil {
			tlsConfig = transportCopy.TLSClientConfig.Clone()
		} else {
			tlsConfig = &tls.Config{}
		}
		tlsConfig.ServerName = originalHostname
		transportCopy.TLSClientConfig = tlsConfig
	}
	return requestCopy, transportCopy, nil
}

func roundTripValidatedHTTPProxyTargets(transport *http.Transport, proxyURL *url.URL, req *http.Request, ips []net.IP) (*http.Response, error) {
	if transport == nil || proxyURL == nil || req == nil || req.URL == nil {
		return nil, fmt.Errorf("invalid validated proxy request")
	}
	replayableBody := req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
	replayableRequest := isIdempotentHTTPMethod(req.Method) && replayableBody
	var lastErr error
	attempts := 0
	for _, ip := range ips {
		if ip == nil {
			continue
		}
		if attempts > 0 && !replayableRequest {
			break
		}
		attemptReq := req
		if attempts > 0 {
			attemptReq = req.Clone(req.Context())
			if req.Body != nil && req.Body != http.NoBody {
				body, err := req.GetBody()
				if err != nil {
					return nil, fmt.Errorf("replay request body for validated proxy fallback: %w", err)
				}
				attemptReq.Body = body
			}
		}
		proxyRequest, proxyTransport, err := requestForValidatedHTTPProxyTarget(transport, proxyURL, attemptReq, ip)
		if err != nil {
			return nil, err
		}
		attempts++
		resp, err := proxyTransport.RoundTrip(proxyRequest)
		if err == nil {
			proxyTransport.CloseIdleConnections()
			return resp, nil
		}
		proxyTransport.CloseIdleConnections()
		lastErr = err
		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}
	}
	if attempts == 0 {
		return nil, fmt.Errorf("validated proxy request has no usable target address")
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("validated proxy request failed without an error")
}

func isIdempotentHTTPMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPut, http.MethodDelete, http.MethodTrace:
		return true
	default:
		return false
	}
}

func (t ssrfGuardedTransport) CloseIdleConnections() {
	if t.base != nil {
		t.base.CloseIdleConnections()
	}
}

func withSSRFProtectionTransport(transport *http.Transport, pinValidatedIPs bool) http.RoundTripper {
	if transport == nil {
		transport = http.DefaultTransport.(*http.Transport).Clone()
	}
	originalDialContext := transport.DialContext
	if originalDialContext == nil {
		dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		originalDialContext = dialer.DialContext
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if !pinValidatedIPs || common.ValidatedURLIPSPinningDisabled(ctx) {
			return originalDialContext(ctx, network, address)
		}
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return originalDialContext(ctx, network, address)
		}
		validatedIPs := common.ValidatedURLIPs(ctx, host)
		if len(validatedIPs) == 0 {
			return originalDialContext(ctx, network, address)
		}
		var lastErr error
		for _, ip := range validatedIPs {
			if ip == nil {
				continue
			}
			conn, dialErr := originalDialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			lastErr = dialErr
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
		if lastErr != nil {
			return nil, lastErr
		}
		return originalDialContext(ctx, network, address)
	}
	return ssrfGuardedTransport{base: transport, pinValidatedIPs: pinValidatedIPs}
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	return checkRedirectWithRemoteDNSMode(req, via, false)
}

// checkRedirectWithRemoteDNS is used by SOCKS clients. Marking the redirect
// request before the client appends it to the redirect chain preserves the
// transport policy for the next hop; each destination is still resolved and
// validated before the connection is pinned.
func checkRedirectWithRemoteDNS(req *http.Request, via []*http.Request) error {
	return checkRedirectWithRemoteDNSMode(req, via, true)
}

func checkRedirectWithRemoteDNSMode(req *http.Request, via []*http.Request, remoteDNS bool) error {
	if !common.RequiresSSRFValidation(req.Context()) {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		return nil
	}
	fetchSetting := system_setting.GetFetchSettingSnapshot()
	if remoteDNS && !common.UsesSSRFRemoteDNS(req.Context()) {
		// Client.Do keeps this request pointer and copies its context into the
		// next redirect request after CheckRedirect returns. Mutating the request
		// value here therefore carries the remote-DNS decision across the whole
		// redirect chain without weakening the unmarked relay path.
		*req = *req.WithContext(common.WithSSRFRemoteDNS(req.Context()))
	}
	urlStr := req.URL.String()
	if _, err := common.ValidateURLWithFetchSettingAndResolveContextOptions(req.Context(), urlStr, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain, true); err != nil {
		return fmt.Errorf("redirect to %s blocked: %v", urlStr, err)
	}
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}
	return nil
}

func InitHttpClient() {
	transport := &http.Transport{
		MaxIdleConns:        common.RelayMaxIdleConns,
		MaxIdleConnsPerHost: common.RelayMaxIdleConnsPerHost,
		ForceAttemptHTTP2:   true,
		Proxy:               http.ProxyFromEnvironment, // Support HTTP_PROXY, HTTPS_PROXY, NO_PROXY env vars
	}
	if common.TLSInsecureSkipVerify {
		transport.TLSClientConfig = common.InsecureTLSConfig
	}

	if common.RelayTimeout == 0 {
		httpClient = &http.Client{
			Transport:     withDefaultUserAgentTransport(withSSRFProtectionTransport(transport, true)),
			CheckRedirect: checkRedirect,
		}
	} else {
		httpClient = &http.Client{
			Transport:     withDefaultUserAgentTransport(withSSRFProtectionTransport(transport, true)),
			Timeout:       time.Duration(common.RelayTimeout) * time.Second,
			CheckRedirect: checkRedirect,
		}
	}
}

func GetHttpClient() *http.Client {
	return httpClient
}

// GetHttpClientWithProxy returns the default client or a proxy-enabled one when proxyURL is provided.
func GetHttpClientWithProxy(proxyURL string) (*http.Client, error) {
	if proxyURL == "" {
		return GetHttpClient(), nil
	}
	return NewProxyHttpClient(proxyURL)
}

// ResetProxyClientCache 清空代理客户端缓存，确保下次使用时重新初始化
func ResetProxyClientCache() {
	proxyClientLock.Lock()
	defer proxyClientLock.Unlock()
	for _, client := range proxyClients {
		if closer, ok := client.Transport.(interface{ CloseIdleConnections() }); ok {
			closer.CloseIdleConnections()
		}
	}
	proxyClients = make(map[string]*http.Client)
}

// NewProxyHttpClient 创建支持代理的 HTTP 客户端
func NewProxyHttpClient(proxyURL string) (*http.Client, error) {
	if proxyURL == "" {
		if client := GetHttpClient(); client != nil {
			return client, nil
		}
		return http.DefaultClient, nil
	}

	proxyClientLock.Lock()
	if client, ok := proxyClients[proxyURL]; ok {
		proxyClientLock.Unlock()
		return client, nil
	}
	proxyClientLock.Unlock()

	parsedURL, err := url.Parse(proxyURL)
	if err != nil {
		return nil, err
	}

	switch parsedURL.Scheme {
	case "http", "https":
		transport := &http.Transport{
			MaxIdleConns:        common.RelayMaxIdleConns,
			MaxIdleConnsPerHost: common.RelayMaxIdleConnsPerHost,
			ForceAttemptHTTP2:   true,
			Proxy:               http.ProxyURL(parsedURL),
		}
		if common.TLSInsecureSkipVerify {
			transport.TLSClientConfig = common.InsecureTLSConfig
		}
		client := &http.Client{
			Transport:     withDefaultUserAgentTransport(withSSRFProtectionTransport(transport, false)),
			CheckRedirect: checkRedirect,
		}
		client.Timeout = time.Duration(common.RelayTimeout) * time.Second
		proxyClientLock.Lock()
		proxyClients[proxyURL] = client
		proxyClientLock.Unlock()
		return client, nil

	case "socks5", "socks5h":
		// 获取认证信息
		var auth *proxy.Auth
		if parsedURL.User != nil {
			auth = &proxy.Auth{
				User:     parsedURL.User.Username(),
				Password: "",
			}
			if password, ok := parsedURL.User.Password(); ok {
				auth.Password = password
			}
		}

		// 创建 SOCKS5 代理拨号器
		// proxy.SOCKS5 使用 tcp 参数，所有 TCP 连接包括 DNS 查询都将通过代理进行。行为与 socks5h 相同
		dialer, err := proxy.SOCKS5("tcp", parsedURL.Host, auth, proxy.Direct)
		if err != nil {
			return nil, err
		}

		transport := &http.Transport{
			MaxIdleConns:        common.RelayMaxIdleConns,
			MaxIdleConnsPerHost: common.RelayMaxIdleConnsPerHost,
			ForceAttemptHTTP2:   true,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				if contextDialer, ok := dialer.(proxy.ContextDialer); ok {
					return contextDialer.DialContext(ctx, network, addr)
				}
				// x/net/proxy dialers supplied by extensions may not implement
				// ContextDialer. Keep cancellation observable in that case and
				// close a late connection instead of handing it to net/http.
				type dialResult struct {
					conn net.Conn
					err  error
				}
				resultCh := make(chan dialResult, 1)
				go func() {
					conn, err := dialer.Dial(network, addr)
					resultCh <- dialResult{conn: conn, err: err}
				}()
				select {
				case result := <-resultCh:
					return result.conn, result.err
				case <-ctx.Done():
					go func() {
						result := <-resultCh
						if result.conn != nil {
							_ = result.conn.Close()
						}
					}()
					return nil, ctx.Err()
				}
			},
		}
		if common.TLSInsecureSkipVerify {
			transport.TLSClientConfig = common.InsecureTLSConfig
		}

		// Keep the hostname intact for the remote-DNS case; protected requests
		// with domain IP filtering enabled are pinned to their validated IP.
		client := &http.Client{Transport: withDefaultUserAgentTransport(withSSRFProtectionTransport(transport, true)), CheckRedirect: checkRedirectWithRemoteDNS}
		client.Timeout = time.Duration(common.RelayTimeout) * time.Second
		proxyClientLock.Lock()
		proxyClients[proxyURL] = client
		proxyClientLock.Unlock()
		return client, nil

	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s, must be http, https, socks5 or socks5h", parsedURL.Scheme)
	}
}
