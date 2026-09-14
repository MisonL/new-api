package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

func TestRouteV2DatabaseSelectionReturnsErrorWhenDatabaseIsUnavailable(t *testing.T) {
	previousDB := DB
	DB = nil
	t.Cleanup(func() { DB = previousDB })

	entries, err := routeV2EntriesFromDatabaseWithRouteFilter(
		"default",
		[]routeModelCandidate{{model: "gpt-5"}},
		nil,
		nil,
		nil,
		false,
		nil,
	)

	require.ErrorIs(t, err, errRouteDatabaseUnavailable)
	require.Nil(t, entries)
}

func TestBuildRouteCandidateCacheUsesCompactMappingSemantics(t *testing.T) {
	compactModel := ratio_setting.WithCompactModelSuffix("public-model")
	priority := int64(10)
	weight := uint(RouteWeightTotalBPS)
	ability := &Ability{
		Group:                    "default",
		Model:                    compactModel,
		ChannelId:                90401,
		Enabled:                  true,
		RoutePriority:            &priority,
		RoutePriorityConfigured:  true,
		RouteWeightBPS:           &weight,
		RouteWeightBPSConfigured: true,
	}
	channel := &Channel{
		Id:           ability.ChannelId,
		Status:       common.ChannelStatusEnabled,
		ModelMapping: common.GetPointer(`{"public-model":"provider-base","public-model-openai-compact":"provider-compact"}`),
	}

	cache := buildRouteCandidateCacheV2([]*Ability{ability}, map[int]*Channel{channel.Id: channel})
	entries := cache[routeCandidateCacheKey("default", compactModel)]
	require.Len(t, entries, 1)
	require.Equal(t, "provider-compact", entries[0].EffectiveRouteModel)
}

func TestBuildRouteCandidateCacheDoesNotReadLegacyRouteFields(t *testing.T) {
	priority := int64(37)
	legacyWeight := uint(9)
	ability := &Ability{
		Group:     "default",
		Model:     "legacy-only-model",
		ChannelId: 90402,
		Enabled:   true,
		Priority:  &priority,
		Weight:    legacyWeight,
	}
	channel := &Channel{Id: ability.ChannelId, Status: common.ChannelStatusEnabled}

	cache := buildRouteCandidateCacheV2([]*Ability{ability}, map[int]*Channel{channel.Id: channel})
	require.Empty(t, cache)
}

func TestBuildRouteCandidateCachePreservesWeightsAcrossRawModelBuckets(t *testing.T) {
	priority := int64(10)
	weightA, weightB := uint(7000), uint(3000)
	channelA := &Channel{
		Id:           90405,
		Status:       common.ChannelStatusEnabled,
		ModelMapping: common.GetPointer(`{"exact-model":"provider-model"}`),
	}
	channelB := &Channel{
		Id:           90406,
		Status:       common.ChannelStatusEnabled,
		ModelMapping: common.GetPointer(`{"normalized-model":"provider-model"}`),
	}
	abilities := []*Ability{
		{
			Group:                    "default",
			Model:                    "exact-model",
			ChannelId:                channelA.Id,
			Enabled:                  true,
			RoutePriority:            &priority,
			RoutePriorityConfigured:  true,
			RouteWeightBPS:           &weightA,
			RouteWeightBPSConfigured: true,
		},
		{
			Group:                    "default",
			Model:                    "normalized-model",
			ChannelId:                channelB.Id,
			Enabled:                  true,
			RoutePriority:            &priority,
			RoutePriorityConfigured:  true,
			RouteWeightBPS:           &weightB,
			RouteWeightBPSConfigured: true,
		},
	}

	cache := buildRouteCandidateCacheV2(abilities, map[int]*Channel{
		channelA.Id: channelA,
		channelB.Id: channelB,
	})

	require.Equal(t, uint(7000), cache[routeCandidateCacheKey("default", "exact-model")][0].RouteWeightBPS)
	require.Equal(t, uint(3000), cache[routeCandidateCacheKey("default", "normalized-model")][0].RouteWeightBPS)
}

func TestCollapseV2RouteCandidatesRejectsConflictingSameChannelRouteFields(t *testing.T) {
	channel := &Channel{Id: 90403}
	entries := []routeCandidateV2{
		{Channel: channel, EffectiveRouteModel: "provider-model", RoutePriority: 10, RouteWeightBPS: 6000, CandidateRank: 0},
		{Channel: channel, EffectiveRouteModel: "provider-model", RoutePriority: 10, RouteWeightBPS: 4000, CandidateRank: 1},
	}

	got := collapseV2RouteCandidates(entries)
	require.Empty(t, got)
}

