import { useEffect, useRef } from 'react'

declare global {
  interface Window {
    turnstile?: {
      render: (
        element: HTMLElement,
        options: Record<string, unknown>
      ) => string | null | undefined
      remove: (widgetId: string) => void
    }
  }
}

const TURNSTILE_SCRIPT_ID = 'cf-turnstile'
const TURNSTILE_SCRIPT_SRC =
  'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit'
const TURNSTILE_READY_RETRY_LIMIT = 20
const TURNSTILE_READY_RETRY_MS = 50
const TURNSTILE_LOADED_DATA_KEY = 'turnstileLoaded'

function createTurnstileScript() {
  const script = document.createElement('script')
  script.id = TURNSTILE_SCRIPT_ID
  script.src = TURNSTILE_SCRIPT_SRC
  script.async = true
  return script
}

function isTurnstileScriptLoaded(script: HTMLElement) {
  if (!(script instanceof HTMLScriptElement)) return false
  const readyState = (script as HTMLScriptElement & { readyState?: string })
    .readyState
  return (
    script.dataset[TURNSTILE_LOADED_DATA_KEY] === 'true' ||
    readyState === 'loaded' ||
    readyState === 'complete'
  )
}

interface TurnstileProps {
  siteKey: string
  onVerify: (token: string) => void
  onExpire?: () => void
  onError?: (errorCode?: string) => void
  className?: string
}

export function Turnstile({
  siteKey,
  onVerify,
  onExpire,
  onError,
  className,
}: TurnstileProps) {
  const ref = useRef<HTMLDivElement | null>(null)
  const handlersRef = useRef({ onVerify, onExpire, onError })
  const renderedSiteKeyRef = useRef<string | null>(null)
  const widgetIdRef = useRef<string | null>(null)

  useEffect(() => {
    handlersRef.current = { onVerify, onExpire, onError }
  }, [onVerify, onExpire, onError])

  useEffect(() => {
    let cancelled = false
    let retryTimer: number | undefined

    const removeWidget = () => {
      const widgetId = widgetIdRef.current
      if (!widgetId || !window.turnstile) return
      try {
        window.turnstile.remove(widgetId)
      } catch (error) {
        // eslint-disable-next-line no-console
        console.error('Failed to remove Turnstile widget:', error)
      } finally {
        widgetIdRef.current = null
        renderedSiteKeyRef.current = null
      }
    }

    const clearRetryTimer = () => {
      if (retryTimer === undefined) return
      window.clearTimeout(retryTimer)
      retryTimer = undefined
    }

    const render = () => {
      if (
        cancelled ||
        !ref.current ||
        !window.turnstile ||
        (renderedSiteKeyRef.current === siteKey && widgetIdRef.current)
      )
        return
      removeWidget()
      try {
        const widgetId = window.turnstile.render(ref.current, {
          sitekey: siteKey,
          callback: (token: string) => handlersRef.current.onVerify(token),
          // Keep validation errors separate from expiration events.
          'error-callback': (errorCode?: string) => {
            if (handlersRef.current.onError)
              handlersRef.current.onError(errorCode)
            else {
              // eslint-disable-next-line no-console
              console.error('Turnstile validation error:', errorCode)
            }
          },
          'expired-callback': () => handlersRef.current.onExpire?.(),
        })
        if (!widgetId) {
          // eslint-disable-next-line no-console
          console.error('Turnstile widget did not return an id')
          return
        }
        widgetIdRef.current = widgetId
        renderedSiteKeyRef.current = siteKey
      } catch (error) {
        // eslint-disable-next-line no-console
        console.error('Failed to render Turnstile widget:', error)
      }
    }

    const retryRenderWhenReady = (attempt = 0) => {
      if (cancelled) return
      if (window.turnstile) {
        render()
        return
      }
      if (attempt >= TURNSTILE_READY_RETRY_LIMIT) {
        // eslint-disable-next-line no-console
        console.error('Turnstile script loaded without window.turnstile')
        return
      }
      retryTimer = window.setTimeout(
        () => retryRenderWhenReady(attempt + 1),
        TURNSTILE_READY_RETRY_MS
      )
    }

    if (window.turnstile) {
      render()
      return () => {
        cancelled = true
        clearRetryTimer()
        removeWidget()
      }
    }
    const handleScriptError = () => {
      if (cancelled) return
      // eslint-disable-next-line no-console
      console.error('Failed to load Turnstile script')
    }

    const bindScript = (script: HTMLElement) => {
      const handleScriptLoad = () => {
        if (script instanceof HTMLScriptElement) {
          script.dataset[TURNSTILE_LOADED_DATA_KEY] = 'true'
        }
        retryRenderWhenReady()
      }

      script.addEventListener('load', handleScriptLoad, { once: true })
      script.addEventListener('error', handleScriptError, { once: true })
      return () => {
        cancelled = true
        clearRetryTimer()
        script.removeEventListener('load', handleScriptLoad)
        script.removeEventListener('error', handleScriptError)
        removeWidget()
      }
    }

    const existingScript = document.getElementById(TURNSTILE_SCRIPT_ID)
    if (existingScript) {
      const cleanup = bindScript(existingScript)
      if (isTurnstileScriptLoaded(existingScript)) retryRenderWhenReady()
      return cleanup
    }

    const s = createTurnstileScript()
    const cleanup = bindScript(s)
    document.head.appendChild(s)
    return cleanup
  }, [siteKey])

  return <div ref={ref} className={className} />
}
