import test from 'node:test';
import assert from 'node:assert/strict';
import { mock } from 'node:test';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import {
  AUTH_FILE,
  CATALOG_FILE,
  CONFIG_FILE,
  RECEIPT_FILE,
  TOOL_ID,
} from '../src/constants.js';
import { runCli } from '../src/cli.js';
import { runDoctor } from '../src/doctor.js';
import { atomicWrite, createBackup, pathExists, restoreBackup } from '../src/files.js';
import { createSetupPlan, writeSetupPlan } from '../src/setup.js';

test('setup writes provider-token config without touching auth.json', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const fetchImpl = okFetch(['gpt-5.5', 'gpt-5.4']);
  const authPath = path.join(codexHome, AUTH_FILE);
  const originalReadFile = fs.readFile;
  mock.method(fs, 'readFile', async (filePath, ...args) => {
    if (String(filePath) === authPath) {
      throw new Error('auth.json should not be read in provider-token mode');
    }
    return originalReadFile.call(fs, filePath, ...args);
  });
  let plan;
  try {
    plan = await createSetupPlan({
      codexHome,
      baseUrl: 'https://example.com',
      apiKey: 'test-key',
      defaultModel: 'gpt-5.5',
      fetchImpl,
    });
  } finally {
    mock.restoreAll();
  }

  assert.equal(plan.selectionMode, 'all');
  assert.equal(plan.authMode, 'provider-token');
  assert.equal(Object.hasOwn(plan.files, AUTH_FILE), false);
  const backupId = await writeSetupPlan(plan);

  const config = await fs.readFile(path.join(codexHome, CONFIG_FILE), 'utf8');
  const catalog = JSON.parse(await fs.readFile(path.join(codexHome, CATALOG_FILE), 'utf8'));
  const receipt = JSON.parse(await fs.readFile(path.join(codexHome, RECEIPT_FILE), 'utf8'));

  assert.match(config, /experimental_bearer_token = "test-key"/);
  assert.equal(catalog.models.length, 2);
  assert.equal(receipt.backupId, backupId);
  assert.match(backupId, /^\d{8}-\d{6}\d{3}Z-[0-9a-f]{6}$/);
  assert.equal(receipt.authMode, 'provider-token');
  assert.equal(await fileExists(path.join(codexHome, AUTH_FILE)), false);
  if (process.platform !== 'win32') {
    const stat = await fs.stat(path.join(codexHome, CONFIG_FILE));
    assert.equal(stat.mode & 0o777, 0o600);
  }

  const doctor = await runDoctor(codexHome);
  assert.equal(doctor.ok, true);
});

test('setup preserves existing auth.json in provider-token mode', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const authPath = path.join(codexHome, AUTH_FILE);
  const authText = '{"OPENAI_API_KEY":"official-key","other":"keep"}\n';
  await fs.writeFile(authPath, authText);

  const plan = await createSetupPlan({
    codexHome,
    baseUrl: 'https://example.com',
    apiKey: 'test-key',
    defaultModel: 'gpt-5.5',
    fetchImpl: okFetch(['gpt-5.5']),
  });

  assert.equal(Object.hasOwn(plan.files, AUTH_FILE), false);
  await writeSetupPlan(plan);

  assert.equal(await fs.readFile(authPath, 'utf8'), authText);
  const doctor = await runDoctor(codexHome);
  assert.equal(doctor.ok, true);
});

test('setup can write auth-json mode and restore absence', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const plan = await createSetupPlan({
    codexHome,
    baseUrl: 'https://example.com/v1',
    apiKey: 'test-key',
    authMode: 'auth-json',
    defaultModel: 'gpt-5.5',
    selectedModels: ['gpt-5.5'],
    fetchImpl: okFetch(['gpt-5.5']),
  });

  const backupId = await writeSetupPlan(plan);
  assert.equal(await fileExists(path.join(codexHome, AUTH_FILE)), true);
  await restoreBackup(codexHome, backupId);

  assert.equal(await fileExists(path.join(codexHome, CONFIG_FILE)), false);
  assert.equal(await fileExists(path.join(codexHome, AUTH_FILE)), false);
  assert.equal(await fileExists(path.join(codexHome, CATALOG_FILE)), false);
});