func TestCollapseV2RouteCandidatesKeepsSameChannelAcrossEffectiveModels(t *testing.T) {
	channel := &Channel{Id: 90407}
	entries := []routeCandidateV2{
		{Channel: channel, EffectiveRouteModel: "provider-model-a", RoutePriority: 10, RouteWeightBPS: 6000, CandidateRank: 0},
		{Channel: channel, EffectiveRouteModel: "provider-model-b", RoutePriority: 10, RouteWeightBPS: 4000, CandidateRank: 1},
	}

	got := collapseV2RouteCandidates(entries)
	require.Len(t, got, 2)
	require.ElementsMatch(t, []string{"provider-model-a", "provider-model-b"}, []string{got[0].EffectiveRouteModel, got[1].EffectiveRouteModel})
}

func TestV2CacheCollapseRunsAfterAllRouteCandidatesAreCollected(t *testing.T) {
	previousReady := routeCandidateCacheV2Ready
	previousCandidates := routeCandidateCacheV2
	previousKnownByChannel := routeCandidateCacheV2KnownByChannel
	previousIndex := group2model2channels
	previousChannels := channelsIDM
	t.Cleanup(func() {
		channelSyncLock.Lock()
		routeCandidateCacheV2Ready = previousReady
		routeCandidateCacheV2 = previousCandidates
		routeCandidateCacheV2KnownByChannel = previousKnownByChannel
		group2model2channels = previousIndex
		channelsIDM = previousChannels
		channelSyncLock.Unlock()
	})

	channel := &Channel{
		Id:           90404,
		Status:       common.ChannelStatusEnabled,
		ModelMapping: common.GetPointer(`{"exact-model":"provider-model","normalized-model":"provider-model"}`),
	}
	channelSyncLock.Lock()
	routeCandidateCacheV2Ready = true
	routeCandidateCacheV2 = map[string][]routeCandidateV2{
		routeCandidateCacheKey("default", "exact-model"): {{
			Group:               "default",
			RequestedModel:      "exact-model",
			EffectiveRouteModel: "provider-model",
			Channel:             channel.CloneForCache(),
			RoutePriority:       10,
			RouteWeightBPS:      6000,
		}},
		routeCandidateCacheKey("default", "normalized-model"): {{
			Group:               "default",
			RequestedModel:      "normalized-model",
			EffectiveRouteModel: "provider-model",
			Channel:             channel.CloneForCache(),
			RoutePriority:       10,
			RouteWeightBPS:      4000,
		}},
	}
	group2model2channels = map[string]map[string][]int{
		"default": {"exact-model": {channel.Id}, "normalized-model": {channel.Id}},
	}
	routeCandidateCacheV2KnownByChannel = map[string]map[int]struct{}{
		routeCandidateCacheKey("default", "exact-model"):      {channel.Id: {}},
		routeCandidateCacheKey("default", "normalized-model"): {channel.Id: {}},
	}
	channelsIDM = map[int]*Channel{channel.Id: channel.CloneForCache()}
	entries, cacheHit := routeV2EntriesFromCache(
		"default",
		[]routeModelCandidate{{model: "exact-model"}, {model: "normalized-model"}},
		nil,
		nil,
		false,
		nil,
	)
	channelSyncLock.Unlock()

	require.True(t, cacheHit)
	require.Empty(t, entries)
}

func TestV2CacheReportsCompleteWhenRequestFilterRemovesAllEntries(t *testing.T) {
	previousReady := routeCandidateCacheV2Ready
	previousCache := routeCandidateCacheV2
	previousKnown := routeCandidateCacheV2KnownByChannel
	previousChannels := channelsIDM
	t.Cleanup(func() {
		channelSyncLock.Lock()
		routeCandidateCacheV2Ready = previousReady
		routeCandidateCacheV2 = previousCache
		routeCandidateCacheV2KnownByChannel = previousKnown
		channelsIDM = previousChannels
		channelSyncLock.Unlock()
	})

	channel := &Channel{Id: 90409, Status: common.ChannelStatusEnabled}
	key := routeCandidateCacheKey("default", "filter-model")
	channelSyncLock.Lock()
	routeCandidateCacheV2Ready = true
	routeCandidateCacheV2 = map[string][]routeCandidateV2{
		key: {{
			Group:               "default",
			RequestedModel:      "filter-model",
			EffectiveRouteModel: "provider/filter-model",
			Channel:             channel.CloneForCache(),
			RoutePriority:       1,
			RouteWeightBPS:      RouteWeightTotalBPS,
		}},
	}
	routeCandidateCacheV2KnownByChannel = map[string]map[int]struct{}{key: {channel.Id: {}}}
	channelsIDM = map[int]*Channel{channel.Id: channel.CloneForCache()}
	entries, cacheHit, cacheComplete := routeV2EntriesFromCacheWithRouteFilterState(
		"default",
		[]routeModelCandidate{{model: "filter-model"}},
		nil,
		func(_ *Channel, _ string, _ string) bool { return false },
		nil,
		false,
		nil,
	)
	channelSyncLock.Unlock()

	require.Empty(t, entries)
	require.True(t, cacheHit)
	require.True(t, cacheComplete)
}

