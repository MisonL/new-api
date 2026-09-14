#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RECOVERY_ENV_FILE="${NEW_API_RECOVERY_ENV_FILE:-${ROOT_DIR}/.env}"

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

LABEL="${NEW_API_RECOVERY_LAUNCHD_LABEL:-com.new-api.macos-tun.new-api.recovery}"
DOMAIN="gui/$(id -u)"
SERVICE_TARGET="${DOMAIN}/${LABEL}"
INSTALL_DIR="${NEW_API_RECOVERY_INSTALL_DIR:-${HOME}/Library/Application Support/New API/recovery}"
PLIST_PATH="${NEW_API_RECOVERY_LAUNCHD_PLIST:-${HOME}/Library/LaunchAgents/${LABEL}.plist}"
WATCHDOG_PATH="${INSTALL_DIR}/watchdog.sh"
LOG_PATH="${NEW_API_RECOVERY_LOG_PATH:-${INSTALL_DIR}/recovery.log}"
CADDY_PLIST_PATH="${NEW_API_RECOVERY_CADDY_PLIST:-}"
CADDY_LOG_FILE="${NEW_API_RECOVERY_CADDY_LOG_FILE:-${HOME}/Library/Application Support/New API/caddy/new-api-macos-tun-caddy-3000.log}"
TEMPLATE_PATH="${NEW_API_RECOVERY_TEMPLATE_PATH:-${ROOT_DIR}/deploy/macos/com.new-api.macos-tun.new-api.recovery.plist.example}"
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
LAN_URL="${NEW_API_RECOVERY_LAN_URL:-}"
LAUNCHCTL_BIN="${NEW_API_RECOVERY_LAUNCHCTL_BIN:-$(command -v launchctl 2>/dev/null || true)}"
BOOTSTRAP_RETRY_DELAY_SECONDS="${NEW_API_RECOVERY_BOOTSTRAP_RETRY_DELAY_SECONDS:-1}"
MODE="apply"

usage() {
  cat <<'EOF'
usage: install-macos-docker-tun-recovery.sh [--check-only|--apply]

The default is --apply for compatibility. --check-only never writes files or
changes the recovery LaunchAgent; only --apply installs and kickstarts it.
EOF
}

case "${1:-}" in
  '')
    ;;
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
  ''|*[!0-9.]* ) BOOTSTRAP_RETRY_DELAY_SECONDS=1 ;;
esac

if [ -z "$LAUNCHCTL_BIN" ]; then
  printf 'launchctl is not available in the installing shell\n' >&2
  exit 1
fi

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

restore_previous_files() {
  if [ -n "${previous_watchdog_path:-}" ] && [ -f "$previous_watchdog_path" ]; then
    cp -p "$previous_watchdog_path" "$WATCHDOG_PATH"
    chmod 0755 "$WATCHDOG_PATH"
  else
    rm -f "$WATCHDOG_PATH"
  fi
  if [ -n "${previous_plist_path:-}" ] && [ -f "$previous_plist_path" ]; then
    cp -p "$previous_plist_path" "$PLIST_PATH"
    chmod 0600 "$PLIST_PATH"
  else
    rm -f "$PLIST_PATH"
  fi
}

restore_previous_install() {
  local had_previous_plist=0
  if [ -n "${previous_plist_path:-}" ] && [ -f "$previous_plist_path" ]; then
    had_previous_plist=1
  fi
  restore_previous_files
  "$LAUNCHCTL_BIN" bootout "$SERVICE_TARGET" >/dev/null 2>&1 || true
  wait_for_service_absent || true
  if [ "$had_previous_plist" = "1" ]; then
    bootstrap_service
  fi
}

render_plist() {
  local destination="$1"
  local script_xml
  local log_xml
  local recovery_log_xml
  local caddy_plist_xml
  local caddy_log_xml
  local recovery_env_xml
  local lan_url_xml
  local label_xml
  local caddy_label_xml
  local docker_xml
  local docker_path

  docker_path="${NEW_API_RECOVERY_DOCKER_BIN:-$(command -v docker 2>/dev/null || true)}"
  if [ -z "$docker_path" ]; then
    printf 'Docker CLI is not available in the installing shell\n' >&2
    return 1
  fi
  script_xml="$(sed_replacement_escape "$(xml_escape "$WATCHDOG_PATH")")"
  log_xml="$(sed_replacement_escape "$(xml_escape "$LOG_PATH")")"
  recovery_log_xml="$(sed_replacement_escape "$(xml_escape "$LOG_PATH")")"
  caddy_plist_xml="$(sed_replacement_escape "$(xml_escape "$CADDY_PLIST_PATH")")"
  caddy_log_xml="$(sed_replacement_escape "$(xml_escape "$CADDY_LOG_FILE")")"
  recovery_env_xml="$(sed_replacement_escape "$(xml_escape "$RECOVERY_ENV_FILE")")"
  lan_url_xml="$(sed_replacement_escape "$(xml_escape "$LAN_URL")")"
  label_xml="$(sed_replacement_escape "$(xml_escape "$LABEL")")"
  caddy_label_xml="$(sed_replacement_escape "$(xml_escape "$CADDY_LABEL")")"
  docker_xml="$(sed_replacement_escape "$(xml_escape "$docker_path")")"

  sed \
    -e "s|__WATCHDOG_PATH__|${script_xml}|g" \
    -e "s|__LOG_PATH__|${log_xml}|g" \
    -e "s|__RECOVERY_LOG_FILE__|${recovery_log_xml}|g" \
    -e "s|__CADDY_LOG_FILE__|${caddy_log_xml}|g" \
    -e "s|__CADDY_PLIST_PATH__|${caddy_plist_xml}|g" \
    -e "s|__RECOVERY_ENV_FILE__|${recovery_env_xml}|g" \
    -e "s|__LAN_HEALTH_URL__|${lan_url_xml}|g" \
    -e "s|__RECOVERY_LABEL__|${label_xml}|g" \
    -e "s|__CADDY_LABEL__|${caddy_label_xml}|g" \
    -e "s|__DOCKER_PATH__|${docker_xml}|g" \
    "$TEMPLATE_PATH" >"$destination"
}

