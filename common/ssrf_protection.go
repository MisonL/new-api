package common

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// lookupIPForSSRF is a variable so callers can exercise the validation
// boundary without opening a network connection. Production uses the standard
// resolver with the request context; tests may replace it temporarily.
var lookupIPForSSRF = func(ctx context.Context, host string) ([]net.IP, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

type validatedURLIPsContextKey struct{}
type validatedURLIPSPinningDisabledContextKey struct{}
type ssrfValidationRequiredContextKey struct{}
type ssrfRemoteDNSContextKey struct{}

// WithSSRFValidation marks a request for destination validation by the shared
// HTTP transport. Relay requests are not marked because their channel URL is a
// trusted configuration value, while user-controlled fetch URLs are.
func WithSSRFValidation(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, ssrfValidationRequiredContextKey{}, true)
}

// RequiresSSRFValidation reports whether the request explicitly opted into the
// fetch-url SSRF boundary.
func RequiresSSRFValidation(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	required, _ := ctx.Value(ssrfValidationRequiredContextKey{}).(bool)
	return required
}

// WithSSRFRemoteDNS marks a protected request that uses a SOCKS transport. The
// marker is carried across redirects; the transport still resolves and
// validates the destination locally before pinning the connection.
func WithSSRFRemoteDNS(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, ssrfRemoteDNSContextKey{}, true)
}

// UsesSSRFRemoteDNS reports whether the protected request uses the SOCKS
// transport policy and must carry its marker through redirects.
func UsesSSRFRemoteDNS(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	remote, _ := ctx.Value(ssrfRemoteDNSContextKey{}).(bool)
	return remote
}

// WithValidatedURLIPs attaches the DNS result that has just passed the SSRF
// policy to a request context. The HTTP transport uses it to dial the exact
// addresses that were validated instead of resolving the hostname again.
func WithValidatedURLIPs(ctx context.Context, host string, ips []net.IP) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	cloned := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		if ip == nil {
			continue
		}
		cloned = append(cloned, append(net.IP(nil), ip...))
	}
	// Attaching a validated snapshot explicitly re-enables pinning. This also
	// prevents a proxy marker inherited through a redirect context from leaking
	// into a later direct request.
	ctx = context.WithValue(ctx, validatedURLIPSPinningDisabledContextKey{}, false)
	return context.WithValue(ctx, validatedURLIPsContextKey{}, validatedURLIPs{host: canonicalizeDomainName(host), ips: cloned})
}

type validatedURLIPs struct {
	host string
	ips  []net.IP
}

// ValidatedURLIPs returns a copy of the validated DNS result for host.
func ValidatedURLIPs(ctx context.Context, host string) []net.IP {
	if ctx == nil {
		return nil
	}
	value, ok := ctx.Value(validatedURLIPsContextKey{}).(validatedURLIPs)
	if !ok || value.host != canonicalizeDomainName(host) {
		return nil
	}
	result := make([]net.IP, 0, len(value.ips))
	for _, ip := range value.ips {
		result = append(result, append(net.IP(nil), ip...))
	}
	return result
}

// WithValidatedURLIPSPinningDisabled marks a request so transports that have
// a validated-IP dial hook must keep the original dial address. This is used
// for proxy connections, whose endpoint is not the validated target host.
func WithValidatedURLIPSPinningDisabled(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, validatedURLIPSPinningDisabledContextKey{}, true)
}

// ValidatedURLIPSPinningDisabled reports whether validated-IP substitution is
// disabled for the current request context.
func ValidatedURLIPSPinningDisabled(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	disabled, _ := ctx.Value(validatedURLIPSPinningDisabledContextKey{}).(bool)
	return disabled
}

// canonicalizeDomainName applies the DNS presentation normalization shared by
// policy matching, lookup and transport context binding. A trailing root dot
// does not change the DNS name, so it must not create a second policy identity.
func canonicalizeDomainName(domain string) string {
	return strings.TrimRight(strings.ToLower(strings.TrimSpace(domain)), ".")
}

// SSRFProtection SSRF防护配置
type SSRFProtection struct {
	AllowPrivateIp         bool
	DomainFilterMode       bool     // true: 白名单, false: 黑名单
	DomainList             []string // domain format, e.g. example.com, *.example.com
	IpFilterMode           bool     // true: 白名单, false: 黑名单
	IpList                 []string // CIDR or single IP
	AllowedPorts           []int    // 允许的端口范围
	ApplyIPFilterForDomain bool     // 对域名启用IP过滤
}

