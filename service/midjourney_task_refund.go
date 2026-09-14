package service

import (
	"context"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

const (
	midjourneyTaskCodeSubmitted = 1
	midjourneyTaskCodeExisting  = 21
	midjourneyTaskCodeQueued    = 22
)

func isRefundableMidjourneyTaskCode(code int) bool {
	switch code {
	case midjourneyTaskCodeSubmitted, midjourneyTaskCodeExisting, midjourneyTaskCodeQueued:
		return true
	default:
		return false
	}
}

// RefundMidjourneyTaskQuota refunds quota for a billed Midjourney task that
// has transitioned to a terminal failure. Callers must guard the transition
// with their compare-and-swap result so repeated polling or webhook deliveries
// cannot issue a second refund.
func RefundMidjourneyTaskQuota(ctx context.Context, task *model.Midjourney, reason string) {
	if task == nil || task.Quota <= 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// Only accepted provider responses are billed by the Midjourney submit
	// handlers. Legacy rows can carry a stale Quota value even when their
	// provider response was rejected; do not turn those rows into credits.
	if !isRefundableMidjourneyTaskCode(task.Code) {
		return
	}
	if err := model.IncreaseUserQuota(task.UserId, task.Quota, false); err != nil {
		logger.LogError(ctx, "fail to increase user quota: "+err.Error())
		return
	}
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId:    task.UserId,
		LogType:   model.LogTypeRefund,
		Content:   "",
		ChannelId: task.ChannelId,
		ModelName: CovertMjpActionToModelName(task.Action),
		Quota:     task.Quota,
		Other: map[string]interface{}{
			"task_id": task.MjId,
			"reason":  reason,
		},
	})
}
