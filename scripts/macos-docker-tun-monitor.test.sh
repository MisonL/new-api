#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MONITOR="$ROOT_DIR/scripts/macos-docker-tun-monitor.sh"
FAKE_DOCKER="$ROOT_DIR/scripts/test-fixtures/fake-recovery-docker.sh"
FAKE_CURL="$ROOT_DIR/scripts/test-fixtures/fake-recovery-curl.sh"
FAKE_LAUNCHCTL="$ROOT_DIR/scripts/test-fixtures/fake-recovery-launchctl.sh"
FAKE_DF="$ROOT_DIR/scripts/test-fixtures/fake-monitor-df.sh"
FAKE_VM_SWAPUSAGE="$ROOT_DIR/scripts/test-fixtures/fake-monitor-vm-swapusage.sh"
FAKE_LOADAVG="$ROOT_DIR/scripts/test-fixtures/fake-monitor-loadavg.sh"
FAKE_STAT="$ROOT_DIR/scripts/test-fixtures/fake-monitor-stat.sh"
FAKE_DU="$ROOT_DIR/scripts/test-fixtures/fake-monitor-du.sh"
TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/new-api-monitor-test.XXXXXX")"
trap 'rm -rf "$TMP_DIR"' EXIT

run_monitor_case() {
  local state_dir="$1"
  shift
  mkdir -p "$state_dir" "$state_dir/app-logs"
  : >"$state_dir/caddy.plist"
  : >"$state_dir/caddy.log"
  : >"$state_dir/recovery.log"
  env \
    NEW_API_RECOVERY_DOCKER_BIN="$FAKE_DOCKER" \
    NEW_API_RECOVERY_CURL_BIN="$FAKE_CURL" \
    NEW_API_RECOVERY_LAUNCHCTL_BIN="$FAKE_LAUNCHCTL" \
    NEW_API_RECOVERY_STATE_FILE="$state_dir/state" \
    NEW_API_RECOVERY_DOCKER_SOCKET="$state_dir/docker.sock" \
    NEW_API_RECOVERY_CADDY_PLIST="$state_dir/caddy.plist" \
    NEW_API_RECOVERY_LAN_URL=http://lan.example.test:3000/api/healthz \
    NEW_API_RECOVERY_CADDY_LOG_FILE="$state_dir/caddy.log" \
    NEW_API_RECOVERY_LOG_FILE="$state_dir/recovery.log" \
    NEW_API_RECOVERY_CURL_CONNECT_TIMEOUT=1 \
    NEW_API_RECOVERY_CURL_MAX_TIME=1 \
    NEW_API_RECOVERY_ATTEMPTS=1 \
    NEW_API_RECOVERY_DELAY_SECONDS=0 \
    NEW_API_RECOVERY_DOCKER_COMMAND_TIMEOUT_SECONDS=0 \
    NEW_API_RECOVERY_APP_RESTART_STATUS_COMMAND_TIMEOUT_SECONDS=1 \
    NEW_API_RECOVERY_CADDY_LAUNCHCTL_COMMAND_TIMEOUT_SECONDS=1 \
    NEW_API_RECOVERY_CADDY_GRACEFUL_TIMEOUT_SECONDS=1 \
    NEW_API_RECOVERY_CADDY_GRACEFUL_POLL_INTERVAL_SECONDS=0 \
    NEW_API_MONITOR_DF_BIN="$FAKE_DF" \
    NEW_API_MONITOR_VM_SWAPUSAGE_BIN="$FAKE_VM_SWAPUSAGE" \
    NEW_API_MONITOR_LOADAVG_BIN="$FAKE_LOADAVG" \
    NEW_API_MONITOR_CPU_COUNT_BIN="$FAKE_LOADAVG" \
    NEW_API_MONITOR_STAT_BIN="$FAKE_STAT" \
    NEW_API_MONITOR_DU_BIN="$FAKE_DU" \
    NEW_API_MONITOR_APP_LOG_DIR="$state_dir/app-logs" \
    NEW_API_MONITOR_STATUS_FILE="$state_dir/monitor.snapshot" \
    NEW_API_MONITOR_PROBE_SNAPSHOT_FILE="$state_dir/probes.snapshot" \
    NEW_API_MONITOR_DISK_MAX_PERCENT=90 \
    NEW_API_MONITOR_DISK_PATHS="/:$state_dir/app-logs" \
    NEW_API_MONITOR_SWAP_MAX_PERCENT=95 \
    NEW_API_MONITOR_LOG_MAX_BYTES=1000 \
    NEW_API_MONITOR_APP_LOG_MAX_BYTES=1000000 \
    NEW_API_MONITOR_APP_LOG_WARN_BYTES=900000 \
    NEW_API_MONITOR_LOAD_WARN_PER_CPU=2 \
    NEW_API_MONITOR_LOAD_MAX_PER_CPU=4 \
    NEW_API_MONITOR_LOCK_DIR="$state_dir/monitor.lock" \
    FAKE_DOCKER_LOG="$state_dir/docker.log" \
    FAKE_CURL_LOG="$state_dir/curl.log" \
    FAKE_LAUNCHCTL_LOG="$state_dir/launchctl.log" \
    FAKE_LAUNCHCTL_STATE="$state_dir/launchctl.state" \
    FAKE_CONTAINER_STATE=running \
    FAKE_CONTAINER_HEALTH=healthy \
    NEW_API_RECOVERY_POSTGRES_PASSWORD=postgres-test \
    NEW_API_RECOVERY_REDIS_PASSWORD=redis-test \
    FAKE_EXPECT_POSTGRES_STDIN=1 \
    FAKE_EXPECT_REDIS_STDIN=1 \
    "$@" "$MONITOR"
}

