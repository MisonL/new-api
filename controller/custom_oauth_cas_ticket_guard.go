package controller

import (
	"container/heap"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const casTicketReplayTTL = 10 * time.Minute
const casTicketRedisTimeout = 500 * time.Millisecond

const releaseCASTicketRedisScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`

var casTicketGuard = newInMemoryCASTicketGuard(casTicketReplayTTL)
var casTicketInMemoryModeLogOnce sync.Once

var errCASTicketReplay = errors.New("cas ticket was already used")
var errCASTicketGuardUnavailable = errors.New("cas ticket guard unavailable")

type inMemoryCASTicketGuard struct {
	mu          sync.Mutex
	ttl         time.Duration
	entries     map[string]time.Time
	expirations casTicketExpirationHeap
}

type casTicketExpiration struct {
	key       string
	expiresAt time.Time
}

type casTicketExpirationHeap []casTicketExpiration

func (h casTicketExpirationHeap) Len() int {
	return len(h)
}

func (h casTicketExpirationHeap) Less(i int, j int) bool {
	return h[i].expiresAt.Before(h[j].expiresAt)
}

func (h casTicketExpirationHeap) Swap(i int, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *casTicketExpirationHeap) Push(x any) {
	*h = append(*h, x.(casTicketExpiration))
}

func (h *casTicketExpirationHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

func newInMemoryCASTicketGuard(ttl time.Duration) *inMemoryCASTicketGuard {
	return &inMemoryCASTicketGuard{
		ttl:     ttl,
		entries: make(map[string]time.Time),
	}
}

// reserveCASTicket reserves a CAS service ticket for the callback attempt. A successful
// reservation returns a non-nil release callback; callers must invoke it when the ticket is
// not consumed by a completed bind or login response.
func reserveCASTicket(providerID int, ticket string, serviceURL string) (func(), error) {
	key := buildCASTicketGuardKey(providerID, ticket, serviceURL)
	if key == "" {
		return nil, fmt.Errorf("%w: key is empty", errCASTicketGuardUnavailable)
	}

	if common.RedisEnabled {
		if common.RDB == nil {
			return nil, fmt.Errorf("%w: redis client is not initialized", errCASTicketGuardUnavailable)
		}
		return reserveCASTicketInRedis(key)
	}
	logCASTicketGuardInMemoryMode()
	return casTicketGuard.reserve(key)
}

func reserveCASTicketInRedis(key string) (func(), error) {
	token, err := newCASTicketReservationToken()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errCASTicketGuardUnavailable, err)
	}
	rdb := common.RDB
	if rdb == nil {
		return nil, fmt.Errorf("%w: redis client is not initialized", errCASTicketGuardUnavailable)
	}
	ctx, cancel := context.WithTimeout(context.Background(), casTicketRedisTimeout)
	defer cancel()
	ok, err := rdb.SetNX(ctx, key, token, casTicketReplayTTL).Result()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errCASTicketGuardUnavailable, err)
	}
	if !ok {
		return nil, errCASTicketReplay
	}
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), casTicketRedisTimeout)
		defer cancel()
		if err := rdb.Eval(ctx, releaseCASTicketRedisScript, []string{key}, token).Err(); err != nil {
			common.SysError("failed to release CAS ticket in redis: key=" + key + " error=" + err.Error())
		}
	}, nil
}

func (g *inMemoryCASTicketGuard) reserve(key string) (func(), error) {
	now := time.Now()
	expiresAt := now.Add(g.ttl)
	g.mu.Lock()
	defer g.mu.Unlock()

	g.pruneExpiredLocked(now)
	if currentExpiresAt, ok := g.entries[key]; ok && currentExpiresAt.After(now) {
		return nil, errCASTicketReplay
	}
	g.entries[key] = expiresAt
	heap.Push(&g.expirations, casTicketExpiration{key: key, expiresAt: expiresAt})
	return func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if currentExpiresAt, ok := g.entries[key]; ok && currentExpiresAt.Equal(expiresAt) {
			delete(g.entries, key)
		}
	}, nil
}

func (g *inMemoryCASTicketGuard) pruneExpiredLocked(now time.Time) {
	for g.expirations.Len() > 0 {
		next := g.expirations[0]
		currentExpiresAt, ok := g.entries[next.key]
		if !ok || !currentExpiresAt.Equal(next.expiresAt) {
			heap.Pop(&g.expirations)
			continue
		}
		if next.expiresAt.After(now) {
			g.compactIfSparseLocked()
			return
		}
		heap.Pop(&g.expirations)
		delete(g.entries, next.key)
	}
	g.compactIfSparseLocked()
}

func (g *inMemoryCASTicketGuard) compactIfSparseLocked() {
	if len(g.expirations) <= len(g.entries)*4 {
		return
	}
	g.expirations = make(casTicketExpirationHeap, 0, len(g.entries))
	for key, expiresAt := range g.entries {
		g.expirations = append(g.expirations, casTicketExpiration{key: key, expiresAt: expiresAt})
	}
	heap.Init(&g.expirations)
}

func logCASTicketGuardInMemoryMode() {
	casTicketInMemoryModeLogOnce.Do(func() {
		nodeName := strings.TrimSpace(common.NodeName)
		if nodeName == "" {
			nodeName = "unknown"
		}
		common.SysLog(fmt.Sprintf(
			"cas_ticket_guard_mode=in_memory node=%s ttl=%s multi_instance_requires_redis_or_sticky_sessions=true",
			nodeName,
			casTicketReplayTTL,
		))
	})
}

func newCASTicketReservationToken() (string, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(token[:]), nil
}

func buildCASTicketGuardKey(providerID int, ticket string, serviceURL string) string {
	ticket = strings.TrimSpace(ticket)
	serviceURL = strings.TrimSpace(serviceURL)
	if providerID <= 0 || ticket == "" || serviceURL == "" {
		return ""
	}
	return "cas_ticket:" + common.GenerateHMAC(
		strings.Join([]string{
			strconv.Itoa(providerID),
			ticket,
			serviceURL,
		}, "\n"),
	)
}

func isCASTicketReplayError(err error) bool {
	return errors.Is(err, errCASTicketReplay)
}
