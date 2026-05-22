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
  CADDY_LOG_FILE=/tmp/<COMPOSE_SERVICE>-macos-tun-caddy-<LAN_PORT>.log
  CADDY_PID_FILE=/tmp/<COMPOSE_SERVICE>-macos-tun-caddy-<LAN_PORT>.pid
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

COMPOSE_SERVICE="${COMPOSE_SERVICE:-$(tun_env_value COMPOSE_SERVICE new-api)}"
LAN_PORT="${NEW_API_LAN_PROXY_PORT:-$(tun_env_value NEW_API_LAN_PROXY_PORT 3000)}"
LOOPBACK_PORT="${NEW_API_LOOPBACK_PORT:-$(tun_env_value NEW_API_LOOPBACK_PORT 13000)}"
CONTAINER_PORT="${NEW_API_CONTAINER_PORT:-$(tun_env_value NEW_API_CONTAINER_PORT 3000)}"
HEALTH_PATH="${NEW_API_HEALTH_PATH:-$(tun_env_value NEW_API_HEALTH_PATH /api/status)}"
ABNORMAL_TUN_IP_REGEX="${NEW_API_ABNORMAL_TUN_IP_REGEX:-$(tun_env_value NEW_API_ABNORMAL_TUN_IP_REGEX '')}"
CONFIGURED_PORT_MAPPING="${NEW_API_PORT_MAPPING:-$(tun_env_value NEW_API_PORT_MAPPING '')}"
PORT_MAPPING="127.0.0.1:${LOOPBACK_PORT}:${CONTAINER_PORT}"
DEFAULT_PORT_MAPPING="${NEW_API_DEFAULT_PORT_MAPPING:-$(tun_env_value NEW_API_DEFAULT_PORT_MAPPING "${LAN_PORT}:${CONTAINER_PORT}")}"
HEALTH_URL="http://127.0.0.1:${LAN_PORT}${HEALTH_PATH}"
BACKEND_URL="http://127.0.0.1:${LOOPBACK_PORT}${HEALTH_PATH}"
CADDY_LOG_FILE="${CADDY_LOG_FILE:-/tmp/${COMPOSE_SERVICE}-macos-tun-caddy-${LAN_PORT}.log}"
CADDY_PID_FILE="${CADDY_PID_FILE:-/tmp/${COMPOSE_SERVICE}-macos-tun-caddy-${LAN_PORT}.pid}"

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
        NEW_API_ABNORMAL_TUN_IP_REGEX | \
        NEW_API_PORT_MAPPING)
        ;;
      *)
        fail "TUN env file must not define ${key}; put runtime settings in ${PROD_ENV_FILE}"
        ;;
    esac
  done <"$TUN_ENV_FILE"
}

