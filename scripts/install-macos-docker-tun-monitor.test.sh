#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALLER="$ROOT_DIR/scripts/install-macos-docker-tun-monitor.sh"
FAKE_LAUNCHCTL="$ROOT_DIR/scripts/test-fixtures/fake-monitor-launchctl.sh"
TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/new-api-monitor-installer-test.XXXXXX")"
trap 'rm -rf "$TMP_DIR"' EXIT

FAKE_DOCKER="$TMP_DIR/docker"
FAKE_CURL="$TMP_DIR/curl"
FAKE_WATCHDOG="$TMP_DIR/watchdog.sh"
ENV_FILE="$TMP_DIR/.env"
INSTALL_DIR="$TMP_DIR/monitor"
PLIST_PATH="$TMP_DIR/LaunchAgents/monitor.plist"
LAUNCHCTL_LOG="$TMP_DIR/launchctl.log"
LAUNCHCTL_STATE="$TMP_DIR/launchctl.state"

for path in "$FAKE_DOCKER" "$FAKE_CURL"; do
  printf '#!/usr/bin/env bash\nexit 0\n' >"$path"
  chmod 0755 "$path"
done
cp "$ROOT_DIR/scripts/macos-docker-tun-recovery-watchdog.sh" "$FAKE_WATCHDOG"
chmod 0755 "$FAKE_WATCHDOG"
printf 'POSTGRES_PASSWORD=installer-test-postgres\nREDIS_PASSWORD=installer-test-redis\n' >"$ENV_FILE"
chmod 0600 "$ENV_FILE"
: >"$LAUNCHCTL_LOG"

run_installer() {
  local caddy_plist="${TEST_MONITOR_CADDY_PLIST_PATH:-$TMP_DIR/caddy.plist}"
  if [ "${TEST_MONITOR_CADDY_PLIST_UNSET:-0}" = "1" ]; then
    caddy_plist=""
  fi
  env \
    NEW_API_MONITOR_INSTALL_DIR="$INSTALL_DIR" \
    NEW_API_MONITOR_LAUNCHD_PLIST="$PLIST_PATH" \
    NEW_API_MONITOR_TEMPLATE_PATH="${TEST_MONITOR_TEMPLATE_PATH:-$ROOT_DIR/deploy/macos/com.new-api.macos-tun.new-api.monitor.plist.example}" \
    NEW_API_MONITOR_LAUNCHCTL_BIN="$FAKE_LAUNCHCTL" \
    NEW_API_MONITOR_DOCKER_BIN="$FAKE_DOCKER" \
    NEW_API_MONITOR_CURL_BIN="$FAKE_CURL" \
    NEW_API_MONITOR_WATCHDOG_PATH="$FAKE_WATCHDOG" \
    NEW_API_MONITOR_ENV_FILE="${TEST_MONITOR_ENV_FILE:-$ENV_FILE}" \
    NEW_API_MONITOR_LOG_PATH="${TEST_MONITOR_LOG_PATH:-$INSTALL_DIR/monitor.log}" \
    NEW_API_MONITOR_STATUS_FILE="${TEST_MONITOR_STATUS_PATH:-$INSTALL_DIR/monitor.status}" \
    NEW_API_MONITOR_LOCK_DIR="$INSTALL_DIR/monitor.lock" \
    NEW_API_MONITOR_DISK_PATHS="/:$TMP_DIR/data-volume" \
    NEW_API_RECOVERY_LAN_URL="http://lan.example.test:3000/api/healthz" \
    NEW_API_RECOVERY_CADDY_PLIST="$caddy_plist" \
    NEW_API_RECOVERY_CADDY_LOG_FILE="${TEST_MONITOR_CADDY_LOG_PATH:-$TMP_DIR/caddy.log}" \
    NEW_API_RECOVERY_LOG_FILE="${TEST_MONITOR_RECOVERY_LOG_PATH:-$TMP_DIR/recovery.log}" \
    FAKE_MONITOR_LAUNCHCTL_LOG="$LAUNCHCTL_LOG" \
    FAKE_MONITOR_LAUNCHCTL_STATE="$LAUNCHCTL_STATE" \
    "$INSTALLER" "$@"
}

