# Responses Synthetic Compact Review - 2026-05-25

## Scope

This record covers the explicit `responses_compact_mode = synthetic_summary` compatibility path for OpenAI-compatible channels.

## Design

- `native` keeps `/v1/responses/compact` and forwards real compact input unchanged.
- `convert` remains a lossy `/v1/responses` conversion path and is excluded from compact auto-routing.
- `synthetic_summary` is an explicit opt-in compatibility mode. It sends a generated summary request to ordinary `/v1/responses`, stores the returned summary in new-api, and returns a synthetic compact marker.
- Later Responses requests that reference the synthetic response id or marker recover the stored summary, inject it as developer context, remove the synthetic compaction item, and clear the local synthetic `previous_response_id`.
- Missing or expired synthetic state fails explicitly. It is not forwarded as a normal upstream `previous_response_id`.

## Boundaries

- This mode does not create real upstream compact encrypted state.
- Opaque-only compaction input without visible text or a stored synthetic state fails.
- Redis is used when configured; the in-process store is local to one process and is suitable only for single-instance fallback.

## Verification

- `go test ./service ./model ./relay/common ./relay/channel/openai ./relay -count=1`
- `cd web/default && bun test tests/channel-responses-compact.test.ts`
