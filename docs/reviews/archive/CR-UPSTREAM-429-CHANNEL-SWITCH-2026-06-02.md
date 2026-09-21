# CR-UPSTREAM-429-CHANNEL-SWITCH-2026-06-02

## Scope

This record captures the current relay behavior when an upstream channel returns HTTP `429`.

Reviewed code paths:

- `controller/relay.go`
- `service/channel_select.go`
- `model/channel_cache.go`
- `model/ability.go`
- `controller/relay_retry_test.go`
- `service/channel_select_test.go`

## Behavior

- HTTP `429` is treated as a retryable upstream rate-limit error when retry budget remains and the normal relay retry status-code setting includes `429`; the default `AutomaticRetryStatusCodes` range includes it.
- A `429` can switch to another channel even when a matched channel-affinity rule has `skip_retry_on_failure` enabled.
- Retried channel selection excludes channel IDs already recorded in the request `use_channel` list.
- `auto` token groups also pass the request-level exclusion set into channel selection.
- After retry selection, `RelayInfo.ChannelMeta` is refreshed from the newly selected channel context.

## Boundaries

- Admin requests with `specific_channel_id` still do not auto-switch channels.
- Task remix or continuation requests with `LockedChannel` intentionally stay bound to the origin task channel.
- Task relay explicitly treats HTTP `429` as retryable when retry budget remains.
- `504` and `524` remain always-skip retry status codes unless a specialized flow explicitly handles them.
- A healthy service or container status does not prove this behavior is active; the running binary must be rebuilt and verified.

## Root Cause

The failing pattern was not only channel affinity. In the normal relay path, `RelayInfo.ChannelMeta` is initialized after the first upstream attempt. The retry path previously treated a non-nil `ChannelMeta` as a signal to keep using the current context channel, so retry could re-enter the same `channel_id` after upstream `429`.

The fix keeps the first request on the distributor-selected channel, then forces retry attempts through channel selection. Selection uses `use_channel` to exclude already attempted channels, and the relay metadata is refreshed after the new channel is installed in the request context.

## Verification

Commands run:

```bash
go test ./controller -run 'Test(ShouldRetryAllowsRateLimitDespiteChannelAffinitySkip|ShouldRetryTaskRelayAllowsRateLimitDespiteChannelAffinitySkip|GetChannelInitialRequestUsesDistributedChannel|GetChannelRateLimitRetrySwitchesChannelAndRefreshesMeta)' -count=1
go test ./service -run TestAutoGroupRetryExcludesUsedChannel -count=1
go test ./model -run 'TestGetRandomSatisfiedChannelExcluding(SkipsUsedChannelsAtSamePriority|KeepsRetryPriorityStable|DatabaseFallbackSkipsExcludedCompactChannel|DatabaseFallbackReturnsNilWhenRetryPriorityExcluded|FallsThroughExcludedExactRoute)' -count=1
go test ./controller ./model ./relay/common ./relay/helper ./service -count=1
git diff --check
```

Result: all commands passed.
