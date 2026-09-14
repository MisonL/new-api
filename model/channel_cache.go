package model

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

var group2model2channels map[string]map[string][]int // enabled channel
var channelsIDM map[int]*Channel                     // all channels include disabled
var channelSyncLock sync.RWMutex
var channelCacheRefreshInFlight atomic.Bool
var channelCacheRefreshPending atomic.Bool
var channelCacheRefreshRequest atomic.Uint64

// channelCacheGeneration changes whenever the in-memory channel snapshot is
// mutated. Database-backed route-cache refreshes take a generation stamp
// before reading abilities and discard their result if a newer cache mutation
// completed while the read was in flight. Without this guard, a slow refresh
// can publish an old model_mapping/priority/weight snapshot over a newer edit.
var channelCacheGeneration atomic.Uint64

// channelCacheSnapshotEpoch changes whenever a complete channel snapshot is
// published. channelCacheVersions changes for incremental mutations of one
// channel. Cache reloads use both values, plus the cached pointer, to reject a
// database result that was read before a newer cache mutation. The values are
// protected by channelSyncLock.
var channelCacheSnapshotEpoch uint64
var channelCacheVersions map[int]uint64

type channelCacheReloadToken struct {
	epoch   uint64
	version uint64
	channel *Channel
}

func captureChannelCacheReloadTokenLocked(id int) channelCacheReloadToken {
	return channelCacheReloadToken{
		epoch:   channelCacheSnapshotEpoch,
		version: channelCacheVersions[id],
		channel: channelsIDM[id],
	}
}

func markChannelCacheMutationLocked(id int) {
	if id <= 0 {
		return
	}
	if channelCacheVersions == nil {
		channelCacheVersions = make(map[int]uint64)
	}
	channelCacheVersions[id]++
}

func channelCacheReloadTokenMatchesLocked(id int, token channelCacheReloadToken) bool {
	return channelCacheSnapshotEpoch == token.epoch &&
		channelCacheVersions[id] == token.version &&
		channelsIDM[id] == token.channel
}

// legacyRouteCandidateCache keeps the ability-level priority/weight metadata
// that the legacy selector needs after request-aware filtering. The historical
// group2model2channels index intentionally stores only channel IDs for other
// callers, so it cannot be used to reconstruct an ability's route priority.
type legacyRouteCandidateCacheEntry struct {
	channel       *Channel
	priority      int64
	abilityWeight uint
	channelWeight uint
}

var legacyRouteCandidateCache map[string][]legacyRouteCandidateCacheEntry

// routeCandidateCacheV2Ready is true only after a complete channel/ability
// snapshot has been installed. Incremental channel updates preserve this
// state; a manually populated or partially rebuilt route map must fall back
// to the database instead of being treated as a complete hit.
var routeCandidateCacheV2Ready bool

// routeCandidateCacheV2KnownByChannel is the independent completeness index
// for the V2 route cache. The legacy group2model2channels index is a separate
// compatibility view and may be disabled, stale, or rebuilt independently;
// it must never decide whether a V2 cache key is complete. The value tracks
// the active channel IDs that contribute each raw group/model key, including
// rows that are not yet V2-configured or whose mapping is invalid. Such rows
// still require a database fallback to prove that the V2 result is empty.
var routeCandidateCacheV2KnownByChannel map[string]map[int]struct{}

func channelCacheSyncEnabled() bool {
	return common.GetEnvOrDefaultBool("CHANNEL_CACHE_SYNC_ENABLED", true)
}

// InitChannelCache rebuilds the in-memory channel cache from database state.
func InitChannelCache() {
	if !common.MemoryCacheEnabled {
		return
	}
	db := DB
	for {
		channelCacheRefreshRequest.Add(1)
		channelCacheRefreshPending.Store(true)
		if channelCacheRefreshInFlight.CompareAndSwap(false, true) {
			runChannelCacheRefreshLoop(db)
			return
		}
		for channelCacheRefreshInFlight.Load() {
			time.Sleep(10 * time.Millisecond)
		}
		// The in-flight refresh may have consumed the pending marker just
		// before it released the flag. Retry when this caller's request is
		// still pending instead of returning with a stale cache snapshot.
		if !channelCacheRefreshPending.Load() {
			return
		}
	}
}

func buildChannelCacheSnapshot(db *gorm.DB) error {
	// A full rebuild reads channels and abilities as one database snapshot and
	// publishes only if no incremental cache mutation completed while the read
	// was in flight. This prevents the periodic refresh from undoing a newer
	// channel status, key index, or model-mapping edit.
	buildGeneration := channelCacheGeneration.Load()
	newChannelId2channel := make(map[int]*Channel)
	var channels []*Channel
	var abilities []*Ability
	if db == nil {
		return errors.New("failed to sync channels from database: database is nil")
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Find(&channels).Error; err != nil {
			return fmt.Errorf("failed to sync channels from database: %w", err)
		}
		if err := tx.Find(&abilities).Error; err != nil {
			return fmt.Errorf("failed to sync abilities from database: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	for _, channel := range channels {
		if channel == nil {
			continue
		}
		newChannelId2channel[channel.Id] = channel.CloneForCache()
	}
	newGroup2model2channels := make(map[string]map[string][]int)
	newLegacyRouteCandidateCache := make(map[string][]legacyRouteCandidateCacheEntry)
	newRouteCandidateCacheV2KnownByChannel := make(map[string]map[int]struct{})
	for _, ability := range abilities {
		if !ability.Enabled {
			continue
		}
		channel, ok := newChannelId2channel[ability.ChannelId]
		if !ok || channel.Status != common.ChannelStatusEnabled {
			continue
		}
		knownKey := routeCandidateCacheKey(ability.Group, ability.Model)
		knownChannels := newRouteCandidateCacheV2KnownByChannel[knownKey]
		if knownChannels == nil {
			knownChannels = make(map[int]struct{})
			newRouteCandidateCacheV2KnownByChannel[knownKey] = knownChannels
		}
		knownChannels[ability.ChannelId] = struct{}{}
		if _, ok := newGroup2model2channels[ability.Group]; !ok {
			newGroup2model2channels[ability.Group] = make(map[string][]int)
		}
		newGroup2model2channels[ability.Group][ability.Model] = append(
			newGroup2model2channels[ability.Group][ability.Model],
			ability.ChannelId,
		)
		newLegacyRouteCandidateCache[routeCandidateCacheKey(ability.Group, ability.Model)] = append(
			newLegacyRouteCandidateCache[routeCandidateCacheKey(ability.Group, ability.Model)],
			legacyRouteCandidateCacheEntry{
				channel:       channel.CloneForCache(),
				priority:      legacyAbilityPriority(*ability),
				abilityWeight: ability.Weight,
				channelWeight: uint(channel.GetWeight()),
			},
		)
	}
	newRouteCandidateCacheV2 := buildRouteCandidateCacheV2(abilities, newChannelId2channel)

	// dedupe and sort by priority
	for group, model2channels := range newGroup2model2channels {
		for model, channels := range model2channels {
			seen := make(map[int]struct{}, len(channels))
			deduped := make([]int, 0, len(channels))
			for _, channelId := range channels {
				if _, ok := seen[channelId]; ok {
					continue
				}
				seen[channelId] = struct{}{}
				deduped = append(deduped, channelId)
			}
			sort.Slice(deduped, func(i, j int) bool {
				return newChannelId2channel[deduped[i]].GetPriority() > newChannelId2channel[deduped[j]].GetPriority()
			})
			newGroup2model2channels[group][model] = deduped
		}
	}

	channelSyncLock.Lock()
	if channelCacheGeneration.Load() != buildGeneration {
		channelSyncLock.Unlock()
		// Let the refresh loop retry against the latest incremental snapshot. The
		// loop retains the database handle captured at scheduling time, so it does
		// not race with tests or lifecycle code replacing the global DB pointer.
		channelCacheRefreshPending.Store(true)
		return nil
	}
	group2model2channels = newGroup2model2channels
	legacyRouteCandidateCache = newLegacyRouteCandidateCache
	routeCandidateCacheV2 = newRouteCandidateCacheV2
	routeCandidateCacheV2KnownByChannel = newRouteCandidateCacheV2KnownByChannel
	//channelsIDM = newChannelId2channel
	for i, channel := range newChannelId2channel {
		if channel.ChannelInfo.IsMultiKey {
			channel.Keys = channel.GetKeys()
			if channel.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling {
				if oldChannel, ok := channelsIDM[i]; ok {
					// 存在旧的渠道，如果是多key且轮询，保留轮询索引信息
					if oldChannel.ChannelInfo.IsMultiKey && oldChannel.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling {
						channel.ChannelInfo.MultiKeyPollingIndex = oldChannel.ChannelInfo.MultiKeyPollingIndex
					}
				}
			}
		}
	}
	channelsIDM = newChannelId2channel
	routeCandidateCacheV2Ready = true
	channelCacheSnapshotEpoch++
	channelCacheGeneration.Add(1)
	channelSyncLock.Unlock()
	common.SysLog("channels synced from database")
	return nil
}

func buildLegacyRouteCandidateCacheFromAbilities(abilities []*Ability, channels map[int]*Channel) map[string][]legacyRouteCandidateCacheEntry {
	result := make(map[string][]legacyRouteCandidateCacheEntry)
	for _, ability := range abilities {
		if ability == nil || !ability.Enabled {
			continue
		}
		channel := channels[ability.ChannelId]
		if channel == nil || channel.Status != common.ChannelStatusEnabled {
			continue
		}
		key := routeCandidateCacheKey(ability.Group, ability.Model)
		result[key] = append(result[key], legacyRouteCandidateCacheEntry{
			channel:       channel,
			priority:      legacyAbilityPriority(*ability),
			abilityWeight: ability.Weight,
			channelWeight: uint(channel.GetWeight()),
		})
	}
	return result
}

func runChannelCacheRefreshLoop(db *gorm.DB) {
	ownsInFlight := true
	defer func() {
		if ownsInFlight {
			channelCacheRefreshInFlight.Store(false)
		}
	}()
	for {
		requestGeneration := channelCacheRefreshRequest.Load()
		channelCacheRefreshPending.Store(false)
		if err := buildChannelCacheSnapshot(db); err != nil {
			common.SysError(err.Error())
		}
		if channelCacheRefreshPending.Load() || channelCacheRefreshRequest.Load() != requestGeneration {
			continue
		}
		// Close the in-flight window before the final pending check. A request
		// arriving while the loop was finishing may have observed in-flight=true
		// and therefore could not start another loop; take ownership of that
		// pending request here instead of leaving it for the next periodic tick.
		channelCacheRefreshInFlight.Store(false)
		ownsInFlight = false
		if !channelCacheRefreshPending.Load() && channelCacheRefreshRequest.Load() == requestGeneration {
			return
		}
		if channelCacheRefreshInFlight.CompareAndSwap(false, true) {
			ownsInFlight = true
			continue
		}
		return
	}
}

// SyncChannelCache periodically refreshes the in-memory channel cache.
func SyncChannelCache(frequency int) {
	if !channelCacheSyncEnabled() {
		common.SysLog("channel cache sync disabled by CHANNEL_CACHE_SYNC_ENABLED")
		return
	}
	common.SleepBeforeMaintenanceLoop(common.ChannelCacheSyncInitialDelay)
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		common.SysLog("syncing channels from database")
		InitChannelCache()
	}
}

func requestChannelCacheRefreshAsync() {
	if !common.MemoryCacheEnabled {
		return
	}
	db := DB
	channelCacheRefreshRequest.Add(1)
	channelCacheRefreshPending.Store(true)
	if !channelCacheRefreshInFlight.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				common.SysLog(fmt.Sprintf("InitChannelCache panic: %v", r))
			}
		}()
		runChannelCacheRefreshLoop(db)
	}()
}

