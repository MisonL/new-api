#!/usr/bin/env bash
set -euo pipefail
umask 077

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WATCHDOG_PATH="${NEW_API_RECOVERY_WATCHDOG_PATH:-$ROOT_DIR/scripts/macos-docker-tun-recovery-watchdog.sh}"
RECOVERY_ENV_FILE="${NEW_API_RECOVERY_ENV_FILE:-$ROOT_DIR/.env}"
MONITOR_LOCK_DIR="${NEW_API_MONITOR_LOCK_DIR:-${HOME}/Library/Application Support/New API/recovery/monitor.lock}"
MONITOR_CURL_BIN="${NEW_API_MONITOR_CURL_BIN:-${NEW_API_RECOVERY_CURL_BIN:-}}"
BACKEND_URL="${NEW_API_RECOVERY_BACKEND_URL:-http://127.0.0.1:13000/api/healthz}"
PUBLIC_URL="${NEW_API_RECOVERY_PUBLIC_URL:-http://127.0.0.1:3000/api/healthz}"
LAN_URL="${NEW_API_RECOVERY_LAN_URL:-}"
DF_BIN="${NEW_API_MONITOR_DF_BIN:-/bin/df}"
VM_SWAPUSAGE_BIN="${NEW_API_MONITOR_VM_SWAPUSAGE_BIN:-/usr/sbin/sysctl}"
LOADAVG_BIN="${NEW_API_MONITOR_LOADAVG_BIN:-/usr/sbin/sysctl}"
CPU_COUNT_BIN="${NEW_API_MONITOR_CPU_COUNT_BIN:-/usr/sbin/sysctl}"
STAT_BIN="${NEW_API_MONITOR_STAT_BIN:-/usr/bin/stat}"
DU_BIN="${NEW_API_MONITOR_DU_BIN:-/usr/bin/du}"
MONITOR_LOG_FILE="${NEW_API_MONITOR_LOG_FILE:-}"
MONITOR_LOG_ROTATION_MAX_BYTES="${NEW_API_MONITOR_LOG_ROTATION_MAX_BYTES:-10485760}"
MONITOR_LOG_ROTATION_KEEP="${NEW_API_MONITOR_LOG_ROTATION_KEEP:-3}"
APPLICATION_LOG_DIR="${NEW_API_MONITOR_APP_LOG_DIR:-}"
APP_LOG_MAX_BYTES="${NEW_API_MONITOR_APP_LOG_MAX_BYTES:-5368709120}"
APP_LOG_WARN_BYTES="${NEW_API_MONITOR_APP_LOG_WARN_BYTES:-4294967296}"
DISK_MAX_PERCENT="${NEW_API_MONITOR_DISK_MAX_PERCENT:-90}"
DISK_PATHS="${NEW_API_MONITOR_DISK_PATHS:-/}"
SWAP_MAX_PERCENT="${NEW_API_MONITOR_SWAP_MAX_PERCENT:-95}"
LOG_MAX_BYTES="${NEW_API_MONITOR_LOG_MAX_BYTES:-5368709120}"
DISK_WARN_PERCENT="${NEW_API_MONITOR_DISK_WARN_PERCENT:-80}"
SWAP_WARN_PERCENT="${NEW_API_MONITOR_SWAP_WARN_PERCENT:-85}"
LOG_WARN_BYTES="${NEW_API_MONITOR_LOG_WARN_BYTES:-4294967296}"
LOAD_MAX_PER_CPU="${NEW_API_MONITOR_LOAD_MAX_PER_CPU:-4}"
LOAD_WARN_PER_CPU="${NEW_API_MONITOR_LOAD_WARN_PER_CPU:-2}"
LATENCY_WARN_MS="${NEW_API_MONITOR_LATENCY_WARN_MS:-1000}"
LATENCY_MAX_MS="${NEW_API_MONITOR_LATENCY_MAX_MS:-5000}"
LATENCY_CONNECT_TIMEOUT="${NEW_API_MONITOR_LATENCY_CONNECT_TIMEOUT:-1}"
LATENCY_MAX_TIME="${NEW_API_MONITOR_LATENCY_MAX_TIME:-5}"
CADDY_LOG_FILE="${NEW_API_RECOVERY_CADDY_LOG_FILE:-${HOME}/Library/Application Support/New API/caddy/new-api-macos-tun-caddy-3000.log}"
RECOVERY_LOG_FILE="${NEW_API_RECOVERY_LOG_FILE:-${HOME}/Library/Application Support/New API/recovery/recovery.log}"
MONITOR_STATUS_FILE="${NEW_API_MONITOR_STATUS_FILE:-}"
MONITOR_PROBE_SNAPSHOT_FILE="${NEW_API_MONITOR_PROBE_SNAPSHOT_FILE:-}"
MONITOR_PROBE_SNAPSHOT_MAX_AGE_SECONDS="${NEW_API_MONITOR_PROBE_SNAPSHOT_MAX_AGE_SECONDS:-600}"
DATE_BIN="${NEW_API_MONITOR_DATE_BIN:-/bin/date}"
RESOURCE_FAILURE=0
RESOURCE_WARNING=0
RESOURCE_FAILURE_REASONS=""
RESOURCE_WARNING_REASONS=""

