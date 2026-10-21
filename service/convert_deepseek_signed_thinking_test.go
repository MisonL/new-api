package service

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func signedEmptyThinkingRequest(model string, signature string) dto.ClaudeRequest {
	return dto.ClaudeRequest{
		Model: model,
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "run echo hi"},
			{
				Role: "assistant",
				Content: []any{
					map[string]any{
						"type":      "thinking",
						"thinking":  "",
						"signature": signature,
					},
					map[string]any{
						"type": "tool_use",
						"id":   "toolu_signed",
						"name": "Bash",
						"input": map[string]any{
							"command": "echo hi",
						},
					},
				},
			},
			{
				Role: "user",
				Content: []any{
					map[string]any{
						"type":        "tool_result",
						"tool_use_id": "toolu_signed",
						"content":     "hi",
					},
				},
			},
		},
	}
}

// DeepSeek's Anthropic endpoint stamps empty thinking blocks with a UUID
// signature (the display=omitted artifact) and Claude Code echoes it verbatim.
// The signature carries no recoverable state for this model family, so the
// request must survive the OpenAI-protocol hop as an explicit empty
// reasoning_content instead of failing as opaque.
func TestClaudeToOpenAIRequestTreatsDeepSeekSignedEmptyThinkingAsEmpty(t *testing.T) {
	claudeRequest := signedEmptyThinkingRequest("deepseek-v4-flash", "7c07fd2e-b711-410e-808d-ad2cdaee920f")

	openAIRequest, err := ClaudeToOpenAIRequest(claudeRequest, &relaycommon.RelayInfo{
		OriginModelName: "deepseek-v4-flash",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
		},
	})
	require.NoError(t, err)
	require.Len(t, openAIRequest.Messages, 3)
	require.NotNil(t, openAIRequest.Messages[1].ReasoningContent)
	require.Empty(t, *openAIRequest.Messages[1].ReasoningContent)
}

// Non-DeepSeek models keep the fail-closed rule: their signatures may be real
// opaque state that OpenAI reasoning_content cannot represent.
func TestClaudeToOpenAIRequestStillRejectsSignedEmptyThinkingForOtherModels(t *testing.T) {
	claudeRequest := signedEmptyThinkingRequest("claude-sonnet-4-5-20250929", "real-opaque-signature")

	_, err := ClaudeToOpenAIRequest(claudeRequest, &relaycommon.RelayInfo{
		OriginModelName: "claude-sonnet-4-5-20250929",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
		},
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "opaque thinking state")
}
