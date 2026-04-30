import assert from 'node:assert/strict';
import test from 'node:test';

import {
  clearAuthExpiredRedirecting,
  consumeAuthRedirectTarget,
  getAuthRedirectTargetFromLocation,
  handleAuthExpired,
  normalizeAuthRedirectTarget,
  rememberAuthRedirectTarget,
} from '../src/helpers/authRedirect.js';
import { setUserData } from '../src/helpers/data.js';

function createStorage() {
  const values = new Map();
  return {
    getItem(key) {
      return values.has(key) ? values.get(key) : null;
    },
    setItem(key, value) {
      values.set(key, String(value));
    },
    removeItem(key) {
      values.delete(key);
    },
  };
}

test('normalizeAuthRedirectTarget only allows local app paths', () => {
  assert.equal(
    normalizeAuthRedirectTarget('/console/topup?gift_code=abc#receive'),
    '/console/topup?gift_code=abc#receive',
  );
  assert.equal(normalizeAuthRedirectTarget('https://example.com/a'), '');
  assert.equal(normalizeAuthRedirectTarget('//example.com/a'), '');
  assert.equal(normalizeAuthRedirectTarget('/login'), '');
  assert.equal(normalizeAuthRedirectTarget('/register'), '');
});

test('rememberAuthRedirectTarget preserves query and hash for login', () => {
  const previousStorage = globalThis.sessionStorage;
  const storage = createStorage();
  Object.defineProperty(globalThis, 'sessionStorage', {
    configurable: true,
    value: storage,
  });
  try {
    const location = {
      pathname: '/console/topup',
      search: '?gift_code=abc',
      hash: '#receive',
    };
    assert.equal(
      getAuthRedirectTargetFromLocation(location),
      '/console/topup?gift_code=abc#receive',
    );
    rememberAuthRedirectTarget(location);
    storage.setItem('new-api-auth-expired-redirecting', '1');
    assert.equal(
      consumeAuthRedirectTarget(null, '/console'),
      '/console/topup?gift_code=abc#receive',
    );
    assert.equal(storage.getItem('new-api-auth-expired-redirecting'), null);
    assert.equal(consumeAuthRedirectTarget(null, '/console'), '/console');
  } finally {
    Object.defineProperty(globalThis, 'sessionStorage', {
      configurable: true,
      value: previousStorage,
    });
  }
});

test('consumeAuthRedirectTarget prefers stored target over stale router state', () => {
  const previousStorage = globalThis.sessionStorage;
  const storage = createStorage();
  Object.defineProperty(globalThis, 'sessionStorage', {
    configurable: true,
    value: storage,
  });
  try {
    rememberAuthRedirectTarget({
      pathname: '/console/token',
      search: '',
      hash: '',
    });
    assert.equal(
      consumeAuthRedirectTarget(
        {
          from: {
            pathname: '/console/topup',
            search: '?gift_code=abc',
            hash: '',
          },
        },
        '/console',
      ),
      '/console/token',
    );
    assert.equal(consumeAuthRedirectTarget(null, '/console'), '/console');
  } finally {
    Object.defineProperty(globalThis, 'sessionStorage', {
      configurable: true,
      value: previousStorage,
    });
  }
});

test('handleAuthExpired clears user, preserves current route, and redirects once', () => {
  const previousStorage = globalThis.sessionStorage;
  const previousLocalStorage = globalThis.localStorage;
  const previousLocation = globalThis.location;
  const storage = createStorage();
  const localStorage = createStorage();
  const redirects = [];

  localStorage.setItem('user', '{"id":1}');
  Object.defineProperty(globalThis, 'sessionStorage', {
    configurable: true,
    value: storage,
  });
  Object.defineProperty(globalThis, 'localStorage', {
    configurable: true,
    value: localStorage,
  });
  Object.defineProperty(globalThis, 'location', {
    configurable: true,
    value: {
      pathname: '/console/topup',
      search: '?gift_code=abc',
      hash: '#receive',
      assign(target) {
        redirects.push(target);
      },
    },
  });

  try {
    assert.equal(handleAuthExpired(globalThis.location), true);
    assert.equal(localStorage.getItem('user'), null);
    assert.deepEqual(redirects, ['/login?expired=true']);
    assert.equal(handleAuthExpired(globalThis.location), false);
    assert.deepEqual(redirects, ['/login?expired=true']);
    assert.equal(
      consumeAuthRedirectTarget(null, '/console'),
      '/console/topup?gift_code=abc#receive',
    );
    clearAuthExpiredRedirecting();
    assert.equal(handleAuthExpired(globalThis.location), true);
    assert.deepEqual(redirects, ['/login?expired=true', '/login?expired=true']);
  } finally {
    Object.defineProperty(globalThis, 'sessionStorage', {
      configurable: true,
      value: previousStorage,
    });
    Object.defineProperty(globalThis, 'localStorage', {
      configurable: true,
      value: previousLocalStorage,
    });
    Object.defineProperty(globalThis, 'location', {
      configurable: true,
      value: previousLocation,
    });
  }
});

test('setUserData clears expired redirect lock after successful login', () => {
  const previousStorage = globalThis.sessionStorage;
  const previousLocalStorage = globalThis.localStorage;
  const storage = createStorage();
  const localStorage = createStorage();

  storage.setItem('new-api-auth-expired-redirecting', '1');
  Object.defineProperty(globalThis, 'sessionStorage', {
    configurable: true,
    value: storage,
  });
  Object.defineProperty(globalThis, 'localStorage', {
    configurable: true,
    value: localStorage,
  });

  try {
    setUserData({ id: 1, username: 'demo' });
    assert.equal(storage.getItem('new-api-auth-expired-redirecting'), null);
    assert.equal(
      localStorage.getItem('user'),
      JSON.stringify({ id: 1, username: 'demo' }),
    );
  } finally {
    Object.defineProperty(globalThis, 'sessionStorage', {
      configurable: true,
      value: previousStorage,
    });
    Object.defineProperty(globalThis, 'localStorage', {
      configurable: true,
      value: previousLocalStorage,
    });
  }
});
