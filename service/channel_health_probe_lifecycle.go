package service

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

// channelHealthProbeRenewalContextKey marks the request context installed by
// the half-open probe renewal worker. A concurrency lease may wrap that
// request with another context before the probe is finalized; the marker lets
// cleanup recognize the nested wrapper and restore the original request
// safely instead of leaving a canceled probe context for the next retry.
type channelHealthProbeRenewalContextKey struct{}

const clearChannelHealthProbeScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
	redis.call("DEL", KEYS[1], KEYS[2], KEYS[3], KEYS[4], KEYS[5])
	redis.call("ZREM", KEYS[6], ARGV[2])
	return 1
end
return 0
`

const releaseChannelHealthProbeScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  redis.call("DEL", KEYS[1])
  return 1
end
return 0
`

const renewChannelHealthProbeScript = `
if redis.call("GET", KEYS[1]) ~= ARGV[1] then
  return 0
end
local remaining = redis.call("PTTL", KEYS[1])
if remaining <= 0 then
  redis.call("DEL", KEYS[1])
  return 0
end
redis.call("PEXPIRE", KEYS[1], ARGV[2])
redis.call("PEXPIRE", KEYS[2], ARGV[3])
redis.call("PEXPIRE", KEYS[3], ARGV[3])
local redis_time = redis.call("TIME")
local now_ms = tonumber(redis_time[1]) * 1000 + math.floor(tonumber(redis_time[2]) / 1000)
local runtime_expiry_unix = math.floor((now_ms + tonumber(ARGV[3]) + 999) / 1000)
redis.call("ZADD", KEYS[4], runtime_expiry_unix, ARGV[4])
return 1
`

// MarkChannelHealthProbeSuccess clears a half-open state only when this request
// still owns the probe lease. A concurrent failure therefore wins over a stale success.
func MarkChannelHealthProbeSuccess(c *gin.Context, info *relaycommon.RelayInfo) {
	state := channelHealthRouting(c)
	if state == nil || state.Probe == nil || info == nil || info.ChannelMeta == nil {
		return
	}
	probe := *state.Probe
	if !channelHealthProbeMatchesRelayInfo(state, probe, info) {
		return
	}
	// A renewal worker may have lost the lease and canceled the request while
	// the upstream operation was still unwinding. Do not turn that canceled
	// operation into a successful probe; let the request goroutine perform the
	// normal release and cleanup instead.
	if channelHealthProbeRenewalTerminated(state, probe.Token) {
		ReleaseChannelHealthProbeForChannel(c, probe.ChannelID)
		return
	}
	rdb, ok := channelHealthRedisClient(c)
	if !ok {
		clearLocalChannelHealthProbe(state, probe.Token)
		return
	}
	cooldownKey, historyKey, probeKey, failuresKey := channelHealthKeysForRoute(probe.Group, probe.Route)
	metadataKey := channelHealthMetadataKeyForRoute(probe.Group, probe.Route)
	digest := channelHealthDigestForRoute(probe.Group, probe.Route)
	ctx, cancel := context.WithTimeout(context.Background(), channelHealthRedisTimeout)
	defer cancel()
	cleared, err := rdb.Eval(
		ctx,
		clearChannelHealthProbeScript,
		[]string{probeKey, cooldownKey, historyKey, metadataKey, failuresKey, channelHealthRuntimeIndexKey},
		probe.Token,
		digest,
	).Int()
	if err != nil {
		common.SysError(fmt.Sprintf("channel health half-open recovery failed: channel_id=%d error=%v", probe.ChannelID, err))
		markChannelHealthUnavailable(c)
		clearLocalChannelHealthProbe(state, probe.Token)
		return
	}
	clearLocalChannelHealthProbe(state, probe.Token)
	if cleared == 1 {
		recordChannelHealthEventForRoute(c, probe.Group, probe.ModelName, probe.ChannelID, probe.Route, ChannelHealthSelectionHalfOpenRecovered, 0, "")
		recordChannelHealthRuntimeEventForRoute(c, probe.Group, probe.ModelName, probe.ChannelID, probe.Route, ChannelHealthSelectionHalfOpenRecovered, 0, "")
		common.SysLog(fmt.Sprintf("channel health half-open probe recovered: group=%s model=%s channel_id=%d", probe.Group, probe.ModelName, probe.ChannelID))
		return
	}
	recordChannelHealthEventForRoute(c, probe.Group, probe.ModelName, probe.ChannelID, probe.Route, ChannelHealthSelectionHalfOpenRecoverySuperseded, 0, "")
	recordChannelHealthRuntimeEventForRoute(c, probe.Group, probe.ModelName, probe.ChannelID, probe.Route, ChannelHealthSelectionHalfOpenRecoverySuperseded, 0, "")
}

