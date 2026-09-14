#!/usr/bin/env bash
set -euo pipefail
umask 077

DOCKER_BIN="${NEW_API_RECOVERY_DOCKER_BIN:-}"
LAUNCHCTL_BIN="${NEW_API_RECOVERY_LAUNCHCTL_BIN:-}"
APP_CONTAINER="${NEW_API_RECOVERY_APP_CONTAINER:-new-api}"
POSTGRES_CONTAINER="${NEW_API_RECOVERY_POSTGRES_CONTAINER:-postgres}"
REDIS_CONTAINER="${NEW_API_RECOVERY_REDIS_CONTAINER:-redis}"
POSTGRES_USER="${NEW_API_RECOVERY_POSTGRES_USER:-root}"
POSTGRES_DB="${NEW_API_RECOVERY_POSTGRES_DB:-new-api}"
POSTGRES_PASSWORD="${NEW_API_RECOVERY_POSTGRES_PASSWORD:-}"
POSTGRES_PASSWORD_FILE="${NEW_API_RECOVERY_POSTGRES_PASSWORD_FILE:-}"
REDIS_PASSWORD="${NEW_API_RECOVERY_REDIS_PASSWORD:-}"
REDIS_PASSWORD_FILE="${NEW_API_RECOVERY_REDIS_PASSWORD_FILE:-}"
RECOVERY_ENV_FILE="${NEW_API_RECOVERY_ENV_FILE:-}"
BACKEND_URL="${NEW_API_RECOVERY_BACKEND_URL:-http://127.0.0.1:13000/api/healthz}"
PUBLIC_URL="${NEW_API_RECOVERY_PUBLIC_URL:-http://127.0.0.1:3000/api/healthz}"
LAN_URL="${NEW_API_RECOVERY_LAN_URL:-}"
CADDY_LOG_FILE="${NEW_API_RECOVERY_CADDY_LOG_FILE:-${HOME}/Library/Application Support/New API/caddy/new-api-macos-tun-caddy-3000.log}"
RECOVERY_LOG_FILE="${NEW_API_RECOVERY_LOG_FILE:-${HOME}/Library/Application Support/New API/recovery/recovery.log}"
CURL_BIN="${NEW_API_RECOVERY_CURL_BIN:-}"
PGREP_BIN="${NEW_API_RECOVERY_PGREP_BIN:-/usr/bin/pgrep}"
OPEN_BIN="${NEW_API_RECOVERY_OPEN_BIN:-/usr/bin/open}"
DOCKER_SOCKET="${NEW_API_RECOVERY_DOCKER_SOCKET:-}"
CADDY_LABEL="${NEW_API_RECOVERY_CADDY_LABEL:-${CADDY_LAUNCHD_LABEL:-}}"
CADDY_LABEL_EXPLICIT=0
if [ -n "${NEW_API_RECOVERY_CADDY_LABEL:-}" ] || [ -n "${CADDY_LAUNCHD_LABEL:-}" ]; then
  CADDY_LABEL_EXPLICIT=1
fi
CADDY_PLIST="${NEW_API_RECOVERY_CADDY_PLIST:-}"
CADDY_PLIST_EXPLICIT=0
if [ -n "${NEW_API_RECOVERY_CADDY_PLIST:-}" ]; then
  CADDY_PLIST_EXPLICIT=1
fi
if [ -z "$CADDY_LABEL" ] && [ -n "$CADDY_PLIST" ]; then
  CADDY_LABEL="$(basename "$CADDY_PLIST" .plist)"
fi
CADDY_LABEL="${CADDY_LABEL:-com.new-api.macos-tun.new-api.3000}"
if [ -z "$CADDY_PLIST" ]; then
  CADDY_PLIST="${HOME}/Library/LaunchAgents/${CADDY_LABEL}.plist"
fi
DOCKER_APP_PATH="${NEW_API_RECOVERY_DOCKER_APP_PATH:-/Applications/Docker.app}"
STATE_FILE="${NEW_API_RECOVERY_STATE_FILE:-${HOME}/Library/Application Support/New API/recovery/state}"
STATE_DIR="$(dirname "$STATE_FILE")"
HEALTH_SNAPSHOT_FILE="${NEW_API_RECOVERY_HEALTH_SNAPSHOT_FILE:-${STATE_FILE}.snapshot}"
LOCK_DIR="${NEW_API_RECOVERY_LOCK_DIR:-${STATE_FILE}.lock}"
APP_FAILURE_COUNT_FILE="${NEW_API_RECOVERY_APP_FAILURE_COUNT_FILE:-${STATE_FILE}.app-failures}"
DOCKER_FAILURE_COUNT_FILE="${NEW_API_RECOVERY_DOCKER_FAILURE_COUNT_FILE:-${STATE_FILE}.docker-failures}"
APP_RESTART_STAMP_FILE="${NEW_API_RECOVERY_APP_RESTART_STAMP_FILE:-${STATE_FILE}.app-restart-at}"
DOCKER_RESTART_STAMP_FILE="${NEW_API_RECOVERY_DOCKER_RESTART_STAMP_FILE:-${STATE_FILE}.docker-restart-at}"
DOCKER_START_FAILURE_COUNT_FILE="${NEW_API_RECOVERY_DOCKER_START_FAILURE_COUNT_FILE:-${STATE_FILE}.docker-start-failures}"
DOCKER_START_STAMP_FILE="${NEW_API_RECOVERY_DOCKER_START_STAMP_FILE:-${STATE_FILE}.docker-start-at}"
APP_RESTART_FAILURE_COUNT_FILE="${NEW_API_RECOVERY_APP_RESTART_FAILURE_COUNT_FILE:-${STATE_FILE}.app-restart-failures}"
DOCKER_RESTART_FAILURE_COUNT_FILE="${NEW_API_RECOVERY_DOCKER_RESTART_FAILURE_COUNT_FILE:-${STATE_FILE}.docker-restart-failures}"
CADDY_FAILURE_COUNT_FILE="${NEW_API_RECOVERY_CADDY_FAILURE_COUNT_FILE:-${STATE_FILE}.caddy-failures}"
CADDY_RESTART_FAILURE_COUNT_FILE="${NEW_API_RECOVERY_CADDY_RESTART_FAILURE_COUNT_FILE:-${STATE_FILE}.caddy-restart-failures}"
CADDY_RESTART_STAMP_FILE="${NEW_API_RECOVERY_CADDY_RESTART_STAMP_FILE:-${STATE_FILE}.caddy-restart-at}"
LAN_FAILURE_COUNT_FILE="${NEW_API_RECOVERY_LAN_FAILURE_COUNT_FILE:-${STATE_FILE}.lan-failures}"
CADDY_RESTART_FAILURE_THRESHOLD="${NEW_API_RECOVERY_CADDY_RESTART_FAILURE_THRESHOLD:-${NEW_API_RECOVERY_PUBLIC_FAILURE_THRESHOLD:-2}}"
ATTEMPTS="${NEW_API_RECOVERY_ATTEMPTS:-3}"
DELAY_SECONDS="${NEW_API_RECOVERY_DELAY_SECONDS:-2}"
CURL_CONNECT_TIMEOUT="${NEW_API_RECOVERY_CURL_CONNECT_TIMEOUT:-1}"
CURL_MAX_TIME="${NEW_API_RECOVERY_CURL_MAX_TIME:-10}"
DOCKER_PING_CONNECT_TIMEOUT="${NEW_API_RECOVERY_DOCKER_PING_CONNECT_TIMEOUT:-1}"
DOCKER_PING_MAX_TIME="${NEW_API_RECOVERY_DOCKER_PING_MAX_TIME:-3}"
CADDY_LOG_ROTATION_MAX_BYTES="${NEW_API_RECOVERY_CADDY_LOG_ROTATION_MAX_BYTES:-52428800}"
CADDY_LOG_ROTATION_KEEP="${NEW_API_RECOVERY_CADDY_LOG_ROTATION_KEEP:-5}"
RECOVERY_LOG_ROTATION_MAX_BYTES="${NEW_API_RECOVERY_LOG_ROTATION_MAX_BYTES:-10485760}"
RECOVERY_LOG_ROTATION_KEEP="${NEW_API_RECOVERY_LOG_ROTATION_KEEP:-3}"
APP_RESTART_FAILURE_THRESHOLD="${NEW_API_RECOVERY_APP_RESTART_FAILURE_THRESHOLD:-3}"
APP_RESTART_MAX_FAILURES="${NEW_API_RECOVERY_APP_RESTART_MAX_FAILURES:-3}"
DOCKER_RESTART_FAILURE_THRESHOLD="${NEW_API_RECOVERY_DOCKER_RESTART_FAILURE_THRESHOLD:-3}"
CADDY_RESTART_MAX_FAILURES="${NEW_API_RECOVERY_CADDY_RESTART_MAX_FAILURES:-3}"
CADDY_LAUNCHCTL_COMMAND_TIMEOUT_SECONDS="${NEW_API_RECOVERY_CADDY_LAUNCHCTL_COMMAND_TIMEOUT_SECONDS:-5}"
CADDY_GRACEFUL_TIMEOUT_SECONDS="${NEW_API_RECOVERY_CADDY_GRACEFUL_TIMEOUT_SECONDS:-30}"
CADDY_GRACEFUL_POLL_INTERVAL_SECONDS="${NEW_API_RECOVERY_CADDY_GRACEFUL_POLL_INTERVAL_SECONDS:-1}"
RESTART_COOLDOWN_SECONDS="${NEW_API_RECOVERY_RESTART_COOLDOWN_SECONDS:-600}"
MAX_RESTART_COOLDOWN_SECONDS="${NEW_API_RECOVERY_MAX_RESTART_COOLDOWN_SECONDS:-3600}"
APP_RESTART_TIMEOUT_SECONDS="${NEW_API_RECOVERY_APP_RESTART_TIMEOUT_SECONDS:-20}"
APP_RESTART_COMMAND_TIMEOUT_SECONDS="${NEW_API_RECOVERY_APP_RESTART_COMMAND_TIMEOUT_SECONDS:-30}"
APP_RESTART_COMMAND_GRACE_SECONDS="${NEW_API_RECOVERY_APP_RESTART_COMMAND_GRACE_SECONDS:-10}"
APP_RESTART_STATUS_COMMAND_TIMEOUT_SECONDS="${NEW_API_RECOVERY_APP_RESTART_STATUS_COMMAND_TIMEOUT_SECONDS:-5}"
DOCKER_COMMAND_TIMEOUT_SECONDS="${NEW_API_RECOVERY_DOCKER_COMMAND_TIMEOUT_SECONDS:-15}"
DOCKER_DESKTOP_COMMAND_TIMEOUT_SECONDS="${NEW_API_RECOVERY_DOCKER_DESKTOP_COMMAND_TIMEOUT_SECONDS:-75}"
DOCKER_DESKTOP_COMMAND_GRACE_SECONDS="${NEW_API_RECOVERY_DOCKER_DESKTOP_COMMAND_GRACE_SECONDS:-15}"
DOCKER_DESKTOP_STOP_TIMEOUT_SECONDS="${NEW_API_RECOVERY_DOCKER_DESKTOP_STOP_TIMEOUT_SECONDS:-60}"
DOCKER_DESKTOP_RECOVERY_ATTEMPTS="${NEW_API_RECOVERY_DOCKER_DESKTOP_RECOVERY_ATTEMPTS:-6}"
DOCKER_DESKTOP_RECOVERY_DELAY_SECONDS="${NEW_API_RECOVERY_DOCKER_DESKTOP_RECOVERY_DELAY_SECONDS:-5}"
DOCKER_START_COOLDOWN_SECONDS="${NEW_API_RECOVERY_DOCKER_START_COOLDOWN_SECONDS:-60}"
DOCKER_START_MAX_COOLDOWN_SECONDS="${NEW_API_RECOVERY_DOCKER_START_MAX_COOLDOWN_SECONDS:-900}"
MISSING_CONTAINER_RETRY_COOLDOWN_SECONDS="${NEW_API_RECOVERY_MISSING_CONTAINER_RETRY_COOLDOWN_SECONDS:-300}"
MISSING_CONTAINER_MAX_RETRY_COOLDOWN_SECONDS="${NEW_API_RECOVERY_MISSING_CONTAINER_MAX_RETRY_COOLDOWN_SECONDS:-3600}"
FORCE_DOCKER_RESTART="${NEW_API_RECOVERY_FORCE_DOCKER_RESTART:-0}"
DRY_RUN="${NEW_API_RECOVERY_DRY_RUN:-0}"
WRITE_DRY_RUN_SNAPSHOT="${NEW_API_RECOVERY_WRITE_DRY_RUN_SNAPSHOT:-0}"
MONITOR_ONLY="${NEW_API_RECOVERY_MONITOR_ONLY:-0}"
LOCK_HELD=0
SNAPSHOT_STATE="unknown"
SNAPSHOT_DOCKER_DAEMON="unknown"
SNAPSHOT_POSTGRES="unknown"
SNAPSHOT_REDIS="unknown"
SNAPSHOT_POSTGRES_DOCKER_HEALTH="unknown"
SNAPSHOT_REDIS_DOCKER_HEALTH="unknown"
SNAPSHOT_BACKEND="unknown"
SNAPSHOT_PUBLIC="unknown"
SNAPSHOT_LAN="unknown"
SNAPSHOT_APP_STATE="unknown"
SNAPSHOT_APP_HEALTH="unknown"
SNAPSHOT_APP_RESTART_COUNT="unknown"
SNAPSHOT_APP_OOM="unknown"
SNAPSHOT_FAILURE_REASON="none"
SNAPSHOT_ACTION="none"
APP_HEALTH_DRIFT=0

