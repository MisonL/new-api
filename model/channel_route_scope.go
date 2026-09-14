package model

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RouteWeightTotalBPS is the fixed denominator used by route scopes.
const RouteWeightTotalBPS uint = 10000

var ErrRouteScopeVersionConflict = errors.New("channel route scope version conflict")
var ErrRouteScopeMigrationConflicts = errors.New("channel route scope migration has unresolved conflicts")
var ErrLegacyRouteFieldWriteBlocked = errors.New("legacy route priority/weight writes are disabled")
var ErrRouteScopeNoPositiveWeight = errors.New("route scope must contain at least one positive weight")

func uniqueChannelIDs(abilities []Ability) []int {
	ids := make([]int, 0, len(abilities))
	seen := make(map[int]struct{}, len(abilities))
	for _, ability := range abilities {
		if ability.ChannelId <= 0 {
			continue
		}
		if _, exists := seen[ability.ChannelId]; exists {
			continue
		}
		seen[ability.ChannelId] = struct{}{}
		ids = append(ids, ability.ChannelId)
	}
	return ids
}

// ChannelRouteScope is the administrative view of one group/model/priority
// routing pool. The ID is an opaque, URL-safe encoding of its key.
type ChannelRouteScope struct {
	ID                  string                  `json:"id"`
	Group               string                  `json:"group"`
	EffectiveRouteModel string                  `json:"effective_route_model"`
	RoutePriority       int64                   `json:"route_priority"`
	Version             string                  `json:"version"`
	MemberHash          string                  `json:"member_hash"`
	TotalWeightBPS      uint                    `json:"total_weight_bps"`
	Items               []ChannelRouteScopeItem `json:"items"`
}

type ChannelRouteScopeItem struct {
	ChannelID      int    `json:"channel_id"`
	ChannelName    string `json:"channel_name"`
	ChannelType    int    `json:"channel_type"`
	ChannelStatus  int    `json:"channel_status"`
	AbilityEnabled bool   `json:"ability_enabled"`
	RoutePriority  int64  `json:"route_priority"`
	RouteWeightBPS uint   `json:"route_weight_bps"`
	LegacyWeight   uint   `json:"legacy_weight,omitempty"`
	MaxConcurrency int    `json:"max_concurrency"`

	// sourceModels is intentionally kept out of the API response. A single
	// channel can expose more than one raw ability that maps to the same
	// effective route model. Scope writes must retain those raw identities
	// instead of using the effective model as a database key.
	sourceModels []string
}

type routeScopeBuildMember struct {
	item      ChannelRouteScopeItem
	abilities []Ability
}

type ChannelRouteScopeWeightItem struct {
	ChannelID      int  `json:"channel_id"`
	RouteWeightBPS uint `json:"route_weight_bps"`
}

type ChannelRouteScopeWeightUpdate struct {
	Version string                        `json:"version"`
	Mode    string                        `json:"mode,omitempty"`
	Items   []ChannelRouteScopeWeightItem `json:"items"`
}

type ChannelRouteScopePriorityUpdate struct {
	Version       string `json:"version"`
	RoutePriority int64  `json:"route_priority"`
}

// ChannelRouteScopeMigrationItem describes one channel member during a
// route-field backfill. A channel can expose multiple raw abilities that map
// to the same effective route model; migration therefore reports and writes
// one route weight per channel, not one weight per raw ability.
type ChannelRouteScopeMigrationItem struct {
	ChannelID           int    `json:"channel_id"`
	RawAbilityCount     int    `json:"raw_ability_count"`
	OldRoutePriority    *int64 `json:"old_route_priority,omitempty"`
	OldRouteWeightBPS   *uint  `json:"old_route_weight_bps,omitempty"`
	LegacyVotes         uint64 `json:"legacy_votes"`
	NewRoutePriority    int64  `json:"new_route_priority"`
	NewRouteWeightBPS   uint   `json:"new_route_weight_bps"`
	NeedsBackfill       bool   `json:"needs_backfill"`
	PartialWeightConfig bool   `json:"partial_weight_config"`
}

type ChannelRouteScopeMigrationScope struct {
	ID                  string                           `json:"id"`
	Group               string                           `json:"group"`
	EffectiveRouteModel string                           `json:"effective_route_model"`
	RoutePriority       int64                            `json:"route_priority"`
	CandidateCount      int                              `json:"candidate_count"`
	Items               []ChannelRouteScopeMigrationItem `json:"items"`
	MappingConflicts    []string                         `json:"mapping_conflicts,omitempty"`
	WeightConflicts     []string                         `json:"weight_conflicts,omitempty"`
	Skipped             bool                             `json:"skipped"`
	Error               string                           `json:"error,omitempty"`
}

// ChannelRouteScopeMigrationReport is returned by both preview and apply.
// Counts are kept at ability and channel granularity because a channel may
// expose multiple raw abilities that map to one effective route model.
type ChannelRouteScopeMigrationReport struct {
	DryRun              bool                              `json:"dry_run"`
	ScopeCount          int                               `json:"scope_count"`
	CandidateScopes     int                               `json:"candidate_scopes"`
	AbilitiesToBackfill int                               `json:"abilities_to_backfill"`
	ChannelsToBackfill  int                               `json:"channels_to_backfill"`
	ZeroBPSCount        int                               `json:"zero_bps_count"`
	SkippedScopes       int                               `json:"skipped_scopes"`
	MappingConflicts    []string                          `json:"mapping_conflicts,omitempty"`
	WeightConflicts     []string                          `json:"weight_conflicts,omitempty"`
	Errors              []string                          `json:"errors,omitempty"`
	Scopes              []ChannelRouteScopeMigrationScope `json:"scopes"`
}

type routeScopeKey struct {
	Group               string
	EffectiveRouteModel string
	RoutePriority       int64
}

func IsChannelRoutingV2Enabled() bool {
	return common.GetEnvOrDefaultBool("CHANNEL_ROUTING_V2_ENABLED", false)
}

func IsChannelRoutingShadowEnabled() bool {
	return common.GetEnvOrDefaultBool("CHANNEL_ROUTING_SHADOW_ENABLED", false)
}

