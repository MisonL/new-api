package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

const (
	channelConcurrencyRedisPrefix        = "new-api:channel-concurrency:v1"
	channelConcurrencyRedisTimeout       = 250 * time.Millisecond
	channelConcurrencyDefaultLeaseTTL    = 30 * time.Second
	channelConcurrencyMinLeaseTTL        = 5 * time.Second
	channelConcurrencyMaxLeaseTTL        = 10 * time.Minute
	channelConcurrencyDefaultReselectCap = 5
	channelConcurrencyAcquireAttempts    = 2
	channelConcurrencyAcquireRetryDelay  = 10 * time.Millisecond
	channelConcurrencyRenewFailureLimit  = 3
	channelConcurrencyReleaseAttempts    = 2
	channelConcurrencyUnavailableLogGap  = time.Minute
)

var (
	ErrChannelConcurrencyExceeded       = errors.New("channel concurrency limit reached")
	ErrChannelConcurrencyUnavailable    = errors.New("channel concurrency coordinator unavailable")
	ErrChannelConcurrencyInvalid        = errors.New("channel concurrency limit is invalid")
	errChannelConcurrencyStateUncertain = errors.New("channel concurrency state is uncertain")
)

const channelConcurrencyAcquireScript = `
local redis_time = redis.call("TIME")
local now = tonumber(redis_time[1]) * 1000 + math.floor(tonumber(redis_time[2]) / 1000)
local max_concurrency = tonumber(ARGV[1])
local ttl_ms = tonumber(ARGV[2])
redis.call("ZREMRANGEBYSCORE", KEYS[1], "-inf", now)
local current = redis.call("ZCARD", KEYS[1])
if current >= max_concurrency then
  return 0
end
redis.call("ZADD", KEYS[1], now + ttl_ms, ARGV[3])
redis.call("PEXPIRE", KEYS[1], ttl_ms + 1000)
return 1
`

const channelConcurrencyReleaseScript = `
return redis.call("ZREM", KEYS[1], ARGV[1])
`

const channelConcurrencyRenewScript = `
local score = redis.call("ZSCORE", KEYS[1], ARGV[1])
if score == false then
  return 0
end
local redis_time = redis.call("TIME")
local now = tonumber(redis_time[1]) * 1000 + math.floor(tonumber(redis_time[2]) / 1000)
if tonumber(score) <= now then
  redis.call("ZREM", KEYS[1], ARGV[1])
  return 0
end
local ttl_ms = tonumber(ARGV[2])
redis.call("ZADD", KEYS[1], now + ttl_ms, ARGV[1])
redis.call("PEXPIRE", KEYS[1], ttl_ms + 1000)
return 1
`

// channelConcurrencySnapshotScript uses Redis' clock for both expiry cleanup
// and the count read. Using the application host clock here would make the
// administrator snapshot disagree with Acquire/Renew on hosts whose clocks
// are skewed relative to Redis.
const channelConcurrencySnapshotScript = `
local redis_time = redis.call("TIME")
local now = tonumber(redis_time[1]) * 1000 + math.floor(tonumber(redis_time[2]) / 1000)
redis.call("ZREMRANGEBYSCORE", KEYS[1], "-inf", now)
return redis.call("ZCARD", KEYS[1])
`

type localChannelConcurrencyState struct {
	mu     sync.Mutex
	leases map[int]map[string]time.Time
}

var localChannelConcurrency = localChannelConcurrencyState{
	leases: make(map[int]map[string]time.Time),
}

var channelConcurrencyUnavailableLogAt atomic.Int64
var channelConcurrencyTokenSequence atomic.Uint64

var channelConcurrencyMetrics struct {
	acquireSuccess     atomic.Uint64
	acquireRejected    atomic.Uint64
	acquireUnavailable atomic.Uint64
	reselectSkipped    atomic.Uint64
	strict429          atomic.Uint64
	redisUnavailable   atomic.Uint64
	renewFailure       atomic.Uint64
	releaseFailure     atomic.Uint64
	saturatedEvents    atomic.Uint64
	currentLocal       atomic.Uint64
}