normalize_uint() {
  local value="$1"
  local fallback="$2"
  case "$value" in
    ''|*[!0-9]*) printf '%s' "$fallback" ;;
    *) printf '%s' "$value" ;;
  esac
}

normalize_positive_uint() {
  local configured="$1"
  local fallback="$2"
  local name="$3"
  local value
  value="$(normalize_uint "$configured" "$fallback")"
  if [ "$value" -lt 1 ]; then
    printf 'invalid recovery timeout name=%s configured=%s fallback=%s\n' "$name" "$configured" "$fallback" >&2
    printf '%s' "$fallback"
    return 0
  fi
  printf '%s' "$value"
}

ATTEMPTS="$(normalize_uint "$ATTEMPTS" 3)"
DELAY_SECONDS="$(normalize_uint "$DELAY_SECONDS" 2)"
CURL_CONNECT_TIMEOUT="$(normalize_positive_uint "$CURL_CONNECT_TIMEOUT" 1 curl-connect-timeout)"
CURL_MAX_TIME="$(normalize_positive_uint "$CURL_MAX_TIME" 10 curl-max-time)"
DOCKER_PING_CONNECT_TIMEOUT="$(normalize_positive_uint "$DOCKER_PING_CONNECT_TIMEOUT" 1 docker-ping-connect-timeout)"
DOCKER_PING_MAX_TIME="$(normalize_positive_uint "$DOCKER_PING_MAX_TIME" 3 docker-ping-max-time)"
CADDY_LOG_ROTATION_MAX_BYTES="$(normalize_uint "$CADDY_LOG_ROTATION_MAX_BYTES" 52428800)"
CADDY_LOG_ROTATION_KEEP="$(normalize_uint "$CADDY_LOG_ROTATION_KEEP" 5)"
if [ "$CADDY_LOG_ROTATION_KEEP" -lt 1 ]; then
  CADDY_LOG_ROTATION_KEEP=1
fi
RECOVERY_LOG_ROTATION_MAX_BYTES="$(normalize_uint "$RECOVERY_LOG_ROTATION_MAX_BYTES" 10485760)"
RECOVERY_LOG_ROTATION_KEEP="$(normalize_uint "$RECOVERY_LOG_ROTATION_KEEP" 3)"
if [ "$RECOVERY_LOG_ROTATION_KEEP" -lt 1 ]; then
  RECOVERY_LOG_ROTATION_KEEP=1
fi
APP_RESTART_FAILURE_THRESHOLD="$(normalize_uint "$APP_RESTART_FAILURE_THRESHOLD" 3)"
APP_RESTART_MAX_FAILURES="$(normalize_uint "$APP_RESTART_MAX_FAILURES" 3)"
DOCKER_RESTART_FAILURE_THRESHOLD="$(normalize_uint "$DOCKER_RESTART_FAILURE_THRESHOLD" 3)"
CADDY_RESTART_FAILURE_THRESHOLD="$(normalize_uint "$CADDY_RESTART_FAILURE_THRESHOLD" 2)"
CADDY_RESTART_MAX_FAILURES="$(normalize_uint "$CADDY_RESTART_MAX_FAILURES" 3)"
CADDY_LAUNCHCTL_COMMAND_TIMEOUT_SECONDS="$(normalize_positive_uint "$CADDY_LAUNCHCTL_COMMAND_TIMEOUT_SECONDS" 5 caddy-launchctl-command-timeout)"
CADDY_GRACEFUL_TIMEOUT_SECONDS="$(normalize_uint "$CADDY_GRACEFUL_TIMEOUT_SECONDS" 30)"
CADDY_GRACEFUL_POLL_INTERVAL_SECONDS="$(normalize_uint "$CADDY_GRACEFUL_POLL_INTERVAL_SECONDS" 1)"
RESTART_COOLDOWN_SECONDS="$(normalize_uint "$RESTART_COOLDOWN_SECONDS" 600)"
MAX_RESTART_COOLDOWN_SECONDS="$(normalize_uint "$MAX_RESTART_COOLDOWN_SECONDS" 3600)"
APP_RESTART_TIMEOUT_SECONDS="$(normalize_uint "$APP_RESTART_TIMEOUT_SECONDS" 20)"
APP_RESTART_COMMAND_TIMEOUT_SECONDS="$(normalize_positive_uint "$APP_RESTART_COMMAND_TIMEOUT_SECONDS" 30 app-restart-command-timeout)"
APP_RESTART_COMMAND_GRACE_SECONDS="$(normalize_uint "$APP_RESTART_COMMAND_GRACE_SECONDS" 10)"
APP_RESTART_STATUS_COMMAND_TIMEOUT_SECONDS="$(normalize_positive_uint "$APP_RESTART_STATUS_COMMAND_TIMEOUT_SECONDS" 5 app-restart-status-command-timeout)"
DOCKER_COMMAND_TIMEOUT_SECONDS="$(normalize_positive_uint "$DOCKER_COMMAND_TIMEOUT_SECONDS" 15 docker-command-timeout)"
DOCKER_DESKTOP_COMMAND_TIMEOUT_SECONDS="$(normalize_positive_uint "$DOCKER_DESKTOP_COMMAND_TIMEOUT_SECONDS" 75 docker-desktop-command-timeout)"
DOCKER_DESKTOP_COMMAND_GRACE_SECONDS="$(normalize_uint "$DOCKER_DESKTOP_COMMAND_GRACE_SECONDS" 15)"
DOCKER_DESKTOP_STOP_TIMEOUT_SECONDS="$(normalize_uint "$DOCKER_DESKTOP_STOP_TIMEOUT_SECONDS" 60)"
DOCKER_DESKTOP_RECOVERY_ATTEMPTS="$(normalize_uint "$DOCKER_DESKTOP_RECOVERY_ATTEMPTS" 6)"
DOCKER_DESKTOP_RECOVERY_DELAY_SECONDS="$(normalize_uint "$DOCKER_DESKTOP_RECOVERY_DELAY_SECONDS" 5)"
DOCKER_START_COOLDOWN_SECONDS="$(normalize_uint "$DOCKER_START_COOLDOWN_SECONDS" 60)"
DOCKER_START_MAX_COOLDOWN_SECONDS="$(normalize_uint "$DOCKER_START_MAX_COOLDOWN_SECONDS" 900)"
MISSING_CONTAINER_RETRY_COOLDOWN_SECONDS="$(normalize_uint "$MISSING_CONTAINER_RETRY_COOLDOWN_SECONDS" 300)"
MISSING_CONTAINER_MAX_RETRY_COOLDOWN_SECONDS="$(normalize_uint "$MISSING_CONTAINER_MAX_RETRY_COOLDOWN_SECONDS" 3600)"

if [ -z "$DOCKER_BIN" ]; then
  DOCKER_BIN="$(command -v docker 2>/dev/null || true)"
fi

if [ -z "$CURL_BIN" ]; then
  CURL_BIN="$(command -v curl 2>/dev/null || true)"
fi
if [ -z "$LAUNCHCTL_BIN" ]; then
  LAUNCHCTL_BIN="$(command -v launchctl 2>/dev/null || true)"
fi
if [ -z "$DOCKER_BIN" ]; then
  for candidate in /usr/local/bin/docker /opt/homebrew/bin/docker; do
    if [ -x "$candidate" ]; then
      DOCKER_BIN="$candidate"
      break
    fi
  done
fi

log() {
  printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S %Z')" "$*"
}

