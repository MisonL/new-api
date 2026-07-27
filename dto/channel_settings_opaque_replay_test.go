package dto

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestResponsesEncryptedReasoningReplayCapability(t *testing.T) {
	unsupported := false
	supported := true
	tests := []struct {
		name        string
		channelType int
		settings    ChannelOtherSettings
		want        ResponsesEncryptedReasoningReplayCapability
	}{
		{
			name:        "unconfigured openai remains unknown",
			channelType: constant.ChannelTypeOpenAI,
			want:        ResponsesEncryptedReasoningReplayCapabilityUnknown,
		},
		{
			name:        "generic openai remains unknown",
			channelType: constant.ChannelTypeOpenAI,
			settings: ChannelOtherSettings{
				ResponsesUpstreamProfile: ResponsesUpstreamProfileGenericOpenAI,
			},
			want: ResponsesEncryptedReasoningReplayCapabilityUnknown,
		},
		{
			name:        "official openai supports replay",
			channelType: constant.ChannelTypeOpenAI,
			settings: ChannelOtherSettings{
				ResponsesUpstreamProfile: ResponsesUpstreamProfileOfficialOpenAI,
			},
			want: ResponsesEncryptedReasoningReplayCapabilitySupported,
		},
		{
			name:        "native codex supports replay",
			channelType: constant.ChannelTypeCodex,
			want:        ResponsesEncryptedReasoningReplayCapabilitySupported,
		},
		{
			name:        "sub2api http remains unknown until verified",
			channelType: constant.ChannelTypeOpenAI,
			settings: ChannelOtherSettings{
				ResponsesUpstreamProfile: ResponsesUpstreamProfileSub2APIHTTP,
			},
			want: ResponsesEncryptedReasoningReplayCapabilityUnknown,
		},
		{
			name:        "legacy strip setting is unsupported",
			channelType: constant.ChannelTypeOpenAI,
			settings: ChannelOtherSettings{
				ResponsesUpstreamProfile:   ResponsesUpstreamProfileOfficialOpenAI,
				StripCodexEncryptedContext: true,
			},
			want: ResponsesEncryptedReasoningReplayCapabilityUnsupported,
		},
		{
			name:        "explicit registry disables replay",
			channelType: constant.ChannelTypeOpenAI,
			settings: ChannelOtherSettings{
				ResponsesUpstreamProfile: ResponsesUpstreamProfileOfficialOpenAI,
				ResponsesCapabilityRegistry: &ResponsesChannelCapabilityRegistry{
					SupportsEncryptedReasoningReplay: &unsupported,
				},
			},
			want: ResponsesEncryptedReasoningReplayCapabilityUnsupported,
		},
		{
			name:        "explicit registry verifies generic replay",
			channelType: constant.ChannelTypeOpenAI,
			settings: ChannelOtherSettings{
				ResponsesUpstreamProfile: ResponsesUpstreamProfileGenericOpenAI,
				ResponsesCapabilityRegistry: &ResponsesChannelCapabilityRegistry{
					SupportsEncryptedReasoningReplay: &supported,
				},
			},
			want: ResponsesEncryptedReasoningReplayCapabilitySupported,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.settings.ResolveResponsesEncryptedReasoningReplayCapability(tt.channelType)
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.want == ResponsesEncryptedReasoningReplayCapabilityUnsupported, tt.settings.ExplicitlyDisablesResponsesEncryptedReasoningReplay(tt.channelType))

			raw, err := json.Marshal(tt.settings)
			require.NoError(t, err)
			var roundTripped ChannelOtherSettings
			require.NoError(t, json.Unmarshal(raw, &roundTripped))
			require.Equal(t, tt.want, roundTripped.ResolveResponsesEncryptedReasoningReplayCapability(tt.channelType))
		})
	}
}

func TestResponsesEncryptedReasoningReplaySnapshotHonorsLegacyStripPrecedence(t *testing.T) {
	supported := true
	settings := ChannelOtherSettings{
		ResponsesUpstreamProfile:   ResponsesUpstreamProfileOfficialOpenAI,
		StripCodexEncryptedContext: true,
		ResponsesCapabilityRegistry: &ResponsesChannelCapabilityRegistry{
			SupportsEncryptedReasoningReplay: &supported,
		},
	}

	snapshot := settings.ResolveResponsesChannelCapability(constant.ChannelTypeOpenAI)

	require.Equal(t, ResponsesEncryptedReasoningReplayCapabilityUnsupported, settings.ResolveResponsesEncryptedReasoningReplayCapability(constant.ChannelTypeOpenAI))
	require.Equal(t, ResponsesEncryptedReasoningReplayCapabilityUnsupported, snapshot.EncryptedReasoningReplayCapability)
	require.NotNil(t, snapshot.SupportsEncryptedReasoningReplay)
	require.False(t, *snapshot.SupportsEncryptedReasoningReplay)
}

func TestResponsesChannelCapabilitySnapshotKeepsIndependentReplayAndStreamOptionsValues(t *testing.T) {
	unsupported := false
	supported := true
	settings := ChannelOtherSettings{
		ResponsesCapabilityRegistry: &ResponsesChannelCapabilityRegistry{
			SupportsStreamOptionsIncludeUsage: &unsupported,
			SupportsEncryptedReasoningReplay:  &supported,
		},
	}

	snapshot := settings.ResolveResponsesChannelCapability(constant.ChannelTypeOpenAI)

	require.NotNil(t, snapshot.SupportsStreamOptionsIncludeUsage)
	require.False(t, *snapshot.SupportsStreamOptionsIncludeUsage)
	require.NotNil(t, snapshot.SupportsEncryptedReasoningReplay)
	require.True(t, *snapshot.SupportsEncryptedReasoningReplay)
	require.Equal(t, ResponsesEncryptedReasoningReplayCapabilitySupported, snapshot.EncryptedReasoningReplayCapability)
}
