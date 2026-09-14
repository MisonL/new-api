package model

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

var errRouteDatabaseUnavailable = errors.New("channel route database is unavailable")

type routeCandidateV2 struct {
	Group               string
	EffectiveRouteModel string
	MappingApplied      bool
	// RequestedModel is the ability/model key used to enter the candidate pool.
	// EffectiveRouteModel is resolved from the selected channel's mapping before
	// weights are normalized, so mapping failures cannot reach relay dispatch.
	RequestedModel string
	Channel        *Channel
	RoutePriority  int64
	RouteWeightBPS uint
	// LegacyWeight is retained for migration/shadow diagnostics only. It must
	// never participate in V2 selection.
	LegacyWeight   uint
	ExplicitWeight bool
	CandidateRank  int
}

var routeCandidateCacheV2 map[string][]routeCandidateV2

func routeCandidateCacheKey(group string, model string) string {
	return strings.TrimSpace(group) + "\x00" + strings.TrimSpace(model)
}

func buildRouteCandidateCacheV2(abilities []*Ability, channels map[int]*Channel) map[string][]routeCandidateV2 {
	result := make(map[string][]routeCandidateV2)
	for _, ability := range abilities {
		if ability == nil {
			continue
		}
		channel := channels[ability.ChannelId]
		if channel == nil || !ability.Enabled || channel.Status != common.ChannelStatusEnabled {
			continue
		}
		priority, priorityConfigured := ability.GetConfiguredRoutePriority()
		weight, weightConfigured := ability.GetConfiguredRouteWeightBPS()
		if !priorityConfigured || !weightConfigured {
			// V2 is migration-gated. Do not reintroduce legacy priority/weight
			// as an implicit fallback for an unbackfilled row.
			continue
		}
		_, compactRequest := ratio_setting.CompactBaseModelName(ability.Model)
		effectiveModel, mapped, err := ResolveChannelModelMappingForRouting(channel.GetModelMapping(), ability.Model, compactRequest)
		if err != nil {
			// A stale/legacy invalid row must be excluded from routing rather than
			// selected and failed later as a misleading upstream 504. Keep the
			// identity in the log so a cache refresh cannot silently make a channel
			// disappear from one model pool.
			common.SysError(fmt.Sprintf("failed to resolve model mapping for route cache: channel_id=%d group=%s requested_model=%s error=%v", channel.Id, ability.Group, ability.Model, err))
			continue
		}
		key := routeCandidateCacheKey(ability.Group, ability.Model)
		result[key] = append(result[key], routeCandidateV2{Group: ability.Group, RequestedModel: ability.Model, EffectiveRouteModel: effectiveModel, MappingApplied: mapped, Channel: channel.CloneForCache(), RoutePriority: priority, RouteWeightBPS: weight, LegacyWeight: ability.Weight, ExplicitWeight: true})
	}
	for key, entries := range result {
		// Keep the persisted route weight unchanged in each raw ability bucket.
		// A request may pool several raw model keys (for example an exact model
		// and its normalized alias) into one effective route scope. Normalizing
		// each bucket independently would turn a 70/30 scope into 50/50 before
		// the buckets are merged. The common collection path performs collapse
		// and normalization once after all route candidates have been collected.
		result[key] = collapseV2RouteCandidates(entries)
	}
	return result
}

func normalizeV2RouteEntries(entries []routeCandidateV2) []routeCandidateV2 {
	if len(entries) == 0 {
		return entries
	}
	sortV2RouteEntries(entries)
	// A scope is isolated by effective upstream model as well as priority.
	// Two channels exposing the same requested model but mapping to different
	// upstream models are not interchangeable traffic candidates.
	type scopeKey struct {
		priority  int64
		effective string
	}
	byScope := make(map[scopeKey][]int)
	for i := range entries {
		byScope[scopeKey{priority: entries[i].RoutePriority, effective: entries[i].EffectiveRouteModel}] = append(byScope[scopeKey{priority: entries[i].RoutePriority, effective: entries[i].EffectiveRouteModel}], i)
	}
	for _, indexes := range byScope {
		numerators := make([]uint64, len(indexes))
		var total uint64
		for i, index := range indexes {
			numerators[i] = uint64(entries[index].RouteWeightBPS)
			if ^uint64(0)-total < numerators[i] {
				total = ^uint64(0)
			} else {
				total += numerators[i]
			}
		}
		if total == uint64(RouteWeightTotalBPS) {
			continue
		}
		bps := NormalizeRouteWeights(numerators)
		for i, index := range indexes {
			entries[index].RouteWeightBPS = bps[i]
		}
	}
	return entries
}

