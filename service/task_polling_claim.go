package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

const (
	taskPollingClaimRedisPrefix       = "new-api:task-polling-claim:v1"
	taskPollingClaimRedisTimeout      = 250 * time.Millisecond
	taskPollingClaimDefaultLeaseTTL   = 60 * time.Second
	taskPollingClaimMinLeaseTTL       = 5 * time.Second
	taskPollingClaimMaxLeaseTTL       = 30 * time.Minute
	taskPollingClaimRenewFailureLimit = 3
	taskPollingClaimLogGap            = time.Minute
)

var (
	ErrTaskPollingClaimBusy        = errors.New("task polling claim is held by another worker")
	ErrTaskPollingClaimUnavailable = errors.New("task polling claim coordinator unavailable")
)

const taskPollingClaimReleaseScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`

const taskPollingClaimRenewScript = `
if redis.call("GET", KEYS[1]) ~= ARGV[1] then
  return 0
end
redis.call("PEXPIRE", KEYS[1], ARGV[2])
return 1
`

type TaskPollingClaimMetrics struct {
	AcquireSuccess     uint64 `json:"acquire_success"`
	AcquireBusy        uint64 `json:"acquire_busy"`
	AcquireUnavailable uint64 `json:"acquire_unavailable"`
	RenewFailure       uint64 `json:"renew_failure"`
	LeaseLost          uint64 `json:"lease_lost"`
	ReleaseFailure     uint64 `json:"release_failure"`
	LocalFallback      uint64 `json:"local_fallback"`
}

var taskPollingClaimMetrics struct {
	acquireSuccess     atomic.Uint64
	acquireBusy        atomic.Uint64
	acquireUnavailable atomic.Uint64
	renewFailure       atomic.Uint64
	leaseLost          atomic.Uint64
	releaseFailure     atomic.Uint64
	localFallback      atomic.Uint64
}

var taskPollingClaimUnavailableLogAt atomic.Int64

type localTaskPollingClaimState struct {
	mu     sync.Mutex
	leases map[string]localTaskPollingClaimLease
}

type localTaskPollingClaimLease struct {
	token     string
	expiresAt time.Time
}

var localTaskPollingClaims = localTaskPollingClaimState{leases: make(map[string]localTaskPollingClaimLease)}

// TaskPollingClaimMetricsSnapshot exposes process-local counters for bounded
// operator diagnostics. Redis is still the source of truth for cross-instance
// exclusion; LocalFallback is explicitly unconfirmed cluster coordination.
func TaskPollingClaimMetricsSnapshot() TaskPollingClaimMetrics {
	return TaskPollingClaimMetrics{
		AcquireSuccess:     taskPollingClaimMetrics.acquireSuccess.Load(),
		AcquireBusy:        taskPollingClaimMetrics.acquireBusy.Load(),
		AcquireUnavailable: taskPollingClaimMetrics.acquireUnavailable.Load(),
		RenewFailure:       taskPollingClaimMetrics.renewFailure.Load(),
		LeaseLost:          taskPollingClaimMetrics.leaseLost.Load(),
		ReleaseFailure:     taskPollingClaimMetrics.releaseFailure.Load(),
		LocalFallback:      taskPollingClaimMetrics.localFallback.Load(),
	}
}

func taskPollingClaimLeaseTTL() time.Duration {
	seconds := common.GetEnvOrDefault("TASK_POLLING_CLAIM_LEASE_TTL_SECONDS", int(taskPollingClaimDefaultLeaseTTL/time.Second))
	if seconds <= 0 {
		return taskPollingClaimDefaultLeaseTTL
	}
	ttl := time.Duration(seconds) * time.Second
	if ttl < taskPollingClaimMinLeaseTTL {
		return taskPollingClaimMinLeaseTTL
	}
	if ttl > taskPollingClaimMaxLeaseTTL {
		return taskPollingClaimMaxLeaseTTL
	}
	return ttl
}

func taskPollingClaimStrict() bool {
	// Deployments without Redis remain supported as single-instance mode. Once
	// Redis is configured, the default is fail-closed so a coordinator outage
	// cannot silently turn a cluster-wide claim into a process-local lock.
	return common.GetEnvOrDefaultBool("TASK_POLLING_CLAIM_STRICT", common.RedisEnabled)
}

func taskPollingClaimRedisAvailable() bool {
	return common.RedisEnabled && common.RDB != nil
}

func taskPollingClaimKey(task *model.Task) (string, error) {
	if task == nil {
		return "", errors.New("task polling claim task is nil")
	}
	identity := ""
	if task.ID > 0 {
		identity = fmt.Sprintf("db:%d", task.ID)
	} else {
		identity = fmt.Sprintf("legacy:%d:%d:%s:%s:%s", task.ChannelId, task.UserId, task.Platform, task.TaskID, task.GetUpstreamTaskID())
	}
	if identity == "" {
		return "", errors.New("task polling claim task identity is empty")
	}
	digest := sha256.Sum256([]byte(identity))
	return taskPollingClaimRedisPrefix + ":" + hex.EncodeToString(digest[:]), nil
}

func taskPollingClaimToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate task polling claim token: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func logTaskPollingClaimUnavailable(reason string) {
	now := time.Now().UnixNano()
	last := taskPollingClaimUnavailableLogAt.Load()
	if last != 0 && now-last < taskPollingClaimLogGap.Nanoseconds() {
		return
	}
	if !taskPollingClaimUnavailableLogAt.CompareAndSwap(last, now) {
		return
	}
	logger.LogError(context.Background(), "task polling claim Redis unavailable: "+reason)
}

func cleanupLocalTaskPollingClaimsLocked(now time.Time) {
	for key, lease := range localTaskPollingClaims.leases {
		if !lease.expiresAt.After(now) {
			delete(localTaskPollingClaims.leases, key)
		}
	}
}

func acquireLocalTaskPollingClaim(key string, token string, ttl time.Duration) bool {
	localTaskPollingClaims.mu.Lock()
	defer localTaskPollingClaims.mu.Unlock()
	cleanupLocalTaskPollingClaimsLocked(time.Now())
	if _, exists := localTaskPollingClaims.leases[key]; exists {
		return false
	}
	localTaskPollingClaims.leases[key] = localTaskPollingClaimLease{token: token, expiresAt: time.Now().Add(ttl)}
	return true
}

func renewLocalTaskPollingClaim(key string, token string, ttl time.Duration) bool {
	localTaskPollingClaims.mu.Lock()
	defer localTaskPollingClaims.mu.Unlock()
	lease, exists := localTaskPollingClaims.leases[key]
	if !exists || lease.token != token || !lease.expiresAt.After(time.Now()) {
		if exists && lease.token == token {
			delete(localTaskPollingClaims.leases, key)
		}
		return false
	}
	lease.expiresAt = time.Now().Add(ttl)
	localTaskPollingClaims.leases[key] = lease
	return true
}

func releaseLocalTaskPollingClaim(key string, token string) {
	localTaskPollingClaims.mu.Lock()
	defer localTaskPollingClaims.mu.Unlock()
	if lease, exists := localTaskPollingClaims.leases[key]; exists && lease.token == token {
		delete(localTaskPollingClaims.leases, key)
	}
}

func releaseTaskPollingClaimRedis(key string, token string) error {
	if !taskPollingClaimRedisAvailable() {
		return errors.New("client is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), taskPollingClaimRedisTimeout)
	_, err := common.RDB.Eval(ctx, taskPollingClaimReleaseScript, []string{key}, token).Int()
	cancel()
	return err
}

// TaskPollingClaimLease is an idempotent distributed lease for one persisted
// task. Its context is canceled if ownership expires or repeated renewal
// failures make ownership uncertain, preventing a worker from continuing the
// upstream request after another instance may have acquired the task.
type TaskPollingClaimLease struct {
	key       string
	token     string
	ttl       time.Duration
	redis     bool
	local     bool
	ctx       context.Context
	cancel    context.CancelFunc
	released  sync.Once
	renewFail atomic.Int32
}

// Context returns the lease-bound context for the actual upstream request.
// Callers must use it instead of the original maintenance context.
func (lease *TaskPollingClaimLease) Context() context.Context {
	if lease == nil || lease.ctx == nil {
		return context.Background()
	}
	return lease.ctx
}

// Done closes when the lease is released, expires, or loses coordinator
// ownership. It is useful when one upstream batch is guarded by several task
// leases.
func (lease *TaskPollingClaimLease) Done() <-chan struct{} {
	return lease.Context().Done()
}

func (lease *TaskPollingClaimLease) renewInterval() time.Duration {
	// Keep the failure threshold strictly inside one lease lifetime. With three
	// consecutive failures, a TTL/4 cadence cancels at roughly 75% of the TTL,
	// leaving room for scheduler delay and Redis timeout before another worker
	// can observe the expired claim.
	interval := lease.ttl / (taskPollingClaimRenewFailureLimit + 1)
	if interval < time.Second {
		return time.Second
	}
	return interval
}

func (lease *TaskPollingClaimLease) renewLoop() {
	ticker := time.NewTicker(lease.renewInterval())
	defer ticker.Stop()
	for {
		select {
		case <-lease.ctx.Done():
			return
		case <-ticker.C:
			if lease.redis {
				if !taskPollingClaimRedisAvailable() {
					taskPollingClaimMetrics.renewFailure.Add(1)
					if lease.renewFail.Add(1) >= taskPollingClaimRenewFailureLimit {
						taskPollingClaimMetrics.leaseLost.Add(1)
						lease.cancel()
						return
					}
					continue
				}
				ctx, cancel := context.WithTimeout(context.Background(), taskPollingClaimRedisTimeout)
				result, err := common.RDB.Eval(ctx, taskPollingClaimRenewScript, []string{lease.key}, lease.token, lease.ttl.Milliseconds()).Int()
				cancel()
				if err != nil {
					taskPollingClaimMetrics.renewFailure.Add(1)
					logTaskPollingClaimUnavailable(err.Error())
					if lease.renewFail.Add(1) >= taskPollingClaimRenewFailureLimit {
						taskPollingClaimMetrics.leaseLost.Add(1)
						lease.cancel()
						return
					}
					continue
				}
				if result == 0 {
					taskPollingClaimMetrics.leaseLost.Add(1)
					lease.cancel()
					return
				}
				lease.renewFail.Store(0)
				continue
			}
			if !renewLocalTaskPollingClaim(lease.key, lease.token, lease.ttl) {
				taskPollingClaimMetrics.leaseLost.Add(1)
				lease.cancel()
				return
			}
		}
	}
}

// Release relinquishes ownership exactly once. Redis release is token-guarded
// so a late cleanup cannot delete a newer worker's claim after TTL expiry.
func (lease *TaskPollingClaimLease) Release() {
	if lease == nil {
		return
	}
	lease.released.Do(func() {
		lease.cancel()
		if lease.local {
			releaseLocalTaskPollingClaim(lease.key, lease.token)
		}
		if !lease.redis {
			return
		}
		err := releaseTaskPollingClaimRedis(lease.key, lease.token)
		if err != nil {
			taskPollingClaimMetrics.releaseFailure.Add(1)
			logTaskPollingClaimUnavailable(err.Error())
		}
	})
}

// AcquireTaskPollingClaim claims a task before any provider status request.
// Redis is required by default when configured. If Redis is intentionally
// disabled, a process-local fallback is allowed only with the explicit
// single-instance policy represented by TASK_POLLING_CLAIM_STRICT=false (the
// default when no Redis connection is configured).
func AcquireTaskPollingClaim(parent context.Context, task *model.Task) (*TaskPollingClaimLease, error) {
	key, err := taskPollingClaimKey(task)
	if err != nil {
		return nil, err
	}
	if parent == nil {
		parent = context.Background()
	}
	if err := parent.Err(); err != nil {
		return nil, err
	}
	ttl := taskPollingClaimLeaseTTL()
	token, err := taskPollingClaimToken()
	if err != nil {
		taskPollingClaimMetrics.acquireUnavailable.Add(1)
		return nil, err
	}
	useRedis := taskPollingClaimRedisAvailable()
	useLocal := false
	if useRedis {
		ctx, cancel := context.WithTimeout(parent, taskPollingClaimRedisTimeout)
		acquired, setErr := common.RDB.SetNX(ctx, key, token, ttl).Result()
		cancel()
		if setErr != nil {
			taskPollingClaimMetrics.acquireUnavailable.Add(1)
			logTaskPollingClaimUnavailable(setErr.Error())
			// SetNX may have committed before its response was lost. A token-
			// guarded cleanup is safe and avoids holding an abandoned claim for
			// the full TTL; if cleanup is also unavailable, expiry remains the
			// final recovery boundary and the failure stays observable.
			if cleanupErr := releaseTaskPollingClaimRedis(key, token); cleanupErr != nil {
				logTaskPollingClaimUnavailable(cleanupErr.Error())
			}
			return nil, fmt.Errorf("%w: %v", ErrTaskPollingClaimUnavailable, setErr)
		}
		if !acquired {
			taskPollingClaimMetrics.acquireBusy.Add(1)
			return nil, ErrTaskPollingClaimBusy
		}
	} else {
		if taskPollingClaimStrict() {
			taskPollingClaimMetrics.acquireUnavailable.Add(1)
			logTaskPollingClaimUnavailable("client is not configured")
			return nil, ErrTaskPollingClaimUnavailable
		}
		if !acquireLocalTaskPollingClaim(key, token, ttl) {
			taskPollingClaimMetrics.acquireBusy.Add(1)
			return nil, ErrTaskPollingClaimBusy
		}
		useLocal = true
		taskPollingClaimMetrics.localFallback.Add(1)
	}
	ctx, cancel := context.WithCancel(parent)
	lease := &TaskPollingClaimLease{key: key, token: token, ttl: ttl, redis: useRedis, local: useLocal, ctx: ctx, cancel: cancel}
	taskPollingClaimMetrics.acquireSuccess.Add(1)
	go lease.renewLoop()
	return lease, nil
}
