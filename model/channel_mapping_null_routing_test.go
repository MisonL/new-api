package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// A channel whose persisted mapping is the legacy literal null must stay in the
// routing candidate cache, exactly like a channel with an empty mapping. Before
// the fix the V2 cache dropped it, which removed the channel from routing.
func TestBuildRouteCandidateCacheKeepsChannelWithLiteralNullMapping(t *testing.T) {
	for _, mapping := range []string{"", "null", " null "} {
		t.Run("mapping="+mapping, func(t *testing.T) {
			priority := int64(10)
			weight := uint(RouteWeightTotalBPS)
			ability := &Ability{
				Group:                    "default",
				Model:                    "public-model",
				ChannelId:                90501,
				Enabled:                  true,
				RoutePriority:            &priority,
				RoutePriorityConfigured:  true,
				RouteWeightBPS:           &weight,
				RouteWeightBPSConfigured: true,
			}
			channel := &Channel{
				Id:           ability.ChannelId,
				Status:       common.ChannelStatusEnabled,
				ModelMapping: common.GetPointer(mapping),
			}

			cache := buildRouteCandidateCacheV2([]*Ability{ability}, map[int]*Channel{channel.Id: channel})
			entries := cache[routeCandidateCacheKey("default", "public-model")]
			require.Len(t, entries, 1)
			require.Equal(t, "public-model", entries[0].EffectiveRouteModel)
		})
	}
}

// The strict parser must keep rejecting a mapping that is genuinely malformed,
// so the null fix does not mask real configuration errors.
func TestBuildRouteCandidateCacheStillDropsMalformedMapping(t *testing.T) {
	priority := int64(10)
	weight := uint(RouteWeightTotalBPS)
	ability := &Ability{
		Group:                    "default",
		Model:                    "public-model",
		ChannelId:                90502,
		Enabled:                  true,
		RoutePriority:            &priority,
		RoutePriorityConfigured:  true,
		RouteWeightBPS:           &weight,
		RouteWeightBPSConfigured: true,
	}
	channel := &Channel{
		Id:           ability.ChannelId,
		Status:       common.ChannelStatusEnabled,
		ModelMapping: common.GetPointer(`{"broken":`),
	}

	cache := buildRouteCandidateCacheV2([]*Ability{ability}, map[int]*Channel{channel.Id: channel})
	require.Empty(t, cache[routeCandidateCacheKey("default", "public-model")])
}