// ChannelConcurrencyMetricsSnapshot exposes bounded process counters for the
// administrator runtime view. Distributed Redis lease counts are queried per
// channel; currentLocal is deliberately labelled as process-local.
func ChannelConcurrencyMetricsSnapshot() dto.ChannelConcurrencyMetrics {
	return dto.ChannelConcurrencyMetrics{
		AcquireSuccess:     channelConcurrencyMetrics.acquireSuccess.Load(),
		AcquireRejected:    channelConcurrencyMetrics.acquireRejected.Load(),
		AcquireUnavailable: channelConcurrencyMetrics.acquireUnavailable.Load(),
		ReselectSkipped:    channelConcurrencyMetrics.reselectSkipped.Load(),
		Strict429:          channelConcurrencyMetrics.strict429.Load(),
		RedisUnavailable:   channelConcurrencyMetrics.redisUnavailable.Load(),
		RenewFailure:       channelConcurrencyMetrics.renewFailure.Load(),
		ReleaseFailure:     channelConcurrencyMetrics.releaseFailure.Load(),
		SaturatedEvents:    channelConcurrencyMetrics.saturatedEvents.Load(),
		CurrentLeasesLocal: channelConcurrencyMetrics.currentLocal.Load(),
	}
}

func RecordChannelConcurrencyReselect() {
	channelConcurrencyMetrics.reselectSkipped.Add(1)
}

func RecordChannelConcurrencyStrict429() {
	channelConcurrencyMetrics.strict429.Add(1)
}

// ChannelConcurrencyLease is an idempotent reservation for one channel-level
// in-flight request. Release is safe from defer, retry, cancellation and panic
// paths and never decrements a counter more than once.
type ChannelConcurrencyLease struct {
	channelID int
	token     string
	redis     bool
	local     bool
	// redisCleanup is set for a local fallback entered after a Redis Eval
	// error. The Eval may have committed remotely before the client observed
	// the error, so Release must still attempt to remove that token.
	redisCleanup bool
	ttl          time.Duration
	ctx          context.Context
	cancel       context.CancelFunc
	owner        *gin.Context
	request      *http.Request
	rootRequest  *http.Request
	previous     *http.Request
	released     sync.Once
	closed       atomic.Bool
	renewFail    atomic.Int32
	renewTerm    atomic.Bool
}

type ChannelConcurrencySnapshot struct {
	ChannelID       int  `json:"channel_id"`
	MaxConcurrency  int  `json:"max_concurrency"`
	CurrentInFlight int  `json:"current_in_flight"`
	AvailableSlots  int  `json:"available_slots"`
	RedisAvailable  bool `json:"redis_available"`
	// Source identifies the counter used for this snapshot. Redis is the only
	// cluster-wide source; local_fallback is process-local and unconfirmed.
	Source    string `json:"source"`
	Confirmed bool   `json:"confirmed"`
}

type channelConcurrencyRenewResult uint8

const (
	channelConcurrencyRenewed channelConcurrencyRenewResult = iota + 1
	channelConcurrencyRenewLeaseMissing
	channelConcurrencyRenewRetryableError
)

func channelConcurrencyLeaseTTL() time.Duration {
	seconds := common.GetEnvOrDefault("CHANNEL_CONCURRENCY_LEASE_TTL_SECONDS", int(channelConcurrencyDefaultLeaseTTL/time.Second))
	if seconds <= 0 {
		return channelConcurrencyDefaultLeaseTTL
	}
	ttl := time.Duration(seconds) * time.Second
	if ttl < channelConcurrencyMinLeaseTTL {
		return channelConcurrencyMinLeaseTTL
	}
	if ttl > channelConcurrencyMaxLeaseTTL {
		return channelConcurrencyMaxLeaseTTL
	}
	return ttl
}

func channelConcurrencyRenewInterval(ttl time.Duration) time.Duration {
	// Keep the failure threshold strictly inside one lease lifetime. With three
	// consecutive failures, a TTL/(limit+1) cadence cancels at roughly 75% of
	// the TTL, leaving room for scheduler delay and Redis timeout before another
	// worker can observe the expired reservation.
	interval := ttl / (channelConcurrencyRenewFailureLimit + 1)
	if interval < time.Second {
		interval = time.Second
	}
	return interval
}

func channelConcurrencyStrict() bool {
	return common.GetEnvOrDefaultBool("CHANNEL_CONCURRENCY_STRICT", true)
}

func ChannelConcurrencyReselectLimit() int {
	limit := common.GetEnvOrDefault("CHANNEL_CONCURRENCY_RESELECT_LIMIT", channelConcurrencyDefaultReselectCap)
	if limit <= 0 {
		return channelConcurrencyDefaultReselectCap
	}
	return limit
}

