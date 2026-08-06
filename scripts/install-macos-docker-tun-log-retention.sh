#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LABEL="${NEW_API_LOG_RETENTION_LAUNCHD_LABEL:-com.new-api.macos-tun.new-api.log-retention}"
DOMAIN="gui/$(id -u)"
SERVICE_TARGET="${DOMAIN}/${LABEL}"
APP_SUPPORT_DIR="${HOME}/Library/Application Support/New API"
INSTALL_DIR="${NEW_API_LOG_RETENTION_INSTALL_DIR:-${APP_SUPPORT_DIR}/log-retention}"
SCRIPT_PATH="${NEW_API_LOG_RETENTION_SCRIPT_PATH:-${INSTALL_DIR}/retention.sh}"
PLIST_PATH="${NEW_API_LOG_RETENTION_LAUNCHD_PLIST:-${HOME}/Library/LaunchAgents/${LABEL}.plist}"
LOG_PATH="${NEW_API_LOG_RETENTION_LOG_PATH:-${INSTALL_DIR}/retention.log}"
STATE_PATH="${NEW_API_LOG_RETENTION_STATE_FILE:-${INSTALL_DIR}/last-run}"
LOG_DIR="${NEW_API_LOG_RETENTION_DIR:-${ROOT_DIR}/logs}"
RETENTION_DAYS="${NEW_API_LOG_RETENTION_DAYS:-60}"
KEEP_FILES="${NEW_API_LOG_RETENTION_KEEP_FILES:-4}"
MIN_INTERVAL_SECONDS="${NEW_API_LOG_RETENTION_MIN_INTERVAL_SECONDS:-86400}"
TEMPLATE_PATH="${ROOT_DIR}/deploy/macos/com.new-api.macos-tun.new-api.log-retention.plist.example"
SOURCE_SCRIPT_PATH="${ROOT_DIR}/scripts/macos-docker-tun-log-retention.sh"
LAUNCHCTL_BIN="${NEW_API_LOG_RETENTION_LAUNCHCTL_BIN:-$(command -v launchctl 2>/dev/null || true)}"
PLUTIL_BIN="${NEW_API_LOG_RETENTION_PLUTIL_BIN:-$(command -v plutil 2>/dev/null || true)}"
BOOTSTRAP_RETRY_DELAY_SECONDS="${NEW_API_LOG_RETENTION_BOOTSTRAP_RETRY_DELAY_SECONDS:-1}"
MODE="check-only"

usage() {
  cat <<'EOF'
usage: install-macos-docker-tun-log-retention.sh [--check-only|--apply]

The default is --check-only. Only --apply installs and kickstarts the
low-frequency log retention LaunchAgent.
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

case "$BOOTSTRAP_RETRY_DELAY_SECONDS" in
  ''|*[!0-9.]*) BOOTSTRAP_RETRY_DELAY_SECONDS=1 ;;
esac

fail() {
  printf 'log_retention_install_status=fail reason=%s\n' "$1" >&2
  exit 1
}

[ -f "$TEMPLATE_PATH" ] || fail template-missing
[ -x "$SOURCE_SCRIPT_PATH" ] || fail source-script-unavailable
[ -n "$LAUNCHCTL_BIN" ] && [ -x "$LAUNCHCTL_BIN" ] || fail launchctl-unavailable
[ -n "$PLUTIL_BIN" ] && [ -x "$PLUTIL_BIN" ] || fail plutil-unavailable
[ -d "$LOG_DIR" ] || fail log-dir-missing

xml_escape() {
  printf '%s' "$1" \
    | sed -e 's/&/\&amp;/g' \
          -e 's/</\&lt;/g' \
          -e 's/>/\&gt;/g' \
          -e 's/"/\&quot;/g' \
          -e "s/'/\&apos;/g"
}