if [ -z "$MONITOR_PROBE_SNAPSHOT_FILE" ] && [ -n "$MONITOR_STATUS_FILE" ]; then
  MONITOR_PROBE_SNAPSHOT_FILE="${MONITOR_STATUS_FILE}.probes"
fi

normalize_uint() {
  local value="$1"
  local fallback="$2"
  case "$value" in
    ''|*[!0-9]*) printf '%s' "$fallback" ;;
    *) printf '%s' "$value" ;;
  esac
}

DISK_MAX_PERCENT="$(normalize_uint "$DISK_MAX_PERCENT" 90)"
SWAP_MAX_PERCENT="$(normalize_uint "$SWAP_MAX_PERCENT" 95)"
LOG_MAX_BYTES="$(normalize_uint "$LOG_MAX_BYTES" 5368709120)"
APP_LOG_MAX_BYTES="$(normalize_uint "$APP_LOG_MAX_BYTES" 5368709120)"
DISK_WARN_PERCENT="$(normalize_uint "$DISK_WARN_PERCENT" 80)"
SWAP_WARN_PERCENT="$(normalize_uint "$SWAP_WARN_PERCENT" 85)"
LOG_WARN_BYTES="$(normalize_uint "$LOG_WARN_BYTES" 4294967296)"
APP_LOG_WARN_BYTES="$(normalize_uint "$APP_LOG_WARN_BYTES" 4294967296)"
LOAD_MAX_PER_CPU="$(normalize_uint "$LOAD_MAX_PER_CPU" 4)"
LOAD_WARN_PER_CPU="$(normalize_uint "$LOAD_WARN_PER_CPU" 2)"
LATENCY_WARN_MS="$(normalize_uint "$LATENCY_WARN_MS" 1000)"
LATENCY_MAX_MS="$(normalize_uint "$LATENCY_MAX_MS" 5000)"
LATENCY_CONNECT_TIMEOUT="$(normalize_uint "$LATENCY_CONNECT_TIMEOUT" 1)"
LATENCY_MAX_TIME="$(normalize_uint "$LATENCY_MAX_TIME" 5)"
MONITOR_LOG_ROTATION_MAX_BYTES="$(normalize_uint "$MONITOR_LOG_ROTATION_MAX_BYTES" 10485760)"
MONITOR_LOG_ROTATION_KEEP="$(normalize_uint "$MONITOR_LOG_ROTATION_KEEP" 3)"
MONITOR_PROBE_SNAPSHOT_MAX_AGE_SECONDS="$(normalize_uint "$MONITOR_PROBE_SNAPSHOT_MAX_AGE_SECONDS" 600)"
if [ "$MONITOR_LOG_ROTATION_KEEP" -lt 1 ]; then
  MONITOR_LOG_ROTATION_KEEP=1
fi
if [ "$MONITOR_PROBE_SNAPSHOT_MAX_AGE_SECONDS" -lt 1 ]; then
  MONITOR_PROBE_SNAPSHOT_MAX_AGE_SECONDS=600
fi

if [ "$LATENCY_WARN_MS" -lt 1 ]; then
  LATENCY_WARN_MS=1000
fi
if [ "$LATENCY_MAX_MS" -lt "$LATENCY_WARN_MS" ]; then
  LATENCY_MAX_MS="$LATENCY_WARN_MS"
fi
if [ "$LATENCY_CONNECT_TIMEOUT" -lt 1 ]; then
  LATENCY_CONNECT_TIMEOUT=1