// IsChannelRoutingV2StrictWrites controls the migration-time guard that rejects
// legacy channel priority/weight writes. Once V2 selection is enabled, the
// guard is on by default; operators can explicitly set the flag to false for a
// bounded compatibility window during frontend migration.
func IsChannelRoutingV2StrictWrites() bool {
	return common.GetEnvOrDefaultBool("CHANNEL_ROUTING_V2_STRICT_WRITES", IsChannelRoutingV2Enabled())
}

func effectiveRouteModelForAbility(ability Ability, channel *Channel) (string, error) {
	requested := strings.TrimSpace(ability.Model)
	if channel == nil || requested == "" {
		return requested, nil
	}
	_, compact := ratio_setting.CompactBaseModelName(requested)
	effective, _, err := ResolveChannelModelMapping(channel.GetModelMapping(), requested, compact)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(effective) == "" {
		return requested, nil
	}
	return strings.TrimSpace(effective), nil
}

func EncodeRouteScopeID(group string, effectiveRouteModel string, priority int64) string {
	key := strings.Join([]string{strings.TrimSpace(group), strings.TrimSpace(effectiveRouteModel), strconv.FormatInt(priority, 10)}, "\x00")
	return base64.RawURLEncoding.EncodeToString([]byte(key))
}

func DecodeRouteScopeID(id string) (group string, effectiveRouteModel string, priority int64, err error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(id))
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid route scope id: %w", err)
	}
	parts := strings.Split(string(raw), "\x00")
	if len(parts) != 3 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", 0, errors.New("invalid route scope id")
	}
	priority, err = strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return "", "", 0, errors.New("invalid route scope priority")
	}
	// Legacy channel priorities may be negative. Keep those scopes addressable
	// for read/weight updates during migration; newly written priorities remain
	// non-negative through UpdateChannelRouteScopePriority and the UI.
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), priority, nil
}

func routeScopeVersion(items []ChannelRouteScopeItem) string {
	return routeScopeMemberHash(items)
}

func routeScopeMemberHash(items []ChannelRouteScopeItem) string {
	ordered := append([]ChannelRouteScopeItem(nil), items...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ChannelID < ordered[j].ChannelID })
	var builder strings.Builder
	for _, item := range ordered {
		builder.WriteString(strconv.Itoa(item.ChannelID))
		builder.WriteByte(':')
		builder.WriteString(item.ChannelName)
		builder.WriteByte(':')
		builder.WriteString(strconv.Itoa(item.ChannelType))
		builder.WriteByte(':')
		builder.WriteString(strconv.Itoa(item.ChannelStatus))
		builder.WriteByte(':')
		if item.AbilityEnabled {
			builder.WriteByte('1')
		} else {
			builder.WriteByte('0')
		}
		builder.WriteByte(':')
		builder.WriteString(strconv.FormatInt(item.RoutePriority, 10))
		builder.WriteByte(':')
		builder.WriteString(strconv.FormatUint(uint64(item.RouteWeightBPS), 10))
		builder.WriteByte(':')
		builder.WriteString(strconv.Itoa(item.MaxConcurrency))
		builder.WriteByte(':')
		rawModels := append([]string(nil), item.sourceModels...)
		sort.Strings(rawModels)
		builder.WriteString(strings.Join(rawModels, ","))
		builder.WriteByte(';')
	}
	digest := sha256.Sum256([]byte(builder.String()))
	return hex.EncodeToString(digest[:])
}

func legacyVotesToBPS(weights []uint) []uint {
	if len(weights) == 0 {
		return nil
	}
	return normalizeWeightsByNumerators(func(i int) uint64 {
		value := uint64(weights[i])
		const maxUint64 = ^uint64(0)
		if value > maxUint64-10 {
			return maxUint64
		}
		return value + 10
	}, len(weights))
}

// NormalizeRouteWeights converts arbitrary positive numerators to exact bps
// using largest remainder. Zero numerators remain zero and never get revived.
func NormalizeRouteWeights(numerators []uint64) []uint {
	return normalizeWeightsByNumerators(func(i int) uint64 { return numerators[i] }, len(numerators))
}

func normalizeWeightsByNumerators(numerator func(int) uint64, length int) []uint {
	result := make([]uint, length)
	if length == 0 {
		return result
	}
	values := make([]*big.Int, length)
	total := new(big.Int)
	for i := 0; i < length; i++ {
		value := new(big.Int).SetUint64(numerator(i))
		values[i] = value
		total.Add(total, value)
	}
	if total.Sign() == 0 {
		return result
	}
	type remainder struct {
		index int
		value *big.Int
	}
	remainders := make([]remainder, 0, length)
	var assigned uint64
	for i := 0; i < length; i++ {
		product := new(big.Int).Mul(values[i], new(big.Int).SetUint64(uint64(RouteWeightTotalBPS)))
		quotient, remValue := new(big.Int), new(big.Int)
		quotient.QuoRem(product, total, remValue)
		base := quotient.Uint64()
		if base > uint64(RouteWeightTotalBPS) {
			base = uint64(RouteWeightTotalBPS)
		}
		result[i] = uint(base)
		assigned += base
		remainders = append(remainders, remainder{index: i, value: remValue})
	}
	remaining := uint64(RouteWeightTotalBPS) - assigned
	sort.SliceStable(remainders, func(i, j int) bool {
		if remainders[i].value.Cmp(remainders[j].value) == 0 {
			return remainders[i].index < remainders[j].index
		}
		return remainders[i].value.Cmp(remainders[j].value) > 0
	})
	for i := uint64(0); i < remaining; i++ {
		result[remainders[i%uint64(len(remainders))].index]++
	}
	return result
}

