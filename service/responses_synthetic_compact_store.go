package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
)

const syntheticCompactDatabaseBackfillConcurrency = 4

var syntheticCompactMemoryJanitorOnce sync.Once
var syntheticCompactDatabaseBackfillOnce sync.Map
var syntheticCompactDatabaseBackfillSlots = make(chan struct{}, syntheticCompactDatabaseBackfillConcurrency)

type syntheticCompactMemoryEntry struct {
	state      SyntheticCompactState
	expiresAt  time.Time
	persistent bool
}

type syntheticCompactRateLimitedLog struct {
	mu     sync.Mutex
	last   time.Time
	count  int
	window time.Duration
}

var syntheticCompactRecoveryLog = syntheticCompactRateLimitedLog{window: time.Minute}

func resetSyntheticCompactMemoryStoreForTest() {
	syntheticCompactMemoryStore.Reset()
}

func startSyntheticCompactMemoryJanitor() {
	syntheticCompactMemoryJanitorOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(time.Hour)
			defer ticker.Stop()
			for range ticker.C {
				pruneExpiredSyntheticCompactMemory(time.Now())
			}
		}()
	})
}

func pruneExpiredSyntheticCompactMemory(now time.Time) {
	syntheticCompactMemoryStore.Range(func(key, value any) bool {
		entry, ok := value.(syntheticCompactMemoryEntry)
		if ok && !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
			syntheticCompactMemoryStore.Delete(key)
		}
		return true
	})
}

func syntheticCompactRedisKey(id string) string {
	return syntheticCompactRedisPrefix + strings.TrimSpace(id)
}

func (logState *syntheticCompactRateLimitedLog) log(message string) {
	if logState.window <= 0 {
		common.SysLog(message)
		return
	}
	now := time.Now()
	logState.mu.Lock()
	defer logState.mu.Unlock()
	if logState.last.IsZero() || now.Sub(logState.last) >= logState.window {
		if logState.count > 0 {
			message = fmt.Sprintf("%s (suppressed %d repeated synthetic compact recovery logs)", message, logState.count)
			logState.count = 0
		}
		logState.last = now
		common.SysLog(message)
		return
	}
	logState.count++
}

func storeSyntheticCompactState(ctx context.Context, state SyntheticCompactState) error {
	state.ID = strings.TrimSpace(state.ID)
	summaryForValidation := strings.TrimSpace(state.Summary)
	if model.NormalizeSyntheticCompactStateKind(state.Kind) != model.SyntheticCompactStateKindNativeOpaque {
		state.Summary = summaryForValidation
	}
	if state.ID == "" {
		return fmt.Errorf("synthetic compact state id is required")
	}
	if summaryForValidation == "" {
		return fmt.Errorf("synthetic compact summary is required")
	}
	if len(state.Summary) > syntheticCompactSummaryMax {
		return fmt.Errorf("synthetic compact summary exceeds max size: %d > %d", len(state.Summary), syntheticCompactSummaryMax)
	}
	if state.CreatedAt == 0 {
		state.CreatedAt = time.Now().Unix()
	}
	storeCtx, cancel := syntheticCompactStoreContext(ctx)
	defer cancel()
	persistedToDatabase := false
	record, err := syntheticCompactStateRecord(storeCtx, state, time.Now())
	if err != nil {
		return fmt.Errorf("encrypt synthetic compact state: %w", err)
	}
	state = syntheticCompactStateFromPersistedRecord(record, state.Summary)
	recordTTL := syntheticCompactRecordTTL(record.ExpiresAt, time.Now())
	if recordTTL <= 0 {
		return fmt.Errorf("synthetic compact state expires before it can be stored")
	}
	if model.DB != nil {
		if err := model.SaveSyntheticCompactStateRecord(storeCtx, record); err != nil {
			return fmt.Errorf("store synthetic compact state in database: %w", err)
		}
		persistedToDatabase = true
	}
	if common.RedisEnabled && common.RDB != nil {
		data, err := common.Marshal(record)
		if err != nil {
			return err
		}
		if err := common.RDB.Set(storeCtx, syntheticCompactRedisKey(state.ID), string(data), recordTTL).Err(); err != nil {
			if persistedToDatabase {
				syntheticCompactRecoveryLog.log(fmt.Sprintf("store synthetic compact state in redis failed, database fallback available: %s", err.Error()))
				rememberSyntheticCompactState(state)
				return nil
			}
			return fmt.Errorf("store synthetic compact state in redis: %w", err)
		}
		rememberSyntheticCompactState(state)
		return nil
	}
	rememberSyntheticCompactState(state)
	return nil
}