test('restore keeps config restrictive when backup contains provider token', async () => {
  if (process.platform === 'win32') {
    return;
  }
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const configPath = path.join(codexHome, CONFIG_FILE);
  await fs.writeFile(
    configPath,
    [
      '[model_providers.new-api]',
      'experimental_bearer_token = "old-key"',
      '',
    ].join('\n'),
    { mode: 0o644 },
  );
  const { backupId } = await createBackup(codexHome);
  const backupConfigPath = path.join(codexHome, 'backups', 'new-api-codex-bridge', backupId, CONFIG_FILE);
  const backupRootStat = await fs.stat(path.join(codexHome, 'backups', 'new-api-codex-bridge'));
  const backupDirStat = await fs.stat(path.dirname(backupConfigPath));
  const backupConfigStat = await fs.stat(backupConfigPath);
  assert.equal(backupRootStat.mode & 0o777, 0o700);
  assert.equal(backupDirStat.mode & 0o777, 0o700);
  assert.equal(backupConfigStat.mode & 0o777, 0o600);

  await fs.writeFile(configPath, 'model = "changed"\n', { mode: 0o644 });
  await restoreBackup(codexHome, backupId);

  const restored = await fs.readFile(configPath, 'utf8');
  const stat = await fs.stat(configPath);
  assert.match(restored, /experimental_bearer_token = "old-key"/);
  assert.equal(stat.mode & 0o777, 0o600);
});

test('create backup removes partial backup directory when copy fails', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  await fs.writeFile(path.join(codexHome, CONFIG_FILE), 'model = "safe"\n');

  mock.method(fs, 'copyFile', async () => {
    throw new Error('copy failed');
  });

  try {
    await assert.rejects(() => createBackup(codexHome), /copy failed/);
    const backupRoot = path.join(codexHome, 'backups', 'new-api-codex-bridge');
    const backups = await fs.readdir(backupRoot).catch((error) => {
      if (error?.code === 'ENOENT') {
        return [];
      }
      throw error;
    });
    assert.deepEqual(backups, []);
  } finally {
    mock.restoreAll();
  }
});

test('pathExists only treats ENOENT as missing', async () => {
  const denied = Object.assign(new Error('permission denied'), { code: 'EACCES' });
  mock.method(fs, 'access', async (filePath) => {
    if (String(filePath).endsWith('missing')) {
      throw Object.assign(new Error('missing'), { code: 'ENOENT' });
    }
    throw denied;
  });

  try {
    assert.equal(await pathExists('/tmp/missing'), false);
    await assert.rejects(() => pathExists('/tmp/denied'), /permission denied/);
  } finally {
    mock.restoreAll();
  }
});

test('write setup plan surfaces restore failures', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  await fs.writeFile(path.join(codexHome, CONFIG_FILE), 'model = "old"\n');
  const originalWriteFile = fs.writeFile;
  const originalReadFile = fs.readFile;
  let writeFileCount = 0;

  mock.method(fs, 'writeFile', async (...args) => {
    writeFileCount += 1;
    if (writeFileCount === 2) {
      throw new Error('catalog failed');
    }
    return originalWriteFile.apply(fs, args);
  });
  mock.method(fs, 'readFile', async (filePath, ...args) => {
    if (String(filePath).endsWith(CONFIG_FILE)) {
      throw new Error('restore failed');
    }
    return originalReadFile.call(fs, filePath, ...args);
  });

  try {
    await assert.rejects(
      () =>
        writeSetupPlan({
          codexHome,
          providerBaseUrl: 'https://example.com/v1',
          defaultModel: 'gpt-5.5',
          selectedModels: ['gpt-5.5'],
          selectionMode: 'all',
          authMode: 'provider-token',
          envKey: 'OPENAI_API_KEY',
          sensitiveConfig: true,
          files: {
            [CONFIG_FILE]: 'model = "new"\n',
            [CATALOG_FILE]: '{"models":[]}\n',
            [RECEIPT_FILE]: `${JSON.stringify({ tool: TOOL_ID })}\n`,
          },
        }),
      /回滚失败/,
    );
  } finally {
    mock.restoreAll();
  }
});

