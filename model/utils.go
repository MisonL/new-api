package model

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

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

// batchUpdateRunMu serializes the periodic updater with the shutdown flush.
// Without it, CloseDB could run while a ticker-triggered write is still using
// the SQL connection pool.
var batchUpdateRunMu sync.Mutex

var batchUpdaterLifecycleMu sync.Mutex
var batchUpdaterStop chan struct{}
var batchUpdaterDone chan struct{}

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
	batchUpdaterLifecycleMu.Lock()
	if batchUpdaterStop != nil {
		batchUpdaterLifecycleMu.Unlock()
		return
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	batchUpdaterStop = stop
	batchUpdaterDone = done
	batchUpdaterLifecycleMu.Unlock()

	go func() {
		defer close(done)
		interval := time.Duration(common.BatchUpdateInterval) * time.Second
		if interval <= 0 {
			interval = time.Second
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				runBatchUpdateSafely()
			case <-stop:
				return
			}
		}
	}()
}

// runBatchUpdateSafely runs one periodic batch and contains a panic inside the
// updater goroutine. This loop previously ran inside gopool, whose worker
// recovers per task; a plain goroutine must recover itself, otherwise a panic
// in a single batch write takes the whole process down — the opposite of what
// the crash-resilience work is for. The panicking batch is lost (it was already
// swapped out of the stores), so the error is logged loudly rather than
// swallowed.
func runBatchUpdateSafely() {
	defer func() {
		if r := recover(); r != nil {
			common.SysError(fmt.Sprintf("panic in batch update: %v", r))
		}
	}()
	batchUpdate()
}

// batchUpdaterStopWaitUpperBound 是等待写入协程收尾的上限。超时后返回并记录
// 错误，而不是永久挂起：FlushBatchUpdates 用 batchUpdateRunMu 与在途的写入
// 串行化，继续关闭仍不会与它并发写库；真正的兜底是进程外的终止信号。
// 无界等待会让一次数据库故障把整个关闭流程挂死，超时值之外的等待没有意义。
var batchUpdaterStopWaitUpperBound = 15 * time.Second

// StopBatchUpdater stops the periodic writer and waits for an in-flight batch
// to finish. Call it before FlushBatchUpdates and CloseDB during shutdown.
// It is safe to call when the updater was never started or was already stopped.
func StopBatchUpdater() {
	batchUpdaterLifecycleMu.Lock()
	stop := batchUpdaterStop
	done := batchUpdaterDone
	if stop == nil {
		batchUpdaterLifecycleMu.Unlock()
		return
	}
	batchUpdaterStop = nil
	batchUpdaterDone = nil
	close(stop)
	batchUpdaterLifecycleMu.Unlock()

	select {
	case <-done:
	case <-time.After(batchUpdaterStopWaitUpperBound):
		common.SysError(fmt.Sprintf(
			"batch updater did not stop within %s, continuing shutdown anyway",
			batchUpdaterStopWaitUpperBound,
		))
	}
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

// FlushBatchUpdates writes the counters accumulated in memory to the database.
// It is called on graceful shutdown so quota charged during the final batch
// interval is not lost, and is safe to call at any time: batchUpdate swaps each
// store out under its lock before writing, so a concurrent ticker call and this
// one cannot double-apply a delta, and a call with nothing pending returns
// without touching the database.
//
// After StopBatchUpdater the periodic writer is gone, so the only thing that can
// hold batchUpdateRunMu here is an in-flight batch from before shutdown. If that
// write hangs on a dead database, blocking here forever would hang the whole
// shutdown, so the wait is bounded: the in-flight write carries its own already
// swapped deltas, and losing them is the same failure as that write failing.
func FlushBatchUpdates() {
	deadline := time.Now().Add(batchUpdaterStopWaitUpperBound)
	for {
		if batchUpdateRunMu.TryLock() {
			defer batchUpdateRunMu.Unlock()
			batchUpdateOnce()
			return
		}
		if time.Now().After(deadline) {
			common.SysError(fmt.Sprintf(
				"flush skipped: previous batch write still running after %s",
				batchUpdaterStopWaitUpperBound,
			))
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func batchUpdate() {
	batchUpdateRunMu.Lock()
	defer batchUpdateRunMu.Unlock()

	batchUpdateOnce()
}

func batchUpdateOnce() {
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
	if DB == nil {
		requeueBatchUpdates(stores)
		common.SysError("batch update skipped: database is not initialized")
		return
	}

	common.SysLog("batch update started")

	for userId, delta := range collectUserBatchDeltas(stores) {
		if err := updateUserBatchDelta(userId, delta); err != nil {
			requeueUserBatchDelta(userId, delta)
			common.SysLog("failed to batch update user counters: " + err.Error())
		}
	}
	for key, value := range stores[BatchUpdateTypeTokenQuota] {
		if err := increaseTokenQuota(key, value); err != nil {
			requeueBatchDelta(BatchUpdateTypeTokenQuota, key, value)
			common.SysLog("failed to batch update token quota: " + err.Error())
		}
	}
	for key, value := range stores[BatchUpdateTypeChannelUsedQuota] {
		if err := updateChannelUsedQuota(key, value); err != nil {
			requeueBatchDelta(BatchUpdateTypeChannelUsedQuota, key, value)
		}
	}
	common.SysLog("batch update finished")
}

func requeueUserBatchDelta(id int, delta userBatchDelta) {
	requeueBatchDelta(BatchUpdateTypeUserQuota, id, delta.quota)
	requeueBatchDelta(BatchUpdateTypeUsedQuota, id, delta.usedQuota)
	requeueBatchDelta(BatchUpdateTypeRequestCount, id, delta.requestCount)
}

func requeueBatchUpdates(stores []map[int]int) {
	for type_, records := range stores {
		for id, value := range records {
			requeueBatchDelta(type_, id, value)
		}
	}
}

func requeueBatchDelta(type_ int, id int, value int) {
	if value == 0 {
		return
	}
	batchUpdateLocks[type_].Lock()
	batchUpdateStores[type_][id] += value
	batchUpdateLocks[type_].Unlock()
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
