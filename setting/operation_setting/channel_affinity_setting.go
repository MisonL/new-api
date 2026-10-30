package operation_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

const (
	ResponsesEncryptedContentAffinityRuleName       = "responses encrypted content"
	ResponsesEncryptedContentAffinityTTLSeconds int = 300
)

type ChannelAffinityKeySource struct {
	Type string `json:"type"` // context_int, context_string, gjson
	Key  string `json:"key,omitempty"`
	Path string `json:"path,omitempty"`
}

type ChannelAffinityRule struct {
	Name             string                     `json:"name"`
	ModelRegex       []string                   `json:"model_regex"`
	PathRegex        []string                   `json:"path_regex"`
	UserAgentInclude []string                   `json:"user_agent_include,omitempty"`
	KeySources       []ChannelAffinityKeySource `json:"key_sources"`

	ValueRegex string `json:"value_regex"`
	TTLSeconds int    `json:"ttl_seconds"`

	ParamOverrideTemplate map[string]interface{} `json:"param_override_template,omitempty"`

	SkipRetryOnFailure bool `json:"skip_retry_on_failure"`

	IncludeUsingGroup bool `json:"include_using_group"`
	IncludeModelName  bool `json:"include_model_name"`
	IncludeRuleName   bool `json:"include_rule_name"`
}

type ChannelAffinitySetting struct {
	Enabled           bool                  `json:"enabled"`
	SwitchOnSuccess   bool                  `json:"switch_on_success"`
	MaxEntries        int                   `json:"max_entries"`
	DefaultTTLSeconds int                   `json:"default_ttl_seconds"`
	Rules             []ChannelAffinityRule `json:"rules"`
}

var CodexCliPassThroughHeaders = []string{
	"Originator",
	"Session_id",
	"Session-Id",
	"Thread-Id",
	"X-Codex-Beta-Features",
	"X-Codex-Installation-Id",
	"X-Codex-Parent-Thread-Id",
	"X-Codex-Turn-Metadata",
	"X-Codex-Turn-State",
	"X-Codex-Window-Id",
	"X-Client-Request-Id",
}

var CodexDesktopPassThroughHeaders = append([]string(nil), CodexCliPassThroughHeaders...)

// IsCodexDynamicRequestHeader identifies request metadata that must be
// forwarded from the real Codex client when a Codex header profile is used.
// Originator is intentionally excluded: it is a static profile identity, while
// session/thread/request identifiers and X-Codex-* values are per-request.
func IsCodexDynamicRequestHeader(headerName string) bool {
	normalized := strings.ToLower(strings.TrimSpace(headerName))
	if strings.HasPrefix(normalized, "x-codex-") {
		return true
	}
	switch normalized {
	case "session_id", "session-id", "thread-id", "x-client-request-id":
		return true
	default:
		return false
	}
}

var ClaudeCliPassThroughHeaders = []string{
	"X-Claude-Code-Session-Id",
	"X-Stainless-Arch",
	"X-Stainless-Lang",
	"X-Stainless-OS",
	"X-Stainless-Package-Version",
	"X-Stainless-Retry-Count",
	"X-Stainless-Runtime",
	"X-Stainless-Runtime-Version",
	"X-Stainless-Timeout",
	"X-App",
	"Anthropic-Beta",
	"Anthropic-Dangerous-Direct-Browser-Access",
	"Anthropic-Version",
}

var QwenCodeCliPassThroughHeaders = []string{
	"X-Stainless-Arch",
	"X-Stainless-Lang",
	"X-Stainless-OS",
	"X-Stainless-Package-Version",
	"X-Stainless-Retry-Count",
	"X-Stainless-Runtime",
	"X-Stainless-Runtime-Version",
}

var DroidCliPassThroughHeaders = []string{
	"X-Stainless-Arch",
	"X-Stainless-Lang",
	"X-Stainless-OS",
	"X-Stainless-Package-Version",
	"X-Stainless-Retry-Count",
	"X-Stainless-Runtime",
	"X-Stainless-Runtime-Version",
}

var GeminiCliPassThroughHeaders = []string{
	"X-Goog-Api-Client",
}