func TestV2CacheReportsIncompleteWhenMappingRejectsCachedEntry(t *testing.T) {
	previousReady := routeCandidateCacheV2Ready
	previousCache := routeCandidateCacheV2
	previousKnown := routeCandidateCacheV2KnownByChannel
	previousChannels := channelsIDM
	t.Cleanup(func() {
		channelSyncLock.Lock()
		routeCandidateCacheV2Ready = previousReady
		routeCandidateCacheV2 = previousCache
		routeCandidateCacheV2KnownByChannel = previousKnown
		channelsIDM = previousChannels
		channelSyncLock.Unlock()
	})

	invalidMapping := "{"
	channel := &Channel{Id: 90410, Status: common.ChannelStatusEnabled, ModelMapping: &invalidMapping}
	key := routeCandidateCacheKey("default", "mapping-error-model")
	channelSyncLock.Lock()
	routeCandidateCacheV2Ready = true
	routeCandidateCacheV2 = map[string][]routeCandidateV2{
		key: {{
			Group:               "default",
			RequestedModel:      "mapping-error-model",
			EffectiveRouteModel: "provider/mapping-error-model",
			Channel:             channel.CloneForCache(),
			RoutePriority:       1,
			RouteWeightBPS:      RouteWeightTotalBPS,
		}},
	}
	routeCandidateCacheV2KnownByChannel = map[string]map[int]struct{}{key: {channel.Id: {}}}
	channelsIDM = map[int]*Channel{channel.Id: channel.CloneForCache()}
	entries, cacheHit, cacheComplete := routeV2EntriesFromCacheWithRouteFilterState(
		"default",
		[]routeModelCandidate{{model: "mapping-error-model"}},
		nil,
		nil,
		nil,
		false,
		nil,
	)
	channelSyncLock.Unlock()

	require.Empty(t, entries)
	require.True(t, cacheHit)
	require.False(t, cacheComplete)
}

func TestChooseV2RouteChannelKeepsRouteCandidateOrderWithinPriority(t *testing.T) {
	exact := &Channel{Id: 90411}
	fallback := &Channel{Id: 90412}
	entries := []routeCandidateV2{
		{
			Channel:             exact,
			EffectiveRouteModel: "z-provider-model",
			RoutePriority:       10,
			RouteWeightBPS:      RouteWeightTotalBPS,
			CandidateRank:       0,
		},
		{
			Channel:             fallback,
			EffectiveRouteModel: "a-provider-model",
			RoutePriority:       10,
			RouteWeightBPS:      RouteWeightTotalBPS,
			CandidateRank:       1,
		},
	}
	routeCandidateCacheV2KnownByChannel = map[string]map[int]struct{}{
		routeCandidateCacheKey("default", "exact-model"):      {exact.Id: {}},
		routeCandidateCacheKey("default", "normalized-model"): {fallback.Id: {}},
	}

	require.Same(t, exact, chooseV2RouteChannel(entries, 0, nil, "requested", false))
	require.Same(t, fallback, chooseV2RouteChannel(entries, 1, nil, "requested", false))
}

func TestChooseV2RouteEntryPreservesSelectedRawCandidate(t *testing.T) {
	channel := &Channel{Id: 90418}
	entries := []routeCandidateV2{
		{
			Channel:             channel,
			RequestedModel:      "compact-model",
			EffectiveRouteModel: "provider/compact",
			RoutePriority:       10,
			RouteWeightBPS:      RouteWeightTotalBPS,
			CandidateRank:       0,
		},
		{
			Channel:             channel,
			RequestedModel:      "base-model",
			EffectiveRouteModel: "provider/base",
			RoutePriority:       10,
			RouteWeightBPS:      RouteWeightTotalBPS,
			CandidateRank:       1,
		},
	}

	selected := chooseV2RouteEntry(entries, 1, nil, "requested-model", false)
	require.NotNil(t, selected)
	require.Equal(t, "base-model", selected.RequestedModel)
	require.Equal(t, "provider/base", selected.EffectiveRouteModel)

	diagnostics := &dto.ChannelRouteSelectionDiagnostics{}
	fillV2SelectedRouteDiagnosticsForEntry(diagnostics, "default", "requested-model", selected)
	require.Equal(t, "base-model", diagnostics.SelectedCandidateModel)
	require.Equal(t, "provider/base", diagnostics.EffectiveRouteModel)
}

