# Compact E2E Image Traceability Review

## Scope

The compact control-plane E2E script can restart the isolated application many times. Before this change, Compose loaded `DEV_NEW_API_IMAGE` only from the environment file, so a run from a clean candidate worktree could silently recreate the service with an older `new-api-local:dev` image.

## Change

`scripts/compact-control-plane-e2e.sh` now accepts an explicit `COMPACT_E2E_DEV_NEW_API_IMAGE` and `COMPACT_E2E_EXPECTED_COMMIT`. When either is supplied, startup and restart use the requested image and verify the container image plus `/new-api --build-info` commit. Readiness and artifact failures are explicitly returned to the caller, including cleanup.

## Verification

Commands and results:

```text
bash -n scripts/compact-control-plane-e2e.sh scripts/compact-control-plane-e2e-lib.sh
exit=0

COMPACT_E2E_EXPECTED_COMMIT=not-a-real-commit bash scripts/compact-control-plane-e2e.sh
exit=1
result: commit mismatch was reported before any business case; database and Redis snapshots matched and the E2E lock was released.

COMPACT_E2E_DEV_NEW_API_IMAGE=new-api-local:cecc5a8a2-clean \
COMPACT_E2E_EXPECTED_COMMIT=cecc5a8a2d111a6d4246b1b273b8379962db9ac3 \
bash scripts/compact-control-plane-e2e.sh
exit=0
result: all compact, stream option, recoverable 413, synthetic, opaque replay, sub2api previous-response, model-switch, and Codex history cases passed; every restart matched the expected image and commit; database_snapshot_match=true; redis_snapshot_match=true; compact_e2e_lock_released=true; e2e_done=true.
```

The E2E used only the isolated `3001` application, PostgreSQL, Redis, and fake upstream. The formal service and formal database were not modified.