var HeaderProfilePassThroughHeaders = map[string][]string{
	"codex-cli":     CodexCliPassThroughHeaders,
	"codex-desktop": CodexDesktopPassThroughHeaders,
	"claude-code":   ClaudeCliPassThroughHeaders,
	"qwen-code":     QwenCodeCliPassThroughHeaders,
	"gemini-cli":    GeminiCliPassThroughHeaders,
	"droid":         DroidCliPassThroughHeaders,
}

func buildPassHeaderTemplate(headers []string) map[string]interface{} {
	clonedHeaders := make([]string, 0, len(headers))
	clonedHeaders = append(clonedHeaders, headers...)
	return map[string]interface{}{
		"operations": []map[string]interface{}{
			{
				"mode":        "pass_headers",
				"value":       clonedHeaders,
				"keep_origin": true,
			},
		},
	}
}

func BuildCodexHeaderPassthroughTemplate(headers []string) map[string]interface{} {
	clonedHeaders := make([]string, 0, len(headers))
	clonedHeaders = append(clonedHeaders, headers...)
	return map[string]interface{}{
		"operations": []map[string]interface{}{
			{
				"mode":        "pass_headers",
				"value":       clonedHeaders,
				"keep_origin": true,
			},
			{
				"mode":        "copy_header",
				"from":        "X-Client-Request-Id",
				"to":          "Session_id",
				"keep_origin": true,
			},
		},
	}
}

var channelAffinitySetting = ChannelAffinitySetting{
	Enabled:           true,
	SwitchOnSuccess:   true,
	MaxEntries:        100_000,
	DefaultTTLSeconds: 3600,
	Rules: []ChannelAffinityRule{
		{
			Name:       ResponsesEncryptedContentAffinityRuleName,
			ModelRegex: []string{"^gpt-.*$"},
			PathRegex:  []string{"/v1/responses"},
			KeySources: []ChannelAffinityKeySource{
				{Type: "responses_encrypted_content"},
			},
			ValueRegex:         "",
			TTLSeconds:         ResponsesEncryptedContentAffinityTTLSeconds,
			SkipRetryOnFailure: true,
			IncludeUsingGroup:  true,
			IncludeRuleName:    true,
			UserAgentInclude:   nil,
		},
		{
			Name:       "codex cli trace",
			ModelRegex: []string{"^gpt-.*$"},
			PathRegex:  []string{"/v1/responses"},
			KeySources: []ChannelAffinityKeySource{
				{Type: "gjson", Path: "prompt_cache_key"},
			},
			ValueRegex:         "",
			TTLSeconds:         0,
			SkipRetryOnFailure: true,
			IncludeUsingGroup:  true,
			IncludeRuleName:    true,
			UserAgentInclude:   nil,
		},
		{
			Name:       "claude cli trace",
			ModelRegex: []string{"^claude-.*$"},
			PathRegex:  []string{"/v1/messages"},
			KeySources: []ChannelAffinityKeySource{
				{Type: "gjson", Path: "metadata.user_id"},
			},
			ValueRegex:         "",
			TTLSeconds:         0,
			SkipRetryOnFailure: true,
			IncludeUsingGroup:  true,
			IncludeRuleName:    true,
			UserAgentInclude:   nil,
		},
	},
}

func init() {
	config.GlobalConfig.Register("channel_affinity_setting", &channelAffinitySetting)
}

// GetChannelAffinitySetting 指针 getter 只保留给测试播种配置；生产代码读
// ChannelAffinitySettingSnapshot，避免与写锁内的逐字段重写构成数据竞争。
func GetChannelAffinitySetting() *ChannelAffinitySetting {
	return &channelAffinitySetting
}

// ChannelAffinitySettingSnapshot 返回当前渠道亲和配置的浅拷贝。Rules 切片在
// 写入时整体替换，旧切片不再被改写，浅拷贝足以让调用方在锁外安全读取。
func ChannelAffinitySettingSnapshot() ChannelAffinitySetting {
	if snapshot, ok := config.SnapshotOf[ChannelAffinitySetting]("channel_affinity_setting"); ok {
		return snapshot
	}
	return channelAffinitySetting
}