// DefaultSSRFProtection 默认SSRF防护配置
var DefaultSSRFProtection = &SSRFProtection{
	AllowPrivateIp:   false,
	DomainFilterMode: true,
	DomainList:       []string{},
	IpFilterMode:     true,
	IpList:           []string{},
	AllowedPorts:     []int{},
}

// privateIPv4Nets IPv4 私有/保留/特殊用途网段
// 参考 IANA IPv4 Special-Purpose Address Registry
// https://www.iana.org/assignments/iana-ipv4-special-registry/
var privateIPv4Nets = []net.IPNet{
	{IP: net.IPv4(0, 0, 0, 0), Mask: net.CIDRMask(8, 32)},          // 0.0.0.0/8 ("This network" / 未指定)
	{IP: net.IPv4(10, 0, 0, 0), Mask: net.CIDRMask(8, 32)},         // 10.0.0.0/8 (私有)
	{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)},      // 100.64.0.0/10 (运营商级 NAT / CGNAT)
	{IP: net.IPv4(127, 0, 0, 0), Mask: net.CIDRMask(8, 32)},        // 127.0.0.0/8 (回环)
	{IP: net.IPv4(169, 254, 0, 0), Mask: net.CIDRMask(16, 32)},     // 169.254.0.0/16 (链路本地)
	{IP: net.IPv4(172, 16, 0, 0), Mask: net.CIDRMask(12, 32)},      // 172.16.0.0/12 (私有)
	{IP: net.IPv4(192, 0, 0, 0), Mask: net.CIDRMask(24, 32)},       // 192.0.0.0/24 (IETF 协议分配)
	{IP: net.IPv4(192, 0, 2, 0), Mask: net.CIDRMask(24, 32)},       // 192.0.2.0/24 (TEST-NET-1)
	{IP: net.IPv4(192, 168, 0, 0), Mask: net.CIDRMask(16, 32)},     // 192.168.0.0/16 (私有)
	{IP: net.IPv4(198, 18, 0, 0), Mask: net.CIDRMask(15, 32)},      // 198.18.0.0/15 (基准测试)
	{IP: net.IPv4(198, 51, 100, 0), Mask: net.CIDRMask(24, 32)},    // 198.51.100.0/24 (TEST-NET-2)
	{IP: net.IPv4(203, 0, 113, 0), Mask: net.CIDRMask(24, 32)},     // 203.0.113.0/24 (TEST-NET-3)
	{IP: net.IPv4(224, 0, 0, 0), Mask: net.CIDRMask(4, 32)},        // 224.0.0.0/4 (组播)
	{IP: net.IPv4(240, 0, 0, 0), Mask: net.CIDRMask(4, 32)},        // 240.0.0.0/4 (保留)
	{IP: net.IPv4(255, 255, 255, 255), Mask: net.CIDRMask(32, 32)}, // 255.255.255.255/32 (受限广播)
}

// privateIPv6Nets IPv6 私有/保留/特殊用途网段
// 参考 IANA IPv6 Special-Purpose Address Registry
// https://www.iana.org/assignments/iana-ipv6-special-registry/
var privateIPv6Nets = func() []net.IPNet {
	cidrs := []string{
		"::/128",        // 未指定地址
		"::1/128",       // 回环
		"::ffff:0:0/96", // IPv4-mapped
		"64:ff9b::/96",  // IPv4/IPv6 translation
		"100::/64",      // Discard-Only
		"2001::/23",     // IETF Protocol Assignments
		"2001:db8::/32", // 文档
		"fc00::/7",      // Unique Local Address (ULA)
		"fe80::/10",     // 链路本地
		"ff00::/8",      // 组播
	}
	nets := make([]net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		if _, n, err := net.ParseCIDR(c); err == nil && n != nil {
			nets = append(nets, *n)
		}
	}
	return nets
}()