func channelConcurrencyRedisAvailable() bool {
	return common.RedisEnabled && common.RDB != nil
}

func channelConcurrencyKey(channelID int) string {
	return channelConcurrencyRedisPrefix + ":" + strconv.Itoa(channelID) + ":leases"
}

func newChannelConcurrencyToken() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return hex.EncodeToString(raw[:])
	}
	return fmt.Sprintf("local-%d", channelConcurrencyTokenSequence.Add(1))
}

func logChannelConcurrencyRedisUnavailable(reason string) {
	now := time.Now().UnixNano()
	last := channelConcurrencyUnavailableLogAt.Load()
	if last != 0 && now-last < channelConcurrencyUnavailableLogGap.Nanoseconds() {
		return
	}
	if !channelConcurrencyUnavailableLogAt.CompareAndSwap(last, now) {
		return
	}
	common.SysError("channel concurrency Redis unavailable: " + reason)
}

func cleanupLocalChannelConcurrencyLocked(now time.Time, channelID int) {
	leases := localChannelConcurrency.leases[channelID]
	for token, expiresAt := range leases {
		if !expiresAt.After(now) {
			delete(leases, token)
			channelConcurrencyMetrics.currentLocal.Add(^uint64(0))
		}
	}
	if len(leases) == 0 {
		delete(localChannelConcurrency.leases, channelID)
	}
}

func acquireLocalChannelConcurrency(channelID int, max int, token string, ttl time.Duration) bool {
	now := time.Now()
	localChannelConcurrency.mu.Lock()
	defer localChannelConcurrency.mu.Unlock()
	cleanupLocalChannelConcurrencyLocked(now, channelID)
	leases := localChannelConcurrency.leases[channelID]
	if len(leases) >= max {
		return false
	}
	if leases == nil {
		leases = make(map[string]time.Time)
		localChannelConcurrency.leases[channelID] = leases
	}
	leases[token] = now.Add(ttl)
	channelConcurrencyMetrics.currentLocal.Add(1)
	return true
}

func releaseLocalChannelConcurrency(channelID int, token string) {
	localChannelConcurrency.mu.Lock()
	defer localChannelConcurrency.mu.Unlock()
	if leases := localChannelConcurrency.leases[channelID]; leases != nil {
		if _, exists := leases[token]; exists {
			delete(leases, token)
			channelConcurrencyMetrics.currentLocal.Add(^uint64(0))
		}
		if len(leases) == 0 {
			delete(localChannelConcurrency.leases, channelID)
		}
	}
}

func renewLocalChannelConcurrency(channelID int, token string, ttl time.Duration) bool {
	localChannelConcurrency.mu.Lock()
	defer localChannelConcurrency.mu.Unlock()
	leases := localChannelConcurrency.leases[channelID]
	if leases == nil {
		return false
	}
	if expiresAt, ok := leases[token]; !ok || !expiresAt.After(time.Now()) {
		return false
	} else {
		leases[token] = time.Now().Add(ttl)
		return true
	}
}

func AcquireChannelConcurrency(c *gin.Context, channel *model.Channel) (*ChannelConcurrencyLease, error) {
	if channel == nil || channel.MaxConcurrency == 0 {
		return nil, nil
	}
	if channel.MaxConcurrency < 0 {
		return nil, ErrChannelConcurrencyInvalid
	}
	parentContext := channelConcurrencyRequestContext(c)
	if err := parentContext.Err(); err != nil {
		return nil, err
	}
	ttl := channelConcurrencyLeaseTTL()
	token := newChannelConcurrencyToken()
	if channelConcurrencyRedisAvailable() {
		reserved, err := acquireRedisChannelConcurrency(parentContext, channel.Id, channel.MaxConcurrency, token, ttl)
		if err == nil {
			if reserved == 0 {
				channelConcurrencyMetrics.acquireRejected.Add(1)
				channelConcurrencyMetrics.saturatedEvents.Add(1)
				return nil, ErrChannelConcurrencyExceeded
			}
			channelConcurrencyMetrics.acquireSuccess.Add(1)
			lease := newChannelConcurrencyLease(c, channel.Id, token, true, ttl)
			return lease, nil
		}
		if parentErr := parentContext.Err(); parentErr != nil {
			return nil, parentErr
		}
		// A failed Eval may have committed the remote reservation. If its cleanup
		// also failed, the distributed state is unknown and a local fallback would
		// create a second lease for the same request. Fail closed in every mode.
		if errors.Is(err, errChannelConcurrencyStateUncertain) {
			return nil, ErrChannelConcurrencyUnavailable
		}
		if channelConcurrencyStrict() {
			return nil, ErrChannelConcurrencyUnavailable
		}
	} else if channelConcurrencyStrict() {
		channelConcurrencyMetrics.acquireUnavailable.Add(1)
		channelConcurrencyMetrics.redisUnavailable.Add(1)
		logChannelConcurrencyRedisUnavailable("client is not configured")
		return nil, ErrChannelConcurrencyUnavailable
	}
	if !acquireLocalChannelConcurrency(channel.Id, channel.MaxConcurrency, token, ttl) {
		channelConcurrencyMetrics.acquireRejected.Add(1)
		channelConcurrencyMetrics.saturatedEvents.Add(1)
		return nil, ErrChannelConcurrencyExceeded
	}
	channelConcurrencyMetrics.acquireSuccess.Add(1)
	return newChannelConcurrencyLeaseWithOptions(c, channel.Id, token, false, true, channelConcurrencyRedisAvailable(), ttl), nil
}

