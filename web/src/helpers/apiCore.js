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
import { getUserIdFromLocalStorage, showError } from './utils';
import { handleAuthExpired } from './authRedirect';
import {
  IS_READONLY_FRONTEND,
  READONLY_FRONTEND_MESSAGE,
} from '../constants/runtime.constants';

export let API = axios.create({
  baseURL: import.meta.env.VITE_REACT_APP_SERVER_URL
    ? import.meta.env.VITE_REACT_APP_SERVER_URL
    : '',
  headers: {
    'New-API-User': getUserIdFromLocalStorage(),
    'Cache-Control': 'no-store',
  },
});

const READONLY_SAFE_METHODS = new Set(['get', 'head', 'options']);
const READONLY_BLOCKED_PATHS = [
  /^\/api\/oauth\//,
  /^\/api\/auth\/external\//,
  /^\/api\/user\/logout$/,
  /^\/api\/setup$/,
  /^\/api\/verification$/,
  /^\/api\/reset_password$/,
];

const DEFAULT_LOCAL_SERVER_ADDRESSES = new Set([
  'http://localhost:3000',
  'https://localhost:3000',
  'http://127.0.0.1:3000',
  'https://127.0.0.1:3000',
]);

function normalizeRequestPath(url) {
  if (typeof url !== 'string' || url.trim() === '') {
    return '';
  }

  try {
    return new URL(url, window.location.origin).pathname;
  } catch {
    return url;
  }
}

function applyReadonlyRequestGuard(instance) {
  instance.interceptors.request.use((config) => {
    if (!IS_READONLY_FRONTEND) {
      return config;
    }

    const method = String(config.method || 'get').toLowerCase();
    const path = normalizeRequestPath(config.url);

    if (!READONLY_SAFE_METHODS.has(method)) {
      return Promise.reject(new Error(READONLY_FRONTEND_MESSAGE));
    }

    const isBlockedReadonlyPath = READONLY_BLOCKED_PATHS.some((pattern) =>
      pattern.test(path),
    );
    if (isBlockedReadonlyPath) {
      return Promise.reject(new Error(READONLY_FRONTEND_MESSAGE));
    }

    return config;
  });
}

function patchAPIInstance(instance) {
  const originalGet = instance.get.bind(instance);
  const inFlightGetRequests = new Map();

  const genKey = (url, config = {}) => {
    const params = config.params ? JSON.stringify(config.params) : '{}';
    return `${url}?${params}`;
  };

  instance.get = (url, config = {}) => {
    if (config?.disableDuplicate) {
      return originalGet(url, config);
    }

    const key = genKey(url, config);
    if (inFlightGetRequests.has(key)) {
      return inFlightGetRequests.get(key);
    }

    const reqPromise = originalGet(url, config).finally(() => {
      inFlightGetRequests.delete(key);
    });

    inFlightGetRequests.set(key, reqPromise);
    return reqPromise;
  };
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
    headers: {
      'New-API-User': getUserIdFromLocalStorage(),
      'Cache-Control': 'no-store',
    },
  });

  configureAPIInstance(API);
}

configureAPIInstance(API);
