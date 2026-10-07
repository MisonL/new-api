import { beforeEach, describe, expect, mock, test } from 'bun:test'
import { AxiosError, type AxiosAdapter, type AxiosRequestConfig } from 'axios'
import { MutationObserver, QueryClient } from '@tanstack/react-query'

// readonly-guard.test.ts 在模块加载时读取这个开关，而 bun 的
// import.meta.env 只读文件自己的赋值、值不跨文件传播。本文件排在它前面
// 加载 @/lib/api，开关不一致会让对方的 beforeAll 断言失败；两边必须
// 约定同一个值。
import.meta.env.VITE_REACT_APP_READONLY_MODE = 'true'

const toastError = mock((..._args: unknown[]) => undefined)

mock.module('sonner', () => ({
  toast: {
    error: toastError,
    success: mock(() => undefined),
    warning: mock(() => undefined),
    info: mock(() => undefined),
  },
}))
mock.module('i18next', () => ({ default: { t: (key: string) => key } }))

const { api } = await import('@/lib/api')
const { handleServerError } = await import('@/lib/handle-server-error')

// axios 拦截器在 reject 前已 toast 过的错误，React Query 的默认
// mutations.onError（main.tsx 里就是 handleServerError）会再 toast 一次，
// 用户看到两个同因不同文案的提示。

function axiosReject(config: AxiosRequestConfig, status: number, data?: unknown) {
  return new AxiosError(
    `Request failed with status code ${status}`,
    'ERR_BAD_REQUEST',
    config,
    {},
    { status, statusText: '', headers: {}, config, data: data ?? {} },
  )
}

// 驱动与 main.tsx 相同的接线：无本地 onError 的 mutation 走默认 onError。
async function runMutationRejection(status: number, data?: unknown) {
  const config: AxiosRequestConfig = { url: `/api/toast-probe-${status}` }
  api.defaults.adapter = ((_config: AxiosRequestConfig) =>
    Promise.reject(axiosReject(config, status, data))) as AxiosAdapter

  const queryClient = new QueryClient({
    defaultOptions: { mutations: { onError: handleServerError } },
  })
  const observer = new MutationObserver(queryClient, {
    mutationFn: () => api.get(config.url!).then((response) => response.data),
  })
  await observer.mutate().catch(() => undefined)
  await new Promise((resolve) => setTimeout(resolve, 0))
  queryClient.clear()
}

beforeEach(() => {
  toastError.mockClear()
})

describe('handleServerError on a rejection the interceptor already toasted', () => {
  test('a failing mutation produces exactly one toast', async () => {
    await runMutationRejection(500, { message: 'upstream exploded' })

    expect(toastError).toHaveBeenCalledTimes(1)
  })

  test('a 304 mutation rejection produces exactly one toast', async () => {
    await runMutationRejection(304)

    expect(toastError).toHaveBeenCalledTimes(1)
  })
})

describe('handleServerError for callers that skipped the interceptor', () => {
  test('an unmarked error is still reported once', () => {
    handleServerError(axiosReject({}, 500, { message: 'upstream exploded' }))

    expect(toastError).toHaveBeenCalledTimes(1)
    expect(toastError.mock.calls[0][0]).toContain('upstream exploded')
  })

  test('an unmarked 304 reports the not-modified message', () => {
    handleServerError(axiosReject({}, 304))

    expect(toastError).toHaveBeenCalledTimes(1)
    expect(toastError.mock.calls[0][0]).toBe('Content not modified!')
  })
})