assert_contains() {
  local needle="$1"
  local path="$2"
  if ! rg -Fq "$needle" "$path"; then
    printf 'assertion failed: missing=%s file=%s\n' "$needle" "$path" >&2
    exit 1
  fi
}

assert_equals() {
  local expected="$1"
  local actual="$2"
  local message="$3"
  if [ "$expected" != "$actual" ]; then
    printf 'assertion failed: %s expected=%s actual=%s\n' "$message" "$expected" "$actual" >&2
    exit 1
  fi
}

mkdir -p "$TMP_DIR/healthy" "$TMP_DIR/daemon" "$TMP_DIR/stopped" "$TMP_DIR/backend" "$TMP_DIR/app-health-drift" \
  "$TMP_DIR/disk" "$TMP_DIR/swap" "$TMP_DIR/log" "$TMP_DIR/warning" \
 "$TMP_DIR/load-warning" "$TMP_DIR/load-fail" "$TMP_DIR/latency-warning" "$TMP_DIR/latency-fail" \
  "$TMP_DIR/latency-timeout" "$TMP_DIR/env-file" "$TMP_DIR/rotation" "$TMP_DIR/app-log"
mkdir -p "$TMP_DIR/stale/monitor.lock" "$TMP_DIR/invalid/monitor.lock"

printf '%s\n' "$$" >"$TMP_DIR/stale/monitor.lock/pid"
cat >"$TMP_DIR/stale/probes.snapshot" <<'EOF'
schema_version=1
checked_at_utc=2000-01-01T00:00:00Z
state=healthy
docker_daemon=pass
postgres=pass
redis=pass
backend=pass
public=pass
lan=pass
app_health=healthy
EOF
set +e
run_monitor_case "$TMP_DIR/stale" env FAKE_CURL_MODE=success \
  >"$TMP_DIR/stale/stdout" 2>"$TMP_DIR/stale/stderr"
stale_status=$?
set -e
assert_equals 1 "$stale_status" "stale watchdog snapshot must fail closed"
assert_contains "monitor_probe_snapshot_status=fail status=stale" "$TMP_DIR/stale/stderr"
assert_contains "probe_snapshot_status=stale" "$TMP_DIR/stale/monitor.snapshot"
assert_contains "probe_state=unknown" "$TMP_DIR/stale/monitor.snapshot"

printf '%s\n' "$$" >"$TMP_DIR/invalid/monitor.lock/pid"
cat >"$TMP_DIR/invalid/probes.snapshot" <<'EOF'
schema_version=1
checked_at_utc=not-a-timestamp
state=healthy
EOF
set +e
run_monitor_case "$TMP_DIR/invalid" env FAKE_CURL_MODE=success \
  >"$TMP_DIR/invalid/stdout" 2>"$TMP_DIR/invalid/stderr"
