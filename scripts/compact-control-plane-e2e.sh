#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASE_URL="${COMPACT_E2E_BASE_URL:-http://127.0.0.1:3001}"
REQUEST_MODE="${COMPACT_E2E_REQUEST_MODE:-host}"
NETWORK_NAME="${COMPACT_E2E_DOCKER_NETWORK:-}"
NETWORK_BASE_URL="${COMPACT_E2E_DOCKER_BASE_URL:-http://new-api:3000}"
NETWORK_CURL_IMAGE="${COMPACT_E2E_NETWORK_CURL_IMAGE:-curlimages/curl:8.10.1}"
NETWORK_CLIENT_USER="${COMPACT_E2E_NETWORK_CLIENT_USER:-$(id -u):$(id -g)}"
UPSTREAM_HOST_URL="${COMPACT_E2E_UPSTREAM_HOST_URL:-http://127.0.0.1:18081}"
UPSTREAM_CONTAINER_URL="${COMPACT_E2E_UPSTREAM_CONTAINER_URL:-http://host.docker.internal:18081}"
POSTGRES_CONTAINER="${COMPACT_E2E_POSTGRES_CONTAINER:-}"
POSTGRES_USER="${COMPACT_E2E_POSTGRES_USER:-${DEV_POSTGRES_USER:-}}"
POSTGRES_DB="${COMPACT_E2E_POSTGRES_DB:-${DEV_POSTGRES_DB:-}}"
APP_CONTAINER="${COMPACT_E2E_APP_CONTAINER:-}"
REDIS_CONTAINER="${COMPACT_E2E_REDIS_CONTAINER:-}"
COMPOSE_FILE="${COMPACT_E2E_COMPOSE_FILE:-deploy/compose/dev-isolated.yml}"
ENV_FILE="${COMPACT_E2E_ENV_FILE:-deploy/env/dev-isolated.env}"
APP_SERVICE="${COMPACT_E2E_APP_SERVICE:-new-api}"
COMPOSE_PROJECT_NAME="${COMPACT_E2E_COMPOSE_PROJECT_NAME:-}"
CURL_CONNECT_TIMEOUT="${COMPACT_E2E_CURL_CONNECT_TIMEOUT:-5}"
CURL_MAX_TIME="${COMPACT_E2E_CURL_MAX_TIME:-60}"
HOST_BRIDGE_PROBE_MAX_TIME="${COMPACT_E2E_HOST_BRIDGE_PROBE_MAX_TIME:-5}"
HOST_BRIDGE_PROBE_ATTEMPTS="${COMPACT_E2E_HOST_BRIDGE_PROBE_ATTEMPTS:-1}"
DEV_RECREATE_ATTEMPTS="${COMPACT_E2E_DEV_RECREATE_ATTEMPTS:-2}"
DEV_RECREATE_RETRY_DELAY_SECONDS="${COMPACT_E2E_DEV_RECREATE_RETRY_DELAY_SECONDS:-2}"
DEV_READY_ATTEMPTS="${COMPACT_E2E_DEV_READY_ATTEMPTS:-90}"
DEV_STOP_TIMEOUT_SECONDS="${COMPACT_E2E_DEV_STOP_TIMEOUT_SECONDS:-180}"
UPSTREAM_CURL_MAX_TIME="${COMPACT_E2E_UPSTREAM_CURL_MAX_TIME:-30}"
REMOTE_COMPACTION_BODY_LIMIT_BYTES="${COMPACT_E2E_REMOTE_COMPACTION_BODY_LIMIT_BYTES:-32768}"
REMOTE_COMPACTION_MEDIA_BYTES="${COMPACT_E2E_REMOTE_COMPACTION_MEDIA_BYTES:-65536}"
LOCAL_COMPACTION_BODY_LIMIT_BYTES="${COMPACT_E2E_LOCAL_COMPACTION_BODY_LIMIT_BYTES:-32768}"
LOCAL_COMPACTION_PREEMPTIVE_IMAGE_COUNT="${COMPACT_E2E_LOCAL_COMPACTION_PREEMPTIVE_IMAGE_COUNT:-17}"
LOCAL_COMPACTION_PREEMPTIVE_IMAGE_BYTES="${COMPACT_E2E_LOCAL_COMPACTION_PREEMPTIVE_IMAGE_BYTES:-16777216}"
LOCAL_COMPACTION_PREEMPTIVE_TOOL_BYTES="${COMPACT_E2E_LOCAL_COMPACTION_PREEMPTIVE_TOOL_BYTES:-34603008}"
RUN_ID="compact_e2e_$(date +%Y%m%d%H%M%S)_$$"
TEST_GROUP="${COMPACT_E2E_GROUP:-compact-e2e-${RUN_ID}}"
TOKEN_ID="${COMPACT_E2E_TOKEN_ID:-}"
CHANNEL_NATIVE_NEWAPI="${COMPACT_E2E_CHANNEL_NATIVE_NEWAPI:-}"
CHANNEL_SUB2API_HTTP="${COMPACT_E2E_CHANNEL_SUB2API_HTTP:-}"
CHANNEL_SYNTHETIC_NEWAPI="${COMPACT_E2E_CHANNEL_SYNTHETIC_NEWAPI:-}"
CHANNEL_GENERIC_OPENAI="${COMPACT_E2E_CHANNEL_GENERIC_OPENAI:-}"
TOKEN_NAME="compact-control-plane-e2e-${RUN_ID}"
USERNAME="compact_e2e_user_${RUN_ID}"
CHANNEL_NATIVE_NAME="codex-compact-e2e-${RUN_ID}-native-newapi"
CHANNEL_SUB2API_NAME="codex-compact-e2e-${RUN_ID}-sub2api-http"
CHANNEL_SYNTHETIC_NAME="codex-compact-e2e-${RUN_ID}-synthetic-newapi"
CHANNEL_GENERIC_NAME="codex-compact-e2e-${RUN_ID}-generic-openai"
BACKUP_CHANNELS="${RUN_ID}_channels"
BACKUP_ABILITIES="${RUN_ID}_abilities"
BACKUP_TOKENS="${RUN_ID}_tokens"
BACKUP_USERS="${RUN_ID}_users"
BACKUP_OPTIONS="${RUN_ID}_options"
BACKUP_SEQUENCES="${RUN_ID}_sequences"
TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/newapi-compact-e2e.XXXXXX")"
FAKE_PID=""
HISTORY_FIXTURE_PATH="${COMPACT_E2E_HISTORY_FIXTURE:-}"
RESULT_FILE="${COMPACT_E2E_RESULT_FILE:-}"
CASE_SELECTION="${COMPACT_E2E_CASES:-all}"
HOST_BRIDGE_PROBE_TOTAL=0
HOST_BRIDGE_PROBE_FAILURES=0
DEV_REFRESH_COUNT=0
DB_LOCK_HOLDER_PID=""
DB_SEEDED=0
DB_LOCK_APP_NAME="compact-e2e-lock-${RUN_ID}"
APP_STOPPED=0

cd "$ROOT_DIR"

. "$ROOT_DIR/scripts/compact-control-plane-e2e-lib.sh"

