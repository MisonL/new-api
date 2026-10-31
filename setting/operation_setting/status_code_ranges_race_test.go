package operation_setting

import (
	"sync"
	"testing"
)

// 管理端保存设置会在 OptionMapRWMutex 写锁内调用这两个 FromString，直接给包级
// 切片赋值；而中继热路径的 ShouldRetryByStatusCode / ShouldDisableByStatusCode
// 无锁裸读同一个切片头。切片头是三字（指针/长度/容量），撕裂读可能把新指针和
// 旧长度拼在一起。用例在 -race 下必须无告警。
func TestStatusCodeRangesDoNotRaceWithReads(t *testing.T) {
	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = ShouldRetryByStatusCode(400)
			_ = ShouldRetryByStatusCode(502)
			_ = ShouldDisableByStatusCode(401)
			_ = AutomaticRetryStatusCodesToString()
			_ = AutomaticDisableStatusCodesToString()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 3000; i++ {
			if err := AutomaticRetryStatusCodesFromString("100-199,300-399,400-407,409-499,500-503,505-523,525-599"); err != nil {
				t.Errorf("retry ranges: %v", err)
				return
			}
			if err := AutomaticDisableStatusCodesFromString("401"); err != nil {
				t.Errorf("disable ranges: %v", err)
				return
			}
		}
		close(stop)
	}()

	wg.Wait()
}
