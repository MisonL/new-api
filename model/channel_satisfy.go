package model

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
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
	for _, routeModel := range getGroupModelRouteCandidates(modelName) {
		if isChannelIDInList(group2model2channels[group][routeModel], channelID) {
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
	routeModels := getGroupModelRouteCandidates(modelName)
	if !common.MemoryCacheEnabled {
		return hasResponsesBootstrapRecoveryEnabledChannelDB(groups, routeModels)
	}

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	for _, channel := range channelsIDM {
		if channel == nil || channel.Status != common.ChannelStatusEnabled {
			continue
		}
		if !IsChannelResponsesBootstrapRecoveryEnabled(channel) {
			continue
		}
		if !channelMatchesAnyGroup(channel, groups) {
			continue
		}
		if channelSupportsAnyModel(channel, routeModels) {
			return true
		}
	}
	return false
}

func HasResponsesBootstrapRecoveryCandidateChannel(groups []string, modelName string) bool {
	if len(groups) == 0 || modelName == "" {
		return false
	}
	routeModels := getGroupModelRouteCandidates(modelName)
	if !common.MemoryCacheEnabled {
		return hasResponsesBootstrapRecoveryCandidateChannelDB(groups, routeModels)
	}

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	for _, channel := range channelsIDM {
		if channel == nil {
			continue
		}
		if !IsChannelResponsesBootstrapRecoveryEnabled(channel) {
			continue
		}
		if !channelMatchesAnyGroup(channel, groups) {
			continue
		}
		if channelSupportsAnyModel(channel, routeModels) {
			return true
		}
	}
	return false
}

func isChannelEnabledForGroupModelDB(group string, modelName string, channelID int) bool {
	groupColumn := "abilities." + commonGroupCol
	for _, routeModel := range getGroupModelRouteCandidates(modelName) {
		var count int64
		err := DB.Model(&Ability{}).
			Joins("JOIN channels ON channels.id = abilities.channel_id").
			Where(groupColumn+" = ? and abilities.model = ? and abilities.channel_id = ? and abilities.enabled = ? and channels.status = ?", group, routeModel, channelID, true, common.ChannelStatusEnabled).
			Count(&count).Error
		if err == nil && count > 0 {
			return true
		}
	}
	return false
}

func hasResponsesBootstrapRecoveryEnabledChannelDB(groups []string, routeModels []string) bool {
	var channels []*Channel
	if err := DB.Where("status = ?", common.ChannelStatusEnabled).Find(&channels).Error; err != nil {
		return false
	}
	for _, channel := range channels {
		if channel == nil {
			continue
		}
		if !IsChannelResponsesBootstrapRecoveryEnabled(channel) {
			continue
		}
		if !channelMatchesAnyGroup(channel, groups) {
			continue
		}
		if channelSupportsAnyModel(channel, routeModels) {
			return true
		}
	}
	return false
}

func hasResponsesBootstrapRecoveryCandidateChannelDB(groups []string, routeModels []string) bool {
	var channels []*Channel
	if err := DB.Find(&channels).Error; err != nil {
		return false
	}
	for _, channel := range channels {
		if channel == nil {
			continue
		}
		if !IsChannelResponsesBootstrapRecoveryEnabled(channel) {
			continue
		}
		if !channelMatchesAnyGroup(channel, groups) {
			continue
		}
		if channelSupportsAnyModel(channel, routeModels) {
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

func channelSupportsAnyModel(channel *Channel, routeModels []string) bool {
	if len(routeModels) == 0 {
		return false
	}
	for _, model := range channel.GetModels() {
		trimmed := strings.TrimSpace(model)
		for _, routeModel := range routeModels {
			if trimmed == routeModel {
				return true
			}
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
