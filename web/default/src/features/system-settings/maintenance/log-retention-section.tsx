import { useEffect, useRef, useState, type ReactNode } from 'react'
import * as z from 'zod'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQueryClient } from '@tanstack/react-query'
import { useForm, type Path, type UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { LazyMount } from '@/components/lazy-mount'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { executeLogRetention } from '../api'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import type {
  LogRetentionExecutionResult,
  LogRetentionPolicyResult,
  PayloadRetentionResult,
} from '../types'
import { formatBytes } from './performance-types'

const logRetentionSchema = z.object({
  log_retention_setting: z.object({
    enabled: z.boolean(),
    run_interval_hours: z.number().min(1).max(720),
    batch_size: z.number().min(1).max(5000),
    max_batches: z.number().min(1).max(1000),
    consume_retention_days: z.number().min(0).max(3650),
    error_retention_days: z.number().min(0).max(3650),
    system_retention_days: z.number().min(0).max(3650),
    manage_retention_days: z.number().min(0).max(3650),
    topup_retention_days: z.number().min(0).max(3650),
    refund_retention_days: z.number().min(0).max(3650),
    unknown_retention_days: z.number().min(0).max(3650),
    request_payload_retention_days: z.number().min(0).max(3650),
    response_payload_retention_days: z.number().min(0).max(3650),
    server_log_cleanup_enabled: z.boolean(),
    server_log_keep_files: z.number().min(0).max(10000),
    server_log_keep_days: z.number().min(0).max(3650),
    server_log_max_total_size_mb: z.number().min(0).max(1048576),
  }),
})

type LogRetentionFormState = z.infer<typeof logRetentionSchema>
type LogRetentionSettingValues = LogRetentionFormState['log_retention_setting']
type LogRetentionSettingKey = keyof LogRetentionSettingValues
type LogRetentionFieldName =
  `log_retention_setting.${LogRetentionSettingKey & string}`

export type LogRetentionFormValues = {
  [K in LogRetentionSettingKey as `log_retention_setting.${K & string}`]: LogRetentionSettingValues[K]
}

type LogRetentionSectionProps = {
  defaultValues: LogRetentionFormValues
}

type NumberFieldConfig = {
  name: LogRetentionFieldName
  label: string
  description?: string
  min: number
  max: number
}

type ChangedLogRetentionEntry = [
  LogRetentionFieldName,
  string | number | boolean,
]

const toFormValues = (
  values: LogRetentionFormValues
): LogRetentionFormState => ({
  log_retention_setting: {
    enabled: values['log_retention_setting.enabled'],
    run_interval_hours: values['log_retention_setting.run_interval_hours'],
    batch_size: values['log_retention_setting.batch_size'],
    max_batches: values['log_retention_setting.max_batches'],
    consume_retention_days:
      values['log_retention_setting.consume_retention_days'],
    error_retention_days: values['log_retention_setting.error_retention_days'],
    system_retention_days:
      values['log_retention_setting.system_retention_days'],
    manage_retention_days:
      values['log_retention_setting.manage_retention_days'],
    topup_retention_days: values['log_retention_setting.topup_retention_days'],
    refund_retention_days:
      values['log_retention_setting.refund_retention_days'],
    unknown_retention_days:
      values['log_retention_setting.unknown_retention_days'],
    request_payload_retention_days:
      values['log_retention_setting.request_payload_retention_days'],
    response_payload_retention_days:
      values['log_retention_setting.response_payload_retention_days'],
    server_log_cleanup_enabled:
      values['log_retention_setting.server_log_cleanup_enabled'],
    server_log_keep_files:
      values['log_retention_setting.server_log_keep_files'],
    server_log_keep_days:
      values['log_retention_setting.server_log_keep_days'],
    server_log_max_total_size_mb:
      values['log_retention_setting.server_log_max_total_size_mb'],
  },
})

const toFlatValues = (
  values: LogRetentionFormState
): LogRetentionFormValues => ({
  'log_retention_setting.enabled': values.log_retention_setting.enabled,
  'log_retention_setting.run_interval_hours':
    values.log_retention_setting.run_interval_hours,
  'log_retention_setting.batch_size':
    values.log_retention_setting.batch_size,
  'log_retention_setting.max_batches':
    values.log_retention_setting.max_batches,
  'log_retention_setting.consume_retention_days':
    values.log_retention_setting.consume_retention_days,
  'log_retention_setting.error_retention_days':
    values.log_retention_setting.error_retention_days,
  'log_retention_setting.system_retention_days':
    values.log_retention_setting.system_retention_days,
  'log_retention_setting.manage_retention_days':
    values.log_retention_setting.manage_retention_days,
  'log_retention_setting.topup_retention_days':
    values.log_retention_setting.topup_retention_days,
  'log_retention_setting.refund_retention_days':
    values.log_retention_setting.refund_retention_days,
  'log_retention_setting.unknown_retention_days':
    values.log_retention_setting.unknown_retention_days,
  'log_retention_setting.request_payload_retention_days':
    values.log_retention_setting.request_payload_retention_days,
  'log_retention_setting.response_payload_retention_days':
    values.log_retention_setting.response_payload_retention_days,
  'log_retention_setting.server_log_cleanup_enabled':
    values.log_retention_setting.server_log_cleanup_enabled,
  'log_retention_setting.server_log_keep_files':
    values.log_retention_setting.server_log_keep_files,
  'log_retention_setting.server_log_keep_days':
    values.log_retention_setting.server_log_keep_days,
  'log_retention_setting.server_log_max_total_size_mb':
    values.log_retention_setting.server_log_max_total_size_mb,
})

const serializeFlatValues = (values: LogRetentionFormValues) =>
  JSON.stringify(values)

const hasUnsavedLogRetentionChanges = (
  form: UseFormReturn<LogRetentionFormState>,
  baseline: LogRetentionFormValues
) =>
  (
    Object.entries(toFlatValues(form.getValues())) as ChangedLogRetentionEntry[]
  ).some(([key, value]) => value !== baseline[key])

const schedulerFields: NumberFieldConfig[] = [
  {
    name: 'log_retention_setting.run_interval_hours',
    label: 'Execution interval',
    description: 'Hours between automatic cleanup runs.',
    min: 1,
    max: 720,
  },
  {
    name: 'log_retention_setting.batch_size',
    label: 'Batch size',
    description: 'Rows processed per database cleanup batch.',
    min: 1,
    max: 5000,
  },
  {
    name: 'log_retention_setting.max_batches',
    label: 'Max batches per run',
    description: 'Upper bound for one automatic run.',
    min: 1,
    max: 1000,
  },
]

const databaseFields: NumberFieldConfig[] = [
  { name: 'log_retention_setting.consume_retention_days', label: 'Usage logs', min: 0, max: 3650 },
  { name: 'log_retention_setting.error_retention_days', label: 'Error logs', min: 0, max: 3650 },
  { name: 'log_retention_setting.system_retention_days', label: 'System logs', min: 0, max: 3650 },
  { name: 'log_retention_setting.manage_retention_days', label: 'Manage logs', min: 0, max: 3650 },
  { name: 'log_retention_setting.topup_retention_days', label: 'Top-up logs', min: 0, max: 3650 },
  { name: 'log_retention_setting.refund_retention_days', label: 'Refund logs', min: 0, max: 3650 },
  { name: 'log_retention_setting.unknown_retention_days', label: 'Unknown logs', min: 0, max: 3650 },
]

const payloadFields: NumberFieldConfig[] = [
  {
    name: 'log_retention_setting.request_payload_retention_days',
    label: 'Request payloads',
    min: 0,
    max: 3650,
  },
  {
    name: 'log_retention_setting.response_payload_retention_days',
    label: 'Response payloads',
    min: 0,
    max: 3650,
  },
]

const serverFields: NumberFieldConfig[] = [
  {
    name: 'log_retention_setting.server_log_keep_files',
    label: 'Keep recent files',
    description: '0 disables file-count cleanup.',
    min: 0,
    max: 10000,
  },
  {
    name: 'log_retention_setting.server_log_keep_days',
    label: 'Keep recent days',
    description: '0 disables age cleanup.',
    min: 0,
    max: 3650,
  },
  {
    name: 'log_retention_setting.server_log_max_total_size_mb',
    label: 'Max total size',
    description: '0 disables size cleanup.',
    min: 0,
    max: 1048576,
  },
]

const logPolicyResultLabels: Record<string, string> = {
  consume: 'Usage logs',
  error: 'Error logs',
  manage: 'Manage logs',
  refund: 'Refund logs',
  system: 'System logs',
  topup: 'Top-up logs',
  unknown: 'Unknown logs',
}

function NumberField(props: {
  form: UseFormReturn<LogRetentionFormState>
  field: NumberFieldConfig
}) {
  const { t } = useTranslation()
  return (
    <FormField
      control={props.form.control}
      name={props.field.name as Path<LogRetentionFormState>}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t(props.field.label)}</FormLabel>
          <FormControl>
            <Input
              type='number'
              min={props.field.min}
              max={props.field.max}
              value={String(field.value)}
              onChange={(event) => field.onChange(Number(event.target.value))}
            />
          </FormControl>
          <FormDescription>
            {props.field.description ? t(props.field.description) : t('days')}
          </FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

const sumMatches = (
  items: LogRetentionPolicyResult[] | PayloadRetentionResult[]
) => items.reduce((total, item) => total + item.matched, 0)

function ResultSummary(props: { result: LogRetentionExecutionResult | null }) {
  const { t } = useTranslation()
  if (!props.result) return null
  const databaseMatches = sumMatches(props.result.database_logs ?? [])
  const payloadMatches = sumMatches(props.result.payloads ?? [])
  const serverFiles = props.result.server_log_files?.matched_files ?? 0
  const serverFailureCount =
    props.result.server_log_files?.failed_files?.length ?? 0
  const databaseDeleted = (props.result.database_logs ?? []).reduce(
    (total, item) => total + item.deleted,
    0
  )
  const payloadUpdated = (props.result.payloads ?? []).reduce(
    (total, item) => total + item.updated,
    0
  )
  const serverDeleted = props.result.server_log_files?.deleted_files ?? 0

  return (
    <div className='space-y-4 rounded-lg border p-4 text-sm'>
      <div className='grid gap-3 md:grid-cols-3'>
        <ResultMetric
          label={t('Database log matches')}
          value={
            props.result.preview
              ? String(databaseMatches)
              : t('{{count}} deleted', { count: databaseDeleted })
          }
        />
        <ResultMetric
          label={t('Payload matches')}
          value={
            props.result.preview
              ? String(payloadMatches)
              : t('{{count}} updated', { count: payloadUpdated })
          }
        />
        <ResultMetric
          label={t('Server log files')}
          value={
            props.result.preview
              ? String(serverFiles)
              : t('{{count}} deleted, {{size}} freed', {
                  count: serverDeleted,
                  size: formatBytes(
                    props.result.server_log_files?.freed_bytes ?? 0
                  ),
                })
          }
        />
      </div>
      <div className='grid gap-4 lg:grid-cols-3'>
        <ResultList
          title={t('Database log details')}
          emptyText={t('No database log policy matched.')}
          items={(props.result.database_logs ?? []).map((item) => ({
            key: item.name,
            label: t(logPolicyResultLabels[item.name] ?? item.name),
            value: props.result?.preview
              ? t('{{count}} matched', { count: item.matched })
              : t('{{count}} matched, {{deleted}} deleted', {
                  count: item.matched,
                  deleted: item.deleted,
                }),
          }))}
        />
        <ResultList
          title={t('Payload audit details')}
          emptyText={t('No payload audit policy matched.')}
          items={(props.result.payloads ?? []).map((item) => ({
            key: item.field,
            label: item.field === 'request' ? t('Request') : t('Response'),
            value: props.result?.preview
              ? t('{{count}} matched', { count: item.matched })
              : t('{{count}} matched, {{updated}} updated', {
                  count: item.matched,
                  updated: item.updated,
                }),
          }))}
        />
        <ResultList
          title={t('Server log file details')}
          emptyText={t('Server log file cleanup is disabled or has no match.')}
          items={[
            {
              key: 'server-log-files',
              label: t('Files'),
              value: props.result.server_log_files?.enabled
                ? props.result.preview
                  ? t('{{count}} matched', { count: serverFiles })
                  : t('{{count}} deleted, {{size}} freed', {
                      count: serverDeleted,
                      size: formatBytes(
                        props.result.server_log_files?.freed_bytes ?? 0
                      ),
                    })
                : t('Disabled'),
            },
            ...(serverFailureCount > 0
              ? [
                  {
                    key: 'server-log-failures',
                    label: t('Failed files'),
                    value: t('{{count}} failed', {
                      count: serverFailureCount,
                    }),
                  },
                ]
              : []),
          ]}
        />
      </div>
    </div>
  )
}

function ResultMetric(props: { label: string; value: string }) {
  return (
    <div>
      <div className='text-muted-foreground text-xs'>{props.label}</div>
      <div className='font-medium'>{props.value}</div>
    </div>
  )
}

function ResultList(props: {
  title: string
  emptyText: string
  items: Array<{ key: string; label: string; value: string }>
}) {
  const hasItems = props.items.length > 0
  return (
    <div className='space-y-2'>
      <div className='text-muted-foreground text-xs font-medium'>
        {props.title}
      </div>
      {hasItems ? (
        <div className='space-y-1'>
          {props.items.map((item) => (
            <div
              key={item.key}
              className='flex items-start justify-between gap-3'
            >
              <span className='text-muted-foreground min-w-0'>
                {item.label}
              </span>
              <span className='min-w-0 text-end font-medium break-words'>
                {item.value}
              </span>
            </div>
          ))}
        </div>
      ) : (
        <div className='text-muted-foreground'>{props.emptyText}</div>
      )}
    </div>
  )
}

export function LogRetentionSection({
  defaultValues,
}: LogRetentionSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const queryClient = useQueryClient()
  const form = useForm<LogRetentionFormState>({
    resolver: zodResolver(logRetentionSchema),
    defaultValues: toFormValues(defaultValues),
  })
  const [isRunning, setIsRunning] = useState(false)
  const [showRunConfirmDialog, setShowRunConfirmDialog] = useState(false)
  const [result, setResult] = useState<LogRetentionExecutionResult | null>(null)
  const baselineRef = useRef(defaultValues)
  const lastSerializedDefaults = useRef(serializeFlatValues(defaultValues))
  const isSaving = form.formState.isSubmitting || updateOption.isPending

  useEffect(() => {
    const serializedDefaults = serializeFlatValues(defaultValues)
    if (serializedDefaults === lastSerializedDefaults.current) {
      return
    }
    baselineRef.current = defaultValues
    lastSerializedDefaults.current = serializedDefaults
    form.reset(toFormValues(defaultValues))
  }, [defaultValues, form])

  const getChangedEntries = (
    values: LogRetentionFormState
  ): ChangedLogRetentionEntry[] =>
    (
      Object.entries(toFlatValues(values)) as ChangedLogRetentionEntry[]
    ).filter(([key, value]) => value !== baselineRef.current[key])

  const onSubmit = async (values: LogRetentionFormState) => {
    const changed = getChangedEntries(values)
    if (changed.length === 0) {
      toast.info(t('No changes to save'))
      return
    }
    try {
      for (const [key, value] of changed) {
        await updateOption.mutateAsync({ key, value, silent: true })
      }
    } catch (error) {
      const message =
        error instanceof Error ? error.message : t('Save failed, please retry')
      queryClient.invalidateQueries({ queryKey: ['system-options'] })
      setResult(null)
      toast.error(message)
      return
    }
    const flatValues = toFlatValues(values)
    baselineRef.current = flatValues
    lastSerializedDefaults.current = serializeFlatValues(flatValues)
    form.reset(values)
    setResult(null)
    toast.success(t('Saved successfully'))
  }

  const runRetention = async (preview: boolean) => {
    if (hasUnsavedLogRetentionChanges(form, baselineRef.current)) {
      toast.error(t('Save the retention policy before running cleanup.'))
      return
    }
    setIsRunning(true)
    try {
      const res = await executeLogRetention(preview)
      if (!res.success || !res.data) {
        throw new Error(res.message || t('Log retention execution failed'))
      }
      setResult(res.data)
      toast.success(
        preview
          ? t('Log retention preview completed')
          : t('Log retention cleanup completed')
      )
    } catch (error) {
      const message =
        error instanceof Error
          ? error.message
          : t('Log retention execution failed')
      setResult(null)
      toast.error(message)
    } finally {
      setIsRunning(false)
    }
  }

  const requestRunCleanup = () => {
    if (hasUnsavedLogRetentionChanges(form, baselineRef.current)) {
      toast.error(t('Save the retention policy before running cleanup.'))
      return
    }
    setShowRunConfirmDialog(true)
  }

  return (
    <SettingsSection
      title={t('Automatic Log Retention')}
      description={t(
        'Configure scheduled cleanup for database logs, payload audit fields, and server log files.'
      )}
    >
      <Form {...form}>
        <form onSubmit={form.handleSubmit(onSubmit)} className='space-y-6'>
          <FormField
            control={form.control}
            name='log_retention_setting.enabled'
            render={({ field }) => (
              <FormItem className='flex flex-row items-start justify-between gap-4 rounded-lg border p-4'>
                <div className='space-y-0.5 pe-4'>
                  <FormLabel>{t('Enable scheduler')}</FormLabel>
                  <FormDescription>
                    {t('Automatic cleanup runs only on the master node.')}
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <div className='grid gap-3 md:grid-cols-3'>
            {schedulerFields.map((field) => (
              <NumberField key={field.name} form={form} field={field} />
            ))}
          </div>

          <PolicyGroup
            title={t('Database log retention')}
            description={t('0 disables automatic deletion for that log type.')}
            columns='md:grid-cols-4'
          >
            {databaseFields.map((field) => (
              <NumberField key={field.name} form={form} field={field} />
            ))}
          </PolicyGroup>

          <PolicyGroup
            title={t('Payload audit retention')}
            description={t(
              'Payload cleanup removes stored request or response bodies while keeping log metadata.'
            )}
            columns='md:grid-cols-2'
          >
            {payloadFields.map((field) => (
              <NumberField key={field.name} form={form} field={field} />
            ))}
          </PolicyGroup>

          <FormField
            control={form.control}
            name='log_retention_setting.server_log_cleanup_enabled'
            render={({ field }) => (
              <FormItem className='flex flex-row items-start justify-between gap-4 rounded-lg border p-4'>
                <div className='space-y-0.5 pe-4'>
                  <FormLabel>{t('Server log file cleanup')}</FormLabel>
                  <FormDescription>
                    {t('The active oneapi log file is always skipped.')}
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <div className='grid gap-3 md:grid-cols-3'>
            {serverFields.map((field) => (
              <NumberField key={field.name} form={form} field={field} />
            ))}
          </div>

          <div className='flex flex-wrap gap-3'>
            <Button type='submit' disabled={isSaving}>
              {isSaving ? t('Saving...') : t('Save retention policy')}
            </Button>
            <Button
              type='button'
              variant='outline'
              onClick={() => runRetention(true)}
              disabled={isRunning || isSaving}
            >
              {isRunning ? t('Running...') : t('Preview cleanup')}
            </Button>
            <Button
              type='button'
              variant='destructive'
              onClick={requestRunCleanup}
              disabled={isRunning || isSaving}
            >
              {isRunning ? t('Running...') : t('Run cleanup now')}
            </Button>
          </div>

          <ResultSummary result={result} />
        </form>
      </Form>
      <LazyMount open={showRunConfirmDialog}>
        <AlertDialog
          open={showRunConfirmDialog}
          onOpenChange={setShowRunConfirmDialog}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>
                {t('Confirm log retention cleanup')}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {t(
                  'This will permanently apply the saved retention policy to matching database logs, payload audit fields, and server log files.'
                )}{' '}
                {t('This action cannot be undone.')}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel disabled={isRunning}>
                {t('Cancel')}
              </AlertDialogCancel>
              <AlertDialogAction
                onClick={() => runRetention(false)}
                disabled={isRunning}
                className='bg-destructive text-destructive-foreground hover:bg-destructive/90'
              >
                {isRunning ? t('Running...') : t('Run cleanup now')}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </LazyMount>
    </SettingsSection>
  )
}

function PolicyGroup(props: {
  title: string
  description: string
  columns: string
  children: ReactNode
}) {
  return (
    <div className='space-y-3'>
      <div>
        <h4 className='text-sm font-medium'>{props.title}</h4>
        <p className='text-muted-foreground text-xs'>{props.description}</p>
      </div>
      <div className={`grid gap-3 ${props.columns}`}>{props.children}</div>
    </div>
  )
}
