import type { UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import {
  formatBytes,
  type PerfFormValues,
  type PerformanceStats,
} from './performance-types'

export function DiskCacheFields(props: {
  form: UseFormReturn<PerfFormValues>
  stats: PerformanceStats | null
  enabled: boolean
  lowDiskSpace: boolean | 0 | undefined
  maxCacheSizeMb: number
}) {
  const { t } = useTranslation()

  return (
    <>
      <div>
        <h4 className='font-medium'>{t('Disk Cache Settings')}</h4>
        <p className='text-muted-foreground mt-1 text-xs'>
          {t(
            'When enabled, large request bodies are temporarily stored on disk instead of memory, significantly reducing memory usage. SSD recommended.'
          )}
        </p>
      </div>

      <div className='grid grid-cols-1 gap-4 md:grid-cols-3'>
        <FormField
          control={props.form.control}
          name='performance_setting.disk_cache_enabled'
          render={({ field }) => (
            <FormItem className='flex items-center gap-2'>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
              <FormLabel>{t('Enable Disk Cache')}</FormLabel>
            </FormItem>
          )}
        />
        <FormField
          control={props.form.control}
          name='performance_setting.disk_cache_threshold_mb'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Disk Cache Threshold (MB)')}</FormLabel>
              <FormControl>
                <Input type='number' {...field} disabled={!props.enabled} />
              </FormControl>
              <FormDescription>
                {t('Use disk cache when request body exceeds this size')}
              </FormDescription>
            </FormItem>
          )}
        />
        <FormField
          control={props.form.control}
          name='performance_setting.disk_cache_max_size_mb'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Max Disk Cache Size (MB)')}</FormLabel>
              <FormControl>
                <Input type='number' {...field} disabled={!props.enabled} />
              </FormControl>
              {props.stats?.disk_space_info &&
                props.stats.disk_space_info.total > 0 && (
                  <FormDescription>
                    {t('Free: {{free}} / Total: {{total}}', {
                      free: formatBytes(props.stats.disk_space_info.free),
                      total: formatBytes(props.stats.disk_space_info.total),
                    })}
                  </FormDescription>
                )}
            </FormItem>
          )}
        />
      </div>

      {props.lowDiskSpace && (
        <Alert variant='destructive'>
          <AlertDescription>
            {`${t('Warning')}: ${t('Available disk space')} (${formatBytes(
              props.stats?.disk_space_info?.free ?? 0
            )}) ${t('is less than the configured maximum cache size')} (${
              props.maxCacheSizeMb
            } MB). ${t('This may cause cache failures.')}`}
          </AlertDescription>
        </Alert>
      )}

      {!props.stats?.config?.is_running_in_container && (
        <FormField
          control={props.form.control}
          name='performance_setting.disk_cache_path'
          render={({ field }) => (
            <FormItem className='max-w-md'>
              <FormLabel>{t('Cache Directory')}</FormLabel>
              <FormControl>
                <Input
                  placeholder={t('Leave empty to use system temp directory')}
                  {...field}
                  value={field.value ?? ''}
                  disabled={!props.enabled}
                />
              </FormControl>
            </FormItem>
          )}
        />
      )}
    </>
  )
}
