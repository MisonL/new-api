import { RefreshCcwIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { StatusBadge } from '@/components/status-badge'

export type NpmCLIVersionLastError = {
  code: string
  message: string
  source: string
  updated_at: string
}

export type NpmCLIVersionDiagnostic = {
  package: string
  source: string
  refreshed_at?: string
  cache_age_ms?: number
  latest_version?: string
  option_count: number
  recorded: boolean
  recommended_action?: string
  last_error?: NpmCLIVersionLastError
  last_error_scope?: string
  recent_errors?: NpmCLIVersionLastError[]
}

export type NpmCLIVersionDiagnosticsSummary = {
  package_count: number
  recorded_count: number
  missing_count: number
  last_error_count: number
  process_last_error_count: number
  recorded_last_error_count: number
  newest_refreshed_at?: string
  oldest_refreshed_at?: string
  max_cache_age_ms?: number
}

export type NpmCLIVersionRefreshMetrics = {
  scheduled_runs: number
  scheduled_successful_packages: number
  scheduled_failed_packages: number
  manual_runs: number
  manual_successes: number
  manual_failures: number
  last_run_at?: string
  last_run_duration_ms?: number
  last_run_refreshed: number
  last_run_failed: number
  last_manual_refresh_at?: string
  last_manual_duration_ms?: number
  last_manual_package?: string
  last_manual_code?: string
}

export type NpmCLIVersionDiagnosticsData = {
  generated_at?: string
  refresh_interval_ms?: number
  registry_timeout_ms?: number
  summary?: NpmCLIVersionDiagnosticsSummary
  metrics?: NpmCLIVersionRefreshMetrics
  packages?: NpmCLIVersionDiagnostic[]
}

export type NpmCLIVersionDiagnosticsApiResponse = {
  success: boolean
  message?: string
  data?: NpmCLIVersionDiagnosticsData
}

type Props = {
  diagnostics: NpmCLIVersionDiagnosticsData | null
  loading: boolean
  error: string
  onRefresh: () => void
}

export function NpmCLIVersionDiagnostics(props: Props) {
  const { t } = useTranslation()
  const summary = props.diagnostics?.summary
  const metrics = props.diagnostics?.metrics
  const packages = props.diagnostics?.packages ?? []

  return (
    <div className='space-y-4'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='min-w-0'>
          <h4 className='font-medium'>{t('npm CLI Version Diagnostics')}</h4>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Read-only cache status for CLI package versions, including recorded data and recent refresh failures.'
            )}
          </p>
        </div>
        <Button
          variant='outline'
          size='sm'
          onClick={props.onRefresh}
          disabled={props.loading}
        >
          <RefreshCcwIcon className='mr-2 h-4 w-4' />
          {props.loading ? t('Loading') : t('Refresh')}
        </Button>
      </div>

      {props.error ? (
        <div className='text-warning rounded-md border px-3 py-2 text-xs'>
          {props.error}
        </div>
      ) : (
        <>
          <div className='grid grid-cols-2 gap-3 text-xs md:grid-cols-4'>
            <DiagnosticStat
              label={t('Recorded packages')}
              value={`${summary?.recorded_count ?? 0} / ${summary?.package_count ?? 0}`}
            />
            <DiagnosticStat
              label={t('Missing packages')}
              value={summary?.missing_count ?? 0}
            />
            <DiagnosticStat
              label={t('Recent errors')}
              value={summary?.last_error_count ?? 0}
            />
            <DiagnosticStat
              label={t('Max cache age')}
              value={formatDuration(summary?.max_cache_age_ms)}
            />
          </div>

          <div className='grid gap-3 text-xs md:grid-cols-2'>
            <div className='rounded-lg border p-3'>
              <p className='mb-2 font-medium'>{t('Refresh metrics')}</p>
              <div className='grid grid-cols-2 gap-2'>
                <Field
                  label={t('Scheduled runs')}
                  value={metrics?.scheduled_runs ?? 0}
                />
                <Field
                  label={t('Scheduled success')}
                  value={metrics?.scheduled_successful_packages ?? 0}
                />
                <Field
                  label={t('Scheduled failed')}
                  value={metrics?.scheduled_failed_packages ?? 0}
                />
                <Field
                  label={t('Last scheduled')}
                  value={formatDate(metrics?.last_run_at)}
                />
                <Field
                  label={t('Manual runs')}
                  value={metrics?.manual_runs ?? 0}
                />
                <Field
                  label={t('Manual failed')}
                  value={metrics?.manual_failures ?? 0}
                />
                <Field
                  label={t('Last manual package')}
                  value={metrics?.last_manual_package || '-'}
                />
                <Field
                  label={t('Last manual code')}
                  value={metrics?.last_manual_code || '-'}
                />
              </div>
            </div>
            <div className='rounded-lg border p-3'>
              <p className='mb-2 font-medium'>{t('Refresh settings')}</p>
              <div className='grid grid-cols-2 gap-2'>
                <Field
                  label={t('Generated at')}
                  value={formatDate(props.diagnostics?.generated_at)}
                />
                <Field
                  label={t('Refresh interval')}
                  value={formatDuration(props.diagnostics?.refresh_interval_ms)}
                />
                <Field
                  label={t('Registry timeout')}
                  value={formatDuration(props.diagnostics?.registry_timeout_ms)}
                />
                <Field
                  label={t('Newest cache')}
                  value={formatDate(summary?.newest_refreshed_at)}
                />
                <Field
                  label={t('Oldest cache')}
                  value={formatDate(summary?.oldest_refreshed_at)}
                />
                <Field
                  label={t('Error scopes')}
                  value={`${summary?.process_last_error_count ?? 0} / ${summary?.recorded_last_error_count ?? 0}`}
                />
              </div>
            </div>
          </div>

          <div className='overflow-hidden rounded-lg border'>
            <div className='bg-muted/40 grid grid-cols-[minmax(0,1.25fr)_minmax(0,0.8fr)_minmax(0,0.8fr)_minmax(0,1.15fr)_minmax(0,1.5fr)] gap-2 px-3 py-2 text-xs font-medium'>
              <span>{t('Package')}</span>
              <span>{t('Source')}</span>
              <span>{t('Version')}</span>
              <span>{t('Recommended action')}</span>
              <span>{t('Last state')}</span>
            </div>
            {packages.length === 0 ? (
              <div className='text-muted-foreground px-3 py-3 text-xs'>
                {props.loading ? t('Loading') : t('No diagnostics')}
              </div>
            ) : (
              <div className='divide-y'>
                {packages.map((item) => (
                  <div
                    key={item.package}
                    className='grid grid-cols-[minmax(0,1.25fr)_minmax(0,0.8fr)_minmax(0,0.8fr)_minmax(0,1.15fr)_minmax(0,1.5fr)] gap-2 px-3 py-2 text-xs'
                  >
                    <span className='truncate font-medium'>{item.package}</span>
                    <span>
                      <SourceBadge source={item.source} />
                    </span>
                    <span className='text-muted-foreground truncate'>
                      {item.latest_version || '-'} / {item.option_count ?? 0}
                    </span>
                    <span
                      className='text-muted-foreground truncate'
                      title={formatRecommendedAction(
                        item.recommended_action,
                        t
                      )}
                    >
                      {formatRecommendedAction(item.recommended_action, t)}
                    </span>
                    <span
                      className={cn(
                        'min-w-0',
                        item.last_error
                          ? 'text-warning'
                          : 'text-muted-foreground'
                      )}
                      title={formatPackageStateWithHistory(item)}
                    >
                      <span className='block truncate'>
                        {formatPackageState(item)}
                      </span>
                      {item.recent_errors?.length ? (
                        <span className='text-muted-foreground block truncate'>
                          {t('History')}: {formatRecentErrors(item)}
                        </span>
                      ) : null}
                    </span>
                  </div>
                ))}
              </div>
            )}
          </div>
        </>
      )}
    </div>
  )
}

