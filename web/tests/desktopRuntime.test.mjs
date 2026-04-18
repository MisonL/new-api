import test, { after } from 'node:test';
import assert from 'node:assert/strict';
import {
  getDesktopRuntime,
  isDesktopApp,
  openDesktopExternalUrl,
} from '../src/helpers/desktopRuntime.js';

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
    openExternalUrl: null,
  });
  assert.equal(isDesktopApp(), false);
});

test('优先读取注入的桌面运行时', () => {
  const openExternalUrl = async () => {};
  globalThis.window = {
    __NEW_API_DESKTOP_RUNTIME__: {
      platform: 'tauri',
      dataDir: '/tmp/new-api',
      openExternalUrl,
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
    openExternalUrl,
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
    openExternalUrl: null,
  });
});

test('普通 Web 环境保持非桌面语义', () => {
  globalThis.window = {};

  assert.deepEqual(getDesktopRuntime(), {
    isDesktopApp: false,
    platform: 'web',
    dataDir: '',
    openExternalUrl: null,
  });
  assert.equal(isDesktopApp(), false);
});

test('桌面运行时优先使用注入的外部浏览器打开函数', async () => {
  const calls = [];
  globalThis.window = {
    __NEW_API_DESKTOP_RUNTIME__: {
      platform: 'tauri',
      dataDir: '/tmp/new-api',
      openExternalUrl: async (url) => {
        calls.push(url);
      },
    },
    open: () => {
      throw new Error('should not fallback to window.open');
    },
  };

  await openDesktopExternalUrl('https://example.com/oauth');
  assert.deepEqual(calls, ['https://example.com/oauth']);
});

test('无桌面注入时回退到 window.open', async () => {
  const calls = [];
  globalThis.window = {
    open: (...args) => {
      calls.push(args);
    },
  };

  await openDesktopExternalUrl('https://example.com/oauth');
  assert.deepEqual(calls, [
    ['https://example.com/oauth', '_blank', 'noopener,noreferrer'],
  ]);
});

after(restoreWindow);
