#!/usr/bin/env bash
# shellcheck disable=SC2329 # check functions are invoked indirectly through run_check.
set -euo pipefail

POSTGRES_CONTAINER_SOURCE="default"
POSTGRES_USER_SOURCE="default"
POSTGRES_DB_SOURCE="default"
NEW_API_STATUS_URL_SOURCE="default"
[ -n "${POSTGRES_CONTAINER:-}" ] && POSTGRES_CONTAINER_SOURCE="env"
[ -n "${POSTGRES_USER:-}" ] && POSTGRES_USER_SOURCE="env"
[ -n "${POSTGRES_DB:-}" ] && POSTGRES_DB_SOURCE="env"
[ -n "${NEW_API_STATUS_URL:-}" ] && NEW_API_STATUS_URL_SOURCE="env"

POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-postgres}"
POSTGRES_USER="${POSTGRES_USER:-root}"
POSTGRES_DB="${POSTGRES_DB:-new-api}"
NEW_API_STATUS_URL="${NEW_API_STATUS_URL:-http://127.0.0.1:3000/api/status}"
NEW_API_BASE_URL="${NEW_API_BASE_URL:-}"
NEW_API_LOGIN_URL="${NEW_API_LOGIN_URL:-}"
NEW_API_AUTH_COOKIE="${NEW_API_AUTH_COOKIE:-}"
NEW_API_USER_ID="${NEW_API_USER_ID:-}"
NEW_API_POSTGRES_DATA_DIR="${NEW_API_POSTGRES_DATA_DIR:-}"
NEW_API_POSTGRES_MARKER="${NEW_API_POSTGRES_MARKER:-}"
NEW_API_POSTGRES_MARKER_VALUE="${NEW_API_POSTGRES_MARKER_VALUE:-}"
POSTGRES_MARKER_CONTAINER_PATH="${POSTGRES_MARKER_CONTAINER_PATH:-/run/new-api/postgres-storage.marker}"
NEW_API_EXPECT_USERS_COUNT="${NEW_API_EXPECT_USERS_COUNT:-}"
NEW_API_EXPECT_CHANNELS_COUNT="${NEW_API_EXPECT_CHANNELS_COUNT:-}"
NEW_API_EXPECT_TOKENS_COUNT="${NEW_API_EXPECT_TOKENS_COUNT:-}"
NEW_API_EXPECT_LOGS_COUNT="${NEW_API_EXPECT_LOGS_COUNT:-}"
CURL_CONNECT_TIMEOUT="${CURL_CONNECT_TIMEOUT:-5}"
CURL_MAX_TIME="${CURL_MAX_TIME:-10}"
FAILURES=0
TEMP_FILES=()
URLS_READY="false"

cleanup_temp_files() {
  if [ "${#TEMP_FILES[@]}" -gt 0 ]; then
    rm -f "${TEMP_FILES[@]}"
  fi
}

trap cleanup_temp_files EXIT

have() {
  command -v "$1" >/dev/null 2>&1
}

section() {
  printf '\n## %s\n' "$1"
}

psql_query() {
  local sql="$1"
  local output rc
  local err_file
  err_file="$(mktemp)"
  TEMP_FILES+=("$err_file")
  set +e
  output="$(docker exec "$POSTGRES_CONTAINER" psql -q -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atc "$sql" 2>"$err_file" | tr -d '\r')"
  rc=$?
  set -e
  if [ "$rc" -ne 0 ]; then
    printf 'psql_query_failed container=%s db=%s rc=%s\n' "$POSTGRES_CONTAINER" "$POSTGRES_DB" "$rc" >&2
    printf 'sql=%s\n' "$sql" >&2
    cat "$err_file" >&2
    return "$rc"
  fi
  printf '%s\n' "$output"
}

run_check() {
  local name="$1"
  shift
  if ! "$@"; then
    printf 'check_failed=%s\n' "$name" >&2
    FAILURES=$((FAILURES + 1))
  fi
}

