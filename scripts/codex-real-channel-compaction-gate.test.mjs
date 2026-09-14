import assert from 'node:assert/strict';
import { mkdtempSync, readFileSync, rmSync, writeFileSync, chmodSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';
import test from 'node:test';

const repoRoot = resolve(import.meta.dirname, '..');
const gateScript = resolve(repoRoot, 'scripts/codex-real-channel-compaction-gate.sh');
const cliSmokeScript = resolve(repoRoot, 'scripts/codex-cli-isolated-smoke.sh');
const gateSource = readFileSync(gateScript, 'utf8');
const cliSmokeSource = readFileSync(cliSmokeScript, 'utf8');

test('derives isolated Docker resource names from the configured Compose project', () => {
  assert.match(gateSource, /resolve_isolated_compose_project\(\)/);
  assert.match(gateSource, /DEV_COMPOSE_PROJECT_NAME/);
  assert.match(gateSource, /ISOLATED_NETWORK="\$\{COMPOSE_PROJECT_NAME\}_dev-isolated-network"/);
  assert.match(gateSource, /POSTGRES_CONTAINER="\$\{COMPOSE_PROJECT_NAME\}-postgres-1"/);
  assert.match(gateSource, /REDIS_CONTAINER="\$\{COMPOSE_PROJECT_NAME\}-redis-1"/);
  assert.match(gateSource, /APP_CONTAINER="\$\{COMPOSE_PROJECT_NAME\}-new-api-1"/);
});

test('uses the configured isolated PostgreSQL credentials and environment file', () => {
  assert.match(gateSource, /CODEX_REAL_GATE_COMPOSE_FILE/);
  assert.match(gateSource, /CODEX_REAL_GATE_ENV_FILE/);
  assert.match(gateSource, /ISOLATED_POSTGRES_USER="\$\{CODEX_REAL_GATE_POSTGRES_USER:-\$\{DEV_POSTGRES_USER:-\}\}"/);
  assert.match(gateSource, /ISOLATED_POSTGRES_DB="\$\{CODEX_REAL_GATE_POSTGRES_DB:-\$\{DEV_POSTGRES_DB:-\}\}"/);
  assert.match(gateSource, /read_env_file_value DEV_POSTGRES_USER/);
  assert.match(gateSource, /read_env_file_value DEV_POSTGRES_DB/);
  assert.match(gateSource, /psql -X -q -v ON_ERROR_STOP=1 -U "\$ISOLATED_POSTGRES_USER" -d "\$ISOLATED_POSTGRES_DB"/);
});

test('selects a healthy loopback endpoint for host-side Codex probes while keeping data-plane traffic isolated', () => {
  assert.match(gateSource, /DEV_PORT="\$\{DEV_NEW_API_PORT:-3001\}"/);
  assert.match(gateSource, /HOST_BASE_URL=""/);
  assert.match(gateSource, /HOST_BASE_URL_OVERRIDE="\$\{CODEX_REAL_GATE_HOST_BASE_URL:-\}"/);
  assert.match(gateSource, /select_host_base_url\(\)/);
  assert.match(gateSource, /http:\/\/127\.0\.0\.1:\$\{DEV_PORT\}/);
  assert.match(gateSource, /http:\/\/\[::1\]:\$\{DEV_PORT\}/);
  assert.match(gateSource, /--connect-timeout 1 --max-time 3/);
  assert.match(gateSource, /NETWORK_BASE_URL="http:\/\/new-api:3000"/);
  assert.match(
    gateSource,
    /if \[\[ "\$NETWORK_BASE_URL" != "http:\/\/new-api:3000" \]\]; then/,
  );
  assert.match(gateSource, /refusing non-loopback host base URL/);
});

test('allows only a same-port loopback override and probes it before use', () => {
  assert.match(gateSource, /validate_host_base_url_override\(\)/);
  assert.match(gateSource, /CODEX_REAL_GATE_HOST_BASE_URL must be an http loopback URL using DEV_NEW_API_PORT/);
  assert.match(gateSource, /"http:\/\/127\.0\.0\.1:\$\{DEV_PORT\}"\|"http:\/\/\[::1\]:\$\{DEV_PORT\}"/);
  assert.match(gateSource, /CODEX_REAL_GATE_HOST_BASE_URL/);
  assert.match(gateSource, /healthy isolated host endpoint/);
});

test('pins the isolated image across stop and restart when no image override is provided', () => {
  assert.match(gateSource, /ISOLATED_APP_IMAGE=""/);
  assert.match(gateSource, /docker inspect -f '\{\{\.Config\.Image\}\}' "\$APP_CONTAINER"/);
  assert.match(gateSource, /isolated_compose\(\)/);
  assert.match(gateSource, /DEV_NEW_API_IMAGE="\$ISOLATED_APP_IMAGE" DEV_NEW_API_PORT="\$DEV_PORT"/);
  assert.match(gateSource, /gate_target=isolated_%s[\s\S]*image=%s/);
});

test('pins the current isolated Redis host port across Compose restarts', () => {
  assert.match(gateSource, /ISOLATED_REDIS_PORT=""/);
  assert.match(gateSource, /resolve_isolated_redis_port\(\)/);
  assert.match(gateSource, /CODEX_REAL_GATE_REDIS_PORT/);
  assert.match(gateSource, /6379\/tcp/);
  assert.match(gateSource, /DEV_REDIS_PORT="\$ISOLATED_REDIS_PORT"/);
  assert.match(gateSource, /isolated_redis_port=\$\{ISOLATED_REDIS_PORT\} source=\$\{source\}/);
});

test('classifies isolated Redis container readiness without dereferencing absent Health state', () => {
  assert.match(gateSource, /isolated_redis_readiness_state\(\)/);
  assert.match(gateSource, /\{\{if \.State\.Health\}\}\{\{\.State\.Health\.Status\}\}\{\{else\}\}none\{\{end\}\}/);
  assert.match(gateSource, /container_missing error=/);
  assert.match(gateSource, /not_running status=/);
  assert.match(gateSource, /no_health status=/);
  assert.match(gateSource, /printf 'healthy\\n'/);
  assert.match(gateSource, /printf 'unhealthy\\n'/);
  assert.doesNotMatch(gateSource, /docker inspect -f '\{\{\.State\.Health\.Status\}\}' "\$REDIS_CONTAINER"/);
});

test('requires explicit opt-in before changing copied channels for dynamic Codex header verification', () => {
  assert.match(gateSource, /APPLY_TEST_CODEX_PASSTHROUGH="\$\{CODEX_REAL_GATE_APPLY_CODEX_PASSTHROUGH:-0\}"/);
  assert.match(gateSource, /validate_test_codex_passthrough\(\)/);
  assert.match(gateSource, /configure_test_codex_passthrough\(\)/);
  assert.match(gateSource, /if \[\[ "\$APPLY_TEST_CODEX_PASSTHROUGH" != "1" \]\]; then/);
  assert.match(gateSource, /update channels\s+set param_override = :'codex_param_override'/);
  assert.match(gateSource, /gate_test_codex_passthrough=true/);
  assert.match(gateSource, /CODEX_PASSTHROUGH_PARAM_OVERRIDE=.*pass_headers/);
  assert.match(gateSource, /CODEX_PASSTHROUGH_PARAM_OVERRIDE=.*copy_header/);
});

test('records only allowlisted transport cause codes', () => {
  assert.match(gateSource, /transport_error_code=/);
  assert.match(gateSource, /safe_observation_value "\$transport_error_code"/);
});

test('pins production candidate discovery and binary copies to one exported read-only snapshot', () => {
  assert.match(gateSource, /start_production_snapshot\(\)[\s\S]*pg_export_snapshot\(\)/);
  assert.match(
    gateSource,
    /production_psql_transaction\(\)[\s\S]*set transaction snapshot '\$\{PRODUCTION_SNAPSHOT_ID\}'/,
  );
  assert.match(
    gateSource,
    /guard_production_postgres_source[\s\S]*start_production_snapshot[\s\S]*configure_gate_channels/,
  );
  assert.match(
    gateSource,
    /copy_production_table_into_isolated\(\)[\s\S]*production_psql_transaction/,
  );
  assert.match(gateSource, /cleanup\(\)[\s\S]*stop_production_snapshot/);
});

test('continues with compaction probes after a CLI smoke failure', () => {
  assert.match(
    gateSource,
    /elif ! run_cli_smoke "\$channel"; then\s+overall_rc=1\s+fi\s+run_compaction_gate "\$channel" compaction_baseline/s,
  );
  assert.doesNotMatch(
    gateSource,
    /elif ! run_cli_smoke "\$channel"; then\s+overall_rc=1\s+continue/s,
  );
});

test('records a secret-free CLI failure stage and exit code for real channel diagnostics', () => {
  assert.match(cliSmokeSource, /stage=initialization/);
  assert.match(cliSmokeSource, /stage=codex_exec/);
  assert.match(cliSmokeSource, /stage=marker_presence/);
  assert.match(cliSmokeSource, /stage=marker_content/);
  assert.match(
    cliSmokeSource,
    /status=failed stage=\$\{stage\} exit=\$\{status\} channel=\$\{channel_id\} model=\$\{model\}/,
  );
  assert.match(
    gateSource,
    /cli_stage=\$\(safe_observation_value "\$cli_stage"\) cli_exit=\$\(safe_observation_value "\$cli_exit"\)/,
  );
  assert.doesNotMatch(gateSource, /for \(index = 1;/);
});

test('correlates failed CLI smoke with only safe gateway error-log metadata', () => {
  assert.match(
    gateSource,
    /read_cli_error_log_metadata\(\)[\s\S]*where channel_id = :channel_id[\s\S]*token_id = :token_id[\s\S]*id > :minimum_id[\s\S]*type = 5/,
  );
  assert.match(gateSource, /responses_routing_channel_skips,0,reason/);
  assert.match(
    gateSource,
    /cli_log_observed=\$\{observed_cli_log\} cli_log_type=\$\(safe_observation_value "\$cli_log_type"\) cli_log_status=\$\(safe_observation_value "\$cli_log_status"\) cli_log_error_code=\$\(safe_observation_value "\$cli_log_error_code"\) cli_log_skip=\$\(safe_observation_value "\$cli_log_skip"\)/,
  );
});

test('reports codex execution failures with a structured smoke result', () => {
  const directory = mkdtempSync(join(tmpdir(), 'newapi-codex-cli-smoke-test-'));
  const resultPath = join(directory, 'result');
  try {
    const result = spawnSync('sh', [cliSmokeScript, '155'], {
      cwd: repoRoot,
      encoding: 'utf8',
      env: {
        ...process.env,
        CODEX_BIN: 'false',
        NEWAPI_CLI_SMOKE_RESULT_FILE: resultPath,
        NEWAPI_SMOKE_TOKEN: 'test-token-not-used-for-network',
      },
    });

    assert.equal(result.status, 1, result.stderr);
    assert.equal(
      readFileSync(resultPath, 'utf8'),
      'status=failed stage=codex_exec exit=1 channel=155 model=gpt-5.6-sol\n',
    );
  } finally {
    rmSync(directory, { force: true, recursive: true });
  }
});

test('snapshots the scoped Responses runtime configuration and latest CLI version record', () => {
  assert.match(gateSource, /GATE_OPTION_KEYS_SQL=.*global\.chat_completions_to_responses_policy/);
  assert.match(gateSource, /GATE_OPTION_KEYS_SQL=.*NpmCLIVersionRecordedOptions/);
  assert.match(gateSource, /copy_production_options_into_isolated/);
  assert.match(gateSource, /copy_production_header_states_into_isolated/);
  assert.match(gateSource, /restore_gate_database/);
});

test('snapshots every token pricing option needed by real channel probes', () => {
  for (const key of ['ModelRatio', 'ModelPrice', 'CompletionRatio', 'CacheRatio', 'CreateCacheRatio']) {
    assert.match(gateSource, new RegExp(`GATE_OPTION_KEYS_SQL=.*'${key}'`));
  }
});

test('restores every copied runtime configuration scope before restarting the isolated service', () => {
  assert.match(gateSource, /verify_snapshot_schema/);
  assert.doesNotMatch(gateSource, /verify_channel_copy_schema/);
  assert.match(gateSource, /copy_production_source_tags_into_isolated/);
  assert.match(gateSource, /BACKUP_OPTIONS/);
  assert.match(gateSource, /BACKUP_TAG_POLICIES/);
  assert.match(gateSource, /BACKUP_HEADER_STATES/);
  assert.match(gateSource, /delete from options where key in \(\$\{GATE_OPTION_KEYS_SQL\}\);/);
  assert.match(gateSource, /delete from tag_request_header_policies[\s\S]*BACKUP_TAG_SCOPE/);
  assert.match(gateSource, /delete from request_header_strategy_states[\s\S]*BACKUP_TAG_SCOPE/);
  assert.match(gateSource, /drop table \$\{BACKUP_OPTIONS\};/);
  assert.match(gateSource, /drop table \$\{BACKUP_TAG_POLICIES\};/);
  assert.match(gateSource, /drop table \$\{BACKUP_HEADER_STATES\};/);
});

test('lists only production-enabled candidates through a read-only source', () => {
  withFakeDocker({ candidates: '2,75,208' }, ({ env, logPath }) => {
    const result = runGate(['--list-production-candidates'], env);

    assert.equal(result.status, 0, result.stderr);
    assert.equal(result.stdout.trim(), 'production_eligible_channels=2,75,208');
    assert.match(readFileSync(logPath, 'utf8'), /default_transaction_read_only=on/);
    assert.match(readFileSync(logPath, 'utf8'), /com\.docker\.compose\.project/);
    assert.match(readFileSync(logPath, 'utf8'), /com\.docker\.compose\.service/);
    assert.match(readFileSync(logPath, 'utf8'), /channels\.status = 1/);
    assert.match(readFileSync(logPath, 'utf8'), /abilities\.enabled = true/);
    assert.match(readFileSync(logPath, 'utf8'), /gpt-5\.6-sol/);
    assert.match(readFileSync(logPath, 'utf8'), /pg_export_snapshot/);
    assert.match(readFileSync(logPath, 'utf8'), /set transaction snapshot/);
  });
});

test('allows a safe model override when discovering production candidates', () => {
  withFakeDocker({ candidates: '205' }, ({ env, logPath }) => {
    const result = runGate(['--list-production-candidates'], {
      ...env,
      CODEX_REAL_GATE_MODEL: 'gpt-5.6-terra',
    });

    assert.equal(result.status, 0, result.stderr);
    assert.equal(result.stdout.trim(), 'production_eligible_channels=205');
    assert.match(readFileSync(logPath, 'utf8'), /gpt-5\.6-terra/);
  });
});

test('excludes only explicitly requested production candidates', () => {
  withFakeDocker({ candidates: '72,204,205' }, ({ env }) => {
    const result = runGate(['--list-production-candidates'], {
      ...env,
      CODEX_REAL_GATE_EXCLUDE_CHANNEL_IDS: '204',
    });

    assert.equal(result.status, 0, result.stderr);
    assert.equal(result.stdout.trim(), 'production_eligible_channels=72,205');
  });
});

test('rejects an invalid channel exclusion list before reading the production source', () => {
  const result = runGate(['--list-production-candidates'], {
    ...process.env,
    CODEX_REAL_GATE_EXCLUDE_CHANNEL_IDS: '204,not-a-channel',
  });

  assert.equal(result.status, 2);
  assert.match(result.stderr, /invalid CODEX_REAL_GATE_EXCLUDE_CHANNEL_IDS/);
});

test('rejects an unsafe model override before reading the production source', () => {
  const result = runGate(['--list-production-candidates'], {
    ...process.env,
    CODEX_REAL_GATE_MODEL: "gpt-5.6-terra'; select 1; --",
  });

  assert.equal(result.status, 2);
  assert.match(result.stderr, /invalid CODEX_REAL_GATE_MODEL/);
});

test('rejects a production source outside the expected Compose group', () => {
  withFakeDocker({ project: 'new-api-dev-isolated' }, ({ env }) => {
    const result = runGate(['--list-production-candidates'], env);

    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /production PostgreSQL Compose project must be new-api/);
  });
});

test('rejects a production source that is not the PostgreSQL service', () => {
  withFakeDocker({ service: 'redis' }, ({ env }) => {
    const result = runGate(['--list-production-candidates'], env);

    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /production PostgreSQL Compose service must be postgres/);
  });
});

