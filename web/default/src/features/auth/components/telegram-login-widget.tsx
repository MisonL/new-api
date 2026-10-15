import { useEffect, useId, useRef, useState } from 'react'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { TelegramAuthData } from './telegram-auth'

interface TelegramLoginWidgetProps {
  botName: string
  onAuth: (data: TelegramAuthData) => void | Promise<void>
  className?: string
}

export function TelegramLoginWidget(props: TelegramLoginWidgetProps) {
  const { t } = useTranslation()
  const { botName, onAuth, className } = props
  const scriptHostRef = useRef<HTMLDivElement>(null)
  const onAuthRef = useRef(onAuth)
  const reactId = useId()
  const callbackName = `newApiTelegramAuth_${reactId.replace(/:/g, '')}`
  const [loadedBotName, setLoadedBotName] = useState<string | null>(null)
  const [loadFailedBotName, setLoadFailedBotName] = useState<string | null>(
    null
  )
  const normalizedBotName = botName.trim()
  const loading =
    Boolean(normalizedBotName) &&
    loadedBotName !== normalizedBotName &&
    loadFailedBotName !== normalizedBotName
  const failed =
    Boolean(normalizedBotName) && loadFailedBotName === normalizedBotName

  useEffect(() => {
    onAuthRef.current = onAuth
  }, [onAuth])

  useEffect(() => {
    const scriptHost = scriptHostRef.current
    if (!scriptHost || !normalizedBotName) return

    let script: HTMLScriptElement | null = document.createElement('script')
    const callback = (payload: TelegramAuthData) => {
      void onAuthRef.current(payload)
    }
    ;(window as unknown as Record<string, unknown>)[callbackName] = callback
    script.async = true
    script.src = 'https://telegram.org/js/telegram-widget.js?22'
    script.dataset.telegramLogin = normalizedBotName
    script.dataset.size = 'large'
    script.dataset.userpic = 'false'
    script.dataset.requestAccess = 'write'
    // Telegram requires the callback expression to include its user argument.
    // A bare function name is not invoked by the widget script.
    script.dataset.onauth = `${callbackName}(user)`
    // A failed load must not look like a successful one: the iframe is never
    // injected, so reporting success left an empty bordered box with no
    // explanation. telegram.org is unreachable from some networks.
    script.onload = () => {
      setLoadedBotName(normalizedBotName)
      setLoadFailedBotName(null)
    }
    script.onerror = () => {
      setLoadFailedBotName(normalizedBotName)
      setLoadedBotName(null)
    }
    scriptHost.replaceChildren(script)

    return () => {
      delete (window as unknown as Record<string, unknown>)[callbackName]
      script?.remove()
      script = null
    }
  }, [normalizedBotName, callbackName])

  return (
    <div className={className}>
      <div ref={scriptHostRef} />
      {loading && (
        <span className='text-muted-foreground inline-flex items-center gap-2 text-sm'>
          <Loader2 className='size-4 animate-spin' />
          {t('Loading Telegram login...')}
        </span>
      )}
      {failed && (
        <span className='text-destructive text-sm'>
          {t('Failed to load Telegram login. Check your network and reload.')}
        </span>
      )}
    </div>
  )
}
