package common

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
	"unsafe"
)

var (
	maskURLPattern    = regexp.MustCompile(`(http|https)://[^\s/$.?#].[^\s]*`)
	maskDomainPattern = regexp.MustCompile(`\b(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}\b`)
	maskIPPattern     = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	// IPv6 is validated with net.ParseIP after this deliberately broad token
	// scan. This avoids treating ordinary colon-separated text as an address.
	maskIPv6Pattern = regexp.MustCompile(`(?i)(?:[0-9a-f]{0,4}:){2,}[0-9a-z:%]{0,32}`)
	// maskApiKeyPattern masks API keys in common header/query-style forms such as
	// "Api-Key: secret", "api_key=secret" and quoted JSON-like values.
	maskApiKeyPattern = regexp.MustCompile(`(?i)(^|[^[:alnum:]_])((?:['"])?api[-_]key(?:['"])?\s*[:=]\s*(?:['"])?)([^\s,;}\]"')]+)(['"]?)`)
	// maskAuthTokenPattern masks credentials carried in common authentication
	// schemes, including short Basic/Bearer/Token values. The replacement
	// callback below preserves a small set of unambiguous prose words after
	// "token" so ordinary diagnostics do not get rewritten as credentials.
	maskAuthTokenPattern = regexp.MustCompile(`(?i)\b(basic|bearer|token)\s+([A-Za-z0-9][^\s,;}\]"']*)`)
	// maskSensitiveHeaderPattern covers authentication values that do not use
	// Bearer/Token syntax, including Basic and proxy credentials.
	maskSensitiveHeaderPattern = regexp.MustCompile(`(?i)(^|[^[:alnum:]_-])((?:authorization|proxy-authorization|x-api-key|x-goog-api-key|api[-_]?key|apikey|cookie|set-cookie)\s*[:=]\s*)((?:basic|bearer|token)\s+)?([^\s,;}\]"']+)`)
	// maskStandaloneKeyPattern masks provider-style keys when they are not preceded by a label.
	maskStandaloneKeyPattern = regexp.MustCompile(`(?i)(^|[^[:alnum:]_-])(?:sk|rk|sess)(?:-|_)[^\s,;}\]"')]+`)
	// maskNamedSecretPattern also covers query-style credentials without a URL
	// scheme, for example "api.example.com?token=secret".
	maskNamedSecretPattern = regexp.MustCompile(`(?i)(^|[^[:alnum:]_-])((?:access[-_]?token|refresh[-_]?token|id[-_]?token|token|secret|password|passwd|api[-_]?key|apikey|key)\s*[:=]\s*)(['"]?)([^\s,;&}\]"']+)(['"]?)`)
)

// Responses event names contain dots but are protocol identifiers, not hostnames.
// Keep them intact when masking free-form error and log text.
var protectedResponsesEventNames = map[string]struct{}{
	"response.completed":                     {},
	"response.created":                       {},
	"response.done":                          {},
	"response.error":                         {},
	"response.failed":                        {},
	"response.incomplete":                    {},
	"response.queued":                        {},
	"response.compaction":                    {},
	"response.create":                        {},
	"response.metadata":                      {},
	"response.output_item.added":             {},
	"response.output_item.done":              {},
	"response.output_text.delta":             {},
	"response.output_text.done":              {},
	"response.function_call_arguments.delta": {},
	"response.function_call_arguments.done":  {},
	"response.reasoning_summary_part.added":  {},
	"response.reasoning_summary_part.done":   {},
	"response.reasoning_summary_text.delta":  {},
	"response.reasoning_summary_text.done":   {},
	"response.reasoning_text.delta":          {},
	"response.reasoning_text.done":           {},
	"response.reasoning.unknown":             {},
	"response.content_part.added":            {},
	"response.content_part.done":             {},
	"response.custom_tool_call_input.delta":  {},
	"response.custom_tool_call_input.done":   {},
	"response.audio.delta":                   {},
	"response.audio_transcript.delta":        {},
}

const LocalLogContentLimit = 2048

func LocalLogPreview(content string) string {
	if DebugEnabled || len(content) <= LocalLogContentLimit {
		return content
	}
	end := LocalLogContentLimit
	validUTF8 := utf8.ValidString(content)
	if validUTF8 {
		for end > 0 && !utf8.RuneStart(content[end]) {
			end--
		}
		if end == 0 {
			// Defensive fallback: valid UTF-8 input should always have a rune start before the limit.
			SysLog(fmt.Sprintf("LocalLogPreview unexpected rune boundary fallback: content_length=%d limit=%d", len(content), LocalLogContentLimit))
			end = LocalLogContentLimit
		}
	}
	preview := content[:end]
	if !validUTF8 {
		preview = strings.ToValidUTF8(preview, "?")
		return fmt.Sprintf("%s... [truncated, contains invalid UTF-8, original_length=%d, limit=%d]", preview, len(content), LocalLogContentLimit)
	}
	return fmt.Sprintf("%s... [truncated, original_length=%d, limit=%d]", preview, len(content), LocalLogContentLimit)
}

