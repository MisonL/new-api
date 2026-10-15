import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Gift, Inbox, Link2, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  formatQuota,
  parseQuotaFromDollars,
  parseTimestampFromInput,
  formatTimestamp,
} from '@/lib/format'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import {
  createGiftCode,
  getGiftCode,
  listGiftCodes,
  receiveGiftCode,
} from '../api'
import type { GiftCode, UserWalletData } from '../types'

interface GiftCodeCardProps {
  user: UserWalletData | null
  onRefresh: () => Promise<void>
  initialGiftCode?: string
  onInitialGiftCodeHandled?: () => void
}

function statusLabel(t: (key: string) => string, code: GiftCode): string {
  if (code.status === 3) return t('Received')
  if (code.status === 2) return t('Disabled')
  if (code.expired_time && code.expired_time < Math.floor(Date.now() / 1000)) {
    return t('Expired')
  }
  return t('Available')
}

export function GiftCodeCard(props: GiftCodeCardProps) {
  const { t } = useTranslation()
  const { copyToClipboard } = useCopyToClipboard({ notify: false })
  const { initialGiftCode, onInitialGiftCodeHandled, onRefresh } = props
  const lastInitialGiftCodeRef = useRef<string | null>(null)
  const [codes, setCodes] = useState<GiftCode[]>([])
  const [quota, setQuota] = useState('')
  const [receiverType, setReceiverType] = useState('none')
  const [receiverValue, setReceiverValue] = useState('')
  const [expiresAt, setExpiresAt] = useState('')
  const [message, setMessage] = useState('')
  const [loading, setLoading] = useState(false)
  const [receiveLoading, setReceiveLoading] = useState(false)
  const [receiveCode, setReceiveCode] = useState<GiftCode | null>(null)
  const [thankMessage, setThankMessage] = useState('')

  const minAmount = useMemo(() => 1, [])
  const linkFor = (code: string) =>
    `${window.location.origin}/wallet?gift_code=${encodeURIComponent(code)}`

  const loadCodes = useCallback(async () => {
    try {
      const response = await listGiftCodes()
      if (response.success) setCodes(response.data ?? [])
      else toast.error(response.message || t('Failed to load gift codes'))
    } catch (_error) {
      toast.error(t('Failed to load gift codes'))
    }
  }, [t])

  const openReceive = useCallback(
    async (code: string) => {
      setReceiveLoading(true)
      try {
        const response = await getGiftCode(code)
        if (response.success && response.data) {
          setReceiveCode(response.data)
          onInitialGiftCodeHandled?.()
        } else toast.error(response.message || t('Invalid gift code'))
      } catch (_error) {
        toast.error(t('Failed to load gift code'))
      } finally {
        setReceiveLoading(false)
      }
    },
    [onInitialGiftCodeHandled, t]
  )

  useEffect(() => {
    void loadCodes()
  }, [loadCodes])

  useEffect(() => {
    const code = initialGiftCode?.trim() || ''
    if (!code) {
      lastInitialGiftCodeRef.current = null
      return
    }
    if (lastInitialGiftCodeRef.current === code) return
    lastInitialGiftCodeRef.current = code
    void openReceive(code)
  }, [initialGiftCode, openReceive])

  const create = async () => {
    const amount = Number(quota)
    if (!Number.isFinite(amount) || amount < minAmount) {
      toast.error(t('Gift code amount is below the minimum'))
      return
    }
    const quotaUnits = parseQuotaFromDollars(amount)
    if (receiverType !== 'none' && !receiverValue.trim()) {
      toast.error(t('Enter a recipient'))
      return
    }
    setLoading(true)
    try {
      const response = await createGiftCode({
        quota: quotaUnits,
        receiver_bind_type: receiverType === 'none' ? '' : receiverType,
        receiver_bind_value:
          receiverType === 'none' ? '' : receiverValue.trim(),
        expired_time: expiresAt ? parseTimestampFromInput(expiresAt) : 0,
        message: message.trim(),
      })
      if (!response.success || !response.data) {
        toast.error(response.message || t('Failed to create gift code'))
        return
      }
      const link = linkFor(response.data.code)
      // The link is the only delivery path for a gift code, so a failed copy
      // must not be reported as success.
      const copied = await copyToClipboard(link)
      if (copied) {
        toast.success(t('Gift code created and link copied'))
      } else {
        toast.warning(t('Gift code created, but the link could not be copied'))
      }
      setQuota('')
      setReceiverValue('')
      setMessage('')
      setExpiresAt('')
      await loadCodes()
      await onRefresh()
    } catch (_error) {
      toast.error(t('Failed to create gift code'))
    } finally {
      setLoading(false)
    }
  }

  const receive = async () => {
    if (!receiveCode) return
    setReceiveLoading(true)
    try {
      const response = await receiveGiftCode(receiveCode.code, {
        thank_message: thankMessage.trim(),
      })
      if (!response.success) {
        toast.error(response.message || t('Failed to receive gift code'))
        return
      }
      toast.success(t('Gift code received'))
      setReceiveCode(null)
      setThankMessage('')
      await loadCodes()
      await onRefresh()
    } catch (_error) {
      toast.error(t('Failed to receive gift code'))
    } finally {
      setReceiveLoading(false)
    }
  }

  return (
    <>
      <Card>
        <CardHeader className='flex flex-row items-center gap-2 pb-3'>
          <Gift className='size-4' />
          <div>
            <h3 className='font-semibold'>{t('Gift Codes')}</h3>
            <p className='text-muted-foreground text-xs'>
              {t('Create a balance gift code for another user')}
            </p>
          </div>
        </CardHeader>
        <CardContent className='space-y-3'>
          <div className='grid gap-3 sm:grid-cols-2'>
            <div className='space-y-1.5'>
              <Label htmlFor='gift-quota'>{t('Amount')}</Label>
              <Input
                id='gift-quota'
                type='number'
                min={minAmount}
                value={quota}
                onChange={(e) => setQuota(e.target.value)}
                placeholder={t('Amount in display currency')}
              />
            </div>
            <div className='space-y-1.5'>
              <Label htmlFor='gift-expiry'>{t('Expiration')}</Label>
              <Input
                id='gift-expiry'
                type='datetime-local'
                value={expiresAt}
                onChange={(e) => setExpiresAt(e.target.value)}
              />
            </div>
          </div>
          <div className='grid gap-3 sm:grid-cols-2'>
            <div className='space-y-1.5'>
              <Label>{t('Recipient binding')}</Label>
              <Select value={receiverType} onValueChange={setReceiverType}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='none'>{t('No binding')}</SelectItem>
                  <SelectItem value='username'>{t('Username')}</SelectItem>
                  <SelectItem value='id'>{t('User ID')}</SelectItem>
                  <SelectItem value='email'>{t('Email')}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className='space-y-1.5'>
              <Label htmlFor='gift-recipient'>{t('Recipient')}</Label>
              <Input
                id='gift-recipient'
                value={receiverValue}
                disabled={receiverType === 'none'}
                onChange={(e) => setReceiverValue(e.target.value)}
              />
            </div>
          </div>
          <Textarea
            value={message}
            onChange={(e) => setMessage(e.target.value)}
            maxLength={500}
            placeholder={t('Optional message')}
          />
          <Button onClick={create} disabled={loading || !props.user}>
            {loading && <Loader2 className='mr-2 size-4 animate-spin' />}
            {t('Create Gift Code')}
          </Button>
          {codes.length > 0 && (
            <div className='space-y-2 border-t pt-3'>
              <Label>{t('Recently created')}</Label>
              {codes.slice(0, 5).map((code) => (
                <div
                  key={code.id}
                  className='flex items-center justify-between gap-2 rounded-md border p-2 text-sm'
                >
                  <div className='min-w-0'>
                    <div className='flex gap-2'>
                      <span>{formatQuota(code.quota)}</span>
                      <span className='text-muted-foreground'>
                        {statusLabel(t, code)}
                      </span>
                    </div>
                    <div className='text-muted-foreground text-xs'>
                      {formatTimestamp(code.created_time)}
                    </div>
                  </div>
                  <Button
                    size='sm'
                    variant='outline'
                    onClick={() => void copyToClipboard(linkFor(code.code))}
                    aria-label={t('Copy gift code link')}
                  >
                    <Link2 className='size-4' />
                  </Button>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      <Dialog
        open={receiveCode !== null}
        onOpenChange={(open) => !open && setReceiveCode(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              <Inbox className='mr-2 inline size-4' />
              {t('Receive Gift Code')}
            </DialogTitle>
            <DialogDescription>
              {t('Review the gift code before adding it to your balance.')}
            </DialogDescription>
          </DialogHeader>
          {receiveCode && (
            <div className='space-y-3 py-2'>
              <p>
                {t('From')}: {receiveCode.creator_username || '-'}
              </p>
              <p>
                {t('Amount')}: {formatQuota(receiveCode.quota)}
              </p>
              {receiveCode.message && (
                <p>
                  {t('Message')}: {receiveCode.message}
                </p>
              )}
              <Textarea
                value={thankMessage}
                onChange={(e) => setThankMessage(e.target.value)}
                maxLength={500}
                placeholder={t('Optional thank-you message')}
              />
            </div>
          )}
          <DialogFooter>
            <Button variant='outline' onClick={() => setReceiveCode(null)}>
              {t('Cancel')}
            </Button>
            <Button onClick={receive} disabled={receiveLoading}>
              {receiveLoading && (
                <Loader2 className='mr-2 size-4 animate-spin' />
              )}
              {t('Receive')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
