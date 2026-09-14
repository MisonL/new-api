#!/usr/bin/env bash
set -Eeuo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/deploy-macos-docker-tun.sh --check-only
  scripts/deploy-macos-docker-tun.sh --apply

Environment overrides:
  PROD_ENV_FILE=.env
  TUN_ENV_FILE=deploy/env/macos-docker-tun.env
  CADDYFILE=deploy/proxy/macos-docker-tun/Caddyfile
  COMPOSE_FILE_PATH=docker-compose.yml
  NEW_API_COMPOSE_PROJECT_NAME=
  COMPOSE_SERVICE=new-api
  NEW_API_LAN_PROXY_PORT=3000
  NEW_API_LOOPBACK_PORT=13000
  NEW_API_CONTAINER_PORT=3000
  NEW_API_HEALTH_PATH=/api/status
  NEW_API_DEFAULT_PORT_MAPPING=3000:3000
  NEW_API_PORT_MAPPING=127.0.0.1:13000:3000
  NEW_API_ABNORMAL_TUN_IP_REGEX=
  NEW_API_ALLOW_ACTIVE_REQUESTS=0
  NEW_API_ACTIVE_CONNECTION_DRAIN_TIMEOUT=30
  NEW_API_ALLOW_IMAGE_CHANGE=0
  NEW_API_RELEASE_APPROVAL_FILE=
  NEW_API_EXPECTED_IMAGE=
  NEW_API_EXPECTED_REVISION=
  CADDY_LOG_FILE="$HOME/Library/Application Support/New API/caddy/<COMPOSE_SERVICE>-macos-tun-caddy-<LAN_PORT>.log"
  CADDY_RUNTIME_CONFIG_FILE="$HOME/Library/Application Support/New API/caddy/<COMPOSE_SERVICE>-macos-tun.Caddyfile"
  CADDY_PID_FILE=/tmp/<COMPOSE_SERVICE>-macos-tun-caddy-<LAN_PORT>.pid
  NEW_API_CADDY_EXIT_TIMEOUT_SECONDS=30
  NEW_API_CADDY_THROTTLE_INTERVAL_SECONDS=10
  NEW_API_LSOF_TIMEOUT_SECONDS=3
  CADDY_LAUNCHD_LABEL=com.new-api.macos-tun.<COMPOSE_SERVICE>.<LAN_PORT>
  CADDY_LAUNCHD_PLIST=~/Library/LaunchAgents/<LABEL>.plist
EOF
}

if [[ $# -ne 1 ]]; then
  usage
  exit 2
fi

MODE="$1"
case "$MODE" in
  --check-only) APPLY=0 ;;
  --apply) APPLY=1 ;;
  *) usage; exit 2 ;;
esac
ROLLBACK_NEEDED=0
ROLLBACK_RUNNING=0
VALIDATED_IMAGE_REF=""
VALIDATED_IMAGE_ID=""
BASELINE_CONTAINER_IMAGE_ID=""

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

PROD_ENV_FILE="${PROD_ENV_FILE:-.env}"
TUN_ENV_FILE="${TUN_ENV_FILE:-deploy/env/macos-docker-tun.env}"
CADDYFILE="${CADDYFILE:-deploy/proxy/macos-docker-tun/Caddyfile}"
COMPOSE_FILE_PATH="${COMPOSE_FILE_PATH:-docker-compose.yml}"

