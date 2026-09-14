#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
  echo "usage: $0 <channel-id>" >&2
  exit 2
fi

if [ -z "${NEWAPI_SMOKE_TOKEN:-}" ]; then
  echo "NEWAPI_SMOKE_TOKEN is required" >&2
  exit 2
fi

channel_id=$1
case "$channel_id" in
  *[!0-9]*|'')
    echo "channel-id must be numeric" >&2
    exit 2
    ;;
esac

base_url=${NEWAPI_BASE_URL:-http://[::1]:3001}
base_url=${base_url%/}
model=${NEWAPI_SMOKE_MODEL:-gpt-5.6-sol}
codex_bin=${CODEX_BIN:-codex}
result_file=${NEWAPI_CLI_SMOKE_RESULT_FILE:-}
home_dir=$(mktemp -d)
work_dir=$(mktemp -d)
stage=initialization

write_result() {
  if [ -n "$result_file" ]; then
    printf '%s\n' "$1" > "$result_file"
  fi
}

cleanup() {
  status=$?
  if [ "$status" -ne 0 ]; then
    write_result "status=failed stage=${stage} exit=${status} channel=${channel_id} model=${model}"
  fi
  rm -rf "$home_dir" "$work_dir"
}
trap cleanup EXIT HUP INT TERM

git -C "$work_dir" init -q
export CODEX_HOME="$home_dir"
export NEWAPI_CHANNEL_TOKEN="${NEWAPI_SMOKE_TOKEN}-${channel_id}"

stage=codex_exec
"$codex_bin" \
  --ask-for-approval never \
  exec \
  --ephemeral \
  --ignore-user-config \
  --sandbox workspace-write \
  --model "$model" \
  --config 'model_provider="newapi_dev"' \
  --config 'model_providers.newapi_dev.name="New API isolated development"' \
  --config "model_providers.newapi_dev.base_url=\"${base_url}/v1\"" \
  --config 'model_providers.newapi_dev.env_key="NEWAPI_CHANNEL_TOKEN"' \
  --config 'model_providers.newapi_dev.wire_api="responses"' \
  --config 'model_providers.newapi_dev.requires_openai_auth=false' \
  --config 'model_providers.newapi_dev.request_max_retries=0' \
  --config 'model_providers.newapi_dev.stream_max_retries=0' \
  --cd "$work_dir" \
  'Create codex_cli_marker.txt in the current repository containing exactly CODEX_CLI_TOOL_OK and nothing else. Use the available coding tools to make the file, then give a concise final response.'

stage=marker_presence
if [ ! -f "$work_dir/codex_cli_marker.txt" ]; then
  echo "codex CLI completed without creating the requested marker" >&2
  exit 1
fi

stage=marker_content
if [ "$(tr -d '\r\n' < "$work_dir/codex_cli_marker.txt")" != "CODEX_CLI_TOOL_OK" ]; then
  echo "codex CLI marker content did not match" >&2
  exit 1
fi

write_result "status=passed channel=${channel_id} model=${model}"
echo "codex_cli_smoke=marker_verified channel=${channel_id} model=${model}"
