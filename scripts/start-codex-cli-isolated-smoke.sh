#!/bin/sh
set -eu

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <channel-id> <result-file>" >&2
  exit 2
fi

if [ -z "${NEWAPI_SMOKE_TOKEN:-}" ]; then
  echo "NEWAPI_SMOKE_TOKEN is required" >&2
  exit 2
fi

script_dir=$(cd "$(dirname "$0")" && pwd -P)
child_script="$script_dir/codex-cli-isolated-smoke.sh"
if [ ! -f "$child_script" ]; then
  echo "isolated smoke script is missing: $child_script" >&2
  exit 1
fi

channel_id=$1
result_file=$2
case "$channel_id" in
  *[!0-9]*|'')
    echo "channel-id must be numeric" >&2
    exit 2
    ;;
esac

tmp_root=$(cd -P /tmp 2>/dev/null && pwd -P) || {
  echo "unable to resolve /tmp" >&2
  exit 2
}
result_dir=$(cd "$(dirname "$result_file")" 2>/dev/null && pwd -P) || {
  echo "result-file parent must already exist under /tmp" >&2
  exit 2
}
if [ "$result_dir" != "$tmp_root" ]; then
  echo "result-file must be a direct child of /tmp" >&2
  exit 2
fi
result_basename=$(basename "$result_file")
if [ -z "$result_basename" ] || [ "$result_basename" = "." ] || [ "$result_basename" = ".." ]; then
  echo "result-file must name a regular file under /tmp" >&2
  exit 2
fi
result_file="$tmp_root/$result_basename"
log_file="${result_file}.log"
if [ -L "$result_file" ] || [ -L "$log_file" ]; then
  echo "result-file and its log must not be symbolic links" >&2
  exit 2
fi

printf 'status=running channel=%s\n' "$channel_id" > "$result_file"
NEWAPI_CLI_SMOKE_RESULT_FILE="$result_file" \
  nohup sh "$child_script" "$channel_id" >"$log_file" 2>&1 &
pid=$!
printf 'status=running channel=%s pid=%s\n' "$channel_id" "$pid" > "$result_file"
printf 'codex_cli_smoke_started channel=%s pid=%s log=%s\n' "$channel_id" "$pid" "$log_file"
