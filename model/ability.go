package model

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/samber/lo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errNoMatchingAbilities = errors.New("no matching abilities found")

type Ability struct {
	Group     string `json:"group" gorm:"type:varchar(64);primaryKey;autoIncrement:false"`
	Model     string `json:"model" gorm:"type:varchar(255);primaryKey;autoIncrement:false"`
	ChannelId int    `json:"channel_id" gorm:"primaryKey;autoIncrement:false;index"`
	Enabled   bool   `json:"enabled"`
	// Priority and Weight are retained for compatibility with legacy clients.
	// New route selection uses RoutePriority and RouteWeightBPS when the v2
	// selector is enabled.
	Priority       *int64 `json:"priority" gorm:"bigint;default:0;index"`
	Weight         uint   `json:"weight" gorm:"default:0;index"`
	RoutePriority  *int64 `json:"route_priority,omitempty" gorm:"bigint;default:0;index"`
	RouteWeightBPS *uint  `json:"route_weight_bps,omitempty" gorm:"default:0;index"`
	// RoutePriorityConfigured preserves an explicitly configured priority 0.
	// Legacy rows may read the database default as a non-nil pointer even though
	// the V2 field was never configured.
	RoutePriorityConfigured bool `json:"-" gorm:"not null;default:false;index"`
	// RouteWeightBPSConfigured distinguishes an explicitly configured 0% route
	// from legacy rows whose nullable route_weight_bps column was materialized
	// as the database default 0 during the compatibility migration.
	RouteWeightBPSConfigured bool    `json:"-" gorm:"not null;default:false;index"`
	Tag                      *string `json:"tag" gorm:"index"`
}

func (ability *Ability) BeforeCreate(tx *gorm.DB) error {
	if ability != nil {
		if ability.RoutePriority != nil {
			ability.RoutePriorityConfigured = true
		}
		if ability.RouteWeightBPS != nil {
			ability.RouteWeightBPSConfigured = true
		}
	}
	return nil
}

type AbilityWithChannel struct {
	Ability
	ChannelType int `json:"channel_type"`
}

func (ability Ability) GetEffectiveRoutePriority() int64 {
	if ability.HasExplicitRoutePriority() {
		return *ability.RoutePriority
	}
	if ability.Priority != nil {
		return *ability.Priority
	}
	return 0
}

// GetConfiguredRoutePriority returns only the V2 route priority. It does not
// fall back to the legacy priority because an unbackfilled row must not enter
// a V2 scope with an implicit value.
func (ability Ability) GetConfiguredRoutePriority() (int64, bool) {
	if !ability.HasExplicitRoutePriority() || ability.RoutePriority == nil {
		return 0, false
	}
	return *ability.RoutePriority, true
}

func (ability Ability) HasExplicitRoutePriority() bool {
	return ability.RoutePriority != nil && (ability.RoutePriorityConfigured || *ability.RoutePriority != 0)
}

func (ability Ability) GetEffectiveRouteWeightBPS() uint {
	if ability.HasExplicitRouteWeight() {
		return *ability.RouteWeightBPS
	}
	return ability.Weight
}

// GetConfiguredRouteWeightBPS returns only the V2 route weight. A configured
// zero is valid and remains distinguishable from a legacy row with no V2
// configuration.
func (ability Ability) GetConfiguredRouteWeightBPS() (uint, bool) {
	if !ability.HasExplicitRouteWeight() || ability.RouteWeightBPS == nil {
		return 0, false
	}
	return *ability.RouteWeightBPS, true
}

func (ability Ability) HasExplicitRouteWeight() bool {
	return ability.RouteWeightBPS != nil && (ability.RouteWeightBPSConfigured || *ability.RouteWeightBPS != 0)
}

func ensureAbilityColumnsInitialized() {
	if commonGroupCol == "" {
		initCol()
	}
}

func GetAllEnableAbilityWithChannels() ([]AbilityWithChannel, error) {
	var abilities []AbilityWithChannel
	err := DB.Table("abilities").
		Select("abilities.*, channels.type as channel_type").
		Joins("left join channels on abilities.channel_id = channels.id").
		Where("abilities.enabled = ?", true).
		Scan(&abilities).Error
	return abilities, err
}

func GetGroupEnabledModels(group string) []string {
	ensureAbilityColumnsInitialized()
	var models []string
	// Find distinct models
	DB.Table("abilities").Where(commonGroupCol+" = ? and enabled = ?", group, true).Distinct("model").Pluck("model", &models)
	return models
}

func GetEnabledModels() []string {
	var models []string
	// Find distinct models
	DB.Table("abilities").Where("enabled = ?", true).Distinct("model").Pluck("model", &models)
	return models
}

func GetAllEnableAbilities() []Ability {
	var abilities []Ability
	DB.Find(&abilities, "enabled = ?", true)
	return abilities
}

func getPriority(group string, model string, retry int) (int, error) {
	ensureAbilityColumnsInitialized()

	var priorities []int
	err := getEnabledAbilityQuery(group, model).
		Select("DISTINCT(abilities.priority)").
		Order("abilities.priority DESC").
		Pluck("abilities.priority", &priorities).Error

	if err != nil {
		// 处理错误
		return 0, err
	}

	if len(priorities) == 0 {
		return 0, errNoMatchingAbilities
	}

	// 确定要使用的优先级
	var priorityToUse int
	if retry >= len(priorities) {
		// 如果重试次数大于优先级数，则使用最小的优先级
		priorityToUse = priorities[len(priorities)-1]
	} else {
		priorityToUse = priorities[retry]
	}
	return priorityToUse, nil
}

