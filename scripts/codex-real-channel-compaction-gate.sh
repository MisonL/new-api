#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# Docker Desktop can accept an IPv4 loopback connection while delaying the
# response. Use the published IPv6 loopback endpoint for the host-side Codex
# CLI probe; data-plane requests remain on the isolated Docker network.
DEV_PORT="${DEV_NEW_API_PORT:-3001}"
HOST_BASE_URL="http://[::1]:${DEV_PORT}"
NETWORK_BASE_URL="http://new-api:3000"
ISOLATED_NETWORK="new-api-dev-isolated_dev-isolated-network"
NETWORK_CLIENT_IMAGE="node:24-alpine"
POSTGRES_CONTAINER="new-api-dev-isolated-postgres-1"
REDIS_CONTAINER="new-api-dev-isolated-redis-1"
APP_CONTAINER="new-api-dev-isolated-new-api-1"
PRODUCTION_POSTGRES_CONTAINER="${CODEX_REAL_GATE_PRODUCTION_POSTGRES_CONTAINER:-postgres}"
PRODUCTION_POSTGRES_USER="${CODEX_REAL_GATE_PRODUCTION_POSTGRES_USER:-root}"
PRODUCTION_POSTGRES_DB="${CODEX_REAL_GATE_PRODUCTION_POSTGRES_DB:-new-api}"
COMPOSE_FILE="${ROOT_DIR}/deploy/compose/dev-isolated.yml"
ENV_FILE="${ROOT_DIR}/deploy/env/dev-isolated.env"
MODEL="${CODEX_REAL_GATE_MODEL:-gpt-5.6-sol}"
EXCLUDED_CHANNEL_IDS="${CODEX_REAL_GATE_EXCLUDE_CHANNEL_IDS:-}"
TOOL_BYTES="$((33 * 1024 * 1024))"
BASELINE_TOOL_BYTES=1024
SAFE_BODY_BYTES="$((32 * 1024 * 1024))"
CHANNEL_IDS=()
STRUCTURE_PROBE_SHAPES=()
GATE_OPTION_KEYS_SQL="'global.pass_through_request_enabled','global.chat_completions_to_responses_policy','global.request_body_limit_policy','general_setting.responses_stream_bootstrap_recovery_enabled','general_setting.responses_stream_bootstrap_grace_period_seconds','general_setting.responses_stream_bootstrap_probe_interval_milliseconds','general_setting.responses_stream_bootstrap_ping_interval_seconds','general_setting.responses_stream_bootstrap_retryable_status_codes','general_setting.channel_health_enabled','general_setting.channel_health_cooldown_seconds','general_setting.channel_health_probe_lease_seconds','RequestHeaderPolicyDefaultMode','RequestHeaderPolicyAuxiliaryRequestsEnabled','NpmCLIVersionRecordedOptions','RetryTimes','UpstreamRateLimitRetryLimit','AutomaticRetryStatusCodes','AutomaticDisableChannelEnabled','ChannelDisableThreshold','AutomaticDisableStatusCodes','AutomaticDisableKeywords','ModelRatio','ModelPrice','CompletionRatio','CacheRatio','CreateCacheRatio'"
CLI_TIMEOUT_SECONDS="${CODEX_REAL_GATE_CLI_TIMEOUT_SECONDS:-360}"
COMPACTION_TIMEOUT_MS="${CODEX_REAL_GATE_COMPACTION_TIMEOUT_MS:-180000}"
READINESS_TIMEOUT_SECONDS="${CODEX_REAL_GATE_READINESS_TIMEOUT_SECONDS:-60}"
STATUS_TIMEOUT_SECONDS="${CODEX_REAL_GATE_STATUS_TIMEOUT_SECONDS:-90}"
FAKE_UPSTREAM_URL="${CODEX_REAL_GATE_FAKE_UPSTREAM_URL:-}"
FAKE_UPSTREAM_MODE=false
CHANNEL_IDS_SQL=""

RUN_SUFFIX="$(date -u +%Y%m%d%H%M%S)_$$_$(openssl rand -hex 4)"
RUN_ID="codex_real_gate_${RUN_SUFFIX}"
BACKUP_CHANNELS="${RUN_ID}_channels"
BACKUP_ABILITIES="${RUN_ID}_abilities"
BACKUP_USERS="${RUN_ID}_users"
BACKUP_TOKENS="${RUN_ID}_tokens"
BACKUP_OPTIONS="${RUN_ID}_opts"
BACKUP_TAG_POLICIES="${RUN_ID}_tagpol"
BACKUP_TAG_SCOPE="${RUN_ID}_tags"
PRODUCTION_SOURCE_TAGS="${RUN_ID}_prodtags"
BACKUP_HEADER_STATES="${RUN_ID}_hdrstate"
BACKUP_STATE="${RUN_ID}_state"
TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/newapi-codex-real-gate.XXXXXX")"
RESULTS_FILE="${TMP_DIR}/results.txt"
EXTERNAL_RESULTS_FILE="${CODEX_REAL_GATE_RESULT_FILE:-}"
TOKEN_KEY="cg$(openssl rand -hex 24)"
USERNAME="cg$(openssl rand -hex 8)"
GATE_USER_ID=""
GATE_TOKEN_ID=""
BACKUP_CHANNEL_COUNT=""
BACKUP_ABILITY_COUNT=""
BACKUP_OPTION_COUNT=""
BACKUP_TAG_POLICY_COUNT=""
BACKUP_HEADER_STATE_COUNT=""
PRODUCTION_ABILITY_COUNT=""
PRODUCTION_OPTION_COUNT=""
PRODUCTION_TAG_POLICY_COUNT=""
PRODUCTION_HEADER_STATE_COUNT=""
PRODUCTION_SOURCE_TAG_COUNT=""
PRODUCTION_SNAPSHOT_ID=""
PRODUCTION_SNAPSHOT_FIFO=""
PRODUCTION_SNAPSHOT_OUTPUT=""
PRODUCTION_SNAPSHOT_ERROR=""
PRODUCTION_SNAPSHOT_PSQL_PID=""
PRODUCTION_SNAPSHOT_FD_OPEN=false
PRODUCTION_SNAPSHOT_ACTIVE=false
SNAPSHOT_READY=false
SNAPSHOT_TABLES_CREATED=false
PREPARE_ONLY=false
STRUCTURE_PROBE=false
LIST_PRODUCTION_CANDIDATES=false
ISOLATED_APP_STOPPED=false
ISOLATED_APP_RESTARTED=false
ISOLATED_APP_IMAGE=""

umask 077
cd "$ROOT_DIR"

