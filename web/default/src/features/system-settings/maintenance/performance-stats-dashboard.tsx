import { useTranslation } from 'react-i18next'
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
import { Progress } from '@/components/ui/progress'
import { StatusBadge } from '@/components/status-badge'
import { formatBytes, type PerformanceStats } from './performance-types'

export function PerformanceStatsDashboard(props: {
  stats: PerformanceStats | null
  diskCachePercent: number
  onRefresh: () => void
  onClearDiskCache: () => void
  onResetStats: () => void
  onForceGC: () => void
}) {
  const { t } = useTranslation()

  return (
    <div className='space-y-4'>
      <div className='flex items-center gap-2'>
        <h4 className='font-medium'>{t('Performance Monitor')}</h4>
        <Button variant='outline' size='sm' onClick={props.onRefresh}>
          {t('Refresh Stats')}
        </Button>
        <AlertDialog>
          <AlertDialogTrigger asChild>
            <Button variant='outline' size='sm'>
              {t('Clean up inactive cache')}
            </Button>
          </AlertDialogTrigger>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>
                {t('Confirm cleanup of inactive disk cache?')}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {t(
                  'This will delete temporary cache files that have not been used for more than 10 minutes'
                )}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
              <AlertDialogAction onClick={props.onClearDiskCache}>
                {t('Confirm')}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
        <Button variant='outline' size='sm' onClick={props.onResetStats}>
          {t('Reset Stats')}
        </Button>
        <Button variant='outline' size='sm' onClick={props.onForceGC}>
          {t('Run GC')}
        </Button>
      </div>

      {props.stats && (
        <>
          <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
            <div className='space-y-2 rounded-lg border p-4'>
              <p className='text-sm font-medium'>
                {t('Request Body Disk Cache')}
              </p>
              <Progress value={props.diskCachePercent} />
              <div className='text-muted-foreground flex justify-between text-xs'>
                <span>
                  {formatBytes(
                    props.stats.cache_stats?.current_disk_usage_bytes ?? 0
                  )}{' '}
                  /{' '}
                  {formatBytes(
                    props.stats.cache_stats?.disk_cache_max_bytes ?? 0
                  )}
                </span>
                <span>
                  {t('Active Files')}:{' '}
                  {props.stats.cache_stats?.active_disk_files ?? 0}
                </span>
              </div>
              <StatusBadge variant='neutral' copyable={false}>
                {t('Disk Hits')}:{' '}
                {props.stats.cache_stats?.disk_cache_hits ?? 0}
              </StatusBadge>
            </div>
            <div className='space-y-2 rounded-lg border p-4'>
              <p className='text-sm font-medium'>
                {t('Request Body Memory Cache')}
              </p>
              <div className='text-muted-foreground flex justify-between text-xs'>
                <span>
                  {t('Current Cache Size')}:{' '}
                  {formatBytes(
                    props.stats.cache_stats?.current_memory_usage_bytes ?? 0
                  )}
                </span>
                <span>
                  {t('Active Cache Count')}:{' '}
                  {props.stats.cache_stats?.active_memory_buffers ?? 0}
                </span>
              </div>
              <StatusBadge variant='neutral' copyable={false}>
                {t('Memory Hits')}:{' '}
                {props.stats.cache_stats?.memory_cache_hits ?? 0}
              </StatusBadge>
            </div>
          </div>

          {props.stats.disk_space_info &&
            props.stats.disk_space_info.total > 0 && (
              <div className='rounded-lg border p-4'>
                <p className='mb-2 text-sm font-medium'>
                  {t('Cache Directory Disk Space')}
                </p>
                <Progress
                  value={Math.round(props.stats.disk_space_info.used_percent)}
                />
                <div className='text-muted-foreground mt-2 flex justify-between text-xs'>
                  <span>
                    {t('Used')}: {formatBytes(props.stats.disk_space_info.used)}
                  </span>
                  <span>
                    {t('Available')}:{' '}
                    {formatBytes(props.stats.disk_space_info.free)}
                  </span>
                  <span>
                    {t('Total')}:{' '}
                    {formatBytes(props.stats.disk_space_info.total)}
                  </span>
                </div>
              </div>
            )}

          {props.stats.memory_stats && (
            <div className='rounded-lg border p-4'>
              <p className='mb-2 text-sm font-medium'>
                {t('System Memory Stats')}
              </p>
              <div className='grid grid-cols-2 gap-2 text-xs md:grid-cols-5'>
                <div>
                  <span className='text-muted-foreground'>
                    {t('Allocated Memory')}:
                  </span>{' '}
                  {formatBytes(props.stats.memory_stats.alloc)}
                </div>
                <div>
                  <span className='text-muted-foreground'>
                    {t('Total Allocated')}:
                  </span>{' '}
                  {formatBytes(props.stats.memory_stats.total_alloc)}
                </div>
                <div>
                  <span className='text-muted-foreground'>
                    {t('System Memory')}:
                  </span>{' '}
                  {formatBytes(props.stats.memory_stats.sys)}
                </div>
                <div>
                  <span className='text-muted-foreground'>
                    {t('GC Count')}:
                  </span>{' '}
                  {props.stats.memory_stats.num_gc}
                </div>
                <div>
                  <span className='text-muted-foreground'>Goroutines:</span>{' '}
                  {props.stats.memory_stats.num_goroutine}
                </div>
              </div>
            </div>
          )}

          {props.stats.disk_cache_info && (
            <div className='rounded-lg border p-4'>
              <p className='mb-2 text-sm font-medium'>
                {t('Cache Directory Info')}
              </p>
              <div className='grid grid-cols-3 gap-2 text-xs'>
                <div>
                  <span className='text-muted-foreground'>
                    {t('Cache Directory')}:
                  </span>{' '}
                  <span className='font-mono'>
                    {props.stats.disk_cache_info.path}
                  </span>
                </div>
                <div>
                  <span className='text-muted-foreground'>
                    {t('Directory File Count')}:
                  </span>{' '}
                  {props.stats.disk_cache_info.file_count}
                </div>
                <div>
                  <span className='text-muted-foreground'>
                    {t('Directory Total Size')}:
                  </span>{' '}
                  {formatBytes(props.stats.disk_cache_info.total_size)}
                </div>
              </div>
            </div>
          )}
        </>
      )}
    </div>
  )
}