run_docker_with_timeout() {
  local timeout_seconds="$1"
  local operation
  local child_pid
  local started_seconds
  local status=0
  shift
  operation="${1:-unknown}"

  if [ "$timeout_seconds" = "0" ]; then
    "$DOCKER_BIN" "$@"
    return
  fi

  "$DOCKER_BIN" "$@" <&0 &
  child_pid=$!
  started_seconds=$SECONDS
  while kill -0 "$child_pid" 2>/dev/null; do
    if [ "$((SECONDS - started_seconds))" -ge "$timeout_seconds" ]; then
      kill -TERM "$child_pid" 2>/dev/null || true
      sleep 1
      kill -KILL "$child_pid" 2>/dev/null || true
      wait "$child_pid" 2>/dev/null || true
      log "docker command timed out timeout=${timeout_seconds}s operation=$operation" >&2
      return 142
    fi
    sleep 0.1
  done
  wait "$child_pid" 2>/dev/null || status=$?
  return "$status"
}

run_docker() {
  run_docker_with_timeout "$DOCKER_COMMAND_TIMEOUT_SECONDS" "$@"
}

run_launchctl_with_timeout() {
  local timeout_seconds="$1"
  local operation
  local child_pid
  local started_seconds
  local status=0
  shift
  operation="${1:-unknown}"

  if [ -z "$LAUNCHCTL_BIN" ]; then
    log "launchctl is unavailable operation=$operation" >&2
    return 127
  fi
  if [ "$timeout_seconds" = "0" ]; then
    "$LAUNCHCTL_BIN" "$@"
    return
  fi

  "$LAUNCHCTL_BIN" "$@" <&0 &
  child_pid=$!
  started_seconds=$SECONDS
  while kill -0 "$child_pid" 2>/dev/null; do
    if [ "$((SECONDS - started_seconds))" -ge "$timeout_seconds" ]; then
      kill -TERM "$child_pid" 2>/dev/null || true
      sleep 1
      kill -KILL "$child_pid" 2>/dev/null || true
      wait "$child_pid" 2>/dev/null || true
      log "launchctl command timed out timeout=${timeout_seconds}s operation=$operation" >&2
      return 142
    fi
    sleep 0.1
  done
  wait "$child_pid" 2>/dev/null || status=$?
  return "$status"
}

docker_socket_path() {
  local docker_host="${DOCKER_HOST:-}"
  if [ -n "$DOCKER_SOCKET" ]; then
    printf '%s' "$DOCKER_SOCKET"
    return 0
  fi
  case "$docker_host" in
    unix://*)
      printf '%s' "${docker_host#unix://}"
      return 0
      ;;
  esac
  if [ -S /var/run/docker.sock ]; then
    printf '%s' /var/run/docker.sock
    return 0
  fi
  if [ -S "${HOME}/.docker/run/docker.sock" ]; then
    printf '%s' "${HOME}/.docker/run/docker.sock"
    return 0
  fi
  printf '%s' /var/run/docker.sock
}

docker_daemon_ready() {
  local socket_path
  local response
  if [ -z "$CURL_BIN" ] || [ ! -x "$CURL_BIN" ]; then
    log "curl is unavailable; cannot check Docker daemon _ping" >&2
    return 1
  fi
  socket_path="$(docker_socket_path)"
  response="$("$CURL_BIN" --unix-socket "$socket_path" \
    --connect-timeout "$DOCKER_PING_CONNECT_TIMEOUT" \
    --max-time "$DOCKER_PING_MAX_TIME" \
    http://localhost/_ping 2>/dev/null || true)"
  response="${response//$'\r'/}"
  response="${response//$'\n'/}"
  if [ "$response" = "OK" ]; then
    return 0
  fi
  log "Docker daemon _ping failed socket=$socket_path response=${response:-empty}" >&2
  return 1
}

docker_desktop_command_timeout() {
  local minimum_timeout
  minimum_timeout=$((DOCKER_DESKTOP_STOP_TIMEOUT_SECONDS + DOCKER_DESKTOP_COMMAND_GRACE_SECONDS))
  if [ "$DOCKER_DESKTOP_COMMAND_TIMEOUT_SECONDS" -lt "$minimum_timeout" ]; then
    log "Docker Desktop command timeout is too short; raising it configured=${DOCKER_DESKTOP_COMMAND_TIMEOUT_SECONDS}s minimum=${minimum_timeout}s" >&2
    printf '%s' "$minimum_timeout"
    return 0
  fi
  printf '%s' "$DOCKER_DESKTOP_COMMAND_TIMEOUT_SECONDS"
}

wait_for_docker_desktop_recovery() {
  local attempt
  for ((attempt = 1; attempt <= DOCKER_DESKTOP_RECOVERY_ATTEMPTS; attempt++)); do
    if docker_daemon_ready; then
      log "Docker Desktop daemon recovery probe passed attempt=$attempt"
      return 0
    fi
    log "Docker Desktop daemon recovery probe failed attempt=$attempt/$DOCKER_DESKTOP_RECOVERY_ATTEMPTS" >&2
    if [ "$attempt" -lt "$DOCKER_DESKTOP_RECOVERY_ATTEMPTS" ] && [ "$DOCKER_DESKTOP_RECOVERY_DELAY_SECONDS" -gt 0 ]; then
      sleep "$DOCKER_DESKTOP_RECOVERY_DELAY_SECONDS"
    fi
  done
  return 1
}

rotate_log_if_needed() {
  local label="$1"
  local log_file="$2"
  local max_bytes="$3"
  local keep="$4"
  local size
  local source
  local destination
  local index

  if [ "$max_bytes" = "0" ] || [ ! -f "$log_file" ]; then
    return 0
  fi
  size="$(stat -f '%z' "$log_file" 2>/dev/null || printf '0')"
  case "$size" in
    ''|*[!0-9]*) return 0 ;;
  esac
  if [ "$size" -lt "$max_bytes" ]; then
    return 0
  fi

  if [ "$DRY_RUN" = "1" ]; then
    log "dry-run: rotate ${label} log path=$log_file size=${size}B max=${max_bytes}B keep=$keep"
    return 0
  fi

  chmod 0600 "$log_file" 2>/dev/null || true
  for ((index = keep - 1; index >= 1; index--)); do
    source="${log_file}.${index}"
    destination="${log_file}.$((index + 1))"
    if [ -f "$source" ]; then
      if ! mv -f "$source" "$destination"; then
        log "${label} log rotation failed while moving source=$source destination=$destination" >&2
        return 1
      fi
      chmod 0600 "$destination" 2>/dev/null || true
    fi
  done
  if ! cp "$log_file" "${log_file}.1"; then
    log "${label} log rotation failed while copying path=$log_file" >&2
    return 1
  fi
  chmod 0600 "${log_file}.1"
  if ! : >"$log_file"; then
    log "${label} log rotation failed while truncating path=$log_file" >&2
    return 1
  fi
  chmod 0600 "$log_file"
  log "rotated ${label} log path=$log_file size=${size}B max=${max_bytes}B keep=$keep"
}

rotate_caddy_log_if_needed() {
  rotate_log_if_needed "Caddy" "$CADDY_LOG_FILE" "$CADDY_LOG_ROTATION_MAX_BYTES" "$CADDY_LOG_ROTATION_KEEP"
}

rotate_recovery_log_if_needed() {
  rotate_log_if_needed "recovery" "$RECOVERY_LOG_FILE" "$RECOVERY_LOG_ROTATION_MAX_BYTES" "$RECOVERY_LOG_ROTATION_KEEP"
}

write_health_snapshot() {
  local snapshot_dir
  local temporary_file
  if [ "$DRY_RUN" = "1" ] && [ "$WRITE_DRY_RUN_SNAPSHOT" != "1" ]; then
    return 0
  fi
  snapshot_dir="$(dirname "$HEALTH_SNAPSHOT_FILE")"
  temporary_file="${HEALTH_SNAPSHOT_FILE}.tmp.$$"
  if ! mkdir -p "$snapshot_dir" || ! chmod 0700 "$snapshot_dir"; then
    log "could not prepare health snapshot directory path=$snapshot_dir" >&2
    return 1
  fi
  if ! {
    printf 'schema_version=1\n'
    printf 'checked_at_utc=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
    printf 'state=%s\n' "$SNAPSHOT_STATE"
    printf 'docker_daemon=%s\n' "$SNAPSHOT_DOCKER_DAEMON"
    printf 'postgres=%s\n' "$SNAPSHOT_POSTGRES"
    printf 'redis=%s\n' "$SNAPSHOT_REDIS"
    printf 'postgres_docker_health=%s\n' "$SNAPSHOT_POSTGRES_DOCKER_HEALTH"
    printf 'redis_docker_health=%s\n' "$SNAPSHOT_REDIS_DOCKER_HEALTH"
    printf 'backend=%s\n' "$SNAPSHOT_BACKEND"
    printf 'public=%s\n' "$SNAPSHOT_PUBLIC"
    printf 'lan=%s\n' "$SNAPSHOT_LAN"
    printf 'app_state=%s\n' "$SNAPSHOT_APP_STATE"
    printf 'app_health=%s\n' "$SNAPSHOT_APP_HEALTH"
    printf 'app_restart_count=%s\n' "$SNAPSHOT_APP_RESTART_COUNT"
    printf 'app_oom=%s\n' "$SNAPSHOT_APP_OOM"
    printf 'failure_reason=%s\n' "$SNAPSHOT_FAILURE_REASON"
    printf 'action=%s\n' "$SNAPSHOT_ACTION"
    printf 'app_failures=%s\n' "$(read_counter "$APP_FAILURE_COUNT_FILE")"
    printf 'app_restart_failures=%s\n' "$(read_counter "$APP_RESTART_FAILURE_COUNT_FILE")"
    printf 'app_restart_at=%s\n' "$(read_counter "$APP_RESTART_STAMP_FILE")"
    printf 'docker_failures=%s\n' "$(read_counter "$DOCKER_FAILURE_COUNT_FILE")"
    printf 'docker_start_attempts=%s\n' "$(read_counter "$DOCKER_START_FAILURE_COUNT_FILE")"
    printf 'docker_start_at=%s\n' "$(read_counter "$DOCKER_START_STAMP_FILE")"
    printf 'postgres_missing_failures=%s\n' "$(read_counter "$(container_missing_failure_count_file "$POSTGRES_CONTAINER")")"
    printf 'postgres_missing_at=%s\n' "$(read_counter "$(container_missing_stamp_file "$POSTGRES_CONTAINER")")"
    printf 'redis_missing_failures=%s\n' "$(read_counter "$(container_missing_failure_count_file "$REDIS_CONTAINER")")"
    printf 'redis_missing_at=%s\n' "$(read_counter "$(container_missing_stamp_file "$REDIS_CONTAINER")")"
    printf 'app_missing_failures=%s\n' "$(read_counter "$(container_missing_failure_count_file "$APP_CONTAINER")")"
    printf 'app_missing_at=%s\n' "$(read_counter "$(container_missing_stamp_file "$APP_CONTAINER")")"
    printf 'docker_restart_failures=%s\n' "$(read_counter "$DOCKER_RESTART_FAILURE_COUNT_FILE")"
    printf 'docker_restart_at=%s\n' "$(read_counter "$DOCKER_RESTART_STAMP_FILE")"
    printf 'caddy_failures=%s\n' "$(read_counter "$CADDY_FAILURE_COUNT_FILE")"
    printf 'caddy_restart_failures=%s\n' "$(read_counter "$CADDY_RESTART_FAILURE_COUNT_FILE")"
    printf 'caddy_restart_at=%s\n' "$(read_counter "$CADDY_RESTART_STAMP_FILE")"
    printf 'lan_failures=%s\n' "$(read_counter "$LAN_FAILURE_COUNT_FILE")"
  } >"$temporary_file"; then
    rm -f "$temporary_file"
    log "could not write health snapshot path=$HEALTH_SNAPSHOT_FILE" >&2
    return 1
  fi
  chmod 0600 "$temporary_file"
  if ! mv -f "$temporary_file" "$HEALTH_SNAPSHOT_FILE"; then
    rm -f "$temporary_file"
    log "could not install health snapshot path=$HEALTH_SNAPSHOT_FILE" >&2
    return 1
  fi
}