// isPrivateIP 检查IP是否为私有/保留/特殊用途地址
func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	// 未指定地址 (0.0.0.0, ::)
	if ip.IsUnspecified() {
		return true
	}
	// 回环、链路本地 (unicast/multicast)
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	// 接口本地组播 (IPv6 ff01::/16 等)
	if ip.IsInterfaceLocalMulticast() {
		return true
	}

	if v4 := ip.To4(); v4 != nil {
		for _, privateNet := range privateIPv4Nets {
			if privateNet.Contains(v4) {
				return true
			}
		}
		return false
	}

	// IPv6 检查
	for _, privateNet := range privateIPv6Nets {
		if privateNet.Contains(ip) {
			return true
		}
	}
	// 兜底: Go 标准库识别的其他私有地址
	if ip.IsPrivate() {
		return true
	}
	return false
}

// parsePortRanges 解析端口范围配置
// 支持格式: "80", "443", "8000-9000"
func parsePortRanges(portConfigs []string) ([]int, error) {
	var ports []int

	for _, config := range portConfigs {
		config = strings.TrimSpace(config)
		if config == "" {
			continue
		}

		if strings.Contains(config, "-") {
			// 处理端口范围 "8000-9000"
			parts := strings.Split(config, "-")
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid port range format: %s", config)
			}

			startPort, err := strconv.Atoi(strings.TrimSpace(parts[0]))
			if err != nil {
				return nil, fmt.Errorf("invalid start port in range %s: %v", config, err)
			}

			endPort, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				return nil, fmt.Errorf("invalid end port in range %s: %v", config, err)
			}

			if startPort > endPort {
				return nil, fmt.Errorf("invalid port range %s: start port cannot be greater than end port", config)
			}

			if startPort < 1 || startPort > 65535 || endPort < 1 || endPort > 65535 {
				return nil, fmt.Errorf("port range %s contains invalid port numbers (must be 1-65535)", config)
			}

			// 添加范围内的所有端口
			for port := startPort; port <= endPort; port++ {
				ports = append(ports, port)
			}
		} else {
			// 处理单个端口 "80"
			port, err := strconv.Atoi(config)
			if err != nil {
				return nil, fmt.Errorf("invalid port number: %s", config)
			}

			if port < 1 || port > 65535 {
				return nil, fmt.Errorf("invalid port number %d (must be 1-65535)", port)
			}

			ports = append(ports, port)
		}
	}

	return ports, nil
}

// isAllowedPort 检查端口是否被允许
func (p *SSRFProtection) isAllowedPort(port int) bool {
	if len(p.AllowedPorts) == 0 {
		return true // 如果没有配置端口限制，则允许所有端口
	}

	for _, allowedPort := range p.AllowedPorts {
		if port == allowedPort {
			return true
		}
	}
	return false
}

// isDomainWhitelisted 检查域名是否在白名单中
func isDomainListed(domain string, list []string) bool {
	if len(list) == 0 {
		return false
	}

	domain = canonicalizeDomainName(domain)
	for _, item := range list {
		item = canonicalizeDomainName(item)
		if item == "" {
			continue
		}
		// 精确匹配
		if domain == item {
			return true
		}
		// 通配符匹配 (*.example.com)
		if strings.HasPrefix(item, "*.") {
			suffix := strings.TrimPrefix(item, "*.")
			if strings.HasSuffix(domain, "."+suffix) || domain == suffix {
				return true
			}
		}
	}
	return false
}

func (p *SSRFProtection) isDomainAllowed(domain string) bool {
	listed := isDomainListed(domain, p.DomainList)
	if p.DomainFilterMode { // 白名单
		return listed
	}
	// 黑名单
	return !listed
}

// isIPWhitelisted 检查IP是否在白名单中

func isIPListed(ip net.IP, list []string) bool {
	if len(list) == 0 {
		return false
	}

	return IsIpInCIDRList(ip, list)
}

// IsIPAccessAllowed 检查IP是否允许访问
func (p *SSRFProtection) IsIPAccessAllowed(ip net.IP) bool {
	// 私有IP限制
	if isPrivateIP(ip) && !p.AllowPrivateIp {
		return false
	}

	listed := isIPListed(ip, p.IpList)
	if p.IpFilterMode { // 白名单
		return listed
	}
	// 黑名单
	return !listed
}

// ValidateURL 验证URL是否安全
func (p *SSRFProtection) ValidateURL(urlStr string) error {
	_, err := p.ValidateURLAndResolve(urlStr)
	return err
}

