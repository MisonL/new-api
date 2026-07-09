import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { CATALOG_FILE, CONFIG_FILE, RECEIPT_FILE } from '../src/constants.js';
import { runDoctor } from '../src/doctor.js';
import { refreshFromReceipt } from '../src/refresh.js';
import { createSetupPlan, writeSetupPlan } from '../src/setup.js';

test('doctor reports missing files', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const result = await runDoctor(codexHome);

  assert.equal(result.ok, false);
  assert.ok(result.problems.includes(`缺少 ${CONFIG_FILE}`));
  assert.ok(result.problems.includes(`缺少 ${CATALOG_FILE}`));
  assert.ok(result.problems.includes(`缺少 ${RECEIPT_FILE}`));
});

test('refresh updates all-model receipts without storing key in receipt', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const plan = await createSetupPlan({
    codexHome,
    baseUrl: 'https://example.com',
    apiKey: 'test-key',
    defaultModel: 'gpt-5.5',
    fetchImpl: okFetch(['gpt-5.5']),
  });
  await writeSetupPlan(plan);

  const refreshed = await refreshFromReceipt({
    codexHome,
    fetchImpl: okFetch(['gpt-5.5', 'gpt-5.4']),
  });

  const catalog = JSON.parse(await fs.readFile(path.join(codexHome, CATALOG_FILE), 'utf8'));
  const config = await fs.readFile(path.join(codexHome, CONFIG_FILE), 'utf8');
  const receiptText = await fs.readFile(path.join(codexHome, 'new-api-codex-bridge-receipt.json'), 'utf8');

  assert.deepEqual(catalog.models.map((model) => model.slug), ['gpt-5.5', 'gpt-5.4']);
  assert.equal(refreshed.plan.selectionMode, 'all');
  assert.match(config, /experimental_bearer_token = "test-key"/);
  assert.doesNotMatch(receiptText, /test-key/);
});

test('doctor reports malformed receipt and model catalog mismatches', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const plan = await createSetupPlan({
    codexHome,
    baseUrl: 'https://example.com',
    apiKey: 'test-key',
    authMode: 'auth-json',
    defaultModel: 'gpt-5.5',
    selectedModels: ['gpt-5.5'],
    fetchImpl: okFetch(['gpt-5.5']),
  });
  await writeSetupPlan(plan);

  await fs.writeFile(
    path.join(codexHome, 'new-api-codex-bridge-receipt.json'),
    '{',
  );
  const malformed = await runDoctor(codexHome);
  assert.equal(malformed.ok, false);
  assert.match(malformed.problems.join('\n'), /receipt 不是有效 JSON/);

  await writeSetupPlan(plan);
  await fs.writeFile(
    path.join(codexHome, CATALOG_FILE),
    JSON.stringify({ models: [{ slug: 'tampered', base_instructions: 'x' }] }, null, 2) + '\n',
  );
  const tampered = await runDoctor(codexHome);
  assert.equal(tampered.ok, false);
  assert.match(tampered.problems.join('\n'), /默认模型.*不在模型目录/);
});

test('doctor reports invalid model catalog entries without treating JSON as malformed', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const plan = await createSetupPlan({
    codexHome,
    baseUrl: 'https://example.com',
    apiKey: 'test-key',
    defaultModel: 'gpt-5.5',
    fetchImpl: okFetch(['gpt-5.5']),
  });
  await writeSetupPlan(plan);
  await fs.writeFile(
    path.join(codexHome, CATALOG_FILE),
    JSON.stringify({ models: [null, { slug: 'gpt-5.5' }] }, null, 2) + '\n',
  );

  const result = await runDoctor(codexHome);

  assert.equal(result.ok, false);
  assert.match(result.problems.join('\n'), /缺少 Codex 必要字段/);
  assert.doesNotMatch(result.problems.join('\n'), /模型目录不是有效 JSON/);
});

