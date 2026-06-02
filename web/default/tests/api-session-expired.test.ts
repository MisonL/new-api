import { afterEach, beforeEach, describe, expect, mock, test } from 'bun:test'
import type { AxiosAdapter } from 'axios'

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

const { api } = await import('../src/lib/api')
const { useAuthStore } = await import('../src/stores/auth-store')

const originalReset = useAuthStore.getState().auth.reset
const runtimeConsole = globalThis['console']
const originalConsoleError = runtimeConsole.error
const consoleError = mock(() => undefined)

function failing401Adapter(): AxiosAdapter {
  return (config) =>
    Promise.reject({
      config,
      response: {
        status: 401,
        data: {},
      },
    })
}

async function trigger401WithReset(reset: () => void) {
  useAuthStore.setState((state) => ({
    ...state,
    auth: {
      ...state.auth,
      reset,
    },
  }))
  await expect(
    api.get('/api/session-expired-test', {
      adapter: failing401Adapter(),
    })
  ).rejects.toBeTruthy()
}

describe('api 401 session reset handling', () => {
  beforeEach(() => {
    toastError.mockClear()
    translate.mockClear()
    consoleError.mockClear()
    runtimeConsole.error = consoleError as typeof runtimeConsole.error
  })

  afterEach(() => {
    useAuthStore.setState((state) => ({
      ...state,
      auth: {
        ...state.auth,
        reset: originalReset,
      },
    }))
    runtimeConsole.error = originalConsoleError
  })

  test('keeps the session expired message when reset throws a non-empty Error', async () => {
    await trigger401WithReset(() => {
      throw new Error('custom reset failure')
    })

    expect(toastError).toHaveBeenCalledWith('Session expired!')
    expect(consoleError).toHaveBeenCalled()
  })

  test('falls back when reset throws a non-Error value', async () => {
    await trigger401WithReset(() => {
      throw 'reset failed'
    })

    expect(toastError).toHaveBeenCalledWith('Session expired!')
    expect(consoleError).toHaveBeenCalled()
  })

  test('falls back when reset throws an Error with an empty message', async () => {
    await trigger401WithReset(() => {
      throw new Error('')
    })

    expect(toastError).toHaveBeenCalledWith('Session expired!')
    expect(consoleError).toHaveBeenCalled()
  })
})
