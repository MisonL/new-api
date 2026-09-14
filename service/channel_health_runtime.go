package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"

	"github.com/go-redis/redis/v8"
)

const channelHealthRuntimeStatusTimeout = 2 * time.Second

const cleanupChannelHealthRuntimeIndexScript = `
local redis_time = redis.call("TIME")
local now_unix = tonumber(redis_time[1])
redis.call("ZREMRANGEBYSCORE", KEYS[1], "-inf", now_unix - 1)
return 1
`

type channelHealthRuntimeCommands struct {
	metadata []*redis.StringCmd
	cooldown []*redis.DurationCmd
	history  []*redis.DurationCmd
	probe    []*redis.DurationCmd
}

type channelHealthRuntimeEntryResult struct {
	entry   dto.ChannelHealthRuntimeEntry
	include bool
	stale   bool
}

// GetChannelHealthRuntimeStatus returns the bounded, short-lived Redis state
// behind channel cooling and half-open probes for administrator observability.
func GetChannelHealthRuntimeStatus() dto.ChannelHealthRuntimeStatus {
	cfg := getChannelHealthConfig()
	status := dto.ChannelHealthRuntimeStatus{
		Enabled:             cfg.Enabled,
		CooldownSeconds:     int(cfg.Cooldown.Seconds()),
		ProbeLeaseSeconds:   int(cfg.ProbeLease.Seconds()),
		AsyncTaskQueueDepth: model.TaskCountUnfinished(),
		Concurrency:         ChannelConcurrencyMetricsSnapshot(),
		Entries:             make([]dto.ChannelHealthRuntimeEntry, 0),
		RecentEvents:        make([]dto.ChannelHealthRuntimeEvent, 0),
	}
	if !cfg.Enabled || !common.RedisEnabled || common.RDB == nil {
		return status
	}

	ctx, cancel := context.WithTimeout(context.Background(), channelHealthRuntimeStatusTimeout)
	defer cancel()
	entries, entriesOK := loadChannelHealthRuntimeEntries(ctx)
	if entriesOK {
		status.Entries = entries
	}
	events, eventsOK := loadChannelHealthRuntimeEvents(ctx)
	if eventsOK {
		status.RecentEvents = events
	}
	// A partial response is still useful for observability when one bounded
	// query fails. Mark Redis available when at least one query completed; both
	// slices remain empty only when Redis was entirely unavailable.
	status.RedisAvailable = entriesOK || eventsOK
	return status
}

func loadChannelHealthRuntimeEntries(ctx context.Context) ([]dto.ChannelHealthRuntimeEntry, bool) {
	if err := common.RDB.Eval(ctx, cleanupChannelHealthRuntimeIndexScript, []string{channelHealthRuntimeIndexKey}).Err(); err != nil {
		common.SysError(fmt.Sprintf("channel health runtime index cleanup failed: error=%v", err))
		return nil, false
	}

	digests, err := common.RDB.ZRangeByScore(
		ctx,
		channelHealthRuntimeIndexKey,
		&redis.ZRangeBy{Min: "-inf", Max: "+inf", Offset: 0, Count: maxChannelHealthRuntimeEntries},
	).Result()
	if err != nil {
		common.SysError(fmt.Sprintf("channel health runtime index lookup failed: error=%v", err))
		return nil, false
	}
	if len(digests) == 0 {
		return make([]dto.ChannelHealthRuntimeEntry, 0), true
	}

	commands, ok := executeChannelHealthRuntimeLookup(ctx, digests)
	if !ok {
		return nil, false
	}
	entries := make([]dto.ChannelHealthRuntimeEntry, 0, len(digests))
	staleDigests := make([]interface{}, 0)
	for index, digest := range digests {
		result := decodeChannelHealthRuntimeEntry(commands, index)
		if result.include {
			entries = append(entries, result.entry)
		}
		if result.stale {
			staleDigests = append(staleDigests, digest)
		}
	}
	removeStaleChannelHealthRuntimeDigests(ctx, staleDigests)
	return entries, true
}

func executeChannelHealthRuntimeLookup(ctx context.Context, digests []string) (channelHealthRuntimeCommands, bool) {
	commands := channelHealthRuntimeCommands{
		metadata: make([]*redis.StringCmd, len(digests)),
		cooldown: make([]*redis.DurationCmd, len(digests)),
		history:  make([]*redis.DurationCmd, len(digests)),
		probe:    make([]*redis.DurationCmd, len(digests)),
	}
	pipeline := common.RDB.Pipeline()
	for index, digest := range digests {
		commands.metadata[index] = pipeline.Get(ctx, channelHealthMetadataKeyFromDigest(digest))
		prefix := channelHealthRedisPrefix + ":" + digest
		commands.cooldown[index] = pipeline.TTL(ctx, prefix+":cooldown")
		commands.history[index] = pipeline.TTL(ctx, prefix+":history")
		commands.probe[index] = pipeline.TTL(ctx, prefix+":probe")
	}
	if _, err := pipeline.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		common.SysError(fmt.Sprintf("channel health runtime state lookup failed: error=%v", err))
		return channelHealthRuntimeCommands{}, false
	}
	return commands, true
}

