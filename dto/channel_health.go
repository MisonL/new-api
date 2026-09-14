package dto

// ChannelHealthRoutingEvent records a health-gate decision for a routing attempt.
// It intentionally excludes request content.
type ChannelHealthRoutingEvent struct {
	Group          string `json:"group,omitempty"`
	ModelName      string `json:"model_name,omitempty"`
	ChannelID      int    `json:"channel_id"`
	Action         string `json:"action"`
	StatusCode     int    `json:"status_code,omitempty"`
	ErrorCode      string `json:"error_code,omitempty"`
	RouteKey       string `json:"route_key,omitempty"`
	EffectiveModel string `json:"effective_model,omitempty"`
	Protocol       string `json:"protocol,omitempty"`
	Operation      string `json:"operation,omitempty"`
	UpstreamPath   string `json:"upstream_path,omitempty"`
	MultiKeyIndex  int    `json:"multi_key_index,omitempty"`
}

type ChannelHealthRoutingInfo struct {
	Events           []ChannelHealthRoutingEvent `json:"events,omitempty"`
	RedisUnavailable bool                        `json:"redis_unavailable,omitempty"`
}

// ChannelHealthRuntimeEntry is the short-lived operational state of one
// channel-health scope. It deliberately excludes request payloads and secrets.
type ChannelHealthRuntimeEntry struct {
	Group                      string `json:"group"`
	ModelName                  string `json:"model_name"`
	ChannelID                  int    `json:"channel_id"`
	State                      string `json:"state"`
	CooldownRemainingSeconds   int    `json:"cooldown_remaining_seconds,omitempty"`
	ProbeLeaseRemainingSeconds int    `json:"probe_lease_remaining_seconds,omitempty"`
	StatusCode                 int    `json:"status_code,omitempty"`
	ErrorCode                  string `json:"error_code,omitempty"`
	RouteKey                   string `json:"route_key,omitempty"`
	EffectiveModel             string `json:"effective_model,omitempty"`
	Protocol                   string `json:"protocol,omitempty"`
	Operation                  string `json:"operation,omitempty"`
	UpstreamPath               string `json:"upstream_path,omitempty"`
	MultiKeyIndex              int    `json:"multi_key_index,omitempty"`
}

// ChannelHealthRuntimeEvent records one bounded, short-lived lifecycle
// transition for administrator observability. It excludes request content and
// credentials by design.
type ChannelHealthRuntimeEvent struct {
	OccurredAt     int64  `json:"occurred_at"`
	Group          string `json:"group"`
	ModelName      string `json:"model_name"`
	ChannelID      int    `json:"channel_id"`
	Action         string `json:"action"`
	StatusCode     int    `json:"status_code,omitempty"`
	ErrorCode      string `json:"error_code,omitempty"`
	RouteKey       string `json:"route_key,omitempty"`
	EffectiveModel string `json:"effective_model,omitempty"`
	Protocol       string `json:"protocol,omitempty"`
	Operation      string `json:"operation,omitempty"`
	UpstreamPath   string `json:"upstream_path,omitempty"`
	MultiKeyIndex  int    `json:"multi_key_index,omitempty"`
}

// ChannelHealthRuntimeStatus describes the current shared Redis state used by
// temporary channel cooling and half-open probing.
type ChannelHealthRuntimeStatus struct {
	Enabled           bool `json:"enabled"`
	RedisAvailable    bool `json:"redis_available"`
	CooldownSeconds   int  `json:"cooldown_seconds"`
	ProbeLeaseSeconds int  `json:"probe_lease_seconds"`
	// AsyncTaskQueueDepth is the current count of unfinished tasks in the
	// persistent async task table. It is an observation metric, not a runtime
	// task lease or a queue admission decision.
	AsyncTaskQueueDepth int64                       `json:"async_task_queue_depth"`
	Concurrency         ChannelConcurrencyMetrics   `json:"concurrency"`
	Entries             []ChannelHealthRuntimeEntry `json:"entries"`
	RecentEvents        []ChannelHealthRuntimeEvent `json:"recent_events"`
}

// ChannelConcurrencyMetrics contains process counters for capacity decisions.
// Redis-backed leases are distributed; current_leases_local is explicitly
// scoped to this process and must not be presented as a cluster-wide count.
type ChannelConcurrencyMetrics struct {
	AcquireSuccess     uint64 `json:"acquire_success"`
	AcquireRejected    uint64 `json:"acquire_rejected"`
	AcquireUnavailable uint64 `json:"acquire_unavailable"`
	ReselectSkipped    uint64 `json:"reselect_skipped"`
	Strict429          uint64 `json:"strict_429"`
	RedisUnavailable   uint64 `json:"redis_unavailable"`
	RenewFailure       uint64 `json:"renew_failure"`
	ReleaseFailure     uint64 `json:"release_failure"`
	SaturatedEvents    uint64 `json:"saturated_events"`
	CurrentLeasesLocal uint64 `json:"current_leases_local"`
}
