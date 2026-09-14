#!/usr/bin/env bash
set -euo pipefail

printf '%s %s\n' "${FAKE_MONITOR_LOG_DIR_SIZE_KB:-100}" "${*: -1}"
