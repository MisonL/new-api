import { useEffect, useState } from 'react'
import { BarChart3, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatQuota, formatTimestamp } from '@/lib/format'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { getInvitationRecords } from '../../api'
import type { InvitationRecord } from '../../types'

interface InvitationRecordsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function InvitationRecordsDialog(props: InvitationRecordsDialogProps) {
  const { t } = useTranslation()
  const [records, setRecords] = useState<InvitationRecord[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [keyword, setKeyword] = useState('')
  const [recordType, setRecordType] = useState('all')
  const [source, setSource] = useState('all')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!props.open) return
    let cancelled = false
    queueMicrotask(() => {
      if (!cancelled) setLoading(true)
    })
    void getInvitationRecords(page, pageSize, {
      keyword,
      record_type: recordType === 'all' ? '' : recordType,
      source: source === 'all' ? '' : source,
    })
      .then((response) => {
        if (cancelled) return
        if (response.success && response.data) {
          setRecords(response.data.items || [])
          setTotal(response.data.total || 0)
        }
      })
      .catch(() => undefined)
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [keyword, page, pageSize, props.open, recordType, source])

  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='max-h-[calc(100dvh-2rem)] max-w-4xl'>
        <DialogHeader>
          <DialogTitle>
            <BarChart3 className='mr-2 inline size-4' />
            {t('Invitation Records')}
          </DialogTitle>
          <DialogDescription>
            {t('Review registration rewards and referral rebates.')}
          </DialogDescription>
        </DialogHeader>
        <div className='flex flex-wrap gap-2'>
          <Input
            className='min-w-48 flex-1'
            value={keyword}
            onChange={(e) => {
              setKeyword(e.target.value)
              setPage(1)
            }}
            placeholder={t('Search invitee')}
          />
          <Select
            value={recordType}
            onValueChange={(value) => {
              setRecordType(value)
              setPage(1)
            }}
          >
            <SelectTrigger className='w-36'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='all'>{t('All types')}</SelectItem>
              <SelectItem value='register'>
                {t('Registration reward')}
              </SelectItem>
              <SelectItem value='topup_rebate'>{t('Topup rebate')}</SelectItem>
            </SelectContent>
          </Select>
          <Select
            value={source}
            onValueChange={(value) => {
              setSource(value)
              setPage(1)
            }}
          >
            <SelectTrigger className='w-36'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='all'>{t('All sources')}</SelectItem>
              <SelectItem value='register'>{t('Registration')}</SelectItem>
              <SelectItem value='stripe'>Stripe</SelectItem>
              <SelectItem value='creem'>Creem</SelectItem>
              <SelectItem value='waffo'>Waffo</SelectItem>
              <SelectItem value='admin'>{t('Admin')}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className='max-h-[50vh] overflow-auto rounded-md border'>
          {loading ? (
            <div className='flex h-40 items-center justify-center'>
              <Loader2 className='size-5 animate-spin' />
            </div>
          ) : records.length === 0 ? (
            <div className='text-muted-foreground p-8 text-center text-sm'>
              {t('No invitation records found')}
            </div>
          ) : (
            <table className='w-full text-sm'>
              <thead className='bg-muted/50'>
                <tr className='border-b text-left'>
                  <th className='p-2'>{t('Invitee')}</th>
                  <th className='p-2'>{t('Type')}</th>
                  <th className='p-2'>{t('Source')}</th>
                  <th className='p-2'>{t('Topup')}</th>
                  <th className='p-2'>{t('Reward')}</th>
                  <th className='p-2'>{t('Time')}</th>
                </tr>
              </thead>
              <tbody>
                {records.map((record) => (
                  <tr key={record.id} className='border-b last:border-0'>
                    <td className='p-2'>
                      {record.invitee_username || record.invitee_id}
                    </td>
                    <td className='p-2'>
                      {record.record_type === 'topup_rebate'
                        ? t('Topup rebate')
                        : t('Registration reward')}
                    </td>
                    <td className='p-2'>{record.source || '-'}</td>
                    <td className='p-2'>
                      {record.topup_quota
                        ? formatQuota(record.topup_quota)
                        : '-'}
                    </td>
                    <td className='p-2'>
                      {formatQuota(record.reward_quota || 0)}
                    </td>
                    <td className='p-2 whitespace-nowrap'>
                      {formatTimestamp(record.created_time)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
        <div className='flex items-center justify-between gap-2 text-sm'>
          <span className='text-muted-foreground'>
            {t('{{count}} records', { count: total })}
          </span>
          <div className='flex items-center gap-2'>
            <Select
              value={String(pageSize)}
              onValueChange={(value) => {
                setPageSize(Number(value))
                setPage(1)
              }}
            >
              <SelectTrigger className='w-28'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='10'>10 / {t('page')}</SelectItem>
                <SelectItem value='20'>20 / {t('page')}</SelectItem>
                <SelectItem value='50'>50 / {t('page')}</SelectItem>
              </SelectContent>
            </Select>
            <Button
              variant='outline'
              size='sm'
              disabled={page <= 1}
              onClick={() => setPage((current) => current - 1)}
            >
              {t('Previous')}
            </Button>
            <span>
              {page} / {totalPages}
            </span>
            <Button
              variant='outline'
              size='sm'
              disabled={page >= totalPages}
              onClick={() => setPage((current) => current + 1)}
            >
              {t('Next')}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
