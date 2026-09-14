package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestUpdateAbilityStatusRefreshesRouteCaches(t *testing.T) {
	prepareChannelCacheTest(t)
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCacheEnabled })

	priority := int64(10)
	weight := uint(RouteWeightTotalBPS)
	channel := &Channel{
		Id:       90701,
		Name:     "ability-status-cache-sync",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "ability-status-model",
		Priority: &priority,
		Weight:   common.GetPointer(uint(1)),
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:                    "default",
		Model:                    "ability-status-model",
		ChannelId:                channel.Id,
		Enabled:                  true,
		Priority:                 &priority,
		Weight:                   1,
		RoutePriority:            &priority,
		RouteWeightBPS:           &weight,
		RoutePriorityConfigured:  true,
		RouteWeightBPSConfigured: true,
	}).Error)
	InitChannelCache()

	key := routeCandidateCacheKey("default", "ability-status-model")
	channelSyncLock.RLock()
	require.Len(t, routeCandidateCacheV2[key], 1)
	require.Len(t, legacyRouteCandidateCache[key], 1)
	channelSyncLock.RUnlock()

	require.NoError(t, UpdateAbilityStatus(channel.Id, false))
	channelSyncLock.RLock()
	require.Empty(t, routeCandidateCacheV2[key])
	require.Empty(t, legacyRouteCandidateCache[key])
	require.Empty(t, group2model2channels["default"]["ability-status-model"])
	channelSyncLock.RUnlock()

	require.NoError(t, UpdateAbilityStatus(channel.Id, true))
	channelSyncLock.RLock()
	require.Len(t, routeCandidateCacheV2[key], 1)
	require.Len(t, legacyRouteCandidateCache[key], 1)
	channelSyncLock.RUnlock()
}

func TestUpdateAbilityStatusByTagRefreshesEveryAffectedChannel(t *testing.T) {
	prepareChannelCacheTest(t)
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCacheEnabled })

	tag := "ability-status-tag-cache-sync"
	priority := int64(10)
	weight := uint(RouteWeightTotalBPS)
	for _, id := range []int{90702, 90703} {
		channel := &Channel{
			Id:       id,
			Name:     "ability-status-tag-cache-sync",
			Status:   common.ChannelStatusEnabled,
			Group:    "default",
			Models:   "ability-status-tag-model",
			Tag:      &tag,
			Priority: &priority,
			Weight:   common.GetPointer(uint(1)),
		}
		require.NoError(t, DB.Create(channel).Error)
		require.NoError(t, DB.Create(&Ability{
			Group:                    "default",
			Model:                    "ability-status-tag-model",
			ChannelId:                id,
			Enabled:                  true,
			Priority:                 &priority,
			Weight:                   1,
			RoutePriority:            &priority,
			RouteWeightBPS:           &weight,
			RoutePriorityConfigured:  true,
			RouteWeightBPSConfigured: true,
			Tag:                      &tag,
		}).Error)
	}
	InitChannelCache()

	key := routeCandidateCacheKey("default", "ability-status-tag-model")
	channelSyncLock.RLock()
	require.Len(t, routeCandidateCacheV2[key], 2)
	channelSyncLock.RUnlock()

	require.NoError(t, UpdateAbilityStatusByTag(tag, false))
	channelSyncLock.RLock()
	require.Empty(t, routeCandidateCacheV2[key])
	require.Empty(t, legacyRouteCandidateCache[key])
	require.Empty(t, group2model2channels["default"]["ability-status-tag-model"])
	channelSyncLock.RUnlock()

	require.NoError(t, UpdateAbilityStatusByTag(tag, true))
	channelSyncLock.RLock()
	require.Len(t, routeCandidateCacheV2[key], 2)
	require.Len(t, legacyRouteCandidateCache[key], 2)
	channelSyncLock.RUnlock()
}

func TestUpdateAbilityByTagRefreshesLegacyRouteMetadataAndTag(t *testing.T) {
	prepareChannelCacheTest(t)
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Setenv("CHANNEL_ROUTING_V2_STRICT_WRITES", "false")
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCacheEnabled })

	oldTag := "ability-metadata-old-tag"
	newTag := "ability-metadata-new-tag"
	oldPriority := int64(10)
	newPriority := int64(20)
	oldWeight := uint(3)
	newWeight := uint(17)
	channel := &Channel{
		Id:       90704,
		Name:     "ability-metadata-cache-sync",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "ability-metadata-model",
		Tag:      &oldTag,
		Priority: &oldPriority,
		Weight:   &oldWeight,
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "ability-metadata-model",
		ChannelId: channel.Id,
		Enabled:   true,
		Priority:  &oldPriority,
		Weight:    oldWeight,
		Tag:       &oldTag,
	}).Error)
	InitChannelCache()

	key := routeCandidateCacheKey("default", "ability-metadata-model")
	require.NoError(t, UpdateAbilityByTag(oldTag, &newTag, &newPriority, &newWeight))

	channelSyncLock.RLock()
	entries := legacyRouteCandidateCache[key]
	require.Len(t, entries, 1)
	require.Equal(t, newPriority, entries[0].priority)
	require.Equal(t, newWeight, entries[0].abilityWeight)
	channelSyncLock.RUnlock()

	var persisted Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).First(&persisted).Error)
	require.NotNil(t, persisted.Tag)
	require.Equal(t, newTag, *persisted.Tag)
}
