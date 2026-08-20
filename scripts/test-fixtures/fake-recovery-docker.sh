#!/usr/bin/env bash
set -euo pipefail

log_path="${FAKE_DOCKER_LOG:?FAKE_DOCKER_LOG is required}"
printf '%s\n' "$*" >>"$log_path"

case "${1:-}" in
  info)
    exit 0
    ;;
  inspect)
    if [ "${FAKE_INSPECT_MODE:-}" = "error" ]; then
      printf 'Error response from daemon: temporary inspect failure\n' >&2
      exit 1
    fi
    if [[ "$*" == *"{{.RestartCount}}"* ]]; then
      container_health="${FAKE_CONTAINER_HEALTH:-unhealthy}"
      if [[ "$*" == *"new-api"* ]] && [ -n "${FAKE_APP_CONTAINER_HEALTH:-}" ]; then
        container_health="$FAKE_APP_CONTAINER_HEALTH"
      fi
      printf 'state=%s running=%s restarting=false oom_killed=%s exit_code=%s error=%s started_at=2026-08-03T00:00:00Z finished_at=2026-08-02T23:59:00Z restart_count=%s restart_policy=%s restart_max=0 health=%s\n' \
        "${FAKE_CONTAINER_STATE:-running}" \
        "${FAKE_CONTAINER_RUNNING:-true}" \
        "${FAKE_OOM_KILLED:-false}" \
        "${FAKE_EXIT_CODE:-0}" \
        "${FAKE_STATE_ERROR:-}" \
        "${FAKE_RESTART_COUNT:-0}" \
        "${FAKE_RESTART_POLICY:-always}" \
        "$container_health"
      exit 0
    fi
    if [[ "$*" == *"{{.State.Status}}"* ]]; then
      printf '%s\n' "${FAKE_CONTAINER_STATE:-running}"
    elif [[ "$*" == *"{{range .NetworkSettings.Networks}}"* ]]; then
      printf '%s\n' "${FAKE_POSTGRES_HOST:-127.0.0.1}"
    else
      container_health="${FAKE_CONTAINER_HEALTH:-unhealthy}"
      if [[ "$*" == *"new-api"* ]] && [ -n "${FAKE_APP_CONTAINER_HEALTH:-}" ]; then
        container_health="$FAKE_APP_CONTAINER_HEALTH"
      fi
      printf '%s\n' "$container_health"
    fi
    ;;
  exec)
    if [[ "$*" == *"pg_isready"* ]]; then
      exit 0
    fi
    if [[ "$*" == *"psql"* ]]; then
      if [ "${FAKE_EXPECT_POSTGRES_STDIN:-0}" = "1" ]; then
        if ! IFS= read -r password; then
          printf 'postgres_auth_stdin_missing\n' >>"$log_path"
          exit 1
        fi
        if [ -n "${FAKE_EXPECT_POSTGRES_PASSWORD:-}" ] && [ "$password" != "$FAKE_EXPECT_POSTGRES_PASSWORD" ]; then
          printf 'postgres_auth_stdin_invalid\n' >>"$log_path"
          exit 1
        fi
      fi
      if [ "${FAKE_POSTGRES_MODE:-success}" = "query-fail" ]; then
        printf '0\n'
        exit 0
      fi
      printf '1\n'
      exit 0
    fi
    if [[ "$*" == *"redis-cli"* ]]; then
      if [ "${FAKE_EXPECT_REDIS_STDIN:-0}" = "1" ]; then
        if ! IFS= read -r password; then
          printf 'redis_auth_stdin_missing\n' >>"$log_path"
          exit 1
        fi
        if [ -n "${FAKE_EXPECT_REDIS_PASSWORD:-}" ] && [ "$password" != "$FAKE_EXPECT_REDIS_PASSWORD" ]; then
          printf 'redis_auth_stdin_invalid\n' >>"$log_path"
          exit 1
        fi
      fi
      if [ "${FAKE_REDIS_MODE:-success}" = "noauth" ]; then
        printf 'NOAUTH Authentication required\n'
        exit 0
      fi
      printf 'PONG\n'
      exit 0
    fi
    exit 1
    ;;
  restart)
    sleep "${FAKE_RESTART_SLEEP:-0}"
    exit "${FAKE_RESTART_EXIT:-0}"
    ;;
  desktop)
    if [[ "$*" == *"desktop stop"* ]]; then
      if [ -n "${FAKE_DAEMON_READY_FILE:-}" ]; then
        rm -f "$FAKE_DAEMON_READY_FILE"
      fi
      sleep "${FAKE_DESKTOP_STOP_SLEEP:-0}"
      status="${FAKE_DESKTOP_STOP_EXIT:-0}"
    else
      sleep "${FAKE_DESKTOP_START_SLEEP:-0}"
      status="${FAKE_DESKTOP_START_EXIT:-0}"
      if [ "$status" = "0" ] && [ -n "${FAKE_DAEMON_READY_FILE:-}" ]; then
        : >"$FAKE_DAEMON_READY_FILE"
      fi
    fi
    if [ "$status" != "0" ]; then
      printf 'fake Docker Desktop operation failed status=%s\n' "$status" >&2
    fi
    exit "$status"
    ;;
  start|unpause)
    exit "${FAKE_START_EXIT:-0}"
    ;;
  *)
    exit 0
    ;;
esac