invalid_snapshot_status=$?
set -e
assert_equals 1 "$invalid_snapshot_status" "invalid watchdog snapshot timestamp must fail closed"
assert_contains "monitor_probe_snapshot_status=fail status=invalid" "$TMP_DIR/invalid/stderr"
assert_contains "probe_snapshot_status=invalid" "$TMP_DIR/invalid/monitor.snapshot"

run_monitor_case "$TMP_DIR/healthy" env FAKE_CURL_MODE=success >"$TMP_DIR/healthy/stdout" 2>"$TMP_DIR/healthy/stderr"
assert_contains "monitor_status=pass" "$TMP_DIR/healthy/stdout"
assert_contains "monitor_resource_disk" "$TMP_DIR/healthy/stdout"
assert_contains "monitor_resource_swap" "$TMP_DIR/healthy/stdout"
assert_contains "monitor_resource_load" "$TMP_DIR/healthy/stdout"
assert_contains "monitor_resource_log" "$TMP_DIR/healthy/stdout"
assert_contains "monitor_resource_log_dir label=application" "$TMP_DIR/healthy/stdout"
assert_contains "monitor_latency label=backend status=200" "$TMP_DIR/healthy/stdout"
assert_contains "monitor_latency label=public status=200" "$TMP_DIR/healthy/stdout"
assert_contains "monitor_latency label=lan status=200" "$TMP_DIR/healthy/stdout"
assert_contains "monitor_status=pass" "$TMP_DIR/healthy/monitor.snapshot"
assert_contains "resource_failure_reasons=none" "$TMP_DIR/healthy/monitor.snapshot"
assert_contains "path=$TMP_DIR/healthy/app-logs" "$TMP_DIR/healthy/stdout"
assert_contains "probe_snapshot_status=pass" "$TMP_DIR/healthy/monitor.snapshot"
assert_contains "probe_state=healthy" "$TMP_DIR/healthy/monitor.snapshot"
assert_contains "probe_docker_daemon=pass" "$TMP_DIR/healthy/monitor.snapshot"
assert_contains "probe_postgres=pass" "$TMP_DIR/healthy/monitor.snapshot"
assert_contains "probe_redis=pass" "$TMP_DIR/healthy/monitor.snapshot"
assert_contains "probe_backend=pass" "$TMP_DIR/healthy/monitor.snapshot"
assert_contains "probe_public=pass" "$TMP_DIR/healthy/monitor.snapshot"
assert_contains "probe_lan=pass" "$TMP_DIR/healthy/monitor.snapshot"
assert_contains "probe_app_health=healthy" "$TMP_DIR/healthy/monitor.snapshot"
if [ "$(stat -f '%Lp' "$TMP_DIR/healthy/probes.snapshot")" != "600" ]; then
  printf 'assertion failed: probe snapshot must be mode 0600\n' >&2
  exit 1
fi
if rg -q 'postgres-test|redis-test' "$TMP_DIR/healthy/monitor.snapshot" "$TMP_DIR/healthy/probes.snapshot"; then
  printf 'assertion failed: probe snapshots must not contain credentials\n' >&2
  exit 1
fi
if [ "$(stat -f '%Lp' "$TMP_DIR/healthy/monitor.snapshot")" != "600" ]; then
  printf 'assertion failed: monitor status snapshot must be mode 0600\n' >&2
  exit 1
fi
if [ -f "$TMP_DIR/healthy/docker.log" ] && rg -q '(^| )(start|restart|unpause|desktop) ' "$TMP_DIR/healthy/docker.log"; then
  printf 'assertion failed: healthy monitor submitted a Docker action\n' >&2
  exit 1
fi

printf 'POSTGRES_PASSWORD=postgres-from-env-file\nREDIS_PASSWORD=redis-from-env-file\n' >"$TMP_DIR/env-file/.env"
run_monitor_case "$TMP_DIR/env-file" env \
  -u NEW_API_RECOVERY_POSTGRES_PASSWORD \
  -u NEW_API_RECOVERY_REDIS_PASSWORD \
  NEW_API_RECOVERY_ENV_FILE="$TMP_DIR/env-file/.env" \
  FAKE_CURL_MODE=success >"$TMP_DIR/env-file/stdout" 2>"$TMP_DIR/env-file/stderr"
assert_contains "monitor_status=pass" "$TMP_DIR/env-file/stdout"
if rg -Fq 'postgres-from-env-file' "$TMP_DIR/env-file/stdout" "$TMP_DIR/env-file/stderr" || \
   rg -Fq 'redis-from-env-file' "$TMP_DIR/env-file/stdout" "$TMP_DIR/env-file/stderr"; then
  printf 'assertion failed: monitor must not print credentials loaded from the env file\n' >&2
  exit 1
