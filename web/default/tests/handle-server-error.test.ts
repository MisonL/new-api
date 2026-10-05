import { beforeEach, describe, expect, mock, test } from 'bun:test'
import { AxiosError, AxiosHeaders } from 'axios'

const toastError = mock((_message?: string) => undefined)
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

const { handleServerError } = await import('../src/lib/handle-server-error')

// Every backend rejection funnels through handleServerError, so the message the
// backend actually sent is the only diagnostic the user ever sees. types.OpenAIError
// serialises to `{message, success}`; reading any other field discards it entirely.
function backendError(data: unknown, status = 400) {
  const config = { headers: new AxiosHeaders() }
  return new AxiosError('Request failed with status code', String(status), config, undefined, {
    data,
    status,
    statusText: '',
    headers: {},
    config,
  })
}

function lastToastMessage(): unknown {
  const call = toastError.mock.calls.at(-1)
  return call?.[0]
}

describe('handleServerError', () => {
  beforeEach(() => {
    toastError.mockClear()
    translate.mockClear()
  })

  test('shows the message field of a backend error body', () => {
    handleServerError(
      backendError({
        message: 'Unauthorized, not logged in and no access token provided',
        success: false,
      }, 401)
    )

    expect(lastToastMessage()).toBe(
      'Unauthorized, not logged in and no access token provided'
    )
  })

  test('falls back to the generic message when the body carries none', () => {
    handleServerError(backendError({ success: false }, 500))

    expect(lastToastMessage()).toBe('Something went wrong!')
  })

  test('falls back to the generic message when the response body is missing', () => {
    handleServerError(new AxiosError('Network Error', 'ERR_NETWORK', undefined))

    expect(lastToastMessage()).toBe('Something went wrong!')
  })

  test('a 204 response keeps the not-found copy instead of an empty toast', () => {
    handleServerError(backendError({ message: 'ignored', success: false }, 204))

    expect(lastToastMessage()).toBe('Content not found.')
  })
})