#!/usr/bin/env bash
set -euo pipefail

printf 'Filesystem 1024-blocks Used Available Capacity Mounted on\n'
printf 'fake 100 20 80 %s%% %s\n' "${FAKE_MONITOR_DF_USED_PERCENT:-20}" "${*: -1}"