func loadSyntheticCompactState(ctx context.Context, id string) (*SyntheticCompactState, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, false, nil
	}
	if ctx != nil && ctx.Err() != nil {
		return nil, false, ctx.Err()
	}
	loadCtx, cancel := syntheticCompactLoadContext(ctx)
	defer cancel()
	preferCache := common.RedisEnabled && common.RDB != nil
	if preferCache {
		if state, ok := loadPersistentSyntheticCompactStateFromMemory(id); ok {
			return state, true, nil
		}
	}
	if preferCache {
		state, found, err := loadSyntheticCompactStateFromRedis(loadCtx, id)
		if err != nil {
			return loadSyntheticCompactStateFromDatabaseAfterRedisFailure(ctx, id, err)
		}
		if found {
			return state, true, nil
		}
	}
	if model.DB != nil {
		state, found, err := loadSyntheticCompactStateFromDatabase(loadCtx, id)
		if err != nil || found {
			return state, found, err
		}
	}
	if state, ok := loadPersistentSyntheticCompactStateFromMemory(id); ok {
		return state, true, nil
	}
	return nil, false, nil
}

func loadSyntheticCompactStateFromRedis(ctx context.Context, id string) (*SyntheticCompactState, bool, error) {
	raw, err := common.RDB.Get(ctx, syntheticCompactRedisKey(id)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	state, record, err := syntheticCompactStateFromRedisValue(raw)
	if err != nil {
		return nil, false, fmt.Errorf("decode synthetic compact state from redis: %w", err)
	}
	now := time.Now()
	ttl, err := common.RDB.TTL(ctx, syntheticCompactRedisKey(id)).Result()
	if err != nil {
		common.SysError(fmt.Sprintf("load synthetic compact state ttl from redis failed: %s", err.Error()))
	} else {
		expiresAt, valid := syntheticCompactRedisStateExpiresAt(state.ExpiresAt, ttl, now)
		if !valid {
			_ = common.RDB.Del(ctx, syntheticCompactRedisKey(id)).Err()
			return nil, false, nil
		}
		state.ExpiresAt = expiresAt.Unix()
		if record != nil {
			record.ExpiresAt = state.ExpiresAt
			model.PrepareSyntheticCompactStateRecord(record, now.Unix())
			state.StateHash = strings.TrimSpace(record.StateHash)
		}
		rememberSyntheticCompactStateUntil(*state, expiresAt)
	}
	scheduleSyntheticCompactDatabaseBackfill(*state, record)
	return state, true, nil
}

func loadSyntheticCompactStateFromDatabaseAfterRedisFailure(ctx context.Context, id string, redisErr error) (*SyntheticCompactState, bool, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, false, ctx.Err()
	}
	dbCtx, dbCancel := syntheticCompactLoadContext(ctx)
	defer dbCancel()
	state, found, dbErr := loadSyntheticCompactStateFromDatabase(dbCtx, id)
	if dbErr != nil {
		return nil, false, fmt.Errorf("load synthetic compact state from redis: %w; database fallback: %v", redisErr, dbErr)
	}
	if found {
		syntheticCompactRecoveryLog.log(fmt.Sprintf("load synthetic compact state from redis failed, recovered from database: %s", redisErr.Error()))
		return state, true, nil
	}
	if model.DB != nil {
		syntheticCompactRecoveryLog.log(fmt.Sprintf("load synthetic compact state from redis failed, database fallback missed: %s", redisErr.Error()))
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("load synthetic compact state from redis: %w", redisErr)
}

func syntheticCompactStateFromRedisValue(raw string) (*SyntheticCompactState, *model.SyntheticCompactStateRecord, error) {
	var record model.SyntheticCompactStateRecord
	if err := common.UnmarshalJsonStr(raw, &record); err == nil &&
		strings.TrimSpace(string(record.SummaryCiphertext)) != "" {
		state, err := syntheticCompactStateFromRecord(&record)
		if err != nil {
			return nil, nil, err
		}
		if state == nil || strings.TrimSpace(state.ID) == "" {
			return nil, nil, fmt.Errorf("redis synthetic compact state missing id")
		}
		return state, &record, nil
	}
	var state SyntheticCompactState
	if err := common.UnmarshalJsonStr(raw, &state); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(state.ID) == "" {
		return nil, nil, fmt.Errorf("redis synthetic compact state missing id")
	}
	return &state, nil, nil
}

func scheduleSyntheticCompactDatabaseBackfill(state SyntheticCompactState, record *model.SyntheticCompactStateRecord) {
	if model.DB == nil || strings.TrimSpace(state.ID) == "" {
		return
	}
	if _, loaded := syntheticCompactDatabaseBackfillOnce.LoadOrStore(state.ID, struct{}{}); loaded {
		return
	}
	select {
	case syntheticCompactDatabaseBackfillSlots <- struct{}{}:
	default:
		syntheticCompactDatabaseBackfillOnce.Delete(state.ID)
		common.SysError(fmt.Sprintf("backfill synthetic compact state from redis skipped: concurrency limit reached id=%s", state.ID))
		return
	}
	var recordCopy *model.SyntheticCompactStateRecord
	if record != nil {
		copied := *record
		recordCopy = &copied
	}
	go func() {
		defer syntheticCompactDatabaseBackfillOnce.Delete(state.ID)
		defer func() {
			<-syntheticCompactDatabaseBackfillSlots
		}()
		defer func() {
			if r := recover(); r != nil {
				common.SysError(fmt.Sprintf("backfill synthetic compact state from redis panic: id=%s panic=%v", state.ID, r))
			}
		}()
		ctx, cancel := syntheticCompactStoreContext(context.Background())
		defer cancel()
		if err := backfillSyntheticCompactDatabaseFromRedis(ctx, state, recordCopy); err != nil {
			common.SysError(fmt.Sprintf("backfill synthetic compact state from redis failed: %s", err.Error()))
		}
	}()
}

func backfillSyntheticCompactDatabaseFromRedis(ctx context.Context, state SyntheticCompactState, record *model.SyntheticCompactStateRecord) error {
	if model.DB == nil {
		return nil
	}
	_, found, err := model.GetSyntheticCompactStateRecord(ctx, state.ID, time.Now().Unix())
	if err != nil {
		return err
	}
	if found {
		return nil
	}
	recordValue := model.SyntheticCompactStateRecord{}
	if record != nil {
		recordValue = *record
	} else {
		var err error
		recordValue, err = syntheticCompactStateRecord(ctx, state, time.Now())
		if err != nil {
			return err
		}
	}
	if err := model.SaveSyntheticCompactStateRecord(ctx, recordValue); err != nil {
		return err
	}
	return nil
}

func loadSyntheticCompactStateFromMemory(id string) (*SyntheticCompactState, bool) {
	if entry, ok := syntheticCompactMemoryStore.LoadFresh(id, time.Now()); ok {
		state := entry.state
		return &state, true
	}
	return nil, false
}

func loadPersistentSyntheticCompactStateFromMemory(id string) (*SyntheticCompactState, bool) {
	if entry, ok := syntheticCompactMemoryStore.LoadFresh(id, time.Now()); ok && entry.persistent {
		state := entry.state
		return &state, true
	}
	return nil, false
}

func syntheticCompactStateRecord(ctx context.Context, state SyntheticCompactState, now time.Time) (model.SyntheticCompactStateRecord, error) {
	createdAt := state.CreatedAt
	if createdAt == 0 {
		createdAt = now.Unix()
	}
	sourceInstance := strings.TrimSpace(state.SourceInstance)
	if sourceInstance == "" {
		var err error
		sourceInstance, err = syntheticCompactLocalInstanceID(ctx)
		if err != nil {
			return model.SyntheticCompactStateRecord{}, err
		}
	}
	modelAtCreation := strings.TrimSpace(state.ModelAtCreation)
	if modelAtCreation == "" {
		modelAtCreation = strings.TrimSpace(state.Model)
	}
	expiresAt := state.ExpiresAt
	if expiresAt == 0 {
		expiresAt = now.Add(syntheticCompactTTL).Unix()
	}
	record := model.SyntheticCompactStateRecord{
		ID:                 strings.TrimSpace(state.ID),
		Kind:               model.NormalizeSyntheticCompactStateKind(state.Kind),
		Model:              strings.TrimSpace(state.Model),
		ModelAtCreation:    modelAtCreation,
		UserID:             state.UserID,
		TokenID:            state.TokenID,
		Group:              strings.TrimSpace(state.Group),
		OwnerScope:         strings.TrimSpace(state.OwnerScope),
		ScopePolicy:        strings.TrimSpace(state.ScopePolicy),
		UpstreamResponseID: strings.TrimSpace(state.UpstreamResponseID),
		ChannelID:          state.ChannelID,
		ChannelType:        state.ChannelType,
		SourceInstance:     sourceInstance,
		CreatedAt:          createdAt,
		LastAccessAt:       createdAt,
		ExpiresAt:          expiresAt,
	}
	model.PrepareSyntheticCompactStateRecord(&record, now.Unix())
	summaryForRecord := state.Summary
	if record.Kind != model.SyntheticCompactStateKindNativeOpaque {
		summaryForRecord = strings.TrimSpace(summaryForRecord)
	}
	summaryCiphertext, err := encryptSyntheticCompactSummaryForRecord(record, summaryForRecord)
	if err != nil {
		return model.SyntheticCompactStateRecord{}, err
	}
	record.SummaryCiphertext = model.SyntheticCompactSummaryCiphertext(summaryCiphertext)
	// Recompute the final hash after ciphertext is attached.
	model.PrepareSyntheticCompactStateRecord(&record, now.Unix())
	return record, nil
}

func syntheticCompactStateFromPersistedRecord(record model.SyntheticCompactStateRecord, summary string) SyntheticCompactState {
	kind := model.NormalizeSyntheticCompactStateKind(record.Kind)
	summaryValue := summary
	if kind != model.SyntheticCompactStateKindNativeOpaque {
		summaryValue = strings.TrimSpace(summaryValue)
	}
	return SyntheticCompactState{
		ID:                 strings.TrimSpace(record.ID),
		Kind:               kind,
		Model:              strings.TrimSpace(record.Model),
		ModelAtCreation:    strings.TrimSpace(record.ModelAtCreation),
		Summary:            summaryValue,
		UserID:             record.UserID,
		TokenID:            record.TokenID,
		Group:              strings.TrimSpace(record.Group),
		OwnerScope:         strings.TrimSpace(record.OwnerScope),
		ScopePolicy:        model.NormalizeSyntheticCompactScopePolicy(record.ScopePolicy, record.Kind),
		UpstreamResponseID: strings.TrimSpace(record.UpstreamResponseID),
		ChannelID:          record.ChannelID,
		ChannelType:        record.ChannelType,
		SourceInstance:     strings.TrimSpace(record.SourceInstance),
		StateHash:          strings.TrimSpace(record.StateHash),
		CreatedAt:          record.CreatedAt,
		LastAccessAt:       record.LastAccessAt,
		ExpiresAt:          record.ExpiresAt,
	}
}

func syntheticCompactStateFromRecord(record *model.SyntheticCompactStateRecord) (*SyntheticCompactState, error) {
	if record == nil {
		return nil, nil
	}
	summary, err := decryptSyntheticCompactSummaryForRecord(*record)
	if err != nil {
		return nil, err
	}
	state := syntheticCompactStateFromPersistedRecord(*record, summary)
	return &state, nil
}

func loadSyntheticCompactStateFromDatabase(ctx context.Context, id string) (*SyntheticCompactState, bool, error) {
	record, found, err := model.GetSyntheticCompactStateRecord(ctx, id, time.Now().Unix())
	if err != nil {
		common.SysError(fmt.Sprintf("load synthetic compact state from database failed: %s", err.Error()))
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}
	state, err := syntheticCompactStateFromRecord(record)
	if err != nil {
		return nil, false, err
	}
	if state == nil {
		return nil, false, nil
	}
	rememberSyntheticCompactStateUntil(*state, syntheticCompactRecordExpiresAt(record.ExpiresAt))
	return state, true, nil
}

func rememberSyntheticCompactState(state SyntheticCompactState) {
	rememberSyntheticCompactStateUntil(state, syntheticCompactStateMemoryExpiresAt(state, time.Now()))
}

func rememberSyntheticCompactStateUntil(state SyntheticCompactState, expiresAt time.Time) {
	startSyntheticCompactMemoryJanitor()
	syntheticCompactMemoryStore.Store(state.ID, syntheticCompactMemoryEntry{
		state:      state,
		expiresAt:  expiresAt,
		persistent: true,
	})
}

func syntheticCompactRecordExpiresAt(expiresAt int64) time.Time {
	if expiresAt <= 0 {
		return time.Time{}
	}
	return time.Unix(expiresAt, 0)
}

func syntheticCompactStateMemoryExpiresAt(state SyntheticCompactState, now time.Time) time.Time {
	if state.ExpiresAt > 0 {
		return time.Unix(state.ExpiresAt, 0)
	}
	return now.Add(syntheticCompactTTL)
}

func syntheticCompactRecordTTL(expiresAt int64, now time.Time) time.Duration {
	if expiresAt <= 0 {
		return syntheticCompactTTL
	}
	return time.Unix(expiresAt, 0).Sub(now)
}

func syntheticCompactRedisStateExpiresAt(stateExpiresAt int64, ttl time.Duration, now time.Time) (time.Time, bool) {
	if stateExpiresAt > 0 {
		expiresAt := time.Unix(stateExpiresAt, 0)
		return expiresAt, expiresAt.After(now)
	}
	expiresAt := now.Add(syntheticCompactTTL)
	if ttl > 0 {
		expiresAt = now.Add(ttl)
	}
	return expiresAt, expiresAt.After(now)
}

// Synthetic compact store calls should finish with their own timeout even if the client disconnects.
func syntheticCompactStoreContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(ctx), syntheticCompactStoreTimeout)
}

func syntheticCompactLoadContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, syntheticCompactStoreTimeout)
}
