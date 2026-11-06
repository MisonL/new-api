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

import axios from 'axios';
import i18next from 'i18next';
import { getUserIdFromLocalStorage, showError } from './utils';
import { handleAuthExpired } from './authRedirect';
import {
  isReadonlyBlockedRequest as shouldBlockReadonlyRequest,
  installReadonlyXHRGuard,
} from './readonlyGuard';
import { installGetDeduplication } from './apiGetDedupe';
import {
  IS_READONLY_FRONTEND,
  READONLY_FRONTEND_MESSAGE,
} from '../constants/runtime.constants';

// 默认 30s 超时，理由同 default 前端：挂住的请求必须有终点。慢端点
// （渠道测试/余额/拉模型、上游同步）在调用点显式放宽 timeout。
export const DEFAULT_API_TIMEOUT_MS = 30_000;

export let API = axios.create({
  baseURL: import.meta.env.VITE_REACT_APP_SERVER_URL
    ? import.meta.env.VITE_REACT_APP_SERVER_URL
    : '',
  timeout: DEFAULT_API_TIMEOUT_MS,
  headers: {
    'New-API-User': getUserIdFromLocalStorage(),
    'Cache-Control': 'no-store',
  },
});

const DEFAULT_LOCAL_SERVER_ADDRESSES = new Set([
  'http://localhost:3000',
  'https://localhost:3000',
  'http://127.0.0.1:3000',
  'https://127.0.0.1:3000',
]);

// Whether the read-only frontend must reject this request. Kept in its own
// module so the decision is unit testable without pulling in the Semi UI
// runtime that this module's axios instance drags in.
function isReadonlyBlockedRequest(method, url) {
  return shouldBlockReadonlyRequest(method, url, window.location.origin);
}

function applyReadonlyRequestGuard(instance) {
  instance.interceptors.request.use((config) => {
    if (!IS_READONLY_FRONTEND) {
      return config;
    }

    if (isReadonlyBlockedRequest(config.method, config.url)) {
      return Promise.reject(new Error(i18next.t(READONLY_FRONTEND_MESSAGE)));
    }

    return config;
  });
}

// Installs the guard on the global fetch. The axios instance covers the API
// helpers, but fetch is used directly (the playground request path, the Ollama
// pull stream) and any future call site would silently bypass the guard.
// Patching the global makes the guard a layer instead of an opt-in helper.
let fetchGuardInstalled = false;

function installReadonlyFetchGuard() {
  if (!IS_READONLY_FRONTEND || fetchGuardInstalled) {
    return;
  }
  if (typeof globalThis.fetch !== 'function') {
    return;
  }
  fetchGuardInstalled = true;

  const originalFetch = globalThis.fetch.bind(globalThis);
  globalThis.fetch = (input, init) => {
    const method =
      init?.method ??
      (typeof Request !== 'undefined' && input instanceof Request
        ? input.method
        : 'get');
    const url =
      typeof input === 'string'
        ? input
        : input instanceof URL
          ? input.href
          : input.url;
    if (isReadonlyBlockedRequest(method, url)) {
      return Promise.reject(new Error(i18next.t(READONLY_FRONTEND_MESSAGE)));
    }
    return originalFetch(input, init);
  };
}

function patchAPIInstance(instance) {
  installGetDeduplication(instance);
}

function applyResponseErrorHandler(instance) {
  instance.interceptors.response.use(
    (response) => response,
    (error) => {
      if (error.config && error.config.skipErrorHandler) {
        return Promise.reject(error);
      }
      if (error?.response?.status === 401) {
        handleAuthExpired(window.location);
      }
      showError(error);
      return Promise.reject(error);
    },
  );
}

function configureAPIInstance(instance) {
  patchAPIInstance(instance);
  applyReadonlyRequestGuard(instance);
  installReadonlyFetchGuard();
  installReadonlyXHRGuard({
    isBlocked: (method, url) => isReadonlyBlockedRequest(method, url),
    message: i18next.t(READONLY_FRONTEND_MESSAGE),
    enabled: IS_READONLY_FRONTEND,
  });
  applyResponseErrorHandler(instance);
}

function normalizeServerAddress(
  address,
  fallbackOrigin = window.location.origin,
) {
  if (typeof address !== 'string' || address.trim() === '') {
    return '';
  }
  try {
    return new URL(address.trim(), fallbackOrigin)
      .toString()
      .replace(/\/$/, '');
  } catch {
    return '';
  }
}

export function getEffectiveServerAddress(configuredAddress) {
  const currentOrigin = normalizeServerAddress(window.location.origin);
  const normalizedConfigured = normalizeServerAddress(configuredAddress);

  if (!normalizedConfigured) {
    return currentOrigin;
  }

  if (
    DEFAULT_LOCAL_SERVER_ADDRESSES.has(normalizedConfigured) &&
    normalizedConfigured !== currentOrigin
  ) {
    return currentOrigin;
  }

  return normalizedConfigured;
}

export function isUsingRuntimeServerAddress(configuredAddress) {
  const currentOrigin = normalizeServerAddress(window.location.origin);
  const normalizedConfigured = normalizeServerAddress(configuredAddress);

  if (!normalizedConfigured) {
    return true;
  }

  return (
    DEFAULT_LOCAL_SERVER_ADDRESSES.has(normalizedConfigured) &&
    normalizedConfigured !== currentOrigin
  );
}

export function buildAPIURL(path) {
  const normalizedPath = path.startsWith('/') ? path : `/${path}`;
  const configuredBaseURL =
    typeof API.defaults?.baseURL === 'string'
      ? API.defaults.baseURL.trim()
      : '';

  if (!configuredBaseURL) {
    return new URL(normalizedPath, window.location.origin);
  }

  const baseURL = new URL(configuredBaseURL, window.location.origin);
  const basePath = baseURL.pathname.replace(/\/+$/, '');
  baseURL.pathname = `${basePath}${normalizedPath}`;
  baseURL.search = '';
  baseURL.hash = '';
  return baseURL;
}

export function updateAPI() {
  API = axios.create({
    baseURL: import.meta.env.VITE_REACT_APP_SERVER_URL
      ? import.meta.env.VITE_REACT_APP_SERVER_URL
      : '',
    timeout: DEFAULT_API_TIMEOUT_MS,
    headers: {
      'New-API-User': getUserIdFromLocalStorage(),
      'Cache-Control': 'no-store',
    },
  });

  configureAPIInstance(API);
}

configureAPIInstance(API);
