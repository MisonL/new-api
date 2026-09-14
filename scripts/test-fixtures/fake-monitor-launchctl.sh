#!/usr/bin/env bash
set -euo pipefail

log_path="${FAKE_MONITOR_LAUNCHCTL_LOG:?FAKE_MONITOR_LAUNCHCTL_LOG is required}"
state_path="${FAKE_MONITOR_LAUNCHCTL_STATE:?FAKE_MONITOR_LAUNCHCTL_STATE is required}"
command_name="${1:-}"
shift || true
printf '%s %s\n' "$command_name" "$*" >>"$log_path"

case "$command_name" in
  print)
    if [ -f "$state_path" ]; then
      printf 'state = running\npid = 1000\n'
      exit 0
    fi
    exit 1
    ;;
  bootout)
    rm -f "$state_path"
    ;;
  bootstrap)
    printf 'bootstrapped\n' >"$state_path"
    ;;
  kickstart)
    [ -f "$state_path" ] || exit 1
    printf 'kickstarted\n' >"$state_path"
    ;;
  *)
    exit 1
    ;;
esac