func GetStringIfEmpty(str string, defaultValue string) string {
	if str == "" {
		return defaultValue
	}
	return str
}

// IsAutoGroup reports whether a group value denotes the reserved automatic
// group. The sentinel is case-insensitive at all request boundaries so
// "auto", "AUTO", and "Auto" cannot take different routing paths.
func IsAutoGroup(group string) bool {
	return strings.EqualFold(strings.TrimSpace(group), "auto")
}

func GetRandomString(length int) string {
	if length <= 0 {
		return ""
	}
	return randomStringFromCrypto(length)
}

func MapToJsonStr(m map[string]interface{}) string {
	bytes, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(bytes)
}

func StrToMap(str string) (map[string]interface{}, error) {
	m := make(map[string]interface{})
	err := Unmarshal([]byte(str), &m)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func StrToJsonArray(str string) ([]interface{}, error) {
	var js []interface{}
	err := json.Unmarshal([]byte(str), &js)
	if err != nil {
		return nil, err
	}
	return js, nil
}

func IsJsonArray(str string) bool {
	var js []interface{}
	return json.Unmarshal([]byte(str), &js) == nil
}

func IsJsonObject(str string) bool {
	var js map[string]interface{}
	return json.Unmarshal([]byte(str), &js) == nil
}

func String2Int(str string) int {
	num, err := strconv.Atoi(str)
	if err != nil {
		return 0
	}
	return num
}

func StringsContains(strs []string, str string) bool {
	for _, s := range strs {
		if s == str {
			return true
		}
	}
	return false
}

// StringToByteSlice []byte only read, panic on append
func StringToByteSlice(s string) []byte {
	tmp1 := (*[2]uintptr)(unsafe.Pointer(&s))
	tmp2 := [3]uintptr{tmp1[0], tmp1[1], tmp1[1]}
	return *(*[]byte)(unsafe.Pointer(&tmp2))
}

func EncodeBase64(str string) string {
	return base64.StdEncoding.EncodeToString([]byte(str))
}

func GetJsonString(data any) string {
	if data == nil {
		return ""
	}
	b, _ := json.Marshal(data)
	return string(b)
}

// NormalizeBillingPreference clamps the billing preference to valid values.
func NormalizeBillingPreference(pref string) string {
	switch strings.TrimSpace(pref) {
	case "subscription_first", "wallet_first", "subscription_only", "wallet_only":
		return strings.TrimSpace(pref)
	default:
		return "subscription_first"
	}
}

// MaskEmail masks a user email to prevent PII leakage in logs
// Returns "***masked***" if email is empty, otherwise shows only the domain part
func MaskEmail(email string) string {
	if email == "" {
		return "***masked***"
	}

	// Find the @ symbol
	atIndex := strings.Index(email, "@")
	if atIndex == -1 {
		// No @ symbol found, return masked
		return "***masked***"
	}

	// Return only the domain part with @ symbol
	return "***@" + email[atIndex+1:]
}

// maskHostTail returns the tail parts of a domain/host that should be preserved.
// It keeps 2 parts for likely country-code TLDs (e.g., co.uk, com.cn), otherwise keeps only the TLD.
func maskHostTail(parts []string) []string {
	if len(parts) < 2 {
		return parts
	}
	lastPart := parts[len(parts)-1]
	secondLastPart := parts[len(parts)-2]
	if len(lastPart) == 2 && len(secondLastPart) <= 3 {
		// Likely country code TLD like co.uk, com.cn
		return []string{secondLastPart, lastPart}
	}
	return []string{lastPart}
}

// maskHostForURL collapses subdomains and keeps only masked prefix + preserved tail.
// Example: api.openai.com -> ***.com, sub.domain.co.uk -> ***.co.uk
func maskHostForURL(host string) string {
	host = strings.TrimSpace(host)
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		if strings.Contains(host, ":") {
			return "[***]"
		}
		return "***"
	}
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return "***"
	}
	tail := maskHostTail(parts)
	return "***." + strings.Join(tail, ".")
}

