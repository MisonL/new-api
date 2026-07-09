import test from 'node:test';
import assert from 'node:assert/strict';
import path from 'node:path';
import { defaultCodexHome, findWindowsCodexHomes, isWsl } from '../src/paths.js';

test('respects CODEX_HOME', () => {
  assert.equal(defaultCodexHome({ env: { CODEX_HOME: '/tmp/codex' }, homedir: '/home/u' }), '/tmp/codex');
});

test('uses platform defaults', () => {
  assert.equal(defaultCodexHome({ env: {}, platform: 'linux', homedir: '/home/u' }), '/home/u/.codex');
  assert.equal(
    defaultCodexHome({ env: { USERPROFILE: 'C:\\Users\\me' }, platform: 'win32', homedir: '/unused' }),
    path.win32.join('C:\\Users\\me', '.codex'),
  );
});

test('detects WSL from environment', () => {
  assert.equal(isWsl({ env: { WSL_DISTRO_NAME: 'Ubuntu' }, procVersionPath: '/nope' }), true);
});

test('detects WSL from proc version and handles read failures', () => {
  assert.equal(
    isWsl({
      env: {},
      procVersionPath: '/proc/version',
      fsImpl: {
        readFileSync: () => 'Linux version 6.0.0-microsoft-standard-WSL2',
      },
    }),
    true,
  );
  assert.equal(
    isWsl({
      env: {},
      procVersionPath: '/proc/version',
      fsImpl: {
        readFileSync: () => {
          throw new Error('missing');
        },
      },
    }),
    false,
  );
});

test('finds Windows Codex homes from WSL mount', () => {
  const homes = findWindowsCodexHomes({
    usersRoot: '/mnt/c/Users',
    fsImpl: {
      readdirSync: () => [
        { name: 'Public', isDirectory: () => true },
        { name: 'Alice', isDirectory: () => true },
        { name: 'file.txt', isDirectory: () => false },
      ],
    },
  });
  assert.deepEqual(homes, [path.join('/mnt/c/Users', 'Alice', '.codex')]);
});

test('returns empty homes list when the Windows user root is unavailable', () => {
  const homes = findWindowsCodexHomes({
    usersRoot: '/mnt/c/Users',
    fsImpl: {
      readdirSync: () => {
        throw new Error('missing');
      },
    },
  });
  assert.deepEqual(homes, []);
});
