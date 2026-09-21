# Classic and Default parity: usage log column order

Sampling time: 2026-09-19 Asia/Shanghai

## Finding

The Classic usage log page persisted a role-specific column order and exposed
visibility, move-up, move-down, and pointer-drag controls. Default already had
persisted visibility, but it did not pass `columnOrder` to TanStack Table and
had no order editor.

## Change

- Added normalized, persisted column-order state for Default usage logs.
- Migrated the existing Classic administrator and user order keys when the
  Default key is absent; stale keys are ignored and newly added columns are
  appended in the current source order.
- Added an accessible order dialog with visibility checkboxes, keyboard
  move-up/move-down buttons, reset, and desktop native drag-and-drop.
- Kept the common-log actions column fixed while allowing the other columns to
  move around hidden columns without losing their stored positions.
- Added pure-function tests for normalization, movement, fixed-column guards,
  and Classic key migration.

## Verification

| Command | Result |
| --- | --- |
| `cd web/default && bun test` | 205 pass, 0 fail, 866 expect calls |
| `cd web/default && bun run typecheck` | Exit 0 |
| `cd web/default && bun run lint` | Exit 0, no errors or warnings |
| `cd web/default && bun run format:check` | Exit 0 |
| `cd web/default && bun run i18n:sync` | Exit 0, source scan 0 missing keys |
| `cd web/default && bun run build` | Exit 0, Safari and browser compatibility checks passed |
| `cd web/classic && bun run test:node` | 137 pass, 0 fail |
| `cd web/classic && bun run eslint` | Exit 0 |
| `cd web/classic && bun run build` | Exit 0, 10817 modules transformed, Safari compatibility passed |
| `git diff --check` | Exit 0 |

## Boundary

The repository contains unrelated pre-existing dirty changes; they were not
reverted or cleaned. This change was verified in source and local build/test
environments only. The production service on port 13000 was not restarted,
rebuilt, or written to in this audit.

