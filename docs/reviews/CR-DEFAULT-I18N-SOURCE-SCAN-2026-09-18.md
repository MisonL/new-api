# Default i18n source key scan review

Date: 2026-09-18
Scope: `web/default` source translation key detection and locale parity.

## Changes

- Extended `web/default/scripts/sync-i18n.mjs` to scan `.ts`, `.tsx`, `.js`, and `.jsx` source files for static `t(...)` and `i18next.t(...)` calls.
- Added comment-aware and quote-aware parsing for single-quoted, double-quoted, and static template literals.
- Included `src/i18n/static-keys.ts` in the source key set for runtime-selected labels.
- Added the source scan result to `_sync-report.json` and made missing source keys fail `bun run i18n:sync`.
- Added the 47 previously undetected runtime-selected keys to `static-keys.ts` and all six Default locales. These cover system settings descriptions, Playground feedback, user operation feedback, Custom OAuth options, and billing conditions.

## Verification

Command: `bun run i18n:sync`

Result: exit code 0; 910 source files scanned, 4581 source keys, 0 missing source keys.

Locale report: `en`, `zh`, `fr`, `ja`, `ru`, and `vi` each have 0 missing, 0 extra, and 0 untranslated keys.

Failure-path check: a temporary source call with an unknown key returned exit code 1 and listed the missing key; the fixture was removed and the passing sync was rerun.

Additional checks:

- Locale JSON parse and key order parity: passed for 5123 keys across six locales.
- Placeholder parity: passed for all locale values.
- `./node_modules/.bin/eslint scripts/sync-i18n.mjs`: exit code 0.
- `./node_modules/.bin/prettier --check scripts/sync-i18n.mjs`: exit code 0.
- `bun run format:check`: exit code 0.
- `bun run lint`: exit code 0.
- `bun run build:check`: exit code 0.
- `git diff --check` on changed i18n files: exit code 0.

## Boundary

This review covers static source calls and the maintained dynamic key registry. Runtime-generated keys that are neither literal calls nor registered in `src/i18n/static-keys.ts` remain outside static analysis; the remaining dynamic call sites were reviewed and are either backed by the registry or intentionally pass server-provided/technical values through unchanged.
