package model

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// IsChannelEnabledForGroupModel reports whether a channel is enabled for a group/model pair.
func IsChannelEnabledForGroupModel(group string, modelName string, channelID int) bool {
	if group == "" || modelName == "" || channelID <= 0 {
		return false
	}
	if !common.MemoryCacheEnabled {
		return isChannelEnabledForGroupModelDB(group, modelName, channelID)
	}

	channelSyncLock.RLock()
	if group2model2channels == nil {
		channelSyncLock.RUnlock()
		return isChannelEnabledForGroupModelDB(group, modelName, channelID)
	}

	if isChannelIDInList(group2model2channels[group][modelName], channelID) {
		channelSyncLock.RUnlock()
		return true
	}
	normalized := ratio_setting.FormatMatchingModelName(modelName)
	if normalized != "" && normalized != modelName {
		if isChannelIDInList(group2model2channels[group][normalized], channelID) {
			channelSyncLock.RUnlock()
			return true
		}
	}
	channelSyncLock.RUnlock()
	return isChannelEnabledForGroupModelDB(group, modelName, channelID)
}

// IsChannelEnabledForAnyGroupModel reports whether a channel is enabled for any group/model pair.
func IsChannelEnabledForAnyGroupModel(groups []string, modelName string, channelID int) bool {
	if len(groups) == 0 {
		return false
	}
	for _, g := range groups {
		if IsChannelEnabledForGroupModel(g, modelName, channelID) {
			return true
		}
	}
	return false
}

func HasResponsesBootstrapRecoveryEnabledChannel(groups []string, modelName string) bool {
	if len(groups) == 0 || modelName == "" {
		return false
	}
	normalized := ratio_setting.FormatMatchingModelName(modelName)
	if !common.MemoryCacheEnabled {
		return hasResponsesBootstrapRecoveryEnabledChannelDB(groups, modelName, normalized)
	}

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	for _, channel := range channelsIDM {
		if channel == nil || channel.Status != common.ChannelStatusEnabled {
			continue
		}
		if !channel.GetOtherSettings().ResponsesStreamBootstrapRecoveryEnabled {
			continue
		}
		if !channelMatchesAnyGroup(channel, groups) {
			continue
		}
		if channelSupportsModel(channel, modelName, normalized) {
			return true
		}
	}
	return false
}

func HasResponsesBootstrapRecoveryCandidateChannel(groups []string, modelName string) bool {
	if len(groups) == 0 || modelName == "" {
		return false
	}
	normalized := ratio_setting.FormatMatchingModelName(modelName)
	if !common.MemoryCacheEnabled {
		return hasResponsesBootstrapRecoveryCandidateChannelDB(groups, modelName, normalized)
	}

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	for _, channel := range channelsIDM {
		if channel == nil {
			continue
		}
		if !channel.GetOtherSettings().ResponsesStreamBootstrapRecoveryEnabled {
			continue
		}
		if !channelMatchesAnyGroup(channel, groups) {
			continue
		}
		if channelSupportsModel(channel, modelName, normalized) {
			return true
		}
	}
	return false
}

func isChannelEnabledForGroupModelDB(group string, modelName string, channelID int) bool {
	var count int64
	groupColumn := "abilities." + commonGroupCol
	err := DB.Model(&Ability{}).
		Joins("JOIN channels ON channels.id = abilities.channel_id").
		Where(groupColumn+" = ? and abilities.model = ? and abilities.channel_id = ? and abilities.enabled = ? and channels.status = ?", group, modelName, channelID, true, common.ChannelStatusEnabled).
		Count(&count).Error
	if err == nil && count > 0 {
		return true
	}
	normalized := ratio_setting.FormatMatchingModelName(modelName)
	if normalized == "" || normalized == modelName {
		return false
	}
	count = 0
	err = DB.Model(&Ability{}).
		Joins("JOIN channels ON channels.id = abilities.channel_id").
		Where(groupColumn+" = ? and abilities.model = ? and abilities.channel_id = ? and abilities.enabled = ? and channels.status = ?", group, normalized, channelID, true, common.ChannelStatusEnabled).
		Count(&count).Error
	return err == nil && count > 0
}

func hasResponsesBootstrapRecoveryEnabledChannelDB(groups []string, modelName string, normalized string) bool {
	var channels []*Channel
	if err := DB.Where("status = ?", common.ChannelStatusEnabled).Find(&channels).Error; err != nil {
		return false
	}
	for _, channel := range channels {
		if channel == nil {
			continue
		}
		if !channel.GetOtherSettings().ResponsesStreamBootstrapRecoveryEnabled {
			continue
		}
		if !channelMatchesAnyGroup(channel, groups) {
			continue
		}
		if channelSupportsModel(channel, modelName, normalized) {
			return true
		}
	}
	return false
}

func hasResponsesBootstrapRecoveryCandidateChannelDB(groups []string, modelName string, normalized string) bool {
	var channels []*Channel
	if err := DB.Find(&channels).Error; err != nil {
		return false
	}
	for _, channel := range channels {
		if channel == nil {
			continue
		}
		if !channel.GetOtherSettings().ResponsesStreamBootstrapRecoveryEnabled {
			continue
		}
		if !channelMatchesAnyGroup(channel, groups) {
			continue
		}
		if channelSupportsModel(channel, modelName, normalized) {
			return true
		}
	}
	return false
}

func channelMatchesAnyGroup(channel *Channel, groups []string) bool {
	for _, group := range channel.GetGroups() {
		for _, candidate := range groups {
			if strings.TrimSpace(group) == strings.TrimSpace(candidate) {
				return true
			}
		}
	}
	return false
}

func channelSupportsModel(channel *Channel, modelName string, normalized string) bool {
	for _, model := range channel.GetModels() {
		trimmed := strings.TrimSpace(model)
		if trimmed == modelName {
			return true
		}
		if normalized != "" && normalized != modelName && trimmed == normalized {
			return true
		}
	}
	return false
}

func isChannelIDInList(list []int, channelID int) bool {
	for _, id := range list {
		if id == channelID {
			return true
		}
	}
	return false
}
