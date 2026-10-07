import type { AxiosInstance } from 'axios'

/**
 * Read-only request-layer guard.
 *
 * `deploy/compose/frontend-readonly-proxy.yml` puts an nginx proxy in front of a
 * real backend so the frontend can be developed against production data. That
 * proxy only rejects unsafe HTTP methods, so it cannot see intent: several
 * gateway endpoints are registered as GET but mutate state, and the browser
 * passes them straight through. AGENTS.md section 7 therefore requires the
 * frontend request layer to block unsafe methods and high-risk entry points on
 * its own, before the proxy is even reached.
 *
 * `web/classic` has had this guard since the proxy was introduced; `web/default`
 * did not, which left the default frontend with a single layer. This module is
 * that second layer. It is inert unless VITE_REACT_APP_READONLY_MODE is 'true',
 * so it costs nothing in a normal build.
 */

export const IS_READONLY_FRONTEND =
  import.meta.env.VITE_REACT_APP_READONLY_MODE === 'true'

export const READONLY_FRONTEND_MESSAGE =
  (import.meta.env.VITE_REACT_APP_READONLY_MESSAGE as string | undefined) ||
  '当前是只读前端联调环境：仅允许查看正式后端数据，已阻止登录、登出、绑定和所有写入请求。'

const READONLY_SAFE_METHODS = new Set(['get', 'head', 'options'])

/**
 * Paths the read-only frontend must never reach, even with a safe method.
 *
 * The first group is the classic guard's list: authentication, account
 * lifecycle and callback entry points. The second group is gateway endpoints
 * that are registered as GET but write state, so the proxy's method check does
 * not stop them. Each was verified against router/api-router.go:
 *
 * - `/api/channel/test`, `/api/channel/test/:id` run upstream probes and then
 *   update response times and disable failing channels.
 * - `/api/channel/update_balance`, `/api/channel/update_balance/:id` write the
 *   fetched balance and disable channels whose balance is exhausted.
 * - `/api/channel/fetch_models/:id` and `/api/channel/ollama/version/:id` make
 *   outbound calls to the upstream provider.
 * - `/api/channel/:id/codex/usage` refreshes and persists the channel key.
 * - `/api/user/token` generates and persists a new access token.
 * - `/api/user/aff` generates and persists an invite code.
 * - `/api/user/epay/notify` and `/api/subscription/epay/(notify|return)`
 *   complete a payment order.
 * - `/api/oauth/*` writes OAuth state into the session.
 *
 * The exact-path entries are GET endpoints with side effects, so the proxy's
 * method check cannot stop them. POST /api/setup is not listed separately
 * because every unsafe method is already rejected above -- and unlike its
 * siblings `GET /api/setup` is a pure read (router/api-router.go:23) that the
 * root bootstrap calls once per session, so blocking it surfaced an error toast
 * for a request that has no side effect.
 */
const READONLY_BLOCKED_PATHS = [
  /^\/api\/oauth\//,
  /^\/api\/auth\/external\//,
  /^\/api\/user\/logout$/,
  // GET /api/verification and GET /api/reset_password are on this list as GET
  // endpoints that have side effects: both send an email (router/api-router.go:43-44).
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
]

function getBaseOrigin(): string {
  if (typeof window !== 'undefined' && window.location?.origin) {
    return window.location.origin
  }
  return 'http://localhost'
}

type ParsedRequestUrl = {
  path: string
  sameOrigin: boolean
}

/**
 * Normalises a pathname the way the gateway resolves it before routing.
 *
 * URL.pathname keeps the spelling the caller used, but the router does not.
 * Measured against the running backend, the router does two things before it
 * matches a route:
 *
 * - it percent-decodes the path, so /api/user%2Ftoken, /api/%75ser/token and
 *   /api/user/tok%65n all reach the same handler as /api/user/token;
 * - it answers a single trailing slash with a 301 to the canonical path, which
 *   the browser follows, so /api/user/token/ reaches that handler too.
 *
 * Matching the raw pathname missed both, so every blocked endpoint was
 * reachable in a disguised spelling.
 *
 * Nothing else is normalised, because the backend does nothing else. Measured
 * on the same backend:
 *
 * - an interior or repeated slash is NOT collapsed: /api//user/token and
 *   /api/user/token// are both rejected as Invalid URL;
 * - matrix parameters and an encoded space are NOT stripped or trimmed, so
 *   /api/user/token;a=b and /api/user/token%20 route to /api/user/:id, a
 *   different handler;
 * - "." and ".." are not resolved by the router, but the URL parser has
 *   already resolved them before this function runs -- including the
 *   percent-encoded forms, which the URL spec counts as dot segments.
 *
 * Normalising any of those would block requests that are not blocked
 * endpoints. Decoding can only reveal a blocked path, never hide one, so it is
 * applied before the match.
 */