cleanup_lock() {
  if [ "$LOCK_HELD" != "1" ]; then
    return 0
  fi
  rm -f "$LOCK_DIR/pid"
  rmdir "$LOCK_DIR" 2>/dev/null || true
  LOCK_HELD=0
}

cleanup_exit() {
  write_health_snapshot || true
  cleanup_lock
}

secure_state_files() {
  local path
  for path in \
    "$STATE_FILE" \
    "$APP_FAILURE_COUNT_FILE" \
    "$DOCKER_FAILURE_COUNT_FILE" \
    "$APP_RESTART_STAMP_FILE" \
    "$DOCKER_RESTART_STAMP_FILE" \
    "$DOCKER_START_FAILURE_COUNT_FILE" \
    "$DOCKER_START_STAMP_FILE" \
    "$(container_missing_failure_count_file "$POSTGRES_CONTAINER")" \
    "$(container_missing_stamp_file "$POSTGRES_CONTAINER")" \
    "$(container_missing_failure_count_file "$REDIS_CONTAINER")" \
    "$(container_missing_stamp_file "$REDIS_CONTAINER")" \
    "$(container_missing_failure_count_file "$APP_CONTAINER")" \
    "$(container_missing_stamp_file "$APP_CONTAINER")" \
    "$APP_RESTART_FAILURE_COUNT_FILE" \
    "$DOCKER_RESTART_FAILURE_COUNT_FILE" \
    "$CADDY_FAILURE_COUNT_FILE" \
    "$CADDY_RESTART_FAILURE_COUNT_FILE" \
    "$CADDY_RESTART_STAMP_FILE" \
    "$LAN_FAILURE_COUNT_FILE" \
    "$HEALTH_SNAPSHOT_FILE"; do
    if [ -e "$path" ]; then
      chmod 0600 "$path" 2>/dev/null || true
    fi
  done
}

acquire_lock() {
  local owner=""
  mkdir -p "$STATE_DIR"
  chmod 0700 "$STATE_DIR"
  secure_state_files
  if mkdir "$LOCK_DIR" 2>/dev/null; then
    LOCK_HELD=1
    printf '%s\n' "$$" >"$LOCK_DIR/pid"
    trap cleanup_exit EXIT
    return 0
  fi

  if [ -f "$LOCK_DIR/pid" ]; then
    owner="$(<"$LOCK_DIR/pid")"
  fi
  if [ -n "$owner" ] && kill -0 "$owner" 2>/dev/null; then
    return 1
  fi

  rm -f "$LOCK_DIR/pid"
  rmdir "$LOCK_DIR" 2>/dev/null || return 1
  if ! mkdir "$LOCK_DIR" 2>/dev/null; then
    return 1
  fi
  LOCK_HELD=1
  printf '%s\n' "$$" >"$LOCK_DIR/pid"
  trap cleanup_exit EXIT
}

read_counter() {
  local path="$1"
  local value=0
  if [ -f "$path" ]; then
    value="$(<"$path")"
  fi
  case "$value" in
    ''|*[!0-9]*) value=0 ;;
  esac
  printf '%s' "$value"
}

write_state_value() {
  local path="$1"
  local value="$2"
  local directory
  local temporary_file
  directory="$(dirname "$path")"
  temporary_file="${path}.tmp.$$"
  if ! mkdir -p "$directory" || ! chmod 0700 "$directory"; then
    log "could not prepare recovery state directory path=$directory" >&2
    return 1
  fi
  if ! (umask 077; printf '%s\n' "$value" >"$temporary_file"); then
    rm -f "$temporary_file"
    log "could not write recovery state value path=$path" >&2
    return 1
  fi
  if ! chmod 0600 "$temporary_file" || ! mv -f "$temporary_file" "$path"; then
    rm -f "$temporary_file"
    log "could not install recovery state value path=$path" >&2
    return 1
  fi
}

read_secret_file() {
  local path="$1"
  local value=""
  if [ ! -r "$path" ]; then
    return 1
  fi
  IFS= read -r value <"$path" || true
  if [ -n "$value" ]; then
    printf '%s' "$value"
    return 0
  fi
  return 1
}

