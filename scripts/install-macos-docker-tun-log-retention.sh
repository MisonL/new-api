#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LABEL="${NEW_API_LOG_RETENTION_LAUNCHD_LABEL:-com.new-api.macos-tun.new-api.log-retention}"
DOMAIN="gui/$(id -u)"
SERVICE_TARGET="${DOMAIN}/${LABEL}"
APP_SUPPORT_DIR="${HOME}/Library/Application Support/New API"
INSTALL_DIR="${NEW_API_LOG_RETENTION_INSTALL_DIR:-${APP_SUPPORT_DIR}/log-retention}"
SCRIPT_PATH="${NEW_API_LOG_RETENTION_SCRIPT_PATH:-${INSTALL_DIR}/retention.sh}"
RUNNER_PATH="${NEW_API_LOG_RETENTION_RUNNER_PATH:-${HOME}/.local/bin/run-cleanup-task}"
LOCK_PATH="${NEW_API_LOG_RETENTION_LOCK_PATH:-${INSTALL_DIR}/retention.lock}"
PLIST_PATH="${NEW_API_LOG_RETENTION_LAUNCHD_PLIST:-${HOME}/Library/LaunchAgents/${LABEL}.plist}"
LOG_PATH="${NEW_API_LOG_RETENTION_LOG_PATH:-${INSTALL_DIR}/retention.log}"
LOG_KEEP_FILES="${NEW_API_LOG_RETENTION_LOG_KEEP_FILES:-4}"
TIMEOUT_SECONDS="${NEW_API_LOG_RETENTION_TIMEOUT_SECONDS:-900}"
STATE_PATH="${NEW_API_LOG_RETENTION_STATE_FILE:-${INSTALL_DIR}/last-run}"
LOG_DIR="${NEW_API_LOG_RETENTION_DIR:-${ROOT_DIR}/logs}"
RETENTION_DAYS="${NEW_API_LOG_RETENTION_DAYS:-60}"
KEEP_FILES="${NEW_API_LOG_RETENTION_KEEP_FILES:-4}"
MIN_INTERVAL_SECONDS="${NEW_API_LOG_RETENTION_MIN_INTERVAL_SECONDS:-86400}"
OPEN_PATH_CHECKER="${NEW_API_LOG_RETENTION_OPEN_PATH_CHECKER:-}"
VOLUME_PATH="${NEW_API_LOG_RETENTION_VOLUME_PATH:-}"
VOLUME_UUID="${NEW_API_LOG_RETENTION_VOLUME_UUID:-}"
DISKUTIL_BIN="${NEW_API_LOG_RETENTION_DISKUTIL_BIN:-/usr/sbin/diskutil}"
TEMPLATE_PATH="${NEW_API_LOG_RETENTION_TEMPLATE_PATH:-${ROOT_DIR}/deploy/macos/com.new-api.macos-tun.new-api.log-retention.plist.example}"
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

fail() {
  printf 'log_retention_install_status=fail reason=%s\n' "$1" >&2
  exit 1
}

case "$BOOTSTRAP_RETRY_DELAY_SECONDS" in
  ''|*[!0-9.]*) BOOTSTRAP_RETRY_DELAY_SECONDS=1 ;;
esac

for numeric_value in "$RETENTION_DAYS" "$KEEP_FILES" "$MIN_INTERVAL_SECONDS" "$LOG_KEEP_FILES" "$TIMEOUT_SECONDS"; do
  case "$numeric_value" in
    ''|*[!0-9]*) fail invalid-numeric-config ;;
  esac