// ValidateURLAndResolve validates a URL and returns the exact DNS addresses
// observed during validation. Returning the addresses is important: validating
// with one resolver call and letting net/http resolve the hostname again leaves
// a DNS-rebinding time-of-check/time-of-use window.
func (p *SSRFProtection) ValidateURLAndResolve(urlStr string) ([]net.IP, error) {
	return p.ValidateURLAndResolveContext(context.Background(), urlStr)
}

// ValidateURLAndResolveContext is the cancellation-aware form used by HTTP
// transports. DNS resolution must observe the originating request context so
// a slow resolver cannot hold an upstream request until the gateway returns a
// timeout without cancelling the validation work.
func (p *SSRFProtection) ValidateURLAndResolveContext(ctx context.Context, urlStr string) ([]net.IP, error) {
	return p.ValidateURLAndResolveContextWithOptions(ctx, urlStr, true)
}

// ValidateURLAndResolveContextWithOptions validates a URL and optionally
// resolves domain names. Protected user-controlled fetch requests must keep
// resolveDomain enabled so private and special DNS results cannot bypass the
// SSRF boundary; the false mode is reserved for callers with an independently
// enforced destination policy.
func (p *SSRFProtection) ValidateURLAndResolveContextWithOptions(ctx context.Context, urlStr string, resolveDomain bool) ([]net.IP, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// 解析URL
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("invalid URL format: %v", err)
	}

	// 只允许HTTP/HTTPS协议
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported protocol: %s (only http/https allowed)", u.Scheme)
	}

	// url.Parse accepts scheme-only and authority-less forms such as
	// "http:///path". They cannot identify a network destination and, in
	// blacklist mode, an empty hostname would otherwise look like an
	// unlisted domain. Reject them before port/domain/IP evaluation so the
	// SSRF gate has one deterministic interpretation for every URL shape.
	if strings.TrimSpace(u.Host) == "" || strings.TrimSpace(u.Hostname()) == "" {
		return nil, fmt.Errorf("invalid URL host: authority and hostname are required")
	}

	// 解析主机和端口
	host, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		// 没有端口，使用默认端口
		host = u.Hostname()
		if u.Scheme == "https" {
			portStr = "443"
		} else {
			portStr = "80"
		}
	}
	host = canonicalizeDomainName(host)
	if host == "" {
		return nil, fmt.Errorf("invalid URL host: authority and hostname are required")
	}

	// 验证端口
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid port: %s", portStr)
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid port: %d (must be 1-65535)", port)
	}

	if !p.isAllowedPort(port) {
		return nil, fmt.Errorf("port %d is not allowed", port)
	}

	// 如果 host 是 IP，则跳过域名检查
	if ip := net.ParseIP(host); ip != nil {
		if !p.IsIPAccessAllowed(ip) {
			if isPrivateIP(ip) {
				return nil, fmt.Errorf("private IP address not allowed: %s", ip.String())
			}
			if p.IpFilterMode {
				return nil, fmt.Errorf("ip not in whitelist: %s", ip.String())
			}
			return nil, fmt.Errorf("ip in blacklist: %s", ip.String())
		}
		return []net.IP{append(net.IP(nil), ip...)}, nil
	}

	// 先进行域名过滤
	if !p.isDomainAllowed(host) {
		if p.DomainFilterMode {
			return nil, fmt.Errorf("domain not in whitelist: %s", host)
		}
		return nil, fmt.Errorf("domain in blacklist: %s", host)
	}
	if !resolveDomain {
		return nil, nil
	}

	// Resolve the hostname even when the optional domain IP list is disabled.
	// The basic private/special-address check is still required to prevent a
	// DNS-rebinding or split-horizon name from turning a domain allow-list into
	// an SSRF bypass. ApplyIPFilterForDomain only controls the custom IpList
	// policy; the validated result is also used to pin direct connections.
	ips, err := lookupIPForSSRF(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("DNS resolution failed for %s: %v", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("DNS resolution returned no addresses for %s", host)
	}
	validated := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		// A nil or malformed resolver result must never be treated as a
		// successful validation. If it reached the transport with an empty
		// validated set, net/http would resolve the hostname again and reopen
		// the DNS TOCTOU window this boundary is meant to close.
		if ip == nil || ip.To16() == nil {
			return nil, fmt.Errorf("DNS resolution returned an invalid address for %s", host)
		}
		validated = append(validated, append(net.IP(nil), ip...))
	}
	if len(validated) == 0 {
		return nil, fmt.Errorf("DNS resolution returned no valid addresses for %s", host)
	}
	for _, ip := range validated {
		if isPrivateIP(ip) && !p.AllowPrivateIp {
			return nil, fmt.Errorf("private IP address not allowed: %s resolves to %s", host, ip.String())
		}
		if p.ApplyIPFilterForDomain && !p.IsIPAccessAllowed(ip) {
			if p.IpFilterMode {
				return nil, fmt.Errorf("ip not in whitelist: %s resolves to %s", host, ip.String())
			}
			return nil, fmt.Errorf("ip in blacklist: %s resolves to %s", host, ip.String())
		}
	}
	return validated, nil
}

