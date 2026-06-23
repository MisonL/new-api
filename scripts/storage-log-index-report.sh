#!/usr/bin/env bash
set -euo pipefail

POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-}"
POSTGRES_USER="${POSTGRES_USER:-root}"
POSTGRES_DB="${POSTGRES_DB:-}"
REPORT_LIMIT="${REPORT_LIMIT:-50}"
SAMPLE_USER_ID="${SAMPLE_USER_ID:-}"
SAMPLE_USERNAME="${SAMPLE_USERNAME:-}"
SAMPLE_TOKEN_NAME="${SAMPLE_TOKEN_NAME:-}"
SAMPLE_MODEL_NAME="${SAMPLE_MODEL_NAME:-}"
SAMPLE_CHANNEL_ID="${SAMPLE_CHANNEL_ID:-}"
SAMPLE_GROUP="${SAMPLE_GROUP:-}"
SAMPLE_REQUEST_ID="${SAMPLE_REQUEST_ID:-}"
SAMPLE_UPSTREAM_REQUEST_ID="${SAMPLE_UPSTREAM_REQUEST_ID:-}"
SAMPLE_START_TS="${SAMPLE_START_TS:-}"
SAMPLE_END_TS="${SAMPLE_END_TS:-}"
REPORT_UNMASK_SENSITIVE_VALUES="${REPORT_UNMASK_SENSITIVE_VALUES:-}"

section() {
  printf '\n## %s\n' "$1"
}

usage() {
  cat <<'EOF'
Usage:
  POSTGRES_CONTAINER=<container> POSTGRES_DB=<database> scripts/storage-log-index-report.sh

Environment:
  POSTGRES_CONTAINER     required
  POSTGRES_USER          default: root
  POSTGRES_DB            required
  REPORT_LIMIT           default: 50

Optional sample filters:
  SAMPLE_USER_ID
  SAMPLE_USERNAME
  SAMPLE_TOKEN_NAME
  SAMPLE_MODEL_NAME
  SAMPLE_CHANNEL_ID
  SAMPLE_GROUP
  SAMPLE_REQUEST_ID
  SAMPLE_UPSTREAM_REQUEST_ID
  SAMPLE_START_TS
  SAMPLE_END_TS
  REPORT_UNMASK_SENSITIVE_VALUES  set to 1 to print unmasked sample identifiers
EOF
}

require_target() {
  if [ -z "$POSTGRES_CONTAINER" ]; then
    printf 'POSTGRES_CONTAINER is required. Refusing to guess a database container for log index reporting.\n' >&2
    usage >&2
    exit 2
  fi
  if [ -z "$POSTGRES_DB" ]; then
    printf 'POSTGRES_DB is required. Refusing to guess a database name for log index reporting.\n' >&2
    usage >&2
    exit 2
  fi
}

psql_query() {
  local sql="$1"
  docker exec "$POSTGRES_CONTAINER" psql -q -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atc "$sql" | tr -d '\r'
}

psql_vars() {
  docker exec -i "$POSTGRES_CONTAINER" psql -q -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
    -v user_id="$SAMPLE_USER_ID" \
    -v username="$SAMPLE_USERNAME" \
    -v token_name="$SAMPLE_TOKEN_NAME" \
    -v model_name="$SAMPLE_MODEL_NAME" \
    -v channel_id="$SAMPLE_CHANNEL_ID" \
    -v group_name="$SAMPLE_GROUP" \
    -v request_id="$SAMPLE_REQUEST_ID" \
    -v upstream_request_id="$SAMPLE_UPSTREAM_REQUEST_ID" \
    -v start_ts="$SAMPLE_START_TS" \
    -v end_ts="$SAMPLE_END_TS" \
    -At <<SQL | tr -d '\r'
$1
SQL
}

require_positive_int() {
  local name="$1"
  local value="$2"
  case "$value" in
    ''|*[!0-9]*)
      printf '%s must be a positive integer, got: %s\n' "$name" "$value" >&2
      exit 2
      ;;
  esac
  if [ "$value" -le 0 ]; then
    printf '%s must be greater than 0, got: %s\n' "$name" "$value" >&2
    exit 2
  fi
}

require_non_negative_int() {
  local name="$1"
  local value="$2"
  case "$value" in
    ''|*[!0-9]*)
      printf '%s must be a non-negative integer, got: %s\n' "$name" "$value" >&2
      exit 2
      ;;
  esac
}

masked_sample_value() {
  local label="$1"
  local value="$2"
  if [ -z "$value" ]; then
    return
  fi
  if [ "$REPORT_UNMASK_SENSITIVE_VALUES" = "1" ]; then
    printf '%s' "$value"
    return
  fi
  printf '[redacted:%s]' "$label"
}

