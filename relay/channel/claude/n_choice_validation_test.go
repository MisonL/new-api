package claude

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func intPtr(v int) *int { return &v }

// The Claude Messages API returns exactly one completion. Before this check the
// gateway forwarded the request with n silently dropped and answered with a
// single choice, so a client asking for three got one and no indication why.
func TestRequestOpenAI2ClaudeMessageRejectsMultipleChoices(t *testing.T) {
	_, err := RequestOpenAI2ClaudeMessage(nil, dto.GeneralOpenAIRequest{
		Model:     "claude-sonnet-5",
		N:         intPtr(3),
		MaxTokens: common.GetPointer(uint(16)),
		Messages: []dto.Message{
			{Role: "user", Content: "hi"},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "n>1 is not supported by the Claude Messages API")
}

func TestRequestOpenAI2ClaudeMessageAcceptsSingleChoice(t *testing.T) {
	for _, n := range []*int{nil, intPtr(0), intPtr(1)} {
		_, err := RequestOpenAI2ClaudeMessage(nil, dto.GeneralOpenAIRequest{
			Model:     "claude-sonnet-5",
			N:         n,
			MaxTokens: common.GetPointer(uint(16)),
			Messages: []dto.Message{
				{Role: "user", Content: "hi"},
			},
		})
		require.NoError(t, err)
	}
}
