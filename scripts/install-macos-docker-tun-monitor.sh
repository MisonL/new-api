#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LABEL="${NEW_API_MONITOR_LAUNCHD_LABEL:-com.new-api.macos-tun.new-api.monitor}"
DOMAIN="gui/$(id -u)"
SERVICE_TARGET="${DOMAIN}/${LABEL}"
APP_SUPPORT_DIR="${HOME}/Library/Application Support/New API"
INSTALL_DIR="${NEW_API_MONITOR_INSTALL_DIR:-${APP_SUPPORT_DIR}/monitor}"
PLIST_PATH="${NEW_API_MONITOR_LAUNCHD_PLIST:-${HOME}/Library/LaunchAgents/${LABEL}.plist}"
MONITOR_SCRIPT_PATH="${NEW_API_MONITOR_SCRIPT_PATH:-${INSTALL_DIR}/monitor.sh}"
LOG_PATH="${NEW_API_MONITOR_LOG_PATH:-${INSTALL_DIR}/monitor.log}"
STATUS_PATH="${NEW_API_MONITOR_STATUS_FILE:-${INSTALL_DIR}/monitor.status}"
LOCK_DIR="${NEW_API_MONITOR_LOCK_DIR:-${INSTALL_DIR}/monitor.lock}"
TEMPLATE_PATH="${NEW_API_MONITOR_TEMPLATE_PATH:-${ROOT_DIR}/deploy/macos/com.new-api.macos-tun.new-api.monitor.plist.example}"
SOURCE_MONITOR_PATH="${ROOT_DIR}/scripts/macos-docker-tun-monitor.sh"
RECOVERY_INSTALL_DIR="${NEW_API_RECOVERY_INSTALL_DIR:-${APP_SUPPORT_DIR}/recovery}"
WATCHDOG_PATH="${NEW_API_MONITOR_WATCHDOG_PATH:-${RECOVERY_INSTALL_DIR}/watchdog.sh}"
RECOVERY_ENV_FILE="${NEW_API_MONITOR_ENV_FILE:-${NEW_API_RECOVERY_ENV_FILE:-${ROOT_DIR}/.env}}"

