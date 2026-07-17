package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MarkResponsesStreamOptionsIncludeUsageUnsupported records an upstream capability
// only after a request has recovered by retrying without include_usage.
func MarkResponsesStreamOptionsIncludeUsageUnsupported(channelId int, observation dto.ResponsesCapabilityObservation) (dto.ChannelOtherSettings, error) {
	if channelId <= 0 || observation.ObservedAt <= 0 {
		return dto.ChannelOtherSettings{}, nil
	}
	if settings, ok, err := cachedResponsesStreamOptionsIncludeUsageUnsupportedSettings(channelId, observation); err != nil {
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
		needsCapabilityUpdate := registry.SupportsStreamOptionsIncludeUsage == nil
		needsObservationUpdate := shouldUpdateResponsesCapabilityObservation(registry.Observed, observation)
		if !needsCapabilityUpdate && !needsObservationUpdate {
			updatedSettings = settings
			return nil
		}

		if needsCapabilityUpdate {
			registry.SupportsStreamOptionsIncludeUsage = common.GetPointer(false)
		}
		if needsObservationUpdate {
			registry.Observed = &observation
		}
		registry.Source = "observed_calls"
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

func cachedResponsesStreamOptionsIncludeUsageUnsupportedSettings(channelId int, observation dto.ResponsesCapabilityObservation) (dto.ChannelOtherSettings, bool, error) {
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
	registry := settings.ResponsesCapabilityRegistry
	if registry == nil || registry.SupportsStreamOptionsIncludeUsage == nil ||
		shouldUpdateResponsesCapabilityObservation(registry.Observed, observation) {
		return dto.ChannelOtherSettings{}, false, nil
	}
	return settings, true, nil
}