read_env_file_value() {
  local key="$1"
  local line value
  if [[ ! -r "$ENV_FILE" ]]; then
    return 1
  fi
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line#"${line%%[![:space:]]*}"}"
    line="${line%"${line##*[![:space:]]}"}"
    if [[ "$line" == export\ * ]]; then
      line="${line#export }"
    fi
    if [[ "$line" != "$key="* ]]; then
      continue
    fi
    value="${line#*=}"
    value="${value#"${value%%[![:space:]]*}"}"
    value="${value%"${value##*[![:space:]]}"}"
    if [[ ${#value} -ge 2 && "${value:0:1}" == '"' && "${value: -1}" == '"' ]]; then
      value="${value:1:${#value}-2}"
    elif [[ ${#value} -ge 2 && "${value:0:1}" == "'" && "${value: -1}" == "'" ]]; then
      value="${value:1:${#value}-2}"
    fi
    printf '%s' "$value"
    return 0
  done <"$ENV_FILE"
  return 1
}

resolve_compose_resources() {
  local configured="${COMPOSE_PROJECT_NAME:-}"
  local value
  if [[ -z "$configured" && -f "$ENV_FILE" ]]; then
    configured="$(read_env_file_value DEV_COMPOSE_PROJECT_NAME || true)"
  fi
  configured="${configured:-new-api-dev-isolated}"
  if [[ ! "$configured" =~ ^[a-z0-9][a-z0-9_-]*$ ]]; then
    echo "invalid COMPACT_E2E_COMPOSE_PROJECT_NAME" >&2
    return 1
  fi
  COMPOSE_PROJECT_NAME="$configured"
  if [[ -z "$POSTGRES_USER" ]]; then
    value="$(read_env_file_value DEV_POSTGRES_USER || true)"
    POSTGRES_USER="${value:-root}"
  fi
  if [[ -z "$POSTGRES_DB" ]]; then
    value="$(read_env_file_value DEV_POSTGRES_DB || true)"
    POSTGRES_DB="${value:-new-api-dev}"
  fi
  NETWORK_NAME="${NETWORK_NAME:-${COMPOSE_PROJECT_NAME}_dev-isolated-network}"
  POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-${COMPOSE_PROJECT_NAME}-postgres-1}"
  APP_CONTAINER="${APP_CONTAINER:-${COMPOSE_PROJECT_NAME}-new-api-1}"
  REDIS_CONTAINER="${REDIS_CONTAINER:-${COMPOSE_PROJECT_NAME}-redis-1}"
}

psql_exec() { docker exec -i "$POSTGRES_CONTAINER" psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 "$@"; }

psql_query() { psql_exec -At "$@"; }

run_compose() {
  docker compose --project-name "$COMPOSE_PROJECT_NAME" -f "$COMPOSE_FILE" --env-file "$ENV_FILE" "$@"
}

redis_exec() {
  docker exec "$REDIS_CONTAINER" sh -c \
    'REDISCLI_AUTH="${REDIS_PASSWORD:-}"; export REDISCLI_AUTH; exec redis-cli --no-auth-warning "$@"' \
    sh "$@"
}

# redis-cli can return exit code 0 even when Redis replies with a server-side
# error (for example WRONGTYPE or NOAUTH). Cleanup must not treat such replies
# as successful, so normalize both transport and protocol failures here.
redis_exec_checked() {
  local output rc first_line
  output="$(redis_exec "$@" 2>&1)"
  rc=$?
  if (( rc != 0 )); then
    [[ -n "$output" ]] && printf '%s\n' "$output" >&2
    return "$rc"
  fi
  first_line="${output%%$'\n'*}"
  case "$first_line" in
    ERR\ *|WRONGTYPE\ *|NOAUTH\ *|NOPERM\ *|MISCONF\ *|LOADING\ *|BUSY\ *|NOSCRIPT\ *|OOM\ *|READONLY\ *|EXECABORT\ *|CROSSSLOT\ *|TRYAGAIN\ *|CLUSTERDOWN\ *|NOGROUP\ *|UNBLOCKED\ *|DENIED\ *|error:*|ERROR,*|\(error\)\ *)
      [[ -n "$output" ]] && printf '%s\n' "$output" >&2
      return 1
      ;;
  esac
  printf '%s' "$output"
}

capture_synthetic_state_ids() {
  local table_exists
  if ! table_exists="$(psql_query -c "select to_regclass('public.synthetic_compact_state_records') is not null;" 2>/dev/null)"; then
    return 1
  fi
  : >"$TMP_DIR/synthetic-state-ids"
  if [[ "$table_exists" != "t" ]]; then
    return 0
  fi
  psql_exec -At \
    -v user_id="$TOKEN_ID" \
    -v token_id="$TOKEN_ID" \
    -v channel_native_newapi="$CHANNEL_NATIVE_NEWAPI" \
    -v channel_sub2api_http="$CHANNEL_SUB2API_HTTP" \
    -v channel_synthetic_newapi="$CHANNEL_SYNTHETIC_NEWAPI" \
    -v channel_generic_openai="$CHANNEL_GENERIC_OPENAI" <<'SQL' >"$TMP_DIR/synthetic-state-ids"
select id
from synthetic_compact_state_records
where user_id = :user_id
   or token_id = :token_id
   or channel_id in (:channel_native_newapi, :channel_sub2api_http, :channel_synthetic_newapi, :channel_generic_openai);
SQL
}

cleanup_redis_state() {
  local key state_key metadata metadata_key digest value
  local cached_token_id cached_user_id
  local cleanup_failed=0

  if ! redis_exec_checked ping >/dev/null; then
    cleanup_failed=1
  fi
  if ! redis_exec_checked DEL \
    "user:${TOKEN_ID}" \
    "rateLimit:${TOKEN_ID}" \
    "rateLimit:MRRL:${TOKEN_ID}" \
    "rateLimit:MRRLS:${TOKEN_ID}" \
    "new-api:channel-concurrency:v1:${CHANNEL_NATIVE_NEWAPI}:leases" \
    "new-api:channel-concurrency:v1:${CHANNEL_SUB2API_HTTP}:leases" \
    "new-api:channel-concurrency:v1:${CHANNEL_SYNTHETIC_NEWAPI}:leases" \
    "new-api:channel-concurrency:v1:${CHANNEL_GENERIC_OPENAI}:leases" >/dev/null; then
    cleanup_failed=1
  fi

  # Token cache keys are HMAC-based and cannot be derived from the fixture ID.
  # Match the cached token object by its ID/user fields instead of deleting the
  # whole token namespace or exposing the raw token key.
  if ! redis_exec_checked --scan --pattern "token:*" >"$TMP_DIR/redis-token-keys"; then
    cleanup_failed=1
    : >"$TMP_DIR/redis-token-keys"
  fi
  while IFS= read -r key; do
    [[ -n "$key" ]] || continue
    if ! cached_token_id="$(redis_exec_checked HGET "$key" Id)"; then
      cleanup_failed=1
      continue
    fi
    if ! cached_user_id="$(redis_exec_checked HGET "$key" UserId)"; then
      cleanup_failed=1
      continue
    fi
    if [[ "$cached_token_id" == "$TOKEN_ID" || "$cached_user_id" == "$TOKEN_ID" ]]; then
      if ! redis_exec_checked DEL "$key" >/dev/null; then
        cleanup_failed=1
      fi
    fi
  done <"$TMP_DIR/redis-token-keys"

  # Future model-rate-limit marks may add another suffix, so remove only keys
  # whose final component is this fixture's user ID.
  if ! redis_exec_checked --scan --pattern "rateLimit:*:${TOKEN_ID}" >"$TMP_DIR/redis-model-rate-keys"; then
    cleanup_failed=1
    : >"$TMP_DIR/redis-model-rate-keys"
  fi
  while IFS= read -r key; do
    [[ -n "$key" ]] || continue
    if ! redis_exec_checked DEL "$key" >/dev/null; then
      cleanup_failed=1
    fi
  done <"$TMP_DIR/redis-model-rate-keys"

  if ! redis_exec_checked --scan --pattern "*user:${TOKEN_ID}" >"$TMP_DIR/redis-user-keys"; then
    cleanup_failed=1
    : >"$TMP_DIR/redis-user-keys"
  fi
  while IFS= read -r key; do
    [[ -n "$key" ]] || continue
    if ! redis_exec_checked DEL "$key" >/dev/null; then
      cleanup_failed=1
    fi
  done <"$TMP_DIR/redis-user-keys"

  # Perf buckets are keyed by model/group/time. The generated group is unique
  # to this run, so this removes only the fixture's buckets.
  if ! redis_exec_checked --scan --pattern "perf:*:${TEST_GROUP}:*" >"$TMP_DIR/redis-perf-keys"; then
    cleanup_failed=1
    : >"$TMP_DIR/redis-perf-keys"
  fi
  while IFS= read -r key; do
    [[ -n "$key" ]] || continue
    if ! redis_exec_checked DEL "$key" >/dev/null; then
      cleanup_failed=1
    fi
  done <"$TMP_DIR/redis-perf-keys"

  # Channel-health state is keyed by an opaque route digest. Metadata carries
  # the fixture group, which lets us remove the matching route keys and index
  # entries without touching other channels' health state.
  local channel_health_prefix="new-api:channel-health:v1"
  if ! redis_exec_checked --scan --pattern "${channel_health_prefix}:*:metadata" >"$TMP_DIR/redis-channel-health-metadata-keys"; then
    cleanup_failed=1
    : >"$TMP_DIR/redis-channel-health-metadata-keys"
  fi
  while IFS= read -r metadata_key; do
    [[ -n "$metadata_key" ]] || continue
    if ! metadata="$(redis_exec_checked GET "$metadata_key")"; then
      cleanup_failed=1
      continue
    fi
    if [[ "$metadata" != *"\"group\":\"${TEST_GROUP}\""* ]]; then
      continue
    fi
    digest="${metadata_key#"${channel_health_prefix}:"}"
    digest="${digest%:metadata}"
    if ! redis_exec_checked DEL \
      "$metadata_key" \
      "${channel_health_prefix}:${digest}:cooldown" \
      "${channel_health_prefix}:${digest}:history" \
      "${channel_health_prefix}:${digest}:probe" \
      "${channel_health_prefix}:${digest}:failures" >/dev/null; then
      cleanup_failed=1
    fi
    if ! redis_exec_checked ZREM "${channel_health_prefix}:runtime-index" "$digest" >/dev/null; then
      cleanup_failed=1
    fi
  done <"$TMP_DIR/redis-channel-health-metadata-keys"

  # Runtime events share one bounded list. Filter only entries carrying this
  # fixture group and preserve all events from other requests.
  if ! redis_exec_checked EVAL '
local key = KEYS[1]
local marker = "\"group\":\"" .. ARGV[1] .. "\""
local values = redis.call("LRANGE", key, 0, -1)
local removed = 0
for _, value in ipairs(values) do
  if string.find(value, marker, 1, true) then
    removed = removed + redis.call("LREM", key, 0, value)
  end
end
return removed
' 1 "${channel_health_prefix}:runtime-events" "$TEST_GROUP" >/dev/null; then
    cleanup_failed=1
  fi

  # Affinity caches use either the fixture group in the key or the fixture
  # channel ID as the value. Both checks are scoped and avoid a namespace-wide
  # purge when affinity is enabled in the isolated settings.
  local affinity_prefix="new-api:channel_affinity:v1"
  if ! redis_exec_checked --scan --pattern "${affinity_prefix}:*" >"$TMP_DIR/redis-affinity-keys"; then
    cleanup_failed=1
    : >"$TMP_DIR/redis-affinity-keys"
  fi
  while IFS= read -r key; do
    [[ -n "$key" ]] || continue
    if ! value="$(redis_exec_checked GET "$key")"; then
      cleanup_failed=1
      continue
    fi
    if [[ "$key" == *":${TEST_GROUP}:"* || "$value" == "$CHANNEL_NATIVE_NEWAPI" || "$value" == "$CHANNEL_SUB2API_HTTP" || "$value" == "$CHANNEL_SYNTHETIC_NEWAPI" || "$value" == "$CHANNEL_GENERIC_OPENAI" ]]; then
      if ! redis_exec_checked DEL "$key" >/dev/null; then
        cleanup_failed=1
      fi
    fi
  done <"$TMP_DIR/redis-affinity-keys"

  local affinity_stats_prefix="new-api:channel_affinity_usage_cache_stats:v1"
  if ! redis_exec_checked --scan --pattern "${affinity_stats_prefix}:*" >"$TMP_DIR/redis-affinity-stats-keys"; then
    cleanup_failed=1
    : >"$TMP_DIR/redis-affinity-stats-keys"
  fi
  while IFS= read -r key; do
    [[ -n "$key" ]] || continue
    if [[ "$key" == *$'\n'"${TEST_GROUP}"$'\n'* ]]; then
      if ! redis_exec_checked DEL "$key" >/dev/null; then
        cleanup_failed=1
      fi
    fi
  done <"$TMP_DIR/redis-affinity-stats-keys"

  if [[ ! -r "$TMP_DIR/synthetic-state-ids" ]]; then
    cleanup_failed=1
    : >"$TMP_DIR/synthetic-state-ids"
  fi
  while IFS= read -r state_key; do
    [[ -n "$state_key" ]] || continue
    if ! redis_exec_checked DEL "new-api:responses:synthetic-compact:${state_key}" >/dev/null; then
      cleanup_failed=1
    fi
  done <"$TMP_DIR/synthetic-state-ids"

  return "$cleanup_failed"
}

stop_dev_app() {
  local stop_log="$TMP_DIR/stop-dev.log"
  if (( APP_STOPPED == 1 )); then
    return 0
  fi
  if ! run_compose stop --timeout "$DEV_STOP_TIMEOUT_SECONDS" "$APP_SERVICE" >"$stop_log" 2>&1; then
    echo "failed to stop isolated app service=$APP_SERVICE" >&2
    cat "$stop_log" >&2 || true
    return 1
  fi
  APP_STOPPED=1
}

acquire_db_lock() {
  local lock_log="$TMP_DIR/db-lock.log"
  local lock_state attempt

  docker exec -e "PGAPPNAME=${DB_LOCK_APP_NAME}" "$POSTGRES_CONTAINER" psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -Atc \
    "select pg_advisory_lock(2147483647); select pg_sleep(2147483647::double precision);" \
    >"$lock_log" 2>&1 &
  DB_LOCK_HOLDER_PID="$!"
  for ((attempt = 1; attempt <= 100; attempt++)); do
    lock_state="$(docker exec "$POSTGRES_CONTAINER" psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -Atc \
      "select exists (select 1 from pg_locks l join pg_stat_activity a on a.pid = l.pid where l.locktype = 'advisory' and l.classid = 0 and l.objid = 2147483647 and l.granted and a.application_name = '${DB_LOCK_APP_NAME}');" \
      2>/dev/null || true)"
    if [[ "$lock_state" == "t" ]]; then
      return 0
    fi
    if ! kill -0 "$DB_LOCK_HOLDER_PID" >/dev/null 2>&1; then
      echo "database advisory lock holder exited before acquiring the lock" >&2
      cat "$lock_log" >&2 || true
      DB_LOCK_HOLDER_PID=""
      return 1
    fi
    sleep 0.1
  done
  echo "timed out waiting for the database advisory lock" >&2
  cat "$lock_log" >&2 || true
  terminate_db_lock_sessions
  kill "$DB_LOCK_HOLDER_PID" >/dev/null 2>&1 || true
  wait "$DB_LOCK_HOLDER_PID" >/dev/null 2>&1 || true
  DB_LOCK_HOLDER_PID=""
  return 1
}

terminate_db_lock_sessions() {
  docker exec "$POSTGRES_CONTAINER" psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -Atc \
    "select pg_terminate_backend(pid) from pg_stat_activity where application_name = '${DB_LOCK_APP_NAME}' and pid <> pg_backend_pid();" \
    >/dev/null 2>&1 || true
}

release_db_lock() {
  if [[ -z "$DB_LOCK_HOLDER_PID" ]]; then
    return 0
  fi
  terminate_db_lock_sessions
  kill "$DB_LOCK_HOLDER_PID" >/dev/null 2>&1 || true
  wait "$DB_LOCK_HOLDER_PID" >/dev/null 2>&1 || true
  DB_LOCK_HOLDER_PID=""
}

is_positive_integer() {
  [[ "$1" =~ ^[1-9][0-9]*$ ]]
}

allocate_fixture_ids() {
  local attempt nonce base collisions

  for attempt in {1..5}; do
    if [[ -n "${COMPACT_E2E_TOKEN_ID:-}" && -n "${COMPACT_E2E_CHANNEL_NATIVE_NEWAPI:-}" && -n "${COMPACT_E2E_CHANNEL_SUB2API_HTTP:-}" && -n "${COMPACT_E2E_CHANNEL_SYNTHETIC_NEWAPI:-}" && -n "${COMPACT_E2E_CHANNEL_GENERIC_OPENAI:-}" ]]; then
      TOKEN_ID="$COMPACT_E2E_TOKEN_ID"
      CHANNEL_NATIVE_NEWAPI="$COMPACT_E2E_CHANNEL_NATIVE_NEWAPI"
      CHANNEL_SUB2API_HTTP="$COMPACT_E2E_CHANNEL_SUB2API_HTTP"
      CHANNEL_SYNTHETIC_NEWAPI="$COMPACT_E2E_CHANNEL_SYNTHETIC_NEWAPI"
      CHANNEL_GENERIC_OPENAI="$COMPACT_E2E_CHANNEL_GENERIC_OPENAI"
    else
      nonce="$(openssl rand -hex 4)"
      base=$((900000000 + (16#$nonce % 9000000)))
      TOKEN_ID="${COMPACT_E2E_TOKEN_ID:-$base}"
      CHANNEL_NATIVE_NEWAPI="${COMPACT_E2E_CHANNEL_NATIVE_NEWAPI:-$((base + 1))}"
      CHANNEL_SUB2API_HTTP="${COMPACT_E2E_CHANNEL_SUB2API_HTTP:-$((base + 2))}"
      CHANNEL_SYNTHETIC_NEWAPI="${COMPACT_E2E_CHANNEL_SYNTHETIC_NEWAPI:-$((base + 3))}"
      CHANNEL_GENERIC_OPENAI="${COMPACT_E2E_CHANNEL_GENERIC_OPENAI:-$((base + 4))}"
    fi

    for value in "$TOKEN_ID" "$CHANNEL_NATIVE_NEWAPI" "$CHANNEL_SUB2API_HTTP" "$CHANNEL_SYNTHETIC_NEWAPI" "$CHANNEL_GENERIC_OPENAI"; do
      if ! is_positive_integer "$value"; then
        echo "fixture IDs must be positive integers, got: $value" >&2
        return 1
      fi
    done

    collisions="$(psql_exec -At \
      -v test_group="$TEST_GROUP" \
      -v token_id="$TOKEN_ID" \
      -v channel_native_newapi="$CHANNEL_NATIVE_NEWAPI" \
      -v channel_sub2api_http="$CHANNEL_SUB2API_HTTP" \
      -v channel_synthetic_newapi="$CHANNEL_SYNTHETIC_NEWAPI" \
      -v channel_generic_openai="$CHANNEL_GENERIC_OPENAI" \
      -v token_name="$TOKEN_NAME" \
      -v username="$USERNAME" \
      -v channel_native_name="$CHANNEL_NATIVE_NAME" \
      -v channel_sub2api_name="$CHANNEL_SUB2API_NAME" \
      -v channel_synthetic_name="$CHANNEL_SYNTHETIC_NAME" \
      -v channel_generic_name="$CHANNEL_GENERIC_NAME" <<'SQL'
select count(*) from (
  select id from channels
  where id in (:channel_native_newapi, :channel_sub2api_http, :channel_synthetic_newapi, :channel_generic_openai)
     or name in (:'channel_native_name', :'channel_sub2api_name', :'channel_synthetic_name', :'channel_generic_name')
  union all
  select id from tokens where id = :token_id or name = :'token_name'
  union all
  select id from users where id = :token_id or username = :'username'
  union all
  select channel_id from abilities
  where "group" = :'test_group'
     or channel_id in (:channel_native_newapi, :channel_sub2api_http, :channel_synthetic_newapi, :channel_generic_openai)
  union all
  select 0 from logs
  where user_id = :token_id
     or token_id = :token_id
     or channel_id in (:channel_native_newapi, :channel_sub2api_http, :channel_synthetic_newapi, :channel_generic_openai)
  union all
  select 0 from quota_data where user_id = :token_id
  union all
  select 0 from synthetic_compact_state_records
  where user_id = :token_id
     or token_id = :token_id
     or group_name = :'test_group'
     or channel_id in (:channel_native_newapi, :channel_sub2api_http, :channel_synthetic_newapi, :channel_generic_openai)
  union all
  select 0 from perf_metrics where "group" = :'test_group'
  union all
  select 0 from options
  where key in (
    'GroupRatio',
    'UserUsableGroups',
    'ModelRatio',
    'CompletionRatio',
    'CacheRatio',
    'CreateCacheRatio'
  )
    -- These options are JSON objects in normal operation, but an invalid or
    -- legacy value must not make collision detection abort with a cast error.
    -- A quoted textual match is intentionally conservative: a false positive
    -- aborts the run rather than risking deletion of an existing fixture.
    and position('"' || :'test_group' || '"' in value) > 0
) collisions;
SQL
    )"
    if [[ "$collisions" == "0" ]]; then
      return 0
    fi
  done

  echo "unable to allocate collision-free E2E fixture IDs" >&2
  return 1
}

network_tmp_path() {
  local host_path="$1"
  local filename="${host_path##*/}"
  if [[ "$host_path" != "$TMP_DIR/$filename" || -z "$filename" ]]; then
    echo "docker-network request path must be a direct child of the E2E temp directory" >&2
    return 1
  fi
  printf '/work/%s\n' "$filename"
}

network_curl() {
  docker run --rm \
    --network "$NETWORK_NAME" \
    --user "$NETWORK_CLIENT_USER" \
    --mount "type=bind,source=$TMP_DIR,target=/work" \
    "$NETWORK_CURL_IMAGE" \
    "$@"
}

data_plane_status_probe() {
  case "$REQUEST_MODE" in
    host)
      curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$CURL_MAX_TIME" -fsS -o /dev/null "$BASE_URL/api/status"
      ;;
    docker-network)
      network_curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$CURL_MAX_TIME" -fsS -o /dev/null "$NETWORK_BASE_URL/api/status"
      ;;
  esac
}

record_host_bridge_probe() {
  local attempt rc
  for ((attempt = 1; attempt <= HOST_BRIDGE_PROBE_ATTEMPTS; attempt++)); do
    HOST_BRIDGE_PROBE_TOTAL=$((HOST_BRIDGE_PROBE_TOTAL + 1))
    if curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$HOST_BRIDGE_PROBE_MAX_TIME" -fsS -o /dev/null "$BASE_URL/api/status"; then
      echo "host_bridge_probe mode=$REQUEST_MODE attempt=$attempt status=ok"
      continue
    else
      rc=$?
    fi
    HOST_BRIDGE_PROBE_FAILURES=$((HOST_BRIDGE_PROBE_FAILURES + 1))
    echo "host_bridge_probe mode=$REQUEST_MODE attempt=$attempt status=failed rc=$rc" >&2
  done
}

guard_dev_target() {
  resolve_compose_resources
  if [[ "${COMPACT_E2E_FORCE:-}" == "1" ]]; then
    return
  fi
  if [[ "$BASE_URL" != "http://127.0.0.1:3001" ]]; then
    echo "refusing to run against non-dev base URL: $BASE_URL" >&2
    echo "set COMPACT_E2E_FORCE=1 only for an isolated throwaway environment" >&2
    exit 1
  fi
  if [[ "$POSTGRES_CONTAINER" != "${COMPOSE_PROJECT_NAME}-postgres-"* ||
    "$REDIS_CONTAINER" != "${COMPOSE_PROJECT_NAME}-redis-"* ||
    "$APP_CONTAINER" != "${COMPOSE_PROJECT_NAME}-new-api-"* ]]; then
    echo "refusing to run outside the configured dev-isolated Compose project" >&2
    exit 1
  fi
  case "$REQUEST_MODE" in
    host)
      ;;
    docker-network)
      if [[ "$NETWORK_BASE_URL" != "http://new-api:3000" ]]; then
        echo "docker-network mode only permits http://new-api:3000 as its data-plane URL" >&2
        exit 1
      fi
      if [[ "$NETWORK_NAME" != "${COMPOSE_PROJECT_NAME}_"* ]]; then
        echo "docker-network mode only permits a network in the configured Compose project" >&2
        exit 1
      fi
      if ! docker network inspect "$NETWORK_NAME" >/dev/null 2>&1; then
        echo "docker-network mode cannot inspect network: $NETWORK_NAME" >&2
        exit 1
      fi
      if ! docker network inspect --format '{{range .Containers}}{{println .Name}}{{end}}' "$NETWORK_NAME" | grep -Fx "${APP_CONTAINER#/}" >/dev/null; then
        echo "docker-network mode requires $APP_CONTAINER on $NETWORK_NAME" >&2
        exit 1
      fi
      ;;
    *)
      echo "unsupported COMPACT_E2E_REQUEST_MODE: $REQUEST_MODE" >&2
      exit 1
      ;;
  esac
  for value in "$HOST_BRIDGE_PROBE_ATTEMPTS" "$DEV_RECREATE_ATTEMPTS" "$DEV_READY_ATTEMPTS" "$DEV_STOP_TIMEOUT_SECONDS"; do
    if ! is_positive_integer "$value"; then
      echo "expected a positive integer, got: $value" >&2
      exit 1
    fi
  done
  export COMPACT_E2E_ALLOW_DB_RESET=1
}

cleanup() {
  local original_rc=$?
  local rc=0
  local cleanup_log="$TMP_DIR/cleanup.log"
  local synthetic_state_capture_ok=1
  local app_quiesced=0
  local db_restored=0
  local redis_cleaned=0
  local restart_allowed=0
  set +e
  if (( DB_SEEDED == 1 )); then
    if stop_dev_app; then
      echo "cleanup: isolated app stopped before database restoration" >&2
      app_quiesced=1
    else
      echo "cleanup: refusing database restoration because isolated app could not be stopped" >&2
      rc=1
    fi
    if ! capture_synthetic_state_ids; then
      echo "cleanup: failed to capture synthetic compact state keys" >&2
      synthetic_state_capture_ok=0
      rc=1
    fi
  fi
  if (( DB_SEEDED == 1 && app_quiesced == 0 )); then
    echo "cleanup: database restoration skipped because isolated app is not quiesced" >&2
  elif (( DB_SEEDED == 1 )) && psql_exec \
    -v test_group="$TEST_GROUP" \
    -v token_id="$TOKEN_ID" \
    -v user_id="$TOKEN_ID" \
    -v token_name="$TOKEN_NAME" \
    -v username="$USERNAME" \
    -v channel_native_newapi="$CHANNEL_NATIVE_NEWAPI" \
    -v channel_sub2api_http="$CHANNEL_SUB2API_HTTP" \
    -v channel_synthetic_newapi="$CHANNEL_SYNTHETIC_NEWAPI" \
    -v channel_generic_openai="$CHANNEL_GENERIC_OPENAI" \
    -v channel_native_name="$CHANNEL_NATIVE_NAME" \
    -v channel_sub2api_name="$CHANNEL_SUB2API_NAME" \
    -v channel_synthetic_name="$CHANNEL_SYNTHETIC_NAME" \
    -v channel_generic_name="$CHANNEL_GENERIC_NAME" \
    -v backup_options="$BACKUP_OPTIONS" \
    -v backup_sequences="$BACKUP_SEQUENCES" \
    >"$cleanup_log" 2>&1 <<SQL
begin;
set local compact_e2e.test_group = :'test_group';
set local compact_e2e.user_id = :'user_id';
set local compact_e2e.token_id = :'token_id';
set local compact_e2e.channel_native_newapi = :'channel_native_newapi';
set local compact_e2e.channel_sub2api_http = :'channel_sub2api_http';
set local compact_e2e.channel_synthetic_newapi = :'channel_synthetic_newapi';
set local compact_e2e.channel_generic_openai = :'channel_generic_openai';
do \$\$
begin
  if to_regclass('${BACKUP_ABILITIES}') is null
    or to_regclass('${BACKUP_CHANNELS}') is null
    or to_regclass('${BACKUP_TOKENS}') is null
    or to_regclass('${BACKUP_USERS}') is null
    or to_regclass('${BACKUP_OPTIONS}') is null
    or to_regclass('${BACKUP_SEQUENCES}') is null then
    raise exception 'compact e2e backup tables are missing';
  end if;
end
\$\$;
delete from abilities where "group" = :'test_group';
insert into abilities select * from ${BACKUP_ABILITIES};
delete from logs
where user_id = :user_id
   or token_id = :token_id
   or channel_id in (:channel_native_newapi, :channel_sub2api_http, :channel_synthetic_newapi, :channel_generic_openai);
delete from quota_data where user_id = :user_id;
delete from synthetic_compact_state_records
where user_id = :user_id
   or token_id = :token_id
   or channel_id in (:channel_native_newapi, :channel_sub2api_http, :channel_synthetic_newapi, :channel_generic_openai);
delete from perf_metrics where "group" = :'test_group';
delete from channels
where id in (:channel_native_newapi, :channel_sub2api_http, :channel_synthetic_newapi, :channel_generic_openai);
insert into channels select * from ${BACKUP_CHANNELS};
delete from tokens where id = :token_id;
insert into tokens select * from ${BACKUP_TOKENS};
delete from users where id = :user_id;
insert into users select * from ${BACKUP_USERS};
delete from options where key in (
  'GroupRatio',
  'UserUsableGroups',
  'ModelRatio',
  'CompletionRatio',
  'CacheRatio',
  'CreateCacheRatio',
  'runtime.responses_synthetic_compact_instance_id'
);
insert into options select * from ${BACKUP_OPTIONS};
do \$\$
declare
  sequence_row record;
begin
  for sequence_row in select schemaname, sequencename, last_value, is_called from ${BACKUP_SEQUENCES} loop
    execute format('select setval(%L::regclass, %s, %L)',
      format('%I.%I', sequence_row.schemaname, sequence_row.sequencename),
      sequence_row.last_value,
      sequence_row.is_called);
  end loop;
end
\$\$;
do \$\$
begin
  if exists (
       select 1 from logs
       where user_id = current_setting('compact_e2e.user_id')::bigint
          or token_id = current_setting('compact_e2e.token_id')::bigint
          or channel_id in (
               current_setting('compact_e2e.channel_native_newapi')::bigint,
               current_setting('compact_e2e.channel_sub2api_http')::bigint,
               current_setting('compact_e2e.channel_synthetic_newapi')::bigint,
               current_setting('compact_e2e.channel_generic_openai')::bigint
             )
     )
    or exists (
       select 1 from quota_data
       where user_id = current_setting('compact_e2e.user_id')::bigint
     )
    or exists (
       select 1 from synthetic_compact_state_records
       where user_id = current_setting('compact_e2e.user_id')::bigint
          or token_id = current_setting('compact_e2e.token_id')::bigint
          or channel_id in (
               current_setting('compact_e2e.channel_native_newapi')::bigint,
               current_setting('compact_e2e.channel_sub2api_http')::bigint,
               current_setting('compact_e2e.channel_synthetic_newapi')::bigint,
               current_setting('compact_e2e.channel_generic_openai')::bigint
             )
     )
    or exists (
       select 1 from perf_metrics
       where "group" = current_setting('compact_e2e.test_group')
     ) then
    raise exception 'compact e2e generated state cleanup mismatch';
  end if;
end
\$\$;
drop table if exists ${BACKUP_CHANNELS};
drop table if exists ${BACKUP_ABILITIES};
drop table if exists ${BACKUP_TOKENS};
drop table if exists ${BACKUP_USERS};
drop table if exists ${BACKUP_OPTIONS};
drop table if exists ${BACKUP_SEQUENCES};
commit;
SQL
  then
    echo "cleanup: database state restored" >&2
    db_restored=1
  elif (( DB_SEEDED == 0 )); then
    echo "cleanup: database was not seeded; skipping restoration" >&2
  else
    echo "cleanup: database restoration failed" >&2
    cat "$cleanup_log" >&2
    rc=1
  fi
  if (( DB_SEEDED == 1 && app_quiesced == 1 && synthetic_state_capture_ok == 1 )); then
    if cleanup_redis_state; then
      echo "cleanup: isolated Redis test state removed" >&2
      redis_cleaned=1
    else
      echo "cleanup: isolated Redis test state cleanup failed" >&2
      rc=1
    fi
  fi
  if [[ -n "$FAKE_PID" ]]; then
    kill "$FAKE_PID" >/dev/null 2>&1 || true
  fi
  if (( DB_SEEDED == 1 )); then
    if (( app_quiesced == 1 && db_restored == 1 && redis_cleaned == 1 )); then
      restart_allowed=1
    else
      echo "cleanup: refusing to restart dev container because database or Redis restoration did not complete" >&2
    fi
  elif (( APP_STOPPED == 1 )); then
    restart_allowed=1
  fi
  if (( restart_allowed == 1 )); then
    local restart_log="$TMP_DIR/restart-dev.log"
    if restart_dev >"$restart_log" 2>&1; then
      echo "cleanup: dev container restarted" >&2
    else
      echo "cleanup: dev container restart failed" >&2
      cat "$restart_log" >&2
      rc=1
    fi
  else
    echo "cleanup: dev container restart skipped; leave the isolated app stopped for manual recovery" >&2
  fi
  if [[ "$original_rc" -eq 0 && "$rc" -eq 0 ]]; then
    if ! write_success_result; then
      echo "cleanup: failed to write E2E success result" >&2
      rc=1
    fi
  fi
  if [[ ("$original_rc" -eq 0 && "$rc" -eq 0) || -n "${COMPACT_E2E_CLEANUP_DELETE_LOGS:-}" ]]; then
    rm -rf "$TMP_DIR"
  else
    echo "cleanup: retained logs in $TMP_DIR" >&2
  fi
  if [[ "$original_rc" -ne 0 ]]; then
    release_db_lock
    return "$original_rc"
  fi
  release_db_lock
  return "$rc"
}

restart_dev() {
  local attempt action command_log
  for ((attempt = 1; attempt <= DEV_RECREATE_ATTEMPTS; attempt++)); do
    command_log="$TMP_DIR/dev-service-${DEV_REFRESH_COUNT}-${attempt}.log"
    if (( DEV_REFRESH_COUNT == 0 || attempt > 1 )); then
      action="force_recreate"
      if ! run_compose up -d --no-deps --force-recreate "$APP_SERVICE" >"$command_log" 2>&1; then
        echo "dev_service_action=$action attempt=$attempt status=failed" >&2
        cat "$command_log" >&2
        sleep "$DEV_RECREATE_RETRY_DELAY_SECONDS"
        continue
      fi
    else
      action="restart"
      if ! run_compose restart --timeout "$DEV_STOP_TIMEOUT_SECONDS" "$APP_SERVICE" >"$command_log" 2>&1; then
        echo "dev_service_action=$action attempt=$attempt status=failed" >&2
        cat "$command_log" >&2
        sleep "$DEV_RECREATE_RETRY_DELAY_SECONDS"
        continue
      fi
    fi

    APP_STOPPED=0
    echo "dev_service_action=$action attempt=$attempt status=started"
    for ((ready_attempt = 1; ready_attempt <= DEV_READY_ATTEMPTS; ready_attempt++)); do
      if data_plane_status_probe; then
        DEV_REFRESH_COUNT=$((DEV_REFRESH_COUNT + 1))
        APP_STOPPED=0
        record_host_bridge_probe
        return 0
      fi
      sleep 1
    done
    echo "dev_service_action=$action attempt=$attempt status=not_ready request_mode=$REQUEST_MODE" >&2
    sleep "$DEV_RECREATE_RETRY_DELAY_SECONDS"
  done
  echo "dev container did not become ready after $DEV_RECREATE_ATTEMPTS attempts request_mode=$REQUEST_MODE" >&2
  return 1
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
  COMPACT_E2E_REMOTE_COMPACTION_BODY_LIMIT_BYTES="$REMOTE_COMPACTION_BODY_LIMIT_BYTES" \
    COMPACT_E2E_LOCAL_COMPACTION_BODY_LIMIT_BYTES="$LOCAL_COMPACTION_BODY_LIMIT_BYTES" \
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
  case "$REQUEST_MODE" in
    host)
      curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$CURL_MAX_TIME" -sS -o "$out" -w '%{http_code}' -K "$CURL_CONFIG" --data "$payload" "$BASE_URL${path}"
      ;;
    docker-network)
      network_curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$CURL_MAX_TIME" -sS -o "$(network_tmp_path "$out")" -w '%{http_code}' -K /work/curl.conf --data "$payload" "$NETWORK_BASE_URL${path}"
      ;;
  esac
}

post_json_remote_compaction() {
  local path="$1"
  local payload="$2"
  local out="$3"
  case "$REQUEST_MODE" in
    host)
      curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$CURL_MAX_TIME" -sS -o "$out" -w '%{http_code}' -K "$CURL_CONFIG" -H 'X-Codex-Beta-Features: remote_compaction_v2' --data "$payload" "$BASE_URL${path}"
      ;;
    docker-network)
      network_curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$CURL_MAX_TIME" -sS -o "$(network_tmp_path "$out")" -w '%{http_code}' -K /work/curl.conf -H 'X-Codex-Beta-Features: remote_compaction_v2' --data "$payload" "$NETWORK_BASE_URL${path}"
      ;;
  esac
}