func getChannelQuery(group string, model string, retry int) (*gorm.DB, error) {
	ensureAbilityColumnsInitialized()
	maxPrioritySubQuery := getEnabledAbilityQuery(group, model).Select("MAX(abilities.priority)")
	channelQuery := getEnabledAbilityQuery(group, model).Where("abilities.priority = (?)", maxPrioritySubQuery)
	if retry != 0 {
		priority, err := getPriority(group, model, retry)
		if err != nil {
			return nil, err
		} else {
			channelQuery = getEnabledAbilityQuery(group, model).Where("abilities.priority = ?", priority)
		}
	}

	return channelQuery, nil
}

func GetChannel(group string, model string, retry int) (*Channel, error) {
	return getChannelExcluding(group, model, retry, nil)
}

func getChannelExcluding(group string, model string, retry int, excluded map[int]struct{}) (*Channel, error) {
	return getChannelExcludingWithFilter(group, model, retry, excluded, nil)
}

func getChannelExcludingAfterExclusion(group string, model string, retry int, excluded map[int]struct{}) (*Channel, error) {
	return getChannelExcludingAfterExclusionWithFilter(group, model, retry, excluded, nil)
}

func getChannelExcludingWithFilter(group string, model string, retry int, excluded map[int]struct{}, candidateFilter func(*Channel) bool) (*Channel, error) {
	return getChannelExcludingInternal(group, model, retry, excluded, false, candidateFilter, nil, nil, nil)
}

func getChannelExcludingAfterExclusionWithFilter(group string, model string, retry int, excluded map[int]struct{}, candidateFilter func(*Channel) bool) (*Channel, error) {
	return getChannelExcludingInternal(group, model, retry, excluded, true, candidateFilter, nil, nil, nil)
}

func getChannelExcludingWithFilterAndResolver(group string, model string, retry int, excluded map[int]struct{}, candidateFilter func(*Channel) bool, resolver RouteModelResolver) (*Channel, error) {
	return getChannelExcludingInternal(group, model, retry, excluded, false, candidateFilter, nil, resolver, nil)
}

func getChannelExcludingAfterExclusionWithFilterAndResolver(group string, model string, retry int, excluded map[int]struct{}, candidateFilter func(*Channel) bool, resolver RouteModelResolver) (*Channel, error) {
	return getChannelExcludingInternal(group, model, retry, excluded, true, candidateFilter, nil, resolver, nil)
}

func getChannelExcludingWithRouteFilterAndResolver(group string, model string, retry int, excluded map[int]struct{}, candidateFilter func(*Channel) bool, routeFilter RouteCandidateFilter, resolver RouteModelResolver, diagnostics *dto.ChannelRouteSelectionDiagnostics) (*Channel, error) {
	return getChannelExcludingInternal(group, model, retry, excluded, false, candidateFilter, routeFilter, resolver, diagnostics)
}

func getChannelExcludingAfterExclusionWithRouteFilterAndResolver(group string, model string, retry int, excluded map[int]struct{}, candidateFilter func(*Channel) bool, routeFilter RouteCandidateFilter, resolver RouteModelResolver, diagnostics *dto.ChannelRouteSelectionDiagnostics) (*Channel, error) {
	return getChannelExcludingInternal(group, model, retry, excluded, true, candidateFilter, routeFilter, resolver, diagnostics)
}

func getChannelExcludingInternal(group string, model string, retry int, excluded map[int]struct{}, deferPriorityFiltering bool, candidateFilter func(*Channel) bool, routeFilter RouteCandidateFilter, resolver RouteModelResolver, diagnostics *dto.ChannelRouteSelectionDiagnostics) (*Channel, error) {
	routeCandidates := getGroupModelRouteCandidateMeta(model)
	if shouldPoolCompactRouteCandidates(routeCandidates) {
		channel, found, err := getChannelByRouteModels(group, model, routeCandidates, retry, excluded, deferPriorityFiltering, candidateFilter, routeFilter, resolver, diagnostics)
		if err != nil {
			return nil, err
		}
		if found {
			return channel, nil
		}
		return nil, nil
	}
	for _, routeCandidate := range routeCandidates {
		channel, found, err := getChannelByRouteModel(group, model, routeCandidate, retry, excluded, deferPriorityFiltering, candidateFilter, routeFilter, resolver, diagnostics)
		if err != nil {
			return nil, err
		}
		if found {
			// A raw candidate can be present in the database while every channel
			// in that candidate is excluded for this request. Continue through
			// normalized/wildcard candidates instead of treating that empty result
			// as an authoritative stop. If there is no later candidate the loop
			// naturally returns nil below.
			if channel != nil {
				return channel, nil
			}
			continue
		}
	}
	return nil, nil
}

