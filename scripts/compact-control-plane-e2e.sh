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
COMPOSE_FILE="${COMPACT_E2E_COMPOSE_FILE:-deploy/compose/dev-isolated.yml}"
ENV_FILE="${COMPACT_E2E_ENV_FILE:-deploy/env/dev-isolated.env}"
APP_SERVICE="${COMPACT_E2E_APP_SERVICE:-new-api}"
CURL_CONNECT_TIMEOUT="${COMPACT_E2E_CURL_CONNECT_TIMEOUT:-5}"
CURL_MAX_TIME="${COMPACT_E2E_CURL_MAX_TIME:-60}"
UPSTREAM_CURL_MAX_TIME="${COMPACT_E2E_UPSTREAM_CURL_MAX_TIME:-30}"
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
TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/newapi-compact-e2e.XXXXXX")"
FAKE_PID=""
HISTORY_FIXTURE_PATH="${COMPACT_E2E_HISTORY_FIXTURE:-}"

cd "$ROOT_DIR"

. "$ROOT_DIR/scripts/compact-control-plane-e2e-lib.sh"

psql_exec() { docker exec -i "$POSTGRES_CONTAINER" psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 "$@"; }

psql_query() { psql_exec -At; }

guard_dev_target() {
  if [[ "${COMPACT_E2E_FORCE:-}" == "1" ]]; then
    return
  fi
  if [[ "$BASE_URL" != "http://127.0.0.1:3001" ]]; then
    echo "refusing to run against non-dev base URL: $BASE_URL" >&2
    echo "set COMPACT_E2E_FORCE=1 only for an isolated throwaway environment" >&2
    exit 1
  fi
  if [[ "$POSTGRES_CONTAINER" != *"dev-isolated"* || "$APP_CONTAINER" != *"dev-isolated"* ]]; then
    echo "refusing to run outside dev-isolated containers" >&2
    exit 1
  fi
  export COMPACT_E2E_ALLOW_DB_RESET=1
}