post_json_remote_compaction_file() {
  local path="$1"
  local payload_file="$2"
  local out="$3"
  case "$REQUEST_MODE" in
    host)
      curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$CURL_MAX_TIME" -sS -o "$out" -w '%{http_code}' -K "$CURL_CONFIG" -H 'X-Codex-Beta-Features: remote_compaction_v2' --data-binary "@${payload_file}" "$BASE_URL${path}"
      ;;
    docker-network)
      network_curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$CURL_MAX_TIME" -sS -o "$(network_tmp_path "$out")" -w '%{http_code}' -K /work/curl.conf -H 'X-Codex-Beta-Features: remote_compaction_v2' --data-binary "@$(network_tmp_path "$payload_file")" "$NETWORK_BASE_URL${path}"
      ;;
  esac
}

post_json_local_compaction_file() {
  local path="$1"
  local payload_file="$2"
  local out="$3"
  case "$REQUEST_MODE" in
    host)
      curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$CURL_MAX_TIME" -sS -o "$out" -w '%{http_code}' -K "$CURL_CONFIG" -H 'X-Codex-Turn-Metadata: {"request_kind":"compaction","compaction":{"implementation":"responses"}}' --data-binary "@${payload_file}" "$BASE_URL${path}"
      ;;
    docker-network)
      network_curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$CURL_MAX_TIME" -sS -o "$(network_tmp_path "$out")" -w '%{http_code}' -K /work/curl.conf -H 'X-Codex-Turn-Metadata: {"request_kind":"compaction","compaction":{"implementation":"responses"}}' --data-binary "@$(network_tmp_path "$payload_file")" "$NETWORK_BASE_URL${path}"
      ;;
  esac
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

