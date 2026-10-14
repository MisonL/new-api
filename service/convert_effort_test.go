package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestClaudeToOpenAIRequestForwardsEffortAsReasoningEffort(t *testing.T) {
	converted, err := ClaudeToOpenAIRequest(dto.ClaudeRequest{
		Model:        "deepseek-v4-flash",
		OutputConfig: common.RawMessage(`{"effort":"high"}`),
	}, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI},
	})

	require.NoError(t, err)
	require.Equal(t, "high", converted.ReasoningEffort)
}

func TestClaudeToOpenAIRequestWithoutEffortLeavesReasoningEffortEmpty(t *testing.T) {
	converted, err := ClaudeToOpenAIRequest(dto.ClaudeRequest{
		Model: "deepseek-v4-flash",
	}, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI},
	})

	require.NoError(t, err)
	require.Empty(t, converted.ReasoningEffort)
}

func TestClaudeToOpenAIRequestOpenRouterEffortMappingUnchanged(t *testing.T) {
	converted, err := ClaudeToOpenAIRequest(dto.ClaudeRequest{
		Model:        "deepseek-v4-flash",
		OutputConfig: common.RawMessage(`{"effort":"high"}`),
	}, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter},
	})

	require.NoError(t, err)
	require.Empty(t, converted.ReasoningEffort)
	require.JSONEq(t, `"high"`, string(converted.Verbosity))
}
