package service

import (
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
)

// 敏感词列表由管理端保存设置时整体替换，而每个中继请求都会遍历它做检测。
// 请求路径此前直接读包级切片，与替换构成数据竞争（切片头可能被读到撕裂的
// 长度/指针）。这里用 -race 覆盖这条路径，并断言检测结果只受一次快照影响。
func TestSensitiveWordContainsIsRaceFreeUnderConcurrentUpdate(t *testing.T) {
	previous := setting.SensitiveWordsSnapshot()
	t.Cleanup(func() {
		setting.SensitiveWordsFromString(strings.Join(previous, "\n"))
	})

	setting.SensitiveWordsFromString("alpha\nbeta\ngamma")

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			_, _ = SensitiveWordContains("hello alpha world")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			setting.SensitiveWordsFromString("delta\nepsilon\nzeta")
		}
	}()
	wg.Wait()
}

// 快照必须是副本：调用方拿到的是独立切片，后续替换不会改变已取到的值。
func TestSensitiveWordsSnapshotIsACopy(t *testing.T) {
	previous := setting.SensitiveWordsSnapshot()
	t.Cleanup(func() {
		setting.SensitiveWordsFromString(strings.Join(previous, "\n"))
	})

	setting.SensitiveWordsFromString("alpha\nbeta")
	snapshot := setting.SensitiveWordsSnapshot()
	require.Equal(t, []string{"alpha", "beta"}, snapshot)

	setting.SensitiveWordsFromString("gamma")
	require.Equal(t, []string{"alpha", "beta"}, snapshot, "the snapshot must not observe later replacement")

	detected, words := SensitiveWordContains("alpha and beta")
	require.False(t, detected, "replaced list must not still match the old words")
	require.Empty(t, words)
}
