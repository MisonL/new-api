#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASE_URL="${COMPACT_E2E_BASE_URL:-http://127.0.0.1:3001}"
UPSTREAM_HOST_URL="${COMPACT_E2E_UPSTREAM_HOST_URL:-http://127.0.0.1:18081}"
UPSTREAM_CONTAINER_URL="${COMPACT_E2E_UPSTREAM_CONTAINER_URL:-http://host.docker.internal:18081}"
POSTGRES_CONTAINER="${COMPACT_E2E_POSTGRES_CONTAINER:-new-api-dev-isolated-postgres-1}"
POSTGRES_USER="${COMPACT_E2E_POSTGRES_USER:-root}"
POSTGRES_DB="${COMPACT_E2E_POSTGRES_DB:-new-api-dev}"
APP_CONTAINER="${COMPACT_E2E_APP_CONTAINER:-new-api-dev-isolated-new-api-1}"
REDIS_CONTAINER="${COMPACT_E2E_REDIS_CONTAINER:-new-api-dev-isolated-redis-1}"
REDIS_PASSWORD="${COMPACT_E2E_REDIS_PASSWORD:-}"
COMPOSE_FILE="${COMPACT_E2E_COMPOSE_FILE:-deploy/compose/dev-isolated.yml}"
ENV_FILE="${COMPACT_E2E_ENV_FILE:-deploy/env/dev-isolated.env}"
APP_SERVICE="${COMPACT_E2E_APP_SERVICE:-new-api}"
COMPOSE_PROJECT_NAME="${COMPACT_E2E_COMPOSE_PROJECT_NAME:-}"
CURL_CONNECT_TIMEOUT="${COMPACT_E2E_CURL_CONNECT_TIMEOUT:-5}"
CURL_MAX_TIME="${COMPACT_E2E_CURL_MAX_TIME:-60}"
HEALTH_CURL_MAX_TIME="${COMPACT_E2E_HEALTH_CURL_MAX_TIME:-5}"
READY_TIMEOUT_SECONDS="${COMPACT_E2E_READY_TIMEOUT_SECONDS:-180}"
RESTART_TIMEOUT_SECONDS="${COMPACT_E2E_RESTART_TIMEOUT_SECONDS:-120}"
UPSTREAM_CURL_MAX_TIME="${COMPACT_E2E_UPSTREAM_CURL_MAX_TIME:-30}"
REDIS_EXPIRY_ASSERTION_MARGIN_MS="${COMPACT_E2E_REDIS_EXPIRY_ASSERTION_MARGIN_MS:-2000}"
TOKEN_ID="${COMPACT_E2E_TOKEN_ID:-910001}"
CHANNEL_NATIVE_NEWAPI="${COMPACT_E2E_CHANNEL_NATIVE_NEWAPI:-910101}"
CHANNEL_SUB2API_HTTP="${COMPACT_E2E_CHANNEL_SUB2API_HTTP:-910102}"
CHANNEL_SYNTHETIC_NEWAPI="${COMPACT_E2E_CHANNEL_SYNTHETIC_NEWAPI:-910103}"
CHANNEL_GENERIC_OPENAI="${COMPACT_E2E_CHANNEL_GENERIC_OPENAI:-910104}"
TEST_GROUP="${COMPACT_E2E_GROUP:-compact-e2e}"
RUN_ID="compact_e2e_$(date +%Y%m%d%H%M%S)_$$"
BACKUP_CHANNELS="${RUN_ID}_channels"
BACKUP_ABILITIES="${RUN_ID}_abilities"
BACKUP_TOKENS="${RUN_ID}_tokens"
BACKUP_USERS="${RUN_ID}_users"
BACKUP_OPTIONS="${RUN_ID}_options"
BACKUP_LOGS="${RUN_ID}_logs"
BACKUP_SYNTHETIC_STATES="${RUN_ID}_synthetic_states"
BACKUP_QUOTA_DATA="${RUN_ID}_quota_data"
BACKUP_PERF_METRICS="${RUN_ID}_perf_metrics"
BACKUP_SEQUENCES="${RUN_ID}_sequences"
TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/newapi-compact-e2e.XXXXXX")"
REDIS_SNAPSHOT_DIR="$TMP_DIR/redis-snapshot"
REDIS_SNAPSHOT_MANIFEST="$REDIS_SNAPSHOT_DIR/manifest.tsv"
E2E_LOCK_DIR="${COMPACT_E2E_LOCK_DIR:-/tmp/newapi-compact-e2e.lock}"
E2E_LOCK_OWNER_FILE="$E2E_LOCK_DIR/pid"
FAKE_PID=""
HISTORY_FIXTURE_PATH="${COMPACT_E2E_HISTORY_FIXTURE:-}"
DB_SNAPSHOT_READY=0
REDIS_SNAPSHOT_READY=0
APP_QUIESCED=0
E2E_LOCK_HELD=0

cd "$ROOT_DIR"

. "$ROOT_DIR/scripts/compact-control-plane-e2e-lib.sh"

psql_exec() { docker exec -i "$POSTGRES_CONTAINER" psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 "$@"; }

psql_query() { psql_exec -At "$@"; }

redis_cli() {
  docker exec -e REDISCLI_AUTH="$REDIS_PASSWORD" "$REDIS_CONTAINER" redis-cli --no-auth-warning -e "$@"
}

redis_cli_stdin() {
  docker exec -i -e REDISCLI_AUTH="$REDIS_PASSWORD" "$REDIS_CONTAINER" redis-cli --no-auth-warning -e "$@"
}

resolve_redis_password() {
  if [[ -n "$REDIS_PASSWORD" ]]; then
    return 0
  fi

  REDIS_PASSWORD="$(docker inspect --format '{{json .Config.Cmd}}' "$REDIS_CONTAINER" | node -e '
    let raw = "";
    process.stdin.setEncoding("utf8");
    process.stdin.on("data", (chunk) => { raw += chunk; });
    process.stdin.on("end", () => {
      try {
        const args = JSON.parse(raw);
        const index = args.indexOf("--requirepass");
        if (index >= 0 && args[index + 1]) {
          process.stdout.write(args[index + 1]);
        }
      } catch {
        process.exitCode = 1;
      }
    });
  ')"
  if [[ -z "$REDIS_PASSWORD" ]]; then
    echo "cannot resolve the isolated Redis password from ${REDIS_CONTAINER}; set COMPACT_E2E_REDIS_PASSWORD explicitly" >&2
    return 1
  fi
}

compact_e2e_lock_owner_is_active() {
  local owner_pid="$1"
  local command
  [[ "$owner_pid" =~ ^[0-9]+$ ]] || return 1
  command="$(ps -p "$owner_pid" -o command= 2>/dev/null || true)"
  [[ "$command" == *"compact-control-plane-e2e.sh"* ]]
}