// AdjustRouteWeights applies the UI's equal or proportional redistribution
// while preserving an exact 10000 bps total. locked entries are not changed.
func AdjustRouteWeights(current []uint, targetIndex int, target uint, mode string, locked []bool) ([]uint, error) {
	if targetIndex < 0 || targetIndex >= len(current) || target > RouteWeightTotalBPS {
		return nil, errors.New("route weight is out of range")
	}
	if len(locked) != 0 && len(locked) != len(current) {
		return nil, errors.New("locked entries length mismatch")
	}
	if len(locked) != 0 && locked[targetIndex] {
		return nil, errors.New("locked route weight cannot be adjusted")
	}
	if sumRouteWeights(current) != RouteWeightTotalBPS {
		return nil, errors.New("route weights must sum to 10000")
	}
	result := append([]uint(nil), current...)
	old := result[targetIndex]
	result[targetIndex] = target
	delta := int64(target) - int64(old)
	if delta == 0 {
		return result, nil
	}
	participants := make([]int, 0, len(current))
	for i := range current {
		if i == targetIndex || (len(locked) != 0 && locked[i]) {
			continue
		}
		participants = append(participants, i)
	}
	if len(participants) == 0 {
		return nil, errors.New("no unlocked route weights available for adjustment")
	}
	if delta > 0 {
		if uint(delta) > sumIndexes(result, participants) {
			return nil, errors.New("route weight redistribution underflow")
		}
		if mode == "proportional_adjust" || mode == "proportional" {
			redistributeProportional(result, participants, uint(delta), false)
		} else {
			redistributeEqual(result, participants, uint(delta), true)
		}
	} else {
		amount := uint(-delta)
		if mode == "proportional_adjust" || mode == "proportional" {
			redistributeProportional(result, participants, amount, true)
		} else {
			redistributeEqual(result, participants, amount, false)
		}
	}
	if sumRouteWeights(result) != RouteWeightTotalBPS {
		return nil, errors.New("route weight adjustment did not preserve total")
	}
	return result, nil
}

func sumRouteWeights(values []uint) uint {
	var total uint64
	for _, value := range values {
		total += uint64(value)
	}
	if total > uint64(^uint(0)) {
		return ^uint(0)
	}
	return uint(total)
}

func sumIndexes(values []uint, indexes []int) uint {
	var total uint
	for _, index := range indexes {
		total += values[index]
	}
	return total
}

func redistributeEqual(values []uint, indexes []int, amount uint, subtract bool) {
	if amount == 0 || len(indexes) == 0 {
		return
	}
	for amount > 0 {
		// Do not reuse indexes' backing array here. When a participant reaches
		// zero it is filtered out on the next round; writing the filtered list
		// into indexes would overwrite the source list and can duplicate one
		// participant or drop another (which changes the intended water-filling
		// distribution while still preserving the total).
		active := make([]int, 0, len(indexes))
		for _, index := range indexes {
			if !subtract || values[index] > 0 {
				active = append(active, index)
			}
		}
		if len(active) == 0 {
			return
		}
		share := amount / uint(len(active))
		if share == 0 {
			share = 1
		}
		progressed := false
		for _, index := range active {
			if amount == 0 {
				break
			}
			step := share
			if step > amount {
				step = amount
			}
			if subtract {
				if step > values[index] {
					step = values[index]
				}
				values[index] -= step
			} else {
				capacity := RouteWeightTotalBPS - values[index]
				if step > capacity {
					step = capacity
				}
				values[index] += step
			}
			if step > 0 {
				progressed = true
			}
			amount -= step
		}
		if !progressed {
			return
		}
	}
}

func redistributeProportional(values []uint, indexes []int, amount uint, add bool) {
	if amount == 0 || len(indexes) == 0 {
		return
	}
	numerators := make([]uint64, len(indexes))
	for i, index := range indexes {
		numerators[i] = uint64(values[index])
	}
	if sumUint64(numerators) == 0 {
		redistributeEqual(values, indexes, amount, !add)
		return
	}
	shares := normalizeToTotal(numerators, uint64(amount))
	for i, index := range indexes {
		if add {
			values[index] += uint(shares[i])
		} else {
			if shares[i] > uint64(values[index]) {
				values[index] = 0
			} else {
				values[index] -= uint(shares[i])
			}
		}
	}
}

func sumUint64(values []uint64) uint64 {
	var total uint64
	for _, value := range values {
		if ^uint64(0)-total < value {
			return ^uint64(0)
		}
		total += value
	}
	return total
}

func normalizeToTotal(numerators []uint64, target uint64) []uint64 {
	result := make([]uint64, len(numerators))
	values := make([]*big.Int, len(numerators))
	total := new(big.Int)
	for i, value := range numerators {
		values[i] = new(big.Int).SetUint64(value)
		total.Add(total, values[i])
	}
	if total.Sign() == 0 {
		return result
	}
	type rem struct {
		index int
		value *big.Int
	}
	rems := make([]rem, 0, len(numerators))
	var assigned uint64
	for i, value := range values {
		product := new(big.Int).Mul(value, new(big.Int).SetUint64(target))
		quotient, remValue := new(big.Int), new(big.Int)
		quotient.QuoRem(product, total, remValue)
		result[i] = quotient.Uint64()
		assigned += result[i]
		rems = append(rems, rem{i, remValue})
	}
	sort.SliceStable(rems, func(i, j int) bool {
		if rems[i].value.Cmp(rems[j].value) == 0 {
			return rems[i].index < rems[j].index
		}
		return rems[i].value.Cmp(rems[j].value) > 0
	})
	for i := uint64(0); i < target-assigned; i++ {
		result[rems[i%uint64(len(rems))].index]++
	}
	return result
}

func ListChannelRouteScopes(groupFilter string, modelFilter string) ([]ChannelRouteScope, error) {
	ensureAbilityColumnsInitialized()
	var abilities []Ability
	query := DB.Order(commonGroupCol + " ASC, model ASC, COALESCE(route_priority, priority, 0) DESC, channel_id ASC")
	if strings.TrimSpace(groupFilter) != "" {
		query = query.Where(commonGroupCol+" = ?", strings.TrimSpace(groupFilter))
	}
	if err := query.Find(&abilities).Error; err != nil {
		return nil, err
	}
	scopes, err := buildRouteScopesFromAbilities(abilities)
	if err != nil {
		return nil, err
	}
	modelFilter = strings.TrimSpace(modelFilter)
	if modelFilter == "" {
		return scopes, nil
	}
	filtered := make([]ChannelRouteScope, 0, len(scopes))
	for _, scope := range scopes {
		if scope.EffectiveRouteModel == modelFilter {
			filtered = append(filtered, scope)
		}
	}
	return filtered, nil
}

