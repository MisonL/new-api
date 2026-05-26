package model

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"

	"github.com/samber/lo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errNoMatchingAbilities = errors.New("no matching abilities found")

type Ability struct {
	Group     string  `json:"group" gorm:"type:varchar(64);primaryKey;autoIncrement:false"`
	Model     string  `json:"model" gorm:"type:varchar(255);primaryKey;autoIncrement:false"`
	ChannelId int     `json:"channel_id" gorm:"primaryKey;autoIncrement:false;index"`
	Enabled   bool    `json:"enabled"`
	Priority  *int64  `json:"priority" gorm:"bigint;default:0;index"`
	Weight    uint    `json:"weight" gorm:"default:0;index"`
	Tag       *string `json:"tag" gorm:"index"`
}

type AbilityWithChannel struct {
	Ability
	ChannelType int `json:"channel_type"`
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
	err := DB.Model(&Ability{}).
		Select("DISTINCT(priority)").
		Where(commonGroupCol+" = ? and model = ? and enabled = ?", group, model, true).
		Order("priority DESC").              // 按优先级降序排序
		Pluck("priority", &priorities).Error // Pluck用于将查询的结果直接扫描到一个切片中

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
	maxPrioritySubQuery := DB.Model(&Ability{}).Select("MAX(priority)").Where(commonGroupCol+" = ? and model = ? and enabled = ?", group, model, true)
	channelQuery := DB.Where(commonGroupCol+" = ? and model = ? and enabled = ? and priority = (?)", group, model, true, maxPrioritySubQuery)
	if retry != 0 {
		priority, err := getPriority(group, model, retry)
		if err != nil {
			return nil, err
		} else {
			channelQuery = DB.Where(commonGroupCol+" = ? and model = ? and enabled = ? and priority = ?", group, model, true, priority)
		}
	}

	return channelQuery, nil
}

func GetChannel(group string, model string, retry int) (*Channel, error) {
	return getChannelExcluding(group, model, retry, nil)
}

func getChannelExcluding(group string, model string, retry int, excluded map[int]struct{}) (*Channel, error) {
	routeCandidates := getGroupModelRouteCandidateMeta(model)
	if shouldPoolCompactRouteCandidates(routeCandidates) {
		channel, found, err := getChannelByRouteModels(group, routeCandidates, retry, excluded)
		if err != nil {
			return nil, err
		}
		if found {
			return channel, nil
		}
		return nil, nil
	}
	for _, routeCandidate := range routeCandidates {
		channel, found, err := getChannelByRouteModel(group, routeCandidate, retry, excluded)
		if err != nil {
			return nil, err
		}
		if found {
			return channel, nil
		}
	}
	return nil, nil
}

func getRouteCandidateAbilityQuery(group string, routeCandidate routeModelCandidate, retry int) (*gorm.DB, error) {
	if routeCandidate.compactRequest {
		return DB.Where(commonGroupCol+" = ? and model = ? and enabled = ?", group, routeCandidate.model, true), nil
	}
	return getChannelQuery(group, routeCandidate.model, retry)
}