func TestV2SelectionBindsDiagnosticsToSelectedRouteCandidate(t *testing.T) {
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	previousGroupRouteHelperEnabled := common.GroupModelRouteHelperEnabled
	previousReady := routeCandidateCacheV2Ready
	previousCache := routeCandidateCacheV2
	previousKnown := routeCandidateCacheV2KnownByChannel
	previousChannels := channelsIDM
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
		common.GroupModelRouteHelperEnabled = previousGroupRouteHelperEnabled
		channelSyncLock.Lock()
		routeCandidateCacheV2Ready = previousReady
		routeCandidateCacheV2 = previousCache
		routeCandidateCacheV2KnownByChannel = previousKnown
		channelsIDM = previousChannels
		channelSyncLock.Unlock()
	})

	common.MemoryCacheEnabled = true
	common.GroupModelRouteHelperEnabled = true
	t.Setenv("CHANNEL_ROUTING_V2_ENABLED", "true")
	compactModel := ratio_setting.WithCompactModelSuffix("route-identity-model")
	channelID := 90420
	channel := &Channel{
		Id:     channelID,
		Type:   constant.ChannelTypeCodex,
		Status: common.ChannelStatusEnabled,
		ModelMapping: common.GetPointer(
			`{"route-identity-model-openai-compact":"provider/compact","route-identity-model":"provider/base"}`,
		),
	}
	channelSyncLock.Lock()
	routeCandidateCacheV2Ready = true
	routeCandidateCacheV2 = map[string][]routeCandidateV2{
		routeCandidateCacheKey("default", compactModel): {{
			Channel:             channel.CloneForCache(),
			EffectiveRouteModel: "provider/compact",
			RoutePriority:       10,
			RouteWeightBPS:      RouteWeightTotalBPS,
		}},
		routeCandidateCacheKey("default", "route-identity-model"): {{
			Channel:             channel.CloneForCache(),
			EffectiveRouteModel: "provider/base",
			RoutePriority:       10,
			RouteWeightBPS:      RouteWeightTotalBPS,
		}},
	}
	routeCandidateCacheV2KnownByChannel = map[string]map[int]struct{}{
		routeCandidateCacheKey("default", compactModel):           {channelID: {}},
		routeCandidateCacheKey("default", "route-identity-model"): {channelID: {}},
	}
	channelsIDM = map[int]*Channel{channelID: channel.CloneForCache()}
	channelSyncLock.Unlock()

	diagnostics := &dto.ChannelRouteSelectionDiagnostics{}
	selected, err := GetRandomSatisfiedChannelWithOptions(
		"default",
		compactModel,
		1,
		nil,
		SatisfiedChannelSelectionOptions{Diagnostics: diagnostics},
	)
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, channelID, selected.Id)
	require.Equal(t, "route-identity-model", diagnostics.SelectedCandidateModel)
	require.Equal(t, "provider/base", diagnostics.EffectiveRouteModel)
	require.Equal(t, channelID, diagnostics.SelectedChannelID)
}

