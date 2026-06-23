#!/usr/bin/env bash
set -euo pipefail

MODE="${1:---dry-run}"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-}"
POSTGRES_USER="${POSTGRES_USER:-root}"
POSTGRES_DB="${POSTGRES_DB:-}"
CONFIRM_LOG_INDEX_MAINTENANCE="${CONFIRM_LOG_INDEX_MAINTENANCE:-}"

REQUIRED_INDEXES=(
  idx_logs_created_at_id
  idx_logs_upstream_request_created_at_id
)

DROP_CANDIDATES=(
  idx_created_at_id
  idx_logs_upstream_request_id_created_at
)

section() {
  printf '\n## %s\n' "$1"
}

usage() {
  cat <<'EOF'
Usage:
  POSTGRES_CONTAINER=<container> POSTGRES_DB=<database> scripts/storage-log-index-maintenance.sh --dry-run
  POSTGRES_CONTAINER=<container> POSTGRES_DB=<database> scripts/storage-log-index-maintenance.sh --execute

Environment:
  POSTGRES_CONTAINER     required
  POSTGRES_USER          default: root
  POSTGRES_DB            required
  CONFIRM_LOG_INDEX_MAINTENANCE

Execution confirmation format:
  execute:<POSTGRES_CONTAINER>:<POSTGRES_DB>:drop-legacy-log-indexes

This script only manages known legacy logs indexes. It does not inspect or
drop arbitrary prefix-contained candidates.
EOF
}

psql_query() {
  local sql="$1"
  docker exec "$POSTGRES_CONTAINER" psql -q -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atc "$sql" | tr -d '\r'
}

psql_exec_stdin() {
  docker exec -i "$POSTGRES_CONTAINER" psql -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"
}

quote_sql_literal() {
  local value="$1"
  printf "'%s'" "${value//\'/\'\'}"
}

quote_sql_identifier() {
  local value="$1"
  printf '"%s"' "${value//\"/\"\"}"
}

index_exists() {
  local index_name="$1"
  local quoted
  quoted="$(quote_sql_literal "$index_name")"
  [ "$(psql_query "select exists (
    select 1
    from pg_class idx
    join pg_namespace idx_ns on idx_ns.oid = idx.relnamespace
    join pg_index i on i.indexrelid = idx.oid
    join pg_class tbl on tbl.oid = i.indrelid
    join pg_namespace tbl_ns on tbl_ns.oid = tbl.relnamespace
    where idx_ns.nspname = 'public'
      and tbl_ns.nspname = 'public'
      and tbl.relname = 'logs'
      and idx.relkind = 'i'
      and idx.relname = $quoted
      and i.indisvalid
      and i.indisready
  );")" = "t" ]
}

index_size_bytes() {
  local index_name="$1"
  local quoted
  quoted="$(quote_sql_literal "$index_name")"
  psql_query "select coalesce((select pg_relation_size(c.oid)::text from pg_class c join pg_namespace n on n.oid = c.relnamespace where n.nspname = 'public' and c.relkind = 'i' and c.relname = $quoted), '0');"
}

print_index_def() {
  local index_name="$1"
  local quoted
  quoted="$(quote_sql_literal "$index_name")"
  psql_query "select coalesce((select pg_get_indexdef(c.oid) from pg_class c join pg_namespace n on n.oid = c.relnamespace where n.nspname = 'public' and c.relkind = 'i' and c.relname = $quoted), 'missing');"
}

confirmation_value() {
  printf 'execute:%s:%s:drop-legacy-log-indexes' "$POSTGRES_CONTAINER" "$POSTGRES_DB"
}

validate_mode() {
  case "$MODE" in
    --dry-run|--execute) ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      usage >&2
      exit 2
      ;;
  esac
}

