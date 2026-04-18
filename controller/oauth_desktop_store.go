package controller

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

const (
	desktopOAuthRedisPrefix = "new-api:desktop_oauth:v1"
)

type desktopOAuthRequestStore interface {
	Create(request *desktopOAuthRequest) error
	GetByState(state string) (*desktopOAuthRequest, bool, error)
	GetByHandoff(handoffToken string) (*desktopOAuthRequest, bool, error)
	Complete(state string, resultUserID int) error
	Fail(state string, message string) error
	Consume(handoffToken string) (*desktopOAuthRequest, bool, error)
}

type memoryDesktopOAuthStore struct {
	mu        sync.Mutex
	byState   map[string]*desktopOAuthRequest
	byHandoff map[string]*desktopOAuthRequest
}

type redisDesktopOAuthStore struct {
	client *redis.Client
}

var (
	desktopOAuthStoreOverride desktopOAuthRequestStore
	desktopOAuthMemoryStore   = newMemoryDesktopOAuthStore()
	desktopOAuthConsumeScript = redis.NewScript(`
local payload = redis.call("GET", KEYS[1])
if not payload then
	return nil
end
local decoded = cjson.decode(payload)
redis.call("DEL", KEYS[1])
if decoded["State"] then
	redis.call("DEL", ARGV[1] .. decoded["State"])
end
return payload
`)
)

func newMemoryDesktopOAuthStore() *memoryDesktopOAuthStore {
	return &memoryDesktopOAuthStore{
		byState:   map[string]*desktopOAuthRequest{},
		byHandoff: map[string]*desktopOAuthRequest{},
	}
}

func currentDesktopOAuthStore() desktopOAuthRequestStore {
	if desktopOAuthStoreOverride != nil {
		return desktopOAuthStoreOverride
	}
	if common.RedisEnabled && common.RDB != nil {
		return &redisDesktopOAuthStore{client: common.RDB}
	}
	return desktopOAuthMemoryStore
}

func resetDesktopOAuthStoreForTest(store desktopOAuthRequestStore) {
	desktopOAuthStoreOverride = store
}

func (s *memoryDesktopOAuthStore) Create(request *desktopOAuthRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupExpiredLocked(time.Now())
	s.byState[request.State] = cloneDesktopOAuthRequest(request)
	s.byHandoff[request.HandoffToken] = cloneDesktopOAuthRequest(request)
	return nil
}

func (s *memoryDesktopOAuthStore) GetByState(state string) (*desktopOAuthRequest, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupExpiredLocked(time.Now())
	request, found := s.byState[state]
	if !found {
		return nil, false, nil
	}
	return cloneDesktopOAuthRequest(request), true, nil
}

func (s *memoryDesktopOAuthStore) GetByHandoff(handoffToken string) (*desktopOAuthRequest, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupExpiredLocked(time.Now())
	request, found := s.byHandoff[handoffToken]
	if !found {
		return nil, false, nil
	}
	return cloneDesktopOAuthRequest(request), true, nil
}

func (s *memoryDesktopOAuthStore) Complete(state string, resultUserID int) error {
	return s.update(state, func(request *desktopOAuthRequest) {
		request.ResultUserID = resultUserID
		request.CompletedAt = time.Now()
		request.ErrorMessage = ""
	})
}

func (s *memoryDesktopOAuthStore) Fail(state string, message string) error {
	return s.update(state, func(request *desktopOAuthRequest) {
		request.ErrorMessage = message
		request.CompletedAt = time.Now()
		request.ResultUserID = 0
	})
}

func (s *memoryDesktopOAuthStore) Consume(handoffToken string) (*desktopOAuthRequest, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupExpiredLocked(time.Now())
	request, found := s.byHandoff[handoffToken]
	if !found {
		return nil, false, nil
	}
	delete(s.byHandoff, handoffToken)
	delete(s.byState, request.State)
	return cloneDesktopOAuthRequest(request), true, nil
}

func (s *memoryDesktopOAuthStore) update(state string, mutate func(request *desktopOAuthRequest)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupExpiredLocked(time.Now())
	request, found := s.byState[state]
	if !found {
		return nil
	}
	mutate(request)
	if handoffRequest, ok := s.byHandoff[request.HandoffToken]; ok {
		*handoffRequest = *request
	}
	return nil
}

func (s *memoryDesktopOAuthStore) cleanupExpiredLocked(now time.Time) {
	for handoffToken, request := range s.byHandoff {
		if now.Sub(request.CreatedAt) <= desktopOAuthTTL {
			continue
		}
		delete(s.byHandoff, handoffToken)
		delete(s.byState, request.State)
	}
}