function DiagnosticStat(props: { label: string; value: string | number }) {
  return (
    <div className='rounded-lg border p-3'>
      <p className='text-muted-foreground'>{props.label}</p>
      <p className='mt-1 font-medium'>{props.value}</p>
    </div>
  )
}

function Field(props: { label: string; value: string | number }) {
  return (
    <div className='min-w-0'>
      <p className='text-muted-foreground truncate'>{props.label}</p>
      <p className='truncate font-medium'>{props.value}</p>
    </div>
  )
}

function SourceBadge(props: { source: string }) {
  const variant =
    props.source === 'missing'
      ? 'danger'
      : props.source === 'npm'
        ? 'success'
        : 'neutral'
  return (
    <StatusBadge variant={variant} copyable={false}>
      {props.source || '-'}
    </StatusBadge>
  )
}

function formatPackageState(item: NpmCLIVersionDiagnostic) {
  if (item.last_error) {
    return `${item.last_error_scope || item.last_error.source || 'error'}: ${item.last_error.code}`
  }
  if (item.refreshed_at) return formatDate(item.refreshed_at)
  return '-'
}

function formatPackageStateWithHistory(item: NpmCLIVersionDiagnostic) {
  const state = formatPackageState(item)
  const history = formatRecentErrors(item)
  if (!history) return state
  return `${state} | ${history}`
}

function formatRecommendedAction(
  action: string | undefined,
  t: (key: string) => string
) {
  switch (action) {
    case 'refresh_package':
      return t('Refresh package')
    case 'check_npm_registry_connectivity':
      return t('Check npm registry connectivity')
    case 'check_database_persistence':
      return t('Check database persistence')
    case 'inspect_registry_metadata':
      return t('Inspect registry metadata')
    case 'check_scheduler_or_master_node':
      return t('Check scheduler or master node')
    case 'none':
      return t('No action')
    default:
      return action || '-'
  }
}

function formatRecentErrors(item: NpmCLIVersionDiagnostic) {
  const recentErrors = item.recent_errors ?? []
  if (recentErrors.length === 0) return ''
  return recentErrors
    .map((error) => `${error.source || 'error'}:${error.code}`)
    .join(', ')
}

function formatDate(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return date.toLocaleString()
}

function formatDuration(value?: number) {
  if (!value || value < 0) return '-'
  if (value < 1000) return `${value} ms`
  const seconds = value / 1000
  if (seconds < 60) return `${seconds.toFixed(1)} s`
  const minutes = Math.floor(seconds / 60)
  const remainingSeconds = Math.round(seconds % 60)
  return `${minutes} m ${remainingSeconds} s`
}