func getRandomSatisfiedChannelFromCache(group string, model string, retry int, excluded map[int]struct{}) (*Channel, error, bool) {
	return getRandomSatisfiedChannelFromCacheWithFilter(group, model, retry, excluded, nil)
}

func getRandomSatisfiedChannelFromCacheAfterExclusion(group string, model string, retry int, excluded map[int]struct{}) (*Channel, error, bool) {
	return getRandomSatisfiedChannelFromCacheAfterExclusionWithFilter(group, model, retry, excluded, nil)
}

func getRandomSatisfiedChannelFromCacheWithFilter(group string, model string, retry int, excluded map[int]struct{}, candidateFilter func(*Channel) bool) (*Channel, error, bool) {
	return getRandomSatisfiedChannelFromCacheWithPriorityModeAndResolver(group, model, retry, excluded, false, candidateFilter, nil)
}

func getRandomSatisfiedChannelFromCacheAfterExclusionWithFilter(group string, model string, retry int, excluded map[int]struct{}, candidateFilter func(*Channel) bool) (*Channel, error, bool) {
	return getRandomSatisfiedChannelFromCacheWithPriorityModeAndResolver(group, model, retry, excluded, true, candidateFilter, nil)
}

func getRandomSatisfiedChannelFromCacheWithPriorityMode(group string, model string, retry int, excluded map[int]struct{}, excludeBeforePriority bool, candidateFilter func(*Channel) bool) (*Channel, error, bool) {
	return getRandomSatisfiedChannelFromCacheWithPriorityModeAndResolver(group, model, retry, excluded, excludeBeforePriority, candidateFilter, nil)
}

func getRandomSatisfiedChannelFromCacheWithPriorityModeAndResolver(group string, model string, retry int, excluded map[int]struct{}, excludeBeforePriority bool, candidateFilter func(*Channel) bool, resolver RouteModelResolver) (*Channel, error, bool) {
	return getRandomSatisfiedChannelFromCacheWithPriorityModeAndRouteFilterAndResolver(group, model, retry, excluded, excludeBeforePriority, candidateFilter, nil, resolver, nil)
}

func getRandomSatisfiedChannelFromCacheWithPriorityModeAndRouteFilterAndResolver(group string, model string, retry int, excluded map[int]struct{}, excludeBeforePriority bool, candidateFilter func(*Channel) bool, routeFilter RouteCandidateFilter, resolver RouteModelResolver, diagnostics *dto.ChannelRouteSelectionDiagnostics) (*Channel, error, bool) {
	channel, err, cacheHit, _ := getRandomSatisfiedChannelFromCacheWithPriorityModeAndRouteFilterAndResolverWithCompleteness(group, model, retry, excluded, excludeBeforePriority, candidateFilter, routeFilter, resolver, diagnostics)
	return channel, err, cacheHit
}

func getRandomSatisfiedChannelFromCacheWithPriorityModeAndRouteFilterAndResolverWithCompleteness(group string, model string, retry int, excluded map[int]struct{}, excludeBeforePriority bool, candidateFilter func(*Channel) bool, routeFilter RouteCandidateFilter, resolver RouteModelResolver, diagnostics *dto.ChannelRouteSelectionDiagnostics) (*Channel, error, bool, bool) {
	cacheHit := false
	cacheComplete := true
	var lastErr error
	routeCandidates := getGroupModelRouteCandidateMeta(model)
	if shouldPoolCompactRouteCandidates(routeCandidates) {
		return getRandomSatisfiedPooledRouteModelsFromCacheWithRouteFilterAndCompleteness(group, routeCandidates, retry, excluded, excludeBeforePriority, candidateFilter, routeFilter, resolver, diagnostics)
	}
	for _, routeCandidate := range routeCandidates {
		channel, err, hit, complete := getRandomSatisfiedRouteModelFromCacheWithRouteFilterAndCompleteness(group, routeCandidate, retry, excluded, excludeBeforePriority, candidateFilter, routeFilter, resolver, diagnostics)
		if !hit {
			cacheComplete = false
			continue
		}
		cacheHit = true
		if !complete {
			cacheComplete = false
		}
		if channel != nil || err != nil {
			return channel, err, true, cacheComplete
		}
		lastErr = err
	}
	return nil, lastErr, cacheHit, cacheComplete
}

func getRandomSatisfiedPooledRouteModelsFromCache(group string, routeCandidates []routeModelCandidate, retry int, excluded map[int]struct{}, excludeBeforePriority bool, candidateFilter func(*Channel) bool, resolver RouteModelResolver) (*Channel, error, bool) {
	return getRandomSatisfiedPooledRouteModelsFromCacheWithRouteFilter(group, routeCandidates, retry, excluded, excludeBeforePriority, candidateFilter, nil, resolver, nil)
}