func GetChannelRouteScope(id string) (*ChannelRouteScope, error) {
	ensureAbilityColumnsInitialized()
	group, routeModel, priority, err := DecodeRouteScopeID(id)
	if err != nil {
		return nil, err
	}
	abilities, err := loadAbilitiesForRouteScope(DB, group, routeModel)
	if err != nil {
		return nil, err
	}
	scopes, err := buildRouteScopesFromAbilities(abilities)
	if err != nil {
		return nil, err
	}
	for i := range scopes {
		if scopes[i].RoutePriority == priority {
			return &scopes[i], nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func loadAbilitiesForRouteScope(tx *gorm.DB, group string, effectiveModel string) ([]Ability, error) {
	if tx == nil {
		tx = DB
	}
	var abilities []Ability
	if err := tx.Where(commonGroupCol+" = ?", group).Find(&abilities).Error; err != nil {
		return nil, err
	}
	if len(abilities) == 0 {
		return abilities, nil
	}
	return filterAbilitiesForRouteScope(tx, abilities, effectiveModel)
}

func filterAbilitiesForRouteScope(tx *gorm.DB, abilities []Ability, effectiveModel string) ([]Ability, error) {
	if tx == nil {
		tx = DB
	}
	if len(abilities) == 0 {
		return []Ability{}, nil
	}
	ids := uniqueChannelIDs(abilities)
	var channels []Channel
	if err := tx.Where("id IN ?", ids).Find(&channels).Error; err != nil {
		return nil, err
	}
	channelByID := make(map[int]*Channel, len(channels))
	for i := range channels {
		channelByID[channels[i].Id] = &channels[i]
	}
	filtered := make([]Ability, 0, len(abilities))
	for _, ability := range abilities {
		resolved, err := effectiveRouteModelForAbility(ability, channelByID[ability.ChannelId])
		if err != nil {
			if IsChannelModelMappingError(err) {
				// A malformed mapping belongs to this ability/channel only. Keep
				// unrelated valid scopes readable and leave the explicit conflict
				// report to the migration preview/apply workflow.
				common.SysError(fmt.Sprintf("route scope skipped invalid model mapping: group=%s model=%s channel_id=%d error=%v", ability.Group, ability.Model, ability.ChannelId, err))
				continue
			}
			return nil, err
		}
		if resolved == effectiveModel {
			filtered = append(filtered, ability)
		}
	}
	return filtered, nil
}

func buildRouteScopesFromAbilities(abilities []Ability) ([]ChannelRouteScope, error) {
	if len(abilities) == 0 {
		return []ChannelRouteScope{}, nil
	}
	ids := uniqueChannelIDs(abilities)
	var channels []Channel
	if err := DB.Where("id IN ?", ids).Find(&channels).Error; err != nil {
		return nil, err
	}
	channelByID := make(map[int]*Channel, len(channels))
	for i := range channels {
		channelByID[channels[i].Id] = &channels[i]
	}
	return buildRouteScopesFromAbilityAndChannels(abilities, channelByID)
}

func buildRouteScopesFromAbilityAndChannels(abilities []Ability, channelByID map[int]*Channel) ([]ChannelRouteScope, error) {
	grouped := make(map[routeScopeKey][]Ability)
	for _, ability := range abilities {
		channel := channelByID[ability.ChannelId]
		if channel == nil {
			continue
		}
		effectiveModel, err := effectiveRouteModelForAbility(ability, channel)
		if err != nil {
			if IsChannelModelMappingError(err) {
				// Do not let one broken channel mapping make the entire scope list
				// unreadable. The migration report still records this conflict so it
				// cannot be mistaken for a successful repair.
				common.SysError(fmt.Sprintf("route scope skipped invalid model mapping: group=%s model=%s channel_id=%d error=%v", ability.Group, ability.Model, ability.ChannelId, err))
				continue
			}
			return nil, err
		}
		key := routeScopeKey{Group: strings.TrimSpace(ability.Group), EffectiveRouteModel: effectiveModel, RoutePriority: ability.GetEffectiveRoutePriority()}
		grouped[key] = append(grouped[key], ability)
	}
	keys := make([]routeScopeKey, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Group != keys[j].Group {
			return keys[i].Group < keys[j].Group
		}
		if keys[i].EffectiveRouteModel != keys[j].EffectiveRouteModel {
			return keys[i].EffectiveRouteModel < keys[j].EffectiveRouteModel
		}
		return keys[i].RoutePriority > keys[j].RoutePriority
	})
	result := make([]ChannelRouteScope, 0, len(keys))
	for _, key := range keys {
		itemsAbilities := grouped[key]
		byChannel := make(map[int][]Ability)
		for _, ability := range itemsAbilities {
			byChannel[ability.ChannelId] = append(byChannel[ability.ChannelId], ability)
		}
		channelIDs := make([]int, 0, len(byChannel))
		for channelID := range byChannel {
			channelIDs = append(channelIDs, channelID)
		}
		sort.Ints(channelIDs)
		items := make([]ChannelRouteScopeItem, 0, len(channelIDs))
		members := make([][]Ability, 0, len(channelIDs))
		for _, channelID := range channelIDs {
			channel := channelByID[channelID]
			if channel == nil {
				continue
			}
			memberAbilities := byChannel[channelID]
			sort.Slice(memberAbilities, func(i, j int) bool { return memberAbilities[i].Model < memberAbilities[j].Model })
			item, err := buildRouteScopeItem(channel, key.RoutePriority, memberAbilities)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
			members = append(members, memberAbilities)
		}
		applyDerivedRouteWeights(items, members)
		sort.Slice(items, func(i, j int) bool { return items[i].ChannelID < items[j].ChannelID })
		hash := routeScopeMemberHash(items)
		result = append(result, ChannelRouteScope{ID: EncodeRouteScopeID(key.Group, key.EffectiveRouteModel, key.RoutePriority), Group: key.Group, EffectiveRouteModel: key.EffectiveRouteModel, RoutePriority: key.RoutePriority, Version: hash, MemberHash: hash, TotalWeightBPS: sumRouteWeightsFromItems(items), Items: items})
	}
	return result, nil
}

func valueOrZero(value *uint) uint {
	if value == nil {
		return 0
	}
	return *value
}

func buildRouteScopeItem(channel *Channel, priority int64, abilities []Ability) (ChannelRouteScopeItem, error) {
	if channel == nil || len(abilities) == 0 {
		return ChannelRouteScopeItem{}, errors.New("route scope member is missing")
	}
	item := ChannelRouteScopeItem{
		ChannelID:      channel.Id,
		ChannelName:    channel.Name,
		ChannelType:    channel.Type,
		ChannelStatus:  channel.Status,
		AbilityEnabled: false,
		RoutePriority:  priority,
		MaxConcurrency: channel.MaxConcurrency,
		sourceModels:   make([]string, 0, len(abilities)),
	}
	var explicitWeight *uint
	var legacyWeight uint64
	for _, ability := range abilities {
		item.sourceModels = append(item.sourceModels, ability.Model)
		item.AbilityEnabled = item.AbilityEnabled || ability.Enabled
		legacyWeight += uint64(ability.Weight)
		if ability.HasExplicitRouteWeight() {
			if explicitWeight == nil {
				value := *ability.RouteWeightBPS
				explicitWeight = &value
			} else if *explicitWeight != *ability.RouteWeightBPS {
				return ChannelRouteScopeItem{}, fmt.Errorf("route scope channel %d has conflicting weights across mapped abilities", channel.Id)
			}
		}
	}
	if legacyWeight > uint64(^uint(0)) {
		item.LegacyWeight = ^uint(0)
	} else {
		item.LegacyWeight = uint(legacyWeight)
	}
	if explicitWeight != nil {
		item.RouteWeightBPS = *explicitWeight
	}
	return item, nil
}

func sumRouteWeightsFromItems(items []ChannelRouteScopeItem) uint {
	var total uint
	for _, item := range items {
		total += item.RouteWeightBPS
	}
	return total
}

func updateRouteScopeAbility(tx *gorm.DB, ability Ability, priority int64, weight uint) error {
	values := map[string]any{
		"route_priority":              priority,
		"route_priority_configured":   true,
		"route_weight_bps":            weight,
		"route_weight_bps_configured": true,
	}
	result := tx.Model(&Ability{}).
		Where(commonGroupCol+" = ? AND model = ? AND channel_id = ?", ability.Group, ability.Model, ability.ChannelId).
		Updates(values)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("route scope ability not found: group=%s model=%s channel_id=%d", ability.Group, ability.Model, ability.ChannelId)
	}
	return nil
}

func updateRouteScopeItem(tx *gorm.DB, group string, item ChannelRouteScopeItem, priority int64, weight uint) error {
	if len(item.sourceModels) == 0 {
		return fmt.Errorf("route scope channel %d has no source ability identity", item.ChannelID)
	}
	for _, rawModel := range item.sourceModels {
		ability := Ability{Group: group, Model: rawModel, ChannelId: item.ChannelID}
		if err := updateRouteScopeAbility(tx, ability, priority, weight); err != nil {
			return err
		}
	}
	return nil
}

// applyDerivedRouteWeights keeps explicit bps values intact when a scope is
// fully migrated. During a partial migration, missing values participate using
// their legacy vote so the scope remains deterministic without silently
// replacing already configured route weights.
func applyDerivedRouteWeights(items []ChannelRouteScopeItem, abilitiesByItem [][]Ability) {
	if len(items) == 0 || len(items) != len(abilitiesByItem) {
		return
	}
	missing := false
	for _, abilities := range abilitiesByItem {
		if !routeScopeAbilitiesHaveExplicitWeight(abilities) {
			missing = true
			break
		}
	}
	if !missing {
		return
	}
	numerators := make([]uint64, len(items))
	for i, abilities := range abilitiesByItem {
		if routeScopeAbilitiesHaveExplicitWeight(abilities) {
			numerators[i] = uint64(items[i].RouteWeightBPS)
			continue
		}
		legacy := uint64(items[i].LegacyWeight)
		if legacy > ^uint64(0)-10 {
			legacy = ^uint64(0)
		} else {
			legacy += 10
		}
		numerators[i] = legacy
	}
	bps := NormalizeRouteWeights(numerators)
	for i := range items {
		items[i].RouteWeightBPS = bps[i]
	}
}

func routeScopeAbilitiesHaveExplicitWeight(abilities []Ability) bool {
	if len(abilities) == 0 {
		return false
	}
	for _, ability := range abilities {
		if !ability.HasExplicitRouteWeight() {
			return false
		}
	}
	return true
}

func UpdateChannelRouteScopeWeights(id string, update ChannelRouteScopeWeightUpdate) (*ChannelRouteScope, error) {
	ensureAbilityColumnsInitialized()
	group, routeModel, priority, err := DecodeRouteScopeID(id)
	if err != nil {
		return nil, err
	}
	if len(update.Items) == 0 {
		return nil, errors.New("route scope items cannot be empty")
	}
	if strings.TrimSpace(update.Version) == "" {
		return nil, errors.New("route scope version is required")
	}
	if mode := strings.TrimSpace(update.Mode); mode != "" && mode != "equal_adjust" && mode != "proportional_adjust" && mode != "equal" && mode != "proportional" {
		return nil, errors.New("unsupported route weight adjustment mode")
	}
	seen := make(map[int]struct{}, len(update.Items))
	var total uint64
	for _, item := range update.Items {
		if item.ChannelID <= 0 || item.RouteWeightBPS > RouteWeightTotalBPS {
			return nil, errors.New("route weight is out of range")
		}
		if _, ok := seen[item.ChannelID]; ok {
			return nil, errors.New("duplicate route scope channel")
		}
		seen[item.ChannelID] = struct{}{}
		total += uint64(item.RouteWeightBPS)
	}
	if total != uint64(RouteWeightTotalBPS) {
		return nil, errors.New("route weights must sum to 10000")
	}
	returnScope := (*ChannelRouteScope)(nil)
	err = DB.Transaction(func(tx *gorm.DB) error {
		var abilities []Ability
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(commonGroupCol+" = ?", group).Find(&abilities).Error; err != nil {
			return err
		}
		abilities, err = filterAbilitiesForRouteScope(tx, abilities, routeModel)
		if err != nil {
			return err
		}
		if err := lockRouteScopeChannels(tx, abilities); err != nil {
			return err
		}
		current, err := buildRouteScopesFromAbilitiesWithDB(tx, abilities)
		if err != nil {
			return err
		}
		var scope *ChannelRouteScope
		for i := range current {
			if current[i].RoutePriority == priority {
				scope = &current[i]
				break
			}
		}
		if scope == nil {
			return gorm.ErrRecordNotFound
		}
		if update.Version != scope.Version {
			return ErrRouteScopeVersionConflict
		}
		member := make(map[int]ChannelRouteScopeItem, len(scope.Items))
		for _, item := range scope.Items {
			member[item.ChannelID] = item
		}
		if len(member) != len(update.Items) {
			return errors.New("route scope membership changed")
		}
		for _, item := range update.Items {
			if _, ok := member[item.ChannelID]; !ok {
				return errors.New("channel is not a member of route scope")
			}
		}
		for _, item := range update.Items {
			value := item.RouteWeightBPS
			if err := updateRouteScopeItem(tx, group, member[item.ChannelID], priority, value); err != nil {
				return err
			}
		}
		updated, err := buildRouteScopesFromDB(tx, group, routeModel)
		if err != nil {
			return err
		}
		for i := range updated {
			if updated[i].RoutePriority == priority {
				returnScope = &updated[i]
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	requestChannelCacheRefreshAsync()
	return returnScope, nil
}

func UpdateChannelRouteScopePriority(id string, update ChannelRouteScopePriorityUpdate) (*ChannelRouteScope, error) {
	ensureAbilityColumnsInitialized()
	group, routeModel, oldPriority, err := DecodeRouteScopeID(id)
	if err != nil {
		return nil, err
	}
	if update.RoutePriority < 0 {
		return nil, errors.New("route priority is out of range")
	}
	if strings.TrimSpace(update.Version) == "" {
		return nil, errors.New("route scope version is required")
	}
	var returnScope *ChannelRouteScope
	err = DB.Transaction(func(tx *gorm.DB) error {
		var abilities []Ability
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(commonGroupCol+" = ?", group).Find(&abilities).Error; err != nil {
			return err
		}
		abilities, err = filterAbilitiesForRouteScope(tx, abilities, routeModel)
		if err != nil {
			return err
		}
		if err := lockRouteScopeChannels(tx, abilities); err != nil {
			return err
		}
		scopes, err := buildRouteScopesFromAbilitiesWithDB(tx, abilities)
		if err != nil {
			return err
		}
		var source, target *ChannelRouteScope
		for i := range scopes {
			switch scopes[i].RoutePriority {
			case oldPriority:
				source = &scopes[i]
			case update.RoutePriority:
				target = &scopes[i]
			}
		}
		if source == nil {
			return gorm.ErrRecordNotFound
		}
		if update.Version != source.Version {
			return ErrRouteScopeVersionConflict
		}
		if oldPriority == update.RoutePriority {
			copyScope := *source
			returnScope = &copyScope
			return nil
		}

		// Moving a whole scope into an existing priority must merge the two
		// pools and normalize once; otherwise the target scope can exceed 100%.
		weights := make(map[int]uint, len(source.Items))
		sourceItems := make(map[int]ChannelRouteScopeItem, len(source.Items))
		for _, item := range source.Items {
			weights[item.ChannelID] = item.RouteWeightBPS
			sourceItems[item.ChannelID] = item
		}
		targetItems := make(map[int]ChannelRouteScopeItem)
		if target != nil {
			for _, item := range target.Items {
				if _, exists := sourceItems[item.ChannelID]; exists {
					return errors.New("route scope priority move has ambiguous channel identity")
				}
				weights[item.ChannelID] = item.RouteWeightBPS
				targetItems[item.ChannelID] = item
			}
		}
		ids := make([]int, 0, len(weights))
		numerators := make([]uint64, 0, len(weights))
		for channelID, weight := range weights {
			ids = append(ids, channelID)
			numerators = append(numerators, uint64(weight))
		}
		// A persisted scope is required to contain at least one active member.
		// NormalizeRouteWeights intentionally preserves an all-zero input as all
		// zero, which is correct for a diagnostic/migration snapshot but would
		// make a priority move silently create an invalid static scope. Reject the
		// move before writing any ability rows; the surrounding transaction keeps
		// the source scope unchanged.
		var numeratorTotal uint64
		for _, numerator := range numerators {
			if ^uint64(0)-numeratorTotal < numerator {
				numeratorTotal = ^uint64(0)
				break
			}
			numeratorTotal += numerator
		}
		if numeratorTotal == 0 {
			return ErrRouteScopeNoPositiveWeight
		}
		order := make([]int, len(ids))
		for i := range ids {
			order[i] = i
		}
		sort.Slice(order, func(i, j int) bool { return ids[order[i]] < ids[order[j]] })
		sortedIDs := make([]int, len(ids))
		sortedNumerators := make([]uint64, len(ids))
		for i, index := range order {
			sortedIDs[i] = ids[index]
			sortedNumerators[i] = numerators[index]
		}
		bps := NormalizeRouteWeights(sortedNumerators)
		for i, channelID := range sortedIDs {
			item, ok := sourceItems[channelID]
			if !ok {
				item = targetItems[channelID]
			}
			if err := updateRouteScopeItem(tx, group, item, update.RoutePriority, bps[i]); err != nil {
				return err
			}
		}
		updated, err := buildRouteScopesFromDB(tx, group, routeModel)
		if err != nil {
			return err
		}
		for i := range updated {
			if updated[i].RoutePriority == update.RoutePriority {
				returnScope = &updated[i]
				break
			}
		}
		if returnScope == nil {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	requestChannelCacheRefreshAsync()
	return returnScope, nil
}

func buildRouteScopesFromDB(tx *gorm.DB, group string, routeModel string) ([]ChannelRouteScope, error) {
	abilities, err := loadAbilitiesForRouteScope(tx, group, routeModel)
	if err != nil {
		return nil, err
	}
	return buildRouteScopesFromAbilitiesWithDB(tx, abilities)
}

func lockRouteScopeChannels(tx *gorm.DB, abilities []Ability) error {
	if tx == nil || len(abilities) == 0 {
		return nil
	}
	ids := uniqueChannelIDs(abilities)
	var channels []Channel
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", ids).Find(&channels).Error
}

func buildRouteScopesFromAbilitiesWithDB(tx *gorm.DB, abilities []Ability) ([]ChannelRouteScope, error) {
	// The scope builder normally uses the process DB. For transactions, load
	// the same channel rows through the transaction and temporarily assemble
	// the result without changing global DB state.
	if len(abilities) == 0 {
		return []ChannelRouteScope{}, nil
	}
	ids := uniqueChannelIDs(abilities)
	var channels []Channel
	if err := tx.Where("id IN ?", ids).Find(&channels).Error; err != nil {
		return nil, err
	}
	channelByID := make(map[int]*Channel, len(channels))
	for i := range channels {
		channelByID[channels[i].Id] = &channels[i]
	}
	return buildRouteScopesFromAbilityAndChannels(abilities, channelByID)
}

// EnsureRouteFieldsBackfilled explicitly writes compatibility values for
// abilities that have not yet been migrated. It is intentionally not called
// implicitly by request routing; operators can run it as a controlled step.
func EnsureRouteFieldsBackfilled(tx *gorm.DB, dryRun bool) (int, error) {
	if dryRun {
		report, err := ensureRouteFieldsBackfilledReport(tx, true)
		return report.AbilitiesToBackfill, err
	}
	report, err := ApplyRouteFieldsBackfill(tx)
	return report.AbilitiesToBackfill, err
}

func saturatingLegacyVotes(weight uint) uint64 {
	value := uint64(weight)
	if value > ^uint64(0)-10 {
		return ^uint64(0)
	}
	return value + 10
}

func addSaturatingUint64(left uint64, right uint64) uint64 {
	if ^uint64(0)-left < right {
		return ^uint64(0)
	}
	return left + right
}

// PreviewRouteFieldsBackfill returns the same report as an apply operation but
// never writes. Keeping preview and apply on one planner prevents dry-run from
// describing a different scope shape than the operation it previews.
func PreviewRouteFieldsBackfill(tx *gorm.DB) (ChannelRouteScopeMigrationReport, error) {
	return ensureRouteFieldsBackfilledReport(tx, true)
}

// ApplyRouteFieldsBackfill writes one priority/weight pair to every raw
// ability belonging to a channel member of a scope.
func ApplyRouteFieldsBackfill(tx *gorm.DB) (ChannelRouteScopeMigrationReport, error) {
	if tx == nil {
		tx = DB
	}
	var report ChannelRouteScopeMigrationReport
	err := tx.Transaction(func(transaction *gorm.DB) error {
		var err error
		report, err = ensureRouteFieldsBackfilledReport(transaction, false)
		if err != nil {
			return err
		}
		if report.SkippedScopes > 0 || len(report.MappingConflicts) > 0 || len(report.Errors) > 0 {
			return ErrRouteScopeMigrationConflicts
		}
		return nil
	})
	return report, err
}

func ensureRouteFieldsBackfilledReport(tx *gorm.DB, dryRun bool) (ChannelRouteScopeMigrationReport, error) {
	ensureAbilityColumnsInitialized()
	if tx == nil {
		tx = DB
	}
	report := ChannelRouteScopeMigrationReport{DryRun: dryRun, Scopes: []ChannelRouteScopeMigrationScope{}}
	var abilities []Ability
	if err := tx.Find(&abilities).Error; err != nil {
		return report, err
	}
	channelIDs := make([]int, 0, len(abilities))
	seenChannelIDs := make(map[int]struct{})
	for _, ability := range abilities {
		if _, seen := seenChannelIDs[ability.ChannelId]; seen {
			continue
		}
		seenChannelIDs[ability.ChannelId] = struct{}{}
		channelIDs = append(channelIDs, ability.ChannelId)
	}
	var channels []Channel
	if len(channelIDs) > 0 {
		if err := tx.Where("id IN ?", channelIDs).Find(&channels).Error; err != nil {
			return report, err
		}
	}
	channelByID := make(map[int]*Channel, len(channels))
	for i := range channels {
		channelByID[channels[i].Id] = &channels[i]
	}

	grouped := make(map[routeScopeKey]map[int][]Ability)
	for _, ability := range abilities {
		channel := channelByID[ability.ChannelId]
		if channel == nil {
			report.Errors = append(report.Errors, fmt.Sprintf("channel %d is missing for ability %s/%s", ability.ChannelId, ability.Group, ability.Model))
			continue
		}
		effectiveModel, err := effectiveRouteModelForAbility(ability, channel)
		if err != nil {
			report.MappingConflicts = append(report.MappingConflicts, fmt.Sprintf("channel %d model %s mapping: %v", ability.ChannelId, ability.Model, err))
			continue
		}
		key := routeScopeKey{Group: strings.TrimSpace(ability.Group), EffectiveRouteModel: effectiveModel, RoutePriority: ability.GetEffectiveRoutePriority()}
		members := grouped[key]
		if members == nil {
			members = make(map[int][]Ability)
			grouped[key] = members
		}
		members[ability.ChannelId] = append(members[ability.ChannelId], ability)
	}

	keys := make([]routeScopeKey, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Group != keys[j].Group {
			return keys[i].Group < keys[j].Group
		}
		if keys[i].EffectiveRouteModel != keys[j].EffectiveRouteModel {
			return keys[i].EffectiveRouteModel < keys[j].EffectiveRouteModel
		}
		return keys[i].RoutePriority > keys[j].RoutePriority
	})
	report.ScopeCount = len(keys)
	report.CandidateScopes = len(keys)

	for _, key := range keys {
		members := grouped[key]
		channelIDs := make([]int, 0, len(members))
		for channelID := range members {
			channelIDs = append(channelIDs, channelID)
		}
		sort.Ints(channelIDs)
		scopeReport := ChannelRouteScopeMigrationScope{
			ID:                  EncodeRouteScopeID(key.Group, key.EffectiveRouteModel, key.RoutePriority),
			Group:               key.Group,
			EffectiveRouteModel: key.EffectiveRouteModel,
			RoutePriority:       key.RoutePriority,
			CandidateCount:      len(channelIDs),
			Items:               make([]ChannelRouteScopeMigrationItem, 0, len(channelIDs)),
		}
		numerators := make([]uint64, len(channelIDs))
		pendingScope := false
		for index, channelID := range channelIDs {
			memberAbilities := members[channelID]
			sort.Slice(memberAbilities, func(i, j int) bool { return memberAbilities[i].Model < memberAbilities[j].Model })
			item := ChannelRouteScopeMigrationItem{ChannelID: channelID, RawAbilityCount: len(memberAbilities), NewRoutePriority: key.RoutePriority}
			var explicitWeight *uint
			allExplicitWeight := true
			for _, ability := range memberAbilities {
				if item.OldRoutePriority == nil && ability.RoutePriority != nil {
					value := *ability.RoutePriority
					item.OldRoutePriority = &value
				}
				if item.OldRouteWeightBPS == nil && ability.RouteWeightBPS != nil {
					value := *ability.RouteWeightBPS
					item.OldRouteWeightBPS = &value
				}
				item.LegacyVotes = addSaturatingUint64(item.LegacyVotes, saturatingLegacyVotes(ability.Weight))
				if !ability.HasExplicitRouteWeight() {
					allExplicitWeight = false
					continue
				}
				value := *ability.RouteWeightBPS
				if explicitWeight == nil {
					explicitWeight = &value
				} else if *explicitWeight != value {
					message := fmt.Sprintf("scope %s channel %d has conflicting explicit route weights", scopeReport.ID, channelID)
					scopeReport.WeightConflicts = append(scopeReport.WeightConflicts, message)
					report.WeightConflicts = append(report.WeightConflicts, message)
				}
			}
			for _, ability := range memberAbilities {
				if !ability.HasExplicitRoutePriority() || !ability.HasExplicitRouteWeight() {
					item.NeedsBackfill = true
					pendingScope = true
					break
				}
			}
			item.PartialWeightConfig = explicitWeight != nil && !allExplicitWeight
			if item.PartialWeightConfig {
				message := fmt.Sprintf("scope %s channel %d has partial explicit route weight configuration", scopeReport.ID, channelID)
				scopeReport.WeightConflicts = append(scopeReport.WeightConflicts, message)
				report.WeightConflicts = append(report.WeightConflicts, message)
			}
			if explicitWeight != nil {
				numerators[index] = uint64(*explicitWeight)
			} else {
				numerators[index] = item.LegacyVotes
			}
			scopeReport.Items = append(scopeReport.Items, item)
		}
		if !pendingScope {
			if len(scopeReport.WeightConflicts) > 0 {
				// An already-backfilled scope can still be ambiguous when multiple
				// raw abilities of the same channel carry different explicit
				// weights. Do not report it as successfully migrated merely because
				// no fields need backfilling; an operator must resolve the conflict
				// before this scope is considered safe to apply.
				scopeReport.Skipped = true
				scopeReport.Error = "conflicting route weights require manual resolution"
				report.SkippedScopes++
				report.Scopes = append(report.Scopes, scopeReport)
				continue
			}
			var explicitTotal uint64
			for index := range scopeReport.Items {
				item := &scopeReport.Items[index]
				item.NewRouteWeightBPS = valueOrZero(item.OldRouteWeightBPS)
				explicitTotal += uint64(item.NewRouteWeightBPS)
				if item.NewRouteWeightBPS == 0 {
					report.ZeroBPSCount++
				}
			}
			if explicitTotal != uint64(RouteWeightTotalBPS) {
				message := fmt.Sprintf("scope %s has explicit route weights totaling %d bps; expected %d", scopeReport.ID, explicitTotal, RouteWeightTotalBPS)
				scopeReport.WeightConflicts = append(scopeReport.WeightConflicts, message)
				report.WeightConflicts = append(report.WeightConflicts, message)
				scopeReport.Skipped = true
				scopeReport.Error = "explicit route weights must sum to 10000 bps"
				report.SkippedScopes++
				report.Scopes = append(report.Scopes, scopeReport)
				continue
			}
			report.Scopes = append(report.Scopes, scopeReport)
			continue
		}
		bps := NormalizeRouteWeights(numerators)
		for index := range scopeReport.Items {
			scopeReport.Items[index].NewRouteWeightBPS = bps[index]
			if bps[index] == 0 {
				report.ZeroBPSCount++
			}
			if scopeReport.Items[index].NeedsBackfill {
				report.ChannelsToBackfill++
			}
			for _, ability := range members[scopeReport.Items[index].ChannelID] {
				if !ability.HasExplicitRoutePriority() || !ability.HasExplicitRouteWeight() {
					report.AbilitiesToBackfill++
				}
			}
		}
		if len(scopeReport.WeightConflicts) > 0 {
			scopeReport.Skipped = true
			scopeReport.Error = "conflicting route weights require manual resolution"
			report.SkippedScopes++
			report.Scopes = append(report.Scopes, scopeReport)
			continue
		}
		if !dryRun {
			for index, channelID := range channelIDs {
				for _, ability := range members[channelID] {
					if err := updateRouteScopeAbility(tx, ability, key.RoutePriority, scopeReport.Items[index].NewRouteWeightBPS); err != nil {
						report.Errors = append(report.Errors, err.Error())
						return report, err
					}
				}
			}
		}
		report.Scopes = append(report.Scopes, scopeReport)
	}
	return report, nil
}