func sortV2RouteEntries(entries []routeCandidateV2) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].RoutePriority != entries[j].RoutePriority {
			return entries[i].RoutePriority > entries[j].RoutePriority
		}
		if entries[i].EffectiveRouteModel != entries[j].EffectiveRouteModel {
			return entries[i].EffectiveRouteModel < entries[j].EffectiveRouteModel
		}
		if entries[i].CandidateRank != entries[j].CandidateRank {
			return entries[i].CandidateRank < entries[j].CandidateRank
		}
		if entries[i].Channel == nil || entries[j].Channel == nil {
			return entries[i].Channel != nil
		}
		return entries[i].Channel.Id < entries[j].Channel.Id
	})
}

// collapseV2RouteCandidates applies the same channel identity rule to DB and
// cache snapshots. A channel may appear under several raw ability keys because
// model mapping and normalization are many-to-one. The first candidate rank is
// preferred, but conflicting route weights for the same effective model and
// priority make that route member ambiguous and therefore ineligible instead
// of depending on DB row order. Different effective models remain independent
// scopes even when they use the same physical channel.
func collapseV2RouteCandidates(entries []routeCandidateV2) []routeCandidateV2 {
	if len(entries) < 2 {
		return entries
	}
	// A physical channel may legitimately expose multiple effective upstream
	// models. Those are separate route scopes and must remain separate entries;
	// only duplicate representations of the same channel *within one effective
	// model and priority* are ambiguous.
	type collapseKey struct {
		channelID int
		effective string
		priority  int64
	}
	byChannel := make(map[collapseKey][]routeCandidateV2)
	for _, entry := range entries {
		if entry.Channel == nil || entry.Channel.Id <= 0 {
			continue
		}
		key := collapseKey{
			channelID: entry.Channel.Id,
			effective: strings.TrimSpace(entry.EffectiveRouteModel),
			priority:  entry.RoutePriority,
		}
		byChannel[key] = append(byChannel[key], entry)
	}
	result := make([]routeCandidateV2, 0, len(byChannel))
	for _, channelEntries := range byChannel {
		sort.SliceStable(channelEntries, func(i, j int) bool {
			if channelEntries[i].CandidateRank != channelEntries[j].CandidateRank {
				return channelEntries[i].CandidateRank < channelEntries[j].CandidateRank
			}
			if channelEntries[i].EffectiveRouteModel != channelEntries[j].EffectiveRouteModel {
				return channelEntries[i].EffectiveRouteModel < channelEntries[j].EffectiveRouteModel
			}
			if channelEntries[i].RoutePriority != channelEntries[j].RoutePriority {
				return channelEntries[i].RoutePriority > channelEntries[j].RoutePriority
			}
			return channelEntries[i].RouteWeightBPS > channelEntries[j].RouteWeightBPS
		})
		selected := channelEntries[0]
		ambiguous := false
		for _, candidate := range channelEntries[1:] {
			if candidate.RouteWeightBPS != selected.RouteWeightBPS {
				ambiguous = true
				break
			}
		}
		if !ambiguous {
			result = append(result, selected)
		}
	}
	sortV2RouteEntries(result)
	return result
}

