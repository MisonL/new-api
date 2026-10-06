import { afterAll, beforeAll, describe, expect, mock, test } from 'bun:test'
import type { AxiosAdapter, AxiosRequestConfig } from 'axios'

// The guard reads its flag once at module load, so it must be set before the
// module under test is imported.
import.meta.env.VITE_REACT_APP_READONLY_MODE = 'true'

const toastError = mock(() => undefined)
const translate = mock((key: string) => key)

mock.module('sonner', () => ({
  toast: {
    error: toastError,
  },
}))

mock.module('i18next', () => ({
  default: {
    t: translate,
  },
}))

// The guard patches the global fetch, so the stub has to be installed before
// the module that installs it is loaded.
const originalFetch = globalThis.fetch
const fetchSpy = mock((_input: unknown, _init?: unknown) =>
  Promise.resolve(new Response('{}', { status: 200 }))
)
globalThis.fetch = fetchSpy as unknown as typeof globalThis.fetch

const { api } = await import('../src/lib/api')
const {
  IS_READONLY_FRONTEND,
  READONLY_FRONTEND_MESSAGE,
  isReadonlyBlockedRequest,
} = await import('../src/lib/readonly-guard')

beforeAll(() => {
  expect(IS_READONLY_FRONTEND).toBe(true)
})

afterAll(() => {
  globalThis.fetch = originalFetch
})

/**
 * An adapter that records the request instead of sending it. Reaching the
 * adapter means the request interceptor let the call through.
 */
function recordingAdapter(seen: AxiosRequestConfig[]): AxiosAdapter {
  return (config) => {
    seen.push(config)
    return Promise.resolve({
      data: { success: true, data: null },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    })
  }
}

describe('readonly guard blocks unsafe requests', () => {
  test('rejects every non-safe HTTP method', async () => {
    for (const method of ['post', 'put', 'patch', 'delete']) {
      const seen: AxiosRequestConfig[] = []
      const request = api.request({
        method,
        url: '/api/status',
        adapter: recordingAdapter(seen),
      })
      await expect(request).rejects.toThrow(READONLY_FRONTEND_MESSAGE)
      expect(seen).toHaveLength(0)
    }
  })

  test('rejects the auth and account-lifecycle entry points', async () => {
    const blocked = [
      '/api/oauth/github',
      '/api/oauth/state',
      '/api/auth/external/foo/cas/start',
      '/api/user/logout',
      '/api/setup',
      '/api/verification',
      '/api/reset_password',
    ]
    for (const url of blocked) {
      const seen: AxiosRequestConfig[] = []
      await expect(
        api.get(url, { adapter: recordingAdapter(seen) })
      ).rejects.toThrow(READONLY_FRONTEND_MESSAGE)
      expect(seen).toHaveLength(0)
    }
  })

  // These routes are registered with GET in router/api-router.go but write
  // state, so the proxy's method check alone does not stop them. Each one was
  // read in the handler before being listed here.
  test('rejects GET endpoints that mutate state', async () => {
    const blocked = [
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
    ]
    for (const url of blocked) {
      const seen: AxiosRequestConfig[] = []
      await expect(
        api.get(url, { adapter: recordingAdapter(seen) })
      ).rejects.toThrow(READONLY_FRONTEND_MESSAGE)
      expect(seen).toHaveLength(0)
    }
  })

  test('blocks an absolute URL that points at a gateway /api path', async () => {
    expect(
      isReadonlyBlockedRequest(
        'get',
        'http://127.0.0.1:3000/api/channel/update_balance'
      )
    ).toBe(true)
  })
})

