package model

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 关闭流程里的两处无界等待：StopBatchUpdater 的 <-done 与 FlushBatchUpdates
// 经 batchUpdate 抢的 batchUpdateRunMu。一次挂死的数据库写入会让 SIGTERM
// 之后进程永不出栈，编排层只能升级成 SIGKILL，反而丢掉所有未落盘数据。
// 这里用一个长期占用 batchUpdateRunMu 的 goroutine 模拟挂死的写入。
func TestFlushBatchUpdatesDoesNotBlockForeverOnStuckWrite(t *testing.T) {
	resetBatchUpdateStoresForTest()
	t.Cleanup(resetBatchUpdateStoresForTest)

	originalBound := batchUpdaterStopWaitUpperBound
	batchUpdaterStopWaitUpperBound = 300 * time.Millisecond
	t.Cleanup(func() { batchUpdaterStopWaitUpperBound = originalBound })

	batchUpdateRunMu.Lock()
	release := make(chan struct{})
	var once sync.Once
	unlock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unlock)
	go func() {
		<-release
		batchUpdateRunMu.Unlock()
	}()

	completed := make(chan struct{})
	start := time.Now()
	go func() {
		FlushBatchUpdates()
		close(completed)
	}()

	select {
	case <-completed:
	case <-time.After(5 * time.Second):
		unlock()
		t.Fatalf("FlushBatchUpdates blocked on a stuck write for more than %s", batchUpdaterStopWaitUpperBound)
	}
	elapsed := time.Since(start)
	require.GreaterOrEqual(t, elapsed, batchUpdaterStopWaitUpperBound/2,
		"必须真的等到上限而不是立即放弃")
}

// StopBatchUpdater 同样不能无界等待：写入协程挂在挂死的语句上时，
// <-done 会把 SIGTERM 处理永久卡住。这里直接注入一个永不变 close 的
// done 通道，模拟写入协程不退出。
func TestStopBatchUpdaterTimesOutOnStuckWriter(t *testing.T) {
	originalBound := batchUpdaterStopWaitUpperBound
	batchUpdaterStopWaitUpperBound = 300 * time.Millisecond
	t.Cleanup(func() { batchUpdaterStopWaitUpperBound = originalBound })

	stop := make(chan struct{})
	done := make(chan struct{})

	batchUpdaterLifecycleMu.Lock()
	previousStop, previousDone := batchUpdaterStop, batchUpdaterDone
	batchUpdaterStop, batchUpdaterDone = stop, done
	batchUpdaterLifecycleMu.Unlock()

	t.Cleanup(func() {
		batchUpdaterLifecycleMu.Lock()
		batchUpdaterStop, batchUpdaterDone = previousStop, previousDone
		batchUpdaterLifecycleMu.Unlock()
		close(done)
	})

	start := time.Now()
	finished := make(chan struct{})
	go func() {
		StopBatchUpdater()
		close(finished)
	}()

	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("StopBatchUpdater blocked on a stuck writer instead of timing out")
	}
	elapsed := time.Since(start)
	require.GreaterOrEqual(t, elapsed, batchUpdaterStopWaitUpperBound/2,
		"必须真的等到上限而不是立即放弃")
	require.Less(t, elapsed, batchUpdaterStopWaitUpperBound+5*time.Second)
}
