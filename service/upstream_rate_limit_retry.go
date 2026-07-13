package service

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type UpstreamRateLimitRetryState struct {
	Limit            int
	Used             int
	Triggered        bool
	Terminal         bool
	TriggerChannelID int
}

func GetUpstreamRateLimitRetryState(c *gin.Context) (*UpstreamRateLimitRetryState, bool) {
	return common.GetContextKeyType[*UpstreamRateLimitRetryState](c, constant.ContextKeyUpstreamRateLimitRetryState)
}

func ObserveUpstreamRateLimitRetry(c *gin.Context, channelID int) {
	if c == nil {
		return
	}
	state, ok := GetUpstreamRateLimitRetryState(c)
	if !ok || state == nil {
		limit := operation_setting.GetUpstreamRateLimitRetryLimit()
		if limit < 0 {
			limit = 0
		}
		state = &UpstreamRateLimitRetryState{Limit: limit}
		common.SetContextKey(c, constant.ContextKeyUpstreamRateLimitRetryState, state)
	}
	state.Triggered = true
	if state.TriggerChannelID == 0 {
		state.TriggerChannelID = channelID
	}
	if state.Used >= state.Limit {
		state.Terminal = true
	}
}

func StartUpstreamRateLimitRetryFollowup(c *gin.Context) bool {
	state, ok := GetUpstreamRateLimitRetryState(c)
	if !ok || state == nil || !state.Triggered {
		return true
	}
	if state.Terminal {
		return false
	}
	state.Used++
	if state.Used >= state.Limit {
		state.Terminal = true
	}
	return true
}

func NewUpstreamRateLimitRetryExhaustedError() *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		errors.New("upstream rate limit retry limit reached"),
		types.ErrorCodeDoRequestFailed,
		http.StatusTooManyRequests,
		types.ErrOptionWithSkipRetry(),
	)
}

func TerminateUpstreamRateLimitRetry(c *gin.Context) {
	state, ok := GetUpstreamRateLimitRetryState(c)
	if !ok || state == nil {
		return
	}
	state.Terminal = true
}

func IsUpstreamRateLimitRetryTerminal(c *gin.Context) bool {
	state, ok := GetUpstreamRateLimitRetryState(c)
	return ok && state != nil && state.Terminal
}

func AppendUpstreamRateLimitRetryAdminInfo(c *gin.Context, adminInfo map[string]interface{}) {
	if adminInfo == nil {
		return
	}
	state, ok := GetUpstreamRateLimitRetryState(c)
	if !ok || state == nil || !state.Triggered {
		return
	}
	adminInfo["upstream_rate_limit_retry_limit"] = state.Limit
	adminInfo["upstream_rate_limit_retry_used"] = state.Used
	adminInfo["upstream_rate_limit_retry_terminal"] = state.Terminal
	adminInfo["upstream_rate_limit_trigger_channel_id"] = state.TriggerChannelID
}
