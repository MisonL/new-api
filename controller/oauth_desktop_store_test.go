package controller

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
)

func TestDesktopOAuthStoreLifecycle(t *testing.T) {
	resetDesktopOAuthStoreForTest(newMemoryDesktopOAuthStore())
	t.Cleanup(func() {
		resetDesktopOAuthStoreForTest(nil)
	})

	request, err := createDesktopOAuthRequest("dex-local", desktopOAuthModeLogin, 0, "aff-test")
	if err != nil {
		t.Fatalf("failed to create desktop oauth request: %v", err)
	}
	if request.State == "" || request.HandoffToken == "" {
		t.Fatalf("expected non-empty desktop oauth request identifiers")
	}

	byState, found, err := getDesktopOAuthRequestByState(request.State)
	if err != nil {
		t.Fatalf("failed to load by state: %v", err)
	}
	if !found {
		t.Fatalf("expected request to be retrievable by state")
	}
	if byState.AffCode != "aff-test" {
		t.Fatalf("expected affiliate code to be preserved, got %q", byState.AffCode)
	}

	if err := completeDesktopOAuthRequest(request.State, 42); err != nil {
		t.Fatalf("failed to complete desktop oauth request: %v", err)
	}
	byHandoff, found, err := getDesktopOAuthRequestByHandoff(request.HandoffToken)
	if err != nil {
		t.Fatalf("failed to load by handoff: %v", err)
	}
	if !found {
		t.Fatalf("expected request to be retrievable by handoff token")
	}
	if byHandoff.ResultUserID != 42 {
		t.Fatalf("expected completed request to store user id 42, got %d", byHandoff.ResultUserID)
	}
	if byHandoff.CompletedAt.IsZero() {
		t.Fatalf("expected completed request to have completion timestamp")
	}

	if _, found, err = consumeDesktopOAuthRequest(request.HandoffToken); err != nil {
		t.Fatalf("failed to consume by handoff: %v", err)
	}
	if !found {
		t.Fatalf("expected consume to return the stored request")
	}
	if _, found, err = getDesktopOAuthRequestByState(request.State); err != nil {
		t.Fatalf("failed to verify state removal: %v", err)
	}
	if found {
		t.Fatalf("expected request to be removed after consumption")
	}
}

func TestDesktopOAuthRequestCleanupRemovesExpiredEntries(t *testing.T) {
	store := newMemoryDesktopOAuthStore()
	createdAt := time.Now().Add(-desktopOAuthTTL - time.Minute)
	request := &desktopOAuthRequest{
		State:        "expired-state",
		HandoffToken: "expired-handoff",
		CreatedAt:    createdAt,
	}
	store.byState[request.State] = request
	store.byHandoff[request.HandoffToken] = request

	store.mu.Lock()
	store.cleanupExpiredLocked(time.Now())
	store.mu.Unlock()

	if _, found, err := store.GetByState(request.State); err != nil {
		t.Fatalf("failed to query state after cleanup: %v", err)
	} else if found {
		t.Fatalf("expected expired request to be removed from state index")
	}
	if _, found, err := store.GetByHandoff(request.HandoffToken); err != nil {
		t.Fatalf("failed to query handoff after cleanup: %v", err)
	} else if found {
		t.Fatalf("expected expired request to be removed from handoff index")
	}
}

func TestRedisDesktopOAuthStoreLifecycle(t *testing.T) {
	server, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer server.Close()

	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()

	originalRedisEnabled := common.RedisEnabled
	originalRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	resetDesktopOAuthStoreForTest(nil)
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRDB
		resetDesktopOAuthStoreForTest(nil)
	})

	request, err := createDesktopOAuthRequest("dex-local", desktopOAuthModeLogin, 7, "aff-redis")
	if err != nil {
		t.Fatalf("failed to create redis-backed desktop oauth request: %v", err)
	}

	if err := failDesktopOAuthRequest(request.State, "provider failed"); err != nil {
		t.Fatalf("failed to mark redis-backed desktop oauth request failed: %v", err)
	}

	stored, found, err := getDesktopOAuthRequestByHandoff(request.HandoffToken)
	if err != nil {
		t.Fatalf("failed to load redis-backed request: %v", err)
	}
	if !found {
		t.Fatalf("expected redis-backed request to exist")
	}
	if stored.ErrorMessage != "provider failed" {
		t.Fatalf("expected redis-backed error message to persist, got %q", stored.ErrorMessage)
	}

	consumed, found, err := consumeDesktopOAuthRequest(request.HandoffToken)
	if err != nil {
		t.Fatalf("failed to consume redis-backed request: %v", err)
	}
	if !found {
		t.Fatalf("expected redis-backed consume to return a request")
	}
	if consumed.State != request.State {
		t.Fatalf("expected consumed request state %q, got %q", request.State, consumed.State)
	}
	if _, found, err := getDesktopOAuthRequestByState(request.State); err != nil {
		t.Fatalf("failed to verify redis-backed state cleanup: %v", err)
	} else if found {
		t.Fatalf("expected redis-backed request to be removed after consume")
	}
}