validate_default_port_mapping() {
  local config_json

  config_json="$(compose_default config --format json)" || return 1
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

managed_caddy_is_running() {
  local pid
  if ! pid="$(managed_caddy_pid)"; then
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
  [[ "$command_line" == *caddy* && "$command_line" == *"$CADDYFILE"* ]]
}

managed_caddy_owns_lan_port() {
  local pid="$1"
  lsof -nP -a -p "$pid" -iTCP:"$LAN_PORT" -sTCP:LISTEN >/dev/null 2>&1
}

stop_managed_caddy() {
  local pid
  if ! pid="$(managed_caddy_pid)"; then
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
  if ! managed_caddy_owns_lan_port "$pid"; then
    log "not stopping pid ${pid}; it does not listen on LAN proxy port ${LAN_PORT}"
    rm -f "$CADDY_PID_FILE"
    return 0
  fi

  log "stopping managed Caddy pid ${pid}"
  kill "$pid" >/dev/null 2>&1 || true
  for _ in {1..20}; do
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
  rm -f "$CADDY_PID_FILE"
  : >"$CADDY_LOG_FILE"
  if run_caddy start --config "$CADDYFILE" --pidfile "$CADDY_PID_FILE" >>"$CADDY_LOG_FILE" 2>&1; then
    return 0
  fi

  log "caddy start failed; trying nohup caddy run"
  nohup env \
    NEW_API_LAN_PROXY_PORT="$LAN_PORT" \
    NEW_API_LOOPBACK_PORT="$LOOPBACK_PORT" \
    caddy run --config "$CADDYFILE" --pidfile "$CADDY_PID_FILE" \
    >"$CADDY_LOG_FILE" 2>&1 &
}

wait_for_managed_caddy_pid() {
  local pid
  for _ in {1..20}; do
    if pid="$(managed_caddy_pid)" \
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
  local secret_requirement tun_config_json

  [[ "$(uname -s)" == "Darwin" ]] || fail "this script is intended for macOS hosts"
  [[ "$HEALTH_PATH" == /* ]] || fail "NEW_API_HEALTH_PATH must start with /"
  validate_port NEW_API_LAN_PROXY_PORT "$LAN_PORT"
  validate_port NEW_API_LOOPBACK_PORT "$LOOPBACK_PORT"
  validate_port NEW_API_CONTAINER_PORT "$CONTAINER_PORT"
  [[ "$LAN_PORT" != "$LOOPBACK_PORT" ]] || fail "NEW_API_LAN_PROXY_PORT and NEW_API_LOOPBACK_PORT must differ"
  if [[ -n "$CONFIGURED_PORT_MAPPING" && "$CONFIGURED_PORT_MAPPING" != "$PORT_MAPPING" ]]; then
    fail "NEW_API_PORT_MAPPING=${CONFIGURED_PORT_MAPPING} does not match derived ${PORT_MAPPING}"
  fi
  command -v docker >/dev/null || fail "docker is required"
  command -v caddy >/dev/null || fail "caddy is required"
  command -v curl >/dev/null || fail "curl is required"
  command -v lsof >/dev/null || fail "lsof is required"
  command -v python3 >/dev/null || fail "python3 is required"
  [[ -f "$COMPOSE_FILE_PATH" ]] || fail "missing compose file: $COMPOSE_FILE_PATH"
  [[ -f "$CADDYFILE" ]] || fail "missing Caddyfile: $CADDYFILE"
  [[ -f "$TUN_ENV_FILE" ]] || fail "missing TUN env file: $TUN_ENV_FILE"
  validate_tun_env_scope

  log "validating Caddyfile"
  run_caddy validate --config "$CADDYFILE" >/dev/null

  log "validating compose port mapping"
  tun_config_json="$(compose_tun config --format json)" || fail "compose does not render expected port mapping ${PORT_MAPPING}"
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
  validate_default_port_mapping || fail "compose does not render expected rollback port mapping ${DEFAULT_PORT_MAPPING}"

  log "validating required service secrets"
  secret_requirement="be set"
  if [[ "$APPLY" -eq 1 ]]; then
    secret_requirement="be set to non-placeholder values"
  fi
  validate_required_service_env tun "$APPLY" || fail "SESSION_SECRET and CRYPTO_SECRET must ${secret_requirement} for TUN deployment"
  validate_required_service_env default "$APPLY" || fail "SESSION_SECRET and CRYPTO_SECRET must ${secret_requirement} for rollback deployment"

  if [[ "$APPLY" -eq 1 ]]; then
    if lsof -nP -iTCP:"$LAN_PORT" -sTCP:LISTEN >/dev/null 2>&1 && ! lan_port_owner_is_expected; then
      fail "LAN proxy port ${LAN_PORT} is already in use by an unmanaged process"
    fi
    if lsof -nP -iTCP:"$LOOPBACK_PORT" -sTCP:LISTEN >/dev/null 2>&1; then
      if current_service_has_loopback_port; then
        log "loopback backend port ${LOOPBACK_PORT} is already used by ${COMPOSE_SERVICE}; continuing"
      else
        fail "loopback backend port ${LOOPBACK_PORT} is already in use"
      fi
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
  local container_id

  log "checking backend ${BACKEND_URL}"
  wait_for_url "$BACKEND_URL" 60 || fail "backend health check failed"
  container_id="$(compose_service_container_id)"

  start_caddy_proxy

  log "checking public entry ${HEALTH_URL}"
  wait_for_url "$HEALTH_URL" 30 || fail "public entry health check failed"

  curl -fsS --max-time 5 "$HEALTH_URL" >/dev/null
  sleep 1

  local recent_logs
  recent_logs="$(docker logs --since 20s "$container_id" 2>&1 || true)"
  if [[ -n "$ABNORMAL_TUN_IP_REGEX" ]] && printf '%s\n' "$recent_logs" | grep -E -q "${ABNORMAL_TUN_IP_REGEX}.*GET ${HEALTH_PATH}"; then
    fail "abnormal TUN IP still appears in new-api logs"
  fi
  if ! printf '%s\n' "$recent_logs" | grep -q "127\\.0\\.0\\.1.*GET ${HEALTH_PATH}"; then
    log "warning: did not find 127.0.0.1 health log in the last 20 seconds; inspect docker logs manually"
  fi

  log "current Docker port mapping:"
  docker inspect "$container_id" --format '{{json .NetworkSettings.Ports}}'
}

preflight

if [[ "$APPLY" -eq 0 ]]; then
  log "check-only passed"
  exit 0
fi

ROLLBACK_NEEDED=1
log "recreating ${COMPOSE_SERVICE} with ${PORT_MAPPING}"
compose_tun up -d --no-deps --force-recreate "$COMPOSE_SERVICE"
verify_after_apply
ROLLBACK_NEEDED=0
trap - ERR
log "apply completed"