redact_sensitive_output() {
  if [ "$REPORT_UNMASK_SENSITIVE_VALUES" = "1" ]; then
    cat
    return
  fi
  awk \
    -v username="$SAMPLE_USERNAME" \
    -v token_name="$SAMPLE_TOKEN_NAME" \
    -v request_id="$SAMPLE_REQUEST_ID" \
    -v upstream_request_id="$SAMPLE_UPSTREAM_REQUEST_ID" '
function replace_all(line, needle, replacement,    out, pos) {
  if (needle == "") {
    return line
  }
  out = ""
  while ((pos = index(line, needle)) > 0) {
    out = out substr(line, 1, pos - 1) replacement
    line = substr(line, pos + length(needle))
  }
  return out line
}
{
  line = $0
  line = replace_all(line, username, "[redacted:username]")
  line = replace_all(line, token_name, "[redacted:token_name]")
  line = replace_all(line, request_id, "[redacted:request_id]")
  line = replace_all(line, upstream_request_id, "[redacted:upstream_request_id]")
  print line
}'
}

first_value() {
  psql_query "$1" | head -n 1
}

populate_sample_defaults() {
  if [ -z "$SAMPLE_USER_ID" ]; then
    SAMPLE_USER_ID="$(first_value "select coalesce((select user_id::text from logs where user_id <> 0 order by created_at desc limit 1), '');")"
  fi
  if [ -z "$SAMPLE_USERNAME" ]; then
    SAMPLE_USERNAME="$(first_value "select coalesce((select username from logs where username <> '' order by created_at desc limit 1), '');")"
  fi
  if [ -z "$SAMPLE_TOKEN_NAME" ]; then
    SAMPLE_TOKEN_NAME="$(first_value "select coalesce((select token_name from logs where token_name <> '' order by created_at desc limit 1), '');")"
  fi
  if [ -z "$SAMPLE_MODEL_NAME" ]; then
    SAMPLE_MODEL_NAME="$(first_value "select coalesce((select model_name from logs where model_name <> '' order by created_at desc limit 1), '');")"
  fi
  if [ -z "$SAMPLE_CHANNEL_ID" ]; then
    SAMPLE_CHANNEL_ID="$(first_value "select coalesce((select channel_id::text from logs where channel_id <> 0 order by created_at desc limit 1), '');")"
  fi
  if [ -z "$SAMPLE_GROUP" ]; then
    SAMPLE_GROUP="$(first_value "select coalesce((select \"group\" from logs where \"group\" <> '' order by created_at desc limit 1), '');")"
  fi
  if [ -z "$SAMPLE_REQUEST_ID" ]; then
    SAMPLE_REQUEST_ID="$(first_value "select coalesce((select request_id from logs where request_id <> '' order by created_at desc limit 1), '');")"
  fi
  if [ -z "$SAMPLE_UPSTREAM_REQUEST_ID" ]; then
    SAMPLE_UPSTREAM_REQUEST_ID="$(first_value "select coalesce((select upstream_request_id from logs where upstream_request_id <> '' order by created_at desc limit 1), '');")"
  fi
  if [ -z "$SAMPLE_END_TS" ]; then
    SAMPLE_END_TS="$(first_value "select coalesce((select max(created_at)::text from logs), '');")"
  fi
  if [ -n "$SAMPLE_END_TS" ]; then
    require_non_negative_int "SAMPLE_END_TS" "$SAMPLE_END_TS"
  fi
  if [ -n "$SAMPLE_START_TS" ]; then
    require_non_negative_int "SAMPLE_START_TS" "$SAMPLE_START_TS"
  fi
  if [ -z "$SAMPLE_START_TS" ] && [ -n "$SAMPLE_END_TS" ]; then
    if [ "$SAMPLE_END_TS" -gt 86400 ]; then
      SAMPLE_START_TS="$((SAMPLE_END_TS - 86400))"
    else
      SAMPLE_START_TS=0
    fi
  fi
}

explain_query() {
  local name="$1"
  local sql="$2"
  section "query-plan: $name"
  psql_vars "explain (analyze false, costs true, verbose false, buffers false, format text) $sql" | redact_sensitive_output
}

main() {
  require_target
  require_positive_int "REPORT_LIMIT" "$REPORT_LIMIT"
  populate_sample_defaults

  section "summary"
  printf 'postgres_container=%s\n' "$POSTGRES_CONTAINER"
  printf 'postgres_db=%s\n' "$POSTGRES_DB"
  printf 'report_limit=%s\n' "$REPORT_LIMIT"
  psql_query "select 'logs_rows=' || count(*) from logs;"
  psql_query "select 'logs_heap=' || pg_size_pretty(pg_relation_size('logs'));"
  psql_query "select 'logs_indexes=' || pg_size_pretty(pg_indexes_size('logs'));"
  psql_query "select 'logs_total=' || pg_size_pretty(pg_total_relation_size('logs'));"
  psql_query "select 'logs_index_count=' || count(*) from pg_indexes where tablename = 'logs';"

  section "index-size-and-usage"
  psql_query "
    select concat_ws(
      E'\t',
      s.indexrelname,
      pg_size_pretty(pg_relation_size(s.indexrelid)),
      pg_relation_size(s.indexrelid),
      s.idx_scan,
      s.idx_tup_read,
      s.idx_tup_fetch
    )
    from pg_stat_user_indexes s
    where s.relname = 'logs'
    order by pg_relation_size(s.indexrelid) desc, s.indexrelname
    limit $REPORT_LIMIT;
  "

  section "index-definitions"
  psql_query "
    select indexname || E'\t' || indexdef
    from pg_indexes
    where tablename = 'logs'
    order by indexname
    limit $REPORT_LIMIT;
  "

  section "prefix-contained-candidates"
  psql_query "
    with idx as (
      select
        c2.relname as index_name,
        pg_get_indexdef(i.indexrelid) as indexdef,
        pg_relation_size(i.indexrelid) as size_bytes,
        array(
          select a.attname
          from unnest(i.indkey) with ordinality as k(attnum, ord)
          join pg_attribute a on a.attrelid = i.indrelid and a.attnum = k.attnum
          where k.attnum > 0
          order by k.ord
        ) as cols
      from pg_index i
      join pg_class c on c.oid = i.indrelid
      join pg_class c2 on c2.oid = i.indexrelid
      join pg_namespace n on n.oid = c.relnamespace
      where n.nspname = 'public'
        and c.relname = 'logs'
        and i.indisvalid
        and i.indisready
    )
    select concat_ws(
      E'\t',
      small.index_name,
      big.index_name,
      array_to_string(small.cols, ','),
      pg_size_pretty(small.size_bytes),
      pg_size_pretty(big.size_bytes)
    )
    from idx small
    join idx big
      on small.index_name <> big.index_name
      and array_length(small.cols, 1) < array_length(big.cols, 1)
      and small.cols = big.cols[1:array_length(small.cols, 1)]
    order by small.size_bytes desc, small.index_name, big.index_name
    limit $REPORT_LIMIT;
  "

  section "sample-values"
  printf 'sample_user_id=%s\n' "$SAMPLE_USER_ID"
  printf 'sample_username=%s\n' "$(masked_sample_value username "$SAMPLE_USERNAME")"
  printf 'sample_token_name=%s\n' "$(masked_sample_value token_name "$SAMPLE_TOKEN_NAME")"
  printf 'sample_model_name=%s\n' "$SAMPLE_MODEL_NAME"
  printf 'sample_channel_id=%s\n' "$SAMPLE_CHANNEL_ID"
  printf 'sample_group=%s\n' "$SAMPLE_GROUP"
  printf 'sample_request_id=%s\n' "$(masked_sample_value request_id "$SAMPLE_REQUEST_ID")"
  printf 'sample_upstream_request_id=%s\n' "$(masked_sample_value upstream_request_id "$SAMPLE_UPSTREAM_REQUEST_ID")"
  printf 'sample_start_ts=%s\n' "$SAMPLE_START_TS"
  printf 'sample_end_ts=%s\n' "$SAMPLE_END_TS"

  explain_query "admin-recent" \
    "select id from logs order by created_at desc, id desc limit 20;"
  explain_query "time-range-consume" \
    "select id from logs where type = 2 and created_at >= coalesce(nullif(:'start_ts', '')::bigint, 0) and created_at <= coalesce(nullif(:'end_ts', '')::bigint, 9223372036854775807) order by created_at desc, id desc limit 20;"

  if [ -n "$SAMPLE_USER_ID" ]; then
    explain_query "user-recent" \
      "select id from logs where user_id = :'user_id'::int order by created_at desc, id desc limit 20;"
  fi
  if [ -n "$SAMPLE_USERNAME" ]; then
    explain_query "username-recent" \
      "select id from logs where username = :'username' order by created_at desc, id desc limit 20;"
  fi
  if [ -n "$SAMPLE_TOKEN_NAME" ]; then
    explain_query "token-recent" \
      "select id from logs where token_name = :'token_name' order by created_at desc, id desc limit 20;"
  fi
  if [ -n "$SAMPLE_MODEL_NAME" ]; then
    explain_query "model-recent" \
      "select id from logs where model_name = :'model_name' order by created_at desc, id desc limit 20;"
  fi
  if [ -n "$SAMPLE_CHANNEL_ID" ]; then
    explain_query "channel-recent" \
      "select id from logs where channel_id = :'channel_id'::int order by created_at desc, id desc limit 20;"
  fi
  if [ -n "$SAMPLE_GROUP" ]; then
    explain_query "group-recent" \
      "select id from logs where \"group\" = :'group_name' order by created_at desc, id desc limit 20;"
  fi
  if [ -n "$SAMPLE_REQUEST_ID" ]; then
    explain_query "request-id" \
      "select id from logs where request_id = :'request_id' order by created_at desc, id desc limit 20;"
  fi
  if [ -n "$SAMPLE_UPSTREAM_REQUEST_ID" ]; then
    explain_query "upstream-request-id" \
      "select id from logs where upstream_request_id = :'upstream_request_id' order by created_at desc, id desc limit 20;"
  fi
}

main "$@"
