#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WATCHDOG="$ROOT_DIR/scripts/macos-docker-tun-recovery-watchdog.sh"
FAKE_DOCKER="$ROOT_DIR/scripts/test-fixtures/fake-recovery-docker.sh"
FAKE_CURL="$ROOT_DIR/scripts/test-fixtures/fake-recovery-curl.sh"
FAKE_LAUNCHCTL="$ROOT_DIR/scripts/test-fixtures/fake-recovery-launchctl.sh"
FAKE_OPEN="$ROOT_DIR/scripts/test-fixtures/fake-recovery-open.sh"
TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/new-api-recovery-test.XXXXXX")"
trap 'rm -rf "$TMP_DIR"' EXIT

run_case() {
  local state_dir="$1"
  local redis_password="${NEW_API_RECOVERY_REDIS_PASSWORD:-redis-secret-for-test}"
  local postgres_password="${NEW_API_RECOVERY_POSTGRES_PASSWORD:-postgres-secret-for-test}"
  local caddy_plist="$state_dir/caddy.plist"
  if [ "${NEW_API_RECOVERY_TEST_CADDY_PLIST_UNSET:-0}" = "1" ]; then
    caddy_plist=""
  fi
  local -a recovery_env=(
    "NEW_API_RECOVERY_DOCKER_BIN=$FAKE_DOCKER"
    "NEW_API_RECOVERY_CURL_BIN=$FAKE_CURL"
    "NEW_API_RECOVERY_LAUNCHCTL_BIN=$FAKE_LAUNCHCTL"
    "NEW_API_RECOVERY_STATE_FILE=$state_dir/state"
    "NEW_API_RECOVERY_HEALTH_SNAPSHOT_FILE=$state_dir/health.snapshot"
    "NEW_API_RECOVERY_DOCKER_SOCKET=$state_dir/docker.sock"
    "NEW_API_RECOVERY_DOCKER_APP_PATH=$state_dir/no-docker.app"
    "NEW_API_RECOVERY_CADDY_PLIST=$caddy_plist"
    "NEW_API_RECOVERY_LAN_URL=http://lan.example.test:3000/api/healthz"
    "NEW_API_RECOVERY_CADDY_LOG_FILE=$state_dir/caddy.log"
    "NEW_API_RECOVERY_LOG_FILE=$state_dir/recovery.log"
    "NEW_API_RECOVERY_APP_FAILURE_COUNT_FILE=$state_dir/app-failures"
    "NEW_API_RECOVERY_APP_RESTART_FAILURE_COUNT_FILE=$state_dir/app-restart-failures"
    "NEW_API_RECOVERY_APP_RESTART_STAMP_FILE=$state_dir/app-restart-at"
    "NEW_API_RECOVERY_DOCKER_FAILURE_COUNT_FILE=$state_dir/docker-failures"
    "NEW_API_RECOVERY_DOCKER_START_FAILURE_COUNT_FILE=$state_dir/docker-start-failures"
    "NEW_API_RECOVERY_DOCKER_START_STAMP_FILE=$state_dir/docker-start-at"
    "NEW_API_RECOVERY_CADDY_FAILURE_COUNT_FILE=$state_dir/caddy-failures"
    "NEW_API_RECOVERY_CADDY_RESTART_FAILURE_COUNT_FILE=$state_dir/caddy-restart-failures"
    "NEW_API_RECOVERY_CADDY_RESTART_STAMP_FILE=$state_dir/caddy-restart-at"
    "NEW_API_RECOVERY_LAN_FAILURE_COUNT_FILE=$state_dir/lan-failures"
    "NEW_API_RECOVERY_CURL_CONNECT_TIMEOUT=1"
    "NEW_API_RECOVERY_CURL_MAX_TIME=1"
    "NEW_API_RECOVERY_ATTEMPTS=1"
    "NEW_API_RECOVERY_DELAY_SECONDS=0"
    "NEW_API_RECOVERY_DOCKER_COMMAND_TIMEOUT_SECONDS=5"
    "NEW_API_RECOVERY_APP_RESTART_FAILURE_THRESHOLD=1"
    "NEW_API_RECOVERY_APP_RESTART_TIMEOUT_SECONDS=1"
    "NEW_API_RECOVERY_APP_RESTART_STATUS_COMMAND_TIMEOUT_SECONDS=5"
    "NEW_API_RECOVERY_DOCKER_DESKTOP_COMMAND_TIMEOUT_SECONDS=2"
    "NEW_API_RECOVERY_DOCKER_DESKTOP_COMMAND_GRACE_SECONDS=1"
    "NEW_API_RECOVERY_DOCKER_DESKTOP_STOP_TIMEOUT_SECONDS=1"
    "NEW_API_RECOVERY_DOCKER_DESKTOP_RECOVERY_ATTEMPTS=1"
    "NEW_API_RECOVERY_DOCKER_DESKTOP_RECOVERY_DELAY_SECONDS=0"
    "NEW_API_RECOVERY_CADDY_LAUNCHCTL_COMMAND_TIMEOUT_SECONDS=5"
    "NEW_API_RECOVERY_CADDY_GRACEFUL_TIMEOUT_SECONDS=1"
    "NEW_API_RECOVERY_CADDY_GRACEFUL_POLL_INTERVAL_SECONDS=0"
    "NEW_API_RECOVERY_CURL_MODE=fail"
    "FAKE_CURL_MODE=ping"
    "FAKE_DOCKER_LOG=$state_dir/docker.log"
    "FAKE_CURL_LOG=$state_dir/curl.log"
    "FAKE_LAUNCHCTL_LOG=$state_dir/launchctl.log"
    "FAKE_LAUNCHCTL_STATE=$state_dir/launchctl.state"
    "FAKE_DAEMON_READY_FILE=$state_dir/daemon-ready"
    "FAKE_CONTAINER_STATE=running"
    "FAKE_CONTAINER_HEALTH=unhealthy"
  )
  shift
  mkdir -p "$state_dir"
  if [ -n "$caddy_plist" ]; then
    : >"$caddy_plist"
  fi
  if [ -n "${NEW_API_RECOVERY_TEST_ENV_FILE:-}" ]; then
    recovery_env+=("NEW_API_RECOVERY_ENV_FILE=$NEW_API_RECOVERY_TEST_ENV_FILE")
  fi
  if [ "${NEW_API_RECOVERY_TEST_NO_REDIS_PASSWORD:-0}" != "1" ]; then
    recovery_env+=("NEW_API_RECOVERY_REDIS_PASSWORD=$redis_password")
  fi
  if [ "${NEW_API_RECOVERY_TEST_NO_POSTGRES_PASSWORD:-0}" != "1" ]; then
    recovery_env+=("NEW_API_RECOVERY_POSTGRES_PASSWORD=$postgres_password")
  fi
  env "${recovery_env[@]}" "$@" "$WATCHDOG"
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

assert_contains() {
  local needle="$1"
  local path="$2"
  local message="$3"
  if ! rg -Fq -- "$needle" "$path"; then
    printf 'assertion failed: %s missing=%s file=%s\n' "$message" "$needle" "$path" >&2
    exit 1
  fi
}

mkdir -p \
  "$TMP_DIR/failure" \
  "$TMP_DIR/backend-failed-public-healthy" \
  "$TMP_DIR/timeout" \
  "$TMP_DIR/timeout-zero" \
  "$TMP_DIR/minimum" \
  "$TMP_DIR/ping-failure" \
  "$TMP_DIR/probe-window" \
  "$TMP_DIR/docker-restart-dry-run" \
  "$TMP_DIR/docker-restart-success" \
  "$TMP_DIR/docker-restart-start-failure" \
  "$TMP_DIR/docker-restart-timeout" \
  "$TMP_DIR/app-health-drift" \
  "$TMP_DIR/redis-noauth" \
  "$TMP_DIR/redis-auth" \
  "$TMP_DIR/redis-missing" \
  "$TMP_DIR/http-non-200" \
  "$TMP_DIR/postgres-auth" \
  "$TMP_DIR/credentials-from-dsn" \
  "$TMP_DIR/dsn-without-userinfo" \
  "$TMP_DIR/postgres-missing" \
  "$TMP_DIR/postgres-query-failure" \
  "$TMP_DIR/lan-failure" \
  "$TMP_DIR/caddy-failure" \
  "$TMP_DIR/caddy-threshold" \
  "$TMP_DIR/caddy-graceful" \
  "$TMP_DIR/caddy-fallback" \
  "$TMP_DIR/caddy-job-missing" \
  "$TMP_DIR/caddy-literal-tilde" \
  "$TMP_DIR/caddy-restart-cooldown" \
  "$TMP_DIR/caddy-restart-circuit" \
  "$TMP_DIR/caddy-rotation" \
  "$TMP_DIR/caddy-rotation-backend-failure" \
  "$TMP_DIR/recovery-rotation" \
  "$TMP_DIR/healthy-counter-reset" \
  "$TMP_DIR/missing-container" \
  "$TMP_DIR/inspect-error" \
  "$TMP_DIR/start" \
  "$TMP_DIR/start-failure" \
  "$TMP_DIR/docker-start" \
  "$TMP_DIR/docker-start-failure"

set +e
run_case "$TMP_DIR/failure" env FAKE_RESTART_EXIT=1 >"$TMP_DIR/failure/stdout" 2>"$TMP_DIR/failure/stderr"
first_status=$?
set -e
assert_equals 1 "$first_status" "failed restart remains observable"
restart_count="$(rg -c '^restart ' "$TMP_DIR/failure/docker.log")"
assert_equals 1 "$restart_count" "first failed restart is submitted once"
test -s "$TMP_DIR/failure/app-restart-at"
assert_equals 1 "$(<"$TMP_DIR/failure/app-restart-failures")" "failed restart counter"
assert_contains "container lifecycle container=new-api" "$TMP_DIR/failure/stdout" "lifecycle snapshot before failed restart"
assert_contains "oom_killed=false" "$TMP_DIR/failure/stdout" "lifecycle snapshot records OOM state"
assert_contains "restart_policy=always" "$TMP_DIR/failure/stdout" "lifecycle snapshot records restart policy"
assert_contains "schema_version=1" "$TMP_DIR/failure/health.snapshot" "health snapshot has a schema version"
assert_contains "state=app-restart-failed" "$TMP_DIR/failure/health.snapshot" "health snapshot records final restart failure"
assert_contains "backend=fail" "$TMP_DIR/failure/health.snapshot" "health snapshot records backend probe failure"
assert_contains "failure_reason=backend-failed" "$TMP_DIR/failure/health.snapshot" "health snapshot preserves backend failure reason"
assert_contains "action=app-restart" "$TMP_DIR/failure/health.snapshot" "health snapshot records restart action"
assert_contains "app_restart_failures=1" "$TMP_DIR/failure/health.snapshot" "health snapshot records app restart failures"
if ! rg -q '^app_restart_at=[1-9][0-9]*$' "$TMP_DIR/failure/health.snapshot"; then
  printf 'assertion failed: health snapshot must record app restart timestamp\n' >&2
  exit 1
fi

set +e
run_case "$TMP_DIR/backend-failed-public-healthy" env \
  FAKE_APP_CONTAINER_HEALTH=unhealthy \
  FAKE_CURL_MODE=backend-fail-public-success \
  NEW_API_RECOVERY_APP_RESTART_FAILURE_THRESHOLD=1 >"$TMP_DIR/backend-failed-public-healthy/stdout" 2>"$TMP_DIR/backend-failed-public-healthy/stderr"
backend_public_guard_status=$?
set -e
assert_equals 1 "$backend_public_guard_status" "backend-only failure remains observable"
assert_equals "backend-failed-public-healthy" "$(<"$TMP_DIR/backend-failed-public-healthy/state")" "public and LAN health suppress app restart"
assert_contains "while public and LAN entries remained healthy" "$TMP_DIR/backend-failed-public-healthy/stdout" "restart guard is observable"
assert_contains "action=none" "$TMP_DIR/backend-failed-public-healthy/health.snapshot" "restart guard records no lifecycle action"
if [ -f "$TMP_DIR/backend-failed-public-healthy/docker.log" ] && rg -q '^restart ' "$TMP_DIR/backend-failed-public-healthy/docker.log"; then
  printf 'assertion failed: public and LAN health must suppress app restart\n' >&2
  exit 1
fi

set +e
run_case "$TMP_DIR/failure" env FAKE_RESTART_EXIT=1 >"$TMP_DIR/failure/stdout-2" 2>"$TMP_DIR/failure/stderr-2"
second_status=$?
set -e
assert_equals 1 "$second_status" "cooldown does not hide backend failure"
restart_count="$(rg -c '^restart ' "$TMP_DIR/failure/docker.log")"
assert_equals 1 "$restart_count" "cooldown prevents duplicate restart"

set +e
run_case "$TMP_DIR/timeout" env FAKE_RESTART_SLEEP=5 FAKE_RESTART_EXIT=1 \
  NEW_API_RECOVERY_APP_RESTART_COMMAND_TIMEOUT_SECONDS=2 \
  NEW_API_RECOVERY_APP_RESTART_COMMAND_GRACE_SECONDS=1 >"$TMP_DIR/timeout/stdout" 2>"$TMP_DIR/timeout/stderr"
timeout_status=$?
set -e
assert_equals 1 "$timeout_status" "restart timeout remains a failure"
assert_contains "docker command timed out" "$TMP_DIR/timeout/stderr" "timeout is observable"
assert_equals 1 "$(rg -c '^restart ' "$TMP_DIR/timeout/docker.log")" "timed out restart is submitted once"

set +e
run_case "$TMP_DIR/minimum" env FAKE_RESTART_EXIT=1 \
  NEW_API_RECOVERY_APP_RESTART_TIMEOUT_SECONDS=5 \
  NEW_API_RECOVERY_APP_RESTART_COMMAND_TIMEOUT_SECONDS=1 \
  NEW_API_RECOVERY_APP_RESTART_COMMAND_GRACE_SECONDS=2 >"$TMP_DIR/minimum/stdout" 2>"$TMP_DIR/minimum/stderr"
minimum_status=$?
set -e
assert_equals 1 "$minimum_status" "minimum timeout case remains a failure"
assert_contains "app restart command timeout is too short" "$TMP_DIR/minimum/stdout" "short timeout is explicit"

set +e
run_case "$TMP_DIR/ping-failure" env FAKE_CURL_MODE=fail >"$TMP_DIR/ping-failure/stdout" 2>"$TMP_DIR/ping-failure/stderr"
ping_status=$?
set -e
assert_equals 0 "$ping_status" "daemon ping failure is recorded without a restart"
assert_equals "docker-daemon-unavailable" "$(<"$TMP_DIR/ping-failure/state")" "daemon ping failure state"
if [ -f "$TMP_DIR/ping-failure/docker.log" ] && rg -q '^info$' "$TMP_DIR/ping-failure/docker.log"; then
  printf 'assertion failed: daemon readiness must not fall back to docker info\n' >&2
  exit 1
fi
assert_contains "unix-socket" "$TMP_DIR/ping-failure/curl.log" "daemon readiness uses the Unix socket"

set +e
run_case "$TMP_DIR/probe-window" env \
  FAKE_CURL_MODE=success \
  FAKE_APP_CONTAINER_HEALTH=healthy \
  NEW_API_RECOVERY_CURL_MAX_TIME=10 >"$TMP_DIR/probe-window/stdout" 2>"$TMP_DIR/probe-window/stderr"
probe_window_status=$?
set -e
assert_equals 0 "$probe_window_status" "backend probe tolerates the configured slow-response window"
assert_contains "max-time 10" "$TMP_DIR/probe-window/curl.log" "backend probe uses the ten-second response window"

set +e
run_case "$TMP_DIR/timeout-zero" env \
  FAKE_CURL_MODE=success \
  FAKE_APP_CONTAINER_HEALTH=healthy \
  NEW_API_RECOVERY_CURL_MAX_TIME=0 \
  NEW_API_RECOVERY_DOCKER_COMMAND_TIMEOUT_SECONDS=0 \
  NEW_API_RECOVERY_APP_RESTART_STATUS_COMMAND_TIMEOUT_SECONDS=0 \
  NEW_API_RECOVERY_CADDY_LAUNCHCTL_COMMAND_TIMEOUT_SECONDS=0 >"$TMP_DIR/timeout-zero/stdout" 2>"$TMP_DIR/timeout-zero/stderr"
timeout_zero_status=$?
set -e
assert_equals 0 "$timeout_zero_status" "zero command timeouts fall back to bounded defaults"
assert_contains "invalid recovery timeout name=curl-max-time configured=0 fallback=10" "$TMP_DIR/timeout-zero/stderr" "zero curl timeout is rejected"
assert_contains "invalid recovery timeout name=docker-command-timeout configured=0 fallback=15" "$TMP_DIR/timeout-zero/stderr" "zero Docker timeout is rejected"
assert_contains "invalid recovery timeout name=app-restart-status-command-timeout configured=0 fallback=5" "$TMP_DIR/timeout-zero/stderr" "zero app status timeout is rejected"
assert_contains "invalid recovery timeout name=caddy-launchctl-command-timeout configured=0 fallback=5" "$TMP_DIR/timeout-zero/stderr" "zero Caddy timeout is rejected"

set +e
run_case "$TMP_DIR/http-non-200" env \
  FAKE_CURL_MODE=success-non-200 \
  FAKE_APP_CONTAINER_HEALTH=healthy >"$TMP_DIR/http-non-200/stdout" 2>"$TMP_DIR/http-non-200/stderr"
http_non_200_status=$?
set -e
assert_equals 1 "$http_non_200_status" "non-200 health response remains a probe failure"
assert_contains "-w %{http_code}" "$TMP_DIR/http-non-200/curl.log" "recovery URL probe requests the HTTP status"
assert_contains "state=backend-failed" "$TMP_DIR/http-non-200/health.snapshot" "non-200 response records backend failure"
if [ -f "$TMP_DIR/http-non-200/docker.log" ] && rg -q '^restart ' "$TMP_DIR/http-non-200/docker.log"; then
  printf 'assertion failed: non-200 health response must not trigger a restart when Docker health is healthy\n' >&2
  exit 1
fi

set +e
run_case "$TMP_DIR/docker-restart-dry-run" env \
  FAKE_CURL_MODE=fail \
  NEW_API_RECOVERY_FORCE_DOCKER_RESTART=1 \
  NEW_API_RECOVERY_DOCKER_RESTART_FAILURE_THRESHOLD=1 \
  NEW_API_RECOVERY_DRY_RUN=1 >"$TMP_DIR/docker-restart-dry-run/stdout" 2>"$TMP_DIR/docker-restart-dry-run/stderr"
docker_restart_dry_run_status=$?
set -e
assert_equals 0 "$docker_restart_dry_run_status" "forced Docker Desktop restart dry-run remains non-destructive"
assert_contains "dry-run: docker desktop stop" "$TMP_DIR/docker-restart-dry-run/stdout" "dry-run records Docker Desktop stop"
assert_contains "dry-run: wait for Docker daemon and verify" "$TMP_DIR/docker-restart-dry-run/stdout" "dry-run records post-restart probes"
if [ -f "$TMP_DIR/docker-restart-dry-run/docker.log" ] && rg -q '^desktop ' "$TMP_DIR/docker-restart-dry-run/docker.log"; then
  printf 'assertion failed: Docker Desktop dry-run must not execute desktop commands\n' >&2
  exit 1
fi

set +e
run_case "$TMP_DIR/docker-restart-success" env \
  FAKE_CURL_MODE=daemon-fail-until-restart \
  FAKE_CONTAINER_HEALTH=healthy \
  NEW_API_RECOVERY_FORCE_DOCKER_RESTART=1 \
  NEW_API_RECOVERY_DOCKER_RESTART_FAILURE_THRESHOLD=1 >"$TMP_DIR/docker-restart-success/stdout" 2>"$TMP_DIR/docker-restart-success/stderr"
docker_restart_success_status=$?
set -e
assert_equals 0 "$docker_restart_success_status" "Docker Desktop restart succeeds only after full recovery probes"
assert_contains "desktop stop --force --timeout 1" "$TMP_DIR/docker-restart-success/docker.log" "Docker Desktop stop timeout is explicit"
assert_contains "desktop start --detach" "$TMP_DIR/docker-restart-success/docker.log" "Docker Desktop start is explicit"
assert_contains "Docker Desktop daemon recovery probe passed" "$TMP_DIR/docker-restart-success/stdout" "daemon recovery probe is observable"
assert_contains "state=healthy" "$TMP_DIR/docker-restart-success/health.snapshot" "full Docker Desktop recovery records healthy state"
assert_contains "action=docker-restart" "$TMP_DIR/docker-restart-success/health.snapshot" "snapshot records Docker Desktop recovery action"
assert_contains "backend=pass" "$TMP_DIR/docker-restart-success/health.snapshot" "post-restart backend probe passes"
assert_contains "public=pass" "$TMP_DIR/docker-restart-success/health.snapshot" "post-restart Caddy probe passes"
assert_contains "lan=pass" "$TMP_DIR/docker-restart-success/health.snapshot" "post-restart LAN probe passes"
test ! -e "$TMP_DIR/docker-restart-success/state.docker-restart-failures"

for counter in \
  app-failures \
  app-restart-failures \
  app-restart-at \
  docker-failures \
  docker-start-failures \
  docker-start-at \
  caddy-failures \
  caddy-restart-failures \
  caddy-restart-at \
  lan-failures; do
  printf '9\n' >"$TMP_DIR/healthy-counter-reset/$counter"
done
printf '9\n' >"$TMP_DIR/healthy-counter-reset/state.docker-restart-failures"
printf '9\n' >"$TMP_DIR/healthy-counter-reset/state.docker-restart-at"
set +e
run_case "$TMP_DIR/healthy-counter-reset" env \
  FAKE_CURL_MODE=success \
  FAKE_CONTAINER_HEALTH=healthy >"$TMP_DIR/healthy-counter-reset/stdout" 2>"$TMP_DIR/healthy-counter-reset/stderr"
healthy_counter_reset_status=$?
set -e
assert_equals 0 "$healthy_counter_reset_status" "healthy probes succeed with stale recovery state"
for counter in \
  app-failures \
  app-restart-failures \
  app-restart-at \
  docker-failures \
  docker-start-failures \
  docker-start-at \
  caddy-failures \
  caddy-restart-failures \
  caddy-restart-at \
  lan-failures; do
  test ! -e "$TMP_DIR/healthy-counter-reset/$counter"
done
test ! -e "$TMP_DIR/healthy-counter-reset/state.docker-restart-failures"
test ! -e "$TMP_DIR/healthy-counter-reset/state.docker-restart-at"
assert_contains "healthy recovery cleared stale failure state" "$TMP_DIR/healthy-counter-reset/stdout" "healthy recovery state cleanup is observable"
assert_contains "app_restart_failures=0" "$TMP_DIR/healthy-counter-reset/health.snapshot" "healthy snapshot clears app restart failures"
assert_contains "docker_start_attempts=0" "$TMP_DIR/healthy-counter-reset/health.snapshot" "healthy snapshot clears Docker startup attempts"

set +e
run_case "$TMP_DIR/app-health-drift" env \
  FAKE_CURL_MODE=success \
  FAKE_CONTAINER_HEALTH=unhealthy >"$TMP_DIR/app-health-drift/stdout" 2>"$TMP_DIR/app-health-drift/stderr"
app_health_drift_status=$?
set -e
assert_equals 1 "$app_health_drift_status" "app health drift remains observable when HTTP probes pass"
assert_contains "state=app-health-drift" "$TMP_DIR/app-health-drift/health.snapshot" "app health drift is not reported as healthy"
assert_contains "failure_reason=app-health-drift" "$TMP_DIR/app-health-drift/health.snapshot" "app health drift has an explicit failure reason"
assert_contains "app_health=unhealthy" "$TMP_DIR/app-health-drift/health.snapshot" "app health drift preserves Docker health"
assert_contains "backend=pass" "$TMP_DIR/app-health-drift/health.snapshot" "app health drift preserves backend probe result"
assert_contains "public=pass" "$TMP_DIR/app-health-drift/health.snapshot" "app health drift preserves public probe result"
assert_contains "lan=pass" "$TMP_DIR/app-health-drift/health.snapshot" "app health drift preserves LAN probe result"
if [ -f "$TMP_DIR/app-health-drift/docker.log" ] && rg -q '^restart ' "$TMP_DIR/app-health-drift/docker.log"; then
  printf 'assertion failed: app health drift alone must not restart the application container\n' >&2
  exit 1
fi

set +e
run_case "$TMP_DIR/docker-restart-start-failure" env \
  FAKE_CURL_MODE=daemon-fail-until-restart \
  NEW_API_RECOVERY_FORCE_DOCKER_RESTART=1 \
  NEW_API_RECOVERY_DOCKER_RESTART_FAILURE_THRESHOLD=1 \
  FAKE_DESKTOP_START_EXIT=7 >"$TMP_DIR/docker-restart-start-failure/stdout" 2>"$TMP_DIR/docker-restart-start-failure/stderr"
docker_restart_start_failure_status=$?
set -e
assert_equals 0 "$docker_restart_start_failure_status" "Docker Desktop start failure does not crash the LaunchAgent cycle"
assert_contains "fake Docker Desktop operation failed status=7" "$TMP_DIR/docker-restart-start-failure/stderr" "Docker Desktop start stderr is preserved"
assert_contains "Docker Desktop start failed status=7" "$TMP_DIR/docker-restart-start-failure/stdout" "Docker Desktop start failure is recorded"
assert_contains "state=docker-restart-failed" "$TMP_DIR/docker-restart-start-failure/health.snapshot" "Docker Desktop start failure reaches failure state"
assert_contains "docker_restart_failures=1" "$TMP_DIR/docker-restart-start-failure/health.snapshot" "health snapshot records Docker Desktop restart failures"
assert_equals 1 "$(<"$TMP_DIR/docker-restart-start-failure/state.docker-restart-failures")" "Docker Desktop start failure increments circuit counter"

set +e
run_case "$TMP_DIR/docker-restart-timeout" env \
  FAKE_CURL_MODE=daemon-fail-until-restart \
  NEW_API_RECOVERY_FORCE_DOCKER_RESTART=1 \
  NEW_API_RECOVERY_DOCKER_RESTART_FAILURE_THRESHOLD=1 \
  FAKE_DESKTOP_STOP_SLEEP=3 >"$TMP_DIR/docker-restart-timeout/stdout" 2>"$TMP_DIR/docker-restart-timeout/stderr"
docker_restart_timeout_status=$?
set -e
assert_equals 0 "$docker_restart_timeout_status" "Docker Desktop timeout remains non-fatal to the watchdog cycle"
assert_contains "docker command timed out" "$TMP_DIR/docker-restart-timeout/stderr" "Docker Desktop timeout is observable"
assert_contains "state=docker-restart-failed" "$TMP_DIR/docker-restart-timeout/health.snapshot" "Docker Desktop timeout reaches failure state"

set +e
run_case "$TMP_DIR/redis-noauth" env FAKE_REDIS_MODE=noauth >"$TMP_DIR/redis-noauth/stdout" 2>"$TMP_DIR/redis-noauth/stderr"
redis_status=$?
set -e
assert_equals 1 "$redis_status" "Redis NOAUTH remains a dependency failure"
assert_equals "redis-not-ready" "$(<"$TMP_DIR/redis-noauth/state")" "Redis NOAUTH state"
if [ -f "$TMP_DIR/redis-noauth/docker.log" ] && rg -q '^restart ' "$TMP_DIR/redis-noauth/docker.log"; then
  printf 'assertion failed: Redis NOAUTH must not trigger a restart\n' >&2
  exit 1
fi

set +e
NEW_API_RECOVERY_TEST_NO_REDIS_PASSWORD=1 \
  run_case "$TMP_DIR/redis-missing" env FAKE_CURL_MODE=success >"$TMP_DIR/redis-missing/stdout" 2>"$TMP_DIR/redis-missing/stderr"
redis_missing_status=$?
set -e
assert_equals 1 "$redis_missing_status" "missing Redis credentials remain a dependency failure"
assert_equals "redis-not-ready" "$(<"$TMP_DIR/redis-missing/state")" "missing Redis credentials state"
if [ -f "$TMP_DIR/redis-missing/docker.log" ] && rg -q 'redis-cli' "$TMP_DIR/redis-missing/docker.log"; then
  printf 'assertion failed: missing Redis credentials must not run an unauthenticated probe\n' >&2
  exit 1
fi

set +e
run_case "$TMP_DIR/redis-auth" env \
  NEW_API_RECOVERY_REDIS_PASSWORD='redis-secret-for-test' \
  FAKE_EXPECT_REDIS_STDIN=1 \
  FAKE_APP_CONTAINER_HEALTH=healthy \
  FAKE_CURL_MODE=success >"$TMP_DIR/redis-auth/stdout" 2>"$TMP_DIR/redis-auth/stderr"
redis_auth_status=$?
set -e
assert_equals 0 "$redis_auth_status" "Redis authenticated probe succeeds"
if rg -Fq -- 'redis-cli --no-auth-warning -a' "$TMP_DIR/redis-auth/docker.log"; then
  printf 'assertion failed: Redis password must not use redis-cli -a\n' >&2
  exit 1
fi
if rg -Fq 'redis-secret-for-test' "$TMP_DIR/redis-auth/docker.log"; then
  printf 'assertion failed: Redis password must not appear in docker argv log\n' >&2
  exit 1
fi
if rg -Fq 'redis-secret-for-test' "$TMP_DIR/redis-auth/stdout" "$TMP_DIR/redis-auth/stderr"; then
  printf 'assertion failed: Redis password must not appear in probe output\n' >&2
  exit 1
fi
if rg -Fq 'redis_auth_stdin_missing' "$TMP_DIR/redis-auth/docker.log"; then
  printf 'assertion failed: Redis password was not delivered through stdin\n' >&2
  exit 1
fi
assert_contains "exec -i redis sh -c" "$TMP_DIR/redis-auth/docker.log" "Redis probe uses an interactive stdin exec"
assert_contains "state=healthy" "$TMP_DIR/redis-auth/health.snapshot" "health snapshot records healthy state"
assert_contains "docker_daemon=pass" "$TMP_DIR/redis-auth/health.snapshot" "health snapshot records Docker daemon readiness"
assert_contains "postgres=pass" "$TMP_DIR/redis-auth/health.snapshot" "health snapshot records PostgreSQL readiness"
assert_contains "redis=pass" "$TMP_DIR/redis-auth/health.snapshot" "health snapshot records Redis readiness"
assert_contains "postgres_docker_health=unhealthy" "$TMP_DIR/redis-auth/health.snapshot" "health snapshot separates PostgreSQL Docker health"
assert_contains "redis_docker_health=unhealthy" "$TMP_DIR/redis-auth/health.snapshot" "health snapshot separates Redis Docker health"
assert_contains "backend=pass" "$TMP_DIR/redis-auth/health.snapshot" "health snapshot records backend success"
assert_contains "public=pass" "$TMP_DIR/redis-auth/health.snapshot" "health snapshot records public success"
assert_contains "lan=pass" "$TMP_DIR/redis-auth/health.snapshot" "health snapshot records LAN success"
if rg -Fq 'redis-secret-for-test' "$TMP_DIR/redis-auth/health.snapshot"; then
  printf 'assertion failed: health snapshot must not contain Redis password\n' >&2
  exit 1
fi

set +e
run_case "$TMP_DIR/postgres-auth" env \
  NEW_API_RECOVERY_POSTGRES_PASSWORD='postgres-secret-for-test' \
  FAKE_EXPECT_POSTGRES_STDIN=1 \
  FAKE_APP_CONTAINER_HEALTH=healthy \
  FAKE_CURL_MODE=success >"$TMP_DIR/postgres-auth/stdout" 2>"$TMP_DIR/postgres-auth/stderr"
postgres_auth_status=$?
set -e
assert_equals 0 "$postgres_auth_status" "PostgreSQL authenticated probe succeeds"
if rg -Fq -- 'PGPASSWORD=' "$TMP_DIR/postgres-auth/docker.log"; then
  printf 'assertion failed: PostgreSQL password must not use docker exec environment argv\n' >&2
  exit 1
fi
if rg -Fq 'postgres-secret-for-test' "$TMP_DIR/postgres-auth/docker.log" "$TMP_DIR/postgres-auth/stdout" "$TMP_DIR/postgres-auth/stderr"; then
  printf 'assertion failed: PostgreSQL password must not appear in probe output\n' >&2
  exit 1
fi
if rg -Fq 'postgres_auth_stdin_missing' "$TMP_DIR/postgres-auth/docker.log"; then
  printf 'assertion failed: PostgreSQL password was not delivered through stdin\n' >&2
  exit 1
fi
assert_contains "exec -i postgres sh -c" "$TMP_DIR/postgres-auth/docker.log" "PostgreSQL probe uses an interactive stdin exec"

dsn_env_file="$TMP_DIR/credentials-from-dsn/recovery.env"
printf '%s\n' \
  'SQL_DSN=postgresql://root:postgres%40dsn%3Asecret@postgres:5432/new-api' \
  'REDIS_CONN_STRING=redis://:redis%40dsn%3Asecret@redis:6379/0' >"$dsn_env_file"
set +e
NEW_API_RECOVERY_TEST_NO_REDIS_PASSWORD=1 \
  NEW_API_RECOVERY_TEST_NO_POSTGRES_PASSWORD=1 \
  run_case "$TMP_DIR/credentials-from-dsn" env \
    NEW_API_RECOVERY_ENV_FILE="$dsn_env_file" \
    FAKE_EXPECT_REDIS_STDIN=1 \
    FAKE_EXPECT_REDIS_PASSWORD='redis@dsn:secret' \
    FAKE_EXPECT_POSTGRES_STDIN=1 \
    FAKE_EXPECT_POSTGRES_PASSWORD='postgres@dsn:secret' \
    FAKE_APP_CONTAINER_HEALTH=healthy \
    FAKE_CURL_MODE=success >"$TMP_DIR/credentials-from-dsn/stdout" 2>"$TMP_DIR/credentials-from-dsn/stderr"
credentials_from_dsn_status=$?
set -e
assert_equals 0 "$credentials_from_dsn_status" "URI-encoded Redis and PostgreSQL credentials are decoded"
if rg -Fq 'postgres%40dsn%3Asecret' "$TMP_DIR/credentials-from-dsn/docker.log" "$TMP_DIR/credentials-from-dsn/stdout" "$TMP_DIR/credentials-from-dsn/stderr"; then
  printf 'assertion failed: encoded PostgreSQL password must not appear in probe output\n' >&2
  exit 1
fi
if rg -Fq 'redis%40dsn%3Asecret' "$TMP_DIR/credentials-from-dsn/docker.log" "$TMP_DIR/credentials-from-dsn/stdout" "$TMP_DIR/credentials-from-dsn/stderr"; then
  printf 'assertion failed: encoded Redis password must not appear in probe output\n' >&2
  exit 1
fi

dsn_without_userinfo_file="$TMP_DIR/dsn-without-userinfo/recovery.env"
printf '%s\n' 'SQL_DSN=postgresql://postgres:5432/new-api' >"$dsn_without_userinfo_file"
set +e
NEW_API_RECOVERY_TEST_NO_POSTGRES_PASSWORD=1 \
  run_case "$TMP_DIR/dsn-without-userinfo" env \
    NEW_API_RECOVERY_ENV_FILE="$dsn_without_userinfo_file" \
    FAKE_CURL_MODE=success >"$TMP_DIR/dsn-without-userinfo/stdout" 2>"$TMP_DIR/dsn-without-userinfo/stderr"
dsn_without_userinfo_status=$?
set -e
assert_equals 1 "$dsn_without_userinfo_status" "PostgreSQL URI without user info is not treated as a password"
if [ -f "$TMP_DIR/dsn-without-userinfo/docker.log" ] && rg -q 'psql' "$TMP_DIR/dsn-without-userinfo/docker.log"; then
  printf 'assertion failed: PostgreSQL URI without user info must not run SELECT 1\n' >&2
  exit 1
fi

set +e
run_case "$TMP_DIR/postgres-query-failure" env FAKE_POSTGRES_MODE=query-fail >"$TMP_DIR/postgres-query-failure/stdout" 2>"$TMP_DIR/postgres-query-failure/stderr"
postgres_status=$?
set -e
assert_equals 1 "$postgres_status" "PostgreSQL SELECT 1 failure remains a dependency failure"
assert_equals "postgres-not-ready" "$(<"$TMP_DIR/postgres-query-failure/state")" "PostgreSQL query failure state"
if [ -f "$TMP_DIR/postgres-query-failure/docker.log" ] && rg -q '^restart ' "$TMP_DIR/postgres-query-failure/docker.log"; then
  printf 'assertion failed: PostgreSQL query failure must not trigger a restart\n' >&2
  exit 1
fi

set +e
NEW_API_RECOVERY_TEST_NO_POSTGRES_PASSWORD=1 \
  run_case "$TMP_DIR/postgres-missing" env FAKE_CURL_MODE=success >"$TMP_DIR/postgres-missing/stdout" 2>"$TMP_DIR/postgres-missing/stderr"
postgres_missing_status=$?
set -e
assert_equals 1 "$postgres_missing_status" "missing PostgreSQL credentials remain a dependency failure"
assert_equals "postgres-not-ready" "$(<"$TMP_DIR/postgres-missing/state")" "missing PostgreSQL credentials state"
if [ -f "$TMP_DIR/postgres-missing/docker.log" ] && rg -q 'psql' "$TMP_DIR/postgres-missing/docker.log"; then
  printf 'assertion failed: missing PostgreSQL credentials must not run SELECT 1\n' >&2
  exit 1
fi

set +e
run_case "$TMP_DIR/lan-failure" env \
  FAKE_CURL_MODE=lan-fail >"$TMP_DIR/lan-failure/stdout" 2>"$TMP_DIR/lan-failure/stderr"
lan_status=$?
set -e
assert_equals 1 "$lan_status" "LAN failure remains observable after loopback health succeeds"
assert_equals "lan-failed" "$(<"$TMP_DIR/lan-failure/state")" "LAN failure has an independent state"
assert_contains "LAN health check failed" "$TMP_DIR/lan-failure/stdout" "LAN failure is recorded"
assert_contains "lan=fail" "$TMP_DIR/lan-failure/health.snapshot" "health snapshot records LAN failure"
if [ -f "$TMP_DIR/lan-failure/launchctl.log" ] && rg -q '^(kill|kickstart) ' "$TMP_DIR/lan-failure/launchctl.log"; then
  printf 'assertion failed: LAN-only failure must not restart Caddy\n' >&2
  exit 1
fi
if [ -f "$TMP_DIR/lan-failure/docker.log" ] && rg -q '^restart ' "$TMP_DIR/lan-failure/docker.log"; then
  printf 'assertion failed: LAN-only failure must not restart the application container\n' >&2
  exit 1
fi

mkdir -p "$TMP_DIR/lan-disabled"
run_case "$TMP_DIR/lan-disabled" env \
  NEW_API_RECOVERY_LAN_URL= \
  FAKE_APP_CONTAINER_HEALTH=healthy \
  FAKE_CURL_MODE=success >"$TMP_DIR/lan-disabled/stdout" 2>"$TMP_DIR/lan-disabled/stderr"
assert_equals "healthy" "$(<"$TMP_DIR/lan-disabled/state")" "empty LAN URL disables the optional probe"
assert_contains "lan=disabled" "$TMP_DIR/lan-disabled/health.snapshot" "health snapshot records disabled LAN probe"

set +e
run_case "$TMP_DIR/caddy-failure" env \
  FAKE_CURL_MODE=backend-success-public-fail \
  NEW_API_RECOVERY_DRY_RUN=1 \
  NEW_API_RECOVERY_WRITE_DRY_RUN_SNAPSHOT=1 \
  NEW_API_RECOVERY_PUBLIC_FAILURE_THRESHOLD=0 \
  NEW_API_RECOVERY_CADDY_RESTART_FAILURE_THRESHOLD=0 >"$TMP_DIR/caddy-failure/stdout" 2>"$TMP_DIR/caddy-failure/stderr"
caddy_status=$?
set -e
assert_equals 1 "$caddy_status" "Caddy public failure remains observable after dry-run restart planning"
assert_contains "dry-run: launchctl kill SIGTERM" "$TMP_DIR/caddy-failure/stdout" "Caddy graceful restart planning is observable"
if [ -f "$TMP_DIR/caddy-failure/docker.log" ] && rg -q '^restart ' "$TMP_DIR/caddy-failure/docker.log"; then
  printf 'assertion failed: Caddy failure must not restart the application container\n' >&2
  exit 1
fi

set +e
run_case "$TMP_DIR/caddy-threshold" env \
  FAKE_CURL_MODE=backend-success-public-fail \
  NEW_API_RECOVERY_DRY_RUN=1 \
  NEW_API_RECOVERY_WRITE_DRY_RUN_SNAPSHOT=1 \
  NEW_API_RECOVERY_PUBLIC_FAILURE_THRESHOLD=0 \
  NEW_API_RECOVERY_CADDY_RESTART_FAILURE_THRESHOLD=2 >"$TMP_DIR/caddy-threshold/stdout" 2>"$TMP_DIR/caddy-threshold/stderr"
caddy_threshold_status=$?
set -e
assert_equals 1 "$caddy_threshold_status" "Caddy threshold defers restart until its own threshold"
if [ -f "$TMP_DIR/caddy-threshold/launchctl.log" ] && rg -q '^(kill|kickstart) ' "$TMP_DIR/caddy-threshold/launchctl.log"; then
  printf 'assertion failed: Caddy restart must wait for CADDY_RESTART_FAILURE_THRESHOLD\n' >&2
  exit 1
fi
assert_contains "failures=1" "$TMP_DIR/caddy-threshold/stdout" "Caddy threshold records the first public failure"

set +e
run_case "$TMP_DIR/caddy-graceful" env \
  FAKE_CURL_MODE=backend-success-public-fail \
  FAKE_LAUNCHCTL_CHANGE_PID=1 \
  NEW_API_RECOVERY_PUBLIC_FAILURE_THRESHOLD=0 \
  NEW_API_RECOVERY_CADDY_RESTART_FAILURE_THRESHOLD=0 >"$TMP_DIR/caddy-graceful/stdout" 2>"$TMP_DIR/caddy-graceful/stderr"
caddy_graceful_status=$?
set -e
assert_equals 1 "$caddy_graceful_status" "Caddy graceful restart case keeps public failure visible"
assert_contains "kill SIGTERM" "$TMP_DIR/caddy-graceful/launchctl.log" "Caddy graceful path sends SIGTERM"
if rg -q '^kickstart ' "$TMP_DIR/caddy-graceful/launchctl.log"; then
  printf 'assertion failed: successful Caddy graceful restart must not use kickstart fallback\n' >&2
  exit 1
fi
assert_contains "Caddy restarted after graceful SIGTERM" "$TMP_DIR/caddy-graceful/stdout" "Caddy graceful restart is recorded"
assert_equals 1 "$(<"$TMP_DIR/caddy-graceful/caddy-restart-failures")" "Caddy post-restart probe failure is counted"
assert_contains "state=caddy-restart-probe-failed" "$TMP_DIR/caddy-graceful/health.snapshot" "Caddy post-restart probe failure has an explicit state"

set +e
run_case "$TMP_DIR/caddy-graceful" env \
  FAKE_CURL_MODE=backend-success-public-fail \
  FAKE_LAUNCHCTL_CHANGE_PID=1 \
  NEW_API_RECOVERY_PUBLIC_FAILURE_THRESHOLD=0 \
  NEW_API_RECOVERY_CADDY_RESTART_FAILURE_THRESHOLD=0 \
  NEW_API_RECOVERY_CADDY_RESTART_MAX_FAILURES=1 >"$TMP_DIR/caddy-graceful/stdout-2" 2>"$TMP_DIR/caddy-graceful/stderr-2"
caddy_graceful_circuit_status=$?
set -e
assert_equals 1 "$caddy_graceful_circuit_status" "Caddy post-restart probe circuit keeps public failure visible"
assert_contains "state=caddy-restart-circuit-open" "$TMP_DIR/caddy-graceful/health.snapshot" "Caddy post-restart probe failure opens the circuit"
assert_equals 1 "$(rg -c '^kill ' "$TMP_DIR/caddy-graceful/launchctl.log")" "Caddy post-restart probe circuit prevents repeated restarts"

set +e
run_case "$TMP_DIR/caddy-fallback" env \
  FAKE_CURL_MODE=backend-success-public-fail \
  FAKE_LAUNCHCTL_CHANGE_PID=0 \
  NEW_API_RECOVERY_PUBLIC_FAILURE_THRESHOLD=0 \
  NEW_API_RECOVERY_CADDY_RESTART_FAILURE_THRESHOLD=0 >"$TMP_DIR/caddy-fallback/stdout" 2>"$TMP_DIR/caddy-fallback/stderr"
caddy_fallback_status=$?
set -e
assert_equals 1 "$caddy_fallback_status" "Caddy fallback case keeps public failure visible"
assert_contains "kill SIGTERM" "$TMP_DIR/caddy-fallback/launchctl.log" "Caddy fallback attempts graceful SIGTERM first"
assert_contains "kickstart -k" "$TMP_DIR/caddy-fallback/launchctl.log" "Caddy fallback uses kickstart after graceful timeout"
assert_contains "using kickstart fallback" "$TMP_DIR/caddy-fallback/stderr" "Caddy fallback is observable"

set +e
run_case "$TMP_DIR/caddy-job-missing" env \
  FAKE_CURL_MODE=backend-success-public-fail \
  FAKE_LAUNCHCTL_JOB_DISAPPEARS=1 \
  NEW_API_RECOVERY_CADDY_GRACEFUL_TIMEOUT_SECONDS=1 \
  NEW_API_RECOVERY_CADDY_GRACEFUL_POLL_INTERVAL_SECONDS=0 \
  NEW_API_RECOVERY_PUBLIC_FAILURE_THRESHOLD=0 \
  NEW_API_RECOVERY_CADDY_RESTART_FAILURE_THRESHOLD=0 >"$TMP_DIR/caddy-job-missing/stdout" 2>"$TMP_DIR/caddy-job-missing/stderr"
caddy_job_missing_status=$?
set -e
assert_equals 1 "$caddy_job_missing_status" "Caddy missing job remains a public failure"
assert_contains "kill SIGTERM" "$TMP_DIR/caddy-job-missing/launchctl.log" "missing-job path attempts graceful SIGTERM first"
assert_contains "kickstart -k" "$TMP_DIR/caddy-job-missing/launchctl.log" "missing-job path uses kickstart fallback"
assert_contains "using kickstart fallback" "$TMP_DIR/caddy-job-missing/stderr" "missing-job fallback is observable"

custom_caddy_env="$TMP_DIR/custom-caddy.env"
custom_caddy_plist="$TMP_DIR/custom-caddy.plist"
mkdir -p "$TMP_DIR/caddy-custom-env"
printf '%s\n' \
  'CADDY_LAUNCHD_LABEL=com.example.custom-caddy' \
  "CADDY_LAUNCHD_PLIST=$custom_caddy_plist" >"$custom_caddy_env"
: >"$custom_caddy_plist"
set +e
NEW_API_RECOVERY_TEST_ENV_FILE="$custom_caddy_env" \
NEW_API_RECOVERY_TEST_CADDY_PLIST_UNSET=1 \
  run_case "$TMP_DIR/caddy-custom-env" env \
  FAKE_CURL_MODE=backend-success-public-fail \
  FAKE_LAUNCHCTL_PRINT_MODE=missing \
  NEW_API_RECOVERY_PUBLIC_FAILURE_THRESHOLD=0 \
  NEW_API_RECOVERY_CADDY_RESTART_FAILURE_THRESHOLD=0 >"$TMP_DIR/caddy-custom-env/stdout" 2>"$TMP_DIR/caddy-custom-env/stderr"
caddy_custom_env_status=$?
set -e
assert_equals 1 "$caddy_custom_env_status" "custom Caddy env remains a public failure"
assert_contains "print gui/$(id -u)/com.example.custom-caddy" "$TMP_DIR/caddy-custom-env/launchctl.log" "watchdog uses the env-file Caddy label"
assert_contains "bootstrap gui/$(id -u) $custom_caddy_plist" "$TMP_DIR/caddy-custom-env/launchctl.log" "watchdog uses the env-file Caddy plist"

literal_tilde_env="$TMP_DIR/literal-tilde.env"
literal_tilde_home="$TMP_DIR/literal-tilde-home"
printf '%s\n' 'CADDY_LAUNCHD_PLIST=~/Library/LaunchAgents/literal-caddy.plist' >"$literal_tilde_env"
mkdir -p "$literal_tilde_home/Library/LaunchAgents"
: >"$literal_tilde_home/Library/LaunchAgents/literal-caddy.plist"
set +e
HOME="$literal_tilde_home" \
NEW_API_RECOVERY_TEST_ENV_FILE="$literal_tilde_env" \
NEW_API_RECOVERY_TEST_CADDY_PLIST_UNSET=1 \
  run_case "$TMP_DIR/caddy-literal-tilde" env \
  FAKE_CURL_MODE=backend-success-public-fail \
  FAKE_LAUNCHCTL_PRINT_MODE=missing \
  NEW_API_RECOVERY_PUBLIC_FAILURE_THRESHOLD=0 \
  NEW_API_RECOVERY_CADDY_RESTART_FAILURE_THRESHOLD=0 >"$TMP_DIR/caddy-literal-tilde/stdout" 2>"$TMP_DIR/caddy-literal-tilde/stderr"
caddy_literal_tilde_status=$?
set -e
assert_equals 1 "$caddy_literal_tilde_status" "literal tilde Caddy env remains a public failure"
assert_contains "print gui/$(id -u)/literal-caddy" "$TMP_DIR/caddy-literal-tilde/launchctl.log" "watchdog derives the literal tilde Caddy label"
assert_contains "bootstrap gui/$(id -u) $literal_tilde_home/Library/LaunchAgents/literal-caddy.plist" "$TMP_DIR/caddy-literal-tilde/launchctl.log" "watchdog expands the literal tilde Caddy plist"

set +e
run_case "$TMP_DIR/caddy-restart-cooldown" env \
  FAKE_CURL_MODE=backend-success-public-fail \
  FAKE_LAUNCHCTL_PRINT_MODE=missing \
  NEW_API_RECOVERY_CADDY_PLIST="$TMP_DIR/caddy-restart-cooldown/missing.plist" \
  NEW_API_RECOVERY_PUBLIC_FAILURE_THRESHOLD=0 \
  NEW_API_RECOVERY_CADDY_RESTART_FAILURE_THRESHOLD=0 \
  NEW_API_RECOVERY_CADDY_RESTART_MAX_FAILURES=3 >"$TMP_DIR/caddy-restart-cooldown/stdout" 2>"$TMP_DIR/caddy-restart-cooldown/stderr"
caddy_restart_failure_status=$?
set -e
assert_equals 1 "$caddy_restart_failure_status" "failed Caddy restart remains observable"
assert_equals 1 "$(<"$TMP_DIR/caddy-restart-cooldown/caddy-restart-failures")" "Caddy restart failure is counted"
assert_contains "state=caddy-restart-failed" "$TMP_DIR/caddy-restart-cooldown/health.snapshot" "Caddy restart failure has an explicit state"
assert_contains "restart_failures=1" "$TMP_DIR/caddy-restart-cooldown/health.snapshot" "Caddy restart failure count is snapshotted"

set +e
run_case "$TMP_DIR/caddy-restart-cooldown" env \
  FAKE_CURL_MODE=backend-success-public-fail \
  FAKE_LAUNCHCTL_PRINT_MODE=missing \
  NEW_API_RECOVERY_CADDY_PLIST="$TMP_DIR/caddy-restart-cooldown/missing.plist" \
  NEW_API_RECOVERY_PUBLIC_FAILURE_THRESHOLD=0 \
  NEW_API_RECOVERY_CADDY_RESTART_FAILURE_THRESHOLD=0 \
  NEW_API_RECOVERY_CADDY_RESTART_MAX_FAILURES=3 >"$TMP_DIR/caddy-restart-cooldown/stdout-2" 2>"$TMP_DIR/caddy-restart-cooldown/stderr-2"
caddy_restart_cooldown_status=$?
set -e
assert_equals 1 "$caddy_restart_cooldown_status" "Caddy restart cooldown keeps public failure visible"
assert_contains "state=caddy-restart-cooldown" "$TMP_DIR/caddy-restart-cooldown/health.snapshot" "Caddy restart cooldown has an explicit state"
assert_contains "Caddy restart cooldown active" "$TMP_DIR/caddy-restart-cooldown/stdout-2" "Caddy restart cooldown is observable"
assert_equals 1 "$(rg -c '^print ' "$TMP_DIR/caddy-restart-cooldown/launchctl.log")" "Caddy cooldown prevents duplicate launchctl restart"

set +e
run_case "$TMP_DIR/caddy-restart-circuit" env \
  FAKE_CURL_MODE=backend-success-public-fail \
  FAKE_LAUNCHCTL_PRINT_MODE=missing \
  NEW_API_RECOVERY_CADDY_PLIST="$TMP_DIR/caddy-restart-circuit/missing.plist" \
  NEW_API_RECOVERY_PUBLIC_FAILURE_THRESHOLD=0 \
  NEW_API_RECOVERY_CADDY_RESTART_FAILURE_THRESHOLD=0 \
  NEW_API_RECOVERY_CADDY_RESTART_MAX_FAILURES=1 >"$TMP_DIR/caddy-restart-circuit/stdout" 2>"$TMP_DIR/caddy-restart-circuit/stderr"
caddy_restart_circuit_failure_status=$?
set -e
assert_equals 1 "$caddy_restart_circuit_failure_status" "Caddy circuit seed failure remains observable"

set +e
run_case "$TMP_DIR/caddy-restart-circuit" env \
  FAKE_CURL_MODE=backend-success-public-fail \
  FAKE_LAUNCHCTL_PRINT_MODE=missing \
  NEW_API_RECOVERY_CADDY_PLIST="$TMP_DIR/caddy-restart-circuit/missing.plist" \
  NEW_API_RECOVERY_PUBLIC_FAILURE_THRESHOLD=0 \
  NEW_API_RECOVERY_CADDY_RESTART_FAILURE_THRESHOLD=0 \
  NEW_API_RECOVERY_CADDY_RESTART_MAX_FAILURES=1 >"$TMP_DIR/caddy-restart-circuit/stdout-2" 2>"$TMP_DIR/caddy-restart-circuit/stderr-2"
caddy_restart_circuit_status=$?
set -e
assert_equals 1 "$caddy_restart_circuit_status" "Caddy restart circuit keeps public failure visible"
assert_contains "state=caddy-restart-circuit-open" "$TMP_DIR/caddy-restart-circuit/health.snapshot" "Caddy restart circuit has an explicit state"
assert_contains "Caddy restart circuit open" "$TMP_DIR/caddy-restart-circuit/stdout-2" "Caddy restart circuit is observable"
assert_equals 1 "$(rg -c '^print ' "$TMP_DIR/caddy-restart-circuit/launchctl.log")" "Caddy circuit prevents duplicate launchctl restart"

printf 'oversized caddy log fixture\n' >"$TMP_DIR/caddy-rotation/caddy.log"
set +e
run_case "$TMP_DIR/caddy-rotation" env \
  FAKE_APP_CONTAINER_HEALTH=healthy \
  FAKE_CURL_MODE=success \
  NEW_API_RECOVERY_CADDY_LOG_ROTATION_MAX_BYTES=10 \
  NEW_API_RECOVERY_CADDY_LOG_ROTATION_KEEP=2 >"$TMP_DIR/caddy-rotation/stdout" 2>"$TMP_DIR/caddy-rotation/stderr"
rotation_status=$?
set -e
assert_equals 0 "$rotation_status" "oversized Caddy log is rotated after a healthy check"
test -f "$TMP_DIR/caddy-rotation/caddy.log.1"
test ! -s "$TMP_DIR/caddy-rotation/caddy.log"
assert_contains "rotated Caddy log" "$TMP_DIR/caddy-rotation/stdout" "Caddy log rotation is observable"

printf 'oversized Caddy log before backend failure\n' >"$TMP_DIR/caddy-rotation-backend-failure/caddy.log"
set +e
run_case "$TMP_DIR/caddy-rotation-backend-failure" env \
  FAKE_APP_CONTAINER_HEALTH=healthy \
  FAKE_CURL_MODE=backend-success-public-fail \
  NEW_API_RECOVERY_BACKEND_URL=http://backend.invalid/api/healthz \
  NEW_API_RECOVERY_CADDY_LOG_ROTATION_MAX_BYTES=10 \
  NEW_API_RECOVERY_CADDY_LOG_ROTATION_KEEP=2 >"$TMP_DIR/caddy-rotation-backend-failure/stdout" 2>"$TMP_DIR/caddy-rotation-backend-failure/stderr"
backend_rotation_status=$?
set -e
assert_equals 1 "$backend_rotation_status" "oversized Caddy log is rotated before a backend failure"
test -f "$TMP_DIR/caddy-rotation-backend-failure/caddy.log.1"
test ! -s "$TMP_DIR/caddy-rotation-backend-failure/caddy.log"
assert_contains "rotated Caddy log" "$TMP_DIR/caddy-rotation-backend-failure/stdout" "early Caddy log rotation is observable"

printf 'oversized recovery log fixture\n' >"$TMP_DIR/recovery-rotation/recovery.log"
set +e
run_case "$TMP_DIR/recovery-rotation" env \
  FAKE_APP_CONTAINER_HEALTH=healthy \
  FAKE_CURL_MODE=success \
  NEW_API_RECOVERY_LOG_ROTATION_MAX_BYTES=10 \
  NEW_API_RECOVERY_LOG_ROTATION_KEEP=2 >"$TMP_DIR/recovery-rotation/stdout" 2>"$TMP_DIR/recovery-rotation/stderr"
recovery_rotation_status=$?
set -e
assert_equals 0 "$recovery_rotation_status" "oversized recovery log is rotated after a healthy check"
test -f "$TMP_DIR/recovery-rotation/recovery.log.1"
assert_contains "oversized recovery log fixture" "$TMP_DIR/recovery-rotation/recovery.log.1" "recovery log archive contains prior content"
if rg -Fq "oversized recovery log fixture" "$TMP_DIR/recovery-rotation/recovery.log"; then
  printf 'assertion failed: active recovery log retained old content after rotation\n' >&2
  exit 1
fi
assert_contains "rotated recovery log" "$TMP_DIR/recovery-rotation/stdout" "recovery log rotation is observable"

set +e
run_case "$TMP_DIR/start" env \
  FAKE_CONTAINER_STATE=exited \
  FAKE_APP_CONTAINER_HEALTH=healthy \
  FAKE_CURL_MODE=success >"$TMP_DIR/start/stdout" 2>"$TMP_DIR/start/stderr"
start_status=$?
set -e
assert_equals 0 "$start_status" "stopped containers can be started without recreation"
assert_contains "container lifecycle container=new-api" "$TMP_DIR/start/stdout" "lifecycle snapshot surrounds docker start"
assert_contains "start new-api" "$TMP_DIR/start/docker.log" "existing container start is the only recovery action"
assert_equals 1 "$(rg -c '^start postgres$' "$TMP_DIR/start/docker.log")" "PostgreSQL start is submitted once"
assert_equals 1 "$(rg -c '^start redis$' "$TMP_DIR/start/docker.log")" "Redis start is submitted once"
assert_equals 1 "$(rg -c '^start new-api$' "$TMP_DIR/start/docker.log")" "new-api start is submitted once"

set +e
run_case "$TMP_DIR/start-failure" env \
  FAKE_CONTAINER_STATE=exited \
  FAKE_START_EXIT=7 >"$TMP_DIR/start-failure/stdout" 2>"$TMP_DIR/start-failure/stderr"
start_failure_status=$?
set -e
assert_equals 1 "$start_failure_status" "failed dependency container start remains observable"
assert_contains "state=postgres-container-recovery-failed" "$TMP_DIR/start-failure/health.snapshot" "dependency start failure has an explicit state"
assert_contains "failure_reason=postgres-container-recovery-failed" "$TMP_DIR/start-failure/health.snapshot" "dependency start failure has an explicit reason"
assert_contains "start postgres" "$TMP_DIR/start-failure/docker.log" "dependency start was attempted"
if rg -q '^start new-api$' "$TMP_DIR/start-failure/docker.log"; then
  printf 'assertion failed: failed dependency start must stop before starting new-api\n' >&2
  exit 1
fi

set +e
run_case "$TMP_DIR/missing-container" env \
  FAKE_CONTAINER_STATE=missing \
  NEW_API_RECOVERY_MISSING_CONTAINER_RETRY_COOLDOWN_SECONDS=300 \
  NEW_API_RECOVERY_MISSING_CONTAINER_MAX_RETRY_COOLDOWN_SECONDS=900 \
  FAKE_CURL_MODE=success >"$TMP_DIR/missing-container/stdout" 2>"$TMP_DIR/missing-container/stderr"
missing_container_status=$?
set -e
assert_equals 1 "$missing_container_status" "missing dependency container remains observable"
assert_equals "postgres-container-recovery-failed" "$(<"$TMP_DIR/missing-container/state")" "missing PostgreSQL state"
assert_equals 1 "$(<"$TMP_DIR/missing-container/state.missing-postgres-failures")" "missing PostgreSQL failure counter"
assert_contains "postgres_missing_failures=1" "$TMP_DIR/missing-container/health.snapshot" "missing PostgreSQL counter is snapshotted"
if [ -f "$TMP_DIR/missing-container/docker.log" ] && rg -q '^(start|restart|unpause) ' "$TMP_DIR/missing-container/docker.log"; then
  printf 'assertion failed: missing dependency must not submit a Docker lifecycle action\n' >&2
  exit 1
fi

set +e
run_case "$TMP_DIR/missing-container" env \
  FAKE_CONTAINER_STATE=missing \
  NEW_API_RECOVERY_MISSING_CONTAINER_RETRY_COOLDOWN_SECONDS=300 \
  NEW_API_RECOVERY_MISSING_CONTAINER_MAX_RETRY_COOLDOWN_SECONDS=900 \
  FAKE_CURL_MODE=success >"$TMP_DIR/missing-container/stdout-2" 2>"$TMP_DIR/missing-container/stderr-2"
missing_container_cooldown_status=$?
set -e
assert_equals 1 "$missing_container_cooldown_status" "missing dependency cooldown remains observable"
assert_contains "retry cooldown active failures=1" "$TMP_DIR/missing-container/stdout-2" "missing dependency cooldown is observable"
assert_equals 1 "$(<"$TMP_DIR/missing-container/state.missing-postgres-failures")" "missing PostgreSQL cooldown prevents duplicate attempts"

set +e
run_case "$TMP_DIR/inspect-error" env \
  FAKE_INSPECT_MODE=error \
  FAKE_CURL_MODE=ping >"$TMP_DIR/inspect-error/stdout" 2>"$TMP_DIR/inspect-error/stderr"
inspect_error_status=$?
set -e
assert_equals 1 "$inspect_error_status" "temporary Docker inspect error remains observable"
assert_equals "postgres-container-inspect-failed" "$(<"$TMP_DIR/inspect-error/state")" "inspect error has a distinct PostgreSQL state"
assert_contains "state=postgres-container-inspect-failed" "$TMP_DIR/inspect-error/health.snapshot" "inspect error is snapshotted distinctly"
assert_contains "container inspect failed container=postgres" "$TMP_DIR/inspect-error/stderr" "inspect error is logged with container context"
if [ -e "$TMP_DIR/inspect-error/state.missing-postgres-failures" ] || \
   { [ -f "$TMP_DIR/inspect-error/docker.log" ] && rg -q '^(start|restart|unpause) ' "$TMP_DIR/inspect-error/docker.log"; }; then
  printf 'assertion failed: inspect error must not be treated as missing or submit a lifecycle action\n' >&2
  exit 1
fi

mkdir -p "$TMP_DIR/docker-start/app"
set +e
run_case "$TMP_DIR/docker-start" env \
  FAKE_CURL_MODE=fail \
  NEW_API_RECOVERY_DOCKER_APP_PATH="$TMP_DIR/docker-start/app" \
  NEW_API_RECOVERY_PGREP_BIN=/usr/bin/false \
  NEW_API_RECOVERY_OPEN_BIN="$FAKE_OPEN" \
  FAKE_OPEN_LOG="$TMP_DIR/docker-start/open.log" \
  NEW_API_RECOVERY_DOCKER_START_COOLDOWN_SECONDS=60 \
  NEW_API_RECOVERY_DOCKER_START_MAX_COOLDOWN_SECONDS=300 >"$TMP_DIR/docker-start/stdout" 2>"$TMP_DIR/docker-start/stderr"
docker_start_status=$?
set -e
assert_equals 0 "$docker_start_status" "Docker Desktop startup request does not block the health cycle"
assert_equals 1 "$(rg -c '^open ' "$TMP_DIR/docker-start/open.log")" "Docker Desktop startup is requested once"
assert_equals 1 "$(<"$TMP_DIR/docker-start/docker-start-failures")" "Docker Desktop startup attempt is counted"
assert_contains "action=docker-start" "$TMP_DIR/docker-start/health.snapshot" "snapshot records Docker Desktop startup action"
assert_contains "docker_start_attempts=1" "$TMP_DIR/docker-start/health.snapshot" "snapshot records startup attempt count"

set +e
run_case "$TMP_DIR/docker-start" env \
  FAKE_CURL_MODE=fail \
  NEW_API_RECOVERY_DOCKER_APP_PATH="$TMP_DIR/docker-start/app" \
  NEW_API_RECOVERY_PGREP_BIN=/usr/bin/false \
  NEW_API_RECOVERY_OPEN_BIN="$FAKE_OPEN" \
  FAKE_OPEN_LOG="$TMP_DIR/docker-start/open.log" \
  NEW_API_RECOVERY_DOCKER_START_COOLDOWN_SECONDS=60 \
  NEW_API_RECOVERY_DOCKER_START_MAX_COOLDOWN_SECONDS=300 >"$TMP_DIR/docker-start/stdout-2" 2>"$TMP_DIR/docker-start/stderr-2"
docker_start_cooldown_status=$?
set -e
assert_equals 0 "$docker_start_cooldown_status" "Docker Desktop startup cooldown does not block health checks"
assert_equals 1 "$(rg -c '^open ' "$TMP_DIR/docker-start/open.log")" "Docker Desktop startup cooldown prevents duplicate open"
assert_contains "startup cooldown active" "$TMP_DIR/docker-start/stdout-2" "Docker Desktop startup cooldown is observable"

mkdir -p "$TMP_DIR/docker-start-failure/app"
set +e
run_case "$TMP_DIR/docker-start-failure" env \
  FAKE_CURL_MODE=fail \
  NEW_API_RECOVERY_DOCKER_APP_PATH="$TMP_DIR/docker-start-failure/app" \
  NEW_API_RECOVERY_PGREP_BIN=/usr/bin/false \
  NEW_API_RECOVERY_OPEN_BIN="$FAKE_OPEN" \
  FAKE_OPEN_LOG="$TMP_DIR/docker-start-failure/open.log" \
  FAKE_OPEN_EXIT=1 \
  NEW_API_RECOVERY_DOCKER_START_COOLDOWN_SECONDS=0 >"$TMP_DIR/docker-start-failure/stdout" 2>"$TMP_DIR/docker-start-failure/stderr"
docker_start_failure_status=$?
set -e
assert_equals 0 "$docker_start_failure_status" "failed Docker Desktop startup request remains non-fatal"
assert_contains "startup request failed" "$TMP_DIR/docker-start-failure/stderr" "failed Docker Desktop startup is observable"
assert_equals 1 "$(<"$TMP_DIR/docker-start-failure/docker-start-failures")" "failed startup request is counted"

printf 'macos-docker-tun-recovery-watchdog tests passed\n'
