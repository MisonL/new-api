package model

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestMarkResponsesToolCapabilityObservationPromotesUnknownOnly(t *testing.T) {
	unsupported := false
	channel := &Channel{
		Id:     8610,
		Name:   "tool-capability-observed",
		Key:    "test-key",
		Status: 1,
		Group:  "default",
		Models: "gpt-5.6-sol",
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
			SupportsNamespaceTools: &unsupported,
		},
	})
	require.NoError(t, DB.Delete(&Channel{}, channel.Id).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Delete(&Channel{}, channel.Id).Error)
	})
	require.NoError(t, DB.Create(channel).Error)

	settings, err := MarkResponsesToolCapabilityObservation(channel.Id, dto.ResponsesToolCapabilityObservation{
		ObservedAt:        100,
		CustomToolCall:    true,
		NamespaceToolCall: true,
		ToolSearchCall:    true,
	})
	require.NoError(t, err)
	require.NotNil(t, settings.ResponsesCapabilityRegistry)
	require.NotNil(t, settings.ResponsesCapabilityRegistry.ObservedTools)
	require.Equal(t, int64(100), settings.ResponsesCapabilityRegistry.ObservedTools.ObservedAt)
	require.True(t, settings.ResponsesCapabilityRegistry.ObservedTools.CustomToolCall)
	require.True(t, settings.ResponsesCapabilityRegistry.ObservedTools.NamespaceToolCall)
	require.True(t, settings.ResponsesCapabilityRegistry.ObservedTools.ToolSearchCall)
	require.NotNil(t, settings.ResponsesCapabilityRegistry.SupportsCustomTools)
	require.True(t, *settings.ResponsesCapabilityRegistry.SupportsCustomTools)
	require.NotNil(t, settings.ResponsesCapabilityRegistry.SupportsNamespaceTools)
	require.False(t, *settings.ResponsesCapabilityRegistry.SupportsNamespaceTools)
	require.NotNil(t, settings.ResponsesCapabilityRegistry.SupportsToolSearch)
	require.True(t, *settings.ResponsesCapabilityRegistry.SupportsToolSearch)

	var persisted Channel
	require.NoError(t, DB.First(&persisted, channel.Id).Error)
	persistedSettings := persisted.GetOtherSettings()
	require.NotNil(t, persistedSettings.ResponsesCapabilityRegistry.ObservedTools)
	require.False(t, *persistedSettings.ResponsesCapabilityRegistry.SupportsNamespaceTools)
}

func TestMarkResponsesToolCapabilityObservationMergesEvidence(t *testing.T) {
	channel := &Channel{
		Id:     8611,
		Name:   "tool-capability-merge",
		Key:    "test-key",
		Status: 1,
		Group:  "default",
		Models: "gpt-5.6-sol",
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
			ObservedTools: &dto.ResponsesToolCapabilityObservation{
				ObservedAt:     100,
				CustomToolCall: true,
			},
		},
	})
	require.NoError(t, DB.Delete(&Channel{}, channel.Id).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Delete(&Channel{}, channel.Id).Error)
	})
	require.NoError(t, DB.Create(channel).Error)

	settings, err := MarkResponsesToolCapabilityObservation(channel.Id, dto.ResponsesToolCapabilityObservation{
		ObservedAt:        101,
		NamespaceToolCall: true,
	})
	require.NoError(t, err)
	require.True(t, settings.ResponsesCapabilityRegistry.ObservedTools.CustomToolCall)
	require.True(t, settings.ResponsesCapabilityRegistry.ObservedTools.NamespaceToolCall)
	require.False(t, settings.ResponsesCapabilityRegistry.ObservedTools.ToolSearchCall)
	require.Equal(t, int64(101), settings.ResponsesCapabilityRegistry.ObservedTools.ObservedAt)
}

func TestMarkResponsesToolCapabilityObservationRejectsMalformedSettings(t *testing.T) {
	channel := &Channel{
		Id:            8612,
		Name:          "tool-capability-malformed-settings",
		Key:           "test-key",
		Status:        1,
		Group:         "default",
		Models:        "gpt-5.6-sol",
		OtherSettings: `{`,
	}
	require.NoError(t, DB.Delete(&Channel{}, channel.Id).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Delete(&Channel{}, channel.Id).Error)
	})
	require.NoError(t, DB.Create(channel).Error)

	_, err := MarkResponsesToolCapabilityObservation(channel.Id, dto.ResponsesToolCapabilityObservation{
		ObservedAt:     100,
		CustomToolCall: true,
	})
	require.Error(t, err)

	var persisted Channel
	require.NoError(t, DB.First(&persisted, channel.Id).Error)
	require.Equal(t, `{`, persisted.OtherSettings)
}