check_output="$TMP_DIR/check.stdout"
check_error="$TMP_DIR/check.stderr"
run_installer --check-only >"$check_output" 2>"$check_error"
rg -Fq 'monitor_install_status=check-only' "$check_output"
if [ -e "$INSTALL_DIR" ] || [ -e "$PLIST_PATH" ] || [ -e "$LAUNCHCTL_STATE" ]; then
  printf 'assertion failed: check-only wrote an install artifact\n' >&2
  exit 1
fi
if rg -Fq 'installer-test-postgres' "$check_output" "$check_error" || \
   rg -Fq 'installer-test-redis' "$check_output" "$check_error"; then
  printf 'assertion failed: check-only leaked credentials\n' >&2
  exit 1
fi

apply_output="$TMP_DIR/apply.stdout"
run_installer --apply >"$apply_output" 2>"$TMP_DIR/apply.stderr"
rg -Fq 'monitor_install_status=applied' "$apply_output"
[ -x "$INSTALL_DIR/monitor.sh" ]
[ -f "$PLIST_PATH" ]
[ -f "$INSTALL_DIR/monitor.log" ]
[ -f "$INSTALL_DIR/monitor.status" ]
[ "$(stat -f '%Lp' "$INSTALL_DIR")" = "700" ]
[ "$(stat -f '%Lp' "$INSTALL_DIR/monitor.log")" = "600" ]
[ "$(stat -f '%Lp' "$INSTALL_DIR/monitor.status")" = "600" ]
[ "$(stat -f '%Lp' "$PLIST_PATH")" = "600" ]
/usr/bin/plutil -lint "$PLIST_PATH" >/dev/null
rg -Fq '<integer>300</integer>' "$PLIST_PATH"
rg -A1 -Fq '<key>NEW_API_RECOVERY_FORCE_DOCKER_RESTART</key>' "$PLIST_PATH"
rg -Fq '<string>1</string>' "$PLIST_PATH"
rg -A1 -Fq '<key>NEW_API_MONITOR_APP_LOG_DIR</key>' "$PLIST_PATH"
rg -A1 -Fq '<key>NEW_API_MONITOR_APP_LOG_MAX_BYTES</key>' "$PLIST_PATH"
rg -A1 -Fq '<key>NEW_API_MONITOR_APP_LOG_WARN_BYTES</key>' "$PLIST_PATH"
rg -A1 -Fq '<key>NEW_API_MONITOR_PROBE_SNAPSHOT_MAX_AGE_SECONDS</key>' "$PLIST_PATH"
rg -Fq 'http://lan.example.test:3000/api/healthz' "$PLIST_PATH"
rg -Fq "$TMP_DIR/data-volume" "$PLIST_PATH"
domain="gui/$(id -u)"
rg -Fq "bootstrap ${domain}" "$LAUNCHCTL_LOG"
rg -Fq "kickstart -k ${domain}/com.new-api.macos-tun.new-api.monitor" "$LAUNCHCTL_LOG"
if rg -Fq 'installer-test-postgres' "$apply_output" "$TMP_DIR/apply.stderr" || \
   rg -Fq 'installer-test-redis' "$apply_output" "$TMP_DIR/apply.stderr"; then
	printf 'assertion failed: apply leaked credentials\n' >&2
	exit 1
fi

custom_env_file="$TMP_DIR/custom.env"
custom_caddy_plist="$TMP_DIR/custom-caddy.plist"
printf '%s\n' \
  'POSTGRES_PASSWORD=installer-test-postgres' \
  'REDIS_PASSWORD=installer-test-redis' \
  'CADDY_LAUNCHD_LABEL=com.example.custom-caddy' \
  "CADDY_LAUNCHD_PLIST=$custom_caddy_plist" >"$custom_env_file"
TEST_MONITOR_ENV_FILE="$custom_env_file" \
TEST_MONITOR_CADDY_PLIST_UNSET=1 \
  run_installer --apply >"$TMP_DIR/custom.stdout" 2>"$TMP_DIR/custom.stderr"
[ "$(/usr/bin/plutil -extract EnvironmentVariables.NEW_API_RECOVERY_CADDY_LABEL raw "$PLIST_PATH")" = "com.example.custom-caddy" ]
[ "$(/usr/bin/plutil -extract EnvironmentVariables.NEW_API_RECOVERY_CADDY_PLIST raw "$PLIST_PATH")" = "$custom_caddy_plist" ]

