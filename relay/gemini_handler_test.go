package relay

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestApplyGeminiNoThinkingBillingModelPreservesMappedUpstreamModel(t *testing.T) {
	info := &relaycommon.RelayInfo{
		OriginModelName: "client-gemini",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "provider/gemini-2.5-pro",
			IsModelMapped:     true,
		},
	}

	applyGeminiNoThinkingBillingModel(info, func(modelName string) bool {
		return modelName == "client-gemini-nothinking"
	})

	require.Equal(t, "client-gemini-nothinking", info.OriginModelName)
	require.Equal(t, "provider/gemini-2.5-pro", info.UpstreamModelName)
}

func TestApplyGeminiNoThinkingBillingModelKeepsUnmappedCompatibility(t *testing.T) {
	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-2.5-pro",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-2.5-pro",
		},
	}

	applyGeminiNoThinkingBillingModel(info, func(modelName string) bool {
		return modelName == "gemini-2.5-pro-nothinking"
	})

	require.Equal(t, "gemini-2.5-pro-nothinking", info.OriginModelName)
	require.Equal(t, "gemini-2.5-pro-nothinking", info.UpstreamModelName)
}

func TestApplyGeminiNoThinkingBillingModelDoesNotChangeWithoutBillingConfig(t *testing.T) {
	info := &relaycommon.RelayInfo{
		OriginModelName: "client-gemini",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "provider/gemini-2.5-pro",
			IsModelMapped:     true,
		},
	}

	applyGeminiNoThinkingBillingModel(info, func(string) bool { return false })

	require.Equal(t, "client-gemini", info.OriginModelName)
	require.Equal(t, "provider/gemini-2.5-pro", info.UpstreamModelName)
}
