package model

import (
	"testing"
	"time"

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

func TestResponsesToolCapabilityObservationSourceComparisonIsCaseInsensitive(t *testing.T) {
	registry := &dto.ResponsesChannelCapabilityRegistry{
		ObservedTools: &dto.ResponsesToolCapabilityObservation{
			ObservedAt: 100,
			Source:     "NATIVE",
		},
	}
	next := dto.ResponsesToolCapabilityObservation{
		ObservedAt: 100,
		Source:     dto.ResponsesToolCapabilityObservationSourceNative,
	}
	require.False(t, responsesToolCapabilityObservationChanges(registry, next))
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

func TestMarkResponsesToolCapabilityObservationBridgedDoesNotPromoteRouting(t *testing.T) {
	channel := &Channel{
		Id:     8613,
		Name:   "tool-capability-bridged",
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

	settings, err := MarkResponsesToolCapabilityObservation(channel.Id, dto.ResponsesToolCapabilityObservation{
		ObservedAt:        time.Now().UTC().Unix(),
		Source:            dto.ResponsesToolCapabilityObservationSourceBridged,
		NamespaceToolCall: true,
		ToolSearchCall:    true,
	})
	require.NoError(t, err)
	require.NotNil(t, settings.ResponsesCapabilityRegistry)
	require.Nil(t, settings.ResponsesCapabilityRegistry.SupportsNamespaceTools)
	require.Nil(t, settings.ResponsesCapabilityRegistry.SupportsToolSearch)
	require.Equal(t, dto.ResponsesToolCapabilityObservationSourceBridged,
		settings.ResponsesCapabilityRegistry.ObservedTools.Source)
}

func TestPromoteUnknownResponsesToolCapabilitiesDoesNotPromoteModelScopedObservation(t *testing.T) {
	registry := &dto.ResponsesChannelCapabilityRegistry{}
	promoteUnknownResponsesToolCapabilities(registry, dto.ResponsesToolCapabilityObservation{
		ObservedAt:        time.Now().UTC().Unix(),
		Model:             "gpt-5.6-sol",
		NamespaceToolCall: true,
		ToolSearchCall:    true,
	})

	require.Nil(t, registry.SupportsNamespaceTools)
	require.Nil(t, registry.SupportsToolSearch)
}

func TestMarkResponsesToolCapabilityObservationExpiresLegacyObservedRoutingOverride(t *testing.T) {
	supported := true
	settings := dto.ChannelOtherSettings{
		ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
			ObservedTools: &dto.ResponsesToolCapabilityObservation{
				ObservedAt:        time.Now().Add(-48 * time.Hour).Unix(),
				ExpiresAt:         time.Now().Add(-24 * time.Hour).Unix(),
				NamespaceToolCall: true,
			},
			SupportsNamespaceTools:       &supported,
			SupportsNamespaceToolsSource: "",
		},
	}

	require.Equal(t, dto.ResponsesToolCapabilityUnknown,
		settings.ResolveResponsesNamespaceToolsCapability(0))
}

func TestExplicitUnsupportedResponsesToolCapabilityDoesNotExpireWithObservedEvidence(t *testing.T) {
	unsupported := false
	settings := dto.ChannelOtherSettings{
		ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
			ObservedTools: &dto.ResponsesToolCapabilityObservation{
				ObservedAt:        time.Now().Add(-48 * time.Hour).Unix(),
				ExpiresAt:         time.Now().Add(-24 * time.Hour).Unix(),
				NamespaceToolCall: true,
			},
			SupportsNamespaceTools:       &unsupported,
			SupportsNamespaceToolsSource: "",
		},
	}

	require.Equal(t, dto.ResponsesToolCapabilityUnsupported,
		settings.ResolveResponsesNamespaceToolsCapability(0))
}