validate_plist() {
  local path="$1"
  if ! plutil -lint "$path" >/dev/null; then
    printf 'recovery plist is invalid path=%s\n' "$path" >&2
    return 1
  fi
  if grep -E -q '__[A-Z_]+__' "$path"; then
    printf 'recovery plist still contains a template placeholder path=%s\n' "$path" >&2
    return 1
  fi
}

if [ ! -f "$TEMPLATE_PATH" ]; then
  printf 'missing LaunchAgent template: %s\n' "$TEMPLATE_PATH" >&2
  exit 1
fi

if [ "$MODE" = "check-only" ]; then
  if [ ! -x "$ROOT_DIR/scripts/macos-docker-tun-recovery-watchdog.sh" ]; then
    printf 'missing executable recovery watchdog source\n' >&2
    exit 1
  fi
  temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/new-api-recovery-plist.XXXXXX")"
  temporary_plist="$temporary_dir/recovery.plist"
  trap 'rm -f "$temporary_plist"; rmdir "$temporary_dir" 2>/dev/null || true' EXIT
  render_plist "$temporary_plist"
  validate_plist "$temporary_plist"
  printf 'recovery_install_status=check-only label=%s interval_seconds=30\n' "$LABEL"
  exit 0
fi

mkdir -p "$INSTALL_DIR" "$(dirname "$PLIST_PATH")"
chmod 0700 "$INSTALL_DIR"
temporary_dir="$(mktemp -d "$INSTALL_DIR/.recovery-install.XXXXXX")"
temporary_plist="$temporary_dir/recovery.plist"
temporary_watchdog="$temporary_dir/watchdog.sh"
trap 'rm -rf "$temporary_dir"' EXIT
install -m 0755 "$ROOT_DIR/scripts/macos-docker-tun-recovery-watchdog.sh" "$temporary_watchdog"
render_plist "$temporary_plist"
validate_plist "$temporary_plist"
chmod 0600 "$temporary_plist"

backup_dir="${INSTALL_DIR}/backups"
mkdir -p "$backup_dir"
chmod 0700 "$backup_dir"
backup_stamp="$(date '+%Y%m%d-%H%M%S')"
previous_watchdog_path=""
previous_plist_path=""
if [ -f "$WATCHDOG_PATH" ]; then
  previous_watchdog_path="$backup_dir/watchdog.sh.before-install.${backup_stamp}"
  cp -p "$WATCHDOG_PATH" "$previous_watchdog_path"
  chmod 0600 "$previous_watchdog_path"
fi
if [ -f "$PLIST_PATH" ]; then
  previous_plist_path="$backup_dir/recovery.plist.before-install.${backup_stamp}"
  cp -p "$PLIST_PATH" "$previous_plist_path"
  chmod 0600 "$previous_plist_path"
fi
touch "$LOG_PATH"
chmod 0600 "$LOG_PATH"
mv -f "$temporary_watchdog" "$WATCHDOG_PATH"
if ! mv -f "$temporary_plist" "$PLIST_PATH"; then
  restore_previous_install || true
  printf 'failed to replace recovery LaunchAgent plist label=%s\n' "$LABEL" >&2
  exit 1
fi
chmod 0600 "$PLIST_PATH"
"$LAUNCHCTL_BIN" bootout "$SERVICE_TARGET" >/dev/null 2>&1 || true
if ! wait_for_service_absent; then
  printf 'recovery LaunchAgent did not leave the domain before re-bootstrap label=%s\n' "$LABEL" >&2
  restore_previous_install || true
  exit 1
fi
if ! bootstrap_service; then
  printf 'failed to bootstrap recovery LaunchAgent label=%s\n' "$LABEL" >&2
  restore_previous_install || true
  exit 1
fi

kickstart_ok=0
for attempt in 1 2 3; do
  if "$LAUNCHCTL_BIN" kickstart -k "$SERVICE_TARGET" && \
     "$LAUNCHCTL_BIN" print "$SERVICE_TARGET" >/dev/null 2>&1; then
    kickstart_ok=1
    break
  fi
  if ! "$LAUNCHCTL_BIN" print "$SERVICE_TARGET" >/dev/null 2>&1; then
    bootstrap_service || true
  fi
  if [ "$attempt" -lt 3 ]; then
    sleep "$BOOTSTRAP_RETRY_DELAY_SECONDS"
  fi
done
if [ "$kickstart_ok" != "1" ]; then
  printf 'failed to kickstart and verify recovery LaunchAgent label=%s\n' "$LABEL" >&2
  restore_previous_install || true
  exit 1
fi

printf 'installed recovery LaunchAgent label=%s plist=%s\n' "$LABEL" "$PLIST_PATH"
