# Real Channel Gate Pricing Snapshot Review

## Root Cause

The real-channel compaction gate copied scoped routing and Responses options into the isolated database but omitted the runtime pricing options. A probe for `gpt-5.6-sol` therefore failed in local pre-consumption with `price not configured` before an upstream request was attempted.

## Change

The gate now snapshots and restores `ModelRatio`, `ModelPrice`, `CompletionRatio`, `CacheRatio`, and `CreateCacheRatio` together with its existing scoped options. The production source remains protected by a repeatable-read, read-only PostgreSQL snapshot.

## Verification

```text
bash -n scripts/codex-real-channel-compaction-gate.sh
node --test scripts/codex-real-channel-compaction-gate.test.mjs
exit=0
tests=17 passed

DEV_NEW_API_IMAGE=new-api-local:cecc5a8a2-clean \
CODEX_REAL_GATE_MODEL=gpt-5.6-sol \
bash scripts/codex-real-channel-compaction-gate.sh --prepare-only
exit=0
gate_prepare_only=true
cleanup=passed
isolated_redis_recreated=true
isolated_app_network_ready=true
isolated_app_status_verified=true
```

The preparation snapshot discovered the following production-enabled channels at the sampling time: `75 114 150 169 199 201 205 206 207 219 222 223`.

`--prepare-only` does not prove upstream protocol success. It only proves that the selected production snapshot, including pricing, can be imported into the isolated database and fully restored. Real channel probes remain a separate gate.