test('doctor rejects receipts with mismatched provider metadata', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const plan = await createSetupPlan({
    codexHome,
    baseUrl: 'https://example.com',
    apiKey: 'test-key',
    authMode: 'auth-json',
    defaultModel: 'gpt-5.5',
    selectedModels: ['gpt-5.5'],
    fetchImpl: okFetch(['gpt-5.5']),
  });
  await writeSetupPlan(plan);

  const receiptPath = path.join(codexHome, 'new-api-codex-bridge-receipt.json');
  const receipt = JSON.parse(await fs.readFile(receiptPath, 'utf8'));
  receipt.providerId = 'other-provider';
  await fs.writeFile(receiptPath, JSON.stringify(receipt, null, 2) + '\n');

  const result = await runDoctor(codexHome);
  assert.equal(result.ok, false);
  assert.match(result.problems.join('\n'), /providerId 与 new-api 不一致/);
});

test('refresh reports malformed auth.json clearly', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const plan = await createSetupPlan({
    codexHome,
    baseUrl: 'https://example.com',
    apiKey: 'test-key',
    authMode: 'auth-json',
    defaultModel: 'gpt-5.5',
    selectedModels: ['gpt-5.5'],
    fetchImpl: okFetch(['gpt-5.5']),
  });
  await writeSetupPlan(plan);
  await fs.writeFile(path.join(codexHome, 'auth.json'), '{');

  await assert.rejects(
    () => refreshFromReceipt({ codexHome, fetchImpl: okFetch(['gpt-5.5']) }),
    /auth\.json 不是有效 JSON/,
  );
});

test('doctor reads auth settings only from the new-api provider table', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const plan = await createSetupPlan({
    codexHome,
    baseUrl: 'https://example.com',
    apiKey: 'test-key',
    authMode: 'env',
    envKey: 'NEW_API_KEY',
    defaultModel: 'gpt-5.5',
    fetchImpl: okFetch(['gpt-5.5']),
  });
  await writeSetupPlan(plan);

  const configPath = path.join(codexHome, CONFIG_FILE);
  const config = await fs.readFile(configPath, 'utf8');
  await fs.writeFile(
    configPath,
    `${config.replace('env_key = "NEW_API_KEY"', '')}\n[model_providers.other]\nenv_key = "NEW_API_KEY"\n`,
  );

  const result = await runDoctor(codexHome);
  assert.equal(result.ok, false);
  assert.match(result.problems.join('\n'), /config.toml 不包含 env_key/);
});

test('doctor does not accept commented model catalog settings', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const plan = await createSetupPlan({
    codexHome,
    baseUrl: 'https://example.com',
    apiKey: 'test-key',
    defaultModel: 'gpt-5.5',
    fetchImpl: okFetch(['gpt-5.5']),
  });
  await writeSetupPlan(plan);

  const configPath = path.join(codexHome, CONFIG_FILE);
  const config = await fs.readFile(configPath, 'utf8');
  await fs.writeFile(
    configPath,
    config.replace(
      'model_catalog_json = "new-api-model-catalog.json"',
      '# model_catalog_json = "new-api-model-catalog.json"',
    ),
  );

  const result = await runDoctor(codexHome);
  assert.equal(result.ok, false);
  assert.match(result.problems.join('\n'), /new-api 模型目录/);
});

test('doctor only accepts top-level model config assignments', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const plan = await createSetupPlan({
    codexHome,
    baseUrl: 'https://example.com',
    apiKey: 'test-key',
    defaultModel: 'gpt-5.5',
    fetchImpl: okFetch(['gpt-5.5']),
  });
  await writeSetupPlan(plan);

  const configPath = path.join(codexHome, CONFIG_FILE);
  const config = await fs.readFile(configPath, 'utf8');
  await fs.writeFile(
    configPath,
    `${config
      .replace('model_provider = "new-api"', 'model_provider = "old-provider"')
      .replace(
        'model_catalog_json = "new-api-model-catalog.json"',
        'model_catalog_json = "other-catalog.json"',
      )}\n[profiles.dev]\nmodel_provider = "new-api"\nmodel_catalog_json = "new-api-model-catalog.json"\n`,
  );

  const result = await runDoctor(codexHome);
  assert.equal(result.ok, false);
  assert.match(result.problems.join('\n'), /model_provider = "new-api"/);
  assert.match(result.problems.join('\n'), /new-api 模型目录/);
});

