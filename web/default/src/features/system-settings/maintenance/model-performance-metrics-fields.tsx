import type { UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import type { PerfFormValues } from './performance-types'

export function ModelPerformanceMetricsFields(props: {
  form: UseFormReturn<PerfFormValues>
  enabled: boolean
}) {
  const { t } = useTranslation()

  return (
    <>
      <div>
        <h4 className='font-medium'>{t('Model performance metrics')}</h4>
        <p className='text-muted-foreground mt-1 text-xs'>
          {t(
            'Collect relay latency and success-rate metrics for the model square.'
          )}
        </p>
      </div>

      <div className='grid grid-cols-1 gap-4 md:grid-cols-4'>
        <FormField
          control={props.form.control}
          name='perf_metrics_setting.enabled'
          render={({ field }) => (
            <FormItem className='flex items-center gap-2'>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
              <FormLabel>{t('Enable model performance metrics')}</FormLabel>
            </FormItem>
          )}
        />
        <FormField
          control={props.form.control}
          name='perf_metrics_setting.flush_interval'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Flush interval (minutes)')}</FormLabel>
              <FormControl>
                <Input
                  type='number'
                  min={1}
                  {...field}
                  disabled={!props.enabled}
                />
              </FormControl>
            </FormItem>
          )}
        />
        <FormField
          control={props.form.control}
          name='perf_metrics_setting.bucket_time'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Aggregation bucket')}</FormLabel>
              <Select
                value={field.value}
                onValueChange={field.onChange}
                disabled={!props.enabled}
              >
                <FormControl>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                </FormControl>
                <SelectContent>
                  <SelectItem value='minute'>{t('1 minute')}</SelectItem>
                  <SelectItem value='5min'>{t('5 minutes')}</SelectItem>
                  <SelectItem value='hour'>{t('1 hour')}</SelectItem>
                </SelectContent>
              </Select>
            </FormItem>
          )}
        />
        <FormField
          control={props.form.control}
          name='perf_metrics_setting.retention_days'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Retention days')}</FormLabel>
              <FormControl>
                <Input
                  type='number'
                  min={0}
                  {...field}
                  disabled={!props.enabled}
                />
              </FormControl>
              <FormDescription>
                {t('0 means data is kept permanently')}
              </FormDescription>
            </FormItem>
          )}
        />
      </div>
    </>
  )
}