acquire_e2e_lock() {
  local owner_pid=""
  if mkdir "$E2E_LOCK_DIR" 2>/dev/null; then
    chmod 700 "$E2E_LOCK_DIR"
    printf '%s\n' "$$" >"$E2E_LOCK_OWNER_FILE"
    chmod 600 "$E2E_LOCK_OWNER_FILE"
    E2E_LOCK_HELD=1
    echo "compact_e2e_lock_acquired=true"
    return 0
  fi

  if [[ ! -r "$E2E_LOCK_OWNER_FILE" ]]; then
    echo "compact e2e lock exists without a readable owner: $E2E_LOCK_DIR" >&2
    return 1
  fi
  owner_pid="$(<"$E2E_LOCK_OWNER_FILE")"
  if compact_e2e_lock_owner_is_active "$owner_pid"; then
    echo "compact e2e already running: pid=${owner_pid} lock=${E2E_LOCK_DIR}" >&2
    return 1
  fi

  rm -f "$E2E_LOCK_OWNER_FILE"
  if ! rmdir "$E2E_LOCK_DIR"; then
    echo "cannot clear stale compact e2e lock: $E2E_LOCK_DIR" >&2
    return 1
  fi
  acquire_e2e_lock
}

release_e2e_lock() {
  local owner_pid=""
  [[ "$E2E_LOCK_HELD" -eq 1 ]] || return 0
  if [[ -r "$E2E_LOCK_OWNER_FILE" ]]; then
    owner_pid="$(<"$E2E_LOCK_OWNER_FILE")"
  fi
  if [[ "$owner_pid" != "$$" ]]; then
    echo "compact e2e lock owner mismatch during release: $E2E_LOCK_DIR" >&2
    return 1
  fi
  rm -f "$E2E_LOCK_OWNER_FILE"
  if ! rmdir "$E2E_LOCK_DIR"; then
    echo "compact e2e lock release failed: $E2E_LOCK_DIR" >&2
    return 1
  fi
  E2E_LOCK_HELD=0
  echo "compact_e2e_lock_released=true"
}

guard_dev_target() {
  if [[ "${COMPACT_E2E_FORCE:-}" == "1" ]]; then
    return
  fi
  if [[ "$BASE_URL" != "http://127.0.0.1:3001" ]]; then
    echo "refusing to run against non-dev base URL: $BASE_URL" >&2
    echo "set COMPACT_E2E_FORCE=1 only for an isolated throwaway environment" >&2
    exit 1
  fi
  if [[ "$POSTGRES_CONTAINER" != *"dev-isolated"* || "$APP_CONTAINER" != *"dev-isolated"* || "$REDIS_CONTAINER" != *"dev-isolated"* ]]; then
    echo "refusing to run outside dev-isolated containers" >&2
    exit 1
  fi
  export COMPACT_E2E_ALLOW_DB_RESET=1
}

validate_e2e_environment() {
  if [[ ! -r "$COMPOSE_FILE" ]]; then
    echo "compact e2e compose file is not readable: $COMPOSE_FILE" >&2
    return 1
  fi
  if [[ ! -r "$ENV_FILE" ]]; then
    echo "compact e2e isolated env file is not readable: $ENV_FILE" >&2
    echo "set COMPACT_E2E_ENV_FILE to the isolated development env file" >&2
    return 1
  fi
  for container in "$APP_CONTAINER" "$POSTGRES_CONTAINER" "$REDIS_CONTAINER"; do
    if ! docker inspect "$container" >/dev/null 2>&1; then
      echo "compact e2e container is unavailable: $container" >&2
      return 1
    fi
  done
  if ! [[ "$REDIS_EXPIRY_ASSERTION_MARGIN_MS" =~ ^[0-9]+$ ]]; then
    echo "COMPACT_E2E_REDIS_EXPIRY_ASSERTION_MARGIN_MS must be a non-negative integer" >&2
    return 1
  fi
}

cleanup() {
  local original_rc=$?
  local rc=0
  local cleanup_log="$TMP_DIR/cleanup.log"
  set +e
  if ! stop_dev >"$TMP_DIR/stop-dev.log" 2>&1; then
    echo "cleanup: dev container stop failed" >&2
    cat "$TMP_DIR/stop-dev.log" >&2
    rc=1
  fi
  if [[ "$APP_QUIESCED" -ne 1 && ("$DB_SNAPSHOT_READY" -eq 1 || "$REDIS_SNAPSHOT_READY" -eq 1) ]]; then
    echo "cleanup: state restoration skipped because the app is not confirmed stopped" >&2
    rc=1
  elif [[ "$DB_SNAPSHOT_READY" -eq 1 ]]; then
    if restore_database_state >"$cleanup_log" 2>&1; then
      echo "cleanup: database state restored" >&2
    else
      echo "cleanup: database restoration failed" >&2
      cat "$cleanup_log" >&2
      rc=1
    fi
  fi
  if [[ "$APP_QUIESCED" -eq 1 && "$REDIS_SNAPSHOT_READY" -eq 1 ]]; then
    if restore_redis_state >"$TMP_DIR/redis-restore.log" 2>&1; then
      echo "cleanup: redis state restored" >&2
    else
      echo "cleanup: redis restoration failed" >&2
      cat "$TMP_DIR/redis-restore.log" >&2
      rc=1
    fi
  fi
  if [[ "$DB_SNAPSHOT_READY" -eq 1 ]]; then
    if assert_database_state_restored >"$TMP_DIR/database-assert.log" 2>&1; then
      cat "$TMP_DIR/database-assert.log"
    else
      echo "cleanup: database state verification failed" >&2
      cat "$TMP_DIR/database-assert.log" >&2
      rc=1
    fi
  fi
  if [[ "$REDIS_SNAPSHOT_READY" -eq 1 ]]; then
    if assert_redis_state_restored >"$TMP_DIR/redis-assert.log" 2>&1; then
      cat "$TMP_DIR/redis-assert.log"
    else
      echo "cleanup: redis state verification failed" >&2
      cat "$TMP_DIR/redis-assert.log" >&2
      rc=1
    fi
  fi
  if [[ -n "$FAKE_PID" ]]; then
    kill "$FAKE_PID" >/dev/null 2>&1 || true
  fi
  local start_log="$TMP_DIR/start-dev.log"
  if start_dev >"$start_log" 2>&1; then
    echo "cleanup: dev container started" >&2
  else
    echo "cleanup: dev container start failed" >&2
    cat "$start_log" >&2
    rc=1
  fi
  if [[ "$DB_SNAPSHOT_READY" -eq 1 ]]; then
    if assert_database_state_restored >"$TMP_DIR/database-post-start-assert.log" 2>&1; then
      cat "$TMP_DIR/database-post-start-assert.log"
    else
      echo "cleanup: post-start database state verification failed" >&2
      cat "$TMP_DIR/database-post-start-assert.log" >&2
      rc=1
    fi
  fi
  if [[ "$REDIS_SNAPSHOT_READY" -eq 1 ]]; then
    if assert_redis_state_restored >"$TMP_DIR/redis-post-start-assert.log" 2>&1; then
      cat "$TMP_DIR/redis-post-start-assert.log"
    else
      echo "cleanup: post-start redis state verification failed" >&2
      cat "$TMP_DIR/redis-post-start-assert.log" >&2
      rc=1
    fi
  fi
  if [[ "$rc" -eq 0 && "$DB_SNAPSHOT_READY" -eq 1 ]]; then
    if drop_database_backups >>"$cleanup_log" 2>&1; then
      DB_SNAPSHOT_READY=0
    else
      echo "cleanup: database backup drop failed" >&2
      cat "$cleanup_log" >&2
      rc=1
    fi
  fi
  if ! release_e2e_lock; then
    rc=1
  fi
  if [[ "$original_rc" -eq 0 && "$rc" -eq 0 ]]; then
    echo "e2e_done=true"
  fi
  if [[ ("$original_rc" -eq 0 && "$rc" -eq 0) || -n "${COMPACT_E2E_CLEANUP_DELETE_LOGS:-}" ]]; then
    rm -rf "$TMP_DIR"
  else
    echo "cleanup: retained logs in $TMP_DIR" >&2
  fi
  if [[ "$original_rc" -ne 0 ]]; then
    return "$original_rc"
  fi
  return "$rc"
}

