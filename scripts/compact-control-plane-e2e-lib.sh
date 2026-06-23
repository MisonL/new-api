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
    -v token_key="$token_key" \
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
create table ${BACKUP_CHANNELS} as
  select * from channels where id in (:channel_native_newapi, :channel_sub2api_http, :channel_synthetic_newapi, :channel_generic_openai);
create table ${BACKUP_ABILITIES} as
  select * from abilities where "group" = :'test_group';
create table ${BACKUP_TOKENS} as
  select * from tokens where id = :token_id;

delete from abilities where "group" = :'test_group';
delete from channels where id in (:channel_native_newapi, :channel_sub2api_http, :channel_synthetic_newapi, :channel_generic_openai);
delete from tokens where id = :token_id;

insert into tokens (id, user_id, key, status, name, created_time, accessed_time, expired_time, remain_quota, unlimited_quota, model_limits_enabled, model_limits, allow_ips, used_quota, "group", cross_group_retry)
select :token_id, coalesce((select min(id) from users), 1), :'token_key', 1, 'compact-control-plane-e2e', extract(epoch from now())::bigint, 0, -1, 0, true, false, '', '', 0, :'test_group', false;

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
