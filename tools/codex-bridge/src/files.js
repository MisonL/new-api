import crypto from 'node:crypto';
import fs from 'node:fs/promises';
import path from 'node:path';
import {
  AUTH_FILE,
  BACKUP_ROOT,
  CATALOG_FILE,
  CONFIG_FILE,
  RECEIPT_FILE,
} from './constants.js';

export async function readTextIfExists(filePath) {
  try {
    return await fs.readFile(filePath, 'utf8');
  } catch (error) {
    if (error && error.code === 'ENOENT') {
      return '';
    }
    throw error;
  }
}

export async function pathExists(filePath) {
  try {
    await fs.access(filePath);
    return true;
  } catch (error) {
    if (error && error.code === 'ENOENT') {
      return false;
    }
    throw error;
  }
}

export async function createBackup(codexHome) {
  const backupRoot = path.join(codexHome, BACKUP_ROOT);
  await fs.mkdir(backupRoot, { recursive: true, mode: 0o700 });
  await chmodIfSupported(backupRoot, 0o700);
  const { backupId, backupDir } = await createUniqueBackupDir(backupRoot);
  try {
    for (const fileName of [CONFIG_FILE, AUTH_FILE, CATALOG_FILE, RECEIPT_FILE]) {
      const source = path.join(codexHome, fileName);
      if (await pathExists(source)) {
        const target = path.join(backupDir, fileName);
        await fs.copyFile(source, target);
        await chmodIfSupported(target, backupMode(fileName));
      }
    }
    return { backupId, backupDir };
  } catch (error) {
    await fs.rm(backupDir, { recursive: true, force: true });
    throw error;
  }
}

async function createUniqueBackupDir(backupRoot) {
  for (let attempt = 0; attempt < 10; attempt += 1) {
    const backupId = `${timestampId()}-${crypto.randomBytes(3).toString('hex')}`;
    const backupDir = path.join(backupRoot, backupId);
    try {
      await fs.mkdir(backupDir, { mode: 0o700 });
      await chmodIfSupported(backupDir, 0o700);
      return { backupId, backupDir };
    } catch (error) {
      if (error && error.code === 'EEXIST') {
        continue;
      }
      throw error;
    }
  }
  throw new Error('无法创建唯一备份目录');
}

export async function atomicWrite(filePath, content, { mode } = {}) {
  await fs.mkdir(path.dirname(filePath), { recursive: true });
  const tempPath = `${filePath}.${process.pid}.${Date.now()}-${crypto.randomBytes(6).toString('hex')}.tmp`;
  let renamed = false;
  try {
    await fs.writeFile(tempPath, content, mode ? { mode } : undefined);
    if (mode) {
      await fs.chmod(tempPath, mode);
    }
    await fs.rename(tempPath, filePath);
    renamed = true;
    if (mode) {
      await fs.chmod(filePath, mode);
    }
  } finally {
    if (!renamed) {
      await fs.rm(tempPath, { force: true });
    }
  }
}

export async function restoreBackup(codexHome, backupId) {
  const backupDir = resolveBackupDir(codexHome, backupId);
  if (!(await pathExists(backupDir))) {
    throw new Error(`找不到备份：${backupId}`);
  }
  const plan = await buildRestorePlan(codexHome, backupDir);
  const snapshots = await snapshotRestoreTargets(plan);
  try {
    await applyRestorePlan(plan);
  } catch (error) {
    try {
      await rollbackRestoreTargets(snapshots);
    } catch (rollbackError) {
      throw new Error(
        `恢复失败（${errorMessage(error)}），且恢复回滚失败（${errorMessage(rollbackError)}）`
      );
    }
    throw error;
  }
}

async function buildRestorePlan(codexHome, backupDir) {
  const plan = [];
  for (const fileName of [CONFIG_FILE, AUTH_FILE, CATALOG_FILE, RECEIPT_FILE]) {
    plan.push({
      fileName,
      backupFile: path.join(backupDir, fileName),
      targetFile: path.join(codexHome, fileName),
    });
  }
  return plan;
}

async function snapshotRestoreTargets(plan) {
  const snapshots = [];
  for (const item of plan) {
    if (await pathExists(item.targetFile)) {
      const content = await fs.readFile(item.targetFile, 'utf8');
      snapshots.push({
        ...item,
        existed: true,
        content,
        mode: restoreMode(item.fileName, content),
      });
    } else {
      snapshots.push({ ...item, existed: false });
    }
  }
  return snapshots;
}

async function applyRestorePlan(plan) {
  for (const item of plan) {
    if (await pathExists(item.backupFile)) {
      const content = await fs.readFile(item.backupFile, 'utf8');
      await atomicWrite(item.targetFile, content, {
        mode: restoreMode(item.fileName, content),
      });
    } else if (await pathExists(item.targetFile)) {
      await fs.rm(item.targetFile, { force: true });
    }
  }
}

async function rollbackRestoreTargets(snapshots) {
  const failures = [];
  for (const snapshot of snapshots) {
    try {
      if (snapshot.existed) {
        await atomicWrite(snapshot.targetFile, snapshot.content, {
          mode: snapshot.mode,
        });
      } else if (await pathExists(snapshot.targetFile)) {
        await fs.rm(snapshot.targetFile, { force: true });
      }
    } catch (error) {
      failures.push(`${snapshot.fileName}: ${errorMessage(error)}`);
    }
  }
  if (failures.length > 0) {
    throw new Error(failures.join('; '));
  }
}

export function resolveBackupDir(codexHome, backupId) {
  if (!isValidBackupId(backupId)) {
    throw new Error(`备份 ID 无效：${backupId}`);
  }
  const backupRoot = path.resolve(codexHome, BACKUP_ROOT);
  const backupDir = path.resolve(backupRoot, backupId);
  if (backupDir !== backupRoot && backupDir.startsWith(`${backupRoot}${path.sep}`)) {
    return backupDir;
  }
  throw new Error(`备份 ID 无效：${backupId}`);
}

export function isValidBackupId(backupId) {
  return (
    typeof backupId === 'string' &&
    /^\d{8}-\d{6}\d{3}Z(?:-[0-9a-f]{6})?$/.test(backupId)
  );
}

function restoreMode(fileName, content) {
  return sensitiveFileMode(fileName, content);
}

function backupMode(fileName) {
  if (process.platform === 'win32') {
    return undefined;
  }
  if (fileName === AUTH_FILE || fileName === CONFIG_FILE) {
    return 0o600;
  }
  return undefined;
}

function sensitiveFileMode(fileName, content) {
  if (process.platform === 'win32') {
    return undefined;
  }
  if (fileName === AUTH_FILE) {
    return 0o600;
  }
  if (fileName === CONFIG_FILE && /^\s*experimental_bearer_token\s*=/m.test(content)) {
    return 0o600;
  }
  return undefined;
}

async function chmodIfSupported(filePath, mode) {
  if (!mode || process.platform === 'win32') {
    return;
  }
  await fs.chmod(filePath, mode);
}

function errorMessage(error) {
  return error instanceof Error ? error.message : String(error);
}

function timestampId() {
  return new Date().toISOString().replace(/[-:.]/g, '').replace('T', '-');
}
