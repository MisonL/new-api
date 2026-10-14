package middleware

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// modelHasEnabledAbilityInGroups reports whether at least one group in the
// caller's group list has an enabled ability for the model. A model with no
// such ability can never be routed, so the distributor answers 404
// model_not_found instead of a retryable 503; the 503 is reserved for a model
// that exists but currently has no free channel.
func modelHasEnabledAbilityInGroups(c *gin.Context, groups string, modelName string) bool {
	if strings.TrimSpace(modelName) == "" || strings.TrimSpace(groups) == "" {
		return false
	}
	groupList := strings.Split(groups, ",")
	model.InitChannelCache()
	for _, group := range groupList {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		for _, enabled := range model.GetGroupEnabledModels(group) {
			if enabled == modelName {
				return true
			}
		}
	}
	return false
}

// abortNoRoutableModel rejects a request whose model cannot be routed. A model
// with no enabled ability anywhere in the caller's groups is a client mistake
// and gets 404 model_not_found; a known model with no free channel right now
// is transient and keeps its 503 get_channel_failed so the caller knows to
// retry.
func abortNoRoutableModel(c *gin.Context, groups string, modelName string) {
	if !modelHasEnabledAbilityInGroups(c, groups, modelName) {
		abortWithOpenAiMessage(c, http.StatusNotFound, "no available channel for model "+modelName, types.ErrorCodeModelNotFound)
		return
	}
	abortWithOpenAiMessage(c, http.StatusServiceUnavailable, "no available channel for model "+modelName, types.ErrorCodeGetChannelFailed)
}
