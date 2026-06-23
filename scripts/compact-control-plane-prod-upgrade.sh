#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

MODE="dry-run"
COMPOSE_FILE_PATH="${COMPACT_PROD_COMPOSE_FILE:-$ROOT_DIR/docker-compose.yml}"
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
LOG_ERROR_PATTERN='panic|fatal|synthetic compact state.*(failed|error|expired|not found)|input item type|chat compatibility mode.*(failed|error|not implemented)|not implemented|No available channel|Service Unavailable|Invalid token|status_code=503| 503 |previous_response_id.*(failed|error|expired|not found)'

usage() {
  cat <<'USAGE'
Usage:
  COMPACT_PROD_CANDIDATE_IMAGE='<image>' scripts/compact-control-plane-prod-upgrade.sh --dry-run
  COMPACT_PROD_CANDIDATE_IMAGE='<image>' CONFIRM_PROD_UPGRADE='<exact phrase>' scripts/compact-control-plane-prod-upgrade.sh --execute

Environment:
  COMPACT_PROD_CANDIDATE_IMAGE   Required candidate image to deploy.
  COMPACT_PROD_ROLLBACK_IMAGE    Rollback image. Defaults to the current new-api container image.
  COMPACT_PROD_COMPOSE_FILE      Compose file path. Default: docker-compose.yml.
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
  docker compose version >/dev/null 2>&1 || die "docker compose subcommand is required"
}

docker_image_id() {
  docker image inspect "$1" --format '{{.Id}}'
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
  curl -fsS "$STATUS_URL" >/dev/null && log "diagnostic_status_url=ok" || log "diagnostic_status_url=failed"
  docker exec "$POSTGRES_CONTAINER" pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB" || true
  docker logs --tail 120 "$APP_CONTAINER" 2>&1 || true
}

ensure_preconditions() {
  [ -f "$COMPOSE_FILE_PATH" ] || die "compose file not found: $COMPOSE_FILE_PATH"
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
  NEW_API_IMAGE="$image" docker compose -f "$COMPOSE_FILE_PATH" up -d --no-deps --force-recreate "$APP_SERVICE"
}

wait_status() {
  local timeout="${1:-60}"
  local i
  for i in $(seq 1 "$timeout"); do
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
    tmp_file="${BACKUP_FILE}.tmp.$$"
    log "run: docker exec $POSTGRES_CONTAINER pg_dump -U $POSTGRES_USER -d $POSTGRES_DB > $tmp_file"
    if ! docker exec "$POSTGRES_CONTAINER" pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" > "$tmp_file"; then
      rm -f "$tmp_file"
      die "pg_dump failed"
    fi
    mv "$tmp_file" "$BACKUP_FILE"
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
  log "verify: status"
  if ! curl -fsS "$STATUS_URL" >/dev/null; then
    log "verify_error=status_url_failed"
    return 1
  fi
  log "verify: recent error scan"
  if docker logs --tail 300 "$APP_CONTAINER" 2>&1 | grep -Eiq "$LOG_ERROR_PATTERN"; then
    docker logs --tail 300 "$APP_CONTAINER" 2>&1 | grep -Ei "$LOG_ERROR_PATTERN" >&2 || true
    return 1
  fi
  return 0
}

rollback() {
  local rc=0
  log "rollback_to=$ROLLBACK_IMAGE"
  if ! NEW_API_IMAGE="$ROLLBACK_IMAGE" docker compose -f "$COMPOSE_FILE_PATH" up -d --no-deps --force-recreate "$APP_SERVICE"; then
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
}

main() {
  parse_args "$@"
  require_tools
  ensure_preconditions
  print_state
  if [ "$MODE" = "dry-run" ]; then
    backup_database
    log "would_run: NEW_API_IMAGE=$CANDIDATE_IMAGE docker compose -f $COMPOSE_FILE_PATH up -d --no-deps --force-recreate $APP_SERVICE"
    log "would_verify: docker exec $APP_CONTAINER /new-api --build-info"
    log "would_verify: curl -fsS $STATUS_URL"
    log "would_verify: docker logs --tail 300 $APP_CONTAINER | grep -Ei '$LOG_ERROR_PATTERN'"
    return 0
  fi
  execute_upgrade
}

main "$@"
