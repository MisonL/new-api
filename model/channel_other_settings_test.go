package model

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestChannelOtherSettingsDefaultsResponsesCompactConvert(t *testing.T) {
	channel := &Channel{OtherSettings: `{}`}

	settings := channel.GetOtherSettings()

	require.False(t, settings.HasNativeResponsesCompact())
	require.Empty(t, settings.ResponsesCompactMode)
	require.Equal(t, dto.ResponsesCompactModeConvert, settings.ResponsesCompactModeOrDefault())
	require.False(t, settings.HasDisabledResponsesCompact())
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
	require.False(t, settings.HasDisabledResponsesCompact())
}

func TestChannelOtherSettingsResponsesCompactCompatibilityModes(t *testing.T) {
	tests := []struct {
		name     string
		rawMode  dto.ResponsesCompactMode
		expected dto.ResponsesCompactMode
		disabled bool
	}{
		{
			name:     "disabled",
			rawMode:  dto.ResponsesCompactModeDisabled,
			expected: dto.ResponsesCompactModeDisabled,
			disabled: true,
		},
		{
			name:     "legacy unsupported",
			rawMode:  dto.ResponsesCompactModeUnsupported,
			expected: dto.ResponsesCompactModeDisabled,
			disabled: true,
		},
		{
			name:     "unknown",
			rawMode:  dto.ResponsesCompactMode("unexpected"),
			expected: dto.ResponsesCompactModeConvert,
			disabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := dto.ChannelOtherSettings{
				ResponsesCompactMode: tt.rawMode,
			}

			require.Equal(t, tt.expected, settings.ResponsesCompactModeOrDefault())
			require.Equal(t, tt.disabled, settings.HasDisabledResponsesCompact())
		})
	}
}
