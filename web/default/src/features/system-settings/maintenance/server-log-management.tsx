import dayjs from '@/lib/dayjs'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { formatBytes, type LogInfo } from './performance-types'

export function ServerLogManagement(props: {
  t: (key: string, options?: Record<string, unknown>) => string
  logInfo: LogInfo | null
  cleanupMode: string
  cleanupValue: number
  cleanupLoading: boolean
  onModeChange: (value: string) => void
  onValueChange: (value: number) => void
  onCleanup: () => void
}) {
  const t = props.t

  return (
    <div className='space-y-4'>
      <div>
        <h4 className='font-medium'>{t('Server Log Management')}</h4>
        <p className='text-muted-foreground mt-1 text-xs'>
          {t(
            'Manage server log files. Log files accumulate over time; regular cleanup is recommended to free disk space.'
          )}
        </p>
      </div>

      {props.logInfo === null ? null : props.logInfo.enabled ? (
        <div className='space-y-4'>
          <div className='rounded-lg border p-4'>
            <div className='grid grid-cols-2 gap-2 text-sm md:grid-cols-4'>
              <div>
                <span className='text-muted-foreground'>
                  {t('Log Directory')}:
                </span>{' '}
                <span className='font-mono text-xs'>
                  {props.logInfo.log_dir}
                </span>
              </div>
              <div>
                <span className='text-muted-foreground'>
                  {t('Log File Count')}:
                </span>{' '}
                {props.logInfo.file_count}
              </div>
              <div>
                <span className='text-muted-foreground'>
                  {t('Total Log Size')}:
                </span>{' '}
                {formatBytes(props.logInfo.total_size)}
              </div>
              {props.logInfo.oldest_time && props.logInfo.newest_time && (
                <div>
                  <span className='text-muted-foreground'>
                    {t('Date Range')}:
                  </span>{' '}
                  {dayjs(props.logInfo.oldest_time).format('YYYY-MM-DD')} ~{' '}
                  {dayjs(props.logInfo.newest_time).format('YYYY-MM-DD')}
                </div>
              )}
            </div>
          </div>

          <div className='flex flex-wrap items-end gap-3'>
            <div className='grid gap-1.5'>
              <Label className='text-xs'>{t('Cleanup Mode')}</Label>
              <Select
                value={props.cleanupMode}
                onValueChange={props.onModeChange}
              >
                <SelectTrigger className='w-[160px]'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='by_count'>
                    {t('Retain last N files')}
                  </SelectItem>
                  <SelectItem value='by_days'>
                    {t('Retain last N days')}
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className='grid gap-1.5'>
              <Label className='text-xs'>
                {props.cleanupMode === 'by_count'
                  ? t('Files to Retain')
                  : t('Days to Retain')}
              </Label>
              <Input
                type='number'
                min={1}
                max={props.cleanupMode === 'by_count' ? 1000 : 3650}
                value={props.cleanupValue}
                onChange={(event) =>
                  props.onValueChange(Number(event.target.value))
                }
                className='w-[120px]'
              />
            </div>
            <AlertDialog>
              <AlertDialogTrigger asChild>
                <Button
                  variant='destructive'
                  size='sm'
                  disabled={props.cleanupLoading}
                >
                  {props.cleanupLoading
                    ? t('Cleaning...')
                    : t('Clean Up Log Files')}
                </Button>
              </AlertDialogTrigger>
              <AlertDialogContent>
                <AlertDialogHeader>
                  <AlertDialogTitle>
                    {t('Confirm log file cleanup?')}
                  </AlertDialogTitle>
                  <AlertDialogDescription>
                    {props.cleanupMode === 'by_count'
                      ? t(
                          'Only the last {{value}} log files will be retained; the rest will be deleted.',
                          { value: props.cleanupValue }
                        )
                      : t(
                          'Log files older than {{value}} days will be deleted.',
                          {
                            value: props.cleanupValue,
                          }
                        )}
                  </AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter>
                  <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
                  <AlertDialogAction onClick={props.onCleanup}>
                    {t('Confirm Cleanup')}
                  </AlertDialogAction>
                </AlertDialogFooter>
              </AlertDialogContent>
            </AlertDialog>
          </div>
        </div>
      ) : (
        <Alert>
          <AlertDescription>
            {t('Server logging is not enabled (log directory not configured)')}
          </AlertDescription>
        </Alert>
      )}
    </div>
  )
}