literal_tilde_env_file="$TMP_DIR/literal-tilde.env"
literal_tilde_home="$TMP_DIR/literal-tilde-home"
printf '%s\n' \
  'POSTGRES_PASSWORD=installer-test-postgres' \
  'REDIS_PASSWORD=installer-test-redis' \
  'CADDY_LAUNCHD_PLIST=~/Library/LaunchAgents/literal-caddy.plist' >"$literal_tilde_env_file"
HOME="$literal_tilde_home" \
TEST_MONITOR_ENV_FILE="$literal_tilde_env_file" \
TEST_MONITOR_CADDY_PLIST_UNSET=1 \
  run_installer --apply >"$TMP_DIR/literal-tilde.stdout" 2>"$TMP_DIR/literal-tilde.stderr"
[ "$(/usr/bin/plutil -extract EnvironmentVariables.NEW_API_RECOVERY_CADDY_LABEL raw "$PLIST_PATH")" = "literal-caddy" ]
[ "$(/usr/bin/plutil -extract EnvironmentVariables.NEW_API_RECOVERY_CADDY_PLIST raw "$PLIST_PATH")" = "$literal_tilde_home/Library/LaunchAgents/literal-caddy.plist" ]

special_log_path="$INSTALL_DIR/log&|\\special.log"
special_status_path="$INSTALL_DIR/status&|\\special.status"
special_caddy_plist_path="$TMP_DIR/caddy&|\\special.plist"
special_caddy_log_path="$TMP_DIR/caddy&|\\special.log"
TEST_MONITOR_LOG_PATH="$special_log_path" \
TEST_MONITOR_STATUS_PATH="$special_status_path" \
TEST_MONITOR_CADDY_PLIST_PATH="$special_caddy_plist_path" \
TEST_MONITOR_CADDY_LOG_PATH="$special_caddy_log_path" \
  run_installer --apply >"$TMP_DIR/special.stdout" 2>"$TMP_DIR/special.stderr"
[ "$(/usr/bin/plutil -extract StandardOutPath raw "$PLIST_PATH")" = "$special_log_path" ]
[ "$(/usr/bin/plutil -extract EnvironmentVariables.NEW_API_MONITOR_STATUS_FILE raw "$PLIST_PATH")" = "$special_status_path" ]
[ "$(/usr/bin/plutil -extract EnvironmentVariables.NEW_API_RECOVERY_CADDY_PLIST raw "$PLIST_PATH")" = "$special_caddy_plist_path" ]
[ "$(/usr/bin/plutil -extract EnvironmentVariables.NEW_API_RECOVERY_CADDY_LOG_FILE raw "$PLIST_PATH")" = "$special_caddy_log_path" ]

cp "$INSTALL_DIR/monitor.sh" "$TMP_DIR/monitor.before-render-failure"
cp "$PLIST_PATH" "$TMP_DIR/monitor.before-render-failure.plist"
printf '<plist><dict><string>__INVALID_TEMPLATE__</string></dict></plist>\n' >"$TMP_DIR/invalid-monitor.plist"
set +e
TEST_MONITOR_TEMPLATE_PATH="$TMP_DIR/invalid-monitor.plist" \
  run_installer --apply >"$TMP_DIR/render-failure.stdout" 2>"$TMP_DIR/render-failure.stderr"
render_failure_status=$?
set -e
[ "$render_failure_status" -ne 0 ]
cmp -s "$TMP_DIR/monitor.before-render-failure" "$INSTALL_DIR/monitor.sh"
cmp -s "$TMP_DIR/monitor.before-render-failure.plist" "$PLIST_PATH"

printf '#!/usr/bin/env bash\nexit 0\n' >"$FAKE_WATCHDOG"
chmod 0755 "$FAKE_WATCHDOG"
set +e
run_installer --check-only >"$TMP_DIR/mismatch.stdout" 2>"$TMP_DIR/mismatch.stderr"
mismatch_status=$?
set -e
[ "$mismatch_status" -eq 1 ]
rg -Fq 'installed-watchdog-mismatch' "$TMP_DIR/mismatch.stderr"

set +e
run_installer --invalid >"$TMP_DIR/invalid.stdout" 2>"$TMP_DIR/invalid.stderr"
invalid_status=$?
set -e
[ "$invalid_status" -eq 2 ]

printf 'install-macos-docker-tun-monitor tests passed\n'