func channelHealthProbeMatchesRelayInfo(state *channelHealthRoutingState, probe channelHealthProbe, info *relaycommon.RelayInfo) bool {
	if state == nil || info == nil || info.ChannelMeta == nil || info.ChannelMeta.ChannelId != probe.ChannelID || info.ChannelHealthRoute == nil {
		return false
	}
	return channelHealthProbeRouteCompatible(*info.ChannelHealthRoute, probe.Route)
}

// ReleaseChannelHealthProbe releases a half-open lease when this request did
// not produce a health failure or a successful upstream result. It preserves
// the prior failure history so the next selected request remains a probe.
func ReleaseChannelHealthProbe(c *gin.Context, info *relaycommon.RelayInfo) {
	if info == nil || info.ChannelMeta == nil {
		return
	}
	ReleaseChannelHealthProbeForChannel(c, info.ChannelMeta.ChannelId)
}

// ReleaseChannelHealthProbeForChannel releases a pending probe before an
// upstream request is sent, such as when local channel setup fails.
func ReleaseChannelHealthProbeForChannel(c *gin.Context, channelID int) {
	state := channelHealthRouting(c)
	if state == nil || state.Probe == nil || channelID <= 0 {
		return
	}
	probe := *state.Probe
	if channelID != probe.ChannelID {
		return
	}
	rdb, ok := channelHealthRedisClient(c)
	if !ok {
		clearLocalChannelHealthProbe(state, probe.Token)
		return
	}
	_, _, probeKey, _ := channelHealthKeysForRoute(probe.Group, probe.Route)
	ctx, cancel := context.WithTimeout(context.Background(), channelHealthRedisTimeout)
	defer cancel()
	released, err := rdb.Eval(ctx, releaseChannelHealthProbeScript, []string{probeKey}, probe.Token).Int()
	if err != nil {
		common.SysError(fmt.Sprintf("channel health half-open release failed: channel_id=%d error=%v", probe.ChannelID, err))
		markChannelHealthUnavailable(c)
		clearLocalChannelHealthProbe(state, probe.Token)
		return
	}
	clearLocalChannelHealthProbe(state, probe.Token)
	if released == 1 {
		recordChannelHealthEventForRoute(c, probe.Group, probe.ModelName, probe.ChannelID, probe.Route, ChannelHealthSelectionHalfOpenReleased, 0, "")
		recordChannelHealthRuntimeEventForRoute(c, probe.Group, probe.ModelName, probe.ChannelID, probe.Route, ChannelHealthSelectionHalfOpenReleased, 0, "")
	}
}

// RenewChannelHealthProbe extends the half-open lease without changing the
// failure history. A late renewal from an expired token is ignored atomically.
func RenewChannelHealthProbe(c *gin.Context, info *relaycommon.RelayInfo) bool {
	state := channelHealthRouting(c)
	if state == nil || state.Probe == nil {
		return false
	}
	probe := *state.Probe
	if info != nil && (info.ChannelMeta == nil || info.ChannelMeta.ChannelId != probe.ChannelID) {
		return false
	}
	rdb, ok := channelHealthRedisClient(c)
	if !ok {
		return false
	}
	return renewChannelHealthProbeWithClient(rdb, probe)
}

func renewChannelHealthProbeWithClient(rdb *redis.Client, probe channelHealthProbe) bool {
	return renewChannelHealthProbeStatus(rdb, probe, getChannelHealthConfig().ProbeLease) == channelHealthProbeRenewed
}