// maskHostForPlainDomain masks a plain domain and reflects subdomain depth with multiple ***.
// Example: openai.com -> ***.com, api.openai.com -> ***.***.com, sub.domain.co.uk -> ***.***.co.uk
func maskHostForPlainDomain(domain string) string {
	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return domain
	}
	tail := maskHostTail(parts)
	numStars := len(parts) - len(tail)
	if numStars < 1 {
		numStars = 1
	}
	stars := strings.TrimSuffix(strings.Repeat("***.", numStars), ".")
	return stars + "." + strings.Join(tail, ".")
}

// MaskSensitiveInfo masks sensitive information like URLs, IPs, and domain names in a string
// Example:
// http://example.com -> http://***.com
// https://api.test.org/v1/users/123?key=secret -> https://***.org/***/***/?key=***
// https://sub.domain.co.uk/path/to/resource -> https://***.co.uk/***/***
// 192.168.1.1 -> ***.***.***.***
// openai.com -> ***.com
// www.openai.com -> ***.***.com
// api.openai.com -> ***.***.com
func MaskSensitiveInfo(str string) string {
	// Mask authentication tokens before URL/domain processing so a token that
	// happens to contain dots cannot be partially exposed by the domain masker.
	str = maskAuthTokenPattern.ReplaceAllStringFunc(str, func(match string) string {
		parts := maskAuthTokenPattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return "***"
		}
		if (strings.EqualFold(parts[1], "token") || strings.EqualFold(parts[1], "bearer")) && isAuthSchemeProseWord(parts[2]) {
			return match
		}
		return parts[1] + " ***"
	})
	str = maskStandaloneKeyPattern.ReplaceAllString(str, "${1}***")
	str = maskSensitiveHeaderPattern.ReplaceAllString(str, "${1}${2}${3}***")
	str = maskNamedSecretPattern.ReplaceAllString(str, "${1}${2}${3}***${5}")

	// Mask URLs
	str = maskURLPattern.ReplaceAllStringFunc(str, func(urlStr string) string {
		u, err := url.Parse(urlStr)
		if err != nil {
			return urlStr
		}

		host := u.Hostname()
		if host == "" {
			return urlStr
		}

		// Mask host with unified logic
		maskedHost := maskHostForURL(host)
		if port := u.Port(); port != "" {
			maskedHost += ":" + port
		}

		result := u.Scheme + "://" + maskedHost

		// Mask path
		if u.Path != "" && u.Path != "/" {
			pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")
			maskedPathParts := make([]string, len(pathParts))
			for i := range pathParts {
				if pathParts[i] != "" {
					maskedPathParts[i] = "***"
				}
			}
			if len(maskedPathParts) > 0 {
				result += "/" + strings.Join(maskedPathParts, "/")
			}
		} else if u.Path == "/" {
			result += "/"
		}

		// Mask query parameters
		if u.RawQuery != "" {
			values, err := url.ParseQuery(u.RawQuery)
			if err != nil {
				// If can't parse query, just mask the whole query string
				result += "?***"
			} else {
				keys := make([]string, 0, len(values))
				for key := range values {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				maskedParams := make([]string, 0, len(keys))
				for _, key := range keys {
					maskedParams = append(maskedParams, key+"=***")
				}
				if len(maskedParams) > 0 {
					result += "?" + strings.Join(maskedParams, "&")
				}
			}
		}

		return result
	})

	// Mask domain names without protocol (like openai.com, www.openai.com)
	str = maskDomainPattern.ReplaceAllStringFunc(str, func(domain string) string {
		if _, ok := protectedResponsesEventNames[strings.ToLower(domain)]; ok {
			return domain
		}
		return maskHostForPlainDomain(domain)
	})

	// Mask plain IPv6 literals after URL replacement. The callback validates the
	// candidate so timestamps and other colon-separated diagnostics remain intact.
	str = maskIPv6Pattern.ReplaceAllStringFunc(str, func(candidate string) string {
		original := candidate
		candidate = strings.Trim(candidate, "[]")
		if zoneIndex := strings.LastIndex(candidate, "%"); zoneIndex >= 0 {
			candidate = candidate[:zoneIndex]
		}
		if net.ParseIP(candidate) == nil {
			return original
		}
		return "***"
	})

	// Mask IP addresses
	str = maskIPPattern.ReplaceAllString(str, "***.***.***.***")

	// Mask API keys (e.g., "api_key:example-token" -> "api_key:***")
	str = maskApiKeyPattern.ReplaceAllString(str, "${1}${2}***${4}")

	return str
}

func isAuthSchemeProseWord(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "has", "is", "required", "missing", "unavailable", "invalid", "expired", "present", "not", "no", "none", "was", "were", "needed", "set", "provided", "count", "usage", "token",
		"quota", "remain", "model", "access", "configuration", "id", "endpoint", "authentication", "state", "scope":
		return true
	default:
		return false
	}
}
