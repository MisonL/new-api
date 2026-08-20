#!/usr/bin/env bash
set -euo pipefail

printf 'open %s\n' "$*" >>"${FAKE_OPEN_LOG:?FAKE_OPEN_LOG is required}"
exit "${FAKE_OPEN_EXIT:-0}"