done
[ "$KEEP_FILES" -gt 0 ] || fail invalid-keep-files
[ "$LOG_KEEP_FILES" -gt 0 ] || fail invalid-log-keep-files
[ "$TIMEOUT_SECONDS" -gt 0 ] || fail invalid-timeout
case "$RUNNER_PATH:$LOCK_PATH:$LOG_PATH" in
  /Users/*:/Users/*:/Users/*) ;;
  *) fail unexpected-user-path ;;
esac
# Retention must never be installed without both safety guards. Empty values
# are rejected instead of producing a weaker LaunchAgent on re-install.
[ -n "$OPEN_PATH_CHECKER" ] || fail open-file-checker-required
[ -x "$OPEN_PATH_CHECKER" ] || fail open-file-checker-unavailable
[ -n "$VOLUME_PATH" ] || fail volume-path-required
[ -d "$VOLUME_PATH" ] || fail volume-path-unavailable
[ ! -L "$VOLUME_PATH" ] || fail volume-path-symlink
[ "$VOLUME_PATH" != "/" ] || fail volume-path-invalid
case "$VOLUME_PATH" in
  /Volumes/*) ;;
  *) fail unexpected-volume-path ;;
esac
[ -x "$DISKUTIL_BIN" ] || fail diskutil-unavailable
[ -n "$PLUTIL_BIN" ] && [ -x "$PLUTIL_BIN" ] || fail plutil-unavailable
mount_table="$(/sbin/mount 2>/dev/null || true)"
printf '%s\n' "$mount_table" | grep -Fq " on $VOLUME_PATH (" || fail volume-not-mounted
if [ -z "$VOLUME_UUID" ]; then
  VOLUME_UUID="$($DISKUTIL_BIN info -plist "$VOLUME_PATH" 2>/dev/null | "$PLUTIL_BIN" -extract VolumeUUID raw -o - - 2>/dev/null || true)"
fi
[ -n "$VOLUME_UUID" ] || fail volume-uuid-unavailable
ACTUAL_VOLUME_UUID="$($DISKUTIL_BIN info -plist "$VOLUME_PATH" 2>/dev/null | "$PLUTIL_BIN" -extract VolumeUUID raw -o - - 2>/dev/null || true)"
[ -n "$ACTUAL_VOLUME_UUID" ] || fail volume-uuid-unavailable
[ "$VOLUME_UUID" = "$ACTUAL_VOLUME_UUID" ] || fail volume-uuid-mismatch

[ -f "$TEMPLATE_PATH" ] || fail template-missing
[ -x "$SOURCE_SCRIPT_PATH" ] || fail source-script-unavailable
[ -x "$RUNNER_PATH" ] || fail cleanup-runner-unavailable
[ -n "$LAUNCHCTL_BIN" ] && [ -x "$LAUNCHCTL_BIN" ] || fail launchctl-unavailable
[ -d "$LOG_DIR" ] || fail log-dir-missing
case "$LOG_DIR" in
  "$VOLUME_PATH"/*) ;;
  *) fail log-dir-outside-guarded-volume ;;
esac

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

render_plist() {
  local destination="$1"
  sed \
    -e "s|__LOG_RETENTION_LABEL__|$(sed_replacement_escape "$(xml_escape "$LABEL")")|g" \
    -e "s|__LOG_RETENTION_SCRIPT__|$(sed_replacement_escape "$(xml_escape "$SCRIPT_PATH")")|g" \
    -e "s|__LOG_RETENTION_RUNNER__|$(sed_replacement_escape "$(xml_escape "$RUNNER_PATH")")|g" \
    -e "s|__LOG_RETENTION_LOCK__|$(sed_replacement_escape "$(xml_escape "$LOCK_PATH")")|g" \
    -e "s|__LOG_RETENTION_DIR__|$(sed_replacement_escape "$(xml_escape "$LOG_DIR")")|g" \
    -e "s|__LOG_RETENTION_STATE__|$(sed_replacement_escape "$(xml_escape "$STATE_PATH")")|g" \
    -e "s|__LOG_RETENTION_DAYS__|$(sed_replacement_escape "$(xml_escape "$RETENTION_DAYS")")|g" \
    -e "s|__LOG_RETENTION_KEEP_FILES__|$(sed_replacement_escape "$(xml_escape "$KEEP_FILES")")|g" \
    -e "s|__LOG_RETENTION_MIN_INTERVAL__|$(sed_replacement_escape "$(xml_escape "$MIN_INTERVAL_SECONDS")")|g" \
    -e "s|__LOG_RETENTION_OPEN_PATH_CHECKER__|$(sed_replacement_escape "$(xml_escape "$OPEN_PATH_CHECKER")")|g" \
    -e "s|__LOG_RETENTION_VOLUME_PATH__|$(sed_replacement_escape "$(xml_escape "$VOLUME_PATH")")|g" \
    -e "s|__LOG_RETENTION_VOLUME_UUID__|$(sed_replacement_escape "$(xml_escape "$VOLUME_UUID")")|g" \
    -e "s|__LOG_RETENTION_LOG_KEEP__|$(sed_replacement_escape "$(xml_escape "$LOG_KEEP_FILES")")|g" \
    -e "s|__LOG_RETENTION_TIMEOUT__|$(sed_replacement_escape "$(xml_escape "$TIMEOUT_SECONDS")")|g" \
    -e "s|__LOG_RETENTION_LOG__|$(sed_replacement_escape "$(xml_escape "$LOG_PATH")")|g" \
    "$TEMPLATE_PATH" >"$destination"
}

validate_plist() {
  local path="$1"
  "$PLUTIL_BIN" -lint "$path" >/dev/null || fail plist-invalid
  if rg -n '__LOG_RETENTION_[A-Z_]+__' "$path" >/dev/null 2>&1; then
    fail plist-placeholder-remains
  fi
  rg -A2 -Fq '<key>StartCalendarInterval</key>' "$path" || fail interval-invalid
  rg -Fq '<false/>' "$path" || fail run-at-load-invalid
  rg -Fq -- '--lock' "$path" || fail lock-argument-missing
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

restore_previous_files() {
  if [ -n "${previous_script_path:-}" ] && [ -f "$previous_script_path" ]; then
    cp -p "$previous_script_path" "$SCRIPT_PATH"
    chmod 0755 "$SCRIPT_PATH"
  else
    rm -f "$SCRIPT_PATH"
  fi
  if [ -n "${previous_plist_path:-}" ] && [ -f "$previous_plist_path" ]; then
    cp -p "$previous_plist_path" "$PLIST_PATH"
    chmod 0600 "$PLIST_PATH"
  else
    rm -f "$PLIST_PATH"
  fi
}

restore_previous_install() {
  local restored_service=0
  restore_previous_files
  if [ -n "${previous_plist_path:-}" ] && [ -f "$previous_plist_path" ]; then
    restored_service=1
  fi
  "$LAUNCHCTL_BIN" bootout "$SERVICE_TARGET" >/dev/null 2>&1 || true
  wait_for_service_absent || true
  if [ "$restored_service" = "1" ]; then
    bootstrap_service || true
  fi
}

if [ "$MODE" = "check-only" ]; then
  temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/new-api-log-retention-plist.XXXXXX")"
  temporary_plist="$temporary_dir/retention.plist"
  trap 'rm -rf "$temporary_dir"' EXIT
  render_plist "$temporary_plist"
  validate_plist "$temporary_plist"
  printf 'log_retention_install_status=check-only label=%s schedule=08:00 retention_days=%s keep_files=%s\n' "$LABEL" "$RETENTION_DAYS" "$KEEP_FILES"
  exit 0
fi

mkdir -p "$INSTALL_DIR" "$(dirname "$PLIST_PATH")"
chmod 0700 "$INSTALL_DIR"
temporary_dir="$(mktemp -d "$INSTALL_DIR/.retention-install.XXXXXX")"
temporary_plist="$temporary_dir/retention.plist"
temporary_script="$temporary_dir/retention.sh"
trap 'rm -rf "$temporary_dir"' EXIT
install -m 0755 "$SOURCE_SCRIPT_PATH" "$temporary_script"
render_plist "$temporary_plist"
validate_plist "$temporary_plist"
chmod 0600 "$temporary_plist"

backup_dir="$INSTALL_DIR/backups"
mkdir -p "$backup_dir"
chmod 0700 "$backup_dir"
backup_stamp="$(date '+%Y%m%d-%H%M%S')"
previous_script_path=""
previous_plist_path=""
if [ -f "$SCRIPT_PATH" ]; then
  previous_script_path="$backup_dir/retention.sh.before-install.${backup_stamp}"
  cp -p "$SCRIPT_PATH" "$previous_script_path"
  chmod 0600 "$previous_script_path"
fi
if [ -f "$PLIST_PATH" ]; then
  previous_plist_path="$backup_dir/retention.plist.before-install.${backup_stamp}"
  cp -p "$PLIST_PATH" "$previous_plist_path"
  chmod 0600 "$previous_plist_path"
fi
touch "$LOG_PATH"
chmod 0600 "$LOG_PATH"
mv -f "$temporary_script" "$SCRIPT_PATH"
if ! mv -f "$temporary_plist" "$PLIST_PATH"; then
  restore_previous_install
  fail plist-replace-failed
fi
chmod 0600 "$PLIST_PATH"

"$LAUNCHCTL_BIN" bootout "$SERVICE_TARGET" >/dev/null 2>&1 || true
if ! wait_for_service_absent; then
  restore_previous_install
  fail launchagent-did-not-stop
fi
if ! bootstrap_service; then
  restore_previous_install
  fail launchagent-bootstrap-failed
fi

printf 'log_retention_install_status=applied label=%s plist=%s schedule=08:00 retention_days=%s keep_files=%s\n' "$LABEL" "$PLIST_PATH" "$RETENTION_DAYS" "$KEEP_FILES"
