import test from 'node:test';
import assert from 'node:assert/strict';
import { buildAuthJson, buildConfigToml, readExperimentalBearerToken } from '../src/config.js';

test('writes provider-token config by default', () => {
  const text = buildConfigToml({
    existingText: 'approval_policy = "never"\n',
    defaultModel: 'gpt-5.5',
    providerBaseUrl: 'https://example.com/v1',
    apiKey: ' test-key ',
  });

  assert.match(text, /model = "gpt-5.5"/);
  assert.match(text, /model_catalog_json = "new-api-model-catalog.json"/);
  assert.match(text, /wire_api = "responses"/);
  assert.match(text, /experimental_bearer_token = "test-key"/);
  assert.match(text, /approval_policy = "never"/);
  assert.equal(readExperimentalBearerToken(text), 'test-key');
});

test('preserves intentional blank lines inside existing body values', () => {
  const text = buildConfigToml({
    existingText: [
      'notes = """',
      'first',
      '',
      '',
      'second',
      '"""',
      '',
      'approval_policy = "never"',
      '',
    ].join('\n'),
    defaultModel: 'gpt-5.5',
    providerBaseUrl: 'https://example.com/v1',
    apiKey: 'test-key',
  });

  assert.match(text, /notes = """\nfirst\n\n\nsecond\n"""/);
  assert.match(text, /\n\napproval_policy = "never"\n\n# BEGIN new-api-codex-bridge provider/);
});

test('reads provider-token only from the managed new-api provider table', () => {
  const text = [
    '# BEGIN new-api-codex-bridge provider',
    '[model_providers.other]',
    'experimental_bearer_token = "wrong-key"',
    '',
    '[model_providers.new-api]',
    'experimental_bearer_token = "test-key"',
    '# END new-api-codex-bridge provider',
    '',
  ].join('\n');

  assert.equal(readExperimentalBearerToken(text), 'test-key');
});

test('reads TOML escaped provider-token strings', () => {
  const text = [
    '# BEGIN new-api-codex-bridge provider',
    '[model_providers.new-api]',
    'experimental_bearer_token = "line\\nkey\\u0021"',
    '# END new-api-codex-bridge provider',
    '',
  ].join('\n');

  assert.equal(readExperimentalBearerToken(text), 'line\nkey!');
});

test('reports invalid TOML escaped provider-token strings', () => {
  const text = [
    '# BEGIN new-api-codex-bridge provider',
    '[model_providers.new-api]',
    'experimental_bearer_token = "bad\\x41"',
    '# END new-api-codex-bridge provider',
    '',
  ].join('\n');

  assert.throws(() => readExperimentalBearerToken(text), /experimental_bearer_token 不是有效 TOML 字符串/);
});

test('ignores malformed managed headers while reading provider-token', () => {
  const text = [
    '# BEGIN new-api-codex-bridge provider',
    '[]',
    'experimental_bearer_token = "wrong-key"',
    '',
    '[model_providers.new-api]',
    'experimental_bearer_token = "test-key"',
    '# END new-api-codex-bridge provider',
    '',
  ].join('\n');

  assert.equal(readExperimentalBearerToken(text), 'test-key');
});

test('ignores tokens from sibling providers', () => {
  const text = [
    '# BEGIN new-api-codex-bridge provider',
    '[model_providers.other]',
    'experimental_bearer_token = "wrong-key"',
    '',
    '# END new-api-codex-bridge provider',
    '[profiles.dev]',
    'model = "gpt-5.5"',
    '',
  ].join('\n');

  assert.equal(readExperimentalBearerToken(text), '');
});

test('ignores unmanaged provider tables outside the managed marker block', () => {
  const text = [
    '[model_providers.new-api]',
    'experimental_bearer_token = "wrong-key"',
    '',
    '# BEGIN new-api-codex-bridge provider',
    '[model_providers.other]',
    'experimental_bearer_token = "also-wrong"',
    '# END new-api-codex-bridge provider',
    '',
  ].join('\n');

  assert.equal(readExperimentalBearerToken(text), '');
});

test('writes auth-json provider mode without token in config', () => {
  const text = buildConfigToml({
    defaultModel: 'gpt-5.5',
    providerBaseUrl: 'https://example.com/v1',
    authMode: 'auth-json',
  });

  assert.match(text, /requires_openai_auth = true/);
  assert.doesNotMatch(text, /experimental_bearer_token/);
});

test('writes env provider mode', () => {
  const text = buildConfigToml({
    defaultModel: 'gpt-5.5',
    providerBaseUrl: 'https://example.com/v1',
    authMode: 'env',
    envKey: 'NEW_API_KEY',
  });

  assert.match(text, /env_key = "NEW_API_KEY"/);
});

test('rejects blank provider auth inputs', () => {
  assert.throws(
    () =>
      buildConfigToml({
        defaultModel: 'gpt-5.5',
        providerBaseUrl: 'https://example.com/v1',
        apiKey: '   ',
      }),
    /需要 API Key/,
  );
  assert.throws(
    () =>
      buildConfigToml({
        defaultModel: 'gpt-5.5',
        providerBaseUrl: 'https://example.com/v1',
        authMode: 'env',
        envKey: '   ',
      }),
    /非空环境变量名/,
  );
});

test('auth json preserves existing fields', () => {
  const text = buildAuthJson({
    existingText: JSON.stringify({ other: 'value' }),
    apiKey: 'test-key',
  });
  assert.deepEqual(JSON.parse(text), {
    other: 'value',
    OPENAI_API_KEY: 'test-key',
  });
});

test('auth json rejects invalid existing JSON', () => {
  assert.throws(
    () =>
      buildAuthJson({
        existingText: '{',
        apiKey: 'test-key',
      }),
    /有效 JSON/,
  );
});

test('auth json rejects non-object existing JSON', () => {
  assert.throws(
    () =>
      buildAuthJson({
        existingText: '[]',
        apiKey: 'test-key',
      }),
    /必须是 JSON object/,
  );
});

test('env auth falls back to default env key when not provided explicitly', () => {
  const text = buildConfigToml({
    defaultModel: 'gpt-5.5',
    providerBaseUrl: 'https://example.com/v1',
    authMode: 'env',
  });

  assert.match(text, /env_key = "OPENAI_API_KEY"/);
});

test('auth json rejects blank api key', () => {
  assert.throws(
    () =>
      buildAuthJson({
        existingText: JSON.stringify({ other: 'value' }),
        apiKey: '   ',
      }),
    /需要 API Key/,
  );
});

test('replaces only the managed provider table and preserves following tables', () => {
  const text = buildConfigToml({
    existingText: [
      '[model_providers.new-api]',
      'name = "old"',
      '',
      '[[mcp_servers]]',
      'name = "keep"',
      '',
      '[profiles.dev]',
      'model = "dev"',
      '',
    ].join('\n'),
    defaultModel: 'gpt-5.5',
    providerBaseUrl: 'https://example.com/v1',
    apiKey: 'test-key',
  });

  assert.doesNotMatch(text, /name = "old"/);
  assert.match(text, /\[\[mcp_servers\]\]/);
  assert.match(text, /name = "keep"/);
  assert.match(text, /\[profiles.dev\]/);
  assert.match(text, /model = "dev"/);
});

test('replaces nested managed provider subtables and preserves sibling providers', () => {
  const text = buildConfigToml({
    existingText: [
      '[model_providers.new-api]',
      'name = "old"',
      '',
      '[model_providers.new-api.headers]',
      'x_old = "drop"',
      '',
      '[model_providers.other]',
      'name = "keep"',
      '',
    ].join('\n'),
    defaultModel: 'gpt-5.5',
    providerBaseUrl: 'https://example.com/v1',
    apiKey: 'test-key',
  });

  assert.doesNotMatch(text, /x_old/);
  assert.match(text, /\[model_providers.other\]/);
  assert.match(text, /name = "keep"/);
});

test('rejects incomplete managed blocks', () => {
  assert.throws(
    () =>
      buildConfigToml({
        existingText: [
          '# BEGIN new-api-codex-bridge settings',
          'model = "old"',
          '',
          '[profiles.dev]',
          'model = "keep"',
          '',
        ].join('\n'),
        defaultModel: 'gpt-5.5',
        providerBaseUrl: 'https://example.com/v1',
        apiKey: 'test-key',
      }),
    /已有受管理块不完整/
  );
});

test('replaces managed provider headers with inline comments', () => {
  const text = buildConfigToml({
    existingText: [
      '[model_providers.new-api] # old bridge provider',
      'name = "old"',
      '',
      '[model_providers.other] # keep sibling',
      'name = "keep"',
      '',
    ].join('\n'),
    defaultModel: 'gpt-5.5',
    providerBaseUrl: 'https://example.com/v1',
    apiKey: 'test-key',
  });

  assert.doesNotMatch(text, /name = "old"/);
  assert.match(text, /\[model_providers.other\] # keep sibling/);
  assert.match(text, /name = "keep"/);
});

test('rejects empty TOML table headers while replacing provider config', () => {
  assert.throws(
    () =>
      buildConfigToml({
        existingText: [
          '[model_providers.new-api]',
          'name = "old"',
          '',
          '[[ ]]',
          'name = "broken"',
          '',
        ].join('\n'),
        defaultModel: 'gpt-5.5',
        providerBaseUrl: 'https://example.com/v1',
        apiKey: 'test-key',
      }),
    /TOML 表头名称不能为空/
  );
});
