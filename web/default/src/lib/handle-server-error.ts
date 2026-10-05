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

export function handleServerError(error: unknown) {
  let errMsg = i18next.t('Something went wrong!')

  if (
    error &&
    typeof error === 'object' &&
    'status' in error &&
    Number(error.status) === 204
  ) {
    errMsg = i18next.t('Content not found.')
  } else if (error instanceof AxiosError) {
    errMsg = backendMessage(error.response?.data) ?? errMsg
  }

  toast.error(errMsg)
}