test('rejects an empty production candidate snapshot', () => {
  withFakeDocker({ candidates: '' }, ({ env }) => {
    const result = runGate(['--list-production-candidates'], env);

    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /no production-enabled gpt-5\.6-sol channels were discovered/);
  });
});

function runGate(args, env) {
  return spawnSync('bash', [gateScript, ...args], {
    cwd: repoRoot,
    encoding: 'utf8',
    env,
  });
}

function withFakeDocker(overrides, callback) {
  const directory = mkdtempSync(join(tmpdir(), 'newapi-codex-gate-test-'));
  const dockerPath = join(directory, 'docker');
  const logPath = join(directory, 'docker.log');
  writeFileSync(dockerPath, fakeDockerScript(), 'utf8');
  chmodSync(dockerPath, 0o755);

  const env = {
    ...process.env,
    PATH: `${directory}:${process.env.PATH}`,
    FAKE_DOCKER_LOG: logPath,
    FAKE_DOCKER_CANDIDATES: overrides.candidates ?? '2,75,208',
    FAKE_DOCKER_PROJECT: overrides.project ?? 'new-api',
    FAKE_DOCKER_SERVICE: overrides.service ?? 'postgres',
    CODEX_REAL_GATE_PRODUCTION_POSTGRES_CONTAINER: 'production-postgres',
  };

  try {
    callback({ env, logPath });
  } finally {
    rmSync(directory, { force: true, recursive: true });
  }
}

