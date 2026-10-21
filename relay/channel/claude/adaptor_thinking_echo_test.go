package claude

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func assistantToolUseMessages(request *dto.ClaudeRequest) []dto.ClaudeMessage {
	messages := make([]dto.ClaudeMessage, 0, len(request.Messages))
	for _, message := range request.Messages {
		if message.Role != "assistant" {
			continue
		}
		blocks, err := message.ParseContent()
		if err != nil {
			continue
		}
		for _, block := range blocks {
			if block.Type == "tool_use" {
				messages = append(messages, message)
				break
			}
		}
	}
	return messages
}

func blockTypes(t *testing.T, message dto.ClaudeMessage) []string {
	t.Helper()
	blocks, err := message.ParseContent()
	require.NoError(t, err)
	types := make([]string, 0, len(blocks))
	for _, block := range blocks {
		types = append(types, block.Type)
	}
	return types
}

func deepSeekAnthropicInfo(model string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		OriginModelName: model,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         160,
			ChannelType:       14,
			UpstreamModelName: model,
		},
	}
}

// DeepSeek's Anthropic-compatible endpoint rejects a tool loop whose last
// assistant turn carries tool_use without a thinking block, even when the
// request never asked for thinking. The gateway has to add the empty block
// itself when an OpenAI-protocol turn produced a bare tool_use history.
func TestConvertClaudeRequestAddsEmptyThinkingToBareToolUseForDeepSeek(t *testing.T) {
	request := &dto.ClaudeRequest{
		Model: "deepseek-v4-flash",
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "run echo hi"},
			{
				Role: "assistant",
				Content: []any{
					map[string]any{
						"type":  "tool_use",
						"id":    "toolu_bare",
						"name":  "Bash",
						"input": map[string]any{"command": "echo hi"},
					},
				},
			},
			{
				Role: "user",
				Content: []any{
					map[string]any{
						"type":        "tool_result",
						"tool_use_id": "toolu_bare",
						"content":     "hi",
					},
				},
			},
		},
	}

	converted, err := (&Adaptor{}).ConvertClaudeRequest(nil, deepSeekAnthropicInfo("deepseek-v4-flash"), request)
	require.NoError(t, err)
	claudeRequest, ok := converted.(*dto.ClaudeRequest)
	require.True(t, ok)

	assistants := assistantToolUseMessages(claudeRequest)
	require.Len(t, assistants, 1)
	require.Equal(t, []string{"thinking", "tool_use"}, blockTypes(t, assistants[0]))
	blocks, err := assistants[0].ParseContent()
	require.NoError(t, err)
	require.NotNil(t, blocks[0].Thinking)
	require.Empty(t, *blocks[0].Thinking)
	require.Empty(t, blocks[0].Signature)
}

func TestConvertClaudeRequestLeavesExistingThinkingBlockUntouched(t *testing.T) {
	request := &dto.ClaudeRequest{
		Model: "deepseek-v4-flash",
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "run echo hi"},
			{
				Role: "assistant",
				Content: []any{
					map[string]any{
						"type":      "thinking",
						"thinking":  "",
						"signature": "upstream-signature",
					},
					map[string]any{
						"type":  "tool_use",
						"id":    "toolu_signed",
						"name":  "Bash",
						"input": map[string]any{"command": "echo hi"},
					},
				},
			},
		},
	}

	converted, err := (&Adaptor{}).ConvertClaudeRequest(nil, deepSeekAnthropicInfo("deepseek-v4-flash"), request)
	require.NoError(t, err)
	claudeRequest, ok := converted.(*dto.ClaudeRequest)
	require.True(t, ok)

	assistants := assistantToolUseMessages(claudeRequest)
	require.Len(t, assistants, 1)
	require.Equal(t, []string{"thinking", "tool_use"}, blockTypes(t, assistants[0]))
	blocks, err := assistants[0].ParseContent()
	require.NoError(t, err)
	require.Equal(t, "upstream-signature", blocks[0].Signature, "an existing thinking block must be forwarded verbatim")
}

// Non-DeepSeek Anthropic upstreams do not impose the rule, so the gateway must
// not rewrite their requests.
func TestConvertClaudeRequestDoesNotAddThinkingForOtherModels(t *testing.T) {
	for _, model := range []string{"claude-sonnet-4-5-20250929", "gpt-6-astra", "kimi-k3"} {
		request := &dto.ClaudeRequest{
			Model: model,
			Messages: []dto.ClaudeMessage{
				{Role: "user", Content: "run echo hi"},
				{
					Role: "assistant",
					Content: []any{
						map[string]any{
							"type":  "tool_use",
							"id":    "toolu_bare",
							"name":  "Bash",
							"input": map[string]any{"command": "echo hi"},
						},
					},
				},
			},
		}

		converted, err := (&Adaptor{}).ConvertClaudeRequest(nil, deepSeekAnthropicInfo(model), request)
		require.NoError(t, err)
		claudeRequest, ok := converted.(*dto.ClaudeRequest)
		require.True(t, ok)
		assistants := assistantToolUseMessages(claudeRequest)
		require.Len(t, assistants, 1)
		require.Equal(t, []string{"tool_use"}, blockTypes(t, assistants[0]), "model %q must be left alone", model)
	}
}

// A bare tool_use history is only a problem for the thinking-capable upstream
// when tools are in play; a plain assistant text turn must stay untouched.
func TestConvertClaudeRequestIgnoresAssistantTextTurns(t *testing.T) {
	request := &dto.ClaudeRequest{
		Model: "deepseek-v4-flash",
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi"},
		},
	}

	converted, err := (&Adaptor{}).ConvertClaudeRequest(nil, deepSeekAnthropicInfo("deepseek-v4-flash"), request)
	require.NoError(t, err)
	claudeRequest, ok := converted.(*dto.ClaudeRequest)
	require.True(t, ok)
	require.Equal(t, "hi", claudeRequest.Messages[1].Content)
}
