package openaicompat

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

// The bridged usage must carry output_tokens_details, the spelling a Responses
// client reads. A chat upstream reports those numbers under
// completion_tokens_details, and leaving the Responses field absent made the
// bridged body differ in shape from a native Responses one.
func TestBuildResponsesUsageFromChatFillsOutputTokenDetails(t *testing.T) {
	resp := &dto.OpenAITextResponse{
		Usage: dto.Usage{
			PromptTokens:     10,
			CompletionTokens: 40,
			TotalTokens:      50,
			CompletionTokenDetails: dto.OutputTokenDetails{
				TextTokens:      30,
				ReasoningTokens: 10,
			},
		},
	}

	usage := buildResponsesUsageFromChat(resp)

	require.NotNil(t, usage.OutputTokensDetails)
	require.Equal(t, 30, usage.OutputTokensDetails.TextTokens)
	require.Equal(t, 10, usage.OutputTokensDetails.ReasoningTokens)
	require.NotNil(t, usage.InputTokensDetails)
	require.Equal(t, 10, usage.InputTokens)
	require.Equal(t, 40, usage.OutputTokens)
}

func TestBuildResponsesUsageFromChatKeepsExistingOutputTokenDetails(t *testing.T) {
	existing := &dto.OutputTokenDetails{TextTokens: 7, ReasoningTokens: 3}
	resp := &dto.OpenAITextResponse{
		Usage: dto.Usage{
			PromptTokens:        10,
			CompletionTokens:    40,
			TotalTokens:         50,
			OutputTokensDetails: existing,
			CompletionTokenDetails: dto.OutputTokenDetails{
				TextTokens: 999,
			},
		},
	}

	usage := buildResponsesUsageFromChat(resp)

	require.Same(t, existing, usage.OutputTokensDetails)
	require.Equal(t, 7, usage.OutputTokensDetails.TextTokens)
}
