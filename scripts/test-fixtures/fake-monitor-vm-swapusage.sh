#!/usr/bin/env bash
set -euo pipefail

swap_percent="${FAKE_MONITOR_SWAP_PERCENT:-20}"
printf 'vm.swapusage: total = 100.00M used = %sM free = 80.00M (%s%%)\n' "$swap_percent" "$swap_percent"
