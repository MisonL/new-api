package dto

// ChannelRoutingConfig exposes only non-sensitive routing feature flags to
// administrator UIs. It never includes channel keys or runtime credentials.
type ChannelRoutingConfig struct {
	V2Enabled     bool `json:"v2_enabled"`
	ShadowEnabled bool `json:"shadow_enabled"`
	StrictWrites  bool `json:"strict_writes"`
}

// ChannelRouteSelectionDiagnostics is an internal, credential-free snapshot
// of the selector decision. It is kept separate from channel credentials and
// can therefore be copied into administrator-only routing traces.
type ChannelRouteSelectionDiagnostics struct {
	Group          string `json:"group,omitempty"`
	RequestedModel string `json:"requested_model,omitempty"`
	// SelectedCandidateModel is the raw ability/route-candidate key that won
	// selection. It is distinct from RequestedModel because compact and
	// normalized requests may pool several raw candidates into one selection.
	SelectedCandidateModel string `json:"selected_candidate_model,omitempty"`
	EffectiveRouteModel    string `json:"effective_route_model,omitempty"`
	// SelectedChannelID ties the resolved mapping to the concrete candidate
	// that won this selection attempt. Relay mapping and health checks can then
	// reuse the immutable result instead of parsing model_mapping again.
	SelectedChannelID   int    `json:"selected_channel_id,omitempty"`
	RouteScope          string `json:"route_scope,omitempty"`
	RoutePriority       int64  `json:"route_priority,omitempty"`
	RouteWeightBPS      uint   `json:"route_weight_bps,omitempty"`
	WeightSource        string `json:"weight_source,omitempty"`
	SelectionPath       string `json:"selection_path,omitempty"`
	CandidateCount      int    `json:"candidate_count,omitempty"`
	CandidateChannelIDs []int  `json:"candidate_channel_ids,omitempty"`
	ExcludedChannelIDs  []int  `json:"excluded_channel_ids,omitempty"`
}

// ChannelRouteScopeMetricItem is a point-in-time capacity snapshot for one
// channel in a route scope. Redis-backed values are explicitly marked by
// Source/Confirmed so a process-local fallback cannot be mistaken for a
// cluster-wide count.
type ChannelRouteScopeMetricItem struct {
	ChannelID       int    `json:"channel_id"`
	MaxConcurrency  int    `json:"max_concurrency"`
	CurrentInFlight int    `json:"current_in_flight"`
	AvailableSlots  int    `json:"available_slots"`
	RedisAvailable  bool   `json:"redis_available"`
	Confirmed       bool   `json:"confirmed"`
	Source          string `json:"source"`
}

// ChannelRouteScopeMetrics is intentionally bounded to the selected scope.
// Counters are process-wide diagnostics; Items are the per-channel capacity
// snapshots used by the scope workbench.
type ChannelRouteScopeMetrics struct {
	ScopeID        string                        `json:"scope_id"`
	ScopeVersion   string                        `json:"scope_version"`
	RedisAvailable bool                          `json:"redis_available"`
	Concurrency    ChannelConcurrencyMetrics     `json:"concurrency"`
	Items          []ChannelRouteScopeMetricItem `json:"items"`
}
