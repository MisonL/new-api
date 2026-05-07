import { useCallback, useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Form } from '@/components/ui/form'
import { Separator } from '@/components/ui/separator'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'
import { DiskCacheFields } from './disk-cache-fields'
import { ModelPerformanceMetricsFields } from './model-performance-metrics-fields'
import { PerformanceStatsDashboard } from './performance-stats-dashboard'
import {
  formatBytes,
  perfSchema,
  type LogInfo,
  type PerformanceStats,
  type PerfFormValues,
} from './performance-types'
import { ServerLogManagement } from './server-log-management'
import { SystemPerformanceFields } from './system-performance-fields'

interface Props {
  defaultValues: PerfFormValues
}

export function PerformanceSection(props: Props) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [stats, setStats] = useState<PerformanceStats | null>(null)
  const [logInfo, setLogInfo] = useState<LogInfo | null>(null)
  const [logCleanupMode, setLogCleanupMode] = useState('by_count')
  const [logCleanupValue, setLogCleanupValue] = useState(10)
  const [logCleanupLoading, setLogCleanupLoading] = useState(false)

  const form = useForm<PerfFormValues>({
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    resolver: zodResolver(perfSchema) as any,
    defaultValues: props.defaultValues,
  })

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  useResetForm(form as any, props.defaultValues)

  const fetchStats = useCallback(async () => {
    try {
      const res = await api.get('/api/performance/stats')
      if (res.data.success) setStats(res.data.data)
    } catch {
      /* ignore */
    }
  }, [])

  const fetchLogInfo = useCallback(async () => {
    try {
      const res = await api.get('/api/performance/logs')
      if (res.data.success) setLogInfo(res.data.data)
    } catch {
      /* ignore */
    }
  }, [])

  useEffect(() => {
    fetchStats()
    fetchLogInfo()
  }, [fetchStats, fetchLogInfo])

  const onSubmit = async (data: PerfFormValues) => {
    const entries = Object.entries(data) as [string, unknown][]
    const updates = entries.filter(
      ([key, value]) =>
        value !== (props.defaultValues[key as keyof PerfFormValues] as unknown)
    )
    if (updates.length === 0) {
      toast.info(t('No changes to save'))
      return
    }
    for (const [key, value] of updates) {
      await updateOption.mutateAsync({
        key,
        value: value as string | number | boolean,
      })
    }
    toast.success(t('Saved successfully'))
    fetchStats()
  }

  const clearDiskCache = async () => {
    try {
      const res = await api.delete('/api/performance/disk_cache')
      if (res.data.success) {
        toast.success(t('Disk cache cleared'))
        fetchStats()
      }
    } catch {
      toast.error(t('Cleanup failed'))
    }
  }

  const resetStats = async () => {
    try {
      const res = await api.post('/api/performance/reset_stats')
      if (res.data.success) {
        toast.success(t('Statistics reset'))
        fetchStats()
      }
    } catch {
      toast.error(t('Reset failed'))
    }
  }

  const forceGC = async () => {
    try {
      const res = await api.post('/api/performance/gc')
      if (res.data.success) {
        toast.success(t('GC executed'))
        fetchStats()
      }
    } catch {
      toast.error(t('GC execution failed'))
    }
  }

  const cleanupLogFiles = async () => {
    if (!logCleanupValue || isNaN(logCleanupValue) || logCleanupValue < 1) {
      toast.error(t('Please enter a valid number'))
      return
    }
    setLogCleanupLoading(true)
    try {
      const res = await api.delete(
        `/api/performance/logs?mode=${logCleanupMode}&value=${logCleanupValue}`
      )
      if (res.data.success) {
        const { deleted_count, freed_bytes } = res.data.data
        toast.success(
          t('Cleaned up {{count}} log files, freed {{size}}', {
            count: deleted_count,
            size: formatBytes(freed_bytes),
          })
        )
      } else {
        toast.error(res.data.message || t('Cleanup failed'))
      }
      fetchLogInfo()
    } catch {
      toast.error(t('Cleanup failed'))
    } finally {
      setLogCleanupLoading(false)
    }
  }

  const diskEnabled = form.watch('performance_setting.disk_cache_enabled')
  const monitorEnabled = form.watch('performance_setting.monitor_enabled')
  const perfMetricsEnabled = form.watch('perf_metrics_setting.enabled')
  const maxCacheSizeMb = form.watch(
    'performance_setting.disk_cache_max_size_mb'
  )
  const lowDiskSpace =
    diskEnabled &&
    stats?.disk_space_info &&
    stats.disk_space_info.free > 0 &&
    maxCacheSizeMb > 0 &&
    stats.disk_space_info.free < maxCacheSizeMb * 1024 * 1024
  const diskCachePercent =
    stats?.cache_stats?.disk_cache_max_bytes &&
    stats.cache_stats.disk_cache_max_bytes > 0
      ? Math.round(
          (stats.cache_stats.current_disk_usage_bytes /
            stats.cache_stats.disk_cache_max_bytes) *
            100
        )
      : 0

  return (
    <SettingsSection
      title={t('Performance Settings')}
      description={t(
        'Disk cache, system performance monitoring, and operation statistics'
      )}
    >
      <Form {...form}>
        <form onSubmit={form.handleSubmit(onSubmit)} className='space-y-6'>
          <DiskCacheFields
            form={form}
            stats={stats}
            enabled={diskEnabled}
            lowDiskSpace={lowDiskSpace}
            maxCacheSizeMb={maxCacheSizeMb}
          />
          <Separator />
          <SystemPerformanceFields form={form} enabled={monitorEnabled} />
          <Separator />
          <ModelPerformanceMetricsFields
            form={form}
            enabled={perfMetricsEnabled}
          />
          <Button type='submit' disabled={updateOption.isPending}>
            {updateOption.isPending ? t('Saving...') : t('Save Changes')}
          </Button>
        </form>
      </Form>

      <Separator />

      <ServerLogManagement
        t={t}
        logInfo={logInfo}
        cleanupMode={logCleanupMode}
        cleanupValue={logCleanupValue}
        cleanupLoading={logCleanupLoading}
        onModeChange={setLogCleanupMode}
        onValueChange={setLogCleanupValue}
        onCleanup={cleanupLogFiles}
      />

      <Separator />

      <PerformanceStatsDashboard
        stats={stats}
        diskCachePercent={diskCachePercent}
        onRefresh={fetchStats}
        onClearDiskCache={clearDiskCache}
        onResetStats={resetStats}
        onForceGC={forceGC}
      />
    </SettingsSection>
  )
}
