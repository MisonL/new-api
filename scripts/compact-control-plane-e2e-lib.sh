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
  local username
  token_key="compacte2e$(date +%s)$(openssl rand -hex 12)"
  username="compact_e2e_user_${TOKEN_ID}"
  psql_exec \
    -v test_group="$TEST_GROUP" \
    -v token_id="$TOKEN_ID" \
    -v user_id="$TOKEN_ID" \
    -v token_key="$token_key" \
    -v username="$username" \
    -v backup_options="$BACKUP_OPTIONS" \
    -v channel_native_newapi="$CHANNEL_NATIVE_NEWAPI" \
    -v channel_sub2api_http="$CHANNEL_SUB2API_HTTP" \
    -v channel_synthetic_newapi="$CHANNEL_SYNTHETIC_NEWAPI" \
    -v channel_generic_openai="$CHANNEL_GENERIC_OPENAI" \
    -v upstream_container_url="$UPSTREAM_CONTAINER_URL" \
    >/dev/null <<SQL
begin;
drop table if exists ${BACKUP_CHANNELS};
drop table if exists ${BACKUP_ABILITIES};
drop table if exists ${BACKUP_TOKENS};
drop table if exists ${BACKUP_USERS};
drop table if exists ${BACKUP_OPTIONS};
create table ${BACKUP_CHANNELS} as
  select * from channels where id in (:channel_native_newapi, :channel_sub2api_http, :channel_synthetic_newapi, :channel_generic_openai);
create table ${BACKUP_ABILITIES} as
  select * from abilities where "group" = :'test_group';
create table ${BACKUP_TOKENS} as
  select * from tokens where id = :token_id;
create table ${BACKUP_USERS} as
  select * from users where id = :user_id;
create table ${BACKUP_OPTIONS} as
  select * from options where key in ('GroupRatio', 'UserUsableGroups');

delete from abilities where "group" = :'test_group';
delete from channels where id in (:channel_native_newapi, :channel_sub2api_http, :channel_synthetic_newapi, :channel_generic_openai);
delete from tokens where id = :token_id;
delete from users where id = :user_id;
delete from options where key in ('GroupRatio', 'UserUsableGroups');
insert into options (key, value)
values
  ('GroupRatio', (
    coalesce((select value from ${BACKUP_OPTIONS} where key = 'GroupRatio'), '{}')::jsonb
    || jsonb_build_object(:'test_group', 1.0)
  )::text),
  ('UserUsableGroups', (
    coalesce((select value from ${BACKUP_OPTIONS} where key = 'UserUsableGroups'), '{}')::jsonb
    || jsonb_build_object(:'test_group', 'compact e2e')
  )::text);

insert into users (id, username, password, display_name, role, status, email, quota, used_quota, request_count, "group", aff_count, aff_quota, aff_history, inviter_id, setting, remark, created_at, last_login_at)
values (:user_id, :'username', 'compact-e2e-password', 'compact-e2e', 1, 1, :'username' || '@example.invalid', 1000000000, 0, 0, :'test_group', 0, 0, 0, 0, '', 'compact control plane e2e temp user', extract(epoch from now())::bigint, 0);

insert into tokens (id, user_id, key, status, name, created_time, accessed_time, expired_time, remain_quota, unlimited_quota, model_limits_enabled, model_limits, allow_ips, used_quota, "group", cross_group_retry)
select :token_id, :user_id, :'token_key', 1, 'compact-control-plane-e2e', extract(epoch from now())::bigint, 0, -1, 0, true, false, '', '', 0, :'test_group', false;

insert into channels (id, type, key, status, name, weight, created_time, base_url, models, "group", priority, auto_ban, settings)
values
  (:channel_native_newapi, 1, 'e2e-upstream-native', 2, 'codex-compact-e2e-native-newapi', 0, extract(epoch from now())::bigint, :'upstream_container_url', 'gpt-5.5,gpt-5.5-openai-compact,gpt-5.4,gpt-5.4-openai-compact', :'test_group', 0, 0, '{"responses_compact_mode":"auto","responses_upstream_profile":"official_newapi","allow_service_tier":true}'),
  (:channel_sub2api_http, 1, 'e2e-upstream-sub2api', 2, 'codex-compact-e2e-sub2api-http', 0, extract(epoch from now())::bigint, :'upstream_container_url', 'gpt-5.5,gpt-5.5-openai-compact,gpt-5.4,gpt-5.4-openai-compact', :'test_group', 0, 0, '{"responses_compact_mode":"auto","responses_upstream_profile":"sub2api_http","allow_service_tier":true}'),
  (:channel_synthetic_newapi, 1, 'e2e-upstream-synthetic', 1, 'codex-compact-e2e-synthetic-newapi', 0, extract(epoch from now())::bigint, :'upstream_container_url', 'gpt-5.5,gpt-5.4', :'test_group', 0, 0, '{"allow_service_tier":true,"responses_compact_mode":"synthetic_summary","responses_upstream_profile":"trusted_newapi","responses_compact_summary_fallback_models":["gpt-5.4","gpt-5.5"]}'),
  (:channel_generic_openai, 1, 'e2e-upstream-generic', 2, 'codex-compact-e2e-generic-openai', 0, extract(epoch from now())::bigint, :'upstream_container_url', 'gpt-5.5,gpt-5.4', :'test_group', 0, 0, '{"responses_compact_mode":"auto","responses_upstream_profile":"generic_openai","allow_service_tier":true}');
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
insert into abilities ("group", model, channel_id, enabled, priority, weight)
values (:'test_group', :'model', :channel, true, 0, 1)
on conflict ("group", model, channel_id) do update set enabled = excluded.enabled, priority = excluded.priority, weight = excluded.weight;
insert into abilities ("group", model, channel_id, enabled, priority, weight)
values (:'test_group', replace(:'model', '-openai-compact', ''), :channel, true, 0, 1)
on conflict ("group", model, channel_id) do update set enabled = excluded.enabled, priority = excluded.priority, weight = excluded.weight;
insert into abilities ("group", model, channel_id, enabled, priority, weight)
values (:'test_group', 'gpt-5.4', :channel, true, 0, 1)
on conflict ("group", model, channel_id) do update set enabled = excluded.enabled, priority = excluded.priority, weight = excluded.weight;
update channels set status = 1 where id = :channel;
update channels set status = 2 where id in (:channel_native_newapi, :channel_sub2api_http, :channel_synthetic_newapi, :channel_generic_openai) and id <> :channel;
SQL
  restart_dev
}
