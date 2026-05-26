package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestChannelOtherSettingsDefaultsResponsesCompactAuto(t *testing.T) {
	channel := &Channel{OtherSettings: `{}`}

	settings := channel.GetOtherSettings()

	require.Empty(t, settings.ResponsesCompactMode)
	require.True(t, settings.IsAutoResponsesCompact())
	require.Equal(t, dto.ResponsesCompactModeNative, settings.ResponsesCompactModeOrDefault())
	require.True(t, settings.HasNativeResponsesCompact())
}

func TestChannelOtherSettingsResponsesCompactAutoRoundTrip(t *testing.T) {
	channel := &Channel{}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeAuto,
	})

	settings := channel.GetOtherSettings()

	require.Equal(t, dto.ResponsesCompactModeAuto, settings.ResponsesCompactMode)
	require.True(t, settings.IsAutoResponsesCompact())
	require.True(t, settings.HasNativeResponsesCompact())
	require.False(t, settings.HasSyntheticResponsesCompact())
	require.Equal(t, dto.ResponsesCompactModeNative, settings.ResponsesCompactModeOrDefault())
}

func TestChannelOtherSettingsResponsesCompactAutoFallbackIsDaily(t *testing.T) {
	now := time.Date(2026, 5, 26, 23, 30, 0, 0, time.UTC)
	settings := dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeAuto,
	}

	settings.MarkResponsesCompactAutoFallback(now, "status_code=404")

	require.True(t, settings.HasActiveResponsesCompactAutoFallback(now))
	require.Equal(t, dto.ResponsesCompactModeSynthetic, settings.ResponsesCompactModeOrDefaultAt(now))
	require.False(t, settings.HasActiveResponsesCompactAutoFallback(now.AddDate(0, 0, 1)))
}

func TestChannelOtherSettingsDoesNotMarkFallbackForExplicitNonAutoMode(t *testing.T) {
	now := time.Date(2026, 5, 26, 23, 30, 0, 0, time.UTC)
	settings := dto.ChannelOtherSettings{
		ResponsesCompactMode: dto.ResponsesCompactModeNative,
	}

	settings.MarkResponsesCompactAutoFallback(now, "status_code=404")

	require.Equal(t, dto.ResponsesCompactModeNative, settings.ResponsesCompactMode)
	require.Zero(t, settings.ResponsesCompactAutoFallbackDate)
	require.Empty(t, settings.ResponsesCompactAutoFallbackReason)
}

func TestResponsesCompactAutoFallbackDateUsesUTC(t *testing.T) {
	shanghai := time.FixedZone("UTC+8", 8*60*60)
	localNextDay := time.Date(2026, 5, 27, 1, 30, 0, 0, shanghai)

	require.Equal(t, 20260526, dto.ResponsesCompactAutoFallbackDate(localNextDay))
}

func TestMarkResponsesCompactAutoFallbackPersistsState(t *testing.T) {
	channel := &Channel{
		Id:            8601,
		Name:          "compact-auto-fallback",
		Key:           "test-key",
		Status:        1,
		Group:         "default",
		Models:        "gpt-5",
		OtherSettings: `{"responses_compact_mode":"auto"}`,
	}
	require.NoError(t, DB.Delete(&Channel{}, channel.Id).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Delete(&Channel{}, channel.Id).Error)
	})
	require.NoError(t, DB.Create(channel).Error)

	before := dto.ResponsesCompactAutoFallbackDate(time.Now())
	require.NoError(t, MarkResponsesCompactAutoFallback(channel.Id, "status_code=404"))
	after := dto.ResponsesCompactAutoFallbackDate(time.Now())

	var got Channel
	require.NoError(t, DB.First(&got, channel.Id).Error)
	settings := got.GetOtherSettings()
	require.Equal(t, dto.ResponsesCompactModeAuto, settings.ResponsesCompactMode)
	require.Contains(t, []int{before, after}, settings.ResponsesCompactAutoFallbackDate)
	require.Equal(t, "status_code=404", settings.ResponsesCompactAutoFallbackReason)
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
