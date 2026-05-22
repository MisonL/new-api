package model

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestChannelOtherSettingsDefaultsResponsesCompactUnsupported(t *testing.T) {
	channel := &Channel{OtherSettings: `{}`}

	settings := channel.GetOtherSettings()

	require.False(t, settings.HasNativeResponsesCompact())
	require.Empty(t, settings.ResponsesCompactMode)
}

func TestChannelOtherSettingsResponsesCompactNativeRoundTrip(t *testing.T) {
	channel := &Channel{}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeNative,
	})

	settings := channel.GetOtherSettings()

	require.Equal(t, dto.ResponsesCompactModeNative, settings.ResponsesCompactMode)
	require.True(t, settings.HasNativeResponsesCompact())
}
