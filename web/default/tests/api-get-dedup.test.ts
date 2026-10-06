import { beforeEach, describe, expect, mock, test } from 'bun:test'
import axios, { type AxiosAdapter, type AxiosRequestConfig } from 'axios'

mock.module('sonner', () => ({ toast: { error: () => undefined } }))
mock.module('i18next', () => ({ default: { t: (key: string) => key } }))

// 不 mock @/stores/auth-store：bun 的 mock.module 对同进程的其他测试文件同样生效，
// mock 掉真实 store 会让 api-session-expired.test.ts 的 setState 失效。
const { api } = await import('@/lib/api')

// 受控 adapter：记录每个底层请求，把 resolve/reject 交给测试，并按 axios 的
// 真实语义把 config.signal 接到取消上（axios 的 adapter 负责这件事）。
type PendingRequest = {
  config: AxiosRequestConfig
  resolve: (value: unknown) => void
  reject: (reason: unknown) => void
}
let pending: PendingRequest[] = []

api.defaults.adapter = ((config: AxiosRequestConfig) => {
  return new Promise((resolve, reject) => {
    const entry: PendingRequest = { config, resolve, reject }
    pending.push(entry)
    const signal = config.signal as AbortSignal | undefined
    if (signal) {
      if (signal.aborted) {
        reject(new axios.CanceledError('canceled'))
        return
      }
      signal.addEventListener('abort', () => reject(new axios.CanceledError('canceled')), {
        once: true,
      })
    }
  })
}) as AxiosAdapter

function respond(index: number, data: unknown) {
  const entry = pending[index]
  entry.resolve({
    data,
    status: 200,
    statusText: 'OK',
    headers: {},
    config: entry.config,
  })
}

// axios 的拦截器链是 promise 驱动的，adapter 在微任务里才被调用；
// 断言前必须把微任务冲刷干净，否则看到的是空数组。
async function flushDispatch() {
  for (let i = 0; i < 8; i++) {
    await new Promise((resolve) => setTimeout(resolve, 0))
  }
}

beforeEach(() => {
  pending = []
})

describe('api.get request deduplication', () => {
  test('concurrent signal-less GETs to the same URL share one request', async () => {
    const a = api.get('/api/dedup-probe-1')
    const b = api.get('/api/dedup-probe-1')
    await flushDispatch()

    expect(pending.length).toBe(1)

    respond(0, { success: true, data: 'ok' })
    await expect(a).resolves.toBeDefined()
    await expect(b).resolves.toBeDefined()
  })

  test('signal-bearing GETs keep their own lifecycle instead of joining the dedup map', async () => {
    const first = new AbortController()
    const second = new AbortController()
    const a = api.get('/api/dedup-probe-2', { signal: first.signal })
    const b = api.get('/api/dedup-probe-2', { signal: second.signal })
    await flushDispatch()

    // 一个共享 promise 无法同时满足两份独立的取消生命周期：A 的 abort 会连带
    // 取消 B，B 的 abort 又永远到不了底层请求。带 signal 的调用必须各自成请求。
    expect(pending.length).toBe(2)

    first.abort()
    await expect(a).rejects.toBeDefined()

    respond(1, { success: true, data: 'ok' })
    await expect(b).resolves.toBeDefined()
  })

  test("aborting a signal-bearing caller must not cancel a signal-less caller's request", async () => {
    const first = new AbortController()
    const a = api.get('/api/dedup-probe-3', { signal: first.signal })
    const b = api.get('/api/dedup-probe-3')
    await flushDispatch()

    expect(pending.length).toBe(2)

    first.abort()
    await expect(a).rejects.toBeDefined()

    respond(1, { success: true, data: 'ok' })
    await expect(b).resolves.toBeDefined()
  })
})