// ValidateURLWithFetchSetting 使用FetchSetting配置验证URL
func ValidateURLWithFetchSetting(urlStr string, enableSSRFProtection, allowPrivateIp bool, domainFilterMode bool, ipFilterMode bool, domainList, ipList, allowedPorts []string, applyIPFilterForDomain bool) error {
	// This public validation boundary must always resolve domains. The
	// applyIPFilterForDomain flag only controls the optional custom IP-list
	// policy; using it as resolveDomain would skip the mandatory private-IP and
	// DNS-rebinding checks whenever that optional policy is disabled.
	_, err := ValidateURLWithFetchSettingAndResolveContextOptions(context.Background(), urlStr, enableSSRFProtection, allowPrivateIp, domainFilterMode, ipFilterMode, domainList, ipList, allowedPorts, applyIPFilterForDomain, true)
	return err
}

// ValidateURLWithFetchSettingAndResolve is the fetch-setting variant used by
// HTTP transports that need to pin a request to the validated DNS result.
func ValidateURLWithFetchSettingAndResolve(urlStr string, enableSSRFProtection, allowPrivateIp bool, domainFilterMode bool, ipFilterMode bool, domainList, ipList, allowedPorts []string, applyIPFilterForDomain bool) ([]net.IP, error) {
	return ValidateURLWithFetchSettingAndResolveContext(context.Background(), urlStr, enableSSRFProtection, allowPrivateIp, domainFilterMode, ipFilterMode, domainList, ipList, allowedPorts, applyIPFilterForDomain)
}

// ValidateURLWithFetchSettingAndResolveContext is the request-scoped fetch
// setting variant used by HTTP transports that need both SSRF validation and
// cancellation-aware DNS resolution.
func ValidateURLWithFetchSettingAndResolveContext(ctx context.Context, urlStr string, enableSSRFProtection, allowPrivateIp bool, domainFilterMode bool, ipFilterMode bool, domainList, ipList, allowedPorts []string, applyIPFilterForDomain bool) ([]net.IP, error) {
	return ValidateURLWithFetchSettingAndResolveContextOptions(ctx, urlStr, enableSSRFProtection, allowPrivateIp, domainFilterMode, ipFilterMode, domainList, ipList, allowedPorts, applyIPFilterForDomain, true)
}

// ValidateURLWithFetchSettingAndResolveContextOptions is the request-scoped
// fetch-setting variant with an explicit domain-resolution policy.
func ValidateURLWithFetchSettingAndResolveContextOptions(ctx context.Context, urlStr string, enableSSRFProtection, allowPrivateIp bool, domainFilterMode bool, ipFilterMode bool, domainList, ipList, allowedPorts []string, applyIPFilterForDomain, resolveDomain bool) ([]net.IP, error) {
	// 如果SSRF防护被禁用，直接返回成功
	if !enableSSRFProtection {
		return nil, nil
	}

	// 解析端口范围配置
	allowedPortInts, err := parsePortRanges(allowedPorts)
	if err != nil {
		return nil, fmt.Errorf("request reject - invalid port configuration: %v", err)
	}

	protection := &SSRFProtection{
		AllowPrivateIp:         allowPrivateIp,
		DomainFilterMode:       domainFilterMode,
		DomainList:             domainList,
		IpFilterMode:           ipFilterMode,
		IpList:                 ipList,
		AllowedPorts:           allowedPortInts,
		ApplyIPFilterForDomain: applyIPFilterForDomain,
	}
	return protection.ValidateURLAndResolveContextWithOptions(ctx, urlStr, resolveDomain)
}
