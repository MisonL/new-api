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

import { describe, expect, test } from 'bun:test';

import { isReadonlyBlockedRequest } from '../src/helpers/readonlyGuard.js';

const BASE = 'http://127.0.0.1:5173';

describe('readonly request guard', () => {
  // Every endpoint the guard lists was verified against router/api-router.go.
  test('blocks every high-risk entry point', () => {
    const blocked = [
      '/api/oauth/github',
      '/api/auth/external/github',
      '/api/user/logout',
      '/api/setup',
      '/api/verification',
      '/api/reset_password',
      '/api/channel/test',
      '/api/channel/test/12',
      '/api/channel/update_balance',
      '/api/channel/update_balance/12',
      '/api/channel/fetch_models/12',
      '/api/channel/ollama/version/12',
      '/api/channel/12/codex/usage',
      '/api/user/token',
      '/api/user/aff',
      '/api/user/epay/notify',
      '/api/subscription/epay/notify',
      '/api/subscription/epay/return',
    ];
    for (const path of blocked) {
      expect(isReadonlyBlockedRequest('get', path, BASE)).toBe(true);
    }
  });

  // The read-only proxy only rejects unsafe methods, but the gateway endpoints
  // below are registered as GET and still write state, so the request layer is
  // what stops them.
  test('blocks the GET-but-mutating gateway endpoints', () => {
    expect(isReadonlyBlockedRequest('get', '/api/channel/test', BASE)).toBe(
      true,
    );
    expect(isReadonlyBlockedRequest('get', '/api/user/token', BASE)).toBe(true);
  });

  test('blocks every unsafe method even on an allowed path', () => {
    for (const method of ['post', 'put', 'delete', 'patch']) {
      expect(isReadonlyBlockedRequest(method, '/api/pricing', BASE)).toBe(true);
    }
  });

  test('allows safe reads against the gateway', () => {
    expect(isReadonlyBlockedRequest('get', '/api/pricing', BASE)).toBe(false);
    expect(isReadonlyBlockedRequest('get', '/api/models', BASE)).toBe(false);
    expect(isReadonlyBlockedRequest('get', '/api/status', BASE)).toBe(false);
    expect(isReadonlyBlockedRequest('get', '/api/channel/', BASE)).toBe(false);
  });

  // A cross-origin read of a third-party service is legitimate (a release feed,
  // an OIDC well-known document, a user-supplied speed-test target). The
  // exception is a cross-origin URL carrying the gateway's own /api prefix: that
  // is a direct call to a real backend and skips the proxy entirely.
  test('allows third-party reads but not cross-origin gateway calls', () => {
    expect(
      isReadonlyBlockedRequest(
        'get',
        'https://api.github.com/repos/x/y/releases',
        BASE,
      ),
    ).toBe(false);
    expect(
      isReadonlyBlockedRequest(
        'get',
        'http://127.0.0.1:3000/api/user/token',
        BASE,
      ),
    ).toBe(true);
    expect(
      isReadonlyBlockedRequest(
        'get',
        'http://127.0.0.1:3000/api/pricing',
        BASE,
      ),
    ).toBe(true);
  });

  test('treats a same-origin absolute URL as the gateway itself', () => {
    expect(
      isReadonlyBlockedRequest('get', `${BASE}/api/user/token`, BASE),
    ).toBe(true);
    expect(isReadonlyBlockedRequest('get', `${BASE}/api/pricing`, BASE)).toBe(
      false,
    );
  });

  test('allows an empty or missing url', () => {
    expect(isReadonlyBlockedRequest('get', '', BASE)).toBe(false);
    expect(isReadonlyBlockedRequest('get', '   ', BASE)).toBe(false);
    expect(isReadonlyBlockedRequest('get', undefined, BASE)).toBe(false);
  });

  // URL.pathname keeps the spelling the caller used, but the router does not:
  // it percent-decodes the path and answers a single trailing slash with a
  // redirect to the canonical form. Each URL below was measured against the
  // running backend and reaches the same handler as its canonical spelling,
  // while the raw pathname misses the blocklist's anchored patterns.
  test('sees through path disguises', () => {
    const disguises = [
      '/api/user/token/',
      '/api/user/token/.',
      '/api/user%2Ftoken',
      '/api/%75ser/token',
      '/api/user/tok%65n',
      '/api/channel/test/',
      '/api/%63hannel/test',
      '/api/user/aff/',
      // The URL parser resolves a percent-encoded dot segment before the guard
      // sees the pathname, so this one arrives already rewritten to the
      // blocked path.
      '/api/channel/%2e%2e/user/token',
    ];
    for (const path of disguises) {
      expect(isReadonlyBlockedRequest('get', path, BASE)).toBe(true);
    }
  });

  // Measured counter-examples. The router strips neither matrix parameters nor
  // an encoded space, so these land on /api/user/:id -- a different handler --
  // and must keep passing. Normalising them away would be a false block.
  test('does not block spellings that route elsewhere', () => {
    expect(isReadonlyBlockedRequest('get', '/api/user/token;a=b', BASE)).toBe(
      false,
    );
    expect(isReadonlyBlockedRequest('get', '/api/user/token%20', BASE)).toBe(
      false,
    );
    expect(
      isReadonlyBlockedRequest('get', '/api/pricing/model%2Fname', BASE),
    ).toBe(false);
    // Nor does it collapse a doubled slash or redirect a repeated trailing
    // slash: both are rejected as Invalid URL rather than reaching the token
    // handler, so blocking them would be a false block.
    expect(isReadonlyBlockedRequest('get', '/api//user/token', BASE)).toBe(
      false,
    );
    expect(isReadonlyBlockedRequest('get', '/api/user/token//', BASE)).toBe(
      false,
    );
  });
});
