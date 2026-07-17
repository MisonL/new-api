package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestMarkResponsesStreamOptionsIncludeUsageUnsupportedPersistsUnknownCapability(t *testing.T) {
	channel := &Channel{
		Id:     8613,
		Name:   "stream-options-capability-observed",
		Key:    "test-key",
		Status: 1,
		Group:  "default",
		Models: "gpt-5.6-sol",
	}
	require.NoError(t, DB.Delete(&Channel{}, channel.Id).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Delete(&Channel{}, channel.Id).Error)
	})
	require.NoError(t, DB.Create(channel).Error)

	settings, err := MarkResponsesStreamOptionsIncludeUsageUnsupported(channel.Id, dto.ResponsesCapabilityObservation{
		ObservedAt: 100,
		StatusCode: 400,
		ErrorCode:  "unknown_parameter",
		Reason:     "stream_options.include_usage",
	})

	require.NoError(t, err)
	require.NotNil(t, settings.ResponsesCapabilityRegistry)
	require.NotNil(t, settings.ResponsesCapabilityRegistry.SupportsStreamOptionsIncludeUsage)
	require.False(t, *settings.ResponsesCapabilityRegistry.SupportsStreamOptionsIncludeUsage)
	require.NotNil(t, settings.ResponsesCapabilityRegistry.Observed)
	require.Equal(t, "stream_options.include_usage", settings.ResponsesCapabilityRegistry.Observed.Reason)

	var persisted Channel
	require.NoError(t, DB.First(&persisted, channel.Id).Error)
	persistedSettings := persisted.GetOtherSettings()
	require.True(t, persistedSettings.ExplicitlyDisablesResponsesStreamOptionsIncludeUsage())
}

func TestMarkResponsesStreamOptionsIncludeUsageUnsupportedKeepsExplicitSupport(t *testing.T) {
	supported := true
	channel := &Channel{
		Id:     8614,
		Name:   "stream-options-capability-explicit-support",
		Key:    "test-key",
		Status: 1,
		Group:  "default",
		Models: "gpt-5.6-sol",
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
			SupportsStreamOptionsIncludeUsage: &supported,
		},
	})
	require.NoError(t, DB.Delete(&Channel{}, channel.Id).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Delete(&Channel{}, channel.Id).Error)
	})
	require.NoError(t, DB.Create(channel).Error)

	settings, err := MarkResponsesStreamOptionsIncludeUsageUnsupported(channel.Id, dto.ResponsesCapabilityObservation{
		ObservedAt: 100,
		StatusCode: 400,
		ErrorCode:  "unknown_parameter",
		Reason:     "stream_options.include_usage",
	})

	require.NoError(t, err)
	require.NotNil(t, settings.ResponsesCapabilityRegistry.SupportsStreamOptionsIncludeUsage)
	require.True(t, *settings.ResponsesCapabilityRegistry.SupportsStreamOptionsIncludeUsage)
	require.NotNil(t, settings.ResponsesCapabilityRegistry.Observed)

	channelSyncLock.RLock()
	cached := channelsIDM[channel.Id]
	channelSyncLock.RUnlock()
	if common.MemoryCacheEnabled {
		require.NotNil(t, cached)
		require.True(t, *cached.GetOtherSettings().ResponsesCapabilityRegistry.SupportsStreamOptionsIncludeUsage)
	}
}
