#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

MODE="dry-run"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-}"
NEW_API_CONTAINER="${NEW_API_CONTAINER:-}"
POSTGRES_USER="${POSTGRES_USER:-root}"
POSTGRES_DB="${POSTGRES_DB:-}"
POSTGRES_IMAGE="${POSTGRES_IMAGE:-postgres:15}"
BACKUP_DIR="${BACKUP_DIR:-$ROOT_DIR/.storage-migration/backups}"
DUMP_FILE="${DUMP_FILE:-}"
GLOBALS_FILE="${GLOBALS_FILE:-}"
TARGET_DATA_DIR="${NEW_API_POSTGRES_DATA_DIR:-}"
TARGET_MARKER="${NEW_API_POSTGRES_MARKER:-}"
TARGET_MARKER_VALUE="${NEW_API_POSTGRES_MARKER_VALUE:-}"
COMPOSE_FILES="${COMPOSE_FILES:-docker-compose.yml:deploy/compose/docker-compose.storage-postgres-bind.yml}"
ENV_FILE="${ENV_FILE:-deploy/env/storage-postgres.env}"
ALLOW_NON_EMPTY_TARGET="${ALLOW_NON_EMPTY_TARGET:-false}"
POSTGRES_READY_TIMEOUT_SECONDS="${POSTGRES_READY_TIMEOUT_SECONDS:-60}"
MIGRATION_LOCK_DIR="${MIGRATION_LOCK_DIR:-/tmp/new-api-storage-migrate.lock}"
SKIP_DOCKER_ACTIVITY_CHECK="${SKIP_DOCKER_ACTIVITY_CHECK:-false}"
CONFIRM_STORAGE_MIGRATION="${CONFIRM_STORAGE_MIGRATION:-}"
BACKUP_MIN_FREE_PERCENT="${BACKUP_MIN_FREE_PERCENT:-30}"
PG_RESTORE_EXIT_ON_ERROR="${PG_RESTORE_EXIT_ON_ERROR:-true}"
PGCONNECT_TIMEOUT="${PGCONNECT_TIMEOUT:-30}"
ORIGINAL_ENCODING=""
ORIGINAL_LC_COLLATE=""
ORIGINAL_LC_CTYPE=""
ORIGINAL_OWNER=""
ORIGINAL_LOCALE_PROVIDER=""
ORIGINAL_ICU_LOCALE=""
CURRENT_PGDATA_KIB=""
SOURCE_POSTGRES_MAJOR=""
TARGET_POSTGRES_MAJOR=""
SOURCE_USERS_COUNT=""
SOURCE_CHANNELS_COUNT=""
SOURCE_TOKENS_COUNT=""
SOURCE_LOGS_COUNT=""
NEW_API_STOPPED="false"
POSTGRES_STOPPED="false"
POSTGRES_REPLACE_STARTED="false"
POSTGRES_REPLACED="false"
LOCK_ACQUIRED="false"
DUMP_INCOMPLETE_FILE=""
GLOBALS_INCOMPLETE_FILE=""

usage() {
  cat <<'USAGE'
Usage:
  POSTGRES_CONTAINER=<container> NEW_API_CONTAINER=<container> POSTGRES_DB=<database> scripts/storage-migrate-postgres.sh --dry-run
  POSTGRES_CONTAINER=<container> NEW_API_CONTAINER=<container> POSTGRES_DB=<database> scripts/storage-migrate-postgres.sh --execute

Environment:
  NEW_API_POSTGRES_DATA_DIR       Target PGDATA bind-mount path.
  NEW_API_POSTGRES_MARKER         Marker file path outside PGDATA.
  NEW_API_POSTGRES_MARKER_VALUE   Expected marker content.
  BACKUP_DIR                      Local backup directory.
  DUMP_FILE                       Optional existing dump file.
  GLOBALS_FILE                    Optional existing pg_dumpall --globals-only
                                  file for operator-managed role recovery.
  POSTGRES_CONTAINER              Required.
  NEW_API_CONTAINER               Required.
  POSTGRES_USER                   Default: root.
  POSTGRES_DB                     Required.
  POSTGRES_IMAGE                  Default: postgres:15.
  COMPOSE_FILES                   Colon-separated compose files.
  ENV_FILE                        Default: deploy/env/storage-postgres.env.
  ALLOW_NON_EMPTY_TARGET          Set true only when restoring into an existing
                                  target cluster is intentional.
  POSTGRES_READY_TIMEOUT_SECONDS  Default: 60.
  MIGRATION_LOCK_DIR              Default: /tmp/new-api-storage-migrate.lock.
  SKIP_DOCKER_ACTIVITY_CHECK      Set true only after manual process review.
  CONFIRM_STORAGE_MIGRATION       Required for --execute. Use the exact value
                                  printed by --dry-run for the same target.
  BACKUP_MIN_FREE_PERCENT         Minimum free space in BACKUP_DIR as a
                                  percentage of current PGDATA. Default: 30.
  PG_RESTORE_EXIT_ON_ERROR        Keep pg_restore --exit-on-error enabled by
                                  default. Set false only after manual review.

Safety:
  The default mode is --dry-run. --execute stops services and restores data.
USAGE
}

