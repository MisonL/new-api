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

const AUTH_REDIRECT_TARGET_KEY = 'new-api-auth-redirect-target';
const AUTH_EXPIRED_REDIRECTING_KEY = 'new-api-auth-expired-redirecting';
const AUTH_REDIRECT_BASE_URL = 'https://new-api.local';
const AUTH_EXPIRED_LOGIN_TARGET = '/login?expired=true';

export function normalizeAuthRedirectTarget(target) {
  if (typeof target !== 'string') return '';
  const trimmed = target.trim();
  if (!trimmed || !trimmed.startsWith('/') || trimmed.startsWith('//')) {
    return '';
  }
  try {
    const url = new URL(trimmed, AUTH_REDIRECT_BASE_URL);
    if (url.origin !== AUTH_REDIRECT_BASE_URL) return '';
    if (url.pathname === '/login' || url.pathname === '/register') return '';
    return `${url.pathname}${url.search}${url.hash}`;
  } catch (error) {
    return '';
  }
}

export function getAuthRedirectTargetFromLocation(location) {
  if (!location || typeof location.pathname !== 'string') return '';
  return normalizeAuthRedirectTarget(
    `${location.pathname}${location.search || ''}${location.hash || ''}`,
  );
}

function getSessionStorage() {
  try {
    return globalThis.sessionStorage || null;
  } catch (error) {
    return null;
  }
}

export function rememberAuthRedirectTarget(location) {
  const target = getAuthRedirectTargetFromLocation(location);
  if (!target) return;
  const storage = getSessionStorage();
  if (!storage) return;
  try {
    storage.setItem(AUTH_REDIRECT_TARGET_KEY, target);
  } catch (error) {
    // Storage may be unavailable in private mode or restricted browsers.
  }
}

export function consumeAuthRedirectTarget(
  locationState,
  fallback = '/console',
) {
  clearAuthExpiredRedirecting();
  const stateTarget = getAuthRedirectTargetFromLocation(locationState?.from);
  const storage = getSessionStorage();
  let storedTarget = '';
  if (storage) {
    try {
      storedTarget = normalizeAuthRedirectTarget(
        storage.getItem(AUTH_REDIRECT_TARGET_KEY) || '',
      );
      storage.removeItem(AUTH_REDIRECT_TARGET_KEY);
    } catch (error) {
      storedTarget = '';
    }
  }
  return storedTarget || stateTarget || fallback;
}

export function clearAuthExpiredRedirecting() {
  const storage = getSessionStorage();
  if (!storage) return;
  try {
    storage.removeItem(AUTH_EXPIRED_REDIRECTING_KEY);
  } catch (error) {
    // Storage may be unavailable in private mode or restricted browsers.
  }
}

export function handleAuthExpired(currentLocation = globalThis.location) {
  rememberAuthRedirectTarget(currentLocation);
  try {
    globalThis.localStorage?.removeItem('user');
  } catch (error) {
    // localStorage may be unavailable in restricted browsers.
  }

  const storage = getSessionStorage();
  if (storage) {
    try {
      if (storage.getItem(AUTH_EXPIRED_REDIRECTING_KEY) === '1') {
        return false;
      }
      storage.setItem(AUTH_EXPIRED_REDIRECTING_KEY, '1');
    } catch (error) {
      // Continue with redirect if sessionStorage is not writable.
    }
  }

  if (globalThis.location?.assign) {
    globalThis.location.assign(AUTH_EXPIRED_LOGIN_TARGET);
  } else if (globalThis.location) {
    globalThis.location.href = AUTH_EXPIRED_LOGIN_TARGET;
  }
  return true;
}
