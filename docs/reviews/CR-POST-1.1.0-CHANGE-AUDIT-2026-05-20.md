# CR-POST-1.1.0-CHANGE-AUDIT-2026-05-20

## Scope

- Base: `v1.1.0` (`6d654fe416ababede59cd58c824a7f2c161cc3af`, 2026-04-19 17:07:48 +0800)
- Head: `7497b8e4c71d0becff9fb5554b666257c9b3b8f4`
- Branch: `codex/protocol-conversion-webui`
- Range: `v1.1.0..HEAD`, 321 commits, 1762 changed files by `git diff --name-only`
- Worktree follow-up: current uncommitted audit fixes and artifacts were also reviewed before this report was finalized.

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
- Removed the remaining direct `encoding/json` dependency from `service/header_policy.go` by exposing decoder-related type aliases from `common/json.go`; business decode operations still go through `common.*`.
- Normalized added Markdown docs and code comments to ASCII text where the glyphs were presentational, including billing expression docs, skill docs, Go comments, and default frontend helper comments.
- Replaced remaining classic inline triangle toggle text with Semi icons, and removed the stale warning glyph entry from the i18n lint baseline.
- Fixed the `web/classic` i18n lint script to call the shared gate under `web/scripts`, without rewriting the broader stale baseline.
- Replaced the added classic deployment-location emoji with the existing map-marker icon, and removed the matching stale baseline entry.
- Wrapped newly added classic Stripe and Waffo Pancake payment-setting guidance strings in `t()`, removed decorative switch glyphs, and refreshed the i18n lint baseline after confirming the remaining new loose matches were attributes, SVG paths, or internal keys.
- Replaced the two newly added Claude extended-thinking `TODO: temporary` comments with stable comments that document the upstream sampling-parameter constraint.
- Added keyboard and ARIA semantics to the classic redemption, token, and user raw-quota disclosure toggles after replacing text chevrons with icons.
- Renamed the post-recreate browser screenshot evidence to `.jpg` so the extension matches its actual JPEG content.
- Removed reintroduced protocol-conversion side effects that changed empty `model_patterns` into match-all behavior and made channel tests apply the global responses-to-chat policy implicitly.

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
| Current explicit decorative glyph scan | Passed, no matches for triangle, check, cross, warning, fire, launch, target, or idea glyphs |
| Current added-secret-pattern scan | Passed, no matches |
| `cd web/default && bun run lint` | Re-run passed after follow-up fixes |
| `cd web/default && bun run build` | Re-run passed after follow-up fixes, built in 1 m 36.4 s |
| `coderabbit review --prompt-only -t uncommitted` | Re-run passed after follow-up fixes, no findings |
| `go test ./common ./service` | Re-run passed after Header Profile JSON boundary cleanup |
| Worktree-level added JSON/panic scan | Added JSON encode/decode is limited to `common/json.go` wrappers and test files; no added `panic("implement me")` |
| `go test ./controller ./model ./relay/common ./relay/helper ./service` | Re-run passed after backend boundary cleanup |
| `cd web/classic && bun run build:fast` | Re-run passed, built in 1 m 37 s |
| `cd web/classic && bun run test:node` | Re-run passed, 120 tests |
| Browser check `http://127.0.0.1:5176/` | Default home rendered; screenshot saved to `docs/reviews/artifacts/default-home-5176-2026-05-20.png` |
| Browser check `http://127.0.0.1:5176/pricing` | Default pricing rendered 184 models; screenshot saved to `docs/reviews/artifacts/default-pricing-5176-2026-05-20.png` |
| Browser check `http://127.0.0.1:3001/` | Classic home rendered against isolated dev; screenshot saved to `docs/reviews/artifacts/classic-home-3001-2026-05-20.png` |
| Browser check `http://127.0.0.1:3001/pricing` | Classic pricing rendered 184 models; screenshot saved to `docs/reviews/artifacts/classic-pricing-3001-2026-05-20.png`; browser console had no error/warn |
| Final `coderabbit review --prompt-only -t uncommitted` | Did not complete; stopped after about 9 minutes stuck before `Reviewing` |
| `go test ./controller ./model ./relay/common ./relay/helper ./service ./pkg/billingexpr` | Final re-run passed |
| `cd web/default && bun run lint` | Final re-run passed |
| `cd web/default && bun run build` | Final re-run passed, built in 13.7 s |
| `cd web/default && bun test tests` | Final re-run passed, 23 tests |
| `cd web/classic && bun run build:fast` | Final re-run passed, built in 1 m 24 s |
| `cd web/classic && bun run test:node` | Final re-run passed, 120 tests |
| `cd web/classic && bun run eslint` | Final re-run passed |
| `git diff --check` | Final re-run passed |
| Explicit decorative glyph scan | Final re-run passed, no matches for triangle, check, cross, warning, or similar decorative glyphs outside ignored build/artifact directories |
| Added JSON/panic scan | Final re-run found JSON usage only in `common/json.go` wrappers, test imports, and a comment; no added `panic("implement me")` |
| `node -e "JSON.parse(...web/tests/i18nLintBaseline.json...)"` | Passed |
| `cd web/classic && bun run i18n:lint` | Re-run after script fix initially exposed baseline drift; after fixing real payment-setting strings and refreshing baseline, passed with 303 current items, 303 baseline items, 0 new |
| Added emoji-range scan over `v1.1.0..worktree` | Passed, no added `U+1F300..U+1FAFF` codepoints remain |
| `cd web/classic && bun run eslint` | Re-run passed after i18n script and deployment-location icon fixes |
| `cd web/classic && bun run build:fast` | Re-run passed after deployment-location icon fix, built in 34.16 s |
| `cd web/classic && bun run i18n:lint:update-baseline` | Passed, baseline refreshed to 303 historical issues after real new payment strings were fixed |
| `cd web/classic && bun run i18n:lint` | Final re-run passed, 303 current issues equal 303 baseline issues |
| `cd web/classic && bun run eslint` | Final re-run passed |
| `cd web/classic && bun run build:fast` | Final re-run passed, built in 1 m 39 s |
| `cd web/classic && bun run test:node` | Final re-run passed, 120 tests |
| `go test ./controller ./model ./relay/common ./relay/helper ./service ./pkg/billingexpr` | Final re-run passed |
| `cd web/default && bun run lint` | Final re-run passed |
| `cd web/default && bun test tests` | Final re-run passed, 23 tests |
| `cd web/default && bun run build` | Final re-run passed, built in 58.9 s |
| `docker image inspect new-api-local:dev` | Passed, image `sha256:2da427a9ed8f9f0062a54cf4e9f6c1a437794ed1838537be0f6ddb6b2a19de29` has revision `7497b8e4c71d0becff9fb5554b666257c9b3b8f4-dirty` and build date `2026-05-20T15:12:05Z`; current changed-file mtimes are earlier than the image build time |
| `docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api` | Passed, recreated `new-api-dev-isolated-new-api-1` |
| `docker exec new-api-dev-isolated-new-api-1 /new-api --build-info` | Passed, running binary reports version `v1.1.0`, commit `7497b8e4c71d0becff9fb5554b666257c9b3b8f4-dirty`, build date `2026-05-20T15:12:05Z` |
| `curl -fsS http://127.0.0.1:3001/api/status` | Passed, `success:true`; isolated dev still serves `theme:"classic"` |
| Docker health check for `new-api-dev-isolated-new-api-1` | Passed, container reached `healthy` after recreate |
| Browser check `http://127.0.0.1:3001/` after recreate | Passed, title `New API`, rendered classic home content, browser console error count 0; screenshot saved to `docs/reviews/artifacts/classic-home-3001-after-recreate-2026-05-21.jpg` |
| Added Go JSON/TODO/panic/log scan over `v1.1.0..worktree` | Passed after Claude comment cleanup; added JSON codec use is limited to `common/json.go` wrapper lines and tests, added print/log output is limited to CLI/help or repair tooling, and no added `TODO: 临时处理` or `panic("implement me")` remains |
| `go test ./relay/channel/claude ./relay ./relay/common ./service` | Passed after Claude comment cleanup |
| `coderabbit review --prompt-only -t uncommitted` | Re-run completed with 2 findings: classic disclosure accessibility was fixed; i18n gate directory-generalization suggestion was reviewed and skipped as non-blocking for this repo-local script contract |
| Claude CLI stdin-diff review | Reported possible missing `FaMapMarkerAlt` import; verified false positive because `web/classic/src/components/table/model-deployments/modals/ViewDetailsModal.jsx` already imports it from `react-icons/fa` |
| Gemini CLI stdin-diff review | First attempt failed in Plan mode because shell execution was blocked; stdin-diff retry reported the same `FaMapMarkerAlt` false positive and no other blocker |
| `file docs/reviews/artifacts/classic-home-3001-after-recreate-2026-05-21.jpg` | Passed, file content is JPEG and now matches extension |
| `cd web/classic && bun run eslint` | Passed after raw-quota disclosure accessibility fix |
| `cd web/classic && bun run i18n:lint:update-baseline` | Passed, baseline refreshed to 307 historical issues after verifying the new entries are non-user-visible `aria-controls` ids or existing line drift |
| `cd web/classic && bun run i18n:lint` | Passed after baseline refresh, 307 current issues equal 307 baseline issues |
| `cd web/classic && bun run test:node` | Passed after raw-quota disclosure accessibility fix, 120 tests |
| `cd web/classic && bun run build:fast` | Passed after raw-quota disclosure accessibility fix, built in 3 m 46 s |
| Gemini accidental-edit cleanup scan | Passed, protocol-conversion semantics files across controller, `service/openaicompat`, `setting/model_setting`, `web/default`, and `web/classic` have no residual unapproved diff |
| `cd web/default && bun test tests/protocol-conversion-policy-utils.test.ts` | Passed after restoring empty `model_patterns` non-match semantics, 17 tests |
| `go test -timeout=60s ./controller ./model ./relay/common ./relay/helper ./service` | Passed after removing the unintended channel-test global-policy behavior change |
| `git diff --check` | Passed after Gemini accidental-edit cleanup |
| `coderabbit review --prompt-only -t uncommitted` | Re-run completed with 2 findings: fixed the valid `i18nLintGate.mjs` baseline-display clarity finding; verified the `relay/helper/price.go` arrow finding as a false positive because current source uses literal `->`, not an HTML entity |
| `cd web/classic && bun run i18n:lint` | Passed after adding `baselineDisplayPath`, 307 current issues equal 307 baseline issues |
| `git diff --check` | Passed after `i18nLintGate.mjs` follow-up |
| `coderabbit review --prompt-only -t uncommitted` | Follow-up retry reached `reviewing` then exited with code 143 without emitting findings; CLI warned `--prompt-only` now behaves like `--agent`, and its unapproved protocol-conversion edits were removed before continuing |
| CodeRabbit agent-mode side-effect cleanup scan | Passed, protocol-conversion semantic files checked under controller, `service/openaicompat`, `setting/model_setting`, `web/default`, and `web/classic` have no residual unapproved diff |
| `go test -timeout=60s ./controller ./model ./relay/common ./relay/helper ./service` | Passed after CodeRabbit side-effect cleanup |
| `cd web/default && bun test tests/protocol-conversion-policy-utils.test.ts` | Passed after CodeRabbit side-effect cleanup, 17 tests |
| `cd web/classic && bun run i18n:lint` | Passed after CodeRabbit side-effect cleanup, 307 current issues equal 307 baseline issues |
| `git diff --check` | Passed after CodeRabbit side-effect cleanup |
| Stale build cleanup | Pre-cleanup `web/default` and `web/classic` build processes were stopped after they exceeded normal runtime and were based on the reintroduced protocol semantics; their partial results were not used as evidence |
| Protocol side-effect scan | Passed, no current matches for empty-model match-all tests/copy or channel-test global-policy helper/tests |
| Protocol side-effect diff check | Passed, no residual current protocol-conversion-related diff in checked controller files, `service/openaicompat`, `setting/model_setting`, default protocol policy files, or classic protocol policy files |
| `go test -count=1 -timeout=60s ./controller ./service/openaicompat ./setting/model_setting` | Passed after final protocol semantic cleanup |
| `cd web/default && bun test tests/protocol-conversion-policy-utils.test.ts` | Passed after final protocol semantic cleanup, 17 tests |
| `cd web/classic && bun run test:node -- protocolConversionPolicyUtils.test.js` | Passed, script ran the classic node suite, 120 tests |
| `go test -count=1 -timeout=60s ./controller ./model ./relay/common ./relay/helper ./service ./pkg/billingexpr` | Passed after final cleanup |
| `cd web/default && bun run lint` | Passed after final cleanup |
| `cd web/default && bun test tests` | Passed after final cleanup, 23 tests |
| `cd web/classic && bun run eslint` | Passed after final cleanup |
| `cd web/classic && bun run i18n:lint` | Passed after final cleanup, 307 current issues equal 307 baseline issues |
| `git diff --check` | Passed after final cleanup |
| `cd web/default && bun run build` | Passed after final cleanup, built in 1 m 10.9 s |
| `cd web/classic && bun run build:fast` | Passed after final cleanup, built in 2 m 15 s |
| Added decorative glyph scans over `v1.1.0..worktree` | Passed, no matches for explicit decorative glyphs or `U+1F300..U+1FAFF` emoji codepoints |
| Added Go JSON/TODO/panic scan over `v1.1.0..worktree` | Passed; added JSON codec use is limited to `common/json.go` wrappers and one explanatory comment in `relay/channel/task/taskcommon/helpers.go`; no added `TODO: temporary` or `panic("implement me")` remains |
| Added secret-pattern scan over `v1.1.0..worktree` | Reviewed; matches are dummy test fixture keys in `controller/channel_header_profile_test.go`, not persisted credentials |
| Claude CLI current-diff review after user-requested recheck | Found the reintroduced protocol-conversion blocker: empty `model_patterns` match-all semantics and implicit channel-test global policy; fixed before final state |
| Gemini CLI current-diff review after user-requested recheck | Found the same protocol-conversion blocker and verified JSON wrapper, accessibility, Unicode cleanup, and i18n changes had no additional blocker; fixed before final state |
| Protocol side-effect scan after Claude/Gemini recheck fix | Passed, no current matches for empty-model match-all tests/copy or channel-test global-policy helper/tests |
| Protocol side-effect diff check after Claude/Gemini recheck fix | Passed, no residual current protocol-conversion-related diff in checked controller files, `service/openaicompat`, `setting/model_setting`, default protocol policy files, or classic protocol policy files |
| `go test -count=1 -timeout=60s ./controller ./service/openaicompat ./setting/model_setting` | Passed after Claude/Gemini recheck fix |
| `cd web/default && bun test tests/protocol-conversion-policy-utils.test.ts` | Passed after Claude/Gemini recheck fix, 17 tests |
| `cd web/classic && bun run test:node -- protocolConversionPolicyUtils.test.js` | Passed after Claude/Gemini recheck fix, script ran the classic node suite, 120 tests |