normalize_tun_env_value() {
  local value="$1"

  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  case "$value" in
    \"*)
      if [[ "$value" =~ ^\"(.*)\"[[:space:]]*(#.*)?$ ]]; then
        value="${BASH_REMATCH[1]}"
      fi
      ;;
    \'*)
      if [[ "$value" =~ ^\'(.*)\'[[:space:]]*(#.*)?$ ]]; then
        value="${BASH_REMATCH[1]}"
      fi
      ;;
    *)
      if [[ "$value" == \#* ]]; then
        value=""
      elif [[ "$value" =~ ^(.*[^[:space:]])[[:space:]]+#.*$ ]]; then
        value="${BASH_REMATCH[1]}"
      fi
      ;;
  esac
  printf '%s' "$value"
}

tun_env_value() {
  local name="$1"
  local fallback="$2"
  local line value

  if [[ -f "$TUN_ENV_FILE" ]]; then
    while IFS= read -r line || [[ -n "$line" ]]; do
      line="${line#"${line%%[![:space:]]*}"}"
      if [[ "$line" =~ ^export[[:space:]]+(.+)$ ]]; then
        line="${BASH_REMATCH[1]}"
      fi
      [[ "$line" == "$name="* ]] || continue
      value="${line#*=}"
      normalize_tun_env_value "$value"
      return 0
    done <"$TUN_ENV_FILE"
  fi

  printf '%s' "$fallback"
}

expand_home_path() {
  local path="$1"

  case "$path" in
    \~) printf '%s' "$HOME" ;;
    \~/*) printf '%s/%s' "$HOME" "${path#~/}" ;;
    *) printf '%s' "$path" ;;
  esac
}

resolve_repo_path() {
  local path="$1"

  case "$path" in
    /*) printf '%s' "$path" ;;
    \~) printf '%s' "$HOME" ;;
    \~/*) printf '%s/%s' "$HOME" "${path#~/}" ;;
    *) printf '%s/%s' "$ROOT_DIR" "$path" ;;
  esac
}

CADDYFILE="$(resolve_repo_path "$CADDYFILE")"

COMPOSE_SERVICE="${COMPOSE_SERVICE:-$(tun_env_value COMPOSE_SERVICE new-api)}"
LAN_PORT="${NEW_API_LAN_PROXY_PORT:-$(tun_env_value NEW_API_LAN_PROXY_PORT 3000)}"
LOOPBACK_PORT="${NEW_API_LOOPBACK_PORT:-$(tun_env_value NEW_API_LOOPBACK_PORT 13000)}"
CONTAINER_PORT="${NEW_API_CONTAINER_PORT:-$(tun_env_value NEW_API_CONTAINER_PORT 3000)}"
HEALTH_PATH="${NEW_API_HEALTH_PATH:-$(tun_env_value NEW_API_HEALTH_PATH /api/status)}"
ABNORMAL_TUN_IP_REGEX="${NEW_API_ABNORMAL_TUN_IP_REGEX:-$(tun_env_value NEW_API_ABNORMAL_TUN_IP_REGEX '')}"
ALLOW_ACTIVE_REQUESTS="${NEW_API_ALLOW_ACTIVE_REQUESTS:-$(tun_env_value NEW_API_ALLOW_ACTIVE_REQUESTS 0)}"
ACTIVE_CONNECTION_DRAIN_TIMEOUT="${NEW_API_ACTIVE_CONNECTION_DRAIN_TIMEOUT:-$(tun_env_value NEW_API_ACTIVE_CONNECTION_DRAIN_TIMEOUT 30)}"
ALLOW_IMAGE_CHANGE="${NEW_API_ALLOW_IMAGE_CHANGE:-$(tun_env_value NEW_API_ALLOW_IMAGE_CHANGE 0)}"
RELEASE_APPROVAL_FILE="${NEW_API_RELEASE_APPROVAL_FILE:-$(tun_env_value NEW_API_RELEASE_APPROVAL_FILE '')}"
EXPECTED_IMAGE="${NEW_API_EXPECTED_IMAGE:-$(tun_env_value NEW_API_EXPECTED_IMAGE '')}"
EXPECTED_REVISION="${NEW_API_EXPECTED_REVISION:-$(tun_env_value NEW_API_EXPECTED_REVISION '')}"
CONFIGURED_PORT_MAPPING="${NEW_API_PORT_MAPPING:-$(tun_env_value NEW_API_PORT_MAPPING '')}"
PORT_MAPPING="127.0.0.1:${LOOPBACK_PORT}:${CONTAINER_PORT}"
DEFAULT_PORT_MAPPING="${NEW_API_DEFAULT_PORT_MAPPING:-$(tun_env_value NEW_API_DEFAULT_PORT_MAPPING "${LAN_PORT}:${CONTAINER_PORT}")}"
HEALTH_URL="http://127.0.0.1:${LAN_PORT}${HEALTH_PATH}"
BACKEND_URL="http://127.0.0.1:${LOOPBACK_PORT}${HEALTH_PATH}"
CADDY_LOG_ROOT="${HOME}/Library/Application Support/New API/caddy"
CADDY_LOG_FILE="${CADDY_LOG_FILE:-${CADDY_LOG_ROOT}/${COMPOSE_SERVICE}-macos-tun-caddy-${LAN_PORT}.log}"
CADDY_RUNTIME_CONFIG_FILE="${CADDY_RUNTIME_CONFIG_FILE:-$(tun_env_value CADDY_RUNTIME_CONFIG_FILE "${CADDY_LOG_ROOT}/${COMPOSE_SERVICE}-macos-tun.Caddyfile")}"
CADDY_PID_FILE="${CADDY_PID_FILE:-/tmp/${COMPOSE_SERVICE}-macos-tun-caddy-${LAN_PORT}.pid}"
CADDY_EXIT_TIMEOUT_SECONDS="${NEW_API_CADDY_EXIT_TIMEOUT_SECONDS:-30}"
CADDY_THROTTLE_INTERVAL_SECONDS="${NEW_API_CADDY_THROTTLE_INTERVAL_SECONDS:-10}"
LSOF_TIMEOUT_SECONDS="${NEW_API_LSOF_TIMEOUT_SECONDS:-3}"
LSOF_BIN=""
CADDY_LAUNCHD_LABEL="${CADDY_LAUNCHD_LABEL:-$(tun_env_value CADDY_LAUNCHD_LABEL "com.new-api.macos-tun.${COMPOSE_SERVICE}.${LAN_PORT}")}"
CADDY_LAUNCHD_PLIST="${CADDY_LAUNCHD_PLIST:-$(tun_env_value CADDY_LAUNCHD_PLIST "${HOME}/Library/LaunchAgents/${CADDY_LAUNCHD_LABEL}.plist")}"
CADDY_LAUNCHD_PLIST="$(expand_home_path "$CADDY_LAUNCHD_PLIST")"
CADDY_RUNTIME_CONFIG_FILE="$(expand_home_path "$CADDY_RUNTIME_CONFIG_FILE")"
RELEASE_APPROVAL_FILE="$(expand_home_path "$RELEASE_APPROVAL_FILE")"
LAUNCHD_DOMAIN="gui/$(id -u)"

COMPOSE_ENV_ARGS=()
TUN_COMPOSE_ENV_ARGS=()
if [[ -f "$PROD_ENV_FILE" ]]; then
  COMPOSE_ENV_ARGS+=(--env-file "$PROD_ENV_FILE")
  TUN_COMPOSE_ENV_ARGS+=(--env-file "$PROD_ENV_FILE")
else
  COMPOSE_ENV_ARGS+=(--env-file /dev/null)
  TUN_COMPOSE_ENV_ARGS+=(--env-file /dev/null)
fi

if [[ -f "$TUN_ENV_FILE" ]]; then
  TUN_COMPOSE_ENV_ARGS+=(--env-file "$TUN_ENV_FILE")
fi

log() {
  printf '[macos-tun] %s\n' "$*"
}

docker_compose() {
  if [[ -n "${NEW_API_COMPOSE_PROJECT_NAME:-}" ]]; then
    env -u COMPOSE_FILE -u COMPOSE_PROJECT_NAME \
      docker compose -p "$NEW_API_COMPOSE_PROJECT_NAME" -f "$COMPOSE_FILE_PATH" "$@"
  else
    env -u COMPOSE_FILE -u COMPOSE_PROJECT_NAME \
      docker compose -f "$COMPOSE_FILE_PATH" "$@"
  fi
}

fail() {
  printf '[macos-tun] ERROR: %s\n' "$*" >&2
  run_rollback_if_needed
  exit 1
}

run_rollback_if_needed() {
  if [[ "$APPLY" -eq 1 && "$ROLLBACK_NEEDED" -eq 1 && "$ROLLBACK_RUNNING" -eq 0 ]]; then
    ROLLBACK_RUNNING=1
    if ! rollback; then
      log "warning: rollback did not complete cleanly; inspect Docker and Caddy state manually"
    fi
  fi
}

compose_default() {
  if (( ${#COMPOSE_ENV_ARGS[@]} > 0 )); then
    NEW_API_PORT_MAPPING="$DEFAULT_PORT_MAPPING" docker_compose "${COMPOSE_ENV_ARGS[@]}" "$@"
  else
    NEW_API_PORT_MAPPING="$DEFAULT_PORT_MAPPING" docker_compose "$@"
  fi
}

compose_tun() {
  if (( ${#TUN_COMPOSE_ENV_ARGS[@]} > 0 )); then
    NEW_API_PORT_MAPPING="$PORT_MAPPING" docker_compose "${TUN_COMPOSE_ENV_ARGS[@]}" "$@"
  else
    NEW_API_PORT_MAPPING="$PORT_MAPPING" docker_compose "$@"
  fi
}

compose_service_container_id() {
  local container_id
  container_id="$(compose_tun ps -q "$COMPOSE_SERVICE" 2>/dev/null || true)"
  [[ -n "$container_id" ]] || fail "compose service ${COMPOSE_SERVICE} has no container id"
  printf '%s' "$container_id"
}

current_service_container_id() {
  local container_id

  container_id="$(compose_tun ps -a -q "$COMPOSE_SERVICE" 2>/dev/null | head -n 1 || true)"
  [[ "$container_id" =~ ^[[:xdigit:]]{12,64}$ ]] || return 1
  printf '%s' "$container_id"
}

compose_service_image_ref() {
  local mode="$1"
  local config_json

  if [[ "$mode" == "default" ]]; then
    config_json="$(compose_default config --format json)" || return 1
  else
    config_json="$(compose_tun config --format json)" || return 1
  fi
  printf '%s\n' "$config_json" \
    | COMPOSE_SERVICE="$COMPOSE_SERVICE" python3 -c '
import json
import os
import sys

data = json.load(sys.stdin)
service = data.get("services", {}).get(os.environ["COMPOSE_SERVICE"])
if not service or not service.get("image"):
    raise SystemExit(1)
print(service["image"])
'
}

container_image_metadata() {
  local container_id="$1"

  docker inspect "$container_id" --format '{{.Config.Image}}|{{.Image}}|{{index .Config.Labels "org.opencontainers.image.revision"}}'
}

image_release_approval_is_valid() {
  local approval_file="$1"
  local image_ref="$2"
  local revision="$3"

  [[ -f "$approval_file" ]] || return 1
  [[ ! -L "$approval_file" ]] || return 1
  [[ "$(stat -f '%Su' "$approval_file" 2>/dev/null || true)" == "$(id -un)" ]] || return 1
  [[ "$(stat -f '%Lp' "$approval_file" 2>/dev/null || true)" == "600" ]] || return 1
  python3 - "$approval_file" "$image_ref" "$revision" <<'PY'
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
expected_image = sys.argv[2]
expected_revision = sys.argv[3]
fields = {}
for raw_line in path.read_text(encoding="utf-8").splitlines():
    line = raw_line.strip()
    if not line or line.startswith("#"):
        continue
    key, separator, value = line.partition("=")
    if not separator or key not in {"status", "image", "revision", "worktree"}:
        raise SystemExit(1)
    if key in fields:
        raise SystemExit(1)
    fields[key] = value.strip()

required = {
    "status": "approved",
    "image": expected_image,
    "revision": expected_revision,
    "worktree": "clean",
}
if any(fields.get(key) != value for key, value in required.items()):
    raise SystemExit(1)
PY
}

validate_release_image_guard() {
  local container_id current_image current_image_id current_revision
  local tun_image default_image rendered_image rendered_image_id rendered_revision
  local worktree_status

  container_id="$(current_service_container_id)" || fail "cannot establish the existing ${COMPOSE_SERVICE} container image baseline"
  IFS='|' read -r current_image current_image_id current_revision \
    <<<"$(container_image_metadata "$container_id")" \
    || fail "cannot inspect the existing ${COMPOSE_SERVICE} image provenance"
  [[ -n "$current_image" && "$current_image_id" == sha256:* ]] \
    || fail "existing ${COMPOSE_SERVICE} container has no usable image reference or digest"
  [[ "$current_revision" =~ ^[0-9a-f]{40}$ ]] \
    || fail "existing ${COMPOSE_SERVICE} image revision is missing or not a clean commit"

  tun_image="$(compose_service_image_ref tun)" \
    || fail "cannot determine the TUN Compose image"
  default_image="$(compose_service_image_ref default)" \
    || fail "cannot determine the rollback Compose image"
  [[ "$tun_image" == "$default_image" ]] \
    || fail "TUN and rollback Compose configurations resolve different images"
  rendered_image="$tun_image"

  if [[ -n "$EXPECTED_IMAGE" && "$rendered_image" != "$EXPECTED_IMAGE" ]]; then
    fail "rendered image ${rendered_image} does not match NEW_API_EXPECTED_IMAGE"
  fi
  if [[ -n "$EXPECTED_REVISION" && "$current_revision" != "$EXPECTED_REVISION" ]]; then
    fail "current image revision does not match NEW_API_EXPECTED_REVISION"
  fi

  rendered_image_id="$(docker image inspect "$rendered_image" --format '{{.Id}}' 2>/dev/null || true)"
  [[ "$rendered_image_id" == sha256:* ]] \
    || fail "rendered image ${rendered_image} is not available locally with a digest"

  VALIDATED_IMAGE_REF="$rendered_image"
  VALIDATED_IMAGE_ID="$rendered_image_id"
  BASELINE_CONTAINER_IMAGE_ID="$current_image_id"

  worktree_status="$(git status --porcelain=v1 --untracked-files=all)"
  if [[ -n "$worktree_status" ]]; then
    if [[ "$APPLY" -eq 1 ]]; then
      fail "refusing production apply with a dirty worktree; commit or stash all tracked and untracked changes before --apply"
    fi
    log "dirty worktree detected; check-only will not permit a production apply"
  fi

  if [[ "$rendered_image" == "$current_image" && "$rendered_image_id" == "$current_image_id" ]]; then
    if [[ -z "$worktree_status" ]]; then
      log "release image guard passed with clean worktree and unchanged image digest"
    fi
    return 0
  fi

  [[ "$ALLOW_IMAGE_CHANGE" == "1" ]] \
    || fail "refusing image change from ${current_image}@${current_image_id} to ${rendered_image}@${rendered_image_id}; set NEW_API_ALLOW_IMAGE_CHANGE=1 only with release approval"
  worktree_status="$(git status --porcelain=v1 --untracked-files=all)"
  [[ -z "$worktree_status" ]] \
    || fail "refusing image change while the worktree is dirty; release approval requires a clean worktree"
  rendered_revision="$(docker image inspect "$rendered_image" --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' 2>/dev/null || true)"
  [[ "$rendered_revision" =~ ^[0-9a-f]{40}$ ]] \
    || fail "candidate image ${rendered_image} has no clean OCI revision label"
  [[ -n "$RELEASE_APPROVAL_FILE" ]] \
    || fail "NEW_API_RELEASE_APPROVAL_FILE is required when changing the production image"
  image_release_approval_is_valid "$RELEASE_APPROVAL_FILE" "$rendered_image" "$rendered_revision" \
    || fail "release approval file is invalid or does not match the candidate image"
  log "release approval passed for image ${rendered_image} revision ${rendered_revision}"
}

assert_validated_image_before_apply() {
  local container_id current_image_id rendered_image_id

  [[ -n "$VALIDATED_IMAGE_REF" && -n "$VALIDATED_IMAGE_ID" && -n "$BASELINE_CONTAINER_IMAGE_ID" ]] \
    || fail "production image guard did not establish an apply baseline"
  rendered_image_id="$(docker image inspect "$VALIDATED_IMAGE_REF" --format '{{.Id}}' 2>/dev/null || true)"
  [[ "$rendered_image_id" == "$VALIDATED_IMAGE_ID" ]] \
    || fail "candidate image digest changed after provenance validation"
  container_id="$(current_service_container_id)" \
    || fail "existing ${COMPOSE_SERVICE} container disappeared before apply"
  current_image_id="$(docker inspect "$container_id" --format '{{.Image}}' 2>/dev/null || true)"
  [[ "$current_image_id" == "$BASELINE_CONTAINER_IMAGE_ID" ]] \
    || fail "existing ${COMPOSE_SERVICE} container image changed after provenance validation"
}

env_file_key_has_value() {
  local file="$1"
  local key="$2"
  local line value

  [[ -f "$file" ]] || return 1
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line#"${line%%[![:space:]]*}"}"
    [[ "$line" == export\ * ]] && line="${line#export }"
    [[ "$line" == "$key="* ]] || continue
    value="$(normalize_tun_env_value "${line#*=}")"
    [[ -n "$value" ]] && return 0
  done <"$file"
  return 1
}

effective_compose_env_key_has_value() {
  local key="$1"
  local value

  value="$(printenv "$key" 2>/dev/null || true)"
  [[ -n "$value" ]] && return 0
  env_file_key_has_value "$PROD_ENV_FILE" "$key" && return 0
  env_file_key_has_value "$TUN_ENV_FILE" "$key"
}

compose_required_env_keys() {
  python3 - "$COMPOSE_FILE_PATH" <<'PY'
import re
import sys

with open(sys.argv[1], encoding="utf-8") as compose_file:
    text = compose_file.read()
text = "\n".join(
    line for line in text.splitlines() if not line.lstrip().startswith("#")
)
keys = sorted(
    set(re.findall(r"\$\{([A-Za-z_][A-Za-z0-9_]*)\s*:\?", text))
)
print("\n".join(keys))
PY
}

report_compose_config_error() {
  local scope="$1"
  local output="$2"
  local missing_keys

  printf '%s\n' "$output" >&2
  missing_keys="$(
    {
      printf '%s\n' "$output" | python3 -c '
import re
import sys

text = sys.stdin.read()
patterns = (
    r"required variable ([A-Za-z_][A-Za-z0-9_]*) is missing",
    r"variable ([A-Za-z_][A-Za-z0-9_]*) is not set",
)
keys = set()
for pattern in patterns:
    keys.update(re.findall(pattern, text, flags=re.IGNORECASE))
for key in sorted(keys):
    print(key)
' 2>/dev/null || true
      while IFS= read -r key; do
        [[ -n "$key" ]] || continue
        if ! effective_compose_env_key_has_value "$key"; then
          printf '%s\n' "$key"
        fi
      done < <(compose_required_env_keys 2>/dev/null || true)
    } | sort -u | python3 -c 'import sys; print(", ".join(line.strip() for line in sys.stdin if line.strip()))'
  )"
  if [[ -n "$missing_keys" ]]; then
    printf '[macos-tun] missing required environment keys for %s: %s\n' "$scope" "$missing_keys" >&2
    printf '[macos-tun] set these keys in %s; credential values are intentionally not printed\n' "$PROD_ENV_FILE" >&2
  fi
}

current_service_publishes_port() {
  local expected_port="$1"
  local expected_target="$2"
  local expected_host="${3:-}"
  local container_id

  container_id="$(compose_tun ps -q "$COMPOSE_SERVICE" 2>/dev/null || true)"
  [[ -n "$container_id" ]] || return 1

  docker inspect "$container_id" --format '{{json .NetworkSettings.Ports}}' 2>/dev/null \
    | EXPECTED_PORT="$expected_port" EXPECTED_TARGET="$expected_target" EXPECTED_HOST="$expected_host" python3 -c '
import json
import os
import sys

data = json.load(sys.stdin)
expected_port = os.environ["EXPECTED_PORT"]
expected_target = os.environ["EXPECTED_TARGET"]
expected_host = os.environ["EXPECTED_HOST"]
entries = data.get(f"{expected_target}/tcp") or []
for item in entries:
    if item.get("HostPort") != expected_port:
        continue
    if expected_host and item.get("HostIp") != expected_host:
        continue
    sys.exit(0)
sys.exit(1)
'
}

current_service_has_loopback_port() {
  current_service_publishes_port "$LOOPBACK_PORT" "$CONTAINER_PORT" "127.0.0.1"
}

validate_required_service_env() {
  local mode="$1"
  local reject_placeholders="$2"
  local config_json

  if [[ "$mode" == "default" ]]; then
    config_json="$(compose_default config --format json)" || return 1
  else
    config_json="$(compose_tun config --format json)" || return 1
  fi

  printf '%s\n' "$config_json" \
    | COMPOSE_SERVICE="$COMPOSE_SERVICE" REJECT_PLACEHOLDERS="$reject_placeholders" python3 -c '
import json
import os
import sys

data = json.load(sys.stdin)
service = data["services"].get(os.environ["COMPOSE_SERVICE"])
if service is None:
    sys.exit(1)

raw_env = service.get("environment") or {}
if isinstance(raw_env, dict):
    env = {key: "" if value is None else str(value) for key, value in raw_env.items()}
else:
    env = {}
    for item in raw_env:
        key, sep, value = str(item).partition("=")
        env[key] = value if sep else ""

required = ("SESSION_SECRET", "CRYPTO_SECRET")
if any(not env.get(name) for name in required):
    sys.exit(1)

if os.environ["REJECT_PLACEHOLDERS"] == "1":
    placeholders = {
        "dummy",
        "changeme",
        "change-me",
        "please-change-me",
        "random_string",
        "your_session_secret_here",
        "your_crypto_secret_here",
    }
    for name in required:
        if env[name].strip().lower() in placeholders:
            sys.exit(1)
	'
}

validate_tun_env_scope() {
  local line raw_key key seen_keys
  seen_keys="|"

  [[ -f "$TUN_ENV_FILE" ]] || return 0
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line#"${line%%[![:space:]]*}"}"
    line="${line%"${line##*[![:space:]]}"}"
    [[ -z "$line" || "$line" == \#* ]] && continue
    if [[ "$line" =~ ^export[[:space:]]+(.+)$ ]]; then
      line="${BASH_REMATCH[1]}"
    fi
    [[ "$line" == *=* ]] || fail "unsupported line in TUN env file: ${line}"
    raw_key="${line%%=*}"
    key="${raw_key%"${raw_key##*[![:space:]]}"}"
    [[ "$raw_key" == "$key" ]] || fail "unsupported whitespace before = in TUN env file: ${line}"
    [[ "$key" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || fail "unsupported key in TUN env file: ${key}"
    [[ "$seen_keys" != *"|${key}|"* ]] || fail "duplicate key in TUN env file: ${key}"
    seen_keys="${seen_keys}${key}|"
    case "$key" in
      COMPOSE_SERVICE | \
        NEW_API_LAN_PROXY_PORT | \
        NEW_API_LOOPBACK_PORT | \
        NEW_API_CONTAINER_PORT | \
        NEW_API_HEALTH_PATH | \
        NEW_API_DEFAULT_PORT_MAPPING | \
        NEW_API_ALLOW_ACTIVE_REQUESTS | \
        NEW_API_ACTIVE_CONNECTION_DRAIN_TIMEOUT | \
        NEW_API_LSOF_TIMEOUT_SECONDS | \
        NEW_API_ABNORMAL_TUN_IP_REGEX | \
        NEW_API_PORT_MAPPING | \
        CADDY_LAUNCHD_LABEL | \
        CADDY_LAUNCHD_PLIST | \
        CADDY_RUNTIME_CONFIG_FILE)
        ;;
      *)
        fail "TUN env file must not define ${key}; put runtime settings in ${PROD_ENV_FILE}"
        ;;
    esac
  done <"$TUN_ENV_FILE"
}

validate_default_port_mapping() {
  local config_json

  if ! config_json="$(compose_default config --format json 2>&1)"; then
    report_compose_config_error "default rollback" "$config_json"
    return 2
  fi
  printf '%s\n' "$config_json" \
    | COMPOSE_SERVICE="$COMPOSE_SERVICE" EXPECTED_PORT="$LAN_PORT" EXPECTED_TARGET="$CONTAINER_PORT" python3 -c '
import json
import os
import sys
from ipaddress import ip_address

data = json.load(sys.stdin)
service_name = os.environ["COMPOSE_SERVICE"]
service = data["services"].get(service_name)
if service is None:
    sys.exit(2)
ports = service.get("ports", [])
expected_port = os.environ["EXPECTED_PORT"]
expected_target = int(os.environ["EXPECTED_TARGET"])
if len(ports) != 1:
    sys.exit(1)
item = ports[0]
host_ip = item.get("host_ip", "")
if host_ip:
    try:
        if ip_address(host_ip).is_loopback:
            sys.exit(1)
    except ValueError:
        sys.exit(1)
if (
    str(item.get("published")) == expected_port
    and int(item.get("target")) == expected_target
    and item.get("protocol", "tcp") == "tcp"
):
    sys.exit(0)
sys.exit(1)
  '
}

run_lsof() {
  LSOF_BIN="$LSOF_BIN" LSOF_TIMEOUT_SECONDS="$LSOF_TIMEOUT_SECONDS" python3 - "$@" <<'PY'
import os
import subprocess
import sys

binary = os.environ.get("LSOF_BIN") or "/usr/sbin/lsof"
timeout = float(os.environ.get("LSOF_TIMEOUT_SECONDS", "3"))
try:
    result = subprocess.run(
        [binary, *sys.argv[1:]],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        timeout=timeout,
        check=False,
    )
except subprocess.TimeoutExpired:
    print(f"lsof timed out after {timeout:g}s", file=sys.stderr)
    raise SystemExit(124)

sys.stdout.write(result.stdout)
sys.stderr.write(result.stderr)
raise SystemExit(result.returncode)
PY
}

lsof_listener_pids() {
  local port="$1"
  local output status

  if output="$(run_lsof -nP -iTCP:"$port" -sTCP:LISTEN -t 2>/dev/null)"; then
    printf '%s\n' "$output" | sort -u
    return 0
  else
    status=$?
  fi
  [[ "$status" -eq 1 ]] || return "$status"
  return 0
}

lsof_established_output() {
  local pid="$1"
  local port="$2"
  local output status

  if output="$(run_lsof -nP -a -p "$pid" -iTCP:"$port" -sTCP:ESTABLISHED 2>/dev/null)"; then
    printf '%s' "$output"
    return 0
  else
    status=$?
  fi
  [[ "$status" -eq 1 ]] || return "$status"
  return 0
}

lsof_has_listener() {
  local port="$1"
  local output status

  if output="$(run_lsof -nP -iTCP:"$port" -sTCP:LISTEN 2>/dev/null)"; then
    [[ -n "$output" ]]
    return $?
  else
    status=$?
  fi
  [[ "$status" -eq 1 ]] && return 1
  return "$status"
}

current_service_active_connections() {
  local established_output owner_pids pid total

  total=0
  owner_pids="$(lsof_listener_pids "$LAN_PORT")" || return $?
  [[ -n "$owner_pids" ]] || {
    printf '0'
    return 0
  }

  while IFS= read -r pid; do
    [[ "$pid" =~ ^[0-9]+$ ]] || continue
    established_output="$(lsof_established_output "$pid" "$LAN_PORT")" || return $?
    if [[ -n "$established_output" ]]; then
      total=$((total + $(printf '%s\n' "$established_output" | tail -n +2 | wc -l | tr -d '[:space:]')))
    fi
  done <<<"$owner_pids"

  printf '%s' "$total"
}

print_active_connection_sample() {
  local established_output owner_pids pid

  owner_pids="$(lsof_listener_pids "$LAN_PORT")" || return $?
  [[ -n "$owner_pids" ]] || return 0

  while IFS= read -r pid; do
    [[ "$pid" =~ ^[0-9]+$ ]] || continue
    established_output="$(lsof_established_output "$pid" "$LAN_PORT")" || return $?
    [[ -n "$established_output" ]] || continue
    printf '%s\n' "$established_output" | awk '
          NR == 1 { next }
          shown >= 20 { skipped = 1; next }
          {
            name = $9
            for (i = 10; i <= NF; i++) {
              name = name " " $i
            }
            printf "  %s pid=%s fd=%s %s %s\n", $1, $2, $4, $8, name
            shown++
          }
          END {
            if (skipped) {
              print "  ..."
            }
          }
        '
  done <<<"$owner_pids"
}

wait_for_active_connections_to_drain() {
  local timeout="$1"
  local active_connections start

  start="$(date +%s)"
  while true; do
    active_connections="$(current_service_active_connections)"
    [[ "$active_connections" == "0" ]] && return 0
    if (( "$(date +%s)" - start >= timeout )); then
      return 1
    fi
    sleep 1
  done
}

managed_caddy_is_running() {
  local pid
  pid="$(managed_caddy_pid || true)"
  if [[ -z "$pid" ]]; then
    return 1
  fi
  if ! kill -0 "$pid" >/dev/null 2>&1; then
    return 1
  fi
  managed_caddy_command_matches "$pid" && managed_caddy_owns_lan_port "$pid"
}

lan_port_owner_is_expected() {
  if current_service_publishes_port "$LAN_PORT" "$CONTAINER_PORT"; then
    return 0
  fi
  if managed_caddy_is_running; then
    return 0
  fi
  return 1
}

run_caddy() {
  NEW_API_LAN_PROXY_PORT="$LAN_PORT" \
    NEW_API_LOOPBACK_PORT="$LOOPBACK_PORT" \
    caddy "$@"
}

xml_escape() {
  python3 -c '
import html
import sys

print(html.escape(sys.argv[1], quote=True))
' "$1"
}

validate_port() {
  local name="$1"
  local value="$2"
  local numeric

  [[ "$value" =~ ^[0-9]+$ ]] || fail "${name} must be a TCP port number"
  numeric=$((10#$value))
  (( numeric >= 1 && numeric <= 65535 )) || fail "${name} must be between 1 and 65535"
}

managed_caddy_pid() {
  [[ -f "$CADDY_PID_FILE" ]] || return 1
  local pid
  pid="$(<"$CADDY_PID_FILE")"
  pid="${pid#"${pid%%[![:space:]]*}"}"
  pid="${pid%"${pid##*[![:space:]]}"}"
  [[ "$pid" =~ ^[0-9]+$ ]] || return 1
  (( 10#$pid > 0 )) || return 1
  printf '%s' "$pid"
}

managed_caddy_command_matches() {
  local pid="$1"
  local command_line
  command_line="$(ps -p "$pid" -o command= 2>/dev/null || true)"
  [[ "$command_line" == *caddy* && "$command_line" == *"$CADDY_RUNTIME_CONFIG_FILE"* ]]
}

managed_caddy_owns_lan_port() {
  local pid="$1"
  local output status

  if output="$(run_lsof -nP -a -p "$pid" -iTCP:"$LAN_PORT" -sTCP:LISTEN 2>/dev/null)"; then
    [[ -n "$output" ]]
    return $?
  else
    status=$?
  fi
  [[ "$status" -eq 1 ]] && return 1
  return "$status"
}

stop_managed_caddy() {
  local ownership_status pid graceful_wait_ticks

  if launchctl print "${LAUNCHD_DOMAIN}/${CADDY_LAUNCHD_LABEL}" >/dev/null 2>&1; then
    log "unloading Caddy launch agent ${CADDY_LAUNCHD_LABEL}"
    launchctl bootout "${LAUNCHD_DOMAIN}/${CADDY_LAUNCHD_LABEL}" >/dev/null 2>&1 \
      || launchctl bootout "$LAUNCHD_DOMAIN" "$CADDY_LAUNCHD_PLIST" >/dev/null 2>&1 \
      || true
    for _ in {1..40}; do
      if ! launchctl print "${LAUNCHD_DOMAIN}/${CADDY_LAUNCHD_LABEL}" >/dev/null 2>&1; then
        break
      fi
      sleep 0.25
    done
  fi

  pid="$(managed_caddy_pid || true)"
  if [[ -z "$pid" ]]; then
    rm -f "$CADDY_PID_FILE"
    return 0
  fi
  if ! kill -0 "$pid" >/dev/null 2>&1; then
    rm -f "$CADDY_PID_FILE"
    return 0
  fi
  if ! managed_caddy_command_matches "$pid"; then
    log "not stopping pid ${pid}; it does not match this Caddy config"
    rm -f "$CADDY_PID_FILE"
    return 0
  fi
  if managed_caddy_owns_lan_port "$pid"; then
    :
  else
    ownership_status=$?
    if [[ "$ownership_status" -gt 1 ]]; then
      log "Caddy port ownership probe failed status=${ownership_status}"
      return "$ownership_status"
    fi
    log "not stopping pid ${pid}; it does not listen on LAN proxy port ${LAN_PORT}"
    rm -f "$CADDY_PID_FILE"
    return 0
  fi

  log "stopping managed Caddy pid ${pid}"
  kill "$pid" >/dev/null 2>&1 || true
  for ((graceful_wait_ticks = 0; graceful_wait_ticks < CADDY_EXIT_TIMEOUT_SECONDS * 4; graceful_wait_ticks++)); do
    if ! kill -0 "$pid" >/dev/null 2>&1; then
      rm -f "$CADDY_PID_FILE"
      return 0
    fi
    sleep 0.25
  done
  log "warning: managed Caddy pid ${pid} did not exit after SIGTERM; forcing SIGKILL"
  kill -KILL "$pid" >/dev/null 2>&1 || true
  for _ in {1..20}; do
    if ! kill -0 "$pid" >/dev/null 2>&1; then
      rm -f "$CADDY_PID_FILE"
      return 0
    fi
    sleep 0.25
  done
  log "warning: managed Caddy pid ${pid} did not exit after SIGKILL"
  return 1
}

start_managed_caddy() {
  local caddy_bin escaped_caddyfile escaped_label escaped_log_file escaped_loopback_port escaped_lan_port escaped_pid_file

  rm -f "$CADDY_PID_FILE"
  mkdir -p "$CADDY_LOG_ROOT" "$(dirname "$CADDY_LOG_FILE")" "$(dirname "$CADDY_RUNTIME_CONFIG_FILE")" "$(dirname "$CADDY_LAUNCHD_PLIST")"
  chmod 0700 "$CADDY_LOG_ROOT"
  : >"$CADDY_LOG_FILE"
  chmod 0600 "$CADDY_LOG_FILE"
  install -m 0600 "$CADDYFILE" "$CADDY_RUNTIME_CONFIG_FILE"
  caddy_bin="$(command -v caddy)"

  escaped_label="$(xml_escape "$CADDY_LAUNCHD_LABEL")"
  escaped_caddyfile="$(xml_escape "$CADDY_RUNTIME_CONFIG_FILE")"
  escaped_pid_file="$(xml_escape "$CADDY_PID_FILE")"
  escaped_log_file="$(xml_escape "$CADDY_LOG_FILE")"
  escaped_lan_port="$(xml_escape "$LAN_PORT")"
  escaped_loopback_port="$(xml_escape "$LOOPBACK_PORT")"
  caddy_bin="$(xml_escape "$caddy_bin")"

  cat >"$CADDY_LAUNCHD_PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>${escaped_label}</string>
  <key>ProgramArguments</key>
  <array>
    <string>${caddy_bin}</string>
    <string>run</string>
    <string>--config</string>
    <string>${escaped_caddyfile}</string>
    <string>--pidfile</string>
    <string>${escaped_pid_file}</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>NEW_API_LAN_PROXY_PORT</key>
    <string>${escaped_lan_port}</string>
    <key>NEW_API_LOOPBACK_PORT</key>
    <string>${escaped_loopback_port}</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ExitTimeOut</key>
  <integer>${CADDY_EXIT_TIMEOUT_SECONDS}</integer>
  <key>ThrottleInterval</key>
  <integer>${CADDY_THROTTLE_INTERVAL_SECONDS}</integer>
  <key>StandardOutPath</key>
  <string>${escaped_log_file}</string>
  <key>StandardErrorPath</key>
  <string>${escaped_log_file}</string>
</dict>
</plist>
EOF

  launchctl bootout "$LAUNCHD_DOMAIN" "$CADDY_LAUNCHD_PLIST" >/dev/null 2>&1 || true
  launchctl bootstrap "$LAUNCHD_DOMAIN" "$CADDY_LAUNCHD_PLIST"
  launchctl kickstart -k "${LAUNCHD_DOMAIN}/${CADDY_LAUNCHD_LABEL}"
}

wait_for_managed_caddy_pid() {
  local pid
  for _ in {1..20}; do
    pid="$(managed_caddy_pid || true)"
    if [[ -n "$pid" ]] \
      && kill -0 "$pid" >/dev/null 2>&1 \
      && managed_caddy_command_matches "$pid" \
      && managed_caddy_owns_lan_port "$pid"; then
      return 0
    fi
    sleep 0.25
  done
  return 1
}

wait_for_url() {
  local url="$1"
  local timeout="$2"
  local start
  start="$(date +%s)"
  while true; do
    if curl -fsS --max-time 3 "$url" >/dev/null; then
      return 0
    fi
    if (( "$(date +%s)" - start >= timeout )); then
      return 1
    fi
    sleep 1
  done
}

container_utc_timestamp() {
  local container_id="$1"
  local timeout="${2:-10}"
  local epoch
  local start

  start="$(date +%s)"
  while true; do
    if epoch="$(docker exec "$container_id" date -u +%s 2>/dev/null)" && [[ "$epoch" =~ ^[0-9]+$ ]]; then
      python3 -c '
from datetime import datetime, timezone
import sys

epoch = max(0, int(sys.argv[1]) - 2)
print(datetime.fromtimestamp(epoch, timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"))
' "$epoch"
      return 0
    fi
    if (( "$(date +%s)" - start >= timeout )); then
      return 1
    fi
    sleep 1
  done

  return 1
}

public_health_probe_request_id() {
  curl -fsS --max-time 5 -D - -o /dev/null "$HEALTH_URL" \
    | awk -F': *' '
        tolower($1) == "x-oneapi-request-id" {
          gsub(/^[[:space:]]+/, "", $2)
          gsub(/[[:space:]\r]+$/, "", $2)
          print $2
          exit
        }
      '
}

wait_for_probe_log_line() {
  local container_id="$1"
  local since="$2"
  local request_id="$3"
  local timeout="$4"
  local line logs start

  start="$(date +%s)"
  while true; do
    logs="$(docker logs --since "$since" "$container_id" 2>&1 || true)"
    line="$(printf '%s\n' "$logs" | grep -F "$request_id" | tail -n 1 || true)"
    if [[ -n "$line" ]]; then
      printf '%s\n' "$line"
      return 0
    fi
    if (( "$(date +%s)" - start >= timeout )); then
      return 1
    fi
    sleep 1
  done
}

rollback() {
  local rollback_ok=0

  log "rolling back to default Docker published port"
  if ! stop_managed_caddy; then
    log "warning: managed Caddy did not stop cleanly during rollback"
    rollback_ok=1
  fi
  if ! compose_default up -d --no-deps --force-recreate "$COMPOSE_SERVICE" >/dev/null; then
    log "warning: failed to restore default Docker published port"
    return 1
  fi
  if ! wait_for_url "http://127.0.0.1:${LAN_PORT}${HEALTH_PATH}" 60; then
    log "warning: default Docker published port did not become healthy after rollback"
    rollback_ok=1
  fi
  return "$rollback_ok"
}

on_error() {
  local line="$1"
  if [[ "$APPLY" -eq 1 && "$ROLLBACK_NEEDED" -eq 1 ]]; then
    printf '[macos-tun] ERROR: failed at line %s\n' "$line" >&2
    run_rollback_if_needed
  fi
}
trap 'on_error "$LINENO"' ERR

preflight() {
  local active_connections caddy_validate_output lan_listener_status loopback_listener_status secret_requirement tun_config_json rollback_mapping_status

  [[ "$(uname -s)" == "Darwin" ]] || fail "this script is intended for macOS hosts"
  [[ "$HEALTH_PATH" == /* ]] || fail "NEW_API_HEALTH_PATH must start with /"
  [[ "$ALLOW_ACTIVE_REQUESTS" =~ ^[01]$ ]] || fail "NEW_API_ALLOW_ACTIVE_REQUESTS must be 0 or 1"
  [[ "$ACTIVE_CONNECTION_DRAIN_TIMEOUT" =~ ^[0-9]+$ ]] || fail "NEW_API_ACTIVE_CONNECTION_DRAIN_TIMEOUT must be a non-negative integer"
  [[ "$ALLOW_IMAGE_CHANGE" =~ ^[01]$ ]] || fail "NEW_API_ALLOW_IMAGE_CHANGE must be 0 or 1"
  [[ "$CADDY_LAUNCHD_LABEL" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || fail "CADDY_LAUNCHD_LABEL contains unsupported characters"
  [[ "$CADDY_LAUNCHD_PLIST" == /* ]] || fail "CADDY_LAUNCHD_PLIST must be an absolute path"
  [[ "$CADDY_RUNTIME_CONFIG_FILE" == /* ]] || fail "CADDY_RUNTIME_CONFIG_FILE must be an absolute path"
  if [[ -n "$RELEASE_APPROVAL_FILE" && "$RELEASE_APPROVAL_FILE" != /* ]]; then
    fail "NEW_API_RELEASE_APPROVAL_FILE must be an absolute path"
  fi
  if [[ -n "$EXPECTED_IMAGE" && "$EXPECTED_IMAGE" =~ [[:space:]] ]]; then
    fail "NEW_API_EXPECTED_IMAGE must not contain whitespace"
  fi
  if [[ -n "$EXPECTED_REVISION" && ! "$EXPECTED_REVISION" =~ ^[0-9a-f]{40}$ ]]; then
    fail "NEW_API_EXPECTED_REVISION must be a 40-character lowercase commit"
  fi
  [[ "$CADDY_EXIT_TIMEOUT_SECONDS" =~ ^[1-9][0-9]*$ ]] || fail "NEW_API_CADDY_EXIT_TIMEOUT_SECONDS must be a positive integer"
  [[ "$CADDY_THROTTLE_INTERVAL_SECONDS" =~ ^[1-9][0-9]*$ ]] || fail "NEW_API_CADDY_THROTTLE_INTERVAL_SECONDS must be a positive integer"
  [[ "$LSOF_TIMEOUT_SECONDS" =~ ^[1-9][0-9]*(\.[0-9]+)?$ ]] || fail "NEW_API_LSOF_TIMEOUT_SECONDS must be positive"
  validate_port NEW_API_LAN_PROXY_PORT "$LAN_PORT"
  validate_port NEW_API_LOOPBACK_PORT "$LOOPBACK_PORT"
  validate_port NEW_API_CONTAINER_PORT "$CONTAINER_PORT"
  [[ "$LAN_PORT" != "$LOOPBACK_PORT" ]] || fail "NEW_API_LAN_PROXY_PORT and NEW_API_LOOPBACK_PORT must differ"
  if [[ -n "$CONFIGURED_PORT_MAPPING" && "$CONFIGURED_PORT_MAPPING" != "$PORT_MAPPING" ]]; then
    fail "NEW_API_PORT_MAPPING=${CONFIGURED_PORT_MAPPING} does not match derived ${PORT_MAPPING}"
  fi
  command -v docker >/dev/null || fail "docker is required"
  command -v caddy >/dev/null || fail "caddy is required"
  command -v launchctl >/dev/null || fail "launchctl is required"
  command -v curl >/dev/null || fail "curl is required"
  LSOF_BIN="$(command -v lsof)" || fail "lsof is required"
  command -v python3 >/dev/null || fail "python3 is required"
  command -v git >/dev/null || fail "git is required for the production image guard"
  [[ -f "$COMPOSE_FILE_PATH" ]] || fail "missing compose file: $COMPOSE_FILE_PATH"
  [[ -f "$CADDYFILE" ]] || fail "missing Caddyfile: $CADDYFILE"
  [[ -f "$TUN_ENV_FILE" ]] || fail "missing TUN env file: $TUN_ENV_FILE"
  validate_tun_env_scope

  log "validating Caddyfile"
  if ! caddy_validate_output="$(run_caddy validate --config "$CADDYFILE" 2>&1 >/dev/null)"; then
    printf '%s\n' "$caddy_validate_output" >&2
    fail "Caddyfile validation failed"
  fi

  log "validating compose port mapping"
  if ! tun_config_json="$(compose_tun config --format json 2>&1)"; then
    report_compose_config_error "TUN deployment" "$tun_config_json"
    fail "TUN Compose configuration failed to render; resolve the reported Compose or environment error"
  fi
  printf '%s\n' "$tun_config_json" \
    | COMPOSE_SERVICE="$COMPOSE_SERVICE" EXPECTED_PORT="$LOOPBACK_PORT" EXPECTED_TARGET="$CONTAINER_PORT" python3 -c '
import json
import os
import sys

data = json.load(sys.stdin)
service_name = os.environ["COMPOSE_SERVICE"]
service = data["services"].get(service_name)
if service is None:
    sys.exit(2)
ports = service.get("ports", [])
expected_port = os.environ["EXPECTED_PORT"]
expected_target = int(os.environ["EXPECTED_TARGET"])
if len(ports) != 1:
    sys.exit(1)
item = ports[0]
if (
    item.get("host_ip") == "127.0.0.1"
    and str(item.get("published")) == expected_port
    and int(item.get("target")) == expected_target
    and item.get("protocol", "tcp") == "tcp"
):
    sys.exit(0)
sys.exit(1)
' || fail "compose does not render expected port mapping ${PORT_MAPPING}"

  log "validating rollback compose port mapping"
  if validate_default_port_mapping; then
    :
  else
    rollback_mapping_status=$?
    if [[ "$rollback_mapping_status" -eq 2 ]]; then
      fail "default Compose configuration failed to render; resolve the reported Compose or environment error"
    fi
    fail "compose does not render expected rollback port mapping ${DEFAULT_PORT_MAPPING}"
  fi

  log "validating required service secrets"
  secret_requirement="be set"
  if [[ "$APPLY" -eq 1 ]]; then
    secret_requirement="be set to non-placeholder values"
  fi
  validate_required_service_env tun "$APPLY" || fail "SESSION_SECRET and CRYPTO_SECRET must ${secret_requirement} for TUN deployment"
  validate_required_service_env default "$APPLY" || fail "SESSION_SECRET and CRYPTO_SECRET must ${secret_requirement} for rollback deployment"

  log "validating production image provenance and dirty-worktree boundary"
  validate_release_image_guard

  log "checking current port ownership"
  if lsof_has_listener "$LAN_PORT"; then
    if ! lan_port_owner_is_expected; then
      fail "LAN proxy port ${LAN_PORT} is already in use by an unmanaged process"
    fi
  else
    lan_listener_status=$?
    [[ "$lan_listener_status" -eq 1 ]] || fail "LAN port ownership probe failed status=${lan_listener_status}"
  fi

  if lsof_has_listener "$LOOPBACK_PORT"; then
    if current_service_has_loopback_port; then
      log "loopback backend port ${LOOPBACK_PORT} is already used by ${COMPOSE_SERVICE}; continuing"
    else
      fail "loopback backend port ${LOOPBACK_PORT} is already in use"
    fi
  else
    loopback_listener_status=$?
    [[ "$loopback_listener_status" -eq 1 ]] || fail "loopback port ownership probe failed status=${loopback_listener_status}"
  fi

  active_connections="$(current_service_active_connections)"
  if [[ "$active_connections" != "0" ]]; then
    log "found ${active_connections} established connection(s) on ${LAN_PORT}"
    print_active_connection_sample >&2 || true
    if [[ "$APPLY" -eq 0 ]]; then
      if [[ "$ALLOW_ACTIVE_REQUESTS" == "1" ]]; then
        log "warning: --apply will interrupt these connections because NEW_API_ALLOW_ACTIVE_REQUESTS=1"
      else
        log "warning: default --apply will wait up to ${ACTIVE_CONNECTION_DRAIN_TIMEOUT}s for these connections to drain"
      fi
    elif [[ "$ALLOW_ACTIVE_REQUESTS" == "1" ]]; then
      log "warning: established connections on ${LAN_PORT} will be interrupted"
    else
      if (( 10#$ACTIVE_CONNECTION_DRAIN_TIMEOUT > 0 )); then
        log "waiting up to ${ACTIVE_CONNECTION_DRAIN_TIMEOUT}s for established connections on ${LAN_PORT} to drain"
        if wait_for_active_connections_to_drain "$ACTIVE_CONNECTION_DRAIN_TIMEOUT"; then
          log "established connections drained"
          return 0
        fi
        active_connections="$(current_service_active_connections)"
        log "found ${active_connections} established connection(s) on ${LAN_PORT} after drain timeout"
        print_active_connection_sample >&2 || true
      fi
      fail "${active_connections} established connection(s) on ${LAN_PORT}; set NEW_API_ALLOW_ACTIVE_REQUESTS=1 to proceed"
    fi
  fi
}

start_caddy_proxy() {
  log "starting dedicated host-side Caddy on port ${LAN_PORT}"
  stop_managed_caddy || true
  start_managed_caddy
  if ! wait_for_managed_caddy_pid; then
    if [[ -f "$CADDY_LOG_FILE" ]]; then
      log "recent Caddy log:"
      tail -n 40 "$CADDY_LOG_FILE" >&2 || true
    fi
    fail "Caddy did not keep running with expected pid file ${CADDY_PID_FILE} and config ${CADDYFILE}"
  fi
  wait_for_url "$HEALTH_URL" 10 || fail "public entry health check failed after starting Caddy"
}

verify_after_apply() {
  local container_id probe_log_line probe_request_id probe_start

  log "checking backend ${BACKEND_URL}"
  wait_for_url "$BACKEND_URL" 60 || fail "backend health check failed"
  container_id="$(compose_service_container_id)"

  start_caddy_proxy

  log "checking public entry ${HEALTH_URL}"
  wait_for_url "$HEALTH_URL" 30 || fail "public entry health check failed"

  probe_start="$(container_utc_timestamp "$container_id" 10)" || fail "could not read container UTC timestamp for log verification"
  probe_request_id="$(public_health_probe_request_id)"
  [[ -n "$probe_request_id" ]] || fail "public health probe did not return X-Oneapi-Request-Id; check new-api version compatibility, RequestId middleware, and proxy response headers"
  if ! probe_log_line="$(wait_for_probe_log_line "$container_id" "$probe_start" "$probe_request_id" 10)"; then
    fail "did not find public health probe log after Caddy handoff"
  fi
  if [[ -n "$ABNORMAL_TUN_IP_REGEX" ]] && printf '%s\n' "$probe_log_line" | grep -E -q "${ABNORMAL_TUN_IP_REGEX}"; then
    fail "abnormal TUN IP still appears in new-api logs"
  fi
  if ! printf '%s\n' "$probe_log_line" | grep -q "127\\.0\\.0\\.1"; then
    fail "did not find 127.0.0.1 health log after Caddy handoff"
  fi
  log "verified public health probe log: ${probe_log_line}"

  log "current Docker port mapping:"
  docker inspect "$container_id" --format '{{json .NetworkSettings.Ports}}'
}

preflight

if [[ "$APPLY" -eq 0 ]]; then
  log "check-only passed"
  exit 0
fi

assert_validated_image_before_apply
ROLLBACK_NEEDED=1
log "recreating ${COMPOSE_SERVICE} with ${PORT_MAPPING}"
compose_tun up -d --no-deps --force-recreate "$COMPOSE_SERVICE"
verify_after_apply
ROLLBACK_NEEDED=0
trap - ERR
log "apply completed"
