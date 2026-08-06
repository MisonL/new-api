#!/usr/bin/env bash
set -euo pipefail
umask 077

LOG_DIR="${NEW_API_LOG_RETENTION_DIR:-/Volumes/Work/code/new-api/logs}"
RETENTION_DAYS="${NEW_API_LOG_RETENTION_DAYS:-60}"
KEEP_FILES="${NEW_API_LOG_RETENTION_KEEP_FILES:-4}"
STATE_FILE="${NEW_API_LOG_RETENTION_STATE_FILE:-${HOME}/Library/Application Support/New API/log-retention/last-run}"
MIN_INTERVAL_SECONDS="${NEW_API_LOG_RETENTION_MIN_INTERVAL_SECONDS:-86400}"
ACTIVE_LOG_PATH="${NEW_API_LOG_RETENTION_ACTIVE_LOG_PATH:-}"
STAT_BIN="${NEW_API_LOG_RETENTION_STAT_BIN:-/usr/bin/stat}"
NOW_EPOCH="${NEW_API_LOG_RETENTION_NOW_EPOCH:-}"
MODE="check-only"

usage() {
  cat <<'EOF'
usage: macos-docker-tun-log-retention.sh [--check-only|--apply]

The default is --check-only. Only --apply removes files older than the
retention period, while preserving the newest files and the active path.
EOF
}

case "${1:---check-only}" in
  --check-only) MODE="check-only" ;;
  --apply) MODE="apply" ;;
  --help|-h) usage; exit 0 ;;
  *) usage >&2; exit 2 ;;
esac
if [ "$#" -gt 1 ]; then
  usage >&2
  exit 2
fi

normalize_uint() {
  local value="$1"
  local fallback="$2"
  case "$value" in
    ''|*[!0-9]*) printf '%s' "$fallback" ;;
    *) printf '%s' "$value" ;;
  esac
}

RETENTION_DAYS="$(normalize_uint "$RETENTION_DAYS" 60)"
KEEP_FILES="$(normalize_uint "$KEEP_FILES" 4)"
MIN_INTERVAL_SECONDS="$(normalize_uint "$MIN_INTERVAL_SECONDS" 86400)"
if [ "$KEEP_FILES" -lt 1 ]; then
  KEEP_FILES=1
fi

if [ -z "$LOG_DIR" ] || [ "$LOG_DIR" = "/" ] || [ ! -d "$LOG_DIR" ]; then
  printf 'retention_status=fail reason=invalid-log-dir path=%s\n' "$LOG_DIR" >&2
  exit 1
fi
if [ ! -x "$STAT_BIN" ]; then
  printf 'retention_status=fail reason=stat-unavailable path=%s\n' "$STAT_BIN" >&2
  exit 1
fi

if [ -z "$NOW_EPOCH" ]; then
  NOW_EPOCH="$(date '+%s')"
fi
NOW_EPOCH="$(normalize_uint "$NOW_EPOCH" 0)"
CUTOFF_EPOCH=$((NOW_EPOCH - RETENTION_DAYS * 86400))

if [ "$MODE" = "apply" ] && [ -f "$STATE_FILE" ] && [ "$MIN_INTERVAL_SECONDS" -gt 0 ]; then
  last_run="$(<"$STATE_FILE")"
  case "$last_run" in
    ''|*[!0-9]*) last_run=0 ;;
  esac
  if [ "$NOW_EPOCH" -ge "$last_run" ] && [ "$((NOW_EPOCH - last_run))" -lt "$MIN_INTERVAL_SECONDS" ]; then
    printf 'retention_status=skipped reason=interval last_run=%s next_after_seconds=%s\n' "$last_run" "$MIN_INTERVAL_SECONDS"
    exit 0
  fi
fi

files=()
while IFS= read -r path; do
  files+=("$path")
done < <(find "$LOG_DIR" -maxdepth 1 -type f -name 'oneapi-*.log' -print | LC_ALL=C sort -r)

selected=()
index=0
for path in "${files[@]}"; do
  index=$((index + 1))
  if [ "$index" -le "$KEEP_FILES" ] || { [ -n "$ACTIVE_LOG_PATH" ] && [ "$path" = "$ACTIVE_LOG_PATH" ]; }; then
    continue
  fi
  mtime="$($STAT_BIN -f '%m' "$path" 2>/dev/null || printf '0')"
  size="$($STAT_BIN -f '%z' "$path" 2>/dev/null || printf '0')"
  case "$mtime" in
    ''|*[!0-9]*)
      printf 'retention_skip path=%s reason=mtime-unreadable\n' "$path" >&2
      continue
      ;;
  esac
  if [ "$mtime" -lt "$CUTOFF_EPOCH" ]; then
    selected+=("$path")
    printf 'retention_candidate path=%s mtime=%s size=%s\n' "$path" "$mtime" "$size"
  fi
done

printf 'retention_status=plan mode=%s log_dir=%s files=%s candidates=%s retention_days=%s keep_files=%s cutoff_epoch=%s\n' \
  "$MODE" "$LOG_DIR" "${#files[@]}" "${#selected[@]}" "$RETENTION_DAYS" "$KEEP_FILES" "$CUTOFF_EPOCH"

if [ "$MODE" != "apply" ]; then
  exit 0
fi

deleted=0
failed=0
for path in "${selected[@]}"; do
  if rm -f "$path"; then
    deleted=$((deleted + 1))
    printf 'retention_deleted path=%s\n' "$path"
  else
    failed=$((failed + 1))
    printf 'retention_delete_failed path=%s\n' "$path" >&2
  fi
done

mkdir -p "$(dirname "$STATE_FILE")"
chmod 0700 "$(dirname "$STATE_FILE")"
printf '%s\n' "$NOW_EPOCH" >"$STATE_FILE"
chmod 0600 "$STATE_FILE"
printf 'retention_status=applied deleted=%s failed=%s state_file=%s\n' "$deleted" "$failed" "$STATE_FILE"
if [ "$failed" -gt 0 ]; then
  exit 1
fi
