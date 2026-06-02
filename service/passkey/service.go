package passkey

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/go-webauthn/webauthn/protocol"
	webauthn "github.com/go-webauthn/webauthn/webauthn"
)

const (
	RegistrationSessionKey = "passkey_registration_session"
	LoginSessionKey        = "passkey_login_session"
	VerifySessionKey       = "passkey_verify_session"
)

// BuildWebAuthn constructs a WebAuthn instance using the current passkey settings and request context.
func BuildWebAuthn(r *http.Request) (*webauthn.WebAuthn, error) {
	settings := system_setting.GetPasskeySettings()
	if settings == nil {
		return nil, errors.New("未找到 Passkey 设置")
	}

	displayName := strings.TrimSpace(settings.RPDisplayName)
	if displayName == "" {
		displayName = common.SystemName
	}

	origins, err := resolveOrigins(r, settings)
	if err != nil {
		return nil, err
	}

	rpID, err := resolveRPID(r, settings, origins)
	if err != nil {
		return nil, err
	}

	selection := protocol.AuthenticatorSelection{
		ResidentKey:        protocol.ResidentKeyRequirementRequired,
		RequireResidentKey: protocol.ResidentKeyRequired(),
		UserVerification:   protocol.UserVerificationRequirement(settings.UserVerification),
	}
	if selection.UserVerification == "" {
		selection.UserVerification = protocol.VerificationPreferred
	}
	if attachment := strings.TrimSpace(settings.AttachmentPreference); attachment != "" {
		selection.AuthenticatorAttachment = protocol.AuthenticatorAttachment(attachment)
	}

	config := &webauthn.Config{
		RPID:                   rpID,
		RPDisplayName:          displayName,
		RPOrigins:              origins,
		AuthenticatorSelection: selection,
		Debug:                  common.DebugEnabled,
		Timeouts: webauthn.TimeoutsConfig{
			Login: webauthn.TimeoutConfig{
				Enforce:    true,
				Timeout:    2 * time.Minute,
				TimeoutUVD: 2 * time.Minute,
			},
			Registration: webauthn.TimeoutConfig{
				Enforce:    true,
				Timeout:    2 * time.Minute,
				TimeoutUVD: 2 * time.Minute,
			},
		},
	}

	return webauthn.New(config)
}

func resolveOrigins(r *http.Request, settings *system_setting.PasskeySettings) ([]string, error) {
	originsStr := strings.TrimSpace(settings.Origins)
	if originsStr != "" {
		originList := strings.Split(originsStr, ",")
		origins := make([]string, 0, len(originList))
		for _, origin := range originList {
			trimmed := strings.TrimSpace(origin)
			if trimmed == "" {
				continue
			}
			if !settings.AllowInsecureOrigin && isInsecureNonLoopbackOrigin(trimmed) {
				return nil, fmt.Errorf("Passkey 不允许使用不安全的 Origin: %s", trimmed)
			}
			origins = append(origins, trimmed)
		}
		if len(origins) == 0 {
			// 如果配置了Origins但过滤后为空，使用自动推导
			goto autoDetect
		}
		origins = appendRequestLoopbackOrigin(r, origins)
		return origins, nil
	}

autoDetect:
	scheme := detectScheme(r)
	// 优先使用请求的完整Host（包含端口）
	host := r.Host
	hostFromServerAddress := false

	// 如果无法从请求获取Host，尝试从ServerAddress获取
	if host == "" && system_setting.ServerAddress != "" {
		if parsed, err := url.Parse(system_setting.ServerAddress); err == nil && parsed.Host != "" {
			host = parsed.Host
			hostFromServerAddress = true
			if parsed.Scheme != "" {
				scheme = parsed.Scheme
			}
		}
	}
	if host == "" {
		return nil, fmt.Errorf("无法确定 Passkey 的 Origin，请在系统设置或 Passkey 设置中指定。当前 Host: '%s', ServerAddress: '%s'", r.Host, system_setting.ServerAddress)
	}
	if scheme == "" {
		scheme = "https"
	}
	if scheme == "http" && !settings.AllowInsecureOrigin && !isPasskeyLoopbackHost(host) {
		if hostFromServerAddress {
			return nil, fmt.Errorf("Passkey 仅支持 HTTPS 或环回地址，无法从请求获取 Host，且 ServerAddress 不是环回地址: %s://%s", scheme, host)
		}
		return nil, fmt.Errorf("Passkey 仅支持 HTTPS，当前访问: %s://%s，请在 Passkey 设置中允许不安全 Origin 或配置 HTTPS", scheme, host)
	}
	origin := fmt.Sprintf("%s://%s", scheme, host)
	return []string{origin}, nil
}