restore_database_state() {
  psql_exec \
    -v test_group="$TEST_GROUP" \
    -v token_id="$TOKEN_ID" \
    -v user_id="$TOKEN_ID" \
    -v channel_native_newapi="$CHANNEL_NATIVE_NEWAPI" \
    -v channel_sub2api_http="$CHANNEL_SUB2API_HTTP" \
    -v channel_synthetic_newapi="$CHANNEL_SYNTHETIC_NEWAPI" \
    -v channel_generic_openai="$CHANNEL_GENERIC_OPENAI" \
    >/dev/null <<SQL
begin;
do \$\$
begin
  if to_regclass('${BACKUP_ABILITIES}') is null
    or to_regclass('${BACKUP_CHANNELS}') is null
    or to_regclass('${BACKUP_TOKENS}') is null
    or to_regclass('${BACKUP_USERS}') is null
    or to_regclass('${BACKUP_OPTIONS}') is null
    or to_regclass('${BACKUP_LOGS}') is null
    or to_regclass('${BACKUP_SYNTHETIC_STATES}') is null
    or to_regclass('${BACKUP_QUOTA_DATA}') is null
    or to_regclass('${BACKUP_PERF_METRICS}') is null
    or to_regclass('${BACKUP_SEQUENCES}') is null then
    raise exception 'compact e2e backup tables are missing';
  end if;
end
\$\$;
delete from logs where token_id = :token_id or user_id = :user_id;
insert into logs select * from ${BACKUP_LOGS};
delete from synthetic_compact_state_records where token_id = :token_id or user_id = :user_id or group_name = :'test_group';
insert into synthetic_compact_state_records select * from ${BACKUP_SYNTHETIC_STATES};
delete from quota_data where user_id = :user_id;
insert into quota_data select * from ${BACKUP_QUOTA_DATA};
delete from perf_metrics where "group" = :'test_group';
insert into perf_metrics select * from ${BACKUP_PERF_METRICS};
delete from abilities where "group" = :'test_group';
insert into abilities select * from ${BACKUP_ABILITIES};
delete from channels where id in (:channel_native_newapi, :channel_sub2api_http, :channel_synthetic_newapi, :channel_generic_openai);
insert into channels select * from ${BACKUP_CHANNELS};
delete from tokens where id = :token_id;
insert into tokens select * from ${BACKUP_TOKENS};
delete from users where id = :user_id;
insert into users select * from ${BACKUP_USERS};
delete from options where key in ('GroupRatio', 'UserUsableGroups', 'runtime.responses_synthetic_compact_instance_id');
insert into options select * from ${BACKUP_OPTIONS};
select setval(sequence_name::regclass, last_value, is_called) from ${BACKUP_SEQUENCES};
commit;
SQL
}

assert_database_state_restored() {
  local restored
  restored="$(psql_query -v test_group="$TEST_GROUP" -v token_id="$TOKEN_ID" -v user_id="$TOKEN_ID" -v channel_native_newapi="$CHANNEL_NATIVE_NEWAPI" -v channel_sub2api_http="$CHANNEL_SUB2API_HTTP" -v channel_synthetic_newapi="$CHANNEL_SYNTHETIC_NEWAPI" -v channel_generic_openai="$CHANNEL_GENERIC_OPENAI" <<SQL
select case when
  not exists ((select to_jsonb(row) from (select * from logs where token_id = :token_id or user_id = :user_id) as row) except all (select to_jsonb(row) from (select * from ${BACKUP_LOGS}) as row))
  and not exists ((select to_jsonb(row) from (select * from ${BACKUP_LOGS}) as row) except all (select to_jsonb(row) from (select * from logs where token_id = :token_id or user_id = :user_id) as row))
  and not exists ((select to_jsonb(row) from (select * from synthetic_compact_state_records where token_id = :token_id or user_id = :user_id or group_name = :'test_group') as row) except all (select to_jsonb(row) from (select * from ${BACKUP_SYNTHETIC_STATES}) as row))
  and not exists ((select to_jsonb(row) from (select * from ${BACKUP_SYNTHETIC_STATES}) as row) except all (select to_jsonb(row) from (select * from synthetic_compact_state_records where token_id = :token_id or user_id = :user_id or group_name = :'test_group') as row))
  and not exists ((select to_jsonb(row) from (select * from quota_data where user_id = :user_id) as row) except all (select to_jsonb(row) from (select * from ${BACKUP_QUOTA_DATA}) as row))
  and not exists ((select to_jsonb(row) from (select * from ${BACKUP_QUOTA_DATA}) as row) except all (select to_jsonb(row) from (select * from quota_data where user_id = :user_id) as row))
  and not exists ((select to_jsonb(row) from (select * from perf_metrics where "group" = :'test_group') as row) except all (select to_jsonb(row) from (select * from ${BACKUP_PERF_METRICS}) as row))
  and not exists ((select to_jsonb(row) from (select * from ${BACKUP_PERF_METRICS}) as row) except all (select to_jsonb(row) from (select * from perf_metrics where "group" = :'test_group') as row))
  and not exists ((select to_jsonb(row) from (select * from abilities where "group" = :'test_group') as row) except all (select to_jsonb(row) from (select * from ${BACKUP_ABILITIES}) as row))
  and not exists ((select to_jsonb(row) from (select * from ${BACKUP_ABILITIES}) as row) except all (select to_jsonb(row) from (select * from abilities where "group" = :'test_group') as row))
  and not exists ((select to_jsonb(row) from (select * from channels where id in (:channel_native_newapi, :channel_sub2api_http, :channel_synthetic_newapi, :channel_generic_openai)) as row) except all (select to_jsonb(row) from (select * from ${BACKUP_CHANNELS}) as row))
  and not exists ((select to_jsonb(row) from (select * from ${BACKUP_CHANNELS}) as row) except all (select to_jsonb(row) from (select * from channels where id in (:channel_native_newapi, :channel_sub2api_http, :channel_synthetic_newapi, :channel_generic_openai)) as row))
  and not exists ((select to_jsonb(row) from (select * from tokens where id = :token_id) as row) except all (select to_jsonb(row) from (select * from ${BACKUP_TOKENS}) as row))
  and not exists ((select to_jsonb(row) from (select * from ${BACKUP_TOKENS}) as row) except all (select to_jsonb(row) from (select * from tokens where id = :token_id) as row))
  and not exists ((select to_jsonb(row) from (select * from users where id = :user_id) as row) except all (select to_jsonb(row) from (select * from ${BACKUP_USERS}) as row))
  and not exists ((select to_jsonb(row) from (select * from ${BACKUP_USERS}) as row) except all (select to_jsonb(row) from (select * from users where id = :user_id) as row))
  and not exists ((select to_jsonb(row) from (select * from options where key in ('GroupRatio', 'UserUsableGroups', 'runtime.responses_synthetic_compact_instance_id')) as row) except all (select to_jsonb(row) from (select * from ${BACKUP_OPTIONS}) as row))
  and not exists ((select to_jsonb(row) from (select * from ${BACKUP_OPTIONS}) as row) except all (select to_jsonb(row) from (select * from options where key in ('GroupRatio', 'UserUsableGroups', 'runtime.responses_synthetic_compact_instance_id')) as row))
  and not exists ((select sequence_name, last_value, is_called from ${BACKUP_SEQUENCES}) except all (
    select 'public.logs_id_seq', last_value, is_called from public.logs_id_seq
    union all select 'public.quota_data_id_seq', last_value, is_called from public.quota_data_id_seq
    union all select 'public.perf_metrics_id_seq', last_value, is_called from public.perf_metrics_id_seq
  ))
  and not exists ((
    select 'public.logs_id_seq', last_value, is_called from public.logs_id_seq
    union all select 'public.quota_data_id_seq', last_value, is_called from public.quota_data_id_seq
    union all select 'public.perf_metrics_id_seq', last_value, is_called from public.perf_metrics_id_seq
  ) except all (select sequence_name, last_value, is_called from ${BACKUP_SEQUENCES}))
then 'true' else 'false' end;
SQL
)"
  [[ "$restored" == "true" ]] || {
    echo "database_snapshot_match=false" >&2
    return 1
  }
  echo "database_snapshot_match=true"
}