write_success_result() {
  if [[ -z "$RESULT_FILE" ]]; then
    return
  fi
  umask 077
  printf 'e2e_done=true\ncases=%s\nrequest_mode=%s\nhost_bridge_probe_total=%d\nhost_bridge_probe_failures=%d\ncompleted_at=%s\n' \
    "$CASE_SELECTION" \
    "$REQUEST_MODE" \
    "$HOST_BRIDGE_PROBE_TOTAL" \
    "$HOST_BRIDGE_PROBE_FAILURES" \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >"$RESULT_FILE"
}

run_selected_case() {
  local case_name="$1"
  case "$case_name" in
    native_newapi_compact)
      run_case_native_newapi_compact
      ;;
    native_opaque_passthrough)
      run_case_native_opaque_passthrough
      ;;
    local_compaction_preemptive_body_reduction)
      run_case_local_compaction_preemptive_body_reduction
      ;;
    local_compaction_oversized_top_level_tools)
      run_case_local_compaction_oversized_top_level_tools
      ;;
    local_compaction_oversized_input_additional_tools)
      run_case_local_compaction_oversized_input_additional_tools
      ;;
    stream_options_include_usage_recovery)
      run_case_stream_options_include_usage_recovery
      ;;
    remote_compaction_413_recovery)
      run_case_remote_compaction_413_recovery
      ;;
    remote_compaction_embedded_media_413_recovery)
      run_case_remote_compaction_embedded_media_413_recovery
      ;;
    local_compaction_413_recovery)
      run_case_local_compaction_413_recovery
      ;;
    synthetic_newapi_compact_and_continue)
      run_case_synthetic_continue
      ;;
    stale_local_marker_visible_only)
      if [[ -z "${SYNTH_MARKER:-}" ]]; then
        echo "stale_local_marker_visible_only requires synthetic_newapi_compact_and_continue first" >&2
        return 1
      fi
      run_case_stale_visible_only
      ;;
    generic_openai_rejects_remote_opaque_compaction)
      run_case_generic_rejects_remote_opaque
      ;;
    sub2api_http_rejects_rest_previous_id)
      run_case_sub2api_previous_response_id
      ;;
    model_switch_synthetic_restore)
      if [[ -z "${SYNTH_MARKER:-}" ]]; then
        echo "model_switch_synthetic_restore requires synthetic_newapi_compact_and_continue first" >&2
        return 1
      fi
      run_case_model_switch_restore
      ;;
    real_codex_history_synthetic_restore)
      run_case_real_codex_history_fixture
      ;;
    *)
      echo "unknown COMPACT_E2E_CASES entry: ${case_name}" >&2
      return 1
      ;;
  esac
}

