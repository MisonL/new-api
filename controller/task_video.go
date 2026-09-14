package controller

import (
	"context"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

// UpdateVideoTaskAll is kept as a compatibility entry point for callers that
// historically invoked controller-level polling. The service implementation is
// now canonical so exported and background polling cannot bypass persisted
// credentials, route mapping, health gates or concurrency leases.
func UpdateVideoTaskAll(ctx context.Context, platform constant.TaskPlatform, taskChannelM map[int][]string, taskM map[string]*model.Task) error {
	return service.UpdateVideoTasks(ctx, platform, taskChannelM, taskM)
}
