# CR-RESPONSES-COMPACT-2026-05-22

## Scope

This record covers the Responses Compact fix for OpenAI-compatible channels.
The goal is to prevent unsupported `/v1/responses/compact` traffic from being silently downgraded to ordinary `/v1/responses`, while allowing explicitly native compact channels to preserve compact semantics.

## Code State

- `dto.ChannelOtherSettings.responses_compact_mode` declares compact capability.
- Missing or invalid compact capability is treated as `unsupported`.
- OpenAI-compatible `/v1/responses/compact` requests require `responses_compact_mode = native`.
- Native compact keeps `/v1/responses/compact`, model mapping, `previous_response_id`, and `type: compaction` input.
- Compact virtual models no longer route to ordinary base-model channel candidates.
- Channel WebUI exposes compact capability, warns or blocks risky compact model configuration, and labels channel compact state.
- Channel test performs compact capability diagnosis before sending a compact test request.

## Verification

Commands run on 2026-05-22:

```bash
go test -count=1 ./controller ./model ./relay/common ./relay/helper ./relay/channel/openai ./relay ./service
cd web/default && bun test tests/channel-responses-compact.test.ts
cd web/default && bun run lint
cd web/default && bun run build
```

Results:

- Go package set passed.
- Frontend compact settings test passed.
- Frontend lint passed.
- Frontend build passed.

## Runtime Smoke

Environment:

- Isolated backend: `http://127.0.0.1:3001`
- Runtime build info: `commit=0aca177dc4a8c6e17489402ad13c285a0b739551-dirty`
- Fake upstream: local HTTP capture server, removed after verification.

Observed cases:

- Unsupported OpenAI-compatible channel returned `400` before upstream forwarding. Capture count was `0`.
- Native OpenAI-compatible channel returned `200`; upstream received `/v1/responses/compact`, mapped model `gpt-5`, `previous_response_id=resp_prev_runtime`, and an input item with `type=compaction`.

## Boundary

- Temporary runtime channels, abilities, tokens, and fake upstream process were removed after the smoke test.
- Ports `5176` and `18080` had no listener after cleanup.
- Azure compact behavior was not included in this fix scope; current compact capability UI and hard gate target OpenAI channel type and Codex channel behavior only.
