package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Midjourney struct {
	Id          int    `json:"id" gorm:"index:idx_midjourneys_progress_id,priority:2"`
	Code        int    `json:"code"`
	UserId      int    `json:"user_id" gorm:"index"`
	Action      string `json:"action" gorm:"type:varchar(40);index"`
	MjId        string `json:"mj_id" gorm:"index"`
	Prompt      string `json:"prompt"`
	PromptEn    string `json:"prompt_en"`
	Description string `json:"description"`
	State       string `json:"state"`
	SubmitTime  int64  `json:"submit_time" gorm:"index"`
	StartTime   int64  `json:"start_time" gorm:"index"`
	FinishTime  int64  `json:"finish_time" gorm:"index"`
	ImageUrl    string `json:"image_url"`
	VideoUrl    string `json:"video_url"`
	VideoUrls   string `json:"video_urls"`
	Status      string `json:"status" gorm:"type:varchar(20);index"`
	Progress    string `json:"progress" gorm:"type:varchar(30);index;index:idx_midjourneys_progress_id,priority:1"`
	FailReason  string `json:"fail_reason"`
	ChannelId   int    `json:"channel_id" gorm:"index"`
	Quota       int    `json:"quota"`
	// PrivateData is hidden from API responses and stores the concrete
	// credential selected for this asynchronous task. Without it, a multi-key
	// channel can poll a task with a different account than the submitter.
	PrivateData TaskPrivateData `json:"-" gorm:"column:private_data;type:json"`
	Buttons     string          `json:"buttons"`
	Properties  string          `json:"properties"`
}

// TaskQueryParams 用于包含所有搜索条件的结构体，可以根据需求添加更多字段
type TaskQueryParams struct {
	ChannelID      string
	MjID           string
	StartTimestamp string
	EndTimestamp   string
}

func GetAllUserTask(userId int, startIdx int, num int, queryParams TaskQueryParams) []*Midjourney {
	tasks, _ := GetAllUserTaskWithError(userId, startIdx, num, queryParams)
	return tasks
}

// GetAllUserTaskWithError returns user-scoped Midjourney tasks together with
// any database or private-data decoding error. A malformed route snapshot
// must be reported to the API caller instead of becoming an empty page.
func GetAllUserTaskWithError(userId int, startIdx int, num int, queryParams TaskQueryParams) ([]*Midjourney, error) {
	var tasks []*Midjourney

	// 初始化查询构建器
	query := DB.Where("user_id = ?", userId)

	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		// 假设您已将前端传来的时间戳转换为数据库所需的时间格式，并处理了时间戳的验证和解析
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// 获取数据
	err := query.Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil, err
	}

	return tasks, nil
}

func GetAllTasks(startIdx int, num int, queryParams TaskQueryParams) []*Midjourney {
	tasks, _ := GetAllTasksWithError(startIdx, num, queryParams)
	return tasks
}

// GetAllTasksWithError returns administrator-visible Midjourney tasks
// together with any database or private-data decoding error.
func GetAllTasksWithError(startIdx int, num int, queryParams TaskQueryParams) ([]*Midjourney, error) {
	var tasks []*Midjourney

	// 初始化查询构建器
	query := DB

	// 添加过滤条件
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// 获取数据
	err := query.Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil, err
	}

	return tasks, nil
}

func GetAllUnFinishTasks(limit int) []*Midjourney {
	tasks, _ := GetAllUnFinishTasksWithError(limit)
	return tasks
}

// GetAllUnFinishTasksWithError returns non-terminal Midjourney tasks together
// with any database or private-data decoding error. Polling must surface the
// error so a malformed task row is not interpreted as an empty queue.
func GetAllUnFinishTasksWithError(limit int) ([]*Midjourney, error) {
	if limit <= 0 {
		limit = 100
	}
	// Status is the lifecycle source of truth. A malformed or legacy row may
	// already report 100% progress while still having no terminal status; such
	// a row must remain visible to the poller instead of becoming permanently
	// invisible.
	query := backgroundDB().Model(&Midjourney{}).Where("COALESCE(status, '') NOT IN ?", []string{"FAILURE", "SUCCESS"}).
		Order("id")
	return loadMidjourneyWithPrivateDataIsolation(query, limit)
}

// GetTimedOutUnfinishedMidjourneyTasks returns Midjourney tasks whose persisted submit
// time is older than cutoffUnixMillis and that have not reached a terminal
// status. Rows without a submit time are intentionally excluded because their
// age cannot be established safely; they are handled by the normal polling
// path and local-failure guards instead.
func GetTimedOutUnfinishedMidjourneyTasks(cutoffUnixMillis int64, limit int) []*Midjourney {
	tasks, _ := GetTimedOutUnfinishedMidjourneyTasksWithError(cutoffUnixMillis, limit)
	return tasks
}

