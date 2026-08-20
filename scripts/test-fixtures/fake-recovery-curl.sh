#!/usr/bin/env bash
set -euo pipefail

LAN_URL_FRAGMENT="${FAKE_LAN_URL_FRAGMENT:-10.0.90.200:3000}"

printf '%s\n' "$*" >>"${FAKE_CURL_LOG:?FAKE_CURL_LOG is required}"
has_write_format=0
write_format=""
previous_arg=""
for arg in "$@"; do
  if [ "$previous_arg" = "-w" ]; then
    write_format="$arg"
    break
  fi
  if [ "$arg" = "-w" ]; then
    has_write_format=1
  fi
  previous_arg="$arg"
done

if [ "$has_write_format" -eq 1 ]; then
  latency_seconds="${FAKE_MONITOR_LATENCY_SECONDS:-0.010}"
  latency_status=200
  case "${FAKE_CURL_MODE:-fail}" in
    ping)
      latency_status=500
      ;;
    backend-success-public-fail)
      if [[ "$*" != *"127.0.0.1:13000"* ]]; then
        latency_status=500
      fi
      ;;
    backend-fail-public-success)
      if [[ "$*" == *"127.0.0.1:13000"* ]]; then
        latency_status=500
      fi
      ;;
    lan-fail)
      if [[ "$*" == *"$LAN_URL_FRAGMENT"* ]]; then
        latency_status=500
      fi
      ;;
    success-non-200)
      latency_status=204
      ;;
    timeout)
      latency_status=000
      ;;
    fail)
      latency_status=500
      ;;
  esac
  if [ "$write_format" = "%{http_code} %{time_total}" ]; then
    printf '%s %s\n' "$latency_status" "$latency_seconds"
  else
    printf '%s\n' "$latency_status"
  fi
  if [ "$latency_status" -eq 200 ]; then
    exit 0
  fi
  if [ "${FAKE_CURL_MODE:-fail}" = "timeout" ]; then
    exit 28
  fi
  exit 22
fi

case "${FAKE_CURL_MODE:-fail}" in
  ping)
    if [[ "$*" == *"--unix-socket"* ]]; then
      printf 'OK\n'
      exit 0
    fi
    ;;
  daemon-fail-until-restart)
    if [ -n "${FAKE_DAEMON_READY_FILE:-}" ] && [ -f "$FAKE_DAEMON_READY_FILE" ]; then
      if [[ "$*" == *"--unix-socket"* ]]; then
        printf 'OK\n'
      fi
      exit 0
    fi
    exit 22
    ;;
  backend-success-public-fail)
    if [[ "$*" == *"--unix-socket"* ]]; then
      printf 'OK\n'
      exit 0
    fi
    if [[ "$*" == *"127.0.0.1:13000"* ]]; then
      exit 0
    fi
    ;;
  backend-fail-public-success)
    if [[ "$*" == *"--unix-socket"* ]]; then
      printf 'OK\n'
      exit 0
    fi
    if [[ "$*" == *"127.0.0.1:13000"* ]]; then
      exit 22
    fi
    exit 0
    ;;
  lan-fail)
    if [[ "$*" == *"--unix-socket"* ]]; then
      printf 'OK\n'
      exit 0
    fi
    if [[ "$*" == *"127.0.0.1:13000"* || "$*" == *"127.0.0.1:3000"* ]]; then
      exit 0
    fi
    ;;
  success)
    if [[ "$*" == *"--unix-socket"* ]]; then
      printf 'OK\n'
    fi
    exit 0
    ;;
  success-non-200)
    if [[ "$*" == *"--unix-socket"* ]]; then
      printf 'OK\n'
      exit 0
    fi
    exit 0
    ;;
  timeout)
    if [[ "$*" == *"--unix-socket"* ]]; then
      printf 'OK\n'
      exit 0
    fi
    exit 0
    ;;
esac
exit 22