func isPasskeyLoopbackHost(host string) bool {
	host = hostWithoutPort(host)
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isInsecureNonLoopbackOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || !strings.EqualFold(parsed.Scheme, "http") {
		return false
	}
	return !isPasskeyLoopbackHost(parsed.Host)
}

func resolveRPID(r *http.Request, settings *system_setting.PasskeySettings, origins []string) (string, error) {
	rpID := strings.TrimSpace(settings.RPID)
	if rpID != "" {
		configuredRPID := hostWithoutPort(rpID)
		if isPasskeyLoopbackHost(configuredRPID) && requestLoopbackOriginAllowed(r, origins) {
			return requestHostWithoutPort(r), nil
		}
		return configuredRPID, nil
	}
	if len(origins) == 0 {
		return "", errors.New("Passkey 未配置 Origin，无法推导 RPID")
	}
	if requestLoopbackOriginAllowed(r, origins) {
		return requestHostWithoutPort(r), nil
	}
	parsed, err := url.Parse(origins[0])
	if err != nil {
		return "", fmt.Errorf("无法解析 Passkey Origin: %w", err)
	}
	return hostWithoutPort(parsed.Host), nil
}

func hostWithoutPort(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if strings.Contains(host, ":") {
		if parsedHost, _, err := net.SplitHostPort(host); err == nil {
			host = parsedHost
		}
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		unbracketed := strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
		if net.ParseIP(unbracketed) != nil {
			return unbracketed
		}
	}
	return host
}

func appendRequestLoopbackOrigin(r *http.Request, origins []string) []string {
	requestOrigin := detectRequestOrigin(r)
	if requestOrigin == "" {
		return origins
	}
	parsedRequestOrigin, err := url.Parse(requestOrigin)
	if err != nil || !isPasskeyLoopbackHost(parsedRequestOrigin.Host) {
		return origins
	}
	for _, origin := range origins {
		if strings.EqualFold(origin, requestOrigin) {
			return origins
		}
		parsedOrigin, err := url.Parse(origin)
		if err == nil && loopbackOriginsShareSchemeAndPort(parsedOrigin, parsedRequestOrigin) {
			return append(origins, requestOrigin)
		}
	}
	return origins
}

func loopbackOriginsShareSchemeAndPort(configured *url.URL, request *url.URL) bool {
	if configured == nil || request == nil {
		return false
	}
	if !strings.EqualFold(configured.Scheme, request.Scheme) {
		return false
	}
	if !isPasskeyLoopbackHost(configured.Host) || !isPasskeyLoopbackHost(request.Host) {
		return false
	}
	return originEffectivePort(configured) == originEffectivePort(request)
}

func originEffectivePort(origin *url.URL) string {
	if origin == nil {
		return ""
	}
	if port := origin.Port(); port != "" {
		return port
	}
	switch strings.ToLower(origin.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

func detectRequestOrigin(r *http.Request) string {
	if r == nil {
		return ""
	}
	host := strings.TrimSpace(r.Host)
	if host == "" {
		return ""
	}
	scheme := detectScheme(r)
	if scheme == "" {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, host)
}

func requestHostWithoutPort(r *http.Request) string {
	if r == nil {
		return ""
	}
	return hostWithoutPort(r.Host)
}

func requestLoopbackOriginAllowed(r *http.Request, origins []string) bool {
	requestHost := requestHostWithoutPort(r)
	if !isPasskeyLoopbackHost(requestHost) {
		return false
	}
	requestOrigin := detectRequestOrigin(r)
	if requestOrigin == "" {
		return false
	}
	for _, origin := range origins {
		if strings.EqualFold(strings.TrimSpace(origin), requestOrigin) {
			return true
		}
	}
	return false
}

func detectScheme(r *http.Request) string {
	if r == nil {
		return ""
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		parts := strings.Split(proto, ",")
		return strings.ToLower(strings.TrimSpace(parts[0]))
	}
	if r.TLS != nil {
		return "https"
	}
	if r.URL != nil && r.URL.Scheme != "" {
		return strings.ToLower(r.URL.Scheme)
	}
	if r.Header.Get("X-Forwarded-Protocol") != "" {
		return strings.ToLower(strings.TrimSpace(r.Header.Get("X-Forwarded-Protocol")))
	}
	return "http"
}
