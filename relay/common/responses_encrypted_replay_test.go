package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestSupportsNativeOpaqueResponsesCompact(t *testing.T) {
	supported := true
	unsupported := false
	tests := []struct {
		name        string
		channelType int
		settings    dto.ChannelOtherSettings
		want        bool
	}{
		{
			name:        "openai native default",
			channelType: constant.ChannelTypeOpenAI,
			want:        true,
		},
		{
			name:        "openai synthetic",
			channelType: constant.ChannelTypeOpenAI,
			settings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeSynthetic,
			},
		},
		{
			name:        "openai disabled",
			channelType: constant.ChannelTypeOpenAI,
			settings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeDisabled,
			},
		},
		{
			name:        "codex native default",
			channelType: constant.ChannelTypeCodex,
			want:        true,
		},
		{
			name:        "codex explicitly unsupported",
			channelType: constant.ChannelTypeCodex,
			settings: dto.ChannelOtherSettings{
				ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
					SupportsResponsesCompact: &unsupported,
				},
			},
		},
		{
			name:        "codex synthetic",
			channelType: constant.ChannelTypeCodex,
			settings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeSynthetic,
			},
		},
		{
			name:        "azure unknown",
			channelType: constant.ChannelTypeAzure,
		},
		{
			name:        "azure explicitly supported",
			channelType: constant.ChannelTypeAzure,
			settings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeNative,
				ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
					SupportsResponsesCompact: &supported,
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, SupportsNativeOpaqueResponsesCompact(tt.channelType, tt.settings))
		})
	}
}
