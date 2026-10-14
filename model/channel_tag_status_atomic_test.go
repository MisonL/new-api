package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// seedTagStatusChannels creates one channel per id carrying the same tag, each
// with one enabled ability row, and returns the tag.
func seedTagStatusChannels(t *testing.T, tag string, ids []int) {
	t.Helper()
	priority := int64(10)
	weight := uint(RouteWeightTotalBPS)
	for _, id := range ids {
		require.NoError(t, DB.Create(&Channel{
			Id:       id,
			Name:     "tag-status-atomic",
			Status:   common.ChannelStatusEnabled,
			Group:    "default",
			Models:   "tag-status-atomic-model",
			Tag:      &tag,
			Priority: &priority,
			Weight:   common.GetPointer(uint(1)),
		}).Error)
		require.NoError(t, DB.Create(&Ability{
			Group:                    "default",
			Model:                    "tag-status-atomic-model",
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
	// CacheReloadChannel writes into channelsIDM, which prepareChannelCacheTest
	// resets to nil. The refresh below runs outside the transaction, so the
	// cache must be initialized for the commit path to be exercised.
	InitChannelCache()
}

// A tag-level status change must move the channel rows and the ability rows
// together. Written as two independent statements, a failure in the second
// leaves channels disabled while their abilities stay enabled, and the listing
// paths keep advertising models the router will never select.
func TestDisableChannelByTagRollsBackChannelStatusWhenAbilityWriteFails(t *testing.T) {
	prepareChannelCacheTest(t)
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCacheEnabled })

	tag := "tag-status-atomic-rollback"
	ids := []int{90801, 90802}
	seedTagStatusChannels(t, tag, ids)

	// Fail the ability write only. The channel update runs first inside the
	// same transaction, so this is precisely the half-state that used to be
	// committed.
	const callbackName = "test:fail_ability_status_write"
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "abilities" {
			tx.AddError(gorm.ErrInvalidTransaction)
		}
	}))
	t.Cleanup(func() { _ = DB.Callback().Update().Remove(callbackName) })

	err := DisableChannelByTag(tag)
	require.Error(t, err, "the injected ability-write failure must surface")

	for _, id := range ids {
		var channel Channel
		require.NoError(t, DB.First(&channel, id).Error)
		require.Equal(t, common.ChannelStatusEnabled, channel.Status,
			"channel %d was disabled even though the ability write failed", id)

		var abilities []Ability
		require.NoError(t, DB.Where("channel_id = ?", id).Find(&abilities).Error)
		require.Len(t, abilities, 1)
		require.True(t, abilities[0].Enabled,
			"ability for channel %d must stay enabled after the rollback", id)
	}
}

// The happy path must still move both sides, and must do it for every channel
// carrying the tag rather than only the first one.
func TestDisableChannelByTagUpdatesChannelsAndAbilitiesTogether(t *testing.T) {
	prepareChannelCacheTest(t)
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCacheEnabled })

	tag := "tag-status-atomic-commit"
	ids := []int{90803, 90804}
	seedTagStatusChannels(t, tag, ids)

	require.NoError(t, DisableChannelByTag(tag))

	for _, id := range ids {
		var channel Channel
		require.NoError(t, DB.First(&channel, id).Error)
		require.Equal(t, common.ChannelStatusManuallyDisabled, channel.Status)

		var abilities []Ability
		require.NoError(t, DB.Where("channel_id = ?", id).Find(&abilities).Error)
		require.Len(t, abilities, 1)
		require.False(t, abilities[0].Enabled,
			"ability for channel %d must follow the channel into the disabled state", id)
	}

	require.NoError(t, EnableChannelByTag(tag))
	for _, id := range ids {
		var channel Channel
		require.NoError(t, DB.First(&channel, id).Error)
		require.Equal(t, common.ChannelStatusEnabled, channel.Status)

		var abilities []Ability
		require.NoError(t, DB.Where("channel_id = ?", id).Find(&abilities).Error)
		require.True(t, abilities[0].Enabled)
	}
}

// A tag that matches nothing must not error: the tag endpoints are reachable
// with a stale tag, and an empty result is a no-op rather than a failure.
func TestChannelStatusByTagWithUnknownTagIsNoop(t *testing.T) {
	prepareChannelCacheTest(t)
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCacheEnabled })

	require.NoError(t, DisableChannelByTag("tag-status-atomic-absent"))
}

// The route cache is what actually decides whether a model can be called, so the
// tag status write must leave it consistent with the database. This pins the
// observable behaviour so the refresh sequence can be simplified without
// silently dropping the invalidation.
func TestChannelStatusByTagRefreshesRouteCaches(t *testing.T) {
	prepareChannelCacheTest(t)
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCacheEnabled })

	tag := "tag-status-route-cache"
	ids := []int{90805, 90806}
	seedTagStatusChannels(t, tag, ids)

	key := routeCandidateCacheKey("default", "tag-status-atomic-model")
	channelSyncLock.RLock()
	require.Len(t, routeCandidateCacheV2[key], 2)
	require.Len(t, legacyRouteCandidateCache[key], 2)
	require.Len(t, group2model2channels["default"]["tag-status-atomic-model"], 2)
	channelSyncLock.RUnlock()

	require.NoError(t, DisableChannelByTag(tag))
	channelSyncLock.RLock()
	require.Empty(t, routeCandidateCacheV2[key],
		"a disabled channel must leave the V2 route cache")
	require.Empty(t, legacyRouteCandidateCache[key],
		"a disabled channel must leave the legacy route cache")
	require.Empty(t, group2model2channels["default"]["tag-status-atomic-model"])
	channelSyncLock.RUnlock()

	require.NoError(t, EnableChannelByTag(tag))
	channelSyncLock.RLock()
	require.Len(t, routeCandidateCacheV2[key], 2)
	require.Len(t, legacyRouteCandidateCache[key], 2)
	require.Len(t, group2model2channels["default"]["tag-status-atomic-model"], 2)
	channelSyncLock.RUnlock()
}