init_urls() {
  if [ -z "$NEW_API_BASE_URL" ]; then
    case "$NEW_API_STATUS_URL" in
      */api/status)
        NEW_API_BASE_URL="${NEW_API_STATUS_URL%/api/status}"
        ;;
      *)
        printf 'NEW_API_STATUS_URL must end with /api/status unless NEW_API_BASE_URL is set\n' >&2
        return 1
        ;;
    esac
  fi
  NEW_API_BASE_URL="${NEW_API_BASE_URL%/}"
  case "$NEW_API_BASE_URL" in
    http://*|https://*) ;;
    *)
      printf 'NEW_API_BASE_URL must start with http:// or https://, got: %s\n' "$NEW_API_BASE_URL" >&2
      return 1
      ;;
  esac
  if [ -z "$NEW_API_LOGIN_URL" ]; then
    NEW_API_LOGIN_URL="$NEW_API_BASE_URL/login"
  fi
}

verify_postgres_ready() {
  section "postgres-ready"
  local status restart_count
  status="$(docker inspect --format '{{.State.Status}}' "$POSTGRES_CONTAINER" 2>/dev/null || true)"
  restart_count="$(docker inspect --format '{{.RestartCount}}' "$POSTGRES_CONTAINER" 2>/dev/null || true)"
  printf 'container_status=%s\n' "${status:-unknown}"
  printf 'restart_count=%s\n' "${restart_count:-unknown}"
  docker exec "$POSTGRES_CONTAINER" pg_isready -h 127.0.0.1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" >/dev/null
  psql_query "select 1;" >/dev/null || return
  printf 'ready=true\n'
}

verify_required_tables() {
  section "required-tables"
  local missing
  if ! missing="$(psql_query "
    with required(name) as (
      values ('users'), ('channels'), ('tokens'), ('logs')
    )
    select name from required
    where not exists (
      select 1 from information_schema.tables
      where table_schema = 'public' and table_name = required.name
    )
    order by name;
  ")"; then
    return 1
  fi
  if [ -n "$missing" ]; then
    printf 'missing_tables=%s\n' "$missing" >&2
    return 1
  fi
  printf 'required_tables=present\n'
}

verify_table_counts() {
  section "table-counts"
  local users channels tokens logs
  users="$(psql_query "select count(*) from users;")" || return
  channels="$(psql_query "select count(*) from channels;")" || return
  tokens="$(psql_query "select count(*) from tokens;")" || return
  logs="$(psql_query "select count(*) from logs;")" || return
  printf 'users=%s\n' "$users"
  printf 'channels=%s\n' "$channels"
  printf 'tokens=%s\n' "$tokens"
  printf 'logs=%s\n' "$logs"
  verify_expected_count "users" "$users" "$NEW_API_EXPECT_USERS_COUNT" || return
  verify_expected_count "channels" "$channels" "$NEW_API_EXPECT_CHANNELS_COUNT" || return
  verify_expected_count "tokens" "$tokens" "$NEW_API_EXPECT_TOKENS_COUNT" || return
  verify_expected_count "logs" "$logs" "$NEW_API_EXPECT_LOGS_COUNT" || return
}

verify_expected_count() {
  local name="$1"
  local actual="$2"
  local expected="$3"
  if [ -z "$expected" ]; then
    return 0
  fi
  if [ "$actual" != "$expected" ]; then
    printf 'count_mismatch table=%s expected=%s actual=%s\n' "$name" "$expected" "$actual" >&2
    return 1
  fi
}

verify_latest_rows() {
  section "latest-rows"
  psql_query "
    select 'latest_log_created_at=' || coalesce(max(created_at)::text, '') from logs;
    select 'latest_channel_created_time=' || coalesce(max(created_time)::text, '') from channels;
  " || return
}

verify_postgres_paths() {
  section "postgres-paths"
  local data_directory fsync wal_sync_method
  data_directory="$(psql_query "show data_directory;" | head -n 1)" || return
  fsync="$(psql_query "show fsync;" | head -n 1)" || return
  wal_sync_method="$(psql_query "show wal_sync_method;" | head -n 1)" || return
  printf 'data_directory=%s\n' "$data_directory"
  printf 'fsync=%s\n' "$fsync"
  printf 'wal_sync_method=%s\n' "$wal_sync_method"
  docker inspect --format '{{range .Mounts}}mount_type={{.Type}} source={{.Source}} destination={{.Destination}}{{println}}{{end}}' "$POSTGRES_CONTAINER" || return 1
}

verify_postgres_tuning() {
  section "postgres-tuning"
  psql_query "
    select name || '=' || setting || coalesce(' ' || unit, '')
    from pg_settings
    where name in (
      'shared_buffers',
      'effective_cache_size',
      'maintenance_work_mem',
      'work_mem',
      'checkpoint_timeout',
      'max_wal_size',
      'min_wal_size',
      'checkpoint_completion_target',
      'wal_compression',
      'effective_io_concurrency',
      'random_page_cost',
      'synchronous_commit',
      'full_page_writes',
      'log_min_duration_statement'
    )
    order by name;
  " || return
}