// acquireRedisChannelConcurrency retries a coordinator error after cleaning up
// the possibly committed token. Redis can return an error after executing the
// Lua script (for example during a connection hiccup); treating that single
// response as a definitive outage needlessly turns an otherwise healthy
// request into a strict-mode 503. The retry count is deliberately small and
// bounded so a real outage still fails closed promptly.
func acquireRedisChannelConcurrency(parentContext context.Context, channelID int, maxConcurrency int, token string, ttl time.Duration) (int, error) {
	if parentContext == nil {
		parentContext = context.Background()
	}
	var lastErr error
	for attempt := 0; attempt < channelConcurrencyAcquireAttempts; attempt++ {
		if err := parentContext.Err(); err != nil {
			return 0, err
		}
		if !channelConcurrencyRedisAvailable() {
			lastErr = errors.New("client is not configured")
			break
		}
		ctx, cancel := context.WithTimeout(parentContext, channelConcurrencyRedisTimeout)
		reserved, err := common.RDB.Eval(ctx, channelConcurrencyAcquireScript, []string{channelConcurrencyKey(channelID)}, maxConcurrency, ttl.Milliseconds(), token).Int()
		cancel()
		if err == nil {
			return reserved, nil
		}
		lastErr = err
		channelConcurrencyMetrics.acquireUnavailable.Add(1)
		channelConcurrencyMetrics.redisUnavailable.Add(1)
		logChannelConcurrencyRedisUnavailable(err.Error())

		// A failed Eval may have committed the token. Do not issue another
		// reservation until the uncertain token has been removed. If cleanup also
		// fails, fail closed rather than risking a double reservation.
		if cleanupErr := releaseChannelConcurrencyRedisLease(channelID, token); cleanupErr != nil {
			channelConcurrencyMetrics.releaseFailure.Add(1)
			channelConcurrencyMetrics.redisUnavailable.Add(1)
			logChannelConcurrencyRedisUnavailable(cleanupErr.Error())
			return 0, fmt.Errorf("%w: %v", errChannelConcurrencyStateUncertain, cleanupErr)
		}
		if attempt+1 < channelConcurrencyAcquireAttempts {
			timer := time.NewTimer(channelConcurrencyAcquireRetryDelay)
			select {
			case <-parentContext.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				if stopErr := parentContext.Err(); stopErr != nil {
					return 0, stopErr
				}
				return 0, lastErr
			case <-timer.C:
			}
		}
	}
	return 0, lastErr
}

func newChannelConcurrencyLease(c *gin.Context, channelID int, token string, useRedis bool, ttl time.Duration) *ChannelConcurrencyLease {
	return newChannelConcurrencyLeaseWithOptions(c, channelID, token, useRedis, !useRedis, useRedis, ttl)
}

