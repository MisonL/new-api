package service

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// ErrTaskRouteMappingMissing indicates that a legacy asynchronous task does
// not contain the provider-facing model needed to reconstruct its original
// route. Callers must surface this condition or leave the task pending for
// operator reconciliation; they must never resolve the channel's current
// model_mapping as an implicit historical value.
var ErrTaskRouteMappingMissing = errors.New("task route effective model is not persisted")

// ResolveTaskOriginModelName returns the client-facing model captured when an
// asynchronous task was submitted. Current-schema route snapshots are the
// strongest source of truth; legacy fields are retained only as compatibility
// fallbacks for rows written before route snapshots existed.
func ResolveTaskOriginModelName(task *model.Task) string {
	if task == nil {
		return ""
	}
	if snapshot := task.PrivateData.RouteSnapshot; snapshot != nil && strings.TrimSpace(snapshot.RequestedModel) != "" {
		return strings.TrimSpace(snapshot.RequestedModel)
	}
	if task.PrivateData.BillingContext != nil && strings.TrimSpace(task.PrivateData.BillingContext.OriginModelName) != "" {
		return strings.TrimSpace(task.PrivateData.BillingContext.OriginModelName)
	}
	if strings.TrimSpace(task.Properties.OriginModelName) != "" {
		return strings.TrimSpace(task.Properties.OriginModelName)
	}
	var taskData map[string]any
	if err := common.Unmarshal(task.Data, &taskData); err == nil {
		if modelName, ok := taskData["model"].(string); ok && strings.TrimSpace(modelName) != "" {
			return strings.TrimSpace(modelName)
		}
	}
	if strings.TrimSpace(task.Properties.UpstreamModelName) != "" {
		return strings.TrimSpace(task.Properties.UpstreamModelName)
	}
	return strings.TrimSpace(CoverTaskActionToModelName(task.Platform, task.Action))
}

// ResolveTaskEffectiveModelName returns the provider-facing model captured at
// task submission. It never re-resolves the channel's mutable model_mapping.
func ResolveTaskEffectiveModelName(task *model.Task) string {
	if task == nil {
		return ""
	}
	if effectiveModel := ResolveTaskPersistedEffectiveModelName(task); effectiveModel != "" {
		return effectiveModel
	}
	return ResolveTaskOriginModelName(task)
}

// ResolveTaskPersistedEffectiveModelName returns only an effective model that
// was actually stored with the task. It deliberately does not fall back to the
// requested model: callers using this helper need to distinguish a historical
// row with no mapping snapshot from a task that was explicitly submitted
// without a mapping.
func ResolveTaskPersistedEffectiveModelName(task *model.Task) string {
	if task == nil {
		return ""
	}
	if snapshot := task.PrivateData.RouteSnapshot; snapshot != nil && strings.TrimSpace(snapshot.EffectiveUpstreamModel) != "" {
		return strings.TrimSpace(snapshot.EffectiveUpstreamModel)
	}
	return strings.TrimSpace(task.Properties.UpstreamModelName)
}
