#!/usr/bin/env bash
set -euo pipefail
umask 077

LOG_DIR="${NEW_API_LOG_RETENTION_DIR:-${NEW_API_LOG_DIR:-}}"
RETENTION_DAYS="${NEW_API_LOG_RETENTION_DAYS:-60}"
KEEP_FILES="${NEW_API_LOG_RETENTION_KEEP_FILES:-4}"
STATE_FILE="${NEW_API_LOG_RETENTION_STATE_FILE:-${HOME}/Library/Application Support/New API/log-retention/last-run}"
MIN_INTERVAL_SECONDS="${NEW_API_LOG_RETENTION_MIN_INTERVAL_SECONDS:-86400}"
ACTIVE_LOG_PATH="${NEW_API_LOG_RETENTION_ACTIVE_LOG_PATH:-}"
OPEN_PATH_CHECKER="${NEW_API_LOG_RETENTION_OPEN_PATH_CHECKER:-}"
OPEN_PATH_CHECK_TIMEOUT_SECONDS="${NEW_API_LOG_RETENTION_OPEN_PATH_CHECK_TIMEOUT_SECONDS:-10}"
STAT_BIN="${NEW_API_LOG_RETENTION_STAT_BIN:-/usr/bin/stat}"
VOLUME_PATH="${NEW_API_LOG_RETENTION_VOLUME_PATH:-}"
VOLUME_UUID="${NEW_API_LOG_RETENTION_VOLUME_UUID:-}"
DISKUTIL_BIN="${NEW_API_LOG_RETENTION_DISKUTIL_BIN:-/usr/sbin/diskutil}"
MOUNT_BIN="${NEW_API_LOG_RETENTION_MOUNT_BIN:-/sbin/mount}"
PLUTIL_BIN="${NEW_API_LOG_RETENTION_PLUTIL_BIN:-/usr/bin/plutil}"
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

require_uint() {
  local value="$1"
  local name="$2"
  case "$value" in
    ''|*[!0-9]*)
      printf 'retention_status=fail reason=invalid-config name=%s value=%s\n' "$name" "$value" >&2
      exit 2
      ;;
    *) printf '%s' "$value" ;;
  esac
}

RETENTION_DAYS="$(require_uint "$RETENTION_DAYS" NEW_API_LOG_RETENTION_DAYS)"
KEEP_FILES="$(require_uint "$KEEP_FILES" NEW_API_LOG_RETENTION_KEEP_FILES)"
MIN_INTERVAL_SECONDS="$(require_uint "$MIN_INTERVAL_SECONDS" NEW_API_LOG_RETENTION_MIN_INTERVAL_SECONDS)"
OPEN_PATH_CHECK_TIMEOUT_SECONDS="$(require_uint "$OPEN_PATH_CHECK_TIMEOUT_SECONDS" NEW_API_LOG_RETENTION_OPEN_PATH_CHECK_TIMEOUT_SECONDS)"
if [ "$OPEN_PATH_CHECK_TIMEOUT_SECONDS" -lt 1 ]; then
  printf 'retention_status=fail reason=invalid-config name=NEW_API_LOG_RETENTION_OPEN_PATH_CHECK_TIMEOUT_SECONDS value=%s\n' "$OPEN_PATH_CHECK_TIMEOUT_SECONDS" >&2
  exit 2
fi
if [ "$KEEP_FILES" -lt 1 ]; then
  printf 'retention_status=fail reason=invalid-config name=NEW_API_LOG_RETENTION_KEEP_FILES value=%s\n' "$KEEP_FILES" >&2
  exit 2
fi

if [ -z "$LOG_DIR" ] || [ "$LOG_DIR" = "/" ] || [ ! -d "$LOG_DIR" ] || [ -L "$LOG_DIR" ]; then
  printf 'retention_status=fail reason=invalid-log-dir path=%s\n' "$LOG_DIR" >&2
  exit 1