fi
if [ "$LATENCY_MAX_TIME" -lt 1 ]; then
  LATENCY_MAX_TIME=5
fi

if [ -z "$MONITOR_CURL_BIN" ]; then
  MONITOR_CURL_BIN="$(command -v curl 2>/dev/null || true)"
fi

resource_failure() {
  RESOURCE_FAILURE=1
  if [ -n "$RESOURCE_FAILURE_REASONS" ]; then
    RESOURCE_FAILURE_REASONS="${RESOURCE_FAILURE_REASONS};$1"
  else
    RESOURCE_FAILURE_REASONS="$1"
  fi
  printf 'monitor_resource_status=fail reason=%s\n' "$1" >&2
}

resource_warning() {
  RESOURCE_WARNING=1
  if [ -n "$RESOURCE_WARNING_REASONS" ]; then
    RESOURCE_WARNING_REASONS="${RESOURCE_WARNING_REASONS};$1"
  else
    RESOURCE_WARNING_REASONS="$1"
  fi
  printf 'monitor_resource_status=warn reason=%s\n' "$1" >&2
}

check_disk_path() {
  local path="$1"
  local usage
  if [ ! -d "$path" ]; then
    resource_failure "disk-path-missing:${path}"
    return 0
  fi
  if [ ! -x "$DF_BIN" ]; then
    resource_failure "df-unavailable:${DF_BIN}"
    return 0
  fi
  usage="$("$DF_BIN" -Pk "$path" 2>/dev/null | awk 'NR == 2 { gsub("%", "", $5); print $5 }' || true)"
  case "$usage" in
    ''|*[!0-9]*)
      resource_failure "disk-usage-unreadable:${path}"
      return 0
      ;;
  esac
  printf 'monitor_resource_disk path=%s used_percent=%s warn_percent=%s max_percent=%s\n' "$path" "$usage" "$DISK_WARN_PERCENT" "$DISK_MAX_PERCENT"
  if [ "$usage" -ge "$DISK_MAX_PERCENT" ]; then
    resource_failure "disk-usage-high:${path}:${usage}%"
  elif [ "$usage" -ge "$DISK_WARN_PERCENT" ]; then
    resource_warning "disk-usage-warning:${path}:${usage}%"
  fi
}

check_swap_usage() {
  local output
  local usage
  if [ ! -x "$VM_SWAPUSAGE_BIN" ]; then
    resource_failure "vm-swapusage-unavailable:${VM_SWAPUSAGE_BIN}"
    return 0
  fi
  output="$("$VM_SWAPUSAGE_BIN" -n vm.swapusage 2>/dev/null || true)"
  usage="$(printf '%s\n' "$output" | sed -n 's/.*(\([0-9][0-9.]*\)%).*/\1/p' | tail -n 1)"
  if [ -z "$usage" ]; then
    usage="$(printf '%s\n' "$output" | sed -n 's/.*total = \([0-9][0-9.]*\)M[[:space:]]*used = \([0-9][0-9.]*\)M.*/\1 \2/p' | awk 'NF == 2 && $1 > 0 { printf "%.2f", ($2 * 100) / $1 }')"
  fi
  case "$usage" in
    ''|*[!0-9.]*)
      resource_failure "swap-usage-unreadable"
      return 0
      ;;
  esac
  printf 'monitor_resource_swap used_percent=%s warn_percent=%s max_percent=%s\n' "$usage" "$SWAP_WARN_PERCENT" "$SWAP_MAX_PERCENT"
  if awk -v usage="$usage" -v maximum="$SWAP_MAX_PERCENT" 'BEGIN { exit !(usage >= maximum) }'; then
    resource_failure "swap-usage-high:${usage}%"
  elif awk -v usage="$usage" -v warning="$SWAP_WARN_PERCENT" 'BEGIN { exit !(usage >= warning) }'; then
    resource_warning "swap-usage-warning:${usage}%"
  fi
}

