import type { AxiosRequestConfig } from 'axios'
import { api } from './api'

type DesktopRuntime = {
  isDesktopApp: boolean
  openExternalUrl?: (url: string) => Promise<void> | void
}

declare global {
  interface Window {
    __NEW_API_DESKTOP_RUNTIME__?: DesktopRuntime
  }
}

const POLL_INTERVAL_MS = 1000
const POLL_TIMEOUT_MS = 2 * 60 * 1000

export function isDesktopApp(): boolean {
  return (
    typeof window !== 'undefined' &&
    window.__NEW_API_DESKTOP_RUNTIME__?.isDesktopApp === true
  )
}

async function openExternalUrl(url: string): Promise<void> {
  const runtime = window.__NEW_API_DESKTOP_RUNTIME__
  if (typeof runtime?.openExternalUrl === 'function') {
    await runtime.openExternalUrl(url)
    return
  }
  window.open(url, '_blank', 'noopener,noreferrer')
}

export async function startDesktopOAuth(
  provider: string,
  mode: 'login' | 'bind',
  affCode = ''
): Promise<{ state: string; handoffToken: string }> {
  const response = await api.get('/api/oauth/desktop/start', {
    params: { provider, mode, ...(affCode ? { aff: affCode } : {}) },
    skipBusinessError: true,
  } as AxiosRequestConfig)
  if (!response.data?.success) {
    throw new Error(response.data?.message || 'Failed to start desktop OAuth')
  }
  const state = response.data?.data?.state
  const handoffToken = response.data?.data?.handoff_token
  if (typeof state !== 'string' || typeof handoffToken !== 'string') {
    throw new Error('Desktop OAuth returned incomplete handoff data')
  }
  return { state, handoffToken }
}

export async function completeDesktopOAuth(
  handoffToken: string
): Promise<{ action: 'login' | 'bind'; user?: unknown }> {
  const deadline = Date.now() + POLL_TIMEOUT_MS
  while (Date.now() < deadline) {
    const response = await api.get('/api/oauth/desktop/poll', {
      params: { handoff_token: handoffToken },
      skipBusinessError: true,
      disableDuplicate: true,
    } as AxiosRequestConfig)
    if (!response.data?.success) {
      throw new Error(response.data?.message || 'Desktop OAuth failed')
    }
    const data = response.data?.data
    if (data?.status === 'pending') {
      await new Promise((resolve) => setTimeout(resolve, POLL_INTERVAL_MS))
      continue
    }
    if (data?.action === 'bind') return { action: 'bind' }
    if (data && typeof data === 'object') {
      return { action: 'login', user: data }
    }
    throw new Error('Desktop OAuth did not return a login result')
  }
  throw new Error('Desktop OAuth timed out waiting for the browser')
}

export async function runDesktopOAuth(
  provider: string,
  mode: 'login' | 'bind',
  buildUrl: (state: string) => string,
  affCode = ''
) {
  const { state, handoffToken } = await startDesktopOAuth(
    provider,
    mode,
    affCode
  )
  await openExternalUrl(buildUrl(state))
  return completeDesktopOAuth(handoffToken)
}