fi
case "$STATE_FILE" in
  /Users/*) ;;
  *) printf 'retention_status=fail reason=unexpected-state-file path=%s\n' "$STATE_FILE" >&2; exit 1 ;;
esac
STATE_PARENT="$(dirname "$STATE_FILE")"
if [ -L "$STATE_PARENT" ] || { [ -e "$STATE_PARENT" ] && [ ! -d "$STATE_PARENT" ]; }; then
  printf 'retention_status=fail reason=invalid-state-parent path=%s\n' "$STATE_PARENT" >&2
  exit 1
fi
if [ -L "$STATE_FILE" ] || { [ -e "$STATE_FILE" ] && [ ! -f "$STATE_FILE" ]; }; then
  printf 'retention_status=fail reason=invalid-state-file path=%s\n' "$STATE_FILE" >&2
  exit 1
fi
if [ -n "$ACTIVE_LOG_PATH" ]; then
  case "$ACTIVE_LOG_PATH" in
    "$LOG_DIR/"*) ;;
    *) printf 'retention_status=fail reason=invalid-active-log-path path=%s\n' "$ACTIVE_LOG_PATH" >&2; exit 1 ;;
  esac
fi
if [ -n "$OPEN_PATH_CHECKER" ] && [ ! -x "$OPEN_PATH_CHECKER" ]; then
  printf 'retention_status=fail reason=open-file-check-runtime-unavailable path=%s\n' "$OPEN_PATH_CHECKER" >&2
  exit 1
fi
if [ "$MODE" = "apply" ] && [ -z "$OPEN_PATH_CHECKER" ]; then
  printf 'retention_status=fail reason=missing-open-file-checker\n' >&2
  exit 1
fi
run_open_path_check() {
  /usr/bin/perl -e '
    use POSIX qw(:sys_wait_h);
    my $timeout = shift @ARGV;
    my $pid = fork();
    die "fork failed: $!\n" unless defined $pid;
    if ($pid == 0) {
      setpgrp(0, 0);
      exec @ARGV or exit 127;
    }
    $SIG{ALRM} = sub {
      kill "TERM", -$pid;
      my $deadline = time() + 1;
      while (time() < $deadline) {
        my $result = waitpid($pid, WNOHANG);
        exit 124 if $result == $pid;
        select undef, undef, undef, 0.1;
      }
      kill "KILL", -$pid;
      for (1 .. 10) {
        my $result = waitpid($pid, WNOHANG);
        last if $result == $pid || $result == -1;
        select undef, undef, undef, 0.1;
      }
      exit 124;
    };
    alarm $timeout;
    waitpid($pid, 0);
    alarm 0;
    my $status = $?;
    exit WEXITSTATUS($status) if WIFEXITED($status);
    exit 128 + WTERMSIG($status) if WIFSIGNALED($status);
    exit 1;
  ' "$OPEN_PATH_CHECK_TIMEOUT_SECONDS" "$OPEN_PATH_CHECKER" "$1"
}
if [ -n "$VOLUME_PATH" ]; then
  case "$LOG_DIR" in
    "$VOLUME_PATH"|"$VOLUME_PATH"/*) ;;
    *) printf 'retention_status=fail reason=unexpected-log-dir path=%s\n' "$LOG_DIR" >&2; exit 1 ;;
  esac
fi
if [ ! -x "$STAT_BIN" ]; then
  printf 'retention_status=fail reason=stat-unavailable path=%s\n' "$STAT_BIN" >&2
  exit 1
fi
if [ -n "$VOLUME_PATH" ] || [ -n "$VOLUME_UUID" ]; then
  if [ -z "$VOLUME_PATH" ] || [ -z "$VOLUME_UUID" ] || [ ! -d "$VOLUME_PATH" ] || [ -L "$VOLUME_PATH" ] || \
    [ ! -x "$DISKUTIL_BIN" ] || [ ! -x "$MOUNT_BIN" ] || [ ! -x "$PLUTIL_BIN" ]; then
    printf 'retention_status=fail reason=invalid-volume-guard path=%s uuid=%s\n' "$VOLUME_PATH" "$VOLUME_UUID" >&2
    exit 1
  fi
  mount_table="$("$MOUNT_BIN" 2>/dev/null || true)"
  if ! printf '%s\n' "$mount_table" | /usr/bin/grep -Fq " on $VOLUME_PATH ("; then
    printf 'retention_status=fail reason=volume-unmounted path=%s\n' "$VOLUME_PATH" >&2
    exit 1
  fi
  actual_uuid="$("$DISKUTIL_BIN" info -plist "$VOLUME_PATH" 2>/dev/null | "$PLUTIL_BIN" -extract VolumeUUID raw -o - - 2>/dev/null || true)"
  if [ "$actual_uuid" != "$VOLUME_UUID" ]; then
    printf 'retention_status=fail reason=volume-uuid-mismatch path=%s expected=%s actual=%s\n' "$VOLUME_PATH" "$VOLUME_UUID" "$actual_uuid" >&2
    exit 1
  fi
fi

if [ -z "$NOW_EPOCH" ]; then
  NOW_EPOCH="$(date '+%s')"
fi
NOW_EPOCH="$(require_uint "$NOW_EPOCH" NEW_API_LOG_RETENTION_NOW_EPOCH)"
CUTOFF_EPOCH=$((NOW_EPOCH - RETENTION_DAYS * 86400))

if [ "$MODE" = "apply" ] && [ -f "$STATE_FILE" ] && [ "$MIN_INTERVAL_SECONDS" -gt 0 ]; then
  last_run="$(<"$STATE_FILE")"
  case "$last_run" in
    ''|*[!0-9]*)
      printf 'retention_status=fail reason=invalid-state-value path=%s value=%s\n' "$STATE_FILE" "$last_run" >&2
      exit 1
      ;;
  esac
  if [ "$last_run" -gt "$NOW_EPOCH" ]; then
    printf 'retention_status=fail reason=state-in-future path=%s value=%s now=%s\n' "$STATE_FILE" "$last_run" "$NOW_EPOCH" >&2
    exit 1
  fi
  if [ "$NOW_EPOCH" -ge "$last_run" ] && [ "$((NOW_EPOCH - last_run))" -lt "$MIN_INTERVAL_SECONDS" ]; then
    printf 'retention_status=skipped reason=interval last_run=%s next_after_seconds=%s\n' "$last_run" "$MIN_INTERVAL_SECONDS"
    exit 0
  fi
fi

discovery_file="${TMPDIR:-/tmp}/new-api-log-retention.XXXXXX"
discovery_file="$(mktemp "$discovery_file")"
trap 'rm -f "$discovery_file"' EXIT HUP INT TERM
if ! find -P "$LOG_DIR" -xdev -maxdepth 1 -type f -name 'oneapi-*.log' \
    -exec "$STAT_BIN" -f $'%m\t%N' {} + |
    LC_ALL=C sort -t $'\t' -k1,1nr -k2,2 |
    cut -f2- > "$discovery_file"; then
  printf 'retention_status=fail reason=log-discovery-failed path=%s\n' "$LOG_DIR" >&2
  exit 1
fi

files=()
while IFS= read -r path; do
  files+=("$path")
done < "$discovery_file"
rm -f "$discovery_file"

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
skipped_open=0
for path in "${selected[@]}"; do
  case "$path" in
    "$LOG_DIR/"*) ;;
    *)
      failed=$((failed + 1))
      printf 'retention_delete_failed path=%s reason=path-recheck-failed\n' "$path" >&2
      continue
      ;;
  esac
  if [ -L "$path" ] || [ ! -f "$path" ]; then
    failed=$((failed + 1))
    printf 'retention_delete_failed path=%s reason=target-not-regular\n' "$path" >&2
    continue
  fi
  current_mtime="$("$STAT_BIN" -f '%m' "$path" 2>/dev/null || printf '0')"
  case "$current_mtime" in
    ''|*[!0-9]*)
      failed=$((failed + 1))
      printf 'retention_delete_failed path=%s reason=mtime-recheck-failed\n' "$path" >&2
      continue
      ;;
  esac
  if [ "$current_mtime" -ge "$CUTOFF_EPOCH" ]; then
    printf 'retention_skip path=%s reason=mtime-changed\n' "$path"
    continue
  fi
  if [ -n "$OPEN_PATH_CHECKER" ]; then
    set +e
    run_open_path_check "$path" >/dev/null 2>&1
    open_check_rc=$?
    set -e
    case "$open_check_rc" in
      0)
        skipped_open=$((skipped_open + 1))
        printf 'retention_skip path=%s reason=open-file\n' "$path"
        continue
        ;;
      1) ;;
      124|142)
        skipped_open=$((skipped_open + 1))
        printf 'retention_skip path=%s reason=open-file-check-timeout timeout_seconds=%s\n' "$path" "$OPEN_PATH_CHECK_TIMEOUT_SECONDS"
        continue
        ;;
      *)
        failed=$((failed + 1))
        printf 'retention_delete_failed path=%s reason=open-file-check-failed exit=%s\n' "$path" "$open_check_rc" >&2
        continue
        ;;
    esac
  fi
  if rm -f "$path"; then
    deleted=$((deleted + 1))
    printf 'retention_deleted path=%s\n' "$path"
  else
    failed=$((failed + 1))
    printf 'retention_delete_failed path=%s\n' "$path" >&2
  fi
done

if [ "$failed" -gt 0 ]; then
  printf 'retention_status=apply_failed deleted=%s failed=%s skipped_open=%s state_file=%s\n' "$deleted" "$failed" "$skipped_open" "$STATE_FILE"
  exit 1
fi
mkdir -p "$STATE_PARENT"
chmod 0700 "$STATE_PARENT"
printf '%s\n' "$NOW_EPOCH" >"$STATE_FILE"
chmod 0600 "$STATE_FILE"
printf 'retention_status=applied deleted=%s failed=%s skipped_open=%s state_file=%s\n' "$deleted" "$failed" "$skipped_open" "$STATE_FILE"