test('doctor ignores model config assignments inside quoted dotted tables', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const plan = await createSetupPlan({
    codexHome,
    baseUrl: 'https://example.com',
    apiKey: 'test-key',
    defaultModel: 'gpt-5.5',
    fetchImpl: okFetch(['gpt-5.5']),
  });
  await writeSetupPlan(plan);

  const configPath = path.join(codexHome, CONFIG_FILE);
  const config = await fs.readFile(configPath, 'utf8');
  await fs.writeFile(
    configPath,
    `${config
      .replace('model_provider = "new-api"', 'model_provider = "old-provider"')
      .replace(
        'model_catalog_json = "new-api-model-catalog.json"',
        'model_catalog_json = "other-catalog.json"',
      )}\n[profiles."dev.env"]\nmodel_provider = "new-api"\nmodel_catalog_json = "new-api-model-catalog.json"\n`,
  );

  const result = await runDoctor(codexHome);
  assert.equal(result.ok, false);
  assert.match(result.problems.join('\n'), /model_provider = "new-api"/);
  assert.match(result.problems.join('\n'), /new-api 模型目录/);
});

test('doctor keeps scanning top-level assignments after array values', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const plan = await createSetupPlan({
    codexHome,
    baseUrl: 'https://example.com',
    apiKey: 'test-key',
    defaultModel: 'gpt-5.5',
    fetchImpl: okFetch(['gpt-5.5']),
  });
  await writeSetupPlan(plan);

  const configPath = path.join(codexHome, CONFIG_FILE);
  const config = await fs.readFile(configPath, 'utf8');
  await fs.writeFile(
    configPath,
    `enabled_features = [\n  "shell",\n]\n${config}`,
  );

  const result = await runDoctor(codexHome);
  assert.equal(result.ok, true);
});

test('doctor reports every selected receipt model missing from catalog', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const plan = await createSetupPlan({
    codexHome,
    baseUrl: 'https://example.com',
    apiKey: 'test-key',
    defaultModel: 'gpt-5.5',
    selectedModels: ['gpt-5.5', 'gpt-5.4'],
    fetchImpl: okFetch(['gpt-5.5', 'gpt-5.4']),
  });
  await writeSetupPlan(plan);

  await fs.writeFile(
    path.join(codexHome, CATALOG_FILE),
    JSON.stringify({ models: [{ slug: 'gpt-5.5', base_instructions: 'x' }] }, null, 2) + '\n',
  );

  const result = await runDoctor(codexHome);
  assert.equal(result.ok, false);
  assert.match(result.problems.join('\n'), /模型目录缺少回执中的模型：gpt-5\.4/);
});

test('doctor reports invalid provider token TOML without throwing', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const plan = await createSetupPlan({
    codexHome,
    baseUrl: 'https://example.com',
    apiKey: 'test-key',
    defaultModel: 'gpt-5.5',
    fetchImpl: okFetch(['gpt-5.5']),
  });
  await writeSetupPlan(plan);

  const configPath = path.join(codexHome, CONFIG_FILE);
  const config = await fs.readFile(configPath, 'utf8');
  await fs.writeFile(
    configPath,
    config.replace('experimental_bearer_token = "test-key"', 'experimental_bearer_token = "bad\\x41"'),
  );

  const result = await runDoctor(codexHome);
  assert.equal(result.ok, false);
  assert.match(result.problems.join('\n'), /experimental_bearer_token 不是有效 TOML 字符串/);
});

test('doctor flags auth-json configs missing requires_openai_auth', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const plan = await createSetupPlan({
    codexHome,
    baseUrl: 'https://example.com',
    apiKey: 'test-key',
    authMode: 'auth-json',
    defaultModel: 'gpt-5.5',
    selectedModels: ['gpt-5.5'],
    fetchImpl: okFetch(['gpt-5.5']),
  });
  await writeSetupPlan(plan);

  const configPath = path.join(codexHome, CONFIG_FILE);
  const config = await fs.readFile(configPath, 'utf8');
  await fs.writeFile(configPath, config.replace('requires_openai_auth = true\n', ''));

  const result = await runDoctor(codexHome);
  assert.equal(result.ok, false);
  assert.match(result.problems.join('\n'), /requires_openai_auth = true/);
});

function okFetch(models) {
  return async () => ({
    ok: true,
    status: 200,
    text: async () => JSON.stringify({ data: models.map((id) => ({ id })) }),
  });
}