function fakeDockerScript() {
  return [
    '#!/bin/sh',
    'set -eu',
    'printf "%s\\n" "$*" >> "$FAKE_DOCKER_LOG"',
    'case "$1" in',
    '  inspect)',
    '    case "$*" in',
    '      *".State.Running"*) printf "%s\\n" "true" ;;',
    '      *"com.docker.compose.project"*) printf "%s\\n" "$FAKE_DOCKER_PROJECT" ;;',
    '      *"com.docker.compose.service"*) printf "%s\\n" "$FAKE_DOCKER_SERVICE" ;;',
    '      *) exit 9 ;;',
    '    esac',
    '    ;;',
    '  exec)',
    '    case "$*" in',
    '      *"-qAt"*)',
    '        printf "%s\\n" "pg_export_snapshot" >> "$FAKE_DOCKER_LOG"',
    '        printf "%s\\n" "00000000-00000000-1"',
    '        cat >/dev/null',
    '        ;;',
    '      *"channels.status = 1"*)',
    '        cat >> "$FAKE_DOCKER_LOG"',
    '        printf "%s\\n" "$FAKE_DOCKER_CANDIDATES"',
    '        ;;',
    '      *)',
    '        cat >> "$FAKE_DOCKER_LOG"',
    '        ;;',
    '    esac',
    '    ;;',
    '  *) exit 9 ;;',
    'esac',
    '',
  ].join('\n');
}
