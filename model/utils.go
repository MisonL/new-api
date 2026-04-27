package model

import (
	"errors"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

const (
	BatchUpdateTypeUserQuota = iota
	BatchUpdateTypeTokenQuota
	BatchUpdateTypeUsedQuota
	BatchUpdateTypeChannelUsedQuota
	BatchUpdateTypeRequestCount
	BatchUpdateTypeCount // if you add a new type, you need to add a new map and a new lock
)

var batchUpdateStores []map[int]int
var batchUpdateLocks []sync.Mutex

type userBatchDelta struct {
	quota        int
	usedQuota    int
	requestCount int
}

func init() {
	for i := 0; i < BatchUpdateTypeCount; i++ {
		batchUpdateStores = append(batchUpdateStores, make(map[int]int))
		batchUpdateLocks = append(batchUpdateLocks, sync.Mutex{})
	}
}

func InitBatchUpdater() {
	gopool.Go(func() {
		for {
			time.Sleep(time.Duration(common.BatchUpdateInterval) * time.Second)
			batchUpdate()
		}
	})
}

func addNewRecord(type_ int, id int, value int) {
	batchUpdateLocks[type_].Lock()
	defer batchUpdateLocks[type_].Unlock()
	if _, ok := batchUpdateStores[type_][id]; !ok {
		batchUpdateStores[type_][id] = value
	} else {
		batchUpdateStores[type_][id] += value
	}
}

func batchUpdate() {
	stores := make([]map[int]int, BatchUpdateTypeCount)
	hasData := false
	for i := 0; i < BatchUpdateTypeCount; i++ {
		batchUpdateLocks[i].Lock()
		stores[i] = batchUpdateStores[i]
		batchUpdateStores[i] = make(map[int]int)
		if len(stores[i]) > 0 {
			hasData = true
		}
		batchUpdateLocks[i].Unlock()
	}

	if !hasData {
		return
	}

	common.SysLog("batch update started")

	for userId, delta := range collectUserBatchDeltas(stores) {
		if err := updateUserBatchDelta(userId, delta); err != nil {
			common.SysLog("failed to batch update user counters: " + err.Error())
		}
	}
	for key, value := range stores[BatchUpdateTypeTokenQuota] {
		if err := increaseTokenQuota(key, value); err != nil {
			common.SysLog("failed to batch update token quota: " + err.Error())
		}
	}
	for key, value := range stores[BatchUpdateTypeChannelUsedQuota] {
		updateChannelUsedQuota(key, value)
	}
	common.SysLog("batch update finished")
}

func collectUserBatchDeltas(stores []map[int]int) map[int]userBatchDelta {
	userDeltas := make(map[int]userBatchDelta)
	for key, value := range stores[BatchUpdateTypeUserQuota] {
		delta := userDeltas[key]
		delta.quota += value
		userDeltas[key] = delta
	}
	for key, value := range stores[BatchUpdateTypeUsedQuota] {
		delta := userDeltas[key]
		delta.usedQuota += value
		userDeltas[key] = delta
	}
	for key, value := range stores[BatchUpdateTypeRequestCount] {
		delta := userDeltas[key]
		delta.requestCount += value
		userDeltas[key] = delta
	}
	return userDeltas
}

func updateUserBatchDelta(id int, delta userBatchDelta) error {
	updates := map[string]interface{}{}
	if delta.quota != 0 {
		updates["quota"] = gorm.Expr("quota + ?", delta.quota)
	}
	if delta.usedQuota != 0 {
		updates["used_quota"] = gorm.Expr("used_quota + ?", delta.usedQuota)
	}
	if delta.requestCount != 0 {
		updates["request_count"] = gorm.Expr("request_count + ?", delta.requestCount)
	}
	if len(updates) == 0 {
		return nil
	}
	return DB.Model(&User{}).Where("id = ?", id).Updates(updates).Error
}

func RecordExist(err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return false, err
}

func shouldUpdateRedis(fromDB bool, err error) bool {
	return common.RedisEnabled && fromDB && err == nil
}