test('restore rejects backup ids outside the managed backup directory', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  await fs.writeFile(path.join(codexHome, CONFIG_FILE), 'model = "safe"\n');

  await assert.rejects(
    () => restoreBackup(codexHome, '../../../outside'),
    /备份 ID 无效/,
  );

  assert.equal(await fs.readFile(path.join(codexHome, CONFIG_FILE), 'utf8'), 'model = "safe"\n');
});

test('restore rolls back target files when applying backup fails', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const configPath = path.join(codexHome, CONFIG_FILE);
  const authPath = path.join(codexHome, AUTH_FILE);
  await fs.writeFile(configPath, 'model = "current"\n');
  const { backupId } = await createBackup(codexHome);
  await fs.writeFile(configPath, 'model = "changed"\n');
  await fs.writeFile(authPath, '{"changed":true}\n');

  const originalRm = fs.rm;
  mock.method(fs, 'rm', async (filePath, ...args) => {
    if (String(filePath) === authPath) {
      throw new Error('remove auth failed');
    }
    return originalRm.call(fs, filePath, ...args);
  });

  try {
    await assert.rejects(() => restoreBackup(codexHome, backupId), /remove auth failed/);
  } finally {
    mock.restoreAll();
  }

  assert.equal(await fs.readFile(configPath, 'utf8'), 'model = "changed"\n');
  assert.equal(await fs.readFile(authPath, 'utf8'), '{"changed":true}\n');
});

test('restore reports apply and rollback failures together', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const configPath = path.join(codexHome, CONFIG_FILE);
  const authPath = path.join(codexHome, AUTH_FILE);
  await fs.writeFile(configPath, 'model = "current"\n');
  const { backupId } = await createBackup(codexHome);
  await fs.writeFile(configPath, 'model = "changed"\n');
  await fs.writeFile(authPath, '{"changed":true}\n');

  const originalRm = fs.rm;
  const originalWriteFile = fs.writeFile;
  let restoring = false;
  mock.method(fs, 'rm', async (filePath, ...args) => {
    if (String(filePath) === authPath) {
      restoring = true;
      throw new Error('remove auth failed');
    }
    return originalRm.call(fs, filePath, ...args);
  });
  mock.method(fs, 'writeFile', async (filePath, ...args) => {
    if (restoring && String(filePath).includes(`${CONFIG_FILE}.`)) {
      throw new Error('rollback config failed');
    }
    return originalWriteFile.call(fs, filePath, ...args);
  });

  try {
    await assert.rejects(
      () => restoreBackup(codexHome, backupId),
      /恢复失败（remove auth failed），且恢复回滚失败（config\.toml: rollback config failed）/,
    );
  } finally {
    mock.restoreAll();
  }
});

test('atomic write cleans up temp files when rename fails', async () => {
  const tempDir = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const targetFile = path.join(tempDir, 'config.toml');
  const originalRename = fs.rename;
  let seenTempPath = '';
  mock.method(fs, 'rename', async (from, to) => {
    seenTempPath = from;
    if (to === targetFile) {
      throw new Error('rename failed');
    }
    return originalRename.call(fs, from, to);
  });

  try {
    await assert.rejects(() => atomicWrite(targetFile, 'content'), /rename failed/);
    assert.equal(seenTempPath.endsWith('.tmp'), true);
    const files = await fs.readdir(tempDir);
    assert.equal(files.some((file) => file.endsWith('.tmp')), false);
  } finally {
    mock.restoreAll();
  }
});

test('atomic write applies restrictive mode to temp file before rename', async () => {
  if (process.platform === 'win32') {
    return;
  }
  const tempDir = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const targetFile = path.join(tempDir, 'config.toml');
  const originalRename = fs.rename;
  let tempMode = null;
  mock.method(fs, 'rename', async (from, to) => {
    if (to === targetFile) {
      const stat = await fs.stat(from);
      tempMode = stat.mode & 0o777;
    }
    return originalRename.call(fs, from, to);
  });

  try {
    await atomicWrite(targetFile, 'secret', { mode: 0o600 });
    assert.equal(tempMode, 0o600);
  } finally {
    mock.restoreAll();
  }
});