if [[ -n "$EXTERNAL_RESULTS_FILE" ]]; then
  case "$EXTERNAL_RESULTS_FILE" in
    /tmp/*)
      : >"$EXTERNAL_RESULTS_FILE"
      chmod 600 "$EXTERNAL_RESULTS_FILE"
      ;;
    *)
      echo "CODEX_REAL_GATE_RESULT_FILE must be under /tmp" >&2
      exit 2
      ;;
  esac
fi

require_positive_integer() {
  local value="$1"
  local name="$2"
  if [[ ! "$value" =~ ^[0-9]+$ ]] || (( value <= 0 )); then
    echo "invalid ${name}" >&2
    exit 2
  fi
}

validate_model() {
  if [[ ! "$MODEL" =~ ^[A-Za-z0-9][A-Za-z0-9._:-]*$ ]]; then
    echo "invalid CODEX_REAL_GATE_MODEL" >&2
    exit 2
  fi
}

validate_dev_port() {
  require_positive_integer "$DEV_PORT" "DEV_NEW_API_PORT"
  if (( DEV_PORT > 65535 )); then
    echo "DEV_NEW_API_PORT must be at most 65535" >&2
    exit 2
  fi
}

validate_excluded_channel_ids() {
  if [[ -n "$EXCLUDED_CHANNEL_IDS" && ! "$EXCLUDED_CHANNEL_IDS" =~ ^[1-9][0-9]*(,[1-9][0-9]*)*$ ]]; then
    echo "invalid CODEX_REAL_GATE_EXCLUDE_CHANNEL_IDS" >&2
    exit 2
  fi
}

channel_is_explicitly_excluded() {
  local channel="$1"
  local excluded
  local excluded_ids=()
  if [[ -z "$EXCLUDED_CHANNEL_IDS" ]]; then
    return 1
  fi
  IFS=',' read -r -a excluded_ids <<< "$EXCLUDED_CHANNEL_IDS"
  for excluded in "${excluded_ids[@]}"; do
    if [[ "$channel" == "$excluded" ]]; then
      return 0
    fi
  done
  return 1
}

network_api_request() {
  local path="$1"
  local timeout_seconds="$2"
  docker run --rm \
    --network "$ISOLATED_NETWORK" \
    --read-only \
    --tmpfs /tmp:rw,noexec,nosuid,size=16m \
    --mount "type=bind,src=${ROOT_DIR},dst=/workspace,readonly" \
    --workdir /workspace \
    "$NETWORK_CLIENT_IMAGE" \
    node -e '
const [url, timeoutSeconds] = process.argv.slice(1);
const deadline = Date.now() + Number(timeoutSeconds) * 1000;
let lastFailure = "request_not_started";
while (Date.now() < deadline) {
  try {
    const response = await fetch(url, { signal: AbortSignal.timeout(5000) });
    await response.arrayBuffer();
    if (response.ok) process.exit(0);
    lastFailure = `http_${response.status}`;
  } catch (error) {
    lastFailure = error?.name || "request_failed";
  }
  await new Promise((resolve) => setTimeout(resolve, 1000));
}
process.stderr.write(`network_api_request_failed=${lastFailure}\n`);
process.exit(1);
' "${NETWORK_BASE_URL}${path}" "$timeout_seconds"
}

isolated_app_liveness() {
  [[ "$(docker inspect -f '{{.State.Running}}' "$APP_CONTAINER")" == "true" ]] &&
    network_api_request /api/healthz 6
}

verify_isolated_app_status() {
  network_api_request /api/status "$STATUS_TIMEOUT_SECONDS"
}

wait_for_isolated_app_liveness() {
  local elapsed=0
  while (( elapsed < READINESS_TIMEOUT_SECONDS )); do
    if isolated_app_liveness; then
      return 0
    fi
    sleep 1
    ((elapsed += 1))
  done
  isolated_app_liveness
}

production_psql_readonly() {
  docker exec -i \
    -e "PGOPTIONS=-c default_transaction_read_only=on" \
    "$PRODUCTION_POSTGRES_CONTAINER" \
    psql -X -q -v ON_ERROR_STOP=1 -U "$PRODUCTION_POSTGRES_USER" -d "$PRODUCTION_POSTGRES_DB" "$@"
}

terminate_snapshot_process() {
  local pid="$1"
  local attempt
  if [[ ! "$pid" =~ ^[0-9]+$ ]]; then
    return 0
  fi
  if ! kill -0 "$pid" >/dev/null 2>&1; then
    wait "$pid" >/dev/null 2>&1 || true
    return 0
  fi
  kill -TERM "$pid" >/dev/null 2>&1 || true
  for attempt in {1..20}; do
    if ! kill -0 "$pid" >/dev/null 2>&1; then
      wait "$pid" >/dev/null 2>&1 || true
      return 0
    fi
    sleep 0.1
  done
  kill -KILL "$pid" >/dev/null 2>&1 || true
  wait "$pid" >/dev/null 2>&1 || true
}

stop_production_snapshot() {
  if [[ "$PRODUCTION_SNAPSHOT_FD_OPEN" == "true" ]]; then
    printf '%s\n' 'rollback;' >&9 || true
    exec 9>&-
  fi
  terminate_snapshot_process "$PRODUCTION_SNAPSHOT_PSQL_PID"
  PRODUCTION_SNAPSHOT_ID=""
  PRODUCTION_SNAPSHOT_FIFO=""
  PRODUCTION_SNAPSHOT_OUTPUT=""
  PRODUCTION_SNAPSHOT_ERROR=""
  PRODUCTION_SNAPSHOT_PSQL_PID=""
  PRODUCTION_SNAPSHOT_FD_OPEN=false
  PRODUCTION_SNAPSHOT_ACTIVE=false
}

start_production_snapshot() {
  local snapshot_id=""
  local attempt
  if [[ "$PRODUCTION_SNAPSHOT_ACTIVE" == "true" ]]; then
    return 0
  fi
  PRODUCTION_SNAPSHOT_FIFO="${TMP_DIR}/production-snapshot.input"
  PRODUCTION_SNAPSHOT_OUTPUT="${TMP_DIR}/production-snapshot.output"
  PRODUCTION_SNAPSHOT_ERROR="${TMP_DIR}/production-snapshot.error"
  if ! mkfifo "$PRODUCTION_SNAPSHOT_FIFO"; then
    echo "failed to create production snapshot input pipe" >&2
    return 1
  fi
  docker exec -i \
    -e "PGOPTIONS=-c default_transaction_read_only=on" \
    "$PRODUCTION_POSTGRES_CONTAINER" \
    psql -X -qAt -v ON_ERROR_STOP=1 -U "$PRODUCTION_POSTGRES_USER" -d "$PRODUCTION_POSTGRES_DB" \
    <"$PRODUCTION_SNAPSHOT_FIFO" >"$PRODUCTION_SNAPSHOT_OUTPUT" 2>"$PRODUCTION_SNAPSHOT_ERROR" &
  PRODUCTION_SNAPSHOT_PSQL_PID="$!"
  if ! exec 9>"$PRODUCTION_SNAPSHOT_FIFO"; then
    terminate_snapshot_process "$PRODUCTION_SNAPSHOT_PSQL_PID"
    PRODUCTION_SNAPSHOT_PSQL_PID=""
    echo "failed to open production snapshot input pipe" >&2
    return 1
  fi
  PRODUCTION_SNAPSHOT_FD_OPEN=true
  if ! {
    printf '%s\n' 'begin transaction isolation level repeatable read read only;'
    printf '%s\n' 'select pg_export_snapshot();'
  } >&9; then
    stop_production_snapshot
    echo "failed to initialize production snapshot transaction" >&2
    return 1
  fi
  for attempt in {1..60}; do
    if [[ -s "$PRODUCTION_SNAPSHOT_OUTPUT" ]]; then
      snapshot_id="$(tr -d '\r\n' <"$PRODUCTION_SNAPSHOT_OUTPUT")"
      if [[ "$snapshot_id" =~ ^[0-9A-Fa-f-]+$ ]]; then
        PRODUCTION_SNAPSHOT_ID="$snapshot_id"
        PRODUCTION_SNAPSHOT_ACTIVE=true
        return 0
      fi
    fi
    if ! kill -0 "$PRODUCTION_SNAPSHOT_PSQL_PID" >/dev/null 2>&1; then
      break
    fi
    sleep 0.1
  done
  stop_production_snapshot
  echo "failed to establish a consistent production read-only snapshot" >&2
  return 1
}

production_psql_transaction() {
  local statement="$1"
  local transaction_prefix="begin transaction isolation level repeatable read read only;"
  shift
  if [[ "$PRODUCTION_SNAPSHOT_ACTIVE" == "true" ]]; then
    transaction_prefix="${transaction_prefix} set transaction snapshot '${PRODUCTION_SNAPSHOT_ID}';"
  fi
  production_psql_readonly "$@" -c "${transaction_prefix} ${statement}; commit;"
}

guard_production_postgres_source() {
  local running project service
  if [[ "$PRODUCTION_POSTGRES_CONTAINER" == "$POSTGRES_CONTAINER" ]]; then
    echo "production PostgreSQL container must not be the isolated PostgreSQL container" >&2
    exit 2
  fi
  running="$(docker inspect -f '{{.State.Running}}' "$PRODUCTION_POSTGRES_CONTAINER")"
  if [[ "$running" != "true" ]]; then
    echo "production PostgreSQL container is not running: ${PRODUCTION_POSTGRES_CONTAINER}" >&2
    exit 2
  fi
  project="$(docker inspect -f '{{ index .Config.Labels "com.docker.compose.project" }}' "$PRODUCTION_POSTGRES_CONTAINER")"
  if [[ "$project" != "new-api" ]]; then
    echo "production PostgreSQL Compose project must be new-api" >&2
    exit 2
  fi
  service="$(docker inspect -f '{{ index .Config.Labels "com.docker.compose.service" }}' "$PRODUCTION_POSTGRES_CONTAINER")"
  if [[ "$service" != "postgres" ]]; then
    echo "production PostgreSQL Compose service must be postgres" >&2
    exit 2
  fi
}

production_eligible_channel_ids() {
  production_psql_transaction "
select coalesce(string_agg(channel_id::text, ',' order by channel_id), '')
from (
  select distinct abilities.channel_id
  from abilities
  join channels on channels.id = abilities.channel_id
  where channels.status = 1
    and abilities.enabled = true
    and abilities.model = '${MODEL}'
) as eligible" -At
}

configure_gate_channels() {
  local discovered channel
  local selected_channels=()
  discovered="$(production_eligible_channel_ids)"
  if [[ ! "$discovered" =~ ^[1-9][0-9]*(,[1-9][0-9]*)*$ ]]; then
    echo "no production-enabled ${MODEL} channels were discovered" >&2
    exit 2
  fi
  IFS=',' read -r -a CHANNEL_IDS <<< "$discovered"
  for channel in "${CHANNEL_IDS[@]}"; do
    if [[ ! "$channel" =~ ^[1-9][0-9]*$ ]]; then
      echo "invalid production channel ID" >&2
      exit 2
    fi
    if ! channel_is_explicitly_excluded "$channel"; then
      selected_channels+=("$channel")
    fi
  done
  if (( ${#selected_channels[@]} == 0 )); then
    echo "all production-enabled ${MODEL} channels were explicitly excluded" >&2
    exit 2
  fi
  CHANNEL_IDS=("${selected_channels[@]}")
  CHANNEL_IDS_SQL="$(IFS=,; printf '%s' "${CHANNEL_IDS[*]}")"
}

configure_fake_upstream() {
  if [[ -z "$FAKE_UPSTREAM_URL" ]]; then
    return
  fi
  if [[ "${CODEX_REAL_GATE_ALLOW_FAKE_UPSTREAM:-}" != "1" ]]; then
    echo "CODEX_REAL_GATE_ALLOW_FAKE_UPSTREAM=1 is required for fake upstream observation" >&2
    exit 2
  fi
  if [[ ! "$FAKE_UPSTREAM_URL" =~ ^http://host\.docker\.internal:[0-9]{2,5}$ ]]; then
    echo "CODEX_REAL_GATE_FAKE_UPSTREAM_URL must be http://host.docker.internal:<port>" >&2
    exit 2
  fi
  FAKE_UPSTREAM_MODE=true
}

configure_structure_probe_shapes() {
  local requested="${CODEX_REAL_GATE_STRUCTURE_SHAPES:-baseline,reduced_current,reduced_no_instructions,reduced_empty_additional_tools,reduced_minimal_additional_tool,reduced_empty_top_level_tools,reduced_minimal_top_level_tool,reduced_no_reasoning_context,reduced_no_stream_options}"
  local shape
  IFS=',' read -r -a STRUCTURE_PROBE_SHAPES <<< "$requested"
  if (( ${#STRUCTURE_PROBE_SHAPES[@]} == 0 )); then
    echo "at least one structure probe shape is required" >&2
    exit 2
  fi
  for shape in "${STRUCTURE_PROBE_SHAPES[@]}"; do
    case "$shape" in
      baseline|reduced_current|reduced_no_instructions|reduced_empty_additional_tools|reduced_minimal_additional_tool|reduced_empty_top_level_tools|reduced_minimal_top_level_tool|reduced_no_reasoning_context|reduced_no_stream_options) ;;
      *)
        echo "unsupported structure probe shape: ${shape}" >&2
        exit 2
        ;;
    esac
  done
}

psql_exec() {
  docker exec -i "$POSTGRES_CONTAINER" \
    psql -X -q -v ON_ERROR_STOP=1 -U root -d new-api-dev "$@"
}

guard_isolated_redis_is_ephemeral() {
  local mounts
  mounts="$(docker inspect -f '{{json .Mounts}}' "$REDIS_CONTAINER")"
  if [[ "$mounts" != "[]" ]]; then
    echo "isolated Redis must not use persistent mounts during the channel gate" >&2
    return 1
  fi
}

schema_signature() {
  local database_role="$1"
  local table_name="$2"
  local query
  case "$table_name" in
    channels|abilities|options|tag_request_header_policies|request_header_strategy_states) ;;
    *)
      echo "unsupported snapshot schema table: ${table_name}" >&2
      return 1
      ;;
  esac
  query="select coalesce(string_agg(ordinal_position::text || ':' || column_name || ':' || data_type || ':' || udt_name || ':' || is_nullable, ',' order by ordinal_position), '') from information_schema.columns where table_schema = 'public' and table_name = '${table_name}'"
  if [[ "$database_role" == "production" ]]; then
    production_psql_transaction "$query" -At
    return
  fi
  psql_exec -v table_name="$table_name" -At <<SQL
${query}
SQL
}

verify_snapshot_schema() {
  local table_name source_signature target_signature
  for table_name in channels abilities options tag_request_header_policies request_header_strategy_states; do
    source_signature="$(schema_signature production "$table_name")"
    target_signature="$(schema_signature isolated "$table_name")"
    if [[ -z "$source_signature" || "$source_signature" != "$target_signature" ]]; then
      echo "production and isolated ${table_name} schemas are not compatible for binary COPY" >&2
      return 1
    fi
  done
}

production_ability_count() {
  production_psql_transaction "select count(*) from abilities where channel_id in (${CHANNEL_IDS_SQL})" -At
}

production_option_count() {
  production_psql_transaction "select count(*) from options where key in (${GATE_OPTION_KEYS_SQL})" -At
}

production_tag_policy_count() {
  production_psql_transaction "
select count(*)
from tag_request_header_policies
where tag in (
  select distinct coalesce(tag, '')
  from channels
  where id in (${CHANNEL_IDS_SQL})
    and coalesce(tag, '') <> ''
)" -At
}

production_header_state_count() {
  production_psql_transaction "
select count(*)
from request_header_strategy_states
where scope_key ~ ('^channel:(' || replace('${CHANNEL_IDS_SQL}', ',', '|') || ')(:|$)')
   or scope_key in (
     select 'tag:' || tag
     from (
       select distinct coalesce(tag, '') as tag
       from channels
       where id in (${CHANNEL_IDS_SQL})
        and coalesce(tag, '') <> ''
     ) as source_tags
   )" -At
}

production_source_tag_count() {
  production_psql_transaction "
select count(*)
from (
  select distinct coalesce(tag, '') as tag
  from channels
  where id in (${CHANNEL_IDS_SQL})
    and coalesce(tag, '') <> ''
) as source_tags" -At
}

copy_production_table_into_isolated() {
  local table_name="$1"
  local id_column
  case "$table_name" in
    channels) id_column="id" ;;
    abilities) id_column="channel_id" ;;
    *)
      echo "unsupported production snapshot table: ${table_name}" >&2
      return 1
      ;;
  esac
  production_psql_transaction "copy (select * from ${table_name} where ${id_column} in (${CHANNEL_IDS_SQL})) to stdout with (format binary)" \
    | psql_exec -c "copy ${table_name} from stdin with (format binary);" >/dev/null
}

copy_production_options_into_isolated() {
  production_psql_transaction "copy (select * from options where key in (${GATE_OPTION_KEYS_SQL})) to stdout with (format binary)" \
    | psql_exec -c "copy options from stdin with (format binary);" >/dev/null
}

copy_production_tag_policies_into_isolated() {
  production_psql_transaction "copy (
    select * from tag_request_header_policies
    where tag in (
      select distinct coalesce(tag, '') from channels
      where id in (${CHANNEL_IDS_SQL}) and coalesce(tag, '') <> ''
    )
  ) to stdout with (format binary)" \
    | psql_exec -c "copy tag_request_header_policies from stdin with (format binary);" >/dev/null
}

copy_production_header_states_into_isolated() {
  production_psql_transaction "copy (
    select * from request_header_strategy_states
    where scope_key ~ ('^channel:(' || replace('${CHANNEL_IDS_SQL}', ',', '|') || ')(:|$)')
       or scope_key in (
         select 'tag:' || tag
         from (
           select distinct coalesce(tag, '') as tag
           from channels
           where id in (${CHANNEL_IDS_SQL})
             and coalesce(tag, '') <> ''
         ) as source_tags
       )
  ) to stdout with (format binary)" \
    | psql_exec -c "copy request_header_strategy_states from stdin with (format binary);" >/dev/null
}

copy_production_source_tags_into_isolated() {
  production_psql_transaction "copy (
    select distinct coalesce(tag, '') as tag
    from channels
    where id in (${CHANNEL_IDS_SQL})
      and coalesce(tag, '') <> ''
  ) to stdout with (format binary)" \
    | psql_exec -c "copy ${PRODUCTION_SOURCE_TAGS} from stdin with (format binary);" >/dev/null
}

import_production_gate_channels() {
  local copied_channel_count copied_ability_count copied_eligible_count copied_option_count
  local copied_tag_policy_count copied_header_state_count copied_source_tag_count
  if ! copy_production_table_into_isolated channels; then
    echo "failed to copy production channels into the isolated database" >&2
    return 1
  fi
  if ! copy_production_table_into_isolated abilities; then
    echo "failed to copy production abilities into the isolated database" >&2
    return 1
  fi
  if ! copy_production_options_into_isolated; then
    echo "failed to copy production Responses gate options into the isolated database" >&2
    return 1
  fi
  if ! copy_production_tag_policies_into_isolated; then
    echo "failed to copy production tag request header policies into the isolated database" >&2
    return 1
  fi
  if ! copy_production_header_states_into_isolated; then
    echo "failed to copy production request header strategy states into the isolated database" >&2
    return 1
  fi
  copied_channel_count="$(psql_exec -Atc "select count(*) from channels where id in (${CHANNEL_IDS_SQL});")"
  copied_ability_count="$(psql_exec -Atc "select count(*) from abilities where channel_id in (${CHANNEL_IDS_SQL});")"
  copied_option_count="$(psql_exec -Atc "select count(*) from options where key in (${GATE_OPTION_KEYS_SQL});")"
  copied_tag_policy_count="$(psql_exec -Atc "select count(*) from tag_request_header_policies where tag in (select tag from ${PRODUCTION_SOURCE_TAGS});")"
  copied_header_state_count="$(psql_exec -Atc "select count(*) from request_header_strategy_states where scope_key ~ ('^channel:(' || replace('${CHANNEL_IDS_SQL}', ',', '|') || ')(:|$)') or scope_key in (select 'tag:' || tag from ${PRODUCTION_SOURCE_TAGS});")"
  copied_source_tag_count="$(psql_exec -Atc "select count(*) from ${PRODUCTION_SOURCE_TAGS};")"
  copied_eligible_count="$(psql_exec -v model="$MODEL" -At <<SQL
select count(distinct abilities.channel_id)
from abilities
join channels on channels.id = abilities.channel_id
where channels.id in (${CHANNEL_IDS_SQL})
  and channels.status = 1
  and abilities.enabled = true
  and abilities.model = :'model';
SQL
)"
  if [[ "$copied_channel_count" != "${#CHANNEL_IDS[@]}" ||
    "$copied_ability_count" != "$PRODUCTION_ABILITY_COUNT" ||
    "$copied_eligible_count" != "${#CHANNEL_IDS[@]}" ||
    "$copied_option_count" != "$PRODUCTION_OPTION_COUNT" ||
    "$copied_tag_policy_count" != "$PRODUCTION_TAG_POLICY_COUNT" ||
    "$copied_header_state_count" != "$PRODUCTION_HEADER_STATE_COUNT" ||
    "$copied_source_tag_count" != "$PRODUCTION_SOURCE_TAG_COUNT" ]]; then
    echo "production channel copy verification failed" >&2
    return 1
  fi
}

guard_isolated_target() {
  if [[ "$HOST_BASE_URL" != "http://[::1]:${DEV_PORT}" || "$NETWORK_BASE_URL" != "http://new-api:3000" ]]; then
    echo "refusing non-isolated base URL" >&2
    exit 2
  fi
  if ! docker network inspect "$ISOLATED_NETWORK" >/dev/null; then
    echo "isolated Docker network is unavailable: ${ISOLATED_NETWORK}" >&2
    exit 2
  fi
  if ! docker network inspect -f '{{range .Containers}}{{println .Name}}{{end}}' "$ISOLATED_NETWORK" | grep -Fqx "$APP_CONTAINER"; then
    echo "isolated app is not attached to expected Docker network" >&2
    exit 2
  fi
  for container in "$POSTGRES_CONTAINER" "$REDIS_CONTAINER" "$APP_CONTAINER"; do
    if [[ "$(docker inspect -f '{{.State.Running}}' "$container")" != "true" ]]; then
      echo "required isolated container is not running: ${container}" >&2
      exit 2
    fi
  done
  if [[ -n "${DEV_NEW_API_IMAGE:-}" ]]; then
    ISOLATED_APP_IMAGE="$DEV_NEW_API_IMAGE"
    if ! docker image inspect "$ISOLATED_APP_IMAGE" >/dev/null 2>&1; then
      echo "requested isolated image is unavailable: ${ISOLATED_APP_IMAGE}" >&2
      exit 2
    fi
  else
    ISOLATED_APP_IMAGE="$(docker inspect -f '{{.Config.Image}}' "$APP_CONTAINER")"
    if [[ -z "$ISOLATED_APP_IMAGE" || "$ISOLATED_APP_IMAGE" == "<no value>" ]]; then
      echo "could not determine the current isolated app image" >&2
      exit 2
    fi
  fi
  if ! guard_isolated_redis_is_ephemeral; then
    exit 2
  fi
  if ! wait_for_isolated_app_liveness; then
    echo "isolated API healthz check failed" >&2
    exit 2
  fi
  if ! verify_isolated_app_status; then
    echo "isolated API status check failed after healthz readiness" >&2
    exit 2
  fi
}

isolated_compose() {
  DEV_NEW_API_IMAGE="$ISOLATED_APP_IMAGE" DEV_NEW_API_PORT="$DEV_PORT" \
    docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" "$@"
}

reserve_gate_ids() {
  local candidate_user_id candidate_token_id existing
  for _ in {1..20}; do
    candidate_user_id="$((1000000000 + 16#$(openssl rand -hex 4) % 1000000000))"
    candidate_token_id="$((2000000000 + 16#$(openssl rand -hex 4) % 1000000000))"
    existing="$(psql_exec -Atc "select count(*) from users where id = ${candidate_user_id} union all select count(*) from tokens where id = ${candidate_token_id};")"
    if [[ "$existing" == $'0\n0' ]]; then
      GATE_USER_ID="$candidate_user_id"
      GATE_TOKEN_ID="$candidate_token_id"
      return
    fi
  done
  echo "could not reserve isolated gate IDs" >&2
  exit 2
}

prepare_gate_database() {
  local initial_setup_sql mutation_sql
  BACKUP_CHANNEL_COUNT="$(psql_exec -Atc "select count(*) from channels where id in (${CHANNEL_IDS_SQL});")"
  BACKUP_ABILITY_COUNT="$(psql_exec -Atc "select count(*) from abilities where channel_id in (${CHANNEL_IDS_SQL});")"
  BACKUP_OPTION_COUNT="$(psql_exec -Atc "select count(*) from options where key in (${GATE_OPTION_KEYS_SQL});")"
  PRODUCTION_ABILITY_COUNT="$(production_ability_count)"
  PRODUCTION_OPTION_COUNT="$(production_option_count)"
  PRODUCTION_TAG_POLICY_COUNT="$(production_tag_policy_count)"
  PRODUCTION_HEADER_STATE_COUNT="$(production_header_state_count)"
  PRODUCTION_SOURCE_TAG_COUNT="$(production_source_tag_count)"
  if [[ ! "$BACKUP_CHANNEL_COUNT" =~ ^[0-9]+$ ]] ||
    [[ ! "$BACKUP_ABILITY_COUNT" =~ ^[0-9]+$ ]] ||
    [[ ! "$BACKUP_OPTION_COUNT" =~ ^[0-9]+$ ]] ||
    [[ ! "$PRODUCTION_ABILITY_COUNT" =~ ^[1-9][0-9]*$ ]] ||
    [[ ! "$PRODUCTION_OPTION_COUNT" =~ ^[0-9]+$ ]] ||
    [[ ! "$PRODUCTION_TAG_POLICY_COUNT" =~ ^[0-9]+$ ]] ||
    [[ ! "$PRODUCTION_HEADER_STATE_COUNT" =~ ^[0-9]+$ ]] ||
    [[ ! "$PRODUCTION_SOURCE_TAG_COUNT" =~ ^[0-9]+$ ]]; then
    echo "invalid channel snapshot counts" >&2
    return 1
  fi
  initial_setup_sql="${TMP_DIR}/prepare-initial.sql"
  {
    printf "\\set gate_user_id %s\n" "$GATE_USER_ID"
    printf "\\set gate_token_id %s\n" "$GATE_TOKEN_ID"
    cat <<SQL
begin;
drop table if exists ${BACKUP_CHANNELS};
drop table if exists ${BACKUP_ABILITIES};
drop table if exists ${BACKUP_USERS};
drop table if exists ${BACKUP_TOKENS};
drop table if exists ${BACKUP_OPTIONS};
drop table if exists ${BACKUP_TAG_POLICIES};
drop table if exists ${BACKUP_TAG_SCOPE};
drop table if exists ${PRODUCTION_SOURCE_TAGS};
drop table if exists ${BACKUP_HEADER_STATES};
drop table if exists ${BACKUP_STATE};
create table ${BACKUP_CHANNELS} as select * from channels where id in (${CHANNEL_IDS_SQL});
create table ${BACKUP_ABILITIES} as select * from abilities where channel_id in (${CHANNEL_IDS_SQL});
create table ${BACKUP_USERS} as select * from users where id = :gate_user_id;
create table ${BACKUP_TOKENS} as select * from tokens where id = :gate_token_id;
create table ${BACKUP_OPTIONS} as select * from options where key in (${GATE_OPTION_KEYS_SQL});
create table ${BACKUP_TAG_SCOPE} (tag text primary key);
insert into ${BACKUP_TAG_SCOPE} (tag)
select distinct coalesce(tag, '')
from channels
where id in (${CHANNEL_IDS_SQL})
  and coalesce(tag, '') <> '';
create table ${PRODUCTION_SOURCE_TAGS} (tag text primary key);
commit;
SQL
  } >"$initial_setup_sql"
  psql_exec <"$initial_setup_sql" >/dev/null
  SNAPSHOT_TABLES_CREATED=true

  if ! copy_production_source_tags_into_isolated; then
    echo "failed to copy production channel tags into the isolated database" >&2
    return 1
  fi

  BACKUP_TAG_POLICY_COUNT="$(psql_exec -Atc "select count(*) from tag_request_header_policies where tag in (select tag from ${BACKUP_TAG_SCOPE} union select tag from ${PRODUCTION_SOURCE_TAGS});")"
  BACKUP_HEADER_STATE_COUNT="$(psql_exec -Atc "select count(*) from request_header_strategy_states where scope_key ~ ('^channel:(' || replace('${CHANNEL_IDS_SQL}', ',', '|') || ')(:|$)') or scope_key in (select 'tag:' || tag from (select tag from ${BACKUP_TAG_SCOPE} union select tag from ${PRODUCTION_SOURCE_TAGS}) as tag_scope);")"
  if [[ ! "$BACKUP_TAG_POLICY_COUNT" =~ ^[0-9]+$ ]] ||
    [[ ! "$BACKUP_HEADER_STATE_COUNT" =~ ^[0-9]+$ ]]; then
    echo "invalid isolated runtime policy backup counts" >&2
    return 1
  fi

  mutation_sql="${TMP_DIR}/prepare-mutation.sql"
  {
    printf "\\set gate_token_key '%s'\n" "$TOKEN_KEY"
    printf "\\set gate_username '%s'\n" "$USERNAME"
    printf "\\set gate_user_id %s\n" "$GATE_USER_ID"
    printf "\\set gate_token_id %s\n" "$GATE_TOKEN_ID"
    cat <<SQL
begin;
insert into ${BACKUP_TAG_SCOPE} (tag)
select tag from ${PRODUCTION_SOURCE_TAGS}
on conflict do nothing;
create table ${BACKUP_TAG_POLICIES} as
select * from tag_request_header_policies where tag in (select tag from ${BACKUP_TAG_SCOPE});
create table ${BACKUP_HEADER_STATES} as
select * from request_header_strategy_states
where scope_key ~ ('^channel:(' || replace('${CHANNEL_IDS_SQL}', ',', '|') || ')(:|$)')
   or scope_key in (select 'tag:' || tag from ${BACKUP_TAG_SCOPE});
create table ${BACKUP_STATE} (snapshot_ready boolean not null);
delete from abilities where channel_id in (${CHANNEL_IDS_SQL});
delete from channels where id in (${CHANNEL_IDS_SQL});
delete from options where key in (${GATE_OPTION_KEYS_SQL});
delete from tag_request_header_policies where tag in (select tag from ${BACKUP_TAG_SCOPE});
delete from request_header_strategy_states
where scope_key ~ ('^channel:(' || replace('${CHANNEL_IDS_SQL}', ',', '|') || ')(:|$)')
   or scope_key in (select 'tag:' || tag from ${BACKUP_TAG_SCOPE});
insert into users (
  id, username, password, display_name, role, status, email, quota, used_quota,
  request_count, "group", aff_count, aff_quota, aff_history, inviter_id, setting,
  remark, created_at, last_login_at
) values (
  :gate_user_id, :'gate_username', 'temporary-codex-gate-password', 'codex-gate',
  100, 1, :'gate_username' || '@example.invalid', 1000000000, 0, 0, 'default',
  0, 0, 0, 0, '', 'temporary isolated Codex gate user', extract(epoch from now())::bigint, 0
);
insert into tokens (
  id, user_id, key, status, name, created_time, accessed_time, expired_time,
  remain_quota, unlimited_quota, model_limits_enabled, model_limits, allow_ips,
  used_quota, "group", cross_group_retry
) values (
  :gate_token_id, :gate_user_id, :'gate_token_key', 1, 'codex-real-channel-gate',
  extract(epoch from now())::bigint, 0, -1, 0, true, false, '', '', 0, '', false
);
insert into ${BACKUP_STATE} values (true);
commit;
SQL
  } >"$mutation_sql"
  psql_exec <"$mutation_sql" >/dev/null
  SNAPSHOT_READY=true
}

configure_fake_upstream_for_gate_channels() {
  if [[ "$FAKE_UPSTREAM_MODE" != "true" ]]; then
    return
  fi
  psql_exec -v fake_upstream_url="$FAKE_UPSTREAM_URL" >"${TMP_DIR}/configure-fake-upstream.log" <<SQL
begin;
update channels
set base_url = :'fake_upstream_url'
where id in (${CHANNEL_IDS_SQL});
commit;
SQL
}

wait_for_child() {
  local pid="$1"
  local timeout_seconds="$2"
  local elapsed=0
  while kill -0 "$pid" 2>/dev/null; do
    if (( elapsed >= timeout_seconds )); then
      kill -TERM "$pid" >/dev/null 2>&1 || true
      sleep 2
      kill -KILL "$pid" >/dev/null 2>&1 || true
      wait "$pid" >/dev/null 2>&1 || true
      return 124
    fi
    sleep 1
    ((elapsed += 1))
  done
  if wait "$pid"; then
    return 0
  fi
  return $?
}

record_result() {
  local line="$1"
  printf '%s\n' "$line" >>"$RESULTS_FILE"
  if [[ -n "$EXTERNAL_RESULTS_FILE" ]]; then
    printf '%s\n' "$line" >>"$EXTERNAL_RESULTS_FILE"
  fi
  printf '%s\n' "$line"
}

read_cli_result_field() {
  local field="$1"
  local file="$2"
  if [[ ! -f "$file" ]]; then
    return 0
  fi
  awk -v field="$field" '
    {
      for (field_index = 1; field_index <= NF; field_index++) {
        split($field_index, pair, "=")
        if (pair[1] == field) {
          print substr($field_index, length(field) + 2)
          exit
        }
      }
    }
  ' "$file"
}

run_cli_smoke() {
  local channel="$1"
  local result_file="${TMP_DIR}/cli-${channel}.result"
  local log_file="${TMP_DIR}/cli-${channel}.log"
  local pid rc before_log_id cli_stage cli_exit cli_log_metadata=""
  local cli_log_type cli_log_error_code cli_log_status cli_log_skip observed_cli_log=false

  before_log_id="$(latest_log_id)"
  NEWAPI_SMOKE_TOKEN="$TOKEN_KEY" \
  NEWAPI_BASE_URL="$HOST_BASE_URL" \
  NEWAPI_SMOKE_MODEL="$MODEL" \
  NEWAPI_CLI_SMOKE_RESULT_FILE="$result_file" \
    sh scripts/codex-cli-isolated-smoke.sh "$channel" >"$log_file" 2>&1 &
  pid=$!
  if wait_for_child "$pid" "$CLI_TIMEOUT_SECONDS"; then
    rc=0
  else
    rc=$?
  fi

  if (( rc == 0 )) && [[ -f "$result_file" ]] &&
    grep -Fqx "status=passed channel=${channel} model=${MODEL}" "$result_file"; then
    record_result "phase=cli channel=${channel} status=passed"
    return 0
  fi

  cli_stage="$(read_cli_result_field stage "$result_file")"
  cli_exit="$(read_cli_result_field exit "$result_file")"
  if cli_log_metadata="$(wait_for_cli_error_log_metadata "$channel" "$before_log_id")"; then
    observed_cli_log=true
  fi
  IFS='|' read -r cli_log_type cli_log_error_code cli_log_status cli_log_skip <<< "$cli_log_metadata"
  record_result "phase=cli channel=${channel} status=failed failure_class=$([[ "$rc" == "124" ]] && printf timeout || printf cli_failed) cli_stage=$(safe_observation_value "$cli_stage") cli_exit=$(safe_observation_value "$cli_exit") cli_log_observed=${observed_cli_log} cli_log_type=$(safe_observation_value "$cli_log_type") cli_log_status=$(safe_observation_value "$cli_log_status") cli_log_error_code=$(safe_observation_value "$cli_log_error_code") cli_log_skip=$(safe_observation_value "$cli_log_skip")"
  return 1
}

json_field() {
  local expression="$1"
  local file="$2"
  jq -r "$expression // \"null\"" "$file"
}

latest_log_id() {
  psql_exec -Atc 'select coalesce(max(id), 0) from logs;'
}

read_cli_error_log_metadata() {
  local channel="$1"
  local minimum_id="$2"
  psql_exec -AtF '|' -v channel_id="$channel" -v token_id="$GATE_TOKEN_ID" -v minimum_id="$minimum_id" <<'SQL'
select
  type,
  coalesce(other::jsonb ->> 'error_code', ''),
  coalesce(other::jsonb ->> 'status_code', ''),
  coalesce(other::jsonb #>> '{responses_routing_channel_skips,0,reason}', '')
from logs
where channel_id = :channel_id
  and token_id = :token_id
  and id > :minimum_id
  and type = 5
order by id desc
limit 1;
SQL
}

wait_for_cli_error_log_metadata() {
  local channel="$1"
  local minimum_id="$2"
  local metadata=""
  for _ in {1..10}; do
    metadata="$(read_cli_error_log_metadata "$channel" "$minimum_id")"
    if [[ -n "$metadata" ]]; then
      printf '%s' "$metadata"
      return 0
    fi
    sleep 1
  done
  return 1
}

read_compaction_log_metadata() {
  local channel="$1"
  local minimum_id="$2"
  psql_exec -AtF '|' -v channel_id="$channel" -v token_id="$GATE_TOKEN_ID" -v minimum_id="$minimum_id" <<'SQL'
select
  type,
  coalesce(other::jsonb ->> 'error_code', ''),
  coalesce(other::jsonb ->> 'status_code', ''),
  coalesce(other::jsonb ->> 'responses_codex_local_compaction_ingress_reduced', ''),
  coalesce(other::jsonb ->> 'responses_codex_local_compaction_body_reduction_source', ''),
  coalesce(other::jsonb ->> 'responses_codex_local_compaction_ingress_original_body_bytes', ''),
  coalesce(other::jsonb ->> 'responses_codex_local_compaction_ingress_reduced_body_bytes', ''),
  coalesce(other::jsonb ->> 'responses_compact_mode', ''),
  coalesce(other::jsonb #>> '{stream_status,status}', '')
from logs
where id > :minimum_id and token_id = :token_id and channel_id = :channel_id
order by id desc
limit 1;
SQL
}

wait_for_compaction_log_metadata() {
  local channel="$1"
  local minimum_id="$2"
  local metadata=""
  for _ in {1..15}; do
    metadata="$(read_compaction_log_metadata "$channel" "$minimum_id")"
    if [[ -n "$metadata" ]]; then
      printf '%s' "$metadata"
      return 0
    fi
    sleep 1
  done
  return 1
}

safe_observation_value() {
  local value="$1"
  if [[ -z "$value" ]]; then
    printf 'null'
  elif [[ "$value" =~ ^[A-Za-z0-9_.:-]+$ ]]; then
    printf '%s' "$value"
  else
    printf 'invalid'
  fi
}

valid_body_metadata() {
  local ingress_reduced="$1"
  local source="$2"
  local original_bytes="$3"
  local reduced_bytes="$4"
  local mode="$5"
  local stream_status="$6"
  [[ "$ingress_reduced" == "true" ]] &&
    [[ "$source" == "ingress_preemptive_size" ]] &&
    [[ "$original_bytes" =~ ^[0-9]+$ ]] &&
    [[ "$reduced_bytes" =~ ^[0-9]+$ ]] &&
    (( original_bytes > SAFE_BODY_BYTES )) &&
    (( reduced_bytes > 0 && reduced_bytes <= SAFE_BODY_BYTES )) &&
    [[ "$mode" == "codex_local" ]] &&
    [[ "$stream_status" == "ok" ]]
}

valid_baseline_metadata() {
  local ingress_reduced="$1"
  local mode="$2"
  local stream_status="$3"
  [[ -z "$ingress_reduced" ]] &&
    [[ "$mode" == "codex_local" ]] &&
    [[ "$stream_status" == "ok" ]]
}

run_compaction_gate() {
  local channel="$1"
  local phase="$2"
  local configured_tool_bytes="$3"
  local expected_ingress_reduced="$4"
  local request_shape="${5:-baseline}"
  local output_file="${TMP_DIR}/${phase}-${channel}.json"
  local error_file="${TMP_DIR}/${phase}-${channel}.stderr"
  local before_log_id rc http_status terminal failure_class request_bytes tool_bytes
  local log_metadata="" log_type error_code logged_status ingress_reduced source
  local original_bytes reduced_bytes mode stream_status observed_log=false

  before_log_id="$(latest_log_id)"
  if NEWAPI_GATE_TOKEN="$TOKEN_KEY" \
    docker run --rm \
      --network "$ISOLATED_NETWORK" \
      --read-only \
      --tmpfs /tmp:rw,noexec,nosuid,size=16m \
      --mount "type=bind,src=${ROOT_DIR},dst=/workspace,readonly" \
      --workdir /workspace \
      --env NEWAPI_GATE_TOKEN \
      "$NETWORK_CLIENT_IMAGE" \
      node scripts/codex-local-compaction-gate.mjs \
      --base-url "$NETWORK_BASE_URL" \
      --token-env NEWAPI_GATE_TOKEN \
      --channel "$channel" \
      --model "$MODEL" \
      --tool-bytes "$configured_tool_bytes" \
      --request-shape "$request_shape" \
      --timeout-ms "$COMPACTION_TIMEOUT_MS" >"$output_file" 2>"$error_file"; then
    rc=0
  else
    rc=$?
  fi

  if ! jq -e . "$output_file" >/dev/null 2>&1; then
    record_result "phase=${phase} channel=${channel} status=failed failure_class=probe_output_invalid"
    return 1
  fi

  http_status="$(json_field '.http_status' "$output_file")"
  terminal="$(json_field '.terminal_type' "$output_file")"
  failure_class="$(json_field '.failure_class' "$output_file")"
  request_bytes="$(json_field '.request_body_bytes' "$output_file")"
  tool_bytes="$(json_field '.tool_definition_bytes' "$output_file")"

  if log_metadata="$(wait_for_compaction_log_metadata "$channel" "$before_log_id")"; then
    observed_log=true
  fi
  IFS='|' read -r log_type error_code logged_status ingress_reduced source original_bytes reduced_bytes mode stream_status <<< "$log_metadata"

  if (( rc != 0 )); then
    record_result "phase=${phase} channel=${channel} request_shape=${request_shape} status=failed http=${http_status} terminal=${terminal} failure_class=${failure_class} request_bytes=${request_bytes} tool_bytes=${tool_bytes} observed_log=${observed_log} log_type=$(safe_observation_value "$log_type") log_status=$(safe_observation_value "$logged_status") log_error_code=$(safe_observation_value "$error_code") ingress_reduced=$(safe_observation_value "$ingress_reduced") ingress_original_bytes=$(safe_observation_value "$original_bytes") ingress_reduced_bytes=$(safe_observation_value "$reduced_bytes") compact_mode=$(safe_observation_value "$mode") stream_status=$(safe_observation_value "$stream_status")"
    return 1
  fi

  if [[ "$expected_ingress_reduced" == "true" ]]; then
    if valid_body_metadata "$ingress_reduced" "$source" "$original_bytes" "$reduced_bytes" "$mode" "$stream_status"; then
      record_result "phase=${phase} channel=${channel} request_shape=${request_shape} status=passed http=${http_status} terminal=${terminal} request_bytes=${request_bytes} tool_bytes=${tool_bytes} ingress_reduced=${ingress_reduced} ingress_original_bytes=${original_bytes} ingress_reduced_bytes=${reduced_bytes}"
      return 0
    fi
  elif valid_baseline_metadata "$ingress_reduced" "$mode" "$stream_status"; then
    record_result "phase=${phase} channel=${channel} request_shape=${request_shape} status=passed http=${http_status} terminal=${terminal} request_bytes=${request_bytes} tool_bytes=${tool_bytes} ingress_reduced=false"
    return 0
  fi

  record_result "phase=${phase} channel=${channel} request_shape=${request_shape} status=failed http=${http_status} terminal=${terminal} failure_class=gateway_observability_mismatch request_bytes=${request_bytes} tool_bytes=${tool_bytes} observed_log=${observed_log} log_type=$(safe_observation_value "$log_type") log_status=$(safe_observation_value "$logged_status") log_error_code=$(safe_observation_value "$error_code") ingress_reduced=$(safe_observation_value "$ingress_reduced") ingress_original_bytes=$(safe_observation_value "$original_bytes") ingress_reduced_bytes=$(safe_observation_value "$reduced_bytes") compact_mode=$(safe_observation_value "$mode") stream_status=$(safe_observation_value "$stream_status")"
  return 1
}

run_structure_probe() {
  local channel shape overall_rc=0
  for channel in "${CHANNEL_IDS[@]}"; do
    for shape in "${STRUCTURE_PROBE_SHAPES[@]}"; do
      run_compaction_gate "$channel" "structure_${shape}" "$BASELINE_TOOL_BYTES" false "$shape" || overall_rc=1
    done
  done
  return "$overall_rc"
}

stop_isolated_app() {
  isolated_compose stop new-api >"${TMP_DIR}/stop-app.log" 2>&1
  ISOLATED_APP_STOPPED=true
  ISOLATED_APP_RESTARTED=false
}

restore_gate_database() {
  psql_exec \
    -v gate_user_id="$GATE_USER_ID" \
    -v gate_token_id="$GATE_TOKEN_ID" >"${TMP_DIR}/restore-db.log" 2>&1 <<SQL
begin;
do \$\$
begin
  if to_regclass('${BACKUP_CHANNELS}') is null
    or to_regclass('${BACKUP_ABILITIES}') is null
    or to_regclass('${BACKUP_USERS}') is null
    or to_regclass('${BACKUP_TOKENS}') is null
    or to_regclass('${BACKUP_OPTIONS}') is null
    or to_regclass('${BACKUP_TAG_POLICIES}') is null
    or to_regclass('${BACKUP_TAG_SCOPE}') is null
    or to_regclass('${PRODUCTION_SOURCE_TAGS}') is null
    or to_regclass('${BACKUP_HEADER_STATES}') is null
    or to_regclass('${BACKUP_STATE}') is null then
    raise exception 'Codex real gate backup tables are missing';
  end if;
  if not (select snapshot_ready from ${BACKUP_STATE} limit 1) then
    raise exception 'Codex real gate backup is not committed';
  end if;
end
\$\$;
delete from logs where user_id = :gate_user_id or token_id = :gate_token_id;
delete from quota_data where user_id = :gate_user_id;
delete from synthetic_compact_state_records where user_id = :gate_user_id or token_id = :gate_token_id;
delete from abilities where channel_id in (${CHANNEL_IDS_SQL});
insert into abilities select * from ${BACKUP_ABILITIES};
delete from channels where id in (${CHANNEL_IDS_SQL});
insert into channels select * from ${BACKUP_CHANNELS};
delete from options where key in (${GATE_OPTION_KEYS_SQL});
insert into options select * from ${BACKUP_OPTIONS};
delete from tag_request_header_policies where tag in (select tag from ${BACKUP_TAG_SCOPE});
insert into tag_request_header_policies select * from ${BACKUP_TAG_POLICIES};
delete from request_header_strategy_states
where scope_key ~ ('^channel:(' || replace('${CHANNEL_IDS_SQL}', ',', '|') || ')(:|$)')
   or scope_key in (select 'tag:' || tag from ${BACKUP_TAG_SCOPE});
insert into request_header_strategy_states select * from ${BACKUP_HEADER_STATES};
delete from tokens where id = :gate_token_id;
insert into tokens select * from ${BACKUP_TOKENS};
delete from users where id = :gate_user_id;
insert into users select * from ${BACKUP_USERS};
do \$\$
begin
  if (select count(*) from channels where id in (${CHANNEL_IDS_SQL})) <> ${BACKUP_CHANNEL_COUNT} then
    raise exception 'channel restoration count mismatch';
  end if;
  if (select count(*) from abilities where channel_id in (${CHANNEL_IDS_SQL})) <> ${BACKUP_ABILITY_COUNT} then
    raise exception 'ability restoration count mismatch';
  end if;
  if (select count(*) from options where key in (${GATE_OPTION_KEYS_SQL})) <> ${BACKUP_OPTION_COUNT} then
    raise exception 'option restoration count mismatch';
  end if;
  if (select count(*) from tag_request_header_policies where tag in (select tag from ${BACKUP_TAG_SCOPE})) <> ${BACKUP_TAG_POLICY_COUNT} then
    raise exception 'tag policy restoration count mismatch';
  end if;
  if (select count(*) from request_header_strategy_states
      where scope_key ~ ('^channel:(' || replace('${CHANNEL_IDS_SQL}', ',', '|') || ')(:|$)')
         or scope_key in (select 'tag:' || tag from ${BACKUP_TAG_SCOPE})) <> ${BACKUP_HEADER_STATE_COUNT} then
    raise exception 'header state restoration count mismatch';
  end if;
end
\$\$;
drop table ${BACKUP_CHANNELS};
drop table ${BACKUP_ABILITIES};
drop table ${BACKUP_USERS};
drop table ${BACKUP_TOKENS};
drop table ${BACKUP_OPTIONS};
drop table ${BACKUP_TAG_POLICIES};
drop table ${BACKUP_TAG_SCOPE};
drop table ${PRODUCTION_SOURCE_TAGS};
drop table ${BACKUP_HEADER_STATES};
drop table ${BACKUP_STATE};
commit;
SQL
}

snapshot_database_is_committed() {
  local marker_exists snapshot_ready
  if [[ "$SNAPSHOT_TABLES_CREATED" != "true" ]]; then
    return 1
  fi
  marker_exists="$(psql_exec -Atc "select to_regclass('${BACKUP_STATE}') is not null;" 2>/dev/null)" || return 1
  if [[ "$marker_exists" != "t" ]]; then
    return 1
  fi
  snapshot_ready="$(psql_exec -Atc "select snapshot_ready from ${BACKUP_STATE} limit 1;" 2>/dev/null)" || return 1
  [[ "$snapshot_ready" == "t" ]]
}

drop_uncommitted_snapshot_tables() {
  psql_exec >"${TMP_DIR}/drop-uncommitted-snapshot.log" 2>&1 <<SQL
begin;
drop table if exists ${BACKUP_CHANNELS};
drop table if exists ${BACKUP_ABILITIES};
drop table if exists ${BACKUP_USERS};
drop table if exists ${BACKUP_TOKENS};
drop table if exists ${BACKUP_OPTIONS};
drop table if exists ${BACKUP_TAG_POLICIES};
drop table if exists ${BACKUP_TAG_SCOPE};
drop table if exists ${PRODUCTION_SOURCE_TAGS};
drop table if exists ${BACKUP_HEADER_STATES};
drop table if exists ${BACKUP_STATE};
commit;
SQL
}

restart_isolated_services() {
  isolated_compose up -d --force-recreate redis >"${TMP_DIR}/recreate-redis.log" 2>&1
  for _ in {1..45}; do
    if [[ "$(docker inspect -f '{{.State.Health.Status}}' "$REDIS_CONTAINER")" == "healthy" ]]; then
      break
    fi
    sleep 1
  done
  if [[ "$(docker inspect -f '{{.State.Health.Status}}' "$REDIS_CONTAINER")" != "healthy" ]]; then
    return 1
  fi
  isolated_compose up -d --no-deps --force-recreate new-api >"${TMP_DIR}/restart-app.log" 2>&1
  if ! wait_for_isolated_app_liveness || ! verify_isolated_app_status; then
    return 1
  fi
  ISOLATED_APP_RESTARTED=true
}

verify_cleanup() {
  local counts backup_count
  counts="$(psql_exec -AtF '|' -v gate_user_id="$GATE_USER_ID" -v gate_token_id="$GATE_TOKEN_ID" <<'SQL'
select
  (select count(*) from users where id = :gate_user_id),
  (select count(*) from tokens where id = :gate_token_id),
  (select count(*) from logs where user_id = :gate_user_id or token_id = :gate_token_id),
  (select count(*) from quota_data where user_id = :gate_user_id),
  (select count(*) from synthetic_compact_state_records where user_id = :gate_user_id or token_id = :gate_token_id);
SQL
)"
  backup_count="$(psql_exec -Atc "select count(*) from pg_tables where schemaname = 'public' and tablename in ('${BACKUP_CHANNELS}', '${BACKUP_ABILITIES}', '${BACKUP_USERS}', '${BACKUP_TOKENS}', '${BACKUP_OPTIONS}', '${BACKUP_TAG_POLICIES}', '${BACKUP_TAG_SCOPE}', '${PRODUCTION_SOURCE_TAGS}', '${BACKUP_HEADER_STATES}', '${BACKUP_STATE}');")"
  [[ "$counts" == "0|0|0|0|0" && "$backup_count" == "0" ]]
}

cleanup() {
  local original_rc=$?
  local cleanup_rc=0
  local cleanup_state="not_started"
  trap - EXIT HUP INT TERM
  set +e
  stop_production_snapshot

  if [[ "$LIST_PRODUCTION_CANDIDATES" == "true" ]]; then
    rm -rf "$TMP_DIR"
    exit "$original_rc"
  fi

  if [[ "$SNAPSHOT_READY" == "true" ]] || snapshot_database_is_committed; then
    if [[ "$ISOLATED_APP_RESTARTED" == "true" ]] && ! stop_isolated_app; then
      cleanup_rc=1
      cleanup_state="app_stop_failed"
    elif ! restore_gate_database; then
      cleanup_rc=1
      cleanup_state="database_restore_failed"
    elif ! restart_isolated_services; then
      cleanup_rc=1
      cleanup_state="service_restart_failed"
    elif ! verify_cleanup; then
      cleanup_rc=1
      cleanup_state="post_restore_verification_failed"
    else
      cleanup_state="passed"
    fi
  elif [[ "$SNAPSHOT_TABLES_CREATED" == "true" ]]; then
    if drop_uncommitted_snapshot_tables; then
      if restart_isolated_services; then
        cleanup_state="uncommitted_snapshot_dropped"
      else
        cleanup_rc=1
        cleanup_state="service_restart_failed"
      fi
    else
      cleanup_rc=1
      cleanup_state="uncommitted_snapshot_cleanup_failed"
    fi
  elif [[ "$ISOLATED_APP_STOPPED" == "true" ]]; then
    if restart_isolated_services; then
      cleanup_state="restarted_without_snapshot"
    else
      cleanup_rc=1
      cleanup_state="service_restart_failed"
    fi
  fi
  if [[ "$cleanup_state" == "passed" ]]; then
    record_result 'cleanup=passed isolated_redis_recreated=true isolated_app_network_ready=true isolated_app_status_verified=true'
  elif [[ "$cleanup_state" == "not_started" || "$cleanup_state" == "restarted_without_snapshot" || "$cleanup_state" == "uncommitted_snapshot_dropped" ]]; then
    record_result 'cleanup=not_started'
  else
    record_result "cleanup=failed state=${cleanup_state}"
  fi
  rm -rf "$TMP_DIR"
  if (( original_rc == 0 && cleanup_rc != 0 )); then
    exit "$cleanup_rc"
  fi
  exit "$original_rc"
}

main() {
  local channel overall_rc=0
  if (( $# > 1 )) || { (( $# == 1 )) && [[ "$1" != "--prepare-only" && "$1" != "--structure-probe" && "$1" != "--list-production-candidates" ]]; }; then
    echo "usage: $0 [--prepare-only|--structure-probe|--list-production-candidates]" >&2
    return 2
  fi
  if (( $# == 1 )); then
    case "$1" in
      --prepare-only) PREPARE_ONLY=true ;;
      --structure-probe) STRUCTURE_PROBE=true ;;
      --list-production-candidates) LIST_PRODUCTION_CANDIDATES=true ;;
    esac
  fi
  validate_model
  validate_dev_port
  validate_excluded_channel_ids
  if [[ "$LIST_PRODUCTION_CANDIDATES" == "true" ]]; then
    guard_production_postgres_source
    if ! start_production_snapshot; then
      return 1
    fi
    configure_gate_channels
    printf 'production_eligible_channels=%s\n' "$CHANNEL_IDS_SQL"
    return 0
  fi
  guard_production_postgres_source
  if ! start_production_snapshot; then
    return 1
  fi
  configure_gate_channels
  configure_fake_upstream
  if [[ "$STRUCTURE_PROBE" == "true" ]]; then
    configure_structure_probe_shapes
  fi
  require_positive_integer "$CLI_TIMEOUT_SECONDS" "CLI timeout"
  require_positive_integer "$COMPACTION_TIMEOUT_MS" "compaction timeout"
  require_positive_integer "$READINESS_TIMEOUT_SECONDS" "readiness timeout"
  require_positive_integer "$STATUS_TIMEOUT_SECONDS" "status timeout"
  require_positive_integer "$BASELINE_TOOL_BYTES" "baseline tool bytes"
  if (( COMPACTION_TIMEOUT_MS < 1000 || COMPACTION_TIMEOUT_MS > 300000 )); then
    echo "compaction timeout must be from 1000 to 300000 milliseconds" >&2
    return 2
  fi
  if (( READINESS_TIMEOUT_SECONDS < 10 || READINESS_TIMEOUT_SECONDS > 300 )); then
    echo "readiness timeout must be from 10 to 300 seconds" >&2
    return 2
  fi
  if (( STATUS_TIMEOUT_SECONDS < 15 || STATUS_TIMEOUT_SECONDS > 300 )); then
    echo "status timeout must be from 15 to 300 seconds" >&2
    return 2
  fi
  guard_isolated_target
  if ! verify_snapshot_schema; then
    return 1
  fi
  reserve_gate_ids
  if ! stop_isolated_app; then
    echo "failed to stop isolated app before channel snapshot" >&2
    return 1
  fi
  if ! prepare_gate_database; then
    echo "failed to prepare isolated channel snapshot" >&2
    return 1
  fi
  if ! import_production_gate_channels; then
    return 1
  fi
  stop_production_snapshot
  if ! configure_fake_upstream_for_gate_channels; then
    echo "failed to apply fake upstream observation config" >&2
    return 1
  fi
  if ! restart_isolated_services; then
    echo "failed to restart isolated services after channel snapshot" >&2
    return 1
  fi
  printf 'gate_target=isolated_%s model=%s tool_definition_bytes=%s channels=%s fake_upstream_mode=%s image=%s\n' "$DEV_PORT" "$MODEL" "$TOOL_BYTES" "${CHANNEL_IDS[*]}" "$FAKE_UPSTREAM_MODE" "$ISOLATED_APP_IMAGE"
  if [[ "$PREPARE_ONLY" == "true" ]]; then
    printf 'gate_prepare_only=true\n'
    return 0
  fi

  if [[ "$STRUCTURE_PROBE" == "true" ]]; then
    run_structure_probe
    return $?
  fi

  for channel in "${CHANNEL_IDS[@]}"; do
    if [[ "$FAKE_UPSTREAM_MODE" == "true" ]]; then
      record_result "phase=cli channel=${channel} status=skipped reason=fake_upstream_observation"
    elif ! run_cli_smoke "$channel"; then
      overall_rc=1
    fi
    run_compaction_gate "$channel" compaction_baseline "$BASELINE_TOOL_BYTES" false || overall_rc=1
    run_compaction_gate "$channel" compaction_oversized "$TOOL_BYTES" true || overall_rc=1
  done
  return "$overall_rc"
}

trap 'exit 130' HUP INT TERM
trap cleanup EXIT
main "$@"