cleanup() {
  local original_rc=$?
  local rc=0
  local cleanup_log="$TMP_DIR/cleanup.log"
  set +e
  if psql_exec \
    -v test_group="$TEST_GROUP" \
    -v token_id="$TOKEN_ID" \
    -v user_id="$TOKEN_ID" \
    -v channel_native_newapi="$CHANNEL_NATIVE_NEWAPI" \
    -v channel_sub2api_http="$CHANNEL_SUB2API_HTTP" \
    -v channel_synthetic_newapi="$CHANNEL_SYNTHETIC_NEWAPI" \
    -v channel_generic_openai="$CHANNEL_GENERIC_OPENAI" \
    -v backup_options="$BACKUP_OPTIONS" \
    >"$cleanup_log" 2>&1 <<SQL
begin;
do \$\$
begin
  if to_regclass('${BACKUP_ABILITIES}') is null
    or to_regclass('${BACKUP_CHANNELS}') is null
    or to_regclass('${BACKUP_TOKENS}') is null
    or to_regclass('${BACKUP_USERS}') is null
    or to_regclass('${BACKUP_OPTIONS}') is null then
    raise exception 'compact e2e backup tables are missing';
  end if;
end
\$\$;
delete from abilities where "group" = :'test_group';
insert into abilities select * from ${BACKUP_ABILITIES};
delete from channels where id in (:channel_native_newapi, :channel_sub2api_http, :channel_synthetic_newapi, :channel_generic_openai);
insert into channels select * from ${BACKUP_CHANNELS};
delete from tokens where id = :token_id;
insert into tokens select * from ${BACKUP_TOKENS};
delete from users where id = :user_id;
insert into users select * from ${BACKUP_USERS};
delete from options where key in ('GroupRatio', 'UserUsableGroups');
insert into options select * from ${BACKUP_OPTIONS};
drop table if exists ${BACKUP_CHANNELS};
drop table if exists ${BACKUP_ABILITIES};
drop table if exists ${BACKUP_TOKENS};
drop table if exists ${BACKUP_USERS};
drop table if exists ${BACKUP_OPTIONS};
commit;
SQL
  then
    echo "cleanup: database state restored" >&2
  else
    echo "cleanup: database restoration failed" >&2
    cat "$cleanup_log" >&2
    rc=1
  fi
  if [[ -n "$FAKE_PID" ]]; then
    kill "$FAKE_PID" >/dev/null 2>&1 || true
  fi
  local restart_log="$TMP_DIR/restart-dev.log"
  if restart_dev >"$restart_log" 2>&1; then
    echo "cleanup: dev container restarted" >&2
  else
    echo "cleanup: dev container restart failed" >&2
    cat "$restart_log" >&2
    rc=1
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

restart_dev() {
  docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" up -d --no-deps --force-recreate "$APP_SERVICE" >/dev/null
  for _ in {1..45}; do
    if curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$CURL_MAX_TIME" -fsS "$BASE_URL/api/status" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  echo "dev container did not become healthy" >&2
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

reset_upstream() {
  curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$UPSTREAM_CURL_MAX_TIME" -fsS "$UPSTREAM_HOST_URL/_reset" >/dev/null
}

capture_summary() {
  curl --connect-timeout "$CURL_CONNECT_TIMEOUT" --max-time "$UPSTREAM_CURL_MAX_TIME" -fsS "$UPSTREAM_HOST_URL/_captures/summary"
}

assert_contains() { local file="$1" needle="$2"
  grep -q "$needle" "$file" || { echo "missing ${needle} in ${file}" >&2; cat "$file" >&2; exit 1; }
}

run_case_native_newapi_compact() {
  echo "case=native_newapi_compact"
  set_single_channel "$CHANNEL_NATIVE_NEWAPI" 'gpt-5.5-openai-compact'
  reset_upstream
  local out="$TMP_DIR/native_compact.json"
  local code
  code="$(post_json /v1/responses/compact '{"model":"gpt-5.5-openai-compact","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"compact this codex context"}]}]}' "$out")"
  echo "status=${code}"
  [[ "$code" == "200" ]]
  node -e 'const fs=require("fs"); const o=JSON.parse(fs.readFileSync(process.argv[1],"utf8")); if(!JSON.stringify(o).includes("compaction")) process.exit(2); console.log("has_compaction=true")' "$out"
  capture_summary
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
  [[ "$code" != "200" ]]
  assert_contains "$out" 'compaction'
  capture_summary
}

run_case_sub2api_previous_response_id() {
  echo "case=sub2api_http_rejects_rest_previous_id"
  set_single_channel "$CHANNEL_SUB2API_HTTP" 'gpt-5.5-openai-compact'
  reset_upstream
  local out="$TMP_DIR/sub2api_prev.json"
  local code
  code="$(post_json /v1/responses/compact '{"model":"gpt-5.5-openai-compact","previous_response_id":"resp_remote_prev","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"compact with previous id"}]}]}' "$out")"
  echo "status=${code}"
  [[ "$code" != "200" ]]
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
  ensure_fake_upstream
  trap cleanup EXIT
  backup_and_seed_db
  local token
  token="$(psql_query <<< "select key from tokens where id = ${TOKEN_ID}")"
  if [[ -z "$token" ]]; then
    echo "missing compact e2e token" >&2
    exit 1
  fi
  prepare_curl_config "sk-${token}"
  docker exec "$APP_CONTAINER" /new-api --build-info
  restart_dev
  run_case_native_newapi_compact
  run_case_synthetic_continue
  run_case_stale_visible_only
  run_case_generic_rejects_remote_opaque
  run_case_sub2api_previous_response_id
  run_case_model_switch_restore
  run_case_real_codex_history_fixture
  echo "e2e_done=true"
}

main "$@"
