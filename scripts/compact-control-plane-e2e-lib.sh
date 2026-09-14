# shellcheck shell=bash
backup_and_seed_db() {
  if [[ "${COMPACT_E2E_ALLOW_DB_RESET:-}" != "1" ]]; then
    echo "refusing to reset E2E database without COMPACT_E2E_ALLOW_DB_RESET=1" >&2
    return 1
  fi
  command -v openssl >/dev/null 2>&1 || {
    echo "missing required command: openssl" >&2
    return 1
  }
  local token_key
  token_key="compacte2e$(date +%s)$(openssl rand -hex 12)"
  psql_exec \
    -v test_group="$TEST_GROUP" \
    -v token_id="$TOKEN_ID" \
    -v user_id="$TOKEN_ID" \
    -v token_key="$token_key" \
    -v token_name="$TOKEN_NAME" \
    -v username="$USERNAME" \
    -v backup_options="$BACKUP_OPTIONS" \
    -v channel_native_newapi="$CHANNEL_NATIVE_NEWAPI" \
    -v channel_sub2api_http="$CHANNEL_SUB2API_HTTP" \
    -v channel_synthetic_newapi="$CHANNEL_SYNTHETIC_NEWAPI" \
    -v channel_generic_openai="$CHANNEL_GENERIC_OPENAI" \
    -v channel_native_name="$CHANNEL_NATIVE_NAME" \
    -v channel_sub2api_name="$CHANNEL_SUB2API_NAME" \
    -v channel_synthetic_name="$CHANNEL_SYNTHETIC_NAME" \
    -v channel_generic_name="$CHANNEL_GENERIC_NAME" \
    -v upstream_container_url="$UPSTREAM_CONTAINER_URL" \
    >/dev/null <<SQL
begin;
set local compact_e2e.test_group = :'test_group';
set local compact_e2e.token_id = :'token_id';
set local compact_e2e.user_id = :'user_id';
set local compact_e2e.token_name = :'token_name';
set local compact_e2e.username = :'username';
set local compact_e2e.channel_native_newapi = :'channel_native_newapi';
set local compact_e2e.channel_sub2api_http = :'channel_sub2api_http';
set local compact_e2e.channel_synthetic_newapi = :'channel_synthetic_newapi';
set local compact_e2e.channel_generic_openai = :'channel_generic_openai';
set local compact_e2e.channel_native_name = :'channel_native_name';
set local compact_e2e.channel_sub2api_name = :'channel_sub2api_name';
set local compact_e2e.channel_synthetic_name = :'channel_synthetic_name';
set local compact_e2e.channel_generic_name = :'channel_generic_name';

-- Never delete a row merely because it has a historical E2E-looking name.
-- IDs and names are allocated for this run and collisions abort before the
-- baseline snapshot, so existing data cannot be silently overwritten.
do \$\$
begin
  if to_regclass('${BACKUP_CHANNELS}') is not null
    or to_regclass('${BACKUP_ABILITIES}') is not null
    or to_regclass('${BACKUP_TOKENS}') is not null
    or to_regclass('${BACKUP_USERS}') is not null
    or to_regclass('${BACKUP_OPTIONS}') is not null
    or to_regclass('${BACKUP_SEQUENCES}') is not null then
    raise exception 'compact e2e backup table collision';
  end if;
  if exists (
    select 1 from channels
    where id in (
      current_setting('compact_e2e.channel_native_newapi')::bigint,
      current_setting('compact_e2e.channel_sub2api_http')::bigint,
      current_setting('compact_e2e.channel_synthetic_newapi')::bigint,
      current_setting('compact_e2e.channel_generic_openai')::bigint
    )
       or name in (
         current_setting('compact_e2e.channel_native_name'),
         current_setting('compact_e2e.channel_sub2api_name'),
         current_setting('compact_e2e.channel_synthetic_name'),
         current_setting('compact_e2e.channel_generic_name')
       )
  ) then
    raise exception 'compact e2e channel fixture collision';
  end if;
  if exists (
    select 1 from tokens
    where id = current_setting('compact_e2e.token_id')::bigint
       or name = current_setting('compact_e2e.token_name')
  ) then
    raise exception 'compact e2e token fixture collision';
  end if;
  if exists (
    select 1 from users
    where id = current_setting('compact_e2e.user_id')::bigint
       or username = current_setting('compact_e2e.username')
  ) then
    raise exception 'compact e2e user fixture collision';
  end if;
end
\$\$;

create table ${BACKUP_CHANNELS} as
  select * from channels where id in (:channel_native_newapi, :channel_sub2api_http, :channel_synthetic_newapi, :channel_generic_openai);
create table ${BACKUP_ABILITIES} as
  select * from abilities where "group" = :'test_group';
create table ${BACKUP_TOKENS} as
  select * from tokens where id = :token_id;
create table ${BACKUP_USERS} as
  select * from users where id = :user_id;
create table ${BACKUP_OPTIONS} as
  select * from options where key in (
    'GroupRatio',
    'UserUsableGroups',
    'ModelRatio',
    'CompletionRatio',
    'CacheRatio',
    'CreateCacheRatio',
    'runtime.responses_synthetic_compact_instance_id'
  );
create table ${BACKUP_SEQUENCES} as
  select schemaname, sequencename, last_value, false::boolean as is_called
  from pg_sequences
  where schemaname = 'public'
    and (
      sequencename like 'abilities%'
      or sequencename like 'channels%'
      or sequencename like 'logs%'
      or sequencename like 'options%'
      or sequencename like 'perf_metrics%'
      or sequencename like 'quota_data%'
      or sequencename like 'synthetic_compact_state_records%'
      or sequencename like 'tokens%'
      or sequencename like 'users%'
    );
do \$\$
declare
  sequence_row record;
  sequence_last_value bigint;
  sequence_is_called boolean;
begin
  for sequence_row in select schemaname, sequencename from ${BACKUP_SEQUENCES} loop
    execute format('select last_value, is_called from %I.%I', sequence_row.schemaname, sequence_row.sequencename)
      into sequence_last_value, sequence_is_called;
    update ${BACKUP_SEQUENCES}
       set last_value = sequence_last_value,
           is_called = sequence_is_called
     where schemaname = sequence_row.schemaname
       and sequencename = sequence_row.sequencename;
  end loop;
end
\$\$;

delete from abilities where "group" = :'test_group';
delete from channels where id in (:channel_native_newapi, :channel_sub2api_http, :channel_synthetic_newapi, :channel_generic_openai);
delete from tokens where id = :token_id;
delete from users where id = :user_id;
delete from options where key in (
  'GroupRatio',
  'UserUsableGroups',
  'ModelRatio',
  'CompletionRatio',
  'CacheRatio',
  'CreateCacheRatio',
  'runtime.responses_synthetic_compact_instance_id'
);
insert into options (key, value)
values
  ('GroupRatio', (
    coalesce((select value from ${BACKUP_OPTIONS} where key = 'GroupRatio'), '{}')::jsonb
    || jsonb_build_object(:'test_group', 1.0)
  )::text),
  ('UserUsableGroups', (
    coalesce((select value from ${BACKUP_OPTIONS} where key = 'UserUsableGroups'), '{}')::jsonb
    || jsonb_build_object(:'test_group', 'compact e2e')
  )::text),
  ('ModelRatio', (
    coalesce((select value from ${BACKUP_OPTIONS} where key = 'ModelRatio'), '{}')::jsonb
    || jsonb_build_object('gpt-5.6-sol', 2.5)
  )::text);

insert into users (id, username, password, display_name, role, status, email, quota, used_quota, request_count, "group", aff_count, aff_quota, aff_history, inviter_id, setting, remark, created_at, last_login_at)
values (:user_id, :'username', 'compact-e2e-password', 'compact-e2e', 1, 1, :'username' || '@example.invalid', 1000000000, 0, 0, :'test_group', 0, 0, 0, 0, '', 'compact control plane e2e temp user', extract(epoch from now())::bigint, 0);

insert into tokens (id, user_id, key, status, name, created_time, accessed_time, expired_time, remain_quota, unlimited_quota, model_limits_enabled, model_limits, allow_ips, used_quota, "group", cross_group_retry)
select :token_id, :user_id, :'token_key', 1, :'token_name', extract(epoch from now())::bigint, 0, -1, 0, true, false, '', '', 0, :'test_group', false;

insert into channels (id, type, key, status, name, weight, created_time, base_url, models, "group", priority, auto_ban, settings)
values
  (:channel_native_newapi, 1, 'e2e-upstream-native', 2, :'channel_native_name', 0, extract(epoch from now())::bigint, :'upstream_container_url', 'gpt-5.6-sol,gpt-5.5,gpt-5.5-openai-compact,gpt-5.4,gpt-5.4-openai-compact', :'test_group', 0, 0, '{"responses_compact_mode":"auto","responses_upstream_profile":"official_newapi","allow_service_tier":true,"allow_include_obfuscation":true}'),
  (:channel_sub2api_http, 1, 'e2e-upstream-sub2api', 2, :'channel_sub2api_name', 0, extract(epoch from now())::bigint, :'upstream_container_url', 'gpt-5.5,gpt-5.5-openai-compact,gpt-5.4,gpt-5.4-openai-compact', :'test_group', 0, 0, '{"responses_compact_mode":"auto","responses_upstream_profile":"sub2api_http","allow_service_tier":true}'),
  (:channel_synthetic_newapi, 1, 'e2e-upstream-synthetic', 1, :'channel_synthetic_name', 0, extract(epoch from now())::bigint, :'upstream_container_url', 'gpt-5.5,gpt-5.4', :'test_group', 0, 0, '{"allow_service_tier":true,"responses_compact_mode":"synthetic_summary","responses_upstream_profile":"trusted_newapi","responses_compact_summary_fallback_models":["gpt-5.4","gpt-5.5"]}'),
  (:channel_generic_openai, 1, 'e2e-upstream-generic', 2, :'channel_generic_name', 0, extract(epoch from now())::bigint, :'upstream_container_url', 'gpt-5.5,gpt-5.4', :'test_group', 0, 0, '{"responses_compact_mode":"auto","responses_upstream_profile":"generic_openai","allow_service_tier":true}');
commit;
SQL
}

