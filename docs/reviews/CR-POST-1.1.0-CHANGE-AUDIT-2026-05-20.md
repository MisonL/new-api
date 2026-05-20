# CR-POST-1.1.0-CHANGE-AUDIT-2026-05-20

## Scope

- Base: `v1.1.0` (`6d654fe416ababede59cd58c824a7f2c161cc3af`, 2026-04-19 17:07:48 +0800)
- Head: `9a4bb8ed54096b146826fb22f5547a461f18ada7`
- Branch: `codex/protocol-conversion-webui`
- Range: `v1.1.0..HEAD`, 320 commits, 1760 changed files

## Review Focus

- Backend relay and protocol compatibility paths: OpenAI Responses/Chat conversion, custom tool bridge, model mapping, request validation.
- Channel management and request header policy paths: Header Profile validation, pass-through headers, channel tag policy, channel cache.
- Billing and payment paths: tiered billing, subscription reset, top-up callback guards, gift code and invite rebate flows.
- Frontend default and classic surfaces: protocol conversion editor, channel tables, usage logs, dashboard drilldown, settings pages.
- Deployment and release hygiene: Docker build traceability, embedded assets, dev isolated environment documentation.

## Findings Fixed

- Removed production debug output from default/classic frontend paths.
- Removed decorative Unicode symbols from code comments, user-visible warning strings, skill docs, and installation docs.
- Replaced direct JSON encode/decode calls added in OAuth, video task redaction, and DTO value helpers with `common.*` wrappers.
- Added `common.NewJsonDecoderUseNumber` and `common.DecodeJsonFromDecoder` so Header Profile parsing keeps streaming duplicate-key validation without business code constructing a standard-library decoder.

## Verification

| Command | Result |
| --- | --- |
| `git fetch --all --tags --prune` | Passed |
| `coderabbit review --prompt-only --base-commit 6d654fe416ababede59cd58c824a7f2c161cc3af` | Blocked by CodeRabbit 150-file limit; range has 1743 review files |
| `coderabbit review --prompt-only -t uncommitted` | Returned current-diff findings under limited/free CLI behavior; valid findings were fixed, and the still-running process was stopped after findings were captured |
| `coderabbit review --prompt-only -t uncommitted` | Re-run passed, no findings |
| Gemini CLI current diff review | Passed, no blocking findings |
| Claude CLI current diff review | Not completed; CLI returned a service pause message instead of a review result |
| `go test ./controller ./model ./relay/common ./relay/helper ./service` | Passed |
| `go test ./common ./controller ./dto ./service` | Passed |
| `cd web/default && bun run lint` | Passed |
| `cd web/default && bun run build` | Passed |
| `cd web/default && bun test tests` | Passed, 23 tests |
| `cd web/classic && bun run build:fast` | Passed |
| `cd web/classic && bun run test:node` | Passed, 120 tests |
| `git diff --check` | Passed |
| Current decorative Unicode scan | Passed, no matches |
| Current added-secret-pattern scan | Passed, no matches |

## Current Diff Review Follow-up

- Fixed the `classic-to-default-sync` skill triage table so its header and rows have matching columns.
- Fixed the `formatLocalCurrencyAmount` example comment so it no longer implies the helper applies exchange-rate conversion.
- Updated the classic deployment details modal to prefer `details.status` and fall back to `deployment.status`.
- Added a visible warning banner and second confirmation step when disabling SSRF protection in classic system settings.
- Kept the high-risk status-code retry modal unchanged because it already renders through `RiskAcknowledgementModal` with a warning icon, danger confirm action, checklist, and typed confirmation.

## Residual Boundary

- Full CodeRabbit review could not run because the release diff exceeds its file-count limit. Local review, static scans, builds, and tests were used instead.
- Classic keeps the upstream-required welcome console banner in `web/classic/src/index.jsx`; its content is now plain ASCII.
