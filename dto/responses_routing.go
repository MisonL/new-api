package dto

type ResponsesRoutingRoute struct {
	Group               string `json:"group,omitempty"`
	RequestedModel      string `json:"requested_model,omitempty"`
	EffectiveRouteModel string `json:"effective_route_model,omitempty"`
	RouteScope          string `json:"route_scope,omitempty"`
	RoutePriority       int64  `json:"route_priority,omitempty"`
	RouteWeightBPS      uint   `json:"route_weight_bps,omitempty"`
	WeightSource        string `json:"weight_source,omitempty"`
	SelectionPath       string `json:"selection_path,omitempty"`
}

type ResponsesRoutingCandidate struct {
	ChannelID int                   `json:"channel_id"`
	Route     ResponsesRoutingRoute `json:"route,omitempty"`
}

// ResponsesRoutingAttempt describes one channel that actually received a
// relay attempt. Candidates rejected during preflight are kept separately.
type ResponsesRoutingAttempt struct {
	ChannelID     int                   `json:"channel_id"`
	StatusCode    int                   `json:"status_code,omitempty"`
	ErrorCode     string                `json:"error_code,omitempty"`
	ErrorCategory string                `json:"error_category,omitempty"`
	Outcome       string                `json:"outcome,omitempty"`
	Route         ResponsesRoutingRoute `json:"route,omitempty"`
}

type ResponsesRoutingTrace struct {
	Candidates        []int                         `json:"candidates,omitempty"`
	CandidateDetails  []ResponsesRoutingCandidate   `json:"candidate_details,omitempty"`
	PreflightSkipped  []ResponsesRoutingChannelSkip `json:"preflight_skipped,omitempty"`
	AttemptedChannels []ResponsesRoutingAttempt     `json:"attempted_channels,omitempty"`
	FinalChannel      int                           `json:"final_channel,omitempty"`
	TerminalReason    string                        `json:"terminal_reason,omitempty"`
}

// ResponsesRoutingUserSummary is the redacted routing result exposed to the
// request owner. It deliberately contains no channel identifiers, upstream
// paths, headers, or provider-specific error details.
type ResponsesRoutingUserSummary struct {
	Outcome            string   `json:"outcome,omitempty"`
	AutomaticReroute   bool     `json:"automatic_reroute,omitempty"`
	AttemptCount       int      `json:"attempt_count,omitempty"`
	PreflightSkipCount int      `json:"preflight_skip_count,omitempty"`
	Reasons            []string `json:"reasons,omitempty"`
}