run_selected_cases() {
  local -a cases
  if [[ "$CASE_SELECTION" == "all" ]]; then
    cases=(
      native_newapi_compact
      native_opaque_passthrough
      local_compaction_preemptive_body_reduction
      local_compaction_oversized_top_level_tools
      local_compaction_oversized_input_additional_tools
      stream_options_include_usage_recovery
      remote_compaction_413_recovery
      remote_compaction_embedded_media_413_recovery
      local_compaction_413_recovery
      synthetic_newapi_compact_and_continue
      stale_local_marker_visible_only
      generic_openai_rejects_remote_opaque_compaction
      sub2api_http_rejects_rest_previous_id
      model_switch_synthetic_restore
      real_codex_history_synthetic_restore
    )
  else
    IFS=',' read -r -a cases <<< "$CASE_SELECTION"
  fi

  if [[ "${#cases[@]}" -eq 0 ]]; then
    echo "COMPACT_E2E_CASES must not be empty" >&2
    return 1
  fi
  local case_name
  for case_name in "${cases[@]}"; do
    case_name="${case_name//[[:space:]]/}"
    if [[ -z "$case_name" ]]; then
      echo "COMPACT_E2E_CASES contains an empty entry" >&2
      return 1
    fi
    run_selected_case "$case_name"
  done
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
  [[ "$summary" == *'"previousResponseID":{"present":true,"type":"string"}'* ]]
}