func getRandomSatisfiedChannelV2(group string, requestedModel string, retry int, excluded map[int]struct{}, options SatisfiedChannelSelectionOptions) (*Channel, error) {
	initializeV2RouteSelectionDiagnostics(options.Diagnostics, group, requestedModel, excluded)
	routeCandidates := getGroupModelRouteCandidateMeta(requestedModel)
	if len(routeCandidates) == 0 {
		return nil, nil
	}
	if options.RequestBodySize > 0 {
		// V2 can run before the legacy channel index is populated. Use the
		// database as the authoritative body-limit source on a cold cache.
		bodyExcluded, err := findRequestBodyLimitExcludedChannelsFromDatabase(group, requestedModel, options.RequestBodySize, options.BodyLimitTTLHours, options.Now)
		if err != nil {
			return nil, err
		}
		excluded = mergeExcludedChannels(excluded, bodyExcluded)
		if options.Diagnostics != nil {
			options.Diagnostics.ExcludedChannelIDs = sortedChannelIDs(excluded)
		}
	}

	var entries []routeCandidateV2
	var cacheHit bool
	var cacheComplete bool
	if common.MemoryCacheEnabled {
		channelSyncLock.RLock()
		entries, cacheHit, cacheComplete = routeV2EntriesFromCacheWithRouteFilterState(group, routeCandidates, options.CandidateFilter, options.RouteCandidateFilter, excluded, options.ExcludeBeforePriority, options.RouteModelResolver)
		channelSyncLock.RUnlock()
	}
	if !cacheHit || !cacheComplete {
		var err error
		entries, err = routeV2EntriesFromDatabaseWithRouteFilter(group, routeCandidates, options.CandidateFilter, options.RouteCandidateFilter, excluded, options.ExcludeBeforePriority, options.RouteModelResolver)
		if err != nil {
			return nil, err
		}
		if common.MemoryCacheEnabled {
			requestChannelCacheRefreshAsync()
		}
	}
	if options.Diagnostics != nil {
		options.Diagnostics.SelectionPath = "memory"
		if !cacheHit || !cacheComplete {
			options.Diagnostics.SelectionPath = "database"
		}
		options.Diagnostics.CandidateChannelIDs = routeCandidateIDs(entries)
		options.Diagnostics.CandidateCount = len(options.Diagnostics.CandidateChannelIDs)
	}
	selectedEntry := chooseV2RouteEntry(entries, retry, excluded, requestedModel, options.ExcludeBeforePriority)
	if selectedEntry != nil {
		fillV2SelectedRouteDiagnosticsForEntry(options.Diagnostics, group, requestedModel, selectedEntry)
		return selectedEntry.Channel, nil
	}
	return nil, nil
}

func initializeV2RouteSelectionDiagnostics(diag *dto.ChannelRouteSelectionDiagnostics, group string, requestedModel string, excluded map[int]struct{}) {
	if diag == nil {
		return
	}
	*diag = dto.ChannelRouteSelectionDiagnostics{
		Group:              strings.TrimSpace(group),
		RequestedModel:     strings.TrimSpace(requestedModel),
		ExcludedChannelIDs: sortedChannelIDs(excluded),
	}
}

func routeCandidateIDs(entries []routeCandidateV2) []int {
	seen := make(map[int]struct{}, len(entries))
	for _, entry := range entries {
		if entry.Channel != nil && entry.Channel.Id > 0 {
			seen[entry.Channel.Id] = struct{}{}
		}
	}
	return sortedChannelIDs(seen)
}

func fillV2SelectedRouteDiagnostics(diag *dto.ChannelRouteSelectionDiagnostics, group string, requestedModel string, entries []routeCandidateV2, channel *Channel) {
	if channel == nil {
		return
	}
	for index := range entries {
		if entries[index].Channel == nil || entries[index].Channel.Id != channel.Id {
			continue
		}
		fillV2SelectedRouteDiagnosticsForEntry(diag, group, requestedModel, &entries[index])
		return
	}
}

func fillV2SelectedRouteDiagnosticsForEntry(diag *dto.ChannelRouteSelectionDiagnostics, group string, requestedModel string, entry *routeCandidateV2) {
	if diag == nil || entry == nil || entry.Channel == nil {
		return
	}
	diag.Group = strings.TrimSpace(group)
	diag.RequestedModel = strings.TrimSpace(requestedModel)
	diag.SelectedCandidateModel = strings.TrimSpace(entry.RequestedModel)
	if diag.SelectedCandidateModel == "" {
		diag.SelectedCandidateModel = strings.TrimSpace(requestedModel)
	}
	diag.EffectiveRouteModel = strings.TrimSpace(entry.EffectiveRouteModel)
	diag.SelectedChannelID = entry.Channel.Id
	diag.RouteScope = EncodeRouteScopeID(group, entry.EffectiveRouteModel, entry.RoutePriority)
	diag.RoutePriority = entry.RoutePriority
	diag.RouteWeightBPS = entry.RouteWeightBPS
	diag.WeightSource = "route_weight_bps"
}

