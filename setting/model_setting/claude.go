package model_setting

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

//var claudeHeadersSettings = map[string][]string{}
//
//var ClaudeThinkingAdapterEnabled = true
//var ClaudeThinkingAdapterMaxTokens = 8192
//var ClaudeThinkingAdapterBudgetTokensPercentage = 0.8

// ClaudeSettings 定义Claude模型的配置
type ClaudeSettings struct {
	HeadersSettings                       map[string]map[string][]string `json:"model_headers_settings"`
	DefaultMaxTokens                      map[string]int                 `json:"default_max_tokens"`
	ThinkingAdapterEnabled                bool                           `json:"thinking_adapter_enabled"`
	ThinkingAdapterBudgetTokensPercentage float64                        `json:"thinking_adapter_budget_tokens_percentage"`
}

// 默认配置
var defaultClaudeSettings = ClaudeSettings{
	HeadersSettings:        map[string]map[string][]string{},
	ThinkingAdapterEnabled: true,
	DefaultMaxTokens: map[string]int{
		"default": 8192,
	},
	ThinkingAdapterBudgetTokensPercentage: 0.8,
}

// 全局实例
var claudeSettings = defaultClaudeSettings

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("claude", &claudeSettings)
}

// GetClaudeSettings 获取Claude配置
func GetClaudeSettings() *ClaudeSettings {
	// check default max tokens must have default key
	if _, ok := claudeSettings.DefaultMaxTokens["default"]; !ok {
		claudeSettings.DefaultMaxTokens["default"] = 8192
	}
	return &claudeSettings
}

// credentialHeaderNames are headers whose value is a single credential rather
// than a list. Merging them with the value already on the request would join two
// credentials into one malformed value, so they are replaced instead.
var credentialHeaderNames = map[string]struct{}{
	"authorization":       {},
	"proxy-authorization": {},
	"x-api-key":           {},
	"x-goog-api-key":      {},
}

func isCredentialHeader(name string) bool {
	_, exists := credentialHeaderNames[strings.ToLower(strings.TrimSpace(name))]
	return exists
}

// anthropicCredentialHeaderNames are the two header names the Messages API
// accepts for the same credential: Authorization: Bearer is the canonical form
// and x-api-key the legacy one. An Anthropic-native request carries both (see
// relay/channel/anthropic_auth_header.go) because an upstream may read either,
// so the two must name the same principal.
var anthropicCredentialHeaderNames = map[string]struct{}{
	"authorization": {},
	"x-api-key":     {},
}

func isAnthropicCredentialHeader(name string) bool {
	_, exists := anthropicCredentialHeaderNames[strings.ToLower(strings.TrimSpace(name))]
	return exists
}

// WriteHeaders applies the operator-configured headers for originModel.
//
// List-valued headers (anthropic-beta and similar) are merged with whatever the
// request already carries, deduplicating values. Credential headers are
// replaced: an operator configuring Authorization or x-api-key means to use that
// credential, not to append it to the channel key, and a comma-joined pair
// authenticates as neither.
func (c *ClaudeSettings) WriteHeaders(originModel string, httpHeader *http.Header) {
	headers, ok := c.HeadersSettings[originModel]
	if !ok {
		return
	}
	carriesAnthropicCredentialPair := httpHeader.Get("Authorization") != "" && httpHeader.Get("x-api-key") != ""
	if carriesAnthropicCredentialPair {
		writeAnthropicCredential(headers, httpHeader)
	}
	for headerKey, headerValues := range headers {
		if carriesAnthropicCredentialPair && isAnthropicCredentialHeader(headerKey) {
			// Written as a single credential above; rewriting one of the pair
			// here would put the two back out of step.
			continue
		}
		if isCredentialHeader(headerKey) {
			replacement := normalizeHeaderListValues(headerValues)
			if len(replacement) == 0 {
				continue
			}
			httpHeader.Set(headerKey, strings.Join(replacement, ","))
			continue
		}
		mergedValues := normalizeHeaderListValues(
			append(append([]string(nil), httpHeader.Values(headerKey)...), headerValues...),
		)
		if len(mergedValues) == 0 {
			continue
		}
		httpHeader.Set(headerKey, strings.Join(mergedValues, ","))
	}
}

// writeAnthropicCredential applies an operator-configured Anthropic credential
// to both of its header forms at once.
//
// Overriding only the configured header would leave the other one carrying the
// channel key, so a request would present two different principals depending on
// which header the upstream happens to read - exactly the ambiguity
// SetAnthropicAuthHeaders exists to avoid.
//
// This is only correct on a request that already carries both forms, which is
// how an Anthropic-native request is built. Vertex fills Authorization with a
// Google OAuth token and AWS sets its own credential after this runs; on those
// paths the pair does not exist, so moving the sibling would overwrite a
// credential that has nothing to do with the operator's value (and, for Vertex,
// break the Google authentication). WriteHeaders calls this only when both
// headers are present, leaving single-form paths to the plain replacement below.
//
// Authorization is the canonical form, so it wins when both forms are
// configured and the result does not depend on map iteration order.
func writeAnthropicCredential(headers map[string][]string, httpHeader *http.Header) {
	if value, ok := configuredHeaderValue(headers, "Authorization"); ok {
		httpHeader.Set("Authorization", value)
		httpHeader.Set("x-api-key", anthropicToken(value))
		return
	}
	if value, ok := configuredHeaderValue(headers, "x-api-key"); ok {
		httpHeader.Set("x-api-key", value)
		httpHeader.Set("Authorization", "Bearer "+anthropicToken(value))
	}
}

// configuredHeaderValue returns the operator-configured value for a single
// header name, matched case-insensitively. When the same header is spelled with
// several cases the lexicographically smallest key wins, so the result is stable
// regardless of map iteration order.
func configuredHeaderValue(headers map[string][]string, name string) (string, bool) {
	matchedKey := ""
	for headerKey := range headers {
		if !strings.EqualFold(strings.TrimSpace(headerKey), name) {
			continue
		}
		if matchedKey == "" || headerKey < matchedKey {
			matchedKey = headerKey
		}
	}
	if matchedKey == "" {
		return "", false
	}
	values := normalizeHeaderListValues(headers[matchedKey])
	if len(values) == 0 {
		return "", false
	}
	return strings.Join(values, ","), true
}

// anthropicToken reduces either credential header form to the bare token:
// Authorization carries "Bearer <token>", x-api-key carries <token>.
func anthropicToken(value string) string {
	const bearerPrefix = "bearer "
	if len(value) >= len(bearerPrefix) && strings.EqualFold(value[:len(bearerPrefix)], bearerPrefix) {
		return strings.TrimSpace(value[len(bearerPrefix):])
	}
	return value
}

func normalizeHeaderListValues(values []string) []string {
	normalizedValues := make([]string, 0, len(values))
	seenValues := make(map[string]struct{}, len(values))
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			normalizedItem := strings.TrimSpace(item)
			if normalizedItem == "" {
				continue
			}
			if _, exists := seenValues[normalizedItem]; exists {
				continue
			}
			seenValues[normalizedItem] = struct{}{}
			normalizedValues = append(normalizedValues, normalizedItem)
		}
	}
	return normalizedValues
}

func (c *ClaudeSettings) GetDefaultMaxTokens(model string) int {
	if maxTokens, ok := c.DefaultMaxTokens[model]; ok {
		return maxTokens
	}
	return c.DefaultMaxTokens["default"]
}