verify_postgres_write_stats() {
  section "postgres-write-stats"
  psql_query "
    select
      'checkpoints_timed=' || checkpoints_timed,
      'checkpoints_req=' || checkpoints_req,
      'checkpoint_write_time_ms=' || checkpoint_write_time,
      'checkpoint_sync_time_ms=' || checkpoint_sync_time,
      'buffers_checkpoint=' || buffers_checkpoint,
      'buffers_backend=' || buffers_backend,
      'buffers_backend_fsync=' || buffers_backend_fsync
    from pg_stat_bgwriter;
  " || return
  local server_version_num
  server_version_num="$(psql_query "show server_version_num;")" || return
  if [ "$server_version_num" -lt 140000 ]; then
    printf 'pg_stat_wal=skipped server_version_num=%s reason=requires_postgres_14\n' "$server_version_num"
    return 0
  fi
  psql_query "
    select
      'wal_records=' || wal_records,
      'wal_fpi=' || wal_fpi,
      'wal_bytes=' || wal_bytes,
      'wal_buffers_full=' || wal_buffers_full,
      'wal_write=' || wal_write,
      'wal_sync=' || wal_sync
    from pg_stat_wal;
  " || return
}

verify_log_storage_profile() {
  section "log-storage-profile"
  psql_query "
    select 'logs_heap=' || pg_size_pretty(pg_relation_size('logs'));
    select 'logs_indexes=' || pg_size_pretty(pg_indexes_size('logs'));
    select 'logs_total=' || pg_size_pretty(pg_total_relation_size('logs'));
    select 'logs_index_count=' || count(*) from pg_indexes where tablename = 'logs';
  " || return
}

verify_storage_marker() {
  section "postgres-storage-marker"
  if [ -z "$NEW_API_POSTGRES_DATA_DIR" ] &&
    [ -z "$NEW_API_POSTGRES_MARKER" ] &&
    [ -z "$NEW_API_POSTGRES_MARKER_VALUE" ]; then
    printf 'skipped: set NEW_API_POSTGRES_DATA_DIR, NEW_API_POSTGRES_MARKER, and NEW_API_POSTGRES_MARKER_VALUE to verify the storage overlay marker\n'
    return 0
  fi
  if [ -z "$NEW_API_POSTGRES_DATA_DIR" ] ||
    [ -z "$NEW_API_POSTGRES_MARKER" ] ||
    [ -z "$NEW_API_POSTGRES_MARKER_VALUE" ]; then
    printf 'NEW_API_POSTGRES_DATA_DIR, NEW_API_POSTGRES_MARKER, and NEW_API_POSTGRES_MARKER_VALUE must be provided together when verifying the storage overlay marker\n' >&2
    return 1
  fi
  if ! docker exec "$POSTGRES_CONTAINER" test -f "$POSTGRES_MARKER_CONTAINER_PATH"; then
    printf 'storage marker missing inside postgres container: %s\n' "$POSTGRES_MARKER_CONTAINER_PATH" >&2
    return 1
  fi
  if ! printf '%s' "$NEW_API_POSTGRES_MARKER_VALUE" |
    docker exec -i "$POSTGRES_CONTAINER" sh -ec 'cmp -s "$1" -' sh "$POSTGRES_MARKER_CONTAINER_PATH"; then
    printf 'storage marker mismatch inside postgres container: %s\n' "$POSTGRES_MARKER_CONTAINER_PATH" >&2
    return 1
  fi
  printf 'storage_marker=match\n'
  printf 'container_marker_path=%s\n' "$POSTGRES_MARKER_CONTAINER_PATH"
}

verify_status() {
  local label="$1"
  local url="$2"
  section "$label"
  local output rc
  output=""
  rc=1
  local attempt
  for attempt in $(seq 1 10); do
    if output="$(curl -fsS --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$CURL_MAX_TIME" "$url" 2>&1)"; then
      printf '%s\n' "$output"
      return 0
    fi
    rc=$?
    printf 'attempt=%s failed rc=%s %s\n' "$attempt" "$rc" "$output" >&2
    sleep 2
  done
  return "$rc"
}

