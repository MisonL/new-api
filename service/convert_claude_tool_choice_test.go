package service

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

// Claude 请求的 tool_choice 在转成 OpenAI 请求时必须一并转发。此前
// ClaudeToOpenAIRequest 只映射 tools，丢掉了 tool_choice：Claude 客户端用
// {"type":"tool","name":"x"} 强制某个工具或 {"type":"none"} 禁止调用时，上游
// 按默认 auto 执行（第十轮待确认项，本轮补测试钉住映射）。
func newClaudeToolChoiceRelayInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"},
	}
}

func TestClaudeToOpenAIRequestForwardsToolChoice(t *testing.T) {
	base := dto.ClaudeRequest{
		Model:     "claude-test",
		MaxTokens: func() *uint { v := uint(256); return &v }(),
		Tools: []any{map[string]any{
			"name":        "get_weather",
			"description": "Get weather by city",
			"input_schema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"city": map[string]any{"type": "string"}},
			},
		}},
	}

	t.Run("forced tool", func(t *testing.T) {
		req := base
		req.ToolChoice = map[string]any{"type": "tool", "name": "get_weather"}
		converted, err := ClaudeToOpenAIRequest(req, newClaudeToolChoiceRelayInfo())
		require.NoError(t, err)
		require.NotNil(t, converted.ToolChoice, "tool_choice 不得丢失")

		choice, ok := converted.ToolChoice.(map[string]any)
		require.True(t, ok, "tool_choice 应为对象，实际 %T", converted.ToolChoice)
		require.Equal(t, "function", choice["type"])
		function, ok := choice["function"].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "get_weather", function["name"])
	})

	t.Run("none", func(t *testing.T) {
		req := base
		req.ToolChoice = map[string]any{"type": "none"}
		converted, err := ClaudeToOpenAIRequest(req, newClaudeToolChoiceRelayInfo())
		require.NoError(t, err)
		require.Equal(t, "none", converted.ToolChoice)
	})

	t.Run("auto passthrough", func(t *testing.T) {
		req := base
		req.ToolChoice = map[string]any{"type": "auto"}
		converted, err := ClaudeToOpenAIRequest(req, newClaudeToolChoiceRelayInfo())
		require.NoError(t, err)
		require.Equal(t, "auto", converted.ToolChoice)
	})

	t.Run("absent stays absent", func(t *testing.T) {
		converted, err := ClaudeToOpenAIRequest(base, newClaudeToolChoiceRelayInfo())
		require.NoError(t, err)
		require.Nil(t, converted.ToolChoice)
	})
}

// Claude 的 {"type":"any"} 表示"必须调用某个工具"，OpenAI 的等价物是
// "required"。"any" 不在 OpenAI tool_choice 的封闭枚举里，原样透传要么被
// 上游拒绝（400），要么被当作未知值退化成 auto——两种结果都让"强制工具"
// 的语义丢失。本仓库的反向映射（relay/channel/claude 的 mapToolChoice）
// 已经把 OpenAI 的 "required" 映射成 Claude 的 "any"，这一对是既定契约。
func TestClaudeToOpenAIRequestMapsAnyToolChoiceToRequired(t *testing.T) {
	base := dto.ClaudeRequest{
		Model:     "claude-test",
		MaxTokens: func() *uint { v := uint(256); return &v }(),
		Tools: []any{map[string]any{
			"name":         "get_weather",
			"description":  "Get weather by city",
			"input_schema": map[string]any{"type": "object"},
		}},
	}

	req := base
	req.ToolChoice = map[string]any{"type": "any"}
	converted, err := ClaudeToOpenAIRequest(req, newClaudeToolChoiceRelayInfo())
	require.NoError(t, err)
	require.Equal(t, "required", converted.ToolChoice,
		"Claude 的 any 必须映射成 OpenAI 的 required")
}

// Claude 的 disable_parallel_tool_use 表示"不允许并行调用多个工具"，OpenAI
// 的等价物是 parallel_tool_calls=false。丢弃它会让上游按可并行执行，
// 与客户端的明确要求相反。
func TestClaudeToOpenAIRequestMapsDisableParallelToolUse(t *testing.T) {
	base := dto.ClaudeRequest{
		Model:     "claude-test",
		MaxTokens: func() *uint { v := uint(256); return &v }(),
		Tools: []any{map[string]any{
			"name":         "get_weather",
			"description":  "Get weather by city",
			"input_schema": map[string]any{"type": "object"},
		}},
	}

	req := base
	req.ToolChoice = map[string]any{"type": "any", "disable_parallel_tool_use": true}
	converted, err := ClaudeToOpenAIRequest(req, newClaudeToolChoiceRelayInfo())
	require.NoError(t, err)
	require.NotNil(t, converted.ParallelTooCalls, "parallel_tool_calls 不得被丢弃")
	require.False(t, *converted.ParallelTooCalls,
		"disable_parallel_tool_use 必须映射成 parallel_tool_calls=false")
}
