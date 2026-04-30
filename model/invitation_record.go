package model

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	InvitationRecordTypeRegister    = "register"
	InvitationRecordTypeTopUpRebate = "topup_rebate"
)

type InvitationRecord struct {
	Id              int     `json:"id"`
	InviterId       int     `json:"inviter_id" gorm:"index:idx_invitation_records_inviter_created,priority:1;index"`
	InviteeId       int     `json:"invitee_id" gorm:"index"`
	InviteeUsername string  `json:"invitee_username" gorm:"-:all"`
	RecordType      string  `json:"record_type" gorm:"type:varchar(32);index"`
	Source          string  `json:"source" gorm:"type:varchar(32);index"`
	SourceRef       string  `json:"source_ref" gorm:"type:varchar(255);index"`
	TopUpQuota      int     `json:"topup_quota" gorm:"default:0"`
	RewardQuota     int     `json:"reward_quota" gorm:"default:0"`
	RebateRate      float64 `json:"rebate_rate" gorm:"type:decimal(10,4);default:0"`
	CreatedTime     int64   `json:"created_time" gorm:"type:bigint;index:idx_invitation_records_inviter_created,priority:2"`
}

type InvitationRewardMeta struct {
	Source    string
	SourceRef string
}

type InvitationRecordFilter struct {
	RecordType string
	Source     string
	Keyword    string
}

type InvitationInviteeStat struct {
	InviteeId       int    `json:"invitee_id"`
	InviteeUsername string `json:"invitee_username"`
	RewardQuota     int    `json:"reward_quota"`
	TopUpQuota      int    `json:"topup_quota"`
	RegisterCount   int    `json:"register_count"`
	RebateCount     int    `json:"rebate_count"`
	LatestTime      int64  `json:"latest_time"`
}

func RecordInvitationRegisterReward(inviterId int, inviteeId int, rewardQuota int) {
	if inviterId <= 0 || inviteeId <= 0 {
		return
	}
	record := &InvitationRecord{
		InviterId:   inviterId,
		InviteeId:   inviteeId,
		RecordType:  InvitationRecordTypeRegister,
		Source:      "register",
		RewardQuota: rewardQuota,
		CreatedTime: common.GetTimestamp(),
	}
	if err := DB.Create(record).Error; err != nil {
		common.SysLog("failed to record invitation register reward: " + err.Error())
	}
}

func createInvitationTopUpRecord(tx *gorm.DB, inviterId int, inviteeId int, topUpQuota int, rewardQuota int, rebateRate float64, meta InvitationRewardMeta) error {
	if inviterId <= 0 || inviteeId <= 0 || rewardQuota <= 0 {
		return nil
	}
	record := &InvitationRecord{
		InviterId:   inviterId,
		InviteeId:   inviteeId,
		RecordType:  InvitationRecordTypeTopUpRebate,
		Source:      normalizeInvitationSource(meta.Source),
		SourceRef:   strings.TrimSpace(meta.SourceRef),
		TopUpQuota:  topUpQuota,
		RewardQuota: rewardQuota,
		RebateRate:  rebateRate,
		CreatedTime: common.GetTimestamp(),
	}
	return tx.Create(record).Error
}

func normalizeInvitationSource(source string) string {
	source = strings.TrimSpace(strings.ToLower(source))
	if source == "" {
		return "topup"
	}
	if len(source) > 32 {
		return source[:32]
	}
	return source
}

func validateInvitationRecordFilter(filter InvitationRecordFilter) (InvitationRecordFilter, error) {
	filter.RecordType = strings.TrimSpace(filter.RecordType)
	filter.Source = strings.TrimSpace(strings.ToLower(filter.Source))
	filter.Keyword = strings.TrimSpace(filter.Keyword)
	switch filter.RecordType {
	case "", InvitationRecordTypeRegister, InvitationRecordTypeTopUpRebate:
	default:
		return filter, errors.New("邀请记录类型无效")
	}
	if len(filter.Source) > 32 {
		return filter, errors.New("邀请记录来源过长")
	}
	if len(filter.Keyword) > 128 {
		return filter, errors.New("搜索关键词过长")
	}
	return filter, nil
}