set_single_channel() {
  local channel="$1"
  local model="$2"
  psql_exec \
    -v test_group="$TEST_GROUP" \
    -v model="$model" \
    -v channel="$channel" \
    -v channel_native_newapi="$CHANNEL_NATIVE_NEWAPI" \
    -v channel_sub2api_http="$CHANNEL_SUB2API_HTTP" \
    -v channel_synthetic_newapi="$CHANNEL_SYNTHETIC_NEWAPI" \
    -v channel_generic_openai="$CHANNEL_GENERIC_OPENAI" \
    >/dev/null <<SQL
update abilities set enabled = false where "group" = :'test_group';
insert into abilities (
  "group", model, channel_id, enabled, priority, weight,
  route_priority, route_weight_bps, route_priority_configured, route_weight_bps_configured
)
values (:'test_group', :'model', :channel, true, 0, 1, 0, 10000, true, true)
on conflict ("group", model, channel_id) do update set
  enabled = excluded.enabled,
  priority = excluded.priority,
  weight = excluded.weight,
  route_priority = excluded.route_priority,
  route_weight_bps = excluded.route_weight_bps,
  route_priority_configured = excluded.route_priority_configured,
  route_weight_bps_configured = excluded.route_weight_bps_configured;
insert into abilities (
  "group", model, channel_id, enabled, priority, weight,
  route_priority, route_weight_bps, route_priority_configured, route_weight_bps_configured
)
values (:'test_group', replace(:'model', '-openai-compact', ''), :channel, true, 0, 1, 0, 10000, true, true)
on conflict ("group", model, channel_id) do update set
  enabled = excluded.enabled,
  priority = excluded.priority,
  weight = excluded.weight,
  route_priority = excluded.route_priority,
  route_weight_bps = excluded.route_weight_bps,
  route_priority_configured = excluded.route_priority_configured,
  route_weight_bps_configured = excluded.route_weight_bps_configured;
insert into abilities (
  "group", model, channel_id, enabled, priority, weight,
  route_priority, route_weight_bps, route_priority_configured, route_weight_bps_configured
)
values (:'test_group', 'gpt-5.4', :channel, true, 0, 1, 0, 10000, true, true)
on conflict ("group", model, channel_id) do update set
  enabled = excluded.enabled,
  priority = excluded.priority,
  weight = excluded.weight,
  route_priority = excluded.route_priority,
  route_weight_bps = excluded.route_weight_bps,
  route_priority_configured = excluded.route_priority_configured,
  route_weight_bps_configured = excluded.route_weight_bps_configured;
update channels set status = 1 where id = :channel;
update channels set status = 2 where id in (:channel_native_newapi, :channel_sub2api_http, :channel_synthetic_newapi, :channel_generic_openai) and id <> :channel;
SQL
  restart_dev
}