## Current Diff Review Follow-up

- Fixed the `classic-to-default-sync` skill triage table so its header and rows have matching columns.
- Fixed the `formatLocalCurrencyAmount` example comment so it no longer implies the helper applies exchange-rate conversion.
- Updated the classic deployment details modal to prefer `details.status` and fall back to `deployment.status`.
- Added a visible warning banner and second confirmation step when disabling SSRF protection in classic system settings.
- Kept the high-risk status-code retry modal unchanged because it already renders through `RiskAcknowledgementModal` with a warning icon, danger confirm action, checklist, and typed confirmation.
- Replaced the default channel-affinity advanced section inline triangle glyphs with lucide icons.
- Rewrote the `formatCurrencyFromUSD` recharge-rate examples as ASCII-only text.
- Moved Header Profile streaming decoder type references behind `common.JsonDecoder`, `common.JsonDelim`, and `common.JsonNumber`.
- Compared `panic("implement me")` in `relay/channel/**/adaptor.go` against `v1.1.0`; current active panic sites are historical baseline residue, not newly introduced by this range.
- Replaced classic quota/extended-price expand/collapse triangle text with icon components.
- Normalized added Markdown and comments to ASCII in the low-risk docs/comment surface; retained semantic UI, i18n, formula, mask, and math-symbol glyphs.
- Fixed the classic i18n lint command path so it runs the shared gate, then resolved the real new payment strings before refreshing the baseline.
- Replaced the added deployment-location globe emoji with `FaMapMarkerAlt`.
- Fixed newly added classic payment-setting guidance text so it goes through i18n, removed decorative switch glyphs, and refreshed `web/tests/i18nLintBaseline.json` only after the real new strings were fixed.
- Replaced newly added Claude extended-thinking temporary TODO comments with stable parameter-constraint comments.
- Fixed CodeRabbit's valid accessibility finding by making the classic raw-quota disclosure rows keyboard reachable and exposing `aria-expanded` / `aria-controls`; marked the chevron icons as decorative.
- Verified Claude/Gemini's `FaMapMarkerAlt` import concern as a false positive against current code.
- Kept CodeRabbit's `i18nLintGate.mjs` directory-generalization suggestion as non-blocking because this repo has a single classic `i18next.config.js`, a shared `web/tests/i18nLintBaseline.json`, and the current script commands pass.
- Cleaned accidental Gemini CLI edits before final verification: restored the empty `model_patterns` non-match contract and removed the unintended channel-test global-policy test so the worktree does not contain that unapproved behavior change.
- Fixed CodeRabbit's later valid `i18nLintGate.mjs` clarity finding by naming the baseline output path as `baselineDisplayPath`.
- Verified CodeRabbit's later `relay/helper/price.go` arrow warning as a false positive against current source; the string contains literal `->`, while the review JSON escaped it as `-&gt;`.
- Removed CodeRabbit agent-mode side effects after the CLI reported `--prompt-only` now behaves like `--agent`; the rejected edits tried to change empty `model_patterns` semantics and channel-test global-policy behavior again.
- Repeated the protocol semantic cleanup after the same side effects reappeared in the worktree: empty model patterns remain non-matching, classic/default UI copy no longer says match-all, and channel test conversion remains explicit-only.
- Re-ran Claude and Gemini current-diff reviews on request; both independently found the reintroduced protocol-conversion blocker, which was removed again and verified with protocol scans and targeted tests.