func getRandomSatisfiedRouteModelFromCache(group string, routeCandidate routeModelCandidate, retry int, excluded map[int]struct{}, excludeBeforePriority bool, candidateFilter func(*Channel) bool, resolver RouteModelResolver) (*Channel, error, bool) {
	return getRandomSatisfiedRouteModelFromCacheWithRouteFilter(group, routeCandidate, retry, excluded, excludeBeforePriority, candidateFilter, nil, resolver, nil)
}

func getRandomSatisfiedPooledRouteModelsFromCacheWithRouteFilter(group string, routeCandidates []routeModelCandidate, retry int, excluded map[int]struct{}, excludeBeforePriority bool, candidateFilter func(*Channel) bool, routeFilter RouteCandidateFilter, resolver RouteModelResolver, diagnostics *dto.ChannelRouteSelectionDiagnostics) (*Channel, error, bool) {
	channel, err, cacheHit, _ := getRandomSatisfiedPooledRouteModelsFromCacheWithRouteFilterAndCompleteness(group, routeCandidates, retry, excluded, excludeBeforePriority, candidateFilter, routeFilter, resolver, diagnostics)
	return channel, err, cacheHit
}

func getRandomSatisfiedPooledRouteModelsFromCacheWithRouteFilterAndCompleteness(group string, routeCandidates []routeModelCandidate, retry int, excluded map[int]struct{}, excludeBeforePriority bool, candidateFilter func(*Channel) bool, routeFilter RouteCandidateFilter, resolver RouteModelResolver, diagnostics *dto.ChannelRouteSelectionDiagnostics) (*Channel, error, bool, bool) {
	cacheHit := false
	cacheComplete := true
	seen := make(map[string]struct{})
	candidates := make([]routeSelectionCandidate, 0, len(routeCandidates))
	for _, routeCandidate := range routeCandidates {
		cachedEntries, hit := getLegacyRouteCandidateCacheEntries(group, routeCandidate)
		if !hit {
			cacheComplete = false
			continue
		}
		cacheHit = true
		if len(cachedEntries) == 0 {
			cacheComplete = false
		}
		for _, cachedEntry := range cachedEntries {
			channel := cachedEntry.channel
			if channel == nil {
				continue
			}
			resolvedCandidate, mappingErr := prepareResolvedRouteCandidate(resolver, channel, routeCandidate)
			if mappingErr != nil {
				cacheComplete = false
				if !IsChannelModelMappingError(mappingErr) {
					return nil, mappingErr, true, false
				}
				continue
			}
			if !channelSupportsCompactRouteCandidate(channel, resolvedCandidate) || !routeCandidateAllowed(channel, resolvedCandidate, candidateFilter, routeFilter) {
				continue
			}
			key := fmt.Sprintf("%d\x00%s\x00%d", channel.Id, strings.TrimSpace(resolvedCandidate.effectiveModel), cachedEntry.priority)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			candidates = append(candidates, routeSelectionCandidate{
				channel:        channel,
				routeCandidate: resolvedCandidate,
				priority:       cachedEntry.priority,
				weight:         cachedEntry.channelWeight,
				weightSource:   "legacy_channel_weight",
			})
		}
	}
	if len(candidates) == 0 {
		return nil, nil, cacheHit, cacheComplete
	}
	candidates = filterRouteSelectionCandidatesByCompactPreference(candidates, true, excluded)
	candidates = filterRouteSelectionCandidatesByRetryPriority(candidates, retry, excluded, excludeBeforePriority)
	if len(candidates) == 0 {
		return nil, nil, true, cacheComplete
	}
	selected := chooseWeightedRouteSelectionCandidate(candidates, common.GetRandomInt)
	if selected == nil {
		return nil, nil, true, cacheComplete
	}
	bindSelectedRouteCandidateDiagnostics(diagnostics, group, routeCandidates[0].model, *selected, "memory")
	return selected.channel.CloneForCache(), nil, true, cacheComplete
}

func getRandomSatisfiedRouteModelFromCacheWithRouteFilter(group string, routeCandidate routeModelCandidate, retry int, excluded map[int]struct{}, excludeBeforePriority bool, candidateFilter func(*Channel) bool, routeFilter RouteCandidateFilter, resolver RouteModelResolver, diagnostics *dto.ChannelRouteSelectionDiagnostics) (*Channel, error, bool) {
	channel, err, cacheHit, _ := getRandomSatisfiedRouteModelFromCacheWithRouteFilterAndCompleteness(group, routeCandidate, retry, excluded, excludeBeforePriority, candidateFilter, routeFilter, resolver, diagnostics)
	return channel, err, cacheHit
}

func getRandomSatisfiedRouteModelFromCacheWithRouteFilterAndCompleteness(group string, routeCandidate routeModelCandidate, retry int, excluded map[int]struct{}, excludeBeforePriority bool, candidateFilter func(*Channel) bool, routeFilter RouteCandidateFilter, resolver RouteModelResolver, diagnostics *dto.ChannelRouteSelectionDiagnostics) (*Channel, error, bool, bool) {
	cachedEntries, cacheHit := getLegacyRouteCandidateCacheEntries(group, routeCandidate)
	if !cacheHit {
		return nil, nil, false, false
	}
	cacheComplete := len(cachedEntries) > 0
	candidates := make([]routeSelectionCandidate, 0, len(cachedEntries))
	seen := make(map[string]struct{}, len(cachedEntries))
	for _, cachedEntry := range cachedEntries {
		channel := cachedEntry.channel
		if channel == nil {
			continue
		}
		resolvedCandidate, mappingErr := prepareResolvedRouteCandidate(resolver, channel, routeCandidate)
		if mappingErr != nil {
			cacheComplete = false
			if !IsChannelModelMappingError(mappingErr) {
				return nil, mappingErr, true, false
			}
			continue
		}
		if !channelSupportsCompactRouteCandidate(channel, resolvedCandidate) || !routeCandidateAllowed(channel, resolvedCandidate, candidateFilter, routeFilter) {
			continue
		}
		key := fmt.Sprintf("%d\x00%s\x00%d", channel.Id, strings.TrimSpace(resolvedCandidate.effectiveModel), cachedEntry.priority)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		candidates = append(candidates, routeSelectionCandidate{
			channel:        channel,
			routeCandidate: resolvedCandidate,
			priority:       cachedEntry.priority,
			weight:         cachedEntry.abilityWeight,
			weightSource:   "legacy_ability_weight",
		})
	}
	if len(candidates) == 0 {
		return nil, nil, true, cacheComplete
	}
	candidates = filterRouteSelectionCandidatesByCompactPreference(candidates, routeCandidate.compactRequest, excluded)
	candidates = filterRouteSelectionCandidatesByRetryPriority(candidates, retry, excluded, excludeBeforePriority)
	if len(candidates) == 0 {
		return nil, nil, true, cacheComplete
	}
	selected := chooseWeightedRouteSelectionCandidate(candidates, common.GetRandomInt)
	if selected == nil {
		return nil, nil, true, cacheComplete
	}
	bindSelectedRouteCandidateDiagnostics(diagnostics, group, routeCandidate.model, *selected, "memory")
	return selected.channel.CloneForCache(), nil, true, cacheComplete
}

// getLegacyRouteCandidateCacheEntries returns ability metadata for one raw
// route candidate. The fallback projection is retained only while the
// metadata-rich cache has not been initialized at all, such as a startup
// transition that still has only the historical channel-ID cache. Once the
// metadata cache exists, a missing key is an authoritative miss: projecting
// group2model2channels at that point could resurrect a stale model identity or
// reconstruct route weights from the wrong level.
func getLegacyRouteCandidateCacheEntries(group string, routeCandidate routeModelCandidate) ([]legacyRouteCandidateCacheEntry, bool) {
	key := routeCandidateCacheKey(group, routeCandidate.model)
	if legacyRouteCandidateCache != nil {
		entries, ok := legacyRouteCandidateCache[key]
		return entries, ok
	}
	channels := group2model2channels[group][routeCandidate.model]
	if len(channels) == 0 {
		return nil, false
	}
	entries := make([]legacyRouteCandidateCacheEntry, 0, len(channels))
	for _, channelID := range channels {
		channel := channelsIDM[channelID]
		if channel == nil {
			continue
		}
		entries = append(entries, legacyRouteCandidateCacheEntry{
			channel:       channel.CloneForCache(),
			priority:      channel.GetPriority(),
			abilityWeight: uint(channel.GetWeight()),
			channelWeight: uint(channel.GetWeight()),
		})
	}
	return entries, true
}

