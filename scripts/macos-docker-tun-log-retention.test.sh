#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/macos-docker-tun-log-retention.sh"
TMP_DIR="$(mktemp -d "${HOME}/.new-api-log-retention-test.XXXXXX")"
trap 'rm -rf "$TMP_DIR"' EXIT

LOG_DIR="$TMP_DIR/logs"
STATE_FILE="$TMP_DIR/state/last-run"
mkdir -p "$LOG_DIR"
OPEN_CHECKER="$TMP_DIR/check-open"
printf '#!/usr/bin/env bash\nexit 1\n' >"$OPEN_CHECKER"
chmod +x "$OPEN_CHECKER"
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
  "NEW_API_LOG_RETENTION_OPEN_PATH_CHECKER=$OPEN_CHECKER"
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

VOLUME_DIR="$TMP_DIR/volume"
VOLUME_LOG_DIR="$VOLUME_DIR/logs"
VOLUME_STATE_FILE="$TMP_DIR/volume-state/last-run"
MOUNT_BIN="$TMP_DIR/fake-mount"
DISKUTIL_BIN="$TMP_DIR/fake-diskutil"
PLUTIL_BIN="$TMP_DIR/fake-plutil"
mkdir -p "$VOLUME_LOG_DIR"
touch -t 202608060901 "$VOLUME_LOG_DIR/oneapi-20260806090100.log"
touch -t 202604010901 "$VOLUME_LOG_DIR/oneapi-20260401090100.log"
printf '#!/usr/bin/env bash\nprintf "fake-device on %s (apfs, local, journaled)\\n"\n' "$VOLUME_DIR" >"$MOUNT_BIN"
printf '#!/usr/bin/env bash\nprintf "fake-plist\\n"\n' >"$DISKUTIL_BIN"
printf '#!/usr/bin/env bash\nprintf "generic-volume-uuid\\n"\n' >"$PLUTIL_BIN"
chmod +x "$MOUNT_BIN" "$DISKUTIL_BIN" "$PLUTIL_BIN"

volume_env=(
  "NEW_API_LOG_RETENTION_DIR=$VOLUME_LOG_DIR"
  "NEW_API_LOG_RETENTION_STATE_FILE=$VOLUME_STATE_FILE"
  "NEW_API_LOG_RETENTION_DAYS=60"
  "NEW_API_LOG_RETENTION_KEEP_FILES=1"
  "NEW_API_LOG_RETENTION_NOW_EPOCH=1785984000"
  "NEW_API_LOG_RETENTION_VOLUME_PATH=$VOLUME_DIR"
  "NEW_API_LOG_RETENTION_VOLUME_UUID=generic-volume-uuid"
  "NEW_API_LOG_RETENTION_MOUNT_BIN=$MOUNT_BIN"
  "NEW_API_LOG_RETENTION_DISKUTIL_BIN=$DISKUTIL_BIN"
  "NEW_API_LOG_RETENTION_PLUTIL_BIN=$PLUTIL_BIN"
)
env "${volume_env[@]}" "$SCRIPT" --check-only >"$TMP_DIR/volume.out"
grep -Fq 'retention_status=plan mode=check-only' "$TMP_DIR/volume.out"
grep -Fq 'retention_candidate path='"$VOLUME_LOG_DIR"'/oneapi-20260401090100.log' "$TMP_DIR/volume.out"

printf 'macos-docker-tun-log-retention tests passed\n'