func ListInvitationRecords(inviterId int, filter InvitationRecordFilter, startIdx int, num int) ([]*InvitationRecord, int64, error) {
	if inviterId <= 0 {
		return nil, 0, errors.New("无效的邀请用户")
	}
	filter, err := validateInvitationRecordFilter(filter)
	if err != nil {
		return nil, 0, err
	}
	if filter.RecordType == InvitationRecordTypeTopUpRebate || (filter.Source != "" && filter.Source != "register") {
		return listStoredInvitationRecords(inviterId, filter, startIdx, num)
	}

	storedRecords, err := queryStoredInvitationRecords(inviterId, filter)
	if err != nil {
		return nil, 0, err
	}
	recordsByInvitee := make(map[int][]*InvitationRecord)
	storedRegisterInvitees := make(map[int]bool)
	for _, record := range storedRecords {
		recordsByInvitee[record.InviteeId] = append(recordsByInvitee[record.InviteeId], record)
		if record.RecordType == InvitationRecordTypeRegister {
			storedRegisterInvitees[record.InviteeId] = true
		}
	}

	invitedUsers, err := queryInvitedUsers(inviterId, filter.Keyword)
	if err != nil {
		return nil, 0, err
	}
	records := make([]*InvitationRecord, 0, len(storedRecords)+len(invitedUsers))
	for _, user := range invitedUsers {
		if user.Id <= 0 {
			continue
		}
		if filter.RecordType == "" {
			records = append(records, recordsByInvitee[user.Id]...)
		}
		if storedRegisterInvitees[user.Id] {
			continue
		}
		records = append(records, &InvitationRecord{
			Id:              -user.Id,
			InviterId:       inviterId,
			InviteeId:       user.Id,
			InviteeUsername: user.Username,
			RecordType:      InvitationRecordTypeRegister,
			Source:          "register",
			RewardQuota:     common.QuotaForInviter,
		})
	}
	sortInvitationRecords(records)
	total := int64(len(records))
	if startIdx >= len(records) {
		return []*InvitationRecord{}, total, nil
	}
	endIdx := startIdx + num
	if endIdx > len(records) {
		endIdx = len(records)
	}
	return records[startIdx:endIdx], total, nil
}

func ListInvitationInviteeStats(inviterId int, filter InvitationRecordFilter, limit int) ([]InvitationInviteeStat, error) {
	if inviterId <= 0 {
		return nil, errors.New("无效的邀请用户")
	}
	filter, err := validateInvitationRecordFilter(filter)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 20 {
		limit = 8
	}

	statsByInvitee, err := queryStoredInvitationInviteeStats(inviterId, filter)
	if err != nil {
		return nil, err
	}
	if filter.RecordType != InvitationRecordTypeTopUpRebate && (filter.Source == "" || filter.Source == "register") {
		storedRegisterInvitees, err := queryStoredRegisterInviteeIds(inviterId, filter.Keyword)
		if err != nil {
			return nil, err
		}
		invitedUsers, err := queryInvitedUsers(inviterId, filter.Keyword)
		if err != nil {
			return nil, err
		}
		for _, user := range invitedUsers {
			if user.Id <= 0 || storedRegisterInvitees[user.Id] {
				continue
			}
			record := &InvitationRecord{
				InviteeId:       user.Id,
				InviteeUsername: user.Username,
				RecordType:      InvitationRecordTypeRegister,
				Source:          "register",
				RewardQuota:     common.QuotaForInviter,
			}
			applyInvitationRecordToStat(getInvitationInviteeStat(statsByInvitee, user.Id, user.Username), record)
		}
	}

	stats := make([]InvitationInviteeStat, 0, len(statsByInvitee))
	for _, stat := range statsByInvitee {
		stats = append(stats, *stat)
	}
	sort.SliceStable(stats, func(i, j int) bool {
		if stats[i].RewardQuota != stats[j].RewardQuota {
			return stats[i].RewardQuota > stats[j].RewardQuota
		}
		if stats[i].LatestTime != stats[j].LatestTime {
			return stats[i].LatestTime > stats[j].LatestTime
		}
		return stats[i].InviteeId > stats[j].InviteeId
	})
	if len(stats) > limit {
		stats = stats[:limit]
	}
	return stats, nil
}

type storedInvitationInviteeStatRow struct {
	InviteeId     int
	RewardQuota   int
	TopUpQuota    int
	RegisterCount int
	RebateCount   int
	LatestTime    int64
}