func chooseCachedRouteChannel(targetChannels []*Channel, retry int, excluded map[int]struct{}) (*Channel, error) {
	return chooseCachedRouteChannelWithRandom(targetChannels, retry, excluded, common.GetRandomInt)
}

func chooseCachedRouteChannelWithRandom(targetChannels []*Channel, retry int, excluded map[int]struct{}, randomInt func(int) int) (*Channel, error) {
	return chooseCachedRouteChannelWithPriorityMode(targetChannels, retry, excluded, false, randomInt)
}

func chooseCachedRouteChannelWithPriorityMode(targetChannels []*Channel, retry int, excluded map[int]struct{}, excludeBeforePriority bool, randomInt func(int) int) (*Channel, error) {
	if len(targetChannels) == 0 {
		return nil, nil
	}
	if excludeBeforePriority && len(excluded) > 0 {
		filteredChannels := make([]*Channel, 0, len(targetChannels))
		for _, channel := range targetChannels {
			if _, skip := excluded[channel.Id]; skip {
				continue
			}
			filteredChannels = append(filteredChannels, channel)
		}
		targetChannels = filteredChannels
		if len(targetChannels) == 0 {
			return nil, nil
		}
	}
	uniquePriorities := make(map[int]bool)
	for _, channel := range targetChannels {
		uniquePriorities[int(channel.GetPriority())] = true
	}
	var sortedUniquePriorities []int
	for priority := range uniquePriorities {
		sortedUniquePriorities = append(sortedUniquePriorities, priority)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(sortedUniquePriorities)))

	if retry >= len(uniquePriorities) {
		retry = len(uniquePriorities) - 1
	}
	targetPriority := int64(sortedUniquePriorities[retry])

	filteredChannels := make([]*Channel, 0, len(targetChannels))
	for _, channel := range targetChannels {
		if !excludeBeforePriority {
			if _, skip := excluded[channel.Id]; skip {
				continue
			}
		}
		if channel.GetPriority() == targetPriority {
			filteredChannels = append(filteredChannels, channel)
		}
	}
	targetChannels = filteredChannels
	if len(targetChannels) == 0 {
		return nil, nil
	}

	weights := make([]uint, 0, len(targetChannels))
	for _, channel := range targetChannels {
		weights = append(weights, uint(channel.GetWeight()))
	}
	return chooseWeightedChannelWithRandom(targetChannels, weights, randomInt), nil
}

// GetRandomSatisfiedChannel returns a channel for the requested group/model pair.
func GetRandomSatisfiedChannel(group string, model string, retry int) (*Channel, error) {
	return GetRandomSatisfiedChannelExcluding(group, model, retry, nil)
}

// GetRandomSatisfiedChannelExcluding returns a channel while excluding channels already tried by the current request.
func GetRandomSatisfiedChannelExcluding(group string, model string, retry int, excluded map[int]struct{}) (*Channel, error) {
	return GetRandomSatisfiedChannelExcludingWithRequestBodyLimit(group, model, retry, excluded, 0, 0, time.Time{})
}

type SatisfiedChannelSelectionOptions struct {
	RequestBodySize       int64
	BodyLimitTTLHours     int
	Now                   time.Time
	ExcludeBeforePriority bool
	// CandidateFilter is evaluated before priority and weight selection. It is
	// intentionally a callback so request-scoped route exclusions (for example
	// one mapped model or protocol route cooling down) do not get collapsed into
	// a channel-wide exclusion.
	CandidateFilter func(*Channel) bool
	// CandidatePreflight is an optional request-aware eligibility check. It runs
	// before priority/weight selection and can return a real error; the caller
	// must not observe a candidate that failed request-shape compatibility.
	CandidatePreflight func(*Channel) (bool, error)
	// RouteModelResolver resolves and validates the effective upstream model for
	// a concrete channel/candidate. The caller may memoize a prepared mapping
	// snapshot so legacy and V2 selection paths share one request-scoped result.
	RouteModelResolver RouteModelResolver
	// RouteCandidateFilter is evaluated after the candidate's model mapping has
	// been resolved. Unlike CandidateFilter, it receives the raw route-candidate
	// model and its effective upstream model, so compact pooled candidates do not
	// accidentally share a health decision for a different route identity.
	RouteCandidateFilter RouteCandidateFilter
	// Diagnostics is optional and is populated with credential-free selector
	// metadata for administrator routing traces and shadow comparisons.
	Diagnostics *dto.ChannelRouteSelectionDiagnostics
}

// CandidatePreflightError preserves the distinction between an unavailable
// candidate and a malformed request or failed request-shape prerequisite. The
// controller can map the latter to the request's explicit client-error
// contract without treating a database/selector error as a bad request.
type CandidatePreflightError struct {
	Err error
}

func (e *CandidatePreflightError) Error() string {
	if e == nil || e.Err == nil {
		return "channel candidate preflight failed"
	}
	return e.Err.Error()
}