check_load_average() {
  local load_output
  local load_1m
  local cpu_count
  local normalized
  if [ ! -x "$LOADAVG_BIN" ]; then
    resource_failure "loadavg-unavailable:$LOADAVG_BIN"
    return 0
  fi
  if [ ! -x "$CPU_COUNT_BIN" ]; then
    resource_failure "cpu-count-unavailable:$CPU_COUNT_BIN"
    return 0
  fi
  load_output="$("$LOADAVG_BIN" -n vm.loadavg 2>/dev/null || true)"
  load_1m="$(printf '%s\n' "$load_output" | awk '{ for (i = 1; i <= NF; i++) { gsub(/[{}]/, "", $i); if ($i ~ /^[0-9]+(\.[0-9]+)?$/) { print $i; exit } } }')"
  cpu_count="$("$CPU_COUNT_BIN" -n hw.ncpu 2>/dev/null || true)"
  case "$load_1m" in
    ''|*[!0-9.]*)
      resource_failure "loadavg-unreadable"
      return 0
      ;;
  esac
  case "$cpu_count" in
    ''|*[!0-9]*|0)
      resource_failure "cpu-count-unreadable"
      return 0
      ;;
  esac
  normalized="$(awk -v load="$load_1m" -v cpus="$cpu_count" 'BEGIN { printf "%.2f", load / cpus }')"
  printf 'monitor_resource_load one_minute=%s cpu_count=%s normalized_per_cpu=%s warn_per_cpu=%s max_per_cpu=%s\n' \
    "$load_1m" "$cpu_count" "$normalized" "$LOAD_WARN_PER_CPU" "$LOAD_MAX_PER_CPU"
  if awk -v load="$load_1m" -v cpus="$cpu_count" -v maximum="$LOAD_MAX_PER_CPU" 'BEGIN { exit !(load >= maximum * cpus) }'; then
    resource_failure "load-average-high:$load_1m:${cpu_count}cpu"
  elif awk -v load="$load_1m" -v cpus="$cpu_count" -v warning="$LOAD_WARN_PER_CPU" 'BEGIN { exit !(load >= warning * cpus) }'; then
    resource_warning "load-average-warning:$load_1m:${cpu_count}cpu"
  fi
}

check_log_size() {
  local label="$1"
  local path="$2"
  local size
  if [ "$LOG_MAX_BYTES" -eq 0 ]; then
    return 0
  fi
  if [ ! -f "$path" ]; then
    resource_failure "log-missing:${label}"
    return 0
  fi
  if [ ! -x "$STAT_BIN" ]; then
    resource_failure "stat-unavailable:${STAT_BIN}"
    return 0
  fi
  size="$("$STAT_BIN" -f '%z' "$path" 2>/dev/null || true)"
  case "$size" in
    ''|*[!0-9]*)
      resource_failure "log-size-unreadable:${label}"
      return 0
      ;;
  esac
  printf 'monitor_resource_log label=%s size_bytes=%s warn_bytes=%s max_bytes=%s\n' "$label" "$size" "$LOG_WARN_BYTES" "$LOG_MAX_BYTES"
  if [ "$size" -ge "$LOG_MAX_BYTES" ]; then
    resource_failure "log-size-high:${label}:${size}B"
  elif [ "$size" -ge "$LOG_WARN_BYTES" ]; then
    resource_warning "log-size-warning:${label}:${size}B"
  fi
}

check_log_directory_size() {
  local label="$1"
  local path="$2"
  local size_kb
  local size_bytes

  if [ -z "$path" ] || [ "$APP_LOG_MAX_BYTES" -eq 0 ]; then
    return 0
  fi
  if [ ! -d "$path" ]; then
    resource_failure "log-directory-missing:${label}"
    return 0
  fi
  if [ ! -x "$DU_BIN" ]; then
    resource_failure "du-unavailable:${DU_BIN}"
    return 0
  fi
  size_kb="$("$DU_BIN" -sk "$path" 2>/dev/null | awk 'NR == 1 { print $1 }' || true)"
  case "$size_kb" in
    ''|*[!0-9]*)
      resource_failure "log-directory-size-unreadable:${label}"
      return 0
      ;;
  esac
  size_bytes=$((size_kb * 1024))
  printf 'monitor_resource_log_dir label=%s size_bytes=%s warn_bytes=%s max_bytes=%s\n' \
    "$label" "$size_bytes" "$APP_LOG_WARN_BYTES" "$APP_LOG_MAX_BYTES"
  if [ "$size_bytes" -ge "$APP_LOG_MAX_BYTES" ]; then
    resource_failure "log-directory-size-high:${label}:${size_bytes}B"
  elif [ "$size_bytes" -ge "$APP_LOG_WARN_BYTES" ]; then
    resource_warning "log-directory-size-warning:${label}:${size_bytes}B"
  fi
}

