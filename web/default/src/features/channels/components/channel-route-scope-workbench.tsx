import { useCallback, useEffect, useMemo, useState } from 'react'
import { RefreshCw, Save } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  getChannelRouteScopes,
  getChannelRouteScopeMetrics,
  updateChannelRouteScopePriority,
  updateChannelRouteScopeWeights,
} from '../api'
import {
  adjustRouteWeights,
  formatRouteWeightPercent,
  parseRouteWeightPercent,
  ROUTE_WEIGHT_TOTAL_BPS,
} from '../lib/channel-route-weight'
import type { ChannelRouteScope, ChannelRouteScopeMetrics } from '../types'
import { useChannels } from './channels-provider'

export function ChannelRouteScopeWorkbench() {
  const { t } = useTranslation()
  const { channelRoutingConfig } = useChannels()
  const [scopes, setScopes] = useState<ChannelRouteScope[]>([])
  const [selectedID, setSelectedID] = useState('')
  // The editor stores percentage strings; conversion to bps happens only at
  // the API boundary so the backend remains the single source of truth.
  const [weightInputs, setWeightInputs] = useState<Record<number, string>>({})
  const [priority, setPriority] = useState('')
  const [locked, setLocked] = useState<Record<number, boolean>>({})
  const [redistributionMode, setRedistributionMode] = useState<
    'equal_adjust' | 'proportional_adjust'
  >('equal_adjust')
  const [groupFilter, setGroupFilter] = useState('')
  const [modelFilter, setModelFilter] = useState('')
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [metrics, setMetrics] = useState<ChannelRouteScopeMetrics | null>(null)
  const routingConfigLoaded = channelRoutingConfig !== undefined
  const v2RoutingEnabled = channelRoutingConfig?.v2_enabled === true

  const selectedScope = useMemo(
    () => scopes.find((scope) => scope.id === selectedID) ?? scopes[0],
    [scopes, selectedID]
  )

  const loadScopes = useCallback(async () => {
    setLoading(true)
    try {
      const response = await getChannelRouteScopes({
        group: groupFilter.trim() || undefined,
        model: modelFilter.trim() || undefined,
      })
      if (!response.success) throw new Error(response.message)
      const nextScopes = response.data ?? []
      setScopes(nextScopes)
      setSelectedID((current) =>
        nextScopes.some((scope) => scope.id === current)
          ? current
          : (nextScopes[0]?.id ?? '')
      )
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to load routing scopes')
      )
    } finally {
      setLoading(false)
    }
  }, [groupFilter, modelFilter, t])

  useEffect(() => {
    void loadScopes()
  }, [loadScopes])

  useEffect(() => {
    if (!selectedScope) {
      setWeightInputs({})
      setPriority('')
      return
    }
    setWeightInputs(
      Object.fromEntries(
        selectedScope.items.map((item) => [
          item.channel_id,
          formatRouteWeightPercent(item.route_weight_bps),
        ])
      )
    )
    setPriority(String(selectedScope.route_priority))
    setLocked({})
    setMetrics(null)
    void getChannelRouteScopeMetrics(selectedScope.id)
      .then((response) => {
        if (response.success) setMetrics(response.data ?? null)
      })
      .catch(() => {
        // Capacity is observability-only; a transient metrics failure must not
        // prevent route scope edits or hide the last loaded configuration.
      })
  }, [selectedScope])

  const metricsByChannel = useMemo(
    () =>
      new Map((metrics?.items ?? []).map((item) => [item.channel_id, item])),
    [metrics]
  )

  const changeWeight = (channelID: number, rawValue: string) => {
    const target = parseRouteWeightPercent(rawValue)
    if (target === null) {
      setWeightInputs((current) => ({ ...current, [channelID]: rawValue }))
      return
    }
    const currentValues =
      selectedScope?.items.map(
        (item) =>
          parseRouteWeightPercent(weightInputs[item.channel_id] ?? '') ?? 0
      ) ?? []
    const targetIndex =
      selectedScope?.items.findIndex((item) => item.channel_id === channelID) ??
      -1
    if (targetIndex < 0) return
    if (locked[channelID]) {
      toast.error(t('Unlock this channel before adjusting its weight'))
      return
    }
    const next = adjustRouteWeights(
      currentValues,
      targetIndex,
      target,
      redistributionMode,
      (selectedScope?.items ?? []).map((item) =>
        Boolean(locked[item.channel_id])
      )
    )
    if (!next) {
      toast.error(t('Unlock another channel before adjusting this weight'))
      return
    }
    setWeightInputs(
      Object.fromEntries(
        (selectedScope?.items ?? []).map((item, index) => [
          item.channel_id,
          formatRouteWeightPercent(next[index]),
        ])
      )
    )
  }

  const weightTotal = selectedScope
    ? selectedScope.items.reduce((total, item) => {
        const bps = parseRouteWeightPercent(weightInputs[item.channel_id] ?? '')
        return bps === null ? Number.NaN : total + bps
      }, 0)
    : 0
  const allWeightsZero =
    selectedScope !== undefined &&
    selectedScope.items.length > 0 &&
    selectedScope.items.every(
      (item) =>
        parseRouteWeightPercent(weightInputs[item.channel_id] ?? '') === 0
    )

  const saveWeights = async () => {
    if (!selectedScope) return
    if (!v2RoutingEnabled) {
      toast.error(t('V2 routing is disabled; scope edits are read-only'))
      return
    }
    if (
      selectedScope.items.some(
        (item) =>
          parseRouteWeightPercent(weightInputs[item.channel_id] ?? '') === null
      ) ||
      weightTotal !== ROUTE_WEIGHT_TOTAL_BPS
    ) {
      toast.error(t('Route weights must sum to 100%'))
      return
    }
    setSaving(true)
    try {
      const response = await updateChannelRouteScopeWeights(selectedScope.id, {
        version: selectedScope.version,
        mode: redistributionMode,
        items: selectedScope.items.map((item) => ({
          channel_id: item.channel_id,
          route_weight_bps: parseRouteWeightPercent(
            weightInputs[item.channel_id] ?? ''
          ) as number,
        })),
      })
      if (!response.success) throw new Error(response.message)
      toast.success(t('Routing scope weights saved'))
      await loadScopes()
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to save routing scope')
      )
      await loadScopes()
    } finally {
      setSaving(false)
    }
  }

  const savePriority = async () => {
    if (
      !selectedScope ||
      !Number.isInteger(Number(priority)) ||
      Number(priority) < 0
    ) {
      toast.error(t('Route priority must be a non-negative integer'))
      return
    }
    if (!v2RoutingEnabled) {
      toast.error(t('V2 routing is disabled; scope edits are read-only'))
      return
    }
    setSaving(true)
    try {
      const response = await updateChannelRouteScopePriority(selectedScope.id, {
        version: selectedScope.version,
        route_priority: Number(priority),
      })
      if (!response.success) throw new Error(response.message)
      toast.success(t('Routing scope priority saved'))
      await loadScopes()
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to save routing scope')
      )
      await loadScopes()
    } finally {
      setSaving(false)
    }
  }

  const resetDraft = () => {
    if (!selectedScope) return
    setWeightInputs(
      Object.fromEntries(
        selectedScope.items.map((item) => [
          item.channel_id,
          formatRouteWeightPercent(item.route_weight_bps),
        ])
      )
    )
    setPriority(String(selectedScope.route_priority))
    setLocked({})
  }

  return (
    <section className='space-y-3 border-y py-3 sm:py-4'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <div>
          <h2 className='text-sm font-semibold'>{t('Routing scopes')}</h2>
          <p className='text-muted-foreground text-xs leading-5'>
            {t('Weights are stored per group, effective model, and priority.')}
          </p>
        </div>
        <Button
          variant='outline'
          size='sm'
          onClick={() => void loadScopes()}
          disabled={loading || saving}
        >
          <RefreshCw
            className={`mr-1 size-3.5 ${loading ? 'animate-spin' : ''}`}
          />
          {t('Refresh scopes')}
        </Button>
      </div>

      {!routingConfigLoaded ? (
        <p className='text-muted-foreground text-sm'>
          {t('Loading routing configuration')}
        </p>
      ) : !v2RoutingEnabled ? (
        <p className='rounded-md border border-amber-500/50 bg-amber-50 px-3 py-2 text-sm text-amber-900 dark:bg-amber-950/30 dark:text-amber-200'>
          {channelRoutingConfig?.shadow_enabled
            ? t(
                'Routing shadow mode is active; legacy selector is serving traffic'
              )
            : t('V2 routing is disabled; scope edits are read-only')}
        </p>
      ) : null}

      <div className='flex flex-wrap items-end gap-2'>
        <label className='space-y-1 text-sm'>
          <span className='text-muted-foreground'>{t('Group filter')}</span>
          <Input
            className='w-40'
            value={groupFilter}
            onChange={(event) => setGroupFilter(event.target.value)}
            placeholder={t('All groups')}
          />
        </label>
        <label className='space-y-1 text-sm'>
          <span className='text-muted-foreground'>{t('Model filter')}</span>
          <Input
            className='w-48'
            value={modelFilter}
            onChange={(event) => setModelFilter(event.target.value)}
            placeholder={t('All models')}
          />
        </label>
        <Button
          variant='outline'
          size='sm'
          onClick={() => void loadScopes()}
          disabled={loading || saving}
        >
          {t('Apply filters')}
        </Button>
        <Button
          variant='ghost'
          size='sm'
          onClick={() => {
            setGroupFilter('')
            setModelFilter('')
            void getChannelRouteScopes().then((response) => {
              if (response.success) {
                setScopes(response.data ?? [])
                setSelectedID((current) =>
                  (response.data ?? []).some((scope) => scope.id === current)
                    ? current
                    : (response.data?.[0]?.id ?? '')
                )
              }
            })
          }}
          disabled={loading || saving}
        >
          {t('Reset filters')}
        </Button>
      </div>

      {scopes.length === 0 ? (
        <p className='text-muted-foreground text-sm'>
          {loading ? t('Loading routing scopes') : t('No routing scopes found')}
        </p>
      ) : (
        <div className='grid gap-4 lg:grid-cols-[minmax(220px,0.35fr)_minmax(0,1fr)]'>
          <div className='space-y-1 rounded-md border p-2'>
            {scopes.map((scope) => (
              <button
                key={scope.id}
                type='button'
                className={`w-full rounded px-3 py-2 text-left text-sm ${
                  scope.id === selectedScope?.id
                    ? 'bg-accent text-accent-foreground'
                    : 'hover:bg-muted'
                }`}
                onClick={() => setSelectedID(scope.id)}
              >
                <div className='font-medium'>{scope.effective_route_model}</div>
                <div className='text-muted-foreground mt-1 text-xs'>
                  {scope.group} · {t('Priority')} {scope.route_priority}
                </div>
              </button>
            ))}
          </div>

          {selectedScope ? (
            <div className='space-y-3 rounded-md border p-3'>
              <div className='flex flex-wrap items-center gap-2 text-sm'>
                <Badge variant='outline'>{selectedScope.group}</Badge>
                <Badge variant='secondary'>
                  {selectedScope.effective_route_model}
                </Badge>
                <span className='text-muted-foreground'>
                  {t('Version')} {selectedScope.version.slice(0, 12)}
                </span>
              </div>
              <div className='flex flex-wrap items-end gap-2'>
                <label className='space-y-1 text-sm'>
                  <span className='text-muted-foreground'>{t('Priority')}</span>
                  <Input
                    className='w-28'
                    type='number'
                    min={0}
                    value={priority}
                    onChange={(event) => setPriority(event.target.value)}
                    disabled={saving || !v2RoutingEnabled}
                  />
                </label>
                <Button
                  variant='outline'
                  size='sm'
                  onClick={() => void savePriority()}
                  disabled={saving || !v2RoutingEnabled}
                >
                  <Save className='mr-1 size-3.5' />
                  {t('Save priority')}
                </Button>
                <label className='space-y-1 text-sm'>
                  <span className='text-muted-foreground'>
                    {t('Redistribution mode')}
                  </span>
                  <Select
                    value={redistributionMode}
                    disabled={
                      saving ||
                      !v2RoutingEnabled ||
                      selectedScope.items.length <= 1
                    }
                    onValueChange={(value) =>
                      setRedistributionMode(
                        value as 'equal_adjust' | 'proportional_adjust'
                      )
                    }
                  >
                    <SelectTrigger className='w-44'>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value='equal_adjust'>
                        {t('Equal adjustment')}
                      </SelectItem>
                      <SelectItem value='proportional_adjust'>
                        {t('Proportional adjustment')}
                      </SelectItem>
                    </SelectContent>
                  </Select>
                </label>
                <span
                  className={
                    weightTotal === ROUTE_WEIGHT_TOTAL_BPS
                      ? 'text-muted-foreground text-sm'
                      : 'text-destructive text-sm'
                  }
                >
                  {t('Total')}:{' '}
                  {Number.isFinite(weightTotal)
                    ? (weightTotal / 100).toFixed(2)
                    : '-'}
                  % / 100.00%
                </span>
              </div>
              <div className='overflow-x-auto'>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('Channel')}</TableHead>
                      <TableHead>{t('Weight (%)')}</TableHead>
                      <TableHead>{t('Max concurrency')}</TableHead>
                      <TableHead>{t('Capacity')}</TableHead>
                      <TableHead>{t('Locked')}</TableHead>
                      <TableHead>{t('Status')}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {selectedScope.items.map((item) => {
                      const isLocked = Boolean(locked[item.channel_id])
                      return (
                        <TableRow
                          key={item.channel_id}
                          className={isLocked ? 'bg-muted/30' : undefined}
                        >
                          <TableCell>
                            <div className='flex flex-wrap items-center gap-2 font-medium'>
                              <span>
                                #{item.channel_id} {item.channel_name}
                              </span>
                              {isLocked ? (
                                <Badge variant='outline'>{t('Locked')}</Badge>
                              ) : null}
                            </div>
                            <div className='text-muted-foreground text-xs'>
                              {item.ability_enabled
                                ? t('Ability enabled')
                                : t('Ability disabled')}
                            </div>
                          </TableCell>
                          <TableCell>
                            <Input
                              className='w-32'
                              type='number'
                              min={0}
                              max={100}
                              step='0.01'
                              value={weightInputs[item.channel_id] ?? ''}
                              onChange={(event) =>
                                changeWeight(
                                  item.channel_id,
                                  event.target.value
                                )
                              }
                              disabled={saving || !v2RoutingEnabled || isLocked}
                              aria-label={t('Route weight for {{channel}}', {
                                channel: item.channel_name,
                              })}
                            />
                          </TableCell>
                          <TableCell>
                            {item.max_concurrency || t('Unlimited')}
                          </TableCell>
                          <TableCell>
                            {(() => {
                              const snapshot = metricsByChannel.get(
                                item.channel_id
                              )
                              if (!snapshot || item.max_concurrency <= 0) {
                                return (
                                  <span className='text-muted-foreground'>
                                    -
                                  </span>
                                )
                              }
                              const source = snapshot.confirmed
                                ? snapshot.source
                                : t('Unconfirmed')
                              return `${snapshot.current_in_flight}/${item.max_concurrency} (${source})`
                            })()}
                          </TableCell>
                          <TableCell>
                            <Switch
                              checked={isLocked}
                              onCheckedChange={(checked) =>
                                setLocked((current) => ({
                                  ...current,
                                  [item.channel_id]: checked,
                                }))
                              }
                              disabled={saving || !v2RoutingEnabled}
                              aria-label={t('Lock route weight')}
                            />
                          </TableCell>
                          <TableCell>
                            {item.channel_status === 1
                              ? t('Enabled')
                              : t('Disabled')}
                          </TableCell>
                        </TableRow>
                      )
                    })}
                  </TableBody>
                </Table>
              </div>
              {allWeightsZero ? (
                <p className='rounded-md border border-amber-500/50 bg-amber-50 px-3 py-2 text-sm text-amber-900 dark:bg-amber-950/30 dark:text-amber-200'>
                  {t(
                    'All route weights are 0%; this scope will not receive traffic'
                  )}
                </p>
              ) : null}
              <div className='flex flex-wrap gap-2'>
                <Button
                  variant='outline'
                  onClick={resetDraft}
                  disabled={saving}
                >
                  {t('Reset draft')}
                </Button>
                <Button
                  onClick={() => void saveWeights()}
                  disabled={saving || !v2RoutingEnabled}
                >
                  <Save className='mr-1 size-3.5' />
                  {t('Save weights')}
                </Button>
              </div>
            </div>
          ) : null}
        </div>
      )}
    </section>
  )
}