fi

set +e
run_monitor_case "$TMP_DIR/daemon" env FAKE_CURL_MODE=fail >"$TMP_DIR/daemon/stdout" 2>"$TMP_DIR/daemon/stderr"
daemon_status=$?
set -e
if [ "$daemon_status" -eq 0 ]; then
  printf 'assertion failed: daemon failure must be non-zero in monitor mode\n' >&2
  exit 1
fi
assert_contains "monitor_status=fail" "$TMP_DIR/daemon/stderr"
assert_contains "monitor_status=fail" "$TMP_DIR/daemon/monitor.snapshot"
if [ -f "$TMP_DIR/daemon/docker.log" ] && rg -q '(^| )(start|restart|unpause|desktop) ' "$TMP_DIR/daemon/docker.log"; then
  printf 'assertion failed: daemon monitor submitted a Docker action\n' >&2
  exit 1
fi

set +e
run_monitor_case "$TMP_DIR/stopped" env FAKE_CURL_MODE=success FAKE_CONTAINER_STATE=exited \
  >"$TMP_DIR/stopped/stdout" 2>"$TMP_DIR/stopped/stderr"
stopped_status=$?
set -e
if [ "$stopped_status" -eq 0 ]; then
  printf 'assertion failed: stopped container must be non-zero in monitor mode\n' >&2
  exit 1
fi
if [ -f "$TMP_DIR/stopped/docker.log" ] && rg -q '(^| )start ' "$TMP_DIR/stopped/docker.log"; then
  printf 'assertion failed: stopped container monitor submitted docker start\n' >&2
  exit 1
fi

set +e
run_monitor_case "$TMP_DIR/app-health-drift" env \
  FAKE_CURL_MODE=success FAKE_APP_CONTAINER_HEALTH=unhealthy \
  >"$TMP_DIR/app-health-drift/stdout" 2>"$TMP_DIR/app-health-drift/stderr"
app_health_drift_status=$?
set -e
if [ "$app_health_drift_status" -eq 0 ]; then
  printf 'assertion failed: app health drift must remain non-zero in monitor mode\n' >&2
  exit 1
fi
assert_contains "monitor_status=fail" "$TMP_DIR/app-health-drift/stderr"
assert_contains "probe_backend=pass" "$TMP_DIR/app-health-drift/monitor.snapshot"
assert_contains "probe_public=pass" "$TMP_DIR/app-health-drift/monitor.snapshot"
assert_contains "probe_lan=pass" "$TMP_DIR/app-health-drift/monitor.snapshot"
assert_contains "probe_app_health=unhealthy" "$TMP_DIR/app-health-drift/monitor.snapshot"
assert_contains "probe_failure_reason=app-health-drift" "$TMP_DIR/app-health-drift/monitor.snapshot"
if [ -f "$TMP_DIR/app-health-drift/docker.log" ] && rg -q '(^| )(start|restart|unpause|desktop) ' "$TMP_DIR/app-health-drift/docker.log"; then
  printf 'assertion failed: app health drift monitor submitted a Docker action\n' >&2
  exit 1
fi

set +e
run_monitor_case "$TMP_DIR/backend" env FAKE_CURL_MODE=backend-success-public-fail \
  >"$TMP_DIR/backend/stdout" 2>"$TMP_DIR/backend/stderr"
backend_status=$?
set -e
if [ "$backend_status" -eq 0 ]; then
  printf 'assertion failed: public failure must be non-zero in monitor mode\n' >&2
  exit 1
fi
assert_contains "monitor_status=fail" "$TMP_DIR/backend/stderr"
if [ -f "$TMP_DIR/backend/launchctl.log" ] && rg -q '^(kill|kickstart) ' "$TMP_DIR/backend/launchctl.log"; then
  printf 'assertion failed: monitor submitted a Caddy action\n' >&2
  exit 1
fi

set +e
run_monitor_case "$TMP_DIR/disk" env FAKE_CURL_MODE=success FAKE_MONITOR_DF_USED_PERCENT=95 \
  >"$TMP_DIR/disk/stdout" 2>"$TMP_DIR/disk/stderr"
