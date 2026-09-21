# Classic and Default parity: model-test runtime configuration

Sampling time: 2026-09-19 Asia/Shanghai

## Finding

The Classic channel model-test dialog could choose whether a test applies the
channel Header Profile or header override, parameter override, proxy, and
model mapping. It also exposed the test prompt, maximum output tokens, and
response protocol. Default previously sent only the model, endpoint, and
stream flags, so its model test could not reproduce the configured channel
runtime path.

## Change

- Added a typed Default runtime configuration model with the same defaults as
  Classic: runtime settings enabled, all four channel options enabled, native
  Responses protocol, `hi` prompt, and 16 maximum output tokens.
- Added the Default model-test runtime panel with a master switch, four
  independently controlled options, configured-state indicators, prompt and
  token inputs, response protocol selection, and an edit-channel action.
- Extended the Default test request to send all backend-supported query
  parameters, including explicit `false` values:
  `runtime_config`, `header_config`, `param_override`, `proxy`,
  `model_mapping`, `response_protocol`, `test_prompt`, and `max_tokens`.
- Added runtime configuration normalization and capability snapshot helpers so
  the UI does not infer configured state from malformed JSON.
- Extended the result type and diagnostics to show whether headers, parameter
  overrides, proxy, and model mapping were actually applied, including Profile,
  model mapping, and backend configuration warnings.
- Guarded the Chat Completions conversion option so it is unavailable for
  explicit non-Responses endpoints, matching the backend protocol contract.
- Added regression tests for defaults, explicit false serialization, input
  normalization, and channel capability detection.

## Verification

| Command | Result |
| --- | --- |
| `cd web/default && bun test` | 211 pass, 0 fail, 866 expect calls |
| `cd web/default && bun run typecheck` | Exit 0 |
| `cd web/default && bun run lint` | Exit 0 |
| `cd web/default && bun run i18n:sync` | Exit 0, source scan 0 missing keys |
| `cd web/default && prettier --check <changed files>` | Exit 0 |
| `cd web/default && bun run build` | Exit 0, Safari and browser compatibility checks passed |
| `git diff --check` on changed files | Exit 0 |

The repository-wide Default `bun run format:check` still reports two files
already modified outside this task: `src/features/models/components/dialogs/view-details-dialog.tsx`
and `src/features/wallet/index.tsx`. The changed files for this task pass the
targeted formatter check; those unrelated files were not rewritten.

## Boundary

The backend query contract was verified in
`controller/channel_test_runtime_config.go`,
`controller/channel_test_runtime_summary.go`, and
`controller/channel_test_internal_test.go`. This change was verified in the
local source and frontend build/test environment only. The formal service on
port `13000` was not restarted, rebuilt, deployed, or written to, and no
manual browser acceptance was performed in this pass.