rotate_monitor_log_if_needed() {
  local size
  local source
  local destination
  local index

  if [ -z "$MONITOR_LOG_FILE" ] || [ "$MONITOR_LOG_ROTATION_MAX_BYTES" -eq 0 ] || [ ! -f "$MONITOR_LOG_FILE" ]; then
    return 0
  fi
  if [ ! -x "$STAT_BIN" ]; then
    printf 'monitor_log_rotation=failed reason=stat-unavailable\n' >&2
    return 1
  fi
  size="$("$STAT_BIN" -f '%z' "$MONITOR_LOG_FILE" 2>/dev/null || printf '0')"
  case "$size" in
    ''|*[!0-9]*) return 0 ;;
  esac
  if [ "$size" -lt "$MONITOR_LOG_ROTATION_MAX_BYTES" ]; then
    return 0
  fi

  chmod 0600 "$MONITOR_LOG_FILE" 2>/dev/null || true
  for ((index = MONITOR_LOG_ROTATION_KEEP - 1; index >= 1; index--)); do
    source="${MONITOR_LOG_FILE}.${index}"
    destination="${MONITOR_LOG_FILE}.$((index + 1))"
    if [ -f "$source" ]; then
      if ! mv -f "$source" "$destination"; then
        printf 'monitor_log_rotation=failed reason=move\n' >&2
        return 1
      fi
      chmod 0600 "$destination" 2>/dev/null || true
    fi
  done
  if ! cp "$MONITOR_LOG_FILE" "${MONITOR_LOG_FILE}.1"; then
    printf 'monitor_log_rotation=failed reason=copy\n' >&2
    return 1
  fi
  chmod 0600 "${MONITOR_LOG_FILE}.1"
  if ! : >"$MONITOR_LOG_FILE"; then
    printf 'monitor_log_rotation=failed reason=truncate\n' >&2
    return 1
  fi
  chmod 0600 "$MONITOR_LOG_FILE"
  printf 'monitor_log_rotation=rotated size_bytes=%s keep=%s\n' "$size" "$MONITOR_LOG_ROTATION_KEEP"
}

check_latency_probe() {
  local label="$1"
  local url="$2"
  local result=""
  local status=""
  local seconds=""
  local time_ms=""
  local curl_exit=0

  if [ -z "$url" ]; then
    printf 'monitor_latency label=%s status=disabled\n' "$label"
    return 0
  fi
  if [ -z "$MONITOR_CURL_BIN" ] || [ ! -x "$MONITOR_CURL_BIN" ]; then
    resource_failure "latency-curl-unavailable:${label}"
    return 0
  fi

  result="$("$MONITOR_CURL_BIN" -fsS \
    --connect-timeout "$LATENCY_CONNECT_TIMEOUT" \
    --max-time "$LATENCY_MAX_TIME" \
    -o /dev/null \
    -w '%{http_code} %{time_total}' \
    "$url" 2>/dev/null)" || curl_exit=$?
  if ! read -r status seconds <<<"$result"; then
    resource_failure "latency-probe-failed:${label}"
    return 0
  fi
  case "$status" in
    ''|*[!0-9]*)
      resource_failure "latency-status-unreadable:${label}"
      return 0
      ;;
    000)
      case "$curl_exit" in
        28)
          resource_failure "latency-timeout:${label}"
          ;;
        6|7)
          resource_failure "latency-connect-failed:${label}:${curl_exit}"
          ;;
        *)
          resource_failure "latency-no-response:${label}:${curl_exit}"
          ;;
      esac
      return 0
      ;;
  esac
  time_ms="$(awk -v value="$seconds" 'BEGIN { if (value !~ /^[0-9]+([.][0-9]+)?$/) exit 1; printf "%.0f", value * 1000 }' 2>/dev/null || true)"
  case "$time_ms" in
    ''|*[!0-9]*)
      resource_failure "latency-value-unreadable:${label}"
      return 0
      ;;
  esac
  printf 'monitor_latency label=%s status=%s time_ms=%s curl_exit=%s warn_ms=%s max_ms=%s\n' \
    "$label" "$status" "$time_ms" "$curl_exit" "$LATENCY_WARN_MS" "$LATENCY_MAX_MS"
  if [ "$status" -ne 200 ]; then
    resource_failure "latency-http:${label}:${status}"
  elif [ "$time_ms" -ge "$LATENCY_MAX_MS" ]; then
    resource_failure "latency-high:${label}:${time_ms}ms"
  elif [ "$time_ms" -ge "$LATENCY_WARN_MS" ]; then
    resource_warning "latency-warning:${label}:${time_ms}ms"
  fi
}