func decodeChannelHealthRuntimeEntry(commands channelHealthRuntimeCommands, index int) channelHealthRuntimeEntryResult {
	metadataJSON, err := commands.metadata[index].Result()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			common.SysError(fmt.Sprintf("channel health runtime metadata lookup failed: error=%v", err))
		}
		return channelHealthRuntimeEntryResult{stale: true}
	}

	var metadata channelHealthRuntimeMetadata
	if err := common.UnmarshalJsonStr(metadataJSON, &metadata); err != nil {
		// The runtime index and state keys may still be live when a metadata
		// value is malformed (for example after a partial write or an older
		// schema rollout). Do not remove the live index entry merely because
		// this response cannot render it; let the normal TTL cleanup retire it.
		common.SysError(fmt.Sprintf("channel health runtime metadata decode failed: error=%v", err))
		return channelHealthRuntimeEntryResult{}
	}
	if metadata.Group == "" || metadata.ModelName == "" || metadata.ChannelID <= 0 {
		// As with malformed JSON, preserve the index while the associated state
		// keys are within their TTL. A later read can render the entry after a
		// compatible metadata rewrite, and expiry remains the authoritative
		// cleanup boundary.
		common.SysError("channel health runtime metadata is incomplete")
		return channelHealthRuntimeEntryResult{}
	}

	cooldownTTL, cooldownErr := commands.cooldown[index].Result()
	historyTTL, historyErr := commands.history[index].Result()
	probeTTL, probeErr := commands.probe[index].Result()
	if cooldownErr != nil || historyErr != nil || probeErr != nil {
		common.SysError(fmt.Sprintf("channel health runtime ttl lookup failed: cooldown=%v history=%v probe=%v", cooldownErr, historyErr, probeErr))
		return channelHealthRuntimeEntryResult{}
	}
	if historyTTL <= 0 {
		return channelHealthRuntimeEntryResult{stale: true}
	}

	entry := dto.ChannelHealthRuntimeEntry{
		Group:          metadata.Group,
		ModelName:      metadata.ModelName,
		ChannelID:      metadata.ChannelID,
		RouteKey:       metadata.RouteKey,
		EffectiveModel: metadata.EffectiveModel,
		Protocol:       metadata.Protocol,
		Operation:      metadata.Operation,
		UpstreamPath:   metadata.UpstreamPath,
		MultiKeyIndex:  metadata.MultiKeyIndex,
		StatusCode:     metadata.StatusCode,
		ErrorCode:      metadata.ErrorCode,
	}
	switch {
	case cooldownTTL > 0:
		entry.State = "cooldown"
		entry.CooldownRemainingSeconds = int(cooldownTTL.Round(time.Second).Seconds())
	case probeTTL > 0:
		entry.State = "half_open_probe"
		entry.ProbeLeaseRemainingSeconds = int(probeTTL.Round(time.Second).Seconds())
	default:
		entry.State = "awaiting_half_open_probe"
	}
	return channelHealthRuntimeEntryResult{entry: entry, include: true}
}

func removeStaleChannelHealthRuntimeDigests(ctx context.Context, digests []interface{}) {
	if len(digests) == 0 {
		return
	}
	if err := common.RDB.ZRem(ctx, channelHealthRuntimeIndexKey, digests...).Err(); err != nil {
		common.SysError(fmt.Sprintf("channel health runtime stale index cleanup failed: error=%v", err))
	}
}

func loadChannelHealthRuntimeEvents(ctx context.Context) ([]dto.ChannelHealthRuntimeEvent, bool) {
	runtimeEvents, err := common.RDB.LRange(ctx, channelHealthRuntimeEventsKey, 0, channelHealthRuntimeEventLimit-1).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		common.SysError(fmt.Sprintf("channel health runtime event lookup failed: error=%v", err))
		return nil, false
	}
	events := make([]dto.ChannelHealthRuntimeEvent, 0, len(runtimeEvents))
	for _, runtimeEventJSON := range runtimeEvents {
		var event dto.ChannelHealthRuntimeEvent
		if err := common.UnmarshalJsonStr(runtimeEventJSON, &event); err != nil ||
			event.OccurredAt <= 0 || event.Group == "" || event.ModelName == "" ||
			event.ChannelID <= 0 || event.Action == "" {
			if err != nil {
				common.SysError(fmt.Sprintf("channel health runtime event decode failed: error=%v", err))
			}
			continue
		}
		events = append(events, event)
	}
	return events, true
}