func getChannelByRouteModel(group string, routeCandidate routeModelCandidate, retry int, excluded map[int]struct{}) (*Channel, bool, error) {
	channelQuery, err := getRouteCandidateAbilityQuery(group, routeCandidate, retry)
	if err != nil {
		if errors.Is(err, errNoMatchingAbilities) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var abilities []Ability
	if common.UsingSQLite || common.UsingPostgreSQL {
		err = channelQuery.Order("priority DESC, weight DESC").Find(&abilities).Error
	} else {
		err = channelQuery.Order("priority DESC, weight DESC").Find(&abilities).Error
	}
	if err != nil {
		return nil, false, err
	}
	if len(abilities) == 0 {
		return nil, false, nil
	}

	channels, channelWeights, err := loadRouteCandidateChannels(abilities, routeCandidate, false)
	if err != nil {
		return nil, false, err
	}
	if len(channels) == 0 {
		return nil, false, nil
	}
	channels, channelWeights = filterChannelsByRetryPriority(channels, channelWeights, retry, excluded)
	if len(channels) == 0 {
		return nil, true, nil
	}
	return chooseWeightedChannel(channels, channelWeights), true, nil
}

func getChannelByRouteModels(group string, routeCandidates []routeModelCandidate, retry int, excluded map[int]struct{}) (*Channel, bool, error) {
	channels := make([]*Channel, 0, len(routeCandidates))
	channelWeights := make([]uint, 0, len(routeCandidates))
	seen := make(map[int]struct{})
	for _, routeCandidate := range routeCandidates {
		channelQuery, err := getRouteCandidateAbilityQuery(group, routeCandidate, retry)
		if err != nil {
			if errors.Is(err, errNoMatchingAbilities) {
				continue
			}
			return nil, false, err
		}
		var abilities []Ability
		if err := channelQuery.Order("priority DESC, weight DESC").Find(&abilities).Error; err != nil {
			return nil, false, err
		}
		candidateChannels, candidateWeights, err := loadRouteCandidateChannels(abilities, routeCandidate, true)
		if err != nil {
			return nil, false, err
		}
		for i, channel := range candidateChannels {
			if _, ok := seen[channel.Id]; ok {
				continue
			}
			seen[channel.Id] = struct{}{}
			channels = append(channels, channel)
			channelWeights = append(channelWeights, candidateWeights[i])
		}
	}
	if len(channels) == 0 {
		return nil, false, nil
	}
	channels, channelWeights = filterChannelsByRetryPriority(channels, channelWeights, retry, excluded)
	if len(channels) == 0 {
		return nil, true, nil
	}
	return chooseWeightedChannel(channels, channelWeights), true, nil
}

func loadRouteCandidateChannels(abilities []Ability, routeCandidate routeModelCandidate, useChannelWeights bool) ([]*Channel, []uint, error) {
	channels := make([]*Channel, 0, len(abilities))
	weights := make([]uint, 0, len(abilities))
	for _, ability := range abilities {
		channel := Channel{}
		if err := DB.First(&channel, "id = ?", ability.ChannelId).Error; err != nil {
			return nil, nil, err
		}
		if !channelSupportsCompactRouteCandidate(&channel, routeCandidate) {
			continue
		}
		channels = append(channels, &channel)
		weight := ability.Weight
		if useChannelWeights {
			weight = uint(channel.GetWeight())
		}
		weights = append(weights, weight)
	}
	return channels, weights, nil
}

func filterChannelsByRetryPriority(channels []*Channel, weights []uint, retry int, excluded map[int]struct{}) ([]*Channel, []uint) {
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
		if _, skip := excluded[channel.Id]; skip {
			continue
		}
		filteredChannels = append(filteredChannels, channel)
		filteredWeights = append(filteredWeights, weights[i])
	}
	return filteredChannels, filteredWeights
}

func chooseWeightedChannel(channels []*Channel, weights []uint) *Channel {
	weightSum := uint(0)
	for _, weight := range weights {
		weightSum += weight + 10
	}
	weight := common.GetRandomInt(int(weightSum))
	for i, channel := range channels {
		weight -= int(weights[i]) + 10
		if weight <= 0 {
			return channel
		}
	}
	return channels[len(channels)-1]
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

	// First delete all abilities of this channel
	err := tx.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error
	if err != nil {
		if isNewTx {
			tx.Rollback()
		}
		return err
	}

	// Then add new abilities
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
	return DB.Model(&Ability{}).Where("channel_id = ?", channelId).Select("enabled").Update("enabled", status).Error
}

func UpdateAbilityStatusByTag(tag string, status bool) error {
	return DB.Model(&Ability{}).Where("tag = ?", tag).Select("enabled").Update("enabled", status).Error
}

func UpdateAbilityByTag(tag string, newTag *string, priority *int64, weight *uint) error {
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
	return DB.Model(&Ability{}).Where("tag = ?", tag).Updates(ability).Error
}

var fixLock = sync.Mutex{}

func FixAbility() (int, int, error) {
	lock := fixLock.TryLock()
	if !lock {
		return 0, 0, errors.New("已经有一个修复任务在运行中，请稍后再试")
	}
	defer fixLock.Unlock()

	// truncate abilities table
	if common.UsingSQLite {
		err := DB.Exec("DELETE FROM abilities").Error
		if err != nil {
			common.SysLog(fmt.Sprintf("Delete abilities failed: %s", err.Error()))
			return 0, 0, err
		}
	} else {
		err := DB.Exec("TRUNCATE TABLE abilities").Error
		if err != nil {
			common.SysLog(fmt.Sprintf("Truncate abilities failed: %s", err.Error()))
			return 0, 0, err
		}
	}
	var channels []*Channel
	// Find all channels
	err := DB.Model(&Channel{}).Find(&channels).Error
	if err != nil {
		return 0, 0, err
	}
	if len(channels) == 0 {
		return 0, 0, nil
	}
	successCount := 0
	failCount := 0
	for _, chunk := range lo.Chunk(channels, 50) {
		ids := lo.Map(chunk, func(c *Channel, _ int) int { return c.Id })
		// Delete all abilities of this channel
		err = DB.Where("channel_id IN ?", ids).Delete(&Ability{}).Error
		if err != nil {
			common.SysLog(fmt.Sprintf("Delete abilities failed: %s", err.Error()))
			failCount += len(chunk)
			continue
		}
		// Then add new abilities
		for _, channel := range chunk {
			err = channel.AddAbilities(nil)
			if err != nil {
				common.SysLog(fmt.Sprintf("Add abilities for channel %d failed: %s", channel.Id, err.Error()))
				failCount++
			} else {
				successCount++
			}
		}
	}
	InitChannelCache()
	return successCount, failCount, nil
}
