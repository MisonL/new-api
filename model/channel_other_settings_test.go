package model

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestChannelOtherSettingsDefaultsResponsesCompactNative(t *testing.T) {
	channel := &Channel{OtherSettings: `{}`}

	settings := channel.GetOtherSettings()

	require.Empty(t, settings.ResponsesCompactMode)
	require.Equal(t, dto.ResponsesCompactModeNative, settings.ResponsesCompactModeOrDefault())
	require.True(t, settings.HasNativeResponsesCompact())
}

func TestChannelOtherSettingsResponsesCompactNativeRoundTrip(t *testing.T) {
	channel := &Channel{}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeNative,
	})

	settings := channel.GetOtherSettings()

	require.Equal(t, dto.ResponsesCompactModeNative, settings.ResponsesCompactMode)
	require.True(t, settings.HasNativeResponsesCompact())
	require.Equal(t, dto.ResponsesCompactModeNative, settings.ResponsesCompactModeOrDefault())
}

func TestChannelOtherSettingsResponsesCompactSyntheticRoundTrip(t *testing.T) {
	channel := &Channel{}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeSynthetic,
	})

	settings := channel.GetOtherSettings()

	require.Equal(t, dto.ResponsesCompactModeSynthetic, settings.ResponsesCompactMode)
	require.True(t, settings.HasSyntheticResponsesCompact())
	require.Equal(t, dto.ResponsesCompactModeSynthetic, settings.ResponsesCompactModeOrDefault())
	require.False(t, settings.HasNativeResponsesCompact())
}

func TestChannelOtherSettingsResponsesCompactLegacyModesNormalizeSafely(t *testing.T) {
	tests := []struct {
		name     string
		rawMode  dto.ResponsesCompactMode
		expected dto.ResponsesCompactMode
	}{
		{
			name:     "legacy convert",
			rawMode:  dto.ResponsesCompactMode("convert"),
			expected: dto.ResponsesCompactModeSynthetic,
		},
		{
			name:     "legacy auto",
			rawMode:  dto.ResponsesCompactMode("auto"),
			expected: dto.ResponsesCompactModeNative,
		},
		{
			name:     "legacy disabled",
			rawMode:  dto.ResponsesCompactMode("disabled"),
			expected: dto.ResponsesCompactModeNative,
		},
		{
			name:     "legacy unsupported",
			rawMode:  dto.ResponsesCompactMode("unsupported"),
			expected: dto.ResponsesCompactModeNative,
		},
		{
			name:     "unknown",
			rawMode:  dto.ResponsesCompactMode("unexpected"),
			expected: dto.ResponsesCompactModeNative,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := dto.ChannelOtherSettings{
				ResponsesCompactMode: tt.rawMode,
			}

			require.Equal(t, tt.expected, settings.ResponsesCompactModeOrDefault())
			require.Equal(t, tt.expected == dto.ResponsesCompactModeNative, settings.HasNativeResponsesCompact())
			require.Equal(t, tt.expected == dto.ResponsesCompactModeSynthetic, settings.HasSyntheticResponsesCompact())
		})
	}
}