check_latency() {
  check_latency_probe "backend" "$BACKEND_URL"
  check_latency_probe "public" "$PUBLIC_URL"
  check_latency_probe "lan" "$LAN_URL"
}

write_monitor_status() {
  local status="$1"
  local exit_code="$2"
  local watchdog_exit_code="$3"
  local status_dir
  local temporary_file

  [ -n "$MONITOR_STATUS_FILE" ] || return 0
  status_dir="$(dirname "$MONITOR_STATUS_FILE")"
  temporary_file="${MONITOR_STATUS_FILE}.tmp.$$"
  if ! mkdir -p "$status_dir" || ! chmod 0700 "$status_dir"; then
    printf 'monitor_status_file=error reason=directory-unavailable\n' >&2
    return 1
  fi
  if ! {
    printf 'schema_version=1\n'
    printf 'checked_at_utc=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
    printf 'monitor_status=%s\n' "$status"
    printf 'exit_code=%s\n' "$exit_code"
    printf 'watchdog_exit_code=%s\n' "$watchdog_exit_code"
    printf 'resource_failure=%s\n' "$RESOURCE_FAILURE"
    printf 'resource_warning=%s\n' "$RESOURCE_WARNING"
    printf 'resource_failure_reasons=%s\n' "${RESOURCE_FAILURE_REASONS:-none}"
    printf 'resource_warning_reasons=%s\n' "${RESOURCE_WARNING_REASONS:-none}"
    printf 'probe_snapshot_status=%s\n' "$PROBE_SNAPSHOT_STATUS"
    printf 'probe_snapshot_age_seconds=%s\n' "$PROBE_SNAPSHOT_AGE_SECONDS"
    printf 'probe_checked_at_utc=%s\n' "$PROBE_CHECKED_AT_UTC"
    printf 'probe_state=%s\n' "$PROBE_STATE"
    printf 'probe_docker_daemon=%s\n' "$PROBE_DOCKER_DAEMON"
    printf 'probe_postgres=%s\n' "$PROBE_POSTGRES"
    printf 'probe_redis=%s\n' "$PROBE_REDIS"
    printf 'probe_postgres_docker_health=%s\n' "$PROBE_POSTGRES_DOCKER_HEALTH"
    printf 'probe_redis_docker_health=%s\n' "$PROBE_REDIS_DOCKER_HEALTH"
    printf 'probe_backend=%s\n' "$PROBE_BACKEND"
    printf 'probe_public=%s\n' "$PROBE_PUBLIC"
    printf 'probe_lan=%s\n' "$PROBE_LAN"
    printf 'probe_app_state=%s\n' "$PROBE_APP_STATE"
    printf 'probe_app_health=%s\n' "$PROBE_APP_HEALTH"
    printf 'probe_app_restart_count=%s\n' "$PROBE_APP_RESTART_COUNT"
    printf 'probe_app_oom=%s\n' "$PROBE_APP_OOM"
    printf 'probe_failure_reason=%s\n' "$PROBE_FAILURE_REASON"
    printf 'probe_action=%s\n' "$PROBE_ACTION"
  } >"$temporary_file"; then
    rm -f "$temporary_file"
    printf 'monitor_status_file=error reason=write-failed\n' >&2
    return 1
  fi
  chmod 0600 "$temporary_file" || {
    rm -f "$temporary_file"
    printf 'monitor_status_file=error reason=permission-failed\n' >&2
    return 1
  }
  if ! mv -f "$temporary_file" "$MONITOR_STATUS_FILE"; then
    rm -f "$temporary_file"
    printf 'monitor_status_file=error reason=install-failed\n' >&2
    return 1
  fi
  printf 'monitor_status_file=written path=%s\n' "$MONITOR_STATUS_FILE"
}