assert_single_upstream_capture_matches_payload() {
  local case_name="$1"
  local expected_path="$2"
  local payload="$3"
  local captures="$TMP_DIR/${case_name}_captures.json"
  capture_summary >"$captures"
  # shellcheck disable=SC2016
  node -e '
    const crypto = require("crypto");
    const fs = require("fs");
    const [captureFile, caseName, expectedPath, payload] = process.argv.slice(1);
    const captures = JSON.parse(fs.readFileSync(captureFile, "utf8"));
    if (captures.length !== 1) throw new Error(`${caseName}: expected one upstream call, got ${captures.length}`);
    const capture = captures[0];
    const expectedHash = crypto.createHash("sha256").update(payload).digest("hex");
    if (capture.request?.path !== expectedPath) throw new Error(`${caseName}: expected path ${expectedPath}, got ${capture.request?.path}`);
    if (capture.bodySHA256 !== expectedHash) throw new Error(`${caseName}: outbound body digest changed`);
    if (capture.contentLengthMatchesBody !== true) throw new Error(`${caseName}: outbound content length mismatch`);
    console.log(`${caseName}_upstream_calls=1 body_sha256_match=true path=${expectedPath}`);
  ' "$captures" "$case_name" "$expected_path" "$payload"
}

assert_no_upstream_capture() {
  local case_name="$1"
  local captures="$TMP_DIR/${case_name}_captures.json"
  capture_summary >"$captures"
  # shellcheck disable=SC2016
  node -e '
    const fs = require("fs");
    const [captureFile, caseName] = process.argv.slice(1);
    const captures = JSON.parse(fs.readFileSync(captureFile, "utf8"));
    if (captures.length !== 0) throw new Error(`${caseName}: expected zero upstream calls, got ${captures.length}`);
    console.log(`${caseName}_upstream_calls=0`);
  ' "$captures" "$case_name"
}

assert_native_opaque_client_response() {
  local mode="$1"
  local response_file="$2"
  # shellcheck disable=SC2016
  node -e '
    const fs = require("fs");
    const [mode, responseFile] = process.argv.slice(1);
    const raw = fs.readFileSync(responseFile, "utf8");
    const findReasoning = (output) => Array.isArray(output)
      ? output.find((item) => item?.type === "reasoning" || item?.type === "compaction")
      : undefined;
    const assertOpaque = (item, expected, version) => {
      if (item?.encrypted_content !== expected) throw new Error(`${mode}: encrypted_content changed or missing`);
      if (item?.future_state?.version !== version) throw new Error(`${mode}: future output state changed or missing`);
    };

    if (mode === "nonstream") {
      const response = JSON.parse(raw);
      assertOpaque(findReasoning(response.output), "OPAQUE_RESPONSE_NONSTREAM_E2E", 5);
      if (response.future_response_state?.preserved !== true) throw new Error(`${mode}: future response state changed or missing`);
    } else if (mode === "stream") {
      const events = raw
        .split(/\r?\n/)
        .filter((line) => line.startsWith("data: "))
        .map((line) => line.slice(6))
        .filter((data) => data !== "[DONE]")
        .map((data) => JSON.parse(data));
      const itemDone = events.find((event) => event.type === "response.output_item.done");
      const completed = events.find((event) => event.type === "response.completed");
      assertOpaque(itemDone?.item, "OPAQUE_RESPONSE_STREAM_E2E", 6);
      assertOpaque(findReasoning(completed?.response?.output), "OPAQUE_RESPONSE_STREAM_E2E", 6);
      if (itemDone?.future_event_state?.preserved !== true || completed?.future_event_state?.preserved !== true) {
        throw new Error(`${mode}: future event state changed or missing`);
      }
      if (completed?.response?.future_response_state?.preserved !== true) {
        throw new Error(`${mode}: future response state changed or missing`);
      }
    } else if (mode === "compact") {
      const response = JSON.parse(raw);
      assertOpaque(findReasoning(response.output), "OPAQUE_RESPONSE_COMPACT_E2E", 7);
      if (response.compaction?.encrypted_content !== "OPAQUE_RESPONSE_COMPACT_E2E") {
        throw new Error(`${mode}: top-level compaction state changed or missing`);
      }
      if (response.future_response_state?.preserved !== true) throw new Error(`${mode}: future response state changed or missing`);
    } else {
      throw new Error(`unsupported opaque response mode: ${mode}`);
    }
    console.log(`native_opaque_${mode}_client_encrypted_content_preserved=true`);
  ' "$mode" "$response_file"
}

