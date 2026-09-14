package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
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
			name:        "sub2api http is unsupported",
			channelType: constant.ChannelTypeOpenAI,
			settings: ChannelOtherSettings{
				ResponsesUpstreamProfile: ResponsesUpstreamProfileSub2APIHTTP,
			},
			want: ResponsesEncryptedReasoningReplayCapabilityUnsupported,
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

			raw, err := common.Marshal(tt.settings)
			require.NoError(t, err)
			var roundTripped ChannelOtherSettings
			require.NoError(t, common.Unmarshal(raw, &roundTripped))
			require.Equal(t, tt.want, roundTripped.ResolveResponsesEncryptedReasoningReplayCapability(tt.channelType))
		})
	}
}

func TestResponsesCompactionItemPassthroughCapability(t *testing.T) {
	unsupported := false
	supported := true
	tests := []struct {
		name        string
		channelType int
		settings    ChannelOtherSettings
		want        bool
	}{
		{
			name:        "unconfigured openai supports passthrough by default",
			channelType: constant.ChannelTypeOpenAI,
			want:        true,
		},
		{
			name:        "generic openai does not support passthrough",
			channelType: constant.ChannelTypeOpenAI,
			settings: ChannelOtherSettings{
				ResponsesUpstreamProfile: ResponsesUpstreamProfileGenericOpenAI,
			},
			want: false,
		},
		{
			name:        "sub2api http does not support passthrough",
			channelType: constant.ChannelTypeOpenAI,
			settings: ChannelOtherSettings{
				ResponsesUpstreamProfile: ResponsesUpstreamProfileSub2APIHTTP,
			},
			want: false,
		},
		{
			name:        "official openai supports passthrough",
			channelType: constant.ChannelTypeOpenAI,
			settings: ChannelOtherSettings{
				ResponsesUpstreamProfile: ResponsesUpstreamProfileOfficialOpenAI,
			},
			want: true,
		},
		{
			name:        "native codex supports passthrough",
			channelType: constant.ChannelTypeCodex,
			want:        true,
		},
		{
			name:        "generic proxy is unsupported",
			channelType: constant.ChannelTypeOpenAI,
			settings: ChannelOtherSettings{
				ResponsesUpstreamProfile: ResponsesUpstreamProfileGenericProxy,
			},
			want: false,
		},
		{
			name:        "explicit registry disables passthrough",
			channelType: constant.ChannelTypeOpenAI,
			settings: ChannelOtherSettings{
				ResponsesUpstreamProfile: ResponsesUpstreamProfileOfficialOpenAI,
				ResponsesCapabilityRegistry: &ResponsesChannelCapabilityRegistry{
					SupportsCompactionItemPassthrough: &unsupported,
				},
			},
			want: false,
		},
		{
			name:        "explicit registry verifies generic passthrough",
			channelType: constant.ChannelTypeOpenAI,
			settings: ChannelOtherSettings{
				ResponsesUpstreamProfile: ResponsesUpstreamProfileGenericOpenAI,
				ResponsesCapabilityRegistry: &ResponsesChannelCapabilityRegistry{
					SupportsCompactionItemPassthrough: &supported,
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := tt.settings.ResolveResponsesChannelCapability(tt.channelType)
			require.Equal(t, tt.want, snapshot.SupportsCompactionItemPassthrough)

			raw, err := common.Marshal(tt.settings)
			require.NoError(t, err)
			var roundTripped ChannelOtherSettings
			require.NoError(t, common.Unmarshal(raw, &roundTripped))
			require.Equal(t, tt.want, roundTripped.ResolveResponsesChannelCapability(tt.channelType).SupportsCompactionItemPassthrough)
		})
	}
}

func TestNilChannelOtherSettingsKeepsCompactionItemPassthroughDefault(t *testing.T) {
	var settings *ChannelOtherSettings

	require.True(t, settings.ResolveResponsesChannelCapability(constant.ChannelTypeOpenAI).SupportsCompactionItemPassthrough)
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