type channelHealthProbeRenewResult uint8

const (
	channelHealthProbeRenewed channelHealthProbeRenewResult = iota + 1
	channelHealthProbeRenewLeaseMissing
	channelHealthProbeRenewRetryableError
)

func renewChannelHealthProbeStatus(rdb *redis.Client, probe channelHealthProbe, probeLease time.Duration) channelHealthProbeRenewResult {
	if rdb == nil || probe.Token == "" {
		return channelHealthProbeRenewLeaseMissing
	}
	if probeLease <= 0 {
		return channelHealthProbeRenewLeaseMissing
	}
	_, historyKey, probeKey, _ := channelHealthKeysForRoute(probe.Group, probe.Route)
	metadataKey := channelHealthMetadataKeyForRoute(probe.Group, probe.Route)
	historyTTL := probeLease * channelHealthProbeHistoryLeases
	ctx, cancel := context.WithTimeout(context.Background(), channelHealthRedisTimeout)
	defer cancel()
	digest := channelHealthDigestForRoute(probe.Group, probe.Route)
	result, err := rdb.Eval(ctx, renewChannelHealthProbeScript, []string{probeKey, historyKey, metadataKey, channelHealthRuntimeIndexKey}, probe.Token, probeLease.Milliseconds(), historyTTL.Milliseconds(), digest).Int()
	if err != nil {
		common.SysError(fmt.Sprintf("channel health half-open lease renewal failed: channel_id=%d error=%v", probe.ChannelID, err))
		return channelHealthProbeRenewRetryableError
	}
	if result != 1 {
		return channelHealthProbeRenewLeaseMissing
	}
	return channelHealthProbeRenewed
}

func startChannelHealthProbeLeaseRenewal(c *gin.Context) {
	if c == nil || c.Request == nil {
		return
	}
	state := channelHealthRouting(c)
	if state == nil || state.Probe == nil || state.Probe.Token == "" {
		return
	}
	rdb, ok := channelHealthRedisClient(c)
	if !ok {
		return
	}
	probe := *state.Probe
	probeLease := getChannelHealthConfig().ProbeLease
	renewalContext, cancel := context.WithCancel(c.Request.Context())
	renewalContext = context.WithValue(renewalContext, channelHealthProbeRenewalContextKey{}, probe.Token)
	state.LeaseRenewalMu.Lock()
	if state.LeaseRenewals == nil {
		state.LeaseRenewals = make(map[string]context.CancelFunc)
	}
	if state.LeaseRenewalState == nil {
		state.LeaseRenewalState = make(map[string]*channelHealthProbeRenewal)
	}
	if _, exists := state.LeaseRenewals[probe.Token]; exists {
		state.LeaseRenewalMu.Unlock()
		cancel()
		return
	}
	previousRequest := c.Request
	wrappedRequest := previousRequest.WithContext(renewalContext)
	c.Request = wrappedRequest
	state.LeaseRenewals[probe.Token] = cancel
	state.LeaseRenewalState[probe.Token] = &channelHealthProbeRenewal{
		cancel:          cancel,
		owner:           c,
		wrappedRequest:  wrappedRequest,
		previousRequest: previousRequest,
	}
	state.LeaseRenewalMu.Unlock()
	interval := probeLease / 2
	if interval < 250*time.Millisecond {
		interval = 250 * time.Millisecond
	}
	go func() {
		defer func() {
			state.LeaseRenewalMu.Lock()
			delete(state.LeaseRenewals, probe.Token)
			state.LeaseRenewalMu.Unlock()
			cancel()
		}()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-renewalContext.Done():
				return
			case <-ticker.C:
				switch renewChannelHealthProbeStatus(rdb, probe, probeLease) {
				case channelHealthProbeRenewed:
					state.LeaseRenewalMu.Lock()
					if entry := state.LeaseRenewalState[probe.Token]; entry != nil {
						entry.failures = 0
					}
					state.LeaseRenewalMu.Unlock()
				case channelHealthProbeRenewLeaseMissing:
					state.LeaseRenewalMu.Lock()
					entry := state.LeaseRenewalState[probe.Token]
					if entry != nil {
						entry.terminated = true
					}
					state.LeaseRenewalMu.Unlock()
					// A lost probe lease means another request may already own a new
					// half-open probe. Stop this request instead of allowing a stale
					// probe to continue without ownership. The renewal state entry may
					// already have been removed by request cleanup, but cancellation is
					// idempotent and must still reach the wrapped request in that race.
					cancel()
					return
				case channelHealthProbeRenewRetryableError:
					state.LeaseRenewalMu.Lock()
					entry := state.LeaseRenewalState[probe.Token]
					if entry == nil {
						state.LeaseRenewalMu.Unlock()
						return
					}
					entry.failures++
					threshold := entry.failures >= channelHealthProbeRenewFailureLimit
					if threshold {
						entry.terminated = true
					}
					state.LeaseRenewalMu.Unlock()
					if threshold {
						common.SysError(fmt.Sprintf("channel health probe renewal exceeded failure threshold: channel_id=%d", probe.ChannelID))
						cancel()
						return
					}
				}
			}
		}
	}()
}

