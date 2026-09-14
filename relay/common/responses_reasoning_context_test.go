package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestResolveResponsesReasoningContextCapabilityDecision(t *testing.T) {
	unsupported := false
	tests := []struct {
		name       string
		info       *RelayInfo
		wantCap    dto.ResponsesReasoningContextCapability
		wantAction string
	}{
		{
			name: "explicit unsupported",
			info: &RelayInfo{ChannelMeta: &ChannelMeta{
				ChannelType: constant.ChannelTypeOpenAI,
				ChannelOtherSettings: dto.ChannelOtherSettings{
					ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{SupportsReasoningContext: &unsupported},
				},
			}},
			wantCap:    dto.ResponsesReasoningContextCapabilityUnsupported,
			wantAction: ResponsesReasoningContextActionStrippedUnsupported,
		},
		{
			name:       "codex default support",
			info:       &RelayInfo{ChannelMeta: &ChannelMeta{ChannelType: constant.ChannelTypeCodex}},
			wantCap:    dto.ResponsesReasoningContextCapabilitySupported,
			wantAction: ResponsesReasoningContextActionForwardedSupported,
		},
		{
			name:       "unknown generic profile",
			info:       &RelayInfo{ChannelMeta: &ChannelMeta{ChannelType: constant.ChannelTypeOpenAI}},
			wantCap:    dto.ResponsesReasoningContextCapabilityUnknown,
			wantAction: ResponsesReasoningContextActionForwardedUnverified,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := ResolveResponsesReasoningContextCapabilityDecision(tt.info, "gpt-5.6-sol")
			require.Equal(t, tt.wantCap, decision.Capability)
			require.Equal(t, tt.wantAction, decision.Action)
		})
	}
}
