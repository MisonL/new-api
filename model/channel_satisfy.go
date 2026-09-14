package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// IsChannelEnabledForGroupModel reports whether a channel is enabled for a group/model pair.
func IsChannelEnabledForGroupModel(group string, modelName string, channelID int) bool {
	return IsChannelEnabledForGroupModelWithResolver(group, modelName, channelID, nil)
}

// IsChannelEnabledForGroupModelWithResolver is the request-aware variant used
// by specific-channel and affinity compact checks. It resolves model_mapping
// through the same immutable resolver as the main selector, so a compact
// capability decision cannot observe a different mapping snapshot.
func IsChannelEnabledForGroupModelWithResolver(group string, modelName string, channelID int, resolver RouteModelResolver) bool {
	if group == "" || modelName == "" || channelID <= 0 {
		return false
	}
	if !common.MemoryCacheEnabled {
		return isChannelEnabledForGroupDBWithResolver(group, modelName, channelID, resolver)
	}

	channelSyncLock.RLock()
	if group2model2channels == nil {
		channelSyncLock.RUnlock()
		return isChannelEnabledForGroupDBWithResolver(group, modelName, channelID, resolver)
	}
	cacheReady := routeCandidateCacheV2Ready
	for _, routeCandidate := range getGroupModelRouteCandidateMeta(modelName) {
		if !isChannelIDInList(group2model2channels[group][routeCandidate.model], channelID) {
			continue
		}
		channel, ok := channelsIDM[channelID]
		if !ok {
			continue
		}
		resolvedCandidate, mappingErr := prepareResolvedRouteCandidate(resolver, channel, routeCandidate)
		if mappingErr != nil {
			// Invalid mappings are configuration errors, not capability matches.
			continue
		}
		if channelSupportsCompactRouteCandidate(channel, resolvedCandidate) {
			channelSyncLock.RUnlock()
			return true
		}
	}
	channelSyncLock.RUnlock()
	// Once the complete in-memory snapshot is published, a negative index
	// result is authoritative. Falling back to one DB query per route candidate
	// (and per group for auto-group callers) creates an avoidable N+1 pattern and
	// can observe a different model_mapping snapshot from the request resolver.
	if cacheReady {
		return false
	}
	return isChannelEnabledForGroupDBWithResolver(group, modelName, channelID, resolver)
}

// IsChannelEnabledForAnyGroupModel reports whether a channel is enabled for any group/model pair.
func IsChannelEnabledForAnyGroupModel(groups []string, modelName string, channelID int) bool {
	return IsChannelEnabledForAnyGroupModelWithResolver(groups, modelName, channelID, nil)
}

func IsChannelEnabledForAnyGroupModelWithResolver(groups []string, modelName string, channelID int, resolver RouteModelResolver) bool {
	if len(groups) == 0 {
		return false
	}
	for _, g := range groups {
		if IsChannelEnabledForGroupModelWithResolver(g, modelName, channelID, resolver) {
			return true
		}
	}
	return false
}

func HasResponsesBootstrapRecoveryEnabledChannel(groups []string, modelName string) bool {
	return HasResponsesBootstrapRecoveryEnabledChannelWithResolver(groups, modelName, nil)
}

// HasResponsesBootstrapRecoveryEnabledChannelWithResolver checks the compact
// bootstrap candidate pool with the same request-scoped model mapping used by
// normal route selection. This prevents a malformed or changed mapping from
// making bootstrap recovery wait for a channel that cannot actually receive
// the request.
func HasResponsesBootstrapRecoveryEnabledChannelWithResolver(groups []string, modelName string, resolver RouteModelResolver) bool {
	if len(groups) == 0 || modelName == "" {
		return false
	}
	routeCandidates := getGroupModelRouteCandidateMeta(modelName)
	if !common.MemoryCacheEnabled {
		return hasResponsesBootstrapRecoveryEnabledChannelDB(groups, routeCandidates, resolver)
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
		if channelSupportsAnyModelWithResolver(channel, routeCandidates, resolver) {
			return true
		}
	}
	return false
}

func HasResponsesBootstrapRecoveryCandidateChannel(groups []string, modelName string) bool {
	return HasResponsesBootstrapRecoveryCandidateChannelWithResolver(groups, modelName, nil)
}

// HasResponsesBootstrapRecoveryCandidateChannelWithResolver is the disabled
// or enabled candidate check used before the first Responses channel is
// selected. It must validate model_mapping before reporting a candidate.
func HasResponsesBootstrapRecoveryCandidateChannelWithResolver(groups []string, modelName string, resolver RouteModelResolver) bool {
	if len(groups) == 0 || modelName == "" {
		return false
	}
	routeCandidates := getGroupModelRouteCandidateMeta(modelName)
	if !common.MemoryCacheEnabled {
		return hasResponsesBootstrapRecoveryCandidateChannelDB(groups, routeCandidates, resolver)
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
		if channelSupportsAnyModelWithResolver(channel, routeCandidates, resolver) {
			return true
		}
	}
	return false
}

func isChannelEnabledForGroupModelDB(group string, modelName string, channelID int) bool {
	return isChannelEnabledForGroupDBWithResolver(group, modelName, channelID, nil)
}

func isChannelEnabledForGroupDBWithResolver(group string, modelName string, channelID int, resolver RouteModelResolver) bool {
	groupColumn := "abilities." + commonGroupCol
	for _, routeCandidate := range getGroupModelRouteCandidateMeta(modelName) {
		var channel Channel
		err := DB.Table("abilities").
			Select("channels.*").
			Joins("JOIN channels ON channels.id = abilities.channel_id").
			Where(groupColumn+" = ? and abilities.model = ? and abilities.channel_id = ? and abilities.enabled = ? and channels.status = ?", group, routeCandidate.model, channelID, true, common.ChannelStatusEnabled).
			Take(&channel).Error
		if err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				common.SysError("failed to query channel capability: " + err.Error())
			}
			continue
		}
		resolvedCandidate, mappingErr := prepareResolvedRouteCandidate(resolver, &channel, routeCandidate)
		if mappingErr == nil && channelSupportsCompactRouteCandidate(&channel, resolvedCandidate) {
			return true
		}
	}
	return false
}

func hasResponsesBootstrapRecoveryEnabledChannelDB(groups []string, routeCandidates []routeModelCandidate, resolver RouteModelResolver) bool {
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
		if channelSupportsAnyModelWithResolver(channel, routeCandidates, resolver) {
			return true
		}
	}
	return false
}

func hasResponsesBootstrapRecoveryCandidateChannelDB(groups []string, routeCandidates []routeModelCandidate, resolver RouteModelResolver) bool {
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
		if channelSupportsAnyModelWithResolver(channel, routeCandidates, resolver) {
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

func channelSupportsAnyModelWithResolver(channel *Channel, routeCandidates []routeModelCandidate, resolver RouteModelResolver) bool {
	if channel == nil || len(routeCandidates) == 0 {
		return false
	}
	for _, rawModel := range channel.GetModels() {
		rawModel = strings.TrimSpace(rawModel)
		if rawModel == "" {
			continue
		}
		for _, routeCandidate := range routeCandidates {
			if rawModel != routeCandidate.model {
				continue
			}
			resolvedCandidate, err := prepareResolvedRouteCandidate(resolver, channel, routeCandidate)
			if err != nil {
				continue
			}
			if channelSupportsCompactRouteCandidate(channel, resolvedCandidate) {
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