require_target() {
  if [ -z "$POSTGRES_CONTAINER" ]; then
    printf 'POSTGRES_CONTAINER is required. Refusing to guess a database container for log index maintenance.\n' >&2
    usage >&2
    exit 2
  fi
  if [ -z "$POSTGRES_DB" ]; then
    printf 'POSTGRES_DB is required. Refusing to guess a database name for log index maintenance.\n' >&2
    usage >&2
    exit 2
  fi
}

verify_required_indexes() {
  section "required-indexes"
  local missing=0
  local index_name
  for index_name in "${REQUIRED_INDEXES[@]}"; do
    if index_exists "$index_name"; then
      printf 'required_index=%s status=present\n' "$index_name"
      print_index_def "$index_name"
    else
      printf 'required_index=%s status=missing\n' "$index_name" >&2
      missing=1
    fi
  done
  if [ "$missing" -ne 0 ]; then
    printf 'Missing required replacement index. Run application migration or create the replacement index before dropping legacy indexes.\n' >&2
    exit 1
  fi
}

print_candidate_summary() {
  section "drop-candidates"
  local total_bytes=0
  local index_name size_bytes
  for index_name in "${DROP_CANDIDATES[@]}"; do
    if index_exists "$index_name"; then
      size_bytes="$(index_size_bytes "$index_name")"
      total_bytes=$((total_bytes + size_bytes))
      printf 'drop_candidate=%s status=present size_bytes=%s\n' "$index_name" "$size_bytes"
      print_index_def "$index_name"
    else
      printf 'drop_candidate=%s status=missing size_bytes=0\n' "$index_name"
    fi
  done
  printf 'drop_candidate_total_bytes=%s\n' "$total_bytes"
}

print_usage_stats() {
  section "candidate-usage"
  local quoted_list
  quoted_list="$(printf ",%s" "${DROP_CANDIDATES[@]}")"
  quoted_list="${quoted_list#,}"
  quoted_list="'${quoted_list//,/','}'"
  psql_query "
    select concat_ws(
      E'\t',
      s.indexrelname,
      pg_size_pretty(pg_relation_size(s.indexrelid)),
      s.idx_scan,
      s.idx_tup_read,
      s.idx_tup_fetch
    )
    from pg_stat_user_indexes s
    where s.relname = 'logs'
      and s.indexrelname in ($quoted_list)
    order by s.indexrelname;
  "
}

drop_candidates() {
  section "execute"
  local expected
  expected="$(confirmation_value)"
  if [ "$CONFIRM_LOG_INDEX_MAINTENANCE" != "$expected" ]; then
    printf 'confirmation mismatch\n' >&2
    printf 'expected=%s\n' "$expected" >&2
    exit 3
  fi
  {
    local index_name
    for index_name in "${DROP_CANDIDATES[@]}"; do
      printf 'DROP INDEX CONCURRENTLY IF EXISTS public.%s;\n' "$(quote_sql_identifier "$index_name")"
    done
  } | psql_exec_stdin
  printf 'dropped_legacy_log_indexes=true\n'
}

main() {
  validate_mode
  require_target
  section "summary"
  printf 'mode=%s\n' "$MODE"
  printf 'postgres_container=%s\n' "$POSTGRES_CONTAINER"
  printf 'postgres_db=%s\n' "$POSTGRES_DB"
  printf 'execute_confirmation_required=%s\n' "$(confirmation_value)"
  psql_query "select 'logs_indexes_before=' || pg_size_pretty(pg_indexes_size('logs'));"
  psql_query "select 'logs_index_count_before=' || count(*) from pg_indexes where tablename = 'logs';"
  verify_required_indexes
  print_candidate_summary
  print_usage_stats
  if [ "$MODE" = "--execute" ]; then
    drop_candidates
    psql_query "select 'logs_indexes_after=' || pg_size_pretty(pg_indexes_size('logs'));"
    psql_query "select 'logs_index_count_after=' || count(*) from pg_indexes where tablename = 'logs';"
  fi
}

main "$@"