func newChannelConcurrencyLeaseWithOptions(c *gin.Context, channelID int, token string, useRedis bool, useLocal bool, redisCleanup bool, ttl time.Duration) *ChannelConcurrencyLease {
	rootRequest := channelConcurrencyRootRequest(c)
	requestContext := channelConcurrencyRequestContext(c)
	var request *http.Request
	var previous *http.Request
	if c != nil && c.Request != nil && c.Request.Context() != nil {
		previous = c.Request
		if rootRequest == nil {
			rootRequest = previous
			common.SetContextKey(c, constant.ContextKeyChannelConcurrencyRootRequest, rootRequest)
		}
		// Keep the active request context selected above. When a half-open
		// channel-health probe is renewing, that context carries the probe's
		// cancellation marker; replacing it with rootRequest.Context() would
		// let a lost probe continue consuming a concurrency slot and sending
		// upstream traffic after ownership had expired. rootRequest is used
		// only as the request restoration target below.
		if requestContext == nil {
			requestContext = previous.Context()
		}
	}
	renewContext, cancel := context.WithCancel(requestContext)
	if rootRequest != nil {
		request = rootRequest.WithContext(renewContext)
	} else if previous != nil {
		request = previous.WithContext(renewContext)
	}
	if request != nil && c != nil {
		c.Request = request
	}
	lease := &ChannelConcurrencyLease{
		channelID:    channelID,
		token:        token,
		redis:        useRedis,
		local:        useLocal,
		redisCleanup: redisCleanup,
		ttl:          ttl,
		ctx:          renewContext,
		cancel:       cancel,
		owner:        c,
		request:      request,
		rootRequest:  rootRequest,
		previous:     previous,
	}
	go lease.renewLoop()
	return lease
}

func channelConcurrencyRequestContext(c *gin.Context) context.Context {
	// A half-open health probe owns a cancellable wrapper around the request.
	// Preserve that wrapper as the parent of the attempt lease so a lost probe
	// lease still terminates the in-flight operation. The original request is
	// recovered separately by channelConcurrencyRootRequest for restoration.
	if c != nil && c.Request != nil && c.Request.Context() != nil {
		if token, ok := c.Request.Context().Value(channelHealthProbeRenewalContextKey{}).(string); ok {
			// Once the probe renewal worker has terminated, the wrapper is
			// intentionally canceled to stop the in-flight attempt. Cleanup and a
			// bounded retry must then use the original request context instead of
			// inheriting that terminal cancellation.
			if state := channelHealthRouting(c); state != nil && channelHealthProbeRenewalTerminated(state, token) {
				if root := channelHealthProbeRenewalParentRequest(c); root != nil && root.Context() != nil {
					return root.Context()
				}
			}
			return c.Request.Context()
		}
	}
	if root := channelConcurrencyRootRequest(c); root != nil && root.Context() != nil {
		return root.Context()
	}
	if c != nil && c.Request != nil && c.Request.Context() != nil {
		return c.Request.Context()
	}
	return context.Background()
}

// channelConcurrencyRootRequest returns the original request captured before
// the first concurrency lease wrapped c.Request. A retry must not derive its
// lease context from a previous attempt wrapper: releasing that wrapper would
// otherwise cancel every later lease through context parentage.
func channelConcurrencyRootRequest(c *gin.Context) *http.Request {
	if c == nil {
		return nil
	}
	if root := channelHealthProbeRenewalParentRequest(c); root != nil {
		common.SetContextKey(c, constant.ContextKeyChannelConcurrencyRootRequest, root)
		return root
	}
	if root, ok := common.GetContextKeyType[*http.Request](c, constant.ContextKeyChannelConcurrencyRootRequest); ok && root != nil {
		return root
	}
	if c.Request == nil || c.Request.Context() == nil {
		return nil
	}
	root := c.Request
	common.SetContextKey(c, constant.ContextKeyChannelConcurrencyRootRequest, root)
	return root
}

func (lease *ChannelConcurrencyLease) renewLoop() {
	if lease == nil {
		return
	}
	ticker := time.NewTicker(channelConcurrencyRenewInterval(lease.ttl))
	defer ticker.Stop()
	for {
		select {
		case <-lease.ctx.Done():
			return
		case <-ticker.C:
			switch lease.renewStatus() {
			case channelConcurrencyRenewed:
				lease.renewFail.Store(0)
			case channelConcurrencyRenewLeaseMissing:
				// Losing an active lease is not safe to ignore: the request may
				// otherwise continue after its slot has expired and exceed the
				// configured limit. Cancel the child request context and release
				// any remaining local/Redis state immediately.
				lease.renewTerm.Store(true)
				lease.cancel()
				lease.releaseResources()
				return
			case channelConcurrencyRenewRetryableError:
				if lease.renewFail.Add(1) >= channelConcurrencyRenewFailureLimit {
					common.SysError(fmt.Sprintf("channel concurrency lease renewal exceeded failure threshold: channel_id=%d", lease.channelID))
					lease.renewTerm.Store(true)
					lease.cancel()
					lease.releaseResources()
					return
				}
			}
		}
	}
}