run_case_native_opaque_passthrough() {
  echo "case=native_opaque_passthrough"
  set_single_channel "$CHANNEL_NATIVE_NEWAPI" 'gpt-5.6-sol'
  local payload out code

  reset_upstream
  payload='{"model":"gpt-5.6-sol","input":[{"type":"context_compaction","id":"cmp_client_generated_id","encrypted_content":"OPAQUE_PASSTHROUGH_NONSTREAM_E2E","future_state":{"version":2}},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue without changing opaque state"}]}],"instructions":"keep-original-instructions","service_tier":"priority","stream":false,"future_top_level":{"enabled":true}}'
  out="$TMP_DIR/native_opaque_nonstream.json"
  code="$(post_json /v1/responses "$payload" "$out")"
  echo "nonstream_status=${code}"
  [[ "$code" == "200" ]]
  assert_single_upstream_capture_matches_payload native_opaque_nonstream /v1/responses "$payload"
  assert_native_opaque_client_response nonstream "$out"

  reset_upstream
  payload='{"model":"gpt-5.6-sol","input":[{"type":"reasoning","id":"rs_client_generated_id","encrypted_content":{"marker":"OPAQUE_PASSTHROUGH_STREAM_E2E"},"future_state":{"version":3}},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue streaming without changing opaque state"}]}],"instructions":"keep-stream-instructions","stream":true,"stream_options":{"include_usage":true,"reasoning_summary_delivery":"detailed","future_stream_option":{"enabled":true}},"future_top_level":{"enabled":true}}'
  out="$TMP_DIR/native_opaque_stream.txt"
  code="$(post_json /v1/responses "$payload" "$out")"
  echo "stream_status=${code}"
  [[ "$code" == "200" ]]
  assert_single_upstream_capture_matches_payload native_opaque_stream /v1/responses "$payload"
  assert_native_opaque_client_response stream "$out"

  reset_upstream
  payload='{"model":"gpt-5.6-sol","input":[{"type":"reasoning","id":"rs_client_generated_id","encrypted_content":"OPAQUE_PASSTHROUGH_COMPACT_E2E","future_state":{"version":4}},{"type":"message","role":"user","content":"compact without changing opaque state"}],"reasoning":{"effort":"high","future_option":{"enabled":true}},"future_compact_option":{"mode":"native"}}'
  out="$TMP_DIR/native_opaque_compact.json"
  code="$(post_json /v1/responses/compact "$payload" "$out")"
  echo "compact_status=${code}"
  [[ "$code" == "200" ]]
  assert_single_upstream_capture_matches_payload native_opaque_compact /v1/responses/compact "$payload"
  assert_native_opaque_client_response compact "$out"
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

run_case_remote_compaction_embedded_media_413_recovery() {
  echo "case=remote_compaction_embedded_media_413_recovery"
  set_single_channel "$CHANNEL_NATIVE_NEWAPI" 'gpt-5.5'
  reset_upstream

  local payload_file out code captures request_body_limit_present
  if (( REMOTE_COMPACTION_MEDIA_BYTES <= REMOTE_COMPACTION_BODY_LIMIT_BYTES )); then
    echo "COMPACT_E2E_REMOTE_COMPACTION_MEDIA_BYTES must exceed COMPACT_E2E_REMOTE_COMPACTION_BODY_LIMIT_BYTES" >&2
    return 1
  fi
  payload_file="$TMP_DIR/remote_compaction_embedded_media_413.json"
  # shellcheck disable=SC2016 # JavaScript template literals must stay literal.
  node -e '
    const mediaBytes = Number.parseInt(process.argv[1], 10);
    // Keep a valid PNG header so new-api runs its normal image token counter.
    // PNG decoders accept trailing bytes, which lets this fixture exercise body-size limits.
    const tinyPng = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M/wHwAF/gL+5E9qAAAAAElFTkSuQmCC", "base64");
    const imageBytes = Buffer.concat([tinyPng, Buffer.alloc(mediaBytes)]);
    const image = `data:image/png;base64,${imageBytes.toString("base64")}`;
    console.log(JSON.stringify({
      model: "gpt-5.5",
      stream: true,
      input: [
        { type: "message", role: "user", content: [{ type: "input_text", text: "REMOTE_COMPACTION_MEDIA_413_E2E" }] },
        {
          type: "custom_tool_call_output",
          call_id: "call_e2e_image",
          output: [{ type: "input_image", image_url: image }]
        },
        { type: "compaction_trigger" }
      ]
    }));
  ' "$REMOTE_COMPACTION_MEDIA_BYTES" >"$payload_file"
  out="$TMP_DIR/remote_compaction_embedded_media_413.sse"
  code="$(post_json_remote_compaction_file /v1/responses "$payload_file" "$out")"
  echo "status=${code}"
  [[ "$code" == "200" ]]
  assert_contains "$out" 'compaction'
  assert_not_contains "$out" 'data:image/'

  captures="$TMP_DIR/remote_compaction_embedded_media_413_captures.json"
  curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$UPSTREAM_CURL_MAX_TIME" -fsS "$UPSTREAM_HOST_URL/_captures" >"$captures"
  node -e '
    const fs = require("fs");
    const captures = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
    const limit = Number.parseInt(process.argv[2], 10);
    if (captures.length !== 2) process.exit(2);
    const [first, second] = captures;
    if (!first.hasCompactionTrigger || !first.hasEmbeddedMediaDataURI || first.bodyBytes <= limit) process.exit(3);
    if (second.hasCompactionTrigger || second.hasEmbeddedMediaDataURI || second.bodyBytes > limit) process.exit(4);
    console.log("embedded_media_413_recovered_with_bounded_synthetic_compact=true");
  ' "$captures" "$REMOTE_COMPACTION_BODY_LIMIT_BYTES"

  request_body_limit_present="$(psql_query <<< "select coalesce(settings::jsonb ? 'request_body_limit', false) from channels where id = ${CHANNEL_NATIVE_NEWAPI}")"
  [[ "$request_body_limit_present" == "f" ]]
  echo "embedded_media_remote_413_did_not_set_request_body_limit=true"
}

run_case_local_compaction_413_recovery() {
  echo "case=local_compaction_413_recovery"
  set_single_channel "$CHANNEL_NATIVE_NEWAPI" 'gpt-5.5'
  reset_upstream

  local payload_file out code captures
  if (( REMOTE_COMPACTION_MEDIA_BYTES <= LOCAL_COMPACTION_BODY_LIMIT_BYTES )); then
    echo "COMPACT_E2E_REMOTE_COMPACTION_MEDIA_BYTES must exceed COMPACT_E2E_LOCAL_COMPACTION_BODY_LIMIT_BYTES" >&2
    return 1
  fi
  payload_file="$TMP_DIR/local_compaction_413.json"
  # shellcheck disable=SC2016 # JavaScript template literals must stay literal.
  node -e '
    const mediaBytes = Number.parseInt(process.argv[1], 10);
    const tinyPng = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M/wHwAF/gL+5E9qAAAAAElFTkSuQmCC", "base64");
    const imageBytes = Buffer.concat([tinyPng, Buffer.alloc(mediaBytes)]);
    const image = `data:image/png;base64,${imageBytes.toString("base64")}`;
    console.log(JSON.stringify({
      model: "gpt-5.5",
      stream: true,
      input: [
        { type: "message", role: "user", content: [{ type: "input_text", text: "LOCAL_COMPACTION_413_E2E latest task context must survive" }] },
        {
          type: "custom_tool_call_output",
          call_id: "call_e2e_local_image",
          output: [{ type: "input_image", image_url: image }]
        }
      ]
    }));
  ' "$REMOTE_COMPACTION_MEDIA_BYTES" >"$payload_file"
  out="$TMP_DIR/local_compaction_413.sse"
  code="$(post_json_local_compaction_file /v1/responses "$payload_file" "$out")"
  echo "status=${code}"
  [[ "$code" == "200" ]]
  assert_contains "$out" 'local_compaction_413_ok'
  assert_not_contains "$out" 'upstream request too large'

  captures="$TMP_DIR/local_compaction_413_captures.json"
  curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$UPSTREAM_CURL_MAX_TIME" -fsS "$UPSTREAM_HOST_URL/_captures" >"$captures"
  node -e '
    const fs = require("fs");
    const captures = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
    const limit = Number.parseInt(process.argv[2], 10);
    if (captures.length !== 2) process.exit(2);
    const [first, second] = captures;
    if (!first.hasLocalCompaction413Marker || first.bodyBytes <= limit || !first.hasEmbeddedMediaDataURI) process.exit(3);
    if (!second.hasLocalCompaction413Marker || second.hasEmbeddedMediaDataURI || second.bodyBytes > limit) process.exit(4);
    if (second.inputItemCount !== 1 || second.inputTypes.length !== 1 || second.inputTypes[0] !== "message") process.exit(5);
    console.log("local_compaction_413_recovered_with_bounded_request=true");
  ' "$captures" "$LOCAL_COMPACTION_BODY_LIMIT_BYTES"
}

run_case_local_compaction_preemptive_body_reduction() {
  echo "case=local_compaction_preemptive_body_reduction"
  set_single_channel "$CHANNEL_NATIVE_NEWAPI" 'gpt-5.5'
  reset_upstream

  local payload_file out code captures
  if [[ ! "$LOCAL_COMPACTION_PREEMPTIVE_IMAGE_COUNT" =~ ^[0-9]{1,3}$ ]] || (( LOCAL_COMPACTION_PREEMPTIVE_IMAGE_COUNT < 2 || LOCAL_COMPACTION_PREEMPTIVE_IMAGE_COUNT > 512 )); then
    echo "COMPACT_E2E_LOCAL_COMPACTION_PREEMPTIVE_IMAGE_COUNT must be at least 2" >&2
    return 1
  fi
  if [[ ! "$LOCAL_COMPACTION_PREEMPTIVE_IMAGE_BYTES" =~ ^[0-9]{1,8}$ ]] || (( LOCAL_COMPACTION_PREEMPTIVE_IMAGE_BYTES <= 0 || LOCAL_COMPACTION_PREEMPTIVE_IMAGE_BYTES > 32 * 1024 * 1024 )); then
    echo "COMPACT_E2E_LOCAL_COMPACTION_PREEMPTIVE_IMAGE_BYTES must be in (0, 32 MiB]" >&2
    return 1
  fi
  if (( LOCAL_COMPACTION_PREEMPTIVE_IMAGE_COUNT * LOCAL_COMPACTION_PREEMPTIVE_IMAGE_BYTES <= 256 * 1024 * 1024 )); then
    echo "preemptive fixture must exceed 256 MiB in total" >&2
    return 1
  fi
  if (( LOCAL_COMPACTION_PREEMPTIVE_IMAGE_COUNT * LOCAL_COMPACTION_PREEMPTIVE_IMAGE_BYTES > 512 * 1024 * 1024 )); then
    echo "preemptive fixture must not exceed 512 MiB in total" >&2
    return 1
  fi
  payload_file="$TMP_DIR/local_compaction_preemptive.json"
  # shellcheck disable=SC2016 # JavaScript template literals must stay literal.
  node -e '
    const fs = require("fs");
    const imageCount = Number.parseInt(process.argv[1], 10);
    const imageDataBytes = Number.parseInt(process.argv[2], 10);
    const write = (value) => fs.writeSync(1, value);
    const writeRepeated = (value, count) => {
      const chunkSize = 1024 * 1024;
      for (let remaining = count; remaining > 0;) {
        const size = Math.min(remaining, chunkSize);
        write(value.repeat(size));
        remaining -= size;
      }
    };
    const prefix = JSON.stringify({
      model: "gpt-5.5",
      stream: true,
      reasoning: { effort: "high", mode: "summary", context: "all_turns" },
      stream_options: { include_usage: true, reasoning_summary_delivery: "detailed" },
      tools: [{ type: "function", name: "TOP_LEVEL_TOOL_MUST_NOT_REACH_UPSTREAM" }],
      additional_tools: [{ type: "custom", name: "TOP_LEVEL_ADDITIONAL_TOOL_MUST_NOT_REACH_UPSTREAM" }]
    });
    write(prefix.slice(0, -1));
    write(",\"input\":[");
    write(JSON.stringify({ type: "message", role: "user", content: [{ type: "input_text", text: "LOCAL_COMPACTION_PREEMPTIVE_E2E latest task context must survive" }] }));
    // The fixed prefix above ends at the first message item. Append each large
    // image item incrementally so Node never holds the complete payload.
    for (let index = 0; index < imageCount; index += 1) {
      write(",{");
      write(`"type":"custom_tool_call_output","call_id":${JSON.stringify(`call_e2e_preemptive_image_${index}`)},"output":[{"type":"input_image","image_url":"data:image/png;base64,`);
      writeRepeated("A", imageDataBytes);
      write("\"}]}");
    }
    write("]}");
    write("\n");
  ' "$LOCAL_COMPACTION_PREEMPTIVE_IMAGE_COUNT" "$LOCAL_COMPACTION_PREEMPTIVE_IMAGE_BYTES" >"$payload_file"
  out="$TMP_DIR/local_compaction_preemptive.sse"
  code="$(post_json_local_compaction_file /v1/responses "$payload_file" "$out")"
  echo "status=${code}"
  [[ "$code" == "200" ]]
  assert_contains "$out" 'local_compaction_413_ok'
  assert_not_contains "$out" 'upstream request too large'

  captures="$TMP_DIR/local_compaction_preemptive_captures.json"
  curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$UPSTREAM_CURL_MAX_TIME" -fsS "$UPSTREAM_HOST_URL/_captures" >"$captures"
  node -e '
    const fs = require("fs");
    const captures = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
    const safeLimit = 32 * 1024 * 1024;
    if (captures.length !== 1) process.exit(2);
    const [request] = captures;
    if (!request.hasLocalCompactionPreemptiveMarker || request.hasEmbeddedMediaDataURI || request.bodyBytes > safeLimit) process.exit(3);
    if (request.customToolOutputCount !== 0 || request.inputTypes.includes("additional_tools") || request.hasTopLevelTools || request.hasTopLevelAdditionalTools) process.exit(4);
    if (request.inputItemCount !== 1 || request.inputTypes.length !== 1 || request.inputTypes[0] !== "message") process.exit(5);
    if (!request.reasoning.hasExpectedContextAllTurns || !request.reasoning.hasExpectedModeSummary) process.exit(5);
    if (!request.hasStreamOptionsIncludeUsage || !request.hasReasoningSummaryDelivery) process.exit(6);
    console.log("local_compaction_preemptive_multi_image_reduction_sent_one_bounded_request=true");
  ' "$captures"
}

run_case_local_compaction_oversized_top_level_tools() {
  echo "case=local_compaction_oversized_top_level_tools"
  set_single_channel "$CHANNEL_NATIVE_NEWAPI" 'gpt-5.5'
  reset_upstream

  local payload_file out code captures
  if (( LOCAL_COMPACTION_PREEMPTIVE_TOOL_BYTES <= 32 * 1024 * 1024 )); then
    echo "COMPACT_E2E_LOCAL_COMPACTION_PREEMPTIVE_TOOL_BYTES must exceed 32 MiB" >&2
    return 1
  fi
  payload_file="$TMP_DIR/local_compaction_oversized_top_level_tools.json"
  # shellcheck disable=SC2016 # JavaScript template literals must stay literal.
  node -e '
    const toolBytes = Number.parseInt(process.argv[1], 10);
    console.log(JSON.stringify({
      model: "gpt-5.5",
      stream: true,
      tools: [{
        type: "function",
        name: "OVERSIZED_TOP_LEVEL_TOOL_MUST_NOT_REACH_UPSTREAM",
        description: "A".repeat(toolBytes)
      }],
      input: [{
        type: "message",
        role: "user",
        content: [{ type: "input_text", text: "LOCAL_COMPACTION_PREEMPTIVE_E2E oversized top level tools must compact" }]
      }]
    }));
  ' "$LOCAL_COMPACTION_PREEMPTIVE_TOOL_BYTES" >"$payload_file"
  out="$TMP_DIR/local_compaction_oversized_top_level_tools.sse"
  code="$(post_json_local_compaction_file /v1/responses "$payload_file" "$out")"
  echo "status=${code}"
  [[ "$code" == "200" ]]
  assert_contains "$out" 'local_compaction_413_ok'

  captures="$TMP_DIR/local_compaction_oversized_top_level_tools_captures.json"
  curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$UPSTREAM_CURL_MAX_TIME" -fsS "$UPSTREAM_HOST_URL/_captures" >"$captures"
  node -e '
    const fs = require("fs");
    const captures = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
    const safeLimit = 32 * 1024 * 1024;
    if (captures.length !== 1) process.exit(2);
    const [request] = captures;
    if (!request.hasLocalCompactionPreemptiveMarker || request.bodyBytes > safeLimit) process.exit(3);
    if (request.hasTopLevelTools || request.hasTopLevelAdditionalTools || request.inputTypes.includes("additional_tools")) process.exit(4);
    if (request.inputItemCount !== 1 || request.inputTypes.length !== 1 || request.inputTypes[0] !== "message") process.exit(5);
    console.log("oversized_top_level_tools_reduced_before_upstream=true");
  ' "$captures"
}

run_case_local_compaction_oversized_input_additional_tools() {
  echo "case=local_compaction_oversized_input_additional_tools"
  set_single_channel "$CHANNEL_NATIVE_NEWAPI" 'gpt-5.5'
  reset_upstream

  local payload_file out code captures
  if (( LOCAL_COMPACTION_PREEMPTIVE_TOOL_BYTES <= 32 * 1024 * 1024 )); then
    echo "COMPACT_E2E_LOCAL_COMPACTION_PREEMPTIVE_TOOL_BYTES must exceed 32 MiB" >&2
    return 1
  fi
  payload_file="$TMP_DIR/local_compaction_oversized_input_additional_tools.json"
  # shellcheck disable=SC2016 # Keep tools before type to prove the parser is field-order independent.
  node -e '
    const toolBytes = Number.parseInt(process.argv[1], 10);
    console.log(JSON.stringify({
      model: "gpt-5.5",
      stream: true,
      input: [
        {
          tools: [{
            type: "custom",
            name: "OVERSIZED_INPUT_ADDITIONAL_TOOL_MUST_NOT_REACH_UPSTREAM",
            description: "A".repeat(toolBytes)
          }],
          type: "additional_tools",
          role: "developer"
        },
        {
          type: "message",
          role: "user",
          content: [{ type: "input_text", text: "LOCAL_COMPACTION_PREEMPTIVE_E2E oversized input additional tools must compact" }]
        }
      ]
    }));
  ' "$LOCAL_COMPACTION_PREEMPTIVE_TOOL_BYTES" >"$payload_file"
  out="$TMP_DIR/local_compaction_oversized_input_additional_tools.sse"
  code="$(post_json_local_compaction_file /v1/responses "$payload_file" "$out")"
  echo "status=${code}"
  [[ "$code" == "200" ]]
  assert_contains "$out" 'local_compaction_413_ok'

  captures="$TMP_DIR/local_compaction_oversized_input_additional_tools_captures.json"
  curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$UPSTREAM_CURL_MAX_TIME" -fsS "$UPSTREAM_HOST_URL/_captures" >"$captures"
  node -e '
    const fs = require("fs");
    const captures = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
    const safeLimit = 32 * 1024 * 1024;
    if (captures.length !== 1) process.exit(2);
    const [request] = captures;
    if (!request.hasLocalCompactionPreemptiveMarker || request.bodyBytes > safeLimit) process.exit(3);
    if (request.hasTopLevelTools || request.hasTopLevelAdditionalTools || request.inputTypes.includes("additional_tools")) process.exit(4);
    if (request.inputItemCount !== 1 || request.inputTypes.length !== 1 || request.inputTypes[0] !== "message") process.exit(5);
    console.log("oversized_input_additional_tools_reduced_before_upstream=true");
  ' "$captures"
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
  assert_no_upstream_capture generic_openai_rejects_remote_opaque_compaction
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
  for cmd in docker curl node openssl; do
    command -v "$cmd" >/dev/null 2>&1 || { echo "missing required command: $cmd" >&2; exit 1; }
  done
  guard_dev_target
  trap release_db_lock EXIT
  acquire_db_lock
  allocate_fixture_ids
  ensure_fake_upstream
  trap cleanup EXIT
  if ! stop_dev_app; then
    echo "failed to stop isolated app before database seeding" >&2
    exit 1
  fi
  backup_and_seed_db
  DB_SEEDED=1
  local token
  token="$(psql_query <<< "select key from tokens where id = ${TOKEN_ID}")"
  if [[ -z "$token" ]]; then
    echo "missing compact e2e token" >&2
    exit 1
  fi
  prepare_curl_config "sk-${token}"
  restart_dev
  docker exec "$APP_CONTAINER" /new-api --build-info
  run_selected_cases
  echo "e2e_done=true"
}

main "$@"
