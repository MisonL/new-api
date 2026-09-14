package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestCloneForCacheDeepCopiesMutableChannelFields(t *testing.T) {
	weight := uint(7)
	priority := int64(9)
	setting := `{"proxy":"http://proxy.example"}`
	original := &Channel{
		Id:       90101,
		Weight:   &weight,
		Priority: &priority,
		Setting:  &setting,
		ChannelInfo: ChannelInfo{
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusEnabled},
		},
	}

	cloned := original.CloneForCache()
	require.NotNil(t, cloned)

	*cloned.Weight = 11
	*cloned.Priority = 12
	*cloned.Setting = `{"proxy":"http://changed.example"}`
	cloned.ChannelInfo.MultiKeyStatusList[0] = common.ChannelStatusManuallyDisabled

	require.Equal(t, uint(7), *original.Weight)
	require.Equal(t, int64(9), *original.Priority)
	require.Equal(t, setting, *original.Setting)
	require.Equal(t, common.ChannelStatusEnabled, original.ChannelInfo.MultiKeyStatusList[0])
}

func TestLegacyRouteCacheReturnsIndependentChannelSnapshot(t *testing.T) {
	previous := legacyRouteCandidateCache
	t.Cleanup(func() { legacyRouteCandidateCache = previous })

	weight := uint(100)
	channel := &Channel{Id: 90102, Status: common.ChannelStatusEnabled, Weight: &weight}
	legacyRouteCandidateCache = map[string][]legacyRouteCandidateCacheEntry{
		routeCandidateCacheKey("default", "model-a"): {{
			channel:       channel,
			priority:      1,
			abilityWeight: 100,
		}},
	}

	selected, err, hit, _ := getRandomSatisfiedRouteModelFromCacheWithRouteFilterAndCompleteness(
		"default",
		routeModelCandidate{model: "model-a"},
		0,
		nil,
		false,
		nil,
		nil,
		nil,
		nil,
	)
	require.True(t, hit)
	require.NoError(t, err)
	require.NotNil(t, selected)

	*selected.Weight = 200
	require.Equal(t, uint(100), *channel.Weight)
}

func TestRouteCandidateCacheBuildUsesIndependentChannelSnapshots(t *testing.T) {
	priority := int64(1)
	weight := uint(RouteWeightTotalBPS)
	channel := &Channel{Id: 90103, Status: common.ChannelStatusEnabled}
	ability := &Ability{
		Group:                    "default",
		Model:                    "model-a",
		ChannelId:                channel.Id,
		Enabled:                  true,
		RoutePriority:            &priority,
		RoutePriorityConfigured:  true,
		RouteWeightBPS:           &weight,
		RouteWeightBPSConfigured: true,
	}

	cache := buildRouteCandidateCacheV2([]*Ability{ability}, map[int]*Channel{channel.Id: channel})
	entry := cache[routeCandidateCacheKey("default", "model-a")][0]
	require.NotSame(t, channel, entry.Channel)
	entry.Channel.Status = common.ChannelStatusManuallyDisabled
	require.Equal(t, common.ChannelStatusEnabled, channel.Status)
}