func (lease *ChannelConcurrencyLease) renew() bool {
	return lease.renewStatus() == channelConcurrencyRenewed
}

func (lease *ChannelConcurrencyLease) renewStatus() channelConcurrencyRenewResult {
	if lease == nil || lease.releasedDone() {
		return channelConcurrencyRenewLeaseMissing
	}
	if lease.ctx != nil && lease.ctx.Err() != nil {
		return channelConcurrencyRenewLeaseMissing
	}
	if !lease.redis {
		ok := renewLocalChannelConcurrency(lease.channelID, lease.token, lease.ttl)
		if !ok {
			channelConcurrencyMetrics.renewFailure.Add(1)
			return channelConcurrencyRenewLeaseMissing
		}
		return channelConcurrencyRenewed
	}
	if !channelConcurrencyRedisAvailable() {
		channelConcurrencyMetrics.renewFailure.Add(1)
		channelConcurrencyMetrics.redisUnavailable.Add(1)
		logChannelConcurrencyRedisUnavailable("client is not configured during renewal")
		return channelConcurrencyRenewRetryableError
	}
	ctx, cancel := context.WithTimeout(lease.ctx, channelConcurrencyRedisTimeout)
	result, err := common.RDB.Eval(ctx, channelConcurrencyRenewScript, []string{channelConcurrencyKey(lease.channelID)}, lease.token, lease.ttl.Milliseconds()).Int()
	cancel()
	if err != nil {
		channelConcurrencyMetrics.renewFailure.Add(1)
		channelConcurrencyMetrics.redisUnavailable.Add(1)
		logChannelConcurrencyRedisUnavailable(err.Error())
		return channelConcurrencyRenewRetryableError
	}
	if result == 0 {
		return channelConcurrencyRenewLeaseMissing
	}
	return channelConcurrencyRenewed
}

func (lease *ChannelConcurrencyLease) releasedDone() bool {
	return lease == nil || lease.closed.Load()
}

func (lease *ChannelConcurrencyLease) Release() {
	if lease == nil {
		return
	}
	lease.releaseResources()
	lease.restoreOwnerRequest()
}

// releaseResources performs idempotent reservation cleanup without touching
// c.Request. The renewal goroutine can invoke it while the handler is still
// reading c.Request; request restoration stays on the handler-side Release.
func (lease *ChannelConcurrencyLease) releaseResources() {
	if lease == nil {
		return
	}
	lease.released.Do(func() {
		lease.closed.Store(true)
		if lease.cancel != nil {
			lease.cancel()
		}
		if lease.local {
			releaseLocalChannelConcurrency(lease.channelID, lease.token)
		}
		if !lease.redis && !lease.redisCleanup {
			return
		}
		if !channelConcurrencyRedisAvailable() {
			channelConcurrencyMetrics.releaseFailure.Add(1)
			channelConcurrencyMetrics.redisUnavailable.Add(1)
			logChannelConcurrencyRedisUnavailable("client is not configured during release")
			return
		}
		releaseErr := releaseChannelConcurrencyRedisLease(lease.channelID, lease.token)
		if releaseErr != nil {
			channelConcurrencyMetrics.releaseFailure.Add(1)
			channelConcurrencyMetrics.redisUnavailable.Add(1)
			logChannelConcurrencyRedisUnavailable(releaseErr.Error())
		}
	})
}

func (lease *ChannelConcurrencyLease) restoreOwnerRequest() {
	if lease == nil || lease.owner == nil || lease.owner.Request != lease.request {
		return
	}
	// Restore the original request, never a previous attempt wrapper whose
	// context may already have been cancelled by its own Release call.
	if lease.rootRequest != nil {
		lease.owner.Request = lease.rootRequest
		return
	}
	if lease.previous != nil {
		lease.owner.Request = lease.previous
	}
}

func releaseChannelConcurrencyRedisLease(channelID int, token string) error {
	if !channelConcurrencyRedisAvailable() {
		return errors.New("client is not configured")
	}
	var releaseErr error
	for attempt := 0; attempt < channelConcurrencyReleaseAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), channelConcurrencyRedisTimeout)
		_, releaseErr = common.RDB.Eval(ctx, channelConcurrencyReleaseScript, []string{channelConcurrencyKey(channelID)}, token).Int()
		cancel()
		if releaseErr == nil {
			return nil
		}
	}
	return releaseErr
}

