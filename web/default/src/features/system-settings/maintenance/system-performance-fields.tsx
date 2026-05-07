import type { UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import type { PerfFormValues } from './performance-types'

export function SystemPerformanceFields(props: {
  form: UseFormReturn<PerfFormValues>
  enabled: boolean
}) {
  const { t } = useTranslation()

  return (
    <>
      <div>
        <h4 className='font-medium'>{t('System Performance Monitoring')}</h4>
        <p className='text-muted-foreground mt-1 text-xs'>
          {t(
            'When performance monitoring is enabled and system resource usage exceeds the set threshold, new Relay requests will be rejected.'
          )}
        </p>
      </div>

      <div className='grid grid-cols-1 gap-4 md:grid-cols-4'>
        <FormField
          control={props.form.control}
          name='performance_setting.monitor_enabled'
          render={({ field }) => (
            <FormItem className='flex items-center gap-2'>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
              <FormLabel>{t('Enable Performance Monitoring')}</FormLabel>
            </FormItem>
          )}
        />
        <FormField
          control={props.form.control}
          name='performance_setting.monitor_cpu_threshold'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('CPU Threshold (%)')}</FormLabel>
              <FormControl>
                <Input type='number' {...field} disabled={!props.enabled} />
              </FormControl>
            </FormItem>
          )}
        />
        <FormField
          control={props.form.control}
          name='performance_setting.monitor_memory_threshold'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Memory Threshold (%)')}</FormLabel>
              <FormControl>
                <Input type='number' {...field} disabled={!props.enabled} />
              </FormControl>
            </FormItem>
          )}
        />
        <FormField
          control={props.form.control}
          name='performance_setting.monitor_disk_threshold'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Disk Threshold (%)')}</FormLabel>
              <FormControl>
                <Input type='number' {...field} disabled={!props.enabled} />
              </FormControl>
            </FormItem>
          )}
        />
      </div>
    </>
  )
}