drop_database_backups() {
  psql_exec >/dev/null <<SQL
drop table if exists ${BACKUP_CHANNELS};
drop table if exists ${BACKUP_ABILITIES};
drop table if exists ${BACKUP_TOKENS};
drop table if exists ${BACKUP_USERS};
drop table if exists ${BACKUP_OPTIONS};
drop table if exists ${BACKUP_LOGS};
drop table if exists ${BACKUP_SYNTHETIC_STATES};
drop table if exists ${BACKUP_QUOTA_DATA};
drop table if exists ${BACKUP_PERF_METRICS};
drop table if exists ${BACKUP_SEQUENCES};
SQL
}

compose_dev() {
  local compose_args=(-f "$COMPOSE_FILE" --env-file "$ENV_FILE")
  if [[ -n "$COMPOSE_PROJECT_NAME" ]]; then
    compose_args=(--project-name "$COMPOSE_PROJECT_NAME" "${compose_args[@]}")
  fi
  docker compose "${compose_args[@]}" "$@"
}

wait_for_dev() {
	local deadline=$((SECONDS + READY_TIMEOUT_SECONDS))
	while (( SECONDS < deadline )); do
		if curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$HEALTH_CURL_MAX_TIME" -fsS "$BASE_URL/api/status" >/dev/null 2>&1; then
			return 0
		fi
		sleep 1
	done
	echo "dev container did not become ready within ${READY_TIMEOUT_SECONDS}s" >&2
	compose_dev ps >&2 || true
	compose_dev logs --tail 120 "$APP_SERVICE" >&2 || true
	return 1
}

start_dev() {
  compose_dev up -d --no-deps "$APP_SERVICE" >/dev/null || return 1
  wait_for_dev
  APP_QUIESCED=0
}

stop_dev() {
  if [[ "$(docker inspect -f '{{.State.Running}}' "$APP_CONTAINER" 2>/dev/null || true)" != "true" ]]; then
    APP_QUIESCED=1
    return 0
  fi
  compose_dev stop -t "$RESTART_TIMEOUT_SECONDS" "$APP_SERVICE" >/dev/null || return 1
  if [[ "$(docker inspect -f '{{.State.Running}}' "$APP_CONTAINER" 2>/dev/null || true)" == "true" ]]; then
    echo "dev container is still running after stop: $APP_CONTAINER" >&2
    return 1
  fi
  APP_QUIESCED=1
}

restart_dev() {
  compose_dev restart -t "$RESTART_TIMEOUT_SECONDS" "$APP_SERVICE" >/dev/null || return 1
  wait_for_dev
  APP_QUIESCED=0
}

redis_snapshot_patterns() {
  printf '%s\n' \
    "new-api:channel_affinity:v1:*:${TEST_GROUP}:*" \
    "new-api:channel_affinity_usage_cache_stats:v1:*:${TEST_GROUP}:*" \
    "perf:*:${TEST_GROUP}:*" \
    "rateLimit:${TOKEN_ID}" \
    "rateLimit:*:${TOKEN_ID}" \
    "rateLimit:*:user:${TOKEN_ID}" \
    "user:${TOKEN_ID}"
}

redis_synthetic_key_is_e2e_owned() {
  local key="$1"
  redis_cli --raw GET "$key" | node -e '
    let raw = "";
    process.stdin.setEncoding("utf8");
    process.stdin.on("data", (chunk) => { raw += chunk; });
    process.stdin.on("end", () => {
      try {
        const value = JSON.parse(raw);
        const tokenID = Number(value.TokenID ?? value.token_id ?? 0);
        const userID = Number(value.UserID ?? value.user_id ?? 0);
        const group = String(value.Group ?? value.group ?? value.GroupName ?? value.group_name ?? "");
        process.exit(tokenID === Number(process.argv[1]) || userID === Number(process.argv[1]) || group === process.argv[2] ? 0 : 1);
      } catch {
        process.exit(1);
      }
    });
  ' "$TOKEN_ID" "$TEST_GROUP"
}

redis_token_key_is_e2e_owned() {
  local key="$1"
  local token_id user_id
  token_id="$(redis_cli --raw HGET "$key" Id)"
  user_id="$(redis_cli --raw HGET "$key" UserId)"
  [[ "$token_id" == "$TOKEN_ID" || "$user_id" == "$TOKEN_ID" ]]
}

redis_matching_keys() {
  local pattern key
  {
    while IFS= read -r pattern; do
      redis_cli --raw --scan --pattern "$pattern"
    done < <(redis_snapshot_patterns)
    while IFS= read -r key; do
      [[ -n "$key" ]] || continue
      if redis_synthetic_key_is_e2e_owned "$key"; then
        printf '%s\n' "$key"
      fi
    done < <(redis_cli --raw --scan --pattern 'new-api:responses:synthetic-compact:*')
    while IFS= read -r key; do
      [[ -n "$key" ]] || continue
      if redis_token_key_is_e2e_owned "$key"; then
        printf '%s\n' "$key"
      fi
    done < <(redis_cli --raw --scan --pattern 'token:*')
  } | LC_ALL=C sort -u
}

redis_dump_to_file() {
  local key="$1"
  local dump_file="$2"
  redis_cli -D '' --raw DUMP "$key" >"$dump_file"
}

redis_now_ms() {
  node -e 'process.stdout.write(String(Date.now()))'
}

redis_expiry_is_within_assertion_margin() {
  local expires_at="$1"
  local now_ms
  [[ "$expires_at" =~ ^[0-9]+$ ]] || return 1
  now_ms="$(redis_now_ms)"
  (( expires_at <= now_ms + REDIS_EXPIRY_ASSERTION_MARGIN_MS ))
}