test('write setup plan restores backup when a later write fails', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const configPath = path.join(codexHome, CONFIG_FILE);
  await fs.writeFile(configPath, 'model = "old"\n');
  const originalWriteFile = fs.writeFile;
  let writeFileCount = 0;

  mock.method(fs, 'writeFile', async (...args) => {
    writeFileCount += 1;
    if (writeFileCount === 2) {
      throw new Error('catalog failed');
    }
    return originalWriteFile.apply(fs, args);
  });

  try {
    await assert.rejects(
      () =>
        writeSetupPlan({
          codexHome,
          providerBaseUrl: 'https://example.com/v1',
          defaultModel: 'gpt-5.5',
          selectedModels: ['gpt-5.5'],
          selectionMode: 'all',
          authMode: 'provider-token',
          envKey: 'OPENAI_API_KEY',
          sensitiveConfig: true,
          files: {
            [CONFIG_FILE]: 'model = "new"\n',
            [CATALOG_FILE]: '{"models":[]}\n',
            [RECEIPT_FILE]: `${JSON.stringify({ tool: TOOL_ID })}\n`,
          },
        }),
      /catalog failed/,
    );
    assert.equal(await fs.readFile(configPath, 'utf8'), 'model = "old"\n');
  } finally {
    mock.restoreAll();
  }
});

test('rejects selected models not returned by new-api', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  await assert.rejects(
    () => createSetupPlan({
      codexHome,
      baseUrl: 'https://example.com',
      apiKey: 'test-key',
      selectedModels: ['missing-model'],
      fetchImpl: okFetch(['gpt-5.5']),
    }),
    /所选模型/,
  );
});

test('rejects empty explicit selected models', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  await assert.rejects(
    () => createSetupPlan({
      codexHome,
      baseUrl: 'https://example.com',
      apiKey: 'test-key',
      selectedModels: ['   '],
      fetchImpl: okFetch(['gpt-5.5']),
    }),
    /所选模型不能为空/,
  );
});

test('rejects empty discovered model lists', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  await assert.rejects(
    () =>
      createSetupPlan({
        codexHome,
        baseUrl: 'https://example.com',
        apiKey: 'test-key',
        discovery: {
          providerBaseUrl: 'https://example.com/v1',
          modelIds: [],
        },
      }),
    /未发现可用模型/,
  );
});

test('create setup plan reuses supplied model discovery', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const plan = await createSetupPlan({
    codexHome,
    baseUrl: 'https://unused.example.com',
    apiKey: 'test-key',
    defaultModel: 'gpt-5.5',
    fetchImpl: async () => {
      throw new Error('fetch should not be called');
    },
    discovery: {
      providerBaseUrl: 'https://example.com/v1',
      modelIds: ['gpt-5.5', 'gpt-5.4'],
    },
  });

  assert.equal(plan.providerBaseUrl, 'https://example.com/v1');
  assert.deepEqual(plan.discoveredModels, ['gpt-5.5', 'gpt-5.4']);
  assert.deepEqual(plan.selectedModels, ['gpt-5.5', 'gpt-5.4']);
});

test('setup resolves env auth mode using the default env key', async () => {
  const codexHome = await fs.mkdtemp(path.join(os.tmpdir(), 'codex-bridge-'));
  const output = [];
  await runCli([
    'print',
    '--base-url',
    'https://example.com',
    '--auth-mode',
    'env',
    '--codex-home',
    codexHome,
    '--default-model',
    'gpt-5.5',
    '--all-models',
  ], {
    env: { OPENAI_API_KEY: 'test-key' },
    stdin: { isTTY: false },
    stdout: { write: (chunk) => output.push(String(chunk)) },
    stderr: { write: (chunk) => output.push(String(chunk)) },
    fetchImpl: okFetch(['gpt-5.5']),
  });

  const text = output.join('');
  assert.match(text, /env_key = "OPENAI_API_KEY"/);
  assert.doesNotMatch(text, /test-key/);
});

function okFetch(models) {
  return async () => ({
    ok: true,
    status: 200,
    text: async () => JSON.stringify({ data: models.map((id) => ({ id })) }),
  });
}

async function fileExists(filePath) {
  try {
    await fs.access(filePath);
    return true;
  } catch {
    return false;
  }
}