func channelHealthProbeRenewalTerminated(state *channelHealthRoutingState, token string) bool {
	if state == nil || token == "" {
		return false
	}
	state.LeaseRenewalMu.Lock()
	defer state.LeaseRenewalMu.Unlock()
	entry := state.LeaseRenewalState[token]
	return entry != nil && entry.terminated
}

// channelHealthProbeRenewalParentRequest returns the request that existed
// before the probe renewal worker installed its cancellable wrapper. A later
// attempt-scoped wrapper must use this request as its restoration root while
// still deriving its context from the active probe wrapper so lease renewal
// cancellation propagates to the in-flight operation.
func channelHealthProbeRenewalParentRequest(c *gin.Context) *http.Request {
	if c == nil || c.Request == nil || c.Request.Context() == nil {
		return nil
	}
	token, ok := c.Request.Context().Value(channelHealthProbeRenewalContextKey{}).(string)
	if !ok || token == "" {
		return nil
	}
	state := channelHealthRouting(c)
	if state == nil {
		return nil
	}
	state.LeaseRenewalMu.Lock()
	defer state.LeaseRenewalMu.Unlock()
	entry := state.LeaseRenewalState[token]
	if entry == nil {
		return nil
	}
	return entry.previousRequest
}

func stopChannelHealthProbeLeaseRenewal(state *channelHealthRoutingState, token string) {
	if state == nil || token == "" {
		return
	}
	state.LeaseRenewalMu.Lock()
	cancel := state.LeaseRenewals[token]
	entry := state.LeaseRenewalState[token]
	delete(state.LeaseRenewals, token)
	delete(state.LeaseRenewalState, token)
	state.LeaseRenewalMu.Unlock()
	if entry != nil && entry.owner != nil && entry.previousRequest != nil &&
		channelHealthProbeRenewalRequestOwnedBy(entry.owner.Request, entry.wrappedRequest, token) {
		entry.owner.Request = entry.previousRequest
	}
	if cancel != nil {
		cancel()
	}
}

func channelHealthProbeRenewalRequestOwnedBy(current *http.Request, wrapped *http.Request, token string) bool {
	if current == nil || token == "" {
		return false
	}
	if current == wrapped {
		return true
	}
	// A later attempt-scoped wrapper (currently the channel concurrency lease)
	// inherits this marker through context.WithCancel/WithValue. Do not unwrap
	// an unrelated request that replaced the context chain entirely.
	value, ok := current.Context().Value(channelHealthProbeRenewalContextKey{}).(string)
	return ok && value == token
}

func clearLocalChannelHealthProbe(state *channelHealthRoutingState, token string) {
	if state == nil || token == "" {
		return
	}
	stopChannelHealthProbeLeaseRenewal(state, token)
	if state.Probe != nil && state.Probe.Token == token {
		state.Probe = nil
	}
}