disk_status=$?
set -e
if [ "$disk_status" -eq 0 ]; then
  printf 'assertion failed: high disk usage must be non-zero\n' >&2
  exit 1
fi
assert_contains "disk-usage-high" "$TMP_DIR/disk/stderr"

set +e
run_monitor_case "$TMP_DIR/swap" env FAKE_CURL_MODE=success FAKE_MONITOR_SWAP_PERCENT=96 \
  >"$TMP_DIR/swap/stdout" 2>"$TMP_DIR/swap/stderr"
swap_status=$?
set -e
if [ "$swap_status" -eq 0 ]; then
  printf 'assertion failed: high swap usage must be non-zero\n' >&2
  exit 1
fi
assert_contains "swap-usage-high" "$TMP_DIR/swap/stderr"

set +e
run_monitor_case "$TMP_DIR/log" env FAKE_CURL_MODE=success FAKE_MONITOR_LOG_SIZE=2000 \
  >"$TMP_DIR/log/stdout" 2>"$TMP_DIR/log/stderr"
log_status=$?
set -e
if [ "$log_status" -eq 0 ]; then
  printf 'assertion failed: high log size must be non-zero\n' >&2
  exit 1
fi
assert_contains "log-size-high" "$TMP_DIR/log/stderr"

set +e
run_monitor_case "$TMP_DIR/app-log" env FAKE_CURL_MODE=success FAKE_MONITOR_LOG_DIR_SIZE_KB=2000 \
  >"$TMP_DIR/app-log/stdout" 2>"$TMP_DIR/app-log/stderr"
app_log_status=$?
set -e
if [ "$app_log_status" -eq 0 ]; then
  printf 'assertion failed: high application log directory size must be non-zero\n' >&2
  exit 1
fi
assert_contains "log-directory-size-high:application" "$TMP_DIR/app-log/stderr"

set +e
run_monitor_case "$TMP_DIR/load-warning" env FAKE_CURL_MODE=success FAKE_MONITOR_LOADAVG=40 \
  FAKE_MONITOR_CPU_COUNT=16 >"$TMP_DIR/load-warning/stdout" 2>"$TMP_DIR/load-warning/stderr"
load_warning_status=$?
set -e
if [ "$load_warning_status" -ne 0 ]; then
  printf 'assertion failed: load warning must remain zero-free\n' >&2
  exit 1
fi
assert_contains "load-average-warning" "$TMP_DIR/load-warning/stderr"
assert_contains "monitor_status=warn" "$TMP_DIR/load-warning/stdout"

set +e
run_monitor_case "$TMP_DIR/load-fail" env FAKE_CURL_MODE=success FAKE_MONITOR_LOADAVG=65 \
  FAKE_MONITOR_CPU_COUNT=16 >"$TMP_DIR/load-fail/stdout" 2>"$TMP_DIR/load-fail/stderr"
load_fail_status=$?
set -e
if [ "$load_fail_status" -eq 0 ]; then
  printf 'assertion failed: high load must be non-zero\n' >&2
  exit 1
fi
assert_contains "load-average-high" "$TMP_DIR/load-fail/stderr"

set +e
run_monitor_case "$TMP_DIR/warning" env FAKE_CURL_MODE=success FAKE_MONITOR_SWAP_PERCENT=90 \
  NEW_API_MONITOR_SWAP_WARN_PERCENT=85 NEW_API_MONITOR_SWAP_MAX_PERCENT=95 \
  >"$TMP_DIR/warning/stdout" 2>"$TMP_DIR/warning/stderr"
warning_status=$?
set -e
if [ "$warning_status" -ne 0 ]; then
  printf 'assertion failed: warning resource state must remain non-zero-free\n' >&2
  exit 1
fi
assert_contains "monitor_resource_status=warn" "$TMP_DIR/warning/stderr"
assert_contains "monitor_status=warn" "$TMP_DIR/warning/stdout"
if rg -q 'monitor_status=fail' "$TMP_DIR/warning/stdout" "$TMP_DIR/warning/stderr"; then
  printf 'assertion failed: warning resource state must not be reported as failure\n' >&2
  exit 1
fi

set +e
run_monitor_case "$TMP_DIR/latency-warning" env FAKE_CURL_MODE=success \
  FAKE_MONITOR_LATENCY_SECONDS=1.500 \
  NEW_API_MONITOR_LATENCY_WARN_MS=1000 \
  NEW_API_MONITOR_LATENCY_MAX_MS=5000 >"$TMP_DIR/latency-warning/stdout" 2>"$TMP_DIR/latency-warning/stderr"