snapshot_redis_state() {
  local key index=0 expires_at dump_file digest
  mkdir -p "$REDIS_SNAPSHOT_DIR"
  chmod 700 "$REDIS_SNAPSHOT_DIR"
  : >"$REDIS_SNAPSHOT_MANIFEST"
  chmod 600 "$REDIS_SNAPSHOT_MANIFEST"
  while IFS= read -r key; do
    [[ -n "$key" ]] || continue
    [[ "$(redis_cli --raw EXISTS "$key")" == "1" ]] || continue
    expires_at="$(redis_cli --raw PEXPIRETIME "$key")"
    [[ "$expires_at" != "-2" ]] || continue
    index=$((index + 1))
    dump_file="$REDIS_SNAPSHOT_DIR/${index}.dump"
    redis_dump_to_file "$key" "$dump_file"
    [[ -s "$dump_file" ]] || {
      echo "redis snapshot dump is empty: key index=${index}" >&2
      return 1
    }
    digest="$(shasum -a 256 "$dump_file" | awk '{print $1}')"
    printf '%s\t%s\t%s\t%s\n' "$index" "$expires_at" "$digest" "$key" >>"$REDIS_SNAPSHOT_MANIFEST"
  done < <(redis_matching_keys)
  REDIS_SNAPSHOT_READY=1
  echo "redis_snapshot_keys=${index}"
}

restore_redis_state() {
  local key index expires_at digest dump_file now_ms
  while IFS= read -r key; do
    [[ -n "$key" ]] || continue
    redis_cli DEL "$key" >/dev/null
  done < <(redis_matching_keys)
  while IFS=$'\t' read -r index expires_at digest key; do
    [[ -n "$index" && -n "$key" ]] || continue
    now_ms="$(redis_now_ms)"
    if [[ "$expires_at" =~ ^[0-9]+$ ]] && (( expires_at <= now_ms )); then
      continue
    fi
    dump_file="$REDIS_SNAPSHOT_DIR/${index}.dump"
    [[ -s "$dump_file" ]] || {
      echo "redis snapshot dump missing: key index=${index}" >&2
      return 1
    }
    redis_cli_stdin -X dump_tag RESTORE "$key" 0 dump_tag REPLACE <"$dump_file" >/dev/null
    if [[ "$expires_at" -gt 0 ]]; then
      redis_cli PEXPIREAT "$key" "$expires_at" >/dev/null
    fi
  done <"$REDIS_SNAPSHOT_MANIFEST"
}

assert_redis_state_restored() {
  local key index expires_at expected_digest actual_digest actual_expires_at actual_dump_file
  local actual_assertable_keys="$TMP_DIR/redis-actual-assertable-keys.txt"
  local expected_keys="$TMP_DIR/redis-expected-keys.txt"
  local actual_keys="$TMP_DIR/redis-actual-keys.txt"
  : >"$expected_keys"
  while IFS=$'\t' read -r index expires_at expected_digest key; do
    [[ -n "$index" && -n "$key" ]] || continue
    actual_expires_at="$(redis_cli --raw PEXPIRETIME "$key")"
    if redis_expiry_is_within_assertion_margin "$expires_at"; then
      if [[ "$actual_expires_at" == "-2" ]] || redis_expiry_is_within_assertion_margin "$actual_expires_at"; then
        continue
      fi
      echo "redis_snapshot_expiring_entry_match=false key_index=${index} expected_expire_at=${expires_at} actual_expire_at=${actual_expires_at}" >&2
      return 1
    fi
    if [[ "$actual_expires_at" == "-2" ]]; then
      echo "redis_snapshot_entry_missing=true key_index=${index}" >&2
      return 1
    fi
    if [[ "$actual_expires_at" != "$expires_at" ]]; then
      echo "redis_snapshot_expiry_match=false key_index=${index} expected_expire_at=${expires_at} actual_expire_at=${actual_expires_at}" >&2
      return 1
    fi
    if [[ "$actual_expires_at" != "-1" ]] && ! [[ "$actual_expires_at" =~ ^[0-9]+$ ]]; then
      echo "redis_snapshot_invalid_expiry=true key_index=${index} actual_expire_at=${actual_expires_at}" >&2
      return 1
    fi
    if [[ "$actual_expires_at" != "-1" ]] && redis_expiry_is_within_assertion_margin "$actual_expires_at"; then
      continue
    fi
    actual_dump_file="$TMP_DIR/redis-actual-${index}.dump"
    redis_dump_to_file "$key" "$actual_dump_file"
    actual_digest="$(shasum -a 256 "$actual_dump_file" | awk '{print $1}')"
    if [[ "$actual_digest" != "$expected_digest" ]]; then
      echo "redis_snapshot_value_match=false key_index=${index} expected_digest=${expected_digest} actual_digest=${actual_digest}" >&2
      return 1
    fi
    printf '%s\n' "$key" >>"$expected_keys"
  done <"$REDIS_SNAPSHOT_MANIFEST"
  redis_matching_keys >"$actual_keys"
  : >"$actual_assertable_keys"
  while IFS= read -r key; do
    [[ -n "$key" ]] || continue
    actual_expires_at="$(redis_cli --raw PEXPIRETIME "$key")"
    if [[ "$actual_expires_at" == "-2" ]] || redis_expiry_is_within_assertion_margin "$actual_expires_at"; then
      continue
    fi
    printf '%s\n' "$key" >>"$actual_assertable_keys"
  done <"$actual_keys"
  if ! cmp -s <(LC_ALL=C sort -u "$expected_keys") <(LC_ALL=C sort -u "$actual_assertable_keys"); then
    echo "redis_snapshot_key_set_match=false" >&2
    return 1
  fi
  echo "redis_snapshot_match=true"
}

snapshot_channel_model_mapping() {
  local channel="$1"
  local snapshot
  snapshot="$(psql_query -v channel="$channel" <<'SQL'
select json_build_object('is_null', model_mapping is null, 'value', model_mapping)::text
from channels
where id = :channel;
SQL
)"
  if [[ -z "$snapshot" ]]; then
    echo "cannot snapshot model mapping for channel ${channel}" >&2
    return 1
  fi
  printf '%s\n' "$snapshot"
}

restore_channel_model_mapping() {
  local channel="$1"
  local snapshot="$2"
  if [[ -z "$snapshot" ]]; then
    echo "cannot restore an empty model mapping snapshot for channel ${channel}" >&2
    return 1
  fi
  psql_exec -v channel="$channel" -v snapshot="$snapshot" >/dev/null <<'SQL'
with saved as (
  select :'snapshot'::jsonb as value
)
update channels
set model_mapping = case
  when (select (value ->> 'is_null')::boolean from saved) then null
  else (select value ->> 'value' from saved)
end
where id = :channel;
SQL
  restart_dev
}