func routeV2EntriesFromCache(group string, routeCandidates []routeModelCandidate, filter func(*Channel) bool, excluded map[int]struct{}, excludeBeforePriority bool, optionsRouteModelResolver RouteModelResolver) ([]routeCandidateV2, bool) {
	return routeV2EntriesFromCacheWithRouteFilter(group, routeCandidates, filter, nil, excluded, excludeBeforePriority, optionsRouteModelResolver)
}

func routeV2EntriesFromCacheWithRouteFilter(group string, routeCandidates []routeModelCandidate, filter func(*Channel) bool, routeFilter RouteCandidateFilter, excluded map[int]struct{}, excludeBeforePriority bool, optionsRouteModelResolver RouteModelResolver) ([]routeCandidateV2, bool) {
	entries, cacheHit, _ := routeV2EntriesFromCacheWithRouteFilterState(group, routeCandidates, filter, routeFilter, excluded, excludeBeforePriority, optionsRouteModelResolver)
	return entries, cacheHit
}

// routeV2EntriesFromCacheWithRouteFilterState also reports whether the cache
// snapshot was complete. A complete snapshot may legitimately produce zero
// entries after request-scoped filtering; that is different from a cache miss
// or an incomplete snapshot and should not trigger an unnecessary DB reload.
// A mapping resolver error is different: the cached candidate could not be
// evaluated against the request's immutable route identity, so the database
// selector must get a chance to rebuild the candidate from the same resolver.
func routeV2EntriesFromCacheWithRouteFilterState(group string, routeCandidates []routeModelCandidate, filter func(*Channel) bool, routeFilter RouteCandidateFilter, excluded map[int]struct{}, excludeBeforePriority bool, optionsRouteModelResolver RouteModelResolver) ([]routeCandidateV2, bool, bool) {
	if !routeCandidateCacheV2Ready || len(routeCandidateCacheV2) == 0 || routeCandidateCacheV2KnownByChannel == nil {
		return nil, false, false
	}
	entries := make([]routeCandidateV2, 0)
	cacheHit := false
	cacheComplete := true
	for rank, candidate := range routeCandidates {
		key := routeCandidateCacheKey(group, candidate.model)
		cached, ok := routeCandidateCacheV2[key]
		if !ok {
			// A complete snapshot records enabled abilities in the legacy index
			// even when mapping validation excludes every V2 entry. Therefore a
			// missing V2 key is a cache miss whenever that model is known to have
			// candidates. This prevents a partial V2 map from hiding a DB-only
			// fallback route.
			if _, modelKnown := routeCandidateCacheV2KnownByChannel[key]; modelKnown {
				cacheComplete = false
				break
			}
			continue
		}
		// A key can remain present while an incremental channel/ability update
		// has only refreshed the independent known-channel index. Comparing the
		// two sets is necessary: checking key existence alone would let the old
		// slice hide a newly added lower-priority fallback channel. The index also
		// intentionally includes unbackfilled or invalid-mapping abilities, so a
		// mismatch conservatively takes the authoritative DB path.
		if !routeV2CacheKeyHasCompleteChannelSet(key, cached) {
			cacheComplete = false
			break
		}
		cacheHit = true
		for _, entry := range cached {
			if entry.Channel == nil {
				continue
			}
			entry.CandidateRank = rank
			entry.RequestedModel = candidate.model
			// The cache stores a validated model for its ability key. Resolve the
			// original request model from the same request-scoped snapshot when one
			// is supplied (or directly when this low-level helper is used) so compact
			// fallback candidates retain explicit mapping semantics without a second
			// compact-only JSON parser.
			resolvedCandidate, mappingErr := prepareResolvedRouteCandidate(optionsRouteModelResolver, entry.Channel, candidate)
			if mappingErr != nil {
				cacheComplete = false
				continue
			}
			entry.MappingApplied = resolvedCandidate.mappingApplied
			resolvedCandidate.effectiveModel = strings.TrimSpace(resolvedCandidate.effectiveModel)
			if resolvedCandidate.effectiveModel == "" {
				continue
			}
			entry.EffectiveRouteModel = resolvedCandidate.effectiveModel
			if !channelSupportsCompactRouteCandidate(entry.Channel, resolvedCandidate) {
				continue
			}
			if !routeCandidateAllowed(entry.Channel, resolvedCandidate, filter, routeFilter) {
				continue
			}
			if excludeBeforePriority {
				if _, skip := excluded[entry.Channel.Id]; skip {
					continue
				}
			}
			entry.Channel = entry.Channel.CloneForCache()
			entries = append(entries, entry)
		}
	}
	if !cacheComplete {
		// The requested key was present in the cache, but at least one cached
		// candidate could not be evaluated (for example, an invalid mapping).
		// Preserve the hit bit for diagnostics while marking the snapshot
		// incomplete so callers take the authoritative database fallback.
		return nil, cacheHit, false
	}
	return normalizeV2RouteEntries(collapseV2RouteCandidates(entries)), cacheHit, true
}