read_env_value() {
  local key="$1"
  local value=""
  if [ -z "$RECOVERY_ENV_FILE" ] || [ ! -r "$RECOVERY_ENV_FILE" ]; then
    return 1
  fi
  value="$(awk -v key="$key" '
    {
      line = $0
      sub(/^[[:space:]]*/, "", line)
      if (index(line, key "=") != 1) {
        next
      }
      value = substr(line, length(key) + 2)
      sub(/[[:space:]]*$/, "", value)
      if (value ~ /^".*"$/) {
        sub(/^"/, "", value)
        sub(/"$/, "", value)
      }
      print value
      exit
    }
  ' "$RECOVERY_ENV_FILE")"
  if [ -n "$value" ]; then
    printf '%s' "$value"
    return 0
  fi
  return 1
}

resolve_container_names_from_env() {
  local value
  if [ -z "${NEW_API_RECOVERY_APP_CONTAINER:-}" ] && [ "$APP_CONTAINER" = "new-api" ]; then
    value="$(read_env_value NEW_API_CONTAINER_NAME || true)"
    if [ -n "$value" ]; then
      APP_CONTAINER="$value"
    fi
  fi
  if [ -z "${NEW_API_RECOVERY_POSTGRES_CONTAINER:-}" ] && [ "$POSTGRES_CONTAINER" = "postgres" ]; then
    value="$(read_env_value POSTGRES_CONTAINER_NAME || true)"
    if [ -n "$value" ]; then
      POSTGRES_CONTAINER="$value"
    fi
  fi
  if [ -z "${NEW_API_RECOVERY_REDIS_CONTAINER:-}" ] && [ "$REDIS_CONTAINER" = "redis" ]; then
    value="$(read_env_value REDIS_CONTAINER_NAME || true)"
    if [ -n "$value" ]; then
      REDIS_CONTAINER="$value"
    fi
  fi
  return 0
}

resolve_caddy_config_from_env() {
  local value env_label env_plist
  if [ "$CADDY_LABEL_EXPLICIT" = "1" ] || [ "$CADDY_PLIST_EXPLICIT" = "1" ]; then
    return 0
  fi
  env_label="$(read_env_value CADDY_LAUNCHD_LABEL || true)"
  env_plist="$(read_env_value CADDY_LAUNCHD_PLIST || true)"
  if [[ "$env_plist" == [~]/* ]]; then
    env_plist="$HOME/${env_plist:2}"
  fi
  if [ -n "$env_label" ]; then
    CADDY_LABEL="$env_label"
  elif [ -n "$env_plist" ]; then
    CADDY_LABEL="$(basename "$env_plist" .plist)"
  fi
  if [ -n "$env_plist" ]; then
    CADDY_PLIST="$env_plist"
  elif [ -n "$env_label" ]; then
    CADDY_PLIST="${HOME}/Library/LaunchAgents/${CADDY_LABEL}.plist"
  fi
}

url_percent_decode() {
  local encoded="$1"
  local decoded=""
  local hex
  local character
  while [ -n "$encoded" ]; do
    case "$encoded" in
      %??*)
        hex="${encoded:1:2}"
        case "$hex" in
          [0-9A-Fa-f][0-9A-Fa-f])
            printf -v character '%b' "\\x$hex"
            decoded+="$character"
            encoded="${encoded:3}"
            ;;
          *) return 1 ;;
        esac
        ;;
      %*) return 1 ;;
      *)
        decoded+="${encoded:0:1}"
        encoded="${encoded:1}"
        ;;
    esac
  done
  printf '%s' "$decoded"
}

resolve_redis_password() {
  local value=""
  if [ -n "$REDIS_PASSWORD" ]; then
    printf '%s' "$REDIS_PASSWORD"
    return 0
  fi
  if [ -n "$REDIS_PASSWORD_FILE" ]; then
    read_secret_file "$REDIS_PASSWORD_FILE" && return 0
  fi
  value="$(read_env_value REDIS_PASSWORD || true)"
  if [ -n "$value" ]; then
    printf '%s' "$value"
    return 0
  fi
  value="$(read_env_value REDIS_CONN_STRING || true)"
  case "$value" in
    redis://:*@*)
      value="${value#redis://:}"
      value="$(url_percent_decode "${value%%@*}")" || return 1
      printf '%s' "$value"
      return 0
      ;;
  esac
  return 1
}

resolve_postgres_password() {
  local value=""
  local authority=""
  local credentials=""
  if [ -n "$POSTGRES_PASSWORD" ]; then
    printf '%s' "$POSTGRES_PASSWORD"
    return 0
  fi
  if [ -n "$POSTGRES_PASSWORD_FILE" ]; then
    read_secret_file "$POSTGRES_PASSWORD_FILE" && return 0
  fi
  value="$(read_env_value POSTGRES_PASSWORD || true)"
  if [ -n "$value" ]; then
    printf '%s' "$value"
    return 0
  fi
  value="$(read_env_value SQL_DSN || true)"
  case "$value" in
    postgresql://*|postgres://*)
      authority="${value#*://}"
      authority="${authority%%/*}"
      case "$authority" in
        *@*)
          credentials="${authority%@*}"
          case "$credentials" in
            *:*)
              value="$(url_percent_decode "${credentials#*:}")" || return 1
              printf '%s' "$value"
              return 0
              ;;
          esac
          ;;
      esac
      ;;
  esac
  return 1
}

increment_counter() {
  local path="$1"
  local value
  value="$(read_counter "$path")"
  value=$((value + 1))
  if [ "$DRY_RUN" != "1" ]; then
    write_state_value "$path" "$value" || return 1
  fi
  printf '%s' "$value"
}

clear_counter() {
  if [ "$DRY_RUN" != "1" ]; then
    rm -f "$1"
  fi
}

cooldown_elapsed_with_base() {
  local path="$1"
  local failures="${2:-0}"
  local base_seconds="$3"
  local maximum_seconds="$4"
  local now last
  local multiplier=1
  local cooldown
  if [ ! -f "$path" ]; then
    return 0
  fi
  now="$(date '+%s')"
  last="$(<"$path")"
  case "$last" in
    ''|*[!0-9]*) return 0 ;;
  esac
  case "$failures" in
    ''|*[!0-9]*) failures=0 ;;
  esac
  for ((attempt = 0; attempt < failures; attempt++)); do
    if [ "$multiplier" -ge "$maximum_seconds" ]; then
      break
    fi
    multiplier=$((multiplier * 2))
  done
  cooldown=$((base_seconds * multiplier))
  if [ "$cooldown" -gt "$maximum_seconds" ]; then
    cooldown="$maximum_seconds"
  fi
  if [ "$now" -lt "$last" ]; then
    return 1
  fi
  (( now - last >= cooldown ))
}

cooldown_elapsed() {
  cooldown_elapsed_with_base "$1" "${2:-0}" "$RESTART_COOLDOWN_SECONDS" "$MAX_RESTART_COOLDOWN_SECONDS"
}

docker_start_cooldown_elapsed() {
  cooldown_elapsed_with_base "$DOCKER_START_STAMP_FILE" "${1:-0}" "$DOCKER_START_COOLDOWN_SECONDS" "$DOCKER_START_MAX_COOLDOWN_SECONDS"
}

container_state_token() {
  case "$1" in
    "$POSTGRES_CONTAINER") printf 'postgres' ;;
    "$REDIS_CONTAINER") printf 'redis' ;;
    "$APP_CONTAINER") printf 'app' ;;
    *) printf 'other' ;;
  esac
}

container_missing_failure_count_file() {
  printf '%s.missing-%s-failures' "$STATE_FILE" "$(container_state_token "$1")"
}

container_missing_stamp_file() {
  printf '%s.missing-%s-at' "$STATE_FILE" "$(container_state_token "$1")"
}

missing_container_cooldown_elapsed() {
  cooldown_elapsed_with_base "$1" "${2:-0}" "$MISSING_CONTAINER_RETRY_COOLDOWN_SECONDS" "$MISSING_CONTAINER_MAX_RETRY_COOLDOWN_SECONDS"
}

clear_missing_container_state() {
  clear_counter "$(container_missing_failure_count_file "$1")"
  clear_counter "$(container_missing_stamp_file "$1")"
}

record_restart_stamp() {
  if [ "$DRY_RUN" != "1" ]; then
    write_state_value "$1" "$(date '+%s')" || return 1
  fi
}

record_state() {
  local state="$1"
  local message="$2"
  local previous=""
  SNAPSHOT_STATE="$state"
  if [ -f "$STATE_FILE" ]; then
    previous="$(<"$STATE_FILE")"
  fi
  if [ "$previous" = "$state" ]; then
    return 0
  fi
  if [ "$DRY_RUN" = "1" ]; then
    log "dry-run: $message"
    return 0
  fi
  log "$message"
  if ! write_state_value "$STATE_FILE" "$state"; then
    log "could not persist recovery state path=$STATE_FILE"
  fi
}

launchd_domain() {
  printf 'gui/%s' "$(id -u)"
}

container_state() {
  local name="$1"
  local state
  if ! state="$(run_docker inspect -f '{{.State.Status}}' "$name" 2>&1)"; then
    state="${state//$'\r'/ }"
    state="${state//$'\n'/ }"
    case "$state" in
      *"No such container"*|*"No such object"*|*"no such container"*|*"not found"*)
        printf 'missing'
        ;;
      *)
        log "container inspect failed container=$name detail=${state:0:256}" >&2
        printf 'inspect-error'
        ;;
    esac
    return 0
  fi
  state="${state%%$'\n'*}"
  state="${state//$'\r'/}"
  if [ -n "$state" ]; then
    printf '%s' "$state"
  else
    log "container inspect returned empty state container=$name" >&2
    printf 'inspect-error'
  fi
}

container_health() {
  local name="$1"
  local health
  if ! health="$(run_docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$name" 2>&1)"; then
    health="${health//$'\r'/ }"
    health="${health//$'\n'/ }"
    case "$health" in
      *"No such container"*|*"No such object"*|*"no such container"*|*"not found"*)
        printf 'missing'
        ;;
      *)
        log "container health inspect failed container=$name detail=${health:0:256}" >&2
        printf 'inspect-error'
        ;;
    esac
    return 0
  fi
  health="${health%%$'\n'*}"
  health="${health//$'\r'/}"
  if [ -n "$health" ]; then
    printf '%s' "$health"
  else
    log "container health inspect returned empty result container=$name" >&2
    printf 'inspect-error'
  fi
}

refresh_app_snapshot() {
  local runtime
  local field
  local key
  local value
  runtime="$(run_docker_with_timeout "$APP_RESTART_STATUS_COMMAND_TIMEOUT_SECONDS" inspect -f 'state={{.State.Status}} health={{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}} restart={{.RestartCount}} oom={{.State.OOMKilled}}' "$APP_CONTAINER" 2>/dev/null || true)"
  for field in $runtime; do
    key="${field%%=*}"
    value="${field#*=}"
    case "$key" in
      state) SNAPSHOT_APP_STATE="$value" ;;
      health) SNAPSHOT_APP_HEALTH="$value" ;;
      restart) SNAPSHOT_APP_RESTART_COUNT="$value" ;;
      oom) SNAPSHOT_APP_OOM="$value" ;;
    esac
  done
}

clear_healthy_recovery_state() {
  local path
  local cleared=0
  for path in \
    "$APP_FAILURE_COUNT_FILE" \
    "$APP_RESTART_FAILURE_COUNT_FILE" \
    "$APP_RESTART_STAMP_FILE" \
    "$DOCKER_FAILURE_COUNT_FILE" \
    "$DOCKER_START_FAILURE_COUNT_FILE" \
    "$DOCKER_START_STAMP_FILE" \
    "$DOCKER_RESTART_FAILURE_COUNT_FILE" \
    "$DOCKER_RESTART_STAMP_FILE" \
    "$CADDY_FAILURE_COUNT_FILE" \
    "$CADDY_RESTART_FAILURE_COUNT_FILE" \
    "$CADDY_RESTART_STAMP_FILE" \
    "$LAN_FAILURE_COUNT_FILE"; do
    if [ -e "$path" ]; then
      clear_counter "$path"
      cleared=$((cleared + 1))
    fi
  done
  clear_missing_container_state "$POSTGRES_CONTAINER"
  clear_missing_container_state "$REDIS_CONTAINER"
  clear_missing_container_state "$APP_CONTAINER"
  if [ "$cleared" -gt 0 ]; then
    log "healthy recovery cleared stale failure state files=$cleared"
  fi
}

record_healthy_state() {
  local message="$1"
  if [ "$SNAPSHOT_APP_HEALTH" != "healthy" ]; then
    SNAPSHOT_FAILURE_REASON="app-health-drift"
    record_state "app-health-drift" "new-api Docker health is not healthy health=$SNAPSHOT_APP_HEALTH; backend, public and LAN probes passed"
    return 1
  fi
  clear_healthy_recovery_state
  SNAPSHOT_FAILURE_REASON="none"
  record_state "healthy" "$message"
}

container_lifecycle_snapshot() {
  local name="$1"
  local snapshot
  if snapshot="$(run_docker_with_timeout "$APP_RESTART_STATUS_COMMAND_TIMEOUT_SECONDS" inspect -f 'state={{.State.Status}} running={{.State.Running}} restarting={{.State.Restarting}} oom_killed={{.State.OOMKilled}} exit_code={{.State.ExitCode}} error={{printf "%q" .State.Error}} started_at={{.State.StartedAt}} finished_at={{.State.FinishedAt}} restart_count={{.RestartCount}} restart_policy={{.HostConfig.RestartPolicy.Name}} restart_max={{.HostConfig.RestartPolicy.MaximumRetryCount}} health={{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$name" 2>&1)"; then
    snapshot="${snapshot//$'\r'/ }"
    snapshot="${snapshot//$'\n'/ }"
    log "container lifecycle container=$name ${snapshot:0:512}"
    return 0
  fi
  snapshot="${snapshot//$'\r'/ }"
  snapshot="${snapshot//$'\n'/ }"
  log "container lifecycle snapshot failed container=$name detail=${snapshot:0:512}"
  return 1
}

start_container_if_needed() {
  local name="$1"
  local state
  local missing_failure_count_file
  local missing_stamp_file
  local missing_failures
  state="$(container_state "$name")"
  if [ "$MONITOR_ONLY" = "1" ] && [ "$state" != "running" ]; then
    log "monitor-only: container=$name is not running state=$state"
    return 1
  fi
  case "$state" in
    running)
      clear_missing_container_state "$name"
      return 0
      ;;
    paused)
      clear_missing_container_state "$name"
      if [ "$DRY_RUN" = "1" ]; then
        log "dry-run: docker unpause $name"
      else
        log "unpausing container=$name"
        container_lifecycle_snapshot "$name" || true
        if run_docker unpause "$name" >/dev/null; then
          container_lifecycle_snapshot "$name" || true
        else
          container_lifecycle_snapshot "$name" || true
          return 1
        fi
      fi
      ;;
    created|exited|dead)
      clear_missing_container_state "$name"
      if [ "$DRY_RUN" = "1" ]; then
        log "dry-run: docker start $name"
      else
        log "starting container=$name state=$state"
        container_lifecycle_snapshot "$name" || true
        if run_docker start "$name" >/dev/null; then
          container_lifecycle_snapshot "$name" || true
        else
          container_lifecycle_snapshot "$name" || true
          return 1
        fi
      fi
      ;;
    restarting)
      clear_missing_container_state "$name"
      log "container=$name is already restarting"
      return 1
      ;;
    missing)
      missing_failure_count_file="$(container_missing_failure_count_file "$name")"
      missing_stamp_file="$(container_missing_stamp_file "$name")"
      missing_failures="$(read_counter "$missing_failure_count_file")"
      SNAPSHOT_ACTION="container-missing"
      if ! missing_container_cooldown_elapsed "$missing_stamp_file" "$missing_failures"; then
        log "container=$name is missing; refusing to create or recreate it; retry cooldown active failures=$missing_failures"
        return 1
      fi
      record_restart_stamp "$missing_stamp_file"
      missing_failures="$(increment_counter "$missing_failure_count_file")"
      log "container=$name is missing; refusing to create or recreate it; failures=$missing_failures"
      return 1
      ;;
    inspect-error)
      clear_missing_container_state "$name"
      SNAPSHOT_ACTION="container-inspect-error"
      log "container=$name inspect failed; refusing lifecycle action and waiting for the next interval"
      return 1
      ;;
    *)
      clear_missing_container_state "$name"
      log "container=$name has unsupported state=$state"
      return 1
      ;;
  esac
}

wait_for_postgres() {
  local attempt
  local password=""
  local response=""
  password="$(resolve_postgres_password || true)"
  if [ -z "$password" ]; then
    log "PostgreSQL password is unavailable; refusing unauthenticated SELECT 1" >&2
    return 1
  fi
  for ((attempt = 1; attempt <= ATTEMPTS; attempt++)); do
    # shellcheck disable=SC2016
    response="$(
      printf '%s\n' "$password" |
        run_docker exec -i "$POSTGRES_CONTAINER" sh -c \
          'IFS= read -r PGPASSWORD || exit 1; export PGPASSWORD; exec psql -h 127.0.0.1 -X -q -tA -v ON_ERROR_STOP=1 -U "$1" -d "$2" -c "SELECT 1"' \
          sh "$POSTGRES_USER" "$POSTGRES_DB" \
          2>/dev/null || true
    )"
    response="${response//[[:space:]]/}"
    if [ "$response" = "1" ]; then
      return 0
    fi
    sleep "$DELAY_SECONDS"
  done
  return 1
}

wait_for_redis() {
  local attempt response password=""
  password="$(resolve_redis_password || true)"
  if [ -z "$password" ]; then
    log "Redis password is unavailable; refusing unauthenticated probe" >&2
    return 1
  fi
  for ((attempt = 1; attempt <= ATTEMPTS; attempt++)); do
    if [ -n "$password" ]; then
      response="$(
        printf '%s\n' "$password" |
          run_docker exec -i "$REDIS_CONTAINER" sh -c \
            'IFS= read -r REDISCLI_AUTH || exit 1; export REDISCLI_AUTH; exec redis-cli --no-auth-warning ping' \
            2>/dev/null || true
      )"
    fi
    if [ "$response" = "PONG" ]; then
      return 0
    fi
    sleep "$DELAY_SECONDS"
  done
  return 1
}

wait_for_url() {
  local url="$1"
  local attempt
  local response
  if [ -z "$CURL_BIN" ] || [ ! -x "$CURL_BIN" ]; then
    log "curl is unavailable; cannot check url=$url"
    return 1
  fi
  for ((attempt = 1; attempt <= ATTEMPTS; attempt++)); do
    response="$("$CURL_BIN" -fsS \
      --connect-timeout "$CURL_CONNECT_TIMEOUT" \
      --max-time "$CURL_MAX_TIME" \
      -o /dev/null \
      -w '%{http_code}' \
      "$url" 2>/dev/null || true)"
    response="$(printf '%s' "$response" | tr -d '\r\n')"
    if [ "$response" = "200" ]; then
      return 0
    fi
    sleep "$DELAY_SECONDS"
  done
  return 1
}

probe_backend() {
  if wait_for_url "$BACKEND_URL"; then
    SNAPSHOT_BACKEND="pass"
    return 0
  fi
  SNAPSHOT_BACKEND="fail"
  return 1
}

probe_public() {
  if wait_for_url "$PUBLIC_URL"; then
    SNAPSHOT_PUBLIC="pass"
    return 0
  fi
  SNAPSHOT_PUBLIC="fail"
  return 1
}

check_lan_entry() {
  local failure_count
  if [ -z "$LAN_URL" ]; then
    SNAPSHOT_LAN="disabled"
    clear_counter "$LAN_FAILURE_COUNT_FILE"
    return 0
  fi
  if wait_for_url "$LAN_URL"; then
    SNAPSHOT_LAN="pass"
    clear_counter "$LAN_FAILURE_COUNT_FILE"
    return 0
  fi
  SNAPSHOT_LAN="fail"
  SNAPSHOT_FAILURE_REASON="lan-failed"
  failure_count="$(increment_counter "$LAN_FAILURE_COUNT_FILE")"
  record_state "lan-failed" "LAN health check failed url=$LAN_URL; loopback public entry is healthy failures=$failure_count"
  return 1
}

caddy_job_pid() {
  local target="$1"
  local output
  output="$(run_launchctl_with_timeout "$CADDY_LAUNCHCTL_COMMAND_TIMEOUT_SECONDS" print "$target" 2>/dev/null)" || return 1
  printf '%s\n' "$output" | awk '$1 == "pid" && $2 == "=" { print $3; exit }'
}

wait_for_caddy_graceful_restart() {
  local target="$1"
  local previous_pid="$2"
  local elapsed=0
  local current_pid
  while [ "$elapsed" -le "$CADDY_GRACEFUL_TIMEOUT_SECONDS" ]; do
    if ! run_launchctl_with_timeout "$CADDY_LAUNCHCTL_COMMAND_TIMEOUT_SECONDS" print "$target" >/dev/null 2>&1; then
      return 1
    fi
    current_pid="$(caddy_job_pid "$target" || true)"
    if [ -n "$current_pid" ] && [ "$current_pid" != "$previous_pid" ]; then
      return 0
    fi
    if [ "$CADDY_GRACEFUL_POLL_INTERVAL_SECONDS" -gt 0 ]; then
      sleep "$CADDY_GRACEFUL_POLL_INTERVAL_SECONDS"
      elapsed=$((elapsed + CADDY_GRACEFUL_POLL_INTERVAL_SECONDS))
    else
      elapsed=$((elapsed + 1))
    fi
  done
  return 1
}

wait_for_app_restart_result() {
  local attempt
  local state
  local health
  for ((attempt = 1; attempt <= ATTEMPTS; attempt++)); do
    state="$(run_docker_with_timeout "$APP_RESTART_STATUS_COMMAND_TIMEOUT_SECONDS" inspect -f '{{.State.Status}}' "$APP_CONTAINER" 2>/dev/null || true)"
    health="$(run_docker_with_timeout "$APP_RESTART_STATUS_COMMAND_TIMEOUT_SECONDS" inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$APP_CONTAINER" 2>/dev/null || true)"
    state="${state:-missing}"
    health="${health:-missing}"
    log "app restart result probe attempt=$attempt state=$state health=$health"
    if [ "$state" = "running" ] && [ "$health" = "healthy" ]; then
      return 0
    fi
    if [ "$attempt" -lt "$ATTEMPTS" ]; then
      sleep "$DELAY_SECONDS"
    fi
  done
  return 1
}

verify_docker_desktop_recovery() {
  if ! wait_for_docker_desktop_recovery; then
    SNAPSHOT_DOCKER_DAEMON="fail"
    SNAPSHOT_FAILURE_REASON="docker-daemon-recovery-failed"
    return 1
  fi
  SNAPSHOT_DOCKER_DAEMON="pass"

  if ! start_container_if_needed "$POSTGRES_CONTAINER"; then
    SNAPSHOT_POSTGRES="fail"
    SNAPSHOT_POSTGRES_DOCKER_HEALTH="$(container_health "$POSTGRES_CONTAINER")"
    if [ "$SNAPSHOT_ACTION" = "container-inspect-error" ]; then
      SNAPSHOT_FAILURE_REASON="postgres-container-inspect-failed"
      record_state "postgres-container-inspect-failed" "Docker inspect failed for PostgreSQL container=$POSTGRES_CONTAINER; refusing lifecycle action"
    else
      SNAPSHOT_FAILURE_REASON="postgres-container-recovery-failed"
      record_state "postgres-container-recovery-failed" "failed to start PostgreSQL container=$POSTGRES_CONTAINER"
    fi
    return 1
  fi
  if ! start_container_if_needed "$REDIS_CONTAINER"; then
    SNAPSHOT_REDIS="fail"
    SNAPSHOT_REDIS_DOCKER_HEALTH="$(container_health "$REDIS_CONTAINER")"
    if [ "$SNAPSHOT_ACTION" = "container-inspect-error" ]; then
      SNAPSHOT_FAILURE_REASON="redis-container-inspect-failed"
      record_state "redis-container-inspect-failed" "Docker inspect failed for Redis container=$REDIS_CONTAINER; refusing lifecycle action"
    else
      SNAPSHOT_FAILURE_REASON="redis-container-recovery-failed"
      record_state "redis-container-recovery-failed" "failed to start Redis container=$REDIS_CONTAINER"
    fi
    return 1
  fi
  SNAPSHOT_POSTGRES_DOCKER_HEALTH="$(container_health "$POSTGRES_CONTAINER")"
  SNAPSHOT_REDIS_DOCKER_HEALTH="$(container_health "$REDIS_CONTAINER")"
  if ! wait_for_postgres; then
    SNAPSHOT_POSTGRES="fail"
    SNAPSHOT_FAILURE_REASON="postgres-not-ready-after-docker-restart"
    return 1
  fi
  SNAPSHOT_POSTGRES="pass"
  if ! wait_for_redis; then
    SNAPSHOT_REDIS="fail"
    SNAPSHOT_FAILURE_REASON="redis-not-ready-after-docker-restart"
    return 1
  fi
  SNAPSHOT_REDIS="pass"

  if ! start_container_if_needed "$APP_CONTAINER"; then
    SNAPSHOT_FAILURE_REASON="app-container-recovery-failed"
    return 1
  fi
  refresh_app_snapshot
  if [ "$SNAPSHOT_APP_HEALTH" != "healthy" ] && ! wait_for_app_restart_result; then
    SNAPSHOT_FAILURE_REASON="app-not-healthy-after-docker-restart"
    return 1
  fi
  refresh_app_snapshot
  if ! probe_backend || ! probe_public || ! check_lan_entry; then
    SNAPSHOT_FAILURE_REASON="entrypoint-failed-after-docker-restart"
    return 1
  fi
  return 0
}

restart_app_if_needed() {
  local failure_count="$1"
  local health
  local restart_failures
  local command_timeout
  local minimum_command_timeout
  health="$(container_health "$APP_CONTAINER")"
  if [ "$health" != "unhealthy" ] || [ "$failure_count" -lt "$APP_RESTART_FAILURE_THRESHOLD" ]; then
    return 1
  fi
  restart_failures="$(read_counter "$APP_RESTART_FAILURE_COUNT_FILE")"
  if [ "$restart_failures" -ge "$APP_RESTART_MAX_FAILURES" ]; then
    if ! cooldown_elapsed "$APP_RESTART_STAMP_FILE" "$restart_failures"; then
      log "app restart circuit open failures=$restart_failures"
      return 1
    fi
    clear_counter "$APP_RESTART_FAILURE_COUNT_FILE"
    restart_failures=0
  fi
  if ! cooldown_elapsed "$APP_RESTART_STAMP_FILE" "$restart_failures"; then
    log "app restart cooldown active failures=$restart_failures"
    return 1
  fi

  command_timeout="$APP_RESTART_COMMAND_TIMEOUT_SECONDS"
  minimum_command_timeout=$((APP_RESTART_TIMEOUT_SECONDS + APP_RESTART_COMMAND_GRACE_SECONDS))
  if [ "$command_timeout" -lt "$minimum_command_timeout" ]; then
    log "app restart command timeout is too short; raising it configured=${command_timeout}s minimum=${minimum_command_timeout}s"
    command_timeout="$minimum_command_timeout"
  fi

  if [ "$DRY_RUN" = "1" ]; then
    log "dry-run: docker restart --timeout $APP_RESTART_TIMEOUT_SECONDS $APP_CONTAINER command_timeout=${command_timeout}s health=$health failures=$failure_count restart_failures=$restart_failures"
    return 0
  fi
  record_restart_stamp "$APP_RESTART_STAMP_FILE"
  SNAPSHOT_ACTION="app-restart"
  log "restarting existing container=$APP_CONTAINER health=$health failures=$failure_count"
  container_lifecycle_snapshot "$APP_CONTAINER" || true
  if run_docker_with_timeout "$command_timeout" restart --timeout "$APP_RESTART_TIMEOUT_SECONDS" "$APP_CONTAINER" >/dev/null; then
    container_lifecycle_snapshot "$APP_CONTAINER" || true
    clear_counter "$APP_FAILURE_COUNT_FILE"
    clear_counter "$APP_RESTART_FAILURE_COUNT_FILE"
    record_state "app-restarted" "existing new-api container restarted after repeated health failures"
    return 0
  fi
  container_lifecycle_snapshot "$APP_CONTAINER" || true
  if wait_for_app_restart_result; then
    container_lifecycle_snapshot "$APP_CONTAINER" || true
    clear_counter "$APP_FAILURE_COUNT_FILE"
    clear_counter "$APP_RESTART_FAILURE_COUNT_FILE"
    record_state "app-recovered-after-restart-error" "new-api became healthy after Docker restart returned an error"
    return 0
  fi
  restart_failures="$(increment_counter "$APP_RESTART_FAILURE_COUNT_FILE")"
  record_state "app-restart-failed" "failed to restart existing new-api container restart_failures=$restart_failures"
  return 1
}

restart_caddy() {
  local public_failure_count="$1"
  local domain
  local target
  local restart_failures
  local original_pid
  domain="$(launchd_domain)"
  target="${domain}/${CADDY_LABEL}"
  restart_failures="$(read_counter "$CADDY_RESTART_FAILURE_COUNT_FILE")"
  if [ "$restart_failures" -ge "$CADDY_RESTART_MAX_FAILURES" ]; then
    if ! cooldown_elapsed "$CADDY_RESTART_STAMP_FILE" "$restart_failures"; then
      record_state "caddy-restart-circuit-open" "Caddy restart circuit open public_failures=$public_failure_count restart_failures=$restart_failures"
      return 1
    fi
    clear_counter "$CADDY_RESTART_FAILURE_COUNT_FILE"
    restart_failures=0
  fi
  if ! cooldown_elapsed "$CADDY_RESTART_STAMP_FILE" "$restart_failures"; then
    record_state "caddy-restart-cooldown" "Caddy restart cooldown active public_failures=$public_failure_count restart_failures=$restart_failures"
    return 1
  fi
  if [ "$DRY_RUN" = "1" ]; then
    log "dry-run: launchctl kill SIGTERM ${target} graceful_timeout=${CADDY_GRACEFUL_TIMEOUT_SECONDS}s fallback=launchctl kickstart -k ${target} public_failures=$public_failure_count restart_failures=$restart_failures"
    return 0
  fi
  record_restart_stamp "$CADDY_RESTART_STAMP_FILE"
  SNAPSHOT_ACTION="caddy-restart"
  if ! run_launchctl_with_timeout "$CADDY_LAUNCHCTL_COMMAND_TIMEOUT_SECONDS" print "$target" >/dev/null 2>&1; then
    if [ -f "$CADDY_PLIST" ]; then
      log "loading Caddy launchd job label=$CADDY_LABEL"
      if ! run_launchctl_with_timeout "$CADDY_LAUNCHCTL_COMMAND_TIMEOUT_SECONDS" bootstrap "$domain" "$CADDY_PLIST" >/dev/null 2>&1 && \
         ! run_launchctl_with_timeout "$CADDY_LAUNCHCTL_COMMAND_TIMEOUT_SECONDS" print "$target" >/dev/null 2>&1; then
        restart_failures="$(increment_counter "$CADDY_RESTART_FAILURE_COUNT_FILE")"
        record_state "caddy-restart-failed" "Caddy launchd bootstrap failed restart_failures=$restart_failures"
        return 1
      fi
    else
      restart_failures="$(increment_counter "$CADDY_RESTART_FAILURE_COUNT_FILE")"
      record_state "caddy-restart-failed" "Caddy launchd plist is missing path=$CADDY_PLIST restart_failures=$restart_failures"
      return 1
    fi
  fi

  original_pid="$(caddy_job_pid "$target" || true)"
  if [ -n "$original_pid" ]; then
    log "gracefully stopping Caddy launchd job label=$CADDY_LABEL pid=$original_pid timeout=${CADDY_GRACEFUL_TIMEOUT_SECONDS}s"
    if run_launchctl_with_timeout "$CADDY_LAUNCHCTL_COMMAND_TIMEOUT_SECONDS" kill SIGTERM "$target"; then
      if wait_for_caddy_graceful_restart "$target" "$original_pid"; then
        record_state "caddy-graceful-restarted" "Caddy restarted after graceful SIGTERM label=$CADDY_LABEL previous_pid=$original_pid"
        return 0
      fi
      log "Caddy graceful stop timed out label=$CADDY_LABEL pid=$original_pid; using kickstart fallback" >&2
    else
      log "Caddy graceful SIGTERM failed label=$CADDY_LABEL; using kickstart fallback" >&2
    fi
  else
    log "Caddy launchd job has no running pid label=$CADDY_LABEL; using kickstart fallback"
  fi

  log "restarting Caddy launchd job label=$CADDY_LABEL"
  if run_launchctl_with_timeout "$CADDY_LAUNCHCTL_COMMAND_TIMEOUT_SECONDS" kickstart -k "$target"; then
    record_state "caddy-kickstarted" "Caddy kickstart fallback completed label=$CADDY_LABEL"
    return 0
  fi
  restart_failures="$(increment_counter "$CADDY_RESTART_FAILURE_COUNT_FILE")"
  record_state "caddy-restart-failed" "Caddy launchd kickstart fallback failed restart_failures=$restart_failures"
  return 1
}

start_docker_desktop_if_needed() {
  local docker_desktop_process
  local start_attempts
  if [ ! -d "$DOCKER_APP_PATH" ]; then
    clear_counter "$DOCKER_START_FAILURE_COUNT_FILE"
    clear_counter "$DOCKER_START_STAMP_FILE"
    return 0
  fi
  docker_desktop_process="${DOCKER_APP_PATH}/Contents/MacOS/Docker Desktop.app/Contents/MacOS/Docker Desktop"
  if [ -x "$PGREP_BIN" ] && "$PGREP_BIN" -f "$docker_desktop_process" >/dev/null 2>&1; then
    clear_counter "$DOCKER_START_FAILURE_COUNT_FILE"
    clear_counter "$DOCKER_START_STAMP_FILE"
    return 0
  fi
  start_attempts="$(read_counter "$DOCKER_START_FAILURE_COUNT_FILE")"
  if ! docker_start_cooldown_elapsed "$start_attempts"; then
    SNAPSHOT_ACTION="docker-start"
    log "Docker Desktop startup cooldown active attempts=$start_attempts cooldown=${DOCKER_START_COOLDOWN_SECONDS}s"
    return 0
  fi
  SNAPSHOT_ACTION="docker-start"
  if [ "$DRY_RUN" = "1" ]; then
    log "dry-run: open -gj -a Docker startup_attempts=$start_attempts"
    return 0
  fi
  start_attempts="$(increment_counter "$DOCKER_START_FAILURE_COUNT_FILE")"
  record_restart_stamp "$DOCKER_START_STAMP_FILE"
  if [ ! -x "$OPEN_BIN" ]; then
    log "Docker Desktop open command is unavailable path=$OPEN_BIN attempts=$start_attempts" >&2
    return 0
  fi
  log "Docker Desktop process is absent; requesting startup attempts=$start_attempts"
  if ! "$OPEN_BIN" -gj -a Docker; then
    log "Docker Desktop startup request failed attempts=$start_attempts" >&2
  fi
}

restart_docker_desktop_if_needed() {
  local failure_count="$1"
  local restart_failures
  local command_timeout
  local stop_status
  local start_status
  restart_failures="$(read_counter "$DOCKER_RESTART_FAILURE_COUNT_FILE")"
  if [ "$FORCE_DOCKER_RESTART" != "1" ] || [ "$failure_count" -lt "$DOCKER_RESTART_FAILURE_THRESHOLD" ]; then
    return 1
  fi
  if [ "$restart_failures" -ge "$DOCKER_RESTART_FAILURE_THRESHOLD" ] && \
     ! cooldown_elapsed "$DOCKER_RESTART_STAMP_FILE" "$restart_failures"; then
    log "Docker Desktop restart circuit open failures=$restart_failures"
    return 1
  fi
  if [ "$restart_failures" -ge "$DOCKER_RESTART_FAILURE_THRESHOLD" ]; then
    clear_counter "$DOCKER_RESTART_FAILURE_COUNT_FILE"
    restart_failures=0
  fi
  if ! cooldown_elapsed "$DOCKER_RESTART_STAMP_FILE" "$restart_failures"; then
    log "Docker Desktop restart cooldown active failures=$restart_failures"
    return 1
  fi
  command_timeout="$(docker_desktop_command_timeout)"
  if [ "$DRY_RUN" = "1" ]; then
    log "dry-run: docker desktop stop --force --timeout $DOCKER_DESKTOP_STOP_TIMEOUT_SECONDS command_timeout=${command_timeout}s"
    log "dry-run: docker desktop start --detach"
    log "dry-run: wait for Docker daemon and verify PostgreSQL, Redis, new-api, backend, Caddy and LAN"
    record_state "docker-restart-planned" "Docker Desktop restart planned after repeated daemon failures"
    return 0
  fi
  record_restart_stamp "$DOCKER_RESTART_STAMP_FILE"
  SNAPSHOT_ACTION="docker-restart"
  log "Docker daemon failed repeatedly; forcing Docker Desktop restart"
  if run_docker_with_timeout "$command_timeout" desktop stop --force --timeout "$DOCKER_DESKTOP_STOP_TIMEOUT_SECONDS"; then
    :
  else
    stop_status=$?
    restart_failures="$(increment_counter "$DOCKER_RESTART_FAILURE_COUNT_FILE")"
    record_state "docker-restart-failed" "Docker Desktop stop failed status=$stop_status restart_failures=$restart_failures"
    return 1
  fi
  if run_docker_with_timeout "$command_timeout" desktop start --detach; then
    :
  else
    start_status=$?
    restart_failures="$(increment_counter "$DOCKER_RESTART_FAILURE_COUNT_FILE")"
    record_state "docker-restart-failed" "Docker Desktop start failed status=$start_status restart_failures=$restart_failures"
    return 1
  fi
  if verify_docker_desktop_recovery; then
    clear_counter "$DOCKER_FAILURE_COUNT_FILE"
    clear_counter "$DOCKER_RESTART_FAILURE_COUNT_FILE"
    SNAPSHOT_FAILURE_REASON="none"
    record_state "healthy" "Docker Desktop restart and full recovery probes passed"
    return 0
  fi
  restart_failures="$(increment_counter "$DOCKER_RESTART_FAILURE_COUNT_FILE")"
  record_state "docker-restart-failed" "Docker Desktop restart command completed but recovery probes failed restart_failures=$restart_failures; waiting for the next interval"
  return 1
}

main() {
  local docker_failure_count=0
  local app_failure_count=0
  local public_failure_count=0
  local restart_failures=0
  resolve_container_names_from_env
  resolve_caddy_config_from_env
  acquire_lock || return 0
  if ! rotate_recovery_log_if_needed; then
    log "recovery log rotation failed; continuing health checks"
  fi
  if ! rotate_caddy_log_if_needed; then
    log "Caddy log rotation failed; continuing health checks"
  fi

  if [ -z "$DOCKER_BIN" ] || [ ! -x "$DOCKER_BIN" ]; then
    start_docker_desktop_if_needed
    record_state "docker-cli-unavailable" "Docker CLI is unavailable; waiting for the next interval"
    if [ "$MONITOR_ONLY" = "1" ]; then
      return 1
    fi
    return 0
  fi

  if ! docker_daemon_ready; then
    SNAPSHOT_DOCKER_DAEMON="fail"
    SNAPSHOT_FAILURE_REASON="docker-daemon-unavailable"
    start_docker_desktop_if_needed
    docker_failure_count="$(increment_counter "$DOCKER_FAILURE_COUNT_FILE")"
    if restart_docker_desktop_if_needed "$docker_failure_count"; then
      return 0
    fi
    if [ "$SNAPSHOT_STATE" != "docker-restart-failed" ]; then
      record_state "docker-daemon-unavailable" "Docker Desktop daemon is unavailable failures=$docker_failure_count; waiting for the next interval"
    fi
    if [ "$MONITOR_ONLY" = "1" ]; then
      return 1
    fi
    return 0
  fi
  SNAPSHOT_DOCKER_DAEMON="pass"
  clear_counter "$DOCKER_FAILURE_COUNT_FILE"

  if ! start_container_if_needed "$POSTGRES_CONTAINER"; then
    SNAPSHOT_POSTGRES="fail"
    SNAPSHOT_POSTGRES_DOCKER_HEALTH="$(container_health "$POSTGRES_CONTAINER")"
    if [ "$SNAPSHOT_ACTION" = "container-inspect-error" ]; then
      SNAPSHOT_FAILURE_REASON="postgres-container-inspect-failed"
      record_state "postgres-container-inspect-failed" "Docker inspect failed for PostgreSQL container=$POSTGRES_CONTAINER; refusing lifecycle action"
    else
      SNAPSHOT_FAILURE_REASON="postgres-container-recovery-failed"
      record_state "postgres-container-recovery-failed" "failed to start PostgreSQL container=$POSTGRES_CONTAINER"
    fi
    return 1
  fi
  if ! start_container_if_needed "$REDIS_CONTAINER"; then
    SNAPSHOT_REDIS="fail"
    SNAPSHOT_REDIS_DOCKER_HEALTH="$(container_health "$REDIS_CONTAINER")"
    if [ "$SNAPSHOT_ACTION" = "container-inspect-error" ]; then
      SNAPSHOT_FAILURE_REASON="redis-container-inspect-failed"
      record_state "redis-container-inspect-failed" "Docker inspect failed for Redis container=$REDIS_CONTAINER; refusing lifecycle action"
    else
      SNAPSHOT_FAILURE_REASON="redis-container-recovery-failed"
      record_state "redis-container-recovery-failed" "failed to start Redis container=$REDIS_CONTAINER"
    fi
    return 1
  fi
  SNAPSHOT_POSTGRES_DOCKER_HEALTH="$(container_health "$POSTGRES_CONTAINER")"
  SNAPSHOT_REDIS_DOCKER_HEALTH="$(container_health "$REDIS_CONTAINER")"

  if ! wait_for_postgres; then
    SNAPSHOT_POSTGRES="fail"
    SNAPSHOT_FAILURE_REASON="postgres-not-ready"
    record_state "postgres-not-ready" "PostgreSQL is not ready; refusing to start or recreate new-api"
    return 1
  fi
  SNAPSHOT_POSTGRES="pass"

  if ! wait_for_redis; then
    SNAPSHOT_REDIS="fail"
    SNAPSHOT_FAILURE_REASON="redis-not-ready"
    record_state "redis-not-ready" "Redis is not ready; refusing to start or recreate new-api"
    return 1
  fi
  SNAPSHOT_REDIS="pass"

  if ! start_container_if_needed "$APP_CONTAINER"; then
    if [ "$SNAPSHOT_ACTION" = "container-inspect-error" ]; then
      SNAPSHOT_FAILURE_REASON="app-container-inspect-failed"
      record_state "app-container-inspect-failed" "Docker inspect failed for new-api container=$APP_CONTAINER; refusing lifecycle action"
    else
      refresh_app_snapshot
      SNAPSHOT_FAILURE_REASON="app-container-recovery-failed"
      record_state "app-container-recovery-failed" "failed to start new-api container=$APP_CONTAINER"
    fi
    return 1
  fi
  refresh_app_snapshot
  if [ "$MONITOR_ONLY" = "1" ] && [ "$SNAPSHOT_APP_HEALTH" != "healthy" ]; then
    APP_HEALTH_DRIFT=1
    SNAPSHOT_FAILURE_REASON="app-health-drift"
  fi

  if ! probe_backend; then
    SNAPSHOT_FAILURE_REASON="backend-failed"
    app_failure_count="$(increment_counter "$APP_FAILURE_COUNT_FILE")"
    record_state "backend-failed" "backend health check failed url=$BACKEND_URL failures=$app_failure_count"
    if probe_public && check_lan_entry; then
      clear_counter "$APP_FAILURE_COUNT_FILE"
      SNAPSHOT_FAILURE_REASON="backend-failed-public-healthy"
      record_state "backend-failed-public-healthy" "backend health check failed while public and LAN entries remained healthy; refusing app restart"
      return 1
    fi
    if restart_app_if_needed "$app_failure_count"; then
      refresh_app_snapshot
      if probe_backend && probe_public && check_lan_entry; then
        clear_counter "$APP_FAILURE_COUNT_FILE"
        if record_healthy_state "new-api recovery completed after app restart"; then
          return 0
        fi
        return 1
      fi
    fi
    return 1
  fi
  clear_counter "$APP_FAILURE_COUNT_FILE"

  if probe_public; then
    if ! check_lan_entry; then
      return 1
    fi
    if ! rotate_caddy_log_if_needed; then
      record_state "caddy-log-rotation-failed" "Caddy log rotation failed; health checks remain available"
    fi
    clear_counter "$CADDY_FAILURE_COUNT_FILE"
    if [ "$APP_HEALTH_DRIFT" = "1" ]; then
      SNAPSHOT_FAILURE_REASON="app-health-drift"
      record_state "app-health-drift" "new-api Docker health is not healthy health=$SNAPSHOT_APP_HEALTH; backend, public and LAN probes passed"
      return 1
    fi
    if record_healthy_state "new-api recovery check passed"; then
      return 0
    fi
    return 1
  fi

  SNAPSHOT_FAILURE_REASON="public-failed"
  public_failure_count="$(increment_counter "$CADDY_FAILURE_COUNT_FILE")"
  record_state "public-failed" "public health check failed url=$PUBLIC_URL; backend is healthy failures=$public_failure_count"
  if [ "$public_failure_count" -lt "$CADDY_RESTART_FAILURE_THRESHOLD" ]; then
    return 1
  fi
  restart_caddy "$public_failure_count" || return 1
  if ! probe_public; then
    restart_failures="$(increment_counter "$CADDY_RESTART_FAILURE_COUNT_FILE")"
    record_state "caddy-restart-probe-failed" "public health check still failed after Caddy restart restart_failures=$restart_failures"
    return 1
  fi
  clear_counter "$CADDY_RESTART_FAILURE_COUNT_FILE"
  if ! check_lan_entry; then
    return 1
  fi

  if ! rotate_caddy_log_if_needed; then
    record_state "caddy-log-rotation-failed" "Caddy log rotation failed after Caddy restart; health checks remain available"
  fi
  clear_counter "$CADDY_FAILURE_COUNT_FILE"
  record_healthy_state "new-api recovery completed after Caddy restart" || return 1
}

main "$@"
