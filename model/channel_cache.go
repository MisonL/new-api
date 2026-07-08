package model

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
)

var group2model2channels map[string]map[string][]int // enabled channel
var channelsIDM map[int]*Channel                     // all channels include disabled
var channelSyncLock sync.RWMutex
var channelCacheRefreshInFlight atomic.Bool
var channelCacheRefreshPending atomic.Bool

func channelCacheSyncEnabled() bool {
	return common.GetEnvOrDefaultBool("CHANNEL_CACHE_SYNC_ENABLED", true)
}

// InitChannelCache rebuilds the in-memory channel cache from database state.
func InitChannelCache() {
	if !common.MemoryCacheEnabled {
		return
	}
	channelCacheRefreshPending.Store(true)
	if channelCacheRefreshInFlight.CompareAndSwap(false, true) {
		runChannelCacheRefreshLoop()
		return
	}
	for channelCacheRefreshInFlight.Load() {
		time.Sleep(10 * time.Millisecond)
	}
}

func buildChannelCacheSnapshot() error {
	newChannelId2channel := make(map[int]*Channel)
	var channels []*Channel
	if err := DB.Find(&channels).Error; err != nil {
		return fmt.Errorf("failed to sync channels from database: %w", err)
	}
	for _, channel := range channels {
		newChannelId2channel[channel.Id] = channel
	}
	var abilities []*Ability
	if err := DB.Find(&abilities).Error; err != nil {
		return fmt.Errorf("failed to sync abilities from database: %w", err)
	}
	newGroup2model2channels := make(map[string]map[string][]int)
	for _, ability := range abilities {
		if !ability.Enabled {
			continue
		}
		channel, ok := newChannelId2channel[ability.ChannelId]
		if !ok || channel.Status != common.ChannelStatusEnabled {
			continue
		}
		if _, ok := newGroup2model2channels[ability.Group]; !ok {
			newGroup2model2channels[ability.Group] = make(map[string][]int)
		}
		newGroup2model2channels[ability.Group][ability.Model] = append(
			newGroup2model2channels[ability.Group][ability.Model],
			ability.ChannelId,
		)
	}

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
	group2model2channels = newGroup2model2channels
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
	channelSyncLock.Unlock()
	common.SysLog("channels synced from database")
	return nil
}

func runChannelCacheRefreshLoop() {
	defer channelCacheRefreshInFlight.Store(false)
	for {
		channelCacheRefreshPending.Store(false)
		if err := buildChannelCacheSnapshot(); err != nil {
			common.SysError(err.Error())
		}
		if !channelCacheRefreshPending.Load() {
			return
		}
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
		runChannelCacheRefreshLoop()
	}()
}

func getRandomSatisfiedChannelFromCache(group string, model string, retry int, excluded map[int]struct{}) (*Channel, error, bool) {
	return getRandomSatisfiedChannelFromCacheWithPriorityMode(group, model, retry, excluded, false)
}

func getRandomSatisfiedChannelFromCacheAfterExclusion(group string, model string, retry int, excluded map[int]struct{}) (*Channel, error, bool) {
	return getRandomSatisfiedChannelFromCacheWithPriorityMode(group, model, retry, excluded, true)
}

func getRandomSatisfiedChannelFromCacheWithPriorityMode(group string, model string, retry int, excluded map[int]struct{}, excludeBeforePriority bool) (*Channel, error, bool) {
	cacheHit := false
	var lastErr error
	routeCandidates := getGroupModelRouteCandidateMeta(model)
	if shouldPoolCompactRouteCandidates(routeCandidates) {
		return getRandomSatisfiedPooledRouteModelsFromCache(group, routeCandidates, retry, excluded, excludeBeforePriority)
	}
	for _, routeCandidate := range routeCandidates {
		channel, err, hit := getRandomSatisfiedRouteModelFromCache(group, routeCandidate, retry, excluded, excludeBeforePriority)
		if !hit {
			continue
		}
		cacheHit = true
		if channel != nil || err != nil {
			return channel, err, true
		}
		lastErr = err
	}
	return nil, lastErr, cacheHit
}

func getRandomSatisfiedPooledRouteModelsFromCache(group string, routeCandidates []routeModelCandidate, retry int, excluded map[int]struct{}, excludeBeforePriority bool) (*Channel, error, bool) {
	cacheHit := false
	seen := make(map[int]struct{})
	targetChannels := make([]*Channel, 0, len(routeCandidates))
	for _, routeCandidate := range routeCandidates {
		channels := group2model2channels[group][routeCandidate.model]
		if len(channels) == 0 {
			continue
		}
		cacheHit = true
		for _, channelId := range channels {
			if _, ok := seen[channelId]; ok {
				continue
			}
			channel, ok := channelsIDM[channelId]
			if !ok {
				return nil, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", channelId), true
			}
			if !channelSupportsCompactRouteCandidate(channel, routeCandidate) {
				continue
			}
			seen[channelId] = struct{}{}
			targetChannels = append(targetChannels, channel)
		}
	}
	if len(targetChannels) == 0 {
		return nil, nil, cacheHit
	}
	channel, err := chooseCachedRouteChannelWithPriorityMode(targetChannels, retry, excluded, excludeBeforePriority, common.GetRandomInt)
	return channel, err, true
}

