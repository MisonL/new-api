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

// Read-only request-layer guard, kept free of runtime dependencies so it can be
// unit tested and so the decision is one function rather than one per caller.
//
// The read-only frontend runs against a real backend through a proxy that only
// rejects unsafe HTTP methods, so it cannot see intent: several gateway
// endpoints are registered as GET but write state. This module is the request
// layer that closes that gap. It mirrors web/default's
// src/lib/readonly-guard.ts, and the two lists must stay in step.

export const READONLY_SAFE_METHODS = new Set(['get', 'head', 'options']);

// Paths the read-only frontend must never reach, even with a safe method.
//
// The first five are auth, account lifecycle and OAuth/CAS callback entry
// points. setup is deliberately absent: POST /api/setup is already rejected by
// the method check, and GET /api/setup is a pure read that the root bootstrap
// calls once per session to decide whether to redirect to the setup wizard.
// Blocking it made that read fail, so the read-only frontend surfaced an error
// toast for a request that has no side effect. The rest are gateway endpoints
// registered as GET that still write
// state, so the proxy's method check cannot stop them:
//   - /api/channel/test and /api/channel/test/:id run upstream probes and then
//     update response times and disable failing channels.
//   - /api/channel/update_balance and /api/channel/update_balance/:id write the
//     fetched balance and disable exhausted channels.
//   - /api/channel/fetch_models/:id and /api/channel/ollama/version/:id make
//     outbound calls to the upstream provider.
//   - /api/channel/:id/codex/usage refreshes and persists the channel key.
//   - /api/user/token generates and persists a new access token.
//   - /api/user/aff generates and persists an invite code.
//   - /api/user/epay/notify and /api/subscription/epay/(notify|return) complete
//     a payment order.
export const READONLY_BLOCKED_PATHS = [
  /^\/api\/oauth\//,
  /^\/api\/auth\/external\//,
  /^\/api\/user\/logout$/,
  // setup is deliberately absent: POST /api/setup is already rejected by the
  // method check, and GET /api/setup is a pure read the bootstrap needs.
  /^\/api\/verification$/,
  /^\/api\/reset_password$/,
  /^\/api\/channel\/test$/,
  /^\/api\/channel\/test\/[^/]+$/,
  /^\/api\/channel\/update_balance$/,
  /^\/api\/channel\/update_balance\/[^/]+$/,
  /^\/api\/channel\/fetch_models\/[^/]+$/,
  /^\/api\/channel\/ollama\/version\/[^/]+$/,
  /^\/api\/channel\/[^/]+\/codex\/usage$/,
  /^\/api\/user\/token$/,
  /^\/api\/user\/aff$/,
  /^\/api\/user\/epay\/notify$/,
  /^\/api\/subscription\/epay\/notify$/,
  /^\/api\/subscription\/epay\/return$/,
];

// Normalises a pathname the way the gateway resolves it before routing.
//
// URL.pathname keeps the spelling the caller used, but the router does not.
// Measured against the running backend, the router does two things before it
// matches a route:
//
//   - it percent-decodes the path, so /api/user%2Ftoken, /api/%75ser/token and
//     /api/user/tok%65n all reach the same handler as /api/user/token;
//   - it answers a single trailing slash with a 301 to the canonical path,
//     which the browser follows, so /api/user/token/ reaches that handler too.
//
// Matching the raw pathname missed both, so every blocked endpoint was
// reachable in a disguised spelling.
//
// Nothing else is normalised, because the backend does nothing else. Measured
// on the same backend:
//
//   - an interior or repeated slash is NOT collapsed: /api//user/token and
//     /api/user/token// are both rejected as Invalid URL;
//   - matrix parameters and an encoded space are NOT stripped or trimmed, so
//     /api/user/token;a=b and /api/user/token%20 route to /api/user/:id, a
//     different handler;
//   - "." and ".." are not resolved by the router, but the URL parser has
//     already resolved them before this function runs -- including the
//     percent-encoded forms, which the URL spec counts as dot segments.
//
// Normalising any of those would block requests that are not blocked
// endpoints. Decoding can only reveal a blocked path, never hide one, so it is
// applied before the match.
function normalizePathForRouting(pathname) {
  let decoded = pathname;
  try {
    decoded = decodeURIComponent(pathname);
  } catch {
    // A malformed escape sequence is not something the router can decode
    // either; fall back to the raw spelling rather than dropping the check.
    decoded = pathname;
  }

  // Exactly one trailing slash, because that is exactly what the router
  // redirects. /api/user/token// is not redirected, so it is left alone and
  // stays unmatched.
  if (decoded.length > 1 && decoded.endsWith('/')) {
    decoded = decoded.slice(0, -1);
  }

  return decoded;
}

// Whether the read-only frontend must reject this request.
//
// A request with an unsafe method is always rejected. For a safe method, a
// blocked path is rejected, and so is an absolute URL on another host that
// carries the gateway's own /api prefix -- that is a direct call to a real
// backend, which would skip the proxy enforcing these same rules. Other
// cross-origin reads (a release feed, an OIDC well-known document, a
// user-supplied speed-test target) are legitimate and pass.
export function isReadonlyBlockedRequest(method, url, baseOrigin) {
  const normalizedMethod = String(method || 'get').toLowerCase();
  if (!READONLY_SAFE_METHODS.has(normalizedMethod)) {
    return true;
  }

  if (typeof url !== 'string' || url.trim() === '') {
    return false;
  }

  const base = baseOrigin || '';
  let parsed;
  try {
    parsed = new URL(url.trim(), base || undefined);
  } catch {
    return false;
  }

  const path = normalizePathForRouting(parsed.pathname);
  if (READONLY_BLOCKED_PATHS.some((pattern) => pattern.test(path))) {
    return true;
  }
  return Boolean(base) && parsed.origin !== base && path.startsWith('/api/');
}

// Installs the read-only guard on XMLHttpRequest.
//
// The fetch patch and the axios interceptor both miss any request opened with
// `new XMLHttpRequest()` -- the streaming helpers in web/default's playground
// (sse.js) do exactly that, and a future classic call site would too. Blocking
// inside `open()` rejects the request before a socket is opened, so the guard
// stays a layer instead of an opt-in helper.
//
// The decision callback is injected rather than imported so this module keeps
// zero runtime dependencies and the patch itself is unit testable.
const XHR_GUARD_MARK = Symbol.for("new-api.readonly-xhr-guard");

export function installReadonlyXHRGuard({
  isBlocked,
  message,
  enabled = true,
}) {
  if (!enabled) {
    return;
  }
  if (typeof globalThis.XMLHttpRequest !== "function") {
    return;
  }

  const proto = globalThis.XMLHttpRequest.prototype;
  const originalOpen = proto.open;
  if (typeof originalOpen !== "function" || originalOpen[XHR_GUARD_MARK]) {
    return;
  }

  const wrapper = function (method, url, ...rest) {
    if (isBlocked(method, url)) {
      throw new Error(message);
    }
    return originalOpen.call(this, method, url, ...rest);
  };
  wrapper[XHR_GUARD_MARK] = true;
  proto.open = wrapper;
}