ensure_fake_upstream() {
  if curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$UPSTREAM_CURL_MAX_TIME" -fsS "$UPSTREAM_HOST_URL/_captures/summary" >/dev/null 2>&1; then
    curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$UPSTREAM_CURL_MAX_TIME" -fsS "$UPSTREAM_HOST_URL/_reset" >/dev/null
    return
  fi
  if curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$UPSTREAM_CURL_MAX_TIME" -fsS "$UPSTREAM_HOST_URL/_reset" >/dev/null 2>&1; then
    echo "existing fake upstream lacks /_captures/summary: $UPSTREAM_HOST_URL" >&2
    echo "stop the old process or set COMPACT_E2E_UPSTREAM_HOST_URL and COMPACT_E2E_UPSTREAM_CONTAINER_URL" >&2
    exit 1
  fi
  node "$ROOT_DIR/scripts/compact-control-plane-fake-upstream.mjs" >"$TMP_DIR/fake-upstream.log" 2>&1 &
  FAKE_PID="$!"
  for _ in {1..20}; do
    if curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$UPSTREAM_CURL_MAX_TIME" -fsS "$UPSTREAM_HOST_URL/_reset" >/dev/null 2>&1; then
      return
    fi
    sleep 0.2
  done
  echo "fake upstream did not become ready" >&2
  cat "$TMP_DIR/fake-upstream.log" >&2 || true
  exit 1
}

prepare_curl_config() {
  local token="$1"
  CURL_CONFIG="$TMP_DIR/curl.conf"
  chmod 700 "$TMP_DIR"
  local old_umask
  old_umask="$(umask)"
  umask 077
  {
    printf 'header = "Authorization: Bearer %s"\n' "$token"
    printf 'header = "Content-Type: application/json"\n'
    printf 'header = "User-Agent: codex-tui/0.139.0 (Mac OS 15.7.3; x86_64)"\n'
  } >"$CURL_CONFIG"
  umask "$old_umask"
  chmod 600 "$CURL_CONFIG"
}

post_json() {
  local path="$1"
  local payload="$2"
  local out="$3"
  curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$CURL_MAX_TIME" -sS -o "$out" -w '%{http_code}' -K "$CURL_CONFIG" --data "$payload" "$BASE_URL${path}"
}

post_json_local_codex_compaction() {
  local path="$1"
  local payload="$2"
  local out="$3"
  curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$CURL_MAX_TIME" -sS -o "$out" -w '%{http_code}' -K "$CURL_CONFIG" \
    -H 'X-Codex-Turn-Metadata: {"request_kind":"compaction","compaction":{"implementation":"responses"}}' \
    --data "$payload" "$BASE_URL${path}"
}

post_json_remote_compaction() {
  local path="$1"
  local payload="$2"
  local out="$3"
  curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$CURL_MAX_TIME" -sS -o "$out" -w '%{http_code}' -K "$CURL_CONFIG" -H 'X-Codex-Beta-Features: remote_compaction_v2' --data "$payload" "$BASE_URL${path}"
}

reset_upstream() {
  curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$UPSTREAM_CURL_MAX_TIME" -fsS "$UPSTREAM_HOST_URL/_reset" >/dev/null
}

capture_summary() {
  curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$UPSTREAM_CURL_MAX_TIME" -fsS "$UPSTREAM_HOST_URL/_captures/summary"
}

assert_contains() { local file="$1" needle="$2"
  grep -q "$needle" "$file" || { echo "missing ${needle} in ${file}" >&2; cat "$file" >&2; exit 1; }
}

assert_not_contains() { local file="$1" needle="$2"
  ! grep -q "$needle" "$file" || { echo "unexpected ${needle} in ${file}" >&2; cat "$file" >&2; exit 1; }
}

run_case_native_newapi_compact() {
  echo "case=native_newapi_compact"
  set_single_channel "$CHANNEL_NATIVE_NEWAPI" 'gpt-5.5-openai-compact'
  reset_upstream
  local out="$TMP_DIR/native_compact.json"
  local code response_id payload summary
  code="$(post_json /v1/responses/compact '{"model":"gpt-5.5-openai-compact","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"compact this codex context"}]}]}' "$out")"
  echo "status=${code}"
  [[ "$code" == "200" ]]
  node -e 'const fs=require("fs"); const o=JSON.parse(fs.readFileSync(process.argv[1],"utf8")); if(!JSON.stringify(o).includes("compaction")) process.exit(2); console.log("has_compaction=true")' "$out"
  capture_summary
  response_id="$(node -e 'const fs=require("fs"); const o=JSON.parse(fs.readFileSync(process.argv[1],"utf8")); if(!o.id) process.exit(2); console.log(o.id);' "$out")"
  reset_upstream
  payload="$(node -e 'const id=process.argv[1]; console.log(JSON.stringify({model:"gpt-5.5",previous_response_id:id,input:[{type:"message",role:"user",content:[{type:"input_text",text:"continue after native compact"}]}]}))' "$response_id")"
  out="$TMP_DIR/native_continue.json"
  code="$(post_json /v1/responses "$payload" "$out")"
  echo "continue_status=${code}"
  [[ "$code" == "200" ]]
  summary="$(capture_summary)"
  echo "$summary"
  [[ "$summary" == *"\"previous_response_id\":\"${response_id}\""* ]]
}

run_case_stream_options_include_usage_recovery() {
  echo "case=stream_options_include_usage_recovery"
  set_single_channel "$CHANNEL_NATIVE_NEWAPI" 'gpt-5.5'
  reset_upstream

  local payload out code captures capability
  payload='{"model":"gpt-5.5","stream":true,"stream_options":{"include_usage":true,"include_obfuscation":true},"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"STREAM_OPTIONS_INCLUDE_USAGE_E2E"}]}]}'
  out="$TMP_DIR/stream_options_first.sse"
  code="$(post_json /v1/responses "$payload" "$out")"
  echo "first_status=${code}"
  [[ "$code" == "200" ]]
  assert_contains "$out" 'resp_stream_options_compat_ok'
  assert_not_contains "$out" 'resp_stream_options_compat_error'
  assert_not_contains "$out" 'Unknown parameter'

  captures="$TMP_DIR/stream_options_first_captures.json"
  curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$UPSTREAM_CURL_MAX_TIME" -fsS "$UPSTREAM_HOST_URL/_captures" >"$captures"
  node -e '
    const fs = require("fs");
    const captures = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
    if (captures.length !== 2) process.exit(2);
    const [first, second] = captures;
    if (!first.hasStreamOptionsIncludeUsage || !first.hasStreamOptionsIncludeObfuscation) process.exit(3);
    if (second.hasStreamOptionsIncludeUsage || !second.hasStreamOptionsIncludeObfuscation) process.exit(4);
    console.log("first_attempt_retried=true");
  ' "$captures"

  capability="$(psql_query <<< "select settings::jsonb #>> '{responses_capability_registry,supports_stream_options_include_usage}' from channels where id = ${CHANNEL_NATIVE_NEWAPI}")"
  [[ "$capability" == "false" ]]
  echo "capability_persisted=false"

  reset_upstream
  out="$TMP_DIR/stream_options_second.sse"
  code="$(post_json /v1/responses "$payload" "$out")"
  echo "second_status=${code}"
  [[ "$code" == "200" ]]
  assert_contains "$out" 'resp_stream_options_compat_ok'
  assert_not_contains "$out" 'Unknown parameter'

  captures="$TMP_DIR/stream_options_second_captures.json"
  curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$UPSTREAM_CURL_MAX_TIME" -fsS "$UPSTREAM_HOST_URL/_captures" >"$captures"
  node -e '
    const fs = require("fs");
    const captures = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
    if (captures.length !== 1) process.exit(2);
    const [attempt] = captures;
    if (attempt.hasStreamOptionsIncludeUsage || !attempt.hasStreamOptionsIncludeObfuscation) process.exit(3);
    console.log("learned_capability_pre_stripped=true");
  ' "$captures"
}

