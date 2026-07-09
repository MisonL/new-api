#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

MODE="dry-run"
COMPOSE_FILE_PATH="${COMPACT_PROD_COMPOSE_FILE:-$ROOT_DIR/docker-compose.yml}"
COMPOSE_ENV_FILE="${COMPACT_PROD_ENV_FILE:-}"
APP_SERVICE="${COMPACT_PROD_APP_SERVICE:-new-api}"
APP_CONTAINER="${COMPACT_PROD_APP_CONTAINER:-new-api}"
POSTGRES_CONTAINER="${COMPACT_PROD_POSTGRES_CONTAINER:-postgres}"
POSTGRES_USER="${COMPACT_PROD_POSTGRES_USER:-root}"
POSTGRES_DB="${COMPACT_PROD_POSTGRES_DB:-new-api}"
STATUS_URL="${COMPACT_PROD_STATUS_URL:-http://127.0.0.1:13000/api/status}"
CANDIDATE_IMAGE="${COMPACT_PROD_CANDIDATE_IMAGE:-}"
ROLLBACK_IMAGE="${COMPACT_PROD_ROLLBACK_IMAGE:-}"
BACKUP_DIR="${COMPACT_PROD_BACKUP_DIR:-$ROOT_DIR/backups}"
BACKUP_FILE="${COMPACT_PROD_BACKUP_FILE:-}"
CONFIRM_PHRASE="授权升级正式环境，按 CR-COMPACT-CONTROL-PLANE-V2-PROD-UPGRADE-2026-06-18 执行。"
CONFIRM_PROD_UPGRADE="${CONFIRM_PROD_UPGRADE:-}"
LOG_ERROR_PATTERN='panic|fatal|synthetic compact state.*(failed|error|expired|not found)|input item type|chat compatibility mode.*(failed|error|not implemented)|not implemented|No available channel|Service Unavailable|Invalid token|status_code=503|status=503|"status"[[:space:]]*:[[:space:]]*503|HTTP[ /]+503|previous_response_id.*(failed|error|expired|not found)'
VERIFY_SINCE=""

usage() {
  cat <<'USAGE'
Usage:
  COMPACT_PROD_CANDIDATE_IMAGE='<image>' scripts/compact-control-plane-prod-upgrade.sh --dry-run
  COMPACT_PROD_CANDIDATE_IMAGE='<image>' CONFIRM_PROD_UPGRADE='<exact phrase>' scripts/compact-control-plane-prod-upgrade.sh --execute

Environment:
  COMPACT_PROD_CANDIDATE_IMAGE   Required candidate image to deploy.
  COMPACT_PROD_ROLLBACK_IMAGE    Rollback image. Defaults to the current new-api container image.
  COMPACT_PROD_COMPOSE_FILE      Compose file path. Default: docker-compose.yml.
  COMPACT_PROD_ENV_FILE          Optional env file passed to docker compose and loaded for required runtime variables.
  COMPACT_PROD_BACKUP_DIR        Backup directory. Default: ./backups.
  COMPACT_PROD_BACKUP_FILE       Optional explicit pg_dump output file.
  COMPACT_PROD_STATUS_URL        Status URL. Default: http://127.0.0.1:13000/api/status.
  CONFIRM_PROD_UPGRADE           Required for --execute. Must match the exact phrase printed by --dry-run.

Safety:
  --dry-run is read-only.
  --execute writes a database backup and recreates only the new-api app container.
USAGE
}

log() {
  printf '%s\n' "$*"
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

have() {
  command -v "$1" >/dev/null 2>&1
}

parse_args() {
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --dry-run)
        MODE="dry-run"
        ;;
      --execute)
        MODE="execute"
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        die "unknown argument: $1"
        ;;
    esac
    shift
  done
}

require_tools() {
  have docker || die "docker is required"
  have curl || die "curl is required"
  have date || die "date is required"
  have python3 || die "python3 is required"
  docker compose version >/dev/null 2>&1 || die "docker compose subcommand is required"
}

docker_image_id() {
  docker image inspect "$1" --format '{{.Id}}'
}

