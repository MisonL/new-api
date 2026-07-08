import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

const COMMON_WINDOWS_SYSTEM_HOME_NAMES = [
  'All Users',
  'Default',
  'Default User',
  'Default.migrated',
  'Public',
  'WDAGUtilityAccount',
];

export function isWsl({
  env = process.env,
  procVersionPath = '/proc/version',
  fsImpl = fs,
} = {}) {
  if (env.WSL_DISTRO_NAME || env.WSL_INTEROP) {
    return true;
  }
  try {
    return fsImpl.readFileSync(procVersionPath, 'utf8').toLowerCase().includes('microsoft');
  } catch {
    return false;
  }
}

export function defaultCodexHome({ env = process.env, platform = process.platform, homedir = os.homedir() } = {}) {
  const pathImpl = platform === 'win32' ? path.win32 : path.posix;
  if (env.CODEX_HOME) {
    return pathImpl.resolve(env.CODEX_HOME);
  }
  if (platform === 'win32') {
    return pathImpl.join(env.USERPROFILE || homedir, '.codex');
  }
  return pathImpl.join(homedir, '.codex');
}

export function findWindowsCodexHomes({ usersRoot = '/mnt/c/Users', fsImpl = fs } = {}) {
  let users;
  try {
    users = fsImpl.readdirSync(usersRoot, { withFileTypes: true });
  } catch {
    return [];
  }
  return users
    .filter((entry) => entry.isDirectory())
    .map((entry) => entry.name)
    .filter((name) => !COMMON_WINDOWS_SYSTEM_HOME_NAMES.includes(name))
    .map((name) => path.join(usersRoot, name, '.codex'));
}