func queryStoredInvitationInviteeStats(inviterId int, filter InvitationRecordFilter) (map[int]*InvitationInviteeStat, error) {
	tx := DB.Model(&InvitationRecord{}).Where("inviter_id = ?", inviterId)
	if filter.RecordType != "" {
		tx = tx.Where("record_type = ?", filter.RecordType)
	}
	if filter.Source != "" {
		tx = tx.Where("source = ?", filter.Source)
	}
	if filter.Keyword != "" {
		inviteeIds, err := findInvitedUserIds(inviterId, filter.Keyword)
		if err != nil {
			return nil, err
		}
		if len(inviteeIds) == 0 {
			return map[int]*InvitationInviteeStat{}, nil
		}
		tx = tx.Where("invitee_id IN ?", inviteeIds)
	}

	var rows []storedInvitationInviteeStatRow
	err := tx.Select(
		`invitee_id,
		SUM(reward_quota) AS reward_quota,
		SUM(top_up_quota) AS top_up_quota,
		SUM(CASE WHEN record_type = ? THEN 1 ELSE 0 END) AS register_count,
		SUM(CASE WHEN record_type = ? THEN 1 ELSE 0 END) AS rebate_count,
		MAX(created_time) AS latest_time`,
		InvitationRecordTypeRegister,
		InvitationRecordTypeTopUpRebate,
	).Group("invitee_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	stats := make(map[int]*InvitationInviteeStat, len(rows))
	for _, row := range rows {
		if row.InviteeId <= 0 {
			continue
		}
		stats[row.InviteeId] = &InvitationInviteeStat{
			InviteeId:     row.InviteeId,
			RewardQuota:   row.RewardQuota,
			TopUpQuota:    row.TopUpQuota,
			RegisterCount: row.RegisterCount,
			RebateCount:   row.RebateCount,
			LatestTime:    row.LatestTime,
		}
	}
	fillInvitationInviteeStatUsernames(stats)
	return stats, nil
}

func queryStoredRegisterInviteeIds(inviterId int, keyword string) (map[int]bool, error) {
	tx := DB.Model(&InvitationRecord{}).Where("inviter_id = ? AND record_type = ?", inviterId, InvitationRecordTypeRegister)
	if keyword != "" {
		inviteeIds, err := findInvitedUserIds(inviterId, keyword)
		if err != nil {
			return nil, err
		}
		if len(inviteeIds) == 0 {
			return map[int]bool{}, nil
		}
		tx = tx.Where("invitee_id IN ?", inviteeIds)
	}
	var ids []int
	if err := tx.Pluck("invitee_id", &ids).Error; err != nil {
		return nil, err
	}
	seen := make(map[int]bool, len(ids))
	for _, id := range ids {
		if id > 0 {
			seen[id] = true
		}
	}
	return seen, nil
}

func listStoredInvitationRecords(inviterId int, filter InvitationRecordFilter, startIdx int, num int) ([]*InvitationRecord, int64, error) {
	tx := DB.Model(&InvitationRecord{}).Where("inviter_id = ?", inviterId)
	if filter.RecordType != "" {
		tx = tx.Where("record_type = ?", filter.RecordType)
	}
	if filter.Source != "" {
		tx = tx.Where("source = ?", filter.Source)
	}
	if filter.Keyword != "" {
		inviteeIds, err := findInvitedUserIds(inviterId, filter.Keyword)
		if err != nil {
			return nil, 0, err
		}
		if len(inviteeIds) == 0 {
			return []*InvitationRecord{}, 0, nil
		}
		tx = tx.Where("invitee_id IN ?", inviteeIds)
	}

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var records []*InvitationRecord
	if err := tx.Order("created_time desc, id desc").Limit(num).Offset(startIdx).Find(&records).Error; err != nil {
		return nil, 0, err
	}
	fillInvitationRecordUsernames(records)
	return records, total, nil
}

func queryStoredInvitationRecords(inviterId int, filter InvitationRecordFilter) ([]*InvitationRecord, error) {
	tx := DB.Model(&InvitationRecord{}).Where("inviter_id = ?", inviterId)
	if filter.RecordType != "" {
		tx = tx.Where("record_type = ?", filter.RecordType)
	}
	if filter.Source != "" {
		tx = tx.Where("source = ?", filter.Source)
	}
	if filter.Keyword != "" {
		inviteeIds, err := findInvitedUserIds(inviterId, filter.Keyword)
		if err != nil {
			return nil, err
		}
		if len(inviteeIds) == 0 {
			return []*InvitationRecord{}, nil
		}
		tx = tx.Where("invitee_id IN ?", inviteeIds)
	}
	var records []*InvitationRecord
	if err := tx.Order("created_time desc, id desc").Find(&records).Error; err != nil {
		return nil, err
	}
	fillInvitationRecordUsernames(records)
	return records, nil
}

func queryInvitedUsers(inviterId int, keyword string) ([]User, error) {
	tx := DB.Model(&User{}).Where("inviter_id = ?", inviterId)
	if keyword != "" {
		inviteeIds, err := findInvitedUserIds(inviterId, keyword)
		if err != nil {
			return nil, err
		}
		if len(inviteeIds) == 0 {
			return []User{}, nil
		}
		tx = tx.Where("id IN ?", inviteeIds)
	}
	var users []User
	err := tx.Select("id", "username").Order("id desc").Find(&users).Error
	return users, err
}

func findInvitedUserIds(inviterId int, keyword string) ([]int, error) {
	userIds := make([]int, 0)
	if id, err := strconv.Atoi(keyword); err == nil && id > 0 {
		userIds = append(userIds, id)
	}
	pattern := containsLikePattern(keyword)
	query := DB.Model(&User{}).Where("inviter_id = ?", inviterId).
		Where("username LIKE ? ESCAPE '!' OR email LIKE ? ESCAPE '!'", pattern, pattern)
	if err := query.Pluck("id", &userIds).Error; err != nil {
		return nil, err
	}
	return uniquePositiveInts(userIds), nil
}

func sortInvitationRecords(records []*InvitationRecord) {
	sort.SliceStable(records, func(i, j int) bool {
		left := records[i]
		right := records[j]
		if left.CreatedTime != right.CreatedTime {
			return left.CreatedTime > right.CreatedTime
		}
		return left.Id > right.Id
	})
}

func containsLikePattern(input string) string {
	input = strings.ReplaceAll(input, "!", "!!")
	input = strings.ReplaceAll(input, "%", "!%")
	input = strings.ReplaceAll(input, `_`, `!_`)
	return "%" + input + "%"
}

func fillInvitationRecordUsernames(records []*InvitationRecord) {
	if len(records) == 0 {
		return
	}
	userIds := make([]int, 0, len(records))
	seen := map[int]bool{}
	for _, record := range records {
		if record.InviteeId > 0 && !seen[record.InviteeId] {
			userIds = append(userIds, record.InviteeId)
			seen[record.InviteeId] = true
		}
	}
	var users []User
	if err := DB.Select("id", "username").Where("id IN ?", userIds).Find(&users).Error; err != nil {
		return
	}
	names := map[int]string{}
	for _, user := range users {
		names[user.Id] = user.Username
	}
	for _, record := range records {
		record.InviteeUsername = names[record.InviteeId]
	}
}

func getInvitationInviteeStat(stats map[int]*InvitationInviteeStat, inviteeId int, username string) *InvitationInviteeStat {
	stat := stats[inviteeId]
	if stat == nil {
		stat = &InvitationInviteeStat{InviteeId: inviteeId}
		stats[inviteeId] = stat
	}
	if stat.InviteeUsername == "" && username != "" {
		stat.InviteeUsername = username
	}
	return stat
}

func applyInvitationRecordToStat(stat *InvitationInviteeStat, record *InvitationRecord) {
	stat.RewardQuota += record.RewardQuota
	stat.TopUpQuota += record.TopUpQuota
	if record.CreatedTime > stat.LatestTime {
		stat.LatestTime = record.CreatedTime
	}
	switch record.RecordType {
	case InvitationRecordTypeRegister:
		stat.RegisterCount++
	case InvitationRecordTypeTopUpRebate:
		stat.RebateCount++
	}
}

func fillInvitationInviteeStatUsernames(stats map[int]*InvitationInviteeStat) {
	if len(stats) == 0 {
		return
	}
	userIds := make([]int, 0, len(stats))
	for id := range stats {
		if id > 0 {
			userIds = append(userIds, id)
		}
	}
	var users []User
	if err := DB.Select("id", "username").Where("id IN ?", userIds).Find(&users).Error; err != nil {
		return
	}
	for _, user := range users {
		if stat := stats[user.Id]; stat != nil {
			stat.InviteeUsername = user.Username
		}
	}
}

func uniquePositiveInts(values []int) []int {
	seen := make(map[int]bool, len(values))
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
