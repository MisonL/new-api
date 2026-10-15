import { useEffect, useMemo } from 'react'
import * as z from 'zod'
import { useWatch, type Resolver } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
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
import { FormDirtyIndicator } from '../components/form-dirty-indicator'
import { FormNavigationGuard } from '../components/form-navigation-guard'
import { SettingsSection } from '../components/settings-section'
import { useSettingsForm } from '../hooks/use-settings-form'
import { useUpdateOption } from '../hooks/use-update-option'

const retryableStatusCodes = z.string().refine((value) => {
  try {
    const parsed = JSON.parse(value)
    return (
      Array.isArray(parsed) &&
      parsed.length > 0 &&
      parsed.every(
        (code) => Number.isInteger(code) && code >= 100 && code <= 599
      )
    )
  } catch {
    return false
  }
}, 'Enter a JSON array of HTTP status codes.')

// The schema must mirror the nested shape react-hook-form resolves these field
// paths to. Declaring the paths as literal dotted keys made zodResolver return
// only its own defaults, so every edit was dropped on submit.
const responsesBootstrapSchema = z.object({
  general_setting: z.object({
    responses_stream_bootstrap_recovery_enabled: z.boolean(),
    responses_stream_bootstrap_grace_period_seconds: z
      .number()
      .int()
      .min(1)
      .max(3600),
    responses_stream_bootstrap_probe_interval_milliseconds: z
      .number()
      .int()
      .min(1)
      .max(60000),
    responses_stream_bootstrap_ping_interval_seconds: z
      .number()
      .int()
      .min(1)
      .max(300),
    responses_stream_bootstrap_retryable_status_codes: retryableStatusCodes,
  }),
})

type ResponsesBootstrapValues = z.infer<typeof responsesBootstrapSchema>

// The section registry passes the flat option keys (as stored in the database).
// The form itself works on the nested shape, so expand once here.
type ResponsesBootstrapFlatValues = {
  'general_setting.responses_stream_bootstrap_recovery_enabled': boolean
  'general_setting.responses_stream_bootstrap_grace_period_seconds': number
  'general_setting.responses_stream_bootstrap_probe_interval_milliseconds': number
  'general_setting.responses_stream_bootstrap_ping_interval_seconds': number
  'general_setting.responses_stream_bootstrap_retryable_status_codes': string
}

function toNestedValues(
  flat: ResponsesBootstrapFlatValues
): ResponsesBootstrapValues {
  return {
    general_setting: {
      responses_stream_bootstrap_recovery_enabled:
        flat['general_setting.responses_stream_bootstrap_recovery_enabled'],
      responses_stream_bootstrap_grace_period_seconds:
        flat['general_setting.responses_stream_bootstrap_grace_period_seconds'],
      responses_stream_bootstrap_probe_interval_milliseconds:
        flat[
          'general_setting.responses_stream_bootstrap_probe_interval_milliseconds'
        ],
      responses_stream_bootstrap_ping_interval_seconds:
        flat['general_setting.responses_stream_bootstrap_ping_interval_seconds'],
      responses_stream_bootstrap_retryable_status_codes:
        flat[
          'general_setting.responses_stream_bootstrap_retryable_status_codes'
        ],
    },
  }
}