run_case_remote_compaction_413_recovery() {
  echo "case=remote_compaction_413_recovery"
  set_single_channel "$CHANNEL_NATIVE_NEWAPI" 'gpt-5.5'
  reset_upstream

  local payload out code captures request_body_limit_present
  payload='{"model":"gpt-5.5","stream":true,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"REMOTE_COMPACTION_413_E2E"}]},{"type":"compaction_trigger"}]}'
  out="$TMP_DIR/remote_compaction_413.sse"
  code="$(post_json_remote_compaction /v1/responses "$payload" "$out")"
  echo "status=${code}"
  [[ "$code" == "200" ]]
  assert_contains "$out" 'compaction'
  assert_not_contains "$out" 'upstream request too large'

  captures="$TMP_DIR/remote_compaction_413_captures.json"
  curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$UPSTREAM_CURL_MAX_TIME" -fsS "$UPSTREAM_HOST_URL/_captures" >"$captures"
  node -e '
    const fs = require("fs");
    const captures = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
    if (captures.length !== 2) process.exit(2);
    const [first, second] = captures;
    if (!first.hasCompactionTrigger || second.hasCompactionTrigger) process.exit(3);
    console.log("upstream_413_recovered_with_synthetic_compact=true");
  ' "$captures"

  request_body_limit_present="$(psql_query <<< "select coalesce(settings::jsonb ? 'request_body_limit', false) from channels where id = ${CHANNEL_NATIVE_NEWAPI}")"
  [[ "$request_body_limit_present" == "f" ]]
  echo "recoverable_remote_413_did_not_set_request_body_limit=true"
}

run_case_synthetic_continue() {
  echo "case=synthetic_newapi_compact_and_continue"
  set_single_channel "$CHANNEL_SYNTHETIC_NEWAPI" 'gpt-5.5-openai-compact'
  reset_upstream
  local out="$TMP_DIR/synth_compact.json"
  local code marker payload
  code="$(post_json /v1/responses/compact '{"model":"gpt-5.5-openai-compact","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"project state: keep branch and verification checklist"}]}]}' "$out")"
  echo "compact_status=${code}"
  [[ "$code" == "200" ]]
  marker="$(node -e 'const fs=require("fs"); const o=JSON.parse(fs.readFileSync(process.argv[1],"utf8")); const m=JSON.stringify(o).match(/newapi\.synthetic\.compact:[^"\\]+/); if(!m) process.exit(2); console.log(m[0]);' "$out")"
  echo "marker_found=true"
  reset_upstream
  payload="$(node -e 'const marker=process.argv[1]; console.log(JSON.stringify({model:"gpt-5.5",input:[{type:"compaction",encrypted_content:marker},{type:"message",role:"user",content:[{type:"input_text",text:"continue after compact"}]}]}))' "$marker")"
  out="$TMP_DIR/synth_continue.json"
  code="$(post_json /v1/responses "$payload" "$out")"
  echo "continue_status=${code}"
  [[ "$code" == "200" ]]
  capture_summary
  SYNTH_MARKER="$marker"
}

run_case_stale_visible_only() {
  echo "case=stale_local_marker_visible_only"
  set_single_channel "$CHANNEL_SYNTHETIC_NEWAPI" 'gpt-5.5'
  reset_upstream
  local instance stale payload out code
  : "${SYNTH_MARKER:?run_case_stale_visible_only requires run_case_synthetic_continue first}"
  instance="$(node -e 'const marker = process.argv[1]; const match = marker.match(/^newapi\.synthetic\.compact:v2:([^:]+):resp_newapi_synthcmp_\1_/); if (!match) process.exit(2); console.log(match[1]);' "$SYNTH_MARKER")"
  if [[ -z "$instance" ]]; then
    echo "cannot parse synthetic compact instance from current marker" >&2
    echo "run_case_stale_visible_only depends on run_case_synthetic_continue running first and creating synthetic_compact_state_records" >&2
    return 1
  fi
  stale="newapi.synthetic.compact:v2:${instance}:resp_newapi_synthcmp_${instance}_missing_stale"
  payload="$(node -e 'const marker=process.argv[1]; console.log(JSON.stringify({model:"gpt-5.5",input:[{type:"compaction",encrypted_content:marker},{type:"message",role:"user",content:[{type:"input_text",text:"visible follow-up survives stale marker"}]}]}))' "$stale")"
  out="$TMP_DIR/stale_visible.json"
  code="$(post_json /v1/responses "$payload" "$out")"
  echo "status=${code}"
  [[ "$code" == "200" ]]
  capture_summary
}

run_case_generic_rejects_remote_opaque() {
  echo "case=generic_openai_rejects_remote_opaque_compaction"
  set_single_channel "$CHANNEL_GENERIC_OPENAI" 'gpt-5.5'
  reset_upstream
  local out="$TMP_DIR/generic_reject.json"
  local code
  code="$(post_json /v1/responses '{"model":"gpt-5.5","input":[{"type":"compaction","encrypted_content":"remote-native-opaque"},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}' "$out")"
  echo "status=${code}"
  [[ "$code" == "503" ]]
  assert_contains "$out" 'compaction'
  capture_summary
}

run_case_native_opaque_replay_is_byte_preserving_and_not_retried() {
  echo "case=native_opaque_replay_byte_preserving_no_retry"
  set_single_channel "$CHANNEL_NATIVE_NEWAPI" 'gpt-5.5'
  local original_model_mapping
  original_model_mapping="$(snapshot_channel_model_mapping "$CHANNEL_NATIVE_NEWAPI")"
  psql_exec -v channel="$CHANNEL_NATIVE_NEWAPI" >/dev/null <<'SQL'
update channels
set model_mapping = '{"gpt-5.5":"gpt-5.4"}'
where id = :channel;
SQL
  restart_dev
  reset_upstream
  local payload expected_hash out code summary
  payload='{
  "model": "gpt-5.5",
  "input": [
    {"type":"reasoning","encrypted_content":"opaque-native-retry-guard"},
    {"type":"message","role":"user","content":[{"type":"input_text","text":"opaque replay guard"}]}
  ],
  "future_top_level": { "keep": [1, 2, 3] }
}'
  expected_hash="$(node -e 'const crypto=require("crypto"); const body=process.argv[1].replace("\"gpt-5.5\"", "\"gpt-5.4\""); console.log(crypto.createHash("sha256").update(body).digest("hex"));' "$payload")"
  out="$TMP_DIR/native_opaque_replay.json"
  code="$(post_json /v1/responses "$payload" "$out")"
  echo "status=${code}"
  [[ "$code" == "400" ]]
  assert_contains "$out" 'invalid_encrypted_content'
  summary="$(capture_summary)"
  node - "$summary" "$expected_hash" <<'NODE'
const captures = JSON.parse(process.argv[2]);
const expectedHash = process.argv[3];
if (captures.length !== 1) {
  throw new Error(`expected exactly one upstream attempt, got ${captures.length}`);
}
const capture = captures[0];
if (capture.url !== '/v1/responses') throw new Error(`unexpected upstream path: ${capture.url}`);
if (capture.model !== 'gpt-5.4') throw new Error(`mapped model was not forwarded: ${capture.model}`);
if (!capture.hasEncryptedContent) throw new Error('upstream capture lost encrypted_content');
if (capture.bodySha256 !== expectedHash) {
  throw new Error('opaque request changed outside the mapped model');
}
NODE
  restore_channel_model_mapping "$CHANNEL_NATIVE_NEWAPI" "$original_model_mapping"
}

