import { AxiosError } from 'axios'
import i18next from 'i18next'
import { toast } from 'sonner'

// The backend serialises every failure as {message, success} (types.OpenAIError),
// so `message` is the only place the real diagnostic lives.
function backendMessage(data: unknown): string | undefined {
  if (!data || typeof data !== 'object' || !('message' in data)) {
    return undefined
  }
  const message = (data as { message?: unknown }).message
  return typeof message === 'string' && message !== '' ? message : undefined
}

// Errors the axios interceptor already toasted carry this marker so the
// React Query default handlers (main.tsx wires handleServerError into the
// mutation onError) do not emit a second toast for the same failure with
// different wording. The symbol is non-enumerable where defined on objects,
// so serialized error logs are unchanged.
const TOAST_SHOWN = Symbol.for('new-api.toastShown')

export function markToastShown(error: unknown): void {
  if (error === null || typeof error !== 'object') return
  if ((error as Record<symbol, unknown>)[TOAST_SHOWN]) return
  try {
    Object.defineProperty(error, TOAST_SHOWN, {
      value: true,
      enumerable: false,
      configurable: true,
    })
  } catch {
    // A frozen error object simply never carries the marker.
  }
}

export function wasToastShown(error: unknown): boolean {
  return (
    typeof error === 'object' &&
    error !== null &&
    Boolean((error as Record<symbol, unknown>)[TOAST_SHOWN])
  )
}

export function handleServerError(error: unknown) {
  if (wasToastShown(error)) {
    return
  }

  let errMsg = i18next.t('Something went wrong!')

  if (
    error &&
    typeof error === 'object' &&
    'status' in error &&
    Number(error.status) === 204
  ) {
    errMsg = i18next.t('Content not found.')
  } else if (error instanceof AxiosError) {
    if (error.response?.status === 304) {
      errMsg = i18next.t('Content not modified!')
    } else {
      errMsg = backendMessage(error.response?.data) ?? errMsg
    }
  }

  markToastShown(error)
  toast.error(errMsg)
}
