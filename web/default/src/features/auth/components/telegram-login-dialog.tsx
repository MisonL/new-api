import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { toast } from 'sonner'
import {
  telegramAuthParams,
  type TelegramAuthData,
} from './telegram-auth'
import { telegramLoginByParams } from '../api'
import { TelegramLoginWidget } from './telegram-login-widget'

interface TelegramLoginDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  botName: string
  onSuccess: (data: { require_2fa?: boolean; id?: number } | null) => void
}

export function TelegramLoginDialog(props: TelegramLoginDialogProps) {
  const { t } = useTranslation()
  const [submitting, setSubmitting] = useState(false)

  const handleAuth = async (data: TelegramAuthData) => {
    setSubmitting(true)
    try {
      const res = await telegramLoginByParams(telegramAuthParams(data))
      if (res?.success) {
        props.onOpenChange(false)
        props.onSuccess(
          (res.data ?? null) as { require_2fa?: boolean; id?: number } | null
        )
        return
      }
      toast.error(res?.message || t('Telegram login failed'))
    } catch (_error) {
      toast.error(t('Telegram login failed'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Continue with Telegram')}</DialogTitle>
          <DialogDescription>
            {t(
              'Authorize via Telegram to sign in with your linked account.'
            )}
          </DialogDescription>
        </DialogHeader>
        <div className='flex flex-col items-center gap-4 py-2'>
          <TelegramLoginWidget
            botName={props.botName}
            onAuth={handleAuth}
            className={submitting ? 'pointer-events-none opacity-60' : ''}
          />
        </div>
      </DialogContent>
    </Dialog>
  )
}