compose_args() {
  printf ' -f %s' "$COMPOSE_FILE_PATH"
  if [ -n "$COMPOSE_ENV_FILE" ]; then
    printf ' --env-file %s' "$COMPOSE_ENV_FILE"
  fi
}

compose_args_array() {
  COMPOSE_ARGS=(-f "$COMPOSE_FILE_PATH")
  if [ -n "$COMPOSE_ENV_FILE" ]; then
    COMPOSE_ARGS+=(--env-file "$COMPOSE_ENV_FILE")
  fi
}

trim_runtime_value() {
  printf '%s' "$1" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//'
}

runtime_value_is_usable() {
  local name="$1"
  local value="$2"
  local source="$3"
  value="$(trim_runtime_value "$value")"
  [ -n "$value" ] || return 1
  case "$value" in
    '#'*)
      return 1
      ;;
  esac
  case "$value" in
    *'${'*|*'$('*)
      die "$name in $source contains unresolved shell interpolation; export a resolved production value before running"
      ;;
  esac
  case "$name" in
    NEW_API_DATA_DIR|NEW_API_LOG_DIR)
      case "$value" in
        /*)
          ;;
        *)
          die "$name in $source must be an absolute production host path"
          ;;
      esac
      ;;
  esac
  return 0
}

env_file_value() {
  local name="$1"
  [ -n "$COMPOSE_ENV_FILE" ] || return 1
  awk -v key="$name" '
    /^[[:space:]]*($|#)/ { next }
    {
      line=$0
      sub(/^[[:space:]]*export[[:space:]]+/, "", line)
      if (line !~ "^[[:space:]]*" key "[[:space:]]*=") next
      sub(/^[^=]*=/, "", line)
      sub(/^[[:space:]]+/, "", line)
      sub(/[[:space:]]+$/, "", line)
      if ((line ~ /^".*"$/) || (line ~ /^\047.*\047$/)) {
        line=substr(line, 2, length(line)-2)
      }
      if (length(line) > 0) {
        print line
        found=1
        exit
      }
    }
    END { exit found ? 0 : 1 }
  ' "$COMPOSE_ENV_FILE"
}

required_production_env_names() {
  printf '%s\n' \
    SESSION_SECRET \
    CRYPTO_SECRET \
    SQL_DSN \
    REDIS_CONN_STRING \
    NEW_API_PORT_MAPPING \
    NEW_API_DATA_DIR \
    NEW_API_LOG_DIR
}

require_runtime_env() {
  local missing=""
  local name
  local value
  local env_file_candidate
  for name in $(required_production_env_names); do
    value="$(printenv "$name" || true)"
    if runtime_value_is_usable "$name" "$value" "environment"; then
      continue
    fi
    if [ -n "$value" ]; then
      runtime_value_is_usable "$name" "$value" "environment" || true
    fi
    env_file_candidate=""
    if env_file_candidate="$(env_file_value "$name")" && runtime_value_is_usable "$name" "$env_file_candidate" "$COMPOSE_ENV_FILE"; then
      continue
    fi
    if [ -n "$env_file_candidate" ]; then
      runtime_value_is_usable "$name" "$env_file_candidate" "$COMPOSE_ENV_FILE" || true
    fi
    missing="${missing}${missing:+, }${name}"
  done
  [ -z "$missing" ] || die "missing required production runtime variables: $missing"
}

require_compose_config_resolves() {
  local -a COMPOSE_ARGS
  local config_stderr
  local config_json
  compose_args_array
  config_json="$(mktemp)"
  if ! config_stderr="$(NEW_API_IMAGE="$CANDIDATE_IMAGE" docker compose "${COMPOSE_ARGS[@]}" config --format json >"$config_json" 2>&1)"; then
    printf '%s\n' "$config_stderr" >&2
    rm -f "$config_json"
    die "docker compose config failed; fix production compose variables before upgrading"
  fi
  if printf '%s\n' "$config_stderr" | grep -qi 'Defaulting to a blank string'; then
    printf '%s\n' "$config_stderr" >&2
    rm -f "$config_json"
    die "docker compose config resolved one or more variables to blank strings"
  fi
  if ! python3 - "$config_json" "$APP_SERVICE" "$STATUS_URL" <<'PY'
import json
import os
import sys
from urllib.parse import urlparse

config_path, service_name, status_url = sys.argv[1], sys.argv[2], sys.argv[3]
required_env = (
    "SESSION_SECRET",
    "CRYPTO_SECRET",
    "SQL_DSN",
    "REDIS_CONN_STRING",
)

with open(config_path, "r", encoding="utf-8") as f:
    config = json.load(f)

status = urlparse(status_url)
if status.scheme not in ("http", "https") or not status.hostname:
    print(f"status URL is not an absolute http(s) URL: {status_url}", file=sys.stderr)
    sys.exit(1)
try:
    expected_port = status.port or (443 if status.scheme == "https" else 80)
except ValueError as exc:
    print(f"status URL has an invalid port: {exc}", file=sys.stderr)
    sys.exit(1)
expected_host = status.hostname
if expected_host == "localhost":
    expected_host = "127.0.0.1"

service = config.get("services", {}).get(service_name)
if not service:
    print(f"compose service not found after resolution: {service_name}", file=sys.stderr)
    sys.exit(1)

env = service.get("environment") or {}
missing = [name for name in required_env if not str(env.get(name, "")).strip()]
if missing:
    print("compose service resolved empty runtime environment: " + ", ".join(missing), file=sys.stderr)
    sys.exit(1)

ports = service.get("ports") or []
has_expected_port = False
for port in ports:
    host_ip = port.get("host_ip") or ""
    if host_ip == "localhost":
        host_ip = "127.0.0.1"
    if (
        str(port.get("target")) == "3000"
        and str(port.get("published", "")).strip() == str(expected_port)
        and host_ip == expected_host
    ):
        has_expected_port = True
        break
if not has_expected_port:
    print(
        f"compose service does not publish target port 3000 to {expected_host}:{expected_port}",
        file=sys.stderr,
    )
    sys.exit(1)

volumes = service.get("volumes") or []
required_targets = {"/data": False, "/app/logs": False}
for volume in volumes:
    target = volume.get("target")
    source = volume.get("source", "")
    if target in required_targets and volume.get("type") == "bind" and os.path.isabs(source):
        required_targets[target] = True

missing_targets = [target for target, present in required_targets.items() if not present]
if missing_targets:
    print("compose service does not bind absolute production paths for: " + ", ".join(missing_targets), file=sys.stderr)
    sys.exit(1)
PY
  then
    rm -f "$config_json"
    die "docker compose config resolved to an unsafe production runtime shape"
  fi
  rm -f "$config_json"
}

current_container_image() {
  docker inspect "$APP_CONTAINER" --format '{{.Config.Image}}'
}

current_container_id() {
  docker inspect "$APP_CONTAINER" --format '{{.Id}}'
}

diagnose_runtime_state() {
  log "diagnostic_app_container=$APP_CONTAINER"
  docker ps --filter "name=^/${APP_CONTAINER}$" --format 'diagnostic_app={{.Names}} image={{.Image}} status={{.Status}}' || true
  docker inspect "$APP_CONTAINER" --format 'diagnostic_app_image={{.Config.Image}} diagnostic_app_state={{.State.Status}} diagnostic_app_health={{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' || true
  docker exec "$APP_CONTAINER" /new-api --build-info || true
  if curl -fsS "$STATUS_URL" >/dev/null; then
    log "diagnostic_status_url=ok"
  else
    log "diagnostic_status_url=failed"
  fi
  docker exec "$POSTGRES_CONTAINER" pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB" || true
  docker logs --tail 120 "$APP_CONTAINER" 2>&1 || true
}

ensure_preconditions() {
  [ -f "$COMPOSE_FILE_PATH" ] || die "compose file not found: $COMPOSE_FILE_PATH"
  if [ -n "$COMPOSE_ENV_FILE" ]; then
    [ -f "$COMPOSE_ENV_FILE" ] || die "compose env file not found: $COMPOSE_ENV_FILE"
  fi
  require_runtime_env
  docker inspect "$APP_CONTAINER" >/dev/null 2>&1 || die "app container not found: $APP_CONTAINER"
  docker inspect "$POSTGRES_CONTAINER" >/dev/null 2>&1 || die "postgres container not found: $POSTGRES_CONTAINER"
  [ -n "$CANDIDATE_IMAGE" ] || die "COMPACT_PROD_CANDIDATE_IMAGE is required; build the current source into an immutable image tag and pass it explicitly"
  docker_image_id "$CANDIDATE_IMAGE" >/dev/null || die "candidate image not found: $CANDIDATE_IMAGE"

  if [ -z "$ROLLBACK_IMAGE" ]; then
    ROLLBACK_IMAGE="$(current_container_image)"
  fi
  docker_image_id "$ROLLBACK_IMAGE" >/dev/null || die "rollback image not found: $ROLLBACK_IMAGE"

  local candidate_image_id
  local rollback_image_id
  candidate_image_id="$(docker_image_id "$CANDIDATE_IMAGE")"
  rollback_image_id="$(docker_image_id "$ROLLBACK_IMAGE")"
  if [ "$CANDIDATE_IMAGE" = "$ROLLBACK_IMAGE" ] || [ "$candidate_image_id" = "$rollback_image_id" ]; then
    die "candidate image and rollback image are identical: $CANDIDATE_IMAGE"
  fi
  require_compose_config_resolves

  if [ "$MODE" = "execute" ] && [ "$CONFIRM_PROD_UPGRADE" != "$CONFIRM_PHRASE" ]; then
    die "CONFIRM_PROD_UPGRADE must exactly equal: $CONFIRM_PHRASE"
  fi
}

run() {
  if [ "$MODE" = "execute" ]; then
    log "run: $*"
    "$@"
  else
    log "would_run: $*"
  fi
}

compose_recreate_app() {
  local image="$1"
  local -a COMPOSE_ARGS
  compose_args_array
  NEW_API_IMAGE="$image" docker compose "${COMPOSE_ARGS[@]}" up -d --no-deps --force-recreate "$APP_SERVICE"
}

wait_status() {
  local timeout="${1:-60}"
  local _
  for _ in $(seq 1 "$timeout"); do
    if curl -fsS "$STATUS_URL" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  return 1
}

print_state() {
  log "mode=$MODE"
  log "app_container=$APP_CONTAINER"
  log "current_container_id=$(current_container_id)"
  log "current_image=$(current_container_image)"
  log "candidate_image=$CANDIDATE_IMAGE"
  log "candidate_image_id=$(docker_image_id "$CANDIDATE_IMAGE")"
  log "rollback_image=$ROLLBACK_IMAGE"
  log "rollback_image_id=$(docker_image_id "$ROLLBACK_IMAGE")"
  log "status_url=$STATUS_URL"
  log "compose_file=$COMPOSE_FILE_PATH"
  if [ -n "$COMPOSE_ENV_FILE" ]; then
    log "compose_env_file=$COMPOSE_ENV_FILE"
  else
    log "compose_env_file=not-set"
  fi
  for name in $(required_production_env_names); do
    log "runtime_env_${name}=set"
  done
  log "confirm_phrase=$CONFIRM_PHRASE"
}

backup_database() {
  local ts
  ts="$(date -u +%Y%m%dT%H%M%SZ)"
  if [ -z "$BACKUP_FILE" ]; then
    BACKUP_FILE="$BACKUP_DIR/new-api-before-compact-control-plane-v2-${ts}.sql"
  fi
  if [ "$MODE" = "execute" ]; then
    mkdir -p "$BACKUP_DIR"
    local tmp_file
    local old_umask
    tmp_file="${BACKUP_FILE}.tmp.$$"
    log "run: docker exec $POSTGRES_CONTAINER pg_dump -U $POSTGRES_USER -d $POSTGRES_DB > $tmp_file"
    old_umask="$(umask)"
    umask 077
    if ! docker exec "$POSTGRES_CONTAINER" pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" > "$tmp_file"; then
      umask "$old_umask"
      rm -f "$tmp_file"
      die "pg_dump failed"
    fi
    umask "$old_umask"
    chmod 600 "$tmp_file"
    mv "$tmp_file" "$BACKUP_FILE"
    chmod 600 "$BACKUP_FILE"
    [ -s "$BACKUP_FILE" ] || die "backup file is empty: $BACKUP_FILE"
  else
    log "would_run: docker exec $POSTGRES_CONTAINER pg_dump -U $POSTGRES_USER -d $POSTGRES_DB > $BACKUP_FILE"
  fi
}

verify_app() {
  log "verify: build-info"
  if ! docker exec "$APP_CONTAINER" /new-api --build-info; then
    log "verify_error=build_info_failed"
    return 1
  fi
  local expected_image_id actual_image_id
  expected_image_id="$(docker_image_id "$CANDIDATE_IMAGE")"
  actual_image_id="$(docker inspect "$APP_CONTAINER" --format '{{.Image}}')"
  if [ "$expected_image_id" != "$actual_image_id" ]; then
    log "verify_error=image_id_mismatch expected=$expected_image_id actual=$actual_image_id"
    return 1
  fi
  log "verify: status"
  if ! curl -fsS "$STATUS_URL" >/dev/null; then
    log "verify_error=status_url_failed"
    return 1
  fi
  log "verify: recent error scan"
  local logs_since
  logs_since="${VERIFY_SINCE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
  if docker logs --since "$logs_since" "$APP_CONTAINER" 2>&1 | grep -Eiq "$LOG_ERROR_PATTERN"; then
    docker logs --since "$logs_since" "$APP_CONTAINER" 2>&1 | grep -Ei "$LOG_ERROR_PATTERN" >&2 || true
    return 1
  fi
  return 0
}

rollback() {
  local rc=0
  log "rollback_to=$ROLLBACK_IMAGE"
  local -a COMPOSE_ARGS
  compose_args_array
  if ! NEW_API_IMAGE="$ROLLBACK_IMAGE" docker compose "${COMPOSE_ARGS[@]}" up -d --no-deps --force-recreate "$APP_SERVICE"; then
    log "rollback_error=compose_recreate_failed"
    rc=1
  fi
  if ! wait_status 60; then
    log "rollback_error=status_check_failed"
    rc=1
  fi
  if ! docker exec "$APP_CONTAINER" /new-api --build-info; then
    log "rollback_error=build_info_failed"
    rc=1
  fi
  if ! curl -fsS "$STATUS_URL" >/dev/null; then
    log "rollback_error=status_url_failed"
    rc=1
  fi
  if [ "$rc" -ne 0 ]; then
    return "$rc"
  fi
  log "rollback_status=restored"
}

rollback_or_die() {
  local reason="$1"
  if rollback; then
    die "$reason and rollback was attempted"
  fi
  log "rollback_failure_after=$reason"
  diagnose_runtime_state
  die "$reason and rollback also failed; inspect diagnostics above and manually restore NEW_API_IMAGE=$ROLLBACK_IMAGE with docker compose"
}

execute_upgrade() {
  VERIFY_SINCE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  backup_database
  log "deploy_candidate=$CANDIDATE_IMAGE"
  if ! compose_recreate_app "$CANDIDATE_IMAGE"; then
    rollback_or_die "candidate deploy command failed"
  fi
  if ! wait_status 60; then
    rollback_or_die "status check failed"
  fi
  if ! verify_app; then
    rollback_or_die "post-upgrade verification failed"
  fi
  log "upgrade_status=deployed"
  log "manual_required=run real Codex fresh and resume requests through production, then inspect usage logs for no 503/not implemented/state expired false errors"
  log "verify_since=$VERIFY_SINCE"
}

main() {
  parse_args "$@"
  require_tools
  ensure_preconditions
  print_state
  if [ "$MODE" = "dry-run" ]; then
    backup_database
    log "would_run: NEW_API_IMAGE=$CANDIDATE_IMAGE docker compose$(compose_args) up -d --no-deps --force-recreate $APP_SERVICE"
    log "would_verify: docker exec $APP_CONTAINER /new-api --build-info"
    log "would_verify: curl -fsS $STATUS_URL"
    log "would_verify: docker logs --since <verify_since> $APP_CONTAINER | grep -Ei '$LOG_ERROR_PATTERN'"
    return 0
  fi
  execute_upgrade
}

main "$@"
