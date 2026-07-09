import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { runCli } from '../src/cli.js';

test('print redacts provider-token config', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const output = [];
  await runCli([
    'print',
    '--base-url',
    'https://example.com',
    '--api-key-env',
    'NEW_API_KEY',
    '--codex-home',
    codexHome,
    '--default-model',
    'gpt-5.5',
    '--all-models',
  ], {
    env: { NEW_API_KEY: 'test-key' },
    stdin: { isTTY: false },
    stdout: { write: (chunk) => output.push(String(chunk)) },
    stderr: { write: (chunk) => output.push(String(chunk)) },
    fetchImpl: async () => ({
      ok: true,
      status: 200,
      text: async () => JSON.stringify({ data: [{ id: 'gpt-5.5' }] }),
    }),
  });

  const text = output.join('');
  assert.match(text, /\[已脱敏\]/);
  assert.doesNotMatch(text, /test-key/);
});

test('help is available through short option', async () => {
  const output = [];
  await runCli(['-h'], {
    env: {},
    stdin: { isTTY: false },
    stdout: { write: (chunk) => output.push(String(chunk)) },
    stderr: { write: (chunk) => output.push(String(chunk)) },
  });

  assert.match(output.join(''), /用法：new-api-codex-bridge/);
});

test('version is available through long option', async () => {
  const output = [];
  await runCli(['--version'], {
    env: {},
    stdin: { isTTY: false },
    stdout: { write: (chunk) => output.push(String(chunk)) },
    stderr: { write: (chunk) => output.push(String(chunk)) },
  });

  assert.match(output.join(''), /^\d+\.\d+\.\d+/m);
});

test('version is available through subcommand', async () => {
  const output = [];
  await runCli(['version'], {
    env: {},
    stdin: { isTTY: false },
    stdout: { write: (chunk) => output.push(String(chunk)) },
    stderr: { write: (chunk) => output.push(String(chunk)) },
  });

  assert.match(output.join(''), /^\d+\.\d+\.\d+/m);
});

test('setup deduplicates repeated model selections', async () => {
  const output = [];
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  await runCli([
    'setup',
    '--base-url',
    'https://example.com',
    '--api-key-env',
    'NEW_API_KEY',
    '--codex-home',
    codexHome,
    '--default-model',
    'gpt-5.5',
    '--model',
    'gpt-5.5,gpt-5.5',
    '--yes',
  ], {
    env: { NEW_API_KEY: 'test-key' },
    stdin: { isTTY: false },
    stdout: { write: (chunk) => output.push(String(chunk)) },
    stderr: { write: (chunk) => output.push(String(chunk)) },
    fetchImpl: async () => ({
      ok: true,
      status: 200,
      text: async () => JSON.stringify({ data: [{ id: 'gpt-5.5' }] }),
    }),
  });

  const receipt = JSON.parse(
    await fs.readFile(path.join(codexHome, 'new-api-codex-bridge-receipt.json'), 'utf8'),
  );
  assert.deepEqual(receipt.selectedModels, ['gpt-5.5']);
});

test('setup rejects missing values for flag options', async () => {
  const output = [];
  await assert.rejects(
    () =>
      runCli(['setup', '--base-url', '--yes'], {
        env: {},
        stdin: { isTTY: false },
        stdout: { write: (chunk) => output.push(String(chunk)) },
        stderr: { write: (chunk) => output.push(String(chunk)) },
      }),
    /--base-url 需要参数/,
  );
});

test('inline option values preserve equals signs', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const output = [];
  let authorization = '';
  let requestedUrl = '';
  await runCli([
    'print',
    '--base-url=https://example.com/root?ignored=1',
    '--api-key=abc=def==',
    '--codex-home',
    codexHome,
    '--default-model',
    'gpt-5.5',
    '--all-models',
  ], {
    env: {},
    stdin: { isTTY: false },
    stdout: { write: (chunk) => output.push(String(chunk)) },
    stderr: { write: (chunk) => output.push(String(chunk)) },
    fetchImpl: async (url, init) => {
      requestedUrl = url;
      authorization = init.headers.Authorization;
      return {
        ok: true,
        status: 200,
        text: async () => JSON.stringify({ data: [{ id: 'gpt-5.5' }] }),
      };
    },
  });

  assert.equal(authorization, 'Bearer abc=def==');
  assert.equal(requestedUrl, 'https://example.com/root/v1/models');
  assert.match(output.join(''), /\[已脱敏\]/);
});

test('env auth uses explicit credentials for discovery and persists env key for runtime', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const output = [];
  let authorization = '';
  await runCli([
    'print',
    '--base-url',
    'https://example.com',
    '--auth-mode',
    'env',
    '--api-key-env',
    'DISCOVERY_KEY',
    '--env-key',
    'RUNTIME_KEY',
    '--codex-home',
    codexHome,
    '--default-model',
    'gpt-5.5',
    '--all-models',
  ], {
    env: {
      DISCOVERY_KEY: 'discovery-token',
      RUNTIME_KEY: 'runtime-token',
    },
    stdin: { isTTY: false },
    stdout: { write: (chunk) => output.push(String(chunk)) },
    stderr: { write: (chunk) => output.push(String(chunk)) },
    fetchImpl: async (_url, init) => {
      authorization = init.headers.Authorization;
      return {
        ok: true,
        status: 200,
        text: async () => JSON.stringify({ data: [{ id: 'gpt-5.5' }] }),
      };
    },
  });

  assert.equal(authorization, 'Bearer discovery-token');
  assert.match(output.join(''), /env_key = "RUNTIME_KEY"/);
});

test('env auth uses inline api key for discovery and persists env key for runtime', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const output = [];
  let authorization = '';
  await runCli([
    'print',
    '--base-url',
    'https://example.com',
    '--auth-mode',
    'env',
    '--api-key',
    'inline-discovery-token',
    '--env-key',
    'RUNTIME_KEY',
    '--codex-home',
    codexHome,
    '--default-model',
    'gpt-5.5',
    '--all-models',
  ], {
    env: {
      RUNTIME_KEY: 'runtime-token',
    },
    stdin: { isTTY: false },
    stdout: { write: (chunk) => output.push(String(chunk)) },
    stderr: { write: (chunk) => output.push(String(chunk)) },
    fetchImpl: async (_url, init) => {
      authorization = init.headers.Authorization;
      return {
        ok: true,
        status: 200,
        text: async () => JSON.stringify({ data: [{ id: 'gpt-5.5' }] }),
      };
    },
  });

  assert.equal(authorization, 'Bearer inline-discovery-token');
  assert.match(output.join(''), /env_key = "RUNTIME_KEY"/);
});

test('inline api key values may start with a dash', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  let authorization = '';
  await runCli([
    'print',
    '--base-url',
    'https://example.com',
    '--api-key=-dash-token',
    '--codex-home',
    codexHome,
    '--default-model',
    'gpt-5.5',
    '--all-models',
  ], {
    env: {},
    stdin: { isTTY: false },
    stdout: { write: () => {} },
    stderr: { write: () => {} },
    fetchImpl: async (_url, init) => {
      authorization = init.headers.Authorization;
      return {
        ok: true,
        status: 200,
        text: async () => JSON.stringify({ data: [{ id: 'gpt-5.5' }] }),
      };
    },
  });

  assert.equal(authorization, 'Bearer -dash-token');
});
