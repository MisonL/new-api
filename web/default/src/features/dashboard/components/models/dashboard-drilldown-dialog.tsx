import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { getCurrencyDisplay } from '@/lib/currency'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import type { DashboardDrilldownDetail } from '../../lib/drilldown'

interface DashboardDrilldownDialogProps {
  detail: DashboardDrilldownDetail | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function DashboardDrilldownDialog(props: DashboardDrilldownDialogProps) {
  const { t } = useTranslation()
  const quotaFormatter = useMemo(() => createQuotaFormatter(), [])
  const integerFormatter = useMemo(
    () => new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }),
    []
  )
  const detail = props.detail

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='max-h-[calc(100dvh-2rem)] gap-3 overflow-hidden p-4 sm:max-w-3xl sm:p-5'>
        <DialogHeader className='gap-1'>
          <DialogTitle className='text-base'>
            {detail?.time || t('Quota Distribution')}
          </DialogTitle>
          <DialogDescription>
            {t('Total:')} {quotaFormatter(detail?.totalQuota || 0)}
          </DialogDescription>
        </DialogHeader>

        <div className='grid grid-cols-3 gap-2 text-sm'>
          <SummaryValue
            label={t('Quota')}
            value={quotaFormatter(detail?.totalQuota || 0)}
          />
          <SummaryValue
            label={t('Requests')}
            value={integerFormatter.format(detail?.totalCount || 0)}
          />
          <SummaryValue
            label={t('Tokens')}
            value={integerFormatter.format(detail?.totalTokens || 0)}
          />
        </div>

        <div className='min-h-0 overflow-auto rounded-md border'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Model')}</TableHead>
                <TableHead className='text-right'>{t('Quota')}</TableHead>
                <TableHead className='text-right'>{t('Requests')}</TableHead>
                <TableHead className='text-right'>{t('Tokens')}</TableHead>
                <TableHead className='text-right'>{t('Ratio')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {detail?.rows.map((row) => (
                <TableRow key={row.model}>
                  <TableCell className='font-medium'>{row.model}</TableCell>
                  <TableCell className='text-right'>
                    {quotaFormatter(row.quota)}
                  </TableCell>
                  <TableCell className='text-right'>
                    {integerFormatter.format(row.count)}
                  </TableCell>
                  <TableCell className='text-right'>
                    {integerFormatter.format(row.tokens)}
                  </TableCell>
                  <TableCell className='text-right'>
                    {(row.ratio * 100).toFixed(1)}%
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </DialogContent>
    </Dialog>
  )
}

function SummaryValue(props: { label: string; value: string }) {
  return (
    <div className='bg-muted/40 rounded-md border px-3 py-2'>
      <div className='text-muted-foreground text-xs'>{props.label}</div>
      <div className='mt-1 truncate font-semibold'>{props.value}</div>
    </div>
  )
}

function createQuotaFormatter() {
  const { config, meta } = getCurrencyDisplay()
  return (rawQuota: number) => {
    if (meta.kind === 'tokens') return rawQuota.toLocaleString()
    const rate = 'exchangeRate' in meta ? meta.exchangeRate : 1
    const symbol = 'symbol' in meta ? meta.symbol : '$'
    return symbol + ((rawQuota / config.quotaPerUnit) * rate).toFixed(4)
  }
}