// GetTimedOutUnfinishedMidjourneyTasksWithError returns timed-out non-terminal
// Midjourney tasks together with any database or private-data decoding error.
// The error is part of the contract for background pollers and must not be
// collapsed into an empty result.
func GetTimedOutUnfinishedMidjourneyTasksWithError(cutoffUnixMillis int64, limit int) ([]*Midjourney, error) {
	if limit <= 0 {
		limit = 100
	}
	query := backgroundDB().Model(&Midjourney{}).
		Where("COALESCE(status, '') NOT IN ?", []string{"FAILURE", "SUCCESS"}).
		Where("submit_time > 0 AND submit_time < ?", cutoffUnixMillis).
		Order("submit_time")
	return loadMidjourneyWithPrivateDataIsolation(query, limit)
}

// loadMidjourneyWithPrivateDataIsolation reads private_data as raw bytes in a
// small page, loads the remaining columns without the custom scanner, and
// decodes each payload independently so one corrupt value does not prevent
// healthy tasks from reaching the background poller.
func loadMidjourneyWithPrivateDataIsolation(query *gorm.DB, limit int) ([]*Midjourney, error) {
	if query == nil {
		return nil, errors.New("midjourney query is nil")
	}
	if limit <= 0 {
		limit = 100
	}
	tasks := make([]*Midjourney, 0, limit)
	var scanErrors []error
	offset := 0
	for len(tasks) < limit {
		batchLimit := limit - len(tasks)
		var privateRows []midjourneyPrivateDataRow
		if err := query.Session(&gorm.Session{}).Select("id", "private_data").Offset(offset).Limit(batchLimit).Find(&privateRows).Error; err != nil {
			return tasks, errors.Join(append(scanErrors, err)...)
		}
		if len(privateRows) == 0 {
			break
		}
		offset += len(privateRows)
		var pageTasks []*Midjourney
		if err := query.Session(&gorm.Session{}).Omit("PrivateData").Offset(offset - len(privateRows)).Limit(batchLimit).Find(&pageTasks).Error; err != nil {
			return tasks, errors.Join(append(scanErrors, err)...)
		}
		privateByID := make(map[int][]byte, len(privateRows))
		for _, row := range privateRows {
			privateByID[row.ID] = row.PrivateData
		}
		for _, task := range pageTasks {
			raw, ok := privateByID[task.Id]
			if !ok {
				scanErrors = append(scanErrors, fmt.Errorf("scan Midjourney task %d: private data row disappeared during query", task.Id))
				continue
			}
			var privateData TaskPrivateData
			if err := privateData.Scan(raw); err != nil {
				scanErrors = append(scanErrors, fmt.Errorf("scan Midjourney task %d: %w", task.Id, err))
				continue
			}
			task.PrivateData = privateData
			tasks = append(tasks, task)
			if len(tasks) == limit {
				break
			}
		}
	}
	return tasks, errors.Join(scanErrors...)
}

type midjourneyPrivateDataRow struct {
	ID          int    `gorm:"column:id"`
	PrivateData []byte `gorm:"column:private_data"`
}

func GetByOnlyMJId(mjId string) *Midjourney {
	var mj *Midjourney
	var err error
	err = DB.Where("mj_id = ?", mjId).First(&mj).Error
	if err != nil {
		return nil
	}
	return mj
}

func GetByMJId(userId int, mjId string) *Midjourney {
	var mj *Midjourney
	var err error
	err = DB.Where("user_id = ? and mj_id = ?", userId, mjId).First(&mj).Error
	if err != nil {
		return nil
	}
	return mj
}

func GetByMJIds(userId int, mjIds []string) []*Midjourney {
	var mj []*Midjourney
	var err error
	err = DB.Where("user_id = ? and mj_id in (?)", userId, mjIds).Find(&mj).Error
	if err != nil {
		return nil
	}
	return mj
}

func GetMjByuId(id int) *Midjourney {
	var mj *Midjourney
	var err error
	err = DB.Where("id = ?", id).First(&mj).Error
	if err != nil {
		return nil
	}
	return mj
}

func UpdateProgress(id int, progress string) error {
	if id <= 0 {
		return errors.New("midjourney task id is invalid")
	}
	var task Midjourney
	if err := DB.Where("id = ?", id).First(&task).Error; err != nil {
		return fmt.Errorf("load Midjourney task %d before progress update: %w", id, err)
	}
	if err := validateTaskRouteSnapshotForWrite(task.PrivateData.RouteSnapshot, task.ChannelId); err != nil {
		return fmt.Errorf("validate Midjourney task %d before progress update: %w", id, err)
	}
	return DB.Model(&Midjourney{}).Where("id = ?", id).Update("progress", progress).Error
}

