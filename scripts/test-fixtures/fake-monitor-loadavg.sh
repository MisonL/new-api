#!/usr/bin/env bash
set -euo pipefail

case "${*: -1}" in
  vm.loadavg)
    printf '{ %s 1 1 }\n' "${FAKE_MONITOR_LOADAVG:-16}"
    ;;
  hw.ncpu)
    printf '%s\n' "${FAKE_MONITOR_CPU_COUNT:-16}"
    ;;
  *)
    exit 1
    ;;
esac