// routeV2CacheKeyHasCompleteChannelSet verifies that the V2 cache slice and
// its independent completeness index describe the same active ability set.
// The cache may legitimately have no entries for an ability whose mapping is
// invalid or whose V2 fields are not backfilled; without a separate exclusion
// reason index, that ambiguity must be treated as incomplete and resolved from
// the database rather than allowing a stale partial slice to win.
func routeV2CacheKeyHasCompleteChannelSet(key string, cached []routeCandidateV2) bool {
	expected, ok := routeCandidateCacheV2KnownByChannel[key]
	if !ok {
		return false
	}
	actual := make(map[int]struct{}, len(cached))
	for _, entry := range cached {
		if entry.Channel == nil || entry.Channel.Id <= 0 {
			return false
		}
		actual[entry.Channel.Id] = struct{}{}
	}
	if len(actual) != len(expected) {
		return false
	}
	for channelID := range expected {
		if _, ok := actual[channelID]; !ok {
			return false
		}
	}
	return true
}

func routeV2EntriesFromDatabase(group string, routeCandidates []routeModelCandidate, filter func(*Channel) bool, excluded map[int]struct{}, excludeBeforePriority bool, resolver RouteModelResolver) ([]routeCandidateV2, error) {
	return routeV2EntriesFromDatabaseWithRouteFilter(group, routeCandidates, filter, nil, excluded, excludeBeforePriority, resolver)
}

func routeV2EntriesFromDatabaseWithRouteFilter(group string, routeCandidates []routeModelCandidate, filter func(*Channel) bool, routeFilter RouteCandidateFilter, excluded map[int]struct{}, excludeBeforePriority bool, resolver RouteModelResolver) ([]routeCandidateV2, error) {
	ensureAbilityColumnsInitialized()
	db := DB
	if db == nil {
		return nil, errRouteDatabaseUnavailable
	}
	entries := make([]routeCandidateV2, 0)
	for rank, candidate := range routeCandidates {
		var abilities []Ability
		if err := db.Where(commonGroupCol+" = ? AND model = ? AND enabled = ?", group, candidate.model, true).Order("channel_id ASC").Find(&abilities).Error; err != nil {
			return nil, err
		}
		if len(abilities) == 0 {
			continue
		}
		ids := make([]int, 0, len(abilities))
		for _, ability := range abilities {
			ids = append(ids, ability.ChannelId)
		}
		var channels []Channel
		if err := db.Where("id IN ?", ids).Order("id ASC").Find(&channels).Error; err != nil {
			return nil, err
		}
		byID := make(map[int]*Channel, len(channels))
		for i := range channels {
			byID[channels[i].Id] = &channels[i]
		}
		for _, ability := range abilities {
			channel := byID[ability.ChannelId]
			if channel == nil || channel.Status != common.ChannelStatusEnabled {
				continue
			}
			// Keep every eligible raw ability until the common collapse step. A
			// channel can be represented by multiple raw model keys and must not
			// inherit whichever row the database happened to return first.
			if excludeBeforePriority {
				if _, skip := excluded[channel.Id]; skip {
					continue
				}
			}
			priority, priorityConfigured := ability.GetConfiguredRoutePriority()
			weight, weightConfigured := ability.GetConfiguredRouteWeightBPS()
			if !priorityConfigured || !weightConfigured {
				// V2 never reads legacy priority/weight. The migration endpoint must
				// populate both fields before this ability can route.
				continue
			}
			resolvedCandidate, mappingErr := prepareResolvedRouteCandidate(resolver, channel, candidate)
			if mappingErr != nil {
				continue
			}
			if !channelSupportsCompactRouteCandidate(channel, resolvedCandidate) {
				continue
			}
			if !routeCandidateAllowed(channel, resolvedCandidate, filter, routeFilter) {
				continue
			}
			entry := routeCandidateV2{Group: group, RequestedModel: candidate.model, EffectiveRouteModel: resolvedCandidate.effectiveModel, MappingApplied: resolvedCandidate.mappingApplied, Channel: channel, RoutePriority: priority, RouteWeightBPS: weight, LegacyWeight: ability.Weight, ExplicitWeight: true, CandidateRank: rank}
			entries = append(entries, entry)
		}
	}
	return normalizeV2RouteEntries(collapseV2RouteCandidates(entries)), nil
}