func getRouteCandidateAbilityQuery(group string, routeCandidate routeModelCandidate, retry int, deferPriorityFiltering bool) (*gorm.DB, error) {
	ensureAbilityColumnsInitialized()
	if routeCandidate.compactRequest || deferPriorityFiltering {
		return getEnabledAbilityQuery(group, routeCandidate.model), nil
	}
	maxPrioritySubQuery := getEnabledAbilityQuery(group, routeCandidate.model).
		Select("MAX(abilities.priority)")
	query := getEnabledAbilityQuery(group, routeCandidate.model).
		Where("abilities.priority = (?)", maxPrioritySubQuery)
	if retry != 0 {
		priority, err := getPriority(group, routeCandidate.model, retry)
		if err != nil {
			return nil, err
		}
		query = getEnabledAbilityQuery(group, routeCandidate.model).
			Where("abilities.priority = ?", priority)
	}
	return query, nil
}

// getEnabledAbilityQuery is the legacy selector's database boundary. Ability
// rows can remain enabled while their owning channel is manually or
// automatically disabled; such rows must not establish a priority tier or
// enter request-aware mapping/health filters.
func getEnabledAbilityQuery(group string, model string) *gorm.DB {
	ensureAbilityColumnsInitialized()
	return DB.Model(&Ability{}).
		Select("abilities.*").
		Joins("JOIN channels ON channels.id = abilities.channel_id").
		Where("abilities."+commonGroupCol+" = ? AND abilities.model = ? AND abilities.enabled = ? AND channels.status = ?", group, model, true, common.ChannelStatusEnabled)
}

