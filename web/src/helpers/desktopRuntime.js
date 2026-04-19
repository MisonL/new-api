/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

function getWindowObject() {
  if (typeof window === 'undefined') {
    return null;
  }
  return window;
}

function getDefaultRuntime() {
  return {
    isDesktopApp: false,
    platform: 'web',
    dataDir: '',
    openExternalUrl: null,
  };
}

export function getDesktopRuntime() {
  const currentWindow = getWindowObject();
  if (!currentWindow) {
    return getDefaultRuntime();
  }

  const injectedRuntime = currentWindow.__NEW_API_DESKTOP_RUNTIME__;
  if (injectedRuntime && typeof injectedRuntime === 'object') {
    return {
      isDesktopApp: true,
      platform: injectedRuntime.platform || 'desktop',
      dataDir: injectedRuntime.dataDir || '',
      openExternalUrl:
        typeof injectedRuntime.openExternalUrl === 'function'
          ? injectedRuntime.openExternalUrl
          : null,
    };
  }

  const electronRuntime = currentWindow.electron;
  if (electronRuntime?.isElectron) {
    return {
      isDesktopApp: true,
      platform: 'electron',
      dataDir: electronRuntime.dataDir || '',
      openExternalUrl: null,
    };
  }

  return getDefaultRuntime();
}

export function isDesktopApp() {
  return getDesktopRuntime().isDesktopApp;
}

export async function openDesktopExternalUrl(url) {
  const runtime = getDesktopRuntime();
  if (typeof runtime.openExternalUrl === 'function') {
    await runtime.openExternalUrl(url);
    return;
  }

  const currentWindow = getWindowObject();
  if (!currentWindow || typeof currentWindow.open !== 'function') {
    throw new Error('当前环境不支持打开外部链接');
  }

  currentWindow.open(String(url), '_blank', 'noopener,noreferrer');
}