function normalizePathForRouting(pathname: string): string {
  let decoded: string
  try {
    decoded = decodeURIComponent(pathname)
  } catch {
    // A malformed escape sequence is not something the router can decode
    // either; fall back to the raw spelling rather than dropping the check.
    decoded = pathname
  }

  // Exactly one trailing slash, because that is exactly what the router
  // redirects. /api/user/token// is not redirected, so it is left alone and
  // stays unmatched.
  if (decoded.length > 1 && decoded.endsWith('/')) {
    decoded = decoded.slice(0, -1)
  }

  return decoded
}

function parseRequestUrl(url: unknown): ParsedRequestUrl | null {
  if (typeof url !== 'string' || url.trim() === '') {
    return null
  }

  const base = getBaseOrigin()
  try {
    const parsed = new URL(url.trim(), base)
    return {
      path: normalizePathForRouting(parsed.pathname),
      sameOrigin: parsed.origin === base,
    }
  } catch {
    return null
  }
}

/**
 * Whether the read-only frontend must reject this request.
 *
 * Absolute URLs that point at a different host are normally third-party
 * services the UI legitimately reads (the GitHub release feed, an OIDC
 * well-known document, a user-supplied speed-test target), so they are allowed.
 * The exception is an absolute URL that carries the gateway's own `/api`
 * prefix: that is a direct call to a real backend, which would skip the proxy
 * enforcing these same rules.
 */
export function isReadonlyBlockedRequest(
  method: unknown,
  url: unknown
): boolean {
  const normalizedMethod = String(method ?? 'get').toLowerCase()
  if (!READONLY_SAFE_METHODS.has(normalizedMethod)) {
    return true
  }

  const parsed = parseRequestUrl(url)
  if (!parsed) {
    return false
  }
  if (READONLY_BLOCKED_PATHS.some((pattern) => pattern.test(parsed.path))) {
    return true
  }
  return !parsed.sameOrigin && parsed.path.startsWith('/api/')
}

function rejectReadonlyRequest(): never {
  throw new Error(READONLY_FRONTEND_MESSAGE)
}

// The wrapper currently installed on globalThis.fetch, if any. Kept so the
// guard can tell "already installed" apart from "someone replaced fetch after
// the guard was installed" -- the latter silently disables the read-only layer,
// so the install must run again over the new function.
let installedFetchWrapper: typeof globalThis.fetch | null = null

/**
 * Installs the guard on the global `fetch`.
 *
 * The axios instance covers the API helpers, but `fetch` is used directly in
 * several places (the Ollama pull stream, the status bootstrap) and any future
 * call site would silently bypass the guard. Patching the global makes the
 * guard a layer instead of an opt-in helper: a new direct `fetch` to the
 * backend is blocked without anyone having to remember this module.
 *
 * Only requests the guard classifies as unsafe are intercepted; third-party
 * reads (the GitHub release feed, an OIDC well-known document, a speed-test
 * target) and local blob/data URLs keep working.
 */
export function installReadonlyFetchGuard(): void {
  if (!IS_READONLY_FRONTEND) {
    return
  }
  if (typeof globalThis.fetch !== 'function') {
    return
  }
  if (
    installedFetchWrapper !== null &&
    globalThis.fetch === installedFetchWrapper
  ) {
    return
  }

  const originalFetch = globalThis.fetch.bind(globalThis)
  const wrapper = ((input: RequestInfo | URL, init?: RequestInit) => {
    const method =
      init?.method ??
      (typeof Request !== 'undefined' && input instanceof Request
        ? input.method
        : 'get')
    const url =
      typeof input === 'string'
        ? input
        : input instanceof URL
          ? input.href
          : input.url
    if (isReadonlyBlockedRequest(method, url)) {
      return Promise.reject(new Error(READONLY_FRONTEND_MESSAGE))
    }
    return originalFetch(input, init)
  }) as typeof globalThis.fetch
  globalThis.fetch = wrapper
  installedFetchWrapper = wrapper
}

/**
 * Installs the guard on an axios instance. A rejected request interceptor makes
 * axios fail the request before any socket is opened, and the instance's error
 * interceptor surfaces READONLY_FRONTEND_MESSAGE as a toast.
 */
export function applyReadonlyRequestGuard(instance: AxiosInstance): void {
  instance.interceptors.request.use((config) => {
    if (!IS_READONLY_FRONTEND) {
      return config
    }
    if (isReadonlyBlockedRequest(config.method, config.url)) {
      rejectReadonlyRequest()
    }
    return config
  })
}
