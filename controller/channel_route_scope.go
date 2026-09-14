package controller

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func GetChannelRouteScopes(c *gin.Context) {
	scopes, err := model.ListChannelRouteScopes(c.Query("group"), c.Query("model"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, scopes)
}

// GetChannelRoutingConfig returns the routing feature flags used by both
// administrator frontends. The response is deliberately non-sensitive.
func GetChannelRoutingConfig(c *gin.Context) {
	common.ApiSuccess(c, dto.ChannelRoutingConfig{
		V2Enabled:     model.IsChannelRoutingV2Enabled(),
		ShadowEnabled: model.IsChannelRoutingShadowEnabled(),
		StrictWrites:  model.IsChannelRoutingV2StrictWrites(),
	})
}

// GetChannelRouteScopeMetrics returns bounded, point-in-time capacity data for
// one scope. It does not expose lease tokens or channel credentials.
func GetChannelRouteScopeMetrics(c *gin.Context) {
	scopeID := c.Param("scope_id")
	scope, err := model.GetChannelRouteScope(scopeID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "route scope not found"})
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items := make([]dto.ChannelRouteScopeMetricItem, 0, len(scope.Items))
	redisAvailable := true
	for _, item := range scope.Items {
		snapshot := service.GetChannelConcurrencySnapshot(item.ChannelID, item.MaxConcurrency)
		items = append(items, dto.ChannelRouteScopeMetricItem{
			ChannelID:       snapshot.ChannelID,
			MaxConcurrency:  snapshot.MaxConcurrency,
			CurrentInFlight: snapshot.CurrentInFlight,
			AvailableSlots:  snapshot.AvailableSlots,
			RedisAvailable:  snapshot.RedisAvailable,
			Confirmed:       snapshot.Confirmed,
			Source:          snapshot.Source,
		})
		if item.MaxConcurrency > 0 && !snapshot.RedisAvailable {
			redisAvailable = false
		}
	}
	common.ApiSuccess(c, dto.ChannelRouteScopeMetrics{
		ScopeID:        scope.ID,
		ScopeVersion:   scope.Version,
		RedisAvailable: redisAvailable,
		Concurrency:    service.ChannelConcurrencyMetricsSnapshot(),
		Items:          items,
	})
}

func GetChannelRouteScope(c *gin.Context) {
	scope, err := model.GetChannelRouteScope(c.Param("scope_id"))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "route scope not found"})
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, scope)
}

// routeScopeAuditItems converts a scope snapshot to a credential-free,
// deterministic representation suitable for the admin-only audit payload.
// Keeping the old and new snapshots in the log makes scope-wide edits
// reviewable even when one request updates several mapped raw abilities.
func routeScopeAuditItems(scope *model.ChannelRouteScope) []map[string]interface{} {
	if scope == nil {
		return nil
	}
	items := append([]model.ChannelRouteScopeItem(nil), scope.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].ChannelID < items[j].ChannelID })
	result := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		result = append(result, map[string]interface{}{
			"channel_id":       item.ChannelID,
			"channel_name":     item.ChannelName,
			"channel_type":     item.ChannelType,
			"channel_status":   item.ChannelStatus,
			"ability_enabled":  item.AbilityEnabled,
			"route_priority":   item.RoutePriority,
			"route_weight_bps": item.RouteWeightBPS,
			"max_concurrency":  item.MaxConcurrency,
		})
	}
	return result
}

func recordChannelRouteScopeAudit(c *gin.Context, operation string, mode string, oldScope *model.ChannelRouteScope, newScope *model.ChannelRouteScope) {
	if c == nil || oldScope == nil || newScope == nil {
		return
	}
	audit := map[string]interface{}{
		"operation":             operation,
		"adjustment_mode":       strings.TrimSpace(mode),
		"scope_id":              oldScope.ID,
		"old_scope_id":          oldScope.ID,
		"new_scope_id":          newScope.ID,
		"group":                 oldScope.Group,
		"effective_route_model": oldScope.EffectiveRouteModel,
		"old_version":           oldScope.Version,
		"new_version":           newScope.Version,
		"old_route_priority":    oldScope.RoutePriority,
		"new_route_priority":    newScope.RoutePriority,
		"old_items":             routeScopeAuditItems(oldScope),
		"new_items":             routeScopeAuditItems(newScope),
	}
	adminID := c.GetInt("id")
	adminUsername := c.GetString("username")
	model.RecordLogWithAdminInfo(adminID, model.LogTypeManage,
		fmt.Sprintf("route scope %s updated: scope=%s", operation, oldScope.ID),
		map[string]interface{}{
			"admin_id":          adminID,
			"admin_username":    adminUsername,
			"route_scope_audit": audit,
		})
}

func UpdateChannelRouteScopeWeights(c *gin.Context) {
	var request model.ChannelRouteScopeWeightUpdate
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorMsg(c, "invalid route scope weight payload")
		return
	}
	scopeID := c.Param("scope_id")
	oldScope, err := model.GetChannelRouteScope(scopeID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "route scope not found"})
			return
		}
		common.ApiError(c, err)
		return
	}
	scope, err := model.UpdateChannelRouteScopeWeights(scopeID, request)
	if errors.Is(err, model.ErrRouteScopeVersionConflict) {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "route scope version conflict", "code": "route_scope_version_conflict"})
		return
	}
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	recordChannelRouteScopeAudit(c, "weights", request.Mode, oldScope, scope)
	common.ApiSuccess(c, gin.H{"scope": scope, "scope_version": scope.Version})
}

func UpdateChannelRouteScopePriority(c *gin.Context) {
	var request model.ChannelRouteScopePriorityUpdate
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorMsg(c, "invalid route scope priority payload")
		return
	}
	scopeID := c.Param("scope_id")
	oldScope, err := model.GetChannelRouteScope(scopeID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "route scope not found"})
			return
		}
		common.ApiError(c, err)
		return
	}
	scope, err := model.UpdateChannelRouteScopePriority(scopeID, request)
	if errors.Is(err, model.ErrRouteScopeVersionConflict) {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "route scope version conflict", "code": "route_scope_version_conflict"})
		return
	}
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	recordChannelRouteScopeAudit(c, "priority", "", oldScope, scope)
	common.ApiSuccess(c, gin.H{"scope": scope, "scope_version": scope.Version})
}

func PreviewChannelRouteScopeMigration(c *gin.Context) {
	report, err := model.PreviewRouteFieldsBackfill(nil)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, report)
}

func ApplyChannelRouteScopeMigration(c *gin.Context) {
	report, err := model.ApplyRouteFieldsBackfill(nil)
	if errors.Is(err, model.ErrRouteScopeMigrationConflicts) {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"message": "route scope migration has unresolved conflicts",
			"code":    "route_scope_migration_conflicts",
			"data":    report,
		})
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	adminID := c.GetInt("id")
	model.RecordLogWithAdminInfo(adminID, model.LogTypeManage,
		fmt.Sprintf("route scope legacy backfill applied: scopes=%d abilities=%d channels=%d skipped=%d", report.ScopeCount, report.AbilitiesToBackfill, report.ChannelsToBackfill, report.SkippedScopes),
		map[string]interface{}{
			"admin_id":              adminID,
			"admin_username":        c.GetString("username"),
			"migration":             true,
			"dry_run":               false,
			"route_scope_migration": report,
		})
	common.ApiSuccess(c, report)
}
