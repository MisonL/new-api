import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import test from 'node:test';

const repoRoot = resolve(import.meta.dirname, '..');
const e2eSource = readFileSync(resolve(repoRoot, 'scripts/compact-control-plane-e2e.sh'), 'utf8');
const e2eLibSource = readFileSync(resolve(repoRoot, 'scripts/compact-control-plane-e2e-lib.sh'), 'utf8');
const e2eImplementation = `${e2eSource}\n${e2eLibSource}`;

test('derives isolated PostgreSQL credentials from the configured environment file', () => {
  assert.match(e2eSource, /COMPACT_E2E_POSTGRES_USER:-\$\{DEV_POSTGRES_USER:-\}/);
  assert.match(e2eSource, /COMPACT_E2E_POSTGRES_DB:-\$\{DEV_POSTGRES_DB:-\}/);
  assert.match(e2eSource, /read_env_file_value DEV_POSTGRES_USER/);
  assert.match(e2eSource, /read_env_file_value DEV_POSTGRES_DB/);
  assert.match(e2eSource, /psql_exec\(\) \{ docker exec -i "\$POSTGRES_CONTAINER" psql -U "\$POSTGRES_USER" -d "\$POSTGRES_DB"/);
  assert.match(e2eSource, /psql_query\(\) \{ psql_exec -At "\$@"; \}/);
});

test('does not restart the isolated app unless seeded state and Redis state were restored', () => {
  assert.match(e2eSource, /local db_restored=0/);
  assert.match(e2eSource, /local redis_cleaned=0/);
  assert.match(e2eSource, /local restart_allowed=0/);
  assert.match(e2eSource, /db_restored=1/);
  assert.match(e2eSource, /redis_cleaned=1/);
  assert.match(
    e2eSource,
    /if \(\( app_quiesced == 1 && db_restored == 1 && redis_cleaned == 1 \)\); then\s+restart_allowed=1/,
  );
  assert.match(e2eSource, /cleanup: refusing to restart dev container because database or Redis restoration did not complete/);
  assert.match(e2eSource, /if \(\( restart_allowed == 1 \)\); then/);
});

test('cleans fixture-scoped Redis caches beyond the concurrency lease keys', () => {
  assert.match(e2eSource, /"rateLimit:\$\{TOKEN_ID\}"/);
  assert.match(e2eSource, /"rateLimit:MRRL:\$\{TOKEN_ID\}"/);
  assert.match(e2eSource, /"rateLimit:MRRLS:\$\{TOKEN_ID\}"/);
  assert.match(e2eSource, /redis_exec_checked --scan --pattern "token:\*"/);
  assert.match(e2eSource, /redis_exec_checked HGET "\$key" Id/);
  assert.match(e2eSource, /redis_exec_checked HGET "\$key" UserId/);
  assert.match(e2eSource, /redis_exec_checked --scan --pattern "perf:\*:\$\{TEST_GROUP\}:\*"/);
  assert.match(e2eSource, /channel_health_prefix="new-api:channel-health:v1"/);
  assert.match(e2eSource, /redis_exec_checked --scan --pattern "\$\{channel_health_prefix\}:\*:metadata"/);
  assert.match(e2eSource, /redis_exec_checked EVAL/);
  assert.match(e2eSource, /new-api:channel_affinity:v1/);
  assert.match(e2eSource, /new-api:channel_affinity_usage_cache_stats:v1/);
  assert.match(e2eSource, /redis_exec_checked\(\)/);
  assert.match(e2eSource, /ERR\\\ \*\|WRONGTYPE\\\ \*\|NOAUTH\\\ \*/);
  assert.match(e2eSource, /redis_exec_checked EVAL/);
  assert.match(e2eSource, /if ! table_exists="\$\(psql_query -c/);
  assert.doesNotMatch(e2eSource, /table_exists="\$\(psql_query[^\n]*\|\| true\)/);
  assert.match(e2eSource, /local cleanup_failed=0/);
  assert.match(e2eSource, /cleanup_failed=1/);
  assert.match(e2eSource, /return "\$cleanup_failed"/);
});

test('seeds explicit V2 route fields when switching the isolated test channel', () => {
  assert.match(
    e2eImplementation,
    /route_priority,\s*route_weight_bps,\s*route_priority_configured,\s*route_weight_bps_configured/,
  );
  assert.match(e2eImplementation, /true, true\)\s*\n?on conflict/);
  assert.match(e2eImplementation, /route_weight_bps_configured = excluded\.route_weight_bps_configured/);
});