## Runtime UI Evidence

- Default frontend was checked through the existing dev server at `http://127.0.0.1:5176`, proxying to the isolated backend on `3001`.
- Classic frontend was checked through the isolated dev container at `http://127.0.0.1:3001`, whose `/api/status` reports `theme:"classic"`.
- Isolated dev was force-recreated on 2026-05-21 08:42 +0800 and now runs image `sha256:2da427a9ed8f9f0062a54cf4e9f6c1a437794ed1838537be0f6ddb6b2a19de29`, with build-info commit `7497b8e4c71d0becff9fb5554b666257c9b3b8f4-dirty`.
- The post-recreate browser smoke screenshot was captured locally at `docs/reviews/artifacts/classic-home-3001-after-recreate-2026-05-21.jpg`.
- Captured screenshots are local review artifacts under `docs/reviews/artifacts/`; this directory is ignored by Git to avoid committing bulky binary evidence.

## Residual Boundary

- Full CodeRabbit review could not run because the release diff exceeds its file-count limit. Local review, static scans, builds, and tests were used instead.
- The final uncommitted-diff CodeRabbit rerun did not complete after the backend/report follow-up; it was stopped while stuck in setup/summarizing. Earlier current-diff CodeRabbit runs had passed before this follow-up.
- Classic keeps the upstream-required welcome console banner in `web/classic/src/index.jsx`; its content is now plain ASCII.
- A broader Unicode-symbol scan still finds semantic arrows, multiplication signs, bullets, masked-value glyphs, formula hints, token-estimator math symbols, generated OpenAPI auth icons, and pre-existing UI/i18n emoji examples. These were not mechanically normalized in this pass because they affect user-facing copy, generated docs, translation keys, masking, or domain semantics.
