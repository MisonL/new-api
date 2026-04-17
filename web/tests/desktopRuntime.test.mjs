import test, { after } from 'node:test';
import assert from 'node:assert/strict';
import { getDesktopRuntime, isDesktopApp } from '../src/helpers/desktopRuntime.js';

const originalWindow = globalThis.window;

function restoreWindow() {
  if (typeof originalWindow === 'undefined') {
    delete globalThis.window;
    return;
  }
  globalThis.window = originalWindow;
}

test('SSR 环境返回 web 运行时', () => {
  delete globalThis.window;

  assert.deepEqual(getDesktopRuntime(), {
    isDesktopApp: false,
    platform: 'web',
    dataDir: '',
  });
  assert.equal(isDesktopApp(), false);
});

test('优先读取注入的桌面运行时', () => {
  globalThis.window = {
    __NEW_API_DESKTOP_RUNTIME__: {
      platform: 'tauri',
      dataDir: '/tmp/new-api',
    },
    electron: {
      isElectron: true,
      dataDir: '/tmp/electron',
    },
  };

  assert.deepEqual(getDesktopRuntime(), {
    isDesktopApp: true,
    platform: 'tauri',
    dataDir: '/tmp/new-api',
  });
  assert.equal(isDesktopApp(), true);
});

test('无注入运行时时回退到 Electron 桥接', () => {
  globalThis.window = {
    electron: {
      isElectron: true,
      dataDir: '/tmp/electron',
    },
  };

  assert.deepEqual(getDesktopRuntime(), {
    isDesktopApp: true,
    platform: 'electron',
    dataDir: '/tmp/electron',
  });
});

test('普通 Web 环境保持非桌面语义', () => {
  globalThis.window = {};

  assert.deepEqual(getDesktopRuntime(), {
    isDesktopApp: false,
    platform: 'web',
    dataDir: '',
  });
  assert.equal(isDesktopApp(), false);
});

after(restoreWindow);