export function ResponsesBootstrapSection(props: {
  defaultValues: ResponsesBootstrapFlatValues
}) {
  const nestedDefaults = useMemo(
    () => toNestedValues(props.defaultValues),
    [props.defaultValues]
  )

  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const { form, handleSubmit, isDirty, isSubmitting } =
    useSettingsForm<ResponsesBootstrapValues>({
      resolver: zodResolver(responsesBootstrapSchema) as Resolver<
        ResponsesBootstrapValues,
        unknown,
        ResponsesBootstrapValues
      >,
      defaultValues: nestedDefaults,
      onSubmit: async (_data, changedFields) => {
        for (const [key, value] of Object.entries(changedFields)) {
          await updateOption.mutateAsync({
            key,
            value: value as string | number | boolean,
          })
        }
      },
    })

  const enabled = form.watch(
    'general_setting.responses_stream_bootstrap_recovery_enabled'
  )
  const isSaving = updateOption.isPending || isSubmitting

  // Subscribing to both values is what makes the check re-run when the user
  // edits either one. Reading them once in an effect keyed on `enabled` left a
  // stale error with no way to clear it.
  const pingInterval = useWatch({
    control: form.control,
    name: 'general_setting.responses_stream_bootstrap_ping_interval_seconds',
  })
  const gracePeriod = useWatch({
    control: form.control,
    name: 'general_setting.responses_stream_bootstrap_grace_period_seconds',
  })

  useEffect(() => {
    const pingField =
      'general_setting.responses_stream_bootstrap_ping_interval_seconds'
    if (!enabled || typeof pingInterval !== 'number' || typeof gracePeriod !== 'number') {
      form.clearErrors(pingField)
      return
    }
    if (pingInterval >= gracePeriod) {
      form.setError(pingField, {
        message: t('Ping interval must be shorter than the grace period.'),
      })
      return
    }
    form.clearErrors(pingField)
  }, [enabled, form, t, pingInterval, gracePeriod])


  return (
    <SettingsSection
      title={t('Responses stream recovery')}
      description={t(
        'Configure recovery before the first event of a streaming /v1/responses request.'
      )}
    >
      <Alert>
        <AlertDescription className='text-xs leading-5'>
          {t(
            'Recovery only applies before the first real event. It keeps the client connection alive while retryable channel failures are probed; it does not resume a stream after content has already been sent.'
          )}
        </AlertDescription>
      </Alert>

      <FormNavigationGuard when={isDirty} />

      <Form {...form}>
        <form onSubmit={handleSubmit} className='space-y-6'>
          <FormDirtyIndicator isDirty={isDirty} />
          <FormField
            control={form.control}
            name='general_setting.responses_stream_bootstrap_recovery_enabled'
            render={({ field }) => (
              <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                <div className='space-y-0.5 pe-4'>
                  <FormLabel className='text-base'>
                    {t('Enable Responses stream bootstrap recovery')}
                  </FormLabel>
                  <FormDescription>
                    {t(
                      'Only channels that enable the matching channel-level option participate.'
                    )}
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={isSaving}
                  />
                </FormControl>
              </FormItem>
            )}
          />

          <div className='grid gap-6 sm:grid-cols-2'>
            <FormField
              control={form.control}
              name='general_setting.responses_stream_bootstrap_grace_period_seconds'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Bootstrap grace period (seconds)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      max={3600}
                      disabled={!enabled || isSaving}
                      value={field.value as number}
                      onChange={(event) =>
                        field.onChange(event.target.valueAsNumber)
                      }
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Maximum time spent probing before returning the real upstream error.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='general_setting.responses_stream_bootstrap_probe_interval_milliseconds'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Probe interval (milliseconds)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      max={60000}
                      disabled={!enabled || isSaving}
                      value={field.value as number}
                      onChange={(event) =>
                        field.onChange(event.target.valueAsNumber)
                      }
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Delay between availability probes during the recovery window.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='general_setting.responses_stream_bootstrap_ping_interval_seconds'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('SSE keep-alive interval (seconds)')}
                  </FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      max={300}
                      disabled={!enabled || isSaving}
                      value={field.value as number}
                      onChange={(event) =>
                        field.onChange(event.target.valueAsNumber)
                      }
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Sends an SSE comment while recovery is waiting for a usable channel.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='general_setting.responses_stream_bootstrap_retryable_status_codes'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Retryable status codes')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder='[401,403,408,429,500,502,503,504]'
                      disabled={!enabled || isSaving}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'JSON array of HTTP status codes that may trigger bootstrap recovery.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <Button type='submit' disabled={isSaving}>
            {isSaving ? t('Saving...') : t('Save Changes')}
          </Button>
        </form>
      </Form>
    </SettingsSection>
  )
}
