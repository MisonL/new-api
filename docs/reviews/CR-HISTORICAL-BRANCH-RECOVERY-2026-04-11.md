# Historical Branch Recovery Audit

## Scope

- Repository: `new-api`
- Audit date: `2026-04-11`
- Baseline branch: `main`
- Target refs reviewed:
  - `origin/feat/auth-cas-sso`
  - `origin/feat/auth-cas-sso-phase3`
  - `origin/feat/auth-jwt-sso-mvp`
  - `origin/feat/dalle-extra`
  - `origin/feat/suno`

## Method

- Verified which user-authored upstream PR branches are already ancestors of `main`.
- Compared `main` against target refs with `git merge-base`, `git log`, `git diff --name-status`, and focused file diffs.
- Treated current code and tests as the plant, git history as the sensor, and applied only minimal control input for residual gaps.

## Findings

### 1. Historical upstream PR branches

The 12 branches that were actually used as upstream PR heads by the user are already ancestors of `main`. No further recovery work is required for that set.

### 2. Screenshot authentication branches

#### `origin/feat/auth-jwt-sso-mvp`

- This branch is an older JWT-direct phase.
- Direct diff against `main` shows it lacks current browser callback hardening, trusted-header support, CAS support, and later test coverage.
- Conclusion: do not merge this branch. `main` already supersedes it.

#### `origin/feat/auth-cas-sso`

- This branch sits between JWT-direct MVP and the later trusted-header/CAS polish work.
- Direct diff against `main` shows that it would remove newer browser callback and CAS code if merged as-is.
- Conclusion: do not merge this branch. `main` already supersedes it.

#### `origin/feat/auth-cas-sso-phase3`

- This branch contains the mature authentication line: JWT Direct, Trusted Header, and CAS.
- Current `main` already contains the core runtime files and routes for CAS, Trusted Header, and JWT Direct.
- Residual gaps found during audit:
  - README did not explicitly list CAS support.
  - CAS-specific backend default external ID mapping was not restored in `validateCustomOAuthProvider`.
  - CAS-focused tests from the phase branch were missing from `main`.
- Conclusion: recover residual deltas with minimal patches, not by merging the whole branch.

### 3. Long-lived feature forks

#### `origin/feat/dalle-extra`

- This ref is a long-lived divergent branch with thousands of commits relative to `main`.
- It is not suitable for direct merge or blanket recovery.

#### `origin/feat/suno`

- This ref is also a long-lived divergent branch with very large history drift.
- It is not suitable for direct merge or blanket recovery.

## Minimal Recovery Applied

- Restored CAS default external ID mapping for backend-created CAS providers.
- Restored CAS-focused unit and controller tests.
- Updated README enterprise SSO section to reflect current `main` capabilities.

## Gate Result

- `origin/feat/auth-jwt-sso-mvp`: rejected for merge, superseded by `main`
- `origin/feat/auth-cas-sso`: rejected for merge, superseded by `main`
- `origin/feat/auth-cas-sso-phase3`: partially recovered via minimal patches
- `origin/feat/dalle-extra`: blocked pending separate decomposition
- `origin/feat/suno`: blocked pending separate decomposition