describe('readonly guard leaves legitimate reads alone', () => {
  test('passes safe reads through to the adapter', async () => {
    const seen: AxiosRequestConfig[] = []
    await api.get('/api/status', { adapter: recordingAdapter(seen) })
    await api.get('/api/pricing', { adapter: recordingAdapter(seen) })
    await api.get('/api/channel/12', { adapter: recordingAdapter(seen) })
    expect(seen).toHaveLength(3)
  })

  test('does not confuse a longer path with a blocked one', () => {
    // `/api/channel/test` is blocked; the unrelated model-test config read and
    // the channel detail route must not be caught by the same pattern.
    expect(isReadonlyBlockedRequest('get', '/api/channel/test-model')).toBe(
      false
    )
    expect(isReadonlyBlockedRequest('get', '/api/channel/12')).toBe(false)
    expect(
      isReadonlyBlockedRequest('get', '/api/user/aff_transfer_records')
    ).toBe(false)
  })

  test('allows third-party reads the UI legitimately performs', () => {
    expect(
      isReadonlyBlockedRequest(
        'get',
        'https://api.github.com/repos/Calcium-Ion/new-api/releases/latest'
      )
    ).toBe(false)
    expect(
      isReadonlyBlockedRequest('get', 'https://idp.example.com/.well-known/x')
    ).toBe(false)
    expect(isReadonlyBlockedRequest('get', 'blob:http://localhost/abc')).toBe(
      false
    )
  })
})

describe('readonly guard sees through path disguises', () => {
  // URL.pathname keeps the spelling the caller used, but the router does not:
  // it percent-decodes the path and answers a single trailing slash with a
  // redirect to the canonical form. Each URL below was measured against the
  // running backend and reaches the same handler as its canonical spelling,
  // while the raw pathname misses the blocklist's anchored patterns.
  test('rejects blocked endpoints written in another spelling', async () => {
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
    ]
    for (const url of disguises) {
      const seen: AxiosRequestConfig[] = []
      await expect(
        api.get(url, { adapter: recordingAdapter(seen) })
      ).rejects.toThrow(READONLY_FRONTEND_MESSAGE)
      expect(seen).toHaveLength(0)
    }
  })

  // Reverse check: normalising must not turn ordinary reads into blocks.
  test('still lets ordinary reads through', () => {
    expect(isReadonlyBlockedRequest('get', '/api/status')).toBe(false)
    expect(isReadonlyBlockedRequest('get', '/api/user/self')).toBe(false)
    expect(isReadonlyBlockedRequest('get', '/api/channel/12')).toBe(false)
    // An encoded slash inside a path segment is a legitimate model id, not a
    // disguised route boundary.
    expect(
      isReadonlyBlockedRequest('get', '/api/pricing/model%2Fname')
    ).toBe(false)
    expect(
      isReadonlyBlockedRequest('get', '/api/user/aff_transfer_records')
    ).toBe(false)
    // Measured counter-examples. The router strips neither matrix parameters
    // nor an encoded space, so these land on /api/user/:id -- a different
    // handler -- and must keep passing.
    expect(isReadonlyBlockedRequest('get', '/api/user/token;a=b')).toBe(false)
    expect(isReadonlyBlockedRequest('get', '/api/user/token%20')).toBe(false)
    // Nor does it collapse a doubled slash or redirect a repeated trailing
    // slash: both are rejected as Invalid URL rather than reaching the token
    // handler, so blocking them would be a false block.
    expect(isReadonlyBlockedRequest('get', '/api//user/token')).toBe(false)
    expect(isReadonlyBlockedRequest('get', '/api/user/token//')).toBe(false)
  })
})

describe('readonly guard covers direct fetch call sites', () => {
  test('rejects the Ollama pull stream before it reaches the network', async () => {
    fetchSpy.mockClear()
    await expect(
      fetch('/api/channel/ollama/pull/stream', {
        method: 'POST',
        body: '{}',
      })
    ).rejects.toThrow(READONLY_FRONTEND_MESSAGE)
    expect(fetchSpy).not.toHaveBeenCalled()
  })

  test('still forwards an allowed status read to the real fetch', async () => {
    fetchSpy.mockClear()
    await fetch('/api/status')
    expect(fetchSpy).toHaveBeenCalledTimes(1)
  })
})
