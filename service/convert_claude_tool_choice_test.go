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