latency_warning_status=$?
set -e
if [ "$latency_warning_status" -ne 0 ]; then
  printf 'assertion failed: latency warning must remain zero-free\n' >&2
  exit 1
fi
assert_contains "latency-warning:backend:1500ms" "$TMP_DIR/latency-warning/stderr"
assert_contains "monitor_status=warn" "$TMP_DIR/latency-warning/stdout"

set +e
run_monitor_case "$TMP_DIR/latency-fail" env FAKE_CURL_MODE=success \
  FAKE_MONITOR_LATENCY_SECONDS=6.000 \
  NEW_API_MONITOR_LATENCY_WARN_MS=1000 \
  NEW_API_MONITOR_LATENCY_MAX_MS=5000 >"$TMP_DIR/latency-fail/stdout" 2>"$TMP_DIR/latency-fail/stderr"
latency_fail_status=$?
set -e
if [ "$latency_fail_status" -eq 0 ]; then
  printf 'assertion failed: high latency must be non-zero\n' >&2
  exit 1
fi
assert_contains "latency-high:backend:6000ms" "$TMP_DIR/latency-fail/stderr"
assert_contains "monitor_status=fail" "$TMP_DIR/latency-fail/stderr"

set +e
run_monitor_case "$TMP_DIR/latency-timeout" env FAKE_CURL_MODE=timeout \
  >"$TMP_DIR/latency-timeout/stdout" 2>"$TMP_DIR/latency-timeout/stderr"
latency_timeout_status=$?
set -e
if [ "$latency_timeout_status" -eq 0 ]; then
  printf 'assertion failed: latency timeout must be non-zero\n' >&2
  exit 1
fi
assert_contains "latency-timeout:backend" "$TMP_DIR/latency-timeout/stderr"
assert_contains "latency-timeout:public" "$TMP_DIR/latency-timeout/stderr"
assert_contains "latency-timeout:lan" "$TMP_DIR/latency-timeout/stderr"

printf 'older monitor output\n' >"$TMP_DIR/rotation/monitor.log.1"
printf 'current monitor output that must rotate\n' >"$TMP_DIR/rotation/monitor.log"
run_monitor_case "$TMP_DIR/rotation" env FAKE_CURL_MODE=success \
  FAKE_MONITOR_LOG_SIZE=100 \
  NEW_API_MONITOR_LOG_FILE="$TMP_DIR/rotation/monitor.log" \
  NEW_API_MONITOR_LOG_ROTATION_MAX_BYTES=20 \
  NEW_API_MONITOR_LOG_ROTATION_KEEP=2 \
  >"$TMP_DIR/rotation/stdout" 2>"$TMP_DIR/rotation/stderr"
assert_contains "monitor_status=pass" "$TMP_DIR/rotation/stdout"
assert_contains "current monitor output that must rotate" "$TMP_DIR/rotation/monitor.log.1"
assert_contains "older monitor output" "$TMP_DIR/rotation/monitor.log.2"
if [ -s "$TMP_DIR/rotation/monitor.log" ]; then
  printf 'assertion failed: rotated monitor log must be truncated\n' >&2
  exit 1
fi
for path in "$TMP_DIR/rotation/monitor.log" "$TMP_DIR/rotation/monitor.log.1" "$TMP_DIR/rotation/monitor.log.2"; do
  if [ "$(stat -f '%Lp' "$path")" != "600" ]; then
    printf 'assertion failed: rotated monitor log must be mode 0600: %s\n' "$path" >&2
    exit 1
  fi
done
if [ -f "$TMP_DIR/rotation/docker.log" ] && rg -q '(^| )(start|restart|unpause|desktop) ' "$TMP_DIR/rotation/docker.log"; then
  printf 'assertion failed: monitor log rotation submitted a Docker action\n' >&2
  exit 1
fi
if [ -f "$TMP_DIR/rotation/launchctl.log" ] && rg -q '^(kill|kickstart) ' "$TMP_DIR/rotation/launchctl.log"; then
  printf 'assertion failed: monitor log rotation submitted a Caddy action\n' >&2
  exit 1
fi

printf 'macos-docker-tun-monitor tests passed\n'