func (midjourney *Midjourney) Insert() error {
	if midjourney == nil {
		return errors.New("midjourney is nil")
	}
	if err := validateTaskRouteSnapshotForWrite(midjourney.PrivateData.RouteSnapshot, midjourney.ChannelId); err != nil {
		return err
	}
	var err error
	err = DB.Create(midjourney).Error
	return err
}

func (midjourney *Midjourney) Update() error {
	if midjourney == nil {
		return errors.New("midjourney is nil")
	}
	if err := validateTaskRouteSnapshotForWrite(midjourney.PrivateData.RouteSnapshot, midjourney.ChannelId); err != nil {
		return err
	}
	return DB.Save(midjourney).Error
}

// UpdateWithStatus performs a conditional UPDATE guarded by fromStatus (CAS).
// Returns (true, nil) if this caller won the update, (false, nil) if
// another process already moved the task out of fromStatus.
// Uses Model().Select("*").Updates() to avoid GORM Save()'s INSERT fallback.
func (midjourney *Midjourney) UpdateWithStatus(fromStatus string) (bool, error) {
	if midjourney == nil {
		return false, errors.New("midjourney is nil")
	}
	if err := validateTaskRouteSnapshotForWrite(midjourney.PrivateData.RouteSnapshot, midjourney.ChannelId); err != nil {
		return false, err
	}
	result := DB.Model(midjourney).Where("status = ?", fromStatus).Select("*").Updates(midjourney)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

var allowedMidjourneyBulkUpdateFields = map[string]struct{}{
	"progress":    {},
	"image_url":   {},
	"video_url":   {},
	"video_urls":  {},
	"description": {},
	"buttons":     {},
	"properties":  {},
}

func validateMidjourneyBulkUpdateParams(params map[string]any) (map[string]any, error) {
	if len(params) == 0 {
		return nil, nil
	}
	normalized := make(map[string]any, len(params))
	for field, value := range params {
		field = strings.TrimSpace(field)
		if _, ok := allowedMidjourneyBulkUpdateFields[field]; !ok {
			return nil, fmt.Errorf("midjourney bulk update field %q is not allowed", field)
		}
		normalized[field] = value
	}
	return normalized, nil
}

func updateMidjourneyBulk(where func(*gorm.DB) *gorm.DB, params map[string]any) error {
	normalized, err := validateMidjourneyBulkUpdateParams(params)
	if err != nil {
		return err
	}
	if len(normalized) == 0 {
		return nil
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var tasks []*Midjourney
		query := where(tx)
		if !common.UsingSQLite {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.Find(&tasks).Error; err != nil {
			return fmt.Errorf("load Midjourney tasks before bulk update: %w", err)
		}
		for _, task := range tasks {
			if err := validateTaskRouteSnapshotForWrite(task.PrivateData.RouteSnapshot, task.ChannelId); err != nil {
				return fmt.Errorf("validate Midjourney task %d before bulk update: %w", task.Id, err)
			}
		}
		if err := where(tx).Updates(normalized).Error; err != nil {
			return fmt.Errorf("bulk update Midjourney tasks: %w", err)
		}
		return nil
	})
}

func normalizeMidjourneyIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	normalized := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	return normalized
}

func normalizeMidjourneyTaskIDs(ids []int) []int {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(ids))
	normalized := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	return normalized
}

func MjBulkUpdate(mjIds []string, params map[string]any) error {
	mjIds = normalizeMidjourneyIDs(mjIds)
	if len(mjIds) == 0 {
		return nil
	}
	return updateMidjourneyBulk(func(tx *gorm.DB) *gorm.DB {
		return tx.Model(&Midjourney{}).Where("mj_id in (?)", mjIds)
	}, params)
}

func MjBulkUpdateByTaskIds(taskIDs []int, params map[string]any) error {
	taskIDs = normalizeMidjourneyTaskIDs(taskIDs)
	if len(taskIDs) == 0 {
		return nil
	}
	return updateMidjourneyBulk(func(tx *gorm.DB) *gorm.DB {
		return tx.Model(&Midjourney{}).Where("id in (?)", taskIDs)
	}, params)
}

// CountAllTasks returns total midjourney tasks for admin query
func CountAllTasks(queryParams TaskQueryParams) int64 {
	var total int64
	query := DB.Model(&Midjourney{})
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}

// CountAllUserTask returns total midjourney tasks for user
func CountAllUserTask(userId int, queryParams TaskQueryParams) int64 {
	var total int64
	query := DB.Model(&Midjourney{}).Where("user_id = ?", userId)
	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}