render_plist() {
  local destination="$1"
  sed \
    -e "s|__LOG_RETENTION_LABEL__|$(xml_escape "$LABEL")|g" \
    -e "s|__LOG_RETENTION_SCRIPT__|$(xml_escape "$SCRIPT_PATH")|g" \
    -e "s|__LOG_RETENTION_DIR__|$(xml_escape "$LOG_DIR")|g" \
    -e "s|__LOG_RETENTION_STATE__|$(xml_escape "$STATE_PATH")|g" \
    -e "s|__LOG_RETENTION_DAYS__|$(xml_escape "$RETENTION_DAYS")|g" \
    -e "s|__LOG_RETENTION_KEEP_FILES__|$(xml_escape "$KEEP_FILES")|g" \
    -e "s|__LOG_RETENTION_MIN_INTERVAL__|$(xml_escape "$MIN_INTERVAL_SECONDS")|g" \
    -e "s|__LOG_RETENTION_LOG__|$(xml_escape "$LOG_PATH")|g" \
    "$TEMPLATE_PATH" >"$destination"
}

validate_plist() {
  local path="$1"
  "$PLUTIL_BIN" -lint "$path" >/dev/null || fail plist-invalid
  if rg -n '__LOG_RETENTION_[A-Z_]+__' "$path" >/dev/null 2>&1; then
    fail plist-placeholder-remains
  fi
  rg -Fq '<integer>86400</integer>' "$path" || fail interval-invalid
  rg -A1 -Fq '<key>NEW_API_LOG_RETENTION_DAYS</key>' "$path" || fail retention-days-missing
  rg -A1 -Fq '<key>NEW_API_LOG_RETENTION_KEEP_FILES</key>' "$path" || fail keep-files-missing
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

if [ "$MODE" = "check-only" ]; then
  temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/new-api-log-retention-plist.XXXXXX")"
  temporary_plist="$temporary_dir/retention.plist"
  trap 'rm -rf "$temporary_dir"' EXIT
  render_plist "$temporary_plist"
  validate_plist "$temporary_plist"
  printf 'log_retention_install_status=check-only label=%s interval_seconds=86400 retention_days=%s keep_files=%s\n' "$LABEL" "$RETENTION_DAYS" "$KEEP_FILES"
  exit 0
fi

mkdir -p "$INSTALL_DIR" "$(dirname "$PLIST_PATH")"
chmod 0700 "$INSTALL_DIR"
backup_dir="$INSTALL_DIR/backups"
mkdir -p "$backup_dir"
chmod 0700 "$backup_dir"
backup_stamp="$(date '+%Y%m%d-%H%M%S')"
if [ -f "$SCRIPT_PATH" ]; then
  cp -p "$SCRIPT_PATH" "$backup_dir/retention.sh.before-install.${backup_stamp}"
  chmod 0600 "$backup_dir/retention.sh.before-install.${backup_stamp}"
fi
if [ -f "$PLIST_PATH" ]; then
  cp -p "$PLIST_PATH" "$backup_dir/retention.plist.before-install.${backup_stamp}"
  chmod 0600 "$backup_dir/retention.plist.before-install.${backup_stamp}"
fi

install -m 0755 "$SOURCE_SCRIPT_PATH" "$SCRIPT_PATH"
temporary_plist="$INSTALL_DIR/.retention.plist.tmp.$$"
trap 'rm -f "$temporary_plist"' EXIT
render_plist "$temporary_plist"
validate_plist "$temporary_plist"
chmod 0600 "$temporary_plist"
mv -f "$temporary_plist" "$PLIST_PATH"
chmod 0600 "$PLIST_PATH"
touch "$LOG_PATH"
chmod 0600 "$LOG_PATH"

"$LAUNCHCTL_BIN" bootout "$SERVICE_TARGET" >/dev/null 2>&1 || true
wait_for_service_absent || fail launchagent-did-not-stop
bootstrap_service || fail launchagent-bootstrap-failed

printf 'log_retention_install_status=applied label=%s plist=%s interval_seconds=86400 retention_days=%s keep_files=%s\n' "$LABEL" "$PLIST_PATH" "$RETENTION_DAYS" "$KEEP_FILES"