func (s *redisDesktopOAuthStore) Create(request *desktopOAuthRequest) error {
	payload, err := common.Marshal(request)
	if err != nil {
		return err
	}
	ctx := context.Background()
	pipe := s.client.TxPipeline()
	pipe.Set(ctx, s.handoffKey(request.HandoffToken), payload, desktopOAuthTTL)
	pipe.Set(ctx, s.stateKey(request.State), request.HandoffToken, desktopOAuthTTL)
	_, err = pipe.Exec(ctx)
	return err
}

func (s *redisDesktopOAuthStore) GetByState(state string) (*desktopOAuthRequest, bool, error) {
	ctx := context.Background()
	handoffToken, err := s.client.Get(ctx, s.stateKey(state)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, false, nil
		}
		return nil, false, err
	}
	request, found, err := s.GetByHandoff(handoffToken)
	if err != nil || found {
		return request, found, err
	}
	if delErr := s.client.Del(ctx, s.stateKey(state)).Err(); delErr != nil && !errors.Is(delErr, redis.Nil) {
		return nil, false, delErr
	}
	return nil, false, nil
}

func (s *redisDesktopOAuthStore) GetByHandoff(handoffToken string) (*desktopOAuthRequest, bool, error) {
	ctx := context.Background()
	payload, err := s.client.Get(ctx, s.handoffKey(handoffToken)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, false, nil
		}
		return nil, false, err
	}
	request := &desktopOAuthRequest{}
	if err := common.Unmarshal(payload, request); err != nil {
		return nil, false, err
	}
	return request, true, nil
}

func (s *redisDesktopOAuthStore) Complete(state string, resultUserID int) error {
	return s.updateByState(state, func(request *desktopOAuthRequest) {
		request.ResultUserID = resultUserID
		request.CompletedAt = time.Now()
		request.ErrorMessage = ""
	})
}

func (s *redisDesktopOAuthStore) Fail(state string, message string) error {
	return s.updateByState(state, func(request *desktopOAuthRequest) {
		request.ErrorMessage = message
		request.CompletedAt = time.Now()
		request.ResultUserID = 0
	})
}

func (s *redisDesktopOAuthStore) Consume(handoffToken string) (*desktopOAuthRequest, bool, error) {
	ctx := context.Background()
	result, err := desktopOAuthConsumeScript.Run(
		ctx,
		s.client,
		[]string{s.handoffKey(handoffToken)},
		s.stateKeyPrefix(),
	).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, false, nil
		}
		return nil, false, err
	}
	payload, err := redisScriptPayload(result)
	if err != nil {
		return nil, false, err
	}
	request := &desktopOAuthRequest{}
	if err := common.Unmarshal(payload, request); err != nil {
		return nil, false, err
	}
	return request, true, nil
}

func (s *redisDesktopOAuthStore) updateByState(state string, mutate func(request *desktopOAuthRequest)) error {
	request, found, err := s.GetByState(state)
	if err != nil || !found {
		return err
	}
	mutate(request)
	payload, err := common.Marshal(request)
	if err != nil {
		return err
	}
	ctx := context.Background()
	pipe := s.client.TxPipeline()
	pipe.Set(ctx, s.handoffKey(request.HandoffToken), payload, desktopOAuthTTL)
	pipe.Expire(ctx, s.stateKey(state), desktopOAuthTTL)
	_, err = pipe.Exec(ctx)
	return err
}

func (s *redisDesktopOAuthStore) handoffKey(handoffToken string) string {
	return fmt.Sprintf("%s:handoff:%s", desktopOAuthRedisPrefix, handoffToken)
}

func (s *redisDesktopOAuthStore) stateKey(state string) string {
	return fmt.Sprintf("%s:state:%s", desktopOAuthRedisPrefix, state)
}

func (s *redisDesktopOAuthStore) stateKeyPrefix() string {
	return fmt.Sprintf("%s:state:", desktopOAuthRedisPrefix)
}

func redisScriptPayload(result interface{}) ([]byte, error) {
	switch value := result.(type) {
	case string:
		return []byte(value), nil
	case []byte:
		return value, nil
	default:
		return nil, fmt.Errorf("unexpected Redis script payload type %T", result)
	}
}

func cloneDesktopOAuthRequest(request *desktopOAuthRequest) *desktopOAuthRequest {
	clone := *request
	return &clone
}