func getChannelByRouteModel(group string, requestedModel string, routeCandidate routeModelCandidate, retry int, excluded map[int]struct{}, deferPriorityFiltering bool, candidateFilter func(*Channel) bool, routeFilter RouteCandidateFilter, resolver RouteModelResolver, diagnostics *dto.ChannelRouteSelectionDiagnostics) (*Channel, bool, error) {
	// Request-aware mapping/health filters must see every priority tier before
	// selection. If the highest legacy priority has an invalid model_mapping,
	// filtering it after a max-priority SQL query would incorrectly hide a lower
	// valid fallback and surface a misleading no-channel/504 result.
	deferPriorityFiltering = deferPriorityFiltering || candidateFilter != nil || routeFilter != nil || resolver != nil
	channelQuery, err := getRouteCandidateAbilityQuery(group, routeCandidate, retry, deferPriorityFiltering)
	if err != nil {
		if errors.Is(err, errNoMatchingAbilities) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var abilities []Ability
	err = channelQuery.Order("abilities.priority DESC, abilities.weight DESC").Find(&abilities).Error
	if err != nil {
		return nil, false, err
	}
	if len(abilities) == 0 {
		return nil, false, nil
	}

	candidates, err := loadRouteCandidateSelections(abilities, routeCandidate, false, candidateFilter, routeFilter, resolver)
	if err != nil {
		return nil, false, err
	}
	if len(candidates) == 0 {
		return nil, false, nil
	}
	candidates = filterRouteSelectionCandidatesByCompactPreference(candidates, routeCandidate.compactRequest, excluded)
	candidates = filterRouteSelectionCandidatesByRetryPriority(candidates, retry, excluded, deferPriorityFiltering)
	if len(candidates) == 0 {
		return nil, true, nil
	}
	selected := chooseWeightedRouteSelectionCandidate(candidates, common.GetRandomInt)
	if selected == nil {
		return nil, true, nil
	}
	bindSelectedRouteCandidateDiagnostics(diagnostics, group, requestedModel, *selected, "database")
	return selected.channel, true, nil
}

func getChannelByRouteModels(group string, requestedModel string, routeCandidates []routeModelCandidate, retry int, excluded map[int]struct{}, deferPriorityFiltering bool, candidateFilter func(*Channel) bool, routeFilter RouteCandidateFilter, resolver RouteModelResolver, diagnostics *dto.ChannelRouteSelectionDiagnostics) (*Channel, bool, error) {
	candidates := make([]routeSelectionCandidate, 0, len(routeCandidates))
	seen := make(map[string]struct{})
	for _, routeCandidate := range routeCandidates {
		channelQuery, err := getRouteCandidateAbilityQuery(group, routeCandidate, retry, deferPriorityFiltering)
		if err != nil {
			if errors.Is(err, errNoMatchingAbilities) {
				continue
			}
			return nil, false, err
		}
		var abilities []Ability
		if err := channelQuery.Order("abilities.priority DESC, abilities.weight DESC").Find(&abilities).Error; err != nil {
			return nil, false, err
		}
		candidateSelections, err := loadRouteCandidateSelections(abilities, routeCandidate, true, candidateFilter, routeFilter, resolver)
		if err != nil {
			return nil, false, err
		}
		for _, candidateSelection := range candidateSelections {
			// Keep route priority in the identity. A compact pool may expose the
			// same physical channel/effective model through more than one raw
			// ability, and those abilities can intentionally belong to different
			// retry tiers. Channel.Priority is only a legacy channel-level default
			// and must not collapse distinct ability priorities.
			key := fmt.Sprintf("%d\x00%s\x00%d", candidateSelection.channel.Id, strings.TrimSpace(candidateSelection.routeCandidate.effectiveModel), candidateSelection.priority)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			candidates = append(candidates, candidateSelection)
		}
	}
	if len(candidates) == 0 {
		return nil, false, nil
	}
	candidates = filterRouteSelectionCandidatesByCompactPreference(candidates, true, excluded)
	candidates = filterRouteSelectionCandidatesByRetryPriority(candidates, retry, excluded, deferPriorityFiltering)
	if len(candidates) == 0 {
		return nil, true, nil
	}
	selected := chooseWeightedRouteSelectionCandidate(candidates, common.GetRandomInt)
	if selected == nil {
		return nil, true, nil
	}
	bindSelectedRouteCandidateDiagnostics(diagnostics, group, requestedModel, *selected, "database")
	return selected.channel, true, nil
}

func loadRouteCandidateChannels(abilities []Ability, routeCandidate routeModelCandidate, useChannelWeights bool) ([]*Channel, []uint, error) {
	return loadRouteCandidateChannelsWithFilterAndResolver(abilities, routeCandidate, useChannelWeights, nil, nil)
}

func loadRouteCandidateChannelsWithFilter(abilities []Ability, routeCandidate routeModelCandidate, useChannelWeights bool, candidateFilter func(*Channel) bool) ([]*Channel, []uint, error) {
	return loadRouteCandidateChannelsWithFilterAndResolver(abilities, routeCandidate, useChannelWeights, candidateFilter, nil)
}

func loadRouteCandidateChannelsWithFilterAndResolver(abilities []Ability, routeCandidate routeModelCandidate, useChannelWeights bool, candidateFilter func(*Channel) bool, resolver RouteModelResolver) ([]*Channel, []uint, error) {
	candidates, err := loadRouteCandidateSelections(abilities, routeCandidate, useChannelWeights, candidateFilter, nil, resolver)
	if err != nil {
		return nil, nil, err
	}
	channels := make([]*Channel, 0, len(candidates))
	weights := make([]uint, 0, len(candidates))
	for _, candidate := range candidates {
		channels = append(channels, candidate.channel)
		weights = append(weights, candidate.weight)
	}
	return channels, weights, nil
}

func loadRouteCandidateSelections(abilities []Ability, routeCandidate routeModelCandidate, useChannelWeights bool, candidateFilter func(*Channel) bool, routeFilter RouteCandidateFilter, resolver RouteModelResolver) ([]routeSelectionCandidate, error) {
	if len(abilities) == 0 {
		return nil, nil
	}
	channelIds := make([]int, 0, len(abilities))
	for _, ability := range abilities {
		channelIds = append(channelIds, ability.ChannelId)
	}
	var dbChannels []Channel
	if err := DB.Where("id IN ?", channelIds).Find(&dbChannels).Error; err != nil {
		return nil, err
	}
	channelByID := make(map[int]*Channel, len(dbChannels))
	for i := range dbChannels {
		channelByID[dbChannels[i].Id] = &dbChannels[i]
	}

	candidates := make([]routeSelectionCandidate, 0, len(abilities))
	for _, ability := range abilities {
		channel, ok := channelByID[ability.ChannelId]
		if !ok {
			return nil, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", ability.ChannelId)
		}
		if channel == nil || channel.Status != common.ChannelStatusEnabled {
			continue
		}
		resolvedCandidate, mappingErr := prepareResolvedRouteCandidate(resolver, channel, routeCandidate)
		if mappingErr != nil {
			if IsChannelModelMappingError(mappingErr) {
				continue
			}
			return nil, mappingErr
		}
		if !channelSupportsCompactRouteCandidate(channel, resolvedCandidate) {
			continue
		}
		if !routeCandidateAllowed(channel, resolvedCandidate, candidateFilter, routeFilter) {
			continue
		}
		weight := ability.Weight
		weightSource := "legacy_ability_weight"
		if useChannelWeights {
			weight = uint(channel.GetWeight())
			weightSource = "legacy_channel_weight"
		}
		candidates = append(candidates, routeSelectionCandidate{
			channel:        channel,
			routeCandidate: resolvedCandidate,
			priority:       legacyAbilityPriority(ability),
			weight:         weight,
			weightSource:   weightSource,
		})
	}
	return candidates, nil
}

// filterRouteSelectionCandidatesByCompactPreference keeps the compact
// namespace preference at candidate-entry granularity. A physical channel may
// expose several raw model keys with different effective upstream models; a
// channel-only projection would accidentally discard the route identity that
// was already resolved for this candidate.
func filterRouteSelectionCandidatesByCompactPreference(candidates []routeSelectionCandidate, compactRequest bool, excluded map[int]struct{}) []routeSelectionCandidate {
	if !compactRequest || len(candidates) == 0 {
		return candidates
	}
	preferred := make([]routeSelectionCandidate, 0, len(candidates))
	fallback := make([]routeSelectionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.channel == nil {
			continue
		}
		if channelPrefersResponsesCompactionWhenNamespaceUnsupported(candidate.channel) {
			if _, skip := excluded[candidate.channel.Id]; skip {
				continue
			}
			preferred = append(preferred, candidate)
			continue
		}
		fallback = append(fallback, candidate)
	}
	if len(preferred) > 0 {
		return preferred
	}
	if len(fallback) > 0 {
		return fallback
	}
	// Every preferred candidate is currently excluded. Keep the original
	// entries so the retry/priority filter can apply its documented semantics.
	return candidates
}

// filterRouteSelectionCandidatesByRetryPriority mirrors the legacy priority
// contract while preserving the resolved route candidate attached to each
// channel. Exclusions are removed before priority indexing only when requested
// by the caller; they are always removed from the final selected tier.
func filterRouteSelectionCandidatesByRetryPriority(candidates []routeSelectionCandidate, retry int, excluded map[int]struct{}, excludeBeforePriority bool) []routeSelectionCandidate {
	if len(candidates) == 0 {
		return candidates
	}
	if excludeBeforePriority {
		filtered := make([]routeSelectionCandidate, 0, len(candidates))
		for _, candidate := range candidates {
			if candidate.channel == nil {
				continue
			}
			if _, skip := excluded[candidate.channel.Id]; skip {
				continue
			}
			filtered = append(filtered, candidate)
		}
		candidates = filtered
	}
	if len(candidates) == 0 {
		return nil
	}
	priorities := make([]int64, 0, len(candidates))
	seen := make(map[int64]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate.channel == nil {
			continue
		}
		priority := candidate.priority
		if _, ok := seen[priority]; ok {
			continue
		}
		seen[priority] = struct{}{}
		priorities = append(priorities, priority)
	}
	if len(priorities) == 0 {
		return nil
	}
	sort.Slice(priorities, func(i, j int) bool { return priorities[i] > priorities[j] })
	if retry < 0 {
		retry = 0
	}
	if retry >= len(priorities) {
		retry = len(priorities) - 1
	}
	targetPriority := priorities[retry]
	filtered := make([]routeSelectionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.channel == nil || candidate.priority != targetPriority {
			continue
		}
		if _, skip := excluded[candidate.channel.Id]; skip {
			continue
		}
		filtered = append(filtered, candidate)
	}
	return filtered
}

func legacyAbilityPriority(ability Ability) int64 {
	if ability.Priority == nil {
		return 0
	}
	return *ability.Priority
}

func chooseWeightedRouteSelectionCandidate(candidates []routeSelectionCandidate, randomInt func(int) int) *routeSelectionCandidate {
	if len(candidates) == 0 {
		return nil
	}
	if randomInt == nil {
		randomInt = common.GetRandomInt
	}
	maxInt := uint64(^uint(0) >> 1)
	var total uint64
	for _, candidate := range candidates {
		weight := uint64(candidate.weight) + 10
		if maxInt-total < weight {
			total = maxInt
			break
		}
		total += weight
	}
	if total == 0 {
		return &candidates[0]
	}
	point := uint64(randomInt(int(total)))
	for index := range candidates {
		weight := uint64(candidates[index].weight) + 10
		if point < weight {
			return &candidates[index]
		}
		point -= weight
	}
	return &candidates[len(candidates)-1]
}

func filterChannelsByRetryPriority(channels []*Channel, weights []uint, retry int, excluded map[int]struct{}, excludeBeforePriority bool) ([]*Channel, []uint) {
	if excludeBeforePriority && len(excluded) > 0 {
		filteredChannels := make([]*Channel, 0, len(channels))
		filteredWeights := make([]uint, 0, len(weights))
		for i, channel := range channels {
			if _, skip := excluded[channel.Id]; skip {
				continue
			}
			filteredChannels = append(filteredChannels, channel)
			filteredWeights = append(filteredWeights, weights[i])
		}
		channels = filteredChannels
		weights = filteredWeights
	}
	priorities := make([]int64, 0, len(channels))
	seen := make(map[int64]struct{}, len(channels))
	for _, channel := range channels {
		priority := channel.GetPriority()
		if _, ok := seen[priority]; ok {
			continue
		}
		seen[priority] = struct{}{}
		priorities = append(priorities, priority)
	}
	if len(priorities) == 0 {
		return channels, weights
	}
	sort.Slice(priorities, func(i, j int) bool {
		return priorities[i] > priorities[j]
	})
	if retry >= len(priorities) {
		retry = len(priorities) - 1
	}
	targetPriority := priorities[retry]
	filteredChannels := make([]*Channel, 0, len(channels))
	filteredWeights := make([]uint, 0, len(weights))
	for i, channel := range channels {
		if channel.GetPriority() != targetPriority {
			continue
		}
		if !excludeBeforePriority {
			if _, skip := excluded[channel.Id]; skip {
				continue
			}
		}
		filteredChannels = append(filteredChannels, channel)
		filteredWeights = append(filteredWeights, weights[i])
	}
	return filteredChannels, filteredWeights
}

func chooseWeightedChannel(channels []*Channel, weights []uint) *Channel {
	return chooseWeightedChannelWithRandom(channels, weights, common.GetRandomInt)
}

func chooseWeightedChannelWithRandom(channels []*Channel, weights []uint, randomInt func(int) int) *Channel {
	weightSum := uint(0)
	for _, weight := range weights {
		weightSum += weight + 10
	}
	return chooseWeightedChannelAt(channels, weights, randomInt(int(weightSum)))
}

func chooseWeightedChannelAt(channels []*Channel, weights []uint, weight int) *Channel {
	for i, channel := range channels {
		weight -= int(weights[i]) + 10
		if weight < 0 {
			return channel
		}
	}
	return channels[len(channels)-1]
}

type routeSeedIdentity struct {
	group               string
	effectiveRouteModel string
}

type routeSeedScopeState struct {
	exists                   bool
	explicitWeightsByChannel map[int]map[uint]struct{}
}

// seedV2RouteFieldsForNewAbilities gives newly-created ability rows a
// deterministic V2 membership without treating legacy fields as a runtime
// fallback. A new channel gets 10000 bps when it creates a scope; a channel
// added to an existing scope gets 0 bps until an administrator assigns it a
// share. When an existing channel gains another raw ability that maps to the
// same effective model, the existing channel's explicit route fields are
// inherited so all raw identities remain one route member.
func seedV2RouteFieldsForNewAbilities(tx *gorm.DB, channel *Channel, abilities []Ability) error {
	if !IsChannelRoutingV2Enabled() || channel == nil || len(abilities) == 0 {
		return nil
	}
	ensureAbilityColumnsInitialized()
	mapping, err := PrepareChannelModelMapping(channel.GetModelMapping())
	if err != nil {
		return err
	}
	useDB := DB
	if tx != nil {
		useDB = tx
	}
	if useDB == nil {
		return errors.New("database is unavailable while seeding V2 route fields")
	}

	groups := make([]string, 0, len(abilities))
	seenGroups := make(map[string]struct{}, len(abilities))
	for _, ability := range abilities {
		group := strings.TrimSpace(ability.Group)
		if _, ok := seenGroups[group]; ok {
			continue
		}
		seenGroups[group] = struct{}{}
		groups = append(groups, group)
	}
	var existingAbilities []Ability
	if len(groups) > 0 {
		// Only active abilities can contribute to an active route scope. Besides
		// avoiding a full scan of disabled/orphaned rows, this prevents a newly
		// added channel from inheriting a zero weight from a scope that no longer
		// has any routable member.
		if err := useDB.Model(&Ability{}).
			Joins("JOIN channels ON channels.id = abilities.channel_id").
			Where("abilities."+commonGroupCol+" IN ? AND abilities.enabled = ? AND channels.status = ?", groups, true, common.ChannelStatusEnabled).
			Find(&existingAbilities).Error; err != nil {
			return err
		}
	}
	channelIDs := make([]int, 0, len(existingAbilities)+1)
	seenChannelIDs := map[int]struct{}{channel.Id: {}}
	channelIDs = append(channelIDs, channel.Id)
	for _, ability := range existingAbilities {
		if _, ok := seenChannelIDs[ability.ChannelId]; ok {
			continue
		}
		seenChannelIDs[ability.ChannelId] = struct{}{}
		channelIDs = append(channelIDs, ability.ChannelId)
	}
	var existingChannels []Channel
	if len(channelIDs) > 0 {
		if err := useDB.Where("id IN ?", channelIDs).Find(&existingChannels).Error; err != nil {
			return err
		}
	}
	channelByID := make(map[int]*Channel, len(existingChannels)+1)
	channelByID[channel.Id] = channel
	for i := range existingChannels {
		channelByID[existingChannels[i].Id] = &existingChannels[i]
	}

	scopes := make(map[routeScopeKey]*routeSeedScopeState)
	currentPriorities := make(map[routeSeedIdentity]map[int64]struct{})
	for _, ability := range existingAbilities {
		existingChannel := channelByID[ability.ChannelId]
		if existingChannel == nil {
			continue
		}
		requestedModel := strings.TrimSpace(ability.Model)
		_, compact := ratio_setting.CompactBaseModelName(requestedModel)
		existingMapping, mappingErr := PrepareChannelModelMapping(existingChannel.GetModelMapping())
		if mappingErr != nil {
			continue
		}
		effectiveModel, _, mappingErr := existingMapping.Resolve(requestedModel, compact)
		if mappingErr != nil || strings.TrimSpace(effectiveModel) == "" {
			continue
		}
		key := routeScopeKey{
			Group:               strings.TrimSpace(ability.Group),
			EffectiveRouteModel: strings.TrimSpace(effectiveModel),
			RoutePriority:       ability.GetEffectiveRoutePriority(),
		}
		state := scopes[key]
		if state == nil {
			state = &routeSeedScopeState{explicitWeightsByChannel: make(map[int]map[uint]struct{})}
			scopes[key] = state
		}
		state.exists = true
		if ability.HasExplicitRouteWeight() && ability.RouteWeightBPS != nil {
			weights := state.explicitWeightsByChannel[ability.ChannelId]
			if weights == nil {
				weights = make(map[uint]struct{})
				state.explicitWeightsByChannel[ability.ChannelId] = weights
			}
			weights[*ability.RouteWeightBPS] = struct{}{}
		}
		if ability.ChannelId == channel.Id {
			identity := routeSeedIdentity{group: key.Group, effectiveRouteModel: key.EffectiveRouteModel}
			priorities := currentPriorities[identity]
			if priorities == nil {
				priorities = make(map[int64]struct{})
				currentPriorities[identity] = priorities
			}
			priorities[key.RoutePriority] = struct{}{}
		}
	}

	for index := range abilities {
		ability := &abilities[index]
		requestedModel := strings.TrimSpace(ability.Model)
		_, compact := ratio_setting.CompactBaseModelName(requestedModel)
		effectiveModel, _, mappingErr := mapping.Resolve(requestedModel, compact)
		if mappingErr != nil {
			return mappingErr
		}
		effectiveModel = strings.TrimSpace(effectiveModel)
		if effectiveModel == "" {
			return fmt.Errorf("empty effective route model for new ability: group=%s model=%s", ability.Group, ability.Model)
		}
		identity := routeSeedIdentity{group: strings.TrimSpace(ability.Group), effectiveRouteModel: effectiveModel}
		priority := channel.GetPriority()
		if priorities := currentPriorities[identity]; len(priorities) > 1 {
			return fmt.Errorf("channel %d has conflicting route priorities for effective model %s", channel.Id, effectiveModel)
		} else if len(priorities) == 1 {
			for value := range priorities {
				priority = value
			}
		}
		key := routeScopeKey{Group: identity.group, EffectiveRouteModel: identity.effectiveRouteModel, RoutePriority: priority}
		state := scopes[key]
		if state == nil {
			state = &routeSeedScopeState{explicitWeightsByChannel: make(map[int]map[uint]struct{})}
			scopes[key] = state
		}
		weight := RouteWeightTotalBPS
		if weights := state.explicitWeightsByChannel[channel.Id]; len(weights) > 1 {
			return fmt.Errorf("channel %d has conflicting route weights for effective model %s", channel.Id, effectiveModel)
		} else if len(weights) == 1 {
			for value := range weights {
				weight = value
			}
		} else if state.exists {
			weight = 0
		}
		ability.RoutePriority = common.GetPointer(priority)
		ability.RoutePriorityConfigured = true
		ability.RouteWeightBPS = common.GetPointer(weight)
		ability.RouteWeightBPSConfigured = true
		state.exists = true
		weights := state.explicitWeightsByChannel[channel.Id]
		if weights == nil {
			weights = make(map[uint]struct{})
			state.explicitWeightsByChannel[channel.Id] = weights
		}
		weights[weight] = struct{}{}
		priorities := currentPriorities[identity]
		if priorities == nil {
			priorities = make(map[int64]struct{})
			currentPriorities[identity] = priorities
		}
		priorities[priority] = struct{}{}
	}
	return nil
}

func (channel *Channel) AddAbilities(tx *gorm.DB) error {
	models_ := strings.Split(channel.Models, ",")
	groups_ := strings.Split(channel.Group, ",")
	abilitySet := make(map[string]struct{})
	abilities := make([]Ability, 0, len(models_))
	for _, model := range models_ {
		for _, group := range groups_ {
			key := group + "|" + model
			if _, exists := abilitySet[key]; exists {
				continue
			}
			abilitySet[key] = struct{}{}
			ability := Ability{
				Group:     group,
				Model:     model,
				ChannelId: channel.Id,
				Enabled:   channel.Status == common.ChannelStatusEnabled,
				Priority:  channel.Priority,
				Weight:    uint(channel.GetWeight()),
				Tag:       channel.Tag,
			}
			abilities = append(abilities, ability)
		}
	}
	if len(abilities) == 0 {
		return nil
	}
	// choose DB or provided tx
	useDB := DB
	if tx != nil {
		useDB = tx
	}
	if err := seedV2RouteFieldsForNewAbilities(useDB, channel, abilities); err != nil {
		return err
	}
	for _, chunk := range lo.Chunk(abilities, 50) {
		err := useDB.Clauses(clause.OnConflict{DoNothing: true}).Create(&chunk).Error
		if err != nil {
			return err
		}
	}
	return nil
}

func (channel *Channel) DeleteAbilities() error {
	return DB.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error
}

// UpdateAbilities updates abilities of this channel.
// Make sure the channel is completed before calling this function.
func (channel *Channel) UpdateAbilities(tx *gorm.DB) error {
	isNewTx := false
	// 如果没有传入事务，创建新的事务
	if tx == nil {
		tx = DB.Begin()
		if tx.Error != nil {
			return tx.Error
		}
		isNewTx = true
		defer func() {
			if r := recover(); r != nil {
				tx.Rollback()
			}
		}()
	}

	// Preserve route-scope fields while rebuilding abilities. Channel edits that
	// change models/groups must not silently reset the v2 routing configuration
	// for entries that remain in the new ability set.
	var existingAbilities []Ability
	if err := tx.Where("channel_id = ?", channel.Id).Find(&existingAbilities).Error; err != nil {
		if isNewTx {
			tx.Rollback()
		}
		return err
	}
	existingByKey := make(map[string]Ability, len(existingAbilities))
	for _, existing := range existingAbilities {
		existingByKey[existing.Group+"|"+existing.Model] = existing
	}

	// Build the replacement set before deleting the old rows. This lets the
	// V2 seeder inherit an existing channel member's scope fields when a new
	// raw model is added during an edit.
	models_ := strings.Split(channel.Models, ",")
	groups_ := strings.Split(channel.Group, ",")
	abilitySet := make(map[string]struct{})
	abilities := make([]Ability, 0, len(models_))
	newAbilityIndexes := make([]int, 0, len(models_))
	for _, model := range models_ {
		for _, group := range groups_ {
			key := group + "|" + model
			if _, exists := abilitySet[key]; exists {
				continue
			}
			abilitySet[key] = struct{}{}
			ability := Ability{
				Group:     group,
				Model:     model,
				ChannelId: channel.Id,
				Enabled:   channel.Status == common.ChannelStatusEnabled,
				Priority:  channel.Priority,
				Weight:    uint(channel.GetWeight()),
				Tag:       channel.Tag,
			}
			if existing, ok := existingByKey[key]; ok {
				// Nullable route values at zero with a false configured marker are
				// legacy rows. Keep those values unset so the legacy priority and
				// weight remain effective after a repair.
				if existing.HasExplicitRoutePriority() {
					ability.RoutePriority = existing.RoutePriority
					ability.RoutePriorityConfigured = true
				}
				if existing.HasExplicitRouteWeight() {
					ability.RouteWeightBPS = existing.RouteWeightBPS
					ability.RouteWeightBPSConfigured = true
				}
			} else {
				newAbilityIndexes = append(newAbilityIndexes, len(abilities))
			}
			abilities = append(abilities, ability)
		}
	}
	if len(newAbilityIndexes) > 0 {
		newAbilities := make([]Ability, 0, len(newAbilityIndexes))
		for _, index := range newAbilityIndexes {
			newAbilities = append(newAbilities, abilities[index])
		}
		if err := seedV2RouteFieldsForNewAbilities(tx, channel, newAbilities); err != nil {
			if isNewTx {
				tx.Rollback()
			}
			return err
		}
		for index, abilityIndex := range newAbilityIndexes {
			abilities[abilityIndex] = newAbilities[index]
		}
	}

	// Replace all abilities only after the replacement set and its V2 fields
	// have been validated. The surrounding transaction keeps this atomic.
	err := tx.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error
	if err != nil {
		if isNewTx {
			tx.Rollback()
		}
		return err
	}

	if len(abilities) > 0 {
		for _, chunk := range lo.Chunk(abilities, 50) {
			err = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&chunk).Error
			if err != nil {
				if isNewTx {
					tx.Rollback()
				}
				return err
			}
		}
	}

	// 如果是新创建的事务，需要提交
	if isNewTx {
		return tx.Commit().Error
	}

	return nil
}

