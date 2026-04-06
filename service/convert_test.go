package service

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestNormalizeCacheCreationSplitAddsRemainderTo5m(t *testing.T) {
	tokens5m, tokens1h := NormalizeCacheCreationSplit(50, 10, 20)
	require.Equal(t, 30, tokens5m)
	require.Equal(t, 20, tokens1h)
}

func TestStreamResponseOpenAI2ClaudeEmitsUsageForUsageOnlyFinalChunk(t *testing.T) {
	finishReason := "stop"
	info := &relaycommon.RelayInfo{
		ClaudeConvertInfo: &relaycommon.ClaudeConvertInfo{},
	}

	firstChunk := &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				FinishReason: &finishReason,
			},
		},
	}

	responses := StreamResponseOpenAI2Claude(firstChunk, info)
	require.Len(t, responses, 0)
	require.Equal(t, "stop", info.FinishReason)
	require.False(t, info.ClaudeConvertInfo.Done)

	finalChunk := &dto.ChatCompletionsStreamResponse{
		Usage: &dto.Usage{
			PromptTokens:     100,
			CompletionTokens: 20,
			PromptTokensDetails: dto.InputTokenDetails{
				CachedCreationTokens: 50,
			},
		},
	}

	responses = StreamResponseOpenAI2Claude(finalChunk, info)
	require.Len(t, responses, 2)
	require.Equal(t, "message_delta", responses[0].Type)
	require.NotNil(t, responses[0].Usage)
	require.NotNil(t, responses[0].Delta)
	require.NotNil(t, responses[0].Delta.StopReason)
	require.Equal(t, "end_turn", *responses[0].Delta.StopReason)
	require.Equal(t, 50, responses[0].Usage.CacheCreation.Ephemeral5mInputTokens)
	require.Equal(t, "message_stop", responses[1].Type)
	require.True(t, info.ClaudeConvertInfo.Done)
}