check_resources() {
  local disk_path
  local -a disk_paths=()
  IFS=':' read -r -a disk_paths <<<"$DISK_PATHS"
  for disk_path in "${disk_paths[@]}"; do
    [ -n "$disk_path" ] || continue
    check_disk_path "$disk_path"
  done
  check_swap_usage
  check_load_average
  check_log_size "caddy" "$CADDY_LOG_FILE"
  check_log_size "recovery" "$RECOVERY_LOG_FILE"
  check_log_directory_size "application" "$APPLICATION_LOG_DIR"
}

probe_snapshot_value() {
  local key="$1"
  [ -n "$MONITOR_PROBE_SNAPSHOT_FILE" ] || return 0
  [ -r "$MONITOR_PROBE_SNAPSHOT_FILE" ] || return 0
  awk -F= -v key="$key" '$1 == key { print substr($0, index($0, "=") + 1); exit }' "$MONITOR_PROBE_SNAPSHOT_FILE"
}

parse_utc_epoch() {
  local timestamp="$1"
  local epoch

  [[ "$timestamp" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]] || return 1
  epoch="$("$DATE_BIN" -j -u -f '%Y-%m-%dT%H:%M:%SZ' "$timestamp" '+%s' 2>/dev/null || true)"
  if [[ "$epoch" =~ ^[0-9]+$ ]]; then
    printf '%s' "$epoch"
    return 0
  fi
  epoch="$("$DATE_BIN" -u -d "$timestamp" '+%s' 2>/dev/null || true)"
  [[ "$epoch" =~ ^[0-9]+$ ]] || return 1
  printf '%s' "$epoch"
}

current_utc_epoch() {
  local epoch
  epoch="$("$DATE_BIN" -u '+%s' 2>/dev/null || true)"
  [[ "$epoch" =~ ^[0-9]+$ ]] || return 1
  printf '%s' "$epoch"
}

load_probe_snapshot() {
  local current_epoch snapshot_epoch

  PROBE_SNAPSHOT_STATUS="missing"
  PROBE_SNAPSHOT_AGE_SECONDS="unknown"
  PROBE_CHECKED_AT_UTC="unknown"
  PROBE_STATE="unknown"
  PROBE_DOCKER_DAEMON="unknown"
  PROBE_POSTGRES="unknown"
  PROBE_REDIS="unknown"
  PROBE_POSTGRES_DOCKER_HEALTH="unknown"
  PROBE_REDIS_DOCKER_HEALTH="unknown"
  PROBE_BACKEND="unknown"
  PROBE_PUBLIC="unknown"
  PROBE_LAN="unknown"
  PROBE_APP_STATE="unknown"
  PROBE_APP_HEALTH="unknown"
  PROBE_APP_RESTART_COUNT="unknown"
  PROBE_APP_OOM="unknown"
  PROBE_FAILURE_REASON="unknown"
  PROBE_ACTION="unknown"

  if [ -n "$MONITOR_PROBE_SNAPSHOT_FILE" ] && [ -r "$MONITOR_PROBE_SNAPSHOT_FILE" ]; then
    PROBE_CHECKED_AT_UTC="$(probe_snapshot_value checked_at_utc)"
    snapshot_epoch="$(parse_utc_epoch "$PROBE_CHECKED_AT_UTC" || true)"
    current_epoch="$(current_utc_epoch || true)"
    if [[ "$snapshot_epoch" =~ ^[0-9]+$ ]] && [[ "$current_epoch" =~ ^[0-9]+$ ]]; then
      if [ "$snapshot_epoch" -gt "$current_epoch" ]; then
        PROBE_SNAPSHOT_STATUS="invalid"
      else
        PROBE_SNAPSHOT_AGE_SECONDS=$((current_epoch - snapshot_epoch))
        if [ "$PROBE_SNAPSHOT_AGE_SECONDS" -le "$MONITOR_PROBE_SNAPSHOT_MAX_AGE_SECONDS" ]; then
          PROBE_SNAPSHOT_STATUS="pass"
          PROBE_STATE="$(probe_snapshot_value state)"
          PROBE_DOCKER_DAEMON="$(probe_snapshot_value docker_daemon)"
          PROBE_POSTGRES="$(probe_snapshot_value postgres)"
          PROBE_REDIS="$(probe_snapshot_value redis)"
          PROBE_POSTGRES_DOCKER_HEALTH="$(probe_snapshot_value postgres_docker_health)"
          PROBE_REDIS_DOCKER_HEALTH="$(probe_snapshot_value redis_docker_health)"
          PROBE_BACKEND="$(probe_snapshot_value backend)"
          PROBE_PUBLIC="$(probe_snapshot_value public)"
          PROBE_LAN="$(probe_snapshot_value lan)"
          PROBE_APP_STATE="$(probe_snapshot_value app_state)"
          PROBE_APP_HEALTH="$(probe_snapshot_value app_health)"
          PROBE_APP_RESTART_COUNT="$(probe_snapshot_value app_restart_count)"
          PROBE_APP_OOM="$(probe_snapshot_value app_oom)"
          PROBE_FAILURE_REASON="$(probe_snapshot_value failure_reason)"
          PROBE_ACTION="$(probe_snapshot_value action)"
        else
          PROBE_SNAPSHOT_STATUS="stale"
        fi
      fi
    else
      PROBE_SNAPSHOT_STATUS="invalid"
    fi
  fi
}