validate_header_value() {
  local name="$1"
  local value="$2"
  case "$value" in
    *$'\n'*|*$'\r'*)
      printf '%s must not contain CR or LF characters\n' "$name" >&2
      return 1
      ;;
  esac
}

verify_login_page() {
  section "new-api-login-page"
  local http_code
  http_code="$(curl -sS -L --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$CURL_MAX_TIME" -o /dev/null -w '%{http_code}' "$NEW_API_LOGIN_URL")"
  printf 'http_status=%s\n' "$http_code"
  case "$http_code" in
    2??|3??) return 0 ;;
    *) return 1 ;;
  esac
}

verify_log_list() {
  section "new-api-log-list"
  if [ -z "$NEW_API_AUTH_COOKIE" ] || [ -z "$NEW_API_USER_ID" ]; then
    printf 'skipped: set NEW_API_AUTH_COOKIE and NEW_API_USER_ID to verify the protected log list endpoint\n'
    return
  fi
  validate_header_value "NEW_API_AUTH_COOKIE" "$NEW_API_AUTH_COOKIE" || return
  validate_header_value "NEW_API_USER_ID" "$NEW_API_USER_ID" || return
  local output rc
  if output="$(curl -fsS --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$CURL_MAX_TIME" \
    --header "Cookie: $NEW_API_AUTH_COOKIE" \
    --header "New-Api-User: $NEW_API_USER_ID" \
    "$NEW_API_BASE_URL/api/log/?p=0&page_size=1" 2>&1)"; then
    printf '%s' "$output"
    printf '\n'
    return 0
  fi
  rc=$?
  printf 'failed: verify_log_list url=%s/api/log/?p=0&page_size=1 rc=%s %s\n' "$NEW_API_BASE_URL" "$rc" "$output" >&2
  return 1
}

verify_postgres_container() {
  section "postgres-container"
  docker inspect "$POSTGRES_CONTAINER" >/dev/null
  printf 'container=%s\n' "$POSTGRES_CONTAINER"
}

print_target_summary() {
  section "target"
  local target_defaults_used
  target_defaults_used="false"
  if [ "$POSTGRES_CONTAINER_SOURCE" = "default" ] ||
    [ "$POSTGRES_DB_SOURCE" = "default" ] ||
    [ "$NEW_API_STATUS_URL_SOURCE" = "default" ]; then
    target_defaults_used="true"
  fi

  printf 'target_defaults_used=%s\n' "$target_defaults_used"
  printf 'postgres_container=%s\n' "$POSTGRES_CONTAINER"
  printf 'postgres_container_source=%s\n' "$POSTGRES_CONTAINER_SOURCE"
  printf 'postgres_db=%s\n' "$POSTGRES_DB"
  printf 'postgres_db_source=%s\n' "$POSTGRES_DB_SOURCE"
  printf 'postgres_user=%s\n' "$POSTGRES_USER"
  printf 'postgres_user_source=%s\n' "$POSTGRES_USER_SOURCE"
  printf 'new_api_status_url=%s\n' "$NEW_API_STATUS_URL"
  printf 'new_api_status_url_source=%s\n' "$NEW_API_STATUS_URL_SOURCE"
}

main() {
  if ! have docker; then
    printf 'docker is required\n' >&2
    exit 127
  fi
  if ! have curl; then
    printf 'curl is required for HTTP verification\n' >&2
    exit 127
  fi
  if init_urls; then
    URLS_READY="true"
  else
    printf 'check_failed=urls\n' >&2
    FAILURES=$((FAILURES + 1))
  fi
  print_target_summary
  run_check "postgres-container" verify_postgres_container
  run_check "postgres-ready" verify_postgres_ready
  run_check "required-tables" verify_required_tables
  run_check "postgres-paths" verify_postgres_paths
  run_check "postgres-tuning" verify_postgres_tuning
  run_check "postgres-write-stats" verify_postgres_write_stats
  run_check "log-storage-profile" verify_log_storage_profile
  run_check "postgres-storage-marker" verify_storage_marker
  run_check "table-counts" verify_table_counts
  run_check "latest-rows" verify_latest_rows
  if [ "$URLS_READY" = "true" ]; then
    run_check "new-api-status" verify_status "new-api-status" "$NEW_API_STATUS_URL"
    run_check "new-api-login-page" verify_login_page
    run_check "new-api-log-list" verify_log_list
  fi
  if [ "$FAILURES" -gt 0 ]; then
    exit 1
  fi
  exit 0
}

main "$@"