func TestV2CacheRouteFilterReceivesCandidateEffectiveModel(t *testing.T) {
	previousReady := routeCandidateCacheV2Ready
	previousCache := routeCandidateCacheV2
	previousKnown := routeCandidateCacheV2KnownByChannel
	previousChannels := channelsIDM
	t.Cleanup(func() {
		channelSyncLock.Lock()
		routeCandidateCacheV2Ready = previousReady
		routeCandidateCacheV2 = previousCache
		routeCandidateCacheV2KnownByChannel = previousKnown
		channelsIDM = previousChannels
		channelSyncLock.Unlock()
	})

	channelID := 90419
	channel := &Channel{
		Id:           channelID,
		Status:       common.ChannelStatusEnabled,
		ModelMapping: common.GetPointer(`{"exact-model":"provider/exact","normalized-model":"provider/normalized"}`),
	}
	channelSyncLock.Lock()
	routeCandidateCacheV2Ready = true
	routeCandidateCacheV2 = map[string][]routeCandidateV2{
		routeCandidateCacheKey("default", "exact-model"):      {{Channel: channel.CloneForCache(), RoutePriority: 10, RouteWeightBPS: RouteWeightTotalBPS}},
		routeCandidateCacheKey("default", "normalized-model"): {{Channel: channel.CloneForCache(), RoutePriority: 10, RouteWeightBPS: RouteWeightTotalBPS}},
	}
	routeCandidateCacheV2KnownByChannel = map[string]map[int]struct{}{
		routeCandidateCacheKey("default", "exact-model"):      {channelID: {}},
		routeCandidateCacheKey("default", "normalized-model"): {channelID: {}},
	}
	channelsIDM = map[int]*Channel{channelID: channel.CloneForCache()}
	seen := make(map[string]struct{})
	entries, cacheHit := routeV2EntriesFromCacheWithRouteFilter(
		"default",
		[]routeModelCandidate{{model: "exact-model"}, {model: "normalized-model"}},
		nil,
		func(_ *Channel, candidateModel string, effectiveModel string) bool {
			seen[candidateModel] = struct{}{}
			return effectiveModel == "provider/normalized"
		},
		nil,
		false,
		nil,
	)
	channelSyncLock.Unlock()

	require.True(t, cacheHit)
	require.Len(t, entries, 1)
	require.Equal(t, "normalized-model", entries[0].RequestedModel)
	require.Equal(t, "provider/normalized", entries[0].EffectiveRouteModel)
	require.Contains(t, seen, "exact-model")
	require.Contains(t, seen, "normalized-model")
}

func TestChooseV2RouteChannelDoesNotReviveZeroWeightAfterExclusion(t *testing.T) {
	positive := &Channel{Id: 90413}
	zero := &Channel{Id: 90414}
	entries := []routeCandidateV2{
		{Channel: positive, EffectiveRouteModel: "provider-model", RoutePriority: 10, RouteWeightBPS: 7000},
		{Channel: zero, EffectiveRouteModel: "provider-model", RoutePriority: 10, RouteWeightBPS: 0},
	}
	require.Same(t, positive, chooseV2RouteChannel(entries, 0, nil, "requested", false))
	require.Nil(t, chooseV2RouteChannel(entries, 0, map[int]struct{}{positive.Id: {}}, "requested", false))
}

func TestChooseV2RouteEntryFallsBackWhenPreferredCompactChannelHasZeroWeight(t *testing.T) {
	preferred := &Channel{Id: 90421, OtherSettings: `{"prefer_for_responses_compaction_when_namespace_unsupported":true,"responses_namespace_tools_capability":"unsupported"}`}
	fallback := &Channel{Id: 90422}
	entries := []routeCandidateV2{
		{Channel: preferred, EffectiveRouteModel: "provider-model", RoutePriority: 10, RouteWeightBPS: 0},
		{Channel: fallback, EffectiveRouteModel: "provider-model", RoutePriority: 10, RouteWeightBPS: RouteWeightTotalBPS},
	}

	selected := chooseV2RouteEntry(entries, 0, nil, ratio_setting.WithCompactModelSuffix("requested-model"), false)
	require.NotNil(t, selected)
	require.Same(t, fallback, selected.Channel)
}

func TestChooseV2RouteChannelFallsThroughWhenHighPriorityIsExcluded(t *testing.T) {
	high := &Channel{Id: 90415}
	low := &Channel{Id: 90416}
	entries := []routeCandidateV2{
		{Channel: high, EffectiveRouteModel: "provider-model", RoutePriority: 20, RouteWeightBPS: RouteWeightTotalBPS},
		{Channel: low, EffectiveRouteModel: "provider-model", RoutePriority: 10, RouteWeightBPS: RouteWeightTotalBPS},
	}

	got := chooseV2RouteChannel(entries, 0, map[int]struct{}{high.Id: {}}, "requested", false)
	require.Same(t, low, got)
}

func TestPopulateLegacyRouteSelectionDiagnosticsOmitsNegativePriorityScope(t *testing.T) {
	priority := int64(-1)
	channel := &Channel{
		Id:       90417,
		Priority: &priority,
	}
	diagnostics := &dto.ChannelRouteSelectionDiagnostics{}

	populateLegacyRouteSelectionDiagnostics(diagnostics, "default", "gpt-5.5", channel, nil)

	require.Equal(t, int64(-1), diagnostics.RoutePriority)
	require.Equal(t, "gpt-5.5", diagnostics.EffectiveRouteModel)
	require.Empty(t, diagnostics.RouteScope)
}