if [ ! -x "$WATCHDOG_PATH" ]; then
  printf 'monitor_status=error reason=watchdog-unavailable path=%s\n' "$WATCHDOG_PATH" >&2
  exit 2
fi

run_watchdog() {
  if [ -n "$MONITOR_PROBE_SNAPSHOT_FILE" ]; then
    NEW_API_RECOVERY_DRY_RUN=1 \
    NEW_API_RECOVERY_MONITOR_ONLY=1 \
    NEW_API_RECOVERY_FORCE_DOCKER_RESTART=0 \
    NEW_API_RECOVERY_ENV_FILE="$RECOVERY_ENV_FILE" \
    NEW_API_RECOVERY_LOCK_DIR="$MONITOR_LOCK_DIR" \
    NEW_API_RECOVERY_HEALTH_SNAPSHOT_FILE="$MONITOR_PROBE_SNAPSHOT_FILE" \
    NEW_API_RECOVERY_WRITE_DRY_RUN_SNAPSHOT=1 \
      "$WATCHDOG_PATH"
    return $?
  fi
  NEW_API_RECOVERY_DRY_RUN=1 \
  NEW_API_RECOVERY_MONITOR_ONLY=1 \
  NEW_API_RECOVERY_FORCE_DOCKER_RESTART=0 \
  NEW_API_RECOVERY_ENV_FILE="$RECOVERY_ENV_FILE" \
  NEW_API_RECOVERY_LOCK_DIR="$MONITOR_LOCK_DIR" \
    "$WATCHDOG_PATH"
}

set +e
run_watchdog
watchdog_exit_code=$?
set -e

if ! rotate_monitor_log_if_needed; then
  printf 'monitor_log_rotation=warning\n' >&2
fi

load_probe_snapshot
check_resources
check_latency

exit_code="$watchdog_exit_code"
if [ -n "$MONITOR_PROBE_SNAPSHOT_FILE" ] && [ "$PROBE_SNAPSHOT_STATUS" != "pass" ]; then
  printf 'monitor_probe_snapshot_status=fail status=%s checked_at_utc=%s age_seconds=%s max_age_seconds=%s\n' \
    "$PROBE_SNAPSHOT_STATUS" "$PROBE_CHECKED_AT_UTC" "$PROBE_SNAPSHOT_AGE_SECONDS" \
    "$MONITOR_PROBE_SNAPSHOT_MAX_AGE_SECONDS" >&2
  if [ "$exit_code" -eq 0 ]; then
    exit_code=1
  fi
fi
if [ "$exit_code" -eq 0 ] && [ "$RESOURCE_FAILURE" -ne 0 ]; then
  exit_code=1
fi

monitor_status_value="fail"
if [ "$exit_code" -eq 0 ]; then
  if [ "$RESOURCE_WARNING" -ne 0 ]; then
    monitor_status_value="warn"
  else
    monitor_status_value="pass"
  fi
fi

if ! write_monitor_status "$monitor_status_value" "$exit_code" "$watchdog_exit_code"; then
  if [ "$exit_code" -eq 0 ]; then
    exit_code=2
  fi
fi

if [ "$exit_code" -eq 0 ]; then
  printf 'monitor_status=%s\n' "$monitor_status_value"
else
  if [ "$watchdog_exit_code" -ne 0 ]; then
    printf 'monitor_watchdog_status=fail exit_code=%s\n' "$watchdog_exit_code" >&2
  fi
  printf 'monitor_status=fail exit_code=%s\n' "$exit_code" >&2
fi
exit "$exit_code"