log() {
  printf '%s\n' "$*"
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

trim_trailing_slashes() {
  local path="$1"
  while [ "$path" != "/" ]; do
    case "$path" in
      [A-Za-z]:/) break ;;
    esac
    local trimmed="${path%/}"
    [ "$trimmed" != "$path" ] || break
    path="$trimmed"
  done
  printf '%s' "$path"
}

have() {
  command -v "$1" >/dev/null 2>&1
}

host_os() {
  uname -s 2>/dev/null || printf 'unknown'
}

is_windows_shell() {
  case "$(host_os)" in
    MINGW*|MSYS*|CYGWIN*) return 0 ;;
    *) return 1 ;;
  esac
}

is_windows_drive_path() {
  case "$1" in
    [A-Za-z]:/*) return 0 ;;
    *) return 1 ;;
  esac
}

compose_args() {
  local IFS=':'
  local file
  for file in $COMPOSE_FILES; do
    printf ' -f %q' "$file"
  done
  if [ -n "$ENV_FILE" ]; then
    printf ' --env-file %q' "$ENV_FILE"
  fi
}

compose_up_postgres() {
  local args=()
  local IFS=':'
  local file
  for file in $COMPOSE_FILES; do
    args+=("-f" "$file")
  done
  if [ -n "$ENV_FILE" ]; then
    args+=("--env-file" "$ENV_FILE")
  fi
  docker compose "${args[@]}" up -d --no-deps --force-recreate postgres
}

run() {
  if [ "$MODE" = "execute" ]; then
    log "run: $*"
    "$@"
  else
    log "would_run: $*"
  fi
}

on_error() {
  local rc=$?
  trap - ERR
  if [ -n "$DUMP_INCOMPLETE_FILE" ] && [ -f "$DUMP_INCOMPLETE_FILE" ]; then
    rm -f "$DUMP_INCOMPLETE_FILE" 2>/dev/null || true
  fi
  if [ -n "$GLOBALS_INCOMPLETE_FILE" ] && [ -f "$GLOBALS_INCOMPLETE_FILE" ]; then
    rm -f "$GLOBALS_INCOMPLETE_FILE" 2>/dev/null || true
  fi
  if [ "$MODE" = "execute" ] && [ "$POSTGRES_REPLACED" = "true" ]; then
    printf 'migration failed after postgres storage replacement; manual recovery is required\n' >&2
    printf 'mode=%s postgres_replaced=%s new_api_container=%s postgres_container=%s dump_file=%s\n' \
      "$MODE" "$POSTGRES_REPLACED" "$NEW_API_CONTAINER" "$POSTGRES_CONTAINER" "$DUMP_FILE" >&2
    printf 'Recovery options: restore the dump into the current target, or point compose back to the retained old storage and recreate postgres, then start %s.\n' "$NEW_API_CONTAINER" >&2
    printf 'See docs/operations/storage-acceleration.md section "Rollback boundary" for the explicit rollback sequence.\n' >&2
  elif [ "$MODE" = "execute" ] && [ "$POSTGRES_REPLACE_STARTED" = "true" ]; then
    printf 'migration failed while postgres storage replacement was in progress; manual recovery is required\n' >&2
    printf 'Do not assume the old postgres container still exists. Recreate postgres from the retained old storage or restore dump_file=%s into the target storage, then start %s.\n' "$DUMP_FILE" "$NEW_API_CONTAINER" >&2
    printf 'See docs/operations/storage-acceleration.md section "Rollback boundary" for the explicit rollback sequence.\n' >&2
  elif [ "$MODE" = "execute" ] && [ "$POSTGRES_STOPPED" = "true" ]; then
    printf 'migration failed after postgres was stopped but before replacement completed\n' >&2
    printf 'attempting to restart %s; new-api will stay stopped if postgres cannot be restored\n' "$POSTGRES_CONTAINER" >&2
    if docker start "$POSTGRES_CONTAINER" >/dev/null 2>&1 && [ "$NEW_API_STOPPED" = "true" ]; then
      docker start "$NEW_API_CONTAINER" >/dev/null 2>&1 || true
    fi
  elif [ "$MODE" = "execute" ] && [ "$NEW_API_STOPPED" = "true" ] && [ "$POSTGRES_REPLACED" != "true" ]; then
    printf 'migration failed before postgres replacement; attempting to restart %s\n' "$NEW_API_CONTAINER" >&2
    docker start "$NEW_API_CONTAINER" >/dev/null 2>&1 || true
  fi
  exit "$rc"
}

on_exit() {
  if [ "$LOCK_ACQUIRED" = "true" ]; then
    rm -f "$MIGRATION_LOCK_DIR/pid"
    rmdir "$MIGRATION_LOCK_DIR" 2>/dev/null || true
  fi
}

psql_query() {
  local sql="$1"
  docker exec -e PGCONNECT_TIMEOUT="$PGCONNECT_TIMEOUT" "$POSTGRES_CONTAINER" psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -Atc "$sql" | tr -d '\r'
}

psql_postgres() {
  local sql="$1"
  docker exec -e PGCONNECT_TIMEOUT="$PGCONNECT_TIMEOUT" "$POSTGRES_CONTAINER" psql -h 127.0.0.1 -U "$POSTGRES_USER" -d postgres -v ON_ERROR_STOP=1 -Atc "$sql" | tr -d '\r'
}

require_tools() {
  have docker || die "docker is required"
  have date || die "date is required"
  have cmp || die "cmp is required"
  if is_windows_shell; then
    have base64 || die "base64 is required on Windows shells for binary-safe pg_dump streaming"
  fi
  docker compose version >/dev/null 2>&1 || die "docker compose subcommand is required"
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
      --help|-h)
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

validate_inputs() {
  [ -n "$POSTGRES_CONTAINER" ] || die "POSTGRES_CONTAINER is required"
  [ -n "$NEW_API_CONTAINER" ] || die "NEW_API_CONTAINER is required"
  [ -n "$POSTGRES_DB" ] || die "POSTGRES_DB is required"
  [ -n "$TARGET_DATA_DIR" ] || die "NEW_API_POSTGRES_DATA_DIR is required"
  [ -n "$TARGET_MARKER" ] || die "NEW_API_POSTGRES_MARKER is required"
  [ -n "$TARGET_MARKER_VALUE" ] || die "NEW_API_POSTGRES_MARKER_VALUE is required"
  TARGET_DATA_DIR="$(trim_trailing_slashes "$TARGET_DATA_DIR")"
  TARGET_MARKER="$(trim_trailing_slashes "$TARGET_MARKER")"
  [ "$TARGET_DATA_DIR" != "/" ] || die "refusing to use / as PGDATA"
  [ "$TARGET_MARKER" != "/" ] || die "refusing to use / as marker"
  case "$TARGET_DATA_DIR" in
    /*|[A-Za-z]:/*) ;;
    *) die "NEW_API_POSTGRES_DATA_DIR must be an absolute path; Windows paths must use forward slashes such as C:/new-api/postgres/data" ;;
  esac
  case "$TARGET_MARKER" in
    /*|[A-Za-z]:/*) ;;
    *) die "NEW_API_POSTGRES_MARKER must be an absolute path; Windows paths must use forward slashes such as C:/new-api/postgres/storage.marker" ;;
  esac
  if is_windows_drive_path "$TARGET_DATA_DIR" || is_windows_drive_path "$TARGET_MARKER"; then
    if ! is_windows_shell; then
      die "Windows drive paths are only supported from Windows shells; use a Unix absolute path on macOS, Linux, or WSL2"
    fi
  fi
  case "$TARGET_DATA_DIR" in
    *"/../"*|*"/./"*|*"/.."|*"/."|../*|./*|".."|".") die "NEW_API_POSTGRES_DATA_DIR must be a normalized path without dot segments" ;;
  esac
  case "$TARGET_MARKER" in
    *"/../"*|*"/./"*|*"/.."|*"/."|../*|./*|".."|".") die "NEW_API_POSTGRES_MARKER must be a normalized path without dot segments" ;;
  esac
  case "$TARGET_DATA_DIR" in
    /var/lib/postgresql/data|/var/lib/postgresql/data/*) die "target must be a host path, not a container path" ;;
  esac
  case "$POSTGRES_READY_TIMEOUT_SECONDS" in
    ''|*[!0-9]*) die "POSTGRES_READY_TIMEOUT_SECONDS must be a positive integer" ;;
  esac
  [ "$POSTGRES_READY_TIMEOUT_SECONDS" -gt 0 ] || die "POSTGRES_READY_TIMEOUT_SECONDS must be greater than 0"
  case "$BACKUP_MIN_FREE_PERCENT" in
    ''|*[!0-9]*) die "BACKUP_MIN_FREE_PERCENT must be a positive integer" ;;
  esac
  [ "$BACKUP_MIN_FREE_PERCENT" -gt 0 ] || die "BACKUP_MIN_FREE_PERCENT must be greater than 0"
  if [ "$MODE" = "execute" ] && [ -n "$ENV_FILE" ]; then
    [ -f "$ENV_FILE" ] || die "ENV_FILE does not exist: $ENV_FILE"
    [ -r "$ENV_FILE" ] || die "ENV_FILE is not readable: $ENV_FILE"
  fi
  if [ -n "$DUMP_FILE" ]; then
    [ -f "$DUMP_FILE" ] || die "DUMP_FILE does not exist: $DUMP_FILE"
    [ -r "$DUMP_FILE" ] || die "DUMP_FILE is not readable: $DUMP_FILE"
    [ -s "$DUMP_FILE" ] || die "DUMP_FILE is empty: $DUMP_FILE"
  fi
  if [ -n "$GLOBALS_FILE" ]; then
    [ -f "$GLOBALS_FILE" ] || die "GLOBALS_FILE does not exist: $GLOBALS_FILE"
    [ -r "$GLOBALS_FILE" ] || die "GLOBALS_FILE is not readable: $GLOBALS_FILE"
    [ -s "$GLOBALS_FILE" ] || die "GLOBALS_FILE is empty: $GLOBALS_FILE"
  fi
  local marker_dir
  marker_dir="$(dirname "$TARGET_MARKER")"
  if [ -d "$TARGET_DATA_DIR" ] && [ -d "$marker_dir" ]; then
    local normalized_data normalized_marker_dir normalized_marker
    normalized_data="$(cd "$TARGET_DATA_DIR" && pwd -P)"
    normalized_marker_dir="$(cd "$marker_dir" && pwd -P)"
    normalized_marker="$normalized_marker_dir/$(basename "$TARGET_MARKER")"
    case "$normalized_marker" in
      "$normalized_data"|"$normalized_data"/*) die "NEW_API_POSTGRES_MARKER must be outside NEW_API_POSTGRES_DATA_DIR" ;;
    esac
  else
    case "$TARGET_MARKER" in
      "$TARGET_DATA_DIR"|"$TARGET_DATA_DIR"/*) die "NEW_API_POSTGRES_MARKER must be outside NEW_API_POSTGRES_DATA_DIR" ;;
    esac
  fi
}

describe_target_storage() {
  log "target_data_dir_exists=$([ -d "$TARGET_DATA_DIR" ] && printf true || printf false)"
  log "target_data_dir_writable=$([ -w "$TARGET_DATA_DIR" ] && printf true || printf false)"
  if [ -d "$TARGET_DATA_DIR" ]; then
    local target_entries
    if target_entries="$(find "$TARGET_DATA_DIR" -mindepth 1 -maxdepth 1 | wc -l | tr -d ' ')"; then
      log "target_data_dir_entries=$target_entries"
    else
      log "target_data_dir_entries=inspect-failed"
    fi
    if [ -f "$TARGET_DATA_DIR/PG_VERSION" ]; then
      log "target_data_dir_pg_version=$(tr -d '\r\n' < "$TARGET_DATA_DIR/PG_VERSION")"
      log "target_data_dir_looks_like_pgdata=true"
    else
      log "target_data_dir_looks_like_pgdata=false"
    fi
    df -k "$TARGET_DATA_DIR" 2>/dev/null | awk 'NR == 2 { print "target_available_kib=" $4 }' || true
  fi

  log "target_marker_exists=$([ -f "$TARGET_MARKER" ] && printf true || printf false)"
  if [ -f "$TARGET_MARKER" ]; then
    if printf '%s' "$TARGET_MARKER_VALUE" | cmp -s "$TARGET_MARKER" -; then
      log "target_marker_match=true"
    else
      log "target_marker_match=false"
    fi
  fi
}

expected_confirmation() {
  printf 'execute:%s:%s:%s:%s' "$POSTGRES_CONTAINER" "$NEW_API_CONTAINER" "$POSTGRES_DB" "$TARGET_DATA_DIR"
}

confirm_execute() {
  local expected
  expected="$(expected_confirmation)"
  log "execute_confirmation_required=$expected"
  if [ "$MODE" != "execute" ]; then
    return
  fi
  if [ "$CONFIRM_STORAGE_MIGRATION" != "$expected" ]; then
    die "CONFIRM_STORAGE_MIGRATION must exactly equal: $expected"
  fi
}

acquire_lock() {
  if [ "$MODE" != "execute" ]; then
    return
  fi
  [ -n "$MIGRATION_LOCK_DIR" ] || die "MIGRATION_LOCK_DIR is required"
  [ "$MIGRATION_LOCK_DIR" != "/" ] || die "refusing to use / as MIGRATION_LOCK_DIR"
  if mkdir "$MIGRATION_LOCK_DIR" 2>/dev/null; then
    printf '%s\n' "$$" > "$MIGRATION_LOCK_DIR/pid"
    LOCK_ACQUIRED="true"
    return
  fi
  local existing_pid
  existing_pid="$(cat "$MIGRATION_LOCK_DIR/pid" 2>/dev/null || true)"
  case "$existing_pid" in
    ''|*[!0-9]*)
      rm -f "$MIGRATION_LOCK_DIR/pid" 2>/dev/null || true
      ;;
    *)
      if kill -0 "$existing_pid" 2>/dev/null; then
        die "another storage migration appears to be running: $MIGRATION_LOCK_DIR pid=$existing_pid"
      fi
      rm -f "$MIGRATION_LOCK_DIR/pid" 2>/dev/null || true
      ;;
  esac
  # If another process wins the rmdir/mkdir race, fail closed instead of retrying into a possible concurrent migration.
  if rmdir "$MIGRATION_LOCK_DIR" 2>/dev/null && mkdir "$MIGRATION_LOCK_DIR" 2>/dev/null; then
    printf '%s\n' "$$" > "$MIGRATION_LOCK_DIR/pid"
    LOCK_ACQUIRED="true"
    return
  fi
  die "another storage migration appears to be running: $MIGRATION_LOCK_DIR"
}

check_docker_activity() {
  if [ "$MODE" != "execute" ] || [ "$SKIP_DOCKER_ACTIVITY_CHECK" = "true" ]; then
    return
  fi
  local matches
  local docker_activity_pattern
  docker_activity_pattern='(^|[[:space:]/])(docker([[:space:]]+(compose|build|buildx)|$)|docker-compose([[:space:]]|$)|scripts/build-docker-local\.sh([[:space:]]|$))'
  if have pgrep; then
    matches="$(pgrep -af "$docker_activity_pattern" || true)"
  elif is_windows_shell; then
    log "docker_activity_check=skipped windows_shell_no_pgrep"
    return
  else
    # shellcheck disable=SC2009
    matches="$(ps -eo pid=,args= | grep -E "$docker_activity_pattern" | grep -v grep || true)"
  fi
  if [ -n "$matches" ]; then
    printf '%s\n' "$matches" >&2
    die "concurrent Docker build or compose activity detected"
  fi
}

preflight() {
  log "mode=$MODE"
  log "target_data_dir=$TARGET_DATA_DIR"
  log "target_marker=$TARGET_MARKER"
  log "compose_files=$COMPOSE_FILES"
  log "env_file=$ENV_FILE"
  if [ -n "$ENV_FILE" ]; then
    log "env_file_exists=$([ -f "$ENV_FILE" ] && printf true || printf false)"
  fi
  docker inspect "$POSTGRES_CONTAINER" >/dev/null || die "postgres container not found: $POSTGRES_CONTAINER"

  log "current postgres mounts:"
  docker inspect --format '{{range .Mounts}}{{println .Type .Source "->" .Destination}}{{end}}' "$POSTGRES_CONTAINER"
  CURRENT_PGDATA_KIB="$(docker exec "$POSTGRES_CONTAINER" sh -ec 'du -sk "${PGDATA:-/var/lib/postgresql/data}"' 2>/dev/null | awk '{ print $1 }' || true)"
  log "current_pgdata_kib=${CURRENT_PGDATA_KIB:-unknown}"

  log "current counts:"
  SOURCE_USERS_COUNT="$(psql_query "select count(*) from users;")"
  SOURCE_CHANNELS_COUNT="$(psql_query "select count(*) from channels;")"
  SOURCE_TOKENS_COUNT="$(psql_query "select count(*) from tokens;")"
  SOURCE_LOGS_COUNT="$(psql_query "select count(*) from logs;")"
  log "users=$SOURCE_USERS_COUNT"
  log "channels=$SOURCE_CHANNELS_COUNT"
  log "tokens=$SOURCE_TOKENS_COUNT"
  log "logs=$SOURCE_LOGS_COUNT"
  psql_query "select 'latest_log_created_at=' || coalesce(max(created_at)::text, '') from logs;"
}

capture_postgres_versions() {
  local source_version_num target_version
  source_version_num="$(psql_query "show server_version_num;" | head -n 1)"
  case "$source_version_num" in
    ''|*[!0-9]*) die "failed to read source PostgreSQL server_version_num" ;;
  esac
  SOURCE_POSTGRES_MAJOR=$((source_version_num / 10000))
  log "source_postgres_major=$SOURCE_POSTGRES_MAJOR"
  if [ "$MODE" != "execute" ]; then
    log "would inspect target postgres image version: $POSTGRES_IMAGE"
    return
  fi
  target_version="$(docker run --rm --entrypoint postgres "$POSTGRES_IMAGE" --version)"
  target_version="${target_version##*PostgreSQL) }"
  target_version="${target_version%% *}"
  TARGET_POSTGRES_MAJOR="${target_version%%.*}"
  case "$TARGET_POSTGRES_MAJOR" in
    ''|*[!0-9]*) die "failed to parse target PostgreSQL major version from image $POSTGRES_IMAGE" ;;
  esac
  log "target_postgres_major=$TARGET_POSTGRES_MAJOR"
  if [ "$TARGET_POSTGRES_MAJOR" -lt "$SOURCE_POSTGRES_MAJOR" ]; then
    die "target PostgreSQL major version must be >= source version: source=$SOURCE_POSTGRES_MAJOR target=$TARGET_POSTGRES_MAJOR"
  fi
}

capture_original_database_settings() {
  local has_locale_provider settings
  has_locale_provider="$(psql_query "
    select case when exists (
      select 1
      from pg_attribute
      where attrelid = 'pg_catalog.pg_database'::regclass
        and attname = 'datlocprovider'
        and not attisdropped
    ) then 'true' else 'false' end;
  ")"
  if [ "$has_locale_provider" = "true" ]; then
    settings="$(psql_query "
      select
        pg_encoding_to_char(encoding),
        datcollate,
        datctype,
        pg_catalog.pg_get_userbyid(datdba),
        datlocprovider,
        coalesce(daticulocale, '')
      from pg_database
      where datname = current_database();
    ")"
    IFS='|' read -r ORIGINAL_ENCODING ORIGINAL_LC_COLLATE ORIGINAL_LC_CTYPE ORIGINAL_OWNER ORIGINAL_LOCALE_PROVIDER ORIGINAL_ICU_LOCALE <<< "$settings"
  else
    settings="$(psql_query "
      select
        pg_encoding_to_char(encoding),
        datcollate,
        datctype,
        pg_catalog.pg_get_userbyid(datdba)
      from pg_database
      where datname = current_database();
    ")"
    IFS='|' read -r ORIGINAL_ENCODING ORIGINAL_LC_COLLATE ORIGINAL_LC_CTYPE ORIGINAL_OWNER <<< "$settings"
    ORIGINAL_LOCALE_PROVIDER="c"
    ORIGINAL_ICU_LOCALE=""
  fi
  [ -n "$ORIGINAL_ENCODING" ] || die "failed to read original database encoding"
  [ -n "$ORIGINAL_LC_COLLATE" ] || die "failed to read original database lc_collate"
  [ -n "$ORIGINAL_LC_CTYPE" ] || die "failed to read original database lc_ctype"
  [ -n "$ORIGINAL_OWNER" ] || die "failed to read original database owner"
  [ -n "$ORIGINAL_LOCALE_PROVIDER" ] || die "failed to read original database locale provider"
  log "original_database_encoding=$ORIGINAL_ENCODING"
  log "original_database_lc_collate=$ORIGINAL_LC_COLLATE"
  log "original_database_lc_ctype=$ORIGINAL_LC_CTYPE"
  log "original_database_owner=$ORIGINAL_OWNER"
  log "original_database_locale_provider=$ORIGINAL_LOCALE_PROVIDER"
  log "original_database_icu_locale=$ORIGINAL_ICU_LOCALE"
}

validate_backup_target() {
  if [ "$MODE" != "execute" ]; then
    log "would validate BACKUP_DIR free space"
    return
  fi
  mkdir -p "$BACKUP_DIR"
  [ -d "$BACKUP_DIR" ] || die "BACKUP_DIR is not a directory: $BACKUP_DIR"
  [ -w "$BACKUP_DIR" ] || die "BACKUP_DIR is not writable: $BACKUP_DIR"
  case "$CURRENT_PGDATA_KIB" in
    ''|*[!0-9]*) die "current PGDATA size is unknown; refusing to execute migration" ;;
  esac
  local available_kib required_kib
  available_kib="$(df -k "$BACKUP_DIR" | awk 'NR == 2 { print $4 }')"
  case "$available_kib" in
    ''|*[!0-9]*) die "failed to read BACKUP_DIR available space: $BACKUP_DIR" ;;
  esac
  required_kib=$(((CURRENT_PGDATA_KIB * BACKUP_MIN_FREE_PERCENT + 99) / 100))
  log "backup_available_kib=$available_kib"
  log "backup_required_kib=$required_kib"
  if [ "$available_kib" -lt "$required_kib" ]; then
    die "BACKUP_DIR free space is below required threshold: available_kib=$available_kib required_kib=$required_kib"
  fi
}

ensure_marker() {
  [ -f "$TARGET_MARKER" ] || die "target marker is missing; create it explicitly before migration: $TARGET_MARKER"
  if ! printf '%s' "$TARGET_MARKER_VALUE" | cmp -s "$TARGET_MARKER" -; then
    die "existing marker content does not match expected value"
  fi

  if [ "$MODE" = "execute" ]; then
    mkdir -p "$TARGET_DATA_DIR"
    local target_entries
    if ! target_entries="$(find "$TARGET_DATA_DIR" -mindepth 1 -maxdepth 1 | wc -l | tr -d ' ')"; then
      die "failed to inspect target data directory: $TARGET_DATA_DIR"
    fi
    if [ "$target_entries" != "0" ] && [ "$ALLOW_NON_EMPTY_TARGET" != "true" ]; then
      die "target data directory is not empty; set ALLOW_NON_EMPTY_TARGET=true only after manual review"
    fi
  else
    log "marker content verified"
    log "would ensure target directory"
  fi
}

stream_pg_dump_to_file() {
  local output_file="$1"
  if is_windows_shell; then
    docker exec "$POSTGRES_CONTAINER" sh -ec 'pg_dump -U "$1" -d "$2" -Fc | base64' sh "$POSTGRES_USER" "$POSTGRES_DB" | base64 -d > "$output_file"
  else
    docker exec "$POSTGRES_CONTAINER" pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc > "$output_file"
  fi
}

restore_pg_dump_file() {
  local abs_dump_file="$1"
  local restore_args=()
  if [ "$PG_RESTORE_EXIT_ON_ERROR" != "false" ]; then
    restore_args+=("--exit-on-error")
  fi
  restore_args+=("-h" "127.0.0.1" "-U" "$POSTGRES_USER" "-d" "$POSTGRES_DB")
  if is_windows_shell; then
    base64 "$abs_dump_file" | docker exec -i "$POSTGRES_CONTAINER" sh -ec 'base64 -d | pg_restore "$@"' sh "${restore_args[@]}"
  else
    docker exec -i "$POSTGRES_CONTAINER" pg_restore "${restore_args[@]}" < "$abs_dump_file"
  fi
}

create_dump() {
  if [ -n "$DUMP_FILE" ]; then
    log "using existing dump: $DUMP_FILE"
    if [ -n "$GLOBALS_FILE" ]; then
      log "using existing globals file: $GLOBALS_FILE"
    else
      log "globals_file=not-provided"
    fi
    return
  fi
  local timestamp
  timestamp="$(date -u '+%Y%m%dT%H%M%SZ')"
  DUMP_FILE="$BACKUP_DIR/${POSTGRES_DB}-${timestamp}.dump"
  GLOBALS_FILE="$BACKUP_DIR/${POSTGRES_DB}-${timestamp}-globals.sql"
  if [ "$MODE" = "execute" ]; then
    mkdir -p "$BACKUP_DIR"
    local tmp_dump tmp_globals
    tmp_dump="$DUMP_FILE.incomplete"
    tmp_globals="$GLOBALS_FILE.incomplete"
    DUMP_INCOMPLETE_FILE="$tmp_dump"
    GLOBALS_INCOMPLETE_FILE="$tmp_globals"
    rm -f "$tmp_dump"
    rm -f "$tmp_globals"
    log "run: docker exec $POSTGRES_CONTAINER pg_dumpall -U $POSTGRES_USER --globals-only --no-role-passwords > $GLOBALS_FILE"
    if ! docker exec "$POSTGRES_CONTAINER" pg_dumpall -U "$POSTGRES_USER" --globals-only --no-role-passwords > "$tmp_globals"; then
      rm -f "$tmp_globals"
      GLOBALS_INCOMPLETE_FILE=""
      return 1
    fi
    [ -s "$tmp_globals" ] || die "globals dump is empty: $tmp_globals"
    mv "$tmp_globals" "$GLOBALS_FILE"
    GLOBALS_INCOMPLETE_FILE=""
    log "run: docker exec $POSTGRES_CONTAINER pg_dump -U $POSTGRES_USER -d $POSTGRES_DB -Fc > $DUMP_FILE"
    if ! stream_pg_dump_to_file "$tmp_dump"; then
      rm -f "$tmp_dump"
      DUMP_INCOMPLETE_FILE=""
      return 1
    fi
    verify_dump_path "$tmp_dump"
    mv "$tmp_dump" "$DUMP_FILE"
    DUMP_INCOMPLETE_FILE=""
  else
    log "would_run: docker exec $POSTGRES_CONTAINER pg_dumpall -U $POSTGRES_USER --globals-only --no-role-passwords > $GLOBALS_FILE"
    log "would_run: docker exec $POSTGRES_CONTAINER pg_dump -U $POSTGRES_USER -d $POSTGRES_DB -Fc > $DUMP_FILE"
  fi
  log "globals_file=$GLOBALS_FILE"
  log "dump_file=$DUMP_FILE"
}

verify_dump_path() {
  local dump_path="$1"
  [ -f "$dump_path" ] || die "dump file missing: $dump_path"
  local abs_dump_file
  abs_dump_file="$(cd "$(dirname "$dump_path")" && pwd -P)/$(basename "$dump_path")"
  docker run --rm -v "$(dirname "$abs_dump_file"):/backup:ro" "$POSTGRES_IMAGE" \
    pg_restore -l "/backup/$(basename "$abs_dump_file")" >/dev/null
}

verify_dump() {
  if [ "$MODE" = "execute" ]; then
    verify_dump_path "$DUMP_FILE"
  else
    log "would verify dump with pg_restore -l"
  fi
}

stop_new_api() {
  run docker stop "$NEW_API_CONTAINER"
  if [ "$MODE" = "execute" ]; then
    NEW_API_STOPPED="true"
  fi
}

replace_postgres_storage() {
  log "The next steps require the compose overlay and env file to point postgres at the target bind mount."
  log "compose command: docker compose$(compose_args) up -d --no-deps --force-recreate postgres"
  run docker stop "$POSTGRES_CONTAINER"
  if [ "$MODE" = "execute" ]; then
    POSTGRES_STOPPED="true"
  fi
  if [ "$MODE" = "execute" ]; then
    log "run: docker compose$(compose_args) up -d --no-deps --force-recreate postgres"
    POSTGRES_REPLACE_STARTED="true"
    compose_up_postgres
    POSTGRES_REPLACED="true"
    POSTGRES_STOPPED="false"
    assert_postgres_container_running
  else
    log "would_run: docker compose$(compose_args) up -d --no-deps --force-recreate postgres"
  fi
}

assert_postgres_container_running() {
  local status exit_code
  status="$(docker inspect --format '{{.State.Status}}' "$POSTGRES_CONTAINER" 2>/dev/null || true)"
  if [ "$status" = "running" ]; then
    return
  fi
  exit_code="$(docker inspect --format '{{.State.ExitCode}}' "$POSTGRES_CONTAINER" 2>/dev/null || true)"
  printf 'postgres container is not running after compose recreation: status=%s exit_code=%s\n' "${status:-unknown}" "${exit_code:-unknown}" >&2
  docker logs --tail 120 "$POSTGRES_CONTAINER" >&2 || true
  return 1
}

wait_for_postgres() {
  if [ "$MODE" != "execute" ]; then
    log "would wait for postgres readiness"
    return
  fi

  local start now
  start="$(date +%s)"
  while true; do
    if docker exec "$POSTGRES_CONTAINER" pg_isready -h 127.0.0.1 -U "$POSTGRES_USER" -d postgres >/dev/null 2>&1; then
      return
    fi
    now="$(date +%s)"
    if [ $((now - start)) -ge "$POSTGRES_READY_TIMEOUT_SECONDS" ]; then
      die "postgres did not become ready within ${POSTGRES_READY_TIMEOUT_SECONDS}s"
    fi
    sleep 2
  done
}

restore_dump() {
  if [ "$MODE" = "execute" ]; then
    local abs_dump_file
    abs_dump_file="$(cd "$(dirname "$DUMP_FILE")" && pwd -P)/$(basename "$DUMP_FILE")"
    local locale_args=()
    if [ "$TARGET_POSTGRES_MAJOR" -ge 15 ]; then
      case "$ORIGINAL_LOCALE_PROVIDER" in
        c)
          locale_args=("--locale-provider=libc")
          ;;
        i)
          [ -n "$ORIGINAL_ICU_LOCALE" ] || die "original database uses ICU locale provider but daticulocale is empty"
          locale_args=("--locale-provider=icu" "--icu-locale=$ORIGINAL_ICU_LOCALE")
          ;;
        *)
          die "unsupported original database locale provider: $ORIGINAL_LOCALE_PROVIDER"
          ;;
      esac
    elif [ "$ORIGINAL_LOCALE_PROVIDER" = "i" ]; then
      die "target PostgreSQL major version does not support ICU locale provider flags: target=$TARGET_POSTGRES_MAJOR"
    fi
    ensure_database_owner_exists
    if ! docker exec "$POSTGRES_CONTAINER" dropdb -h 127.0.0.1 -U "$POSTGRES_USER" --if-exists "$POSTGRES_DB"; then
      return 1
    fi
    if ! docker exec "$POSTGRES_CONTAINER" createdb -h 127.0.0.1 -U "$POSTGRES_USER" \
      --template=template0 \
      --encoding="$ORIGINAL_ENCODING" \
      --lc-collate="$ORIGINAL_LC_COLLATE" \
      --lc-ctype="$ORIGINAL_LC_CTYPE" \
      "${locale_args[@]}" \
      --owner="$ORIGINAL_OWNER" \
      "$POSTGRES_DB"; then
      return 1
    fi
    if ! restore_pg_dump_file "$abs_dump_file"; then
      return 1
    fi
  else
    log "would restore dump into recreated postgres"
  fi
}

ensure_database_owner_exists() {
  case "$ORIGINAL_OWNER" in
    ''|*[!A-Za-z0-9_]*)
      die "refusing to create unsafe database owner identifier: $ORIGINAL_OWNER"
      ;;
  esac
  local owner_sql
  owner_sql="${ORIGINAL_OWNER//\'/\'\'}"
  psql_postgres "do \$\$
declare role_name text := '$owner_sql';
begin
  if not exists (select 1 from pg_roles where rolname = role_name) then
    execute format('CREATE ROLE %I', role_name);
  end if;
end
\$\$;" >/dev/null
}

start_new_api() {
  run docker start "$NEW_API_CONTAINER"
  if [ "$MODE" = "execute" ]; then
    local status
    status="$(docker inspect --format '{{.State.Status}}' "$NEW_API_CONTAINER" 2>/dev/null || true)"
    if [ "$status" != "running" ]; then
      docker logs --tail 120 "$NEW_API_CONTAINER" >&2 || true
      die "new-api container did not stay running after start: status=${status:-unknown}"
    fi
    NEW_API_STOPPED="false"
  fi
}

post_verify() {
  if [ "$MODE" = "execute" ]; then
    POSTGRES_CONTAINER="$POSTGRES_CONTAINER" \
      POSTGRES_USER="$POSTGRES_USER" \
      POSTGRES_DB="$POSTGRES_DB" \
      NEW_API_POSTGRES_DATA_DIR="$TARGET_DATA_DIR" \
      NEW_API_POSTGRES_MARKER="$TARGET_MARKER" \
      NEW_API_POSTGRES_MARKER_VALUE="$TARGET_MARKER_VALUE" \
      NEW_API_EXPECT_USERS_COUNT="$SOURCE_USERS_COUNT" \
      NEW_API_EXPECT_CHANNELS_COUNT="$SOURCE_CHANNELS_COUNT" \
      NEW_API_EXPECT_TOKENS_COUNT="$SOURCE_TOKENS_COUNT" \
      NEW_API_EXPECT_LOGS_COUNT="$SOURCE_LOGS_COUNT" \
      "$ROOT_DIR/scripts/storage-verify-postgres.sh"
  else
    log "would_run: POSTGRES_CONTAINER=$POSTGRES_CONTAINER POSTGRES_USER=$POSTGRES_USER POSTGRES_DB=$POSTGRES_DB NEW_API_POSTGRES_DATA_DIR=$TARGET_DATA_DIR NEW_API_POSTGRES_MARKER=$TARGET_MARKER NEW_API_POSTGRES_MARKER_VALUE=$TARGET_MARKER_VALUE NEW_API_EXPECT_USERS_COUNT=$SOURCE_USERS_COUNT NEW_API_EXPECT_CHANNELS_COUNT=$SOURCE_CHANNELS_COUNT NEW_API_EXPECT_TOKENS_COUNT=$SOURCE_TOKENS_COUNT NEW_API_EXPECT_LOGS_COUNT=$SOURCE_LOGS_COUNT $ROOT_DIR/scripts/storage-verify-postgres.sh"
  fi
}

main() {
  trap on_error ERR
  trap on_exit EXIT
  cd "$ROOT_DIR"
  parse_args "$@"
  require_tools
  validate_inputs
  confirm_execute
  acquire_lock
  check_docker_activity
  describe_target_storage
  preflight
  capture_postgres_versions
  capture_original_database_settings
  validate_backup_target
  ensure_marker
  stop_new_api
  create_dump
  verify_dump
  replace_postgres_storage
  wait_for_postgres
  restore_dump
  start_new_api
  post_verify
}

main "$@"
