#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/macos-docker-tun-log-retention.sh"
TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/new-api-log-retention-test.XXXXXX")"
trap 'rm -rf "$TMP_DIR"' EXIT

LOG_DIR="$TMP_DIR/logs"
STATE_FILE="$TMP_DIR/state/last-run"
mkdir -p "$LOG_DIR"
touch -t 202608060901 "$LOG_DIR/oneapi-20260806090100.log"
touch -t 202608050901 "$LOG_DIR/oneapi-20260805090100.log"
touch -t 202607310901 "$LOG_DIR/oneapi-20260731090100.log"
touch -t 202604010901 "$LOG_DIR/oneapi-20260401090100.log"

common_env=(
  "NEW_API_LOG_RETENTION_DIR=$LOG_DIR"
  "NEW_API_LOG_RETENTION_STATE_FILE=$STATE_FILE"
  "NEW_API_LOG_RETENTION_DAYS=60"
  "NEW_API_LOG_RETENTION_KEEP_FILES=2"
  "NEW_API_LOG_RETENTION_NOW_EPOCH=1785984000"
)

env "${common_env[@]}" "$SCRIPT" --check-only >"$TMP_DIR/check.out"
test -f "$LOG_DIR/oneapi-20260401090100.log"
grep -Fq 'retention_status=plan mode=check-only' "$TMP_DIR/check.out"
grep -Fq 'retention_candidate path='"$LOG_DIR"'/oneapi-20260401090100.log' "$TMP_DIR/check.out"

env "${common_env[@]}" "$SCRIPT" --apply >"$TMP_DIR/apply.out"
test ! -e "$LOG_DIR/oneapi-20260401090100.log"
test -f "$LOG_DIR/oneapi-20260806090100.log"
test -f "$LOG_DIR/oneapi-20260805090100.log"
test -f "$LOG_DIR/oneapi-20260731090100.log"
grep -Fq 'retention_status=applied deleted=1 failed=0' "$TMP_DIR/apply.out"

env "${common_env[@]}" "$SCRIPT" --apply >"$TMP_DIR/skip.out"
grep -Fq 'retention_status=skipped reason=interval' "$TMP_DIR/skip.out"

printf 'macos-docker-tun-log-retention tests passed\n'