func UpdateAbilityStatus(channelId int, status bool) error {
	if err := DB.Model(&Ability{}).Where("channel_id = ?", channelId).Select("enabled").Update("enabled", status).Error; err != nil {
		return err
	}
	refreshAbilityChannelCaches([]int{channelId})
	return nil
}

func UpdateAbilityStatusByTag(tag string, status bool) error {
	channelIDs, err := abilityChannelIDsByTag(tag)
	if err != nil {
		return err
	}
	if err := DB.Model(&Ability{}).Where("tag = ?", tag).Select("enabled").Update("enabled", status).Error; err != nil {
		return err
	}
	refreshAbilityChannelCaches(channelIDs)
	return nil
}

func UpdateAbilityByTag(tag string, newTag *string, priority *int64, weight *uint) error {
	if IsChannelRoutingV2StrictWrites() && (priority != nil || weight != nil) {
		return fmt.Errorf("%w: 请使用 /api/channel-route-scopes", ErrLegacyRouteFieldWriteBlocked)
	}
	channelIDs, err := abilityChannelIDsByTag(tag)
	if err != nil {
		return err
	}
	ability := Ability{}
	if newTag != nil {
		ability.Tag = newTag
	}
	if priority != nil {
		ability.Priority = priority
	}
	if weight != nil {
		ability.Weight = *weight
	}
	if err := DB.Model(&Ability{}).Where("tag = ?", tag).Updates(ability).Error; err != nil {
		return err
	}
	refreshAbilityChannelCaches(channelIDs)
	return nil
}