func chooseV2RouteEntry(entries []routeCandidateV2, retry int, excluded map[int]struct{}, requestedModel string, excludeBeforePriority bool) *routeCandidateV2 {
	_ = excludeBeforePriority
	if len(entries) == 0 {
		return nil
	}
	if _, compact := ratio_setting.CompactBaseModelName(strings.TrimSpace(requestedModel)); compact {
		preferred := make([]routeCandidateV2, 0, len(entries))
		fallback := make([]routeCandidateV2, 0, len(entries))
		for _, entry := range entries {
			if entry.Channel == nil {
				continue
			}
			// A zero-bps candidate is disabled for active routing. Do not let
			// it establish the compact preference pool and hide a positive-bps
			// ordinary fallback.
			if entry.RouteWeightBPS == 0 {
				continue
			}
			if channelPrefersResponsesCompactionWhenNamespaceUnsupported(entry.Channel) {
				if _, skip := excluded[entry.Channel.Id]; skip {
					continue
				}
				preferred = append(preferred, entry)
			} else {
				fallback = append(fallback, entry)
			}
		}
		if len(preferred) > 0 {
			entries = preferred
		} else if len(fallback) > 0 {
			entries = fallback
		}
	}
	type scopeKey struct {
		priority  int64
		effective string
	}
	byScope := make(map[scopeKey][]routeCandidateV2)
	scopeRank := make(map[scopeKey]int)
	for _, entry := range entries {
		if entry.Channel == nil || entry.RouteWeightBPS == 0 {
			continue
		}
		// Runtime exclusions (already tried, health cooldown, body limits and
		// saturated concurrency) are eligibility filters, not priority tiers.
		// Remove them before building the scope list so an exhausted high tier
		// cannot occupy the retry index and hide an available lower tier.
		if _, skip := excluded[entry.Channel.Id]; skip {
			continue
		}
		key := scopeKey{priority: entry.RoutePriority, effective: entry.EffectiveRouteModel}
		byScope[key] = append(byScope[key], entry)
		if previous, ok := scopeRank[key]; !ok || entry.CandidateRank < previous {
			scopeRank[key] = entry.CandidateRank
		}
	}
	scopes := make([]scopeKey, 0, len(byScope))
	for scope := range byScope {
		scopes = append(scopes, scope)
	}
	sort.Slice(scopes, func(i, j int) bool {
		if scopes[i].priority != scopes[j].priority {
			return scopes[i].priority > scopes[j].priority
		}
		if scopeRank[scopes[i]] != scopeRank[scopes[j]] {
			return scopeRank[scopes[i]] < scopeRank[scopes[j]]
		}
		return scopes[i].effective < scopes[j].effective
	})
	if len(scopes) == 0 {
		return nil
	}
	if retry < 0 {
		retry = 0
	}
	if retry >= len(scopes) {
		retry = len(scopes) - 1
	}
	target := byScope[scopes[retry]]
	if len(target) == 0 {
		return nil
	}
	var total uint64
	for _, entry := range target {
		total += uint64(entry.RouteWeightBPS)
	}
	if total == 0 {
		return nil
	}
	point := uint64(common.GetRandomInt(int(total)))
	for index := range target {
		entry := &target[index]
		weight := uint64(entry.RouteWeightBPS)
		if point < weight {
			return entry
		}
		point -= weight
	}
	return &target[len(target)-1]
}

func chooseV2RouteChannel(entries []routeCandidateV2, retry int, excluded map[int]struct{}, requestedModel string, excludeBeforePriority bool) *Channel {
	selected := chooseV2RouteEntry(entries, retry, excluded, requestedModel, excludeBeforePriority)
	if selected == nil {
		return nil
	}
	return selected.Channel
}