func SetChannelConcurrencyLease(c *gin.Context, lease *ChannelConcurrencyLease) {
	if c == nil {
		return
	}
	if previous, ok := common.GetContextKeyType[*ChannelConcurrencyLease](c, constant.ContextKeyChannelConcurrencyLease); ok && previous != nil && previous != lease {
		// A Gin context is reused across retries. Replacing a lease without
		// releasing it would keep the previous channel occupied until TTL.
		previous.Release()
	}
	common.SetContextKey(c, constant.ContextKeyChannelConcurrencyLease, lease)
}

func ReleaseChannelConcurrency(c *gin.Context) {
	if c == nil {
		return
	}
	lease, ok := common.GetContextKeyType[*ChannelConcurrencyLease](c, constant.ContextKeyChannelConcurrencyLease)
	if !ok || lease == nil {
		return
	}
	lease.Release()
	common.SetContextKey(c, constant.ContextKeyChannelConcurrencyLease, (*ChannelConcurrencyLease)(nil))
}

func ExcludeChannelDueToConcurrency(c *gin.Context, channelID int) {
	if c == nil || channelID <= 0 {
		return
	}
	excluded := ChannelConcurrencyExcludedChannels(c)
	if excluded == nil {
		excluded = make(map[int]struct{})
	}
	excluded[channelID] = struct{}{}
	common.SetContextKey(c, constant.ContextKeyChannelConcurrencyExcluded, excluded)
}

func ChannelConcurrencyExcludedChannels(c *gin.Context) map[int]struct{} {
	if c == nil {
		return nil
	}
	excluded, ok := common.GetContextKeyType[map[int]struct{}](c, constant.ContextKeyChannelConcurrencyExcluded)
	if !ok || len(excluded) == 0 {
		return nil
	}
	copyExcluded := make(map[int]struct{}, len(excluded))
	for channelID := range excluded {
		copyExcluded[channelID] = struct{}{}
	}
	return copyExcluded
}

func GetChannelConcurrencySnapshot(channelID int, maxConcurrency int) ChannelConcurrencySnapshot {
	snapshot := ChannelConcurrencySnapshot{ChannelID: channelID, MaxConcurrency: maxConcurrency, RedisAvailable: channelConcurrencyRedisAvailable(), Source: "disabled", Confirmed: true}
	if channelID <= 0 || maxConcurrency <= 0 {
		return snapshot
	}
	snapshot.Source = "local_fallback"
	snapshot.Confirmed = false
	if snapshot.RedisAvailable {
		ctx, cancel := context.WithTimeout(context.Background(), channelConcurrencyRedisTimeout)
		defer cancel()
		count, err := common.RDB.Eval(ctx, channelConcurrencySnapshotScript, []string{channelConcurrencyKey(channelID)}).Int()
		if err == nil {
			snapshot.Source = "redis"
			snapshot.Confirmed = true
			snapshot.CurrentInFlight = count
			snapshot.AvailableSlots = maxConcurrency - snapshot.CurrentInFlight
			if snapshot.AvailableSlots < 0 {
				snapshot.AvailableSlots = 0
			}
			return snapshot
		}
		// A failed cleanup or read means the Redis count is not a reliable
		// snapshot. Fall through to the explicitly unconfirmed process-local view.
		snapshot.RedisAvailable = false
	}
	localChannelConcurrency.mu.Lock()
	cleanupLocalChannelConcurrencyLocked(time.Now(), channelID)
	snapshot.CurrentInFlight = len(localChannelConcurrency.leases[channelID])
	localChannelConcurrency.mu.Unlock()
	snapshot.AvailableSlots = maxConcurrency - snapshot.CurrentInFlight
	if snapshot.AvailableSlots < 0 {
		snapshot.AvailableSlots = 0
	}
	return snapshot
}

// ResetChannelConcurrencyStateForTest clears only local reservations. It is
// intentionally unexported from production callers; tests in this package use
// it to prove release and TTL behavior without touching application data.
func resetChannelConcurrencyStateForTest() {
	localChannelConcurrency.mu.Lock()
	localChannelConcurrency.leases = make(map[int]map[string]time.Time)
	localChannelConcurrency.mu.Unlock()
	channelConcurrencyMetrics.currentLocal.Store(0)
}
