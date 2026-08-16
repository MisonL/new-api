# Responses Opaque Replay P0 Review

Date: 2026-07-27

## Scope

This review covers the high-priority Codex Responses regression where a request
containing opaque `encrypted_content` could lose reasoning state through request
conversion, synthetic compaction, visible-only fallback, compatibility retry, or
cross-channel replay.

The release boundary is limited to Responses routing, request preparation,
synthetic compaction state handling, channel capability reporting, and the
isolated development verification harness. No database schema, credential, or
production channel configuration changes are included.

## Invariants

- Any non-empty opaque `encrypted_content` in Responses input must use native
  Responses forwarding.
- The upstream body must be the original request bytes except for an explicit
  mapped `model` value.
- Opaque replay must not convert to Chat Completions, strip history, normalize
  item IDs, apply synthetic or visible-only compaction, change summary model, or
  retry through another channel.
- A synthetic compact marker is trusted only when its referenced local state is
  present. A malformed, foreign, expired, or fabricated marker remains opaque.
- The routing and usage-log capability views must report the same replay
  capability. `forwarded_*` means that the upstream returned an HTTP response;
  it does not indicate a successful upstream result.

## Root Cause

The previous compatibility paths treated encrypted reasoning and compaction
markers as fields that could be transformed or removed to obtain a retry. That
is unsafe for Codex state replay because the ciphertext is bound to upstream
state and request history. Prefix-only recognition of local synthetic markers
also allowed a foreign marker to enter a visible-only fallback path.

## Changes Reviewed

- Added three-state encrypted replay capability resolution: `supported`,
  `unknown`, and `unsupported`.
- Added state-aware opaque detection and marker validation backed by the local
  synthetic compact state store.
- Added an original-body forwarding path with model-only replacement and an
  explicit rejection for body-changing parameter overrides.
- Blocked opaque requests from Responses-to-Chat conversion, compatibility
  mutation retries, synthetic compaction, fallback recovery, and channel retry.
- Added routing skip records plus action, capability, and retry-suppression log
  fields without including ciphertext.
- Preserved the actual encrypted replay capability when a request is rejected
  for a separate compaction passthrough limitation.

## L0 And L1 Verification

All commands below completed with exit code 0 in
`/Volumes/Work/code/new-api/.worktrees/responses-opaque-replay-p0`.

```text
go test ./controller ./model ./relay/common ./relay/helper ./service ./relay ./relay/channel/openai ./relay/channel/codex -count=1 -timeout 180s
go test ./controller ./dto ./relay/common ./relay ./relay/channel/openai ./relay/channel/codex ./service -run 'Opaque|EncryptedReasoningReplay|RemoteResponsesCompaction|SyntheticCompactMarker|MissingReferencedState|BootstrapRecoveryRejectsOpaque|PreservesOpaque|DoesNotMutateOpaque|ContextCompaction.*Marker' -count=5 -timeout 240s
go test ./... -count=1 -timeout 180s
go vet ./...
go test -race ./relay/common ./controller ./service ./relay ./relay/channel/openai ./relay/channel/codex -count=1 -timeout 240s
git diff --check
bash -n scripts/compact-control-plane-e2e.sh scripts/compact-control-plane-e2e-lib.sh
node --check scripts/compact-control-plane-fake-upstream.mjs
```

## L2 Gate

The isolated development stack was rebuilt from `new-api-local:dev`. Its build
metadata reported the reviewed source revision, and `http://127.0.0.1:3001/api/status`
returned `success: true`.

The following command completed with exit code 0. The script uses an isolated
database fixture, restores its backup tables during cleanup, and restarts only
the isolated `new-api` service.

```text
COMPACT_E2E_ENV_FILE=/Volumes/Work/code/new-api/deploy/env/dev-isolated.env scripts/compact-control-plane-e2e.sh
```

The run covered native compact, synthetic compact continuation, stale marker
handling, generic-channel rejection, REST previous-response-id rejection, model
switch restoration, and a Codex history fixture. Its opaque replay case observed
exactly one upstream `/v1/responses` request, the expected `gpt-5.5 -> gpt-5.4`
model mapping, retained `encrypted_content`, and a SHA-256 body equal to the
original request after only that model value changed. The fake upstream returned
`invalid_encrypted_content`; the gateway returned that error without a second
request.

Production deployment is still not authorized by this review. It requires a
separate current-channel validation using a development copy of the enabled
production channels.
