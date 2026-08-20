#!/usr/bin/env bash
set -euo pipefail

log_path="${FAKE_LAUNCHCTL_LOG:?FAKE_LAUNCHCTL_LOG is required}"
state_path="${FAKE_LAUNCHCTL_STATE:?FAKE_LAUNCHCTL_STATE is required}"
command_name="${1:-}"
shift || true
printf '%s %s\n' "$command_name" "$*" >>"$log_path"

case "$command_name" in
  print)
    if [ "${FAKE_LAUNCHCTL_PRINT_MODE:-success}" = "missing" ]; then
      exit 1
    fi
    if [ "${FAKE_LAUNCHCTL_JOB_DISAPPEARS:-0}" = "1" ] && \
       [ -f "$state_path" ] && [ "$(<"$state_path")" = "missing" ]; then
      exit 1
    fi
    if [ -f "$state_path" ] && [ "$(<"$state_path")" = "changed" ]; then
      printf 'state = running\npid = %s\n' "${FAKE_LAUNCHCTL_NEW_PID:-2000}"
    else
      printf 'state = running\npid = %s\n' "${FAKE_LAUNCHCTL_PID:-1000}"
    fi
    ;;
  kill)
    if [ "${FAKE_LAUNCHCTL_KILL_MODE:-success}" != "success" ]; then
      exit 1
    fi
    if [ "${FAKE_LAUNCHCTL_JOB_DISAPPEARS:-0}" = "1" ]; then
      printf 'missing\n' >"$state_path"
    elif [ "${FAKE_LAUNCHCTL_CHANGE_PID:-1}" = "1" ]; then
      printf 'changed\n' >"$state_path"
    fi
    ;;
  kickstart)
    printf 'kickstarted\n' >"$state_path"
    ;;
  bootstrap)
    ;;
  *)
    exit 1
    ;;
esac
