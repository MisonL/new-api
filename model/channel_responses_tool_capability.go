package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func MarkResponsesToolCapabilityObservation(channelId int, observation dto.ResponsesToolCapabilityObservation) (dto.ChannelOtherSettings, error) {
	if channelId <= 0 || observation.ObservedAt <= 0 || !observation.HasEvidence() {
		return dto.ChannelOtherSettings{}, nil
	}
	if settings, ok, err := cachedResponsesToolCapabilityObservationSettings(channelId, observation); err != nil {
		return dto.ChannelOtherSettings{}, err
	} else if ok {
		return settings, nil
	}

	var updatedSettings dto.ChannelOtherSettings
	updatedOtherSettings := ""
	shouldReloadCache := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		channel := &Channel{}
		query := tx
		if !common.UsingSQLite {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(channel, channelId).Error; err != nil {
			return err
		}
		settings := dto.ChannelOtherSettings{}
		if channel.OtherSettings != "" {
			if err := common.UnmarshalJsonStr(channel.OtherSettings, &settings); err != nil {
				return err
			}
		}
		if settings.ResponsesCapabilityRegistry == nil {
			settings.ResponsesCapabilityRegistry = &dto.ResponsesChannelCapabilityRegistry{}
		}
		registry := settings.ResponsesCapabilityRegistry
		if !responsesToolCapabilityObservationChanges(registry, observation) {
			updatedSettings = settings
			return nil
		}

		registry.ObservedTools = mergeResponsesToolCapabilityObservation(registry.ObservedTools, observation)
		registry.Source = "observed_calls"
		promoteUnknownResponsesToolCapabilities(registry, observation)
		channel.SetOtherSettings(settings)
		if err := tx.Model(&Channel{}).Where("id = ?", channelId).Update("settings", channel.OtherSettings).Error; err != nil {
			return err
		}
		updatedSettings = settings
		updatedOtherSettings = channel.OtherSettings
		shouldReloadCache = true
		return nil
	})
	if err != nil {
		return dto.ChannelOtherSettings{}, err
	}
	if shouldReloadCache {
		CacheUpdateChannelOtherSettings(channelId, updatedOtherSettings)
	}
	return updatedSettings, nil
}

func cachedResponsesToolCapabilityObservationSettings(channelId int, observation dto.ResponsesToolCapabilityObservation) (dto.ChannelOtherSettings, bool, error) {
	if !common.MemoryCacheEnabled {
		return dto.ChannelOtherSettings{}, false, nil
	}
	channel, err := CacheGetChannel(channelId)
	if err != nil || channel == nil || channel.OtherSettings == "" {
		return dto.ChannelOtherSettings{}, false, nil
	}
	settings := dto.ChannelOtherSettings{}
	if err := common.UnmarshalJsonStr(channel.OtherSettings, &settings); err != nil {
		return dto.ChannelOtherSettings{}, false, err
	}
	if settings.ResponsesCapabilityRegistry == nil ||
		responsesToolCapabilityObservationChanges(settings.ResponsesCapabilityRegistry, observation) {
		return dto.ChannelOtherSettings{}, false, nil
	}
	return settings, true, nil
}

func responsesToolCapabilityObservationChanges(registry *dto.ResponsesChannelCapabilityRegistry, next dto.ResponsesToolCapabilityObservation) bool {
	if registry == nil {
		return true
	}
	existing := registry.ObservedTools
	if existing == nil {
		return true
	}
	if next.CustomToolCall && (!existing.CustomToolCall || registry.SupportsCustomTools == nil) {
		return true
	}
	if next.NamespaceToolCall && (!existing.NamespaceToolCall || registry.SupportsNamespaceTools == nil) {
		return true
	}
	if next.ToolSearchCall && (!existing.ToolSearchCall || registry.SupportsToolSearch == nil) {
		return true
	}
	return next.ObservedAt > existing.ObservedAt &&
		next.ObservedAt-existing.ObservedAt >= responsesCapabilityObservationMinUpdateIntervalSeconds
}

func mergeResponsesToolCapabilityObservation(existing *dto.ResponsesToolCapabilityObservation, next dto.ResponsesToolCapabilityObservation) *dto.ResponsesToolCapabilityObservation {
	merged := next
	if existing == nil {
		return &merged
	}
	if existing.ObservedAt > merged.ObservedAt {
		merged.ObservedAt = existing.ObservedAt
	}
	merged.CustomToolCall = existing.CustomToolCall || next.CustomToolCall
	merged.NamespaceToolCall = existing.NamespaceToolCall || next.NamespaceToolCall
	merged.ToolSearchCall = existing.ToolSearchCall || next.ToolSearchCall
	return &merged
}

func promoteUnknownResponsesToolCapabilities(registry *dto.ResponsesChannelCapabilityRegistry, observation dto.ResponsesToolCapabilityObservation) {
	if registry == nil {
		return
	}
	if observation.CustomToolCall && registry.SupportsCustomTools == nil {
		registry.SupportsCustomTools = common.GetPointer(true)
	}
	if observation.NamespaceToolCall && registry.SupportsNamespaceTools == nil {
		registry.SupportsNamespaceTools = common.GetPointer(true)
	}
	if observation.ToolSearchCall && registry.SupportsToolSearch == nil {
		registry.SupportsToolSearch = common.GetPointer(true)
	}
}