// abilityChannelIDsByTag returns the channel IDs whose ability rows are about
// to be mutated. Direct ability writes do not pass through the channel
// controller, so the caller must capture this set before changing a tag and
// refresh every affected route-cache projection afterwards.
func abilityChannelIDsByTag(tag string) ([]int, error) {
	if !common.MemoryCacheEnabled {
		return nil, nil
	}
	var channelIDs []int
	if err := DB.Model(&Ability{}).
		Where("tag = ?", tag).
		Distinct("channel_id").
		Pluck("channel_id", &channelIDs).Error; err != nil {
		return nil, err
	}
	sort.Ints(channelIDs)
	return channelIDs, nil
}

func refreshAbilityChannelCaches(channelIDs []int) {
	if !common.MemoryCacheEnabled {
		return
	}
	for _, channelID := range channelIDs {
		CacheReloadChannel(channelID)
	}
}

var fixLock = sync.Mutex{}

func FixAbility() (int, int, error) {
	lock := fixLock.TryLock()
	if !lock {
		return 0, 0, errors.New("已经有一个修复任务在运行中，请稍后再试")
	}
	defer fixLock.Unlock()

	var channels []*Channel
	// Find all channels
	err := DB.Model(&Channel{}).Find(&channels).Error
	if err != nil {
		return 0, 0, err
	}
	successCount := 0
	failCount := 0
	// Rebuild each channel through UpdateAbilities instead of truncating the
	// table first. UpdateAbilities snapshots the existing raw abilities before
	// replacing them, so route-scope fields survive a repair. It also keeps the
	// repair compatible with all supported databases and avoids a dialect-only
	// TRUNCATE path.
	for _, channel := range channels {
		if err := channel.UpdateAbilities(nil); err != nil {
			common.SysLog(fmt.Sprintf("Update abilities for channel %d failed: %s", channel.Id, err.Error()))
			failCount++
		} else {
			successCount++
		}
	}
	// Remove orphaned abilities left behind by deleted channels. A NOT EXISTS
	// subquery avoids materializing every channel ID into one potentially
	// oversized NOT IN parameter list and works across SQLite, MySQL, and
	// PostgreSQL. This runs only after channel rebuilds so a failed repair does
	// not erase its existing route configuration.
	orphanedChannel := DB.Model(&Channel{}).
		Select("1").
		Where("channels.id = abilities.channel_id")
	if err := DB.Model(&Ability{}).
		Where("NOT EXISTS (?)", orphanedChannel).
		Delete(&Ability{}).Error; err != nil {
		return successCount, failCount, err
	}
	InitChannelCache()
	return successCount, failCount, nil
}