run_case_local_codex_opaque_replay_is_byte_preserving_and_not_retried() {
  echo "case=local_codex_opaque_replay_byte_preserving_no_retry"
  set_single_channel "$CHANNEL_NATIVE_NEWAPI" 'gpt-5.5'
  local original_model_mapping
  original_model_mapping="$(snapshot_channel_model_mapping "$CHANNEL_NATIVE_NEWAPI")"
  psql_exec -v channel="$CHANNEL_NATIVE_NEWAPI" >/dev/null <<'SQL'
update channels
set model_mapping = '{"gpt-5.5":"gpt-5.4"}'
where id = :channel;
SQL
  restart_dev
  reset_upstream
  local payload expected_hash out code summary
  payload='{
  "model": "gpt-5.5",
  "input": [
    {"type":"reasoning","encrypted_content":"opaque-native-retry-guard"},
    {"type":"message","role":"user","content":[{"type":"input_text","text":"opaque local Codex replay guard"}]}
  ],
  "future_top_level": { "keep": [4, 5, 6] }
}'
  expected_hash="$(node -e 'const crypto=require("crypto"); const body=process.argv[1].replace("\"gpt-5.5\"", "\"gpt-5.4\""); console.log(crypto.createHash("sha256").update(body).digest("hex"));' "$payload")"
  out="$TMP_DIR/local_codex_opaque_replay.json"
  code="$(post_json_local_codex_compaction /v1/responses "$payload" "$out")"
  echo "status=${code}"
  [[ "$code" == "400" ]]
  assert_contains "$out" 'invalid_encrypted_content'
  summary="$(capture_summary)"
  node - "$summary" "$expected_hash" <<'NODE'
const captures = JSON.parse(process.argv[2]);
const expectedHash = process.argv[3];
if (captures.length !== 1) {
  throw new Error(`expected exactly one upstream attempt, got ${captures.length}`);
}
const capture = captures[0];
if (capture.url !== '/v1/responses') throw new Error(`unexpected upstream path: ${capture.url}`);
if (capture.model !== 'gpt-5.4') throw new Error(`mapped model was not forwarded: ${capture.model}`);
if (!capture.hasEncryptedContent) throw new Error('upstream capture lost encrypted_content');
if (capture.bodySha256 !== expectedHash) {
  throw new Error('local Codex opaque request changed outside the mapped model');
}
NODE
  restore_channel_model_mapping "$CHANNEL_NATIVE_NEWAPI" "$original_model_mapping"
}

run_case_sub2api_previous_response_id() {
  echo "case=sub2api_http_rejects_rest_previous_id"
  set_single_channel "$CHANNEL_SUB2API_HTTP" 'gpt-5.5-openai-compact'
  reset_upstream
  local out="$TMP_DIR/sub2api_prev.json"
  local code
  code="$(post_json /v1/responses/compact '{"model":"gpt-5.5-openai-compact","previous_response_id":"resp_remote_prev","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"compact with previous id"}]}]}' "$out")"
  echo "status=${code}"
  [[ "$code" == "400" ]]
  assert_contains "$out" 'previous_response_id'
  capture_summary
}

run_case_model_switch_restore() {
  echo "case=model_switch_synthetic_restore"
  set_single_channel "$CHANNEL_SYNTHETIC_NEWAPI" 'gpt-5.4'
  reset_upstream
  local payload out code
  payload="$(node -e 'const marker=process.argv[1]; console.log(JSON.stringify({model:"gpt-5.4",input:[{type:"compaction",encrypted_content:marker},{type:"message",role:"user",content:[{type:"input_text","text":"continue after model switch"}]}]}))' "$SYNTH_MARKER")"
  out="$TMP_DIR/model_switch.json"
  code="$(post_json /v1/responses "$payload" "$out")"
  echo "status=${code}"
  [[ "$code" == "200" ]]
  capture_summary
}

run_case_real_codex_history_fixture() {
  echo "case=real_codex_history_synthetic_restore"
  local fixture="${HISTORY_FIXTURE_PATH}"
  if [[ -z "$fixture" ]]; then
    fixture="$TMP_DIR/codex-history-fixture.json"
    node "$ROOT_DIR/scripts/codex-history-compact-fixture.mjs" "$fixture"
  fi
  set_single_channel "$CHANNEL_SYNTHETIC_NEWAPI" 'gpt-5.5-openai-compact'
  reset_upstream
  local compact_payload out code marker continue_payload
  compact_payload="$(node -e 'const fs=require("fs"); const f=JSON.parse(fs.readFileSync(process.argv[1],"utf8")); console.log(JSON.stringify({model:f.model,input:f.input}))' "$fixture")"
  out="$TMP_DIR/history_compact.json"
  code="$(post_json /v1/responses/compact "$compact_payload" "$out")"
  echo "compact_status=${code}"
  [[ "$code" == "200" ]]
  marker="$(node -e 'const fs=require("fs"); const o=JSON.parse(fs.readFileSync(process.argv[1],"utf8")); const m=JSON.stringify(o).match(/newapi\.synthetic\.compact:[^"\\]+/); if(!m) process.exit(2); console.log(m[0]);' "$out")"
  reset_upstream
  continue_payload="$(node -e 'const fs=require("fs"); const f=JSON.parse(fs.readFileSync(process.argv[1],"utf8")); const marker=process.argv[2]; console.log(JSON.stringify({model:f.followup_model,input:[{type:"compaction",encrypted_content:marker},f.followup]}))' "$fixture" "$marker")"
  out="$TMP_DIR/history_continue.json"
  code="$(post_json /v1/responses "$continue_payload" "$out")"
  echo "continue_status=${code}"
  [[ "$code" == "200" ]]
  capture_summary
}

main() {
  for cmd in docker curl node openssl shasum; do
    command -v "$cmd" >/dev/null 2>&1 || { echo "missing required command: $cmd" >&2; exit 1; }
  done
  guard_dev_target
  validate_e2e_environment
  resolve_redis_password
  acquire_e2e_lock
  trap release_e2e_lock EXIT
  ensure_fake_upstream
  trap cleanup EXIT
  stop_dev
  snapshot_redis_state
  backup_and_seed_db
  DB_SNAPSHOT_READY=1
  local token
  token="$(psql_query <<< "select key from tokens where id = ${TOKEN_ID}")"
  if [[ -z "$token" ]]; then
    echo "missing compact e2e token" >&2
    exit 1
  fi
  prepare_curl_config "sk-${token}"
  start_dev
  docker exec "$APP_CONTAINER" /new-api --build-info
  run_case_native_newapi_compact
  run_case_stream_options_include_usage_recovery
  run_case_remote_compaction_413_recovery
  run_case_synthetic_continue
  run_case_stale_visible_only
  run_case_generic_rejects_remote_opaque
  run_case_native_opaque_replay_is_byte_preserving_and_not_retried
  run_case_local_codex_opaque_replay_is_byte_preserving_and_not_retried
  run_case_sub2api_previous_response_id
  run_case_model_switch_restore
  run_case_real_codex_history_fixture
}

main "$@"