func getRandomSatisfiedRouteModelFromCache(group string, routeCandidate routeModelCandidate, retry int, excluded map[int]struct{}, excludeBeforePriority bool) (*Channel, error, bool) {
	channels := group2model2channels[group][routeCandidate.model]
	if len(channels) == 0 {
		return nil, nil, false
	}

	if len(channels) == 1 {
		if _, skip := excluded[channels[0]]; skip {
			return nil, nil, true
		}
		if channel, ok := channelsIDM[channels[0]]; ok {
			if !channelSupportsCompactRouteCandidate(channel, routeCandidate) {
				return nil, nil, true
			}
			return channel, nil, true
		}
		return nil, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", channels[0]), true
	}

	var targetChannels []*Channel
	for _, channelId := range channels {
		if channel, ok := channelsIDM[channelId]; ok {
			if !channelSupportsCompactRouteCandidate(channel, routeCandidate) {
				continue
			}
			targetChannels = append(targetChannels, channel)
		} else {
			return nil, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", channelId), true
		}
	}

	if len(targetChannels) == 0 {
		return nil, nil, true
	}

	channel, err := chooseCachedRouteChannelWithPriorityMode(targetChannels, retry, excluded, excludeBeforePriority, common.GetRandomInt)
	return channel, err, true
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

func GetRandomSatisfiedChannelExcludingWithRequestBodyLimit(
	group string,
	model string,
	retry int,
	excluded map[int]struct{},
	requestBodySize int64,
	ttlHours int,
	now time.Time,
) (*Channel, error) {
	// if memory cache is disabled, get channel directly from database
	if !common.MemoryCacheEnabled {
		if requestBodySize > 0 {
			bodyLimitExcluded, err := FindRequestBodyLimitExcludedChannels(group, model, requestBodySize, ttlHours, now)
			if err != nil {
				return nil, err
			}
			excluded = mergeExcludedChannels(excluded, bodyLimitExcluded)
			return getChannelExcludingAfterExclusion(group, model, retry, excluded)
		}
		return getChannelExcluding(group, model, retry, excluded)
	}

	channelSyncLock.RLock()
	if requestBodySize > 0 {
		excluded = mergeExcludedChannels(excluded, findRequestBodyLimitExcludedChannelsFromCacheLocked(group, model, requestBodySize, ttlHours, now))
	}
	var channel *Channel
	var cacheErr error
	var cacheHit bool
	if requestBodySize > 0 {
		channel, cacheErr, cacheHit = getRandomSatisfiedChannelFromCacheAfterExclusion(group, model, retry, excluded)
	} else {
		channel, cacheErr, cacheHit = getRandomSatisfiedChannelFromCache(group, model, retry, excluded)
	}
	channelSyncLock.RUnlock()
	if channel != nil || (cacheHit && cacheErr == nil && len(excluded) == 0) {
		return channel, cacheErr
	}

	if requestBodySize > 0 {
		bodyLimitExcluded, fallbackLimitErr := findRequestBodyLimitExcludedChannelsFromDatabase(group, model, requestBodySize, ttlHours, now)
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
	if requestBodySize > 0 {
		fallbackChannel, fallbackErr = getChannelExcludingAfterExclusion(group, model, retry, excluded)
	} else {
		fallbackChannel, fallbackErr = getChannelExcluding(group, model, retry, excluded)
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
	channelSyncLock.Lock()
	defer channelSyncLock.Unlock()
	if channel, ok := channelsIDM[id]; ok && channel != nil {
		updated := channel.CloneForCache()
		updated.Status = status
		channelsIDM[id] = updated
	}
	if status != common.ChannelStatusEnabled {
		removeChannelIDFromRouteCacheLocked(id)
	}
}

// CacheUpdateChannel updates a cached channel entry in place.
func CacheUpdateChannel(channel *Channel) {
	if !common.MemoryCacheEnabled {
		return
	}
	channelSyncLock.Lock()
	defer channelSyncLock.Unlock()
	if channel == nil {
		return
	}
	updated := channel.CloneForCache()
	if updated.ChannelInfo.IsMultiKey && updated.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling {
		if oldChannel, ok := channelsIDM[channel.Id]; ok && oldChannel != nil &&
			oldChannel.ChannelInfo.IsMultiKey &&
			oldChannel.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling {
			updated.ChannelInfo.MultiKeyPollingIndex = oldChannel.ChannelInfo.MultiKeyPollingIndex
		}
	}
	channelsIDM[channel.Id] = updated
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
		channelsIDM[id] = updated
	}
}

func CacheReloadChannel(id int) {
	if !common.MemoryCacheEnabled || id <= 0 {
		return
	}
	channel := &Channel{}
	if err := DB.First(channel, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			channelSyncLock.Lock()
			defer channelSyncLock.Unlock()
			delete(channelsIDM, id)
			removeChannelIDFromRouteCacheLocked(id)
			return
		}
		common.SysLog(fmt.Sprintf("failed to reload channel cache: channel_id=%d, error=%v", id, err))
		return
	}
	channelSyncLock.Lock()
	defer channelSyncLock.Unlock()
	if oldChannel, ok := channelsIDM[id]; ok && oldChannel != nil &&
		oldChannel.ChannelInfo.IsMultiKey &&
		oldChannel.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling &&
		channel.ChannelInfo.IsMultiKey &&
		channel.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling {
		channel.ChannelInfo.MultiKeyPollingIndex = oldChannel.ChannelInfo.MultiKeyPollingIndex
	}
	channelsIDM[channel.Id] = channel.CloneForCache()
}

func CacheUpdateChannelOtherSettings(id int, settings string) {
	if !common.MemoryCacheEnabled || id <= 0 {
		return
	}
	channelSyncLock.Lock()
	defer channelSyncLock.Unlock()
	if channel, ok := channelsIDM[id]; ok && channel != nil {
		updated := channel.CloneForCache()
		updated.OtherSettings = settings
		channelsIDM[id] = updated
	}
}

func removeChannelIDFromRouteCacheLocked(id int) {
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
