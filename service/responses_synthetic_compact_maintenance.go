package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	syntheticCompactStatePruneInterval = 24 * time.Hour
	syntheticCompactStatePruneTimeout  = 5 * time.Minute
)

var syntheticCompactStatePruneOnce sync.Once

func StartSyntheticCompactStatePruneTask() {
	syntheticCompactStatePruneOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		go func() {
			ticker := time.NewTicker(syntheticCompactStatePruneInterval)
			defer ticker.Stop()
			for {
				startedAt := time.Now()
				ctx, cancel := context.WithTimeout(context.Background(), syntheticCompactStatePruneTimeout)
				deleted, err := model.PruneExpiredSyntheticCompactStateRecords(ctx, startedAt.Unix())
				cancel()
				duration := time.Since(startedAt)
				if err != nil {
					common.SysError(fmt.Sprintf("prune expired synthetic compact states failed: duration=%s error=%s", duration, err.Error()))
				} else if deleted > 0 || common.DebugEnabled {
					common.SysLog(fmt.Sprintf("prune expired synthetic compact states completed: deleted=%d duration=%s", deleted, duration))
				}
				<-ticker.C
			}
		}()
	})
}