func (e *CandidatePreflightError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// RouteModelResolver is the request-scoped mapping boundary used by channel
// selectors. It must not mutate the channel or requested model.
type RouteModelResolver func(channel *Channel, requestedModel string, compact bool) (effectiveModel string, mapped bool, err error)

// RouteCandidateFilter is the request-aware eligibility boundary for one raw
// route candidate after model_mapping has been resolved. The public request
// model and the raw candidate model may differ for compact/normalized pools.
type RouteCandidateFilter func(channel *Channel, candidateModel string, effectiveModel string) bool

type routeSelectionCandidate struct {
	channel        *Channel
	routeCandidate routeModelCandidate
	// priority is the priority of the raw ability that produced this
	// candidate. Legacy selection must retain this value after request-aware
	// filtering; channel.Priority is only a channel-level compatibility field
	// and may differ from the ability row.
	priority     int64
	weight       uint
	weightSource string
}

func routeCandidateAllowed(channel *Channel, candidate routeModelCandidate, candidateFilter func(*Channel) bool, routeFilter RouteCandidateFilter) bool {
	if channel == nil {
		return false
	}
	if candidateFilter != nil && !candidateFilter(channel) {
		return false
	}
	if routeFilter != nil && !routeFilter(channel, candidate.model, strings.TrimSpace(candidate.effectiveModel)) {
		return false
	}
	return true
}

func bindSelectedRouteCandidateDiagnostics(diag *dto.ChannelRouteSelectionDiagnostics, group string, requestedModel string, selected routeSelectionCandidate, selectionPath string) {
	if diag == nil || selected.channel == nil {
		return
	}
	group = strings.TrimSpace(group)
	requestedModel = strings.TrimSpace(requestedModel)
	effectiveModel := strings.TrimSpace(selected.routeCandidate.effectiveModel)
	if effectiveModel == "" {
		return
	}
	diag.Group = group
	diag.RequestedModel = requestedModel
	diag.SelectedCandidateModel = strings.TrimSpace(selected.routeCandidate.model)
	diag.EffectiveRouteModel = effectiveModel
	diag.SelectedChannelID = selected.channel.Id
	diag.RoutePriority = selected.priority
	diag.RouteWeightBPS = selected.weight
	diag.WeightSource = selected.weightSource
	diag.SelectionPath = selectionPath
	if diag.RoutePriority >= 0 {
		diag.RouteScope = EncodeRouteScopeID(group, effectiveModel, diag.RoutePriority)
	}
}

func resolveRouteModel(resolver RouteModelResolver, channel *Channel, requestedModel string, compact bool) (string, bool, error) {
	if resolver != nil {
		return resolver(channel, requestedModel, compact)
	}
	return ResolveChannelModelMappingForRouting(channel.GetModelMapping(), requestedModel, compact)
}

func resolveRouteCandidateModel(resolver RouteModelResolver, channel *Channel, candidate routeModelCandidate) (string, bool, error) {
	return resolveRouteModel(resolver, channel, candidate.model, candidate.compactRequest)
}

func resolveRouteCandidateCompactCapabilityModel(resolver RouteModelResolver, channel *Channel, candidate routeModelCandidate) (string, bool, error) {
	if strings.TrimSpace(candidate.compactAliasModel) == "" || candidate.compactAliasModel == candidate.model {
		return "", false, nil
	}
	effectiveModel, mapped, err := resolveRouteModel(resolver, channel, candidate.compactAliasModel, true)
	if err != nil || !mapped {
		return "", mapped, err
	}
	return effectiveModel, mapped, nil
}

func prepareResolvedRouteCandidate(resolver RouteModelResolver, channel *Channel, candidate routeModelCandidate) (routeModelCandidate, error) {
	if channel == nil {
		return candidate, nil
	}
	if resolver == nil {
		// Low-level compatibility callers may omit a resolver. Prepare one
		// immutable mapping snapshot here so compact capability checks never
		// fall back to reparsing channel.model_mapping independently.
		mapping, err := PrepareChannelModelMappingForRouting(channel.GetModelMapping())
		if err != nil {
			return candidate, err
		}
		effectiveModel, mapped, err := mapping.Resolve(candidate.model, candidate.compactRequest)
		if err != nil {
			return candidate, err
		}
		candidate.effectiveModel = effectiveModel
		candidate.mappingApplied = mapped
		candidate.mappingSnapshotReady = true
		if strings.TrimSpace(candidate.compactAliasModel) != "" && candidate.compactAliasModel != candidate.model {
			capabilityModel, capabilityMapped, err := mapping.Resolve(candidate.compactAliasModel, true)
			if err != nil {
				return candidate, err
			}
			if capabilityMapped {
				candidate.compactCapabilityModel = capabilityModel
			}
		}
		return candidate, nil
	}
	effectiveModel, mapped, err := resolveRouteCandidateModel(resolver, channel, candidate)
	if err != nil {
		return candidate, err
	}
	candidate.effectiveModel = effectiveModel
	candidate.mappingApplied = mapped
	candidate.mappingSnapshotReady = true
	capabilityModel, _, err := resolveRouteCandidateCompactCapabilityModel(resolver, channel, candidate)
	if err != nil {
		return candidate, err
	}
	candidate.compactCapabilityModel = capabilityModel
	return candidate, nil
}

func GetRandomSatisfiedChannelExcludingWithRequestBodyLimit(
	group string,
	model string,
	retry int,
	excluded map[int]struct{},
	requestBodySize int64,
	ttlHours int,
	now time.Time,
) (*Channel, error) {
	return GetRandomSatisfiedChannelWithOptions(
		group,
		model,
		retry,
		excluded,
		SatisfiedChannelSelectionOptions{
			RequestBodySize:       requestBodySize,
			BodyLimitTTLHours:     ttlHours,
			Now:                   now,
			ExcludeBeforePriority: requestBodySize > 0,
		},
	)
}

// GetRandomSatisfiedChannelWithOptions selects a channel using explicit exclusion and body-limit semantics.
func GetRandomSatisfiedChannelWithOptions(
	group string,
	model string,
	retry int,
	excluded map[int]struct{},
	options SatisfiedChannelSelectionOptions,
) (*Channel, error) {
	// Keep the historical bool filter API for cache/DB helpers while adding an
	// error-capable request preflight boundary. Each selector path gets its own
	// closure-captured error slot: shadow comparison must never let a discarded
	// V2 preflight failure change the primary legacy result (or vice versa).
	withPreflight := func(base SatisfiedChannelSelectionOptions) (SatisfiedChannelSelectionOptions, func() error) {
		if base.CandidatePreflight == nil {
			return base, func() error { return nil }
		}
		var preflightErr error
		baseFilter := base.CandidateFilter
		preflight := base.CandidatePreflight
		base.CandidateFilter = func(channel *Channel) bool {
			if preflightErr != nil {
				return false
			}
			allowed, err := preflight(channel)
			if err != nil {
				preflightErr = err
				return false
			}
			if !allowed {
				return false
			}
			return baseFilter == nil || baseFilter(channel)
		}
		base.CandidatePreflight = nil
		return base, func() error { return preflightErr }
	}
	var channel *Channel
	var err error
	if IsChannelRoutingV2Enabled() {
		pathOptions, preflightError := withPreflight(options)
		channel, err = getRandomSatisfiedChannelV2(group, model, retry, excluded, pathOptions)
		if err := preflightError(); err != nil {
			return nil, &CandidatePreflightError{Err: err}
		}
	} else if IsChannelRoutingShadowEnabled() {
		legacyDiagnostics := &dto.ChannelRouteSelectionDiagnostics{}
		shadowDiagnostics := &dto.ChannelRouteSelectionDiagnostics{}
		legacyOptions, legacyPreflightError := withPreflight(options)
		legacyOptions.Diagnostics = legacyDiagnostics
		shadowOptions, shadowPreflightError := withPreflight(options)
		shadowOptions.Diagnostics = shadowDiagnostics
		legacyChannel, legacyErr := getRandomSatisfiedChannelLegacy(group, model, retry, excluded, legacyOptions)
		populateLegacyRouteSelectionDiagnosticsWithResolver(legacyDiagnostics, group, model, legacyChannel, excluded, legacyOptions.RouteModelResolver)
		shadowChannel, shadowErr := getRandomSatisfiedChannelV2(group, model, retry, excluded, shadowOptions)
		if shadowPreflightErr := shadowPreflightError(); shadowPreflightErr != nil {
			common.SysError(fmt.Sprintf("channel routing shadow v2 preflight failed: group=%s model=%s retry=%d error=%v", group, model, retry, shadowPreflightErr))
		}
		if shadowErr != nil {
			common.SysError(fmt.Sprintf("channel routing shadow v2 failed: group=%s model=%s retry=%d error=%v", group, model, retry, shadowErr))
		} else if legacyErr == nil && (channelIDOrZero(legacyChannel) != channelIDOrZero(shadowChannel) || !sameIntSet(legacyDiagnostics.CandidateChannelIDs, shadowDiagnostics.CandidateChannelIDs)) {
			common.SysLog(fmt.Sprintf("channel routing shadow diff: group=%s model=%s retry=%d legacy=%s v2=%s", group, model, retry, formatRouteSelectionDiagnostics(legacyChannel, legacyDiagnostics), formatRouteSelectionDiagnostics(shadowChannel, shadowDiagnostics)))
		}
		if options.Diagnostics != nil {
			*options.Diagnostics = *legacyDiagnostics
		}
		channel, err = legacyChannel, legacyErr
		if legacyPreflightErr := legacyPreflightError(); legacyPreflightErr != nil {
			return nil, &CandidatePreflightError{Err: legacyPreflightErr}
		}
	} else {
		pathOptions, preflightError := withPreflight(options)
		channel, err = getRandomSatisfiedChannelLegacy(group, model, retry, excluded, pathOptions)
		if pathOptions.Diagnostics != nil {
			populateLegacyRouteSelectionDiagnosticsWithResolver(pathOptions.Diagnostics, group, model, channel, excluded, pathOptions.RouteModelResolver)
		}
		if preflightErr := preflightError(); preflightErr != nil {
			return nil, &CandidatePreflightError{Err: preflightErr}
		}
	}
	return channel, err
}

func channelIDOrZero(channel *Channel) int {
	if channel == nil {
		return 0
	}
	return channel.Id
}

func populateLegacyRouteSelectionDiagnostics(diag *dto.ChannelRouteSelectionDiagnostics, group string, requestedModel string, channel *Channel, excluded map[int]struct{}) {
	populateLegacyRouteSelectionDiagnosticsWithResolver(diag, group, requestedModel, channel, excluded, nil)
}

func populateLegacyRouteSelectionDiagnosticsWithResolver(diag *dto.ChannelRouteSelectionDiagnostics, group string, requestedModel string, channel *Channel, excluded map[int]struct{}, resolver RouteModelResolver) {
	if diag == nil {
		return
	}
	// A selector-level entry is authoritative. The legacy compatibility
	// projection cannot reconstruct which raw candidate won when the same
	// channel appears under several compact/normalized keys, so it must not
	// overwrite the already-bound candidate identity.
	if channel != nil && diag.SelectedChannelID == channel.Id &&
		strings.TrimSpace(diag.SelectedCandidateModel) != "" &&
		strings.TrimSpace(diag.EffectiveRouteModel) != "" {
		diag.Group = strings.TrimSpace(group)
		diag.RequestedModel = strings.TrimSpace(requestedModel)
		if len(diag.CandidateChannelIDs) == 0 {
			diag.CandidateChannelIDs = legacyRouteCandidateIDs(group, requestedModel)
			diag.CandidateCount = len(diag.CandidateChannelIDs)
		}
		diag.ExcludedChannelIDs = sortedChannelIDs(excluded)
		if diag.SelectionPath == "" {
			diag.SelectionPath = "legacy"
		}
		return
	}
	diag.Group = strings.TrimSpace(group)
	diag.RequestedModel = strings.TrimSpace(requestedModel)
	diag.SelectionPath = "legacy"
	diag.CandidateChannelIDs = legacyRouteCandidateIDs(group, requestedModel)
	diag.CandidateCount = len(diag.CandidateChannelIDs)
	diag.ExcludedChannelIDs = sortedChannelIDs(excluded)
	if channel == nil {
		return
	}
	diag.SelectedChannelID = channel.Id
	diag.SelectedCandidateModel = strings.TrimSpace(requestedModel)
	diag.RoutePriority = channel.GetPriority()
	weight := channel.GetWeight()
	if weight > 0 {
		diag.RouteWeightBPS = uint(weight)
	}
	diag.WeightSource = "legacy_channel_weight"
	compact := false
	if _, compact = ratio_setting.CompactBaseModelName(requestedModel); compact {
		// The legacy selector may use a compact alias. Mapping resolution remains
		// best effort here because diagnostics must never change selection.
	}
	effective, _, err := resolveRouteModel(resolver, channel, requestedModel, compact)
	if err == nil {
		diag.EffectiveRouteModel = effective
		if diag.RoutePriority >= 0 {
			diag.RouteScope = EncodeRouteScopeID(group, effective, diag.RoutePriority)
		}
	}
}

func legacyRouteCandidateIDs(group string, requestedModel string) []int {
	seen := make(map[int]struct{})
	for _, candidate := range getGroupModelRouteCandidateMeta(requestedModel) {
		if common.MemoryCacheEnabled {
			channelSyncLock.RLock()
			for _, id := range group2model2channels[group][candidate.model] {
				seen[id] = struct{}{}
			}
			channelSyncLock.RUnlock()
			continue
		}
		if DB == nil {
			continue
		}
		var ids []int
		if err := DB.Model(&Ability{}).Where(commonGroupCol+" = ? AND model = ? AND enabled = ?", group, candidate.model, true).Pluck("channel_id", &ids).Error; err == nil {
			for _, id := range ids {
				seen[id] = struct{}{}
			}
		}
	}
	return sortedChannelIDs(seen)
}

func sortedChannelIDs(ids map[int]struct{}) []int {
	if len(ids) == 0 {
		return nil
	}
	result := make([]int, 0, len(ids))
	for id := range ids {
		if id > 0 {
			result = append(result, id)
		}
	}
	sort.Ints(result)
	return result
}

func sameIntSet(left []int, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func formatRouteSelectionDiagnostics(channel *Channel, diag *dto.ChannelRouteSelectionDiagnostics) string {
	if diag == nil {
		return fmt.Sprintf("channel=%d", channelIDOrZero(channel))
	}
	return fmt.Sprintf("channel=%d candidates=%d ids=%v scope=%s priority=%d weight_bps=%d weight_source=%s path=%s excluded=%v", channelIDOrZero(channel), diag.CandidateCount, diag.CandidateChannelIDs, diag.RouteScope, diag.RoutePriority, diag.RouteWeightBPS, diag.WeightSource, diag.SelectionPath, diag.ExcludedChannelIDs)
}

func getRandomSatisfiedChannelLegacy(
	group string,
	model string,
	retry int,
	excluded map[int]struct{},
	options SatisfiedChannelSelectionOptions,
) (*Channel, error) {
	if options.Diagnostics != nil {
		*options.Diagnostics = dto.ChannelRouteSelectionDiagnostics{
			Group:          strings.TrimSpace(group),
			RequestedModel: strings.TrimSpace(model),
		}
	}
	// if memory cache is disabled, get channel directly from database
	if !common.MemoryCacheEnabled {
		if options.RequestBodySize > 0 {
			bodyLimitExcluded, err := FindRequestBodyLimitExcludedChannels(group, model, options.RequestBodySize, options.BodyLimitTTLHours, options.Now)
			if err != nil {
				return nil, err
			}
			excluded = mergeExcludedChannels(excluded, bodyLimitExcluded)
		}
		// A candidate filter can remove every channel in the highest priority
		// tier (for example a route-specific health cooldown). Query all
		// priorities before applying the filter so the selector can fall through
		// to the next eligible tier instead of returning a false empty result.
		if options.ExcludeBeforePriority || options.CandidateFilter != nil || options.RouteCandidateFilter != nil {
			return getChannelExcludingAfterExclusionWithRouteFilterAndResolver(group, model, retry, excluded, options.CandidateFilter, options.RouteCandidateFilter, options.RouteModelResolver, options.Diagnostics)
		}
		return getChannelExcludingWithRouteFilterAndResolver(group, model, retry, excluded, options.CandidateFilter, options.RouteCandidateFilter, options.RouteModelResolver, options.Diagnostics)
	}

	channelSyncLock.RLock()
	if options.RequestBodySize > 0 {
		excluded = mergeExcludedChannels(excluded, findRequestBodyLimitExcludedChannelsFromCacheLocked(group, model, options.RequestBodySize, options.BodyLimitTTLHours, options.Now))
	}
	var channel *Channel
	var cacheErr error
	var cacheHit bool
	var cacheComplete bool
	if options.ExcludeBeforePriority {
		channel, cacheErr, cacheHit, cacheComplete = getRandomSatisfiedChannelFromCacheWithPriorityModeAndRouteFilterAndResolverWithCompleteness(group, model, retry, excluded, true, options.CandidateFilter, options.RouteCandidateFilter, options.RouteModelResolver, options.Diagnostics)
	} else {
		channel, cacheErr, cacheHit, cacheComplete = getRandomSatisfiedChannelFromCacheWithPriorityModeAndRouteFilterAndResolverWithCompleteness(group, model, retry, excluded, false, options.CandidateFilter, options.RouteCandidateFilter, options.RouteModelResolver, options.Diagnostics)
	}
	channelSyncLock.RUnlock()
	// A request-scoped candidate filter is evaluated against the cached
	// channels, but the cache does not contain enough information to prove that
	// the filtered result is complete. If the filter rejects every cached
	// candidate, fall through to the database selector so a freshly changed
	// route/health state cannot turn into a false "no channel" result.
	// A usable cached selection remains authoritative even when the request
	// carries an explicit exclusion set. An exclusion only requires a database
	// fallback when the cache cannot produce a candidate at all (for example a
	// newly added channel is not in the cache yet). Mapping errors still mark
	// the cache incomplete above and always force the database path.
	cacheSelectionComplete := cacheHit && cacheComplete && cacheErr == nil &&
		(len(excluded) == 0 || channel != nil)
	// A non-nil result from a partial cache is not authoritative. This occurs
	// when one raw/compact route candidate was invalidated (for example by a
	// model-mapping error) while another cached candidate remained usable. The
	// database selector must see the complete candidate set before choosing;
	// otherwise a stale partial hit can hide a valid fallback route and surface
	// as a misleading no-channel/504 on the next retry.
	if cacheSelectionComplete {
		return channel, cacheErr
	}

	if options.RequestBodySize > 0 {
		bodyLimitExcluded, fallbackLimitErr := findRequestBodyLimitExcludedChannelsFromDatabase(group, model, options.RequestBodySize, options.BodyLimitTTLHours, options.Now)
		if fallbackLimitErr != nil {
			if cacheErr != nil {
				return nil, cacheErr
			}
			return nil, fallbackLimitErr
		}
		excluded = mergeExcludedChannels(excluded, bodyLimitExcluded)
	}
	var fallbackChannel *Channel
	var fallbackErr error
	if options.ExcludeBeforePriority || options.CandidateFilter != nil || options.RouteCandidateFilter != nil {
		fallbackChannel, fallbackErr = getChannelExcludingAfterExclusionWithRouteFilterAndResolver(group, model, retry, excluded, options.CandidateFilter, options.RouteCandidateFilter, options.RouteModelResolver, options.Diagnostics)
	} else {
		fallbackChannel, fallbackErr = getChannelExcludingWithRouteFilterAndResolver(group, model, retry, excluded, options.CandidateFilter, options.RouteCandidateFilter, options.RouteModelResolver, options.Diagnostics)
	}
	if fallbackErr != nil {
		if cacheErr != nil {
			return nil, cacheErr
		}
		return nil, fallbackErr
	}
	if fallbackChannel != nil && fallbackChannel.Status == common.ChannelStatusEnabled {
		requestChannelCacheRefreshAsync()
		return fallbackChannel, nil
	}
	if cacheErr != nil {
		requestChannelCacheRefreshAsync()
		return nil, cacheErr
	}
	return nil, nil
}

// CacheGetChannel returns a channel from the in-memory cache when available.
func CacheGetChannel(id int) (*Channel, error) {
	if !common.MemoryCacheEnabled {
		return GetChannelById(id, true)
	}
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	c, ok := channelsIDM[id]
	if !ok || c == nil {
		return nil, fmt.Errorf("渠道# %d，已不存在", id)
	}
	return c.CloneForCache(), nil
}

// CacheGetChannelInfo returns cached channel info when available.
func CacheGetChannelInfo(id int) (*ChannelInfo, error) {
	if !common.MemoryCacheEnabled {
		channel, err := GetChannelById(id, true)
		if err != nil {
			return nil, err
		}
		return &channel.ChannelInfo, nil
	}
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	c, ok := channelsIDM[id]
	if !ok || c == nil {
		return nil, fmt.Errorf("渠道# %d，已不存在", id)
	}
	cloned := c.CloneForCache()
	return &cloned.ChannelInfo, nil
}

// CacheUpdateChannelStatus updates cached channel status with a new snapshot.
func CacheUpdateChannelStatus(id int, status int) {
	if !common.MemoryCacheEnabled {
		return
	}
	var updatedChannel *Channel
	channelSyncLock.Lock()
	markChannelCacheMutationLocked(id)
	if channel, ok := channelsIDM[id]; ok && channel != nil {
		updated := channel.CloneForCache()
		updated.Status = status
		channelsIDM[id] = updated
		updatedChannel = updated
		channelCacheGeneration.Add(1)
	}
	if status != common.ChannelStatusEnabled {
		removeChannelIDFromRouteCacheLocked(id)
	}
	channelSyncLock.Unlock()
	refreshRouteCandidateCacheV2(id, updatedChannel)
}

// CacheUpdateChannel updates a cached channel entry in place.
func CacheUpdateChannel(channel *Channel) {
	if !common.MemoryCacheEnabled {
		return
	}
	if channel == nil {
		return
	}
	updated := channel.CloneForCache()
	channelSyncLock.Lock()
	if updated.ChannelInfo.IsMultiKey && updated.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling {
		if oldChannel, ok := channelsIDM[channel.Id]; ok && oldChannel != nil &&
			oldChannel.ChannelInfo.IsMultiKey &&
			oldChannel.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling {
			updated.ChannelInfo.MultiKeyPollingIndex = oldChannel.ChannelInfo.MultiKeyPollingIndex
		}
	}
	markChannelCacheMutationLocked(channel.Id)
	channelsIDM[channel.Id] = updated
	channelCacheGeneration.Add(1)
	channelSyncLock.Unlock()
	refreshRouteCandidateCacheV2(channel.Id, updated)
}

func CacheUpdateChannelPollingIndex(id int, pollingIndex int) {
	if !common.MemoryCacheEnabled || id <= 0 {
		return
	}
	channelSyncLock.Lock()
	defer channelSyncLock.Unlock()
	if channel, ok := channelsIDM[id]; ok && channel != nil {
		updated := channel.CloneForCache()
		updated.ChannelInfo.MultiKeyPollingIndex = pollingIndex
		markChannelCacheMutationLocked(id)
		channelsIDM[id] = updated
		channelCacheGeneration.Add(1)
	}
}

func CacheReloadChannel(id int) {
	if !common.MemoryCacheEnabled || id <= 0 {
		return
	}
	db := DB
	if db == nil {
		common.SysLog(fmt.Sprintf("failed to reload channel cache: channel_id=%d, error=%s", id, errRouteDatabaseUnavailable.Error()))
		return
	}
	// Keep the cache snapshot observed before the database read. A concurrent
	// reload, full rebuild, or in-memory update invalidates this token while
	// holding channelSyncLock; in that case the database result below may be
	// older and must not overwrite the newer snapshot.
	channelSyncLock.RLock()
	reloadToken := captureChannelCacheReloadTokenLocked(id)
	channelSyncLock.RUnlock()
	channel := &Channel{}
	if err := db.First(channel, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			channelSyncLock.Lock()
			if !channelCacheReloadTokenMatchesLocked(id, reloadToken) {
				channelSyncLock.Unlock()
				return
			}
			markChannelCacheMutationLocked(id)
			delete(channelsIDM, id)
			removeChannelIDFromRouteCacheLocked(id)
			channelCacheGeneration.Add(1)
			channelSyncLock.Unlock()
			refreshRouteCandidateCacheV2(id, nil)
			return
		}
		common.SysLog(fmt.Sprintf("failed to reload channel cache: channel_id=%d, error=%v", id, err))
		return
	}
	channelSyncLock.Lock()
	if !channelCacheReloadTokenMatchesLocked(id, reloadToken) {
		channelSyncLock.Unlock()
		return
	}
	if oldChannel, ok := channelsIDM[id]; ok && oldChannel != nil &&
		oldChannel.ChannelInfo.IsMultiKey &&
		oldChannel.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling &&
		channel.ChannelInfo.IsMultiKey &&
		channel.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling {
		channel.ChannelInfo.MultiKeyPollingIndex = oldChannel.ChannelInfo.MultiKeyPollingIndex
	}
	markChannelCacheMutationLocked(id)
	updated := channel.CloneForCache()
	channelsIDM[channel.Id] = updated
	channelCacheGeneration.Add(1)
	channelSyncLock.Unlock()
	refreshRouteCandidateCacheV2(channel.Id, updated)
}

func CacheUpdateChannelOtherSettings(id int, settings string) {
	if !common.MemoryCacheEnabled || id <= 0 {
		return
	}
	var updatedChannel *Channel
	channelSyncLock.Lock()
	if channel, ok := channelsIDM[id]; ok && channel != nil {
		updated := channel.CloneForCache()
		updated.OtherSettings = settings
		markChannelCacheMutationLocked(id)
		channelsIDM[id] = updated
		updatedChannel = updated
		channelCacheGeneration.Add(1)
	}
	channelSyncLock.Unlock()
	refreshRouteCandidateCacheV2(id, updatedChannel)
}

// refreshRouteCandidateCacheV2 keeps the metadata cache's channel snapshots
// and model-mapping identity in sync with local channel mutations. The DB path
// is authoritative when available; the in-memory replacement is retained as a
// best-effort fallback for isolated cache tests and startup transitions.
func refreshRouteCandidateCacheV2(channelID int, channel *Channel) {
	if !common.MemoryCacheEnabled || channelID <= 0 {
		return
	}
	refreshGeneration := channelCacheGeneration.Load()
	var abilities []Ability
	db := DB
	haveAbilities := db != nil && db.Where("channel_id = ?", channelID).Find(&abilities).Error == nil
	channelSyncLock.Lock()
	defer channelSyncLock.Unlock()
	if channelCacheGeneration.Load() != refreshGeneration {
		// A newer cache mutation completed while the abilities query was in
		// flight. Discard this stale result and schedule a full rebuild. The
		// asynchronous helper only changes atomics before starting the loop, so
		// calling it while this write lock is held cannot deadlock; the loop will
		// acquire the lock after this function returns.
		requestChannelCacheRefreshAsync()
		return
	}
	removeLegacyCachedChannel := func() {
		for key, entries := range legacyRouteCandidateCache {
			// Do not reuse the cached slice backing array here. Callers may still
			// hold a snapshot returned under the read lock; compacting in place
			// would mutate that snapshot after the lock is released.
			filtered := make([]legacyRouteCandidateCacheEntry, 0, len(entries))
			for _, entry := range entries {
				if entry.channel == nil || entry.channel.Id != channelID {
					filtered = append(filtered, entry)
				}
			}
			if len(filtered) == 0 {
				delete(legacyRouteCandidateCache, key)
			} else {
				legacyRouteCandidateCache[key] = filtered
			}
		}
	}
	if routeCandidateCacheV2 == nil {
		routeCandidateCacheV2 = make(map[string][]routeCandidateV2)
	}
	// Any authoritative ability read, deletion, or disabled snapshot invalidates
	// every legacy route projection for this channel. Rebuild the enabled
	// ability entries below from the same DB snapshot; otherwise a missing
	// metadata key can fall back to group2model2channels and resurrect an old
	// model/group identity.
	if haveAbilities || channel == nil || channel.Status != common.ChannelStatusEnabled {
		removeChannelIDFromRouteCacheLocked(channelID)
	}
	// The database lookup above intentionally runs outside channelSyncLock, so
	// the caller's snapshot may already be stale by the time we rebuild the
	// route cache. Always re-read the current in-memory channel while holding
	// the lock; otherwise a delayed refresh from an enabled snapshot can
	// reinsert a channel that was disabled or deleted in the meantime.
	currentChannel, currentExists := channelsIDM[channelID]
	if !currentExists || currentChannel == nil {
		channel = nil
	} else {
		channel = currentChannel.CloneForCache()
	}
	removeCachedChannel := func() {
		for key, entries := range routeCandidateCacheV2 {
			// Keep cached snapshots immutable for readers that obtained the
			// previous slice before this refresh acquired the write lock.
			filtered := make([]routeCandidateV2, 0, len(entries))
			for _, entry := range entries {
				if entry.Channel == nil || entry.Channel.Id != channelID {
					filtered = append(filtered, entry)
				}
			}
			if len(filtered) == 0 {
				delete(routeCandidateCacheV2, key)
			} else {
				routeCandidateCacheV2[key] = filtered
			}
		}
	}
	// A successful DB query (including an empty ability set) is authoritative
	// and may remove stale entries. When the DB is unavailable, retain the old
	// entries so a transient outage cannot evict an otherwise healthy channel.
	if haveAbilities || channel == nil || channel.Status != common.ChannelStatusEnabled {
		removeCachedChannel()
		removeLegacyCachedChannel()
	}
	if channel == nil || channel.Status != common.ChannelStatusEnabled {
		return
	}
	if haveAbilities {
		if legacyRouteCandidateCache == nil {
			legacyRouteCandidateCache = make(map[string][]legacyRouteCandidateCacheEntry)
		}
		if group2model2channels == nil {
			group2model2channels = make(map[string]map[string][]int)
		}
		abilityPointers := make([]*Ability, len(abilities))
		for i := range abilities {
			abilityPointers[i] = &abilities[i]
		}
		for _, ability := range abilities {
			if !ability.Enabled {
				continue
			}
			models := group2model2channels[ability.Group]
			if models == nil {
				models = make(map[string][]int)
				group2model2channels[ability.Group] = models
			}
			ids := models[ability.Model]
			if !isChannelIDInList(ids, channelID) {
				ids = append(ids, channelID)
				sort.Slice(ids, func(i, j int) bool {
					left := channelsIDM[ids[i]]
					right := channelsIDM[ids[j]]
					if left == nil || right == nil {
						return ids[i] < ids[j]
					}
					if left.GetPriority() != right.GetPriority() {
						return left.GetPriority() > right.GetPriority()
					}
					return ids[i] < ids[j]
				})
				models[ability.Model] = ids
			}
		}
		for key, entries := range buildLegacyRouteCandidateCacheFromAbilities(abilityPointers, map[int]*Channel{channelID: channel}) {
			legacyRouteCandidateCache[key] = append(legacyRouteCandidateCache[key], entries...)
		}
		if routeCandidateCacheV2KnownByChannel == nil {
			routeCandidateCacheV2KnownByChannel = make(map[string]map[int]struct{})
		}
		for i := range abilities {
			if abilities[i].Enabled {
				key := routeCandidateCacheKey(abilities[i].Group, abilities[i].Model)
				knownChannels := routeCandidateCacheV2KnownByChannel[key]
				if knownChannels == nil {
					knownChannels = make(map[int]struct{})
					routeCandidateCacheV2KnownByChannel[key] = knownChannels
				}
				knownChannels[channelID] = struct{}{}
			}
		}
		fresh := buildRouteCandidateCacheV2(abilityPointers, map[int]*Channel{channelID: channel})
		for key, entries := range fresh {
			routeCandidateCacheV2[key] = append(routeCandidateCacheV2[key], entries...)
			routeCandidateCacheV2[key] = normalizeV2RouteEntries(collapseV2RouteCandidates(routeCandidateCacheV2[key]))
		}
		return
	}
	// A transient ability-query failure must not leave the legacy selector
	// holding an old channel snapshot. Refresh only the channel pointer and
	// keep the ability-level priority/weight metadata intact; the historical
	// group2model2channels fallback already reads the current channelsIDM
	// snapshot. Work on replacement slices so readers holding an earlier
	// snapshot are not mutated after the lock is released.
	if legacyRouteCandidateCache != nil {
		for key, entries := range legacyRouteCandidateCache {
			updatedEntries := append([]legacyRouteCandidateCacheEntry(nil), entries...)
			changed := false
			for index := range updatedEntries {
				if updatedEntries[index].channel == nil || updatedEntries[index].channel.Id != channelID {
					continue
				}
				updatedEntries[index].channel = channel.CloneForCache()
				changed = true
			}
			if changed {
				legacyRouteCandidateCache[key] = updatedEntries
			}
		}
	}
	preparedMapping, mappingErr := PrepareChannelModelMappingForRouting(channel.GetModelMapping())
	if mappingErr != nil {
		common.SysError(fmt.Sprintf("failed to prepare model mapping for route cache: channel_id=%d error=%v", channelID, mappingErr))
	}
	for key, entries := range routeCandidateCacheV2 {
		// Work on a new slice so a refresh cannot mutate a snapshot that a
		// reader obtained before the write lock was taken. The channel pointer
		// itself is replaced below because cache updates must publish a cloned
		// channel snapshot as well.
		updatedEntries := append([]routeCandidateV2(nil), entries...)
		for index := range updatedEntries {
			if updatedEntries[index].Channel == nil || updatedEntries[index].Channel.Id != channelID {
				continue
			}
			updatedEntries[index].Channel = channel.CloneForCache()
			_, compactRequest := ratio_setting.CompactBaseModelName(updatedEntries[index].RequestedModel)
			if mappingErr != nil {
				updatedEntries[index].EffectiveRouteModel = ""
				updatedEntries[index].MappingApplied = false
				updatedEntries[index].RouteWeightBPS = 0
				continue
			}
			effective, mapped, err := preparedMapping.Resolve(updatedEntries[index].RequestedModel, compactRequest)
			if err != nil {
				common.SysError(fmt.Sprintf("failed to resolve model mapping for route cache: channel_id=%d requested_model=%s error=%v", channelID, updatedEntries[index].RequestedModel, err))
				updatedEntries[index].EffectiveRouteModel = ""
				updatedEntries[index].MappingApplied = false
				updatedEntries[index].RouteWeightBPS = 0
				continue
			}
			updatedEntries[index].EffectiveRouteModel = effective
			updatedEntries[index].MappingApplied = mapped
		}
		routeCandidateCacheV2[key] = normalizeV2RouteEntries(collapseV2RouteCandidates(updatedEntries))
	}
}

func removeChannelIDFromRouteCacheLocked(id int) {
	removeChannelIDFromRouteCandidateKnownIndexLocked(id)
	for key, entries := range legacyRouteCandidateCache {
		filtered := make([]legacyRouteCandidateCacheEntry, 0, len(entries))
		for _, entry := range entries {
			if entry.channel == nil || entry.channel.Id != id {
				filtered = append(filtered, entry)
			}
		}
		if len(filtered) == 0 {
			delete(legacyRouteCandidateCache, key)
		} else {
			legacyRouteCandidateCache[key] = filtered
		}
	}
	for group, model2channels := range group2model2channels {
		for model, channelIDs := range model2channels {
			filtered := make([]int, 0, len(channelIDs))
			removed := false
			for _, channelID := range channelIDs {
				if channelID == id {
					removed = true
					continue
				}
				filtered = append(filtered, channelID)
			}
			if !removed {
				continue
			}
			if len(filtered) == 0 {
				delete(model2channels, model)
			} else {
				model2channels[model] = filtered
			}
		}
		if len(model2channels) == 0 {
			delete(group2model2channels, group)
		}
	}
}

func removeChannelIDFromRouteCandidateKnownIndexLocked(id int) {
	if routeCandidateCacheV2KnownByChannel == nil || id <= 0 {
		return
	}
	for key, channelIDs := range routeCandidateCacheV2KnownByChannel {
		delete(channelIDs, id)
		if len(channelIDs) == 0 {
			delete(routeCandidateCacheV2KnownByChannel, key)
		}
	}
}