read_env_file_value() {
  local key="$1"
  local value=""
  if [ ! -r "$RECOVERY_ENV_FILE" ]; then
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

CADDY_PLIST_PATH="${NEW_API_RECOVERY_CADDY_PLIST:-}"
CADDY_LABEL="${NEW_API_RECOVERY_CADDY_LABEL:-${CADDY_LAUNCHD_LABEL:-}}"
if [ -z "$CADDY_LABEL" ] && [ -n "$CADDY_PLIST_PATH" ]; then
  CADDY_LABEL="$(basename "$CADDY_PLIST_PATH" .plist)"
fi
if [ -z "${NEW_API_RECOVERY_CADDY_LABEL:-}" ] && [ -z "${CADDY_LAUNCHD_LABEL:-}" ] && [ -z "${NEW_API_RECOVERY_CADDY_PLIST:-}" ]; then
  env_caddy_label="$(read_env_file_value CADDY_LAUNCHD_LABEL || true)"
  env_caddy_plist="$(read_env_file_value CADDY_LAUNCHD_PLIST || true)"
  if [[ "$env_caddy_plist" == [~]/* ]]; then
    env_caddy_plist="$HOME/${env_caddy_plist:2}"
  fi
  if [ -n "$env_caddy_label" ]; then
    CADDY_LABEL="$env_caddy_label"
  elif [ -n "$env_caddy_plist" ]; then
    CADDY_LABEL="$(basename "$env_caddy_plist" .plist)"
  fi
  if [ -n "$env_caddy_plist" ]; then
    CADDY_PLIST_PATH="$env_caddy_plist"
  fi
fi
CADDY_LABEL="${CADDY_LABEL:-com.new-api.macos-tun.new-api.3000}"
if [ -z "$CADDY_PLIST_PATH" ]; then
  CADDY_PLIST_PATH="${HOME}/Library/LaunchAgents/${CADDY_LABEL}.plist"
fi
CADDY_LOG_FILE="${NEW_API_RECOVERY_CADDY_LOG_FILE:-${APP_SUPPORT_DIR}/caddy/new-api-macos-tun-caddy-3000.log}"
RECOVERY_LOG_FILE="${NEW_API_RECOVERY_LOG_FILE:-${APP_SUPPORT_DIR}/recovery/recovery.log}"
LAUNCHCTL_BIN="${NEW_API_MONITOR_LAUNCHCTL_BIN:-$(command -v launchctl 2>/dev/null || true)}"
DOCKER_BIN="${NEW_API_MONITOR_DOCKER_BIN:-${NEW_API_RECOVERY_DOCKER_BIN:-$(command -v docker 2>/dev/null || true)}}"
CURL_BIN="${NEW_API_MONITOR_CURL_BIN:-${NEW_API_RECOVERY_CURL_BIN:-$(command -v curl 2>/dev/null || true)}}"
PLUTIL_BIN="${NEW_API_MONITOR_PLUTIL_BIN:-$(command -v plutil 2>/dev/null || true)}"
BOOTSTRAP_RETRY_DELAY_SECONDS="${NEW_API_MONITOR_BOOTSTRAP_RETRY_DELAY_SECONDS:-1}"
MONITOR_LOG_ROTATION_MAX_BYTES="${NEW_API_MONITOR_LOG_ROTATION_MAX_BYTES:-10485760}"
MONITOR_LOG_ROTATION_KEEP="${NEW_API_MONITOR_LOG_ROTATION_KEEP:-3}"
APP_LOG_DIR="${NEW_API_MONITOR_APP_LOG_DIR:-$ROOT_DIR/logs}"
DISK_PATHS="${NEW_API_MONITOR_DISK_PATHS:-/}"
APP_LOG_MAX_BYTES="${NEW_API_MONITOR_APP_LOG_MAX_BYTES:-5368709120}"
APP_LOG_WARN_BYTES="${NEW_API_MONITOR_APP_LOG_WARN_BYTES:-4294967296}"
PROBE_SNAPSHOT_MAX_AGE_SECONDS="${NEW_API_MONITOR_PROBE_SNAPSHOT_MAX_AGE_SECONDS:-600}"
MODE="check-only"

usage() {
  cat <<'EOF'
usage: install-macos-docker-tun-monitor.sh [--check-only|--apply]

The default is --check-only. Only --apply writes the monitor LaunchAgent.
EOF
}

case "${1:---check-only}" in
  --check-only)
    MODE="check-only"
    ;;
  --apply)
    MODE="apply"
    ;;
  --help|-h)
    usage
    exit 0
    ;;
  *)
    usage >&2
    exit 2
    ;;
esac
if [ "$#" -gt 1 ]; then
  usage >&2
  exit 2
fi

case "$BOOTSTRAP_RETRY_DELAY_SECONDS" in
  ''|*[!0-9.]*) BOOTSTRAP_RETRY_DELAY_SECONDS=1 ;;
esac

case "$MONITOR_LOG_ROTATION_MAX_BYTES" in
  ''|*[!0-9]*) MONITOR_LOG_ROTATION_MAX_BYTES=10485760 ;;
esac
case "$MONITOR_LOG_ROTATION_KEEP" in
  ''|*[!0-9]*) MONITOR_LOG_ROTATION_KEEP=3 ;;
esac
if [ "$MONITOR_LOG_ROTATION_KEEP" -lt 1 ]; then
  MONITOR_LOG_ROTATION_KEEP=1
fi
case "$APP_LOG_MAX_BYTES" in
  ''|*[!0-9]*) APP_LOG_MAX_BYTES=5368709120 ;;
esac
case "$APP_LOG_WARN_BYTES" in
  ''|*[!0-9]*) APP_LOG_WARN_BYTES=4294967296 ;;
esac
case "$PROBE_SNAPSHOT_MAX_AGE_SECONDS" in
  ''|*[!0-9]*) PROBE_SNAPSHOT_MAX_AGE_SECONDS=600 ;;
esac
if [ "$PROBE_SNAPSHOT_MAX_AGE_SECONDS" -lt 1 ]; then
  PROBE_SNAPSHOT_MAX_AGE_SECONDS=600
fi

fail() {
  printf 'monitor_install_status=fail reason=%s\n' "$1" >&2
  exit 1
}

require_file() {
  local path="$1"
  local description="$2"
  [ -f "$path" ] || fail "${description}-missing"
}

require_executable() {
  local path="$1"
  local description="$2"
  [ -n "$path" ] && [ -x "$path" ] || fail "${description}-unavailable"
}

require_synced_watchdog() {
  if ! cmp -s "$ROOT_DIR/scripts/macos-docker-tun-recovery-watchdog.sh" "$WATCHDOG_PATH"; then
    fail "installed-watchdog-mismatch"
  fi
}

path_under_install_dir() {
  local path="$1"
  case "$path" in
    "$INSTALL_DIR"/*) return 0 ;;
    *) return 1 ;;
  esac
}

xml_escape() {
  printf '%s' "$1" \
    | sed -e 's/&/\&amp;/g' \
          -e 's/</\&lt;/g' \
          -e 's/>/\&gt;/g' \
          -e 's/"/\&quot;/g' \
          -e "s/'/\&apos;/g"
}

sed_replacement_escape() {
  printf '%s' "$1" | sed 's/[\\&|]/\\&/g'
}

check_inputs() {
  require_file "$TEMPLATE_PATH" "monitor-template"
  require_executable "$SOURCE_MONITOR_PATH" "monitor-source"
  require_executable "$WATCHDOG_PATH" "installed-watchdog"
  require_synced_watchdog
  require_file "$RECOVERY_ENV_FILE" "recovery-env-file"
  require_executable "$LAUNCHCTL_BIN" "launchctl"
  require_executable "$DOCKER_BIN" "docker"
  require_executable "$CURL_BIN" "curl"
  require_executable "$PLUTIL_BIN" "plutil"
  path_under_install_dir "$LOG_PATH" || fail "log-path-outside-private-install-dir"
  path_under_install_dir "$STATUS_PATH" || fail "status-path-outside-private-install-dir"
  path_under_install_dir "$LOCK_DIR" || fail "lock-path-outside-private-install-dir"
}

render_plist() {
  local destination="$1"
  local monitor_xml
  local watchdog_xml
  local env_xml
  local docker_xml
  local launchctl_xml
  local curl_xml
  local caddy_plist_xml
  local caddy_log_xml
  local recovery_log_xml
  local status_xml
  local lock_xml
  local log_xml
  local log_rotation_max_bytes_xml
  local log_rotation_keep_xml
  local app_log_dir_xml
  local app_log_max_bytes_xml
  local app_log_warn_bytes_xml
  local probe_snapshot_max_age_seconds_xml
  local disk_paths_xml
  local lan_url_xml
  local label_xml
  local caddy_label_xml

  monitor_xml="$(sed_replacement_escape "$(xml_escape "$MONITOR_SCRIPT_PATH")")"
  watchdog_xml="$(sed_replacement_escape "$(xml_escape "$WATCHDOG_PATH")")"
  env_xml="$(sed_replacement_escape "$(xml_escape "$RECOVERY_ENV_FILE")")"
  docker_xml="$(sed_replacement_escape "$(xml_escape "$DOCKER_BIN")")"
  launchctl_xml="$(sed_replacement_escape "$(xml_escape "$LAUNCHCTL_BIN")")"
  curl_xml="$(sed_replacement_escape "$(xml_escape "$CURL_BIN")")"
  caddy_plist_xml="$(sed_replacement_escape "$(xml_escape "$CADDY_PLIST_PATH")")"
  caddy_log_xml="$(sed_replacement_escape "$(xml_escape "$CADDY_LOG_FILE")")"
  recovery_log_xml="$(sed_replacement_escape "$(xml_escape "$RECOVERY_LOG_FILE")")"
  status_xml="$(sed_replacement_escape "$(xml_escape "$STATUS_PATH")")"
  lock_xml="$(sed_replacement_escape "$(xml_escape "$LOCK_DIR")")"
  log_xml="$(sed_replacement_escape "$(xml_escape "$LOG_PATH")")"
  log_rotation_max_bytes_xml="$(sed_replacement_escape "$(xml_escape "$MONITOR_LOG_ROTATION_MAX_BYTES")")"
  log_rotation_keep_xml="$(sed_replacement_escape "$(xml_escape "$MONITOR_LOG_ROTATION_KEEP")")"
  app_log_dir_xml="$(sed_replacement_escape "$(xml_escape "$APP_LOG_DIR")")"
  app_log_max_bytes_xml="$(sed_replacement_escape "$(xml_escape "$APP_LOG_MAX_BYTES")")"
  app_log_warn_bytes_xml="$(sed_replacement_escape "$(xml_escape "$APP_LOG_WARN_BYTES")")"
  probe_snapshot_max_age_seconds_xml="$(sed_replacement_escape "$(xml_escape "$PROBE_SNAPSHOT_MAX_AGE_SECONDS")")"
  disk_paths_xml="$(sed_replacement_escape "$(xml_escape "$DISK_PATHS")")"
  lan_url_xml="$(sed_replacement_escape "$(xml_escape "${NEW_API_RECOVERY_LAN_URL:-}")")"
  label_xml="$(sed_replacement_escape "$(xml_escape "$LABEL")")"
  caddy_label_xml="$(sed_replacement_escape "$(xml_escape "$CADDY_LABEL")")"

  sed \
    -e "s|__MONITOR_PATH__|${monitor_xml}|g" \
    -e "s|__WATCHDOG_PATH__|${watchdog_xml}|g" \
    -e "s|__RECOVERY_ENV_FILE__|${env_xml}|g" \
    -e "s|__DOCKER_PATH__|${docker_xml}|g" \
    -e "s|__LAUNCHCTL_PATH__|${launchctl_xml}|g" \
    -e "s|__CURL_PATH__|${curl_xml}|g" \
    -e "s|__LAN_HEALTH_URL__|${lan_url_xml}|g" \
    -e "s|__CADDY_PLIST_PATH__|${caddy_plist_xml}|g" \
    -e "s|__CADDY_LOG_FILE__|${caddy_log_xml}|g" \
    -e "s|__RECOVERY_LOG_FILE__|${recovery_log_xml}|g" \
    -e "s|__STATUS_PATH__|${status_xml}|g" \
    -e "s|__LOCK_DIR__|${lock_xml}|g" \
    -e "s|__LOG_PATH__|${log_xml}|g" \
    -e "s|__LOG_ROTATION_MAX_BYTES__|${log_rotation_max_bytes_xml}|g" \
    -e "s|__LOG_ROTATION_KEEP__|${log_rotation_keep_xml}|g" \
    -e "s|__APP_LOG_DIR__|${app_log_dir_xml}|g" \
    -e "s|__APP_LOG_MAX_BYTES__|${app_log_max_bytes_xml}|g" \
    -e "s|__APP_LOG_WARN_BYTES__|${app_log_warn_bytes_xml}|g" \
    -e "s|__PROBE_SNAPSHOT_MAX_AGE_SECONDS__|${probe_snapshot_max_age_seconds_xml}|g" \
    -e "s|__DISK_PATHS__|${disk_paths_xml}|g" \
    -e "s|__MONITOR_LABEL__|${label_xml}|g" \
    -e "s|__CADDY_LABEL__|${caddy_label_xml}|g" \
    "$TEMPLATE_PATH" >"$destination"

  if rg -n '__[A-Z_]+__' "$destination" >/dev/null 2>&1; then
    fail "monitor-template-placeholder-remains"
  fi
}

validate_plist() {
  local path="$1"
  local value
  "$PLUTIL_BIN" -lint "$path" >/dev/null || fail "monitor-plist-invalid"
  rg -Fq '<integer>300</integer>' "$path" || fail "monitor-interval-invalid"
  rg -Fq '<key>NEW_API_RECOVERY_DRY_RUN</key>' "$path" || fail "monitor-dry-run-missing"
  rg -Fq '<key>NEW_API_RECOVERY_MONITOR_ONLY</key>' "$path" || fail "monitor-only-missing"
  rg -Fq '<key>NEW_API_RECOVERY_FORCE_DOCKER_RESTART</key>' "$path" || fail "force-restart-gate-missing"
  rg -A1 -Fq '<key>NEW_API_MONITOR_STATUS_FILE</key>' "$path" || fail "status-path-missing"
  value="$($PLUTIL_BIN -extract EnvironmentVariables.NEW_API_RECOVERY_DRY_RUN raw "$path" 2>/dev/null || true)"
  [ "$value" = "1" ] || fail "monitor-dry-run-value-invalid"
  value="$($PLUTIL_BIN -extract EnvironmentVariables.NEW_API_RECOVERY_MONITOR_ONLY raw "$path" 2>/dev/null || true)"
  [ "$value" = "1" ] || fail "monitor-only-value-invalid"
  value="$($PLUTIL_BIN -extract EnvironmentVariables.NEW_API_RECOVERY_FORCE_DOCKER_RESTART raw "$path" 2>/dev/null || true)"
  [ "$value" = "0" ] || fail "force-restart-value-invalid"
  value="$($PLUTIL_BIN -extract EnvironmentVariables.NEW_API_MONITOR_LOG_FILE raw "$path" 2>/dev/null || true)"
  [ "$value" = "$LOG_PATH" ] || fail "monitor-log-path-invalid"
  value="$($PLUTIL_BIN -extract EnvironmentVariables.NEW_API_MONITOR_LOG_ROTATION_MAX_BYTES raw "$path" 2>/dev/null || true)"
  [ "$value" = "$MONITOR_LOG_ROTATION_MAX_BYTES" ] || fail "monitor-log-rotation-max-invalid"
  value="$($PLUTIL_BIN -extract EnvironmentVariables.NEW_API_MONITOR_LOG_ROTATION_KEEP raw "$path" 2>/dev/null || true)"
  [ "$value" = "$MONITOR_LOG_ROTATION_KEEP" ] || fail "monitor-log-rotation-keep-invalid"
  value="$($PLUTIL_BIN -extract EnvironmentVariables.NEW_API_MONITOR_APP_LOG_DIR raw "$path" 2>/dev/null || true)"
  [ "$value" = "$APP_LOG_DIR" ] || fail "monitor-app-log-dir-invalid"
  value="$($PLUTIL_BIN -extract EnvironmentVariables.NEW_API_MONITOR_APP_LOG_MAX_BYTES raw "$path" 2>/dev/null || true)"
  [ "$value" = "$APP_LOG_MAX_BYTES" ] || fail "monitor-app-log-max-invalid"
  value="$($PLUTIL_BIN -extract EnvironmentVariables.NEW_API_MONITOR_APP_LOG_WARN_BYTES raw "$path" 2>/dev/null || true)"
  [ "$value" = "$APP_LOG_WARN_BYTES" ] || fail "monitor-app-log-warn-invalid"
  value="$($PLUTIL_BIN -extract EnvironmentVariables.NEW_API_MONITOR_PROBE_SNAPSHOT_MAX_AGE_SECONDS raw "$path" 2>/dev/null || true)"
  [ "$value" = "$PROBE_SNAPSHOT_MAX_AGE_SECONDS" ] || fail "monitor-probe-snapshot-max-age-invalid"
  value="$($PLUTIL_BIN -extract EnvironmentVariables.NEW_API_MONITOR_DISK_PATHS raw "$path" 2>/dev/null || true)"
  [ "$value" = "$DISK_PATHS" ] || fail "monitor-disk-paths-invalid"
  value="$($PLUTIL_BIN -extract StartInterval raw "$path" 2>/dev/null || true)"
  [ "$value" = "300" ] || fail "monitor-interval-value-invalid"
}

wait_for_service_absent() {
  local attempt
  for attempt in 1 2 3; do
    if ! "$LAUNCHCTL_BIN" print "$SERVICE_TARGET" >/dev/null 2>&1; then
      return 0
    fi
    if [ "$attempt" -lt 3 ]; then
      sleep "$BOOTSTRAP_RETRY_DELAY_SECONDS"
    fi
  done
  return 1
}

bootstrap_service() {
  local attempt
  for attempt in 1 2 3; do
    if "$LAUNCHCTL_BIN" bootstrap "$DOMAIN" "$PLIST_PATH" && \
       "$LAUNCHCTL_BIN" print "$SERVICE_TARGET" >/dev/null 2>&1; then
      return 0
    fi
    if "$LAUNCHCTL_BIN" print "$SERVICE_TARGET" >/dev/null 2>&1; then
      return 0
    fi
    if [ "$attempt" -lt 3 ]; then
      sleep "$BOOTSTRAP_RETRY_DELAY_SECONDS"
    fi
  done
  return 1
}

backup_existing() {
  local backup_dir="$1"
  local stamp="$2"
  previous_script_path=""
  previous_plist_path=""
  if [ -f "$MONITOR_SCRIPT_PATH" ]; then
    previous_script_path="$backup_dir/monitor.sh.before-install.${stamp}"
    cp -p "$MONITOR_SCRIPT_PATH" "$previous_script_path"
    chmod 0600 "$previous_script_path"
  fi
  if [ -f "$PLIST_PATH" ]; then
    previous_plist_path="$backup_dir/monitor.plist.before-install.${stamp}"
    cp -p "$PLIST_PATH" "$previous_plist_path"
    chmod 0600 "$previous_plist_path"
  fi
}

restore_previous_install() {
  local restored_service=0
  "$LAUNCHCTL_BIN" bootout "$SERVICE_TARGET" >/dev/null 2>&1 || true
  wait_for_service_absent || true
  if [ -n "${previous_script_path:-}" ] && [ -f "$previous_script_path" ]; then
    cp -p "$previous_script_path" "$MONITOR_SCRIPT_PATH"
    chmod 0755 "$MONITOR_SCRIPT_PATH"
  else
    rm -f "$MONITOR_SCRIPT_PATH"
  fi
  if [ -n "${previous_plist_path:-}" ] && [ -f "$previous_plist_path" ]; then
    cp -p "$previous_plist_path" "$PLIST_PATH"
    chmod 0600 "$PLIST_PATH"
    restored_service=1
  else
    rm -f "$PLIST_PATH"
  fi
  if [ "$restored_service" = "1" ]; then
    bootstrap_service || true
  fi
}

check_inputs

if [ "$MODE" = "check-only" ]; then
  temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/new-api-monitor-plist.XXXXXX")"
  trap 'rm -rf "$temporary_dir"' EXIT
  temporary_plist="$temporary_dir/monitor.plist"
  render_plist "$temporary_plist"
  validate_plist "$temporary_plist"
  printf 'monitor_install_status=check-only label=%s interval_seconds=300\n' "$LABEL"
  exit 0
fi

mkdir -p "$INSTALL_DIR" "$(dirname "$PLIST_PATH")"
chmod 0700 "$INSTALL_DIR"
temporary_dir="$(mktemp -d "$INSTALL_DIR/.monitor-install.XXXXXX")"
temporary_plist="$temporary_dir/monitor.plist"
temporary_script="$temporary_dir/monitor.sh"
trap 'rm -rf "$temporary_dir"' EXIT
install -m 0755 "$SOURCE_MONITOR_PATH" "$temporary_script"
render_plist "$temporary_plist"
validate_plist "$temporary_plist"
chmod 0600 "$temporary_plist"

backup_dir="$INSTALL_DIR/backups"
mkdir -p "$backup_dir"
chmod 0700 "$backup_dir"
backup_stamp="$(date '+%Y%m%d-%H%M%S')"
backup_existing "$backup_dir" "$backup_stamp"
touch "$LOG_PATH" "$STATUS_PATH"
chmod 0600 "$LOG_PATH" "$STATUS_PATH"
mv -f "$temporary_script" "$MONITOR_SCRIPT_PATH"
if ! mv -f "$temporary_plist" "$PLIST_PATH"; then
  restore_previous_install
  fail "monitor-plist-replace-failed"
fi
chmod 0600 "$PLIST_PATH"

"$LAUNCHCTL_BIN" bootout "$SERVICE_TARGET" >/dev/null 2>&1 || true
if ! wait_for_service_absent; then
  restore_previous_install
  fail "monitor-launchagent-did-not-stop"
fi
if ! bootstrap_service; then
  restore_previous_install
  fail "monitor-launchagent-bootstrap-failed"
fi

kickstart_ok=0
for attempt in 1 2 3; do
  if "$LAUNCHCTL_BIN" kickstart -k "$SERVICE_TARGET" && \
     "$LAUNCHCTL_BIN" print "$SERVICE_TARGET" >/dev/null 2>&1; then
    kickstart_ok=1
    break
  fi
  if [ "$attempt" -lt 3 ]; then
    sleep "$BOOTSTRAP_RETRY_DELAY_SECONDS"
  fi
done
if [ "$kickstart_ok" != "1" ]; then
  restore_previous_install
  fail "monitor-launchagent-kickstart-failed"
fi

printf 'monitor_install_status=applied label=%s plist=%s interval_seconds=300\n' "$LABEL" "$PLIST_PATH"
