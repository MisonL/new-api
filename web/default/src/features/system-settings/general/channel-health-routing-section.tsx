import { useEffect, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { Activity, ExternalLink, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { StatusBadge } from '@/components/status-badge'
import { ChannelHealthRuntimeEntries } from '@/features/channel-health/components/channel-health-runtime-entries'
import { ChannelHealthRuntimeEvents } from '@/features/channel-health/components/channel-health-runtime-events'
import { useChannelHealthRuntime } from '@/features/channel-health/use-channel-health-runtime'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import {
  getChannelHealthRuntimeEvents,
  getChannelHealthRuntimePresentation,
  summarizeChannelHealthRuntime,
} from './channel-health-runtime'

type ChannelHealthRoutingSettings = {
  'general_setting.channel_health_enabled': boolean
  'general_setting.channel_health_cooldown_seconds': number
  'general_setting.channel_health_probe_lease_seconds': number
}

function parsePositiveInteger(value: number): number | null {
  const parsed = Number(value)
  if (!Number.isInteger(parsed) || parsed < 1) return null
  return parsed
}

export function ChannelHealthRoutingSection(props: {
  defaultValues: ChannelHealthRoutingSettings
}) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [enabled, setEnabled] = useState(
    props.defaultValues['general_setting.channel_health_enabled']
  )
  const [cooldownSeconds, setCooldownSeconds] = useState(
    props.defaultValues['general_setting.channel_health_cooldown_seconds']
  )
  const [probeLeaseSeconds, setProbeLeaseSeconds] = useState(
    props.defaultValues['general_setting.channel_health_probe_lease_seconds']
  )
  const [saving, setSaving] = useState(false)
  const {
    runtime,
    runtimeLoading,
    runtimeUnavailable,
    runtimeUpdatedAt,
    refresh,
  } = useChannelHealthRuntime({ enabled: true })

  useEffect(() => {
    queueMicrotask(() => {
      setEnabled(props.defaultValues['general_setting.channel_health_enabled'])
      setCooldownSeconds(
        props.defaultValues['general_setting.channel_health_cooldown_seconds']
      )
      setProbeLeaseSeconds(
        props.defaultValues[
          'general_setting.channel_health_probe_lease_seconds'
        ]
      )
    })
  }, [props.defaultValues])

  const handleSave = async () => {
    const nextCooldown = parsePositiveInteger(cooldownSeconds)
    const nextProbeLease = parsePositiveInteger(probeLeaseSeconds)
    if (nextCooldown === null || nextCooldown > 300) {
      toast.error(t('Cooldown must be between 1 and 300 seconds'))
      return
    }
    if (nextProbeLease === null || nextProbeLease > 60) {
      toast.error(t('Probe lease must be between 1 and 60 seconds'))
      return
    }

    const updates: Array<{ key: string; value: boolean | number }> = []
    const enabledChanged =
      enabled !== props.defaultValues['general_setting.channel_health_enabled']
    const enabledUpdate = {
      key: 'general_setting.channel_health_enabled',
      value: enabled,
    }
    if (enabledChanged && !enabled) updates.push(enabledUpdate)
    if (
      nextCooldown !==
      props.defaultValues['general_setting.channel_health_cooldown_seconds']
    ) {
      updates.push({
        key: 'general_setting.channel_health_cooldown_seconds',
        value: nextCooldown,
      })
    }
    if (
      nextProbeLease !==
      props.defaultValues['general_setting.channel_health_probe_lease_seconds']
    ) {
      updates.push({
        key: 'general_setting.channel_health_probe_lease_seconds',
        value: nextProbeLease,
      })
    }
    if (enabledChanged && enabled) updates.push(enabledUpdate)
    if (updates.length === 0) {
      toast.info(t('No changes'))
      return
    }

    setSaving(true)
    try {
      for (const update of updates) {
        await updateOption.mutateAsync({ ...update, silent: true })
      }
      toast.success(t('Saved successfully'))
      await refresh()
    } catch {
      toast.error(t('Failed to save'))
    } finally {
      setSaving(false)
    }
  }

  const summary = runtime ? summarizeChannelHealthRuntime(runtime) : null
  const recentEvents = getChannelHealthRuntimeEvents(runtime)
  const runtimePresentation = getChannelHealthRuntimePresentation(
    runtime,
    runtimeUnavailable
  )
  const handleRuntimeRefresh = async () => {
    try {
      await refresh()
    } catch {
      toast.error(t('Failed to load channel health runtime'))
    }
  }

  return (
    <SettingsSection
      title={t('Channel Health Routing')}
      description={t(
        'Temporarily cools only transient upstream transport and gateway failures before retry selection.'
      )}
    >
      <Alert>
        <AlertDescription className='text-xs leading-5'>
          {t(
            'Request parameter errors and 413 are not treated as channel-health failures. A 429 response starts a temporary cooldown using Retry-After when present, or the configured fallback. When Redis is unavailable, routing continues without health filtering and the condition is recorded in administrator logs.'
          )}
        </AlertDescription>
      </Alert>

      <div className='grid gap-4 md:grid-cols-3'>
        <div className='flex min-h-10 items-center gap-2'>
          <Switch checked={enabled} onCheckedChange={setEnabled} />
          <Label>{t('Enable channel health routing')}</Label>
        </div>
        <div className='grid gap-1.5'>
          <Label htmlFor='channel-health-cooldown'>
            {t('Cooldown window (seconds)')}
          </Label>
          <Input
            id='channel-health-cooldown'
            type='number'
            min={1}
            max={300}
            value={cooldownSeconds}
            disabled={!enabled}
            onChange={(event) => setCooldownSeconds(Number(event.target.value))}
          />
          <p className='text-muted-foreground text-xs'>
            {t(
              'After a qualifying gateway failure, this channel is skipped for the same group and model.'
            )}
          </p>
        </div>
        <div className='grid gap-1.5'>
          <Label htmlFor='channel-health-probe-lease'>
            {t('Half-open probe lease (seconds)')}
          </Label>
          <Input
            id='channel-health-probe-lease'
            type='number'
            min={1}
            max={60}
            value={probeLeaseSeconds}
            disabled={!enabled}
            onChange={(event) =>
              setProbeLeaseSeconds(Number(event.target.value))
            }
          />
          <p className='text-muted-foreground text-xs'>
            {t(
              'After cooldown, one request may probe recovery while competing requests continue to bypass it.'
            )}
          </p>
        </div>
      </div>

      <div className='flex flex-wrap items-center gap-2'>
        <Button size='sm' onClick={handleSave} disabled={saving}>
          {saving ? t('Saving...') : t('Save')}
        </Button>
        <Button
          variant='outline'
          size='sm'
          onClick={() => void handleRuntimeRefresh()}
          disabled={runtimeLoading}
        >
          <RefreshCw
            className={`mr-1 size-3.5 ${runtimeLoading ? 'animate-spin' : ''}`}
          />
          {t('Refresh runtime state')}
        </Button>
        <Button variant='outline' size='sm' asChild>
          <Link to='/usage-logs/$section' params={{ section: 'common' }}>
            <ExternalLink className='mr-1 size-3.5' />
            {t('View request routing logs')}
          </Link>
        </Button>
      </div>

      <Separator />

      <div className='space-y-3'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <div className='flex items-center gap-2'>
            <Activity
              className='text-muted-foreground size-4'
              aria-hidden='true'
            />
            <h4 className='text-sm font-semibold'>
              {t('Current runtime state')}
            </h4>
          </div>
          <StatusBadge
            label={t(runtimePresentation.labelKey)}
            variant={runtimePresentation.variant}
            copyable={false}
          />
        </div>

        {runtimeUnavailable && (
          <p className='text-destructive text-sm'>
            {t(
              'The current health-routing state cannot be confirmed. No stale runtime data is shown.'
            )}
          </p>
        )}

        {runtime && summary && (
          <>
            <div className='grid divide-y overflow-hidden rounded-md border sm:grid-cols-3 sm:divide-x sm:divide-y-0'>
              <div className='p-3'>
                <p className='text-xs text-amber-700 dark:text-amber-400'>
                  {t('Cooling channels')}
                </p>
                <p className='mt-1 font-mono text-lg text-amber-700 tabular-nums dark:text-amber-400'>
                  {summary.cooling}
                </p>
              </div>
              <div className='p-3'>
                <p className='text-xs text-sky-700 dark:text-sky-400'>
                  {t('Active probes')}
                </p>
                <p className='mt-1 font-mono text-lg text-sky-700 tabular-nums dark:text-sky-400'>
                  {summary.probing}
                </p>
              </div>
              <div className='p-3'>
                <p className='text-xs text-teal-700 dark:text-teal-400'>
                  {t('Awaiting probe')}
                </p>
                <p className='mt-1 font-mono text-lg text-teal-700 tabular-nums dark:text-teal-400'>
                  {summary.awaitingProbe}
                </p>
              </div>
            </div>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Effective runtime policy: {{cooldown}}s cooldown, {{lease}}s half-open probe lease.',
                {
                  cooldown: runtime.cooldown_seconds,
                  lease: runtime.probe_lease_seconds,
                }
              )}
              {runtimeUpdatedAt
                ? ` ${t('Last refreshed: {{time}}', {
                    time: runtimeUpdatedAt.toLocaleTimeString(),
                  })}`
                : ''}
            </p>
          </>
        )}

        {runtime?.enabled &&
          runtime.redis_available &&
          runtime.entries.length === 0 && (
            <p className='text-muted-foreground text-sm'>
              {t('No channels are currently cooling down or being probed.')}
            </p>
          )}

        {runtime && runtime.entries.length > 0 && (
          <ChannelHealthRuntimeEntries entries={runtime.entries} />
        )}
        <ChannelHealthRuntimeEvents events={recentEvents} />
      </div>
    </SettingsSection>
  )
}
